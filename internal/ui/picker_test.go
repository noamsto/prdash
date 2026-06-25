package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestPickerToggleAndSelected(t *testing.T) {
	p := newPicker("Reviewers", []gh.User{{Login: "alice"}, {Login: "bob"}}, map[string]bool{"alice": true})
	if !p.checked["alice"] {
		t.Fatal("alice should start checked")
	}
	p.toggleCursor() // cursor at 0 = alice → uncheck
	if p.checked["alice"] {
		t.Fatal("alice should be unchecked after toggle")
	}
	p.cursor = 1
	p.toggleCursor() // bob → check
	sel := p.selected()
	if len(sel) != 1 || sel[0] != "bob" {
		t.Fatalf("selected = %v, want [bob]", sel)
	}
}

func TestPickerFuzzyFilter(t *testing.T) {
	p := newPicker("X", []gh.User{{Login: "alice", Name: "Alice"}, {Login: "bob"}}, nil)
	p.filter.SetValue("ali")
	if got := p.visible(); len(got) != 1 || got[0].Login != "alice" {
		t.Fatalf("fuzzy filter should narrow to alice, got %+v", got)
	}
}

func TestOpenPickerFetchesAndPopulates(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})

	m.openPicker("author")
	if !m.showPicker || m.pickerMode != "author" {
		t.Fatal("openPicker should show the picker in author mode")
	}
	m2, _ := m.Update(membersFetchedMsg{users: []gh.User{{Login: "alice"}}})
	m = m2.(Model)
	if len(m.pick.cands) != 1 || m.pick.cands[0].Login != "alice" {
		t.Fatalf("members msg should populate candidates: %+v", m.pick.cands)
	}
	if !strings.Contains(m.View(), "alice") {
		t.Fatalf("picker view should list candidates: %q", m.View())
	}
}

func TestConfirmAuthorSetsFilter(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7}})
	m.openPicker("author")
	m.pick.cands = []gh.User{{Login: "alice"}, {Login: "bob"}}
	m.pick.checked = map[string]bool{"alice": true, "bob": true}
	_ = m.confirmPicker()
	if m.filter != "is:open author:alice author:bob" {
		t.Fatalf("author filter = %q", m.filter)
	}
	if m.presetIdx != -1 {
		t.Fatalf("author filter should be custom (presetIdx -1), got %d", m.presetIdx)
	}
}
