package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// cleanupDone runs the cleanup and carries its error into the badge text, the way
// a single-target bulk failure does: the action's own "Cleanup failed" wording
// doesn't say whether the branch was already gone, the worktree had uncommitted
// work, or prdash is sitting in the worktree it was asked to remove.
func cleanupDone(gitDir, cwd string, p gh.PR, stat *actionStat) tea.Msg {
	if err := cleanupBranch(gitDir, cwd, p); err != nil {
		return actionDoneMsg{stat: stat, err: err, fail: err.Error()}
	}
	return actionDoneMsg{stat: stat}
}

// cleanupBranch deletes a merged PR's leftover local branch and, if it has one,
// its worktree. Every precondition is checked before anything is removed, and the
// worktree goes first — git refuses to delete a branch that is checked out, and an
// aborted removal must not leave the branch already gone.
func cleanupBranch(gitDir, cwd string, p gh.PR) error {
	if !p.IsMerged() {
		return fmt.Errorf("#%d is not merged", p.Number)
	}
	if !gh.BranchExists(gitDir, p.HeadRefName) {
		return fmt.Errorf("no local branch %s", p.HeadRefName)
	}
	if wt, ok := gh.WorktreeForBranch(gitDir, p.HeadRefName); ok {
		if under(cwd, wt) {
			return fmt.Errorf("can't remove the worktree prdash is running in (%s)", wt)
		}
		if err := gh.RemoveWorktree(gitDir, wt); err != nil {
			return err
		}
	}
	return gh.DeleteBranch(gitDir, p.HeadRefName)
}

// under reports whether dir is root or sits inside it. Both sides are resolved
// first because git reports worktree paths with symlinks expanded, which dir
// (prdash's cwd) may not be.
func under(dir, root string) bool {
	d, errDir := filepath.EvalSymlinks(dir)
	r, errRoot := filepath.EvalSymlinks(root)
	if errDir != nil || errRoot != nil {
		d, r = filepath.Clean(dir), filepath.Clean(root)
	}
	rel, err := filepath.Rel(r, d)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
