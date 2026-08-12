package main

import (
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

	contributors := renderContributorsSVG([]Contributor{
		{Username: "amara", FullName: "Amara Okafor", Amount: 4210},
		{Username: "kenji", FullName: "Kenji Watanabe", Amount: 3870},
		{Username: "lucia", FullName: "Lucia Fernandez", Amount: 3120},
		{Username: "piotr", FullName: "Piotr Nowak", Amount: 2650},
		{Username: "hana", FullName: "Hana Kim", Amount: 2400},
		{Username: "diego", FullName: "Diego Alvarez", Amount: 1980},
		{Username: "elin", FullName: "Elin Svensson", Amount: 1600},
		{Username: "raj", FullName: "Raj Patel", Amount: 1340},
		{Username: "noor", FullName: "Noor Hassan", Amount: 1120},
		{Username: "tomas", FullName: "Tomas Novak", Amount: 940},
		{Username: "yui", FullName: "Yui Tanaka", Amount: 810},
		{Username: "ines", FullName: "Ines Costa", Amount: 700},
	}, 30)

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
