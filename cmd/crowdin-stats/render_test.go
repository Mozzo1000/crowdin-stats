package main

import (
	"strings"
	"testing"
)

func TestRenderTableSVG(t *testing.T) {
	svg := renderTableSVG([]LanguageProgress{
		{LanguageName: "French", Percent: 80},
		{LanguageName: "German", Percent: 42},
	}, defaultBadgeColors)
	if !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("expected svg output, got: %s", svg)
	}
	if !strings.Contains(svg, "French") || !strings.Contains(svg, "German") {
		t.Fatalf("expected language names in output: %s", svg)
	}
	if !strings.Contains(svg, "80%") || !strings.Contains(svg, "42%") {
		t.Fatalf("expected percentages in output: %s", svg)
	}
}

func TestRenderTableSVGEmpty(t *testing.T) {
	svg := renderTableSVG(nil, defaultBadgeColors)
	if !strings.Contains(svg, "no language data yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestRenderTableSVGCustomColors(t *testing.T) {
	colors := badgeColors{bg: "#111111", text: "#222222", muted: "#333333", accent: "#444444", border: "#555555"}
	svg := renderTableSVG([]LanguageProgress{{LanguageName: "French", Percent: 80}}, colors)
	for _, hex := range []string{"#111111", "#222222", "#333333", "#444444", "#555555"} {
		if !strings.Contains(svg, hex) {
			t.Fatalf("expected custom color %s in output: %s", hex, svg)
		}
	}
}

func TestRenderContributorsSVG(t *testing.T) {
	svg := renderContributorsSVG([]Contributor{
		{Username: "alice", FullName: "Alice A", Amount: 100},
		{Username: "bob", FullName: "Bob B", Amount: 50},
	}, 30, defaultBadgeColors)
	if !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("expected svg output, got: %s", svg)
	}
	if !strings.Contains(svg, "alice") || !strings.Contains(svg, "bob") {
		t.Fatalf("expected usernames in output: %s", svg)
	}
}

func TestRenderContributorsSVGRespectsLimit(t *testing.T) {
	contributors := make([]Contributor, 0, 10)
	for i := 0; i < 10; i++ {
		contributors = append(contributors, Contributor{Username: string(rune('a' + i)), Amount: int64(10 - i)})
	}
	svg := renderContributorsSVG(contributors, 3, defaultBadgeColors)
	count := strings.Count(svg, "<clipPath")
	if count != 3 {
		t.Fatalf("expected 3 avatars, got %d: %s", count, svg)
	}
}

func TestRenderContributorsSVGEmpty(t *testing.T) {
	svg := renderContributorsSVG(nil, 30, defaultBadgeColors)
	if !strings.Contains(svg, "no contributors yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestSanitizeHexColor(t *testing.T) {
	cases := []struct {
		raw, fallback, want string
	}{
		{"abc123", "#000000", "#abc123"},
		{"#abc123", "#000000", "#abc123"},
		{"fff", "#000000", "#fff"},
		{"#fff", "#000000", "#fff"},
		{"", "#000000", "#000000"},
		{"not-a-color", "#000000", "#000000"},
		{"javascript:alert(1)", "#000000", "#000000"},
		{"gggggg", "#000000", "#000000"},
		{"ffff", "#000000", "#000000"}, // 4 digits, not a valid hex color length
	}
	for _, c := range cases {
		if got := sanitizeHexColor(c.raw, c.fallback); got != c.want {
			t.Errorf("sanitizeHexColor(%q, %q) = %q, want %q", c.raw, c.fallback, got, c.want)
		}
	}
}
