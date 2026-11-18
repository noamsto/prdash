package ui

import (
	"fmt"
	"iter"
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
	Draft        bool      // dim the title; drafts sort last (see sortPRs)
	Held         bool      // gone from the latest refresh or no longer a member; dim until ctrl+r
	Tag          string    // held-row tag ("merged", "closed", "open", "left filter"); "" when not held
	Commented    bool      // viewer's latest review is a comment; the review column shows ◐ instead of the decision dot
	Flag         string    // pre-rendered ! column glyph (conflict/behind), "" when unknown
	Tree         string    // stack chain glyph, rendered between the gutter and the number
	StackMissing string    // complete dim ⧉+N marker, rendered in the title/tag budget
	DiffWidth    int       // cell width of the diffstat column; 0 hides it
	TicketWidth  int       // cell width of the ticket-id column; 0 hides it
	AuthorWidth  int       // fixed cell width for the author column; 0 = natural width (no padding)
	CompactDiff  bool      // render ±N instead of +N -N
	Initials     bool      // 2-char author initials instead of the login
	TitlePrefix  string    // plain label rendered dim before the title, inside its budget (the short repo on a flattened other-repo row)
	Now          time.Time // clock for live run elapsed times; zero shows age instead
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

// grouper is a section that paints group headers. groupLabelAt is the header
// divider's text; groupKeyAt keys the group itself (two repos can share a
// sub-section label); unitLabelAt keys the tightest selectable cluster inside
// it. On the issue board all three coincide — a category has no author
// sub-cluster.
type grouper interface {
	isGrouped() bool
	groupLabelAt(i int) string
	groupKeyAt(i int) string
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

	cats     map[gh.PRRef]string // PR → category label; non-nil switches grouping from author to category
	catOrder []string            // category header order (e.g. Mine, Review requested)
	byAuthor bool                // regroup a categorized board by author; cats stay stored for held rows

	// Repo grouping partitions the categorized board by repo. A folded group is
	// one synthetic stop in shown, encoded -(g+1) for groupRepos[g]; prs holds
	// only real PRs.
	launch       string           // launch repo; its group leads and keeps the category/author sub-grouping
	viewer       string           // splits Mine in groupStages
	folded       map[string]bool  // the Model's explicit fold state; an absent repo is folded iff not launch
	repoGrouping bool             // the shown set is partitioned by repo (see setShownOrdered)
	groupRepos   []string         // repo per group, in display order
	groupRows    map[string][]int // repo → prs indices its group shows, or would show unfolded

	// Stack rendering is derived from the current shown set: an open board can
	// be missing merged links, so the lowest visible position is its root.
	stackRoots   map[gh.PRRef]int // PR → lowest visible PR number in its stack (same repo)
	stackTrees   map[gh.PRRef]string
	stackMissing map[gh.PRRef]string
}

func NewPRSection(filter string) *PRSection { return &PRSection{filter: filter} }
func (s *PRSection) Kind() string           { return "pr" }
func (s *PRSection) Filter() string         { return s.filter }
func (s *PRSection) SetPRs(p []gh.PR) {
	if p == nil {
		p = []gh.PR{}
	}
	s.cats, s.catOrder = nil, nil // flat/author grouping; SetCategorized opts into category grouping
	sortPRs(p, s.state)
	s.prs = p
	s.setShownOrdered(allIdx(len(p)))
}

// SetCategorized paints PRs grouped under category headers (order) instead of by
// author — used by the mine view (Mine / Review requested).
func (s *PRSection) SetCategorized(p []gh.PR, cats map[gh.PRRef]string, order []string) {
	if p == nil {
		p = []gh.PR{}
	}
	sortPRs(p, s.state)
	s.prs = p
	s.cats = cats
	s.catOrder = order
	s.setShownOrdered(allIdx(len(p)))
}

// groupLabel is the header key for shown-row i: category when categorized, else
// author. Other-repo groups have no sub-sections, so their rows and every stop
// have none.
func (s *PRSection) groupLabel(i int) string {
	if s.isFoldStop(i) || s.otherRepo(i) {
		return ""
	}
	p := s.prs[s.shown[i]]
	if root, ok := s.stackRoots[p.Ref()]; ok {
		p = s.prByRef(gh.PRRef{Repo: p.Repo, Number: root})
	}
	if s.categorized() {
		return s.cats[p.Ref()]
	}
	return p.Author.Login
}

// unitLabel is the tightest selectable unit a row belongs to: its stack, else
// its author cluster within its category. Distinct from groupLabel, which is the
// category (or the author, on an uncategorized board). An other-repo group is
// laid out by recency, not by author, so there an unstacked row is its own unit.
//
// The NUL separator keeps a category named like an author from colliding with
// one.
func (s *PRSection) unitLabel(i int) string {
	p := s.prAt(i)
	if k, ok := stackKey(p); ok {
		return "stack:" + k
	}
	if s.isFoldStop(i) || s.otherRepo(i) {
		return s.groupKeyAt(i) + "\x00#" + strconv.Itoa(p.Number)
	}
	return s.groupKeyAt(i) + "\x00" + p.Author.Login
}

// isGrouped, groupLabelAt, unitLabelAt satisfy grouper — wrappers over the
// existing grouped field/groupLabel/unitLabel, not renames (a sibling worker
// holds #89 in this file and owns unitLabel).
func (s *PRSection) isGrouped() bool           { return s.grouped }
func (s *PRSection) groupLabelAt(i int) string { return s.groupLabel(i) }
func (s *PRSection) groupKeyAt(i int) string   { return s.repoAt(i) + "\x00" + s.groupLabel(i) }
func (s *PRSection) unitLabelAt(i int) string  { return s.unitLabel(i) }

func (s *PRSection) Len() int           { return len(s.shown) }
func (s *PRSection) SetShown(idx []int) { s.setShownOrdered(idx) }

// prAt returns the gh.PR at shown-row i (for triage, which needs list fields).
// On a fold stop it is the empty PR of the stop's repo.
func (s *PRSection) prAt(i int) gh.PR {
	if s.isFoldStop(i) {
		return gh.PR{Repo: s.repoAt(i)}
	}
	return s.prs[s.shown[i]]
}

// shownPRs yields the shown set's PRs in display order, fold stops skipped.
func (s *PRSection) shownPRs() iter.Seq[gh.PR] {
	return func(yield func(gh.PR) bool) {
		for _, j := range s.shown {
			if j >= 0 && !yield(s.prs[j]) {
				return
			}
		}
	}
}

// SetRepoGrouping configures the repo partition the next layout applies. It is
// active only on the categorized board when some row's Repo differs from
// launch. folded is the Model's map, which outlives the section.
func (s *PRSection) SetRepoGrouping(launch, viewer string, folded map[string]bool) {
	s.launch, s.viewer, s.folded = launch, viewer, folded
}

func (s *PRSection) repoGrouped() bool { return s.repoGrouping }

// repoAt is the repo of the row or stop at shown index i.
func (s *PRSection) repoAt(i int) string {
	if j := s.shown[i]; j < 0 {
		return s.groupRepos[-j-1]
	}
	return s.prs[s.shown[i]].Repo
}

// isFoldStop is bounds-checked: callers pass a cursor that may sit off an
// emptied board.
func (s *PRSection) isFoldStop(i int) bool { return i >= 0 && i < len(s.shown) && s.shown[i] < 0 }

func (s *PRSection) isFolded(repo string) bool {
	if f, ok := s.folded[repo]; ok {
		return f
	}
	return repo != s.launch
}

// otherRepo reports whether row i sits in a repo group other than launch's.
func (s *PRSection) otherRepo(i int) bool { return s.repoGrouping && s.repoAt(i) != s.launch }

// stopIndex is the shown index of a folded group's stop.
func (s *PRSection) stopIndex(repo string) (int, bool) {
	for i, j := range s.shown {
		if j < 0 && s.groupRepos[-j-1] == repo {
			return i, true
		}
	}
	return 0, false
}

// firstRowIndex is the shown index of an unfolded group's first row.
func (s *PRSection) firstRowIndex(repo string) (int, bool) {
	for i, j := range s.shown {
		if j >= 0 && s.prs[j].Repo == repo {
			return i, true
		}
	}
	return 0, false
}

// groupStages counts repo's group by header bucket over the rows it shows, or
// would show unfolded, so a folded header still summarizes its group.
func (s *PRSection) groupStages(repo string) stageCounts {
	rows := make([]gh.PR, 0, len(s.groupRows[repo]))
	for _, j := range s.groupRows[repo] {
		rows = append(rows, s.prs[j])
	}
	return countStages(rows, s.viewer, func(p gh.PR) int { return stackParentAmong(p, slices.Values(rows)) })
}

// stackParentNumber returns the immediate predecessor when it is visible in the
// current board. A merged or closed predecessor (absent from an open board, or
// held on it) is not a live blocker, so a later link becomes its visible root
// instead of inheriting a stale one.
func (s *PRSection) stackParentNumber(i int) int {
	return stackParentAmong(s.prAt(i), s.shownPRs())
}

// stackParentAmong is p's live immediate predecessor among rows, or 0.
func stackParentAmong(p gh.PR, rows iter.Seq[gh.PR]) int {
	if p.Stack == nil || p.StackPosition <= 1 {
		return 0
	}
	for parent := range rows {
		if parent.State != "MERGED" && parent.State != "CLOSED" && parent.Stack != nil && parent.Repo == p.Repo && parent.Stack.Number == p.Stack.Number && parent.StackPosition == p.StackPosition-1 {
			return parent.Number
		}
	}
	return 0
}

func (s *PRSection) prByRef(r gh.PRRef) gh.PR {
	for _, p := range s.prs {
		if p.Ref() == r {
			return p
		}
	}
	return gh.PR{}
}

// authorOf returns the login that opened PR r, or "" when the board does not
// hold it.
func (s *PRSection) authorOf(r gh.PRRef) string {
	for _, p := range s.prs {
		if p.Ref() == r {
			return p.Author.Login
		}
	}
	return ""
}

// ApplyChecks replaces the rollup on the PRs named in checks. It deliberately
// does not re-sort — the board sorts by PR number, which a CI update never
// changes, but re-sorting on every background beat would still cost a full
// re-render for nothing. Order settles on the next real fetch.
func (s *PRSection) ApplyChecks(checks map[gh.PRRef][]gh.Check) {
	for i := range s.prs {
		if c, ok := checks[s.prs[i].Ref()]; ok {
			s.prs[i].StatusCheckRollup = c
		}
	}
}

// updatePR applies fn to the stored PR r. ok is false when the board does not
// hold it.
func (s *PRSection) updatePR(r gh.PRRef, fn func(*gh.PR)) bool {
	for i := range s.prs {
		if s.prs[i].Ref() == r {
			fn(&s.prs[i])
			return true
		}
	}
	return false
}

func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prAt(i)
	o.Draft = p.IsDraft
	if tree := s.stackTrees[p.Ref()]; tree != "" {
		o.Tree = tree
	}
	o.StackMissing = s.stackMissing[p.Ref()]
	if s.forceFlat && p.Repo != "" && p.Repo != s.launch {
		_, short, _ := strings.Cut(p.Repo, "/")
		o.TitlePrefix = short
	}
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
	if s.isFoldStop(i) {
		return action.Vars{Repo: s.repoAt(i)}
	}
	p := s.prs[s.shown[i]]
	return action.Vars{Repo: p.Repo, Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login, Branch: p.HeadRefName,
		ID: p.ID, Ticket: ticketID(p.HeadRefName)}
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
func (s *PRSection) SetByAuthor(v bool)   { s.byAuthor = v }

// categorized reports whether rows group under category headers rather than by author.
func (s *PRSection) categorized() bool { return len(s.catOrder) > 0 && !s.byAuthor }

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
	// byAuthor only regroups the launch group: other repos still follow it.
	s.repoGrouping = len(s.catOrder) > 0 && !s.forceFlat &&
		slices.ContainsFunc(idx, func(i int) bool { return s.prs[i].Repo != s.launch })
	if s.repoGrouping {
		s.setShownRepoGroups(idx)
		return
	}
	s.groupRepos, s.groupRows = nil, nil
	shown, grouped := s.orderUnits(idx)
	s.grouped = grouped
	s.setShownStacks(shown)
}

// orderUnits lays out idx as one unpartitioned board and reports whether it
// renders group headers.
func (s *PRSection) orderUnits(idx []int) (shown []int, grouped bool) {
	units := stackUnits(s.prs, idx)
	switch {
	case s.forceFlat:
		return flattenUnits(units), false
	case s.categorized():
		return groupStackUnits(s.prs, units, s.cats, s.catOrder, s.state), true
	case s.forceGroup || s.byAuthor || distinctAuthors(s.prs, idx) >= 2:
		return groupStackUnits(s.prs, units, nil, nil, s.state), true
	}
	return flattenUnits(units), false
}

// setShownRepoGroups lays out the launch repo's group as orderUnits would the
// whole board, then each other repo by its latest update: a folded group as one
// stop, an unfolded one as its stack units, newest first.
func (s *PRSection) setShownRepoGroups(idx []int) {
	s.groupRepos = []string{s.launch}
	s.groupRows = map[string][]int{}
	for _, i := range idx {
		r := s.prs[i].Repo
		if _, ok := s.groupRows[r]; !ok && r != s.launch {
			s.groupRepos = append(s.groupRepos, r)
		}
		s.groupRows[r] = append(s.groupRows[r], i)
	}
	slices.SortStableFunc(s.groupRepos[1:], func(a, b string) int {
		if c := latestUpdate(s.prs, s.groupRows[b]).Compare(latestUpdate(s.prs, s.groupRows[a])); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	var shown []int
	for g, repo := range s.groupRepos {
		rows := s.groupRows[repo]
		switch {
		case len(rows) == 0:
		case s.isFolded(repo):
			shown = append(shown, -(g + 1))
		case repo == s.launch:
			launchShown, _ := s.orderUnits(rows)
			shown = append(shown, launchShown...)
		default:
			units := stackUnits(s.prs, rows)
			slices.SortStableFunc(units, func(a, b []int) int {
				return latestUpdate(s.prs, b).Compare(latestUpdate(s.prs, a))
			})
			shown = append(shown, flattenUnits(units)...)
		}
	}
	s.grouped = true
	s.setShownStacks(shown)
}

func latestUpdate(prs []gh.PR, idx []int) time.Time {
	var t time.Time
	for _, i := range idx {
		if prs[i].UpdatedAt.After(t) {
			t = prs[i].UpdatedAt
		}
	}
	return t
}

// expandStackMembers keeps a fuzzy hit from making its chain look shorter than
// it is. A stack is a display unit, so matching any link shows every link the
// section holds before grouping, connector selection, and triage run.
func expandStackMembers(prs []gh.PR, idx []int) []int {
	matched := map[string]bool{}
	for _, i := range idx {
		if k, ok := stackKey(prs[i]); ok {
			matched[k] = true
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
		if k, ok := stackKey(p); ok && !seen[i] && matched[k] {
			out = append(out, i)
		}
	}
	return out
}

// stackUnits keeps every visible stack contiguous and positions its members
// bottom-up. Its first occurrence in idx determines where the stack unit sits
// among ordinary PRs.
func stackUnits(prs []gh.PR, idx []int) [][]int {
	byStack := map[string][]int{}
	units := make([][]int, 0, len(idx))
	seen := map[string]bool{}
	for _, i := range idx {
		k, ok := stackKey(prs[i])
		if !ok {
			units = append(units, []int{i})
			continue
		}
		byStack[k] = append(byStack[k], i)
	}
	for _, i := range idx {
		k, ok := stackKey(prs[i])
		if !ok || seen[k] {
			continue
		}
		seen[k] = true
		members := byStack[k]
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

// stackKey identifies p's stack: stack numbers are only unique within a repo.
func stackKey(p gh.PR) (key string, ok bool) {
	if p.Stack == nil {
		return "", false
	}
	return p.Repo + "#" + strconv.Itoa(p.Stack.Number), true
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
func groupStackUnits(prs []gh.PR, units [][]int, cats map[gh.PRRef]string, order []string, state string) []int {
	groups := map[string][][]int{}
	keys := make([]string, 0)
	for _, unit := range units {
		root := prs[unit[0]]
		cat := ""
		if len(order) > 0 {
			cat = cats[root.Ref()]
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
	s.stackRoots = map[gh.PRRef]int{}
	s.stackTrees = map[gh.PRRef]string{}
	s.stackMissing = map[gh.PRRef]string{}
	byStack := map[string][]int{}
	for _, i := range shown {
		if i < 0 {
			continue
		}
		if k, ok := stackKey(s.prs[i]); ok {
			byStack[k] = append(byStack[k], i)
		}
	}
	for _, members := range byStack {
		root := s.prs[members[0]]
		missing := root.Stack.Size - len(members)
		for n, i := range members {
			p := s.prs[i]
			r := p.Ref()
			s.stackRoots[r] = root.Number
			switch {
			case n == 0 && missing > 0:
				s.stackTrees[r] = stackRootGlyph
				s.stackMissing[r] = stackRootGlyph + "+" + strconv.Itoa(missing)
			case n == 0:
				s.stackTrees[r] = stackRootGlyph
			case n == len(members)-1:
				s.stackTrees[r] = stackLastGlyph
			default:
				s.stackTrees[r] = stackMidGlyph
			}
		}
	}
}

func distinctAuthors(prs []gh.PR, idx []int) int {
	seen := map[string]struct{}{}
	for _, i := range idx {
		seen[prs[i].Author.Login] = struct{}{}
	}
	return len(seen)
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
	if is == nil {
		is = []gh.Issue{}
	}
	s.cats, s.catOrder = nil, nil // flat; SetCategorized opts into category grouping
	sortIssues(is)
	s.issues = is
	s.setShownOrdered(allIdx(len(is)))
}

// SetCategorized paints issues grouped under category headers (order) — used by
// the open issue board (Mine / Others).
func (s *IssueSection) SetCategorized(is []gh.Issue, cats map[int]string, order []string) {
	if is == nil {
		is = []gh.Issue{}
	}
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
func (s *IssueSection) groupKeyAt(i int) string   { return s.groupLabelAt(i) }
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
	missing, diff, ticket, author, age int
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
func reserveRightCols(w, leftW, ageW, diffW, tktW, authorW, missingW int) rightCols {
	slack := w - leftW - ageW - 2 - 1
	c := rightCols{age: ageW}
	if missingW > 0 && slack-missingW >= 0 {
		c.missing = missingW
	}
	if diffW > 0 && slack-c.missing-2-diffW >= 0 {
		c.diff = 2 + diffW
	}
	if tktW > 0 && slack-c.missing-c.diff-2-tktW >= 0 {
		c.ticket = 2 + tktW
	}
	avail := max(0, slack-c.missing-c.diff-c.ticket)
	if authorW > 0 {
		c.author = min(authorW, avail)
	} else {
		c.author = min(authorColMax, avail)
	}
	return c
}

func renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, ticket, author, age, diff, ci, review, auto string) string {
	w := max(o.Width, 24) // floor keeps truncation sane before the first WindowSizeMsg
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
	// slack is the whole budget the title, author, diffstat and ⧉+N marker share:
	// ageW = the age suffix, 2 = the title/right separators, 1 = a minimum title
	// cell. Every optional column is carved out of this one number so the gap
	// below never has to be floored — a floored gap is overflow, not slack.
	//
	// ageW must be measured, not a literal: ageString's days branch is unbounded,
	// so the merged and closed views (which age from MergedAt/ClosedAt) reach 4
	// and 5 cells, and a short reservation over-commits every gate below.
	//
	// Neither the diffstat, the ticket id, nor a tag is truncatable like the
	// author (there's no useful partial rendering of "+412 -18", a half "ENG-77…"
	// is worse than absent, and " merged" clipped is a lie), so once even an
	// empty author can't make room they drop out entirely rather than push the
	// row past w — same responsive-ladder degradation the author gets above.
	// diffExtra/tktExtra also reserve their own "  " separator.
	//
	// The held tag is not in slack at all: it comes out of the title's room
	// after the columns are laid out, so a held row keeps the same columns at
	// the same cells as its siblings and the column header.
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
	missing := ""
	if o.StackMissing != "" {
		missing = " " + o.StackMissing
	}
	cols := reserveRightCols(w, leftW, ageW, o.DiffWidth, o.TicketWidth, o.AuthorWidth, lipgloss.Width(missing))
	missingW, diffExtra, tktExtra, authorCap := cols.missing, cols.diff, cols.ticket, cols.author
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

	titleRoom := w - leftW - rightW - 2 - missingW // -2: title/right separators
	tag := ""
	if held := " " + o.Tag; o.Tag != "" && titleRoom-lipgloss.Width(held) >= 1 {
		tag, titleRoom = held, titleRoom-lipgloss.Width(held)
	}
	if missingW > 0 {
		tag += missing
	}
	if titleRoom < 1 {
		titleRoom = 1
	}
	titleSt := titleStyle
	if o.Focused {
		titleSt = titleSt.Bold(true) // the hovered row is always readable, even if draft
	}
	tags := ""
	if tag != "" {
		tags = dimStyle.Render(tag)
	}
	prefix := ""
	if pw := lipgloss.Width(o.TitlePrefix) + 1; o.TitlePrefix != "" && titleRoom-pw >= 1 {
		prefix, titleRoom = dimStyle.Render(o.TitlePrefix+" "), titleRoom-pw
	}
	titleTxt := prefix + titleSt.Render(truncate(title, titleRoom)) + tags

	gap := max(w-leftW-lipgloss.Width(titleTxt)-rightW, 1)
	line := left + titleTxt + strings.Repeat(" ", gap) + right
	switch {
	case o.Focused:
		line = rowBgWrap(line, theme.RowBg)
	case o.Held:
		line = faintWrap(line) // gone from the latest refresh or no longer a member
	case o.Draft:
		line = faintWrap(line) // a draft recedes as a whole row, not just a gutter glyph
	}
	return line
}

// columnHeader is the sticky label row above the board. It lays out from the
// same reserveRightCols widths the data rows use, so each label sits over its
// column. ageW is fixed at the common 3-cell age; a row with a wider age or a
// ⧉+N marker can drift by a cell, which muted labels tolerate. leftW mirrors
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
	for p := range ps.shownPRs() {
		w = max(w, lipgloss.Width(render(p.Additions, p.Deletions)))
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
	for p := range ps.shownPRs() {
		w = max(w, lipgloss.Width(ticketID(p.HeadRefName)))
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
	for p := range ps.shownPRs() {
		a := p.Author.Login
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
		for p := range x.shownPRs() {
			w = max(w, len(fmt.Sprintf("#%d", p.Number)))
		}
	case *IssueSection:
		for _, i := range x.shown {
			w = max(w, len(fmt.Sprintf("#%d", x.issues[i].Number)))
		}
	}
	return w
}

// truncate shortens s to at most w display cells, adding an ellipsis when it
// cuts. It cuts on grapheme-cluster boundaries, so wide (CJK) runes and
// VS16/ZWJ emoji count their real width, and it leaves ANSI sequences intact,
// so styled input is safe too.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
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
