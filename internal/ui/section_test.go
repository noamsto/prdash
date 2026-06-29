package ui

import (
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
