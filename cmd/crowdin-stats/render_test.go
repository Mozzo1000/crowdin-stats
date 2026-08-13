package main

import (
	"strings"
	"testing"
)

func TestRenderTableSVG(t *testing.T) {
	svg := renderTableSVG([]LanguageProgress{
		{LanguageName: "French", Percent: 80},
		{LanguageName: "German", Percent: 42},
	}, defaultEmbedColors)
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
	svg := renderTableSVG(nil, defaultEmbedColors)
	if !strings.Contains(svg, "no language data yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestRenderTableSVGCustomColors(t *testing.T) {
	colors := embedColors{bg: "#111111", text: "#222222", muted: "#333333", accent: "#444444", border: "#555555"}
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
	}, 30, defaultEmbedColors)
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
	svg := renderContributorsSVG(contributors, 3, defaultEmbedColors)
	count := strings.Count(svg, "<clipPath")
	if count != 3 {
		t.Fatalf("expected 3 avatars, got %d: %s", count, svg)
	}
}

func TestRenderContributorsSVGEmpty(t *testing.T) {
	svg := renderContributorsSVG(nil, 30, defaultEmbedColors)
	if !strings.Contains(svg, "no contributors yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestRenderOverallCardSVG(t *testing.T) {
	langs := []LanguageProgress{
		{WordsTotal: 100, WordsTranslated: 75, PhrasesTotal: 40, PhrasesTranslated: 10},
		{WordsTotal: 100, WordsTranslated: 25, PhrasesTotal: 40, PhrasesTranslated: 10},
	}
	svg := renderOverallCardSVG(langs, OverallUnitWords, MetricBoth, defaultEmbedColors)
	if !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("expected svg output, got: %s", svg)
	}
	if !strings.Contains(svg, "50%") {
		t.Fatalf("expected 50%% (100 translated / 200 total), got: %s", svg)
	}
	if !strings.Contains(svg, "100 / 200 words") {
		t.Fatalf("expected fraction subtitle, got: %s", svg)
	}
}

func TestRenderOverallCardSVGMetrics(t *testing.T) {
	langs := []LanguageProgress{{WordsTotal: 4, WordsTranslated: 1}}

	percentOnly := renderOverallCardSVG(langs, OverallUnitWords, MetricPercentage, defaultEmbedColors)
	if strings.Contains(percentOnly, "1 / 4") {
		t.Fatalf("metric=percentage should not show the fraction: %s", percentOnly)
	}

	fractionOnly := renderOverallCardSVG(langs, OverallUnitWords, MetricFraction, defaultEmbedColors)
	if !strings.Contains(fractionOnly, "1 / 4 words") {
		t.Fatalf("metric=fraction should show the fraction: %s", fractionOnly)
	}
	if strings.Contains(fractionOnly, "25%") {
		t.Fatalf("metric=fraction should not show the percentage: %s", fractionOnly)
	}
}

func TestRenderOverallCardSVGStrings(t *testing.T) {
	langs := []LanguageProgress{{WordsTotal: 100, WordsTranslated: 100, PhrasesTotal: 40, PhrasesTranslated: 10}}
	svg := renderOverallCardSVG(langs, OverallUnitStrings, MetricBoth, defaultEmbedColors)
	if !strings.Contains(svg, "25%") || !strings.Contains(svg, "10 / 40 strings") {
		t.Fatalf("expected strings-based aggregation, got: %s", svg)
	}
}

func TestRenderOverallCardSVGEmpty(t *testing.T) {
	svg := renderOverallCardSVG(nil, OverallUnitWords, MetricBoth, defaultEmbedColors)
	if !strings.Contains(svg, "no translation data yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestRenderOverallCircleSVG(t *testing.T) {
	langs := []LanguageProgress{{WordsTotal: 4, WordsTranslated: 3}}
	svg := renderOverallCircleSVG(langs, OverallUnitWords, defaultEmbedColors)
	if !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("expected svg output, got: %s", svg)
	}
	if !strings.Contains(svg, "75%") {
		t.Fatalf("expected 75%%, got: %s", svg)
	}
	if strings.Contains(svg, "3 / 4") {
		t.Fatalf("circle variant should never show a fraction: %s", svg)
	}
}

func TestRenderOverallCircleSVGEmpty(t *testing.T) {
	svg := renderOverallCircleSVG(nil, OverallUnitWords, defaultEmbedColors)
	if !strings.Contains(svg, "no data") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestFormatThousands(t *testing.T) {
	cases := map[int]string{0: "0", 5: "5", 999: "999", 1000: "1,000", 4213: "4,213", 1234567: "1,234,567"}
	for n, want := range cases {
		if got := formatThousands(n); got != want {
			t.Errorf("formatThousands(%d) = %q, want %q", n, got, want)
		}
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
