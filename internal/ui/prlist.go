package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type Model struct {
	dir    string
	filter string
	cache  *cache.Cache
	table  table.Model
	prs    []gh.PR
	err    error
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "#", Width: 6},
			{Title: "Title", Width: 50},
			{Title: "Author", Width: 14},
			{Title: "CI", Width: 8},
		}),
		table.WithFocused(true),
	)
	return Model{dir: dir, filter: filter, cache: c, table: t}
}

func (m *Model) setPRs(prs []gh.PR) {
	m.prs = prs
	rows := make([]table.Row, 0, len(prs))
	for _, p := range prs {
		rows = append(rows, table.Row{
			fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login, p.CIState(),
		})
	}
	m.table.SetRows(rows)
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case prsFetchedMsg:
		m.setPRs(msg.prs)
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(cache.Key("pr", m.filter, 20, schemaVer), msg.raw)
		}
		return m, nil
	case fetchFailedMsg:
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if len(m.prs) == 0 && m.err == nil {
		return "Loading PRs…  (q to quit)"
	}
	if m.err != nil && len(m.prs) == 0 {
		return "Error: " + m.err.Error() + "  (q to quit)"
	}
	return m.table.View() + "\n(q to quit)"
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v2"
