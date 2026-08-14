package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Regression tests for github.com/Mozzo1000/crowdin-stats/issues/2: colors,
// limit, progress, minPercent, pinned languages, metric, and variant are all
// pure render-time params and must never grow the cache table or vary the
// dataset cache key — only unit/hideOwner (contributors.svg) legitimately
// change what's fetched from Crowdin. Seed the dataset cache directly so
// these tests never make a real Crowdin API call.

func TestTableAndOverallEmbedShareDatasetCache(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-shared", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-shared"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	langs := []LanguageProgress{{
		LanguageID: "fr", LanguageName: "French", Percent: 80, ApprovalPercent: 60,
		WordsTotal: 100, WordsTranslated: 80, WordsApproved: 60,
		PhrasesTotal: 50, PhrasesTranslated: 40, PhrasesApproved: 30,
	}}
	b, err := json.Marshal(langs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	setCache(db, "langdata:pid-shared", string(b), cacheTTL)

	reqs := []struct {
		url     string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/embed/pid-shared/table.svg?bg=ff0000&limit=5&minPercent=10", s.handleTableEmbed},
		{"/embed/pid-shared/table.svg?bg=00ff00&limit=50&progress=approval&languages=fr,de", s.handleTableEmbed},
		{"/embed/pid-shared/overall.svg?unit=strings&variant=circle", s.handleOverallEmbed},
		{"/embed/pid-shared/overall.svg?metric=fraction&bg=0000ff&variant=card", s.handleOverallEmbed},
	}

	for i, tc := range reqs {
		r := httptest.NewRequest(http.MethodGet, tc.url, nil)
		r.SetPathValue("publicID", "pid-shared")
		w := httptest.NewRecorder()
		tc.handler(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d (%s): expected 200, got %d: %s", i, tc.url, w.Code, w.Body.String())
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cache WHERE key LIKE 'langdata:%'`).Scan(&count); err != nil {
		t.Fatalf("count cache rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 langdata cache row shared across table.svg/overall.svg regardless of render params, got %d", count)
	}
}

func TestContributorsEmbedCacheBoundedByUnitAndHideOwner(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-contrib", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-contrib"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	contributors := []Contributor{{Username: "amara", FullName: "Amara Okafor", AvatarURL: "", Amount: 100}}
	b, err := json.Marshal(contributors)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// limit=5 and limit=25 both round up to the 30-avatar tier, so seeding
	// that row alone must satisfy both requests below.
	setCache(db, "contrib-data:pid-contrib:unit=words:hideOwner=false:avatars=30", string(b), cacheTTL)

	urls := []string{
		"/embed/pid-contrib/contributors.svg?bg=ff0000&limit=5",
		"/embed/pid-contrib/contributors.svg?bg=00ff00&limit=25",
	}
	for _, u := range urls {
		r := httptest.NewRequest(http.MethodGet, u, nil)
		r.SetPathValue("publicID", "pid-contrib")
		w := httptest.NewRecorder()
		s.handleContributorsEmbed(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("request %s: expected 200, got %d: %s", u, w.Code, w.Body.String())
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cache WHERE key LIKE 'contrib-data:%'`).Scan(&count); err != nil {
		t.Fatalf("count cache rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 contrib-data cache row for limits sharing the same avatar tier, got %d", count)
	}
}

func TestContributorsEmbedCacheSeparatesAvatarTiers(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-tier", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-tier"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	contributors := []Contributor{{Username: "amara", FullName: "Amara Okafor", AvatarURL: "", Amount: 100}}
	b, err := json.Marshal(contributors)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	setCache(db, "contrib-data:pid-tier:unit=words:hideOwner=false:avatars=30", string(b), cacheTTL)
	setCache(db, "contrib-data:pid-tier:unit=words:hideOwner=false:avatars=100", string(b), cacheTTL)

	urls := []string{
		"/embed/pid-tier/contributors.svg?limit=5",   // 30-avatar tier
		"/embed/pid-tier/contributors.svg?limit=50",  // 100-avatar tier
		"/embed/pid-tier/contributors.svg?limit=100", // 100-avatar tier
	}
	for _, u := range urls {
		r := httptest.NewRequest(http.MethodGet, u, nil)
		r.SetPathValue("publicID", "pid-tier")
		w := httptest.NewRecorder()
		s.handleContributorsEmbed(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("request %s: expected 200, got %d: %s", u, w.Code, w.Body.String())
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cache WHERE key LIKE 'contrib-data:%'`).Scan(&count); err != nil {
		t.Fatalf("count cache rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 contrib-data cache rows (one per avatar tier actually used), got %d", count)
	}
}

// handleEmbedData must reuse the same dataset cache keys as the SVG routes
// rather than maintaining its own separate "data:" namespace.
func TestEmbedDataSharesDatasetCacheWithSVGRoutes(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db}

	if err := insertProject(db, "pid-json", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), "hash-json"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	langs := []LanguageProgress{{LanguageID: "fr", LanguageName: "French", Percent: 80}}
	lb, _ := json.Marshal(langs)
	setCache(db, "langdata:pid-json", string(lb), cacheTTL)

	contributors := []Contributor{{Username: "amara", Amount: 100}}
	cb, _ := json.Marshal(contributors)
	setCache(db, "contrib-data:pid-json:unit=words:hideOwner=false:avatars=100", string(cb), cacheTTL)

	r := httptest.NewRequest(http.MethodGet, "/embed/pid-json/data.json", nil)
	r.SetPathValue("publicID", "pid-json")
	w := httptest.NewRecorder()
	s.handleEmbedData(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cache`).Scan(&count); err != nil {
		t.Fatalf("count cache rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 cache rows (langdata + contrib-data, no separate data.json row), got %d", count)
	}
}
