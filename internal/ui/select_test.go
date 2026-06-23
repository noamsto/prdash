package ui

import "testing"

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
