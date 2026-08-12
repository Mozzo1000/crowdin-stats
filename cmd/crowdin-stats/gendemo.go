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
	})

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
	contributors := renderContributorsSVG(demoContributors, 30)

	if err := os.WriteFile("static/demo-table.svg", []byte(table), 0o644); err != nil {
		slog.Error("write demo-table.svg failed", "error", err)
		os.Exit(1)
	}
	if err := os.WriteFile("static/demo-contributors.svg", []byte(contributors), 0o644); err != nil {
		slog.Error("write demo-contributors.svg failed", "error", err)
		os.Exit(1)
	}
	slog.Info("wrote demo SVGs", "table", "static/demo-table.svg", "contributors", "static/demo-contributors.svg")
}
