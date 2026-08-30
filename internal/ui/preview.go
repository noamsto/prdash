package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
	"github.com/noamsto/prdash/internal/triage"
)

type prDetailMsg struct {
	number int
	detail gh.PRDetail
	raw    []byte // cached to disk so the preview paints instantly next launch
}

// detailSchemaVer is bumped whenever the PR-detail field set changes, so a
// stale-shaped cached detail is a clean miss.
const detailSchemaVer = "v2"

// detailKey scopes a cached PR detail by repo so #7 in one repo can't paint #7
// in another (the shared cache file is keyed by content, not cwd).
func detailKey(repo string, number int) string {
	return cache.Key("prdetail", repo+"#"+strconv.Itoa(number), 0, detailSchemaVer)
}

// issueDetailSchemaVer is bumped whenever the issue-detail field set changes.
const issueDetailSchemaVer = "v1"

func issueDetailKey(repo string, number int) string {
	return cache.Key("issuedetail", repo+"#"+strconv.Itoa(number), 0, issueDetailSchemaVer)
}

// fetchIssueDetailCmd lazily loads the selected issue's body through the
// issue-detail source.
func (m Model) fetchIssueDetailCmd(number int) tea.Cmd {
	src := m.issueDetailSource
	if src == nil {
		return nil
	}
	return func() tea.Msg {
		d, raw, err := src.FetchIssueDetail(number)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return issueDetailMsg{number: number, detail: d, raw: raw}
	}
}

// detailCmdForCursor refetches the cursor row's detail unless it was already
// refreshed this session or its disk cache is still within launchFreshTTL — so
// navigating right after a launch reuses recent detail instead of refetching it.
func (m *Model) detailCmdForCursor() tea.Cmd {
	v, ok := m.cursorVars()
	if !ok {
		return nil
	}
	switch m.section.Kind() {
	case "issue":
		if m.issueFresh[v.Number] || m.cacheFresh(issueDetailKey(m.repo, v.Number)) {
			return nil
		}
		return m.fetchIssueDetailCmd(v.Number)
	case "pr":
		if m.fresh[v.Number] || m.cacheFresh(detailKey(m.repo, v.Number)) {
			return nil
		}
		return m.batchDetailCmd([]int{v.Number})
	}
	return nil
}

// warmDetailCmd warms detail for the cursor row and the prefetch window. It
// fetches the cursor row on its own (so the preview paints as soon as that one
// small query returns) and the rest of the window in a single batched request —
// so a settle costs two HTTP round trips, not a fan-out of one request per PR.
// On the issue board (no PRSection) it warms only the cursor row's detail.
func (m Model) warmDetailCmd() tea.Cmd {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return m.detailCmdForCursor()
	}
	cursorNum := -1
	if v, ok := m.cursorVars(); ok {
		cursorNum = v.Number
	}
	var rest []int
	for _, n := range m.detailWindow(ps) {
		if n != cursorNum {
			rest = append(rest, n)
		}
	}
	return tea.Batch(m.detailCmdForCursor(), m.batchDetailCmd(rest))
}

// detailWindow is the cursor-nearest set of shown PR numbers still needing detail
// (not refreshed this session, not fresh on disk), bounded by prefetchWindow.
func (m Model) detailWindow(ps *PRSection) []int {
	var out []int
	for _, n := range prefetchNumbers(ps, m.cursor, m.fresh, prefetchWindow) {
		if m.cacheFreshFor(detailKey(m.repo, n), m.detailFreshTTL(ps, n)) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// detailFreshTTL tiers how long a relaunch trusts a cached detail. The viewer's
// own PRs revalidate at launchFreshTTL; everyone else's ride the cold poll's
// spacing, which is already how stale the rest of the board is allowed to be.
// The cursor row is not tiered — detailCmdForCursor always uses launchFreshTTL,
// because the row you are looking at is the one you want current.
func (m Model) detailFreshTTL(ps *PRSection, number int) time.Duration {
	if m.viewerLogin != "" && ps.authorOf(number) == m.viewerLogin {
		return launchFreshTTL
	}
	return pollIntervalCold
}

// batchDetailCmd fetches detail for numbers in a single request via the batched
// source, emitting one detailsBatchMsg for the whole window.
func (m Model) batchDetailCmd(numbers []int) tea.Cmd {
	src := m.detailSource
	if src == nil || len(numbers) == 0 {
		return nil
	}
	return func() tea.Msg {
		details, raws, err := src.FetchDetails(numbers)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return detailsBatchMsg{details: details, raws: raws}
	}
}

// prefetchWindow bounds how many uncached PR details we fan out per settle.
const prefetchWindow = 5

// prefetchNumbers returns up to window PR numbers nearest the cursor whose
// detail hasn't been refreshed this session yet. Order is by |i - cursor|
// ascending; on a tie the row below the cursor wins (preserves the old
// downward bias for the first neighbor).
func prefetchNumbers(ps *PRSection, cursor int, fresh map[int]bool, window int) []int {
	n := ps.Len()
	if n == 0 || window <= 0 {
		return nil
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	var out []int
	// d == 0 yields only the cursor; d > 0 yields below then above.
	for d := 0; len(out) < window && d < n; d++ {
		candidates := []int{cursor + d}
		if d > 0 {
			candidates = append(candidates, cursor-d)
		}
		for _, i := range candidates {
			if i < 0 || i >= n {
				continue
			}
			num := ps.prAt(i).Number
			if fresh[num] {
				continue
			}
			out = append(out, num)
			if len(out) >= window {
				return out
			}
		}
	}
	return out
}

// discussionHeader keeps identity and separation on one line. This gives each
// comment a clear start without spending a full row on a divider.
func discussionHeader(meta string, width int) string {
	ruleLen := width - lipgloss.Width(meta) - 1
	if ruleLen < 3 {
		return meta
	}
	return meta + " " + sepStyle.Render(strings.Repeat("─", ruleLen))
}

// renderDiscussionItem renders one GitHub-style comment/review block. Glamour
// owns the padding inside the markdown body; trimming the tail keeps adjacent
// items from accumulating extra blank rows.
func renderDiscussionItem(meta, markdown string, width int) string {
	if markdown == "" {
		return discussionHeader(meta, width)
	}
	body, err := preview.Render(markdown, width)
	if err != nil {
		body = markdown // render failed; show the raw markdown rather than nothing
	}
	return discussionHeader(meta, width) + "\n" + strings.TrimRight(body, "\n")
}

// renderTimeline renders the latest n items expanded, older collapsed.
func renderTimeline(items []preview.Item, n, width int, expanded bool) string {
	older, latest := preview.Fold(items, n)
	if expanded {
		older, latest = 0, items
	}
	blocks := make([]string, 0, len(latest)+1)
	if older > 0 {
		blocks = append(blocks, dimStyle.Render(fmt.Sprintf("▸ %d earlier comments", older)))
	}
	for _, it := range latest {
		blocks = append(blocks, renderDiscussionItem(metaLine(it.Author, it.State, it.At), it.Body, width))
	}
	if len(blocks) == 0 {
		return dimStyle.Render("No conversation yet.")
	}
	return strings.Join(blocks, "\n\n")
}

func (m Model) previewWidth() int {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return 40
	}
	if m.previewMax {
		return m.width - 2 // interior of the full-width box
	}
	return l.SideWidth - 2
}

// identityHeader is the side card's top block: number + title, then author,
// base <- head and age, then the label chips. Labels live here rather than on
// the board row, and the base branch appears alongside the head so the merge
// target is visible without opening the Diff tab.
func identityHeader(pr gh.PR, w int) string {
	lines := []string{
		accentStyle.Render(fmt.Sprintf("#%d", pr.Number)) + " " + headerStyle.Render(pr.Title),
		authorStyle(pr.Author.Login).Render(pr.Author.Login) + "  " +
			dimStyle.Render(headBranchGlyph+" "+pr.BaseRefName+" "+baseArrowGlyph+" "+pr.HeadRefName) +
			dimStyle.Render("  "+ageString(pr.UpdatedAt)),
	}
	if chips := renderChips(pr.Labels, w); chips != "" {
		lines = append(lines, chips)
	}
	return strings.Join(lines, "\n")
}

const (
	descLinesOwn    = 2 // your own PRs collapse tight — you wrote them
	descLinesOthers = 6 // others' PRs show enough to start reviewing
)

// previewDescriptionBody renders the PR body for the preview pane, capped by
// authorship. Empty bodies return "" so the caller omits the section entirely.
func previewDescriptionBody(pr gh.PR, viewer string, w int) string {
	if strings.TrimSpace(pr.Body) == "" {
		return ""
	}
	rendered, err := preview.Render(pr.Body, w)
	if err != nil {
		rendered = pr.Body
	}
	limit := descLinesOthers
	if viewer != "" && pr.Author.Login == viewer {
		limit = descLinesOwn
	}
	lines := strings.Split(strings.Trim(rendered, "\n"), "\n")
	if len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:limit], "\n") + "\n" +
		dimStyle.Render("· full text in Description tab")
}

// sectionHeader is a preview section divider: a glyph plus a Title Case name,
// underlined, in one accent. No rule and no uppercasing — the pane should paint
// its content, not its scaffolding.
func sectionHeader(glyph, label string, w int) string {
	name := strings.ToUpper(label[:1]) + label[1:]
	return sectionLabelStyle.Underline(true).Render(glyph + " " + name)
}

// previewParts splits the side pane into the head that stays pinned to the top
// of the pane — identity lines plus the tab bar — and the body below it, which
// is the only part alt+j/k scrolls. The Overview tab renders via renderOverview
// directly (not expandedBody) because it — not expandedBody's pre-switch
// !cached gate — owns the pre-fill from list-only data (triage.Preliminary), so
// the cursor never lands on a bare "Loading…" before detail arrives.
func (m Model) previewParts() (head, body string) {
	if _, ok := m.cursorVars(); !ok {
		return "", ""
	}
	w := m.previewWidth()
	if is, ok := m.section.(*IssueSection); ok {
		return m.issuePreviewParts(is, w, w-2)
	}
	ps, ok := m.section.(*PRSection)
	if !ok {
		return "", ""
	}
	head = identityHeader(ps.prAt(m.cursor), w-2) + "\n\n" + renderTabBar(expandedTabs, m.expandedTab, w)
	if m.expandedTab == tabOverview {
		return head, m.renderOverview(w)
	}
	return head, m.expandedBody(w)
}

// previewPane is the whole side pane, head and body together — the measurement
// view of previewParts.
func (m Model) previewPane() string {
	head, body := m.previewParts()
	if head == "" {
		return body
	}
	return head + "\n\n" + body
}

// previewScrolled is what the side pane's box gets: the head pinned in place
// with only the body scrolled by previewOffset.
func (m Model) previewScrolled() string {
	head, body := m.previewParts()
	if head == "" {
		return dropLines(body, m.previewOffset)
	}
	return head + "\n\n" + dropLines(body, m.previewOffset)
}

// renderOverview is the Overview tab body: the triage summary shown by default.
// Identity is owned by the container; this is everything below it.
func (m Model) renderOverview(w int) string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	bw := w - 2
	section := func(glyph, label, body string) string {
		return sectionHeader(glyph, label, w) + "\n" + indentLines(strings.TrimRight(body, "\n"), 2)
	}
	d, cached := m.detail[v.Number]
	var blocks []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		if body := previewDescriptionBody(pr, m.viewerLogin, bw); body != "" {
			blocks = append(blocks, section(descriptionGlyph, "description", body))
		}
		parentNumber := ps.stackParentNumber(m.cursor)
		tc := triage.Preliminary(pr, m.viewerLogin, parentNumber)
		if cached {
			tc = triage.Compute(pr, d, m.viewerLogin, parentNumber)
		}
		if card := renderCard(tc, bw); card != "" {
			blocks = append(blocks, section(blockerGlyph, "blocker", card))
		}
		if tc.Kind != triage.KindChecksFailing && tc.Kind != triage.KindChecksRunning {
			if ci := ciLine(pr); ci != "" {
				blocks = append(blocks, section(checksGlyph, "checks", ci))
			}
		}
	}
	if !cached {
		blocks = append(blocks, dimStyle.Render("  loading details…"))
		return strings.Join(blocks, "\n\n")
	}
	blocks = append(blocks, section(reviewGlyph, "review", reviewRoster(d)))
	if ts := m.detail[v.Number].ReviewThreads; len(ts) > 0 {
		label := fmt.Sprintf("threads  %d unresolved", len(preview.Unresolved(ts)))
		if body := renderThreadsSummary(ts, m.previewN, bw); body != "" {
			blocks = append(blocks, section(threadsGlyph, label, body))
		}
	}
	blocks = append(blocks, section(latestGlyph, "latest", renderTimeline(preview.Timeline(d), m.previewN, bw, m.previewExpanded)))
	return strings.Join(blocks, "\n\n")
}

// issuePreviewParts is previewParts for issues: the identity header pins, the
// markdown body scrolls. The body is the whole v1 story; the comments timeline
// lands in a later milestone.
func (m Model) issuePreviewParts(is *IssueSection, w, bw int) (head, body string) {
	iss := is.issueAt(m.cursor)
	head = identityHeaderIssue(iss)
	d, cached := m.issueDetail[iss.Number]
	if !cached {
		return head, dimStyle.Render("  loading details…")
	}
	md, err := preview.Render(d.Body, bw)
	if err != nil {
		md = d.Body
	}
	return head, sectionHeader(descriptionGlyph, "body", w) + "\n" + indentLines(strings.TrimRight(md, "\n"), 2)
}

// identityHeaderIssue mirrors identityHeader for issues (no branch/head ref line).
func identityHeaderIssue(is gh.Issue) string {
	line1 := issueAccentStyle.Render(fmt.Sprintf("#%d", is.Number)) + " " + headerStyle.Render(is.Title)
	line2 := authorStyle(is.Author.Login).Render(is.Author.Login) +
		dimStyle.Render(" · "+ageString(is.UpdatedAt))
	return line1 + "\n" + line2
}

// previewTitle is the side pane's border title.
func (m Model) previewTitle() string {
	base := "Preview"
	if v, ok := m.cursorVars(); ok && v.Number > 0 {
		base = fmt.Sprintf("#%d", v.Number)
	}
	// Zoom hides the keys/actions panel, so fold the recommended action into the
	// title where there's room.
	if m.previewMax {
		if card, ok := m.cursorCard(); ok && card.ActionKey != "" {
			base += " · " + card.ActionLabel + " → " + card.ActionKey
		}
	}
	return base
}

// contentHeight is the list/preview body height. The always-visible filter bar
// (1 row blurred, more while focused) is reserved out of every mode; zoom and a
// confirm prompt additionally reclaim the docked panel's rows so the box fills
// the frame instead of stranding the bottom border mid-screen.
func (m Model) contentHeight(l Layout) int {
	bar := m.filterBarRows()
	// The filter bar is always rendered, so it reserves its row(s) uniformly —
	// the old footer-swap distinction no longer applies. !ShowPanel covers both
	// the footer-hidden small window and the status-bar footer (ShowPanel is only
	// ever set when ShowFooter is).
	if !l.ShowPanel {
		return max(1, l.ContentHeight-bar)
	}
	switch {
	case m.previewMax:
		return max(1, l.ContentHeight+l.PanelRows-bar)
	case m.pending != nil:
		return max(1, l.ContentHeight+l.PanelRows-1-bar) // minus the prompt line
	default:
		return max(1, l.ContentHeight-bar)
	}
}

// ciLine surfaces the check rollup in the quick view independent of the triage
// card, which keys off mergeStateStatus and can mask failing CI behind a
// review/conflict headline.
func ciLine(pr gh.PR) string {
	switch pr.CIState() {
	case "fail":
		var names []string
		for _, c := range pr.Checks() {
			if c.Result() == "fail" {
				names = append(names, c.Label())
			}
		}
		s := failStyle.Render("✗ checks failing")
		if len(names) > 0 {
			s += dimStyle.Render(": " + strings.Join(names, ", "))
		}
		return s
	case "pending":
		return pendStyle.Render(ciRunningGlyph + " checks running")
	default: // pass / none — the row glyph carries it; keep the quick view calm
		return ""
	}
}

// reviewState maps a review state to its roster glyph, label, style, and sort
// rank. Ranked most-actionable first: changes requested, pending, commented,
// approved, dismissed. "PENDING" is our own marker for a requested reviewer who
// has not submitted — GitHub has no such review state.
var reviewState = map[string]struct {
	rank  int
	glyph string
	label string
	style lipgloss.Style
}{
	"CHANGES_REQUESTED": {0, "✗", "changes requested", failStyle},
	"PENDING":           {1, "○", "pending", pendStyle},
	"COMMENTED":         {2, "◐", "commented", dimStyle},
	"APPROVED":          {3, "✓", "approved", passStyle},
	"DISMISSED":         {4, "·", "dismissed", dimStyle},
}

// reviewRoster lists every reviewer on one line with a status glyph, merging
// those who have reviewed with those still requested. A person re-requested
// after reviewing counts as pending — GitHub treats the prior review as stale —
// so an outstanding request overrides any latest review from the same login.
func reviewRoster(d gh.PRDetail) string {
	pending := map[string]bool{}
	for _, r := range d.ReviewRequests {
		if r.Login != "" {
			pending[r.Login] = true
		}
	}
	type entry struct{ login, state string }
	var entries []entry
	seen := map[string]bool{}
	for _, r := range d.LatestReviews {
		login := r.Author.Login
		if login == "" || pending[login] || seen[login] {
			continue
		}
		seen[login] = true
		entries = append(entries, entry{login, r.State})
	}
	added := map[string]bool{}
	for _, r := range d.ReviewRequests {
		if r.Login == "" || added[r.Login] {
			continue
		}
		added[r.Login] = true
		entries = append(entries, entry{r.Login, "PENDING"})
	}
	if len(entries) == 0 {
		return pendStyle.Render("○ no reviewers")
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return reviewState[entries[i].state].rank < reviewState[entries[j].state].rank
	})
	loginW := 0
	for _, e := range entries {
		if w := len(e.login) + 1; w > loginW {
			loginW = w
		}
	}
	var lines []string
	for _, e := range entries {
		s := reviewState[e.state]
		// The login carries its author hue — the same hash the board's Author column
		// uses (keyed on the raw login, not the "@" display text) — so a reviewer
		// reads as the same colour here as in the list. The glyph and label keep the
		// review-status colour.
		login := authorStyle(e.login).Render(fmt.Sprintf("%-*s", loginW, "@"+e.login))
		lines = append(lines, s.style.Render(s.glyph+" ")+login+s.style.Render("  "+s.label))
	}
	return strings.Join(lines, "\n")
}

// flagGlyph is the board's ! column: a conflict (red) or behind-base (yellow)
// marker. It is detail-derived — blank unless the PR's detail is cached, so the
// board never guesses a blocker from the unreliable bulk list.
func flagGlyph(d gh.PRDetail, cached bool) string {
	if !cached {
		return ""
	}
	switch {
	case d.MergeStateStatus == "DIRTY" || d.Mergeable == "CONFLICTING":
		return failStyle.Render(warnGlyph)
	case d.MergeStateStatus == "BEHIND":
		return pendStyle.Render(warnGlyph)
	default:
		return ""
	}
}

// previewHeight is the OUTER height of the side preview box — the single
// authority both render paths and the scroll clamp measure against. The preview
// owns the whole right column: it spans the filter bar's rows (which sit over
// the list column only) and, when the keys panel is docked, its rows too.
func (m Model) previewHeight(l Layout) int {
	switch {
	case m.previewMax:
		return m.contentHeight(l)
	case l.ShowSide && l.ShowPanel:
		return l.ContentHeight + l.PanelRows // renderDocked's bar + list + panel
	default:
		return m.contentHeight(l) + m.filterBarRows()
	}
}

// renderMain lays the filter bar and the bordered list in the left column and
// (when wide) the bordered side preview beside them, full height.
// renderDocked additionally stacks the keys/actions panel beneath the list in
// that column. Unlike renderMain it doesn't go through contentHeight — the
// panel is always docked here — so it reserves the filter bar's rows itself.
func (m Model) renderDocked(l Layout) string {
	tint := accentFor(m.mode)
	bar := m.filterBar()
	ch := max(1, l.ContentHeight-m.filterBarRows())
	list := titledBoxTinted(m.listBody(), l.ListWidth, ch, m.listTitle(), tint)
	panel := m.keysActionsPanel(l.ListWidth)
	left := lipgloss.JoinVertical(lipgloss.Left, bar, list, panel)

	side := titledBoxTinted(m.previewScrolled(), l.SideWidth, m.previewHeight(l), m.previewTitle(), tint)
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, side)
}

// listBody is the list pane's interior: the scrolling viewport, with the sticky
// column-header row pinned above it when the board shows one. The header lives
// outside the viewport so it never scrolls and never enters the cursor math.
func (m Model) listBody() string {
	if m.listColHeader == "" {
		return m.vp.View()
	}
	return m.listColHeader + "\n" + m.vp.View()
}

func (m Model) renderMain() string {
	l := computeLayout(m.width, m.height)
	ch := m.contentHeight(l)
	tint := accentFor(m.mode)
	bar := m.filterBar()
	if m.previewMax {
		return bar + "\n" + titledBoxTinted(m.previewScrolled(), m.width, ch, m.previewTitle(), tint)
	}
	left := lipgloss.JoinVertical(lipgloss.Left, bar, titledBoxTinted(m.listBody(), l.ListWidth, ch, m.listTitle(), tint))
	if !l.ShowSide {
		return left
	}
	side := titledBoxTinted(m.previewScrolled(), l.SideWidth, m.previewHeight(l), m.previewTitle(), tint)
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, side)
}
