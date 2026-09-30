package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
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

// selectedNumbers is the board's selection as numbers, ascending.
func selectedNumbers(m Model) []int {
	n := m.section.(numbered)
	var out []int
	for _, i := range m.sel.indices() {
		out = append(out, n.numberAt(i))
	}
	slices.Sort(out)
	return out
}

// assertMarks checks the selection is exactly want, that every wanted row
// renders its selection bar, and that the header count agrees.
func assertMarks(t *testing.T, m Model, want ...int) {
	t.Helper()
	if got := selectedNumbers(m); !slices.Equal(got, want) {
		t.Errorf("selected = %v, want %v", got, want)
	}
	for _, num := range want {
		if row := rowTextFor(t, m, num); !strings.Contains(row, selBarGlyph) {
			t.Errorf("row #%d = %q, want the selection bar", num, row)
		}
	}
	if n := markedRows(m); n != len(want) {
		t.Errorf("marked rows = %d, want %d", n, len(want))
	}
	if h, count := ansi.Strip(m.header()), fmt.Sprintf("%d selected", len(want)); !strings.Contains(h, count) {
		t.Errorf("header = %q, want %q", h, count)
	}
}

func TestMarksSurviveAReplaceOnTheFlatPRBoard(t *testing.T) {
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{mergedPR(30, "alice"), mergedPR(29, "alice"), mergedPR(28, "alice")})
	m.sel.toggle(shownIndex(m, 30))
	m.sel.toggle(shownIndex(m, 28))

	u, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(30, "alice"), mergedPR(29, "alice"), mergedPR(28, "alice")}, replace: true})
	m = u.(Model)

	assertMarks(t, m, 28, 30)
}

func TestMarksSurviveAReplaceOnTheSectionsBoard(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	review := []gh.PR{openPR(9, "bob")}
	open := []gh.PR{openPR(7, "bob"), openPR(5, "carol")}
	m.setSections(review, nil, open, "")
	m.sel.toggle(shownIndex(m, 9))
	m.sel.toggle(shownIndex(m, 5))

	msg := sectionsMsg(t, review, open)
	msg.replace = true
	u, _ := m.Update(msg)
	m = u.(Model)

	assertMarks(t, m, 5, 9)
}

// TestReplaceDropsOnlyTheMarkOfADepartedRow: a marked PR the replace result
// no longer carries loses its mark; the others keep theirs.
func TestReplaceDropsOnlyTheMarkOfADepartedRow(t *testing.T) {
	m := NewModel("/repo", "is:merged", nil)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{mergedPR(30, "alice"), mergedPR(29, "alice"), mergedPR(28, "alice")})
	m.sel.toggle(shownIndex(m, 30))
	m.sel.toggle(shownIndex(m, 29))
	m.sel.toggle(shownIndex(m, 28))

	u, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(30, "alice"), mergedPR(28, "alice")}, replace: true})
	m = u.(Model)

	if shownIndex(m, 29) >= 0 {
		t.Fatalf("#29 still shown after a replace that dropped it")
	}
	assertMarks(t, m, 28, 30)
}

// TestMarksOnCachedRowsSurviveTheSwitchFetch: a filter switch paints the warm
// cache and fires a replace fetch; rows marked in between keep their marks
// when it lands.
func TestMarksOnCachedRowsSurviveTheSwitchFetch(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	m.setPRs([]gh.PR{openPR(1, "alice")})
	m.sel.toggle(shownIndex(m, 1))
	raw, err := json.Marshal([]gh.PR{mergedPR(40, "alice"), mergedPR(39, "alice")})
	if err != nil {
		t.Fatal(err)
	}
	c.Set(prKey(m.repo, searchFor("pr", "merged", ""), defaultLimit), raw)
	u, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.state != "merged" || m.section.Len() != 2 || m.sel.count() != 0 {
		t.Fatalf("test setup: state = %q, rows = %d, selected = %d, want the merged cache painted with no selection",
			m.state, m.section.Len(), m.sel.count())
	}
	m.sel.toggle(shownIndex(m, 39))

	u, _ = m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{mergedPR(41, "alice"), mergedPR(40, "alice"), mergedPR(39, "alice")}, replace: true})
	m = u.(Model)

	assertMarks(t, m, 39)
}

func TestMarksSurviveAReplaceOnTheIssueBoards(t *testing.T) {
	issues := func() []gh.Issue {
		return []gh.Issue{{Number: 16, Title: "a"}, {Number: 14, Title: "b"}, {Number: 12, Title: "c"}}
	}
	t.Run("flat", func(t *testing.T) {
		m := NewModel("/repo", "is:closed", nil)
		m.mode = "issue"
		m.section = NewIssueSection("is:closed")
		m.width, m.height = 100, 40
		m.setIssues(issues())
		m.sel.toggle(shownIndex(m, 16))
		m.sel.toggle(shownIndex(m, 12))

		u, _ := m.Update(issuesFetchedMsg{filter: m.filter, issues: issues()[1:], replace: true})
		m = u.(Model)

		assertMarks(t, m, 12)
	})
	t.Run("sections", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.mode = "issue"
		m.section = NewIssueSection("is:open")
		m.width, m.height = 100, 40
		m.setIssueSections(nil, nil, issues(), "")
		m.sel.toggle(shownIndex(m, 16))
		m.sel.toggle(shownIndex(m, 14))

		msg := issueSectionsMsg(t, nil, nil, issues())
		msg.replace = true
		u, _ := m.Update(msg)
		m = u.(Model)

		assertMarks(t, m, 14, 16)
	})
}
