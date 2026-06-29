package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestReviewDot(t *testing.T) {
	cases := map[string]string{
		"APPROVED":          "✓",
		"CHANGES_REQUESTED": "✗",
		"REVIEW_REQUIRED":   "●",
		"":                  "·",
	}
	for decision, want := range cases {
		if got := reviewDot(decision); !strings.Contains(got, want) {
			t.Errorf("reviewDot(%q) = %q, want to contain %q", decision, got, want)
		}
	}
}

func TestRenderItemRowIsSingleLine(t *testing.T) {
	o := RowOpts{Width: 80, Focused: true, Selected: true, Flag: failStyle.Render("⚠")}
	row := renderItemRow(o, "#7", "hello world", "alice", "2d",
		ciGlyph("fail"), reviewDot("APPROVED"))
	if strings.Contains(row, "\n") {
		t.Fatalf("dense row must be one line: %q", row)
	}
	for _, want := range []string{"#7", "hello world", "alice", "2d", "▎", "●", "⚠"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row missing %q: %q", want, row)
		}
	}
}

func TestPRSectionRenderRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "hello world", HeadRefName: "feat/x"}})

	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, "#7") || !strings.Contains(row, "hello world") {
		t.Fatalf("row missing number/title: %q", row)
	}

	sel := s.RenderRow(0, RowOpts{Width: 80, Selected: true})
	if !strings.Contains(sel, "●") {
		t.Fatalf("selected row should carry the ● marker: %q", sel)
	}
}

func TestSetPRsSortsByActionability(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{
		{Number: 1, IsDraft: true},
		{Number: 2, ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}},
		{Number: 3, ReviewDecision: "CHANGES_REQUESTED"},
		{Number: 4, StatusCheckRollup: []gh.Check{{Conclusion: "FAILURE"}}},
		{Number: 5, StatusCheckRollup: []gh.Check{{Conclusion: "IN_PROGRESS"}}},
		{Number: 6, ReviewDecision: "REVIEW_REQUIRED"},
	})
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	// ready(2) → changes(3) → fail(4) → running(5) → waiting(6) → draft(1)
	want := []int{2, 3, 4, 5, 6, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("sort order = %v, want %v", got, want)
	}
}

func TestDraftRowIsStyledDistinctly(t *testing.T) {
	args := func(o RowOpts) string {
		return renderItemRow(o, "#1", "title", "alice", "2d", ciGlyph("pass"), reviewDot(""))
	}
	plain := args(RowOpts{Width: 80})
	draft := args(RowOpts{Width: 80, Draft: true})
	if plain == draft {
		t.Fatal("a draft row must render distinctly (dimmed) from a normal row")
	}
}

func TestPRSectionMarksDraftRow(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{Number: 1, Title: "wip", IsDraft: true}})
	normal := NewPRSection("")
	normal.SetPRs([]gh.PR{{Number: 1, Title: "wip"}})
	if s.RenderRow(0, RowOpts{Width: 80}) == normal.RenderRow(0, RowOpts{Width: 80}) {
		t.Fatal("PRSection.RenderRow should style a draft PR distinctly")
	}
}

func TestPadNumRightAligns(t *testing.T) {
	if got := padNum("#7", 5); got != "   #7" {
		t.Fatalf("padNum(#7,5) = %q, want %q", got, "   #7")
	}
	if got := padNum("#1234", 3); got != "#1234" { // never truncates below content
		t.Fatalf("padNum(#1234,3) = %q, want %q", got, "#1234")
	}
}

func TestColumnWidthsUsesWidestNumber(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{Number: 7}, {Number: 1234}})
	if got := columnWidths(s); got != len("#1234") {
		t.Fatalf("columnWidths = %d, want %d", got, len("#1234"))
	}
}
