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
	"github.com/sahilm/fuzzy"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
	"github.com/noamsto/prdash/internal/triage"
)

// boardView is the per-mode selection saved across an i-toggle so flipping back
// lands on the same state/preset the user left.
type boardView struct {
	state, body, filter string
	presetIdx           int
}

type Model struct {
	dir               string
	filter            string
	state             string    // open | merged | closed; the s-toggle dimension
	body              string    // state-agnostic qualifier (e.g. "author:@me", "")
	mode              string    // "pr" | "issue"; the i-toggle dimension
	omniServer        string    // committed server-side qualifier from the omni filter (Phase C); "" on the empty default
	omniSeq           int       // bumped on each server-qualifier change; gates the debounced SWR refetch
	other             boardView // the inactive board's saved state/preset (restored on toggle-back)
	issueDetail       map[int]gh.IssueDetail
	issueFresh        map[int]bool // issue numbers whose body was refetched this session
	cache             *cache.Cache
	runner            gh.Runner
	vp                viewport.Model
	cursor            int // indexes the section's shown set
	cursorLine        int // display-line offset of the cursor row (headers shift it)
	previewOffset     int // ctrl+j/k scroll position within the side preview
	width             int
	height            int
	section           Section
	err               error
	filtering         bool
	filterInput       textinput.Model
	omniSuggestCursor int // highlighted row in the @-mention autocomplete dropdown
	repo              string
	actions           map[string]action.Action
	pending           *action.Action
	showActions       bool
	showLegend        bool
	actionFilter      textinput.Model
	actionCursor      int
	sel               selection
	detail            map[int]gh.PRDetail // painted detail (fresh this session or hydrated from disk)
	fresh             map[int]bool        // PR numbers whose detail was refetched this session; gates revalidation
	detailSeq         int                 // bumped on cursor move; gates the debounced detail fetch
	previewExpanded   bool
	previewN          int
	expanded          bool
	expandedTab       int
	checkCursor       int         // hovered check on the expanded Checks tab
	loaded            bool        // first live fetch has returned; distinguishes empty from loading
	emptyNotice       string      // overrides the empty-board hint (e.g. issues disabled on this repo)
	refreshing        bool        // a list fetch for the current filter is in flight
	spinning          bool        // the refresh spinner tick loop is running
	spinnerFrame      int         // advancing index into spinnerFrames
	polling           bool        // the live-checks poll tick loop is running
	actionStatus      *actionStat // transient inline-action progress shown by the header
	presetIdx         int         // index into defaultPresets; -1 when filter is a custom (author) query
	previewMax        bool        // z: preview takes full width, list hidden
	hideDrafts        bool        // D: exclude draft PRs from the board
	showPicker        bool
	pickerMode        string // "author" | "reviewer"
	pick              picker
	members           []gh.User  // cached assignable users for this repo
	viewerLogin       string     // authenticated user's login; splits Mine from Others in the sections view
	pendingExec       [][]string // exits-TUI commands to run after quit when no orchestrator sink is set
	themeMode         string     // "light"|"dark"; active palette mode
	themeModTime      time.Time  // last-seen mtime of the theme-state file
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	af := textinput.New()
	af.Prompt = "› "
	state, body := splitState(filter, prStates)
	resolved := searchFor("pr", state, body)
	return Model{
		dir: dir, filter: resolved, state: state, body: body, mode: "pr",
		other: boardView{
			state: "open", body: assigneeBody, filter: searchFor("issue", "open", assigneeBody),
			presetIdx: 0, // issuePresets[0] == "mine"
		},
		cache: c, section: NewPRSection(resolved),
		vp: viewport.New(), filterInput: ti, actionFilter: af,
		actions: action.DefaultPRActions(),
		detail:  map[int]gh.PRDetail{}, fresh: map[int]bool{},
		issueDetail: map[int]gh.IssueDetail{}, issueFresh: map[int]bool{},
		previewN:  2,
		presetIdx: -1, refreshing: true, // the PR board has no presets; sections replace them
	}
}

func (m *Model) SetRunner(r gh.Runner) { m.runner = r }
func (m *Model) SetRepo(repo string)   { m.repo = repo }

func (m *Model) setPRs(prs []gh.PR) {
	if s, ok := m.section.(*PRSection); ok {
		// Outside the sections default, group by author even with a single
		// author, so you always see whose PRs you're looking at.
		s.SetState(m.state)
		s.SetForceGroup(!m.sectionsDefault())
		s.SetPRs(prs)
	}
	m.applyFilter()
	if n := m.section.Len(); m.cursor >= n { // a refetch may shrink the shown set
		m.cursor = max(0, n-1)
	}
}

func (m *Model) setIssues(is []gh.Issue) {
	if s, ok := m.section.(*IssueSection); ok {
		s.SetIssues(is)
	}
	m.applyFilter()
	if n := m.section.Len(); m.cursor >= n {
		m.cursor = max(0, n-1)
	}
}

// setSections paints the empty-default open view: Review requested → Mine →
// Others. Precedence is Review > Mine > Others (first match wins). Mine needs the
// real viewer login to split one open list client-side; an empty viewer (login
// not yet resolved) collapses Mine into Others until viewerFetchedMsg re-runs this.
func (m *Model) setSections(review, open []gh.PR, viewer string) {
	cats := make(map[int]string, len(open)+len(review))
	all := make([]gh.PR, 0, len(open)+len(review))
	for _, p := range review {
		cats[p.Number] = "Review requested"
		all = append(all, p)
	}
	for _, p := range open {
		if _, dup := cats[p.Number]; dup {
			continue // already Review requested; precedence wins
		}
		if viewer != "" && p.Author.Login == viewer {
			cats[p.Number] = "Mine"
		} else {
			cats[p.Number] = "Others"
		}
		all = append(all, p)
	}
	if s, ok := m.section.(*PRSection); ok {
		s.SetState(m.state)
		s.SetCategorized(all, cats, []string{"Review requested", "Mine", "Others"})
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
		if isPR && ps.prAt(i).State == "OPEN" {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: innerW, NumWidth: numW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag,
		}))
		b.WriteString("\n")
		line++
	}
	content := b.String()
	if m.section.Len() == 0 {
		m.cursorLine = 0
		hint := "Loading…"
		switch {
		case m.emptyNotice != "":
			hint = m.emptyNotice
		case m.loaded:
			noun := "PRs"
			if m.section.Kind() == "issue" {
				noun = "issues"
			}
			hint = fmt.Sprintf("No %s %s.", m.state, noun)
		}
		content = dimStyle.Render(hint)
	}
	m.vp.SetWidth(innerW)
	m.vp.SetHeight(innerH)
	m.vp.SetContent(content)
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
	query := m.filterInput.Value()
	// The PR board splits server qualifiers from bare fuzzy text: bare text
	// flattens the sections (fuzzy rank), while the issue board fuzzes the raw
	// input as-is.
	if ps, ok := m.section.(*PRSection); ok {
		_, bare := parseOmni(query)
		ps.SetForceFlat(bare != "")
		query = bare
	}
	m.section.SetShown(matchIdx(m.section.Haystacks(), query))
	if m.cursor >= m.section.Len() {
		m.cursor = m.section.Len() - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.renderList()
}

// cursorDelta maps an omni-mode navigation key to a signed row delta; page keys
// jump a screenful, arrows/ctrl-n/p one row. moveCursor clamps the result.
func cursorDelta(key string) int {
	switch key {
	case "up", "ctrl+p":
		return -1
	case "down", "ctrl+n":
		return 1
	case "pgup":
		return -10
	case "pgdown":
		return 10
	}
	return 0
}

// omniServerCmd re-parses the omni input; when the server-qualifier half changed,
// it repoints m.filter and arms a debounced SWR refetch. Bare text is handled by
// applyFilter (instant), so a pure-text edit arms nothing.
func (m *Model) omniServerCmd() tea.Cmd {
	server, _ := parseOmni(m.filterInput.Value())
	if server == m.omniServer {
		return nil
	}
	m.omniServer = server
	m.filter = searchFor("pr", m.state, server)
	m.omniSeq++
	seq := m.omniSeq
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return omniDebounceMsg{seq: seq}
	})
}

// omniActivePartial returns the @-login partial immediately left of the
// cursor, and whether the cursor sits inside such a token. "@" alone yields
// "".
func (m Model) omniActivePartial() (string, bool) {
	r := []rune(m.filterInput.Value())
	pos := min(m.filterInput.Position(), len(r))
	v := string(r[:pos])
	i := strings.LastIndexAny(v, " ")
	tok := v[i+1:]
	if !strings.HasPrefix(tok, "@") {
		return "", false
	}
	return tok[1:], true
}

// omniSuggestions is the active @-partial's member candidates, narrowed by
// fuzzy match; nil outside an @ token or off the PR omni bar.
func (m Model) omniSuggestions() []gh.User {
	if !m.filtering || m.mode != "pr" {
		return nil
	}
	partial, ok := m.omniActivePartial()
	if !ok {
		return nil
	}
	if partial == "" {
		return m.members
	}
	logins := make([]string, len(m.members))
	for i, u := range m.members {
		logins[i] = u.Login
	}
	out := []gh.User{}
	for _, mt := range fuzzy.Find(partial, logins) {
		out = append(out, m.members[mt.Index])
	}
	return out
}

// completeOmniAt replaces the active @-partial with @<login>, moving the
// cursor past the inserted token.
func (m *Model) completeOmniAt(login string) {
	v := m.filterInput.Value()
	pos := m.filterInput.Position()
	left := v[:pos]
	i := strings.LastIndexAny(left, " ")
	rewritten := left[:i+1] + "@" + login + v[pos:]
	m.filterInput.SetValue(rewritten)
	m.filterInput.SetCursor(i + 1 + len("@"+login))
}

// omniSuggestDropdownRows caps how many members the @-mention dropdown lists
// at once; the fuzzy partial narrows the set to reach the rest.
const omniSuggestDropdownRows = 6

// omniSuggestDropdown renders the @-mention candidate list under the omni bar,
// highlighting m.omniSuggestCursor; "" when no suggestions are active.
func (m Model) omniSuggestDropdown() string {
	sug := m.omniSuggestions()
	if len(sug) == 0 {
		return ""
	}
	n := min(len(sug), omniSuggestDropdownRows)
	lines := make([]string, n)
	for i, u := range sug[:n] {
		cur := "  "
		if i == m.omniSuggestCursor {
			cur = accentStyle.Render("▸ ")
		}
		lines[i] = cur + truncate("@"+u.Login, max(1, m.width-2))
	}
	return strings.Join(lines, "\n")
}

// omniHintRows is the height of the dropdown-or-hint block render() draws under the
// filter input while filtering, so contentHeight can reserve it.
func (m Model) omniHintRows() int {
	if !m.filtering {
		return 0
	}
	if dd := m.omniSuggestDropdown(); dd != "" {
		return lipgloss.Height(dd)
	}
	if m.mode == "pr" {
		return 1 // the "@user · is: · text" hint line
	}
	return 0
}

// prKey scopes the cached PR list by repo — the shared cache file holds every
// repo's lists, and a filter like "is:open author:@me" is identical across them,
// so without the repo they collide and bleed between repos.
func prKey(repo, filter string, limit int) string {
	return cache.Key("pr", repo+"\x00"+filter, limit, schemaVer)
}

// cachedPRs returns the cached PR list for a filter, if present and parseable.
func (m *Model) cachedPRs(filter string, limit int) ([]gh.PR, bool) {
	e, ok := m.cache.Get(prKey(m.repo, filter, limit))
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

// issueSchemaVer is bumped whenever issueFields changes shape.
const issueSchemaVer = "v1"

// issueKey scopes the cached issue list by repo, kind-prefixed "issue" so it can
// never collide with the "pr" list cache for the same filter.
func issueKey(repo, filter string) string {
	return cache.Key("issue", repo+"\x00"+filter, defaultLimit, issueSchemaVer)
}

func (m *Model) cachedIssues(filter string) ([]gh.Issue, bool) {
	e, ok := m.cache.Get(issueKey(m.repo, filter))
	if !ok {
		return nil, false
	}
	var is []gh.Issue
	if err := json.Unmarshal(e.Rows, &is); err != nil {
		slog.Debug("issue cache unmarshal failed", "err", err)
		return nil, false
	}
	return is, true
}

// hydrate paints rows for the current view from the cache, reporting whether it
// hit. The mine view combines the two cached searches into its sections.
func (m *Model) hydrate() bool {
	if m.cache == nil {
		return false
	}
	if m.mode == "issue" {
		is, ok := m.cachedIssues(m.filter)
		if !ok {
			return false
		}
		m.setIssues(is)
		m.hydrateIssueDetail()
		return true
	}
	if m.sectionsDefault() {
		rev, ok1 := m.cachedPRs(searchFor("pr", m.state, reviewBody), defaultLimit)
		open, ok2 := m.cachedPRs("is:open", openListLimit)
		if !ok1 && !ok2 {
			return false
		}
		m.setSections(rev, open, m.viewerLogin)
		m.hydrateDetail()
		return true
	}
	prs, ok := m.cachedPRs(m.filter, defaultLimit)
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

// hydrateIssueDetail paints each shown issue's body from the disk cache so the
// preview never opens on a bare Loading… (leaves it non-fresh, so the live
// fetch still revalidates).
func (m *Model) hydrateIssueDetail() {
	if m.cache == nil {
		return
	}
	is, ok := m.section.(*IssueSection)
	if !ok {
		return
	}
	for i := 0; i < is.Len(); i++ {
		num := is.issueAt(i).Number
		if _, ok := m.issueDetail[num]; ok {
			continue
		}
		e, hit := m.cache.Get(issueDetailKey(m.repo, num))
		if !hit {
			continue
		}
		var d gh.IssueDetail
		if err := json.Unmarshal(e.Rows, &d); err != nil {
			slog.Debug("issue detail cache unmarshal failed", "err", err)
			continue
		}
		m.issueDetail[num] = d
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

const viewerSchemaVer = "v1"

// viewerKey scopes the cached viewer login globally: `gh api user` returns the
// same login for every repo on a host, so it is neither repo- nor limit-scoped.
func viewerKey() string { return cache.Key("viewer", "", 0, viewerSchemaVer) }

// hydrateViewer paints the cached viewer login onto the model so the sections
// view can split Mine from Others without waiting on a live fetch.
func (m *Model) hydrateViewer() {
	if m.cache == nil {
		return
	}
	e, ok := m.cache.Get(viewerKey())
	if !ok {
		return
	}
	var login string
	if err := json.Unmarshal(e.Rows, &login); err != nil {
		slog.Debug("viewer cache unmarshal failed", "err", err)
		return
	}
	m.viewerLogin = login
}

func (m *Model) Hydrate() {
	m.hydrateViewer() // must precede hydrate(): setSections partitions Mine/Others by viewerLogin
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

// issueFetchCmd runs `gh issue list` for filter (gh excludes PRs by default).
func (m Model) issueFetchCmd(filter string) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.IssueListArgs(filter, defaultLimit)...)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		is, err := gh.ParseIssues(raw)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		return issuesFetchedMsg{filter: filter, issues: is, raw: raw}
	}
}

// sectionsFetchCmd fetches both halves of the empty-default open view — the
// review-requested search and the wider is:open list — caching each under its
// own filter+limit key. Sequential (not parallel): two quick gh calls.
func (m Model) sectionsFetchCmd() tea.Cmd {
	r, dir := m.runner, m.dir
	state := m.state
	reviewF := searchFor("pr", state, reviewBody)
	return func() tea.Msg {
		revRaw, err := r.Run(dir, gh.PRListArgs(reviewF, defaultLimit)...)
		if err != nil {
			return fetchFailedMsg{err: err, filter: reviewF}
		}
		rev, err := gh.ParsePRs(revRaw)
		if err != nil {
			return fetchFailedMsg{err: err, filter: reviewF}
		}
		openRaw, err := r.Run(dir, gh.PRListArgs("is:open", openListLimit)...)
		if err != nil {
			return fetchFailedMsg{err: err, filter: "is:open"}
		}
		open, err := gh.ParsePRs(openRaw)
		if err != nil {
			return fetchFailedMsg{err: err, filter: "is:open"}
		}
		return sectionsFetchedMsg{state: state, review: rev, reviewRaw: revRaw, open: open, openRaw: openRaw}
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

const pollInterval = 30 * time.Second

// launchFreshTTL bounds how recently a cached fetch must have been written for a
// cold launch to reuse it instead of re-hitting the API. Relaunching within this
// window (e.g. spamming the tmux popup) costs zero GraphQL calls; ctrl+r and the
// live-checks poll always force a real refresh regardless.
const launchFreshTTL = 60 * time.Second

func (m Model) cacheFresh(key string) bool {
	return m.cache != nil && m.cache.Fresh(key, launchFreshTTL)
}

func checksPollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return checksPollMsg{} })
}

// InitTheme reads the system theme mode, applies the matching palette, and seeds
// the watch mtime. Called from main before the program starts, so the first frame
// paints in the right palette. NOT called from NewModel, so tests keep the default
// Mocha globals regardless of the machine's live theme.
func (m *Model) InitTheme() {
	m.themeMode = detectTheme()
	applyTheme(themeFor(m.themeMode))
	preview.SetMode(m.themeMode)
	m.themeModTime, _ = statModTime(themeStatePath())
}

// themePollMsg fires the theme-watch beat. lastMod is the state-file mtime seen
// when the tick was armed, so the handler skips the read when nothing changed.
type themePollMsg struct{ lastMod time.Time }

// themeWatchTick re-arms ~every second. Unlike the other ticks it runs for the
// program's lifetime — the system theme can change at any time.
func themeWatchTick(lastMod time.Time) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return themePollMsg{lastMod: lastMod}
	})
}

// anyChecksRunning reports whether any shown PR row has an in-flight check.
// It scans individual checks rather than PR.CIState(), which collapses to
// "fail" when any check failed and would hide checks still running behind it.
func (m Model) anyChecksRunning() bool {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return false
	}
	for i := 0; i < ps.Len(); i++ {
		for _, c := range ps.prAt(i).Checks() {
			if c.Result() == "pending" {
				return true
			}
		}
	}
	return false
}

// pollBusy reports whether a user interaction or an in-flight fetch should defer
// this poll beat, so the background refresh never reorders rows under the user.
func (m Model) pollBusy() bool {
	return m.refreshing || m.filtering || m.showPicker || m.pending != nil || m.actionRunning()
}

// maybeStartPoll kicks the poll loop when a fetch reveals running checks, unless
// it is already running (one loop only, like the spinner).
func (m *Model) maybeStartPoll() tea.Cmd {
	if m.polling || !m.anyChecksRunning() {
		return nil
	}
	m.polling = true
	return checksPollTick()
}

// backgroundRefresh silently reconciles the current view without clearing rows —
// the same fetch path as a filter switch, minus the row reset.
func (m *Model) backgroundRefresh() tea.Cmd {
	m.refreshing = true
	fetch := m.fetchCmd(m.filter)
	if m.sectionsDefault() {
		fetch = m.sectionsFetchCmd()
	}
	return tea.Batch(fetch, m.startSpinner())
}

// switchToFilter repoints the model at m.filter: it paints cached rows instantly
// when the preset is warm (else clears stale rows), flags a refresh, and returns
// the live fetch to reconcile.
func (m *Model) switchToFilter() tea.Cmd {
	m.cursor = 0
	m.sel.clear()
	m.emptyNotice = ""
	m.refreshing = true
	hit := m.hydrate()
	m.loaded = hit // warm cache shows data/empty-state; a miss shows Loading…
	if m.mode == "issue" {
		if !hit {
			m.setIssues(nil)
		}
		return tea.Batch(m.issueFetchCmd(m.filter), m.startSpinner())
	}
	if !hit {
		if m.sectionsDefault() {
			m.setSections(nil, nil, m.viewerLogin) // drop stale rows while the fetch is in flight
		} else {
			m.setPRs(nil) // drop the previous preset's rows while the fetch is in flight
		}
	}
	fetch := m.fetchCmd(m.filter)
	if m.sectionsDefault() {
		fetch = m.sectionsFetchCmd()
	}
	return tea.Batch(fetch, m.startSpinner())
}

// toggleMode flips the board between PRs and issues: it saves the active board's
// selection, restores the other's, swaps the section + action set, resets all
// per-item/preview view state, and re-fetches (cached → instant).
func (m *Model) toggleMode() tea.Cmd {
	cur := boardView{state: m.state, body: m.body, filter: m.filter, presetIdx: m.presetIdx}
	m.state, m.body, m.filter, m.presetIdx = m.other.state, m.other.body, m.other.filter, m.other.presetIdx
	m.other = cur

	if m.mode == "pr" {
		m.mode = "issue"
		m.section = NewIssueSection(m.filter)
		m.actions = action.DefaultIssueActions()
	} else {
		m.mode = "pr"
		m.section = NewPRSection(m.filter)
		m.actions = action.DefaultPRActions()
	}

	// Reset view state so nothing from the other board leaks through.
	m.previewExpanded = false
	m.previewMax = false
	m.previewOffset = 0
	m.hideDrafts = false
	m.expanded = false
	m.err = nil
	m.detailSeq++ // cancel any in-flight detail debounce/fetch for the old board

	return m.switchToFilter() // resets cursor + selection, hydrates, fetches
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

func (m Model) fetchViewerCmd() tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		login, err := gh.FetchViewerLogin(r, dir)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return viewerFetchedMsg{login: login}
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
		m.body = strings.Join(terms, " ")
		m.filter = searchFor(m.mode, m.state, m.body)
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
	cmds := append([]tea.Cmd{spinnerTick(), themeWatchTick(m.themeModTime)}, m.launchFetchCmds()...)
	return tea.Batch(cmds...)
}

// launchFetchCmds returns the startup reconcile fetches — the sections default
// view plus the prewarmed issue board, member list, and viewer login — omitting
// any whose cache is still fresh. When the current view is reused, it emits
// fetchSkippedMsg so the refresh spinner still clears. Split out so the
// freshness gating is unit-testable without the ticker commands.
func (m Model) launchFetchCmds() []tea.Cmd {
	var cmds []tea.Cmd
	sectionsFresh := m.cacheFresh(prKey(m.repo, searchFor("pr", m.state, reviewBody), defaultLimit)) &&
		m.cacheFresh(prKey(m.repo, "is:open", openListLimit))
	if sectionsFresh {
		cmds = append(cmds, func() tea.Msg { return fetchSkippedMsg{} })
	} else {
		cmds = append(cmds, m.sectionsFetchCmd())
	}
	issueF := searchFor("issue", "open", assigneeBody)
	if !m.cacheFresh(issueKey(m.repo, issueF)) {
		cmds = append(cmds, m.issueFetchCmd(issueF))
	}
	if !m.cacheFresh(membersKey(m.repo)) {
		cmds = append(cmds, m.fetchMembersCmd())
	}
	if m.viewerLogin == "" {
		cmds = append(cmds, m.fetchViewerCmd())
	}
	return cmds
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
			m.cache.Set(prKey(m.repo, msg.filter, defaultLimit), msg.raw)
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
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd(), m.maybeStartPoll())
	case issuesFetchedMsg:
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(issueKey(m.repo, msg.filter), msg.raw)
		}
		if msg.filter != "" && msg.filter != m.filter {
			return m, nil // background prewarm of another issue filter
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear()
		m.setIssues(msg.issues)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		return m, m.detailCmdForCursor()
	case sectionsFetchedMsg:
		if m.cache != nil {
			m.cache.Set(prKey(m.repo, searchFor("pr", msg.state, reviewBody), defaultLimit), msg.reviewRaw)
			m.cache.Set(prKey(m.repo, "is:open", openListLimit), msg.openRaw)
		}
		if !m.sectionsDefault() || msg.state != m.state {
			return m, nil // a server qualifier became active, or state changed: cache only
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear()
		m.setSections(msg.review, msg.open, m.viewerLogin)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd(), m.maybeStartPoll())
	case fetchFailedMsg:
		if msg.filter != "" && msg.filter != m.filter {
			return m, nil // a background prewarm failed; the current view is unaffected
		}
		m.refreshing = false
		if gh.IssuesDisabled(msg.err) {
			// Not an error: this repo tracks issues elsewhere. Show an empty board.
			m.loaded = true
			m.emptyNotice = "Issues are disabled for this repository."
			m.renderList() // repaint the viewport; the m.err path skips it via board()
			return m, nil
		}
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
	case viewerFetchedMsg:
		m.viewerLogin = msg.login
		if m.cache != nil {
			if raw, err := json.Marshal(msg.login); err == nil {
				m.cache.Set(viewerKey(), raw)
			}
		}
		if m.sectionsDefault() {
			rev, _ := m.cachedPRs(searchFor("pr", m.state, reviewBody), defaultLimit)
			open, ok := m.cachedPRs("is:open", openListLimit)
			if ok {
				m.setSections(rev, open, m.viewerLogin)
			}
		}
		return m, nil
	case prDetailMsg:
		m.detail[msg.number] = msg.detail
		m.fresh[msg.number] = true
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(detailKey(m.repo, msg.number), msg.raw)
		}
		if m.expanded {
			m.reflowExpanded() // fold in the fresh detail without losing the reader's place
		} else {
			m.renderList()
		}
		return m, nil
	case issueDetailMsg:
		m.issueDetail[msg.number] = msg.detail
		m.issueFresh[msg.number] = true
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(issueDetailKey(m.repo, msg.number), msg.raw)
		}
		m.renderList()
		return m, nil
	case detailDebounceMsg:
		if msg.seq != m.detailSeq {
			return m, nil
		}
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd())
	case omniDebounceMsg:
		if msg.seq != m.omniSeq || !m.filtering {
			return m, nil // superseded by a later keystroke, or already committed
		}
		return m, m.switchToFilter() // SWR: hydrate cached instant, fetch to reconcile
	case fetchSkippedMsg:
		// Current view served from a fresh cache: no fetch ran, so settle the
		// state the hydrated rows were painted under and warm detail/poll.
		m.refreshing = false
		m.loaded = true
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd(), m.maybeStartPoll())
	case spinnerTickMsg:
		if !m.refreshing && !m.actionRunning() {
			m.spinning = false // fetch/action settled; let the loop die
			return m, nil
		}
		m.spinning = true
		m.spinnerFrame++
		return m, spinnerTick()
	case checksPollMsg:
		if !m.anyChecksRunning() {
			m.polling = false
			return m, nil
		}
		if m.pollBusy() {
			return m, checksPollTick() // skip this beat, keep the loop alive
		}
		return m, tea.Batch(m.backgroundRefresh(), checksPollTick())
	case themePollMsg:
		mod, err := statModTime(themeStatePath())
		if err != nil || mod.Equal(msg.lastMod) {
			return m, themeWatchTick(msg.lastMod) // gone or unchanged: keep watching
		}
		m.themeModTime = mod
		if mode := detectTheme(); mode != m.themeMode {
			m.themeMode = mode
			applyTheme(themeFor(mode))
			preview.SetMode(mode)
			if m.expanded {
				m.reflowExpanded()
			} else {
				m.renderList()
			}
		}
		return m, themeWatchTick(mod)
	case actionDoneMsg:
		// Scope the error to the status line rather than m.err, which blanks the board.
		if m.actionStatus == nil {
			return m, clearStatusCmd()
		}
		m.actionStatus.settled = true
		m.actionStatus.err = msg.err
		if msg.ok != "" {
			m.actionStatus.ok = msg.ok
		}
		if msg.fail != "" {
			m.actionStatus.fail = msg.fail
		}
		cmds := []tea.Cmd{clearStatusCmd()}
		if msg.err == nil && m.actionStatus.refresh {
			for _, n := range m.actionStatus.nums {
				delete(m.fresh, n) // force the detail/summary to revalidate
			}
			cmds = append(cmds, m.backgroundRefresh())
		}
		return m, tea.Batch(cmds...)
	case actionClearMsg:
		if m.actionStatus != nil && m.actionStatus.settled {
			m.actionStatus = nil
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.expanded {
			m.reflowExpanded() // reflow to the new size, keep the reader's place
		} else {
			m.renderList()
		}
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
			if m.mode != "pr" {
				// Issue board: plain local fuzzy filter, untouched by the omni
				// server-qualifier machinery.
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
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.omniServer = ""
				m.omniSuggestCursor = 0
				m.filter = searchFor("pr", m.state, "")
				m.sel.clear()
				return m, m.switchToFilter() // restore the sections default
			case "tab":
				if sug := m.omniSuggestions(); len(sug) > 0 {
					m.completeOmniAt(sug[m.omniSuggestCursor].Login)
					m.omniSuggestCursor = 0
					m.applyFilter()
					return m, m.omniServerCmd()
				}
				return m, nil // no suggestion active: tab is unbound in omni mode
			case "enter":
				if sug := m.omniSuggestions(); len(sug) > 0 {
					m.completeOmniAt(sug[m.omniSuggestCursor].Login)
					m.omniSuggestCursor = 0
					m.applyFilter()
					return m, m.omniServerCmd()
				}
				m.filtering = false
				m.filterInput.Blur()
				if m.omniServer != "" {
					return m, m.switchToFilter() // committed a server query: reconcile now, in case the debounce never fired
				}
				return m, nil // bare-text/empty commit: rows already local, no refetch
			case "up", "down", "ctrl+n", "ctrl+p", "pgup", "pgdown":
				if sug := m.omniSuggestions(); len(sug) > 0 && (msg.String() == "up" || msg.String() == "down") {
					m.omniSuggestCursor = max(0, min(m.omniSuggestCursor+cursorDelta(msg.String()), min(len(sug), omniSuggestDropdownRows)-1))
					return m, nil
				}
				m.moveCursor(cursorDelta(msg.String())) // pass through to the list
				m.detailSeq++
				return m, m.debounceDetailCmd()
			case "backspace":
				if m.filterInput.Value() == "" {
					m.filtering = false
					m.filterInput.Blur()
					return m, nil
				}
			}
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.omniSuggestCursor = 0 // the edit reshapes the @-partial; re-narrow from the top
			m.sel.clear()
			m.applyFilter() // bare text: instant, local
			return m, tea.Batch(cmd, m.omniServerCmd())
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
			if m.mode != "issue" {
				return m, nil // PR board: filtering is via / (omni); f is retired
			}
			// presetIdx is -1 for a custom (author) filter; max(...,0) makes f resume from "mine".
			ps := presetsFor(m.mode)
			m.presetIdx = nextPreset(max(m.presetIdx, 0), ps)
			m.body = ps[m.presetIdx].search
			m.filter = searchFor(m.mode, m.state, m.body)
			return m, m.switchToFilter()
		case "s":
			m.state = nextState(m.state, statesFor(m.mode))
			body := m.body
			if m.mode == "pr" && m.omniServer != "" {
				body = m.omniServer // a committed omni qualifier lives here, not in m.body
			}
			m.filter = searchFor(m.mode, m.state, body)
			return m, m.switchToFilter()
		case "tab":
			return m, m.toggleMode()
		case "ctrl+r":
			return m, m.backgroundRefresh()
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
			if m.mode != "pr" {
				return m, nil
			}
			m.hideDrafts = !m.hideDrafts
			if ps, ok := m.section.(*PRSection); ok {
				ps.SetHideDrafts(m.hideDrafts)
			}
			m.sel.clear() // the shown set changes; stale indexes would point elsewhere
			m.applyFilter()
			return m, nil
		case "R":
			if m.mode != "pr" {
				return m, nil
			}
			if _, ok := m.cursorVars(); ok {
				return m, m.openPicker("reviewer")
			}
			return m, nil
		case "/":
			m.filtering = true
			cmds := []tea.Cmd{m.filterInput.Focus()}
			if m.mode == "pr" && m.members == nil {
				cmds = append(cmds, m.fetchMembersCmd())
			}
			return m, tea.Batch(cmds...)
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
		case "p":
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
			if m.mode != "pr" {
				return m, nil // expanded view is PR-only in v1
			}
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
	if m.filtering {
		out := m.header() + "\n" + m.filterInput.View()
		switch dd := m.omniSuggestDropdown(); {
		case dd != "":
			out += "\n" + dd
		case m.mode == "pr":
			out += "\n" + dimStyle.Render(truncate("@user · is: · text", max(1, m.width)))
		}
		return out + "\n" + m.renderMain()
	}
	// Overlays float over the live board so the layout stays put behind them.
	board := m.board()
	switch {
	case m.pending != nil:
		return overlayTop(board, m.confirmPanel(), m.width, m.height)
	case m.showPicker:
		return overlayTop(board, m.pickerView(), m.width, m.height)
	case m.showLegend:
		return overlayTop(board, m.legendView(), m.width, m.height)
	case m.showActions:
		return overlayTop(board, m.actionsPanel(), m.width, m.height)
	}
	return board
}

// board renders the full PR board — the base layer under any overlay. The
// empty/loading state paints inside the boxed chrome (via the list viewport)
// so the layout stays solid while a fetch is in flight instead of collapsing
// to a bare line.
func (m Model) board() string {
	if m.err != nil && m.section.Len() == 0 {
		return m.header() + "\n\n" + failStyle.Render("  Error: "+m.err.Error()) + "\n" + m.statusBar()
	}
	l := computeLayout(m.width, m.height)
	if m.previewMax {
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

// confirmPanel is the y/n dialog for a pending action.
func (m Model) confirmPanel() string {
	q := ""
	if m.pending.Scope == "per-selected" {
		q = fmt.Sprintf("%s for %d PRs?", m.pending.Label, len(m.selectedOrCursor()))
	} else {
		n := 0
		if v, ok := m.cursorVars(); ok {
			n = v.Number
		}
		q = fmt.Sprintf("%s #%d?", m.pending.Label, n)
	}
	hint := accentStyle.Render("y") + statusBarStyle.Render(" confirm   ") +
		accentStyle.Render("n") + statusBarStyle.Render(" cancel")
	body := titleStyle.Render(q) + "\n\n" + hint
	w := lipgloss.Width(q) + 6
	if w < 34 {
		w = 34
	}
	return titledBox(body, w, 5, "Confirm")
}

// actionsPanel is the floating action menu.
func (m Model) actionsPanel() string {
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
	return titledBox(strings.TrimRight(b.String(), "\n"), 40, len(m.actions)+3, "Actions")
}

// clearStatusCmd wipes a settled action badge after its dwell time.
func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return actionClearMsg{} })
}

// prGlyph / issueGlyph mark each board. Set to your Nerd Font's glyphs.
var (
	prGlyph    = "\uf407" // nerd: nf-oct-git_pull_request
	issueGlyph = "\uf41b" // nerd: nf-oct-issue_opened
)

// modeGlyph returns the board's marker glyph.
func modeGlyph(mode string) string {
	if mode == "issue" {
		return issueGlyph
	}
	return prGlyph
}

// accentFor is the per-board accent: mauve for PRs, teal for Issues. Used for the
// active header segment and the list/preview box titles so each board reads as a
// distinct color at a glance.
func accentFor(mode string) lipgloss.Style {
	if mode == "issue" {
		return issueAccentStyle
	}
	return accentStyle
}

// modeSegments renders the "󰓎 PRs │ 󰝖 Issues" board switch: each segment carries
// its board glyph, and the active one is lit in that board's accent color.
func modeSegments(active string) string {
	seg := func(name, mode string) string {
		label := modeGlyph(mode) + " " + name
		if mode == active {
			return accentFor(mode).Bold(true).Render(label)
		}
		return dimStyle.Render(label)
	}
	return seg("PRs", "pr") + dimStyle.Render(" │ ") + seg("Issues", "issue")
}

// header is the global top line: repo · board segments · (spinner) · (badge) ·
// (selection). The current view (preset/state/count) lives on the list title.
func (m Model) header() string {
	h := headerStyle.Render("  "+m.repo) + "  " + modeSegments(m.mode)
	if m.refreshing {
		spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		h += dimStyle.Render(" · ") + refreshStyle.Render(spin+" refreshing")
	}
	h += m.statusBadge()
	if n := m.sel.count(); n > 0 {
		h += "  " + selMarkStyle.Render(fmt.Sprintf("%d selected", n))
	}
	return h
}

// statusBadge renders the transient inline-action badge (spinner while running,
// ✓/✗ once settled), or "" when idle. Shared by the list header and the
// expanded view, which otherwise wouldn't surface a rerun's outcome.
func (m Model) statusBadge() string {
	s := m.actionStatus
	if s == nil {
		return ""
	}
	switch {
	case !s.settled:
		spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return "  " + runBadgeStyle.Render(spin+" "+s.run+"…")
	case s.err != nil:
		return "  " + failBadgeStyle.Render("✗ "+s.fail)
	default:
		return "  " + passBadgeStyle.Render("✓ "+s.ok)
	}
}

// titleGlyph is the list-title marker: the terminal-state glyph for merged/closed
// PRs, else the board glyph. Issues have no merged state, so they always use theirs.
func (m Model) titleGlyph() string {
	if m.mode == "issue" {
		return issueGlyph
	}
	switch m.state {
	case "merged":
		return mergedGlyph
	case "closed":
		return closedGlyph
	default:
		return prGlyph
	}
}

// listTitle is the list pane's border title — the current view: state glyph +
// preset (or custom author body, or the active omni query) + state + shown count.
func (m Model) listTitle() string {
	label := m.body
	if m.mode == "issue" && m.presetIdx >= 0 {
		label = presetsFor(m.mode)[m.presetIdx].name
	} else if m.mode == "pr" {
		if m.omniServer != "" {
			label = m.omniServer
		} else {
			label = "all"
		}
	}
	return fmt.Sprintf("%s %s · %s · %d", m.titleGlyph(), label, m.state, m.section.Len())
}

// sectionsDefault reports whether the board is the empty-default open PR view —
// the sole state that shows the Review/Mine/Others sections. Any active server
// qualifier or a non-open state drops to the flat setPRs path.
func (m Model) sectionsDefault() bool {
	return m.mode == "pr" && m.state == "open" && m.omniServer == ""
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

// legendView is the ?-toggled glyph + key reference, as a centered modal. It
// lists every board-view key; expanded-view keys live in that view's own footer.
func (m Model) legendView() string {
	key := func(k, label string) string {
		return accentStyle.Render(k) + statusBarStyle.Render(" "+label)
	}
	row := func(items ...string) string { return strings.Join(items, statusBarStyle.Render("   ")) }

	rows := []string{
		accentStyle.Render("CI / review") + statusBarStyle.Render("  ✓ pass   ✗ fail   ● running   · none"),
		accentStyle.Render("state") + statusBarStyle.Render("       "+mergedGlyph+" merged   "+closedGlyph+" closed"),
		accentStyle.Render("!") + statusBarStyle.Render("           ⚠ conflict / behind base"),
		accentStyle.Render("row") + statusBarStyle.Render("         ▎ focus   ● selected   [draft] dimmed"),
		"",
	}

	nav := []string{key("↑↓/jk", "move")}
	if m.mode == "pr" {
		nav = append(nav, key("→/l", "expand"))
	}
	nav = append(nav, key("⇥", "PRs/Issues"))

	filters := []string{key("/", "filter (@user, is:, text)"), key("s", "state")}
	if m.mode == "pr" {
		filters = append(filters, key("R", "reviewers"), key("D", "drafts"))
	}

	preview := []string{}
	if m.mode == "pr" {
		preview = append(preview, key("p", "all comments")) // only the PR preview renders the timeline p unfolds
	}
	preview = append(preview, key("z", "maximize"), key("ctrl+j/k", "scroll"))

	rows = append(rows,
		row(nav...),
		row(preview...),
		row(key("space", "select"), key("V", "all")),
		row(filters...),
		row(key("a", "actions"), key("ctrl+r", "refresh"), key("?", "legend"), key("q", "quit")),
		"",
		row(key("↵", "worktree"), key("W", "bulk"), key("y", "#"), key("Y", "url"), key("b", "branch"), key("o", "open")),
	)
	if m.mode == "pr" {
		rows = append(rows, row(key("m", "merge"), key("r", "rerun"), key("u", "update"), key("M", "ready")))
	}
	body := strings.Join(rows, "\n")
	return titledBox(body, lipgloss.Width(body)+4, len(rows)+2, "Legend")
}

// actionOrder is the display order for the docked panel's actions section, so
// it doesn't jump around with Go's random map iteration.
var actionOrder = []string{"enter", "m", "A", "r", "u", "M", "W", "y", "Y", "b", "o"}

type keyHint struct{ key, label string }

// navHintsFor is the docked-panel cheatsheet for the active board. Issue mode
// drops the PR-only author/reviewer/drafts hints; both modes show the tab-toggle.
func navHintsFor(mode string) []keyHint {
	base := []keyHint{
		{"↑↓", "move"}, {"⇥", "PRs/Issues"}, {"s", "state"},
		{"/", "find"}, {"space", "select"}, {"V", "all"}, {"q", "quit"},
	}
	if mode == "pr" {
		pr := []keyHint{
			{"→", "expand"}, {"z", "max"}, {"ctrl+j/k", "scroll"},
			{"R", "reviewers"}, {"D", "drafts"},
		}
		return append(base, pr...)
	}
	return base
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
func panelBody(innerW int, keyHints []keyHint, actionsLabel string, acts []keyHint) string {
	lw, rw := panelSplit(innerW)
	left := panelColumn("keys", keyHints, lw, false)
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
// Reserved against the full action set (PR mode, the superset of nav hints) so
// the height is stable when batch mode hides the single-only actions, and
// doesn't jump when switching to issue mode's shorter hint list.
func panelContentRows(innerW int) int {
	lw, rw := panelSplit(innerW)
	return max(1+len(gridHints(navHintsFor("pr"), lw, false)), 1+len(gridHints(defaultActionHints(), rw, true)))
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
	return titledBox(panelBody(w-2, navHintsFor(m.mode), label, acts), w, panelRowsFor(w-2), "help")
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
	parts = append(parts,
		hint("↵", "worktree"), hint("a", "actions"), hint("⇥", "PRs/Issues"),
	)
	if m.mode == "pr" {
		parts = append(parts, hint("→", "expand"))
		if computeLayout(m.width, m.height).ShowSide {
			parts = append(parts, hint("p", "all comments")) // only unfolds the side preview's timeline
		}
	}
	parts = append(parts,
		hint("f", "filter"), hint("/", "find"), hint("space", "select"),
	)
	if m.mode == "pr" {
		drafts := draftTagStyle.Render("drafts") // peach while drafts are on the board
		if m.hideDrafts {
			drafts = statusBarStyle.Render("drafts") // dimmed once they're hidden
		}
		parts = append(parts, accentStyle.Render("D")+statusBarStyle.Render(":")+drafts)
	}
	parts = append(parts, hint("q", "quit"))
	rule := sepStyle.Render(strings.Repeat("─", max(m.width, 1)))
	return rule + "\n  " + strings.Join(parts, "  ")
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v3"

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20

// openListLimit is the tail depth for the empty-default open list; the 3-section
// partition needs more than the focused review/terminal boards' defaultLimit.
const openListLimit = 100
