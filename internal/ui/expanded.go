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
		label := truncate(c.Label(), w-4)
		gutter := "  "
		st := titleStyle
		if i == cursor {
			gutter = focusBarStyle.Render("▎") + " "
			st = st.Bold(true)
		}
		b.WriteString(gutter + ciGlyph(c.Result()) + " " + st.Render(label) + "\n")
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
	m.notice = ""
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
// The viewport width tracks the content pane (narrower in two-col mode) so
// markdown and timelines wrap to the pane, not the whole screen.
func (m *Model) renderExpanded() {
	el := ExpandedLayout(m.width, m.height)
	m.vp.SetWidth(el.ContentW)
	m.vp.SetHeight(el.VPHeight)
	m.vp.SetHorizontalStep(8) // < / > pan wide content (tables, diffs) instead of wrapping
	m.vp.SetContent(m.expandedBody(el.ContentW))
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
		m.checkCursor, m.notice = 0, ""
		m.renderExpanded()
		return m, nil
	case "shift+tab", "left", "h":
		// Off the left edge of the tab strip returns to the list — never wrap.
		if m.expandedTab == 0 {
			m.expanded = false
			m.renderList()
			return m, nil
		}
		m.expandedTab--
		m.checkCursor, m.notice = 0, ""
		m.renderExpanded()
		return m, nil
	case "1", "2", "3", "4":
		m.expandedTab = int(msg.String()[0] - '1')
		m.checkCursor, m.notice = 0, ""
		m.renderExpanded()
		return m, nil
	case "j", "down":
		if m.expandedTab == 2 { // Checks tab: j/k move the check cursor
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
	case "r": // rerun the hovered check (Checks tab only)
		if m.expandedTab != 2 {
			return m, nil
		}
		return m.rerunHovered()
	case "R": // rerun all failed checks (Checks tab only)
		if m.expandedTab != 2 {
			return m, nil
		}
		return m.rerunAllFailed()
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
		m.checkCursor, m.notice = 0, ""
		m.renderExpanded()
		return m, m.detailCmds()
	case "K":
		if m.cursor > 0 {
			m.cursor--
		}
		m.checkCursor, m.notice = 0, ""
		m.renderExpanded()
		return m, m.detailCmds()
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
	m.notice = ""
	m.renderExpanded()
}

// rerunHovered reruns the single check under the Checks-tab cursor. External
// (non-Actions) checks have no job to rerun and only set a hint.
func (m Model) rerunHovered() (tea.Model, tea.Cmd) {
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
		m.notice = "⚠ no rerun for external check: " + c.Label()
		return m, nil
	}
	m.notice = "↻ rerun queued: " + c.Label()
	r, dir := m.runner, m.dir
	return m, func() tea.Msg {
		if err := action.RerunCheck(r, dir, job); err != nil {
			return fetchFailedMsg{err}
		}
		return nil
	}
}

// rerunAllFailed reruns every failed check on the PR's latest run.
func (m Model) rerunAllFailed() (tea.Model, tea.Cmd) {
	v, ok := m.cursorVars()
	if !ok {
		return m, nil
	}
	m.notice = "↻ rerun-all-failed queued"
	r, dir, branch := m.runner, m.dir, v.HeadRefName
	return m, func() tea.Msg {
		if err := action.RerunFailed(r, dir, branch); err != nil {
			return fetchFailedMsg{err}
		}
		return nil
	}
}

// expandedView is the full-screen detail. Wide terminals get a metadata rail
// beside the tab/content pane; narrow ones get a compact meta header above it.
func (m Model) expandedView() string {
	el := ExpandedLayout(m.width, m.height)
	var pr gh.PR
	var d gh.PRDetail
	if ps, ok := m.section.(*PRSection); ok {
		pr = ps.prAt(m.cursor)
		d = m.detail[pr.Number]
	}
	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, pr.Number))
	if pr.Title != "" {
		if avail := m.width - lipgloss.Width(head) - 4; avail > 12 {
			head += dimStyle.Render("  " + truncate(pr.Title, avail))
		}
	}
	foot := statusBarStyle.Render(m.expandedFooter())
	content := tabStrip(m.expandedTab) + "\n" + m.vp.View()

	if !el.TwoCol {
		body := content
		if meta := narrowMeta(pr, d, m.width); meta != "" {
			body = meta + "\n" + content
		}
		return head + "\n" + body + "\n" + foot
	}
	rail := lipgloss.NewStyle().Width(el.RailW).Height(el.RailH).
		MaxWidth(el.RailW).MaxHeight(el.RailH).Render(metaRail(pr, d, el.RailW))
	body := lipgloss.JoinHorizontal(lipgloss.Top, rail, "  ", content)
	return head + "\n" + body + "\n" + foot
}

// ciSummary renders a one-line CI state for the rail / narrow header.
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

// metaRail renders the wide-mode left column: identity + the at-a-glance fields
// that otherwise force a tab switch (branch, labels, reviewers, CI, diffstat).
func metaRail(pr gh.PR, d gh.PRDetail, w int) string {
	var b strings.Builder
	b.WriteString(accentStyle.Render(fmt.Sprintf("#%d", pr.Number)))
	if g := reviewGlyph(pr.ReviewDecision); g != "" {
		b.WriteString("  " + g)
	}
	b.WriteString("\n")
	if pr.Title != "" {
		b.WriteString(titleStyle.Bold(true).Render(truncate(pr.Title, w)) + "\n")
	}
	b.WriteString("\n")

	field := func(label, val string) {
		if val == "" {
			return
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("%-9s ", label)) + val + "\n")
	}
	if pr.Author.Login != "" {
		field("author", authorStyle(pr.Author.Login).Render(truncate(pr.Author.Login, w-10)))
	}
	if pr.HeadRefName != "" {
		field("branch", dimStyle.Render(truncate(pr.HeadRefName+" → "+pr.BaseRefName, w-10)))
	}
	field("labels", renderChips(pr.Labels, w-10))
	var revs []string
	for _, r := range d.ReviewRequests {
		if r.Login != "" {
			revs = append(revs, r.Login)
		}
	}
	field("reviewers", dimStyle.Render(truncate(strings.Join(revs, ", "), w-10)))
	field("ci", ciSummary(pr))
	if s := d.Diffstat(); s.Files > 0 {
		field("changes", fmt.Sprintf("%s  %s %s",
			accentStyle.Render(fmt.Sprintf("%d files", s.Files)),
			passStyle.Render(fmt.Sprintf("+%d", s.Additions)),
			failStyle.Render(fmt.Sprintf("-%d", s.Deletions))))
	}
	return b.String()
}

// narrowMeta is the single compact context line shown above the tabs when the
// terminal is too narrow for a rail.
func narrowMeta(pr gh.PR, d gh.PRDetail, w int) string {
	var parts []string
	if pr.Author.Login != "" {
		parts = append(parts, authorStyle(pr.Author.Login).Render(pr.Author.Login))
	}
	if pr.HeadRefName != "" {
		parts = append(parts, dimStyle.Render(truncate(pr.HeadRefName+"→"+pr.BaseRefName, w/2)))
	}
	parts = append(parts, ciSummary(pr))
	var revs int
	for _, r := range d.ReviewRequests {
		if r.Login != "" {
			revs++
		}
	}
	if revs > 0 {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("%d reviewers", revs)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, dimStyle.Render(" · "))
}

// expandedFooter is the bottom hint line: a transient notice wins, else the key
// legend, which swaps to rerun keys on the Checks tab.
func (m Model) expandedFooter() string {
	if m.notice != "" {
		return "  " + m.notice
	}
	if m.expandedTab == 2 {
		return "  j/k move · r rerun · R rerun all · h/l tabs · J/K PR · esc back"
	}
	return "  j/k scroll · <> pan · h/l tabs · J/K PR · ↵ worktree · esc back"
}
