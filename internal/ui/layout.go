package ui

// sideThreshold is the minimum terminal width at which the side preview shows.
const sideThreshold = 120

// chromeRows is the vertical space taken by the header + spacer + status bar
// (rule + keys = 2 rows).
const chromeRows = 4

// panelRows is the height of the docked keys/actions panel (top border + 6
// content lines + bottom border).
const panelRows = 8

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
	ContentHeight int // rows available for the list/side bodies
}

// computeLayout derives pane geometry from the terminal size. The keys/actions
// panel is reserved purely on height (never list length, which would flicker as
// you filter or scroll).
func computeLayout(w, h int) Layout {
	showPanel := h-2-panelRows >= minMainRows
	ch := h - chromeRows
	if showPanel {
		ch = h - 2 - panelRows // header + spacer above, panel below
	}
	if ch < 1 {
		ch = 1
	}
	if w < sideThreshold {
		return Layout{ShowSide: false, ShowPanel: showPanel, ListWidth: w, ContentHeight: ch}
	}
	const gap = 2
	side := w * 55 / 100
	list := w - side - gap
	return Layout{ShowSide: true, ShowPanel: showPanel, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch}
}
