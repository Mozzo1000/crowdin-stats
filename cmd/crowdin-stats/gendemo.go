package main

import (
	"context"
	"log/slog"
	"os"
)

// generateDemoSVGs writes static/demo-table.svg and static/demo-contributors.svg
// using the same renderers real embeds use, so the landing page's example
// never drifts from actual output. Run via `go run . gendemo`.
//
// This same dataset is duplicated in static/embed-builder.js (DEMO_LANGUAGES,
// DEMO_CONTRIBUTORS) for the interactive "build your own embed" widget's
// live preview — keep the two in sync if either changes.
func generateDemoSVGs() {
	demoLanguages := []LanguageProgress{
		{LanguageName: "French", Percent: 96, WordsTotal: 850, WordsTranslated: 816, PhrasesTotal: 620, PhrasesTranslated: 595},
		{LanguageName: "German", Percent: 88, WordsTotal: 850, WordsTranslated: 748, PhrasesTotal: 620, PhrasesTranslated: 546},
		{LanguageName: "Spanish", Percent: 82, WordsTotal: 850, WordsTranslated: 697, PhrasesTotal: 620, PhrasesTranslated: 508},
		{LanguageName: "Japanese", Percent: 67, WordsTotal: 850, WordsTranslated: 570, PhrasesTotal: 620, PhrasesTranslated: 415},
		{LanguageName: "Portuguese", Percent: 54, WordsTotal: 850, WordsTranslated: 459, PhrasesTotal: 620, PhrasesTranslated: 335},
		{LanguageName: "Korean", Percent: 31, WordsTotal: 850, WordsTranslated: 264, PhrasesTotal: 620, PhrasesTranslated: 192},
	}

	table := renderTableSVG(demoLanguages, defaultEmbedColors)
	tableDark := renderTableSVG(demoLanguages, darkEmbedColors)
	overallCard := renderOverallCardSVG(demoLanguages, OverallUnitWords, MetricBoth, ProgressTranslation, defaultEmbedColors)
	overallCardDark := renderOverallCardSVG(demoLanguages, OverallUnitWords, MetricBoth, ProgressTranslation, darkEmbedColors)
	overallCircle := renderOverallCircleSVG(demoLanguages, OverallUnitWords, ProgressTranslation, defaultEmbedColors)
	overallCircleDark := renderOverallCircleSVG(demoLanguages, OverallUnitWords, ProgressTranslation, darkEmbedColors)

	// Avatar URLs point at pravatar.cc's generated-face placeholder service —
	// consistent-per-ID stock faces, not real people, safe to bake into a
	// checked-in demo asset. Two entries (noor, ines) are left without an
	// avatar on purpose, to show the initials-fallback that real embeds use
	// when Crowdin has no avatar on file for a contributor.
	//
	// embedAvatarsAsDataURIs fetches and inlines each avatar as it would for
	// a real embed — required even here, since a browser refuses to load an
	// external <image href> inside an SVG used as <img src> (see avatar.go),
	// which is exactly how this demo asset is displayed on the landing page.
	origHostAllowed := avatarHostAllowed
	avatarHostAllowed = func(host string) bool { return host == "i.pravatar.cc" }
	defer func() { avatarHostAllowed = origHostAllowed }()

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
	}, 10)
	contributors := renderContributorsSVG(demoContributors, 10, defaultEmbedColors)
	contributorsDark := renderContributorsSVG(demoContributors, 10, darkEmbedColors)

	files := map[string]string{
		"static/demo-table.svg":               table,
		"static/demo-table-dark.svg":          tableDark,
		"static/demo-contributors.svg":        contributors,
		"static/demo-contributors-dark.svg":   contributorsDark,
		"static/demo-overall.svg":             overallCard,
		"static/demo-overall-dark.svg":        overallCardDark,
		"static/demo-overall-circle.svg":      overallCircle,
		"static/demo-overall-circle-dark.svg": overallCircleDark,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			slog.Error("write demo SVG failed", "path", path, "error", err)
			os.Exit(1)
		}
	}
	slog.Info("wrote demo SVGs", "count", len(files))
}
