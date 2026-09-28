package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// openPR / mergedPR moved here from mergedsticky_test.go (#141): that file is
// deleted once the held-row mechanism replaces mergedSticky, and these
// fixtures are used by the tests below too.
func openPR(number int, author string) gh.PR {
	p := gh.PR{Number: number, Title: "pr " + author, State: "OPEN", HeadRefName: "feat/x"}
	p.Author.Login = author
	return p
}

func mergedPR(number int, author string) gh.PR {
	p := openPR(number, author)
	p.State, p.MergedAt = "MERGED", time.Now().Add(-2*time.Minute)
	return p
}

// mergeablePR is openPR plus the node ID and Mergeable state runBulkNative's
// merge pre-check requires to actually reach the mutation source.
func mergeablePR(number int, author string) gh.PR {
	p := openPR(number, author)
	p.ID = fmt.Sprintf("pr%dnode", number)
	p.Mergeable = "MERGEABLE"
	return p
}

// findPR scans the section's full PR list (not just the shown set) for
// number, so a state/order assertion can look a row up regardless of where
// it sorted.
func findPR(t *testing.T, ps *PRSection, number int) gh.PR {
	t.Helper()
	for _, p := range ps.prs {
		if p.Number == number {
			return p
		}
	}
	t.Fatalf("PR #%d not found on the board", number)
	return gh.PR{}
}

// partialMergeSource is a MutationSource that fails MergePR for specific node
// IDs while succeeding for the rest — fakeMutationSource (mutationsource_test.go)
// only supports one shared err for every call, so it can't model a partial
// batch failure.
type partialMergeSource struct {
	mergeCalls []string
	failFor    map[string]error
}

func (f *partialMergeSource) MergePR(prID string) error {
	f.mergeCalls = append(f.mergeCalls, prID)
	return f.failFor[prID]
}
func (f *partialMergeSource) EnableAutoMerge(string) error          { return nil }
func (f *partialMergeSource) DisableAutoMerge(string) error         { return nil }
func (f *partialMergeSource) MarkReady(string) error                { return nil }
func (f *partialMergeSource) ConvertToDraft(string) error           { return nil }
func (f *partialMergeSource) UpdateBranch(string) error             { return nil }
func (f *partialMergeSource) ApprovePR(string) error                { return nil }
func (f *partialMergeSource) RequestReviews(string, []string) error { return nil }

// invokeCmdTree drives cmd and, when it returns a tea.BatchMsg, recurses into
// every sub-command — mirroring driveBulk's one-level unwrap but going as
// deep as the tree goes. It exists only to let a counting source (installed
// via SetPRSource) observe whether a list fetch is anywhere in the tree, not
// to collect the messages themselves.
//
// Some leaves are tea.Tick-based (the status-clear tick, the spinner tick)
// and would otherwise block for seconds; each leaf gets its own short timeout
// and is simply skipped if it doesn't return in time.
func invokeCmdTree(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				invokeCmdTree(t, c)
			}
		}
	case <-time.After(200 * time.Millisecond):
		// a tea.Tick-based cmd (status clear, spinner) — not a list fetch.
	}
}

// TestBatchMergeShowsEveryMergedPRDespiteStaleSearch is acceptance A1: a batch
// merge of #11-#13 followed by a refetch that still returns #12 as OPEN (a
// lagging search index) and drops #11/#13 entirely must still show all three
// merged. Red on main: applyMergedSticky only appends a held PR the fetch
// dropped outright, so a stale-OPEN #12 the fetch still returns wins over the
// merge prdash already saw succeed.
func TestBatchMergeShowsEveryMergedPRDespiteStaleSearch(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	var prs []gh.PR
	for n := 10; n <= 14; n++ {
		prs = append(prs, mergeablePR(n, "alice"))
	}
	m.setPRs(prs)
	fs := &fakeMutationSource{}
	m.SetMutationSource(fs)

	// Shown order is number-descending: #14,#13,#12,#11,#10 → indices 0..4.
	m.sel.toggle(1) // #13
	m.sel.toggle(2) // #12
	m.sel.toggle(3) // #11

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	updated, _ := m.Update(done)
	m = updated.(Model)

	fetched := []gh.PR{mergeablePR(10, "alice"), mergeablePR(12, "alice"), mergeablePR(14, "alice")}
	updated, _ = m.Update(prsFetchedMsg{filter: m.filter, prs: fetched})
	m = updated.(Model)

	ps := m.section.(*PRSection)
	wantOrder := []int{14, 13, 12, 11, 10}
	wantState := map[int]string{10: "OPEN", 11: "MERGED", 12: "MERGED", 13: "MERGED", 14: "OPEN"}
	if ps.Len() != len(wantOrder) {
		t.Fatalf("shown rows = %d, want %d: %+v", ps.Len(), len(wantOrder), ps.prs)
	}
	for i, wantNum := range wantOrder {
		got := ps.prAt(i)
		if got.Number != wantNum {
			t.Errorf("row %d = #%d, want #%d", i, got.Number, wantNum)
			continue
		}
		if got.State != wantState[wantNum] {
			t.Errorf("#%d state = %q, want %q", wantNum, got.State, wantState[wantNum])
		}
	}
}

// TestPartialBatchMergeMarksTheSuccessesAndRefreshes is acceptance A2: merging
// three PRs where one MergePR call fails must still mark the two successes
// merged immediately and return a cmd tree that includes a refresh. Red on
// main: runBulkNative never sets actionDoneMsg.partial/merged, so the
// actionDoneMsg arm's `msg.err == nil` guard skips the sticky-marking and the
// refresh entirely on any partial failure.
func TestPartialBatchMergeMarksTheSuccessesAndRefreshes(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	cs := &countingSource{}
	m.SetPRSource(cs)
	prs := []gh.PR{mergeablePR(11, "alice"), mergeablePR(12, "alice"), mergeablePR(13, "alice")}
	m.setPRs(prs)
	fs := &partialMergeSource{failFor: map[string]error{"pr12node": fmt.Errorf("merge blocked")}}
	m.SetMutationSource(fs)

	m.sel.toggle(0) // #13
	m.sel.toggle(1) // #12
	m.sel.toggle(2) // #11

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (one of three failed)", msg)
	}
	updated, cmd := m.Update(done)
	m = updated.(Model)

	ps := m.section.(*PRSection)
	for _, n := range []int{11, 13} {
		if p := findPR(t, ps, n); p.State != "MERGED" {
			t.Errorf("#%d state = %q, want MERGED before any refetch", n, p.State)
		}
	}

	invokeCmdTree(t, cmd)
	if cs.calls.Load() < 1 {
		t.Error("cmd tree from the partial batch settle never called FetchPRs — want a refresh for the successes")
	}
}

// TestCursorFollowsThePRAcrossARefetch is acceptance A3. Red on main: setPRs
// only clamps the cursor index when the list shrinks; it never re-anchors it
// to the PR the user was looking at.
func TestCursorFollowsThePRAcrossARefetch(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{openPR(20, "alice"), openPR(18, "alice"), openPR(16, "alice"), openPR(14, "alice"), openPR(12, "alice")})
	ps := m.section.(*PRSection)
	m.cursor = 3
	if got := ps.prAt(m.cursor).Number; got != 14 {
		t.Fatalf("test setup: index %d holds #%d, want #14", m.cursor, got)
	}

	draft20 := openPR(20, "alice")
	draft20.IsDraft = true // drafts sort last, so #20 moves off the top
	next := []gh.PR{openPR(25, "alice"), openPR(23, "alice"), openPR(14, "alice"), openPR(12, "alice"), draft20}

	updated, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: next})
	m = updated.(Model)
	ps = m.section.(*PRSection)

	// Sanity: the refetch really did reorder rows above the cursor's old index,
	// so the test exercises the bug rather than a no-op board.
	if got := ps.prAt(3).Number; got == 14 {
		t.Fatalf("test setup invalid: index 3 still holds #14 after the refetch")
	}
	if got := ps.prAt(m.cursor).Number; got != 14 {
		t.Errorf("cursor PR = #%d, want #14 — the cursor should follow the PR across the refetch", got)
	}
}

// TestDepartedRowKeepsItsPosition is acceptance A4 (part 1): a row an
// unrequested refetch doesn't return must stay shown, in its old position.
// Red on main: setPRs replaces the section's rows wholesale.
func TestDepartedRowKeepsItsPosition(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{openPR(30, "alice"), openPR(29, "alice"), openPR(28, "alice")})

	updated, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{openPR(30, "alice"), openPR(28, "alice")}})
	m = updated.(Model)
	ps := m.section.(*PRSection)

	if ps.Len() != 3 {
		t.Fatalf("shown rows = %d, want #29 held so the board still shows 3 rows: %+v", ps.Len(), ps.prs)
	}
	if got := ps.prAt(1).Number; got != 29 {
		t.Errorf("row 1 = #%d, want #29 to keep its position", got)
	}
}

// TestMergeHeldPRsCarriesDepartedRowWithCategory is Step 5: a number the
// fetch drops is carried forward at its previous value, keeping its previous
// category, and recorded pending in m.held.
func TestMergeHeldPRsCarriesDepartedRowWithCategory(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	prev := []gh.PR{openPR(30, "alice"), openPR(29, "alice")}
	prevCats := map[int]string{30: "Mine", 29: "Others"}
	fetched := []gh.PR{openPR(30, "alice")}
	cats := map[int]string{30: "Mine"}

	out := m.mergeHeldPRs(prev, prevCats, fetched, cats)

	if len(out) != 2 {
		t.Fatalf("out = %+v, want #29 carried forward alongside #30", out)
	}
	if cats[29] != "Others" {
		t.Errorf("cats[29] = %q, want %q (carried from prevCats)", cats[29], "Others")
	}
	if st, ok := m.held[29]; !ok || st != "" {
		t.Errorf("m.held[29] = (%q, %v), want (\"\", true) — pending lookup", st, ok)
	}
	if _, ok := m.held[30]; ok {
		t.Error("m.held[30] should be absent — #30 is OPEN, matching the open board state")
	}
}

// TestMergeHeldPRsSessionMergeReturnedStaleHeldAsMerged: a fetch that still
// returns a session-merged PR as OPEN has already been overlaid to MERGED by
// overlaySessionMerged before mergeHeldPRs runs; since its State then differs
// from the open board's state, it is held (tagged merged) rather than
// un-held — a lagging search result never outweighs prdash's own knowledge.
func TestMergeHeldPRsSessionMergeReturnedStaleHeldAsMerged(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	stamp := time.Now()
	m.sessionMerged[12] = stamp
	p := openPR(12, "alice")
	p.State, p.MergedAt = "MERGED", stamp // as overlaySessionMerged would have left it
	fetched := []gh.PR{p}

	out := m.mergeHeldPRs(nil, nil, fetched, nil)

	if len(out) != 1 || out[0].State != "MERGED" {
		t.Fatalf("out = %+v, want the single overlaid PR unchanged", out)
	}
	if got := m.held[12]; got != "MERGED" {
		t.Errorf("m.held[12] = %q, want MERGED", got)
	}
}

// TestMergeHeldPRsReturnedMemberUnholds: a number previously held that the
// fetch returns with State equal to the board state is no longer held.
func TestMergeHeldPRsReturnedMemberUnholds(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.held[14] = ""
	fetched := []gh.PR{openPR(14, "alice")}

	m.mergeHeldPRs(nil, nil, fetched, nil)

	if _, ok := m.held[14]; ok {
		t.Error("m.held[14] should be gone — the fetch returned it OPEN, matching the open board")
	}
}

// TestMergeHeldPRsReturnedThenMissingReholds: a row that un-held on one
// refetch is held again, pending, the next time it departs.
func TestMergeHeldPRsReturnedThenMissingReholds(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	prev := []gh.PR{openPR(14, "alice")}
	m.mergeHeldPRs(nil, nil, prev, nil) // returned: not held
	if _, ok := m.held[14]; ok {
		t.Fatalf("test setup: #14 should not be held yet")
	}

	m.mergeHeldPRs(prev, nil, nil, nil) // now missing entirely

	if st, ok := m.held[14]; !ok || st != "" {
		t.Errorf("m.held[14] = (%q, %v), want (\"\", true) after departing", st, ok)
	}
}

// TestApplyHeldStatesPreservesTheClosedBoardsSortKey: a lookup patches State
// but must not touch ClosedAt on the closed board — ClosedAt is its sort key,
// and reordering rows the user is looking at is exactly what this mechanism
// exists to avoid.
func TestApplyHeldStatesPreservesTheClosedBoardsSortKey(t *testing.T) {
	m := NewModel("/repo", "is:closed", nil)
	m.width, m.height = 100, 40
	original := time.Now().Add(-24 * time.Hour)
	p := gh.PR{Number: 50, State: "CLOSED", ClosedAt: original}
	m.setPRs([]gh.PR{p})
	m.held[50] = ""

	m.applyHeldStates(map[int]gh.ItemState{50: {State: "CLOSED", ClosedAt: time.Now()}})

	ps := m.section.(*PRSection)
	got := findPR(t, ps, 50)
	if !got.ClosedAt.Equal(original) {
		t.Errorf("ClosedAt = %v, want unchanged %v — it's the closed board's sort key", got.ClosedAt, original)
	}
	if m.held[50] != "CLOSED" {
		t.Errorf("m.held[50] = %q, want CLOSED recorded from the lookup", m.held[50])
	}
}

// TestCursorAnchorCapturesNumberAndOrder: cursorAnchor reads the cursor row's
// number and the full shown order off the current section.
func TestCursorAnchorCapturesNumberAndOrder(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{openPR(30, "alice"), openPR(20, "alice"), openPR(10, "alice")})
	m.cursor = 1

	num, order := m.cursorAnchor()

	if num != 20 {
		t.Errorf("num = %d, want #20 (the cursor row)", num)
	}
	if want := []int{30, 20, 10}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

// TestCursorAnchorEmptyBoard: an empty board anchors to 0 with no order.
func TestCursorAnchorEmptyBoard(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs(nil)

	num, order := m.cursorAnchor()

	if num != 0 || len(order) != 0 {
		t.Errorf("cursorAnchor() = (%d, %v), want (0, empty)", num, order)
	}
}

// TestRestoreCursorAnchorWalksOutwardPreferringBelowThenAboveThenClamps is
// Step 6: restoreCursor walks the old shown order outward from the departed
// number's old position — next row below first, then above — and clamps only
// once nothing in the old order survived.
func TestRestoreCursorAnchorWalksOutwardPreferringBelowThenAboveThenClamps(t *testing.T) {
	order := []int{50, 40, 30, 20, 10} // old shown order; cursor was on #30 (index 2)

	t.Run("prefers the neighbour below when both survive", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.setPRs([]gh.PR{openPR(40, "alice"), openPR(20, "alice")})
		m.restoreCursor(30, order)
		ps := m.section.(*PRSection)
		if got := ps.prAt(m.cursor).Number; got != 20 {
			t.Errorf("cursor PR = #%d, want #20 (the surviving neighbour below #30)", got)
		}
	})

	t.Run("falls back to the neighbour above", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.setPRs([]gh.PR{openPR(40, "alice"), openPR(10, "alice")})
		m.restoreCursor(30, order)
		ps := m.section.(*PRSection)
		if got := ps.prAt(m.cursor).Number; got != 40 {
			t.Errorf("cursor PR = #%d, want #40 (the surviving neighbour above #30)", got)
		}
	})

	t.Run("clamps when nothing in the old order survived", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.setPRs([]gh.PR{openPR(99, "alice"), openPR(98, "alice")})
		m.cursor = 5 // stale, out of range
		m.restoreCursor(30, order)
		if m.cursor != 1 {
			t.Errorf("cursor = %d, want clamped to the last row (1)", m.cursor)
		}
	})
}

// TestHeldMergedRowRefusesUpdateBranch is acceptance A5 (part 1): a row that
// is no longer OPEN must reject update-branch. Red on main: nativeMutationFn's
// "update-branch" case has no state check at all (unlike merge/mark-ready/
// convert-to-draft/approve).
func TestHeldMergedRowRefusesUpdateBranch(t *testing.T) {
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	p := mergedPR(61, "alice")
	p.ID = "pr61node"
	m.setPRs([]gh.PR{p})
	fs := &fakeMutationSource{}
	m.SetMutationSource(fs)

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["u"]))
	done, ok := msg.(actionDoneMsg)
	if !ok {
		t.Fatalf("msg = %+v, want an actionDoneMsg", msg)
	}
	if len(fs.updateBranchCalls) != 0 {
		t.Errorf("updateBranchCalls = %v, want none — a held (non-OPEN) row is not a mutation target", fs.updateBranchCalls)
	}
	if done.err == nil {
		t.Error("want an error settling update-branch on a held merged row")
	}
}
