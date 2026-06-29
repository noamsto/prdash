package action

import "testing"

func TestDefaultsHaveEnterAndExits(t *testing.T) {
	d := DefaultPRActions()
	enter, ok := d["enter"]
	if !ok || !enter.ExitsTUI {
		t.Fatal("enter must exist and exit the TUI")
	}
	if m := d["m"]; m.ExitsTUI || !m.Confirm {
		t.Fatal("merge must be inline + confirm")
	}
	if d["r"].Command.Builtin != "rerun-failed" {
		t.Fatal("r must be the rerun-failed builtin")
	}
}

func TestDefaultsHaveBulkW(t *testing.T) {
	if w := DefaultPRActions()["W"]; w.Scope != "per-selected" || !w.ExitsTUI {
		t.Fatalf("PR W must be per-selected + exits-tui: %+v", w)
	}
	if w := DefaultIssueActions()["W"]; w.Scope != "per-selected" {
		t.Fatalf("issue W must be per-selected: %+v", w)
	}
}

func TestCopyActionsRebound(t *testing.T) {
	a := DefaultPRActions()
	if a["y"].Label != "Copy URL" {
		t.Fatalf(`y label = %q, want "Copy URL"`, a["y"].Label)
	}
	if a["y"].Command.Builtin != "copy-url" {
		t.Fatalf(`y builtin = %q, want "copy-url"`, a["y"].Command.Builtin)
	}
	if a["Y"].Command.Builtin != "copy-branch" {
		t.Fatalf(`Y builtin = %q, want "copy-branch"`, a["Y"].Command.Builtin)
	}
}

func TestDefaultsHaveUpdateAndReady(t *testing.T) {
	d := DefaultPRActions()
	u := d["u"]
	if u.Command.Argv[0] != "gh" || u.Command.Argv[1] != "pr" || u.Command.Argv[2] != "update-branch" {
		t.Fatalf("u must be gh pr update-branch: %+v", u.Command.Argv)
	}
	if u.ExitsTUI {
		t.Fatal("update-branch is inline, not exits-tui")
	}
	ready := d["ready"]
	if ready.Label != "Mark ready" || ready.Command.Argv[2] != "ready" {
		t.Fatalf("ready action wrong: %+v", ready)
	}
}
