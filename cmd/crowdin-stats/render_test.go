package main

import (
	"net/url"
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

// LanguageName is user-influenced (a Crowdin project can name a custom
// language anything), so it's the primary XSS surface in renderTableSVG —
// this asserts the raw markup never reaches the output unescaped.
func TestRenderTableSVGEscapesLanguageName(t *testing.T) {
	svg := renderTableSVG([]LanguageProgress{
		{LanguageName: `<script>alert(1)</script>`, Percent: 50},
	}, defaultEmbedColors)
	if strings.Contains(svg, "<script>") {
		t.Fatalf("expected language name to be escaped, got raw <script> in: %s", svg)
	}
	if !strings.Contains(svg, "&lt;script&gt;") {
		t.Fatalf("expected escaped language name in output: %s", svg)
	}
}

func TestRenderTableSVGEscapesQuotesInLanguageName(t *testing.T) {
	svg := renderTableSVG([]LanguageProgress{
		{LanguageName: `"><rect/>`, Percent: 50},
	}, defaultEmbedColors)
	if strings.Contains(svg, `"><rect/>`) {
		t.Fatalf("expected quote-breakout attempt to be escaped, got raw markup in: %s", svg)
	}
}

func TestRenderContributorsSVG(t *testing.T) {
	svg := renderContributorsSVG([]Contributor{
		{Username: "alice", Amount: 100},
		{Username: "bob", Amount: 50},
	}, 30, defaultEmbedColors)
	if !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("expected svg output, got: %s", svg)
	}
	if !strings.Contains(svg, "alice") || !strings.Contains(svg, "bob") {
		t.Fatalf("expected usernames in output: %s", svg)
	}
	// Avatars are only ever consumed via <img src=...> embeds, where links
	// inside the SVG's own DOM never activate (see #28) — the wrapping <a>
	// is dead weight and must not be re-added.
	if strings.Contains(svg, "<a ") || strings.Contains(svg, "crowdin.com/profile") {
		t.Fatalf("expected no dead <a> links in output: %s", svg)
	}
}

// FullName/Username come from Crowdin's report response (user-controlled
// display names), so they're the primary XSS surface in
// renderContributorsSVG — this asserts the raw markup never reaches the
// <title> element or the initials fallback unescaped.
func TestRenderContributorsSVGEscapesFullName(t *testing.T) {
	svg := renderContributorsSVG([]Contributor{
		{Username: "alice", FullName: `<script>alert(1)</script>`, Amount: 100},
	}, 30, defaultEmbedColors)
	if strings.Contains(svg, "<script>") {
		t.Fatalf("expected full name to be escaped, got raw <script> in: %s", svg)
	}
	if !strings.Contains(svg, "&lt;script&gt;") {
		t.Fatalf("expected escaped full name in output: %s", svg)
	}
}

// AvatarURL is likewise Crowdin-report-controlled and lands in an <image
// href="..."> attribute — a quote breakout there would let it inject
// arbitrary SVG/markup, not just get treated as an untrusted URL string.
func TestRenderContributorsSVGEscapesAvatarURL(t *testing.T) {
	svg := renderContributorsSVG([]Contributor{
		{Username: "alice", Amount: 100, AvatarURL: `https://example.com/a.png" onload="alert(1)`},
	}, 30, defaultEmbedColors)
	if strings.Contains(svg, `.png" onload="alert(1)`) {
		t.Fatalf("expected avatar URL to be escaped, got raw attribute breakout in: %s", svg)
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
	svg := renderOverallCardSVG(langs, OverallUnitWords, MetricBoth, ProgressTranslation, defaultEmbedColors)
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

	percentOnly := renderOverallCardSVG(langs, OverallUnitWords, MetricPercentage, ProgressTranslation, defaultEmbedColors)
	if strings.Contains(percentOnly, "1 / 4") {
		t.Fatalf("metric=percentage should not show the fraction: %s", percentOnly)
	}

	fractionOnly := renderOverallCardSVG(langs, OverallUnitWords, MetricFraction, ProgressTranslation, defaultEmbedColors)
	if !strings.Contains(fractionOnly, "1 / 4 words") {
		t.Fatalf("metric=fraction should show the fraction: %s", fractionOnly)
	}
	visible := fractionOnly[strings.Index(fractionOnly, "</title>"):]
	if strings.Contains(visible, "25%") {
		t.Fatalf("metric=fraction should not show the percentage outside the accessible title: %s", fractionOnly)
	}
}

func TestRenderOverallCardSVGStrings(t *testing.T) {
	langs := []LanguageProgress{{WordsTotal: 100, WordsTranslated: 100, PhrasesTotal: 40, PhrasesTranslated: 10}}
	svg := renderOverallCardSVG(langs, OverallUnitStrings, MetricBoth, ProgressTranslation, defaultEmbedColors)
	if !strings.Contains(svg, "25%") || !strings.Contains(svg, "10 / 40 strings") {
		t.Fatalf("expected strings-based aggregation, got: %s", svg)
	}
}

func TestRenderOverallCardSVGEmpty(t *testing.T) {
	svg := renderOverallCardSVG(nil, OverallUnitWords, MetricBoth, ProgressTranslation, defaultEmbedColors)
	if !strings.Contains(svg, "no translation data yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestRenderOverallCircleSVG(t *testing.T) {
	langs := []LanguageProgress{{WordsTotal: 4, WordsTranslated: 3}}
	svg := renderOverallCircleSVG(langs, OverallUnitWords, ProgressTranslation, defaultEmbedColors)
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
	svg := renderOverallCircleSVG(nil, OverallUnitWords, ProgressTranslation, defaultEmbedColors)
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

func TestRenderOverallCardSVGApproval(t *testing.T) {
	langs := []LanguageProgress{{WordsTotal: 100, WordsTranslated: 90, WordsApproved: 40}}
	svg := renderOverallCardSVG(langs, OverallUnitWords, MetricBoth, ProgressApproval, defaultEmbedColors)
	if !strings.Contains(svg, "40%") || !strings.Contains(svg, "40 / 100 words") {
		t.Fatalf("expected approval-based aggregation (40/100), got: %s", svg)
	}
	if !strings.Contains(svg, "APPROVAL PROGRESS") {
		t.Fatalf("expected approval label, got: %s", svg)
	}
}

func TestPrepareTableLanguagesLimitAndMinPercent(t *testing.T) {
	langs := []LanguageProgress{
		{LanguageName: "A", Percent: 90},
		{LanguageName: "B", Percent: 10},
		{LanguageName: "C", Percent: 50},
		{LanguageName: "D", Percent: 5},
	}
	out := prepareTableLanguages(langs, ProgressTranslation, 20, 1, nil)
	if len(out) != 1 || out[0].LanguageName != "A" {
		t.Fatalf("expected only the top language above minPercent, got: %+v", out)
	}
}

func TestPrepareTableLanguagesPinnedIsExclusive(t *testing.T) {
	langs := []LanguageProgress{
		{LanguageName: "French", LanguageID: "fr", Percent: 90},
		{LanguageName: "German", LanguageID: "de", Percent: 80},
		{LanguageName: "Korean", LanguageID: "ko", Percent: 2}, // below minPercent, pinned
	}
	pinned := parseLanguagePins("ko")
	out := prepareTableLanguages(langs, ProgressTranslation, 50, 1, pinned)

	if len(out) != 1 || out[0].LanguageName != "Korean" {
		t.Fatalf("expected only the pinned Korean, ignoring minPercent/limit, got: %+v", out)
	}
}

func TestPrepareTableLanguagesApproval(t *testing.T) {
	langs := []LanguageProgress{{LanguageName: "French", Percent: 90, ApprovalPercent: 10}}
	out := prepareTableLanguages(langs, ProgressApproval, 0, 0, nil)
	if out[0].Percent != 10 {
		t.Fatalf("expected Percent swapped to ApprovalPercent, got %d", out[0].Percent)
	}
}

func TestParseLanguagePins(t *testing.T) {
	pinned := parseLanguagePins(" FR, de ,,Japanese")
	want := map[string]bool{"fr": true, "de": true, "japanese": true}
	if len(pinned) != len(want) {
		t.Fatalf("got %v, want %v", pinned, want)
	}
	for k := range want {
		if !pinned[k] {
			t.Fatalf("missing key %q in %v", k, pinned)
		}
	}
	if parseLanguagePins("") != nil {
		t.Fatalf("expected nil for empty input")
	}
}

func TestParseEmbedColorsAuto(t *testing.T) {
	cases := []struct {
		name string
		q    string
		auto bool
	}{
		{"no params at all", "", true},
		{"explicit theme=dark opts out", "theme=dark", false},
		{"explicit theme=light opts out", "theme=light", false},
		{"a single color override opts out", "accent=ff0000", false},
	}
	for _, c := range cases {
		q, err := url.ParseQuery(c.q)
		if err != nil {
			t.Fatalf("bad query %q: %v", c.q, err)
		}
		colors := parseEmbedColors(q)
		if colors.auto != c.auto {
			t.Errorf("%s: parseEmbedColors(%q).auto = %v, want %v", c.name, c.q, colors.auto, c.auto)
		}
	}
}

func TestParseEmbedColorsValues(t *testing.T) {
	q, err := url.ParseQuery("accent=ff0000&bg=badcolor")
	if err != nil {
		t.Fatalf("bad query: %v", err)
	}
	colors := parseEmbedColors(q)
	if colors.accent != "#ff0000" {
		t.Errorf("accent = %q, want %q (valid override honored)", colors.accent, "#ff0000")
	}
	if colors.bg != defaultEmbedColors.bg {
		t.Errorf("bg = %q, want fallback %q (invalid hex ignored)", colors.bg, defaultEmbedColors.bg)
	}
	if colors.text != defaultEmbedColors.text {
		t.Errorf("text = %q, want fallback %q (untouched field keeps base palette)", colors.text, defaultEmbedColors.text)
	}
}

func TestParseEmbedColorsDarkTheme(t *testing.T) {
	q, err := url.ParseQuery("theme=dark")
	if err != nil {
		t.Fatalf("bad query: %v", err)
	}
	colors := parseEmbedColors(q)
	if colors.bg != darkEmbedColors.bg || colors.text != darkEmbedColors.text {
		t.Errorf("theme=dark colors = %+v, want dark palette %+v", colors, darkEmbedColors)
	}
}

func TestAutoThemeEmbedsFollowsViewerColorScheme(t *testing.T) {
	langs := []LanguageProgress{{LanguageName: "French", Percent: 80}}
	auto := embedColors{auto: true,
		bg: defaultEmbedColors.bg, text: defaultEmbedColors.text, muted: defaultEmbedColors.muted,
		accent: defaultEmbedColors.accent, border: defaultEmbedColors.border,
	}
	svg := renderTableSVG(langs, auto)

	if !strings.Contains(svg, "@media (prefers-color-scheme: dark)") {
		t.Fatalf("expected an embedded dark-mode media query, got: %s", svg)
	}
	if !strings.Contains(svg, "var(--cs-bg)") || !strings.Contains(svg, "var(--cs-text)") {
		t.Fatalf("expected fill attributes to reference the auto-theme custom properties, got: %s", svg)
	}
	if !strings.Contains(svg, darkEmbedColors.bg) {
		t.Fatalf("expected the dark palette values to be embedded for the media query, got: %s", svg)
	}
}

func TestSVGRenderersHaveAccessibleName(t *testing.T) {
	langs := []LanguageProgress{{LanguageName: "French", Percent: 80}}
	contributors := []Contributor{{Username: "alice", FullName: "Alice A", Amount: 100}}

	svgs := map[string]string{
		"table":                renderTableSVG(langs, defaultEmbedColors),
		"table empty":          renderTableSVG(nil, defaultEmbedColors),
		"contributors":         renderContributorsSVG(contributors, 30, defaultEmbedColors),
		"contributors empty":   renderContributorsSVG(nil, 30, defaultEmbedColors),
		"overall card":         renderOverallCardSVG(langs, OverallUnitWords, MetricBoth, ProgressTranslation, defaultEmbedColors),
		"overall card empty":   renderOverallCardSVG(nil, OverallUnitWords, MetricBoth, ProgressTranslation, defaultEmbedColors),
		"overall circle":       renderOverallCircleSVG(langs, OverallUnitWords, ProgressTranslation, defaultEmbedColors),
		"overall circle empty": renderOverallCircleSVG(nil, OverallUnitWords, ProgressTranslation, defaultEmbedColors),
	}
	for name, svg := range svgs {
		if !strings.Contains(svg, `role="img"`) {
			t.Errorf("%s: expected role=\"img\" on root svg, got: %s", name, svg)
		}
		if !strings.Contains(svg, "<title>") {
			t.Errorf("%s: expected a <title> element, got: %s", name, svg)
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
