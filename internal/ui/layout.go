package ui

// sideThreshold is the minimum terminal width at which the side preview shows.
const sideThreshold = 120

// chromeRows is the vertical space taken by the header + spacer + status bar
// (rule + keys = 2 rows).
const chromeRows = 4

// minMainRows is the least list height worth keeping; below it the panel yields
// to the one-line status bar so short terminals aren't crowded out.
const minMainRows = 8

// Layout is the computed geometry for one frame.
type Layout struct {
	ShowSide      bool
	ShowPanel     bool // dock the keys/actions panel instead of the status bar
	ListWidth     int
	SideWidth     int
	Gap           int // columns between list and side pane
	PanelRows     int // outer height of the docked panel (0 when not shown)
	ContentHeight int // rows available for the list/side bodies
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

	panelCol := w // narrow: panel spans the whole width
	if showSide {
		panelCol = list // wide: panel sits under the list only
	}
	pr := panelRowsFor(panelCol - 2)
	showPanel := h-2-pr >= minMainRows

	ch := h - chromeRows
	if showPanel {
		ch = h - 2 - pr // header + spacer above; panel (or its column) below
	} else {
		pr = 0
	}
	if ch < 1 {
		ch = 1
	}
	if !showSide {
		return Layout{ShowSide: false, ShowPanel: showPanel, PanelRows: pr, ListWidth: w, ContentHeight: ch}
	}
	return Layout{ShowSide: true, ShowPanel: showPanel, PanelRows: pr, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch}
}
