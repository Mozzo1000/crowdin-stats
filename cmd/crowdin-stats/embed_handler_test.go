package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Regression tests for issue #60: handleTableEmbed/handleContributorsEmbed
// had no httptest coverage for limit clamping, the unit whitelist fallback,
// or the invariant (see handleEmbedError, main.go) that every error path
// still responds with a valid SVG image rather than a plain-text error.

func TestHandleTableEmbedClampsLimit(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-table-limit", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-table-limit"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	var langs []LanguageProgress
	for i := 0; i < 250; i++ {
		langs = append(langs, LanguageProgress{
			LanguageID:   fmt.Sprintf("l%d", i),
			LanguageName: fmt.Sprintf("Language %03d", i),
			Percent:      i % 100,
		})
	}
	b, err := json.Marshal(langs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	setCache(db, "langdata:pid-table-limit", string(b), cacheTTL)

	rowCount := func(body string) int {
		return strings.Count(body, `font-size="12" dominant-baseline="middle">`)
	}

	cases := []struct {
		name string
		url  string
		want int
	}{
		{"limit above the 200 cap is clamped down", "/embed/pid-table-limit/table.svg?limit=100000", 200},
		{"limit of 0 is clamped up to 1", "/embed/pid-table-limit/table.svg?limit=0", 1},
		{"negative limit is clamped up to 1", "/embed/pid-table-limit/table.svg?limit=-5", 1},
		{"limit within range is passed through unchanged", "/embed/pid-table-limit/table.svg?limit=7", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.url, nil)
			r.SetPathValue("publicID", "pid-table-limit")
			w := httptest.NewRecorder()
			s.handleTableEmbed(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if got := rowCount(w.Body.String()); got != tc.want {
				t.Fatalf("rendered %d language rows, want %d", got, tc.want)
			}
		})
	}
}

func TestHandleContributorsEmbedClampsLimit(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-contrib-limit", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-contrib-limit"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	var contributors []Contributor
	for i := 0; i < 150; i++ {
		contributors = append(contributors, Contributor{
			Username: fmt.Sprintf("user%03d", i),
			FullName: fmt.Sprintf("User %03d", i),
			Amount:   int64(150 - i),
		})
	}
	b, err := json.Marshal(contributors)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Seed both avatar tiers so requests landing in either one hit cache
	// rather than attempting a real Crowdin fetch.
	setCache(db, "contrib-data:pid-contrib-limit:unit=words:hideOwner=false:avatars=30", string(b), cacheTTL)
	setCache(db, "contrib-data:pid-contrib-limit:unit=words:hideOwner=false:avatars=100", string(b), cacheTTL)

	avatarCount := func(body string) int {
		return strings.Count(body, "<clipPath id=")
	}

	cases := []struct {
		name string
		url  string
		want int
	}{
		{"limit above the 100 cap is clamped down", "/embed/pid-contrib-limit/contributors.svg?limit=100000", 100},
		{"limit of 0 is clamped up to 1", "/embed/pid-contrib-limit/contributors.svg?limit=0", 1},
		{"negative limit is clamped up to 1", "/embed/pid-contrib-limit/contributors.svg?limit=-5", 1},
		{"limit within range is passed through unchanged", "/embed/pid-contrib-limit/contributors.svg?limit=12", 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.url, nil)
			r.SetPathValue("publicID", "pid-contrib-limit")
			w := httptest.NewRecorder()
			s.handleContributorsEmbed(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if got := avatarCount(w.Body.String()); got != tc.want {
				t.Fatalf("rendered %d avatars, want %d", got, tc.want)
			}
		})
	}
}

// An unrecognized `unit` must fall back to words rather than bubbling up as
// an error — verified end-to-end by seeding the dataset cache only under
// the words-tier key and confirming the handler still serves it (a miss
// would instead hit fetchContributorData, which fails against this test's
// bogus ciphertext/nonce and would render an error placeholder).
func TestHandleContributorsEmbedUnitWhitelistFallsBackToWords(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-unit", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-unit"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	contributors := []Contributor{{Username: "amara", FullName: "Amara Okafor", Amount: 100}}
	b, err := json.Marshal(contributors)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	setCache(db, "contrib-data:pid-unit:unit=words:hideOwner=false:avatars=30", string(b), cacheTTL)

	r := httptest.NewRequest(http.MethodGet, "/embed/pid-unit/contributors.svg?unit=bogus&limit=5", nil)
	r.SetPathValue("publicID", "pid-unit")
	w := httptest.NewRecorder()
	s.handleContributorsEmbed(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Amara Okafor") {
		t.Fatalf("expected the seeded words-tier contributor to be rendered (unit should fall back to words), got: %s", w.Body.String())
	}
}

// Every embed error path — an unknown publicID, or a downstream dataset
// fetch failure — must still respond with a real SVG image, never a
// plain-text error body, since these routes are consumed as <img src> in
// READMEs.
func TestEmbedHandlersAlwaysReturnValidSVGOnError(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-bad-token", "12345", []byte("not-valid-ciphertext"), []byte("bad-nonce"), time.Now().Unix(), "hash-bad-token"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	cases := []struct {
		name    string
		url     string
		handler func(http.ResponseWriter, *http.Request)
		pathID  string
	}{
		{"table.svg: unknown publicID", "/embed/does-not-exist/table.svg", s.handleTableEmbed, "does-not-exist"},
		{"contributors.svg: unknown publicID", "/embed/does-not-exist/contributors.svg", s.handleContributorsEmbed, "does-not-exist"},
		{"table.svg: downstream fetch failure", "/embed/pid-bad-token/table.svg", s.handleTableEmbed, "pid-bad-token"},
		{"contributors.svg: downstream fetch failure", "/embed/pid-bad-token/contributors.svg", s.handleContributorsEmbed, "pid-bad-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.url, nil)
			r.SetPathValue("publicID", tc.pathID)
			w := httptest.NewRecorder()
			tc.handler(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 (placeholder SVGs are served as normal images), got %d", w.Code)
			}
			if ct := w.Header().Get("Content-Type"); ct != "image/svg+xml" {
				t.Fatalf("Content-Type = %q, want image/svg+xml", ct)
			}
			body := w.Body.String()
			if !strings.HasPrefix(body, "<svg") {
				t.Fatalf("expected body to start with <svg, got: %s", body)
			}
			if !strings.HasSuffix(strings.TrimSpace(body), "</svg>") {
				t.Fatalf("expected body to end with </svg>, got: %s", body)
			}
		})
	}
}
