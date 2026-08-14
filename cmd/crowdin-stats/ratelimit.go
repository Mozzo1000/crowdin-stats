package main

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// windowState reads bucketKey's current fixed-window row, if any, and
// reports whether it's still within window (vs. absent/expired, in which
// case callers should treat it as an empty window). Folds unexpected DB
// errors into live=false so callers fail open rather than blocking traffic.
func windowState(db *sql.DB, bucketKey string, window time.Duration) (count int, windowStart int64, live bool) {
	now := time.Now().Unix()
	cutoff := now - int64(window.Seconds())
	var c int
	var ws int64
	err := db.QueryRow(`SELECT count, window_start FROM rate_limits WHERE bucket_key = ?`,
		bucketKey).Scan(&c, &ws)
	if err != nil || ws < cutoff {
		return 0, now, false
	}
	return c, ws, true
}

func retryAfterFor(windowStart int64, window time.Duration) time.Duration {
	retryAfter := time.Duration(windowStart+int64(window.Seconds())-time.Now().Unix()) * time.Second
	if retryAfter < 0 {
		retryAfter = 0
	}
	return retryAfter
}

// rateLimited checks and increments a fixed-window counter for bucketKey.
// Returns true if the caller has exceeded limit requests within window,
// along with how long until the window resets and a new request would be
// admitted — callers surface that to the user instead of a bare "try again
// later" (see main.go handleSetup).
func rateLimited(db *sql.DB, bucketKey string, limit int, window time.Duration) (bool, time.Duration) {
	count, windowStart, live := windowState(db, bucketKey, window)
	if !live {
		db.Exec(`INSERT INTO rate_limits (bucket_key, count, window_start) VALUES (?, 1, ?)
                 ON CONFLICT(bucket_key) DO UPDATE SET count=1, window_start=excluded.window_start`,
			bucketKey, time.Now().Unix())
		return false, 0
	}
	if count >= limit {
		return true, retryAfterFor(windowStart, window)
	}
	db.Exec(`UPDATE rate_limits SET count = count + 1 WHERE bucket_key = ?`, bucketKey)
	return false, 0
}

// rateLimitPeek reports whether bucketKey is currently at/over limit without
// mutating any counter. Used to check a bucket the caller doesn't want to
// increment just by checking it — e.g. a failure bucket that should only
// grow once the request's outcome is known (see recordFailure).
func rateLimitPeek(db *sql.DB, bucketKey string, limit int, window time.Duration) (bool, time.Duration) {
	count, windowStart, live := windowState(db, bucketKey, window)
	if !live || count < limit {
		return false, 0
	}
	return true, retryAfterFor(windowStart, window)
}

// recordFailure unconditionally increments bucketKey's fixed-window counter
// (creating/resetting the window if expired), with no limit check. Callers
// enforce the limit separately via rateLimitPeek before doing the work whose
// outcome recordFailure is meant to capture.
func recordFailure(db *sql.DB, bucketKey string, window time.Duration) {
	_, _, live := windowState(db, bucketKey, window)
	if !live {
		db.Exec(`INSERT INTO rate_limits (bucket_key, count, window_start) VALUES (?, 1, ?)
                 ON CONFLICT(bucket_key) DO UPDATE SET count=1, window_start=excluded.window_start`,
			bucketKey, time.Now().Unix())
		return
	}
	db.Exec(`UPDATE rate_limits SET count = count + 1 WHERE bucket_key = ?`, bucketKey)
}

// formatRetryAfter renders a rate-limit reset delay the way a user reads
// time, not the way a computer does: rounded up to the nearest minute (or
// "less than a minute" below that), since promising the exact second implies
// a precision the fixed-window counter doesn't have.
func formatRetryAfter(d time.Duration) string {
	minutes := int(d.Round(time.Minute) / time.Minute)
	switch {
	case d <= 30*time.Second:
		return "less than a minute"
	case minutes <= 1:
		return "1 minute"
	default:
		return fmt.Sprintf("%d minutes", minutes)
	}
}

func startCleanupTicker(db *sql.DB, now func() int64) func() {
	ticker := time.NewTicker(time.Hour)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				n := now()
				db.Exec(`DELETE FROM rate_limits WHERE window_start < ?`, n-7200)
				db.Exec(`DELETE FROM cache WHERE expires_at < ?`, n-86400)
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}

// clientIP trusts X-Real-IP, which Caddy sets to the true connecting peer's
// address (overwriting, not appending to, any client-supplied value — see
// Caddyfile), falling back to RemoteAddr. X-Forwarded-For is NOT used here:
// Caddy appends to it rather than replacing it, so a client can prepend an
// arbitrary spoofed value that would land at parts[0].
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
