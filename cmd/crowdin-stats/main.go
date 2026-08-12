package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type server struct {
	db        *sql.DB
	masterKey [32]byte
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "gendemo" {
		generateDemoSVGs()
		return
	}

	masterKey, err := loadMasterKey()
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/db.sqlite"
	}

	db, err := openDB(dbPath)
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	stopCleanup := startCleanupTicker(db, func() int64 { return time.Now().Unix() })
	defer stopCleanup()

	s := &server{db: db, masterKey: masterKey}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", serveStaticFile("static/index.html"))
	mux.HandleFunc("GET /setup", serveStaticFile("static/setup.html"))
	mux.HandleFunc("GET /terms", serveStaticFile("static/terms.html"))
	mux.HandleFunc("GET /privacy", serveStaticFile("static/privacy.html"))
	mux.HandleFunc("POST /setup", s.handleSetup)
	mux.HandleFunc("GET /embed/{publicID}/table.svg", s.handleTableBadge)
	mux.HandleFunc("GET /embed/{publicID}/contributors.svg", s.handleContributorsBadge)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	addr := ":8080"
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, requestLogger(mux)); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func serveStaticFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

// requestLogger logs method, path, status, and duration only — never bodies —
// and skips the query string on /setup so a token pasted as a query param
// (misuse, but defensively) never lands in logs either.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type setupRequest struct {
	CrowdinProjectID string `json:"crowdin_project_id"`
	Token            string `json:"token"`
}

type setupResponse struct {
	TableURL        string `json:"table_url"`
	ContributorsURL string `json:"contributors_url"`
	Markdown        string `json:"markdown"`
}

func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)

	ip := clientIP(r)
	if rateLimited(s.db, "setup:"+ip, 5, time.Hour) {
		http.Error(w, "too many setup attempts, try again later", http.StatusTooManyRequests)
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return
	}
	req.CrowdinProjectID = trimToDigits(req.CrowdinProjectID)
	if req.CrowdinProjectID == "" || req.Token == "" {
		http.Error(w, "crowdin_project_id and token are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := ValidateProject(ctx, req.Token, req.CrowdinProjectID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ciphertext, nonce, err := encryptToken(s.masterKey, req.Token)
	if err != nil {
		slog.Error("encrypt token failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Token = "" // drop plaintext reference immediately after encryption

	publicID := uuid.NewString()
	if err := insertProject(s.db, publicID, req.CrowdinProjectID, ciphertext, nonce, time.Now().Unix()); err != nil {
		slog.Error("insert project failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	base := hostBaseURL(r)
	tableURL := base + "/embed/" + publicID + "/table.svg"
	contribURL := base + "/embed/" + publicID + "/contributors.svg?limit=30&unit=words"
	resp := setupResponse{
		TableURL:        tableURL,
		ContributorsURL: contribURL,
		Markdown:        "![Translation Progress](" + tableURL + ")\n![Contributors](" + contribURL + ")",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func hostBaseURL(r *http.Request) string {
	if h := os.Getenv("HOST"); h != "" {
		return "https://" + h
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func trimToDigits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			out = append(out, c)
		}
	}
	return string(out)
}

func (s *server) handleTableBadge(w http.ResponseWriter, r *http.Request) {
	publicID := r.PathValue("publicID")
	p, err := getProject(s.db, publicID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	svg, err := getOrRefresh(r.Context(), s.db, "table:"+publicID, publicID, s.renderTable(p))
	if err != nil {
		s.handleBadgeError(w, err)
		return
	}
	writeSVG(w, svg)
}

func (s *server) handleContributorsBadge(w http.ResponseWriter, r *http.Request) {
	publicID := r.PathValue("publicID")
	p, err := getProject(s.db, publicID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	unit := ReportUnit(r.URL.Query().Get("unit"))
	switch unit {
	case UnitWords, UnitStrings, UnitCharacters:
	default:
		unit = UnitWords
	}

	cacheKey := "contrib:" + publicID + ":limit=" + strconv.Itoa(limit) + ":unit=" + string(unit)
	svg, err := getOrRefresh(r.Context(), s.db, cacheKey, publicID, s.renderContributors(p, limit, unit))
	if err != nil {
		s.handleBadgeError(w, err)
		return
	}
	writeSVG(w, svg)
}

// handleBadgeError always responds with a valid SVG image rather than a
// plain-text error body: these routes are consumed as <img src> in READMEs,
// and a non-image response renders as a broken image icon with no
// explanation. The real error is still logged server-side.
func (s *server) handleBadgeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errRateLimited) {
		writeSVG(w, emptyStateSVG(320, 60, "rate limited, try again shortly"))
		return
	}
	slog.Warn("badge render failed", "error", err)
	writeSVG(w, emptyStateSVG(320, 60, "temporarily unavailable"))
}

func (s *server) renderTable(p project) fetchFunc {
	return func(ctx context.Context) (string, error) {
		token, err := decryptToken(s.masterKey, p.ciphertext, p.nonce)
		if err != nil {
			return "", err
		}
		langs, err := FetchLanguageProgress(ctx, token, p.crowdinProjectID)
		if err != nil {
			return "", err
		}
		return renderTableSVG(langs), nil
	}
}

func (s *server) renderContributors(p project, limit int, unit ReportUnit) fetchFunc {
	return func(ctx context.Context) (string, error) {
		token, err := decryptToken(s.masterKey, p.ciphertext, p.nonce)
		if err != nil {
			return "", err
		}
		contributors, err := FetchTopMembers(ctx, token, p.crowdinProjectID, unit)
		if err != nil {
			return "", err
		}
		return renderContributorsSVG(contributors, limit), nil
	}
}

func writeSVG(w http.ResponseWriter, svg string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte(svg))
}
