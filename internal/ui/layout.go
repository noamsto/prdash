package ui

// sideThreshold is the minimum terminal width at which the side preview shows.
const sideThreshold = 120

// chromeRows is the vertical space taken by the header + spacer + status bar
// (rule + keys = 2 rows).
const chromeRows = 4

// minMainRows is the least list height worth keeping; below it the panel yields
// to the one-line status bar so short terminals aren't crowded out.
const minMainRows = 8

// footerMinHeight/footerMinWidth are the smallest terminal at which the
// always-on keybinding footer (board status bar/panel, expanded footer, log
// footer) earns its row. Below either floor, `?` becomes the only way to see
// the keys and the footer's row(s) go back to content instead.
const (
	footerMinHeight = 20
	footerMinWidth  = 70
)

// twoLineMinRows is the list content height at or above which rows render
// two lines (title, then labels + branch). Below it, rows stay single-line
// and drop labels — there isn't the vertical room to spend on a second line.
const twoLineMinRows = 20

// showFooter is the one threshold every view (board, expanded, log) calls to
// decide whether to render its always-on keybinding footer.
func showFooter(w, h int) bool {
	return w >= footerMinWidth && h >= footerMinHeight
}

// Layout is the computed geometry for one frame.
type Layout struct {
	ShowSide      bool
	ShowFooter    bool // false hides the footer entirely, reclaiming its row(s) for content
	ShowPanel     bool // dock the keys/actions panel instead of the status bar (only when ShowFooter)
	ListWidth     int
	SideWidth     int
	Gap           int // columns between list and side pane
	PanelRows     int // outer height of the docked panel (0 when not shown)
	ContentHeight int // rows available for the list/side bodies
	TwoLine       bool
}

// computeLayout derives pane geometry from the terminal size. The panel is
// reserved purely on height (never list length, which would flicker as you
// filter or scroll), and its height is taken from the width of the column it
// docks under — the list column when the preview is showing, else full width.
func computeLayout(w, h int) Layout {
	const gap = 2
	side := w * 55 / 100
	list := w - side - gap
	showSide := w >= sideThreshold
	footer := showFooter(w, h)

	panelCol := w // narrow: panel spans the whole width
	if showSide {
		panelCol = list // wide: panel sits under the list only
	}
	pr := panelRowsFor(panelCol - 2)
	showPanel := footer && h-2-pr >= minMainRows

	var ch int
	switch {
	case !footer:
		pr = 0
		ch = h - 2 // header + one row of slack; no footer rows to reserve
	case showPanel:
		ch = h - 2 - pr // header + spacer above; panel (or its column) below
	default:
		pr = 0
		ch = h - chromeRows
	}
	if ch < 1 {
		ch = 1
	}
	twoLine := ch >= twoLineMinRows
	if !showSide {
		return Layout{ShowSide: false, ShowFooter: footer, ShowPanel: showPanel, PanelRows: pr, ListWidth: w, ContentHeight: ch, TwoLine: twoLine}
	}
	return Layout{ShowSide: true, ShowFooter: footer, ShowPanel: showPanel, PanelRows: pr, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch, TwoLine: twoLine}
}

const (
	expandedRailMin    = 32                     // rail never narrower than this in two-col
	expandedRailMax    = 44                     // …nor wider (a metadata rail past ~44 is wasted)
	railInset          = 2                      // 1-cell gutter each side of the rail's content
	expandedColGap     = 2                      // cells between rail and content
	expandedContentCap = discussionMaxWidth + 6 // 110, the reading-column cap (was in expandedBoxWidth)
	// two-col only when a full rail AND a full-width content pane both fit.
	expandedTwoColMin = expandedContentCap + expandedRailMin + expandedColGap // 144
)

// ExpandedLayout is the computed geometry for the expanded detail frame. It is
// the single height/width authority for that view — callers never re-derive.
type ExpandedLayout struct {
	TwoCol     bool
	ShowFooter bool
	RailW      int
	RailH      int
	ContentW   int
	VPHeight   int
}

// computeExpandedLayout derives the expanded-view geometry from the terminal
// size and section kind. Two-col is PR-only (issues stay a centered single
// column at every width). The chrome/meta row count is section-aware: a PR
// carries a one-line meta row only in narrow mode (in two-col that content
// moves into the rail), so there is one height authority and no narrow-PR
// off-by-one. Floors mirror the pre-helper box-height (min-3) and
// setExpandedContent (min-1) so tiny terminals never hand vp a negative.
func computeExpandedLayout(w, h int, isPR bool) ExpandedLayout {
	twoCol := isPR && w >= expandedTwoColMin
	footer := showFooter(w, h)

	metaRows := 0
	if isPR && !twoCol {
		metaRows = 1
	}
	footRows := 0
	if footer {
		footRows = 1
	}
	body := h - (1 + footRows + metaRows) // head (+ footer) (+ narrow-PR meta)
	if body < 3 {
		body = 3
	}

	l := ExpandedLayout{TwoCol: twoCol, ShowFooter: footer}
	if twoCol {
		l.ContentW = expandedContentCap
		l.RailW = min(max(w-expandedColGap-l.ContentW, expandedRailMin), expandedRailMax)
		l.RailH = body
	} else {
		l.ContentW = min(w, expandedContentCap)
	}
	l.VPHeight = body - 2 // tabbedBox top tab/border line + bottom border row
	if l.VPHeight < 1 {
		l.VPHeight = 1
	}
	if l.ContentW < 1 {
		l.ContentW = 1
	}
	return l
}
