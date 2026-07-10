package ui

import (
	"strings"
	"testing"
)

func TestBoardGlyphsPresent(t *testing.T) {
	seg := modeSegments("issue")
	if !strings.Contains(seg, prGlyph) || !strings.Contains(seg, issueGlyph) {
		t.Errorf("modeSegments missing a board glyph: %q", seg)
	}

	mi := NewModel(".", "is:open", nil)
	mi.mode = "issue"
	mi.section = NewIssueSection("is:open")
	if !strings.Contains(mi.listTitle(), issueGlyph) {
		t.Errorf("issue listTitle missing issue glyph: %q", mi.listTitle())
	}
	mp := NewModel(".", "is:open", nil)
	if !strings.Contains(mp.listTitle(), prGlyph) {
		t.Errorf("pr listTitle missing pr glyph: %q", mp.listTitle())
	}
}

func TestBoardAccentColorsDiffer(t *testing.T) {
	pr := accentFor("pr").Render("x")
	iss := accentFor("issue").Render("x")
	if pr == iss {
		t.Errorf("PR and Issue accents render identically; boards won't read as different colors")
	}
}
