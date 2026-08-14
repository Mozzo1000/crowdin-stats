package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Error/placeholder SVGs must carry a much shorter Cache-Control than
// successful renders: GitHub's camo proxy caches whatever we send, and a
// long TTL on a "temporarily unavailable" placeholder would keep serving it
// long after a transient upstream blip has cleared (issue #11).
func TestHandleEmbedErrorUsesShortCacheTTL(t *testing.T) {
	s := &server{}
	w := httptest.NewRecorder()

	s.handleEmbedError(w, errRateLimited, defaultEmbedColors)

	got := w.Header().Get("Cache-Control")
	want := "public, max-age=30"
	if got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (placeholder SVGs are served as normal images), got %d", w.Code)
	}
}

func TestWriteSVGDefaultsToLongCacheTTL(t *testing.T) {
	w := httptest.NewRecorder()
	writeSVG(w, "<svg></svg>")

	got := w.Header().Get("Cache-Control")
	want := "public, max-age=300"
	if got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
}
