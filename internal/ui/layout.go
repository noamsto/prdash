package ui

// sideThreshold is the minimum terminal width at which the side preview shows.
const sideThreshold = 120

// chromeRows is the vertical space taken by the header + spacer + status bar.
const chromeRows = 3

// Layout is the computed geometry for one frame. Pure output of computeLayout.
type Layout struct {
	ShowSide      bool
	ListWidth     int
	SideWidth     int
	Gap           int // columns between list and side pane
	ContentHeight int // rows available for the list/side bodies
}

// computeLayout derives pane geometry from the terminal size. Pure + tested.
func computeLayout(w, h int) Layout {
	ch := h - chromeRows
	if ch < 1 {
		ch = 1
	}
	if w < sideThreshold {
		return Layout{ShowSide: false, ListWidth: w, ContentHeight: ch}
	}
	const gap = 2
	side := w * 45 / 100
	list := w - side - gap
	return Layout{ShowSide: true, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch}
}
