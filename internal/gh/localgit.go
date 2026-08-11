package gh

import (
	"fmt"
	"os/exec"
	"strings"
)

// BranchExists reports whether branch is a local branch of the repo at dir.
func BranchExists(dir, branch string) bool {
	err := exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run()
	return err == nil
}

// SwitchRef is the argument to hand `wt switch` for a PR: its head branch when
// this clone can already resolve it, else the pr:{N} shortcut.
//
// The branch form costs ~12ms; pr:{N} makes worktrunk resolve the number
// through gh first, ~2.2s. So take the branch whenever it can work, and reach
// for pr:{N} only where it can't: `wt switch <branch>` matches against refs
// already present locally and never fetches, so a branch pushed since the last
// fetch has nothing to resolve, and a fork's head is not under origin/ at all.
//
// The fork check is not redundant with the ref probe: a fork PR opened from its
// default branch has HeadRefName "main", and origin/main always resolves — to
// the wrong commits, silently.
func SwitchRef(dir string, p PR) string {
	if !p.IsCrossRepository && remoteBranchExists(dir, p.HeadRefName) {
		return p.HeadRefName
	}
	return fmt.Sprintf("pr:%d", p.Number)
}

// remoteBranchExists reports whether dir has already fetched origin/branch.
func remoteBranchExists(dir, branch string) bool {
	if branch == "" {
		return false
	}
	err := exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch).Run()
	return err == nil
}

// WorktreeForBranch returns the path of the worktree branch is checked out in, if
// any. A branch with no worktree of its own reports false.
func WorktreeForBranch(dir, branch string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", false
	}
	// Porcelain output is stanzas separated by blank lines: "worktree <path>",
	// then optional "HEAD <sha>" and "branch refs/heads/<name>" lines.
	path := ""
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path, path != ""
		}
	}
	return "", false
}

// RemoveWorktree removes the worktree at path. It deliberately omits --force, so
// uncommitted work blocks the removal instead of being discarded.
func RemoveWorktree(dir, path string) error {
	out, err := exec.Command("git", "-C", dir, "worktree", "remove", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteBranch deletes a local branch with -D. The force is deliberate: a
// squash-merged branch — prdash's default merge — is not an ancestor of its base,
// so -d refuses it. Callers gate this on GitHub reporting the PR as merged, which
// is the authority on whether the work landed.
func DeleteBranch(dir, branch string) error {
	out, err := exec.Command("git", "-C", dir, "branch", "-D", branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}
