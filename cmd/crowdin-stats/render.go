package main

import (
	"fmt"
	"html"
	"regexp"
	"sort"
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

var defaultEmbedColors = embedColors{
	bg:     "#12161F",
	text:   "#E8EAED",
	muted:  "#8B93A3",
	accent: "#7DD3A8",
	border: "#232834",
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
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s" rx="8"/>`, tableWidth, height, colors.bg)

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
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s" rx="8"/>`, width, height, colors.bg)

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

func emptyStateSVG(width, height int, message string, colors embedColors) string {
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="'Segoe UI', Helvetica, Arial, sans-serif">`+
			`<rect width="%d" height="%d" fill="%s" rx="8"/>`+
			`<text x="%d" y="%d" fill="%s" font-size="12" text-anchor="middle" dominant-baseline="middle">%s</text>`+
			`</svg>`,
		width, height, width, height, width, height, colors.bg, width/2, height/2, colors.muted, html.EscapeString(message))
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
