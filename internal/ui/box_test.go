package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// TestTabSegmentWidthBoundedKeepsActiveVisible guards the tab-strip windowing
// invariant: unlike renderTabBar's side-pane strip (which can just drop trailing tabs),
// tabSegment must never exceed maxW while still windowing the visible tabs so
// the active one — even a trailing tab like Diff — always shows in full. 16 is
// the narrowest width that fits the widest tab ("Conversation", 12+2 padding)
// plus its flanking ticks with no truncation; realistic terminals (ContentW is
// never below footerMinWidth-ish) sit far above this floor.
func TestTabSegmentWidthBoundedKeepsActiveVisible(t *testing.T) {
	tabs := []string{"Overview", "Description", "Conversation", "Reviews", "Checks", "Diff"}
	for maxW := 16; maxW <= 40; maxW++ {
		for active := range tabs {
			seg := tabSegment(tabs, active, maxW)
			if got := lipgloss.Width(seg); got > maxW {
				t.Fatalf("maxW=%d active=%d: segment width %d exceeds maxW: %q", maxW, active, got, seg)
			}
			plain := ansi.Strip(seg)
			if !strings.Contains(plain, tabs[active]) {
				t.Fatalf("maxW=%d active=%d: active tab %q not visible in windowed segment: %q", maxW, active, tabs[active], plain)
			}
		}
	}
}

// TestTabSegmentDegenerateWidthNeverExceedsBudget covers below-floor widths
// (narrower than any single tab needs): the active tab may get truncated down
// to nothing recognizable, but the hard width bound must still hold and the
// function must never panic.
func TestTabSegmentDegenerateWidthNeverExceedsBudget(t *testing.T) {
	tabs := []string{"Overview", "Description", "Conversation", "Reviews", "Checks", "Diff"}
	for maxW := 0; maxW <= 15; maxW++ {
		for active := range tabs {
			seg := tabSegment(tabs, active, maxW)
			if got := lipgloss.Width(seg); got > maxW {
				t.Fatalf("maxW=%d active=%d: segment width %d exceeds maxW: %q", maxW, active, got, seg)
			}
		}
	}
}

// TestTabbedBoxTopLineNeverExceedsWidth locks tabbedBox's contribution to the
// tab-strip-never-overflows invariant: at every width, the composed top edge (tabSegment plus the
// boxTop corner/rule chrome) must stay exactly at the box's outer width.
func TestTabbedBoxTopLineNeverExceedsWidth(t *testing.T) {
	tabs := []string{"Overview", "Description", "Conversation", "Reviews", "Checks", "Diff"}
	for w := 4; w <= 80; w++ {
		for active := range tabs {
			box := tabbedBox("body", w, 5, tabs, active)
			top := strings.SplitN(box, "\n", 2)[0]
			if got := lipgloss.Width(top); got != w {
				t.Fatalf("w=%d active=%d: top line width %d, want %d: %q", w, active, got, w, top)
			}
		}
	}
}

func TestOverlayAnchorsPanelToFixedTop(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 11; i++ {
		b.WriteString(strings.Repeat("x", 40) + "\n")
	}
	base := strings.TrimRight(b.String(), "\n")
	panel := titledBox("body", 12, 3, "Actions")
	out := overlayTop(base, panel, 40, 11)
	lines := strings.Split(out, "\n")
	if len(lines) != 11 {
		t.Fatalf("overlay height = %d, want 11", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != 40 {
			t.Fatalf("line %d width = %d, want 40", i, lipgloss.Width(ln))
		}
	}
	if !strings.Contains(out, "Actions") {
		t.Fatalf("overlay should contain the panel: %q", out)
	}
	if !strings.Contains(out, "x") {
		t.Fatalf("overlay should keep the base visible around the panel: %q", out)
	}
	// Anchored at h/5 = row 2, not centered (which would be row 4) — the rows
	// above stay pure base so the panel top never drifts with panel height.
	for _, i := range []int{0, 1} {
		if strings.Contains(lines[i], "Actions") {
			t.Fatalf("row %d should be clear base above the fixed-top anchor: %q", i, lines[i])
		}
	}
	if !strings.Contains(lines[2], "Actions") {
		t.Fatalf("panel top should sit at the fixed-top row 2: %q", lines[2])
	}
}
