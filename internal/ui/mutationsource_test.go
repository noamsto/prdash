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
	mergeCalls, autoMergeCalls, markReadyCalls, updateBranchCalls, approveCalls []string
	reviewCalls                                                                 []reviewCall
	err                                                                         error // returned by every call, to test failure propagation
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

func (f *fakeMutationSource) ApprovePR(prID string) error {
	f.approveCalls = append(f.approveCalls, prID)
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

// mutationModel builds a Model with prs on the PR board and fs installed as the
// mutation source.
func mutationModel(t *testing.T, prs []gh.PR) (m Model, fs *fakeMutationSource) {
	t.Helper()
	m = NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.SetPRSource(stubSource{}) // the post-mutation refetch fires through it
	m.setPRs(prs)
	fs = &fakeMutationSource{}
	m.SetMutationSource(fs)
	return m, fs
}

func TestMergeRoutesToNativeSource(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "pr7node", State: "OPEN"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.mergeCalls) != 1 || fs.mergeCalls[0] != "pr7node" {
		t.Errorf("mergeCalls = %v, want [pr7node]", fs.mergeCalls)
	}
}

func TestMergeSkipsWhenNotOpen(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "pr7node", State: "MERGED"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (PR not open)", msg)
	}
	if len(fs.mergeCalls) != 0 {
		t.Errorf("mergeCalls = %v, want none — the pre-check should short-circuit before firing", fs.mergeCalls)
	}
}

func TestMergeSkipsWhenConflicting(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "pr7node", State: "OPEN"}})
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
	m, fs := mutationModel(t, []gh.PR{{Number: 9, ID: "pr9node", State: "OPEN"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["A"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.autoMergeCalls) != 1 || fs.autoMergeCalls[0] != "pr9node" {
		t.Errorf("autoMergeCalls = %v, want [pr9node]", fs.autoMergeCalls)
	}
}

func TestMarkReadyRoutesWhenDraft(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 11, ID: "pr11node", State: "OPEN", IsDraft: true}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["M"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.markReadyCalls) != 1 || fs.markReadyCalls[0] != "pr11node" {
		t.Errorf("markReadyCalls = %v, want [pr11node]", fs.markReadyCalls)
	}
}

func TestMarkReadyNoopWhenAlreadyReady(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 11, ID: "pr11node", State: "OPEN", IsDraft: false}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["M"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a benign success — already-ready is a no-op, not a failure", msg)
	}
	if len(fs.markReadyCalls) != 0 {
		t.Errorf("markReadyCalls = %v, want none — already-ready must short-circuit before firing", fs.markReadyCalls)
	}
}

func TestMarkReadyFailsWhenClosed(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 11, ID: "pr11node", State: "CLOSED", IsDraft: true}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["M"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (closed PR)", msg)
	}
	if len(fs.markReadyCalls) != 0 {
		t.Errorf("markReadyCalls = %v, want none", fs.markReadyCalls)
	}
}

func TestUpdateBranchRoutesToNativeSource(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["u"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.updateBranchCalls) != 1 || fs.updateBranchCalls[0] != "pr13node" {
		t.Errorf("updateBranchCalls = %v, want [pr13node]", fs.updateBranchCalls)
	}
}

// TestSingleNativeCmdRoutesToNativeSource exercises runAction's native branch
// directly — every packaged default merge/ready/update-branch keybinding is
// Scope:"per-selected" (so runBulk/runBulkNative above is what actually fires
// today), but a Scope:"single" custom action with the same Command.Native
// marker must route identically.
func TestSingleNativeCmdRoutesToNativeSource(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 21, ID: "pr21node", State: "OPEN"}})
	a := action.Action{
		Key:     "m-single",
		Command: action.Command{Native: "merge-squash"},
		Scope:   "single",
	}
	msg := driveBulk(t, m.runAction(a))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.mergeCalls) != 1 || fs.mergeCalls[0] != "pr21node" {
		t.Errorf("mergeCalls = %v, want [pr21node]", fs.mergeCalls)
	}
}

// TestNativeMutationSkipsWhenNodeIDEmpty covers a stale cache entry written by
// the old gh-CLI prdash (no node id) reaching the native mutation dispatcher:
// it must short-circuit to a clear error instead of ever invoking the fake
// MutationSource with an empty id.
func TestNativeMutationSkipsWhenNodeIDEmpty(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 7, State: "OPEN"}}) // ID left unset
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (empty node id)", msg)
	}
	if len(fs.mergeCalls) != 0 {
		t.Errorf("mergeCalls = %v, want none — the empty-id guard must short-circuit before firing", fs.mergeCalls)
	}
}

func TestApproveRoutesToNativeSource(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 31, ID: "pr31node", State: "OPEN"}})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.approveCalls) != 1 || fs.approveCalls[0] != "pr31node" {
		t.Errorf("approveCalls = %v, want [pr31node]", fs.approveCalls)
	}
}

// TestApproveSkipsOwnPR guards the pre-check that keeps a mixed Mine/Others bulk
// selection from half-failing on GitHub's opaque self-approval 422.
func TestApproveSkipsOwnPR(t *testing.T) {
	pr := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	pr.Author.Login = "me"
	m, fs := mutationModel(t, []gh.PR{pr})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (own PR)", msg)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none — the self-approve guard must short-circuit", fs.approveCalls)
	}
}

// TestApproveFiresWhenViewerUnknown documents the deliberate gap: with no
// resolved viewer login there is nothing to compare against, so the mutation
// fires and GitHub decides.
func TestApproveFiresWhenViewerUnknown(t *testing.T) {
	pr := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	pr.Author.Login = "me"
	m, fs := mutationModel(t, []gh.PR{pr}) // viewerLogin left ""
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.approveCalls) != 1 {
		t.Errorf("approveCalls = %v, want one call", fs.approveCalls)
	}
}

func TestApproveFailsWhenNotOpen(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 31, ID: "pr31node", State: "CLOSED"}})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (closed PR)", msg)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none", fs.approveCalls)
	}
}

func TestApproveSkipsWhenNodeIDEmpty(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 31, State: "OPEN"}}) // ID left unset
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (empty node id)", msg)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none", fs.approveCalls)
	}
}

// TestApproveBulkSkipsOnlyOwnPR is the reason the guard exists: one aggregate
// failure out of two, with the other PR actually approved.
func TestApproveBulkSkipsOnlyOwnPR(t *testing.T) {
	mine := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	mine.Author.Login = "me"
	theirs := gh.PR{Number: 32, ID: "pr32node", State: "OPEN"}
	theirs.Author.Login = "you"
	m, fs := mutationModel(t, []gh.PR{mine, theirs})
	m.viewerLogin = "me"
	for i := 0; i < m.section.Len(); i++ {
		m.sel.toggle(i)
	}
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a partially-failed actionDoneMsg", msg)
	}
	if done.fail != "1 of 2 failed" {
		t.Errorf("fail = %q, want %q", done.fail, "1 of 2 failed")
	}
	if len(fs.approveCalls) != 1 || fs.approveCalls[0] != "pr32node" {
		t.Errorf("approveCalls = %v, want only [pr32node]", fs.approveCalls)
	}
}

// TestApproveSingleTargetSurfacesVerbatimError guards runBulkNative's n == 1
// branch: a single-PR batch reports the underlying error's own message rather
// than the opaque "1 of 1 failed" aggregate.
func TestApproveSingleTargetSurfacesVerbatimError(t *testing.T) {
	pr := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	pr.Author.Login = "me"
	m, fs := mutationModel(t, []gh.PR{pr})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg", msg)
	}
	if want := "can't approve your own PR #31"; done.fail != want {
		t.Errorf("fail = %q, want %q", done.fail, want)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none", fs.approveCalls)
	}
}

func TestRequestReviewsSendsFullSetWithUnionFalse(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 5, ID: "pr5node", State: "OPEN"}})
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
	m, fs := mutationModel(t, []gh.PR{{Number: 5, ID: "pr5node", State: "OPEN"}})
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
	m, fs := mutationModel(t, []gh.PR{{Number: 5, ID: "pr5node", State: "OPEN"}})
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

// TestRequestReviewsSkipsWhenNodeIDEmpty covers the same stale-cache scenario
// as TestNativeMutationSkipsWhenNodeIDEmpty, but for the reviewer-assignment
// path, which reaches the mutation source through assignReviewersCmd rather
// than nativeMutationFn.
func TestRequestReviewsSkipsWhenNodeIDEmpty(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 5, State: "OPEN"}}) // ID left unset
	picked := map[string]bool{"alice": true}
	add, remove := reviewerDiff(nil, picked)

	cmd := m.assignReviewersCmd(5, "", add, remove, picked)
	if cmd == nil {
		t.Fatal("expected a command surfacing the empty-id error")
	}
	msg := cmd()
	if failed, ok := msg.(fetchFailedMsg); !ok || failed.err == nil {
		t.Fatalf("msg = %+v, want a fetchFailedMsg (empty node id)", msg)
	}
	if len(fs.reviewCalls) != 0 {
		t.Errorf("reviewCalls = %+v, want none — the empty-id guard must short-circuit before firing", fs.reviewCalls)
	}
}
