package gh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightSwitchFixtureCollisionAndRebase(t *testing.T) {
	oldList, oldGit := runWtList, gitOutput
	t.Cleanup(func() { runWtList, gitOutput = oldList, oldGit })
	root := filepath.Join(t.TempDir(), ".worktrees", "owner", "repo")
	path := filepath.Join(root, "feature")
	if err := os.MkdirAll(filepath.Join(path, ".git", "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	runWtList = func(string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"branch":"tmp4","path":%q,"working_tree":{"staged":false,"modified":false,"untracked":false},"remote":{"ahead":1,"behind":2}}]`, path)), nil
	}
	gitOutput = func(dir string, args ...string) (string, error) {
		if len(args) == 2 && args[0] == "rev-parse" {
			return filepath.Join(path, ".git"), nil
		}
		if len(args) > 0 && args[0] == "status" {
			return "# branch.head (detached)\nu UU 1 2 3 4 5 6 7 8 9", nil
		}
		return "", nil
	}
	r, err := PreflightSwitch(filepath.Join(root, "main"), "feature")
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != path || r.Occupant != "tmp4" || !strings.Contains(r.Remedy, "git switch feature") {
		t.Fatalf("collision = %+v", r)
	}
	warnings := strings.Join(r.Warnings, " ")
	for _, want := range []string{"rebase-merge in progress", "unresolved conflicts", "detached HEAD", "diverges from PR head"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings %q missing %q", warnings, want)
		}
	}
}

func TestPreflightSwitchFixtureCleanTarget(t *testing.T) {
	old := runWtList
	t.Cleanup(func() { runWtList = old })
	runWtList = func(string) ([]byte, error) { return []byte(`[{"branch":"feature","path":"/tmp/other"}]`), nil }
	r, err := PreflightSwitch("/tmp/repo", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "" {
		t.Fatalf("clean target = %+v", r)
	}
}

func TestPreflightSwitchSlugifiesBranchSeparators(t *testing.T) {
	old := runWtList
	t.Cleanup(func() { runWtList = old })
	root := filepath.Join(t.TempDir(), ".worktrees", "owner", "repo")
	path := filepath.Join(root, "feat-123-x")
	runWtList = func(string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"branch":"tmp4","path":%q}]`, path)), nil
	}
	oldGit := gitOutput
	t.Cleanup(func() { gitOutput = oldGit })
	gitOutput = func(string, ...string) (string, error) { return filepath.Join(path, ".git"), nil }
	r, err := PreflightSwitch(filepath.Join(root, "main"), "feat/123-x")
	if err != nil || r.Path != path || r.Occupant != "tmp4" {
		t.Fatalf("slash collision = %+v, %v", r, err)
	}
}

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

func TestSwitchRefPrefersAFetchedBranch(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "update-ref", "refs/remotes/origin/feat/x", "HEAD")

	pr := PR{Number: 7, HeadRefName: "feat/x"}
	if got := SwitchRef(dir, pr); got != "feat/x" {
		t.Errorf("SwitchRef = %q, want the branch — pr:{N} costs a gh round-trip", got)
	}
}

func TestSwitchRefFallsBackWhenUnfetched(t *testing.T) {
	dir := initRepo(t)

	pr := PR{Number: 7, HeadRefName: "feat/x"}
	if got := SwitchRef(dir, pr); got != "pr:7" {
		t.Errorf("SwitchRef = %q, want pr:7 — wt switch can't resolve an unfetched branch", got)
	}
}

// A fork PR opened from its default branch collides with a ref that always
// resolves, so the ref probe alone would switch to the wrong commits.
func TestSwitchRefFallsBackForAForkEvenWhenTheRefResolves(t *testing.T) {
	dir := initRepo(t)
	git(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")

	pr := PR{Number: 7, HeadRefName: "main", IsCrossRepository: true}
	if got := SwitchRef(dir, pr); got != "pr:7" {
		t.Errorf("SwitchRef = %q, want pr:7 — origin/main is not the fork's head", got)
	}
}
