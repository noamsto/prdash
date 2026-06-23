package ui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type Model struct {
	dir          string
	filter       string
	cache        *cache.Cache
	runner       gh.Runner
	table        table.Model
	prs          []gh.PR
	err          error
	filtering    bool
	filterInput  textinput.Model
	repo         string
	actions      map[string]action.Action
	shown        []gh.PR
	pending      *action.Action
	showActions  bool
	actionFilter textinput.Model
	actionCursor int
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
	af := textinput.New()
	af.Prompt = "› "
	return Model{dir: dir, filter: filter, cache: c, table: t, filterInput: ti, actions: action.DefaultPRActions(), actionFilter: af}
}

func (m *Model) SetRunner(r gh.Runner) { m.runner = r }
func (m *Model) SetRepo(repo string)   { m.repo = repo }

func (m *Model) setPRs(prs []gh.PR) {
	m.prs = prs
	m.applyFilter()
}

func (m *Model) applyFilter() {
	m.shown = filterPRs(m.prs, m.filterInput.Value())
	rows := make([]table.Row, 0, len(m.shown))
	for _, p := range m.shown {
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
	e, ok := m.cache.Get(cache.Key("pr", m.filter, defaultLimit, schemaVer))
	if !ok {
		return
	}
	var prs []gh.PR
	if err := json.Unmarshal(e.Rows, &prs); err != nil {
		slog.Debug("cache unmarshal failed", "err", err)
		return
	}
	m.setPRs(prs)
}

func (m *Model) Hydrate() { m.hydrate() }

func (m Model) fetchCmd(r gh.Runner) tea.Cmd {
	dir, filter := m.dir, m.filter
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.PRListArgs(filter, defaultLimit)...)
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
			m.cache.Set(cache.Key("pr", m.filter, defaultLimit, schemaVer), msg.raw)
		}
		return m, nil
	case fetchFailedMsg:
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		if m.pending != nil {
			if msg.String() == "y" {
				return m, m.confirmAnswer(true)
			}
			return m, m.confirmAnswer(false)
		}
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
		if m.showActions {
			switch msg.String() {
			case "esc":
				m.showActions = false
				m.actionFilter.SetValue("")
				m.actionFilter.Blur()
				m.actionCursor = 0
				return m, nil
			case "enter":
				acts := filterActions(m.actions, m.actionFilter.Value())
				m.showActions = false
				m.actionFilter.Blur()
				m.actionFilter.SetValue("")
				i := m.actionCursor
				m.actionCursor = 0
				if i >= 0 && i < len(acts) {
					a := acts[i]
					if a.Confirm {
						m.pending = &a
						return m, nil
					}
					return m, m.runAction(a)
				}
				return m, nil
			case "up", "ctrl+k":
				if m.actionCursor > 0 {
					m.actionCursor--
				}
				return m, nil
			case "down", "ctrl+j":
				if m.actionCursor < len(filterActions(m.actions, m.actionFilter.Value()))-1 {
					m.actionCursor++
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.actionFilter, cmd = m.actionFilter.Update(msg)
			m.actionCursor = 0
			return m, cmd
		}
		switch msg.String() {
		case "a":
			m.showActions = true
			return m, m.actionFilter.Focus()
		case "/":
			m.filtering = true
			return m, m.filterInput.Focus()
		case "q", "ctrl+c":
			return m, tea.Quit
		default:
			if a, ok := m.actions[msg.String()]; ok {
				if a.Confirm {
					m.pending = &a
					return m, nil
				}
				return m, m.runAction(a)
			}
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.pending != nil {
		n := 0
		if cur := m.cursorPR(); cur != nil {
			n = cur.Number
		}
		return fmt.Sprintf("%s #%d? y/N", m.pending.Label, n) + "\n" + m.table.View()
	}
	if m.showActions {
		acts := filterActions(m.actions, m.actionFilter.Value())
		var b strings.Builder
		b.WriteString(m.actionFilter.View() + "\n")
		for i, a := range acts {
			cursor := "  "
			if i == m.actionCursor {
				cursor = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-6s %s\n", cursor, a.Key, a.Label))
		}
		return b.String()
	}
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

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20
