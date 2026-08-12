package main

import (
	"database/sql"
	"net"
	"net/http"
	"strings"
	"time"
)

// rateLimited checks and increments a fixed-window counter for bucketKey.
// Returns true if the caller has exceeded limit requests within window.
func rateLimited(db *sql.DB, bucketKey string, limit int, window time.Duration) bool {
	now := time.Now().Unix()
	windowStart := now - int64(window.Seconds())

	var count int
	var storedStart int64
	err := db.QueryRow(`SELECT count, window_start FROM rate_limits WHERE bucket_key = ?`,
		bucketKey).Scan(&count, &storedStart)

	if err == sql.ErrNoRows || storedStart < windowStart {
		db.Exec(`INSERT INTO rate_limits (bucket_key, count, window_start) VALUES (?, 1, ?)
                 ON CONFLICT(bucket_key) DO UPDATE SET count=1, window_start=excluded.window_start`,
			bucketKey, now)
		return false
	}
	if err != nil {
		// Fail open on unexpected DB errors rather than blocking all traffic.
		return false
	}
	if count >= limit {
		return true
	}
	db.Exec(`UPDATE rate_limits SET count = count + 1 WHERE bucket_key = ?`, bucketKey)
	return false
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

// clientIP trusts X-Forwarded-For (set by Caddy), falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
