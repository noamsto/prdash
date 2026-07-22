package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/issue"
)

const (
	// chipRowMinWidth is the row width below which list rows show no chips —
	// the title keeps the whole flexible middle on tight rows.
	chipRowMinWidth = 72
	// chipRowMaxW caps the chip budget so labels never starve the title.
	chipRowMaxW = 24
	// chipRowMinTitle is the title floor the chip budget must never squeeze below.
	chipRowMinTitle = 16
)

// RowOpts controls how a section renders one row.
type RowOpts struct {
	Width    int
	NumWidth int // cell width for the right-aligned number column (0 = natural)
	Focused  bool
	Selected bool
	Draft    bool   // dim the title; drafts sort last (see prRank)
	Flag     string // pre-rendered ! column glyph (conflict/behind), "" when unknown
}

type Section interface {
	Kind() string
	Filter() string
	RenderRow(i int, o RowOpts) string // render shown-row i as a dense single line
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
	}
	auto := autoMergeGlyph(p.AutoMergeEnabled())
	// Author is dropped from the row: it's redundant in a single-author (flat)
	// view and hoisted into the group header when grouped.
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title,
		"", age, status, reviewDot(p.ReviewDecision), auto, p.Labels)
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
	return renderItemRow(o, issueAccentStyle, fmt.Sprintf("#%d", is.Number), is.Title,
		is.Author.Login, ageString(is.UpdatedAt), "", "", "", is.Labels)
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

// renderItemRow renders one dense board line:
//
//	‹bar›‹mark› ‹ci› ‹rv› ‹auto› ‹!› ‹num› ‹title…›            ‹author›  ‹age›
func renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review, auto string, labels []gh.Label) string {
	w := o.Width
	if w < 24 {
		w = 24 // floor keeps truncation sane before the first WindowSizeMsg
	}
	bar, mark := " ", " "
	if o.Focused {
		bar = focusBarStyle.Render("▎")
	}
	if o.Selected {
		mark = selMarkStyle.Render("●")
	}
	flag := o.Flag
	if flag == "" {
		flag = " "
	}
	if ci == "" {
		ci = dimStyle.Render("·")
	}
	if review == "" {
		review = dimStyle.Render("·")
	}
	if auto == "" {
		auto = " "
	}
	numCell := num
	if o.NumWidth > 0 {
		numCell = padNum(num, o.NumWidth)
	}
	left := bar + mark + " " + ci + " " + review + " " + auto + " " + flag + " " + numStyle.Render(numCell) + " "
	right := authorStyle(author).Render(author) + dimStyle.Render(fmt.Sprintf("  %3s", age))
	leftW, rightW := lipgloss.Width(left), lipgloss.Width(right)

	// Reserve a bounded chip budget from the flexible middle. Chips are the
	// lowest-priority content, so they elide before the title on tight rows and
	// vanish entirely below chipRowMinWidth. Placed immediately left of the
	// right (age) block.
	chips := ""
	if w >= chipRowMinWidth {
		slack := w - leftW - rightW - chipRowMinTitle - 2 // -2: title/right separators
		budget := min(chipRowMaxW, slack)
		if budget >= 3 { // renderChips floor
			chips = renderChips(labels, budget)
		}
	}
	chipSeg := ""
	if chips != "" {
		chipSeg = chips + " " // one space between chips and the right block
	}
	chipW := lipgloss.Width(chipSeg)

	titleRoom := w - leftW - rightW - chipW - 2
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
	// A draft dims the whole row but paints its tag in the draft accent (peach),
	// so the one thing that stands out on a receded row is what it is.
	draftTag := ""
	if o.Draft {
		const tag = " [draft]"
		draftTag = draftTagStyle.Render(tag)
		if titleRoom -= lipgloss.Width(tag); titleRoom < 1 {
			titleRoom = 1
		}
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom)) + draftTag

	gap := w - leftW - lipgloss.Width(titleTxt) - chipW - rightW
	if gap < 1 {
		gap = 1
	}
	line := left + titleTxt + strings.Repeat(" ", gap) + chipSeg + right
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

// reviewDot is the single-rune review-decision glyph for the dense board row.
func reviewDot(decision string) string {
	switch decision {
	case "APPROVED":
		return passStyle.Render("✓")
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
