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
	// Short terminal (footer shown, panel not reserved): main area is
	// h - chromeRows(4) = 18.
	if l := computeLayout(160, 22); l.ShowPanel || !l.ShowFooter || l.ContentHeight != 18 {
		t.Fatalf("short: ShowFooter=%v ShowPanel=%v ContentHeight=%d, want true/false/18", l.ShowFooter, l.ShowPanel, l.ContentHeight)
	}
}

func TestShowFooterThreshold(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"large", 120, 30, true},
		{"short height", 120, 14, false},
		{"narrow width", 50, 30, false},
		{"both small", 50, 14, false},
		{"just above both floors", footerMinWidth, footerMinHeight, true},
		{"just below height floor", footerMinWidth, footerMinHeight - 1, false},
		{"just below width floor", footerMinWidth - 1, footerMinHeight, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := showFooter(c.w, c.h); got != c.want {
				t.Errorf("showFooter(%d,%d) = %v, want %v", c.w, c.h, got, c.want)
			}
		})
	}
}

func TestLayoutHidesFooterOnSmallWindow(t *testing.T) {
	l := computeLayout(120, 14) // below footerMinHeight
	if l.ShowFooter {
		t.Fatal("small window should hide the footer")
	}
	if l.ShowPanel {
		t.Fatal("panel must never show when the footer itself is hidden")
	}
	// Every row the footer would have used goes back to content: ContentHeight
	// is now h - 2 (header + one row of breathing room, matching the existing
	// slack in the ShowPanel/chromeRows branches), not h - chromeRows(4).
	if want := 14 - 2; l.ContentHeight != want {
		t.Fatalf("ContentHeight = %d, want %d (footer rows reclaimed)", l.ContentHeight, want)
	}

	wide := computeLayout(120, 30)
	if !wide.ShowFooter {
		t.Fatal("large window should show the footer")
	}
}

func TestComputeExpandedLayoutSelection(t *testing.T) {
	const h = 40
	cases := []struct {
		name                 string
		w                    int
		isPR                 bool
		twoCol               bool
		contentW, railW, vpH int
	}{
		// PR: TwoCol false at 143, true at 144 (the expandedTwoColMin boundary).
		{"pr-just-below", 143, true, false, 110, 0, 35},
		{"pr-at-cutoff", 144, true, true, 110, 32, 36},
		{"pr-wide", 200, true, true, 110, 44, 36},
		{"pr-narrow", 90, true, false, 90, 0, 35},
		// Issue: never two-col, even wide → no dead rail.
		{"issue-wide", 160, false, false, 110, 0, 36},
		{"issue-narrow", 90, false, false, 90, 0, 36},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := computeExpandedLayout(c.w, h, c.isPR)
			if l.TwoCol != c.twoCol {
				t.Errorf("TwoCol = %v, want %v", l.TwoCol, c.twoCol)
			}
			if l.ContentW != c.contentW {
				t.Errorf("ContentW = %d, want %d", l.ContentW, c.contentW)
			}
			if l.RailW != c.railW {
				t.Errorf("RailW = %d, want %d", l.RailW, c.railW)
			}
			if l.VPHeight != c.vpH {
				t.Errorf("VPHeight = %d, want %d", l.VPHeight, c.vpH)
			}
			if c.twoCol && l.RailW+expandedColGap+l.ContentW > c.w {
				t.Errorf("two-col columns %d+%d+%d exceed w=%d", l.RailW, expandedColGap, l.ContentW, c.w)
			}
		})
	}
}

func TestComputeExpandedLayoutSectionAwareHeight(t *testing.T) {
	const w, h = 90, 40
	pr := computeExpandedLayout(w, h, true)   // narrow PR: carries a meta row
	iss := computeExpandedLayout(w, h, false) // narrow issue: no meta row
	if pr.VPHeight != iss.VPHeight-1 {
		t.Errorf("narrow PR VPHeight = %d, want one less than issue %d", pr.VPHeight, iss.VPHeight)
	}
	// A two-col PR must NOT lose a row to a phantom narrow-meta line.
	twoCol := computeExpandedLayout(160, h, true)
	if twoCol.VPHeight != iss.VPHeight {
		t.Errorf("two-col PR VPHeight = %d, want %d (no phantom meta row)", twoCol.VPHeight, iss.VPHeight)
	}
}
