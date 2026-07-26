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
	if a["y"].Command.Builtin != "copy-number" {
		t.Fatalf(`y builtin = %q, want "copy-number"`, a["y"].Command.Builtin)
	}
	if a["Y"].Command.Builtin != "copy-url" {
		t.Fatalf(`Y builtin = %q, want "copy-url"`, a["Y"].Command.Builtin)
	}
	if a["b"].Command.Builtin != "copy-branch" {
		t.Fatalf(`b builtin = %q, want "copy-branch"`, a["b"].Command.Builtin)
	}
}

func TestMutatingActionsMarkedRefresh(t *testing.T) {
	a := DefaultPRActions()
	for _, k := range []string{"m", "u", "M", "r"} {
		if !a[k].Refresh {
			t.Errorf("action %q should be Refresh:true", k)
		}
	}
	for _, k := range []string{"y", "Y", "b", "o", "enter"} {
		if a[k].Refresh {
			t.Errorf("non-mutating action %q should not be Refresh", k)
		}
	}
}

func TestIssueActionsHaveCopy(t *testing.T) {
	a := DefaultIssueActions()
	for _, k := range []string{"y", "Y", "b"} {
		if _, ok := a[k]; !ok {
			t.Errorf("issue actions missing copy key %q", k)
		}
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
	ready := d["M"]
	if ready.Label != "Mark ready" || ready.Command.Argv[2] != "ready" {
		t.Fatalf("ready action wrong: %+v", ready)
	}
}

func TestDefaultsConfirmOthers(t *testing.T) {
	d := DefaultPRActions()
	if !d["A"].ConfirmOthers {
		t.Error("auto-merge (A) must confirm on others' PRs")
	}
	if !d["M"].ConfirmOthers {
		t.Error("mark ready (M) must confirm on others' PRs")
	}
	for _, k := range []string{"m", "u", "r", "o", "enter", "W"} {
		if d[k].ConfirmOthers {
			t.Errorf("action %q should not set ConfirmOthers", k)
		}
	}
}
