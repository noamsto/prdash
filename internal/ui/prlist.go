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
	showLegend      bool
	actionFilter    textinput.Model
	actionCursor    int
	sel             selection
	detail          map[int]gh.PRDetail // painted detail (fresh this session or hydrated from disk)
	fresh           map[int]bool        // PR numbers whose detail was refetched this session; gates revalidation
	detailSeq       int                 // bumped on cursor move; gates the debounced detail fetch
	previewExpanded bool
	previewN        int
	expanded        bool
	expandedTab     int
	loaded          bool        // first live fetch has returned; distinguishes empty from loading
	refreshing      bool        // a list fetch for the current filter is in flight
	spinning        bool        // the refresh spinner tick loop is running
	spinnerFrame    int         // advancing index into spinnerFrames
	actionStatus    *actionStat // transient inline-action progress shown by the header
	presetIdx       int         // index into defaultPresets; -1 when filter is a custom (author) query
	previewMax      bool        // z: preview takes full width, list hidden
	hideDrafts      bool        // D: exclude draft PRs from the board
	showPicker      bool
	pickerMode      string // "author" | "reviewer"
	pick            picker
	members         []gh.User  // cached assignable users for this repo
	pendingExec     [][]string // exits-TUI commands to run after quit when no orchestrator sink is set
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	return Model{
		dir: dir, filter: filter, cache: c, section: NewPRSection(filter),
		vp: viewport.New(), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(), detail: map[int]gh.PRDetail{}, fresh: map[int]bool{}, previewN: 2,
		presetIdx: presetIndexFor(filter), refreshing: true,
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

// setMine paints the mine view: authored PRs under "Mine", review-requested
// under "Review requested". A PR that is both stays under Mine.
func (m *Model) setMine(mine, review []gh.PR) {
	cats := make(map[int]string, len(mine)+len(review))
	all := make([]gh.PR, 0, len(mine)+len(review))
	for _, p := range mine {
		cats[p.Number] = "Mine"
		all = append(all, p)
	}
	for _, p := range review {
		if _, dup := cats[p.Number]; dup {
			continue
		}
		cats[p.Number] = "Review requested"
		all = append(all, p)
	}
	if s, ok := m.section.(*PRSection); ok {
		s.SetCategorized(all, cats, []string{"Mine", "Review requested"})
	}
	m.applyFilter()
	if n := m.section.Len(); m.cursor >= n {
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
	innerW := l.ListWidth - 2 // inside the pane's left/right border
	innerH := m.contentHeight(l) - 2
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
	line, prevGroup := 0, ""
	for i := 0; i < m.section.Len(); i++ {
		if grouped {
			if g := ps.groupLabel(i); g != prevGroup {
				if prevGroup != "" { // blank line between groups, not above the first
					b.WriteString("\n")
					line++
				}
				b.WriteString(groupHeader(g, innerW) + "\n")
				line++
				prevGroup = g
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
	visible := m.contentHeight(l) - 2 // inside the pane border
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

// prKey scopes the cached PR list by repo — the shared cache file holds every
// repo's lists, and a filter like "is:open author:@me" is identical across them,
// so without the repo they collide and bleed between repos.
func prKey(repo, filter string) string {
	return cache.Key("pr", repo+"\x00"+filter, defaultLimit, schemaVer)
}

// cachedPRs returns the cached PR list for a filter, if present and parseable.
func (m *Model) cachedPRs(filter string) ([]gh.PR, bool) {
	e, ok := m.cache.Get(prKey(m.repo, filter))
	if !ok {
		return nil, false
	}
	var prs []gh.PR
	if err := json.Unmarshal(e.Rows, &prs); err != nil {
		slog.Debug("cache unmarshal failed", "err", err)
		return nil, false
	}
	return prs, true
}

// hydrate paints rows for the current view from the cache, reporting whether it
// hit. The mine view combines the two cached searches into its sections.
func (m *Model) hydrate() bool {
	if m.cache == nil {
		return false
	}
	if m.isMineView() {
		mine, ok1 := m.cachedPRs(mineFilter)
		rev, ok2 := m.cachedPRs(reviewFilter)
		if !ok1 && !ok2 {
			return false
		}
		m.setMine(mine, rev)
		m.hydrateDetail()
		return true
	}
	prs, ok := m.cachedPRs(m.filter)
	if !ok {
		return false
	}
	m.setPRs(prs)
	m.hydrateDetail()
	return true
}

// hydrateDetail paints each shown PR's detail from the disk cache (leaving it
// non-fresh, so the live prefetch still revalidates). Without this the side
// preview and ! column show Loading… until the first gh pr view returns.
func (m *Model) hydrateDetail() {
	if m.cache == nil {
		return
	}
	ps, ok := m.section.(*PRSection)
	if !ok {
		return
	}
	for i := 0; i < ps.Len(); i++ {
		num := ps.prAt(i).Number
		if _, ok := m.detail[num]; ok {
			continue
		}
		e, hit := m.cache.Get(detailKey(m.repo, num))
		if !hit {
			continue
		}
		var d gh.PRDetail
		if err := json.Unmarshal(e.Rows, &d); err != nil {
			slog.Debug("detail cache unmarshal failed", "err", err)
			continue
		}
		m.detail[num] = d
	}
}

// membersSchemaVer is bumped whenever the assignable-users field set changes.
const membersSchemaVer = "v1"

// membersKey scopes the cached assignable-users list by repo.
func membersKey(repo string) string { return cache.Key("members", repo, 0, membersSchemaVer) }

// hydrateMembers paints the assignable-users list from disk so the author/
// reviewer picker opens instantly; Init refetches once per launch to refresh it.
func (m *Model) hydrateMembers() {
	if m.cache == nil {
		return
	}
	e, ok := m.cache.Get(membersKey(m.repo))
	if !ok {
		return
	}
	var users []gh.User
	if err := json.Unmarshal(e.Rows, &users); err != nil {
		slog.Debug("members cache unmarshal failed", "err", err)
		return
	}
	m.members = users
}

func (m *Model) Hydrate() {
	m.hydrate()
	m.hydrateMembers()
}

// fetchCmd runs `gh pr list` for filter, tagging the result so a background
// prewarm of a non-current preset lands in the cache without repainting the view.
func (m Model) fetchCmd(filter string) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.PRListArgs(filter, defaultLimit)...)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		prs, err := gh.ParsePRs(raw)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		return prsFetchedMsg{filter: filter, prs: prs, raw: raw}
	}
}

// mineFetchCmd fetches both halves of the mine view in one command, caching each
// under its own filter key. Sequential (not parallel) — two quick gh calls.
func (m Model) mineFetchCmd() tea.Cmd {
	r, dir := m.runner, m.dir
	list := func(filter string) ([]gh.PR, []byte, error) {
		raw, err := r.Run(dir, gh.PRListArgs(filter, defaultLimit)...)
		if err != nil {
			return nil, nil, err
		}
		prs, err := gh.ParsePRs(raw)
		return prs, raw, err
	}
	return func() tea.Msg {
		mine, mineRaw, err := list(mineFilter)
		if err != nil {
			return fetchFailedMsg{err: err, filter: mineFilter}
		}
		rev, revRaw, err := list(reviewFilter)
		if err != nil {
			return fetchFailedMsg{err: err, filter: mineFilter}
		}
		return mineFetchedMsg{mine: mine, mineRaw: mineRaw, review: rev, reviewRaw: revRaw}
	}
}

// spinnerFrames is the braille cycle for the header refresh indicator.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// startSpinner kicks the tick loop unless it is already running (one loop only).
func (m *Model) startSpinner() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinnerTick()
}

// switchToFilter repoints the model at m.filter: it paints cached rows instantly
// when the preset is warm (else clears stale rows), flags a refresh, and returns
// the live fetch to reconcile.
func (m *Model) switchToFilter() tea.Cmd {
	m.cursor = 0
	m.sel.clear()
	m.refreshing = true
	hit := m.hydrate()
	m.loaded = hit // warm cache shows data/empty-state; a miss shows Loading…
	if !hit {
		m.setPRs(nil) // drop the previous preset's rows while the fetch is in flight
	}
	fetch := m.fetchCmd(m.filter)
	if m.isMineView() {
		fetch = m.mineFetchCmd()
	}
	return tea.Batch(fetch, m.startSpinner())
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
			return fetchFailedMsg{err: err}
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
		return m.switchToFilter()
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

func (m Model) Init() tea.Cmd {
	// Prewarm both views (the current one paints on arrival, the other just
	// caches), refresh members once, and start the spinner.
	return tea.Batch(
		m.mineFetchCmd(),
		m.fetchCmd("is:open"),
		m.fetchMembersCmd(),
		spinnerTick(),
	)
}

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
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(prKey(m.repo, msg.filter), msg.raw)
		}
		if msg.filter != "" && msg.filter != m.filter {
			return m, nil // background prewarm of another preset: cache only
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear() // selection indexes the shown set; new data invalidates it
		m.setPRs(msg.prs)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
	case mineFetchedMsg:
		if m.cache != nil {
			m.cache.Set(prKey(m.repo, mineFilter), msg.mineRaw)
			m.cache.Set(prKey(m.repo, reviewFilter), msg.reviewRaw)
		}
		if m.filter != mineFilter {
			return m, nil // prewarm while viewing something else
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear()
		m.setMine(msg.mine, msg.review)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
	case fetchFailedMsg:
		if msg.filter != "" && msg.filter != m.filter {
			return m, nil // a background prewarm failed; the current view is unaffected
		}
		m.refreshing = false
		m.err = msg.err
		return m, nil
	case membersFetchedMsg:
		m.members = msg.users
		if m.cache != nil {
			if raw, err := json.Marshal(msg.users); err == nil {
				m.cache.Set(membersKey(m.repo), raw)
			}
		}
		if m.showPicker {
			m.pick.cands = msg.users
		}
		return m, nil
	case prDetailMsg:
		m.detail[msg.number] = msg.detail
		m.fresh[msg.number] = true
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(detailKey(m.repo, msg.number), msg.raw)
		}
		m.renderList()
		return m, nil
	case detailDebounceMsg:
		if msg.seq != m.detailSeq {
			return m, nil
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
	case spinnerTickMsg:
		if !m.refreshing && !m.actionRunning() {
			m.spinning = false // fetch/action settled; let the loop die
			return m, nil
		}
		m.spinning = true
		m.spinnerFrame++
		return m, spinnerTick()
	case actionDoneMsg:
		// Scope the error to the status line rather than m.err, which blanks the board.
		if m.actionStatus != nil {
			m.actionStatus.settled = true
			m.actionStatus.err = msg.err
			if msg.ok != "" {
				m.actionStatus.ok = msg.ok
			}
			if msg.fail != "" {
				m.actionStatus.fail = msg.fail
			}
		}
		return m, clearStatusCmd()
	case actionClearMsg:
		if m.actionStatus != nil && m.actionStatus.settled {
			m.actionStatus = nil
		}
		return m, nil
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
			case "space":
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
						return m, m.startBulk(a)
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
		if m.showLegend {
			m.showLegend = false // any key dismisses the legend
			return m, nil
		}
		switch msg.String() {
		case "a":
			m.showActions = true
			return m, m.actionFilter.Focus()
		case "f":
			// presetIdx is -1 for a custom (author) filter; max(...,0) makes f resume from "mine".
			m.presetIdx = nextPreset(max(m.presetIdx, 0))
			m.filter = defaultPresets[m.presetIdx].search
			return m, m.switchToFilter()
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
		case "?":
			m.showLegend = true
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space":
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
					return m, m.startBulk(a)
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
		prompt := ""
		if m.pending.Scope == "per-selected" {
			prompt = fmt.Sprintf("%s for %d PRs? y/N", m.pending.Label, len(m.selectedOrCursor()))
		} else {
			n := 0
			if v, ok := m.cursorVars(); ok {
				n = v.Number
			}
			prompt = fmt.Sprintf("%s #%d? y/N", m.pending.Label, n)
		}
		return m.header() + "\n" + accentStyle.Render(prompt) + "\n" + m.renderMain()
	}
	if m.showPicker {
		return m.pickerView()
	}
	if m.showLegend {
		return m.legendView()
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
		// Height keys off the full action count, not the filtered one, so the pane
		// stays a constant size as you type instead of shrinking per keystroke.
		panel := titledBox(strings.TrimRight(b.String(), "\n"), 40, len(m.actions)+3, "Actions")
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
	l := computeLayout(m.width, m.height)
	if m.previewMax && l.ShowSide {
		return m.header() + "\n" + m.renderMain() // zoom fills the frame; action folded into the title
	}
	if l.ShowSide && l.ShowPanel {
		return m.header() + "\n" + m.renderDocked(l)
	}
	foot := m.statusBar()
	if l.ShowPanel {
		foot = m.keysActionsPanel(m.width)
	}
	return m.header() + "\n" + m.renderMain() + "\n" + foot
}

// clearStatusCmd wipes a settled action badge after its dwell time.
func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return actionClearMsg{} })
}

// header is the top line: repo · filter · open count.
func (m Model) header() string {
	label := m.filter
	if m.presetIdx >= 0 {
		label = defaultPresets[m.presetIdx].name
	}
	h := headerStyle.Render("  "+m.repo) + dimStyle.Render(fmt.Sprintf("   %s · %d open", label, m.section.Len()))
	if m.refreshing {
		spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		h += dimStyle.Render(" · ") + refreshStyle.Render(spin+" refreshing")
	}
	if s := m.actionStatus; s != nil {
		switch {
		case !s.settled:
			spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
			h += "  " + runBadgeStyle.Render(spin+" "+s.run+"…")
		case s.err != nil:
			h += "  " + failBadgeStyle.Render("✗ "+s.fail)
		default:
			h += "  " + passBadgeStyle.Render("✓ "+s.ok)
		}
	}
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

// legendView is the ?-toggled glyph + key reference, as a centered modal.
func (m Model) legendView() string {
	rows := []string{
		accentStyle.Render("CI / review") + statusBarStyle.Render("  ✓ pass   ✗ fail   ● running   · none"),
		accentStyle.Render("!") + statusBarStyle.Render("           ⚠ conflict / behind base"),
		accentStyle.Render("row") + statusBarStyle.Render("         ▎ focus   ● selected   [draft] dimmed"),
		"",
		accentStyle.Render("↵") + statusBarStyle.Render(" worktree   ") + accentStyle.Render("y") + statusBarStyle.Render(" #  ") + accentStyle.Render("Y") + statusBarStyle.Render(" url  ") + accentStyle.Render("b") + statusBarStyle.Render(" branch   ") + accentStyle.Render("o") + statusBarStyle.Render(" open   ") + accentStyle.Render("a") + statusBarStyle.Render(" actions"),
		accentStyle.Render("f") + statusBarStyle.Render(" filter   ") + accentStyle.Render("F") + statusBarStyle.Render(" author   ") + accentStyle.Render("R") + statusBarStyle.Render(" reviewers   ") + accentStyle.Render("D") + statusBarStyle.Render(" drafts"),
		accentStyle.Render("ctrl+j/k") + statusBarStyle.Render(" scroll preview   ") + accentStyle.Render("z") + statusBarStyle.Render(" maximize   ") + accentStyle.Render("esc") + statusBarStyle.Render(" close"),
	}
	body := strings.Join(rows, "\n")
	panel := titledBox(body, lipgloss.Width(body)+4, len(rows)+2, "Legend")
	return modal(panel, m.width, m.height)
}

// actionOrder is the display order for the docked panel's actions section, so
// it doesn't jump around with Go's random map iteration.
var actionOrder = []string{"enter", "m", "r", "u", "M", "W", "y", "Y", "b", "o"}

type keyHint struct{ key, label string }

// navHints is the keybinding cheatsheet shown in the docked panel's top section.
var navHints = []keyHint{
	{"↑↓", "move"}, {"→", "expand"}, {"z", "max"}, {"ctrl+j/k", "scroll"},
	{"f", "filter"}, {"F", "author"}, {"R", "reviewers"}, {"/", "find"},
	{"space", "select"}, {"V", "all"}, {"D", "drafts"}, {"q", "quit"},
}

// gridHints lays hints into aligned columns: every cell is padded to the widest
// hint's width so columns line up vertically across rows (a greedy pack leaves
// a ragged, cramped-looking grid). Reflows to as many columns as fit in width.
func gridHints(hints []keyHint, width int, alignKeys bool) []string {
	if len(hints) == 0 {
		return nil
	}
	// alignKeys pads every key to the widest so the labels line up in a column.
	keyW := 0
	if alignKeys {
		for _, h := range hints {
			if w := lipgloss.Width(h.key); w > keyW {
				keyW = w
			}
		}
	}
	render := func(h keyHint) string {
		key := accentStyle.Render(h.key)
		if pad := keyW - lipgloss.Width(h.key); pad > 0 {
			key += strings.Repeat(" ", pad)
		}
		return key + statusBarStyle.Render(" "+h.label)
	}
	const gutter = 3
	cellW := 0
	for _, h := range hints {
		if w := lipgloss.Width(render(h)); w > cellW {
			cellW = w
		}
	}
	cellW += gutter
	cols := max(1, (width+gutter)/cellW)
	var lines []string
	for i := 0; i < len(hints); i += cols {
		var b strings.Builder
		for j := i; j < i+cols && j < len(hints); j++ {
			s := render(hints[j])
			b.WriteString(s)
			if j < i+cols-1 && j < len(hints)-1 { // pad every cell but the row's last
				b.WriteString(strings.Repeat(" ", cellW-lipgloss.Width(s)))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// panelHeader is a column heading: just the uppercase label — the box already
// frames the panel, so no trailing rule.
func panelHeader(label string) string {
	return sectionLabelStyle.Render(strings.ToUpper(label))
}

// panelSplit divides the panel interior into a keys column, a 3-wide separator
// (space · rule · space), and an actions column.
func panelSplit(innerW int) (leftW, rightW int) {
	const sepW = 3
	leftW = (innerW - sepW) / 2
	return leftW, innerW - sepW - leftW
}

// panelColumn is a headed, grid-packed block padded to exactly w wide so the
// column to its right lines up. alignKeys column-aligns the labels.
func panelColumn(label string, hints []keyHint, w int, alignKeys bool) string {
	lines := append([]string{panelHeader(label)}, gridHints(hints, w, alignKeys)...)
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}

// panelBody lays keys on the left and actions on the right, split by a vertical
// rule. Narrow columns collapse each side to a single vertical stack. Action
// labels are column-aligned; keys aren't (their widths vary too much).
func panelBody(innerW int, actionsLabel string, acts []keyHint) string {
	lw, rw := panelSplit(innerW)
	left := panelColumn("keys", navHints, lw, false)
	right := panelColumn(actionsLabel, acts, rw, true)
	h := max(lipgloss.Height(left), lipgloss.Height(right))
	// Each separator line must carry its own padding — wrapping the whole
	// multi-line rule in " "+…+" " only pads the first and last rows, jagging
	// the divider and the right border.
	sepLine := " " + sepStyle.Render("│") + " "
	sep := strings.TrimSuffix(strings.Repeat(sepLine+"\n", h), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
}

// panelContentRows is the tallest of the two columns (each = header + grid).
// Reserved against the full action set so the height is stable when batch mode
// hides the single-only actions.
func panelContentRows(innerW int) int {
	lw, rw := panelSplit(innerW)
	return max(1+len(gridHints(navHints, lw, false)), 1+len(gridHints(defaultActionHints(), rw, true)))
}

// defaultActionHints is the action list computeLayout reserves space for,
// without needing a Model.
func defaultActionHints() []keyHint {
	acts := action.DefaultPRActions()
	hs := make([]keyHint, 0, len(actionOrder))
	for _, k := range actionOrder {
		if a, ok := acts[k]; ok {
			hs = append(hs, keyHint{a.Key, a.Label})
		}
	}
	return hs
}

// panelRowsFor is the panel's outer height (border + tallest column) at a given
// interior width.
func panelRowsFor(innerW int) int {
	return panelContentRows(innerW) + 2
}

// batchCapable reports whether an action operates over the whole selection —
// the copy builtins and the per-selected worktree fan-out.
func batchCapable(a action.Action) bool {
	return a.Scope == "per-selected" || strings.HasPrefix(a.Command.Builtin, "copy-")
}

// actionHints is the actions shown in the panel, with a column header. With a
// selection active the panel enters batch mode: only batch-capable actions show
// (the single-only ones act on the cursor, not the selection, so they'd mislead).
func (m Model) actionHints() (label string, hints []keyHint) {
	batch := m.sel.count() > 0
	for _, k := range actionOrder {
		a, ok := m.actions[k]
		if !ok || (batch && !batchCapable(a)) {
			continue
		}
		hints = append(hints, keyHint{a.Key, a.Label})
	}
	if batch {
		return fmt.Sprintf("batch · %d", m.sel.count()), hints
	}
	return "actions", hints
}

// keysActionsPanel is the docked footer: a bordered box with the keybinding
// cheatsheet and the focused view's actions, sized to the given outer width.
func (m Model) keysActionsPanel(w int) string {
	label, acts := m.actionHints()
	return titledBox(panelBody(w-2, label, acts), w, panelRowsFor(w-2), "help")
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
	rule := sepStyle.Render(strings.Repeat("─", max(m.width, 1)))
	return rule + "\n  " + strings.Join(parts, "  ")
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v2"

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20
