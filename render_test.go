package main

import (
	"strings"
	"testing"
)

func TestRenderTableSVG(t *testing.T) {
	svg := renderTableSVG([]LanguageProgress{
		{LanguageName: "French", Percent: 80},
		{LanguageName: "German", Percent: 42},
	})
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
	svg := renderTableSVG(nil)
	if !strings.Contains(svg, "no language data yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}

func TestRenderContributorsSVG(t *testing.T) {
	svg := renderContributorsSVG([]Contributor{
		{Username: "alice", FullName: "Alice A", Amount: 100},
		{Username: "bob", FullName: "Bob B", Amount: 50},
	}, 30)
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
	svg := renderContributorsSVG(contributors, 3)
	count := strings.Count(svg, "<clipPath")
	if count != 3 {
		t.Fatalf("expected 3 avatars, got %d: %s", count, svg)
	}
}

func TestRenderContributorsSVGEmpty(t *testing.T) {
	svg := renderContributorsSVG(nil, 30)
	if !strings.Contains(svg, "no contributors yet") {
		t.Fatalf("expected empty state message, got: %s", svg)
	}
}
