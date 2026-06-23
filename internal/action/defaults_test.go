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
