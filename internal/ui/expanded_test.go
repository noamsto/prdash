package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestJumpTabIndex(t *testing.T) {
	cases := map[string]int{"reviews": 1, "checks": 2, "diff": 3, "conversation": 0, "": 0}
	for jump, want := range cases {
		if got := jumpTabIndex(jump); got != want {
			t.Errorf("jumpTabIndex(%q) = %d, want %d", jump, got, want)
		}
	}
}

func TestRenderChecksListsByName(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"},
		{State: "SUCCESS", Name: "build"},
	}}
	out := renderChecks(pr, 60)
	if !strings.Contains(out, "lint") || !strings.Contains(out, "build") {
		t.Fatalf("checks not listed by name: %q", out)
	}
}

func TestRenderDiffstatTotals(t *testing.T) {
	d := gh.PRDetail{Files: []gh.DiffFile{
		{Path: "a.go", Additions: 10, Deletions: 2},
		{Path: "b.go", Additions: 1, Deletions: 1},
	}}
	out := renderDiffstat(d, 60)
	if !strings.Contains(out, "2 files") || !strings.Contains(out, "a.go") {
		t.Fatalf("diffstat missing totals/files: %q", out)
	}
}

func TestRenderReviewsEmpty(t *testing.T) {
	if !strings.Contains(renderReviews(gh.PRDetail{}, 60), "No reviews") {
		t.Fatal("empty reviews should say so")
	}
}

func TestTabStripMarksActive(t *testing.T) {
	out := tabStrip(2)
	for _, name := range expandedTabs {
		if !strings.Contains(out, name) {
			t.Fatalf("tab %q missing from strip: %q", name, out)
		}
	}
}
