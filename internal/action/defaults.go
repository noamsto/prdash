package action

func DefaultPRActions() map[string]Action {
	return map[string]Action{
		"enter": {Key: "enter", Label: "Open worktree",
			Command:  Command{Argv: []string{"wt", "switch", "{{.Branch}}"}},
			ExitsTUI: true, Scope: "single"},
		"m": {Key: "m", Label: "Merge (squash)",
			Command: Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--squash"}},
			Confirm: true, Scope: "per-selected", Refresh: true,
			Progress: "Merging", Past: "Merged", Fail: "Merge failed"},
		"A": {Key: "A", Label: "Auto-merge (squash)",
			Command:       Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--auto", "--squash"}},
			Scope:         "per-selected", ConfirmOthers: true,
			Progress: "Enabling auto-merge", Past: "Auto-merge on", Fail: "Auto-merge failed"},
		"r": {Key: "r", Label: "Rerun checks",
			Command: Command{Builtin: "rerun-failed"}, Scope: "single", Refresh: true,
			Progress: "Rerunning checks", Past: "Checks rerun", Fail: "Rerun failed"},
		"y": {Key: "y", Label: "Copy PR #",
			Command: Command{Builtin: "copy-number"}, Scope: "single"},
		"Y": {Key: "Y", Label: "Copy URL",
			Command: Command{Builtin: "copy-url"}, Scope: "single"},
		"b": {Key: "b", Label: "Copy branch",
			Command: Command{Builtin: "copy-branch"}, Scope: "single"},
		"o": {Key: "o", Label: "Open in browser",
			Command: Command{Argv: []string{"gh", "pr", "view", "{{.Number}}", "--web"}}, Scope: "per-selected"},
		"W": {Key: "W", Label: "Bulk worktrees",
			Command:  Command{Argv: []string{"wt", "switch", "{{.Branch}}"}},
			ExitsTUI: true, Scope: "per-selected"},
		"u": {Key: "u", Label: "Update branch",
			Command: Command{Argv: []string{"gh", "pr", "update-branch", "{{.Number}}"}}, Scope: "per-selected", Refresh: true,
			Progress: "Updating branch", Past: "Branch updated", Fail: "Update failed"},
		"M": {Key: "M", Label: "Mark ready",
			Command: Command{Argv: []string{"gh", "pr", "ready", "{{.Number}}"}}, Scope: "per-selected", Refresh: true,
			ConfirmOthers: true,
			Progress:      "Marking ready", Past: "Marked ready", Fail: "Mark-ready failed"},
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
			Command: Command{Argv: []string{"gh", "issue", "view", "{{.Number}}", "--web"}}, Scope: "per-selected"},
		"y": {Key: "y", Label: "Copy issue #",
			Command: Command{Builtin: "copy-number"}, Scope: "single"},
		"Y": {Key: "Y", Label: "Copy URL",
			Command: Command{Builtin: "copy-url"}, Scope: "single"},
		"b": {Key: "b", Label: "Copy branch",
			Command: Command{Builtin: "copy-branch"}, Scope: "single"},
	}
}
