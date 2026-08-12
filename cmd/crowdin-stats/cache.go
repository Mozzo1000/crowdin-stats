package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	cacheTTL = 12 * time.Hour
)

var refreshGroup singleflight.Group

type cacheEntry struct {
	svg       string
	cachedAt  int64
	expiresAt int64
}

func getCache(db *sql.DB, key string) (cacheEntry, bool) {
	var e cacheEntry
	err := db.QueryRow(`SELECT svg, cached_at, expires_at FROM cache WHERE key = ?`, key).
		Scan(&e.svg, &e.cachedAt, &e.expiresAt)
	if err != nil {
		return cacheEntry{}, false
	}
	return e, true
}

func setCache(db *sql.DB, key, svg string, ttl time.Duration) {
	now := time.Now().Unix()
	expires := time.Now().Add(ttl).Unix()
	db.Exec(`INSERT INTO cache (key, svg, cached_at, expires_at) VALUES (?, ?, ?, ?)
             ON CONFLICT(key) DO UPDATE SET svg=excluded.svg, cached_at=excluded.cached_at, expires_at=excluded.expires_at`,
		key, svg, now, expires)
}

// fetchFunc produces a freshly rendered SVG for a cache key.
type fetchFunc func(ctx context.Context) (string, error)

// getOrRefresh implements the stale-while-revalidate policy described in
// PLAN.md §7: fresh cache serves immediately; stale cache serves immediately
// while a background refresh is kicked off (subject to refreshRateKey);
// a cold cache blocks on a live fetch, subject to the same rate limit.
//
// ctx bounds only the blocking cold-cache fetch. Background refreshes run
// detached from the triggering request's context (which is canceled the
// moment that request's response is written) with their own timeout.
//
// When disabled is true (the -no-cache flag), the cache table is never read
// or written — every call does a live, rate-limited fetch, for testing
// against real Crowdin data without waiting out the 12h TTL.
func getOrRefresh(ctx context.Context, db *sql.DB, key, refreshRateKey string, fetch fetchFunc, disabled bool) (string, error) {
	if disabled {
		if rateLimited(db, "refresh:"+refreshRateKey, 20, time.Hour) {
			return "", errRateLimited
		}
		return fetch(ctx)
	}

	entry, found := getCache(db, key)
	now := time.Now().Unix()

	if found && now < entry.expiresAt {
		return entry.svg, nil
	}

	if found {
		// Stale: serve what we have, refresh in the background if allowed.
		if !rateLimited(db, "refresh:"+refreshRateKey, 20, time.Hour) {
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				_, _, _ = refreshGroup.Do(key, func() (interface{}, error) {
					svg, err := fetch(bgCtx)
					if err != nil {
						slog.Warn("background refresh failed", "key", key, "error", err)
						return nil, err
					}
					setCache(db, key, svg, cacheTTL)
					return svg, nil
				})
			}()
		}
		return entry.svg, nil
	}

	// Cold cache: block on a live fetch, still rate-limited.
	if rateLimited(db, "refresh:"+refreshRateKey, 20, time.Hour) {
		return "", errRateLimited
	}

	v, err, _ := refreshGroup.Do(key, func() (interface{}, error) {
		svg, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		setCache(db, key, svg, cacheTTL)
		return svg, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}
