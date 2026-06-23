package ui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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
	vp              viewport.Model
	cursor          int // indexes the section's shown set
	width           int
	height          int
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
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	return Model{
		dir: dir, filter: filter, cache: c, section: NewPRSection(filter),
		vp: viewport.New(0, 0), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(), detail: map[int]gh.PRDetail{}, previewN: 3,
	}
}

func (m *Model) SetRunner(r gh.Runner) { m.runner = r }
func (m *Model) SetRepo(repo string)   { m.repo = repo }

func (m *Model) setPRs(prs []gh.PR) {
	if s, ok := m.section.(*PRSection); ok {
		s.SetPRs(prs)
	}
	m.applyFilter()
}

// moveCursor clamps the cursor to the shown set and keeps it visible.
func (m *Model) moveCursor(delta int) {
	n := m.section.Len()
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	m.renderList()
}

// renderList rebuilds the viewport content from the shown rows and scrolls so the cursor row is visible.
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	listW := l.ListWidth
	var b strings.Builder
	for i := 0; i < m.section.Len(); i++ {
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: listW, Focused: i == m.cursor, Selected: m.sel.has(i),
		}))
		b.WriteString("\n\n")
	}
	m.vp.Width = listW
	m.vp.Height = l.ContentHeight
	m.vp.SetContent(b.String())
	m.scrollToCursor()
}

// rowLines is the visual height of one rendered row: a 2-line body + 1 spacer.
const rowLines = 3

// scrollToCursor nudges the viewport offset only when the cursor row would fall
// outside the visible window, so surrounding rows stay in view (no jump-to-top).
func (m *Model) scrollToCursor() {
	top := m.cursor * rowLines
	bottom := top + rowLines - 1
	off := m.vp.YOffset
	switch {
	case top < off:
		off = top
	case bottom >= off+m.vp.Height:
		off = bottom - m.vp.Height + 1
	}
	if off < 0 {
		off = 0
	}
	m.vp.SetYOffset(off)
}

func (m *Model) applyFilter() {
	m.section.SetShown(matchIdx(m.section.Haystacks(), m.filterInput.Value()))
	if m.cursor >= m.section.Len() {
		m.cursor = m.section.Len() - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.renderList()
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
		m.width, m.height = msg.Width, msg.Height
		m.renderList()
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
			m.sel.toggle(m.cursor)
			m.renderList()
			return m, nil
		case "V":
			for i := 0; i < m.section.Len(); i++ {
				if !m.sel.has(i) {
					m.sel.toggle(i)
				}
			}
			m.renderList()
			return m, nil
		case "tab":
			m.previewExpanded = !m.previewExpanded
			return m, m.detailCmdForCursor()
		case "down", "j":
			m.moveCursor(1)
			return m, m.detailCmdForCursor()
		case "up", "k":
			m.moveCursor(-1)
			return m, m.detailCmdForCursor()
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
	return m, nil
}

func (m Model) View() string {
	if m.pending != nil {
		n := 0
		if v, ok := m.cursorVars(); ok {
			n = v.Number
		}
		return m.header() + "\n" + accentStyle.Render(fmt.Sprintf("%s #%d? y/N", m.pending.Label, n)) +
			"\n" + m.renderMain()
	}
	if m.showActions {
		acts := filterActions(m.actions, m.actionFilter.Value())
		var b strings.Builder
		b.WriteString(m.actionFilter.View() + "\n")
		for i, a := range acts {
			cur := "  "
			if i == m.actionCursor {
				cur = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-6s %s\n", cur, a.Key, a.Label))
		}
		return b.String()
	}
	if m.filtering {
		return m.header() + "\n" + m.filterInput.View() + "\n" + m.renderMain()
	}
	if m.section.Len() == 0 && m.err == nil {
		return m.header() + "\n\n" + dimStyle.Render("  Loading…") + "\n" + m.statusBar()
	}
	if m.err != nil && m.section.Len() == 0 {
		return m.header() + "\n\n" + failStyle.Render("  Error: "+m.err.Error()) + "\n" + m.statusBar()
	}
	return m.header() + "\n" + m.renderMain() + "\n" + m.statusBar()
}

// header is the top line: repo · filter · open count.
func (m Model) header() string {
	return headerStyle.Render("  "+m.repo) + dimStyle.Render(
		fmt.Sprintf("   %s · %d open", m.filter, m.section.Len()))
}

// statusBar is the bottom key/context line.
func (m Model) statusBar() string {
	keys := "↑↓ move · → expand · / filter · a actions · space select · q quit"
	if n := m.sel.count(); n > 0 {
		keys = selMarkStyle.Render(fmt.Sprintf("%d selected", n)) + " · " + keys
	}
	return statusBarStyle.Render("  " + keys)
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v2"

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20
