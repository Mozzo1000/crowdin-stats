package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Browsers refuse to load external <image href> resources inside an SVG
// when that SVG is used as an <img src> — the exact way embeds are always
// embedded in READMEs (confirmed against real-world testing; see Firefox
// bug 628747, "SVG-as-an-image shouldn't be able to load external
// resources"). Opening the SVG file directly works fine, which is
// misleading during manual testing. The only reliable fix is to inline
// each avatar as a base64 data: URI at render time, so the SVG carries its
// own image bytes and needs no second network request to display.
const (
	maxAvatarEmbeds     = 100 // matches the embed route's own limit=100 cap
	maxAvatarBodyBytes  = 2 << 20
	avatarFetchTimeout  = 5 * time.Second
	avatarFetchParallel = 6
)

// embedAvatarsAsDataURIs fetches each contributor's avatar server-side and
// replaces AvatarURL with a data: URI, or clears it on any failure so
// renderContributorsSVG falls back to its initials circle. Only the
// highest-ranked contributors, up to min(limit, maxAvatarEmbeds), are
// fetched, to bound cost — renderContributorsSVG's own limit/sort produces
// the same ordering, so this doesn't change which avatars end up visible.
func embedAvatarsAsDataURIs(ctx context.Context, contributors []Contributor, limit int) []Contributor {
	sorted := make([]Contributor, len(contributors))
	copy(sorted, contributors)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Amount != sorted[j].Amount {
			return sorted[i].Amount > sorted[j].Amount
		}
		return sorted[i].Username < sorted[j].Username
	})

	if limit <= 0 || limit > maxAvatarEmbeds {
		limit = maxAvatarEmbeds
	}

	sem := make(chan struct{}, avatarFetchParallel)
	var wg sync.WaitGroup
	for i := range sorted {
		if i >= limit || sorted[i].AvatarURL == "" {
			sorted[i].AvatarURL = ""
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			sorted[idx].AvatarURL = fetchAvatarDataURI(ctx, sorted[idx].AvatarURL)
		}(i)
	}
	wg.Wait()

	return sorted
}

// fetchAvatarDataURI downloads a single avatar and returns it as a data:
// URI, or "" on any failure (network error, non-https, wrong content type,
// too large) — callers treat "" identically to "no avatar on file".
func fetchAvatarDataURI(ctx context.Context, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return ""
	}

	reqCtx, cancel := context.WithTimeout(ctx, avatarFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBodyBytes+1))
	if err != nil || len(body) > maxAvatarBodyBytes {
		return ""
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(body)
}
