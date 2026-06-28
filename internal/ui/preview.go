package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
	"github.com/noamsto/prdash/internal/triage"
)

type prDetailMsg struct {
	number int
	detail gh.PRDetail
}

// fetchDetailCmd lazily loads the selected PR's comments/reviews.
func (m Model) fetchDetailCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		d, err := gh.FetchPRDetail(r, dir, number)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return prDetailMsg{number: number, detail: d}
	}
}

// detailCmdForCursor fetches the cursor PR's detail if it isn't cached yet.
func (m *Model) detailCmdForCursor() tea.Cmd {
	if m.runner == nil || m.section.Kind() != "pr" {
		return nil
	}
	v, ok := m.cursorVars()
	if !ok {
		return nil
	}
	if _, cached := m.detail[v.Number]; cached {
		return nil
	}
	return m.fetchDetailCmd(v.Number)
}

// renderTimeline renders the latest n items expanded, older collapsed.
func renderTimeline(items []preview.Item, n, width int, expanded bool) string {
	older, latest := preview.Fold(items, n)
	if expanded {
		older, latest = 0, items
	}
	var b strings.Builder
	if older > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("▸ %d earlier comments", older)) + "\n\n")
	}
	sep := sepStyle.Render(strings.Repeat("─", width))
	for i, it := range latest {
		if i > 0 {
			b.WriteString(sep + "\n\n")
		}
		body, err := preview.Render(it.Body, width)
		if err != nil {
			body = it.Body // render failed; show the raw markdown rather than nothing
		}
		b.WriteString(metaLine(it.Author, it.State, it.At) + "\n" + body + "\n")
	}
	return b.String()
}

func (m Model) previewWidth() int {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return 40
	}
	if m.previewMax {
		return m.width - 2 // full width minus the pane's left padding
	}
	return l.SideWidth
}

// previewPane renders the triage card (if available) followed by the timeline,
// or a loading/empty hint.
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return "Loading preview…"
	}
	w := m.previewWidth()
	var parts []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		if card := renderCard(triage.Compute(pr, d), w); card != "" {
			parts = append(parts, strings.TrimRight(card, "\n"))
		}
		if ci := ciLine(pr); ci != "" {
			parts = append(parts, ci)
		}
	}
	parts = append(parts, reviewersLine(d.ReviewRequests))
	timeline := renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)
	return strings.Join(parts, "\n") + "\n\n" + timeline
}

// ciLine surfaces the check rollup in the quick view independent of the triage
// card, which keys off mergeStateStatus and can mask failing CI behind a
// review/conflict headline.
func ciLine(pr gh.PR) string {
	switch pr.CIState() {
	case "fail":
		var names []string
		for _, c := range pr.StatusCheckRollup {
			if c.Result() == "fail" {
				names = append(names, c.Label())
			}
		}
		s := failStyle.Render("  ✗ checks failing")
		if len(names) > 0 {
			s += dimStyle.Render(": " + strings.Join(names, ", "))
		}
		return s
	case "pending":
		return pendStyle.Render("  ● checks running")
	default: // pass / none — the row glyph carries it; keep the quick view calm
		return ""
	}
}

// reviewersLine summarises requested reviewers for the quick window. Team
// requests have no login and are skipped.
func reviewersLine(reqs []gh.ReviewRequest) string {
	var logins []string
	for _, r := range reqs {
		if r.Login != "" {
			logins = append(logins, r.Login)
		}
	}
	if len(logins) == 0 {
		return pendStyle.Render("  ⚠ no reviewers")
	}
	return dimStyle.Render("  reviewers: " + strings.Join(logins, ", "))
}

// renderMain lays the list and (when wide) the contained side preview together.
func (m Model) renderMain() string {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return m.vp.View()
	}
	// z maximizes the preview to full width, hiding the list for deep reading.
	if m.previewMax {
		return lipgloss.NewStyle().Width(m.width).Height(l.ContentHeight).
			MaxWidth(m.width).MaxHeight(l.ContentHeight).
			PaddingLeft(2).Render(m.previewPane())
	}
	// MaxWidth/MaxHeight hard-clip the pane: Width/Height only pad up, so a long
	// timeline or wide glamour line would otherwise overflow and scroll the list
	// out of view. The card + reviewers line lead, so only the timeline tail clips.
	side := lipgloss.NewStyle().Width(l.SideWidth).Height(l.ContentHeight).
		MaxWidth(l.SideWidth).MaxHeight(l.ContentHeight).
		PaddingLeft(2).Render(m.previewPane())
	return lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), side)
}
