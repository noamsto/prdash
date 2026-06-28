package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func renderChecks(pr gh.PR, w int) string {
	if len(pr.StatusCheckRollup) == 0 {
		return dimStyle.Render("  No checks.")
	}
	var b strings.Builder
	for _, c := range pr.StatusCheckRollup {
		b.WriteString("  " + ciGlyph(c.Result()) + " " + truncate(c.Label(), w-4) + "\n")
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
			return renderChecks(ps.prAt(m.cursor), w)
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
	m.vp.Width = m.width
	m.vp.Height = l.ContentHeight - 1 // tab strip takes one row
	m.vp.SetHorizontalStep(8)         // < / > pan wide content (tables, diffs) instead of wrapping
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
		m.renderExpanded()
		return m, nil
	case "shift+tab", "left", "h":
		m.expandedTab = (m.expandedTab + len(expandedTabs) - 1) % len(expandedTabs)
		m.renderExpanded()
		return m, nil
	case "1", "2", "3", "4":
		m.expandedTab = int(msg.String()[0] - '1')
		m.renderExpanded()
		return m, nil
	case "j", "down":
		m.vp.LineDown(1)
		return m, nil
	case "k", "up":
		m.vp.LineUp(1)
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
		m.renderExpanded()
		return m, m.detailCmdForCursor()
	case "K":
		if m.cursor > 0 {
			m.cursor--
		}
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

// expandedView is the full-screen detail: header, tab strip, scrollable body, keys.
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
	foot := statusBarStyle.Render("  j/k scroll · <> pan · h/l tabs · J/K PR · ↵ worktree · esc back")
	return head + "\n" + tabStrip(m.expandedTab) + "\n" + m.vp.View() + "\n" + foot
}
