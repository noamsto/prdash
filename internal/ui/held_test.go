package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
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

// partialMergeSource is a MutationSource that fails specific node IDs while
// succeeding for the rest — fakeMutationSource (mutationsource_test.go) only
// supports one shared err for every call, so it can't model a partial batch
// failure. failFor covers merge, approve, and update-branch, the natives the
// partial-batch tests exercise.
type partialMergeSource struct {
	mergeCalls, approveCalls, updateBranchCalls []string
	failFor                                     map[string]error
}

func (f *partialMergeSource) MergePR(prID string) error {
	f.mergeCalls = append(f.mergeCalls, prID)
	return f.failFor[prID]
}
func (f *partialMergeSource) EnableAutoMerge(string) error  { return nil }
func (f *partialMergeSource) DisableAutoMerge(string) error { return nil }
func (f *partialMergeSource) MarkReady(string) error        { return nil }
func (f *partialMergeSource) ConvertToDraft(string) error   { return nil }
func (f *partialMergeSource) UpdateBranch(prID string) error {
	f.updateBranchCalls = append(f.updateBranchCalls, prID)
	return f.failFor[prID]
}
func (f *partialMergeSource) ApprovePR(prID string) error {
	f.approveCalls = append(f.approveCalls, prID)
	return f.failFor[prID]
}
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

// TestPartialBatchApproveDoesNotPaintChecksRunning pins the msg.rerunCI gate
// on the ciRerun-stamping block: approve never re-triggers CI (rerunsCI only
// says yes for update-branch/rerun-failed), so a partial approve failure must
// leave m.ciRerun untouched even though it still has partial numbers.
func TestPartialBatchApproveDoesNotPaintChecksRunning(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	prs := []gh.PR{mergeablePR(11, "alice"), mergeablePR(12, "alice"), mergeablePR(13, "alice")}
	m.setPRs(prs)
	fs := &partialMergeSource{failFor: map[string]error{"pr12node": fmt.Errorf("approve blocked")}}
	m.SetMutationSource(fs)

	m.sel.toggle(0) // #13
	m.sel.toggle(1) // #12
	m.sel.toggle(2) // #11

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (one of three failed)", msg)
	}
	updated, _ := m.Update(done)
	m = updated.(Model)

	if len(m.ciRerun) != 0 {
		t.Errorf("ciRerun = %v, want empty — approve never re-triggers CI", m.ciRerun)
	}
}

// TestPartialBatchUpdateBranchStampsTheSuccesses mirrors the cascade's own
// update-branch, but through a direct bulk press: the two successes must be
// stamped in m.ciRerun (rerunsCI is true for update-branch) and the failed
// one must not.
func TestPartialBatchUpdateBranchStampsTheSuccesses(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	prs := []gh.PR{mergeablePR(11, "alice"), mergeablePR(12, "alice"), mergeablePR(13, "alice")}
	m.setPRs(prs)
	fs := &partialMergeSource{failFor: map[string]error{"pr12node": fmt.Errorf("update blocked")}}
	m.SetMutationSource(fs)

	m.sel.toggle(0) // #13
	m.sel.toggle(1) // #12
	m.sel.toggle(2) // #11

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["u"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (one of three failed)", msg)
	}
	updated, _ := m.Update(done)
	m = updated.(Model)

	for _, n := range []int{11, 13} {
		if _, ok := m.ciRerun[n]; !ok {
			t.Errorf("ciRerun[%d] missing, want it stamped — update-branch succeeded on this PR", n)
		}
	}
	if _, ok := m.ciRerun[12]; ok {
		t.Error("ciRerun[12] stamped, want it absent — update-branch failed on this PR")
	}
}

// TestFailedMergeMarksNothing is acceptance A5's failed-merge counterpart to
// TestBatchMergeShowsEveryMergedPRDespiteStaleSearch: a single merge that
// fails must record nothing merged and leave the row OPEN.
func TestFailedMergeMarksNothing(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	m.setPRs([]gh.PR{mergeablePR(61, "alice")})
	fs := &fakeMutationSource{err: fmt.Errorf("merge blocked")}
	m.SetMutationSource(fs)

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg", msg)
	}
	updated, _ := m.Update(done)
	m = updated.(Model)

	if len(m.sessionMerged) != 0 {
		t.Errorf("sessionMerged = %v, want empty — the merge failed", m.sessionMerged)
	}
	if p := findPR(t, m.section.(*PRSection), 61); p.State != "OPEN" {
		t.Errorf("#61 state = %q, want OPEN — a failed merge must not mark the row merged", p.State)
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

// scriptedFetch is one canned FetchPRs result.
type scriptedFetch struct {
	prs []gh.PR
	err error
}

// scriptedPRSource answers the i-th FetchPRs call with script[i] (the last
// entry once the script runs out), so a test can drive the real fetch cmds
// that ctrl+r and a filter switch return.
type scriptedPRSource struct {
	mu     sync.Mutex
	calls  int
	script []scriptedFetch
}

func (s *scriptedPRSource) FetchPRs(string, int) ([]gh.PR, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.script[min(s.calls, len(s.script)-1)]
	s.calls++
	return f.prs, nil, f.err
}

// listFetchMsg runs cmd's tree and returns the first list-fetch result in it
// (prsFetchedMsg or fetchFailedMsg). Tick-based leaves (spinner, status clear)
// get a short timeout and are skipped, as in invokeCmdTree.
func listFetchMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case prsFetchedMsg, fetchFailedMsg:
			return msg
		case tea.BatchMsg:
			for _, c := range msg {
				if got := listFetchMsg(t, c); got != nil {
					return got
				}
			}
		}
	case <-time.After(200 * time.Millisecond):
	}
	return nil
}

// heldBoard is a merged board showing #30,#29,#28 whose first unrequested
// refetch dropped #29, so #29 is held.
func heldBoard(t *testing.T, src gh.PRSource) Model {
	t.Helper()
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(src)
	m.setPRs([]gh.PR{mergedPR(30, "alice"), mergedPR(29, "alice"), mergedPR(28, "alice")})
	u, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice")}})
	m = u.(Model)
	if _, held := m.held[29]; !held || m.section.Len() != 3 {
		t.Fatalf("test setup: want #29 held on a 3-row board, held = %v, rows = %d", m.held, m.section.Len())
	}
	return m
}

// shownNumbers is the board's shown set, ascending: these boards sort on
// MergedAt/ClosedAt stamped at fixture build time, so display order isn't
// what these tests pin.
func shownNumbers(m Model) []int {
	n := m.section.(numbered)
	var out []int
	for i := range m.section.Len() {
		out = append(out, n.numberAt(i))
	}
	slices.Sort(out)
	return out
}

func TestCtrlRDropsHeldRows(t *testing.T) {
	src := &scriptedPRSource{script: []scriptedFetch{{prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice")}}}}
	m := heldBoard(t, src)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	msg := listFetchMsg(t, cmd)
	if fm, ok := msg.(prsFetchedMsg); !ok || !fm.replace {
		t.Fatalf("ctrl+r fetch = %+v, want a prsFetchedMsg marked replace", msg)
	}
	u, _ := m.Update(msg)
	m = u.(Model)

	if got := shownNumbers(m); !slices.Equal(got, []int{28, 30}) {
		t.Errorf("shown = %v, want [28 30] — ctrl+r drops the held #29", got)
	}
	if len(m.held) != 0 {
		t.Errorf("held = %v, want empty after ctrl+r", m.held)
	}
}

// TestCtrlRWinsOverAnOlderUnrequestedFetch: an unrequested fetch that lands
// between ctrl+r and its result merges as usual (holding what it dropped); the
// ctrl+r result still replaces the board once it lands.
func TestCtrlRWinsOverAnOlderUnrequestedFetch(t *testing.T) {
	src := &scriptedPRSource{script: []scriptedFetch{{prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice")}}}}
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(src)
	m.setPRs([]gh.PR{mergedPR(30, "alice"), mergedPR(29, "alice"), mergedPR(28, "alice"), mergedPR(27, "alice")})
	u, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice"), mergedPR(27, "alice")}})
	m = u.(Model)

	u, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = u.(Model)
	requested := listFetchMsg(t, cmd)

	u, _ = m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice")}})
	m = u.(Model)
	for _, n := range []int{27, 29} {
		if _, held := m.held[n]; !held {
			t.Errorf("#%d not held after the unrequested fetch that landed first: held = %v", n, m.held)
		}
	}

	u, _ = m.Update(requested)
	m = u.(Model)
	if got := shownNumbers(m); !slices.Equal(got, []int{28, 30}) {
		t.Errorf("shown = %v, want [28 30] once the ctrl+r result lands", got)
	}
	if len(m.held) != 0 {
		t.Errorf("held = %v, want empty", m.held)
	}
}

// TestFailedCtrlRKeepsHeldRows: a ctrl+r whose fetch fails leaves nothing
// pending, so the next unrequested refetch still merges.
func TestFailedCtrlRKeepsHeldRows(t *testing.T) {
	src := &scriptedPRSource{script: []scriptedFetch{{err: fmt.Errorf("network down")}}}
	m := heldBoard(t, src)

	u, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = u.(Model)
	msg := listFetchMsg(t, cmd)
	if _, ok := msg.(fetchFailedMsg); !ok {
		t.Fatalf("ctrl+r fetch = %+v, want fetchFailedMsg", msg)
	}
	u, _ = m.Update(msg)
	m = u.(Model)

	u, _ = m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice")}})
	m = u.(Model)
	if got := shownNumbers(m); !slices.Contains(got, 29) {
		t.Errorf("shown = %v, want #29 still held after a failed ctrl+r", got)
	}
	if _, held := m.held[29]; !held {
		t.Errorf("held = %v, want #29 held", m.held)
	}
}

func TestFilterSwitchDropsHeldRows(t *testing.T) {
	closed := func(n int) gh.PR {
		p := openPR(n, "alice")
		p.State, p.ClosedAt = "CLOSED", time.Now()
		return p
	}
	src := &scriptedPRSource{script: []scriptedFetch{{prs: []gh.PR{closed(40), closed(39)}}}}
	m := heldBoard(t, src)
	m.cursor = 2

	u, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.state != "closed" {
		t.Fatalf("test setup: state = %q, want closed after s", m.state)
	}
	msg := listFetchMsg(t, cmd)
	if fm, ok := msg.(prsFetchedMsg); !ok || !fm.replace {
		t.Fatalf("switch fetch = %+v, want a prsFetchedMsg marked replace", msg)
	}
	u, _ = m.Update(msg)
	m = u.(Model)

	if got := shownNumbers(m); !slices.Equal(got, []int{39, 40}) {
		t.Errorf("shown = %v, want only the closed board's rows", got)
	}
	if len(m.held) != 0 {
		t.Errorf("held = %v, want empty after a filter switch", m.held)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after a filter switch", m.cursor)
	}
}

// sectionsMsg builds a sectionsFetchedMsg whose raw halves round-trip through
// the cache, so viewerFetchedMsg can re-split from them.
func sectionsMsg(t *testing.T, review, open []gh.PR) sectionsFetchedMsg {
	t.Helper()
	reviewRaw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	openRaw, err := json.Marshal(open)
	if err != nil {
		t.Fatal(err)
	}
	return sectionsFetchedMsg{state: "open", review: review, reviewRaw: reviewRaw,
		reviewedRaw: []byte("[]"), open: open, openRaw: openRaw}
}

// TestMineHomeJumpSurvivesAReorderingFetch: a sections fetch that reorders
// rows above the cursor before the viewer login resolves must not spend the
// opening jump to Mine; once homed, later fetches anchor the cursor.
func TestMineHomeJumpSurvivesAReorderingFetch(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	open := []gh.PR{openPR(5, "bob"), openPR(4, "me"), openPR(3, "bob")}

	u, _ := m.Update(sectionsMsg(t, []gh.PR{openPR(7, "bob")}, open))
	m = u.(Model)
	u, _ = m.Update(sectionsMsg(t, []gh.PR{openPR(8, "bob"), openPR(7, "bob")}, open)) // #8 lands above the cursor's #7
	m = u.(Model)
	if m.cursor != 0 || m.cursorHomed {
		t.Fatalf("before the login resolves: cursor = %d, homed = %v, want 0 and unspent", m.cursor, m.cursorHomed)
	}

	u, _ = m.Update(viewerFetchedMsg{login: "me"})
	m = u.(Model)
	ps := m.section.(*PRSection)
	if got := ps.prAt(m.cursor).Number; got != 4 {
		t.Fatalf("cursor PR = #%d, want #4 — the first Mine row", got)
	}

	u, _ = m.Update(sectionsMsg(t, []gh.PR{openPR(9, "bob"), openPR(8, "bob"), openPR(7, "bob")}, open))
	m = u.(Model)
	ps = m.section.(*PRSection)
	if got := ps.prAt(m.cursor).Number; got != 4 {
		t.Errorf("cursor PR = #%d, want #4 — after homing, a fetch anchors the cursor", got)
	}
}
