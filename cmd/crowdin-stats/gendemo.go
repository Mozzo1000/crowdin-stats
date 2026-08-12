package main

import (
	"context"
	"log/slog"
	"os"
)

// generateDemoSVGs writes static/demo-table.svg and static/demo-contributors.svg
// using the same renderers real badges use, so the landing page's example
// never drifts from actual output. Run via `go run . gendemo`.
func generateDemoSVGs() {
	table := renderTableSVG([]LanguageProgress{
		{LanguageName: "French", Percent: 96},
		{LanguageName: "German", Percent: 88},
		{LanguageName: "Spanish", Percent: 82},
		{LanguageName: "Japanese", Percent: 67},
		{LanguageName: "Portuguese", Percent: 54},
		{LanguageName: "Korean", Percent: 31},
	}, defaultBadgeColors)

	// A light-theme palette, to demonstrate that colors are actually
	// customizable and not just a design token dump — a real reason someone
	// would reach for this is matching a README written for light mode.
	lightColors := badgeColors{bg: "#FFFFFF", text: "#1F2A33", muted: "#64748B", accent: "#2F6FED", border: "#E2E8F0"}
	tableCustom := renderTableSVG([]LanguageProgress{
		{LanguageName: "French", Percent: 96},
		{LanguageName: "German", Percent: 88},
		{LanguageName: "Spanish", Percent: 82},
		{LanguageName: "Japanese", Percent: 67},
		{LanguageName: "Portuguese", Percent: 54},
		{LanguageName: "Korean", Percent: 31},
	}, lightColors)

	// Avatar URLs point at pravatar.cc's generated-face placeholder service —
	// consistent-per-ID stock faces, not real people, safe to bake into a
	// checked-in demo asset. Two entries (noor, ines) are left without an
	// avatar on purpose, to show the initials-fallback that real badges use
	// when Crowdin has no avatar on file for a contributor.
	//
	// embedAvatarsAsDataURIs fetches and inlines each avatar as it would for
	// a real badge — required even here, since a browser refuses to load an
	// external <image href> inside an SVG used as <img src> (see avatar.go),
	// which is exactly how this demo asset is displayed on the landing page.
	demoContributors := embedAvatarsAsDataURIs(context.Background(), []Contributor{
		{Username: "amara", FullName: "Amara Okafor", Amount: 4210, AvatarURL: "https://i.pravatar.cc/150?img=47"},
		{Username: "kenji", FullName: "Kenji Watanabe", Amount: 3870, AvatarURL: "https://i.pravatar.cc/150?img=52"},
		{Username: "lucia", FullName: "Lucia Fernandez", Amount: 3120, AvatarURL: "https://i.pravatar.cc/150?img=45"},
		{Username: "piotr", FullName: "Piotr Nowak", Amount: 2650, AvatarURL: "https://i.pravatar.cc/150?img=13"},
		{Username: "hana", FullName: "Hana Kim", Amount: 2400, AvatarURL: "https://i.pravatar.cc/150?img=44"},
		{Username: "diego", FullName: "Diego Alvarez", Amount: 1980, AvatarURL: "https://i.pravatar.cc/150?img=14"},
		{Username: "elin", FullName: "Elin Svensson", Amount: 1600, AvatarURL: "https://i.pravatar.cc/150?img=48"},
		{Username: "raj", FullName: "Raj Patel", Amount: 1340, AvatarURL: "https://i.pravatar.cc/150?img=51"},
		{Username: "noor", FullName: "Noor Hassan", Amount: 1120},
		{Username: "tomas", FullName: "Tomas Novak", Amount: 940, AvatarURL: "https://i.pravatar.cc/150?img=53"},
		{Username: "yui", FullName: "Yui Tanaka", Amount: 810, AvatarURL: "https://i.pravatar.cc/150?img=43"},
		{Username: "ines", FullName: "Ines Costa", Amount: 700},
	})
	// Only 12 fake contributors exist above, so the two example renders use
	// limits that actually demonstrate the effect against that dataset (5 vs
	// 10) rather than the real-world default/max (30/100), which would
	// render identically to "no limit" and prove nothing.
	contributorsLimit10 := renderContributorsSVG(demoContributors, 10, defaultBadgeColors)
	// A real limit=5 render, not a CSS crop of the limit=10 image — cropping
	// cut circles off mid-row instead of showing what the parameter actually
	// produces.
	contributorsLimit5 := renderContributorsSVG(demoContributors, 5, defaultBadgeColors)

	if err := os.WriteFile("static/demo-table.svg", []byte(table), 0o644); err != nil {
		slog.Error("write demo-table.svg failed", "error", err)
		os.Exit(1)
	}
	if err := os.WriteFile("static/demo-table-custom-colors.svg", []byte(tableCustom), 0o644); err != nil {
		slog.Error("write demo-table-custom-colors.svg failed", "error", err)
		os.Exit(1)
	}
	if err := os.WriteFile("static/demo-contributors.svg", []byte(contributorsLimit10), 0o644); err != nil {
		slog.Error("write demo-contributors.svg failed", "error", err)
		os.Exit(1)
	}
	if err := os.WriteFile("static/demo-contributors-limit5.svg", []byte(contributorsLimit5), 0o644); err != nil {
		slog.Error("write demo-contributors-limit5.svg failed", "error", err)
		os.Exit(1)
	}
	slog.Info("wrote demo SVGs",
		"table", "static/demo-table.svg",
		"table-custom-colors", "static/demo-table-custom-colors.svg",
		"contributors", "static/demo-contributors.svg",
		"contributors-limit5", "static/demo-contributors-limit5.svg")
}
