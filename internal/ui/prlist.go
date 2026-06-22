package ui

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type Model struct {
	dir         string
	filter      string
	cache       *cache.Cache
	runner      gh.Runner
	table       table.Model
	prs         []gh.PR
	err         error
	filtering   bool
	filterInput textinput.Model
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
	ti := textinput.New()
	ti.Prompt = "/"
	return Model{dir: dir, filter: filter, cache: c, table: t, filterInput: ti}
}

func (m *Model) SetRunner(r gh.Runner) { m.runner = r }

func (m *Model) setPRs(prs []gh.PR) {
	m.prs = prs
	m.applyFilter()
}

func (m *Model) applyFilter() {
	shown := filterPRs(m.prs, m.filterInput.Value())
	rows := make([]table.Row, 0, len(shown))
	for _, p := range shown {
		rows = append(rows, table.Row{
			fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login, p.CIState(),
		})
	}
	m.table.SetRows(rows)
}

func (m *Model) hydrate() {
	if m.cache == nil {
		return
	}
	e, ok := m.cache.Get(cache.Key("pr", m.filter, 20, schemaVer))
	if !ok {
		return
	}
	var prs []gh.PR
	if err := json.Unmarshal(e.Rows, &prs); err != nil {
		return
	}
	m.setPRs(prs)
}

func (m *Model) Hydrate() { m.hydrate() }

func (m Model) fetchCmd(r gh.Runner) tea.Cmd {
	dir, filter := m.dir, m.filter
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.PRListArgs(filter, 20)...)
		if err != nil {
			return fetchFailedMsg{err}
		}
		prs, err := gh.ParsePRs(raw)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return prsFetchedMsg{prs: prs, raw: raw}
	}
}

func (m Model) Init() tea.Cmd { return m.fetchCmd(m.runner) }

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
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.applyFilter()
				return m, nil
			case "enter":
				m.filtering = false
				m.filterInput.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.applyFilter()
			return m, cmd
		}
		switch msg.String() {
		case "/":
			m.filtering = true
			return m, m.filterInput.Focus()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.filtering {
		return m.filterInput.View() + "\n" + m.table.View()
	}
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
