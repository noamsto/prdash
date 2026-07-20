package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
	"github.com/noamsto/prdash/internal/triage"
)

const (
	tabDescription = iota
	tabConversation
	tabReviews
	tabChecks
	tabDiff
)

var expandedTabs = []string{"Description", "Conversation", "Reviews", "Checks", "Diff"}

// jumpTabIndex maps a triage card's JumpTab to a tab index (default Description).
func jumpTabIndex(jump string) int {
	switch jump {
	case "conversation":
		return tabConversation
	case "reviews":
		return tabReviews
	case "checks":
		return tabChecks
	case "diff":
		return tabDiff
	default:
		return tabDescription
	}
}

// renderDescription renders the PR body as the Description tab: the full markdown
// in the reading column. Empty bodies get a dim placeholder.
func renderDescription(pr gh.PR, w int) string {
	if strings.TrimSpace(pr.Body) == "" {
		return dimStyle.Render("  No description provided.")
	}
	return renderDiscussionColumn(w, func(cw int) string {
		body, err := preview.Render(pr.Body, cw)
		if err != nil {
			body = pr.Body
		}
		return strings.TrimRight(body, "\n")
	})
}

func renderReviews(d gh.PRDetail, w int) string {
	if len(d.LatestReviews) == 0 {
		return dimStyle.Render("No reviews yet.")
	}
	blocks := make([]string, 0, len(d.LatestReviews))
	for _, r := range d.LatestReviews {
		blocks = append(blocks, renderDiscussionItem(
			metaLine(r.Author.Login, r.State, r.SubmittedAt), r.Body, w,
		))
	}
	return strings.Join(blocks, "\n\n")
}

const discussionMaxWidth = 104

// renderDiscussionColumn caps prose to a comfortable reading width and centers
// it in wide terminals. Narrow terminals retain a small gutter where possible.
func renderDiscussionColumn(viewportWidth int, render func(int) string) string {
	if viewportWidth < 1 {
		viewportWidth = 1
	}
	contentWidth := viewportWidth
	if viewportWidth >= 48 {
		contentWidth -= 4
	}
	contentWidth = min(contentWidth, discussionMaxWidth)
	gutter := (viewportWidth - contentWidth) / 2
	return indentLines(render(contentWidth), gutter)
}

func renderChecks(pr gh.PR, w, cursor int) string {
	checks := pr.Checks()
	if len(checks) == 0 {
		return dimStyle.Render("  No checks.")
	}
	var b strings.Builder
	for i, c := range checks {
		gutter := "  "
		st := titleStyle
		if i == cursor {
			gutter = focusBarStyle.Render("▎") + " "
			st = st.Bold(true)
		}
		b.WriteString(gutter + ciGlyph(c.Result()) + " " + st.Render(truncate(c.Label(), w-4)) + "\n")
	}
	return b.String()
}

func renderDiffstat(d gh.PRDetail, w int) string {
	s := d.Diffstat()
	if s.Files == 0 {
		return dimStyle.Render("  No file changes.")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s files  %s  %s\n\n", accentStyle.Render(fmt.Sprintf("%d", s.Files)),
		passStyle.Render(fmt.Sprintf("+%d", s.Additions)), failStyle.Render(fmt.Sprintf("-%d", s.Deletions))))
	paths := make([]string, len(d.Files))
	pathW := 0
	for i, f := range d.Files {
		paths[i] = truncate(f.Path, w-16)
		if l := lipgloss.Width(paths[i]); l > pathW {
			pathW = l
		}
	}
	for i, f := range d.Files {
		pad := strings.Repeat(" ", pathW-lipgloss.Width(paths[i]))
		b.WriteString(fmt.Sprintf("  %s%s  %s %s\n", paths[i], pad,
			passStyle.Render(fmt.Sprintf("+%d", f.Additions)), failStyle.Render(fmt.Sprintf("-%d", f.Deletions))))
	}
	return b.String()
}

// enterExpanded opens the focused PR's detail, deep-linking to the tab the
// triage card points at (when its detail is already cached).
func (m *Model) enterExpanded() {
	if m.section.Len() == 0 {
		return
	}
	m.expanded = true
	m.expandedTab = tabDescription
	m.checkCursor = 0
	if v, ok := m.cursorVars(); ok {
		if d, cached := m.detail[v.Number]; cached {
			if ps, ok := m.section.(*PRSection); ok {
				m.expandedTab = jumpTabIndex(triage.Compute(ps.prAt(m.cursor), d).JumpTab)
			}
		}
	}
	m.renderExpanded()
}

// expandedBody renders the active tab's content for the focused PR.
func (m Model) expandedBody(w int) string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	if m.expandedTab == tabDescription {
		if ps, ok := m.section.(*PRSection); ok {
			return renderDescription(ps.prAt(m.cursor), w)
		}
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return dimStyle.Render("  Loading…")
	}
	switch m.expandedTab {
	case tabReviews:
		return renderDiscussionColumn(w, func(contentWidth int) string {
			return renderReviews(d, contentWidth)
		})
	case tabChecks:
		if ps, ok := m.section.(*PRSection); ok {
			return renderChecks(ps.prAt(m.cursor), w, m.checkCursor)
		}
		return ""
	case tabDiff:
		return renderDiffstat(d, w)
	default:
		items := preview.Timeline(d)
		return renderDiscussionColumn(w, func(contentWidth int) string {
			return renderTimeline(items, len(items), contentWidth, true)
		})
	}
}

// expandedBoxWidth is the reading-column cap used by the full-screen log viewer.
// The PR/Issue expanded view derives its width from computeExpandedLayout.
func (m Model) expandedBoxWidth() int {
	return min(m.width, expandedContentCap)
}

// setExpandedContent (re)fills the viewport with the active tab's content at the
// current geometry. It leaves the scroll offset alone — callers pick the anchor.
func (m *Model) setExpandedContent() {
	_, isPR := m.section.(*PRSection)
	l := computeExpandedLayout(m.width, m.height, isPR)
	w := l.ContentW - 2
	if w < 1 {
		w = 1
	}
	m.vp.SetWidth(w)
	m.vp.SetHeight(l.VPHeight)
	m.vp.SetContent(m.expandedBody(w))
}

// renderExpanded rebuilds the active tab and anchors it: Conversation and Reviews
// open at the bottom (most recent), the rest at the top. Used when the tab or the
// focused PR changes — a deliberate move that warrants re-anchoring.
func (m *Model) renderExpanded() {
	m.setExpandedContent()
	if m.expandedTab == tabConversation || m.expandedTab == tabReviews {
		m.vp.GotoBottom()
	} else {
		m.vp.SetYOffset(0)
	}
}

// reflowExpanded rebuilds the active tab in place, preserving the reader's scroll
// position (clamped to the new bounds). Used for background refreshes and resizes,
// which must not yank the view away from what the reader was on.
func (m *Model) reflowExpanded() {
	off := m.vp.YOffset()
	m.setExpandedContent()
	if maxOff := m.vp.TotalLineCount() - m.vp.Height(); off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	m.vp.SetYOffset(off)
}

// updateExpanded handles keys while in expanded mode.
func (m Model) updateExpanded(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.expanded = false
		m.renderList()
		return m, nil
	case "tab", "right", "l":
		m.expandedTab = (m.expandedTab + 1) % len(expandedTabs)
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
	case "left", "h":
		if m.expandedTab == tabDescription {
			m.expanded = false
			m.renderList()
			return m, nil
		}
		m.expandedTab--
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
	case "shift+tab":
		m.expandedTab = (m.expandedTab + len(expandedTabs) - 1) % len(expandedTabs)
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
	case "1", "2", "3", "4", "5":
		m.expandedTab = int(msg.String()[0] - '1')
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
	case "r": // Checks tab: rerun the hovered check
		if m.expandedTab == tabChecks {
			return m.rerunHoveredCheck()
		}
	case "R": // Checks tab: rerun all failed checks
		if m.expandedTab == tabChecks {
			return m.rerunAllFailedChecks()
		}
	case "o": // Checks tab: open the hovered check in the browser
		if m.expandedTab == tabChecks {
			return m.openHoveredCheck()
		}
	case "Y": // Checks tab: copy the hovered check's URL
		if m.expandedTab == tabChecks {
			return m.copyHoveredCheckURL()
		}
	case "j", "down":
		if m.expandedTab == tabChecks {
			m.moveCheckCursor(1)
			return m, nil
		}
		m.vp.ScrollDown(1)
		return m, nil
	case "k", "up":
		if m.expandedTab == tabChecks {
			m.moveCheckCursor(-1)
			return m, nil
		}
		m.vp.ScrollUp(1)
		return m, nil
	case "J":
		if m.cursor < m.section.Len()-1 {
			m.cursor++
		}
		m.checkCursor = 0
		m.renderExpanded()
		return m, m.detailCmdForCursor()
	case "K":
		if m.cursor > 0 {
			m.cursor--
		}
		m.checkCursor = 0
		m.renderExpanded()
		return m, m.detailCmdForCursor()
	case "enter":
		if m.expandedTab == tabChecks { // Checks: drill into the hovered check's logs
			return m.enterLogView()
		}
		if a, ok := m.actions["enter"]; ok {
			return m, m.runAction(a)
		}
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // up/down/pgup/pgdn scroll the content
	return m, cmd
}

func (m Model) checksLen() int {
	if ps, ok := m.section.(*PRSection); ok {
		return len(ps.prAt(m.cursor).Checks())
	}
	return 0
}

// moveCheckCursor moves the Checks-tab cursor within bounds and re-renders.
func (m *Model) moveCheckCursor(d int) {
	n := m.checksLen()
	if n == 0 {
		m.checkCursor = 0
		return
	}
	m.checkCursor = max(0, min(m.checkCursor+d, n-1))
	m.renderExpanded()
}

// rerunHoveredCheck reruns the single check under the Checks-tab cursor. External
// (non-Actions) checks have no job to rerun and settle to a hint instead.
func (m Model) rerunHoveredCheck() (tea.Model, tea.Cmd) {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return m, nil
	}
	checks := ps.prAt(m.cursor).Checks()
	if m.checkCursor < 0 || m.checkCursor >= len(checks) {
		return m, nil
	}
	c := checks[m.checkCursor]
	job := c.JobID()
	if job == "" {
		m.actionStatus = &actionStat{fail: "no rerun for external check", settled: true,
			err: fmt.Errorf("external check %q has no rerunnable job", c.Label())}
		return m, clearStatusCmd()
	}
	r, dir := m.runner, m.dir
	m.actionStatus = &actionStat{run: "rerunning " + c.Label(), ok: "rerun queued: " + c.Label(), fail: "rerun failed",
		refresh: true, nums: []int{ps.prAt(m.cursor).Number}}
	return m, tea.Batch(func() tea.Msg {
		return actionDoneMsg{err: action.RerunCheck(r, dir, job)}
	}, m.startSpinner())
}

// rerunAllFailedChecks reruns every failed check on the PR's latest run.
func (m Model) rerunAllFailedChecks() (tea.Model, tea.Cmd) {
	v, ok := m.cursorVars()
	if !ok {
		return m, nil
	}
	r, dir, branch := m.runner, m.dir, v.HeadRefName
	m.actionStatus = &actionStat{run: "rerunning failed checks", ok: "rerun-all queued", fail: "rerun failed",
		refresh: true, nums: []int{v.Number}}
	return m, tea.Batch(func() tea.Msg {
		return actionDoneMsg{err: action.RerunFailed(r, dir, branch)}
	}, m.startSpinner())
}

// ciSummary is the compact CI state for the expanded metadata line.
func ciSummary(pr gh.PR) string {
	switch pr.CIState() {
	case "pass":
		return passStyle.Render("✓ passing")
	case "fail":
		n := 0
		for _, c := range pr.Checks() {
			if c.Result() == "fail" {
				n++
			}
		}
		return failStyle.Render(fmt.Sprintf("✗ %d failing", n))
	case "pending":
		return pendStyle.Render("● running")
	default:
		return dimStyle.Render("— no checks")
	}
}

// expandedMeta is the responsive at-a-glance line under the header: author,
// branch, label chips, and CI, packed into the width — the fields you check most
// without switching tabs. Narrow terminals drop the least-essential parts first.
func (m Model) expandedMeta(pr gh.PR, w int) string {
	var parts []string
	if pr.Author.Login != "" {
		parts = append(parts, authorStyle(pr.Author.Login).Render("@"+pr.Author.Login))
	}
	if pr.HeadRefName != "" {
		parts = append(parts, dimStyle.Render(truncate(pr.HeadRefName+"→"+pr.BaseRefName, w/3)))
	}
	if chips := renderChips(pr.Labels, w/3); chips != "" {
		parts = append(parts, chips)
	}
	parts = append(parts, ciSummary(pr))
	return "  " + strings.Join(parts, dimStyle.Render(" · "))
}

// renderExpandedRail builds the PR metadata side rail for the two-col expanded
// view: #num + title, author, branch→base, label chips (full renderChips at the
// rail's inner width, not w/3), requested reviewers, and a CI + diffstat
// one-liner. Width-clamped to RailW and height-clamped to RailH so a long label
// or reviewer set can never bleed past the column or push the frame past h.
func (m Model) renderExpandedRail(pr gh.PR, d gh.PRDetail, l ExpandedLayout) string {
	inner := l.RailW - railInset // leave a 1-cell gutter on each side of the rail
	if inner < 1 {
		inner = 1
	}
	var lines []string
	lines = append(lines, titleStyle.Bold(true).Render(truncate(fmt.Sprintf("#%d %s", pr.Number, pr.Title), inner)))
	if pr.Author.Login != "" {
		lines = append(lines, authorStyle(pr.Author.Login).Render(truncate("@"+pr.Author.Login, inner)))
	}
	if pr.HeadRefName != "" {
		lines = append(lines, dimStyle.Render(truncate(pr.HeadRefName+"→"+pr.BaseRefName, inner)))
	}
	if chips := renderChips(pr.Labels, inner); chips != "" {
		lines = append(lines, chips)
	}
	for _, r := range d.ReviewRequests {
		lines = append(lines, dimStyle.Render(truncate("• "+r.Login, inner)))
	}
	lines = append(lines, ciSummary(pr)) // already styled + bounded; rail MaxWidth clips any future growth
	if s := d.Diffstat(); s.Files > 0 {
		lines = append(lines, dimStyle.Render(truncate(fmt.Sprintf("%d files +%d -%d", s.Files, s.Additions, s.Deletions), inner)))
	}
	if len(lines) > l.RailH {
		lines = lines[:l.RailH]
	}
	// Correctness rests on each line being truncated to inner (< RailW) and the
	// lines[:RailH] cap above; Max{Width,Height} is defense-in-depth that makes
	// "the rail box is exactly RailW×RailH" hold even if a future edit adds an
	// un-truncated line (e.g. the raw ciSummary growing past inner).
	return lipgloss.NewStyle().Width(l.RailW).Height(l.RailH).
		MaxWidth(l.RailW).MaxHeight(l.RailH).Render(strings.Join(lines, "\n"))
}

// expandedFooter is the bottom hint line: the Checks tab swaps in the rerun keys.
func (m Model) expandedFooter() string {
	if m.expandedTab == tabChecks {
		return "  ↵ logs · o open · Y url · r rerun · R all · j/k move · esc back"
	}
	return "  j/k scroll · h/l tabs · J/K PR · ↵ worktree · esc back"
}

// expandedView is the full-screen detail: header, metadata line, then the
// active tab's content framed in a tabbed box — the same rounded chrome as the
// board's titled boxes — with the keys hint beneath.
func (m Model) expandedView() string {
	n := 0
	if v, ok := m.cursorVars(); ok {
		n = v.Number
	}
	ps, isPR := m.section.(*PRSection)
	l := computeExpandedLayout(m.width, m.height, isPR)

	blockW := l.ContentW
	if l.TwoCol {
		blockW = l.RailW + expandedColGap + l.ContentW
	}

	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, n))
	if isPR {
		if title := ps.prAt(m.cursor).Title; title != "" {
			if avail := blockW - lipgloss.Width(head) - 4; avail > 12 { // truncate vs FULL block width, not ContentW
				head += dimStyle.Render("  " + truncate(title, avail))
			}
		}
	}
	head += m.statusBadge() // rerun feedback: the header badge isn't visible here otherwise

	contentBox := tabbedBox(m.vp.View(), l.ContentW, l.VPHeight+2, expandedTabs, m.expandedTab)

	var mid string
	if l.TwoCol {
		rail := m.renderExpandedRail(ps.prAt(m.cursor), m.detail[n], l)
		gap := lipgloss.NewStyle().Width(expandedColGap).Render("")
		mid = lipgloss.JoinHorizontal(lipgloss.Top, rail, gap, contentBox)
	} else {
		parts := []string{}
		if isPR {
			parts = append(parts, m.expandedMeta(ps.prAt(m.cursor), l.ContentW-2)) // narrow PR keeps its one-line meta
		}
		parts = append(parts, contentBox)
		mid = strings.Join(parts, "\n")
	}

	parts := []string{head, mid}
	if l.ShowFooter {
		parts = append(parts, statusBarStyle.Render(m.expandedFooter()))
	}
	out := strings.Join(parts, "\n")
	if blockW < m.width { // center the block in a wide terminal
		out = indentLines(out, (m.width-blockW)/2)
	}
	return out
}
