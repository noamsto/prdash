package ui

import (
	"reflect"
	"sort"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeMutationSource mirrors fakeIssueSource (issuesource_test.go) for the PR
// mutation seam: it records every call's PR node ID (and, for RequestReviews,
// the full desired login set) instead of hitting GitHub.
type fakeMutationSource struct {
	mergeCalls, autoMergeCalls, markReadyCalls, updateBranchCalls []string
	reviewCalls                                                   []reviewCall
	err                                                           error // returned by every call, to test failure propagation
}

type reviewCall struct {
	prID   string
	logins []string
}

func (f *fakeMutationSource) MergePR(prID string) error {
	f.mergeCalls = append(f.mergeCalls, prID)
	return f.err
}

func (f *fakeMutationSource) EnableAutoMerge(prID string) error {
	f.autoMergeCalls = append(f.autoMergeCalls, prID)
	return f.err
}

func (f *fakeMutationSource) MarkReady(prID string) error {
	f.markReadyCalls = append(f.markReadyCalls, prID)
	return f.err
}

func (f *fakeMutationSource) UpdateBranch(prID string) error {
	f.updateBranchCalls = append(f.updateBranchCalls, prID)
	return f.err
}

func (f *fakeMutationSource) RequestReviews(prID string, logins []string) error {
	f.reviewCalls = append(f.reviewCalls, reviewCall{prID: prID, logins: append([]string(nil), logins...)})
	return f.err
}

// driveBulk fires the tea.BatchMsg cmd returns and drives every sub-command,
// mirroring TestBulkInlineRunsPerSelected's convention (perf_actions_test.go):
// runBulk/runAction hand back tea.Batch(networkCall, spinnerTick), so the
// action's own completion (actionDoneMsg) is one of the batch's sub-messages,
// alongside a spinnerTickMsg this helper drives but ignores.
func driveBulk(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if m := c(); m != nil {
			if _, isDone := m.(actionDoneMsg); isDone {
				return m
			}
		}
	}
	return nil
}

// mutationModel builds a Model with prs on the PR board, a counting gh-CLI
// runner (so tests can assert the native path skips it), and fs installed as
// the mutation source.
func mutationModel(t *testing.T, prs []gh.PR) (m Model, cr *countingRunner, fs *fakeMutationSource) {
	t.Helper()
	m = NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	cr = &countingRunner{}
	m.SetRunner(cr)
	m.setPRs(prs)
	fs = &fakeMutationSource{}
	m.SetMutationSource(fs)
	return m, cr, fs
}

func TestMergeRoutesToNativeSource(t *testing.T) {
	m, cr, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "pr7node", State: "OPEN"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.mergeCalls) != 1 || fs.mergeCalls[0] != "pr7node" {
		t.Errorf("mergeCalls = %v, want [pr7node]", fs.mergeCalls)
	}
	if cr.calls != 0 {
		t.Errorf("gh CLI runner called %d times, want 0 (native path should skip it)", cr.calls)
	}
}

func TestMergeSkipsWhenNotOpen(t *testing.T) {
	m, _, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "pr7node", State: "MERGED"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (PR not open)", msg)
	}
	if len(fs.mergeCalls) != 0 {
		t.Errorf("mergeCalls = %v, want none — the pre-check should short-circuit before firing", fs.mergeCalls)
	}
}

func TestMergeSkipsWhenConflicting(t *testing.T) {
	m, _, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "pr7node", State: "OPEN"}})
	m.detail[7] = gh.PRDetail{Mergeable: "CONFLICTING"}
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (conflicting)", msg)
	}
	if len(fs.mergeCalls) != 0 {
		t.Errorf("mergeCalls = %v, want none", fs.mergeCalls)
	}
}

func TestAutoMergeRoutesToNativeSource(t *testing.T) {
	m, cr, fs := mutationModel(t, []gh.PR{{Number: 9, ID: "pr9node", State: "OPEN"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["A"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.autoMergeCalls) != 1 || fs.autoMergeCalls[0] != "pr9node" {
		t.Errorf("autoMergeCalls = %v, want [pr9node]", fs.autoMergeCalls)
	}
	if cr.calls != 0 {
		t.Errorf("gh CLI runner called %d times, want 0", cr.calls)
	}
}

func TestMarkReadyRoutesWhenDraft(t *testing.T) {
	m, cr, fs := mutationModel(t, []gh.PR{{Number: 11, ID: "pr11node", State: "OPEN", IsDraft: true}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["M"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.markReadyCalls) != 1 || fs.markReadyCalls[0] != "pr11node" {
		t.Errorf("markReadyCalls = %v, want [pr11node]", fs.markReadyCalls)
	}
	if cr.calls != 0 {
		t.Errorf("gh CLI runner called %d times, want 0", cr.calls)
	}
}

func TestMarkReadyNoopWhenAlreadyReady(t *testing.T) {
	m, cr, fs := mutationModel(t, []gh.PR{{Number: 11, ID: "pr11node", State: "OPEN", IsDraft: false}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["M"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a benign success — already-ready is a no-op, not a failure", msg)
	}
	if len(fs.markReadyCalls) != 0 {
		t.Errorf("markReadyCalls = %v, want none — already-ready must short-circuit before firing", fs.markReadyCalls)
	}
	if cr.calls != 0 {
		t.Errorf("gh CLI runner called %d times, want 0", cr.calls)
	}
}

func TestMarkReadyFailsWhenClosed(t *testing.T) {
	m, _, fs := mutationModel(t, []gh.PR{{Number: 11, ID: "pr11node", State: "CLOSED", IsDraft: true}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["M"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (closed PR)", msg)
	}
	if len(fs.markReadyCalls) != 0 {
		t.Errorf("markReadyCalls = %v, want none", fs.markReadyCalls)
	}
}

func TestUpdateBranchRoutesToNativeSource(t *testing.T) {
	m, cr, fs := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["u"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.updateBranchCalls) != 1 || fs.updateBranchCalls[0] != "pr13node" {
		t.Errorf("updateBranchCalls = %v, want [pr13node]", fs.updateBranchCalls)
	}
	if cr.calls != 0 {
		t.Errorf("gh CLI runner called %d times, want 0", cr.calls)
	}
}

// TestSingleNativeCmdRoutesToNativeSource exercises runAction's native branch
// directly — every packaged default merge/ready/update-branch keybinding is
// Scope:"per-selected" (so runBulk/runBulkNative above is what actually fires
// today), but a Scope:"single" custom action with the same Command.Native
// marker must route identically.
func TestSingleNativeCmdRoutesToNativeSource(t *testing.T) {
	m, cr, fs := mutationModel(t, []gh.PR{{Number: 21, ID: "pr21node", State: "OPEN"}})
	a := action.Action{
		Key: "m-single",
		Command: action.Command{
			Argv:   []string{"gh", "pr", "merge", "{{.Number}}", "--squash"},
			Native: "merge-squash",
		},
		Scope: "single",
	}
	msg := driveBulk(t, m.runAction(a))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.mergeCalls) != 1 || fs.mergeCalls[0] != "pr21node" {
		t.Errorf("mergeCalls = %v, want [pr21node]", fs.mergeCalls)
	}
	if cr.calls != 0 {
		t.Errorf("gh CLI runner called %d times, want 0", cr.calls)
	}
}

// TestMergeGateOffUsesArgv proves the gate-off default is unchanged: with no
// mutation source installed, merge still builds and runs the same gh CLI argv
// it always has.
func TestMergeGateOffUsesArgv(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	rr := &recordRunner{}
	m.SetRunner(rr)
	m.setPRs([]gh.PR{{Number: 7, State: "OPEN"}}) // no ID: mirrors the gh-CLI path, which never populates it

	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(rr.calls) != 1 {
		t.Fatalf("want exactly one gh CLI call, got %d: %v", len(rr.calls), rr.calls)
	}
	want := []string{"pr", "merge", "7", "--squash"}
	if !reflect.DeepEqual(rr.calls[0], want) {
		t.Errorf("argv = %v, want %v", rr.calls[0], want)
	}
}

// TestReviewerGateOffUsesArgv proves the gate-off default is unchanged: with
// no mutation source installed, assigning reviewers still builds and runs
// the same gh CLI argv it always has (rather than routing to
// RequestReviews).
func TestReviewerGateOffUsesArgv(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	rr := &recordRunner{}
	m.SetRunner(rr)
	current := []string{"alice", "carol"}
	picked := map[string]bool{"alice": true, "bob": true, "carol": false}
	add, remove := reviewerDiff(current, picked)

	cmd := m.assignReviewersCmd(7, "", add, remove, picked)
	if cmd == nil {
		t.Fatal("expected a command when reviewers changed")
	}
	cmd() // fires the mutation, then a refetch (also via rr) not asserted here

	if len(rr.calls) == 0 {
		t.Fatal("want at least one gh CLI call, got none")
	}
	want := []string{"pr", "edit", "7", "--add-reviewer", "bob", "--remove-reviewer", "carol"}
	if !reflect.DeepEqual(rr.calls[0], want) {
		t.Errorf("argv = %v, want %v", rr.calls[0], want)
	}
}

func TestRequestReviewsSendsFullSetWithUnionFalse(t *testing.T) {
	m, _, fs := mutationModel(t, []gh.PR{{Number: 5, ID: "pr5node", State: "OPEN"}})
	picked := map[string]bool{"alice": true, "bob": true}
	add, remove := reviewerDiff(nil, picked)

	cmd := m.assignReviewersCmd(5, "pr5node", add, remove, picked)
	if cmd == nil {
		t.Fatal("expected a command when reviewers changed")
	}
	cmd() // fires the mutation; the resulting refetch msg isn't asserted here

	if len(fs.reviewCalls) != 1 || fs.reviewCalls[0].prID != "pr5node" {
		t.Fatalf("reviewCalls = %+v, want one call for pr5node", fs.reviewCalls)
	}
	got := append([]string(nil), fs.reviewCalls[0].logins...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Errorf("logins = %v, want [alice bob]", got)
	}
}

func TestRequestReviewsRemoveAllFiresEmptySet(t *testing.T) {
	m, _, fs := mutationModel(t, []gh.PR{{Number: 5, ID: "pr5node", State: "OPEN"}})
	current := []string{"alice"}
	picked := map[string]bool{"alice": false}
	add, remove := reviewerDiff(current, picked)

	cmd := m.assignReviewersCmd(5, "pr5node", add, remove, picked)
	if cmd == nil {
		t.Fatal("removing the last reviewer is a real change and must still fire")
	}
	cmd()

	if len(fs.reviewCalls) != 1 {
		t.Fatalf("reviewCalls = %+v, want exactly one call", fs.reviewCalls)
	}
	if len(fs.reviewCalls[0].logins) != 0 {
		t.Errorf("logins = %v, want an empty set (remove-all)", fs.reviewCalls[0].logins)
	}
}

func TestRequestReviewsSkipsWhenNothingChanged(t *testing.T) {
	m, _, fs := mutationModel(t, []gh.PR{{Number: 5, ID: "pr5node", State: "OPEN"}})
	current := []string{"alice"}
	picked := map[string]bool{"alice": true}
	add, remove := reviewerDiff(current, picked)

	cmd := m.assignReviewersCmd(5, "pr5node", add, remove, picked)
	if cmd != nil {
		t.Fatal("nothing changed: assignReviewersCmd should skip, not fire")
	}
	if len(fs.reviewCalls) != 0 {
		t.Errorf("reviewCalls = %+v, want none", fs.reviewCalls)
	}
}
