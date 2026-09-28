package ui

import (
	"fmt"
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
