package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// clearHeld drops all held-row state and bumps the generation, so an
// in-flight heldStatesMsg lookup issued under the old generation is dropped
// when it lands (see heldStatesCmd).
func (m *Model) clearHeld() {
	m.held = map[int]string{}
	m.heldGen++
}

// overlaySessionMerged marks a fetched OPEN PR merged when prdash has already
// seen it merge this session: GitHub's search index lags writes by seconds,
// so a lagging search result must never contradict prdash's own knowledge.
// Copy-on-write, like applyCIRerun — fetched slices may share the cache's
// backing array.
func (m *Model) overlaySessionMerged(prs []gh.PR) []gh.PR {
	if len(m.sessionMerged) == 0 {
		return prs
	}
	var out []gh.PR
	for i, p := range prs {
		t, merged := m.sessionMerged[p.Number]
		if !merged || p.State != "OPEN" {
			continue
		}
		if out == nil {
			out = make([]gh.PR, len(prs))
			copy(out, prs)
		}
		out[i].State, out[i].MergedAt = "MERGED", t
	}
	if out == nil {
		return prs
	}
	return out
}

// mergeHeldPRs returns fetched plus every previously-shown PR number it
// dropped, carried forward at its previous value (overlaid with a session
// merge, so a row prdash merged never reverts to its stale OPEN snapshot).
// fetched must already carry the session-merge overlay (overlaySessionMerged)
// — this only overlays the rows it carries forward. It also updates m.held to
// match: a departed number is (re)held; a returned number stops being held,
// except a session merge returned off its board (search still says OPEN),
// which is held with its known state. When cats is
// non-nil (the sections paints), a carried row's previous category is
// written into it so it keeps its group.
func (m *Model) mergeHeldPRs(prev []gh.PR, prevCats map[int]string, fetched []gh.PR, cats map[int]string) []gh.PR {
	boardState := strings.ToUpper(m.state)
	have := make(map[int]bool, len(fetched))
	for _, p := range fetched {
		have[p.Number] = true
		if _, merged := m.sessionMerged[p.Number]; merged && p.State != boardState {
			m.held[p.Number] = p.State
		} else {
			delete(m.held, p.Number)
		}
	}
	out := append([]gh.PR(nil), fetched...)
	for _, p := range prev {
		if have[p.Number] {
			continue
		}
		if t, merged := m.sessionMerged[p.Number]; merged {
			p.State, p.MergedAt = "MERGED", t
			m.held[p.Number] = "MERGED"
		} else if _, held := m.held[p.Number]; !held {
			m.held[p.Number] = ""
		}
		if cats != nil {
			if cat, ok := prevCats[p.Number]; ok {
				cats[p.Number] = cat
			}
		}
		out = append(out, p)
	}
	return out
}

// mergeHeldIssues mirrors mergeHeldPRs for the issue boards, without the
// session-merge overlay or the board-state rule: issues have no sessionMerged
// equivalent, so a returned issue always un-holds.
func (m *Model) mergeHeldIssues(prev []gh.Issue, prevCats map[int]string, fetched []gh.Issue, cats map[int]string) []gh.Issue {
	have := make(map[int]bool, len(fetched))
	for _, is := range fetched {
		have[is.Number] = true
		delete(m.held, is.Number)
	}
	out := append([]gh.Issue(nil), fetched...)
	for _, is := range prev {
		if have[is.Number] {
			continue
		}
		if _, held := m.held[is.Number]; !held {
			m.held[is.Number] = ""
		}
		if cats != nil {
			if cat, ok := prevCats[is.Number]; ok {
				cats[is.Number] = cat
			}
		}
		out = append(out, is)
	}
	return out
}

// heldLookupNumbers returns every held number except those prdash merged this
// session, sorted for a stable query shape. Already-looked-up rows are asked
// again: a row that left the filter can merge or close later.
func (m *Model) heldLookupNumbers() []int {
	nums := make([]int, 0, len(m.held))
	for n := range m.held {
		if _, merged := m.sessionMerged[n]; merged {
			continue
		}
		nums = append(nums, n)
	}
	slices.Sort(nums)
	return nums
}

// heldStatesCmd looks up the current GitHub state of every held row not
// already known merged this session, in one batched request. nil when no
// state backend is installed or there is nothing to look up.
func (m Model) heldStatesCmd() tea.Cmd {
	numbers := m.heldLookupNumbers()
	if m.stateSource == nil || len(numbers) == 0 {
		return nil
	}
	gen := m.heldGen
	src := m.stateSource
	return func() tea.Msg {
		states, err := src.FetchStates(numbers)
		return heldStatesMsg{gen: gen, states: states, err: err}
	}
}

// applyHeldStates records each held row's looked-up state and patches its PR
// section row — State, MergedAt, ClosedAt — without disturbing the board's
// own sort key: a merged board keeps its rows' MergedAt, a closed board its
// ClosedAt, so a lookup can never reshuffle rows the board already sorted.
func (m *Model) applyHeldStates(states map[int]gh.ItemState) {
	ps, isPR := m.section.(*PRSection)
	for n, st := range states {
		if _, held := m.held[n]; !held {
			continue
		}
		if _, merged := m.sessionMerged[n]; merged {
			continue // a lookup issued before the merge landed would revert it to OPEN
		}
		m.held[n] = st.State
		if !isPR {
			continue
		}
		ps.updatePR(n, func(p *gh.PR) {
			p.State = st.State
			if m.state != "merged" {
				p.MergedAt = st.MergedAt
			}
			if m.state != "closed" {
				p.ClosedAt = st.ClosedAt
			}
		})
	}
}

// heldTag reports whether shown row i is held and, if so, the tag to render
// (without its leading space; renderItemRow adds that).
//
// PR board: a held row, or a merged/closed one off its own board (an
// optimistic merge before any refetch), whose State differs from the board
// state is tagged with that State. A held row still at the board state has no
// tag until its lookup lands, then "left filter".
//
// Issue board: gh.Issue carries no State to compare against the board, so a
// row is held purely by membership in m.held: "left filter" once the lookup
// matches the board state, its lower-cased state otherwise, no tag pending.
//
// "left filter" is only claimed when the latest list came back short of its
// limit: every held row is one that list omitted, and a full page may have
// omitted a still-matching row just by pushing it past the limit. Such a row
// stays dim and untagged.
func (m *Model) heldTag(i int) (held bool, tag string) {
	boardState := strings.ToUpper(m.state)
	if ps, ok := m.section.(*PRSection); ok {
		p := ps.prAt(i)
		st, held := m.held[p.Number]
		terminal := p.State == "MERGED" || p.State == "CLOSED"
		switch {
		case (held || terminal) && p.State != boardState:
			return true, strings.ToLower(p.State)
		case held && st != "" && !m.heldPageFull:
			return true, "left filter"
		}
		return held, ""
	}
	is, ok := m.section.(*IssueSection)
	if !ok {
		return false, ""
	}
	st, ok := m.held[is.numberAt(i)]
	if !ok {
		return false, ""
	}
	switch {
	case st == "", st == boardState && m.heldPageFull:
		return true, ""
	case st == boardState:
		return true, "left filter"
	}
	return true, strings.ToLower(st)
}

// numbered exposes a shown row's item number without the caller needing to
// know whether the section holds PRs or issues.
type numbered interface {
	numberAt(i int) int
}

func (s *PRSection) numberAt(i int) int    { return s.prs[s.shown[i]].Number }
func (s *IssueSection) numberAt(i int) int { return s.issues[s.shown[i]].Number }

// cursorAnchor captures the cursor's identity before a repaint: the PR/issue
// number it is on (0 when the board is empty) and the full shown order, so
// restoreCursor can re-anchor it afterward even when that number itself
// departed.
func (m *Model) cursorAnchor() (num int, order []int) {
	n, ok := m.section.(numbered)
	if !ok {
		return 0, nil
	}
	l := m.section.Len()
	order = make([]int, l)
	for i := range l {
		order[i] = n.numberAt(i)
	}
	if m.cursor >= 0 && m.cursor < l {
		num = order[m.cursor]
	}
	return num, order
}

// paintAnchor is cursorAnchor for the four board paints, except while the
// cursor must stay on the top row rather than follow the row it is on (it
// returns no anchor, so restoreCursor keeps index 0): a launch or filter/tab
// switch whose live replace hasn't landed, since the cached top row it would
// follow down is not what the user asked to be on; and, on the sections
// boards (homing), while the opening jump to Mine is unspent, since anchoring
// would move the cursor off 0 and homeCursorOnMine would then spend the jump
// without taking it. Once the user moves off row 0, anchoring applies.
func (m *Model) paintAnchor(homing bool) (num int, order []int) {
	if m.cursor == 0 && (m.cursorPinnedTop || homing && !m.cursorHomed) {
		return 0, nil
	}
	return m.cursorAnchor()
}

// restoreCursor re-anchors the cursor after a repaint. num is the PR/issue
// number it was on before (0 when there wasn't one); order is the shown
// numbers in their previous order. If num is still shown, the cursor moves to
// its new index. Otherwise it walks order outward from the old position —
// next row below first, then above, then further out — landing on the
// nearest surviving neighbour; if nothing in order survived, it clamps into
// range.
func (m *Model) restoreCursor(num int, order []int) {
	n, ok := m.section.(numbered)
	l := m.section.Len()
	if !ok || l == 0 {
		m.cursor = 0
		return
	}
	shown := make(map[int]int, l) // number → new shown index
	for i := range l {
		shown[n.numberAt(i)] = i
	}
	if num != 0 {
		if i, ok := shown[num]; ok {
			m.cursor = i
			return
		}
	}
	if oldIdx := slices.Index(order, num); num != 0 && oldIdx >= 0 {
		for step := 1; step <= len(order); step++ {
			if oldIdx+step < len(order) {
				if i, ok := shown[order[oldIdx+step]]; ok {
					m.cursor = i
					return
				}
			}
			if oldIdx-step >= 0 {
				if i, ok := shown[order[oldIdx-step]]; ok {
					m.cursor = i
					return
				}
			}
		}
	}
	m.cursor = min(max(m.cursor, 0), l-1)
}

// selectedRows captures the selection's identity before a paint mutates the
// section: the selected numbers, and which of them heldTag already reported
// held (so restoreSelection can tell a row that was already held from one
// that becomes held in this same paint).
func (m *Model) selectedRows() (nums []int, wasHeld map[int]bool) {
	n, ok := m.section.(numbered)
	if !ok {
		return nil, nil
	}
	l := m.section.Len()
	wasHeld = make(map[int]bool, m.sel.count())
	for _, i := range m.sel.indices() {
		if i < 0 || i >= l {
			continue
		}
		num := n.numberAt(i)
		nums = append(nums, num)
		held, _ := m.heldTag(i)
		wasHeld[num] = held
	}
	return nums, wasHeld
}

// restoreSelection rebuilds the selection at its members' new shown indexes
// after a paint (nums, wasHeld as captured by selectedRows before the paint;
// nil clears it, as a replace does). A number no longer shown is dropped, and
// so is one that became held in this paint and wasn't already — a row
// already held when selected stays selected, since read-only bulk actions
// still reach it and mutable() refuses mutations at action time regardless.
func (m *Model) restoreSelection(nums []int, wasHeld map[int]bool) {
	m.sel.clear()
	n, ok := m.section.(numbered)
	if !ok {
		return
	}
	l := m.section.Len()
	shown := make(map[int]int, l) // number → new shown index
	for i := range l {
		shown[n.numberAt(i)] = i
	}
	for _, num := range nums {
		i, ok := shown[num]
		if !ok {
			continue
		}
		if held, _ := m.heldTag(i); held && !wasHeld[num] {
			continue
		}
		m.sel.toggle(i)
	}
}

// mutable refuses a mutation on a merged or closed PR, and on a held row
// unless its lookup says it is still open: a departed row keeps its OPEN
// snapshot while the lookup is pending.
func (m *Model) mutable(p gh.PR) error {
	if p.State == "MERGED" || p.State == "CLOSED" {
		return fmt.Errorf("PR #%d is not open", p.Number)
	}
	if st, held := m.held[p.Number]; held && st != "OPEN" {
		return fmt.Errorf("PR #%d is no longer on this board — ctrl+r to refresh", p.Number)
	}
	return nil
}
