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

// ExpandedLayout splits the expanded view by terminal width: a metadata rail box
// beside a content box when wide, a single content box when narrow. RailW/ContentW
// are the OUTER box widths (each box owns its own rounded border + 1-col padding,
// so inner content is width-4); RailH is the box outer height; VPHeight is the
// scrollable viewport's inner height.
func ExpandedLayout(w, h int) ExpandedGeom {
	bodyH := h - 2 // header + footer rows
	if bodyH < 3 {
		bodyH = 3
	}
	if w < sideThreshold {
		vp := bodyH - 4 // box border (2) + meta line + tab strip
		if vp < 1 {
			vp = 1
		}
		return ExpandedGeom{TwoCol: false, ContentW: w, RailH: bodyH, VPHeight: vp}
	}
	const gap = 2
	rail := max(32, min(w*30/100, 44))
	vp := bodyH - 3 // box border (2) + tab strip
	if vp < 1 {
		vp = 1
	}
	return ExpandedGeom{TwoCol: true, RailW: rail, RailH: bodyH, ContentW: w - rail - gap, VPHeight: vp}
}
