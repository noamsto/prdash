package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

func otherOnlyModel(t *testing.T, height int) Model {
	t.Helper()
	cross := []gh.PR{
		repoPR("a/b", 5, "zed", "2026-01-01"),
		repoPR("c/d", 9, "zed", "2026-03-01"),
		repoPR("c/d", 8, "zed", "2026-02-01"),
	}
	for i := range 12 {
		cross = append(cross, repoPR("e/f", 20+i, "zed", "2025-01-01"))
	}
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo(launchRepo)
	m.width, m.height = 100, height
	m.paintSections(nil, nil, nil, cross, "me", true, false)
	return m
}

func TestLaunchGroupEmptyPlaceholder(t *testing.T) {
	m := otherOnlyModel(t, 60)
	ps := m.section.(*PRSection)
	if !ps.launchEmpty() {
		t.Fatal("launchEmpty = false with rows only in other repos")
	}
	out := ansi.Strip(m.View().Content)
	lh := strings.Index(out, launchRepo+"  0")
	ph := strings.Index(out, "  no open PRs")
	oh := strings.Index(out, "c/d")
	if lh < 0 || ph < lh || oh < ph {
		t.Fatalf("want launch header (zero count), placeholder, then first other header; got offsets %d %d %d in\n%s", lh, ph, oh, out)
	}
	if got := boardLayout(m); len(got) == 0 || got[0] != "c/d#09" {
		t.Fatalf("first shown = %v, want c/d#09 first", got)
	}
	if m.cursor != 0 || ps.isFoldStop(0) || ps.repoAt(0) != "c/d" {
		t.Fatalf("cursor %d is not the first real PR row", m.cursor)
	}
}

func TestLaunchGroupNeverFolds(t *testing.T) {
	m := repoGroupModel(t, foldCross())
	m.repoFold[launchRepo] = true // stale entry
	review, open, _ := repoGroupFixture()
	m.paintSections(review, nil, open, foldCross(), "me", true, false)
	before := boardLayout(m)
	if slices.Contains(before, "stop:"+launchRepo) {
		t.Fatalf("stale fold folded the launch group: %v", before)
	}
	m.cursor = 0
	m = press(m, "e")
	if got := boardLayout(m); !slices.Equal(got, before) {
		t.Fatalf("e on launch row changed layout: %v -> %v", before, got)
	}
	// Other groups still fold and unfold.
	ps := m.section.(*PRSection)
	i, ok := ps.firstRowIndex("c/d")
	if !ok {
		t.Fatal("no c/d row")
	}
	m.cursor = i
	m = press(m, "e")
	if !slices.Contains(boardLayout(m), "stop:c/d") {
		t.Fatalf("c/d did not fold: %v", boardLayout(m))
	}
	m = press(m, "e")
	if slices.Contains(boardLayout(m), "stop:c/d") {
		t.Fatalf("c/d did not unfold: %v", boardLayout(m))
	}
}

func TestLaunchGroupCursorWalk(t *testing.T) {
	m := otherOnlyModel(t, 60)
	ps := m.section.(*PRSection)
	if ps.Len() != 15 {
		t.Fatalf("Len = %d, want 15 real rows", ps.Len())
	}
	check := func(step string) {
		t.Helper()
		if m.cursor < 0 || m.cursor >= ps.Len() || ps.isFoldStop(m.cursor) || ps.repoAt(m.cursor) == launchRepo {
			t.Fatalf("after %s cursor %d is off a real row", step, m.cursor)
		}
	}
	for _, k := range []string{"j", "j", "j", "k", "k", "k", "k"} {
		m = press(m, k)
		check(k)
	}
	for range ps.Len() + 2 {
		m = press(m, "j")
		check("j")
	}
}

func TestLaunchGroupEmptyScrollsToHeader(t *testing.T) {
	m := otherOnlyModel(t, 16)
	rows := m.section.Len()
	for range rows {
		m = press(m, "j")
	}
	if m.vp.YOffset() == 0 {
		t.Fatal("walking to the last row did not scroll a tall board")
	}
	for range rows {
		m = press(m, "k")
	}
	if got := m.vp.YOffset(); got != 0 {
		t.Fatalf("YOffset back on row 0 = %d, want 0 so the launch header shows", got)
	}
}
