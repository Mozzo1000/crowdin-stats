package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchAvatarDataURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake-png-bytes"))
		case "/wrong-type":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html></html>"))
		case "/too-big":
			w.Header().Set("Content-Type", "image/png")
			w.Write(make([]byte, maxAvatarBodyBytes+1))
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// httptest.Server serves plain HTTP; fetchAvatarDataURI requires https,
	// so exercise the https-only rule with a URL that's syntactically https
	// but never actually dialed (scheme check happens before any request).
	if got := fetchAvatarDataURI(context.Background(), "http://example.com/avatar.png"); got != "" {
		t.Fatalf("expected non-https URL to be rejected, got %q", got)
	}

	if got := fetchAvatarDataURI(context.Background(), srv.URL+"/ok.png"); got != "" {
		t.Fatalf("expected http test server URL to be rejected (https-only), got %q", got)
	}

	if got := fetchAvatarDataURI(context.Background(), "not a url"); got != "" {
		t.Fatalf("expected invalid URL to be rejected, got %q", got)
	}
}

func TestEmbedAvatarsAsDataURIs(t *testing.T) {
	// fetchAvatarDataURI enforces https, so exercising a genuine successful
	// fetch through embedAvatarsAsDataURIs needs a TLS server (covered by
	// TestFetchAvatarDataURIHTTPS instead); this test verifies the
	// no-avatar and invalid-URL pass-through paths, plus ordering.
	contributors := []Contributor{
		{Username: "b", Amount: 5},
		{Username: "a", Amount: 10, AvatarURL: ""},
		{Username: "c", Amount: 1, AvatarURL: "not-even-a-url"},
	}

	out := embedAvatarsAsDataURIs(context.Background(), contributors, 0)
	if len(out) != len(contributors) {
		t.Fatalf("expected %d contributors, got %d", len(contributors), len(out))
	}
	for _, c := range out {
		if c.AvatarURL != "" {
			t.Fatalf("expected empty/invalid avatar URLs to stay empty, got %q for %s", c.AvatarURL, c.Username)
		}
	}
	// Sorted by Amount descending: a(10), b(5), c(1).
	if out[0].Username != "a" || out[1].Username != "b" || out[2].Username != "c" {
		t.Fatalf("unexpected order: %v", []string{out[0].Username, out[1].Username, out[2].Username})
	}
}

func TestEmbedAvatarsAsDataURIsRespectsLimit(t *testing.T) {
	var fetched int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetched, 1)
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-bytes"))
	}))
	defer srv.Close()

	client := srv.Client()
	orig := http.DefaultClient
	http.DefaultClient = client
	defer func() { http.DefaultClient = orig }()

	contributors := make([]Contributor, 10)
	for i := range contributors {
		contributors[i] = Contributor{
			Username:  string(rune('a' + i)),
			Amount:    int64(10 - i),
			AvatarURL: srv.URL + "/avatar.png",
		}
	}

	const limit = 3
	out := embedAvatarsAsDataURIs(context.Background(), contributors, limit)

	if got := atomic.LoadInt32(&fetched); got != limit {
		t.Fatalf("expected exactly %d avatar fetches, got %d", limit, got)
	}
	for i, c := range out {
		if i < limit && c.AvatarURL == "" {
			t.Fatalf("expected top %d contributors to have avatars embedded, %s did not", limit, c.Username)
		}
		if i >= limit && c.AvatarURL != "" {
			t.Fatalf("expected contributors beyond limit %d to have no avatar, %s did", limit, c.Username)
		}
	}
}

func TestFetchAvatarDataURIHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-bytes"))
	}))
	defer srv.Close()

	client := srv.Client()
	orig := http.DefaultClient
	http.DefaultClient = client
	defer func() { http.DefaultClient = orig }()

	got := fetchAvatarDataURI(context.Background(), srv.URL+"/avatar.png")
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("expected a data URI, got %q", got)
	}
}
