package ui

import "testing"

func TestLayoutWideShowsSide(t *testing.T) {
	l := computeLayout(160, 40)
	if !l.ShowSide {
		t.Fatal("wide terminal should show the side pane")
	}
	if l.ListWidth <= 0 || l.SideWidth <= 0 {
		t.Fatalf("both panes need positive width: %+v", l)
	}
	if l.ListWidth+l.SideWidth+l.Gap > 160 {
		t.Fatalf("panes (%d + gap %d + %d) exceed terminal width 160", l.ListWidth, l.Gap, l.SideWidth)
	}
}

func TestLayoutNarrowHidesSide(t *testing.T) {
	l := computeLayout(90, 40)
	if l.ShowSide {
		t.Fatal("narrow terminal should hide the side pane")
	}
	if l.ListWidth != 90 {
		t.Fatalf("list should take full width when side is hidden: got %d", l.ListWidth)
	}
}

func TestLayoutContentHeight(t *testing.T) {
	// Tall terminal: the docked panel is reserved, so the main area is
	// h - spacerRows(2) - panelRows.
	l := computeLayout(160, 40)
	if !l.ShowPanel {
		t.Fatal("expected the panel to be reserved at h=40")
	}
	if want := 40 - 2 - l.PanelRows; l.ContentHeight != want {
		t.Fatalf("tall ContentHeight = %d, want %d", l.ContentHeight, want)
	}
	// Short terminal: no panel, so the main area is h - chromeRows(4) = 12.
	if l := computeLayout(160, 16); l.ShowPanel || l.ContentHeight != 12 {
		t.Fatalf("short: ShowPanel=%v ContentHeight=%d, want false/12", l.ShowPanel, l.ContentHeight)
	}
}
