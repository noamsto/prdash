package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTitledBoxDimensionsAndTitle(t *testing.T) {
	const w, h = 30, 6
	box := titledBox("alpha\nbeta", w, h, "PRs · 12")
	lines := strings.Split(box, "\n")
	if len(lines) != h {
		t.Fatalf("box height = %d lines, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Fatalf("line %d width = %d, want %d (%q)", i, got, w, ln)
		}
	}
	if !strings.Contains(box, "PRs · 12") {
		t.Fatalf("box should carry its title: %q", box)
	}
	if !strings.Contains(lines[0], "╭") || !strings.Contains(lines[0], "╮") {
		t.Fatalf("top line should have rounded corners: %q", lines[0])
	}
	if !strings.Contains(lines[h-1], "╰") || !strings.Contains(lines[h-1], "╯") {
		t.Fatalf("bottom line should have rounded corners: %q", lines[h-1])
	}
}

func TestTitledBoxClipsOverflow(t *testing.T) {
	tall := strings.Repeat("x\n", 20)
	box := titledBox(tall, 12, 5, "t")
	if got := len(strings.Split(box, "\n")); got != 5 {
		t.Fatalf("overflowing content must clip to the box height; got %d lines, want 5", got)
	}
}

func TestTitledBoxLongTitleStaysWidthW(t *testing.T) {
	const w, h = 16, 4
	box := titledBox("body", w, h, strings.Repeat("A", 40)) // title far wider than the box
	for i, ln := range strings.Split(box, "\n") {
		if got := lipgloss.Width(ln); got != w {
			t.Fatalf("line %d width = %d, want %d (a saturating title must not overflow): %q", i, got, w, ln)
		}
	}
}

func TestClipLines(t *testing.T) {
	if got := clipLines("a\nb\nc\nd", 2); got != "a\nb" {
		t.Fatalf("clipLines = %q, want %q", got, "a\nb")
	}
	if got := clipLines("a\nb", 5); got != "a\nb" { // fewer lines than the cap is untouched
		t.Fatalf("clipLines = %q, want %q", got, "a\nb")
	}
}

func TestDropLines(t *testing.T) {
	if got := dropLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Fatalf("dropLines = %q, want %q", got, "c\nd")
	}
	if got := dropLines("a\nb", 5); got != "" {
		t.Fatalf("dropping more than present should empty: %q", got)
	}
}

func TestModalCentersPanel(t *testing.T) {
	panel := titledBox("body", 12, 3, "Actions")
	out := modal(panel, 40, 11)
	lines := strings.Split(out, "\n")
	if len(lines) != 11 {
		t.Fatalf("modal height = %d, want 11", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != 40 {
			t.Fatalf("line %d width = %d, want 40", i, lipgloss.Width(ln))
		}
	}
	if !strings.Contains(out, "Actions") {
		t.Fatalf("modal should contain the panel: %q", out)
	}
}
