package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// clearHeld drops all held-row state. Bumping heldGen makes a lookup still in
// flight land as stale.
func (m *Model) clearHeld() {
	m.held = map[gh.PRRef]string{}
	m.heldGen++
}

// overlaySessionMerged marks a fetched OPEN PR merged when prdash saw it merge
// this session: GitHub's search index lags writes by seconds. Copy-on-write,
// like applyCIRerun — fetched slices may share the cache's backing array.
func (m *Model) overlaySessionMerged(prs []gh.PR) []gh.PR {
	if len(m.sessionMerged) == 0 {
		return prs
	}
	var out []gh.PR
	for i, p := range prs {
		t, merged := m.sessionMerged[p.Ref()]
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

// mergeHeldPRs returns fetched plus every previous row it dropped, carried
// forward at its previous value so it keeps its sort position, and updates
// m.held to match. fetched must already carry overlaySessionMerged; a
// session-merged row that search still returns OPEN stays held as MERGED. A
// non-nil cats gets each carried row's previous category.
func (m *Model) mergeHeldPRs(prev []gh.PR, prevCats map[gh.PRRef]string, fetched []gh.PR, cats map[gh.PRRef]string) []gh.PR {
	boardState := strings.ToUpper(m.state)
	have := make(map[gh.PRRef]bool, len(fetched))
	for _, p := range fetched {
		r := p.Ref()
		have[r] = true
		if _, merged := m.sessionMerged[r]; merged && p.State != boardState {
			m.held[r] = p.State
		} else {
			delete(m.held, r)
		}
	}
	out := append([]gh.PR{}, fetched...)
	for _, p := range prev {
		r := p.Ref()
		if have[r] {
			continue
		}
		if t, merged := m.sessionMerged[r]; merged {
			p.State, p.MergedAt = "MERGED", t
			m.held[r] = "MERGED"
		} else if _, held := m.held[r]; !held {
			m.held[r] = ""
		}
		if cats != nil {
			if cat, ok := prevCats[r]; ok {
				cats[r] = cat
			}
		}
		out = append(out, p)
	}
	return out
}

// mergeHeldIssues is mergeHeldPRs for the issue boards. prdash merges no
// issues, so a returned issue always stops being held.
func (m *Model) mergeHeldIssues(prev []gh.Issue, prevCats map[int]string, fetched []gh.Issue, cats map[int]string) []gh.Issue {
	have := make(map[int]bool, len(fetched))
	for _, is := range fetched {
		have[is.Number] = true
		delete(m.held, m.launchRef(is.Number))
	}
	out := append([]gh.Issue{}, fetched...)
	for _, is := range prev {
		if have[is.Number] {
			continue
		}
		if _, held := m.held[m.launchRef(is.Number)]; !held {
			m.held[m.launchRef(is.Number)] = ""
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

// heldLookupRefs returns every held row except those prdash merged this
// session, sorted for a stable query shape. Already-looked-up rows are asked
// again: a row that left the filter can merge or close later.
func (m *Model) heldLookupRefs() []gh.PRRef {
	refs := make([]gh.PRRef, 0, len(m.held))
	for r := range m.held {
		if _, merged := m.sessionMerged[r]; merged {
			continue
		}
		refs = append(refs, r)
	}
	slices.SortFunc(refs, compareRefs)
	return refs
}

func compareRefs(a, b gh.PRRef) int {
	if c := strings.Compare(a.Repo, b.Repo); c != 0 {
		return c
	}
	return a.Number - b.Number
}

// heldStatesCmd looks up heldLookupRefs in one batched request.
func (m Model) heldStatesCmd() tea.Cmd {
	refs := m.heldLookupRefs()
	if m.stateSource == nil || len(refs) == 0 {
		return nil
	}
	gen := m.heldGen
	src := m.stateSource
	return func() tea.Msg {
		states, err := src.FetchStates(refs)
		return heldStatesMsg{gen: gen, states: states, err: err}
	}
}

// applyHeldStates records each held row's looked-up state and patches its PR
// row, leaving the board's own sort key alone (MergedAt on the merged board,
// ClosedAt on the closed one) so a lookup never reshuffles rows.
func (m *Model) applyHeldStates(states map[gh.PRRef]gh.ItemState) {
	ps, isPR := m.section.(*PRSection)
	for r, st := range states {
		if _, held := m.held[r]; !held {
			continue
		}
		if _, merged := m.sessionMerged[r]; merged {
			continue // a lookup issued before the merge landed would revert it to OPEN
		}
		m.held[r] = st.State
		if !isPR {
			continue
		}
		ps.updatePR(r, func(p *gh.PR) {
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
	if rs, ok := m.section.(*RunSection); ok {
		return rs.isHeld(rs.runAt(i).ID), ""
	}
	boardState := strings.ToUpper(m.state)
	if ps, ok := m.section.(*PRSection); ok {
		p := ps.prAt(i)
		st, held := m.held[p.Ref()]
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
	st, ok := m.held[m.launchRef(is.issueAt(i).Number)]
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

// refAt is shown row i's identity: the PR's own repo, or the launch repo for an
// issue. ok is false on a board whose rows are not PRs or issues.
func (m *Model) refAt(i int) (gh.PRRef, bool) {
	switch s := m.section.(type) {
	case *PRSection:
		return s.refAt(i), true
	case *IssueSection:
		return m.launchRef(s.issueAt(i).Number), true
	}
	return gh.PRRef{}, false
}

func (s *PRSection) refAt(i int) gh.PRRef { return s.prs[s.shown[i]].Ref() }

// cursorRef is refAt for the cursor row; ok is false off the board's range.
func (m *Model) cursorRef() (gh.PRRef, bool) {
	if m.cursor < 0 || m.cursor >= m.section.Len() {
		return gh.PRRef{}, false
	}
	return m.refAt(m.cursor)
}

// shownRefs maps each shown row's ref to its index; nil on a board without refs.
func (m *Model) shownRefs() map[gh.PRRef]int {
	l := m.section.Len()
	shown := make(map[gh.PRRef]int, l)
	for i := range l {
		r, ok := m.refAt(i)
		if !ok {
			return nil
		}
		shown[r] = i
	}
	return shown
}

// cursorAnchor captures the cursor's row (the zero ref on an empty board) and
// the shown order, for restoreCursor.
func (m *Model) cursorAnchor() (anchor gh.PRRef, order []gh.PRRef) {
	l := m.section.Len()
	order = make([]gh.PRRef, 0, l)
	for i := range l {
		r, ok := m.refAt(i)
		if !ok {
			return gh.PRRef{}, nil
		}
		order = append(order, r)
	}
	if m.cursor >= 0 && m.cursor < l {
		anchor = order[m.cursor]
	}
	return anchor, order
}

// paintAnchor is cursorAnchor for the board paints, but returns no anchor while
// a cursor on row 0 must stay there: after a launch or switch until its live
// replace lands (the cached top row is not what the user asked to be on), and,
// on the sections boards, while the jump to Mine is unspent (moving off 0 would
// make homeCursorOnMine spend it without jumping).
func (m *Model) paintAnchor(homing bool) (anchor gh.PRRef, order []gh.PRRef) {
	if m.cursor == 0 && (m.cursorPinnedTop || homing && !m.cursorHomed) {
		return gh.PRRef{}, nil
	}
	return m.cursorAnchor()
}

// restoreCursor puts the cursor back on anchor, or else on its nearest
// surviving neighbour in the previous order, preferring the row below.
func (m *Model) restoreCursor(anchor gh.PRRef, order []gh.PRRef) {
	shown := m.shownRefs()
	l := m.section.Len()
	if shown == nil || l == 0 {
		m.cursor = 0
		return
	}
	if anchor == (gh.PRRef{}) {
		m.cursor = min(max(m.cursor, 0), l-1)
		return
	}
	if i, found := shown[anchor]; found {
		m.cursor = i
		return
	}
	if oldIdx := slices.Index(order, anchor); oldIdx >= 0 {
		for step := 1; step <= len(order); step++ {
			if oldIdx+step < len(order) {
				if i, found := shown[order[oldIdx+step]]; found {
					m.cursor = i
					return
				}
			}
			if oldIdx-step >= 0 {
				if i, found := shown[order[oldIdx-step]]; found {
					m.cursor = i
					return
				}
			}
		}
	}
	m.cursor = min(max(m.cursor, 0), l-1)
}

// selectedRows captures the selected rows before a paint, and which of them
// were already held.
func (m *Model) selectedRows() (refs []gh.PRRef, wasHeld map[gh.PRRef]bool) {
	l := m.section.Len()
	wasHeld = make(map[gh.PRRef]bool, m.sel.count())
	for _, i := range m.sel.indices() {
		if i < 0 || i >= l {
			continue
		}
		r, ok := m.refAt(i)
		if !ok {
			return nil, nil
		}
		refs = append(refs, r)
		held, _ := m.heldTag(i)
		wasHeld[r] = held
	}
	return refs, wasHeld
}

// restoreSelection reselects refs at their new indexes, dropping any no longer
// shown or that became held in this paint. A row held as of the previous paint
// stays selected: read-only bulk actions still apply, and mutable() refuses
// the rest.
func (m *Model) restoreSelection(refs []gh.PRRef, wasHeld map[gh.PRRef]bool) {
	m.sel.clear()
	shown := m.shownRefs()
	for _, r := range refs {
		i, ok := shown[r]
		if !ok {
			continue
		}
		if held, _ := m.heldTag(i); held && !wasHeld[r] {
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
	if st, held := m.held[p.Ref()]; held && st != "OPEN" {
		return fmt.Errorf("PR #%d is no longer on this board — ctrl+r to refresh", p.Number)
	}
	return nil
}
