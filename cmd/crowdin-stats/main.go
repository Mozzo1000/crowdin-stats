package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type server struct {
	db        *sql.DB
	masterKey [32]byte
	noCache   bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "gendemo" {
		generateDemoSVGs()
		return
	}

	noCache := flag.Bool("no-cache", false, "disable the 12h embed cache — every embed request does a live Crowdin fetch (for testing)")
	flag.Parse()

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

	s := &server{db: db, masterKey: masterKey, noCache: *noCache}
	if s.noCache {
		slog.Warn("caching disabled — every embed request will hit Crowdin live (-no-cache)")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", serveStaticFile("static/index.html"))
	mux.HandleFunc("GET /setup", serveStaticFile("static/setup.html"))
	mux.HandleFunc("GET /terms", serveStaticFile("static/terms.html"))
	mux.HandleFunc("GET /privacy", serveStaticFile("static/privacy.html"))
	mux.HandleFunc("POST /setup", s.handleSetup)
	mux.HandleFunc("GET /embed/{publicID}/table.svg", s.handleTableEmbed)
	mux.HandleFunc("GET /embed/{publicID}/contributors.svg", s.handleContributorsEmbed)
	mux.HandleFunc("GET /embed/{publicID}/overall.svg", s.handleOverallEmbed)
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
	PublicID        string `json:"public_id"`
	EmbedBaseURL    string `json:"embed_base_url"`
	TableURL        string `json:"table_url"`
	ContributorsURL string `json:"contributors_url"`
	OverallURL      string `json:"overall_url"`
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
	overallURL := base + "/embed/" + publicID + "/overall.svg?unit=words&metric=both"
	resp := setupResponse{
		PublicID:        publicID,
		EmbedBaseURL:    base + "/embed/" + publicID,
		TableURL:        tableURL,
		ContributorsURL: contribURL,
		OverallURL:      overallURL,
		Markdown:        "![Translation Progress](" + tableURL + ")\n![Overall](" + overallURL + ")\n![Contributors](" + contribURL + ")",
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

// parseEmbedColors reads the shared bg/text/muted/accent/border query
// params, falling back to defaultEmbedColors per-field for anything
// missing or not a valid 3- or 6-digit hex color.
func parseEmbedColors(q url.Values) embedColors {
	return embedColors{
		bg:     sanitizeHexColor(q.Get("bg"), defaultEmbedColors.bg),
		text:   sanitizeHexColor(q.Get("text"), defaultEmbedColors.text),
		muted:  sanitizeHexColor(q.Get("muted"), defaultEmbedColors.muted),
		accent: sanitizeHexColor(q.Get("accent"), defaultEmbedColors.accent),
		border: sanitizeHexColor(q.Get("border"), defaultEmbedColors.border),
	}
}

// parseProgressType reads the `progress` query param shared by table.svg and
// overall.svg: translation (default) or approval. Both figures are already
// present on every fetched LanguageProgress, so this never needs its own
// Crowdin API call.
func parseProgressType(q url.Values) ProgressType {
	switch ProgressType(q.Get("progress")) {
	case ProgressApproval:
		return ProgressApproval
	default:
		return ProgressTranslation
	}
}

func (s *server) handleTableEmbed(w http.ResponseWriter, r *http.Request) {
	publicID := r.PathValue("publicID")
	p, err := getProject(s.db, publicID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	progressType := parseProgressType(q)

	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
		if limit < 1 {
			limit = 1
		}
		if limit > 200 {
			limit = 200
		}
	}

	minPercent := 0
	if v := q.Get("minPercent"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			minPercent = n
		}
		minPercent = clampPercent(minPercent)
	}

	pinned := parseLanguagePins(q.Get("languages"))

	colors := parseEmbedColors(q)

	cacheKey := "table:" + publicID +
		":progress=" + string(progressType) +
		":limit=" + strconv.Itoa(limit) +
		":minPercent=" + strconv.Itoa(minPercent) +
		":languages=" + pinnedCacheKeyFragment(pinned) +
		":" + colors.cacheKeyFragment()
	svg, err := getOrRefresh(r.Context(), s.db, cacheKey, publicID, s.renderTable(p, progressType, limit, minPercent, pinned, colors), s.noCache)
	if err != nil {
		s.handleEmbedError(w, err, colors)
		return
	}
	writeSVG(w, svg)
}

func (s *server) handleContributorsEmbed(w http.ResponseWriter, r *http.Request) {
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

	hideOwner := r.URL.Query().Get("hideOwner") == "true"
	colors := parseEmbedColors(r.URL.Query())

	cacheKey := "contrib:" + publicID + ":limit=" + strconv.Itoa(limit) + ":unit=" + string(unit) + ":hideOwner=" + strconv.FormatBool(hideOwner) + ":" + colors.cacheKeyFragment()
	svg, err := getOrRefresh(r.Context(), s.db, cacheKey, publicID, s.renderContributors(p, limit, unit, hideOwner, colors), s.noCache)
	if err != nil {
		s.handleEmbedError(w, err, colors)
		return
	}
	writeSVG(w, svg)
}

func (s *server) handleOverallEmbed(w http.ResponseWriter, r *http.Request) {
	publicID := r.PathValue("publicID")
	p, err := getProject(s.db, publicID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()

	unit := OverallUnit(q.Get("unit"))
	switch unit {
	case OverallUnitWords, OverallUnitStrings:
	default:
		unit = OverallUnitWords
	}

	progressType := parseProgressType(q)

	variant := q.Get("variant")
	if variant != "circle" {
		variant = "card"
	}

	// metric only applies to the card variant; normalized to a fixed
	// placeholder for circle so ?metric=percentage and ?metric=fraction
	// don't cache as separate (but visually identical) entries.
	metric := OverallMetric(q.Get("metric"))
	metricKey := string(metric)
	if variant == "circle" {
		metricKey = "n/a"
	} else {
		switch metric {
		case MetricPercentage, MetricFraction, MetricBoth:
		default:
			metric = MetricBoth
		}
		metricKey = string(metric)
	}

	colors := parseEmbedColors(q)

	cacheKey := "overall:" + publicID + ":unit=" + string(unit) + ":progress=" + string(progressType) + ":metric=" + metricKey + ":variant=" + variant + ":" + colors.cacheKeyFragment()
	svg, err := getOrRefresh(r.Context(), s.db, cacheKey, publicID, s.renderOverall(p, unit, metric, progressType, variant, colors), s.noCache)
	if err != nil {
		s.handleEmbedError(w, err, colors)
		return
	}
	writeSVG(w, svg)
}

// handleEmbedError always responds with a valid SVG image rather than a
// plain-text error body: these routes are consumed as <img src> in READMEs,
// and a non-image response renders as a broken image icon with no
// explanation. The real error is still logged server-side.
func (s *server) handleEmbedError(w http.ResponseWriter, err error, colors embedColors) {
	if errors.Is(err, errRateLimited) {
		writeSVG(w, emptyStateSVG(320, 60, "rate limited, try again shortly", colors))
		return
	}
	slog.Warn("embed render failed", "error", err)
	writeSVG(w, emptyStateSVG(320, 60, "temporarily unavailable", colors))
}

func (s *server) renderTable(p project, progressType ProgressType, limit, minPercent int, pinned map[string]bool, colors embedColors) fetchFunc {
	return func(ctx context.Context) (string, error) {
		token, err := decryptToken(s.masterKey, p.ciphertext, p.nonce)
		if err != nil {
			return "", err
		}
		langs, err := FetchLanguageProgress(ctx, token, p.crowdinProjectID)
		if err != nil {
			return "", err
		}
		langs = prepareTableLanguages(langs, progressType, minPercent, limit, pinned)
		return renderTableSVG(langs, colors), nil
	}
}

func (s *server) renderOverall(p project, unit OverallUnit, metric OverallMetric, progressType ProgressType, variant string, colors embedColors) fetchFunc {
	return func(ctx context.Context) (string, error) {
		token, err := decryptToken(s.masterKey, p.ciphertext, p.nonce)
		if err != nil {
			return "", err
		}
		langs, err := FetchLanguageProgress(ctx, token, p.crowdinProjectID)
		if err != nil {
			return "", err
		}
		if variant == "circle" {
			return renderOverallCircleSVG(langs, unit, progressType, colors), nil
		}
		return renderOverallCardSVG(langs, unit, metric, progressType, colors), nil
	}
}

func (s *server) renderContributors(p project, limit int, unit ReportUnit, hideOwner bool, colors embedColors) fetchFunc {
	return func(ctx context.Context) (string, error) {
		token, err := decryptToken(s.masterKey, p.ciphertext, p.nonce)
		if err != nil {
			return "", err
		}
		contributors, err := FetchTopMembers(ctx, token, p.crowdinProjectID, unit, hideOwner)
		if err != nil {
			return "", err
		}
		return renderContributorsSVG(contributors, limit, colors), nil
	}
}

func writeSVG(w http.ResponseWriter, svg string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte(svg))
}
