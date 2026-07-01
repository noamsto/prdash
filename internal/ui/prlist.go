package ui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

type Model struct {
	dir             string
	filter          string
	cache           *cache.Cache
	runner          gh.Runner
	vp              viewport.Model
	cursor          int // indexes the section's shown set
	cursorLine      int // display-line offset of the cursor row (headers shift it)
	previewOffset   int // ctrl+j/k scroll position within the side preview
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
	detailSeq       int // bumped on cursor move; gates the debounced detail fetch
	previewExpanded bool
	previewN        int
	expanded        bool
	expandedTab     int
	loaded          bool // first live fetch has returned; distinguishes empty from loading
	presetIdx       int  // index into defaultPresets; -1 when filter is a custom (author) query
	previewMax      bool // z: preview takes full width, list hidden
	hideDrafts      bool // D: exclude draft PRs from the board
	showPicker      bool
	pickerMode      string // "author" | "reviewer"
	pick            picker
	members         []gh.User // cached assignable users for this repo
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	return Model{
		dir: dir, filter: filter, cache: c, section: NewPRSection(filter),
		vp: viewport.New(), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(), detail: map[int]gh.PRDetail{}, previewN: 2,
		presetIdx: presetIndexFor(filter),
	}
}

func (m *Model) SetRunner(r gh.Runner) { m.runner = r }
func (m *Model) SetRepo(repo string)   { m.repo = repo }

func (m *Model) setPRs(prs []gh.PR) {
	if s, ok := m.section.(*PRSection); ok {
		// Outside the "mine" view, group by author even with a single author, so
		// you always see whose PRs you're looking at.
		s.SetForceGroup(!m.isMineView())
		s.SetPRs(prs)
	}
	m.applyFilter()
	if n := m.section.Len(); m.cursor >= n { // a refetch may shrink the shown set
		m.cursor = max(0, n-1)
	}
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
	m.previewOffset = 0
	m.renderList()
}

// renderList rebuilds the viewport content from the shown rows and scrolls so the cursor row is visible.
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	innerW := l.ListWidth - 2  // inside the pane's left/right border
	innerH := l.ContentHeight - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	numW := columnWidths(m.section)
	ps, isPR := m.section.(*PRSection)
	grouped := isPR && ps.grouped
	var b strings.Builder
	line, prevAuthor := 0, ""
	for i := 0; i < m.section.Len(); i++ {
		if grouped {
			if a := ps.prAt(i).Author.Login; a != prevAuthor {
				if prevAuthor != "" { // blank line between groups, not above the first
					b.WriteString("\n")
					line++
				}
				b.WriteString(groupHeader(a, innerW) + "\n")
				line++
				prevAuthor = a
			}
		}
		if i == m.cursor {
			m.cursorLine = line
		}
		flag := ""
		if isPR {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: innerW, NumWidth: numW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag,
		}))
		b.WriteString("\n")
		line++
	}
	if m.section.Len() == 0 {
		m.cursorLine = 0
	}
	m.vp.SetWidth(innerW)
	m.vp.SetHeight(innerH)
	m.vp.SetContent(b.String())
	m.scrollToCursor()
}

// scrollToCursor nudges the viewport offset only when the cursor row (at its
// display line, headers included) would fall outside the visible window.
func (m *Model) scrollToCursor() {
	top := m.cursorLine
	off := m.vp.YOffset()
	switch {
	case top < off:
		off = top
	case top >= off+m.vp.Height():
		off = top - m.vp.Height() + 1
	}
	if off < 0 {
		off = 0
	}
	m.vp.SetYOffset(off)
}

// previewScrollBy scrolls the side preview by delta lines, clamped so the last
// line can't scroll above the top of the pane.
func (m *Model) previewScrollBy(delta int) {
	l := computeLayout(m.width, m.height)
	visible := l.ContentHeight - 2 // inside the pane border
	over := lipgloss.Height(m.previewPane()) - visible
	if over < 0 {
		over = 0 // content fits the pane; nothing to scroll
	}
	m.previewOffset += delta
	if m.previewOffset > over {
		m.previewOffset = over
	}
	if m.previewOffset < 0 {
		m.previewOffset = 0
	}
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

// openPicker shows the member picker in the given mode, pre-checking the right
// set, and fetches the member list if it isn't cached yet.
func (m *Model) openPicker(mode string) tea.Cmd {
	checked := map[string]bool{}
	title := "Filter by author"
	if mode == "reviewer" {
		title = "Assign reviewers"
		if v, ok := m.cursorVars(); ok {
			if d, cached := m.detail[v.Number]; cached {
				for _, r := range d.ReviewRequests {
					if r.Login != "" {
						checked[r.Login] = true
					}
				}
			}
		}
	}
	m.showPicker = true
	m.pickerMode = mode
	m.pick = newPicker(title, m.members, checked)
	if m.members == nil {
		return m.fetchMembersCmd()
	}
	return nil
}

func (m Model) fetchMembersCmd() tea.Cmd {
	r, dir, repo := m.runner, m.dir, m.repo
	return func() tea.Msg {
		users, err := gh.FetchAssignableUsers(r, dir, repo)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return membersFetchedMsg{users: users}
	}
}

// confirmPicker applies the picker result based on the active mode.
func (m *Model) confirmPicker() tea.Cmd {
	checked := m.pick.checked
	switch m.pickerMode {
	case "author":
		var terms []string
		for login, on := range checked {
			if on {
				terms = append(terms, "author:"+login)
			}
		}
		if len(terms) == 0 {
			return nil // empty selection: keep the current filter
		}
		slices.Sort(terms)
		m.filter = "is:open " + strings.Join(terms, " ")
		m.presetIdx = -1
		m.cursor = 0
		m.loaded = false
		return m.fetchCmd(m.runner)
	case "reviewer":
		v, ok := m.cursorVars()
		if !ok {
			return nil
		}
		var current []string
		if d, cached := m.detail[v.Number]; cached {
			for _, rr := range d.ReviewRequests {
				if rr.Login != "" {
					current = append(current, rr.Login)
				}
			}
		}
		add, remove := reviewerDiff(current, checked)
		return m.assignReviewersCmd(v.Number, add, remove)
	}
	return nil
}

func (m Model) Init() tea.Cmd { return m.fetchCmd(m.runner) }

// debounceDetailCmd schedules a detail fetch ~150ms out, tagged with the current
// seq so a later move cancels it (the stale tick is ignored on arrival).
func (m Model) debounceDetailCmd() tea.Cmd {
	seq := m.detailSeq
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return detailDebounceMsg{seq: seq}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case prsFetchedMsg:
		m.loaded = true
		m.sel.clear() // selection indexes the shown set; new data invalidates it
		m.setPRs(msg.prs)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(cache.Key("pr", m.filter, defaultLimit, schemaVer), msg.raw)
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
	case fetchFailedMsg:
		m.err = msg.err
		return m, nil
	case membersFetchedMsg:
		m.members = msg.users
		if m.showPicker {
			m.pick.cands = msg.users
		}
		return m, nil
	case prDetailMsg:
		m.detail[msg.number] = msg.detail
		m.renderList()
		return m, nil
	case detailDebounceMsg:
		if msg.seq != m.detailSeq {
			return m, nil
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.renderList()
		return m, nil
	case tea.KeyMsg:
		if m.expanded {
			return m.updateExpanded(msg)
		}
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
		if m.showPicker {
			switch msg.String() {
			case "esc":
				m.showPicker = false
				return m, nil
			case "enter":
				m.showPicker = false
				return m, m.confirmPicker()
			case " ":
				m.pick.toggleCursor()
				return m, nil
			case "up", "ctrl+p":
				if m.pick.cursor > 0 {
					m.pick.cursor--
				}
				return m, nil
			case "down", "ctrl+n":
				if m.pick.cursor < len(m.pick.visible())-1 {
					m.pick.cursor++
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.pick.filter, cmd = m.pick.filter.Update(msg)
				m.pick.cursor = 0
				return m, cmd
			}
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
		case "f":
			// presetIdx is -1 for a custom (author) filter; max(...,0) makes f resume from "mine".
			m.presetIdx = nextPreset(max(m.presetIdx, 0))
			m.filter = defaultPresets[m.presetIdx].search
			m.cursor = 0
			m.loaded = false
			return m, m.fetchCmd(m.runner)
		case "z":
			m.previewMax = !m.previewMax
			return m, nil
		case "ctrl+j":
			m.previewScrollBy(1)
			return m, nil
		case "ctrl+k":
			m.previewScrollBy(-1)
			return m, nil
		case "D":
			m.hideDrafts = !m.hideDrafts
			if ps, ok := m.section.(*PRSection); ok {
				ps.SetHideDrafts(m.hideDrafts)
			}
			m.sel.clear() // the shown set changes; stale indexes would point elsewhere
			m.applyFilter()
			return m, nil
		case "F":
			return m, m.openPicker("author")
		case "R":
			if _, ok := m.cursorVars(); ok {
				return m, m.openPicker("reviewer")
			}
			return m, nil
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
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "down", "j":
			m.moveCursor(1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "up", "k":
			m.moveCursor(-1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "right", "l":
			m.enterExpanded()
			m.detailSeq++
			return m, m.debounceDetailCmd()
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

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.expanded {
		return m.expandedView()
	}
	if m.pending != nil {
		n := 0
		if v, ok := m.cursorVars(); ok {
			n = v.Number
		}
		return m.header() + "\n" + accentStyle.Render(fmt.Sprintf("%s #%d? y/N", m.pending.Label, n)) +
			"\n" + m.renderMain()
	}
	if m.showPicker {
		return m.pickerView()
	}
	if m.showActions {
		acts := filterActions(m.actions, m.actionFilter.Value())
		var b strings.Builder
		b.WriteString(m.actionFilter.View() + "\n")
		for i, a := range acts {
			cursor := "  "
			line := fmt.Sprintf("%-6s %s", a.Key, a.Label)
			if i == m.actionCursor {
				cursor = accentStyle.Render("▸ ")
				line = accentStyle.Render(line)
			} else {
				line = statusBarStyle.Render(line)
			}
			b.WriteString(cursor + line + "\n")
		}
		panel := titledBox(strings.TrimRight(b.String(), "\n"), 40, len(acts)+3, "Actions")
		return modal(panel, m.width, m.height)
	}
	if m.filtering {
		return m.header() + "\n" + m.filterInput.View() + "\n" + m.renderMain()
	}
	if m.err != nil && m.section.Len() == 0 {
		return m.header() + "\n\n" + failStyle.Render("  Error: "+m.err.Error()) + "\n" + m.statusBar()
	}
	if m.section.Len() == 0 {
		hint := "  Loading…"
		if m.loaded {
			hint = "  No open PRs."
		}
		return m.header() + "\n\n" + dimStyle.Render(hint) + "\n" + m.statusBar()
	}
	return m.header() + "\n" + m.renderMain() + "\n" + m.statusBar()
}

// header is the top line: repo · filter · open count.
func (m Model) header() string {
	label := m.filter
	if m.presetIdx >= 0 {
		label = defaultPresets[m.presetIdx].name
	}
	h := headerStyle.Render("  "+m.repo) + dimStyle.Render(fmt.Sprintf("   %s · %d open", label, m.section.Len()))
	if n := m.sel.count(); n > 0 {
		h += "  " + selMarkStyle.Render(fmt.Sprintf("%d selected", n))
	}
	return h
}

// listTitle is the list pane's border title: the section kind + shown count.
func (m Model) listTitle() string {
	if m.section.Kind() == "issue" {
		return fmt.Sprintf("Issues · %d", m.section.Len())
	}
	return fmt.Sprintf("PRs · %d", m.section.Len())
}

// isMineView reports whether the active view is the "mine" preset, where every
// PR is the author's own — so grouping by author would be noise.
func (m Model) isMineView() bool {
	return m.presetIdx >= 0 && defaultPresets[m.presetIdx].name == "mine"
}

// cursorCard is the triage card for the focused PR, when its detail is cached.
func (m Model) cursorCard() (triage.Card, bool) {
	ps, ok := m.section.(*PRSection)
	if !ok || m.section.Len() == 0 {
		return triage.Card{}, false
	}
	d, cached := m.detail[ps.prAt(m.cursor).Number]
	if !cached {
		return triage.Card{}, false
	}
	return triage.Compute(ps.prAt(m.cursor), d), true
}

// statusBar is the bottom keybinding line, in the lazytmux picker style:
// accent key + dim ":label", space-separated. It leads with the focused PR's
// recommended action, and a live toggle (drafts) highlights its label when
// active — the indication lives on the key itself, not as floating status text.
func (m Model) statusBar() string {
	hint := func(k, desc string) string {
		return accentStyle.Render(k) + statusBarStyle.Render(":"+desc)
	}
	parts := []string{}
	if card, ok := m.cursorCard(); ok && card.ActionKey != "" {
		parts = append(parts, hint(card.ActionKey, card.ActionLabel))
	}
	drafts := draftTagStyle.Render("drafts") // peach while drafts are on the board
	if m.hideDrafts {
		drafts = statusBarStyle.Render("drafts") // dimmed once they're hidden
	}
	parts = append(parts,
		hint("↵", "worktree"), hint("a", "actions"), hint("→", "expand"),
		hint("f", "filter"), hint("/", "find"), hint("space", "select"),
		accentStyle.Render("D")+statusBarStyle.Render(":")+drafts,
		hint("q", "quit"),
	)
	return "  " + strings.Join(parts, "  ")
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v2"

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20
