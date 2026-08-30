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
	Width        int
	NumWidth     int // cell width for the right-aligned number column (0 = natural)
	Focused      bool
	Selected     bool
	Draft        bool   // dim the title; drafts sort last (see sortPRs)
	Landed       bool   // merged by prdash this session, held on the open board until ctrl+r
	Commented    bool   // viewer's latest review is a comment; the review column shows ◐ instead of the decision dot
	Flag         string // pre-rendered ! column glyph (conflict/behind), "" when unknown
	Tree         string // stack chain glyph, rendered between the gutter and the number
	StackMissing string // complete dim ⧉+N marker, rendered in the title/tag budget
	DiffWidth    int    // cell width of the diffstat column; 0 hides it
	TicketWidth  int    // cell width of the ticket-id column; 0 hides it
	AuthorWidth  int    // fixed cell width for the author column; 0 = natural width (no padding)
	CompactDiff  bool   // render ±N instead of +N -N
	Initials     bool   // 2-char author initials instead of the login
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

// grouper is a section that paints group headers. groupLabelAt keys the header
// divider; unitLabelAt keys the tightest selectable cluster inside it. On the issue
// board the two coincide — a category has no author sub-cluster.
type grouper interface {
	isGrouped() bool
	groupLabelAt(i int) string
	unitLabelAt(i int) string
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

	// Stack rendering is derived from the current shown set: an open board can
	// be missing merged links, so the lowest visible position is its root.
	stackRoots   map[int]int // PR number → lowest visible PR number in its stack
	stackTrees   map[int]string
	stackMissing map[int]string
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
	if root, ok := s.stackRoots[p.Number]; ok {
		p = s.prByNumber(root)
	}
	if len(s.catOrder) > 0 {
		return s.cats[p.Number]
	}
	return p.Author.Login
}

// unitLabel is the tightest selectable unit a row belongs to: its author cluster
// within its category. Distinct from groupLabel, which is the category (or the
// author, on an uncategorized board).
//
// The NUL separator keeps a category named like an author from colliding with
// one. #89 makes this return the stack for stacked rows.
func (s *PRSection) unitLabel(i int) string {
	p := s.prAt(i)
	if p.Stack != nil {
		return "stack:" + strconv.Itoa(p.Stack.Number)
	}
	return s.groupLabel(i) + "\x00" + p.Author.Login
}

// isGrouped, groupLabelAt, unitLabelAt satisfy grouper — wrappers over the
// existing grouped field/groupLabel/unitLabel, not renames (a sibling worker
// holds #89 in this file and owns unitLabel).
func (s *PRSection) isGrouped() bool           { return s.grouped }
func (s *PRSection) groupLabelAt(i int) string { return s.groupLabel(i) }
func (s *PRSection) unitLabelAt(i int) string  { return s.unitLabel(i) }

func (s *PRSection) Len() int           { return len(s.shown) }
func (s *PRSection) SetShown(idx []int) { s.setShownOrdered(idx) }

// prAt returns the gh.PR at shown-row i (for triage, which needs list fields).
func (s *PRSection) prAt(i int) gh.PR { return s.prs[s.shown[i]] }

// stackParentNumber returns the immediate predecessor when it is visible in the
// current board. A merged predecessor is absent from an open board, so a later
// link becomes its visible root instead of inheriting a stale blocker.
func (s *PRSection) stackParentNumber(i int) int {
	p := s.prAt(i)
	if p.Stack == nil || p.StackPosition <= 1 {
		return 0
	}
	for _, j := range s.shown {
		parent := s.prs[j]
		if parent.State != "MERGED" && parent.Stack != nil && parent.Stack.Number == p.Stack.Number && parent.StackPosition == p.StackPosition-1 {
			return parent.Number
		}
	}
	return 0
}

func (s *PRSection) prByNumber(number int) gh.PR {
	for _, p := range s.prs {
		if p.Number == number {
			return p
		}
	}
	return gh.PR{}
}

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
// does not re-sort — the board sorts by PR number, which a CI update never
// changes, but re-sorting on every background beat would still cost a full
// re-render for nothing. Order settles on the next real fetch.
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
	if tree := s.stackTrees[p.Number]; tree != "" {
		o.Tree = tree
	}
	o.StackMissing = s.stackMissing[p.Number]
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
		if o.CompactDiff {
			diff = diffstatCompact(p.Additions, p.Deletions)
		} else {
			diff = diffstat(p.Additions, p.Deletions)
		}
	}
	ticket := ""
	if o.TicketWidth > 0 {
		ticket = ticketID(p.HeadRefName)
	}
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title, ticket,
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

// sortPRs orders the board. Terminal states are chronological (newest event
// first). The open board sorts by PR number instead of actionability — see the
// default arm below for why.
func sortPRs(prs []gh.PR, state string) {
	switch state {
	case "merged":
		slices.SortStableFunc(prs, func(a, b gh.PR) int { return b.MergedAt.Compare(a.MergedAt) })
	case "closed":
		slices.SortStableFunc(prs, func(a, b gh.PR) int { return b.ClosedAt.Compare(a.ClosedAt) })
	default:
		// Number descending, drafts last. NOT actionability: ranking by CI state
		// means a finishing check reorders rows under the cursor (#62). The
		// categories already carry coarse urgency, and the gutter glyphs carry
		// the rest.
		slices.SortStableFunc(prs, func(a, b gh.PR) int {
			if a.IsDraft != b.IsDraft {
				if a.IsDraft {
					return 1
				}
				return -1
			}
			return b.Number - a.Number
		})
	}
}

// setShownOrdered records the shown subset in display order and decides grouping.
// idx arrives in number order (prs is number-sorted; idx preserves it). With
// ≥2 distinct authors the rows are regrouped contiguously by author so the cursor
// still walks them top-to-bottom; with one author the flat number order stands.
func (s *PRSection) SetHideDrafts(v bool) { s.hideDrafts = v }
func (s *PRSection) SetForceGroup(v bool) { s.forceGroup = v }
func (s *PRSection) SetForceFlat(v bool)  { s.forceFlat = v }

// SetState records the view state so the next SetPRs/SetCategorized sorts by the
// right key (merge/close time for terminal states, number descending for open).
func (s *PRSection) SetState(state string) { s.state = state }

func (s *PRSection) setShownOrdered(idx []int) {
	idx = expandStackMembers(s.prs, idx)
	if s.hideDrafts {
		idx = slices.DeleteFunc(slices.Clone(idx), func(i int) bool {
			return s.prs[i].IsDraft && s.prs[i].Stack == nil
		})
	}
	units := stackUnits(s.prs, idx)
	if s.forceFlat {
		s.grouped = false
		s.setShownStacks(flattenUnits(units))
		return
	}
	if len(s.catOrder) > 0 {
		s.grouped = true
		s.setShownStacks(groupStackUnits(s.prs, units, s.cats, s.catOrder, s.state))
		return
	}
	if s.forceGroup || distinctAuthors(s.prs, idx) >= 2 {
		s.grouped = true
		s.setShownStacks(groupStackUnits(s.prs, units, nil, nil, s.state))
		return
	}
	s.grouped = false
	s.setShownStacks(flattenUnits(units))
}

// expandStackMembers keeps a fuzzy hit from making its chain look shorter than
// it is. A stack is a display unit, so matching any link shows every link the
// section holds before grouping, connector selection, and triage run.
func expandStackMembers(prs []gh.PR, idx []int) []int {
	matched := map[int]bool{}
	for _, i := range idx {
		if p := prs[i]; p.Stack != nil {
			matched[p.Stack.Number] = true
		}
	}
	if len(matched) == 0 {
		return idx
	}
	seen := make(map[int]bool, len(idx))
	out := make([]int, 0, len(prs))
	for _, i := range idx {
		seen[i] = true
		out = append(out, i)
	}
	for i, p := range prs {
		if !seen[i] && p.Stack != nil && matched[p.Stack.Number] {
			out = append(out, i)
		}
	}
	return out
}

// stackUnits keeps every visible stack contiguous and positions its members
// bottom-up. Its first occurrence in idx determines where the stack unit sits
// among ordinary PRs.
func stackUnits(prs []gh.PR, idx []int) [][]int {
	byStack := map[int][]int{}
	units := make([][]int, 0, len(idx))
	seen := map[int]bool{}
	for _, i := range idx {
		p := prs[i]
		if p.Stack == nil {
			units = append(units, []int{i})
			continue
		}
		byStack[p.Stack.Number] = append(byStack[p.Stack.Number], i)
	}
	for _, i := range idx {
		p := prs[i]
		if p.Stack == nil || seen[p.Stack.Number] {
			continue
		}
		seen[p.Stack.Number] = true
		members := byStack[p.Stack.Number]
		slices.SortStableFunc(members, func(a, b int) int {
			return prs[a].StackPosition - prs[b].StackPosition
		})
		units = append(units, members)
	}
	// The loops above collect unstacked PRs before stacks; restore each unit's
	// input rank so an ordinary row and a stack share the board's existing order.
	rank := map[int]int{}
	for n, i := range idx {
		rank[i] = n
	}
	slices.SortStableFunc(units, func(a, b []int) int {
		minRank := func(unit []int) int {
			r := len(idx)
			for _, i := range unit {
				r = min(r, rank[i])
			}
			return r
		}
		return minRank(a) - minRank(b)
	})
	return units
}

func flattenUnits(units [][]int) []int {
	out := make([]int, 0)
	for _, unit := range units {
		out = append(out, unit...)
	}
	return out
}

// groupStackUnits uses the root's category and author. A stack is therefore one
// unit even when its links cross categories or authors.
func groupStackUnits(prs []gh.PR, units [][]int, cats map[int]string, order []string, state string) []int {
	groups := map[string][][]int{}
	keys := make([]string, 0)
	for _, unit := range units {
		root := prs[unit[0]]
		cat := ""
		if len(order) > 0 {
			cat = cats[root.Number]
		}
		key := cat + "\x00" + root.Author.Login
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], unit)
	}
	if state != "merged" && state != "closed" {
		best := func(key string) int {
			n := 0
			for _, unit := range groups[key] {
				for _, i := range unit {
					n = max(n, prs[i].Number)
				}
			}
			return n
		}
		slices.SortStableFunc(keys, func(a, b string) int {
			if len(order) > 0 {
				catRank := func(key string) int {
					cat := strings.SplitN(key, "\x00", 2)[0]
					for i, want := range order {
						if cat == want {
							return i
						}
					}
					return len(order)
				}
				if catRank(a) != catRank(b) {
					return catRank(a) - catRank(b)
				}
			}
			if best(a) != best(b) {
				return best(b) - best(a)
			}
			return strings.Compare(a, b)
		})
	}
	out := make([]int, 0)
	for _, key := range keys {
		out = append(out, flattenUnits(groups[key])...)
	}
	return out
}

func (s *PRSection) setShownStacks(shown []int) {
	s.shown = shown
	s.stackRoots = map[int]int{}
	s.stackTrees = map[int]string{}
	s.stackMissing = map[int]string{}
	byStack := map[int][]int{}
	for _, i := range shown {
		if p := s.prs[i]; p.Stack != nil {
			byStack[p.Stack.Number] = append(byStack[p.Stack.Number], i)
		}
	}
	for _, members := range byStack {
		root := s.prs[members[0]]
		missing := root.Stack.Size - len(members)
		for n, i := range members {
			p := s.prs[i]
			s.stackRoots[p.Number] = root.Number
			switch {
			case n == 0 && missing > 0:
				s.stackTrees[p.Number] = "⧉"
				s.stackMissing[p.Number] = "⧉+" + strconv.Itoa(missing)
			case n == 0:
				s.stackTrees[p.Number] = "⧉"
			case n == len(members)-1:
				s.stackTrees[p.Number] = "╰─"
			default:
				s.stackTrees[p.Number] = "├─"
			}
		}
	}
}

// groupByCategory reorders idx so rows cluster under their category in header
// order, and clusters by author within each category. Composing the two keeps
// one ordering concept rather than adding a third.
func groupByCategory(prs []gh.PR, idx []int, cats map[int]string, order []string, state string) []int {
	out := make([]int, 0, len(idx))
	for _, cat := range order {
		members := make([]int, 0, len(idx))
		for _, i := range idx {
			if cats[prs[i].Number] == cat {
				members = append(members, i)
			}
		}
		out = append(out, groupByAuthor(prs, members, state)...)
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
// leads with each author's highest PR number, ties by login. On terminal boards
// (merged/closed) a highest-number lead would fight the chronology, so groups
// keep first-appearance order — and since idx arrives newest-event-first, that
// leads with whichever author has the newest merge/close, extending newest-first
// across groups.
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
		// A cluster leads with its newest PR, so cluster position is fixed by
		// numbers that never change — the whole point of #62.
		best := map[string]int{}
		for a, g := range groups {
			for _, i := range g {
				best[a] = max(best[a], prs[i].Number)
			}
		}
		slices.SortStableFunc(authors, func(x, y string) int {
			if best[x] != best[y] {
				return best[y] - best[x] // descending
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
	filter    string
	issues    []gh.Issue
	shown     []int
	forceFlat bool // suppress grouping — keep the incoming (fuzzy rank) order
	grouped   bool // true when the board renders group headers (see setShownOrdered)

	cats     map[int]string // issue number → category label (Mine, Others)
	catOrder []string       // category header order
}

func NewIssueSection(filter string) *IssueSection { return &IssueSection{filter: filter} }
func (s *IssueSection) Kind() string              { return "issue" }
func (s *IssueSection) Filter() string            { return s.filter }
func (s *IssueSection) Len() int                  { return len(s.shown) }
func (s *IssueSection) SetForceFlat(v bool)       { s.forceFlat = v }

// sortIssues orders by issue number descending, one arm only — issues have no
// draft axis and no ClosedAt field, so sortPRs' state switch has no analogue.
// A key that changes under the cursor moves rows while you read them (#62).
func sortIssues(is []gh.Issue) {
	slices.SortStableFunc(is, func(a, b gh.Issue) int { return b.Number - a.Number })
}

func (s *IssueSection) SetIssues(is []gh.Issue) {
	s.cats, s.catOrder = nil, nil // flat; SetCategorized opts into category grouping
	sortIssues(is)
	s.issues = is
	s.setShownOrdered(allIdx(len(is)))
}

// SetCategorized paints issues grouped under category headers (order) — used by
// the open issue board (Mine / Others).
func (s *IssueSection) SetCategorized(is []gh.Issue, cats map[int]string, order []string) {
	sortIssues(is)
	s.issues = is
	s.cats = cats
	s.catOrder = order
	s.setShownOrdered(allIdx(len(is)))
}

// setShownOrdered records the shown subset in display order and decides grouping.
// grouped is assigned on every exit — never derived as len(catOrder) > 0 — because
// forceFlat can be true while catOrder is still populated (a "/" query on a
// categorized board), and a derived value would paint interleaved
// Mine/Others/Mine/Others… headers.
func (s *IssueSection) setShownOrdered(idx []int) {
	if s.forceFlat || len(s.catOrder) == 0 {
		s.grouped = false
		s.shown = idx
		return
	}
	s.grouped = true
	s.shown = groupIssuesByCategory(s.issues, idx, s.cats, s.catOrder)
}

// groupIssuesByCategory reorders idx so rows cluster under their category in
// header order, preserving idx order within each category. No author
// sub-grouping — that's a PR-board affordance.
func groupIssuesByCategory(is []gh.Issue, idx []int, cats map[int]string, order []string) []int {
	out := make([]int, 0, len(idx))
	for _, cat := range order {
		for _, i := range idx {
			if cats[is[i].Number] == cat {
				out = append(out, i)
			}
		}
	}
	return out
}

func (s *IssueSection) SetShown(idx []int) { s.setShownOrdered(idx) }

// issueAt returns the gh.Issue at shown-row i (mirrors prAt).
func (s *IssueSection) issueAt(i int) gh.Issue { return s.issues[s.shown[i]] }

// isGrouped, groupLabelAt, unitLabelAt satisfy grouper. Group and unit coincide
// here — a category has no author sub-cluster.
func (s *IssueSection) isGrouped() bool           { return s.grouped }
func (s *IssueSection) groupLabelAt(i int) string { return s.cats[s.issueAt(i).Number] }
func (s *IssueSection) unitLabelAt(i int) string  { return s.groupLabelAt(i) }

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
//
// authorColMax caps the author column so one long login can't starve the title.
const authorColMax = 17

// rightCols holds the reserved cell widths of a row's right-hand columns —
// including each column's leading "  " separator for diff/ticket, and the "  "
// prefix for age. The data row and the column-header row both lay out from these
// widths, so the header aligns with every row by construction rather than by a
// second copy of the arithmetic.
type rightCols struct {
	tag, diff, ticket, author, age int
}

// reserveRightCols carves the optional right-hand columns out of one slack
// budget so the gap between title and columns never has to be floored — a
// floored gap is overflow, not slack. Order matters: the ticket is reserved
// after the diffstat because columnLadder sheds it at a wider column, so on the
// way down the ticket goes first without a tie-break.
//
// authorW is the requested author column: 0 means auto (take the leftover slack,
// capped — the natural-width behaviour), a positive value means a fixed column
// sized to the widest shown author so every row's columns land at the same cell.
// Either way the author is the last to be carved and shrinks toward empty before
// the fixed columns drop, so the title never starves.
func reserveRightCols(w, leftW, ageW, diffW, tktW, authorW, tagW int) rightCols {
	slack := w - leftW - ageW - 2 - 1
	c := rightCols{age: ageW}
	if tagW > 0 && slack-tagW >= 0 {
		c.tag = tagW
	}
	if diffW > 0 && slack-c.tag-2-diffW >= 0 {
		c.diff = 2 + diffW
	}
	if tktW > 0 && slack-c.tag-c.diff-2-tktW >= 0 {
		c.ticket = 2 + tktW
	}
	avail := max(0, slack-c.tag-c.diff-c.ticket)
	if authorW > 0 {
		c.author = min(authorW, avail)
	} else {
		c.author = min(authorColMax, avail)
	}
	return c
}

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
	// 1 and nothing shrinks leftW/rightW back). 17 caps the full-login column. At
	// very narrow widths the author drops out entirely, which is what the
	// responsive ladder would do anyway.
	//
	// slack is the whole budget the title, author, diffstat and landed tag share:
	// ageW = the age suffix, 2 = the title/right separators, 1 = a minimum title
	// cell. Every optional column is carved out of this one number so the gap
	// below never has to be floored — a floored gap is overflow, not slack.
	//
	// ageW must be measured, not a literal: ageString's days branch is unbounded,
	// so the merged and closed views (which age from MergedAt/ClosedAt) reach 4
	// and 5 cells, and a short reservation over-commits every gate below.
	//
	// Neither the diffstat, the ticket id, nor the tag is truncatable like the
	// author (there's no useful partial rendering of "+412 -18", a half "ENG-77…"
	// is worse than absent, and " landed" clipped is a lie), so once even an
	// empty author can't make room they drop out entirely rather than push the
	// row past w — same responsive-ladder degradation the author gets above.
	// diffExtra/tktExtra also reserve their own "  " separator.
	//
	// The ticket id is reserved after the diffstat: columnLadder sheds it at a
	// wider column (ladderDropTicket) than the diffstat (ladderDropDiff), so on
	// the way down the ticket is always the first of the two to go — reserving it
	// last makes that the natural outcome instead of something a tie-break has to
	// enforce.
	//
	// authorStyle hashes the login for a stable per-person hue, so it must see
	// the FULL login; only the rendered text is truncated or cut to initials.
	ageW := 2 + max(3, lipgloss.Width(age)) // matches the age suffix rendered below
	tag := ""
	if o.Landed {
		tag += landedTag
	}
	if o.StackMissing != "" {
		tag += " " + o.StackMissing
	}
	cols := reserveRightCols(w, leftW, ageW, o.DiffWidth, o.TicketWidth, o.AuthorWidth, lipgloss.Width(tag))
	tagW, diffExtra, tktExtra, authorCap := cols.tag, cols.diff, cols.ticket, cols.author
	right := ""
	if tktExtra > 0 {
		// Clamp before padding, exactly as the diffstat does below: TicketWidth is
		// the column, so a wider id would render at natural width and push the row
		// past w — here it would ask strings.Repeat for a negative count first.
		ticket = ansi.Truncate(ticket, o.TicketWidth, "")
		right = sectionLabelStyle.Render(ticket) +
			strings.Repeat(" ", o.TicketWidth-lipgloss.Width(ticket)) + "  "
	}
	authorTxt := author
	if o.Initials {
		authorTxt = authorInitials(author)
	}
	// authorStyle sees the full login for a stable hue; only the rendered text is
	// truncated. With a fixed AuthorWidth the slot is right-padded so the ticket,
	// diffstat and age land at the same cell on every row and the column header
	// lines up; in auto mode the author keeps its natural width.
	authorTxt = truncate(authorTxt, authorCap)
	right += authorStyle(author).Render(authorTxt)
	if o.AuthorWidth > 0 {
		right += strings.Repeat(" ", authorCap-lipgloss.Width(authorTxt))
	}
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
	if o.Focused {
		titleSt = titleSt.Bold(true) // the hovered row is always readable, even if draft
	}
	tags := ""
	if tagW > 0 {
		tags = dimStyle.Render(tag)
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom)) + tags

	gap := w - leftW - lipgloss.Width(titleTxt) - rightW
	if gap < 1 {
		gap = 1
	}
	line := left + titleTxt + strings.Repeat(" ", gap) + right
	switch {
	case o.Focused:
		line = rowBgWrap(line, theme.RowBg)
	case o.Draft:
		line = faintWrap(line) // a draft recedes as a whole row, not just a gutter glyph
	}
	return line
}

// columnHeader is the sticky label row above the board. It lays out from the
// same reserveRightCols widths the data rows use, so each label sits over its
// column. ageW is fixed at the common 3-cell age; a row with a wider age or a
// landed tag can drift by a cell, which muted labels tolerate. leftW mirrors
// renderItemRow's left block: a 9-cell gutter, a 3-cell tree slot, the number
// column, and one separating space.
func columnHeader(w, numW, diffW, tktW, authorW int) string {
	const gutterW, treeW, ageW = 9, 3, 5
	leftW := gutterW + treeW + numW + 1
	cols := reserveRightCols(w, leftW, ageW, diffW, tktW, authorW, 0)

	// Each glyph aligns the way its column's data does: the author and ticket are
	// left-aligned, the diffstat and age are right-aligned.
	leftGlyph := func(g string, width int) string {
		return g + strings.Repeat(" ", max(0, width-lipgloss.Width(g)))
	}
	rightGlyph := func(g string, width int) string {
		return strings.Repeat(" ", max(0, width-lipgloss.Width(g))) + g
	}

	// Gutter: a status glyph over the CI cell (cell 2, after the focus bar); the
	// rest of the gutter and the tree slot stay blank. Then "#" over the number.
	left := " " + statusHeadGlyph + strings.Repeat(" ", gutterW-2+treeW) +
		"#" + strings.Repeat(" ", numW-1) + " "
	right := ""
	if cols.ticket > 0 {
		right = leftGlyph(issueHeadGlyph, tktW) + "  "
	}
	right += leftGlyph(authorHeadGlyph, cols.author)
	if cols.diff > 0 {
		right += "  " + rightGlyph(deltaHeadGlyph, diffW)
	}
	right += "  " + rightGlyph(ageHeadGlyph, 3)

	// The title column carries no glyph — a row of titles needs no marker; the gap
	// between the number and the first right-hand glyph is the title's span.
	rightW := lipgloss.Width(right)
	gap := max(1, w-leftW-rightW)
	line := left + strings.Repeat(" ", gap) + right
	return dimStyle.Render(ansi.Truncate(line, w, ""))
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

// faintWrap dims a whole composed row with the SGR faint attribute. Like
// rowBgWrap, it re-applies the attribute after each of lipgloss's resets so the
// dimming survives every styled segment rather than clearing at the first one.
func faintWrap(line string) string {
	const (
		reset = "\x1b[m"
		faint = "\x1b[2m"
	)
	return faint + strings.ReplaceAll(line, reset, reset+faint) + reset
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

// diffstatCompact is the narrow form: one signed magnitude instead of a pair.
func diffstatCompact(add, del int) string {
	return passStyle.Render("±" + abbrevCount(add+del))
}

// authorInitials is the narrow form of the author column. Two letters can
// collide (asaf-s-factify and alex-smith both give "AS"), which is why the full
// login is preferred wherever it fits — here the alternative is no author.
func authorInitials(login string) string {
	parts := strings.FieldsFunc(strings.TrimPrefix(login, "app/"), func(r rune) bool {
		return r == '-' || r == '/' || r == '_'
	})
	out := make([]rune, 0, 2)
	for _, p := range parts {
		if len(out) == 2 {
			break
		}
		out = append(out, []rune(strings.ToUpper(p))[0])
	}
	return string(out)
}

// diffstatWidth is the cell width of the diffstat column: the widest rendering
// across the shown set, so the age column never shifts between rows. It must
// measure whichever form the row will draw, hence compact.
func diffstatWidth(s Section, compact bool) int {
	ps, ok := s.(*PRSection)
	if !ok {
		return 0
	}
	render := diffstat
	if compact {
		render = diffstatCompact
	}
	w := 0
	for _, i := range ps.shown {
		w = max(w, lipgloss.Width(render(ps.prs[i].Additions, ps.prs[i].Deletions)))
	}
	return w
}

// ticketWidth is the cell width of the ticket column: the widest parsed id
// across the shown set, or 0 when none parse. Blank cells are common —
// agents/… and cursor/… branches carry no id — so the column sits after the
// title, where a gap lands against ragged text instead of reading as a hole.
func ticketWidth(s Section) int {
	ps, ok := s.(*PRSection)
	if !ok {
		return 0
	}
	w := 0
	for _, i := range ps.shown {
		w = max(w, lipgloss.Width(ticketID(ps.prs[i].HeadRefName)))
	}
	return w
}

// authorColWidth is the fixed author column: the widest rendered author across
// the shown set (initials or full login, per the layout), capped at authorColMax.
// Feeds RowOpts.AuthorWidth and columnHeader so the board's author column and its
// header label share one width.
func authorColWidth(s Section, initials bool) int {
	ps, ok := s.(*PRSection)
	if !ok {
		return 0
	}
	w := 0
	for _, i := range ps.shown {
		a := ps.prs[i].Author.Login
		if initials {
			a = authorInitials(a)
		}
		w = max(w, lipgloss.Width(a))
	}
	return min(authorColMax, w)
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

// truncateLeft shortens plain text to w cells by dropping the FRONT, prefixing
// an ellipsis. Branch names share long prefixes (eng-7726-…) and differ in the
// tail, so the tail is the part worth keeping.
func truncateLeft(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for i := range r {
		if cand := "…" + string(r[i:]); lipgloss.Width(cand) <= w {
			return cand
		}
	}
	return "…"
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

// groupHeader is an author divider: the login (in its hue, not bold) + a short
// rule — never the full row width. Kept light so the divider frames the cluster
// without competing with the rows. Visual-only; never a selectable cursor target.
func groupHeader(author string, width int) string {
	name := authorStyle(author).Render(author)
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
