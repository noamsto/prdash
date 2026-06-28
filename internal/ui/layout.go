package ui

// sideThreshold is the minimum terminal width at which the side preview shows.
const sideThreshold = 120

// chromeRows is the vertical space taken by the header + spacer + status bar.
const chromeRows = 3

// Layout is the computed geometry for one frame.
type Layout struct {
	ShowSide      bool
	ListWidth     int
	SideWidth     int
	Gap           int // columns between list and side pane
	ContentHeight int // rows available for the list/side bodies
}

// computeLayout derives pane geometry from the terminal size.
func computeLayout(w, h int) Layout {
	ch := h - chromeRows
	if ch < 1 {
		ch = 1
	}
	if w < sideThreshold {
		return Layout{ShowSide: false, ListWidth: w, ContentHeight: ch}
	}
	const gap = 2
	side := w * 55 / 100
	list := w - side - gap
	return Layout{ShowSide: true, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch}
}

// ExpandedGeom is the computed geometry for the expanded detail view.
type ExpandedGeom struct {
	TwoCol   bool
	RailW    int // metadata rail width (two-col only)
	RailH    int // metadata rail height (two-col only)
	ContentW int // width of the tab/content pane (and viewport)
	VPHeight int // scrollable viewport height
}

// ExpandedLayout splits the expanded view by terminal width: a metadata rail +
// content pane when wide, a single column with a compact meta header when narrow.
// Dimensions are the interior of the outer frame (rounded border + h-padding):
// 4 cols and 2 rows narrower than the body between header and footer.
func ExpandedLayout(w, h int) ExpandedGeom {
	innerW := w - 4 // outer border (2) + h-padding (2)
	innerH := h - 4 // header + footer (2) + outer border (2)
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	if w < sideThreshold {
		vp := innerH - 2 // compact meta line + tab strip
		if vp < 1 {
			vp = 1
		}
		return ExpandedGeom{TwoCol: false, ContentW: innerW, VPHeight: vp}
	}
	const gap = 2
	rail := max(32, min(innerW*30/100, 44))
	vp := innerH - 1 // tab strip
	if vp < 1 {
		vp = 1
	}
	return ExpandedGeom{TwoCol: true, RailW: rail, RailH: innerH, ContentW: innerW - rail - gap, VPHeight: vp}
}
