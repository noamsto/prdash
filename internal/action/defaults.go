package action

func DefaultPRActions() map[string]Action {
	return map[string]Action{
		"enter": {Key: "enter", Label: "Open worktree",
			Command:  Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}},
			ExitsTUI: true, Scope: "single"},
		"m": {Key: "m", Label: "Merge (squash)",
			Command: Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--squash"}},
			Confirm: true, Scope: "single"},
		"r": {Key: "r", Label: "Rerun failed",
			Command: Command{Builtin: "rerun-failed"}, Scope: "single"},
		"y": {Key: "y", Label: "Copy branch",
			Command: Command{Builtin: "copy"}, Scope: "single"},
		"o": {Key: "o", Label: "Open in browser",
			Command: Command{Argv: []string{"gh", "pr", "view", "{{.Number}}", "--web"}}, Scope: "single"},
		"W": {Key: "W", Label: "Bulk worktrees",
			Command:  Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}},
			ExitsTUI: true, Scope: "per-selected"},
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
			Command: Command{Argv: []string{"gh", "issue", "view", "{{.Number}}", "--web"}}, Scope: "single"},
	}
}
