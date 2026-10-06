package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func groupLabels(m Model) []string {
	g := m.section.(grouper)
	out := []string{""}
	for i := range m.section.Len() {
		l := g.groupLabelAt(i)
		if out[len(out)-1] != l {
			out = append(out, l)
		}
	}
	return out[1:]
}

func firstLabel(m Model) string {
	if l := groupLabels(m); len(l) > 0 {
		return l[0]
	}
	return ""
}

func focusedNumber(m Model) int { return m.section.(numbered).numberAt(m.cursor) }

func groupByModel(t *testing.T) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.setSections([]gh.PR{openPR(9, "bob")}, nil, []gh.PR{openPR(7, "me"), openPR(5, "carol"), openPR(4, "bob")}, "me")
	return m
}

func pressG(t *testing.T, m Model) Model {
	t.Helper()
	u, cmd := m.Update(keyMsg("g"))
	if cmd != nil {
		t.Fatalf("g on the default board issued a command; the toggle must not refetch")
	}
	return u.(Model)
}

func TestGroupKeyTogglesDefaultBoardLayout(t *testing.T) {
	m := groupByModel(t)
	if got := firstLabel(m); got != "Review requested" {
		t.Fatalf("default layout = %v, want category sections", got)
	}
	m.cursor = shownIndex(m, 5)

	m = pressG(t, m)
	for _, l := range groupLabels(m) {
		if l == "Review requested" || l == "Mine" || l == "Others" {
			t.Fatalf("by-author layout still shows category %q: %v", l, groupLabels(m))
		}
	}
	if focusedNumber(m) != 5 {
		t.Fatalf("cursor moved to #%d, want #5", focusedNumber(m))
	}

	m = pressG(t, m)
	if got := firstLabel(m); got != "Review requested" {
		t.Fatalf("second g layout = %v, want category sections back", got)
	}
	if focusedNumber(m) != 5 {
		t.Fatalf("cursor moved to #%d, want #5", focusedNumber(m))
	}
}

func TestGroupKeyKeepsCursorWhilePinnedToTop(t *testing.T) {
	m := groupByModel(t)
	m.cursor, m.cursorPinnedTop = 0, true
	want := focusedNumber(m)
	m = pressG(t, m)
	if focusedNumber(m) != want {
		t.Fatalf("focused #%d after g, want #%d", focusedNumber(m), want)
	}
}

func TestGroupLayoutSurvivesASectionsRefresh(t *testing.T) {
	m := groupByModel(t)
	m = pressG(t, m)
	review, open := []gh.PR{openPR(9, "bob")}, []gh.PR{openPR(7, "me"), openPR(5, "carol"), openPR(4, "bob")}
	u, _ := m.Update(sectionsMsg(t, review, open))
	m = u.(Model)
	for _, l := range groupLabels(m) {
		if l == "Mine" || l == "Others" {
			t.Fatalf("refresh restored category sections: %v", groupLabels(m))
		}
	}
}

func TestGroupKeyOnMergedViewKeepsAuthorGrouping(t *testing.T) {
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{mergedPR(30, "alice"), mergedPR(29, "bob")})
	before := groupLabels(m)
	if len(before) == 0 {
		t.Fatal("no groups")
	}
	u, _ := m.Update(keyMsg("g"))
	m = u.(Model)
	if m.byAuthor {
		t.Fatal("g flipped the layout flag on a non-default view")
	}
	if got := groupLabels(m); len(got) != len(before) || firstLabel(m) != before[0] {
		t.Fatalf("grouping changed: %v -> %v", before, got)
	}
}

func TestLegendListsGroupKey(t *testing.T) {
	has := func(m Model) bool {
		for _, g := range m.keyPanes() {
			for _, k := range g.hints {
				if k.key == "g" {
					return true
				}
			}
		}
		return false
	}
	m := groupByModel(t)
	if !has(m) {
		t.Fatal("PR-mode legend lacks g")
	}
}

func TestGroupKeyHints(t *testing.T) {
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{mergedPR(30, "alice")})
	u, _ := m.Update(keyMsg("g"))
	if st := u.(Model).actionStatus; st == nil || st.fail != "Already grouped by author in this view" {
		t.Fatalf("merged-view hint = %+v", st)
	}

	im := NewModel("/repo", "is:open", nil)
	im.mode = "issue"
	u, _ = im.Update(keyMsg("g"))
	if st := u.(Model).actionStatus; st == nil || st.fail != "Grouping is PR-only (g)" {
		t.Fatalf("issue-mode hint = %+v", st)
	}
}

func TestGroupKeyKeepsMarks(t *testing.T) {
	m := groupByModel(t)
	m.sel.toggle(shownIndex(m, 5))
	m.sel.toggle(shownIndex(m, 4))
	m = pressG(t, m)
	assertMarks(t, m, 4, 5)
}
