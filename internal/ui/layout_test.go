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
	l := computeLayout(160, 40)
	if l.ContentHeight != 37 {
		t.Fatalf("ContentHeight = %d, want 37", l.ContentHeight)
	}
}
