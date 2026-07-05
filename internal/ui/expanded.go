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

var expandedTabs = []string{"Conversation", "Reviews", "Checks", "Diff"}

// jumpTabIndex maps a triage card's JumpTab to a tab index (default Conversation).
func jumpTabIndex(jump string) int {
	switch jump {
	case "reviews":
		return 1
	case "checks":
		return 2
	case "diff":
		return 3
	default:
		return 0
	}
}

func tabStrip(active int) string {
	parts := make([]string, len(expandedTabs))
	for i, t := range expandedTabs {
		if i == active {
			parts[i] = headerStyle.Render(t)
		} else {
			parts[i] = dimStyle.Render(t)
		}
	}
	return "  " + strings.Join(parts, "   ")
}

func renderReviews(d gh.PRDetail, w int) string {
	if len(d.LatestReviews) == 0 {
		return dimStyle.Render("  No reviews yet.")
	}
	var b strings.Builder
	sep := sepStyle.Render(strings.Repeat("─", w))
	for i, r := range d.LatestReviews {
		if i > 0 {
			b.WriteString(sep + "\n\n")
		}
		b.WriteString(metaLine(r.Author.Login, r.State, r.SubmittedAt) + "\n")
		if r.Body != "" {
			body, err := preview.Render(r.Body, w)
			if err != nil {
				body = r.Body
			}
			b.WriteString(body)
		}
		b.WriteString("\n")
	}
	return b.String()
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
	m.expandedTab = 0
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
	d, cached := m.detail[v.Number]
	if !cached {
		return dimStyle.Render("  Loading…")
	}
	switch m.expandedTab {
	case 1:
		return renderReviews(d, w)
	case 2:
		if ps, ok := m.section.(*PRSection); ok {
			return renderChecks(ps.prAt(m.cursor), w, m.checkCursor)
		}
		return ""
	case 3:
		return renderDiffstat(d, w)
	default:
		items := preview.Timeline(d)
		return renderTimeline(items, len(items), w, true)
	}
}

// renderExpanded fills the viewport with the active tab's content, scroll reset.
func (m *Model) renderExpanded() {
	l := computeLayout(m.width, m.height)
	rows := l.ContentHeight - 1 // tab strip takes one row
	if _, ok := m.section.(*PRSection); ok {
		rows-- // metadata line under the header
	}
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(rows)
	m.vp.SetHorizontalStep(8) // < / > pan wide content (tables, diffs) instead of wrapping
	m.vp.SetContent(m.expandedBody(m.width))
	m.vp.SetYOffset(0)
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
		if m.expandedTab == 0 {
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
	case "1", "2", "3", "4":
		m.expandedTab = int(msg.String()[0] - '1')
		m.checkCursor = 0
		m.renderExpanded()
		return m, nil
	case "r": // Checks tab: rerun the hovered check
		if m.expandedTab == 2 {
			return m.rerunHoveredCheck()
		}
	case "R": // Checks tab: rerun all failed checks
		if m.expandedTab == 2 {
			return m.rerunAllFailedChecks()
		}
	case "j", "down":
		if m.expandedTab == 2 {
			m.moveCheckCursor(1)
			return m, nil
		}
		m.vp.ScrollDown(1)
		return m, nil
	case "k", "up":
		if m.expandedTab == 2 {
			m.moveCheckCursor(-1)
			return m, nil
		}
		m.vp.ScrollUp(1)
		return m, nil
	case ">", ".":
		m.vp.ScrollRight(8)
		return m, nil
	case "<", ",":
		m.vp.ScrollLeft(8)
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
	m.actionStatus = &actionStat{run: "rerunning " + c.Label(), ok: "rerun queued: " + c.Label(), fail: "rerun failed"}
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
	m.actionStatus = &actionStat{run: "rerunning failed checks", ok: "rerun-all queued", fail: "rerun failed"}
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

// expandedFooter is the bottom hint line: the Checks tab swaps in the rerun keys.
func (m Model) expandedFooter() string {
	if m.expandedTab == 2 {
		return "  j/k move · r rerun · R rerun all · h/l tabs · J/K PR · esc back"
	}
	return "  j/k scroll · <> pan · h/l tabs · J/K PR · ↵ worktree · esc back"
}

// expandedView is the full-screen detail: header, metadata line, tab strip,
// scrollable body, keys.
func (m Model) expandedView() string {
	n := 0
	if v, ok := m.cursorVars(); ok {
		n = v.Number
	}
	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, n))
	if ps, ok := m.section.(*PRSection); ok {
		if title := ps.prAt(m.cursor).Title; title != "" {
			if avail := m.width - lipgloss.Width(head) - 4; avail > 12 {
				head += dimStyle.Render("  " + truncate(title, avail))
			}
		}
	}
	head += m.statusBadge() // rerun feedback: the header badge isn't visible here otherwise
	foot := statusBarStyle.Render(m.expandedFooter())
	lines := []string{head}
	if ps, ok := m.section.(*PRSection); ok {
		lines = append(lines, m.expandedMeta(ps.prAt(m.cursor), m.width-2))
	}
	lines = append(lines, tabStrip(m.expandedTab), m.vp.View(), foot)
	return strings.Join(lines, "\n")
}
