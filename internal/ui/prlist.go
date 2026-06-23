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
	dir             string
	filter          string
	cache           *cache.Cache
	runner          gh.Runner
	table           table.Model
	section         Section
	err             error
	filtering       bool
	filterInput     textinput.Model
	repo            string
	actions         map[string]action.Action
	pending         *action.Action
	showActions     bool
	actionFilter    textinput.Model
	actionCursor    int
	sel             selection
	detail          map[int]gh.PRDetail
	previewExpanded bool
	previewN        int
	width           int
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	sec := NewPRSection(filter)
	t := table.New(
		table.WithColumns(sec.Columns()),
		table.WithFocused(true),
	)
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	return Model{dir: dir, filter: filter, cache: c, table: t, section: sec,
		filterInput: ti, actions: action.DefaultPRActions(), actionFilter: af,
		detail: map[int]gh.PRDetail{}, previewN: 3}
}

func (m *Model) SetRunner(r gh.Runner) { m.runner = r }
func (m *Model) SetRepo(repo string)   { m.repo = repo }

func (m *Model) setPRs(prs []gh.PR) {
	if s, ok := m.section.(*PRSection); ok {
		s.SetPRs(prs)
	}
	m.applyFilter()
}

func (m *Model) applyFilter() {
	m.section.SetShown(matchIdx(m.section.Haystacks(), m.filterInput.Value()))
	rows := m.section.Rows()
	for i := range rows {
		if m.sel.has(i) {
			rows[i][0] = "● " + rows[i][0]
		}
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
		m.sel.clear() // selection indexes the shown set; new data invalidates it
		m.setPRs(msg.prs)
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(cache.Key("pr", m.filter, defaultLimit, schemaVer), msg.raw)
		}
		return m, m.detailCmdForCursor()
	case fetchFailedMsg:
		m.err = msg.err
		return m, nil
	case prDetailMsg:
		m.detail[msg.number] = msg.detail
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.table.SetWidth(msg.Width * 55 / 100)
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
				m.sel.clear() // shown set changes; stale indexes would point elsewhere
				m.applyFilter()
				return m, nil
			case "enter":
				m.filtering = false
				m.filterInput.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.sel.clear() // editing the query reorders the shown set
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
					if a.Scope == "per-selected" {
						return m, m.runBulk(a)
					}
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
		case " ":
			m.sel.toggle(m.table.Cursor())
			m.applyFilter()
			return m, nil
		case "V":
			for i := 0; i < m.section.Len(); i++ {
				if !m.sel.has(i) {
					m.sel.toggle(i)
				}
			}
			m.applyFilter()
			return m, nil
		case "tab":
			m.previewExpanded = !m.previewExpanded
			return m, nil
		default:
			if a, ok := m.actions[msg.String()]; ok {
				if a.Scope == "per-selected" {
					return m, m.runBulk(a)
				}
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
	return m, tea.Batch(cmd, m.detailCmdForCursor())
}

func (m Model) View() string {
	if m.pending != nil {
		n := 0
		if v, ok := m.cursorVars(); ok {
			n = v.Number
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
	if m.section.Len() == 0 && m.err == nil {
		return "Loading PRs…  (q to quit)"
	}
	if m.err != nil && m.section.Len() == 0 {
		return "Error: " + m.err.Error() + "  (q to quit)"
	}
	return m.tableWithPreview() + "\n(q to quit)"
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v2"

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20
