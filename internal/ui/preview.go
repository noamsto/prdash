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

// detailCmds fetches detail for the cursor and its immediate neighbors so j/k
// navigation lands on an already-loaded preview. Skips cached / out-of-range rows.
func (m *Model) detailCmds() tea.Cmd {
	ps, ok := m.section.(*PRSection)
	if !ok || m.runner == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, i := range []int{m.cursor, m.cursor - 1, m.cursor + 1} {
		if i < 0 || i >= m.section.Len() {
			continue
		}
		num := ps.prAt(i).Number
		if _, cached := m.detail[num]; cached {
			continue
		}
		cmds = append(cmds, m.fetchDetailCmd(num))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
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
	// Expanded view pans horizontally (wrap 0); the narrow quick view wraps.
	wrap := width
	if expanded {
		wrap = 0
	}
	sep := sepStyle.Render(strings.Repeat("─", width))
	for i, it := range latest {
		if i > 0 {
			b.WriteString(sep + "\n\n")
		}
		body, err := preview.Render(it.Body, wrap)
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
		return m.width - 4 // border (2) + padding (2)
	}
	return l.SideWidth - 4 // border (2) + padding (2)
}

// previewPane renders the triage card followed by the timeline. Before the
// per-PR detail loads it pre-fills a card from list-only data (triage.Preliminary)
// so the cursor never lands on a bare "Loading…" — detail enriches it in place.
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	w := m.previewWidth()
	d, cached := m.detail[v.Number]

	var parts []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		card := triage.Preliminary(pr)
		if cached {
			card = triage.Compute(pr, d)
		}
		if c := renderCard(card, w); c != "" {
			parts = append(parts, strings.TrimRight(c, "\n"))
		}
		// ciLine surfaces failing/running CI only when the card headline is about
		// something else; a checks card already lists them, so skip the dup.
		if card.Kind != triage.KindChecksFailing && card.Kind != triage.KindChecksRunning {
			if ci := ciLine(pr); ci != "" {
				parts = append(parts, ci)
			}
		}
	}
	if !cached {
		if len(parts) == 0 {
			return "Loading preview…"
		}
		return strings.Join(parts, "\n") + "\n\n" + dimStyle.Render("  loading details…")
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
		for _, c := range pr.Checks() {
			if c.Result() == "fail" {
				names = append(names, c.Label())
			}
		}
		s := failStyle.Render("  ✗ " + triage.ChecksFailingHeadline(len(names)))
		for _, n := range names {
			s += "\n" + failStyle.Render("    ✗ ") + dimStyle.Render(n)
		}
		return s
	case "pending":
		return pendStyle.Render("  ● checks running")
	default: // pass / none — the row glyph carries it; keep the quick view calm
		return ""
	}
}

// requestedLogins returns the logins of requested reviewers. Team requests have
// no login and are skipped.
func requestedLogins(reqs []gh.ReviewRequest) []string {
	var logins []string
	for _, r := range reqs {
		if r.Login != "" {
			logins = append(logins, r.Login)
		}
	}
	return logins
}

// reviewersLine summarises requested reviewers for the quick window.
func reviewersLine(reqs []gh.ReviewRequest) string {
	logins := requestedLogins(reqs)
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
		return paneBorder(m.width, l.ContentHeight, 0).Render(m.previewPane())
	}
	// MaxWidth/MaxHeight (inside paneBorder) hard-clip the pane so a long timeline
	// or wide glamour line can't overflow and scroll the list out of view.
	side := paneBorder(l.SideWidth, l.ContentHeight, l.Gap).Render(m.previewPane())
	return lipgloss.JoinHorizontal(lipgloss.Top, m.vp.View(), side)
}

// paneBorder frames a pane: a rounded border + 1-col padding. width/height are
// the OUTER box size (excl. marginLeft); lipgloss Width/Height already account for
// the border and padding, so interior content is width-4 wide × height-2 tall.
func paneBorder(width, height, marginLeft int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).MaxWidth(width+marginLeft).
		Height(height).MaxHeight(height).
		Padding(0, 1).MarginLeft(marginLeft).
		Border(lipgloss.RoundedBorder()).BorderForeground(borderColor)
}
