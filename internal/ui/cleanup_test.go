package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func cleanupRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func p61() gh.PR { return gh.PR{Number: 61, State: "MERGED", HeadRefName: "feat/x"} }

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCleanupBranchRefusesUnmergedPR(t *testing.T) {
	dir := cleanupRepo(t)
	gitIn(t, dir, "branch", "feat/x")
	p := gh.PR{Number: 61, State: "OPEN", HeadRefName: "feat/x"}

	err := cleanupBranch(dir, p)
	if err == nil {
		t.Fatal("cleaned up the branch of an open PR")
	}
	if !strings.Contains(err.Error(), "not merged") {
		t.Errorf("err = %q, want it to say the PR is not merged", err)
	}
	if !gh.BranchExists(dir, "feat/x") {
		t.Error("branch was deleted despite the refusal")
	}
}

func TestCleanupBranchReportsMissingLocalBranch(t *testing.T) {
	dir := cleanupRepo(t)
	p := gh.PR{Number: 61, State: "MERGED", HeadRefName: "feat/gone"}

	err := cleanupBranch(dir, p)
	if err == nil {
		t.Fatal("reported success with no local branch to clean up")
	}
	if !strings.Contains(err.Error(), "feat/gone") {
		t.Errorf("err = %q, want it to name the missing branch", err)
	}
}

// The common case: you merge the PR for the branch you are sitting in.
func TestCleanupBranchRefusesItsOwnWorktree(t *testing.T) {
	dir := cleanupRepo(t)
	wt := filepath.Join(t.TempDir(), "feat-x")
	gitIn(t, dir, "worktree", "add", "-b", "feat/x", wt)
	p := gh.PR{Number: 61, State: "MERGED", HeadRefName: "feat/x"}

	err := cleanupBranch(wt, p) // prdash running *in* the worktree it would remove
	if err == nil {
		t.Fatal("removed the worktree prdash is running in")
	}
	if !strings.Contains(err.Error(), "running in") {
		t.Errorf("err = %q, want it to explain the refusal", err)
	}
	if !gh.BranchExists(dir, "feat/x") {
		t.Error("branch was deleted despite the refusal")
	}
	if _, ok := gh.WorktreeForBranch(dir, "feat/x"); !ok {
		t.Error("worktree was removed despite the refusal")
	}
}

func TestCleanupBranchRemovesWorktreeThenBranch(t *testing.T) {
	dir := cleanupRepo(t)
	wt := filepath.Join(t.TempDir(), "feat-x")
	gitIn(t, dir, "worktree", "add", "-b", "feat/x", wt)
	p := gh.PR{Number: 61, State: "MERGED", HeadRefName: "feat/x"}

	if err := cleanupBranch(dir, p); err != nil {
		t.Fatalf("cleanupBranch: %v", err)
	}
	if _, ok := gh.WorktreeForBranch(dir, "feat/x"); ok {
		t.Error("worktree still registered")
	}
	if gh.BranchExists(dir, "feat/x") {
		t.Error("branch still exists")
	}
}

// A worktree with uncommitted work blocks the whole cleanup, branch included.
func TestCleanupBranchKeepsBranchWhenWorktreeRemovalFails(t *testing.T) {
	dir := cleanupRepo(t)
	wt := filepath.Join(t.TempDir(), "feat-x")
	gitIn(t, dir, "worktree", "add", "-b", "feat/x", wt)
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("work in progress"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", "wip.txt")

	if err := cleanupBranch(dir, p61()); err == nil {
		t.Fatal("cleaned up a worktree with uncommitted work")
	}
	if !gh.BranchExists(dir, "feat/x") {
		t.Error("branch was deleted even though the worktree removal failed")
	}
}

// The badge is the only place a cleanup failure is reported, so it has to carry
// the reason: "Cleanup failed" alone leaves you guessing which precondition bit.
func TestCleanupFailureSurfacesReasonInBadge(t *testing.T) {
	dir := cleanupRepo(t) // has no feat/x branch
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 140, 40
	m.actionStatus = &actionStat{run: "Cleaning up", ok: "Branch cleaned up", fail: "Cleanup failed"}

	updated, _ := m.Update(cleanupDone(dir, p61()))
	m = updated.(Model)

	if got := ansi.Strip(m.header()); !strings.Contains(got, "no local branch feat/x") {
		t.Errorf("header badge = %q, want it to name the reason the cleanup failed", got)
	}
}

func TestCleanupSuccessReportsNoFailure(t *testing.T) {
	dir := cleanupRepo(t)
	gitIn(t, dir, "branch", "feat/x")

	msg, ok := cleanupDone(dir, p61()).(actionDoneMsg)
	if !ok {
		t.Fatalf("cleanupDone returned %T, want actionDoneMsg", msg)
	}
	if msg.err != nil || msg.fail != "" {
		t.Errorf("cleanupDone = %+v, want a clean result", msg)
	}
}

// The X binding must reach the cleanup builtin, confirm first, and name the
// branch it is about to force-delete.
func TestCleanupActionIsConfirmGatedAndNamesTheBranch(t *testing.T) {
	a, ok := action.DefaultPRActions()["X"]
	if !ok {
		t.Fatal("no X binding for branch cleanup")
	}
	if a.Command.Builtin != "cleanup-branch" {
		t.Errorf("X builtin = %q, want cleanup-branch", a.Command.Builtin)
	}
	if !a.Confirm {
		t.Error("branch cleanup is not confirm-gated")
	}
	if a.Refresh {
		t.Error("branch cleanup sets Refresh, but nothing changed on GitHub")
	}

	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{p61()})
	m.pending = &a

	if q := m.confirmQuestion(); !strings.Contains(q, "feat/x") {
		t.Errorf("confirm question = %q, want it to name the branch", q)
	}
}
