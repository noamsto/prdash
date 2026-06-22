package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestSetPRsBuildsRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 7, Title: "hello", HeadRefName: "feat/x"},
		{Number: 9, Title: "world", HeadRefName: "fix/y"},
	})
	if got := len(m.table.Rows()); got != 2 {
		t.Fatalf("table rows = %d, want 2", got)
	}
	if m.table.Rows()[0][0] != "#7" {
		t.Errorf("first row number cell = %q, want #7", m.table.Rows()[0][0])
	}
}
