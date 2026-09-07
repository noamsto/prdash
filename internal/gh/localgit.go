package gh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SwitchPreflight is the read-only result of checking a worktrunk switch.
type SwitchPreflight struct {
	Path, Occupant, Remedy string
	Warnings               []string
}

type worktreeEntry struct {
	Branch string
	Path   string
}

var runWorktreeListCommand = func(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := newBoundedCmd(ctx, name, args...)
	cmd.Dir = dir
	return cmd.Output() // stderr intentionally excluded
}

var runWorktreeList = func(dir string) ([]byte, error) {
	return runWorktreeListCommand(dir, "git", "-C", dir, "worktree", "list", "--porcelain")
}

// PreflightSwitch checks the path worktrunk is likely to use and inspects an
// occupant directly. It never changes either repository.
func PreflightSwitch(dir, branch string) (SwitchPreflight, error) {
	out, err := runWorktreeList(dir)
	if err != nil {
		return SwitchPreflight{}, err
	}
	entries := parseWorktreeList(out)
	path := expectedWorktreePath(branch, entries)
	if path == "" {
		return SwitchPreflight{}, nil
	}
	for _, e := range entries {
		if e.Path == "" || e.Path != path {
			continue
		}
		collision, occupant, warnings := inspectOccupant(e.Path, branch)
		if !collision {
			return SwitchPreflight{}, nil
		}
		if ahead, behind, ok := worktreeDivergence(e.Path, branch); ok && (ahead != 0 || behind != 0) {
			warnings = append(warnings, fmt.Sprintf("diverges from PR head (ahead %d, behind %d)", ahead, behind))
		}
		return SwitchPreflight{
			Path:     e.Path,
			Occupant: occupant,
			Remedy:   fmt.Sprintf("git -C %s switch -- %s", shellQuoteSingle(e.Path), shellQuoteSingle(branch)),
			Warnings: warnings,
		}, nil
	}
	return SwitchPreflight{}, nil
}

// shellQuoteSingle wraps s in single quotes for safe use in a shell command,
// escaping any embedded single quotes with the standard POSIX trick.
func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func expectedWorktreePath(branch string, entries []worktreeEntry) string {
	// Worktrunk flattens branch separators in the directory name (e.g.
	// feat/123-x becomes feat-123-x), while retaining the original branch for Git.
	pathBranch := strings.ReplaceAll(branch, "/", "-")
	// Worktrunk's conventional roots are visible in its own list output.
	for _, e := range entries {
		parts := strings.Split(filepath.Clean(e.Path), string(filepath.Separator))
		for i := range parts {
			if parts[i] == ".worktrees" && i+2 < len(parts) {
				baseParts := parts[:i+3]
				if filepath.IsAbs(e.Path) {
					baseParts = append([]string{string(filepath.Separator)}, parts[1:i+3]...)
				}
				return filepath.Join(filepath.Join(baseParts...), pathBranch)
			}
		}
	}
	return ""
}

// inspectOccupant asks git the ground truth about what occupies path, since an
// inventory branch field can report the rebase target even when HEAD is
// actually detached mid-rebase — exactly the case this feature exists to catch.
func inspectOccupant(path, branch string) (collision bool, occupant string, warnings []string) {
	gitDir, err := gitOutput(path, "rev-parse", "--git-dir")
	if err != nil {
		// Fail open: a stale or deleted worktree entry degrades to today's
		// silent handoff, not a hard error or a false-positive collision.
		return false, "", nil
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(path, gitDir)
	}
	var mid bool
	for _, marker := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		if _, err := os.Stat(filepath.Join(gitDir, marker)); err == nil {
			warnings = append(warnings, marker+" in progress")
			mid = true
		}
	}
	status, err := gitOutput(path, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return false, "", nil
	}
	var hasConflict, hasDirty, hasUntracked bool
	head := ""
	for _, line := range strings.Split(status, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			head = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "u "):
			hasConflict = true
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			hasDirty = true
		case strings.HasPrefix(line, "? "):
			hasUntracked = true
		}
	}
	if hasConflict {
		warnings = append(warnings, "unresolved conflicts")
	}
	if hasDirty {
		warnings = append(warnings, "staged or uncommitted work")
	}
	if hasUntracked {
		warnings = append(warnings, "untracked files")
	}
	if head == "" || head == "(detached)" {
		warnings = append(warnings, "detached HEAD")
		occupant = "(detached HEAD)"
	} else {
		occupant = head
	}
	collision = occupant != branch || mid || hasConflict
	return collision, occupant, warnings
}

func worktreeDivergence(path, branch string) (ahead, behind int, ok bool) {
	out, err := gitOutput(path, "rev-list", "--left-right", "--count", branch+"..."+branch+"@{upstream}")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, false
	}
	ahead, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	behind, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}

var gitOutput = func(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := newBoundedCmd(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// BranchExists reports whether branch is a local branch of the repo at dir.
func BranchExists(dir, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	// A timeout collapses into false, same as "branch not found" — acceptable
	// here since callers already treat "can't tell" the same as "doesn't exist".
	err := newBoundedCmd(ctx, "git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run()
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
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	// Same reasoning as BranchExists: a timeout collapses into false.
	err := newBoundedCmd(ctx, "git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch).Run()
	return err == nil
}

// WorktreeForBranch returns the path of the worktree branch is checked out in, if
// any. A branch with no worktree of its own reports false.
func WorktreeForBranch(dir, branch string) (string, bool) {
	out, err := runWorktreeList(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range parseWorktreeList(out) {
		if entry.Branch == branch {
			return entry.Path, entry.Path != ""
		}
	}
	return "", false
}

// parseWorktreeList reads the stanza-based format emitted by
// `git worktree list --porcelain`.
func parseWorktreeList(out []byte) []worktreeEntry {
	var entries []worktreeEntry
	// Porcelain output is stanzas separated by blank lines: "worktree <path>",
	// then optional "HEAD <sha>" and "branch refs/heads/<name>" lines.
	var entry *worktreeEntry
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "worktree "):
			entries = append(entries, worktreeEntry{Path: strings.TrimPrefix(line, "worktree ")})
			entry = &entries[len(entries)-1]
		case entry != nil && strings.HasPrefix(line, "branch refs/heads/"):
			entry.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	return entries
}

// RemoveWorktree removes the worktree at path. It deliberately omits --force, so
// uncommitted work blocks the removal instead of being discarded.
func RemoveWorktree(dir, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mutatingGitTimeout)
	defer cancel()
	out, err := newBoundedCmd(ctx, "git", "-C", dir, "worktree", "remove", path).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("git worktree remove timed out after %s (the worktree may be half-removed or its lock stale — check `git worktree list`): %w", mutatingGitTimeout, ctx.Err())
		}
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteBranch deletes a local branch with -D. The force is deliberate: a
// squash-merged branch — prdash's default merge — is not an ancestor of its base,
// so -d refuses it. Callers gate this on GitHub reporting the PR as merged, which
// is the authority on whether the work landed.
func DeleteBranch(dir, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mutatingGitTimeout)
	defer cancel()
	out, err := newBoundedCmd(ctx, "git", "-C", dir, "branch", "-D", branch).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("git branch -D timed out after %s: %w", mutatingGitTimeout, ctx.Err())
		}
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}
