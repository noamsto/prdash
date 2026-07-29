package gh

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo builds a real one-commit repo in a temp dir, since these helpers are
// thin wrappers over git and only exercising git proves anything about them.
func initRepo(t *testing.T) string {
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
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestBranchExists(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "branch", "feat/x")

	if !BranchExists(dir, "feat/x") {
		t.Error("BranchExists = false for a branch that exists")
	}
	if BranchExists(dir, "feat/nope") {
		t.Error("BranchExists = true for a branch that doesn't exist")
	}
}

func TestWorktreeForBranch(t *testing.T) {
	dir := initRepo(t)
	wt := filepath.Join(t.TempDir(), "feat-x")
	git(t, dir, "worktree", "add", "-b", "feat/x", wt)

	got, ok := WorktreeForBranch(dir, "feat/x")
	if !ok {
		t.Fatal("WorktreeForBranch found nothing for a branch with a worktree")
	}
	// git resolves symlinks (/tmp → /private/tmp on darwin), so compare resolved paths.
	want, _ := filepath.EvalSymlinks(wt)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != want {
		t.Errorf("WorktreeForBranch = %q, want %q", gotResolved, want)
	}

	git(t, dir, "branch", "feat/no-worktree")
	if got, ok := WorktreeForBranch(dir, "feat/no-worktree"); ok {
		t.Errorf("WorktreeForBranch = %q for a branch with no worktree, want none", got)
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir := initRepo(t)
	wt := filepath.Join(t.TempDir(), "feat-x")
	git(t, dir, "worktree", "add", "-b", "feat/x", wt)

	if err := RemoveWorktree(dir, wt); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, ok := WorktreeForBranch(dir, "feat/x"); ok {
		t.Error("worktree still registered after RemoveWorktree")
	}
}

// No --force, so uncommitted work blocks the cleanup instead of being discarded.
func TestRemoveWorktreeRefusesDirtyTree(t *testing.T) {
	dir := initRepo(t)
	wt := filepath.Join(t.TempDir(), "feat-x")
	git(t, dir, "worktree", "add", "-b", "feat/x", wt)
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("work in progress"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, wt, "add", "scratch.txt")

	if err := RemoveWorktree(dir, wt); err == nil {
		t.Error("RemoveWorktree removed a worktree with uncommitted work")
	}
	if _, ok := WorktreeForBranch(dir, "feat/x"); !ok {
		t.Error("worktree was unregistered despite the refusal")
	}
}

// The -D case this exists for: a squash-merged branch is not an ancestor of main,
// so -d would refuse it.
func TestDeleteBranchForcesUnmergedBranch(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "checkout", "-b", "feat/x")
	git(t, dir, "commit", "--allow-empty", "-m", "work that landed as a squash")
	git(t, dir, "checkout", "main")

	if err := DeleteBranch(dir, "feat/x"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if BranchExists(dir, "feat/x") {
		t.Error("branch still exists after DeleteBranch")
	}
}

func TestDeleteBranchReportsGitError(t *testing.T) {
	dir := initRepo(t)
	if err := DeleteBranch(dir, "feat/nope"); err == nil {
		t.Error("DeleteBranch reported success for a branch that doesn't exist")
	}
}
