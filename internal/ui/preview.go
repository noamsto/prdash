package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
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
		b.WriteString(fmt.Sprintf("▸ %d earlier comments\n\n", older))
	}
	for _, it := range latest {
		hdr := "@" + it.Author
		if it.Kind == preview.KindReview && it.State != "" {
			hdr += " · " + it.State
		}
		body, _ := preview.Render(it.Body, width)
		b.WriteString(hdr + "\n" + body + "\n")
	}
	return b.String()
}

func (m Model) previewWidth() int {
	if m.width <= 0 {
		return 40
	}
	w := m.width * 45 / 100
	if w < 20 {
		w = 20
	}
	return w
}

// previewPane renders the timeline for the cursor PR, or a loading/empty hint.
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return "Loading preview…"
	}
	return renderTimeline(preview.Timeline(d), m.previewN, m.previewWidth(), m.previewExpanded)
}

// tableWithPreview lays the list and preview pane side by side.
func (m Model) tableWithPreview() string {
	pane := lipgloss.NewStyle().PaddingLeft(2).Render(m.previewPane())
	return lipgloss.JoinHorizontal(lipgloss.Top, m.table.View(), pane)
}
