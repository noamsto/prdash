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
