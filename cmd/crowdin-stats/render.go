package main

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// embedColors are the customizable colors shared by both SVG renderers.
// Query params use the same names as the site's own CSS tokens (bg, text,
// text-muted, accent, border) so the customization surface reads as one
// consistent vocabulary rather than two embed-specific ones.
type embedColors struct {
	bg     string // card background
	text   string // primary text (language labels)
	muted  string // secondary text (percentages, initials, empty-state message)
	accent string // progress bar fill
	border string // bar track / avatar ring / fallback circle background
}

// defaultEmbedColors mirrors the site's own light-mode CSS tokens (see
// input.css's :root block) so an embed dropped into a README reads as the
// same product as the site rather than an arbitrary dark mint theme.
var defaultEmbedColors = embedColors{
	bg:     "#ffffff",
	text:   "#1f2a33",
	muted:  "#64748b",
	accent: "#2f6fed",
	border: "#e2e8f0",
}

// darkEmbedColors mirrors the site's :root.dark tokens. Selected via the
// `theme=dark` query param (see parseEmbedColors) for maintainers whose
// README or host page is dark-themed.
var darkEmbedColors = embedColors{
	bg:     "#12161d",
	text:   "#edeff3",
	muted:  "#97a2b4",
	accent: "#5b8dff",
	border: "#2b3340",
}

var hexColorRe = regexp.MustCompile(`^[0-9a-fA-F]{3}$|^[0-9a-fA-F]{6}$`)

// sanitizeHexColor accepts a 3- or 6-digit hex color with or without a
// leading '#' and returns it normalized with a leading '#'. Anything else
// (missing, malformed, an attempt at CSS/JS injection) falls back to the
// default rather than erroring the whole embed over a bad query param.
func sanitizeHexColor(raw, fallback string) string {
	raw = strings.TrimPrefix(raw, "#")
	if hexColorRe.MatchString(raw) {
		return "#" + raw
	}
	return fallback
}

// cacheKeyFragment renders the colors into a stable string so different
// color combinations don't collide in the embed cache table.
func (c embedColors) cacheKeyFragment() string {
	return "bg=" + c.bg + ":text=" + c.text + ":muted=" + c.muted + ":accent=" + c.accent + ":border=" + c.border
}

const (
	tableRowHeight   = 28
	tableWidth       = 360
	tableLabelWidth  = 110
	tableBarWidth    = 160
	tableBarGap      = 8
	tablePercentGap  = 8
	tablePercentArea = 50
	tablePaddingX    = 12
	tablePaddingTop  = 12
)

// ProgressType selects which of the two percentages Crowdin tracks per
// language — translation or approval/proofreading — table.svg and
// overall.svg render. Both are already present on every LanguageProgress
// (see crowdin.go), so switching between them never needs a second API call.
type ProgressType string

const (
	ProgressTranslation ProgressType = "translation"
	ProgressApproval    ProgressType = "approval"
)

// prepareTableLanguages applies table.svg's filters, in order:
//  1. progressType — swap each language's rendered Percent to its approval
//     figure, so sorting/filtering/rendering downstream all agree on which
//     number is "the" percent.
//  2. pinned — languages named in the `languages` query param (matched
//     case-insensitively against either LanguageID or LanguageName). When
//     `languages` is set, it's exclusive: the table shows only the matched
//     languages, in whatever progress state they're in, and minPercent/limit
//     are ignored entirely. This is what lets a maintainer hand-pick exactly
//     which languages appear instead of letting the top-N/min-percent
//     filters decide.
//  3. minPercent / limit — applied only when no languages are pinned: drop
//     anything below minPercent, sort by percent descending, then keep only
//     the top `limit` (0 = unlimited).
func prepareTableLanguages(languages []LanguageProgress, progressType ProgressType, minPercent, limit int, pinned map[string]bool) []LanguageProgress {
	prepared := make([]LanguageProgress, len(languages))
	copy(prepared, languages)
	if progressType == ProgressApproval {
		for i := range prepared {
			prepared[i].Percent = prepared[i].ApprovalPercent
		}
	}

	if len(pinned) > 0 {
		var pinnedOut []LanguageProgress
		for _, lang := range prepared {
			if pinned[strings.ToLower(lang.LanguageID)] || pinned[strings.ToLower(lang.LanguageName)] {
				pinnedOut = append(pinnedOut, lang)
			}
		}
		return pinnedOut
	}

	var rest []LanguageProgress
	for _, lang := range prepared {
		if lang.Percent >= minPercent {
			rest = append(rest, lang)
		}
	}

	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Percent != rest[j].Percent {
			return rest[i].Percent > rest[j].Percent
		}
		return rest[i].LanguageName < rest[j].LanguageName
	})
	if limit > 0 && len(rest) > limit {
		rest = rest[:limit]
	}

	return rest
}

// parseLanguagePins splits the `languages` query param (comma-separated
// codes or display names, e.g. "fr,de,Japanese") into a lowercased lookup
// set for prepareTableLanguages. Capped at 50 entries — this pins languages
// into the table, it isn't meant to be the primary way to select all of
// them.
func parseLanguagePins(raw string) map[string]bool {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		out[p] = true
		if len(out) >= 50 {
			break
		}
	}
	return out
}

// pinnedCacheKeyFragment renders a pinned-language set into a stable,
// order-independent string, so "?languages=fr,de" and "?languages=de, fr"
// share one cache entry instead of fragmenting on formatting differences.
func pinnedCacheKeyFragment(pinned map[string]bool) string {
	if len(pinned) == 0 {
		return ""
	}
	names := make([]string, 0, len(pinned))
	for name := range pinned {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// renderTableSVG renders a horizontal progress bar per language.
func renderTableSVG(languages []LanguageProgress, colors embedColors) string {
	sorted := make([]LanguageProgress, len(languages))
	copy(sorted, languages)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Percent != sorted[j].Percent {
			return sorted[i].Percent > sorted[j].Percent
		}
		return sorted[i].LanguageName < sorted[j].LanguageName
	})

	height := tablePaddingTop*2 + tableRowHeight*len(sorted)
	if len(sorted) == 0 {
		return emptyStateSVG(tableWidth, 60, "no language data yet", colors)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="'Segoe UI', Helvetica, Arial, sans-serif">`,
		tableWidth, height, tableWidth, height)
	fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%d" height="%d" rx="8" fill="%s" stroke="%s"/>`, tableWidth-1, height-1, colors.bg, colors.border)

	barX := tablePaddingX + tableLabelWidth + tableBarGap
	percentX := tableWidth - tablePaddingX
	for i, lang := range sorted {
		y := tablePaddingTop + i*tableRowHeight
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-size="12" dominant-baseline="middle">%s</text>`,
			tablePaddingX, y+tableRowHeight/2, colors.text, truncateLabel(lang.LanguageName, 16))
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="10" rx="5" fill="%s"/>`,
			barX, y+tableRowHeight/2-5, tableBarWidth, colors.border)
		filled := tableBarWidth * clampPercent(lang.Percent) / 100
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="10" rx="5" fill="%s"/>`,
			barX, y+tableRowHeight/2-5, filled, colors.accent)
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-size="11" text-anchor="end" dominant-baseline="middle">%d%%</text>`,
			percentX, y+tableRowHeight/2, colors.muted, clampPercent(lang.Percent))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

const (
	avatarSize   = 48
	avatarGap    = 6
	gridPaddingX = 10
	gridPaddingY = 10
	gridCols     = 8
)

// renderContributorsSVG renders a contrib.rocks-style grid of circular
// avatars, ordered by contribution volume and truncated to limit.
func renderContributorsSVG(contributors []Contributor, limit int, colors embedColors) string {
	sorted := make([]Contributor, len(contributors))
	copy(sorted, contributors)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Amount != sorted[j].Amount {
			return sorted[i].Amount > sorted[j].Amount
		}
		return sorted[i].Username < sorted[j].Username
	})
	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}

	if len(sorted) == 0 {
		return emptyStateSVG(320, 60, "no contributors yet", colors)
	}

	cols := gridCols
	if len(sorted) < cols {
		cols = len(sorted)
	}
	rows := (len(sorted) + gridCols - 1) / gridCols

	cell := avatarSize + avatarGap
	width := gridPaddingX*2 + cell*cols - avatarGap
	height := gridPaddingY*2 + cell*rows - avatarGap

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		width, height, width, height)
	fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%d" height="%d" rx="8" fill="%s" stroke="%s"/>`, width-1, height-1, colors.bg, colors.border)

	for i, c := range sorted {
		col := i % gridCols
		row := i / gridCols
		cx := gridPaddingX + col*cell + avatarSize/2
		cy := gridPaddingY + row*cell + avatarSize/2
		clipID := fmt.Sprintf("clip%d", i)

		title := c.FullName
		if title == "" {
			title = c.Username
		}

		fmt.Fprintf(&b, `<clipPath id="%s"><circle cx="%d" cy="%d" r="%d"/></clipPath>`,
			clipID, cx, cy, avatarSize/2)
		fmt.Fprintf(&b, `<a href="https://crowdin.com/profile/%s" target="_blank">`, html.EscapeString(c.Username))
		fmt.Fprintf(&b, `<title>%s</title>`, html.EscapeString(title))
		if c.AvatarURL != "" {
			fmt.Fprintf(&b, `<image href="%s" x="%d" y="%d" width="%d" height="%d" clip-path="url(#%s)" preserveAspectRatio="xMidYMid slice"/>`,
				html.EscapeString(c.AvatarURL), gridPaddingX+col*cell, gridPaddingY+row*cell, avatarSize, avatarSize, clipID)
		} else {
			fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d" fill="%s"/>`, cx, cy, avatarSize/2, colors.border)
			initial := "?"
			if title != "" {
				initial = strings.ToUpper(string([]rune(title)[0]))
			}
			fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-size="16" text-anchor="middle" dominant-baseline="central">%s</text>`,
				cx, cy, colors.muted, html.EscapeString(initial))
		}
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="1"/>`,
			cx, cy, avatarSize/2, colors.border)
		b.WriteString(`</a>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// OverallUnit is the metric overall.svg aggregates across languages. Unlike
// ReportUnit (used by contributors.svg's top-members report), there is no
// "characters" option here: the per-language progress endpoint that both
// table.svg and overall.svg are built from only reports words and phrases
// (Crowdin's internal name for strings) — adding characters would mean a
// second Crowdin API call this endpoint is explicitly meant to avoid.
type OverallUnit string

const (
	OverallUnitWords   OverallUnit = "words"
	OverallUnitStrings OverallUnit = "strings"
)

// overallProgress is the aggregated total/translated/percent figure shared
// by both overall.svg layouts, so the card and circle variants never
// disagree on the underlying numbers.
type overallProgress struct {
	Total      int
	Translated int
	Percent    int
}

// aggregateOverallProgress sums per-language totals into a single
// project-wide figure for the given unit and progress type (translated vs.
// approved counts).
func aggregateOverallProgress(languages []LanguageProgress, unit OverallUnit, progressType ProgressType) overallProgress {
	var total, translated int
	for _, lang := range languages {
		switch {
		case unit == OverallUnitStrings && progressType == ProgressApproval:
			total += lang.PhrasesTotal
			translated += lang.PhrasesApproved
		case unit == OverallUnitStrings:
			total += lang.PhrasesTotal
			translated += lang.PhrasesTranslated
		case progressType == ProgressApproval:
			total += lang.WordsTotal
			translated += lang.WordsApproved
		default:
			total += lang.WordsTotal
			translated += lang.WordsTranslated
		}
	}
	percent := 0
	if total > 0 {
		percent = clampPercent(translated * 100 / total)
	}
	return overallProgress{Total: total, Translated: translated, Percent: percent}
}

// formatThousands renders an integer with comma thousands separators, e.g.
// 4213 -> "4,213", matching the fraction subtitle in the overall.svg mockup.
func formatThousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// OverallMetric selects what overall.svg's card variant displays. Ignored
// by the circle variant, which always shows percentage only — the
// done/total fraction was tried at 120x120 and dropped as illegible.
type OverallMetric string

const (
	MetricPercentage OverallMetric = "percentage"
	MetricFraction   OverallMetric = "fraction"
	MetricBoth       OverallMetric = "both"
)

const (
	overallCardWidth   = 340
	overallCardHeight  = 140
	overallCardPadding = 20
)

// renderOverallCardSVG renders a wide summary card: label, big percentage,
// optional fraction subtitle, and a thin progress bar.
func renderOverallCardSVG(languages []LanguageProgress, unit OverallUnit, metric OverallMetric, progressType ProgressType, colors embedColors) string {
	prog := aggregateOverallProgress(languages, unit, progressType)
	if prog.Total == 0 {
		return emptyStateSVG(overallCardWidth, 60, "no translation data yet", colors)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="'Segoe UI', Helvetica, Arial, sans-serif">`,
		overallCardWidth, overallCardHeight, overallCardWidth, overallCardHeight)
	fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%d" height="%d" rx="10" fill="%s" stroke="%s"/>`, overallCardWidth-1, overallCardHeight-1, colors.bg, colors.border)
	label := "TRANSLATION PROGRESS"
	if progressType == ProgressApproval {
		label = "APPROVAL PROGRESS"
	}
	fmt.Fprintf(&b, `<text x="%d" y="30" fill="%s" font-size="12" font-weight="600" letter-spacing="0.06em">%s</text>`,
		overallCardPadding, colors.muted, label)

	if metric == MetricFraction {
		fmt.Fprintf(&b, `<text x="%d" y="82" fill="%s" font-size="34" font-weight="700">%s / %s %s</text>`,
			overallCardPadding, colors.accent, formatThousands(prog.Translated), formatThousands(prog.Total), html.EscapeString(string(unit)))
	} else {
		fmt.Fprintf(&b, `<text x="%d" y="82" fill="%s" font-size="44" font-weight="700">%d%%</text>`,
			overallCardPadding, colors.accent, prog.Percent)
		if metric == MetricBoth {
			fmt.Fprintf(&b, `<text x="%d" y="106" fill="%s" font-size="13">%s / %s %s</text>`,
				overallCardPadding, colors.text, formatThousands(prog.Translated), formatThousands(prog.Total), html.EscapeString(string(unit)))
		}
	}

	barY := overallCardHeight - 22
	barWidth := overallCardWidth - overallCardPadding*2
	fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="8" rx="4" fill="%s"/>`,
		overallCardPadding, barY, barWidth, colors.border)
	filled := barWidth * prog.Percent / 100
	fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="8" rx="4" fill="%s"/>`,
		overallCardPadding, barY, filled, colors.accent)

	b.WriteString(`</svg>`)
	return b.String()
}

const (
	overallCircleSize   = 120
	overallCircleRadius = 46
)

// renderOverallCircleSVG renders a compact 120x120 progress ring showing
// percentage only, for inline use where a card is too large.
func renderOverallCircleSVG(languages []LanguageProgress, unit OverallUnit, progressType ProgressType, colors embedColors) string {
	prog := aggregateOverallProgress(languages, unit, progressType)
	if prog.Total == 0 {
		return emptyStateSVG(overallCircleSize, overallCircleSize, "no data", colors)
	}

	circumference := 2 * math.Pi * overallCircleRadius
	filled := circumference * float64(prog.Percent) / 100
	center := overallCircleSize / 2

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="'Segoe UI', Helvetica, Arial, sans-serif">`,
		overallCircleSize, overallCircleSize, overallCircleSize, overallCircleSize)
	fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%d" height="%d" rx="10" fill="%s" stroke="%s"/>`, overallCircleSize-1, overallCircleSize-1, colors.bg, colors.border)
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="10"/>`,
		center, center, overallCircleRadius, colors.border)
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%d" fill="none" stroke="%s" stroke-width="10" stroke-linecap="round" stroke-dasharray="%.2f %.2f" transform="rotate(-90 %d %d)"/>`,
		center, center, overallCircleRadius, colors.accent, filled, circumference, center, center)
	fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-size="26" font-weight="700" text-anchor="middle" dominant-baseline="middle">%d%%</text>`,
		center, center+2, colors.text, prog.Percent)
	b.WriteString(`</svg>`)
	return b.String()
}

func emptyStateSVG(width, height int, message string, colors embedColors) string {
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="'Segoe UI', Helvetica, Arial, sans-serif">`+
			`<rect x="0.5" y="0.5" width="%d" height="%d" rx="8" fill="%s" stroke="%s"/>`+
			`<text x="%d" y="%d" fill="%s" font-size="12" text-anchor="middle" dominant-baseline="middle">%s</text>`+
			`</svg>`,
		width, height, width, height, width-1, height-1, colors.bg, colors.border, width/2, height/2, colors.muted, html.EscapeString(message))
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func truncateLabel(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return html.EscapeString(s)
	}
	return html.EscapeString(string(r[:max-1]) + "…")
}
