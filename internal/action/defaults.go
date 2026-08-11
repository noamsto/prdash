package action

func DefaultPRActions() map[string]Action {
	return map[string]Action{
		// pr:{N}, not {{.Branch}}: wt switch resolves a branch name against local
		// refs only, so a PR pushed since the last fetch has nothing to resolve —
		// and a fork PR's head is never on origin at all. pr:{N} fetches what's
		// missing and still lands on the real head branch.
		"enter": {Key: "enter", Label: "Open worktree",
			Command:  Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}},
			ExitsTUI: true, Scope: "single"},
		"m": {Key: "m", Label: "Merge (squash)",
			Command: Command{Native: "merge-squash"},
			Confirm: true, Scope: "per-selected", Refresh: true,
			Progress: "Merging", Past: "Merged", Fail: "Merge failed"},
		"A": {Key: "A", Label: "Auto-merge (squash)",
			Command: Command{Native: "auto-merge-squash"},
			Scope:   "per-selected", ConfirmOthers: true, Refresh: true,
			Progress: "Enabling auto-merge", Past: "Auto-merge on", Fail: "Auto-merge failed"},
		"r": {Key: "r", Label: "Rerun checks",
			Command: Command{Builtin: "rerun-failed"}, Scope: "single", Refresh: true,
			Progress: "Rerunning checks", Past: "Checks re-running", Fail: "Rerun failed"},
		"y": {Key: "y", Label: "Copy PR #",
			Command: Command{Builtin: "copy-number"}, Scope: "single"},
		"Y": {Key: "Y", Label: "Copy URL",
			Command: Command{Builtin: "copy-url"}, Scope: "single"},
		"b": {Key: "b", Label: "Copy branch",
			Command: Command{Builtin: "copy-branch"}, Scope: "single"},
		"o": {Key: "o", Label: "Open in browser",
			Command: Command{Native: "open-web"}, Scope: "per-selected"},
		"W": {Key: "W", Label: "Bulk worktrees",
			Command:  Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}},
			ExitsTUI: true, Scope: "per-selected"},
		"u": {Key: "u", Label: "Update branch",
			Command: Command{Native: "update-branch"}, Scope: "per-selected", Refresh: true,
			Progress: "Updating branch", Past: "Branch updated — checks re-running", Fail: "Update failed"},
		"M": {Key: "M", Label: "Mark ready",
			Command: Command{Native: "mark-ready"}, Scope: "per-selected", Refresh: true,
			ConfirmOthers: true,
			Progress:      "Marking ready", Past: "Marked ready", Fail: "Mark-ready failed"},
		"L": {Key: "L", Label: "Approve",
			Command: Command{Native: "approve"}, Scope: "per-selected", Refresh: true,
			Confirm: true, ConfirmOthers: true,
			Progress: "Approving", Past: "Approved", Fail: "Approve failed"},
		// Local-only: it deletes a merged PR's leftover branch and worktree, so it
		// needs no Refresh — nothing on GitHub changed.
		"X": {Key: "X", Label: "Clean up branch",
			Command: Command{Builtin: "cleanup-branch"}, Scope: "single", Confirm: true,
			Progress: "Cleaning up", Past: "Branch cleaned up", Fail: "Cleanup failed"},
	}
}

func DefaultIssueActions() map[string]Action {
	return map[string]Action{
		"enter": {Key: "enter", Label: "Open worktree",
			Command:  Command{Argv: []string{"wt", "switch", "-c", "{{.Branch}}"}},
			ExitsTUI: true, Scope: "single"},
		"W": {Key: "W", Label: "Bulk worktrees",
			Command:  Command{Argv: []string{"wt", "switch", "-c", "{{.Branch}}"}},
			ExitsTUI: true, Scope: "per-selected"},
		"o": {Key: "o", Label: "Open in browser",
			Command: Command{Native: "open-web"}, Scope: "per-selected"},
		"y": {Key: "y", Label: "Copy issue #",
			Command: Command{Builtin: "copy-number"}, Scope: "single"},
		"Y": {Key: "Y", Label: "Copy URL",
			Command: Command{Builtin: "copy-url"}, Scope: "single"},
		"b": {Key: "b", Label: "Copy branch",
			Command: Command{Builtin: "copy-branch"}, Scope: "single"},
	}
}
