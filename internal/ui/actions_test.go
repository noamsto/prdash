package ui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// firstBatchMsg runs cmd and, when it's a tea.Batch of multiple commands,
// returns just the first sub-cmd's message — this package's convention for
// tea.Batch(work, startSpinner()) always puts the async work first.
func firstBatchMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("firstBatchMsg: cmd is nil")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) == 0 {
			t.Fatal("firstBatchMsg: empty batch")
		}
		return batch[0]()
	}
	return msg
}

func TestRunActionExitsTUIWritesHandoff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}})
	a := action.Action{Key: "enter", Command: action.Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}, ExitsTUI: true}

	quit := m.runAction(a)
	if quit == nil {
		t.Fatal("exits-tui action must return tea.Quit")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("handoff file not written: %v", err)
	}
}

func TestRunActionSwitchCollisionHoldsScreenAndDoesNotQueue(t *testing.T) {
	old := preflightSwitchFn
	t.Cleanup(func() { preflightSwitchFn = old })
	preflightSwitchFn = func(string, string) (gh.SwitchPreflight, error) {
		return gh.SwitchPreflight{Path: "/occupied", Occupant: "tmp4", Remedy: "cd /occupied && git switch feature"}, nil
	}
	m := NewModel(t.TempDir(), "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feature"}})
	a := action.Action{Key: "enter", Command: action.Command{Argv: []string{"wt", "switch", "feature"}}, ExitsTUI: true}
	cmd := m.runAction(a)
	if cmd == nil {
		t.Fatal("worktree switch must dispatch async, not nil")
	}
	// The check must have actually left the Update goroutine: nothing is
	// decided synchronously yet.
	if len(m.PendingExec()) != 0 || m.switchNotice != "" {
		t.Fatalf("preflight ran synchronously: pending=%v notice=%q", m.PendingExec(), m.switchNotice)
	}
	msg := firstBatchMsg(t, cmd)
	u, _ := m.Update(msg)
	m = u.(Model)
	if len(m.PendingExec()) != 0 || !strings.Contains(m.switchNotice, "/occupied") {
		t.Fatalf("collision action: pending=%v notice=%q", m.PendingExec(), m.switchNotice)
	}
}

func TestRunBulkSwitchCollisionDoesNotWriteHandoff(t *testing.T) {
	oldFn := preflightSwitchFn
	t.Cleanup(func() { preflightSwitchFn = oldFn })
	var gotBranch string
	preflightSwitchFn = func(_ string, branch string) (gh.SwitchPreflight, error) {
		gotBranch = branch
		return gh.SwitchPreflight{Path: "/occupied", Occupant: "tmp4", Remedy: "cd /occupied && git switch feature"}, nil
	}
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel(t.TempDir(), "is:open", nil)
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7, HeadRefName: "feature"}, {Number: 9, HeadRefName: "other"}})
	m.section = sec
	m.sel.toggle(0)
	cmd := m.runBulk(action.Action{Key: "W", Command: action.Command{Argv: []string{"wt", "switch", "{{.HeadRefName}}"}}, ExitsTUI: true, Scope: "per-selected"})
	if cmd == nil {
		t.Fatal("worktree switch must dispatch async, not nil")
	}
	msg := firstBatchMsg(t, cmd)
	u, _ := m.Update(msg)
	m = u.(Model)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("handoff written on collision: %v", err)
	}
	if !strings.Contains(m.switchNotice, "/occupied") {
		t.Fatalf("collision bulk must stay in TUI: notice=%q", m.switchNotice)
	}
	if gotBranch == "" {
		t.Fatal("preflight branch was empty")
	}
}

func TestRunActionIssueSwitchPreflightUsesBranch(t *testing.T) {
	old := preflightSwitchFn
	t.Cleanup(func() { preflightSwitchFn = old })
	var got string
	preflightSwitchFn = func(_ string, branch string) (gh.SwitchPreflight, error) {
		got = branch
		return gh.SwitchPreflight{Path: "/occupied", Occupant: "tmp4", Remedy: "git switch feat/7"}, nil
	}
	m := NewModel(t.TempDir(), "is:open", nil)
	sec := NewIssueSection("is:open")
	sec.SetIssues([]gh.Issue{{Number: 7, Title: "Feature"}})
	m.section = sec
	a := action.Action{Key: "W", Command: action.Command{Argv: []string{"wt", "switch", "{{.Branch}}"}}, ExitsTUI: true}
	cmd := m.runAction(a)
	if cmd == nil {
		t.Fatal("worktree switch must dispatch async, not nil")
	}
	firstBatchMsg(t, cmd) // drives preflightSwitchFn so `got` is populated
	if got == "" {
		t.Fatalf("issue collision: branch=%q", got)
	}
}

func TestRunBulkCollisionNoticeSaysNoSwitchesQueued(t *testing.T) {
	old := preflightSwitchFn
	t.Cleanup(func() { preflightSwitchFn = old })
	calls := 0
	preflightSwitchFn = func(string, string) (gh.SwitchPreflight, error) {
		calls++
		// Every row collides, regardless of which section row the fan-out
		// visits first — the point of this test is the wording, not ordering.
		return gh.SwitchPreflight{Path: "/occupied", Occupant: "tmp4", Remedy: "git -C '/occupied' switch -- 'blocked'"}, nil
	}
	m := NewModel(t.TempDir(), "is:open", nil)
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7, HeadRefName: "clean"}, {Number: 9, HeadRefName: "blocked"}})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)
	cmd := m.runBulk(action.Action{Key: "W", Command: action.Command{Argv: []string{"wt", "switch", "{{.HeadRefName}}"}}, ExitsTUI: true, Scope: "per-selected"})
	if cmd == nil {
		t.Fatal("worktree switch must dispatch async, not nil")
	}
	msg := firstBatchMsg(t, cmd)
	u, _ := m.Update(msg)
	m = u.(Model)
	if !strings.Contains(m.switchNotice, "No switches were queued") {
		t.Fatalf("bulk collision notice missing fail-closed wording: %q", m.switchNotice)
	}
	if !strings.Contains(m.switchNotice, "Row 1 of 2 blocked") {
		t.Fatalf("bulk collision notice missing which row blocked: %q", m.switchNotice)
	}
	if len(m.PendingExec()) != 0 {
		t.Fatalf("no switches should be queued on a bulk collision, got %v", m.PendingExec())
	}
}

func TestRunActionSwitchCheckingGuardsAgainstDoubleDispatch(t *testing.T) {
	old := preflightSwitchFn
	t.Cleanup(func() { preflightSwitchFn = old })
	calls := 0
	preflightSwitchFn = func(string, string) (gh.SwitchPreflight, error) {
		calls++
		return gh.SwitchPreflight{}, nil
	}
	m := NewModel(t.TempDir(), "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feature"}})
	a := action.Action{Key: "enter", Command: action.Command{Argv: []string{"wt", "switch", "feature"}}, ExitsTUI: true}

	first := m.runAction(a)
	if first == nil {
		t.Fatal("first dispatch must return a cmd")
	}
	if second := m.runAction(a); second != nil {
		t.Fatal("a second dispatch while one is in flight must be a no-op")
	}
	firstBatchMsg(t, first)
	if calls != 1 {
		t.Fatalf("preflightSwitchFn called %d times, want exactly 1", calls)
	}
}

func TestConfirmDefaultNoCancels(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	a := action.Action{Key: "m", Confirm: true}
	m.pending = &a
	m.confirmAnswer(false) // default No
	if m.pending != nil {
		t.Fatal("pending should clear on No")
	}
}

func TestBulkWritesPerItem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel("/repo", "is:open", nil) // PR section
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7}, {Number: 9}, {Number: 11}})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(2)

	a := action.Action{Key: "W", Command: action.Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}, ExitsTUI: true, Scope: "per-selected"}
	quit := m.runBulk(a)
	if quit == nil {
		t.Fatal("bulk exits-tui must quit")
	}
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Fatalf("want 2 handoff lines, got %d: %q", n, b)
	}
}

func TestClipboardText(t *testing.T) {
	v := action.Vars{URL: "https://x/pr/7", Branch: "feat/x"}
	if got := clipboardText("copy-url", v); got != v.URL {
		t.Fatalf("copy-url = %q, want %q", got, v.URL)
	}
	if got := clipboardText("copy-branch", v); got != v.Branch {
		t.Fatalf("copy-branch = %q, want %q", got, v.Branch)
	}
}

func TestCopiedLabel(t *testing.T) {
	cases := []struct {
		builtin string
		n       int
		kind    string
		want    string
	}{
		{"copy-url", 1, "pr", "Copied URL"},
		{"copy-url", 3, "issue", "Copied 3 URLs"},
		{"copy-branch", 1, "pr", "Copied branch"},
		{"copy-branch", 2, "issue", "Copied 2 branches"},
		{"copy-number", 1, "pr", "Copied PR number"},
		{"copy-number", 5, "pr", "Copied 5 PR numbers"},
		{"copy-number", 1, "issue", "Copied issue number"},
		{"copy-number", 5, "issue", "Copied 5 issue numbers"},
	}
	for _, c := range cases {
		if got := copiedLabel(c.builtin, c.n, c.kind); got != c.want {
			t.Errorf("copiedLabel(%q, %d, %q) = %q, want %q", c.builtin, c.n, c.kind, got, c.want)
		}
	}
}

func TestCopyClearsSelection(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}, {Number: 9, HeadRefName: "feat/y"}})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)

	a := action.Action{Key: "b", Command: action.Command{Builtin: "copy-branch"}}
	if cmd := m.runAction(a); cmd == nil {
		t.Fatal("copy should return a command")
	}
	if m.sel.count() != 0 {
		t.Fatalf("batch copy should clear the selection, still %d selected", m.sel.count())
	}
}

func TestReviewerDiff(t *testing.T) {
	add, rm := reviewerDiff([]string{"a", "c"}, map[string]bool{"b": true, "c": true})
	if len(add) != 1 || add[0] != "b" {
		t.Fatalf("add = %v, want [b]", add)
	}
	if len(rm) != 1 || rm[0] != "a" {
		t.Fatalf("remove = %v, want [a]", rm)
	}
	add, rm = reviewerDiff([]string{"a"}, map[string]bool{"a": true})
	if len(add) != 0 || len(rm) != 0 {
		t.Fatalf("no change expected, got add=%v rm=%v", add, rm)
	}
}

func automergeAction() action.Action {
	return action.Action{
		Key: "A", Scope: "per-selected", ConfirmOthers: true,
		Command: action.Command{Native: "auto-merge-squash"},
	}
}

func TestConfirmOthersOwnPRRunsImmediately(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 7}
	p.Author.Login = "me"
	sec.SetPRs([]gh.PR{p})
	m.section = sec

	if cmd := m.startBulk(automergeAction()); cmd == nil {
		t.Fatal("own PR should run without a prompt")
	}
	if m.pending != nil {
		t.Fatal("own PR must not set pending")
	}
}

func TestConfirmOthersForeignPRPrompts(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 7}
	p.Author.Login = "alice"
	sec.SetPRs([]gh.PR{p})
	m.section = sec

	if cmd := m.startBulk(automergeAction()); cmd != nil {
		t.Fatal("foreign PR should defer to a prompt (nil cmd)")
	}
	if m.pending == nil {
		t.Fatal("foreign PR must set pending")
	}
}

func TestConfirmOthersBulkAlwaysPrompts(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p1, p2 := gh.PR{Number: 7}, gh.PR{Number: 9}
	p1.Author.Login = "me"
	p2.Author.Login = "me"
	sec.SetPRs([]gh.PR{p1, p2})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)

	if cmd := m.startBulk(automergeAction()); cmd != nil {
		t.Fatal("bulk should always defer to a prompt (nil cmd)")
	}
	if m.pending == nil {
		t.Fatal("bulk must set pending even when all PRs are the viewer's")
	}
}

func TestConfirmOthersUnknownViewerPrompts(t *testing.T) {
	m := NewModel("/repo", "is:open", nil) // viewerLogin == ""
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 7}
	p.Author.Login = "alice"
	sec.SetPRs([]gh.PR{p})
	m.section = sec

	if cmd := m.startBulk(automergeAction()); cmd != nil {
		t.Fatal("unresolved viewer login should defer to a prompt (nil cmd)")
	}
	if m.pending == nil {
		t.Fatal("unresolved viewer login must set pending")
	}
}

func TestConfirmQuestionNamesForeignAuthor(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 42}
	p.Author.Login = "alice"
	sec.SetPRs([]gh.PR{p})
	m.section = sec
	a := automergeAction()
	m.pending = &a

	q := m.confirmQuestion()
	if !strings.Contains(q, "#42") || !strings.Contains(q, "alice") {
		t.Fatalf("foreign single-target wording should name the PR and author: %q", q)
	}
}

func TestConfirmQuestionEmptySectionNoPanic(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	sec := NewPRSection("is:open")
	sec.SetPRs(nil)
	m.section = sec
	a := automergeAction()
	m.pending = &a

	q := m.confirmQuestion() // must not panic
	if q == "" {
		t.Fatal("confirmQuestion should return a non-empty label on an empty section")
	}
}

func TestConfirmQuestionBulkShowsCount(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p1, p2 := gh.PR{Number: 7}, gh.PR{Number: 9}
	p1.Author.Login = "me"
	p2.Author.Login = "me"
	sec.SetPRs([]gh.PR{p1, p2})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)
	a := automergeAction()
	m.pending = &a

	q := m.confirmQuestion()
	if !strings.Contains(q, "for 2 PRs") {
		t.Fatalf("bulk wording should show the count: %q", q)
	}
}

// TestUpdateBranchPaintsChecksInProgress covers the staleness this override
// exists for: GitHub keeps serving the pre-push check rollup for seconds after
// update-branch, which would otherwise show a green ✓ on a branch whose checks
// are about to start over.
func TestUpdateBranchPaintsChecksInProgress(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.ciRerun[13] = time.Now()

	got := m.applyCIRerun([]gh.PR{{
		Number:            13,
		StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}},
	}})
	if got[0].CIState() != "pending" {
		t.Errorf("CIState = %q, want pending", got[0].CIState())
	}
}

func TestApplyCIRerunLeavesUnstampedPRsAlone(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now()

	got := m.applyCIRerun([]gh.PR{{
		Number:            99,
		StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}},
	}})
	if got[0].CIState() != "pass" {
		t.Errorf("CIState = %q, want pass — PR 99 was never stamped", got[0].CIState())
	}
}

// TestApplyCIRerunClearsWhenRealPendingArrives stops the override from
// outliving its usefulness: once a check has genuinely started after the
// stamp, the real rollup is authoritative.
func TestApplyCIRerunClearsWhenRealPendingArrives(t *testing.T) {
	m, _ := mutationModel(t, nil)
	stamp := time.Now()
	m.ciRerun[13] = stamp

	m.applyCIRerun([]gh.PR{{
		Number: 13,
		StatusCheckRollup: []gh.Check{{
			Name: "build", State: "IN_PROGRESS", StartedAt: stamp.Add(time.Second).Format(time.RFC3339),
		}},
	}})
	if _, still := m.ciRerun[13]; still {
		t.Error("ciRerun[13] survived a check that started after the stamp")
	}
}

// TestApplyCIRerunKeepsPreExistingPending is the regression guard for the
// common trigger: you press r/u because a check has already failed, usually
// with a sibling still pending from BEFORE the rerun fired. That pre-existing
// pending check must not clear the override, or the feature no-ops in exactly
// the case it exists for.
func TestApplyCIRerunKeepsPreExistingPending(t *testing.T) {
	m, _ := mutationModel(t, nil)
	stamp := time.Now()
	m.ciRerun[13] = stamp

	got := m.applyCIRerun([]gh.PR{{
		Number: 13,
		StatusCheckRollup: []gh.Check{{
			Name: "build", State: "IN_PROGRESS", StartedAt: stamp.Add(-time.Minute).Format(time.RFC3339),
		}},
	}})
	if _, still := m.ciRerun[13]; !still {
		t.Error("ciRerun[13] cleared on a check that started before the stamp")
	}
	if got[0].CIState() != "pending" {
		t.Errorf("CIState = %q, want pending — the override should still apply", got[0].CIState())
	}
}

// TestApplyCIRerunExpires bounds the lie: a PR whose workflows never re-fire
// (path filters, no push trigger) self-corrects instead of spinning forever.
func TestApplyCIRerunExpires(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now().Add(-3 * time.Minute) // older than ciRerunWindow

	got := m.applyCIRerun([]gh.PR{{
		Number:            13,
		StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}},
	}})
	if got[0].CIState() != "pass" {
		t.Errorf("CIState = %q, want pass — the override expired", got[0].CIState())
	}
	if _, still := m.ciRerun[13]; still {
		t.Error("expired ciRerun entry was not pruned")
	}
}

// TestApplyCIRerunDoesNotMutateInput guards the shared cache: fetched PR values
// are handed out by the cache layer, so the override must copy the rollup rather
// than write through the caller's backing array.
func TestApplyCIRerunDoesNotMutateInput(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now()

	in := []gh.PR{{Number: 13, StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}}}}
	m.applyCIRerun(in)
	if in[0].StatusCheckRollup[0].Conclusion != "SUCCESS" {
		t.Error("applyCIRerun wrote through to the caller's rollup")
	}
}

// TestUpdateBranchStampsCIRerun wires the flag end-to-end: a settled
// update-branch marks its PRs, a merge does not.
func TestUpdateBranchStampsCIRerun(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["u"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if _, ok := m.ciRerun[13]; !ok {
		t.Error("a successful update-branch must stamp ciRerun")
	}
}

func TestMergeDoesNotStampCIRerun(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["m"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if _, ok := m.ciRerun[13]; ok {
		t.Error("merge does not re-trigger checks and must not stamp ciRerun")
	}
}

func TestFailedUpdateBranchDoesNotStampCIRerun(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["u"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{err: errors.New("boom")})
	m = u.(Model)
	if _, ok := m.ciRerun[13]; ok {
		t.Error("a failed update-branch must not stamp ciRerun")
	}
}

// TestDelayedRefreshMsgTriggersFetch is the behavior that matters: when the
// scheduled tick lands, the board refetches.
func TestDelayedRefreshMsgTriggersFetch(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.refreshing = false

	u, cmd := m.Update(delayedRefreshMsg{})
	m = u.(Model)
	if !m.refreshing {
		t.Error("delayedRefreshMsg must start a refresh")
	}
	if cmd == nil {
		t.Error("delayedRefreshMsg must return the fetch command")
	}
}

// TestRerunsCIClassifiesActions pins which actions get the follow-up refetch and
// the optimistic in-progress paint: the two that re-trigger CI, and nothing else.
func TestRerunsCIClassifiesActions(t *testing.T) {
	defaults := action.DefaultPRActions()
	for key, want := range map[string]bool{"u": true, "r": true, "m": false, "A": false, "M": false, "L": false} {
		if got := rerunsCI(defaults[key]); got != want {
			t.Errorf("rerunsCI(%q) = %v, want %v", key, got, want)
		}
	}
}

// TestStartBulkPreservesRerunCI drives update-branch through the real path —
// startBulk → runBulk → runBulkNative → statForBulk — rather than hand-assigning
// statFor as the other rerunCI tests do, so the bulk constructor itself is covered.
func TestStartBulkPreservesRerunCI(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	driveBulk(t, m.startBulk(action.DefaultPRActions()["u"]))
	if m.actionStatus == nil || !m.actionStatus.rerunCI {
		t.Errorf("actionStatus = %+v, want rerunCI = true for update-branch", m.actionStatus)
	}
}

func TestOptimisticAutoMergePaintsGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN", Title: "x", Author: author("alice"),
	}})
	m.width, m.height = 120, 40
	m.actionStatus = statFor(action.DefaultPRActions()["A"])
	m.actionStatus.nums = []int{13}
	m.actionStatus.refresh = true

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	ps := m.section.(*PRSection)
	p := ps.prAt(0)
	if !p.AutoMergeEnabled() {
		t.Fatal("successful auto-merge must arm AutoMergeRequest on the in-memory PR")
	}
	m.renderList()
	if !strings.Contains(m.rowText[0], autoMergeGlyph(true)) {
		t.Fatalf("row should show auto-merge glyph immediately:\n%s", m.rowText[0])
	}
}

func TestFailedAutoMergeDoesNotPaintGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["A"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{err: errors.New("boom")})
	m = u.(Model)
	if m.section.(*PRSection).prAt(0).AutoMergeEnabled() {
		t.Fatal("failed auto-merge must leave AutoMergeRequest unset")
	}
}

func TestOptimisticDisableAutoMergeClearsGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN", Title: "x", Author: author("alice"),
		AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"},
	}})
	m.width, m.height = 120, 40
	m.actionStatus = statFor(action.AutoMergeAction(true))
	m.actionStatus.nums = []int{13}
	m.actionStatus.refresh = true

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	ps := m.section.(*PRSection)
	p := ps.prAt(0)
	if p.AutoMergeEnabled() {
		t.Fatal("successful disable auto-merge must clear AutoMergeRequest on the in-memory PR")
	}
	m.renderList()
	if strings.Contains(m.rowText[0], autoMergeGlyph(true)) {
		t.Fatalf("row should clear auto-merge glyph immediately:\n%s", m.rowText[0])
	}
}

func TestOptimisticMarkReadyAndConvertDraftPaintDraftState(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN", IsDraft: true}})
	m.actionStatus = statFor(action.ReadyAction(true))
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if m.section.(*PRSection).prAt(0).IsDraft {
		t.Fatal("successful mark-ready must clear IsDraft on the in-memory PR")
	}

	m.actionStatus = statFor(action.ReadyAction(false))
	m.actionStatus.nums = []int{13}
	u, _ = m.Update(actionDoneMsg{})
	m = u.(Model)
	if !m.section.(*PRSection).prAt(0).IsDraft {
		t.Fatal("successful convert-to-draft must set IsDraft on the in-memory PR")
	}
}

func TestOptimisticApprovePaintsReviewGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN",
		ReviewDecision: "REVIEW_REQUIRED", Title: "x",
	}})
	m.width, m.height = 120, 40
	m.viewerLogin = "me"
	m.detail[13] = gh.PRDetail{} // empty reviews → upsert path
	m.actionStatus = statFor(action.DefaultPRActions()["L"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if got := m.section.(*PRSection).prAt(0).ReviewDecision; got != "APPROVED" {
		t.Fatalf("ReviewDecision = %q, want APPROVED", got)
	}
	d := m.detail[13]
	found := false
	for _, r := range d.LatestReviews {
		if r.Author.Login == "me" && r.State == "APPROVED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("detail LatestReviews should carry viewer APPROVED, got %+v", d.LatestReviews)
	}
	m.renderList()
	if !strings.Contains(m.rowText[0], reviewApprovedGlyph) {
		t.Fatalf("row should show approved glyph immediately:\n%s", m.rowText[0])
	}
}

func TestOptimisticApproveWithoutDetailDoesNotFabricateEntry(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN",
		ReviewDecision: "REVIEW_REQUIRED", Title: "x",
	}})
	m.viewerLogin = "me"
	m.actionStatus = statFor(action.DefaultPRActions()["L"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if got := m.section.(*PRSection).prAt(0).ReviewDecision; got != "APPROVED" {
		t.Fatalf("ReviewDecision = %q, want APPROVED", got)
	}
	if _, ok := m.detail[13]; ok {
		t.Fatal("approve without pre-seeded detail must not fabricate m.detail entry")
	}
}

func TestFailedApproveDoesNotPaint(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN", ReviewDecision: "REVIEW_REQUIRED",
	}})
	m.actionStatus = statFor(action.DefaultPRActions()["L"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{err: errors.New("boom")})
	m = u.(Model)
	if got := m.section.(*PRSection).prAt(0).ReviewDecision; got != "REVIEW_REQUIRED" {
		t.Fatalf("failed approve must leave ReviewDecision, got %q", got)
	}
}

func TestOptimisticAutoMergeBulkPatchesAllNums(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{
		{Number: 1, ID: "n1", State: "OPEN"},
		{Number: 2, ID: "n2", State: "OPEN"},
	})
	m.actionStatus = statFor(action.DefaultPRActions()["A"])
	m.actionStatus.nums = []int{1, 2}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	ps := m.section.(*PRSection)
	for i := 0; i < ps.Len(); i++ {
		if !ps.prAt(i).AutoMergeEnabled() {
			t.Fatalf("PR #%d should be armed after bulk success", ps.prAt(i).Number)
		}
	}
}

// TestApproveActionContract pins the "L" binding: it must always confirm (own
// selection can't skip GitHub's opaque self-approval 422), name the author when
// approving someone else's PR, act per-selection, and refetch on success.
func TestApproveActionContract(t *testing.T) {
	a := action.DefaultPRActions()["L"]
	if !a.Confirm {
		t.Error("approve (L) must always confirm")
	}
	if !a.ConfirmOthers {
		t.Error("approve (L) must name the author when confirming")
	}
	if a.Scope != "per-selected" {
		t.Errorf("approve (L) scope = %q, want per-selected", a.Scope)
	}
	if !a.Refresh {
		t.Error("approve (L) must refresh on success")
	}
}

// A branch that names no ticket must settle to a hint, not silently no-op.
func TestOpenIssueNoTicketHints(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "agents/no-id-here",
		URL: "https://github.com/noamsto/prdash/pull/7"}})
	a := action.Action{Key: "O", Label: "Open linked issue",
		Command: action.Command{Native: "open-issue"}, Scope: "per-selected"}

	cmd := m.runBulk(a)
	if m.actionStatus == nil {
		t.Fatal("no ticket must set a status, not leave it nil")
	}
	if !m.actionStatus.settled {
		t.Error("hint status should be settled — nothing is in flight")
	}
	if m.actionStatus.err == nil {
		t.Error("hint status must carry an error so it paints ✗, not ✓")
	}
	if !strings.Contains(m.actionStatus.fail, "no linked issue") {
		t.Errorf("status = %q, want it to mention \"no linked issue\"", m.actionStatus.fail)
	}
	if cmd == nil {
		t.Error("hint must return a clear-status cmd so it doesn't stick")
	}
}

// A Linear ticket with no linear CLI on PATH must name the missing binary
// rather than opening a URL we never confirmed.
func TestOpenIssueMissingCLIHints(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no linear, no xdg-open
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 8, HeadRefName: "eng-7659-must-differ-guard",
		URL: "https://github.com/noamsto/prdash/pull/8"}})
	a := action.Action{Key: "O", Label: "Open linked issue",
		Command: action.Command{Native: "open-issue"}, Scope: "per-selected"}

	cmd := m.runBulk(a)
	if m.actionStatus == nil {
		t.Fatal("missing CLI must set a status")
	}
	if !m.actionStatus.settled {
		t.Error("hint status should be settled — nothing is in flight")
	}
	if m.actionStatus.err == nil {
		t.Error("hint status must carry an error so it paints ✗, not ✓")
	}
	if !strings.Contains(m.actionStatus.fail, "linear") {
		t.Errorf("status = %q, want it to name the linear CLI", m.actionStatus.fail)
	}
	if !strings.Contains(m.actionStatus.fail, "ENG-7659") {
		t.Errorf("status = %q, want it to name the ticket", m.actionStatus.fail)
	}
	if cmd == nil {
		t.Error("hint must return a clear-status cmd so it doesn't stick")
	}
}

// A mixed selection where some rows resolve and others don't must still open
// the resolvable ones, and must name the ones it skipped rather than letting
// them vanish with the selection that gets cleared.
func TestOpenIssueMixedSelectionReportsSkipped(t *testing.T) {
	// A stub opener keeps this hermetic: the two resolvable rows really do
	// spawn, so the ×2-plus-skipped wording is exercised end to end.
	dir := t.TempDir()
	stub := filepath.Join(dir, browserArgv(runtime.GOOS)[0])
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	m := NewModel("/repo", "is:open", nil)
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{
		{Number: 1, HeadRefName: "feat/213-a", URL: "https://github.com/o/r/pull/1"},
		{Number: 2, HeadRefName: "feat/214-b", URL: "https://github.com/o/r/pull/2"},
		{Number: 3, HeadRefName: "agents/no-id", URL: "https://github.com/o/r/pull/3"},
	})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)
	m.sel.toggle(2)

	a := action.Action{Key: "O", Label: "Open linked issue",
		Command: action.Command{Native: "open-issue"}, Scope: "per-selected"}
	m.runBulk(a)

	if m.actionStatus == nil {
		t.Fatal("mixed selection must set a status")
	}
	if !strings.Contains(m.actionStatus.ok, "1 skipped") {
		t.Errorf("status = %q, want it to report 1 skipped row", m.actionStatus.ok)
	}
	if !strings.Contains(m.actionStatus.ok, "×2") {
		t.Errorf("status = %q, want it to report the 2 rows that opened", m.actionStatus.ok)
	}
}

func TestDefaultPRActionsHasOpenIssue(t *testing.T) {
	a, ok := action.DefaultPRActions()["O"]
	if !ok {
		t.Fatal("O missing from DefaultPRActions")
	}
	if a.Command.Native != "open-issue" {
		t.Errorf("Native = %q, want open-issue", a.Command.Native)
	}
	if a.Scope != "per-selected" {
		t.Errorf("Scope = %q, want per-selected (matching o)", a.Scope)
	}
	if _, ok := action.DefaultIssueActions()["O"]; ok {
		t.Error("O must not be bound on the issue board — a row there IS the issue")
	}
}
