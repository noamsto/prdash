package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
)

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

func TestColumnLadder(t *testing.T) {
	for _, tc := range []struct {
		cells                           int
		diff, compact, ticket, initials bool
	}{
		{120, true, false, true, false},
		{92, true, false, true, false},
		{91, true, true, true, false},
		{80, true, true, true, false},
		{79, true, true, true, true},
		{70, true, true, true, true},
		{69, true, true, false, true},
		{62, true, true, false, true},
		{61, false, true, false, true},
		{40, false, true, false, true},
	} {
		l := columnLadder(tc.cells)
		if l.ShowDiffstat != tc.diff || l.CompactDiffstat != tc.compact ||
			l.ShowTicket != tc.ticket || l.InitialsAuthor != tc.initials {
			t.Errorf("columnLadder(%d) = diff:%v compact:%v ticket:%v initials:%v, want %v %v %v %v",
				tc.cells, l.ShowDiffstat, l.CompactDiffstat, l.ShowTicket, l.InitialsAuthor,
				tc.diff, tc.compact, tc.ticket, tc.initials)
		}
	}
}

// TestColumnLadderMeasuresThePaneInterior pins which width the rungs are named
// for. computeLayout feeds columnLadder the pane interior, so below
// sideThreshold — where the list takes the whole terminal — each rung fires two
// terminal cells wider than its constant. Asserting the pairs either side of
// every rung is what catches a regression to measuring the bordered column,
// which TestColumnLadder above cannot see: it calls columnLadder directly.
func TestColumnLadderMeasuresThePaneInterior(t *testing.T) {
	for _, tc := range []struct {
		w                               int
		diff, compact, ticket, initials bool
	}{
		{94, true, false, true, false},
		{93, true, true, true, false},
		{82, true, true, true, false},
		{81, true, true, true, true},
		{72, true, true, true, true},
		{71, true, true, false, true},
		{64, true, true, false, true},
		{63, false, true, false, true},
	} {
		l := computeLayout(tc.w, 40)
		if l.ListInner != tc.w-2 {
			t.Fatalf("computeLayout(%d,40).ListInner = %d, want %d (test would sweep the wrong dimension)", tc.w, l.ListInner, tc.w-2)
		}
		if l.ShowDiffstat != tc.diff || l.CompactDiffstat != tc.compact ||
			l.ShowTicket != tc.ticket || l.InitialsAuthor != tc.initials {
			t.Errorf("computeLayout(%d,40) = diff:%v compact:%v ticket:%v initials:%v, want %v %v %v %v",
				tc.w, l.ShowDiffstat, l.CompactDiffstat, l.ShowTicket, l.InitialsAuthor,
				tc.diff, tc.compact, tc.ticket, tc.initials)
		}
	}
}

// TestTitleNeverStarvesAcrossTheSweep is the ladder's whole reason to exist: at
// every width the board can be handed, enough optional columns shed that the
// title still gets a readable slice of the row.
//
// Asserted against a rendered row, not against a hand-recomputed column budget:
// that arithmetic lives in renderItemRow, and a second copy here would drift
// from it and start guarding a fiction. The fixture carries the worst case for
// every optional column at once — a 39-character login (GitHub's maximum), a
// 5-digit diffstat, and a parseable Linear ticket — so the columns the ladder
// sheds are all really present at the top of the sweep.
func TestTitleNeverStarvesAcrossTheSweep(t *testing.T) {
	const title = "responsive ladder title long enough to be truncated"
	probe := title[:8]

	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{
		Number: 3087, Title: title, State: "OPEN", HeadRefName: "eng-7726-ladder",
		Additions: 12300, Deletions: 2000, UpdatedAt: time.Now().Add(-3 * 24 * time.Hour),
	}})
	// A login with a separator, so the initials form is its full 2 cells rather
	// than the 1 cell a single-word login collapses to.
	s.prs[0].Author.Login = strings.Repeat("l", 19) + "-" + strings.Repeat("m", 19)
	s.SetShown([]int{0})

	if full, compact := diffstatWidth(s, false), diffstatWidth(s, true); compact >= full {
		t.Fatalf("fixture diffstat: compact %d cells is not narrower than full %d (sweep would prove nothing)", compact, full)
	}
	if ticketWidth(s) == 0 {
		t.Fatal("fixture head ref parses no ticket id; the ticket column would never be present to shed")
	}

	for w := 40; w <= 200; w++ {
		for h := 10; h <= 60; h += 5 {
			l := computeLayout(w, h)
			diffW := 0
			if l.ShowDiffstat {
				diffW = diffstatWidth(s, l.CompactDiffstat)
			}
			tktW := 0
			if l.ShowTicket {
				tktW = ticketWidth(s)
			}
			innerW := l.ListInner // renderList's row width
			row := s.RenderRow(0, RowOpts{
				Width: innerW, NumWidth: columnWidths(s), DiffWidth: diffW, TicketWidth: tktW,
				CompactDiff: l.CompactDiffstat, Initials: l.InitialsAuthor,
			})
			if got := lipgloss.Width(row); got != innerW {
				t.Fatalf("w=%d h=%d: row width %d, want exactly %d", w, h, got, innerW)
			}
			if !strings.Contains(stripANSIForTest(row), probe) {
				t.Fatalf("w=%d h=%d: title starved (list=%d diffW=%d tktW=%d initials=%v): %q missing from %q",
					w, h, l.ListWidth, diffW, tktW, l.InitialsAuthor, probe, stripANSIForTest(row))
			}
		}
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

func TestComputeExpandedLayoutHidesFooterOnSmallWindow(t *testing.T) {
	small := computeExpandedLayout(90, 14, true) // below footerMinHeight
	if small.ShowFooter {
		t.Fatal("small window should hide the expanded footer")
	}
	large := computeExpandedLayout(90, 40, true)
	if !large.ShowFooter {
		t.Fatal("large window should show the expanded footer")
	}
	// Hiding the footer gives its row back to the viewport: VPHeight at the
	// same width/isPR should be exactly one taller with the footer hidden than
	// shown, all else equal (compare two heights one apart, straddling the
	// footer floor, at the same metaRows state).
	shown := computeExpandedLayout(90, footerMinHeight, true)
	hidden := computeExpandedLayout(90, footerMinHeight-1, true)
	if hidden.VPHeight != shown.VPHeight {
		t.Fatalf("hidden.VPHeight=%d shown.VPHeight=%d, want equal (one less input row, one fewer footer row, cancel out)", hidden.VPHeight, shown.VPHeight)
	}
}
