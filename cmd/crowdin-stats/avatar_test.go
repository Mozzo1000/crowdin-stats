package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAvatarLimitTier(t *testing.T) {
	cases := []struct {
		limit int
		want  int
	}{
		{1, 30},
		{30, 30},
		{31, maxAvatarEmbeds},
		{100, maxAvatarEmbeds},
		{500, maxAvatarEmbeds},
	}
	for _, c := range cases {
		if got := avatarLimitTier(c.limit); got != c.want {
			t.Errorf("avatarLimitTier(%d) = %d, want %d", c.limit, got, c.want)
		}
	}
}

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
	if got := fetchAvatarDataURI(context.Background(), avatarHTTPClient, avatarHostAllowed, "http://example.com/avatar.png"); got != "" {
		t.Fatalf("expected non-https URL to be rejected, got %q", got)
	}

	if got := fetchAvatarDataURI(context.Background(), avatarHTTPClient, avatarHostAllowed, srv.URL+"/ok.png"); got != "" {
		t.Fatalf("expected http test server URL to be rejected (https-only), got %q", got)
	}

	if got := fetchAvatarDataURI(context.Background(), avatarHTTPClient, avatarHostAllowed, "not a url"); got != "" {
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

	contributors := make([]Contributor, 10)
	for i := range contributors {
		contributors[i] = Contributor{
			Username:  string(rune('a' + i)),
			Amount:    int64(10 - i),
			AvatarURL: srv.URL + "/avatar.png",
		}
	}

	const limit = 3
	out := embedAvatarsAsDataURIsWith(context.Background(), srv.Client(), func(string) bool { return true }, contributors, limit)

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

	got := fetchAvatarDataURI(context.Background(), srv.Client(), func(string) bool { return true }, srv.URL+"/avatar.png")
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("expected a data URI, got %q", got)
	}
}

func TestFetchAvatarDataURIBlocksDisallowedHost(t *testing.T) {
	if got := fetchAvatarDataURI(context.Background(), avatarHTTPClient, avatarHostAllowed, "https://evil.example.com/avatar.png"); got != "" {
		t.Fatalf("expected non-Crowdin host to be rejected, got %q", got)
	}
}

func TestAvatarHostAllowed(t *testing.T) {
	cases := map[string]bool{
		"crowdin-static.cf-downloads.crowdin.com": true,
		"crowdin.com":                       true,
		"evil.crowdin.com.attacker.example": false,
		"notcrowdin.com":                    false,
		"attacker.example":                  false,
	}
	for host, want := range cases {
		if got := avatarHostAllowed(host); got != want {
			t.Errorf("avatarHostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestFetchAvatarDataURIBlocksLoopback(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-bytes"))
	}))
	defer srv.Close()

	// Bypass the host allowlist so this test isolates the dial-time IP
	// guard specifically, rather than the (separately tested) host check.
	// avatarHTTPClient is deliberately left as the real guarded client
	// against a server that is genuinely listening and would otherwise
	// respond successfully, to confirm it's the dial-time guard rejecting
	// the loopback address rather than anything else.
	if got := fetchAvatarDataURI(context.Background(), avatarHTTPClient, func(string) bool { return true }, srv.URL+"/avatar.png"); got != "" {
		t.Fatalf("expected loopback address to be blocked by SSRF guard, got %q", got)
	}
}
