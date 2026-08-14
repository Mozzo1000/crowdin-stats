package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
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

// avatarHTTPClient fetches avatar images. Its Transport resolves the target
// host itself and refuses to dial any IP that isn't publicly routable
// (loopback, RFC1918/ULA private ranges, link-local — which covers the
// 169.254.169.254 cloud metadata endpoint — and friends), so the guard
// holds on the initial request and on every redirect hop alike, and can't
// be bypassed by DNS rebinding between the check and the dial. AvatarURL
// comes from Crowdin's own report response rather than directly from user
// input, but this bounds the blast radius if that ever changes.
var avatarHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: guardedDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("refusing to redirect to a non-https URL")
		}
		return nil
	},
}

// guardedDialContext resolves host itself (rather than delegating to the
// standard dialer) so it can inspect every candidate IP before connecting,
// and then dials that exact IP so a second, independent lookup at connect
// time can't return a different (attacker-controlled) address.
func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !isPubliclyRoutableIP(ip) {
			return nil, fmt.Errorf("refusing to dial non-public address %s", ip)
		}
	}

	d := net.Dialer{Timeout: avatarFetchTimeout}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func isPubliclyRoutableIP(ip net.IP) bool {
	return !ip.IsLoopback() &&
		!ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsInterfaceLocalMulticast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified()
}

// fetchAvatarDataURI downloads a single avatar and returns it as a data:
// URI, or "" on any failure (network error, non-https, non-public address,
// wrong content type, too large) — callers treat "" identically to "no
// avatar on file".
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
	resp, err := avatarHTTPClient.Do(req)
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
