package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func TestRunActionExitsTUIWritesHandoff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}})
	a := action.Action{Key: "enter", Command: action.Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}, ExitsTUI: true}

	quit := m.runAction(a)
	if quit == nil {
		t.Fatal("exits-tui action must return tea.Quit")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("handoff file not written: %v", err)
	}
}

func TestConfirmDefaultNoCancels(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	a := action.Action{Key: "m", Confirm: true}
	m.pending = &a
	m.confirmAnswer(false) // default No
	if m.pending != nil {
		t.Fatal("pending should clear on No")
	}
}

func TestBulkWritesPerItem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel("/repo", "is:open", nil) // PR section
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7}, {Number: 9}, {Number: 11}})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(2)

	a := action.Action{Key: "W", Command: action.Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}, ExitsTUI: true, Scope: "per-selected"}
	quit := m.runBulk(a)
	if quit == nil {
		t.Fatal("bulk exits-tui must quit")
	}
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Fatalf("want 2 handoff lines, got %d: %q", n, b)
	}
}

func TestReviewerDiff(t *testing.T) {
	add, rm := reviewerDiff([]string{"a", "c"}, map[string]bool{"b": true, "c": true})
	if len(add) != 1 || add[0] != "b" {
		t.Fatalf("add = %v, want [b]", add)
	}
	if len(rm) != 1 || rm[0] != "a" {
		t.Fatalf("remove = %v, want [a]", rm)
	}
	add, rm = reviewerDiff([]string{"a"}, map[string]bool{"a": true})
	if len(add) != 0 || len(rm) != 0 {
		t.Fatalf("no change expected, got add=%v rm=%v", add, rm)
	}
}
