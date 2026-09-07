package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SwitchPreflight is the read-only result of checking a worktrunk switch.
type SwitchPreflight struct {
	Path, Occupant, Remedy string
	Warnings               []string
}

type wtListEntry struct {
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	Worktree struct {
		Detached bool `json:"detached"`
	} `json:"worktree"`
	Remote struct {
		Ahead  int `json:"ahead"`
		Behind int `json:"behind"`
	} `json:"remote"`
}

var runWtList = func(dir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := newBoundedCmd(ctx, "wt", "list", "--format", "json")
	cmd.Dir = dir
	return cmd.Output() // stderr intentionally excluded
}

// PreflightSwitch checks the path worktrunk is likely to use and inspects an
// occupant directly. It never changes either repository.
func PreflightSwitch(dir, branch string) (SwitchPreflight, error) {
	out, err := runWtList(dir)
	if err != nil {
		return SwitchPreflight{}, err
	}
	var entries []wtListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		// Be permissive about a future envelope while retaining schema-1 support.
		var envelope map[string]json.RawMessage
		if e := json.Unmarshal(out, &envelope); e != nil {
			return SwitchPreflight{}, fmt.Errorf("parse wt list: %w", err)
		}
		for _, key := range []string{"worktrees", "entries", "list"} {
			if raw, ok := envelope[key]; ok && json.Unmarshal(raw, &entries) == nil {
				break
			}
		}
		if entries == nil {
			return SwitchPreflight{}, fmt.Errorf("parse wt list: %w", err)
		}
	}
	path := expectedWorktreePath(dir, branch, entries)
	for _, e := range entries {
		if e.Path == "" || e.Path != path || e.Branch == "" || e.Branch == branch {
			continue
		}
		r := SwitchPreflight{Path: e.Path, Occupant: e.Branch, Remedy: fmt.Sprintf("cd %s && git switch %s", e.Path, branch)}
		r.Warnings = inspectWorktree(e.Path, e.Remote.Ahead, e.Remote.Behind)
		return r, nil
	}
	return SwitchPreflight{}, nil
}

func expectedWorktreePath(dir, branch string, entries []wtListEntry) string {
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
	root := dir
	if p, err := filepath.Abs(dir); err == nil {
		root = p
	}
	return filepath.Join(filepath.Dir(root), pathBranch)
}

func inspectWorktree(path string, ahead, behind int) []string {
	var warnings []string
	gitDir, err := gitOutput(path, "rev-parse", "--git-dir")
	if err != nil {
		return []string{"could not inspect Git state"}
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(path, gitDir)
	}
	for _, marker := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		if _, err := os.Stat(filepath.Join(gitDir, marker)); err == nil {
			warnings = append(warnings, marker+" in progress")
		}
	}
	status, err := gitOutput(path, "status", "--porcelain=v2", "--branch")
	if err == nil {
		for _, line := range strings.Split(status, "\n") {
			if strings.HasPrefix(line, "u ") {
				warnings = append(warnings, "unresolved conflicts")
				break
			}
			if strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ") || strings.HasPrefix(line, "? ") {
				warnings = append(warnings, "staged or uncommitted work")
				break
			}
		}
		if strings.Contains(status, "# branch.head (detached)") {
			warnings = append(warnings, "detached HEAD")
		}
	}
	if ahead != 0 || behind != 0 {
		warnings = append(warnings, fmt.Sprintf("diverges from PR head (ahead %d, behind %d)", ahead, behind))
	}
	return warnings
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
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	// A timeout collapses into ("", false), same as "no worktree list
	// available" — acceptable here since no caller distinguishes why.
	out, err := newBoundedCmd(ctx, "git", "-C", dir, "worktree", "list", "--porcelain").Output()
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
