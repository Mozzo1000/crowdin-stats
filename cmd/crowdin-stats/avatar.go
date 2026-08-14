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

// avatarFetchWorstCase is the longest embedAvatarsAsDataURIs can run: every
// one of maxAvatarEmbeds avatars times out serially in batches of
// avatarFetchParallel. Callers that bound the surrounding fetch (e.g. the
// background cache refresh in cache.go) must budget at least this much time
// on top of whatever the data fetch itself needs.
const avatarFetchWorstCase = ((maxAvatarEmbeds + avatarFetchParallel - 1) / avatarFetchParallel) * avatarFetchTimeout

// avatarLimitTiers buckets how many avatars a contributor-data fetch embeds,
// so the common case (the embed route's own default limit) doesn't pay for
// fetching all maxAvatarEmbeds avatars when a request only needs a
// fraction of them. Requests with the same tier share one cached dataset;
// crossing a tier boundary costs a distinct fetch/cache row. Must stay
// sorted ascending and end in maxAvatarEmbeds.
var avatarLimitTiers = []int{30, maxAvatarEmbeds}

// avatarLimitTier rounds limit up to the smallest tier in avatarLimitTiers
// that can satisfy it.
func avatarLimitTier(limit int) int {
	for _, tier := range avatarLimitTiers {
		if limit <= tier {
			return tier
		}
	}
	return maxAvatarEmbeds
}

// embedAvatarsAsDataURIs fetches each contributor's avatar server-side and
// replaces AvatarURL with a data: URI, or clears it on any failure so
// renderContributorsSVG falls back to its initials circle. Only the
// highest-ranked contributors, up to min(limit, maxAvatarEmbeds), are
// fetched, to bound cost — renderContributorsSVG's own limit/sort produces
// the same ordering, so this doesn't change which avatars end up visible.
//
// Uses the production avatarHTTPClient/avatarHostAllowed; callers that need
// different behavior (gendemo's non-Crowdin demo host, tests) should call
// embedAvatarsAsDataURIsWith directly instead of mutating those globals —
// see fetchAvatarDataURI for why.
func embedAvatarsAsDataURIs(ctx context.Context, contributors []Contributor, limit int) []Contributor {
	return embedAvatarsAsDataURIsWith(ctx, avatarHTTPClient, avatarHostAllowed, contributors, limit)
}

// embedAvatarsAsDataURIsWith is embedAvatarsAsDataURIs with the HTTP client
// and host allowlist passed explicitly, rather than read from package
// globals — lets callers (gendemo, tests) vary that behavior without
// mutating shared state, which would race under parallel tests.
func embedAvatarsAsDataURIsWith(ctx context.Context, client *http.Client, hostAllowed func(string) bool, contributors []Contributor, limit int) []Contributor {
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
			sorted[idx].AvatarURL = fetchAvatarDataURI(ctx, client, hostAllowed, sorted[idx].AvatarURL)
		}(i)
	}
	wg.Wait()

	return sorted
}

// avatarHTTPClient is the production HTTP client fetchAvatarDataURI uses via
// embedAvatarsAsDataURIs. Its Transport resolves the target host itself and
// refuses to dial any IP that isn't publicly routable (loopback,
// RFC1918/ULA private ranges, link-local — which covers the
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

// avatarHostAllowed is the production host allowlist, restricting
// fetchAvatarDataURI to Crowdin's own domain(s) — Crowdin currently serves
// avatars from crowdin-static.cf-downloads.crowdin.com — as a first line of
// defense on top of the dial-time IP guard, in case AvatarURL is ever
// influenced by something other than Crowdin's own report response. Matches
// any *.crowdin.com subdomain rather than the exact host, so a CDN
// subdomain change doesn't silently break every avatar.
//
// gendemo.go passes its own hostAllowed func to embedAvatarsAsDataURIsWith
// instead of using this one, to fetch from i.pravatar.cc for checked-in
// demo assets — those URLs are hardcoded literals, not attacker-influenced,
// so the Crowdin-only allowlist doesn't apply there.
var avatarHostAllowed = func(host string) bool {
	return host == "crowdin.com" || strings.HasSuffix(host, ".crowdin.com")
}

// fetchAvatarDataURI downloads a single avatar and returns it as a data:
// URI, or "" on any failure (network error, non-https, disallowed host,
// non-public address, wrong content type, too large) — callers treat ""
// identically to "no avatar on file".
//
// client and hostAllowed are passed explicitly rather than read from
// package globals so callers that need different behavior (gendemo, tests)
// can vary it without mutating shared state — which would race if tests
// ever ran in parallel.
func fetchAvatarDataURI(ctx context.Context, client *http.Client, hostAllowed func(string) bool, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !hostAllowed(u.Hostname()) {
		return ""
	}

	reqCtx, cancel := context.WithTimeout(ctx, avatarFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
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
