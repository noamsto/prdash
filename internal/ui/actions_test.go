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

func TestClipboardText(t *testing.T) {
	v := action.Vars{URL: "https://x/pr/7", Branch: "feat/x"}
	if got := clipboardText("copy-url", v); got != v.URL {
		t.Fatalf("copy-url = %q, want %q", got, v.URL)
	}
	if got := clipboardText("copy-branch", v); got != v.Branch {
		t.Fatalf("copy-branch = %q, want %q", got, v.Branch)
	}
}

func TestCopiedLabel(t *testing.T) {
	cases := []struct {
		builtin string
		n       int
		want    string
	}{
		{"copy-url", 1, "Copied URL"},
		{"copy-url", 3, "Copied 3 URLs"},
		{"copy-branch", 1, "Copied branch"},
		{"copy-branch", 2, "Copied 2 branches"},
		{"copy-number", 1, "Copied PR number"},
		{"copy-number", 5, "Copied 5 PR numbers"},
	}
	for _, c := range cases {
		if got := copiedLabel(c.builtin, c.n); got != c.want {
			t.Errorf("copiedLabel(%q, %d) = %q, want %q", c.builtin, c.n, got, c.want)
		}
	}
}

func TestCopyClearsSelection(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}, {Number: 9, HeadRefName: "feat/y"}})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)

	a := action.Action{Key: "b", Command: action.Command{Builtin: "copy-branch"}}
	if cmd := m.runAction(a); cmd == nil {
		t.Fatal("copy should return a command")
	}
	if m.sel.count() != 0 {
		t.Fatalf("batch copy should clear the selection, still %d selected", m.sel.count())
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
