package ui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
	prSource          gh.PRSource          // PR-list backend (githubv4)
	detailSource      gh.DetailSource      // batched per-PR detail backend
	checksSource      gh.ChecksSource      // rollup-only backend for the live-checks poll
	issueSource       gh.IssueSource       // issue-list backend
	issueDetailSource gh.IssueDetailSource // per-issue detail backend
	viewerSource      gh.ViewerSource      // viewer-login backend
	membersSource     gh.MembersSource     // assignable-users backend
	mutationSource    gh.MutationSource    // PR-mutation backend (merge/ready/etc.)
	actionsSource     gh.ActionsSource     // Actions rerun/job-log backend
	rateSource        gh.RateSource        // API rate-limit budgets, scraped off responses the other backends make
	rate              gh.RateSnapshot      // last sampled budget; the zero value is "nothing observed yet"
	rowText           []string             // renderList per-row cache: rendered string per shown index
	rowSig            []rowKey             // the inputs each rowText was rendered under; a miss re-renders that row
	rowGen            int                  // bumped whenever the shown set/content changes (applyFilter), invalidating rowText
	vp                viewport.Model
	cursor            int // indexes the section's shown set
	cursorLine        int // display-line offset of the cursor row (headers shift it)
	cursorRows        int // display height of the cursor row
	cursorTop         int // topmost line to keep visible for the cursor (its group header, if any)
	previewOffset     int // alt+j/k scroll position within the side preview
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
	legendQuery       string // live substring filter typed while the legend overlay is open
	actionFilter      textinput.Model
	actionCursor      int
	sel               selection
	detail            map[int]gh.PRDetail // painted detail (fresh this session or hydrated from disk)
	fresh             map[int]bool        // PR numbers whose detail was refetched this session; gates revalidation
	reviewRequested   map[int]bool        // PR numbers in the latest review-requested half; gates the ◐ marker off on re-request
	reviewedSet       map[int]bool        // PR numbers in the latest reviewed-by-me half; the ◐ marker's candidates
	ciRerun           map[int]time.Time   // PR number → stamp time when its checks-in-progress override was applied
	mergedSticky      map[int]gh.PR       // PRs prdash merged this session, kept on the open board until ctrl+r
	detailSeq         int                 // bumped on cursor move; gates the debounced detail fetch
	previewExpanded   bool
	previewN          int
	expanded          bool
	expandedTab       int
	checkCursor       int    // hovered check on the expanded Checks tab
	logView           bool   // the check-log sub-view is active (over the Checks tab)
	logJobID          string // Actions job ID whose log is shown
	logLabel          string // hovered check's label, for the log box title
	logShowAll        bool   // full job log vs failed-steps-only
	logLoading        bool   // a log fetch is in flight
	logErr            error  // last log fetch error
	logSteps          []logStep
	logLines          []logLine
	logCursor         int                  // line cursor within logLines
	logStyled         []string             // renderLogBody cache: styled line (no gutter) per logLines index
	logStyledW        int                  // width logStyled was built at; a change rebuilds it
	logCache          map[string][]logStep // keyed by logCacheKey(job, all)
	loaded            bool                 // first live fetch has returned; distinguishes empty from loading
	emptyNotice       string               // overrides the empty-board hint (e.g. issues disabled on this repo)
	refreshing        bool                 // a list fetch for the current filter is in flight
	spinning          bool                 // the refresh spinner tick loop is running
	spinnerFrame      int                  // advancing index into spinnerFrames
	polling           bool                 // the live-checks poll tick loop is running
	actionStatus      *actionStat          // transient inline-action progress shown by the header
	presetIdx         int                  // index into defaultPresets; -1 when filter is a custom (author) query
	previewMax        bool                 // z: preview takes full width, list hidden
	hideDrafts        bool                 // D: exclude draft PRs from the board
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
		reviewRequested: map[int]bool{}, reviewedSet: map[int]bool{},
		ciRerun: map[int]time.Time{}, mergedSticky: map[int]gh.PR{},
		issueDetail: map[int]gh.IssueDetail{}, issueFresh: map[int]bool{},
		previewN:  2,
		logCache:  map[string][]logStep{},
		presetIdx: -1, refreshing: true, // the PR board has no presets; sections replace them
	}
}

// SetPRSource installs the PR-list backend (githubv4).
func (m *Model) SetPRSource(s gh.PRSource) { m.prSource = s }

// SetDetailSource installs the batched per-PR detail backend: the
// refresh/prefetch path fetches the whole visible window in one request.
func (m *Model) SetDetailSource(s gh.DetailSource) { m.detailSource = s }

// SetChecksSource installs the rollup-only backend the live-checks poll uses
// instead of refetching the whole list.
func (m *Model) SetChecksSource(s gh.ChecksSource) { m.checksSource = s }

// SetIssueSource installs the issue-list backend (githubv4).
func (m *Model) SetIssueSource(s gh.IssueSource) { m.issueSource = s }

// SetIssueDetailSource installs the per-issue detail backend (githubv4).
func (m *Model) SetIssueDetailSource(s gh.IssueDetailSource) { m.issueDetailSource = s }

// SetViewerSource installs the viewer-login backend (githubv4).
func (m *Model) SetViewerSource(s gh.ViewerSource) { m.viewerSource = s }

// SetMembersSource installs the assignable-users backend (githubv4).
func (m *Model) SetMembersSource(s gh.MembersSource) { m.membersSource = s }

// SetMutationSource installs the githubv4 backend for PR mutations (merge,
// auto-merge, mark-ready, update-branch, request-reviewers) and the --web
// open-in-browser action.
func (m *Model) SetMutationSource(s gh.MutationSource) { m.mutationSource = s }

// SetActionsSource installs the REST backend for Actions rerun/job-log
// operations (internal/action.RerunFailed/RerunCheck/JobLog).
func (m *Model) SetActionsSource(s gh.ActionsSource) { m.actionsSource = s }

// SetRateSource installs the backend the header's API-budget segment samples.
func (m *Model) SetRateSource(s gh.RateSource) { m.rateSource = s }

func (m *Model) SetRepo(repo string) { m.repo = repo }

// ciRerunWindow is how long a PR keeps its optimistic checks-in-progress state
// after an update-branch or rerun. Long enough for GitHub to queue the new runs,
// short enough that a PR whose workflows never re-fire (path filters, no push
// trigger) self-corrects instead of showing a permanent phantom spinner.
const ciRerunWindow = 2 * time.Minute

// ciRerunRecheck is how long to wait before the follow-up refetch — long enough
// for GitHub to have queued the re-triggered runs, well inside ciRerunWindow so
// the optimistic state is replaced by real data rather than expiring into a
// stale one.
const ciRerunRecheck = 12 * time.Second

func delayedRefreshCmd() tea.Cmd {
	return tea.Tick(ciRerunRecheck, func(time.Time) tea.Msg { return delayedRefreshMsg{} })
}

// applyCIRerun repaints the checks of PRs that just had them re-triggered as
// in-progress. GitHub keeps serving the pre-push rollup for several seconds
// after update-branch, so without this the row shows a stale ✓ for a branch
// whose checks are about to start over. Both board funnels (setPRs,
// setSections) run PRs through it — whether freshly fetched or hydrated from
// the on-disk cache — so rows, preview, expanded, triage and filter all read
// one consistent CI state.
//
// You press r/u precisely when a check has failed, usually with a sibling
// check still pending from before the rerun — so an entry only clears once a
// check has demonstrably started AFTER the stamp (real new runs have
// appeared) or the window expires. Clearing on any pending check, as before,
// made the override no-op in exactly the case it exists for.
//
// applyOptimisticAction paints mutation results onto the in-memory board as
// soon as the request succeeds, so glyphs do not wait on backgroundRefresh.
func (m *Model) applyOptimisticAction() {
	if m.actionStatus == nil {
		return
	}
	ps, ok := m.section.(*PRSection)
	if !ok {
		return
	}
	switch m.actionStatus.native {
	case "auto-merge-squash":
		for _, n := range m.actionStatus.nums {
			ps.updatePR(n, func(p *gh.PR) {
				p.AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
			})
		}
	case "approve":
		for _, n := range m.actionStatus.nums {
			ps.updatePR(n, func(p *gh.PR) {
				p.ReviewDecision = "APPROVED"
			})
			if m.viewerLogin == "" {
				continue
			}
			d, ok := m.detail[n]
			if !ok {
				continue
			}
			replaced := false
			for i := range d.LatestReviews {
				if d.LatestReviews[i].Author.Login == m.viewerLogin {
					d.LatestReviews[i].State = "APPROVED"
					replaced = true
					break
				}
			}
			if !replaced {
				var r gh.Review
				r.Author.Login = m.viewerLogin
				r.State = "APPROVED"
				d.LatestReviews = append(d.LatestReviews, r)
			}
			m.detail[n] = d
		}
	default:
		return
	}
	m.rowGen++
	m.renderList()
}

func (m *Model) applyCIRerun(prs []gh.PR) []gh.PR {
	now := time.Now()
	for n, stamp := range m.ciRerun {
		if now.Sub(stamp) > ciRerunWindow {
			delete(m.ciRerun, n) // PR left the board (or its own next call): sweep so the fast path below stays true
		}
	}
	if len(m.ciRerun) == 0 {
		return prs
	}
	out := make([]gh.PR, len(prs))
	copy(out, prs)
	for i, p := range out {
		stamp, stamped := m.ciRerun[p.Number]
		if !stamped {
			continue
		}
		if hasCheckStartedAfter(p, stamp) {
			delete(m.ciRerun, p.Number)
			continue
		}
		// Copy before writing: these PR values come from the shared cache.
		rollup := make([]gh.Check, len(p.StatusCheckRollup))
		copy(rollup, p.StatusCheckRollup)
		for j := range rollup {
			rollup[j].State, rollup[j].Conclusion = "IN_PROGRESS", ""
		}
		out[i].StatusCheckRollup = rollup
	}
	return out
}

// hasCheckStartedAfter reports whether the rollup contains a check that began
// after t, i.e. a genuinely new run rather than one already in flight before
// the rerun fired. StatusContext entries leave StartedAt empty (only CheckRun
// entries populate it) and are skipped, along with anything that fails to
// parse as the RFC3339 gh.Check.StartedAt promises.
func hasCheckStartedAfter(p gh.PR, t time.Time) bool {
	for _, c := range p.Checks() {
		if c.StartedAt == "" {
			continue
		}
		started, err := time.Parse(time.RFC3339, c.StartedAt)
		if err != nil {
			continue
		}
		if started.After(t) {
			return true
		}
	}
	return false
}

// applyMergedSticky appends the PRs prdash merged this session that the fetch no
// longer returns, so a landed PR doesn't vanish the instant the post-merge
// refetch lands. Only the open PR board needs the overlay: the merged board
// returns these PRs itself, and on the closed board (is:unmerged) a merged row
// would contradict the query.
func (m *Model) applyMergedSticky(prs []gh.PR) []gh.PR {
	if len(m.mergedSticky) == 0 || !m.openPRBoard() {
		return prs
	}
	have := make(map[int]bool, len(prs))
	for _, p := range prs {
		have[p.Number] = true
	}
	add := make([]gh.PR, 0, len(m.mergedSticky))
	for n, p := range m.mergedSticky {
		if !have[n] {
			add = append(add, p)
		}
	}
	if len(add) == 0 {
		return prs
	}
	// Map order is random; sort so equal-ranked landed rows don't shuffle between frames.
	slices.SortFunc(add, func(a, b gh.PR) int { return b.Number - a.Number })
	// Fresh slice: prs may share its backing array with the cache.
	return append(append(make([]gh.PR, 0, len(prs)+len(add)), prs...), add...)
}

// openPRBoard reports whether the view is the open PR list — the only board a
// landed PR is held on.
func (m Model) openPRBoard() bool { return m.mode == "pr" && m.state == "open" }

// isLanded reports whether this row is a PR prdash merged, held on the open board
// by applyMergedSticky. False on the merged board, where the same PR is just a
// normal result and needs no tag.
func (m *Model) isLanded(number int) bool {
	if !m.openPRBoard() {
		return false
	}
	_, ok := m.mergedSticky[number]
	return ok
}

func (m *Model) setPRs(prs []gh.PR) {
	prs = m.applyMergedSticky(m.applyCIRerun(prs))
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
// The reviewed half unions PRs back into Review requested that GitHub drops from
// review-requested:@me once the viewer submits a review; they keep their place
// and carry the ◐ marker (see commentedByMe) instead of sinking into Others.
func (m *Model) setSections(review, reviewed, open []gh.PR, viewer string) {
	// Landed PRs join the open half so they categorize by author like anything
	// else; a merged PR is no longer awaiting anyone's review.
	open = m.applyMergedSticky(open)
	m.reviewRequested = make(map[int]bool, len(review))
	m.reviewedSet = make(map[int]bool, len(reviewed))
	cats := make(map[int]string, len(open)+len(review)+len(reviewed))
	all := make([]gh.PR, 0, len(open)+len(review)+len(reviewed))
	for _, p := range review {
		m.reviewRequested[p.Number] = true
		cats[p.Number] = "Review requested"
		all = append(all, p)
	}
	for _, p := range reviewed {
		m.reviewedSet[p.Number] = true
		if _, dup := cats[p.Number]; dup {
			continue // re-requested after my review: the review-requested half wins
		}
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
	all = m.applyCIRerun(all)
	if s, ok := m.section.(*PRSection); ok {
		s.SetState(m.state)
		s.SetCategorized(all, cats, []string{"Review requested", "Mine", "Others"})
	}
	m.applyFilter()
	if n := m.section.Len(); m.cursor >= n {
		m.cursor = max(0, n-1)
	}
}

// commentedByMe reports whether the viewer's latest review on number is a
// comment — the ◐ marker's state. A PR back in the review-requested half was
// re-requested after that comment, so the marker clears: my input is stale and
// the row reads as an ordinary awaiting-review again. LatestReviews holds one
// entry per reviewer, so the first match is the viewer's latest.
func (m *Model) commentedByMe(number int) bool {
	if m.viewerLogin == "" || m.reviewRequested[number] || !m.reviewedSet[number] {
		return false
	}
	d, ok := m.detail[number]
	if !ok {
		return false
	}
	for _, r := range d.LatestReviews {
		if r.Author.Login == m.viewerLogin {
			return r.State == "COMMENTED"
		}
	}
	return false
}

// reviewedDetailCmd warms detail for the reviewed-by-me set in one batched
// request, so the ◐ marker lands a beat after the board paints rather than
// waiting for the cursor to visit each row.
func (m Model) reviewedDetailCmd() tea.Cmd {
	var nums []int
	for num := range m.reviewedSet {
		if m.reviewRequested[num] || m.fresh[num] {
			continue
		}
		if m.cacheFreshFor(detailKey(m.repo, num), pollIntervalCold) {
			continue
		}
		nums = append(nums, num)
	}
	return m.batchDetailCmd(nums)
}

// groupRange returns the inclusive [lo, hi] shown-index span of the cursor's
// group. When the board is ungrouped (or not a PR section), the whole shown
// set is one group.
func (m Model) groupRange() (lo, hi int) {
	n := m.section.Len()
	if n == 0 {
		return 0, -1
	}
	ps, ok := m.section.(*PRSection)
	if !ok || !ps.grouped {
		return 0, n - 1
	}
	cur := m.cursor
	if cur < 0 {
		cur = 0
	}
	if cur >= n {
		cur = n - 1
	}
	label := ps.groupLabel(cur)
	lo, hi = cur, cur
	for lo > 0 && ps.groupLabel(lo-1) == label {
		lo--
	}
	for hi+1 < n && ps.groupLabel(hi+1) == label {
		hi++
	}
	return lo, hi
}

// advanceSelection cycles multi-select: Group → All → None, derived from the
// current selection (no stored mode).
func (m *Model) advanceSelection() {
	n := m.section.Len()
	if n == 0 {
		return
	}
	lo, hi := m.groupRange()
	groupFull := true
	for i := lo; i <= hi; i++ {
		if !m.sel.has(i) {
			groupFull = false
			break
		}
	}
	if !groupFull {
		for i := lo; i <= hi; i++ {
			if !m.sel.has(i) {
				m.sel.toggle(i)
			}
		}
		return
	}
	allFull := m.sel.count() == n
	if !allFull {
		for i := 0; i < n; i++ {
			if !m.sel.has(i) {
				m.sel.toggle(i)
			}
		}
		return
	}
	m.sel.clear()
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

// rowKey is the set of inputs a cached row string was rendered under. When the
// live inputs still match, renderList reuses the cached string instead of
// re-styling the row — so a cursor move re-renders only the two rows whose focus
// flipped, not all of them. gen invalidates the whole cache on a content change.
type rowKey struct {
	gen, w, numW, diffW, tktW int
	focused, selected         bool
	flag                      string
	landed                    bool
	commented                 bool
	compactDiff, initials     bool
}

// renderList rebuilds the viewport content from the shown rows and scrolls so the cursor row is visible.
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	innerW := l.ListInner
	innerH := m.contentHeight(l) - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	numW := columnWidths(m.section)
	diffW := 0
	if l.ShowDiffstat {
		diffW = diffstatWidth(m.section, l.CompactDiffstat)
	}
	tktW := 0
	if l.ShowTicket {
		tktW = ticketWidth(m.section)
	}
	ps, isPR := m.section.(*PRSection)
	grouped := isPR && ps.grouped
	n := m.section.Len()
	if len(m.rowText) != n { // shown set resized (or first paint): reset the cache
		m.rowText = make([]string, n)
		m.rowSig = make([]rowKey, n)
	}
	var b strings.Builder
	line, prevGroup := 0, ""
	for i := 0; i < n; i++ {
		headerLine := -1 // this row's group header line, when it opens a new group
		if grouped {
			if g := ps.groupLabel(i); g != prevGroup {
				if prevGroup != "" { // blank line between groups, not above the first
					b.WriteString("\n")
					line++
				}
				headerLine = line
				b.WriteString(groupHeader(g, innerW) + "\n")
				line++
				prevGroup = g
			}
		}
		flag := ""
		if isPR && ps.prAt(i).State == "OPEN" {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
		landed := isPR && m.isLanded(ps.prAt(i).Number)
		commented := isPR && m.openPRBoard() && m.commentedByMe(ps.prAt(i).Number)
		key := rowKey{gen: m.rowGen, w: innerW, numW: numW, diffW: diffW, tktW: tktW, focused: i == m.cursor, selected: m.sel.has(i), flag: flag, landed: landed, commented: commented, compactDiff: l.CompactDiffstat, initials: l.InitialsAuthor}
		if m.rowSig[i] != key || m.rowText[i] == "" {
			m.rowText[i] = m.section.RenderRow(i, RowOpts{
				Width: innerW, NumWidth: numW, DiffWidth: diffW, TicketWidth: tktW, Focused: key.focused, Selected: key.selected, Flag: flag, Landed: landed, Commented: commented,
				CompactDiff: l.CompactDiffstat, Initials: l.InitialsAuthor,
			})
			m.rowSig[i] = key
		}
		row := m.rowText[i]
		rowH := strings.Count(row, "\n") + 1
		if i == m.cursor {
			m.cursorLine = line
			m.cursorRows = rowH
			// Reveal the group header sitting directly above the cursor row when it
			// leads a group, so scrolling up to it doesn't clip the header.
			m.cursorTop = line
			if headerLine >= 0 {
				m.cursorTop = headerLine
			}
		}
		b.WriteString(row)
		b.WriteString("\n")
		line += rowH
	}
	content := b.String()
	if m.section.Len() == 0 {
		m.cursorLine = 0
		m.cursorRows = 1
		m.cursorTop = 0
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
	rows := m.cursorRows
	if rows < 1 {
		rows = 1
	}
	top := m.cursorTop // reveal the group header above the row, when it has one
	bottom := m.cursorLine + rows - 1
	off := m.vp.YOffset()
	switch {
	case top < off:
		off = top
	case bottom >= off+m.vp.Height():
		off = bottom - m.vp.Height() + 1
	}
	if off < 0 {
		off = 0
	}
	m.vp.SetYOffset(off)
}

// repaintActive rebuilds the shared viewport for whichever view currently owns
// it. A background data update must repaint the log or expanded box it's under —
// not the list — or list rows bleed through into a non-list view.
func (m *Model) repaintActive() {
	switch {
	case m.logView:
		m.setLogContent()
	case m.expanded:
		m.reflowExpanded()
	default:
		m.renderList()
	}
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
	m.rowGen++ // the shown set/order/content changed; invalidate the per-row cache
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
	r := []rune(m.filterInput.Value())
	pos := min(m.filterInput.Position(), len(r))
	left := string(r[:pos])
	i := strings.LastIndexAny(left, " ")
	rewritten := left[:i+1] + "@" + login + string(r[pos:])
	m.filterInput.SetValue(rewritten)
	m.filterInput.SetCursor(i + 1 + len([]rune("@"+login)))
}

// omniSuggestDropdownRows caps how many members the @-mention dropdown lists
// at once; the fuzzy partial narrows the set to reach the rest.
const omniSuggestDropdownRows = 6

// omniSuggestDropdown renders the @-mention candidate list as a panel,
// highlighting m.omniSuggestCursor; "" when no suggestions are active. render()
// composites it over the board, so it displaces no rows.
func (m Model) omniSuggestDropdown() string {
	sug := m.omniSuggestions()
	if len(sug) == 0 {
		return ""
	}
	frame := 2 // the box's own edges, on both axes
	fit := max(1, m.height-m.omniDropdownY()-frame)
	n := min(len(sug), omniSuggestDropdownRows, fit)
	lines := make([]string, n)
	inner := 0
	for i, u := range sug[:n] {
		cur := "  "
		if i == m.omniSuggestCursor {
			cur = accentStyle.Render("▸ ")
		}
		lines[i] = cur + truncate("@"+u.Login, max(1, m.width-frame-2))
		inner = max(inner, lipgloss.Width(lines[i]))
	}
	// The title is the only place tab is named, so keep it whole: titledBox
	// reserves 4 cells around the label.
	title := "tab completes"
	w := min(max(inner+frame, lipgloss.Width(title)+4), max(4, m.width))
	return titledBox(strings.Join(lines, "\n"), w, n+frame, title)
}

// omniDropdownY is the row the @-mention panel floats at: directly under the
// filter bar's box (3 rows: top border, input, bottom border), not the input
// line itself — landing there would sit on top of the box's bottom border.
func (m Model) omniDropdownY() int {
	return lipgloss.Height(m.header()) + 3
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

// issueSchemaVer is bumped whenever the cached issue field set changes shape.
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
		revd, _ := m.cachedPRs(searchFor("pr", m.state, reviewedBody), defaultLimit)
		open, ok2 := m.cachedPRs("is:open", openListLimit)
		if !ok1 && !ok2 {
			return false
		}
		m.setSections(rev, revd, open, m.viewerLogin)
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

// fetchCmd fetches the PR list for filter through the PR source, tagging the
// result so a background prewarm of a non-current preset lands in the cache
// without repainting the view.
func (m Model) fetchCmd(filter string) tea.Cmd {
	src := m.prSource
	return func() tea.Msg {
		prs, raw, err := src.FetchPRs(filter, defaultLimit)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		return prsFetchedMsg{filter: filter, prs: prs, raw: raw}
	}
}

// issueFetchCmd fetches the issue list for filter through the issue source.
func (m Model) issueFetchCmd(filter string) tea.Cmd {
	src := m.issueSource
	return func() tea.Msg {
		is, raw, err := src.FetchIssues(filter, defaultLimit)
		if err != nil {
			return fetchFailedMsg{err: err, filter: filter}
		}
		return issuesFetchedMsg{filter: filter, issues: is, raw: raw}
	}
}

// sectionsFetchCmd fetches the thirds of the empty-default open view — the
// review-requested search, the reviewed-by-me search (PRs GitHub drops from the
// first search once the viewer submits a review), and the wider is:open list —
// caching each under its own filter+limit key. The fetches run concurrently:
// they're independent, so wall-clock is the slowest of them, not their sum.
func (m Model) sectionsFetchCmd() tea.Cmd {
	src := m.prSource
	state := m.state
	reviewF := searchFor("pr", state, reviewBody)
	reviewedF := searchFor("pr", state, reviewedBody)
	return func() tea.Msg {
		type half struct {
			prs []gh.PR
			raw []byte
			err error
		}
		var review, reviewed, open half
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			review.prs, review.raw, review.err = src.FetchPRs(reviewF, defaultLimit)
		}()
		go func() {
			defer wg.Done()
			reviewed.prs, reviewed.raw, reviewed.err = src.FetchPRs(reviewedF, defaultLimit)
		}()
		go func() {
			defer wg.Done()
			open.prs, open.raw, open.err = src.FetchPRs("is:open", openListLimit)
		}()
		wg.Wait()
		if review.err != nil {
			return fetchFailedMsg{err: review.err, filter: reviewF}
		}
		if reviewed.err != nil {
			return fetchFailedMsg{err: reviewed.err, filter: reviewedF}
		}
		if open.err != nil {
			return fetchFailedMsg{err: open.err, filter: "is:open"}
		}
		return sectionsFetchedMsg{state: state,
			review: review.prs, reviewRaw: review.raw,
			reviewed: reviewed.prs, reviewedRaw: reviewed.raw,
			open: open.prs, openRaw: open.raw}
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

// Poll beats are tiered by whose CI is running. A session waits on its own PRs
// and on whatever row it is sitting on; everything else can lag by minutes. The
// query is one point either way (one aliased request covers every running PR), so
// the tier buys budget by asking less often, not by asking for less.
const (
	pollIntervalHot  = 30 * time.Second
	pollIntervalCold = 2 * time.Minute
)

// launchFreshTTL bounds how recently a cached fetch must have been written for a
// cold launch to reuse it instead of re-hitting the API. Relaunching within this
// window (e.g. spamming the tmux popup) costs zero GraphQL calls; ctrl+r and the
// live-checks poll always force a real refresh regardless.
const launchFreshTTL = 60 * time.Second

func (m Model) cacheFresh(key string) bool {
	return m.cacheFreshFor(key, launchFreshTTL)
}

// cacheFreshFor is cacheFresh with an explicit window, for the callers that tier
// freshness by whose PR it is (see detailFreshTTL).
func (m Model) cacheFreshFor(key string, ttl time.Duration) bool {
	return m.cache != nil && m.cache.Fresh(key, ttl)
}

func checksPollTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return checksPollMsg{} })
}

// checksPollDelay spaces the next beat: hot while a running check belongs to the
// viewer or to the cursor row, cold when only other people's PRs are churning.
func (m Model) checksPollDelay() time.Duration {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return pollIntervalCold
	}
	cursorNum := -1
	if v, ok := m.cursorVars(); ok {
		cursorNum = v.Number
	}
	for _, i := range runningCheckRows(ps) {
		p := ps.prAt(i)
		if p.Number == cursorNum || (m.viewerLogin != "" && p.Author.Login == m.viewerLogin) {
			return pollIntervalHot
		}
	}
	return pollIntervalCold
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

// runningCheckRows returns the shown indexes whose PR has an in-flight check.
// It scans individual checks rather than PR.CIState(), which collapses to
// "fail" when any check failed and would hide checks still running behind it.
func runningCheckRows(ps *PRSection) []int {
	var out []int
	for i := 0; i < ps.Len(); i++ {
		for _, c := range ps.prAt(i).Checks() {
			if c.Result() == "pending" {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// anyChecksRunning reports whether any shown PR row has an in-flight check.
func (m Model) anyChecksRunning() bool {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return false
	}
	return len(runningCheckRows(ps)) > 0
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
	return checksPollTick(m.checksPollDelay())
}

// pollChecksCmd refetches the rollup for every row with an in-flight check, in
// one aliased request. Deliberately not backgroundRefresh: that refetches both
// list searches (5 points to this one's 1) and re-sorts the board, moving rows
// under the user on a beat they never asked for.
func (m Model) pollChecksCmd() tea.Cmd {
	ps, ok := m.section.(*PRSection)
	if !ok || m.checksSource == nil {
		return nil
	}
	rows := runningCheckRows(ps)
	if len(rows) == 0 {
		return nil
	}
	nums := make([]int, 0, len(rows))
	for _, i := range rows {
		nums = append(nums, ps.prAt(i).Number)
	}
	src := m.checksSource
	return func() tea.Msg {
		checks, err := src.FetchChecks(nums)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return checksFetchedMsg{checks: checks}
	}
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
			m.setSections(nil, nil, nil, m.viewerLogin) // drop stale rows while the fetch is in flight
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

// fetchMembersCmd fetches the assignable-users list through the members source.
func (m Model) fetchMembersCmd() tea.Cmd {
	src := m.membersSource
	return func() tea.Msg {
		users, raw, err := src.FetchAssignableUsers()
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return membersFetchedMsg{users: users, raw: raw}
	}
}

// fetchViewerCmd fetches the authenticated user's login through the viewer source.
func (m Model) fetchViewerCmd() tea.Cmd {
	src := m.viewerSource
	return func() tea.Msg {
		login, err := src.FetchViewer()
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
		return m.assignReviewersCmd(v.Number, v.ID, add, remove, checked)
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
		m.cacheFresh(prKey(m.repo, searchFor("pr", m.state, reviewedBody), defaultLimit)) &&
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
		m.repaintActive() // keep the log/expanded box painted; don't bleed list rows in
		return m, tea.Batch(m.warmDetailCmd(), m.maybeStartPoll())
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
		m.repaintActive()
		return m, m.detailCmdForCursor()
	case sectionsFetchedMsg:
		if m.cache != nil {
			m.cache.Set(prKey(m.repo, searchFor("pr", msg.state, reviewBody), defaultLimit), msg.reviewRaw)
			m.cache.Set(prKey(m.repo, searchFor("pr", msg.state, reviewedBody), defaultLimit), msg.reviewedRaw)
			m.cache.Set(prKey(m.repo, "is:open", openListLimit), msg.openRaw)
		}
		if !m.sectionsDefault() || msg.state != m.state {
			return m, nil // a server qualifier became active, or state changed: cache only
		}
		m.refreshing = false
		m.loaded = true
		m.sel.clear()
		m.setSections(msg.review, msg.reviewed, msg.open, m.viewerLogin)
		if m.expanded && m.section.Len() == 0 {
			m.expanded = false
		}
		m.repaintActive()
		return m, tea.Batch(m.warmDetailCmd(), m.reviewedDetailCmd(), m.maybeStartPoll())
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
			m.cache.Set(membersKey(m.repo), msg.raw)
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
			revd, _ := m.cachedPRs(searchFor("pr", m.state, reviewedBody), defaultLimit)
			open, ok := m.cachedPRs("is:open", openListLimit)
			if ok {
				m.setSections(rev, revd, open, m.viewerLogin)
			}
		}
		return m, nil
	case prDetailMsg:
		m.detail[msg.number] = msg.detail
		m.fresh[msg.number] = true
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(detailKey(m.repo, msg.number), msg.raw)
		}
		m.repaintActive() // fold the fresh detail into the active view without losing place
		return m, nil
	case detailsBatchMsg:
		for num, d := range msg.details {
			m.detail[num] = d
			m.fresh[num] = true
			if m.cache != nil {
				if raw := msg.raws[num]; raw != nil {
					m.cache.Set(detailKey(m.repo, num), raw)
				}
			}
		}
		m.repaintActive()
		return m, nil
	case logFetchedMsg:
		if !m.logView || msg.job != m.logJobID || msg.all != m.logShowAll {
			return m, nil // stale: view closed or variant switched
		}
		m.logLoading = false
		if msg.err != nil {
			m.logErr = msg.err
			m.setLogContent()
			return m, nil
		}
		m.logErr = nil
		steps := parseJobLog(msg.raw, !msg.all)
		m.logCache[logCacheKey(msg.job, msg.all)] = steps
		m.setLogSteps(steps)
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
		return m, m.warmDetailCmd()
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
		return m, tea.Batch(m.warmDetailCmd(), m.reviewedDetailCmd(), m.maybeStartPoll())
	case spinnerTickMsg:
		if !m.refreshing && !m.actionRunning() && !m.logLoading {
			m.spinning = false // fetch/action settled; let the loop die
			return m, nil
		}
		m.spinning = true
		m.spinnerFrame++
		if m.logView && m.logLoading {
			m.setLogContent() // advance the spinner frame in the log body
		}
		return m, spinnerTick()
	case checksPollMsg:
		if !m.anyChecksRunning() {
			m.polling = false
			return m, nil
		}
		if m.pollBusy() {
			return m, checksPollTick(m.checksPollDelay()) // skip this beat, keep the loop alive
		}
		return m, tea.Batch(m.pollChecksCmd(), checksPollTick(m.checksPollDelay()))
	case checksFetchedMsg:
		ps, ok := m.section.(*PRSection)
		if !ok {
			return m, nil
		}
		ps.ApplyChecks(msg.checks)
		m.repaintActive()
		// No reschedule: the tick armed alongside this fetch is still in flight, and
		// it retires the loop itself once nothing is pending.
		return m, nil
	case themePollMsg:
		// This is the only tick that runs for the whole session, so the header's
		// budget countdown rides it rather than arming a second one-second loop.
		if m.rateSource != nil {
			if s, ok := m.rateSource.RateLimit(); ok {
				m.rate = s
			}
		}
		mod, err := statModTime(themeStatePath())
		if err != nil || mod.Equal(msg.lastMod) {
			return m, themeWatchTick(msg.lastMod) // gone or unchanged: keep watching
		}
		m.themeModTime = mod
		if mode := detectTheme(); mode != m.themeMode {
			m.themeMode = mode
			applyTheme(themeFor(mode))
			preview.SetMode(mode)
			switch {
			case m.logView:
				m.setLogContent() // re-tint the log under the new theme
			case m.expanded:
				m.reflowExpanded()
			default:
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
		if msg.err == nil {
			landed := time.Now()
			for _, p := range m.actionStatus.merged {
				p.State, p.MergedAt = "MERGED", landed
				m.mergedSticky[p.Number] = p
				delete(m.ciRerun, p.Number) // a landed PR's checks are moot; keep applyCIRerun off its row
			}
			m.applyOptimisticAction()
		}
		if msg.err == nil && m.actionStatus.refresh {
			for _, n := range m.actionStatus.nums {
				delete(m.fresh, n) // force the detail/summary to revalidate
			}
			cmds = append(cmds, m.backgroundRefresh())
		}
		if msg.err == nil && m.actionStatus.rerunCI {
			stamp := time.Now()
			for _, n := range m.actionStatus.nums {
				m.ciRerun[n] = stamp
			}
			cmds = append(cmds, delayedRefreshCmd())
		}
		return m, tea.Batch(cmds...)
	case actionClearMsg:
		if m.actionStatus != nil && m.actionStatus.settled {
			m.actionStatus = nil
		}
		return m, nil
	case delayedRefreshMsg:
		return m, m.backgroundRefresh()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.repaintActive() // reflow whichever view owns the viewport to the new size
		return m, nil
	case tea.KeyMsg:
		if m.logView {
			return m.updateLogView(msg)
		}
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
				case "esc", "enter":
					m.filtering = false
					m.filterInput.Blur() // keep the query applied so actions work on the filtered set
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
				m.filterInput.Blur() // keep the query applied so actions work on the filtered set
				if m.omniServer != "" {
					return m, m.switchToFilter() // reconcile in case the debounce never fired
				}
				return m, nil
			case "tab":
				if sug := m.omniSuggestions(); len(sug) > 0 {
					m.completeOmniAt(sug[m.omniSuggestCursor].Login)
					m.omniSuggestCursor = 0
					m.applyFilter()
					return m, m.omniServerCmd()
				}
				return m, nil // no suggestion active: tab is unbound in omni mode
			case "enter":
				// Always commits, dropdown or not: a completed @login still matches
				// itself, so accepting a suggestion here would trap the user.
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
			switch msg.String() {
			case "esc", "?", "f1":
				m.showLegend = false
				m.legendQuery = ""
			case "backspace":
				if r := []rune(m.legendQuery); len(r) > 0 {
					m.legendQuery = string(r[:len(r)-1])
				}
			default:
				if s := msg.String(); len(s) == 1 {
					m.legendQuery += s
				}
			}
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
			// The one refresh the user asked for by name: landed rows go. Every other
			// caller of backgroundRefresh (post-action, CI poll) keeps them.
			clear(m.mergedSticky)
			return m, m.backgroundRefresh()
		case "z":
			m.previewMax = !m.previewMax
			return m, nil
		case "alt+j":
			m.previewScrollBy(1)
			return m, nil
		case "alt+k":
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
		case "?", "f1":
			m.showLegend = true
			return m, nil
		case "esc":
			if m.filterInput.Value() == "" {
				return m, tea.Quit
			}
			m.filterInput.SetValue("")
			m.sel.clear()
			if m.mode == "pr" && m.omniServer != "" {
				m.omniServer = ""
				m.omniSuggestCursor = 0
				m.filter = searchFor("pr", m.state, "")
				return m, m.switchToFilter() // restore the sections default
			}
			m.applyFilter()
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space":
			m.sel.toggle(m.cursor)
			m.renderList()
			return m, nil
		case "V":
			m.advanceSelection()
			m.renderList()
			return m, nil
		case "p":
			m.previewExpanded = !m.previewExpanded
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "down", "j", "ctrl+j":
			m.moveCursor(1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "up", "k", "ctrl+k":
			m.moveCursor(-1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "right", "l":
			if m.mode != "pr" {
				return m, nil // tabs/expanded view are PR-only in v1
			}
			if computeLayout(m.width, m.height).ShowSide {
				m.expandedTab = (m.expandedTab + 1) % len(expandedTabs)
				m.checkCursor = 0
				return m, nil
			}
			m.enterExpanded() // narrow: no pane, open full-screen tabs
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "left", "h":
			if m.mode != "pr" || !computeLayout(m.width, m.height).ShowSide {
				return m, nil
			}
			m.expandedTab = (m.expandedTab + len(expandedTabs) - 1) % len(expandedTabs)
			m.checkCursor = 0
			return m, nil
		case "1", "2", "3", "4", "5", "6":
			if m.mode != "pr" || !computeLayout(m.width, m.height).ShowSide {
				return m, nil
			}
			m.expandedTab = int(msg.String()[0] - '1')
			m.checkCursor = 0
			return m, nil
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
	if m.logView {
		base := m.logViewRender()
		if m.showLegend {
			return overlayTop(base, renderLegendGroups("Log keys", m.logLegendGroups(), m.width, m.height), m.width, m.height)
		}
		return base
	}
	if m.expanded {
		base := m.expandedView()
		if m.showLegend {
			return overlayTop(base, renderLegendGroups("Keys", m.expandedLegendGroups(), m.width, m.height), m.width, m.height)
		}
		return base
	}
	// Overlays float over the live board so the layout stays put behind them.
	board := m.board()
	if dd := m.omniSuggestDropdown(); dd != "" {
		return overlayAt(board, dd, 0, m.omniDropdownY(), m.width, m.height)
	}
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

// filterBar renders the omni-filter as a bordered box. It is three rows in every
// state — the primary surface should not change height as it gains and loses
// focus, and filterBarRows measures off this render so contentHeight follows.
func (m Model) filterBar() string {
	inner := max(1, m.width-4) // border (2) + one cell of padding each side

	var body string
	switch {
	case m.filtering:
		body = m.filterInput.View()
	case m.filterInput.Value() != "":
		body = accentStyle.Render(truncate(m.filterInput.Value(), inner)) +
			dimStyle.Render("  esc clears")
	default:
		body = dimStyle.Render("filter — @user · is: · text")
	}
	body = accentStyle.Render(filterGlyph) + " " + body

	// The match count is the lowest-priority element and drops first. Len() is
	// post-filter (SetShown narrows it) and Haystacks() covers the whole set, so
	// the pair is total→shown without storing anything on the model.
	if m.filterInput.Value() != "" {
		total, shown := len(m.section.Haystacks()), m.section.Len()
		count := dimStyle.Render(fmt.Sprintf("%d→%d", total, shown))
		if pad := inner - lipgloss.Width(body) - lipgloss.Width(count); pad > 0 {
			body += strings.Repeat(" ", pad) + count
		}
	}
	// body is already styled, so it must NOT go through truncate — that walks
	// runes and would slice an ANSI escape. lipgloss's Width + MaxWidth clamp
	// styled content safely.
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(m.width).
		MaxWidth(m.width).
		Padding(0, 1).
		Render(body)
}

// filterBarRows is the row-height of filterBar() in its current state, measured
// off the render so contentHeight can't drift from it.
func (m Model) filterBarRows() int {
	return lipgloss.Height(m.filterBar())
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
		return m.header() + "\n" + m.filterBar() + "\n" + m.renderMain() // zoom fills the frame; action folded into the title
	}
	if l.ShowSide && l.ShowPanel {
		return m.header() + "\n" + m.filterBar() + "\n" + m.renderDocked(l)
	}
	if !l.ShowFooter {
		// Small window: the footer's key hints are dropped (press ? for them), but
		// the filter bar stays — it's the primary surface, not chrome.
		return m.header() + "\n" + m.filterBar() + "\n" + m.renderMain()
	}
	foot := m.statusBar()
	if l.ShowPanel {
		foot = m.keysActionsPanel(m.width)
	}
	return m.header() + "\n" + m.filterBar() + "\n" + m.renderMain() + "\n" + foot
}

// confirmQuestion is the y/n prompt text for the pending action. A single target
// the viewer didn't author names its author (so an accidental keystroke on
// someone else's PR is obvious); a bulk fan-out shows the count.
func (m Model) confirmQuestion() string {
	a := m.pending
	if a.Scope != "per-selected" {
		n, branch := 0, ""
		if v, ok := m.cursorVars(); ok {
			n, branch = v.Number, v.HeadRefName
		}
		// A force-delete prompt has to say what it deletes; a PR number alone
		// doesn't identify the branch about to go.
		if a.Command.Builtin == "cleanup-branch" && branch != "" {
			return fmt.Sprintf("%s %s (#%d)?", a.Label, branch, n)
		}
		return fmt.Sprintf("%s #%d?", a.Label, n)
	}
	targets := m.selectedOrCursor()
	if len(targets) != 1 {
		return fmt.Sprintf("%s for %d PRs?", a.Label, len(targets))
	}
	i := targets[0]
	if i < 0 || i >= m.section.Len() {
		return fmt.Sprintf("%s?", a.Label)
	}
	v := m.section.VarsAt(i)
	if a.ConfirmOthers && v.Author != "" && v.Author != m.viewerLogin {
		return fmt.Sprintf("%s #%d by %s?", a.Label, v.Number, v.Author)
	}
	return fmt.Sprintf("%s #%d?", a.Label, v.Number)
}

// confirmPanel is the y/n dialog for a pending action.
func (m Model) confirmPanel() string {
	q := m.confirmQuestion()
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
	h += m.statusBadge(m.width - lipgloss.Width(h))
	if n := m.sel.count(); n > 0 {
		h += "  " + selMarkStyle.Render(fmt.Sprintf("%d selected", n))
	}
	// Last, right-aligned, and out of whatever the badge and selection count left
	// over: the API budget is the header's lowest-priority element and the first
	// to go when the terminal narrows.
	if seg := rateSegment(m.rate, time.Now(), m.width-lipgloss.Width(h)-rateGap); seg != "" {
		h += strings.Repeat(" ", m.width-lipgloss.Width(h)-lipgloss.Width(seg)) + seg
	}
	return h
}

// statusBadge renders the transient inline-action badge (spinner while running,
// ✓/✗ once settled), or "" when idle. Shared by the list header and the
// expanded view, which otherwise wouldn't surface a rerun's outcome. avail is
// the caller's remaining line budget; a failed single-target batch surfaces
// the underlying error verbatim (runBulkNative), which can be an arbitrarily
// long network/GraphQL message, so the fail text is clamped to fit.
func (m Model) statusBadge(avail int) string {
	s := m.actionStatus
	if s == nil {
		return ""
	}
	switch {
	case !s.settled:
		spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return "  " + runBadgeStyle.Render(spin+" "+s.run+"…")
	case s.err != nil:
		// 6 = the "  " prefix (2) + badgeBase's Padding(0, 1) (2) + "✗ " (2),
		// all fixed cells around the truncated text within avail.
		return "  " + failBadgeStyle.Render("✗ "+truncate(s.fail, max(0, avail-6)))
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
	return triage.Compute(ps.prAt(m.cursor), d, m.viewerLogin), true
}

// legendView is the ?-toggled glyph + key reference, as a centered modal. It
// lists every board-view key; expanded-view keys live in that view's own footer.
// legendGroup is one titled, column-aligned section of the legend (glyphs are
// modeled as keyHint{glyph, meaning} pairs so they share the same gridHints
// alignment as real key bindings).
type legendGroup struct {
	title string
	hints []keyHint
}

// legendGroups is every board-view legend section, board-mode-aware (issue
// mode drops the PR-only rows). Order is display order.
func (m Model) legendGroups() []legendGroup {
	groups := []legendGroup{
		{"glyphs", []keyHint{
			{"✓", "CI pass"}, {"✗", "checks failed"}, {ciRunningGlyph, "CI running"}, {"·", "no CI"},
			{mergedGlyph, "merged"}, {closedGlyph, "closed"}, {draftGlyph, "draft"},
			{warnGlyph, "conflict / behind base"}, {autoMergeGlyph(true), "auto-merge armed"},
			{"●", "review required"}, {"✗", "changes requested"}, {reviewCommentedGlyph, "commented by me"},
			{focusBarGlyph, "focus"}, {selBarGlyph, "selected"},
		}},
	}

	nav := []keyHint{{"↑↓/jk", "move"}}
	if m.mode == "pr" {
		nav = append(nav, keyHint{"→/l", "expand"})
	}
	nav = append(nav, keyHint{"⇥", "PRs/Issues"}, keyHint{"space", "select"}, keyHint{"V", "group"})
	groups = append(groups, legendGroup{"navigation", nav})

	filters := []keyHint{{"/", "filter (@user, is:, text)"}, {"s", "state"}}
	if m.mode == "pr" {
		filters = append(filters, keyHint{"R", "reviewers"}, keyHint{"D", "drafts"})
	}
	groups = append(groups, legendGroup{"filters", filters})

	view := []keyHint{}
	if m.mode == "pr" {
		view = append(view, keyHint{"p", "all comments"}) // only the PR preview renders the timeline p unfolds
		if computeLayout(m.width, m.height).ShowSide {
			view = append(view, keyHint{"h/l", "switch tab"}, keyHint{"1-6", "jump tab"})
		}
	}
	view = append(view, keyHint{"z", "maximize"}, keyHint{"alt+j/k", "scroll"})
	groups = append(groups, legendGroup{"view", view})

	actions := []keyHint{
		{"↵", "worktree"}, {"W", "bulk"}, {"y", "#"}, {"Y", "url"}, {"b", "branch"}, {"o", "open"},
	}
	if m.mode == "pr" {
		actions = append(actions, keyHint{"m", "merge"}, keyHint{"r", "rerun"}, keyHint{"u", "update"},
			keyHint{"M", "ready"}, keyHint{"L", "approve"}, keyHint{"X", "cleanup branch"})
	}
	groups = append(groups, legendGroup{"actions", actions})

	groups = append(groups, legendGroup{"", []keyHint{
		{"a", "actions"}, {"ctrl+r", "refresh"}, {"? / F1", "legend"}, {"q", "quit"},
	}})
	return groups
}

// renderLegendGroups lays out grouped keyHints as a titled, column-aligned
// float: each group gets its own gridHints-aligned block (so a wide glyph
// column doesn't force every key elsewhere to the same gutter width), wrapped
// in a titledBox clamped to the terminal size. Below titledBox's own 4x2
// floor the box can exceed a terminal that's smaller still, but every call
// site composites through overlayTop, whose canvas crops any overflow.
func renderLegendGroups(title string, groups []legendGroup, termW, termH int) string {
	maxW := max(20, termW-4)
	var lines []string
	for i, g := range groups {
		if i > 0 {
			lines = append(lines, "")
		}
		if g.title != "" {
			lines = append(lines, panelHeader(g.title))
		}
		lines = append(lines, gridHints(g.hints, maxW, true)...)
	}
	body := strings.Join(lines, "\n")
	w := min(lipgloss.Width(body)+4, termW)
	h := min(len(lines)+2, max(2, termH))
	return titledBox(body, w, h, title)
}

// legendView is the ?-toggled glyph + key reference, as a centered modal. It
// lists every board-view key; expanded-view keys live in that view's own
// legend (see expandedLegendView/logLegendView).
func (m Model) legendView() string {
	groups := m.legendGroups()
	title := "Legend"
	if m.legendQuery != "" {
		q := strings.ToLower(m.legendQuery)
		var filtered []legendGroup
		for _, g := range groups {
			var hints []keyHint
			for _, h := range g.hints {
				if strings.Contains(strings.ToLower(h.key+" "+h.label), q) {
					hints = append(hints, h)
				}
			}
			if len(hints) > 0 {
				filtered = append(filtered, legendGroup{g.title, hints})
			}
		}
		groups = filtered
		title = "Legend: " + m.legendQuery
	}
	return renderLegendGroups(title, groups, m.width, m.height)
}

// actionOrder is the display order for the docked panel's actions section, so
// it doesn't jump around with Go's random map iteration.
var actionOrder = []string{"enter", "m", "A", "r", "u", "M", "L", "W", "y", "Y", "b", "o"}

type keyHint struct{ key, label string }

// navHintsFor is the docked-panel cheatsheet for the active board. Issue mode
// drops the PR-only author/reviewer/drafts hints; both modes show the tab-toggle.
func navHintsFor(mode string) []keyHint {
	base := []keyHint{
		{"↑↓", "move"}, {"⇥", "PRs/Issues"}, {"s", "state"},
		{"/", "find"}, {"space", "select"}, {"V", "group"}, {"q", "quit"},
	}
	if mode == "pr" {
		pr := []keyHint{
			{"→", "tabs"}, {"z", "max"}, {"alt+j/k", "scroll"},
			{"R", "reviewers"}, {"D", "drafts"},
		}
		return append(base, pr...)
	}
	return base
}

const hintGutter = 3

// hintCellWidth is the display width of the cell gridHints renders for h, given
// the key-column width keyW. Styles add only SGR sequences, which carry zero
// display width, so this is computed rather than measured — rendering the cell
// and passing it to lipgloss.Width would strip the ANSI Style.Render just added.
//
// The +1 is the leading space in statusBarStyle.Render(" "+h.label).
func hintCellWidth(h keyHint, keyW int) int {
	return max(keyW, lipgloss.Width(h.key)) + 1 + lipgloss.Width(h.label)
}

// gridGeom is a hint grid's geometry. cellW includes the gutter; cellWidths holds
// each hint's own unpadded width, in hints order.
type gridGeom struct {
	cols, cellW, keyW int
	cellWidths        []int
}

// gridLayout computes the geometry without building any strings, so gridHints and
// panelContentRows can share it.
func gridLayout(hints []keyHint, width int, alignKeys bool) gridGeom {
	g := gridGeom{cellWidths: make([]int, len(hints))}
	// alignKeys pads every key to the widest so the labels line up in a column.
	if alignKeys {
		for _, h := range hints {
			g.keyW = max(g.keyW, lipgloss.Width(h.key))
		}
	}
	for i, h := range hints {
		g.cellWidths[i] = hintCellWidth(h, g.keyW)
		g.cellW = max(g.cellW, g.cellWidths[i])
	}
	g.cellW += hintGutter
	g.cols = max(1, (width+hintGutter)/g.cellW)
	return g
}

// gridRows is how many rows gridHints emits for n hints packed cols wide — zero
// for no hints, matching gridHints' nil return.
func gridRows(n, cols int) int {
	if n == 0 {
		return 0
	}
	return (n + cols - 1) / cols
}

// gridHints lays hints into aligned columns: every cell is padded to the widest
// hint's width so columns line up vertically across rows (a greedy pack leaves
// a ragged, cramped-looking grid). Reflows to as many columns as fit in width.
func gridHints(hints []keyHint, width int, alignKeys bool) []string {
	if len(hints) == 0 {
		return nil
	}
	g := gridLayout(hints, width, alignKeys)
	cells := make([]string, len(hints))
	for i, h := range hints {
		key := accentStyle.Render(h.key)
		if pad := g.keyW - lipgloss.Width(h.key); pad > 0 {
			key += strings.Repeat(" ", pad)
		}
		cells[i] = key + statusBarStyle.Render(" "+h.label)
	}
	lines := make([]string, 0, gridRows(len(hints), g.cols))
	for i := 0; i < len(hints); i += g.cols {
		var b strings.Builder
		for j := i; j < i+g.cols && j < len(hints); j++ {
			b.WriteString(cells[j])
			if j < i+g.cols-1 && j < len(hints)-1 { // pad every cell but the row's last
				b.WriteString(strings.Repeat(" ", g.cellW-g.cellWidths[j]))
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
	nav, acts := navHintsFor("pr"), defaultActionHints()
	return max(
		1+gridRows(len(nav), gridLayout(nav, lw, false).cols),
		1+gridRows(len(acts), gridLayout(acts, rw, true).cols),
	)
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
	if m.mode == "issue" {
		parts = append(parts, hint("f", "preset")) // f cycles issue presets; it's retired on the PR board
	}
	parts = append(parts, hint("/", "find"), hint("space", "select"))
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
// v5 adds additions/deletions/changedFiles and the stack fields; a v4 cache
// entry has none of them and would paint "+0 -0".
const schemaVer = "v5"

// defaultLimit caps the PR list fetch. The fetch, cache write, and cache
// hydrate must all key on the same value or hydration silently misses.
const defaultLimit = 20

// openListLimit is the tail depth for the empty-default open list; the 3-section
// partition needs more than the focused review/terminal boards' defaultLimit.
const openListLimit = 100
