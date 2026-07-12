package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// A long body forces the preview taller than any box, so a missing height clamp
// shows up as an overflow.
func longIssueModel(w, h int, previewMax bool) Model {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.actions = action.DefaultIssueActions()
	m.section.(*IssueSection).SetIssues([]gh.Issue{{Number: 20, Title: "long issue"}})
	m.issueDetail[20] = gh.IssueDetail{Body: strings.Repeat("long body line describing the bug in detail. ", 80)}
	m.loaded = true
	m.previewMax = previewMax
	m.width, m.height = w, h
	m.renderList()
	return m
}

// The rendered frame must never exceed the terminal height — a taller frame
// pushes the bottom border off-screen. Regression for the previewMax overflow on
// terminals too narrow for a side pane (ShowSide=false), where contentHeight
// reclaimed the panel rows but board() still rendered the panel.
func TestRenderNeverExceedsHeight(t *testing.T) {
	sizes := [][2]int{{120, 30}, {120, 24}, {100, 35}, {80, 30}, {70, 40}, {60, 25}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		for _, zoom := range []bool{false, true} {
			m := longIssueModel(w, h, zoom)
			if got := lipgloss.Height(m.render()); got > h {
				t.Errorf("%dx%d previewMax=%v: render height %d exceeds terminal height %d", w, h, zoom, got, h)
			}
		}
	}
}

// The PR expanded view is the other full-frame render path; a long body must not
// push its bottom border off-screen either.
func TestExpandedViewNeverExceedsHeight(t *testing.T) {
	longBody := strings.Repeat("long conversation comment describing things at length. ", 80)
	for _, sz := range [][2]int{{120, 30}, {100, 35}, {80, 30}, {60, 25}} {
		w, h := sz[0], sz[1]
		m := NewModel(".", "is:open author:@me", nil)
		m.setPRs([]gh.PR{{Number: 1, Title: "a pr"}})
		m.detail[1] = gh.PRDetail{Comments: []gh.Comment{{Body: longBody}}}
		m.loaded = true
		m.width, m.height = w, h
		m.enterExpanded()
		if got := lipgloss.Height(m.expandedView()); got > h {
			t.Errorf("%dx%d expanded: view height %d exceeds terminal height %d", w, h, got, h)
		}
	}
}
