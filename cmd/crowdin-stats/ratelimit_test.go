package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name       string
		xRealIP    string
		remoteAddr string
		want       string
	}{
		{"X-Real-IP set by Caddy takes precedence", "203.0.113.7", "10.0.0.1:54321", "203.0.113.7"},
		{"falls back to RemoteAddr host when no X-Real-IP", "", "203.0.113.9:54321", "203.0.113.9"},
		{"RemoteAddr without a port is returned as-is", "", "not-host-port", "not-host-port"},
		{"whitespace-only X-Real-IP is ignored", "   ", "203.0.113.9:54321", "203.0.113.9"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remoteAddr
		if c.xRealIP != "" {
			r.Header.Set("X-Real-IP", c.xRealIP)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("%s: clientIP() = %q, want %q", c.name, got, c.want)
		}
	}
}

// A client-supplied X-Forwarded-For must never be trusted: Caddy appends to
// it rather than replacing it, so a client can prepend an arbitrary spoofed
// value (see clientIP's doc comment, and fixes #15).
func TestClientIPIgnoresForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")

	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("clientIP() = %q, want RemoteAddr host %q (X-Forwarded-For must be ignored)", got, "203.0.113.9")
	}
}
