package ui

import (
	"os"
	"path/filepath"
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
