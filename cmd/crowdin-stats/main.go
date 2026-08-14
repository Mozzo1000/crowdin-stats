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
	db          *sql.DB
	masterKey   [32]byte
	baseURL     string
	noCache     bool
	noRateLimit bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "gendemo" {
		generateDemoSVGs()
		return
	}

	noCache := flag.Bool("no-cache", false, "disable the 12h embed cache — every embed request does a live Crowdin fetch (for testing)")
	noRateLimit := flag.Bool("no-ratelimit", false, "disable rate limiting on /setup and /setup/projects (for local testing)")
	insecureHTTP := flag.Bool("insecure-http", false, "build embed/setup URLs with http:// instead of https:// (for local testing without a TLS-terminating proxy in front)")
	flag.Parse()

	masterKey, err := loadMasterKey()
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}

	host := os.Getenv("HOST")
	if host == "" {
		slog.Error("startup failed", "error", "HOST environment variable must be set to the public hostname this instance is served from")
		os.Exit(1)
	}
	scheme := "https://"
	if *insecureHTTP {
		scheme = "http://"
	}
	baseURL := scheme + host

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

	s := &server{db: db, masterKey: masterKey, baseURL: baseURL, noCache: *noCache, noRateLimit: *noRateLimit}
	if s.noCache {
		slog.Warn("caching disabled — every embed request will hit Crowdin live (-no-cache)")
	}
	if s.noRateLimit {
		slog.Warn("rate limiting disabled on /setup and /setup/projects (-no-ratelimit)")
	}
	if *insecureHTTP {
		slog.Warn("building embed/setup URLs with http:// instead of https:// (-insecure-http)")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", servePage("index"))
	mux.HandleFunc("GET /setup", servePage("setup"))
	mux.HandleFunc("GET /terms", servePage("terms"))
	mux.HandleFunc("GET /privacy", servePage("privacy"))
	mux.HandleFunc("POST /setup", s.handleSetup)
	mux.HandleFunc("POST /setup/projects", s.handleListProjects)
	mux.HandleFunc("GET /embed/{publicID}/table.svg", s.handleTableEmbed)
	mux.HandleFunc("GET /embed/{publicID}/contributors.svg", s.handleContributorsEmbed)
	mux.HandleFunc("GET /embed/{publicID}/overall.svg", s.handleOverallEmbed)
	mux.HandleFunc("GET /embed/{publicID}/data.json", s.handleEmbedData)
	mux.HandleFunc("GET /revoke/{token}", servePage("revoke"))
	mux.HandleFunc("POST /revoke/{token}", s.handleRevoke)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(noDirListingFS{http.Dir("static")})))

	addr := ":8080"
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, requestLogger(mux)); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
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
	RevokeURL       string `json:"revoke_url"`
}

func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)

	ip := clientIP(r)
	if !s.noRateLimit {
		if limited, retryAfter := rateLimited(s.db, "setup:"+ip, 20, time.Hour); limited {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many setup attempts from this network — try again in "+formatRetryAfter(retryAfter), http.StatusTooManyRequests)
			return
		}
		if limited, retryAfter := rateLimitPeek(s.db, "setup-fail:"+ip, 5, time.Hour); limited {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many failed setup attempts from this network — try again in "+formatRetryAfter(retryAfter), http.StatusTooManyRequests)
			return
		}
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return
	}
	rawProjectID := req.CrowdinProjectID
	req.CrowdinProjectID = trimToDigits(rawProjectID)
	switch {
	case rawProjectID == "" && req.Token == "":
		http.Error(w, "crowdin_project_id and token are required", http.StatusBadRequest)
		return
	case rawProjectID == "":
		http.Error(w, "crowdin_project_id is required", http.StatusBadRequest)
		return
	case req.Token == "":
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if req.CrowdinProjectID == "" {
		// rawProjectID was non-empty but contained no digits at all — the
		// user pasted something, just not a project ID or project URL. Still
		// check the token here: if it's also invalid, say so instead of
		// implying the token is fine and only the project ID needs fixing —
		// otherwise the user "fixes" the ID and immediately hits a second,
		// previously-hidden error on the token.
		if _, err := ListProjects(ctx, req.Token); err != nil {
			if !s.noRateLimit {
				recordFailure(s.db, "setup-fail:"+ip, time.Hour)
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "couldn't find a project ID in that — paste the numeric project ID or the full Crowdin project URL", http.StatusBadRequest)
		return
	}

	if err := ValidateProject(ctx, req.Token, req.CrowdinProjectID); err != nil {
		if !s.noRateLimit {
			recordFailure(s.db, "setup-fail:"+ip, time.Hour)
		}
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

	revokeToken, revokeTokenHash, err := generateRevokeToken()
	if err != nil {
		slog.Error("generate revoke token failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	publicID := uuid.NewString()
	if err := insertProject(s.db, publicID, req.CrowdinProjectID, ciphertext, nonce, time.Now().Unix(), revokeTokenHash); err != nil {
		slog.Error("insert project failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	base := s.baseURL
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
		RevokeURL:       base + "/revoke/" + revokeToken,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(resp)
}

type listProjectsRequest struct {
	Token string `json:"token"`
}

type listProjectsResponse struct {
	Projects []ProjectSummary `json:"projects"`
}

// handleListProjects backs the onboarding project picker: given a token, it
// returns the projects that token can see. Rate limited more generously than
// /setup since the frontend calls it once per debounced pause in typing
// rather than once per full form submission.
func (s *server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)

	ip := clientIP(r)
	if !s.noRateLimit {
		if limited, retryAfter := rateLimited(s.db, "setup-projects:"+ip, 20, time.Hour); limited {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many attempts from this network — try again in "+formatRetryAfter(retryAfter), http.StatusTooManyRequests)
			return
		}
		if limited, retryAfter := rateLimitPeek(s.db, "setup-projects-fail:"+ip, 5, time.Hour); limited {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many failed attempts from this network — try again in "+formatRetryAfter(retryAfter), http.StatusTooManyRequests)
			return
		}
	}

	var req listProjectsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "malformed request body", http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	projects, err := ListProjects(ctx, req.Token)
	if err != nil {
		if !s.noRateLimit {
			recordFailure(s.db, "setup-projects-fail:"+ip, time.Hour)
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(listProjectsResponse{Projects: projects})
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
// params, falling back per-field to whichever base palette `theme` selects
// (light, the default, or dark) for anything missing or not a valid 3- or
// 6-digit hex color.
//
// When the request has neither a `theme` nor any individual color override,
// the returned colors are marked auto (see embedColors.auto): rather than
// silently defaulting to the light palette, the SVG embeds its own
// prefers-color-scheme rule and follows the viewer's OS/browser color
// scheme. Setting `theme` (even to `light`) or any single color opts out of
// that and pins the fixed palette a maintainer asked for.
func parseEmbedColors(q url.Values) embedColors {
	theme := q.Get("theme")
	base := defaultEmbedColors
	if theme == "dark" {
		base = darkEmbedColors
	}
	auto := theme == "" &&
		q.Get("bg") == "" && q.Get("text") == "" && q.Get("muted") == "" && q.Get("accent") == "" && q.Get("border") == ""
	return embedColors{
		bg:     sanitizeHexColor(q.Get("bg"), base.bg),
		text:   sanitizeHexColor(q.Get("text"), base.text),
		muted:  sanitizeHexColor(q.Get("muted"), base.muted),
		accent: sanitizeHexColor(q.Get("accent"), base.accent),
		border: sanitizeHexColor(q.Get("border"), base.border),
		auto:   auto,
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

// embedDataLanguage/embedDataContributor/embedDataResponse mirror the field
// names static/embed-builder.js's client-side renderers expect (matching its
// DEMO_LANGUAGES/DEMO_CONTRIBUTORS fixture shape), so the JS can feed real
// data straight into the same render functions it already uses for the demo
// preview.
type embedDataLanguage struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Percent           int    `json:"percent"`
	ApprovalPercent   int    `json:"approvalPercent"`
	WordsTotal        int    `json:"wordsTotal"`
	WordsTranslated   int    `json:"wordsTranslated"`
	WordsApproved     int    `json:"wordsApproved"`
	PhrasesTotal      int    `json:"phrasesTotal"`
	PhrasesTranslated int    `json:"phrasesTranslated"`
	PhrasesApproved   int    `json:"phrasesApproved"`
}

type embedDataContributor struct {
	Username string `json:"username"`
	FullName string `json:"fullName"`
	Amount   int64  `json:"amount"`
	Avatar   string `json:"avatar"`
}

type embedDataResponse struct {
	Languages    []embedDataLanguage    `json:"languages"`
	Contributors []embedDataContributor `json:"contributors"`
}

// handleEmbedData serves the raw Crowdin data (languages + contributors)
// behind the embed builder's live preview, decoupled from any
// color/limit/progress/etc. display parameter. Those are render-only and
// handled entirely client-side (see static/embed-builder.js); this endpoint
// only varies by `unit`/`hideOwner`, the two params that actually change
// which Crowdin report gets requested (see FetchTopMembers). That keeps the
// cache key space small — at most 4 rows per project — so tweaking colors or
// limits in the builder never burns the project's refresh-token budget
// (github.com/Mozzo1000/crowdin-stats/issues/1).
func (s *server) handleEmbedData(w http.ResponseWriter, r *http.Request) {
	publicID := r.PathValue("publicID")
	p, err := getProject(s.db, publicID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	unit := ReportUnit(r.URL.Query().Get("unit"))
	switch unit {
	case UnitWords, UnitStrings, UnitCharacters:
	default:
		unit = UnitWords
	}
	hideOwner := r.URL.Query().Get("hideOwner") == "true"

	cacheKey := "data:" + publicID + ":unit=" + string(unit) + ":hideOwner=" + strconv.FormatBool(hideOwner)
	body, err := getOrRefresh(r.Context(), s.db, cacheKey, publicID, s.fetchEmbedData(p, unit, hideOwner), s.noCache)
	if err != nil {
		s.handleEmbedDataError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte(body))
}

type revokeResponse struct {
	Status string `json:"status"` // "revoked" or "already_revoked"
}

// handleRevoke flips a project's revoked flag using the one-time secret
// token handed back at setup time (never the public embed publicID). It's
// idempotent: revoking an already-revoked project is not an error.
func (s *server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.noRateLimit {
		if limited, retryAfter := rateLimited(s.db, "revoke:"+ip, 20, time.Hour); limited {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many revoke attempts from this network — try again in "+formatRetryAfter(retryAfter), http.StatusTooManyRequests)
			return
		}
	}

	token := r.PathValue("token")
	publicID, revoked, err := getProjectByRevokeTokenHash(s.db, hashRevokeToken(token))
	if errors.Is(err, errProjectNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("lookup revoke token failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !revoked {
		if err := revokeProject(s.db, publicID); err != nil {
			slog.Error("revoke project failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := "revoked"
	if revoked {
		status = "already_revoked"
	}
	json.NewEncoder(w).Encode(revokeResponse{Status: status})
}

func (s *server) fetchEmbedData(p project, unit ReportUnit, hideOwner bool) fetchFunc {
	return func(ctx context.Context) (string, error) {
		token, err := decryptToken(s.masterKey, p.ciphertext, p.nonce)
		if err != nil {
			return "", err
		}
		langs, err := FetchLanguageProgress(ctx, token, p.crowdinProjectID)
		if err != nil {
			return "", err
		}
		// The builder preview slider varies `limit` entirely client-side
		// (see handleEmbedData's comment above), so this endpoint must
		// fetch avatars up to the largest limit the slider allows.
		contributors, err := FetchTopMembers(ctx, token, p.crowdinProjectID, unit, hideOwner, maxAvatarEmbeds)
		if err != nil {
			return "", err
		}

		resp := embedDataResponse{
			Languages:    make([]embedDataLanguage, len(langs)),
			Contributors: make([]embedDataContributor, len(contributors)),
		}
		for i, l := range langs {
			resp.Languages[i] = embedDataLanguage{
				ID:                l.LanguageID,
				Name:              l.LanguageName,
				Percent:           l.Percent,
				ApprovalPercent:   l.ApprovalPercent,
				WordsTotal:        l.WordsTotal,
				WordsTranslated:   l.WordsTranslated,
				WordsApproved:     l.WordsApproved,
				PhrasesTotal:      l.PhrasesTotal,
				PhrasesTranslated: l.PhrasesTranslated,
				PhrasesApproved:   l.PhrasesApproved,
			}
		}
		for i, c := range contributors {
			resp.Contributors[i] = embedDataContributor{
				Username: c.Username,
				FullName: c.FullName,
				Amount:   c.Amount,
				Avatar:   c.AvatarURL,
			}
		}

		b, err := json.Marshal(resp)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// handleEmbedDataError mirrors handleEmbedError's error classification but
// responds with JSON rather than a placeholder SVG, since this endpoint is
// consumed by fetch(), not <img src>.
func (s *server) handleEmbedDataError(w http.ResponseWriter, err error) {
	var status int
	var message string
	switch {
	case errors.Is(err, errRateLimited):
		status, message = http.StatusTooManyRequests, "rate limited, try again shortly"
	case errors.Is(err, errCrowdinAuthInvalid):
		slog.Warn("embed data fetch failed: token invalid", "error", err)
		status, message = http.StatusBadGateway, "token invalid — re-run setup"
	case errors.Is(err, errCrowdinProjectNotFound):
		slog.Warn("embed data fetch failed: project not found", "error", err)
		status, message = http.StatusBadGateway, "project not found — re-run setup"
	default:
		slog.Warn("embed data fetch failed", "error", err)
		status, message = http.StatusBadGateway, "temporarily unavailable"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// handleEmbedError always responds with a valid SVG image rather than a
// plain-text error body: these routes are consumed as <img src> in READMEs,
// and a non-image response renders as a broken image icon with no
// explanation. The real error is still logged server-side.
func (s *server) handleEmbedError(w http.ResponseWriter, err error, colors embedColors) {
	switch {
	case errors.Is(err, errRateLimited):
		writeSVGWithMaxAge(w, emptyStateSVG(320, 60, "rate limited, try again shortly", colors), errorSVGMaxAge)
	case errors.Is(err, errCrowdinAuthInvalid):
		slog.Warn("embed render failed: token invalid", "error", err)
		writeSVGWithMaxAge(w, emptyStateSVG(320, 60, "token invalid — re-run setup", colors), errorSVGMaxAge)
	case errors.Is(err, errCrowdinProjectNotFound):
		slog.Warn("embed render failed: project not found", "error", err)
		writeSVGWithMaxAge(w, emptyStateSVG(320, 60, "project not found — re-run setup", colors), errorSVGMaxAge)
	default:
		slog.Warn("embed render failed", "error", err)
		writeSVGWithMaxAge(w, emptyStateSVG(320, 60, "temporarily unavailable", colors), errorSVGMaxAge)
	}
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
		contributors, err := FetchTopMembers(ctx, token, p.crowdinProjectID, unit, hideOwner, limit)
		if err != nil {
			return "", err
		}
		return renderContributorsSVG(contributors, limit, colors), nil
	}
}

// noDirListingFS wraps an http.FileSystem so that opening a directory never
// falls through to http.FileServer's default behavior of rendering a
// directory listing — it 404s instead, since /static/ only ever needs to
// serve individual named files (fonts, demo images, etc).
type noDirListingFS struct {
	fs http.FileSystem
}

func (n noDirListingFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err == nil && info.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// errorSVGMaxAge is deliberately much shorter than the 300s success TTL:
// GitHub's camo proxy (and browsers) cache whatever we send here, so a long
// TTL on a "temporarily unavailable" placeholder would keep serving it long
// after a transient Crowdin API blip has cleared (github.com/Mozzo1000/crowdin-stats/issues/11).
const errorSVGMaxAge = 30

func writeSVG(w http.ResponseWriter, svg string) {
	writeSVGWithMaxAge(w, svg, 300)
}

func writeSVGWithMaxAge(w http.ResponseWriter, svg string, maxAgeSeconds int) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAgeSeconds))
	w.Write([]byte(svg))
}
