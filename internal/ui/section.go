package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/issue"
)

// RowOpts controls how a section renders one row.
type RowOpts struct {
	Width       int
	NumWidth    int // cell width for the right-aligned number column (0 = natural)
	Focused     bool
	Selected    bool
	Draft       bool   // dim the title; drafts sort last (see prRank)
	Landed      bool   // merged by prdash this session, held on the open board until ctrl+r
	Commented   bool   // viewer's latest review is a comment; the review column shows ◐ instead of the decision dot
	Flag        string // pre-rendered ! column glyph (conflict/behind), "" when unknown
	Tree        string // stack chain glyph, rendered between the gutter and the number
	DiffWidth   int    // cell width of the diffstat column; 0 hides it
	TicketWidth int    // cell width of the ticket-id column; 0 hides it
}

type Section interface {
	Kind() string
	Filter() string
	RenderRow(i int, o RowOpts) string // render shown-row i as a dense single-line row
	Len() int
	VarsAt(i int) action.Vars
	Haystacks() []string
	SetShown(idx []int)
}

// --- PR section ---
type PRSection struct {
	filter     string
	prs        []gh.PR
	shown      []int
	grouped    bool   // true when the board renders group headers (see setShownOrdered)
	hideDrafts bool   // when true, draft PRs are excluded from the shown set
	forceGroup bool   // group even with a single author (non-"mine" views)
	forceFlat  bool   // suppress all grouping — keep the incoming (fuzzy rank) order
	state      string // active view state (open|merged|closed); selects the sort key

	cats     map[int]string // PR number → category label; non-nil switches grouping from author to category
	catOrder []string       // category header order (e.g. Mine, Review requested)
}

func NewPRSection(filter string) *PRSection { return &PRSection{filter: filter} }
func (s *PRSection) Kind() string           { return "pr" }
func (s *PRSection) Filter() string         { return s.filter }
func (s *PRSection) SetPRs(p []gh.PR) {
	s.cats, s.catOrder = nil, nil // flat/author grouping; SetCategorized opts into category grouping
	sortPRs(p, s.state)
	s.prs = p
	s.setShownOrdered(allIdx(len(p)))
}

// SetCategorized paints PRs grouped under category headers (order) instead of by
// author — used by the mine view (Mine / Review requested).
func (s *PRSection) SetCategorized(p []gh.PR, cats map[int]string, order []string) {
	sortPRs(p, s.state)
	s.prs = p
	s.cats = cats
	s.catOrder = order
	s.setShownOrdered(allIdx(len(p)))
}

// groupLabel is the header key for shown-row i: category when categorized, else author.
func (s *PRSection) groupLabel(i int) string {
	p := s.prs[s.shown[i]]
	if len(s.catOrder) > 0 {
		return s.cats[p.Number]
	}
	return p.Author.Login
}
func (s *PRSection) Len() int           { return len(s.shown) }
func (s *PRSection) SetShown(idx []int) { s.setShownOrdered(idx) }

// prAt returns the gh.PR at shown-row i (for triage, which needs list fields).
func (s *PRSection) prAt(i int) gh.PR { return s.prs[s.shown[i]] }

// authorOf returns the login that opened PR number, or "" when the board does not
// hold it.
func (s *PRSection) authorOf(number int) string {
	for _, p := range s.prs {
		if p.Number == number {
			return p.Author.Login
		}
	}
	return ""
}

// ApplyChecks replaces the rollup on the PRs named in checks. It deliberately
// does not re-sort: the sort key ranks by actionability, which CI state feeds, so
// re-sorting here would move rows under the user on a background beat. Order
// settles on the next real fetch.
func (s *PRSection) ApplyChecks(checks map[int][]gh.Check) {
	for i := range s.prs {
		if c, ok := checks[s.prs[i].Number]; ok {
			s.prs[i].StatusCheckRollup = c
		}
	}
}

// updatePR applies fn to the stored PR with the given number. ok is false when
// the board does not hold it.
func (s *PRSection) updatePR(number int, fn func(*gh.PR)) bool {
	for i := range s.prs {
		if s.prs[i].Number == number {
			fn(&s.prs[i])
			return true
		}
	}
	return false
}

func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	o.Draft = p.IsDraft
	// A terminal PR's cell-1 glyph reflects how it ended, not its frozen CI rollup:
	// merged → mauve merge mark, closed → dim ✗. The age column likewise shows the
	// event that ended it (merge/close time) rather than the last update.
	status := ciGlyph(p.CIState())
	age := ageString(p.UpdatedAt)
	switch {
	case p.IsMerged():
		status, age = mergedMark(), ageString(p.MergedAt)
	case p.State == "CLOSED":
		status, age = closedMark(), ageString(p.ClosedAt)
	case p.IsDraft:
		// A draft's CI rollup is the least interesting thing about it, and every
		// other indicator on this row is a gutter glyph. Costs the CI mark; the
		// preview still has it.
		status = draftMark()
	}
	auto := autoMergeGlyph(p.AutoMergeEnabled())
	// A commented-by-me PR keeps the pending color but swaps the dot for ◐:
	// review is still required, the viewer's part is just already in.
	review := reviewDot(p.ReviewDecision)
	if o.Commented {
		review = pendStyle.Render(reviewCommentedGlyph)
	}
	author := p.Author.Login
	diff := ""
	if o.DiffWidth > 0 {
		diff = diffstat(p.Additions, p.Deletions)
	}
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title, "",
		author, age, diff, status, review, auto)
}

func (s *PRSection) VarsAt(i int) action.Vars {
	p := s.prs[s.shown[i]]
	return action.Vars{Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login, Branch: p.HeadRefName,
		ID: p.ID}
}
func (s *PRSection) Haystacks() []string {
	h := make([]string, len(s.prs))
	for i, p := range s.prs {
		h[i] = haystack(p)
	}
	return h
}

// Actionability ranks (lower sorts higher). Drafts always last.
const (
	rankReady = iota
	rankChanges
	rankFail
	rankRunning
	rankWaiting
	rankDraft
)

// prRank scores a PR by how much it needs the author, using only signals that
// are reliable from the bulk `gh pr list` (CI rollup, reviewDecision, isDraft).
// It deliberately ignores mergeStateStatus/conflict — those are detail-derived
// and would reshuffle the board as background prefetch lands.
func prRank(p gh.PR) int {
	ci := p.CIState()
	switch {
	case p.IsDraft:
		return rankDraft
	case p.ReviewDecision == "CHANGES_REQUESTED":
		return rankChanges
	case ci == "fail":
		return rankFail
	case ci == "pending":
		return rankRunning
	case p.ReviewDecision == "APPROVED":
		return rankReady
	default:
		return rankWaiting
	}
}

// sortPRs orders the board. Terminal states are chronological (newest event
// first); the open board keeps the actionability rank, ties broken most-recently
// updated. Rank is meaningless once a PR has landed/closed, so it's skipped there.
func sortPRs(prs []gh.PR, state string) {
	switch state {
	case "merged":
		slices.SortStableFunc(prs, func(a, b gh.PR) int { return b.MergedAt.Compare(a.MergedAt) })
	case "closed":
		slices.SortStableFunc(prs, func(a, b gh.PR) int { return b.ClosedAt.Compare(a.ClosedAt) })
	default:
		slices.SortStableFunc(prs, func(a, b gh.PR) int {
			if d := prRank(a) - prRank(b); d != 0 {
				return d
			}
			return b.UpdatedAt.Compare(a.UpdatedAt)
		})
	}
}

// setShownOrdered records the shown subset in display order and decides grouping.
// idx arrives in actionability order (prs is rank-sorted; idx preserves it). With
// ≥2 distinct authors the rows are regrouped contiguously by author so the cursor
// still walks them top-to-bottom; with one author the flat rank order stands.
func (s *PRSection) SetHideDrafts(v bool) { s.hideDrafts = v }
func (s *PRSection) SetForceGroup(v bool) { s.forceGroup = v }
func (s *PRSection) SetForceFlat(v bool)  { s.forceFlat = v }

// SetState records the view state so the next SetPRs/SetCategorized sorts by the
// right key (merge/close time for terminal states, actionability for open).
func (s *PRSection) SetState(state string) { s.state = state }

func (s *PRSection) setShownOrdered(idx []int) {
	if s.hideDrafts {
		idx = slices.DeleteFunc(slices.Clone(idx), func(i int) bool { return s.prs[i].IsDraft })
	}
	if s.forceFlat {
		s.grouped = false
		s.shown = idx
		return
	}
	if len(s.catOrder) > 0 {
		s.grouped = true
		s.shown = groupByCategory(s.prs, idx, s.cats, s.catOrder)
		return
	}
	if s.forceGroup || distinctAuthors(s.prs, idx) >= 2 {
		s.grouped = true
		s.shown = groupByAuthor(s.prs, idx, s.state)
		return
	}
	s.grouped = false
	s.shown = idx
}

// groupByCategory reorders idx so rows cluster under their category in header order.
func groupByCategory(prs []gh.PR, idx []int, cats map[int]string, order []string) []int {
	out := make([]int, 0, len(idx))
	for _, cat := range order {
		for _, i := range idx {
			if cats[prs[i].Number] == cat {
				out = append(out, i)
			}
		}
	}
	return out
}

func distinctAuthors(prs []gh.PR, idx []int) int {
	seen := map[string]struct{}{}
	for _, i := range idx {
		seen[prs[i].Author.Login] = struct{}{}
	}
	return len(seen)
}

// groupByAuthor reorders idx so each author's rows are contiguous; within a group
// the incoming order is preserved. Group order depends on state: the open board
// leads with each author's best (lowest) member rank, ties by login. Terminal
// boards (merged/closed) have no meaningful rank, so groups keep first-appearance
// order — and since idx arrives newest-event-first, that leads with whichever
// author has the newest merge/close, extending newest-first across groups.
func groupByAuthor(prs []gh.PR, idx []int, state string) []int {
	groups := map[string][]int{}
	authors := make([]string, 0) // first-appearance order
	for _, i := range idx {
		a := prs[i].Author.Login
		if _, ok := groups[a]; !ok {
			authors = append(authors, a)
		}
		groups[a] = append(groups[a], i)
	}
	if state != "merged" && state != "closed" {
		best := map[string]int{}
		for a, g := range groups {
			best[a] = prRank(prs[g[0]])
			for _, i := range g {
				if r := prRank(prs[i]); r < best[a] {
					best[a] = r
				}
			}
		}
		slices.SortStableFunc(authors, func(x, y string) int {
			if best[x] != best[y] {
				return best[x] - best[y]
			}
			return strings.Compare(x, y)
		})
	}
	out := make([]int, 0, len(idx))
	for _, a := range authors {
		out = append(out, groups[a]...)
	}
	return out
}

// --- Issue section ---
type IssueSection struct {
	filter string
	issues []gh.Issue
	shown  []int
}

func NewIssueSection(filter string) *IssueSection { return &IssueSection{filter: filter} }
func (s *IssueSection) Kind() string              { return "issue" }
func (s *IssueSection) Filter() string            { return s.filter }
func (s *IssueSection) SetIssues(is []gh.Issue)   { s.issues = is; s.shown = allIdx(len(is)) }
func (s *IssueSection) Len() int                  { return len(s.shown) }
func (s *IssueSection) SetShown(idx []int)        { s.shown = idx }

// issueAt returns the gh.Issue at shown-row i (mirrors prAt).
func (s *IssueSection) issueAt(i int) gh.Issue { return s.issues[s.shown[i]] }

func (s *IssueSection) RenderRow(i int, o RowOpts) string {
	is := s.issues[s.shown[i]]
	return renderItemRow(o, issueAccentStyle, fmt.Sprintf("#%d", is.Number), is.Title, "",
		is.Author.Login, ageString(is.UpdatedAt), "", "", "", "")
}

func (s *IssueSection) VarsAt(i int) action.Vars {
	is := s.issues[s.shown[i]]
	return action.Vars{Number: is.Number, Title: is.Title, Author: is.Author.Login,
		URL: is.URL, Branch: issue.Branch(is.Number, is.Title, labelSlice(is.Labels))}
}
func (s *IssueSection) Haystacks() []string {
	h := make([]string, len(s.issues))
	for i, is := range s.issues {
		h[i] = fmt.Sprintf("#%d %s %s %s", is.Number, is.Title, is.Author.Login, labelNames(is.Labels))
	}
	return h
}

func allIdx(n int) []int {
	r := make([]int, n)
	for i := range r {
		r[i] = i
	}
	return r
}
func labelNames(ls []gh.Label) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return joinSpace(out)
}
func labelSlice(ls []gh.Label) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}
func joinSpace(s []string) string { return strings.Join(s, " ") }

// oneCell renders s as exactly one display cell, substituting a space when it has
// none. Guards the row's column grid against markers that are non-empty strings
// but zero-width on screen — a styled empty glyph const is the classic case.
func oneCell(s string) string {
	if lipgloss.Width(s) == 0 {
		return " "
	}
	return s
}

// landedTag suffixes the title of a PR merged during this session; without it a
// merge glyph on the open board reads as a live PR. ASCII, so len is its width.
const landedTag = " landed"

// renderItemRow renders one dense row:
//
//	‹bar› ‹ci› ‹rv› ‹auto› ‹!› ‹num› ‹title…›            ‹author›  ‹age›
func renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, ticket, author, age, diff, ci, review, auto string) string {
	w := o.Width
	if w < 24 {
		w = 24 // floor keeps truncation sane before the first WindowSizeMsg
	}
	// One cell, two states that want it: selection wins, because it is what an
	// action fires against. Focus still reads via the row background and the bold
	// title further down.
	bar := " "
	switch {
	case o.Selected:
		bar = selMarkStyle.Render(selBarGlyph)
	case o.Focused:
		bar = focusBarStyle.Render(focusBarGlyph)
	}
	if lipgloss.Width(ci) == 0 {
		ci = dimStyle.Render("·")
	}
	if lipgloss.Width(review) == 0 {
		review = dimStyle.Render("·")
	}
	flag, auto := oneCell(o.Flag), oneCell(auto)
	numCell := num
	if o.NumWidth > 0 {
		numCell = padNum(num, o.NumWidth)
	}
	gutter := bar + ci + " " + review + " " + auto + " " + flag + " "
	// Tree slot: after the state glyphs, so a stacked row's ci/rv/auto/flag stay
	// on the same columns as every other row's.
	const treeW = 3
	// Clip before padding: the slot only freezes the grid if it is a hard 3 cells.
	// ansi.Truncate, not truncate — o.Tree arrives styled.
	tree := ansi.Truncate(o.Tree, treeW, "")
	if pad := treeW - lipgloss.Width(tree); pad > 0 {
		tree += strings.Repeat(" ", pad)
	}
	left := gutter + tree + numStyle.Render(numCell) + " "
	leftW := lipgloss.Width(left)

	// The author gets what's left after the fixed columns, never a fixed
	// fraction of w: a width-derived floor grants cells that may not exist and
	// the row then grows past w instead of shrinking (both floors below sit at
	// 1 and nothing shrinks leftW/rightW back). 17 caps it at the column width
	// Task 8 settles on. At very narrow widths the author drops out entirely,
	// which is what the responsive ladder would do anyway.
	//
	// slack is the whole budget the title, author, diffstat and landed tag share:
	// 5 = the "  NNN" age suffix, 2 = the title/right separators, 1 = a minimum
	// title cell. Every optional column is carved out of this one number so the
	// gap below never has to be floored — a floored gap is overflow, not slack.
	//
	// Neither the diffstat nor the tag is truncatable like the author (there's no
	// useful partial rendering of "+412 -18", and " landed" clipped is a lie), so
	// once even an empty author can't make room they drop out entirely rather
	// than push the row past w — same responsive-ladder degradation the author
	// gets above. diffExtra also reserves the diffstat's own "  " separator.
	//
	// authorStyle hashes the login for a stable per-person hue, so it must see
	// the FULL login; only the rendered text is truncated.
	slack := w - leftW - 5 - 2 - 1
	tagW := 0
	if o.Landed && slack-len(landedTag) >= 0 {
		tagW = len(landedTag)
	}
	diffExtra := 0
	if o.DiffWidth > 0 && slack-tagW-2-o.DiffWidth >= 0 {
		diffExtra = 2 + o.DiffWidth
	}
	authorCap := min(17, max(0, slack-tagW-diffExtra))
	right := ""
	if o.TicketWidth > 0 {
		right = sectionLabelStyle.Render(ticket) +
			strings.Repeat(" ", o.TicketWidth-lipgloss.Width(ticket)) + "  "
	}
	right += authorStyle(author).Render(truncate(author, authorCap))
	if diffExtra > 0 {
		// Clamp before padding: DiffWidth is the column, so a diffstat wider than
		// it would render at natural width and push the row past w.
		// ansi.Truncate, not truncate — diff arrives styled.
		diff = ansi.Truncate(diff, o.DiffWidth, "")
		right += "  " + strings.Repeat(" ", o.DiffWidth-lipgloss.Width(diff)) + diff // right-aligned in a fixed column
	}
	right += dimStyle.Render(fmt.Sprintf("  %3s", age))
	rightW := lipgloss.Width(right)

	titleRoom := w - leftW - rightW - 2 - tagW // -2: title/right separators
	if titleRoom < 1 {
		titleRoom = 1
	}
	titleSt := titleStyle
	switch {
	case o.Focused:
		titleSt = titleSt.Bold(true) // the hovered row is always readable, even if draft
	case o.Draft:
		titleSt = dimStyle
	}
	tags := ""
	if tagW > 0 {
		tags = dimStyle.Render(landedTag)
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom)) + tags

	gap := w - leftW - lipgloss.Width(titleTxt) - rightW
	if gap < 1 {
		gap = 1
	}
	line := left + titleTxt + strings.Repeat(" ", gap) + right
	if o.Focused {
		line = rowBgWrap(line, theme.RowBg)
	}
	return line
}

// rowBgWrap fills a composed row with a background. lipgloss ends each styled
// segment with a full SGR reset, which also clears the background, so a single
// outer Background paints only the first token and the trailing pad. We instead
// re-apply the background's opening sequence (taken from lipgloss, so it honors
// the active color profile) after every reset, filling the whole line.
func rowBgWrap(line, bg string) string {
	probe := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render("X")
	set := probe[:strings.Index(probe, "X")]
	const reset = "\x1b[m"
	return set + strings.ReplaceAll(line, reset, reset+set) + reset
}

// abbrevCount renders a change count, shortening at 1000 so a 5-digit diff can't
// blow the column: 999 -> "999", 1000 -> "1k", 1600 -> "1.6k".
func abbrevCount(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	v := float64(n) / 1000
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + "k"
}

// diffstat renders "+412 -18" with colour on the numbers only — the signs and
// the space stay unstyled so the pair reads as one value.
func diffstat(add, del int) string {
	return passStyle.Render("+"+abbrevCount(add)) + " " + failStyle.Render("-"+abbrevCount(del))
}

// diffstatWidth is the cell width of the diffstat column: the widest rendering
// across the shown set, so the age column never shifts between rows.
func diffstatWidth(s Section) int {
	ps, ok := s.(*PRSection)
	if !ok {
		return 0
	}
	w := 0
	for _, i := range ps.shown {
		w = max(w, lipgloss.Width(diffstat(ps.prs[i].Additions, ps.prs[i].Deletions)))
	}
	return w
}

// padNum right-aligns a plain "#123" string to w cells; never truncates.
func padNum(num string, w int) string {
	if len(num) >= w {
		return num
	}
	return strings.Repeat(" ", w-len(num)) + num
}

// columnWidths returns the cell width for the number column: the widest "#N"
// across the shown set, floored at 4 ("#999").
func columnWidths(s Section) int {
	w := 4
	switch x := s.(type) {
	case *PRSection:
		for _, i := range x.shown {
			w = max(w, len(fmt.Sprintf("#%d", x.prs[i].Number)))
		}
	case *IssueSection:
		for _, i := range x.shown {
			w = max(w, len(fmt.Sprintf("#%d", x.issues[i].Number)))
		}
	}
	return w
}

// truncate shortens a plain (unstyled) string to at most w display cells, adding
// an ellipsis when it cuts. Wide (CJK) runes count as two cells, so the result
// never exceeds w cells even for double-width text. Safe only for plain text
// (the row title/meta).
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	// Keep as many leading runes as fit in w-1 cells, reserving 1 for the ellipsis.
	budget, used := w-1, 0
	var b strings.Builder
	for _, r := range s {
		cw := lipgloss.Width(string(r))
		if used+cw > budget {
			break
		}
		b.WriteRune(r)
		used += cw
	}
	return b.String() + "…"
}

// renderChips renders labels as rounded color pills, packed into maxW cells and
// summarised with a "+N" when they don't all fit. The total rendered width never
// exceeds maxW — including the "+N" suffix, which is budgeted too (a caller that
// clamps a frame to maxW, e.g. the expanded rail, relies on this).
func renderChips(labels []gh.Label, maxW int) string {
	if len(labels) == 0 || maxW < 3 {
		return ""
	}
	// Greedily pack chips into maxW.
	widths := make([]int, 0, len(labels))
	rendered := make([]string, 0, len(labels))
	used := 0
	for _, l := range labels {
		chip := labelChip(l.Name, l.Color)
		cw := lipgloss.Width(chip)
		sep := 0
		if len(rendered) > 0 {
			sep = 1
		}
		if used+sep+cw > maxW {
			break
		}
		rendered = append(rendered, chip)
		widths = append(widths, cw)
		used += sep + cw
	}
	// When some labels are hidden, a " +N" suffix must also fit within maxW; drop
	// trailing chips until it does (dropping raises N, so recompute each time).
	for len(rendered) < len(labels) {
		suffix := dimStyle.Render(fmt.Sprintf(" +%d", len(labels)-len(rendered)))
		if used+lipgloss.Width(suffix) <= maxW {
			break
		}
		if len(rendered) == 0 {
			return "" // not even one chip plus its overflow marker fits
		}
		sep := 0
		if len(rendered) > 1 {
			sep = 1
		}
		used -= sep + widths[len(widths)-1]
		rendered = rendered[:len(rendered)-1]
		widths = widths[:len(widths)-1]
	}
	if len(rendered) == 0 {
		return ""
	}
	var b strings.Builder
	for i, chip := range rendered {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(chip)
	}
	if len(rendered) < len(labels) {
		b.WriteString(dimStyle.Render(fmt.Sprintf(" +%d", len(labels)-len(rendered))))
	}
	return b.String()
}

// reviewDot is the single-glyph review-decision marker for the dense board row.
// Approved uses reviewApprovedGlyph (not ✓) so it reads distinctly from the CI
// pass mark in the adjacent column.
func reviewDot(decision string) string {
	switch decision {
	case "APPROVED":
		return passStyle.Render(reviewApprovedGlyph)
	case "CHANGES_REQUESTED":
		return failStyle.Render("✗")
	case "REVIEW_REQUIRED":
		return pendStyle.Render("●")
	default:
		return dimStyle.Render("·")
	}
}

// groupHeader is an author divider: the login (bold, in its hue) + a short rule
// — never the full row width. Visual-only; never a selectable cursor target.
func groupHeader(author string, width int) string {
	name := authorStyle(author).Bold(true).Render(author)
	ruleLen := 6
	if max := width - lipgloss.Width(name) - 1; ruleLen > max {
		ruleLen = max
	}
	if ruleLen < 0 {
		ruleLen = 0
	}
	return name + " " + sepStyle.Render(strings.Repeat("─", ruleLen))
}

func ageString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
