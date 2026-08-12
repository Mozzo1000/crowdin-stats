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
		if rateLimited(db, "bucket", 3, time.Hour) {
			t.Fatalf("unexpected rate limit at request %d", i)
		}
	}
	if !rateLimited(db, "bucket", 3, time.Hour) {
		t.Fatalf("expected rate limit after exceeding threshold")
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

	svg, err := getOrRefresh(ctx, db, "k1", "rk1", fetch)
	if err != nil {
		t.Fatalf("getOrRefresh cold: %v", err)
	}
	if svg != "<svg>fresh</svg>" || calls != 1 {
		t.Fatalf("expected 1 fetch on cold cache, got calls=%d svg=%s", calls, svg)
	}

	svg, err = getOrRefresh(ctx, db, "k1", "rk1", fetch)
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

	svg, err = getOrRefresh(ctx, db, "k1", "rk1", fetch)
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
