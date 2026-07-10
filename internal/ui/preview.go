package ui

import (
	"fmt"
	"strconv"
	"strings"

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

// detailSchemaVer is bumped whenever PRViewArgs' --json field set changes, so a
// stale-shaped cached detail is a clean miss.
const detailSchemaVer = "v1"

// detailKey scopes a cached PR detail by repo so #7 in one repo can't paint #7
// in another (the shared cache file is keyed by content, not cwd).
func detailKey(repo string, number int) string {
	return cache.Key("prdetail", repo+"#"+strconv.Itoa(number), 0, detailSchemaVer)
}

// issueDetailSchemaVer is bumped whenever IssueViewArgs' --json field set changes.
const issueDetailSchemaVer = "v1"

func issueDetailKey(repo string, number int) string {
	return cache.Key("issuedetail", repo+"#"+strconv.Itoa(number), 0, issueDetailSchemaVer)
}

// fetchDetailCmd lazily loads the selected PR's comments/reviews.
func (m Model) fetchDetailCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.PRViewArgs(number)...)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		d, err := gh.ParsePRDetail(raw)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return prDetailMsg{number: number, detail: d, raw: raw}
	}
}

// fetchIssueDetailCmd lazily loads the selected issue's body.
func (m Model) fetchIssueDetailCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.IssueViewArgs(number)...)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		d, err := gh.ParseIssueDetail(raw)
		if err != nil {
			return fetchFailedMsg{err: err}
		}
		return issueDetailMsg{number: number, detail: d, raw: raw}
	}
}

// detailCmdForCursor refetches the cursor row's detail unless it was already
// refreshed this session; disk-cached (stale) detail still triggers a refetch.
func (m *Model) detailCmdForCursor() tea.Cmd {
	if m.runner == nil {
		return nil
	}
	v, ok := m.cursorVars()
	if !ok {
		return nil
	}
	switch m.section.Kind() {
	case "issue":
		if m.issueFresh[v.Number] {
			return nil
		}
		return m.fetchIssueDetailCmd(v.Number)
	case "pr":
		if m.fresh[v.Number] {
			return nil
		}
		return m.fetchDetailCmd(v.Number)
	}
	return nil
}

// prefetchWindow bounds how many uncached PR details we fan out per settle.
const prefetchWindow = 5

// prefetchNumbers returns up to window PR numbers from cursor downward whose
// detail hasn't been refreshed this session yet.
func prefetchNumbers(ps *PRSection, cursor int, fresh map[int]bool, window int) []int {
	var out []int
	for i := cursor; i < ps.Len() && len(out) < window; i++ {
		num := ps.prAt(i).Number
		if fresh[num] {
			continue
		}
		out = append(out, num)
	}
	return out
}

// prefetchCmd warms detail for a bounded window of visible PRs so the ! column
// and the side card fill in without a fetch per keystroke.
func (m Model) prefetchCmd() tea.Cmd {
	ps, ok := m.section.(*PRSection)
	if !ok || m.runner == nil {
		return nil
	}
	nums := prefetchNumbers(ps, m.cursor, m.fresh, prefetchWindow)
	if len(nums) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(nums))
	for _, n := range nums {
		cmds = append(cmds, m.fetchDetailCmd(n))
	}
	return tea.Batch(cmds...)
}

// renderTimeline renders the latest n items expanded, older collapsed.
func renderTimeline(items []preview.Item, n, width int, expanded bool) string {
	older, latest := preview.Fold(items, n)
	if expanded {
		older, latest = 0, items
	}
	var b strings.Builder
	if older > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("▸ %d earlier comments", older)) + "\n\n")
	}
	sep := sepStyle.Render(strings.Repeat("─", width))
	for i, it := range latest {
		if i > 0 {
			b.WriteString(sep + "\n\n")
		}
		body, err := preview.Render(it.Body, width)
		if err != nil {
			body = it.Body // render failed; show the raw markdown rather than nothing
		}
		b.WriteString(metaLine(it.Author, it.State, it.At) + "\n" + body + "\n")
	}
	return b.String()
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

// identityHeader is the side card's top block: number + title, then a dim
// author · branch · age line. The branch anchors the copy/worktree actions.
func identityHeader(pr gh.PR) string {
	line1 := accentStyle.Render(fmt.Sprintf("#%d", pr.Number)) + " " + headerStyle.Render(pr.Title)
	line2 := authorStyle(pr.Author.Login).Render(pr.Author.Login) +
		dimStyle.Render(" · "+pr.HeadRefName+" · "+ageString(pr.UpdatedAt))
	return line1 + "\n" + line2
}

// sectionRule is a section divider: an UPPERCASE sapphire label (distinct from
// the body text) followed by a short rule — not the full pane width.
func sectionRule(label string, w int) string {
	name := sectionLabelStyle.Render(strings.ToUpper(label))
	ruleLen := 6
	if max := w - lipgloss.Width(name) - 1; ruleLen > max {
		ruleLen = max
	}
	if ruleLen < 0 {
		ruleLen = 0
	}
	return name + " " + sepStyle.Render(strings.Repeat("─", ruleLen))
}

// previewPane renders the triage card followed by the timeline. Before the
// per-PR detail loads it pre-fills the identity header and a card from list-only
// data (triage.Preliminary) so the cursor never lands on a bare "Loading…";
// detail enriches the card and adds the review/timeline sections in place.
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	w := m.previewWidth()
	bw := w - 2 // body width: leave room for the 2-col section indent below
	// A section is its label (flush) + body indented one level under it; the blank
	// line between blocks (the join below) separates sections so they breathe.
	section := func(label, body string) string {
		return sectionRule(label, w) + "\n" + indentLines(strings.TrimRight(body, "\n"), 2)
	}
	if is, ok := m.section.(*IssueSection); ok {
		return m.issuePreviewPane(is, w, bw)
	}
	d, cached := m.detail[v.Number]
	var blocks []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		blocks = append(blocks, identityHeader(pr))
		tc := triage.Preliminary(pr)
		if cached {
			tc = triage.Compute(pr, d)
		}
		if card := renderCard(tc, bw); card != "" {
			blocks = append(blocks, section("blocker", card))
		}
		// The checks section is redundant when the blocker card is already about
		// CI; show it only when the blocker is something else (review/conflict)
		// that would otherwise mask failing checks.
		if tc.Kind != triage.KindChecksFailing && tc.Kind != triage.KindChecksRunning {
			if ci := ciLine(pr); ci != "" {
				blocks = append(blocks, section("checks", ci))
			}
		}
	}
	if !cached {
		blocks = append(blocks, dimStyle.Render("  loading details…"))
		return strings.Join(blocks, "\n\n")
	}
	blocks = append(blocks, section("review", reviewLine(d)))
	blocks = append(blocks, section("latest", renderTimeline(preview.Timeline(d), m.previewN, bw, m.previewExpanded)))
	return strings.Join(blocks, "\n\n")
}

// issuePreviewPane renders the issue identity header + its markdown body. The
// body is the whole v1 story; the comments timeline lands in a later milestone.
func (m Model) issuePreviewPane(is *IssueSection, w, bw int) string {
	iss := is.issueAt(m.cursor)
	blocks := []string{identityHeaderIssue(iss)}
	d, cached := m.issueDetail[iss.Number]
	if !cached {
		blocks = append(blocks, dimStyle.Render("  loading details…"))
		return strings.Join(blocks, "\n\n")
	}
	body, err := preview.Render(d.Body, bw)
	if err != nil {
		body = d.Body
	}
	blocks = append(blocks, sectionRule("body", w)+"\n"+indentLines(strings.TrimRight(body, "\n"), 2))
	return strings.Join(blocks, "\n\n")
}

// identityHeaderIssue mirrors identityHeader for issues (no branch/head ref line).
func identityHeaderIssue(is gh.Issue) string {
	line1 := accentStyle.Render(fmt.Sprintf("#%d", is.Number)) + " " + headerStyle.Render(is.Title)
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

// contentHeight is the list/preview body height. Modes that don't dock the panel
// (zoom, filtering, a confirm prompt) reclaim its reserved rows so the box fills
// the frame instead of stranding the bottom border mid-screen.
func (m Model) contentHeight(l Layout) int {
	if !l.ShowPanel {
		return l.ContentHeight
	}
	switch {
	case m.previewMax:
		return l.ContentHeight + l.PanelRows
	case m.filtering || m.pending != nil:
		return l.ContentHeight + l.PanelRows - 1 // minus the prompt/filter input line
	default:
		return l.ContentHeight
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
		return pendStyle.Render("● checks running")
	default: // pass / none — the row glyph carries it; keep the quick view calm
		return ""
	}
}

// reviewLine summarises the review state: who requested changes (the actionable
// case), else who approved, else the pending requested reviewers.
func reviewLine(d gh.PRDetail) string {
	var changed, approved []string
	for _, r := range d.LatestReviews {
		switch r.State {
		case "CHANGES_REQUESTED":
			changed = append(changed, "@"+r.Author.Login)
		case "APPROVED":
			approved = append(approved, "@"+r.Author.Login)
		}
	}
	switch {
	case len(changed) > 0:
		return failStyle.Render("✗ changes requested by " + strings.Join(changed, ", "))
	case len(approved) > 0:
		return passStyle.Render("✓ approved by " + strings.Join(approved, ", "))
	default:
		return reviewersLine(d.ReviewRequests)
	}
}

// reviewersLine summarises requested reviewers for the quick window. Team
// requests have no login and are skipped.
func reviewersLine(reqs []gh.ReviewRequest) string {
	var logins []string
	for _, r := range reqs {
		if r.Login != "" {
			logins = append(logins, r.Login)
		}
	}
	if len(logins) == 0 {
		return pendStyle.Render("⚠ no reviewers")
	}
	return dimStyle.Render("reviewers: " + strings.Join(logins, ", "))
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
		return failStyle.Render("⚠")
	case d.MergeStateStatus == "BEHIND":
		return pendStyle.Render("⚠")
	default:
		return ""
	}
}

// renderMain lays the bordered list and (when wide) the bordered side preview.
// renderDocked stacks the keys/actions panel beneath the list in the left
// column and lets the preview span the full height on the right.
func (m Model) renderDocked(l Layout) string {
	list := titledBox(m.vp.View(), l.ListWidth, l.ContentHeight, m.listTitle())
	panel := m.keysActionsPanel(l.ListWidth)
	left := lipgloss.JoinVertical(lipgloss.Left, list, panel)

	fullH := l.ContentHeight + l.PanelRows // list + panel, so the preview reaches the bottom
	side := titledBox(dropLines(m.previewPane(), m.previewOffset), l.SideWidth, fullH, m.previewTitle())
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, side)
}

func (m Model) renderMain() string {
	l := computeLayout(m.width, m.height)
	ch := m.contentHeight(l)
	if m.previewMax && l.ShowSide {
		return titledBox(dropLines(m.previewPane(), m.previewOffset), m.width, ch, m.previewTitle())
	}
	list := titledBox(m.vp.View(), l.ListWidth, ch, m.listTitle())
	if !l.ShowSide {
		return list
	}
	side := titledBox(dropLines(m.previewPane(), m.previewOffset), l.SideWidth, ch, m.previewTitle())
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, side)
}
