package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestExpandedStepSkipsFoldStops(t *testing.T) {
	m := foldModel(t)
	m.repoFold["a/b"] = false
	review, open, _ := repoGroupFixture()
	m.paintSections(review, nil, open, foldCross(), "me", true, false)
	// launch rows, stop c/d, a/b#5
	ps := m.section.(*PRSection)
	last := ps.Len() - 1
	if ps.isFoldStop(last) || !ps.isFoldStop(last-1) {
		t.Fatalf("unexpected layout %v", boardLayout(m))
	}
	m.cursor = 2
	m = press(m, "l")
	if !m.expanded {
		t.Fatal("did not expand")
	}
	m = press(m, "J")
	if m.cursor != last {
		t.Errorf("J landed on %d (stop=%v), want real row %d", m.cursor, ps.isFoldStop(m.cursor), last)
	}
	m = press(m, "J")
	if m.cursor != last {
		t.Errorf("J past the end moved to %d", m.cursor)
	}
	m = press(m, "K")
	if m.cursor != 2 {
		t.Errorf("K landed on %d, want the last launch row 2", m.cursor)
	}
}

func TestActionsOnFoldStopAreInert(t *testing.T) {
	m := onStop(t, foldModel(t), "c/d")
	run := func(name string, f func() tea.Cmd) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s on a stop panicked: %v", name, r)
			}
		}()
		_ = f()
		if got := handoffLines(t, os.Getenv("PRDASH_ACTION_FILE")); len(got) != 0 {
			t.Errorf("%s wrote a handoff: %v", name, got)
		}
		if len(m.PendingExec()) != 0 {
			t.Errorf("%s queued %v", name, m.PendingExec())
		}
		if hintText(m) == "" {
			t.Errorf("%s left no stop hint", name)
		}
		m.actionStatus = nil
	}
	run("runAction enter", func() tea.Cmd { return m.runAction(m.actions["enter"]) })
	run("startBulk A", func() tea.Cmd { return m.startBulk(m.actions["A"]) })
}

func TestTwoLineViewportKeepsCursorRow(t *testing.T) {
	for h := 4; h < 30; h++ {
		m := foldModel(t)
		m.SetOuterFrame(true)
		u, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: h})
		m = u.(Model)
		if m.vp.Height() != 2 {
			continue
		}
		m.vp.SetYOffset(m.vp.TotalLineCount())
		m.cursor = 0
		m.renderList()
		if off := m.vp.YOffset(); m.cursorLine < off || m.cursorLine >= off+m.vp.Height() {
			t.Errorf("cursor line %d outside viewport [%d,%d)", m.cursorLine, off, off+m.vp.Height())
		}
		return
	}
	t.Fatal("no height gave a 2-line list viewport")
}

func TestExpandedHeaderKeepsNumber(t *testing.T) {
	repo := strings.Repeat("o", 35) + "/" + strings.Repeat("r", 34)
	h := ansi.Strip(expandedHeader(repo, 123, 40))
	if w := len([]rune(h)); w > 40 {
		t.Errorf("header %q is %d wide, want <= 40", h, w)
	}
	if !strings.HasSuffix(h, " #123") {
		t.Errorf("header %q lost the PR number", h)
	}
}

func TestConsecutiveStopsHaveNoBlankLine(t *testing.T) {
	m := foldModel(t)
	lines := strings.Split(ansi.Strip(m.vp.GetContent()), "\n")
	cd, ab := -1, -1
	for i, l := range lines {
		switch {
		case strings.Contains(l, "c/d"):
			cd = i
		case strings.Contains(l, "a/b"):
			ab = i
		}
	}
	if cd < 0 || ab != cd+1 {
		t.Errorf("stops at lines %d and %d, want consecutive:\n%s", cd, ab, strings.Join(lines, "\n"))
	}
	if blank := lines[cd-1]; strings.TrimSpace(blank) != "" {
		t.Errorf("no blank line before the first stop: %q", blank)
	}
	ps := m.section.(*PRSection)
	i, _ := ps.stopIndex("a/b")
	m.cursor = i
	m.renderList()
	if m.cursorLine != ab {
		t.Errorf("cursorLine = %d, want %d", m.cursorLine, ab)
	}
}
