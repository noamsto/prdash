package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

// assignees builds a gh.Issue.Assignees value from logins, mirroring author().
func assignees(logins ...string) []struct {
	Login string `json:"login"`
} {
	out := make([]struct {
		Login string `json:"login"`
	}, len(logins))
	for i, l := range logins {
		out[i] = struct {
			Login string `json:"login"`
		}{Login: l}
	}
	return out
}

// categoryOf returns the category label the section painted for issue number,
// searching shown rows since setIssueSections may reorder them.
func categoryOf(t *testing.T, s *IssueSection, number int) string {
	t.Helper()
	for i := 0; i < s.Len(); i++ {
		if s.issueAt(i).Number == number {
			return s.groupLabelAt(i)
		}
	}
	t.Fatalf("issue #%d not found among shown rows", number)
	return ""
}

// issueSectionsCase drives setIssueSections and checks the resulting category
// for each issue number named in wantCats.
type issueSectionsCase struct {
	name                     string
	assigned, authored, open []gh.Issue
	viewer                   string
	wantCats                 map[int]string
	wantLen                  int
}

func runIssueSectionsCases(t *testing.T, cases []issueSectionsCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel("/repo", "is:open", nil)
			m.mode = "issue"
			m.section = NewIssueSection("is:open")
			m.width, m.height = 100, 30

			m.setIssueSections(tc.assigned, tc.authored, tc.open, tc.viewer)

			is := m.section.(*IssueSection)
			if is.Len() != tc.wantLen {
				t.Fatalf("Len() = %d, want %d", is.Len(), tc.wantLen)
			}
			for number, want := range tc.wantCats {
				if got := categoryOf(t, is, number); got != want {
					t.Errorf("issue #%d category = %q, want %q", number, got, want)
				}
			}
		})
	}
}

func TestSetIssueSectionsCategorizes(t *testing.T) {
	runIssueSectionsCases(t, []issueSectionsCase{
		{
			// The bug this feature exists to fix: an issue you opened but
			// aren't assigned to was previously "not mine".
			name:     "authored but not assigned to viewer is Mine",
			authored: []gh.Issue{{Number: 1, Author: author("me")}},
			viewer:   "me",
			wantCats: map[int]string{1: "Mine"},
			wantLen:  1,
		},
		{
			name:     "assigned but not authored by viewer is Mine",
			assigned: []gh.Issue{{Number: 2, Author: author("other")}},
			viewer:   "me",
			wantCats: map[int]string{2: "Mine"},
			wantLen:  1,
		},
		{
			name:     "assigned and authored by viewer appears exactly once",
			assigned: []gh.Issue{{Number: 3, Author: author("me")}},
			authored: []gh.Issue{{Number: 3, Author: author("me")}},
			viewer:   "me",
			wantCats: map[int]string{3: "Mine"},
			wantLen:  1,
		},
		{
			name:     "wide half, neither authored nor assigned to viewer is Others",
			open:     []gh.Issue{{Number: 4, Author: author("other")}},
			viewer:   "me",
			wantCats: map[int]string{4: "Others"},
			wantLen:  1,
		},
		{
			name:     "empty viewer collapses wide-half rows to Others",
			open:     []gh.Issue{{Number: 5, Author: author("me")}},
			viewer:   "",
			wantCats: map[int]string{5: "Others"},
			wantLen:  1,
		},
	})
}

// TestSetIssueSectionsWideHalfRecheck exercises the client-side re-check on
// the wide half: an issue that falls outside the assigned/authored fetch
// window (both capped at issueListLimit) but is still mine must still land
// in Mine.
func TestSetIssueSectionsWideHalfRecheck(t *testing.T) {
	runIssueSectionsCases(t, []issueSectionsCase{
		{
			name:     "authored by viewer, present only in the wide half",
			open:     []gh.Issue{{Number: 10, Author: author("me")}},
			viewer:   "me",
			wantCats: map[int]string{10: "Mine"},
			wantLen:  1,
		},
		{
			name:     "assigned to viewer, present only in the wide half",
			open:     []gh.Issue{{Number: 11, Author: author("other"), Assignees: assignees("me")}},
			viewer:   "me",
			wantCats: map[int]string{11: "Mine"},
			wantLen:  1,
		},
	})
}

// TestIssueCategoriesSurviveApplyFilter drives the real model path —
// setIssueSections, which calls applyFilter and renderList internally — and
// checks the painted viewport, not just the section's internal state.
func TestIssueCategoriesSurviveApplyFilter(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.width, m.height = 100, 30

	// Numbers are deliberately interleaved (Mine = 1,3; Others = 2,4) so that
	// number-descending sort alone would scatter them across categories —
	// contiguity can only hold if the section actually groups by category.
	assigned := []gh.Issue{{Number: 3, Title: "assigned to me", Author: author("other")}}
	authored := []gh.Issue{{Number: 1, Title: "authored by me", Author: author("me")}}
	open := []gh.Issue{
		{Number: 4, Title: "wide open one", Author: author("other")},
		{Number: 2, Title: "wide open two", Author: author("other")},
	}
	m.setIssueSections(assigned, authored, open, "me")

	is := m.section.(*IssueSection)
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < is.Len(); i++ {
		lbl := is.groupLabelAt(i)
		if lbl != prev {
			if seen[lbl] {
				t.Fatalf("category %q reappeared at row %d — rows must be contiguous by category", lbl, i)
			}
			seen[lbl] = true
			prev = lbl
		}
	}

	view := m.vp.View()
	if n := strings.Count(view, "Mine"); n != 1 {
		t.Errorf("\"Mine\" header appears %d times in the viewport, want exactly 1:\n%s", n, view)
	}
	if n := strings.Count(view, "Others"); n != 1 {
		t.Errorf("\"Others\" header appears %d times in the viewport, want exactly 1:\n%s", n, view)
	}
}

// TestIssueBoardQueryFlattensGrouping guards that a "/" fuzzy query suppresses
// grouping on the issue board, mirroring the PR board's bare-text flatten.
func TestIssueBoardQueryFlattensGrouping(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.width, m.height = 100, 30
	m.setIssueSections(
		[]gh.Issue{{Number: 1, Title: "alpha", Author: author("me")}},
		nil,
		[]gh.Issue{{Number: 2, Title: "beta", Author: author("other")}},
		"me",
	)
	is := m.section.(*IssueSection)
	if !is.isGrouped() {
		t.Fatal("categorized issues should start grouped")
	}

	m.filterInput.SetValue("alpha")
	m.applyFilter()

	if is.isGrouped() {
		t.Fatal("a fuzzy query should flatten the issue board (suppress grouping)")
	}
}

func TestSetIssuesClearsCategories(t *testing.T) {
	s := NewIssueSection("is:open")
	s.SetCategorized([]gh.Issue{{Number: 1}, {Number: 2}},
		map[int]string{1: "Mine", 2: "Others"}, []string{"Mine", "Others"})
	if !s.isGrouped() {
		t.Fatal("SetCategorized should start grouped")
	}

	s.SetIssues([]gh.Issue{{Number: 1}, {Number: 2}})
	if s.isGrouped() {
		t.Fatal("SetIssues should clear grouping — no stale category headers")
	}
}

func TestIssueCategoriesSortNumberDescendingWithin(t *testing.T) {
	s := NewIssueSection("is:open")
	issues := []gh.Issue{{Number: 3}, {Number: 9}, {Number: 1}, {Number: 5}}
	cats := map[int]string{3: "Mine", 9: "Mine", 1: "Mine", 5: "Mine"}
	s.SetCategorized(issues, cats, []string{"Mine"})

	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.issueAt(i).Number)
	}
	want := []int{9, 5, 3, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v (number descending within category)", got, want)
	}
}

// erroringWidePRSource fails only the PR-side sectionsFetchCmd wide half
// (filter "is:open"), succeeding on review/reviewed — isolates the wide-half
// failure path for TestIssueBoardFailurePathIsModeScoped's mirror case.
type erroringWidePRSource struct{ err error }

func (s erroringWidePRSource) FetchPRs(filter string, _ int) ([]gh.PR, []byte, error) {
	if filter == "is:open" {
		return nil, nil, s.err
	}
	return nil, nil, nil
}

// TestIssueBoardFailurePathIsModeScoped drives the real fetch commands (not a
// hand-built fetchFailedMsg) so the mode field actually gets exercised.
// searchFor("pr","open","") and searchFor("issue","open","") are the same
// string ("is:open"), so without the mode field a failing issue prewarm would
// paint its error onto the PR board at launch, and vice versa.
func TestIssueBoardFailurePathIsModeScoped(t *testing.T) {
	fs := &fakeIssueSource{err: errors.New("the 'factify-inc/mono' repository has disabled issues")}
	m := NewModel("/repo", "is:open", nil)
	m.SetIssueSource(fs)
	_ = m.toggleMode() // pr -> issue; m.filter becomes m.other's default ("is:open")

	msg := m.issueSectionsFetchCmd()()
	ffm, ok := msg.(fetchFailedMsg)
	if !ok {
		t.Fatalf("msg = %T, want fetchFailedMsg", msg)
	}
	if ffm.mode != "issue" {
		t.Fatalf("mode = %q, want issue", ffm.mode)
	}

	t.Run("foreground issue board shows the notice", func(t *testing.T) {
		u, _ := m.Update(ffm)
		got := u.(Model)
		if got.err != nil {
			t.Fatalf("disabled issues should not surface as an error: %v", got.err)
		}
		if !strings.Contains(got.emptyNotice, "disabled") {
			t.Fatalf("emptyNotice = %q, want a disabled-issues notice", got.emptyNotice)
		}
	})

	t.Run("same failure arriving on the pr board changes nothing", func(t *testing.T) {
		m2 := NewModel("/repo", "is:open", nil)
		m2.mode = "pr"
		u, _ := m2.Update(ffm)
		got := u.(Model)
		if got.emptyNotice != "" {
			t.Fatalf("emptyNotice = %q, want unchanged (empty)", got.emptyNotice)
		}
		if got.err != nil {
			t.Fatalf("err = %v, want nil", got.err)
		}
	})

	t.Run("pr-side wide-half failure arriving on the issue board changes nothing", func(t *testing.T) {
		m3 := NewModel("/repo", "is:open", nil)
		m3.SetPRSource(erroringWidePRSource{err: errors.New("boom")})
		_ = m3.toggleMode() // pr -> issue

		prMsg := m3.sectionsFetchCmd()()
		prFfm, ok := prMsg.(fetchFailedMsg)
		if !ok || prFfm.mode != "pr" {
			t.Fatalf("msg = %+v, want a pr-mode fetchFailedMsg", prMsg)
		}

		u, _ := m3.Update(prFfm)
		got := u.(Model)
		if got.emptyNotice != "" {
			t.Fatalf("emptyNotice = %q, want unchanged (empty)", got.emptyNotice)
		}
		if got.err != nil {
			t.Fatalf("err = %v, want nil", got.err)
		}
	})
}

// TestIssueBoardHeadersPaint guards that the Mine/Others headers actually
// reach the rendered viewport, not just the section's internal category map.
func TestIssueBoardHeadersPaint(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.width, m.height = 100, 30
	m.setIssueSections(
		[]gh.Issue{{Number: 1, Title: "mine", Author: author("me")}},
		nil,
		[]gh.Issue{{Number: 2, Title: "not mine", Author: author("other")}},
		"me",
	)

	view := m.vp.View()
	if !strings.Contains(view, "Mine") {
		t.Errorf("viewport should paint a Mine header:\n%s", view)
	}
	if !strings.Contains(view, "Others") {
		t.Errorf("viewport should paint an Others header:\n%s", view)
	}
}
