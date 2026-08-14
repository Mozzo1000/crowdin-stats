package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestProjectRoundTrip(t *testing.T) {
	db := newTestDB(t)

	if err := insertProject(db, "pid-1", "12345", []byte("ct"), []byte("nonce-24-bytes-000000000"), time.Now().Unix()); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	p, err := getProject(db, "pid-1")
	if err != nil {
		t.Fatalf("getProject: %v", err)
	}
	if p.crowdinProjectID != "12345" {
		t.Fatalf("expected project id 12345, got %s", p.crowdinProjectID)
	}

	if _, err := getProject(db, "does-not-exist"); err != errProjectNotFound {
		t.Fatalf("expected errProjectNotFound, got %v", err)
	}
}

func TestGetProjectRevoked(t *testing.T) {
	db := newTestDB(t)
	if err := insertProject(db, "pid-2", "999", []byte("ct"), []byte("n"), time.Now().Unix()); err != nil {
		t.Fatalf("insertProject: %v", err)
	}
	if _, err := db.Exec(`UPDATE projects SET revoked = 1 WHERE public_id = ?`, "pid-2"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := getProject(db, "pid-2"); err != errProjectNotFound {
		t.Fatalf("expected errProjectNotFound for revoked project, got %v", err)
	}
}

func TestRateLimited(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 3; i++ {
		if limited, _ := rateLimited(db, "bucket", 3, time.Hour); limited {
			t.Fatalf("unexpected rate limit at request %d", i)
		}
	}
	limited, retryAfter := rateLimited(db, "bucket", 3, time.Hour)
	if !limited {
		t.Fatalf("expected rate limit after exceeding threshold")
	}
	if retryAfter <= 0 || retryAfter > time.Hour {
		t.Fatalf("expected retryAfter within (0, 1h], got %v", retryAfter)
	}
}

func TestRateLimitPeek(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 5; i++ {
		if limited, retryAfter := rateLimitPeek(db, "bucket", 3, time.Hour); limited || retryAfter != 0 {
			t.Fatalf("peek %d: expected (false, 0) below limit, got (%v, %v)", i, limited, retryAfter)
		}
	}

	for i := 0; i < 3; i++ {
		recordFailure(db, "bucket", time.Hour)
	}

	for i := 0; i < 3; i++ {
		limited, retryAfter := rateLimitPeek(db, "bucket", 3, time.Hour)
		if !limited {
			t.Fatalf("peek %d: expected limited once count reaches limit", i)
		}
		if retryAfter <= 0 || retryAfter > time.Hour {
			t.Fatalf("peek %d: expected retryAfter within (0, 1h], got %v", i, retryAfter)
		}
	}

	if limited, _ := rateLimitPeek(db, "bucket", 4, time.Hour); limited {
		t.Fatalf("expected count of 3 to stay under a higher limit — peek must not have incremented the counter")
	}
}

func TestRecordFailure(t *testing.T) {
	db := newTestDB(t)
	for i := 1; i <= 4; i++ {
		recordFailure(db, "bucket", time.Hour)
		if limited, _ := rateLimitPeek(db, "bucket", i, time.Hour); !limited {
			t.Fatalf("after %d recordFailure calls, expected count to have reached limit %d", i, i)
		}
		if limited, _ := rateLimitPeek(db, "bucket", i+1, time.Hour); limited {
			t.Fatalf("after %d recordFailure calls, expected count to still be under limit %d", i, i+1)
		}
	}
}

func TestRecordFailureWindowExpiry(t *testing.T) {
	db := newTestDB(t)
	staleStart := time.Now().Add(-2 * time.Hour).Unix()
	if _, err := db.Exec(`INSERT INTO rate_limits (bucket_key, count, window_start) VALUES (?, ?, ?)`,
		"bucket", 10, staleStart); err != nil {
		t.Fatalf("seed stale row: %v", err)
	}

	recordFailure(db, "bucket", time.Hour)

	if limited, _ := rateLimitPeek(db, "bucket", 1, time.Hour); !limited {
		t.Fatalf("expected count to be reset to 1 after a stale window, so limit=1 should already be reached")
	}
	if limited, _ := rateLimitPeek(db, "bucket", 2, time.Hour); limited {
		t.Fatalf("expected count to be exactly 1 after window reset, not still 10")
	}
}

func TestGeneralAndFailureBucketsIndependent(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 3; i++ {
		if limited, _ := rateLimited(db, "general", 3, time.Hour); limited {
			t.Fatalf("unexpected rate limit on general bucket at request %d", i)
		}
	}
	if limited, _ := rateLimited(db, "general", 3, time.Hour); !limited {
		t.Fatalf("expected general bucket to be limited after exceeding threshold")
	}

	if limited, _ := rateLimitPeek(db, "failure", 3, time.Hour); limited {
		t.Fatalf("expected failure bucket to be untouched by general bucket traffic")
	}
}

func TestCacheStaleWhileRevalidate(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	calls := 0
	fetch := func(ctx context.Context) (string, error) {
		calls++
		return "<svg>fresh</svg>", nil
	}

	svg, err := getOrRefresh(ctx, db, "k1", "rk1", fetch, false)
	if err != nil {
		t.Fatalf("getOrRefresh cold: %v", err)
	}
	if svg != "<svg>fresh</svg>" || calls != 1 {
		t.Fatalf("expected 1 fetch on cold cache, got calls=%d svg=%s", calls, svg)
	}

	svg, err = getOrRefresh(ctx, db, "k1", "rk1", fetch, false)
	if err != nil {
		t.Fatalf("getOrRefresh warm: %v", err)
	}
	if svg != "<svg>fresh</svg>" || calls != 1 {
		t.Fatalf("expected no extra fetch on fresh cache hit, got calls=%d", calls)
	}

	// force staleness
	if _, err := db.Exec(`UPDATE cache SET expires_at = ? WHERE key = ?`, time.Now().Add(-time.Minute).Unix(), "k1"); err != nil {
		t.Fatalf("force stale: %v", err)
	}

	svg, err = getOrRefresh(ctx, db, "k1", "rk1", fetch, false)
	if err != nil {
		t.Fatalf("getOrRefresh stale: %v", err)
	}
	if svg != "<svg>fresh</svg>" {
		t.Fatalf("expected stale value served immediately, got %s", svg)
	}
	// background refresh is async; give it a moment
	time.Sleep(100 * time.Millisecond)
	if calls != 2 {
		t.Fatalf("expected background refresh to have run, calls=%d", calls)
	}
}

func TestCacheDisabled(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	calls := 0
	fetch := func(ctx context.Context) (string, error) {
		calls++
		return "<svg>fresh</svg>", nil
	}

	for i := 0; i < 3; i++ {
		svg, err := getOrRefresh(ctx, db, "k1", "rk1", fetch, true)
		if err != nil {
			t.Fatalf("getOrRefresh disabled: %v", err)
		}
		if svg != "<svg>fresh</svg>" {
			t.Fatalf("unexpected svg: %s", svg)
		}
	}
	if calls != 3 {
		t.Fatalf("expected every call to fetch live with caching disabled, got calls=%d", calls)
	}
	if _, found := getCache(db, "k1"); found {
		t.Fatalf("expected cache table to stay untouched when disabled")
	}
}
