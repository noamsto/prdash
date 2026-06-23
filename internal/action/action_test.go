package action

import "testing"

func TestExpandArgv(t *testing.T) {
	a := Action{Key: "enter", Command: Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}}
	got, err := a.ExpandArgv(Vars{Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wt", "switch", "pr:7"}
	if len(got) != 3 || got[2] != want[2] {
		t.Fatalf("got %v", got)
	}
}

func TestExpandUsesBranch(t *testing.T) {
	a := Action{Command: Command{Argv: []string{"wt", "switch", "-c", "{{.Branch}}"}}}
	got, _ := a.ExpandArgv(Vars{Branch: "feat/213-x"})
	if got[3] != "feat/213-x" {
		t.Fatalf("got %v", got)
	}
}
