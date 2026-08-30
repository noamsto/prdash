package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// indentLines prefixes every line of s with n spaces.
func indentLines(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// clipLines keeps at most the first n lines of s.
func clipLines(s string, n int) string {
	if n < 0 {
		n = 0
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// dropLines removes the first n lines of s (for scrolling).
func dropLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return ""
	}
	return strings.Join(lines[n:], "\n")
}

// boxFastDisabled forces boxBody onto boxBodyLipgloss, so a test can build the
// expected bytes with the same call it is checking.
var boxFastDisabled bool

// boxBody renders content inside a rounded left/right/bottom border of OUTER
// width w and OUTER height h; the top edge is drawn separately by the caller so
// a label or tab bar can be set into it. Content is clipped to the interior.
// Requires w >= 2 and h >= 2; titledBoxTinted and tabbedBox, the only callers,
// clamp to 4 and 2.
//
// The interior is already exactly the width the layout chose, so lipgloss's
// wrap, align, border and margin passes only re-measure what we picked. When the
// precondition scan can prove that, the rows are built directly; otherwise the
// lipgloss path renders them, bit-for-bit as before.
func boxBody(content string, w, h int) string {
	if boxFastDisabled {
		return boxBodyLipgloss(content, w, h)
	}
	lines, widths, reason := boxFastReason(content, w, h)
	if reason != "" {
		return boxBodyLipgloss(content, w, h)
	}

	rb := lipgloss.RoundedBorder()
	// Derived per call: applyTheme rebuilds sepStyle from the Update loop, so a
	// flank cached at package scope would paint the pre-switch palette.
	left, right := sepStyle.Render(rb.Left), sepStyle.Render(rb.Right)

	innerW := w - 2
	rows := max(1, h-2)
	out := make([]string, 0, rows+1)
	for i := range rows {
		line, lineW := "", 0
		if i < len(lines) {
			line, lineW = lines[i], widths[i]
		}
		out = append(out, left+line+strings.Repeat(" ", innerW-lineW)+right)
	}
	out = append(out, sepStyle.Render(rb.BottomLeft+strings.Repeat(rb.Bottom, innerW)+rb.BottomRight))
	// At h == 2 the single blank row is the whole box and this drops the bottom
	// edge, which is what lipgloss's MaxHeight does today.
	if budget := max(1, h-1); len(out) > budget {
		out = out[:budget]
	}
	return strings.Join(out, "\n")
}

// boxBodyLipgloss is boxBody's original body, kept as the fallback for content
// the fast path cannot prove equivalent.
func boxBodyLipgloss(content string, w, h int) string {
	rb := lipgloss.RoundedBorder()
	return lipgloss.NewStyle().
		Border(rb, false, true, true, true).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(w).Height(h - 1).MaxWidth(w).MaxHeight(h - 1).
		Render(clipLines(content, h-2))
}

// boxFastReason reports why boxBody cannot build content's rows directly at
// w x h, or "" when it can, along with the clipped lines and their widths so the
// fast path never splits or measures a second time.
//
// A width check alone is not enough: Style.Render rewrites its input before it
// measures it. Tabs expand to spaces, CRLF collapses to LF, and a pen left open
// at a line end is reset before the newline and re-emitted after it — none of
// which a display width can see.
func boxFastReason(content string, w, h int) (lines []string, widths []int, reason string) {
	innerW := w - 2
	// Below the documented w >= 2 there is no interior to pad to, and content
	// clipped to zero rows would skip the per-line width check that otherwise
	// catches this.
	if innerW < 0 {
		return nil, nil, fmt.Sprintf("interior width %d is negative", innerW)
	}
	lines = strings.Split(content, "\n")
	if len(lines) > h-2 {
		lines = lines[:max(0, h-2)]
	}
	widths = make([]int, len(lines))
	for i, ln := range lines {
		if strings.ContainsAny(ln, "\t\r") {
			return nil, nil, fmt.Sprintf("line %d has a tab or CR: %q", i, ln)
		}
		if pensOpenAtEnd(ln) {
			return nil, nil, fmt.Sprintf("line %d leaves a pen open: %q", i, ln)
		}
		widths[i] = ansi.StringWidth(ln)
		if widths[i] > innerW {
			return nil, nil, fmt.Sprintf("line %d is %d cells wide, interior is %d: %q", i, widths[i], innerW, ln)
		}
	}
	return lines, widths, ""
}

// pensOpenAtEnd reports whether an SGR style or an OSC 8 hyperlink is still open
// at the end of s. lipgloss's WrapWriter carries both pens independently and
// rewrites the line when either is open at a newline, so both have to be closed
// before rows can be emitted verbatim.
func pensOpenAtEnd(s string) bool {
	style, link := false, false
	for {
		i := strings.IndexByte(s, 0x1b)
		if i < 0 || i+1 >= len(s) {
			return style || link
		}
		rest := s[i+1:]
		switch rest[0] {
		case '[': // CSI, up to its final byte
			j := strings.IndexFunc(rest[1:], func(r rune) bool { return r >= '@' && r <= '~' })
			if j < 0 {
				return style || link
			}
			if rest[1+j] == 'm' {
				p := rest[1 : 1+j]
				style = !(p == "" || p == "0")
			}
			s = rest[2+j:]
		case ']': // OSC, up to ST or BEL
			body := rest[1:]
			end, adv := -1, 0
			if k := strings.IndexByte(body, 0x07); k >= 0 {
				end, adv = k, k+1
			}
			if k := strings.Index(body, "\x1b\\"); k >= 0 && (end < 0 || k < end) {
				end, adv = k, k+2
			}
			// An unterminated OSC leaves its pen open, so the answer is already
			// known. Returning also keeps this walk linear: resuming after the
			// introducer would rescan the whole tail for a terminator that isn't
			// there, once per introducer, which is quadratic on a line of them —
			// and preview and log content is arbitrary text off the network.
			if end < 0 {
				return true
			}
			// Mirrors ultraviolet's ReadLink, which lipgloss feeds the OSC data
			// to: anything but exactly three ;-separated fields — a URI holding
			// its own ';', say — leaves the pen where it was.
			if f := strings.Split(body[:end], ";"); len(f) == 3 && f[0] == "8" {
				link = f[1] != "" || f[2] != ""
			}
			s = body[adv:]
		default:
			s = rest[1:]
		}
	}
}

// joinFastDisabled forces joinBoard onto joinBoardLipgloss, so a test can build
// the expected bytes with the same call it is checking.
var joinFastDisabled bool

// joinBoard stacks the docked column (filter bar, list, and where it is shown
// the keys/actions panel) and sets side, the preview box, gap columns to its
// right — the shape lipgloss.JoinVertical + MarginLeft(gap).Render +
// lipgloss.JoinHorizontal produce.
//
// It takes the same maxima lipgloss takes rather than trusting the widths the
// layout handed out, because it cannot: titledBoxTinted clamps w up to 4, and
// filterBar's Style.Width stops padding at degenerate widths. What it skips is
// the three Style.Render passes, which re-measure blocks this pass has already
// measured once.
func joinBoard(stack []string, side string, gap int) string {
	if joinFastDisabled {
		return joinBoardLipgloss(stack, side, gap)
	}

	var leftLines []string
	var leftWidths []int
	commonW := 0
	for _, s := range stack {
		for _, ln := range strings.Split(s, "\n") {
			w := ansi.StringWidth(ln)
			leftLines = append(leftLines, ln)
			leftWidths = append(leftWidths, w)
			commonW = max(commonW, w)
		}
	}

	sideLines := strings.Split(side, "\n")
	sideWidths := make([]int, len(sideLines))
	sideW := 0
	for i, ln := range sideLines {
		sideWidths[i] = ansi.StringWidth(ln)
		sideW = max(sideW, sideWidths[i])
	}

	rows := max(len(leftLines), len(sideLines))
	lines := make([]string, rows)
	for i := range rows {
		left, leftW := "", 0
		if i < len(leftLines) {
			left, leftW = leftLines[i], leftWidths[i]
		}
		right, rightW := "", 0
		if i < len(sideLines) {
			right, rightW = sideLines[i], sideWidths[i]
		}
		lines[i] = left + strings.Repeat(" ", commonW-leftW) +
			strings.Repeat(" ", gap) + right + strings.Repeat(" ", sideW-rightW)
	}
	return strings.Join(lines, "\n")
}

// joinBoardLipgloss is the triple joinBoard replaces, kept so the fast path has
// something to be checked against and to fall back to.
func joinBoardLipgloss(stack []string, side string, gap int) string {
	left := lipgloss.JoinVertical(lipgloss.Left, stack...)
	side = lipgloss.NewStyle().MarginLeft(gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, side)
}

// boxTop builds the rounded top edge of OUTER width w with a pre-rendered
// segment (carrying its own colors, display width segW) set into it just past
// the left corner, padding the remainder with the border rule.
func boxTop(segment string, segW, w int) string {
	rb := lipgloss.RoundedBorder()
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	rest := w - 3 - segW
	if rest < 0 {
		rest = 0
	}
	return rule.Render(rb.TopLeft+rb.Top) + segment +
		rule.Render(strings.Repeat(rb.Top, rest)+rb.TopRight)
}

// titledBox wraps content in a rounded border of OUTER size w × h, with title
// set into the top edge. lipgloss has no native border title, so the top line
// is hand-built and prepended to a top-less bordered body.
func titledBox(content string, w, h int, title string) string {
	return titledBoxTinted(content, w, h, title, accentStyle)
}

// titledBoxTinted is titledBox with the title painted in a caller-chosen style,
// so the PR and Issue boards can tint their box titles in distinct accents.
func titledBoxTinted(content string, w, h int, title string, tint lipgloss.Style) string {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	label := " " + truncate(title, w-4) + " "
	if lipgloss.Width(label) > w-3 { // cap the label so the top line stays exactly w wide
		label = truncate(label, w-3)
	}
	return boxTop(tint.Render(label), lipgloss.Width(label), w) + "\n" + boxBody(content, w, h)
}

// tabbedBox is a titledBox whose top edge carries a tab bar instead of a single
// title: the active tab is an accent pill, the rest dim — the same accent chrome
// the board's boxes use, so the expanded view frames its content to match.
func tabbedBox(content string, w, h int, tabs []string, active int) string {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	seg := tabSegment(tabs, active, w-3) // boxTop reserves 3 cells (corner+rule ×2) around the segment
	return boxTop(seg, lipgloss.Width(seg), w) + "\n" + boxBody(content, w, h)
}

// tabSegment renders the tab labels as pill-padded names notched into the border
// rule: one rule tick flanks each side and joins adjacent tabs, so the labels
// sit on the top edge rather than floating above it. The rendered width is
// capped at maxW: when every tab doesn't fit, the visible range is windowed
// around active (never dropping the active tab) with an ellipsis marking each
// side that was trimmed — unlike renderTabBar's side-pane strip, which can
// afford to just drop trailing tabs because it never has to keep a specific
// tab on screen.
func tabSegment(tabs []string, active, maxW int) string {
	tick := sepStyle.Render(lipgloss.RoundedBorder().Top)
	tickW := lipgloss.Width(tick)
	n := len(tabs)
	cells := make([]string, n)
	widths := make([]int, n)
	for i, t := range tabs {
		st := tabInactiveStyle
		if i == active {
			st = tabActiveStyle
		}
		cells[i] = st.Render(t)
		widths[i] = lipgloss.Width(cells[i])
	}
	full := tickW
	for _, cw := range widths {
		full += cw + tickW
	}
	if full <= maxW {
		return tick + strings.Join(cells, tick) + tick
	}
	if maxW <= 0 {
		return ""
	}
	if active < 0 || active >= n {
		active = 0
	}

	ellipsis := dimStyle.Render("…")
	ellW := lipgloss.Width(ellipsis)
	fits := func(lo, hi int) bool {
		w := (hi - lo + 2) * tickW
		for i := lo; i <= hi; i++ {
			w += widths[i]
		}
		if lo > 0 {
			w += ellW + tickW
		}
		if hi < n-1 {
			w += ellW + tickW
		}
		return w <= maxW
	}
	lo, hi := active, active
	for fits(lo, hi) {
		grew := false
		if hi+1 < n && fits(lo, hi+1) {
			hi++
			grew = true
		}
		if lo-1 >= 0 && fits(lo-1, hi) {
			lo--
			grew = true
		}
		if !grew {
			break
		}
	}

	parts := make([]string, 0, hi-lo+3)
	if lo > 0 {
		parts = append(parts, ellipsis)
	}
	parts = append(parts, cells[lo:hi+1]...)
	if hi < n-1 {
		parts = append(parts, ellipsis)
	}
	seg := tick + strings.Join(parts, tick) + tick
	if lipgloss.Width(seg) <= maxW {
		return seg
	}
	// Degenerate maxW, below any realistic terminal width: shed the ticks, then
	// shrink the active tab's own label, before ever giving up on showing it.
	if seg = cells[active]; lipgloss.Width(seg) <= maxW {
		return seg
	}
	if padW := lipgloss.Width(tabActiveStyle.Render("")); maxW > padW {
		return tabActiveStyle.Render(truncate(tabs[active], maxW-padW))
	}
	return ""
}

// overlayTop composites panel horizontally centered over base, anchored to a
// fixed row near the top so overlays of differing height don't jump vertically
// as their content changes.
func overlayTop(base, panel string, w, h int) string {
	return overlayAt(base, panel, (w-lipgloss.Width(panel))/2, h/5, w, h)
}

// overlayAt composites panel over base at absolute (x, y), pulled back inside
// the w × h frame only as far as needed to stay on screen. Layer.Draw ignores
// its own x/y, so the positioning has to go through a Compositor, which draws
// each layer at its absolute bounds.
func overlayAt(base, panel string, x, y, w, h int) string {
	pw, ph := lipgloss.Width(panel), lipgloss.Height(panel)
	if x+pw > w {
		x = w - pw
	}
	if y+ph > h {
		y = h - ph
	}
	x, y = max(0, x), max(0, y)
	canvas := lipgloss.NewCanvas(w, h)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(panel).X(x).Y(y).Z(1),
	))
	return canvas.Render()
}
