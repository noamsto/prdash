package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func TestSelectionToggle(t *testing.T) {
	s := selection{}
	s.toggle(2)
	s.toggle(5)
	s.toggle(2) // off again
	if s.has(2) || !s.has(5) {
		t.Fatalf("selection state wrong: %+v", s.set)
	}
	if n := s.count(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

func TestClearSelectionRepaintsRows(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{
		{Number: 7, ID: "pr7", State: "OPEN", Mergeable: "MERGEABLE"},
		{Number: 8, ID: "pr8", State: "OPEN", Mergeable: "MERGEABLE"},
		{Number: 9, ID: "pr9", State: "OPEN", Mergeable: "MERGEABLE"},
	})
	m.width, m.height = 120, 40
	m.sel.toggle(0)
	m.sel.toggle(1)
	m.renderList()
	if n := markedRows(m); n != 2 {
		t.Fatalf("marked rows before the op = %d, want 2", n)
	}

	driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))

	if n := m.sel.count(); n != 0 {
		t.Fatalf("selection after the op = %d, want 0", n)
	}
	if n := markedRows(m); n != 0 {
		t.Errorf("marked rows after the op = %d, want 0", n)
	}
}

func markedRows(m Model) int {
	n := 0
	for _, r := range m.rowText {
		if strings.Contains(r, selBarGlyph) {
			n++
		}
	}
	return n
}
