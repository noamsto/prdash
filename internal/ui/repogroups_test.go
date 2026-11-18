package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

const launchRepo = "owner/repo"

func repoPR(repo string, number int, author string, updated string) gh.PR {
	p := openPR(number, author)
	p.Repo = repo
	t, err := time.Parse("2006-01-02", updated)
	if err != nil {
		panic(err)
	}
	p.UpdatedAt = t
	return p
}

// repoGroupFixture is the launch repo's three category rows plus cross-repo
// rows in two other repos; c/d is the more recently updated.
func repoGroupFixture() (review, open, cross []gh.PR) {
	r1 := repoPR(launchRepo, 1, "bob", "2026-01-01")
	r2 := repoPR(launchRepo, 2, "me", "2026-01-01")
	r3 := repoPR(launchRepo, 3, "bob", "2026-01-01")
	cross = []gh.PR{
		repoPR("a/b", 5, "zed", "2026-01-01"),
		repoPR("c/d", 9, "zed", "2026-03-01"),
		repoPR("c/d", 8, "zed", "2026-02-01"),
	}
	return []gh.PR{r1}, []gh.PR{r1, r2, r3}, cross
}

func repoGroupModel(t *testing.T, cross []gh.PR) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo(launchRepo)
	m.width, m.height = 100, 40
	review, open, _ := repoGroupFixture()
	m.paintSections(review, nil, open, cross, "me", true, false)
	return m
}

// boardLayout is the shown set top to bottom: "stop:<repo>" for a folded
// header, "<repo>#<n>" for a row.
func boardLayout(m Model) []string {
	ps := m.section.(*PRSection)
	var out []string
	for i := range ps.Len() {
		if ps.isFoldStop(i) {
			out = append(out, "stop:"+ps.repoAt(i))
			continue
		}
		r := ps.refAt(i)
		out = append(out, r.Repo+fmt.Sprintf("#%02d", r.Number))
	}
	return out
}

func TestRepoGroupOrder(t *testing.T) {
	_, _, cross := repoGroupFixture()

	t.Run("launch group first, other repos folded by recency", func(t *testing.T) {
		m := repoGroupModel(t, cross)
		ps := m.section.(*PRSection)
		want := []string{"owner/repo#01", "owner/repo#02", "owner/repo#03", "stop:c/d", "stop:a/b"}
		if got := boardLayout(m); !slices.Equal(got, want) {
			t.Fatalf("layout = %v, want %v", got, want)
		}
		if !ps.repoGrouped() {
			t.Fatal("repoGrouped = false with cross-repo rows on the default board")
		}
		if ps.isFolded(launchRepo) {
			t.Error("the launch repo's group is folded")
		}
		for _, r := range []string{"c/d", "a/b"} {
			if !ps.isFolded(r) {
				t.Errorf("%s is not folded by default", r)
			}
			if i, ok := ps.stopIndex(r); !ok || !ps.isFoldStop(i) || ps.repoAt(i) != r {
				t.Errorf("stopIndex(%s) = %d, %v", r, i, ok)
			}
		}
		if _, ok := ps.firstRowIndex("c/d"); ok {
			t.Error("firstRowIndex on a folded group should report none")
		}
		if i, ok := ps.firstRowIndex(launchRepo); !ok || i != 0 {
			t.Errorf("firstRowIndex(launch) = %d, %v, want 0", i, ok)
		}
		wantLabels := []string{"Review requested", "Mine", "Others", ""}
		if got := groupLabels(m); !slices.Equal(got, wantLabels) {
			t.Errorf("sub-labels = %q, want %q", got, wantLabels)
		}
	})

	t.Run("unfolding c/d lists its rows by recency before the a/b stop", func(t *testing.T) {
		m := repoGroupModel(t, cross)
		m.repoFold["c/d"] = false
		review, open, _ := repoGroupFixture()
		m.paintSections(review, nil, open, cross, "me", true, false)
		want := []string{"owner/repo#01", "owner/repo#02", "owner/repo#03", "c/d#09", "c/d#08", "stop:a/b"}
		if got := boardLayout(m); !slices.Equal(got, want) {
			t.Fatalf("layout = %v, want %v", got, want)
		}
		ps := m.section.(*PRSection)
		if i, ok := ps.firstRowIndex("c/d"); !ok || i != 3 {
			t.Errorf("firstRowIndex(c/d) = %d, %v, want 3", i, ok)
		}
		if _, ok := ps.stopIndex("c/d"); ok {
			t.Error("an unfolded group still has a stop")
		}
	})

	t.Run("byAuthor groups the launch rows and the stops still follow", func(t *testing.T) {
		m := NewModel("/repo", "is:open", nil)
		m.SetRepo(launchRepo)
		m.width, m.height = 100, 40
		m.byAuthor = true
		review, open, _ := repoGroupFixture()
		m.paintSections(review, nil, open, cross, "me", true, false)
		ps := m.section.(*PRSection)
		layout := boardLayout(m)
		if n := len(layout); n < 2 || layout[n-2] != "stop:c/d" || layout[n-1] != "stop:a/b" {
			t.Fatalf("layout = %v, want the c/d and a/b stops last", layout)
		}
		labels := map[int]string{}
		for i := range ps.Len() {
			if !ps.isFoldStop(i) {
				labels[ps.refAt(i).Number] = ps.groupLabelAt(i)
			}
		}
		for _, l := range labels {
			if l == "Review requested" || l == "Mine" || l == "Others" {
				t.Fatalf("by-author launch group shows category label %q: %v", l, labels)
			}
		}
		if labels[1] != labels[3] || labels[1] == labels[2] {
			t.Errorf("launch rows not grouped by author (bob #1,#3 / me #2): %v", labels)
		}
	})

	t.Run("no cross rows behaves as before", func(t *testing.T) {
		m := repoGroupModel(t, nil)
		ps := m.section.(*PRSection)
		if ps.repoGrouped() {
			t.Error("repoGrouped with no cross-repo rows")
		}
		want := []string{"owner/repo#01", "owner/repo#02", "owner/repo#03"}
		if got := boardLayout(m); !slices.Equal(got, want) {
			t.Fatalf("layout = %v, want %v", got, want)
		}
		if got, want := groupLabels(m), []string{"Review requested", "Mine", "Others"}; !slices.Equal(got, want) {
			t.Errorf("labels = %q, want %q", got, want)
		}
	})
}

// pendingRepoPR is a cross-repo row with an in-flight check.
func pendingRepoPR(repo string, number int, updated string) gh.PR {
	p := repoPR(repo, number, "zed", updated)
	p.StatusCheckRollup = []gh.Check{{Name: "ci", State: "IN_PROGRESS"}}
	return p
}

func TestFoldedGroupsCostNoFetch(t *testing.T) {
	cross := []gh.PR{
		pendingRepoPR("a/b", 5, "2026-01-01"),
		pendingRepoPR("c/d", 9, "2026-03-01"),
		pendingRepoPR("c/d", 8, "2026-02-01"),
	}
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{}}
	m := NewModel("/repo", "is:open", cache.Open(filepath.Join(t.TempDir(), "c.json")))
	m.SetRepo(launchRepo)
	m.SetDetailSource(fd)
	m.width, m.height = 100, 40
	review, open, _ := repoGroupFixture()
	m.paintSections(review, nil, open, cross, "me", true, false)
	m.cursor = 0
	ps := m.section.(*PRSection)

	otherRepo := func(r gh.PRRef) bool { return r.Repo == "c/d" || r.Repo == "a/b" }

	// warmed runs warmDetailCmd and returns every ref the source was asked for.
	warmed := func(m Model) []gh.PRRef {
		fd.got = nil
		var run func(tea.Cmd)
		run = func(c tea.Cmd) {
			if c == nil {
				return
			}
			if batch, ok := c().(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
			}
		}
		run(m.warmDetailCmd())
		var all []gh.PRRef
		for _, g := range fd.got {
			all = append(all, g...)
		}
		return all
	}
	running := func(m Model) []string {
		ps := m.section.(*PRSection)
		var out []string
		for _, i := range runningCheckRows(ps, m.held) {
			out = append(out, ps.repoAt(i))
		}
		return out
	}

	for _, r := range warmed(m) {
		if otherRepo(r) {
			t.Errorf("folded: warmDetailCmd requested %v", r)
		}
	}
	for _, r := range prefetchRefs(ps, m.cursor, m.fresh, 50) {
		if otherRepo(r) {
			t.Errorf("folded: prefetchRefs returned %v", r)
		}
	}
	if got := running(m); len(got) != 0 {
		t.Errorf("folded: runningCheckRows covers %v, want none", got)
	}

	m.repoFold["c/d"] = false
	m.paintSections(review, nil, open, cross, "me", true, false)
	ps = m.section.(*PRSection)

	got := warmed(m)
	for _, want := range refsIn("c/d", 9, 8) {
		if !slices.Contains(got, want) {
			t.Errorf("unfolded c/d: warmDetailCmd refs %v lack %v", got, want)
		}
	}
	if slices.ContainsFunc(got, func(r gh.PRRef) bool { return r.Repo == "a/b" }) {
		t.Errorf("a/b is still folded but warmDetailCmd requested it: %v", got)
	}
	pre := prefetchRefs(ps, m.cursor, m.fresh, 50)
	for _, want := range refsIn("c/d", 9, 8) {
		if !slices.Contains(pre, want) {
			t.Errorf("unfolded c/d: prefetchRefs %v lack %v", pre, want)
		}
	}
	if r := running(m); !slices.Equal(r, []string{"c/d", "c/d"}) {
		t.Errorf("unfolded c/d: runningCheckRows repos = %v, want both c/d rows", r)
	}
}

func TestRepoGroupCategoryRange(t *testing.T) {
	_, _, cross := repoGroupFixture()
	m := repoGroupModel(t, cross)
	m.repoFold["c/d"] = false
	review, open, _ := repoGroupFixture()
	m.paintSections(review, nil, open, cross, "me", true, false)
	ps := m.section.(*PRSection)
	m.cursor = shownIndexRef(m, gh.PRRef{Repo: "c/d", Number: 9})
	if m.cursor < 0 {
		t.Fatal("c/d#9 not shown")
	}

	for range 2 {
		u, _ := m.Update(keyMsg("V"))
		m = u.(Model)
	}
	var got []gh.PRRef
	for _, i := range m.sel.indices() {
		if ps.isFoldStop(i) {
			t.Fatalf("V selected the fold stop at %d", i)
		}
		got = append(got, ps.refAt(i))
	}
	slices.SortFunc(got, func(a, b gh.PRRef) int { return a.Number - b.Number })
	if want := refsIn("c/d", 8, 9); !slices.Equal(got, want) {
		t.Fatalf("V twice selected %v, want exactly %v", got, want)
	}
}

func shownIndexRef(m Model, want gh.PRRef) int {
	ps := m.section.(*PRSection)
	for i := range ps.Len() {
		if !ps.isFoldStop(i) && ps.refAt(i) == want {
			return i
		}
	}
	return -1
}

func TestUnitLabelIsRepoScoped(t *testing.T) {
	stacked := func(repo string, number, stack int) gh.PR {
		p := repoPR(repo, number, "bob", "2026-01-01")
		p.Stack = &gh.PRStack{Number: stack, Size: 2}
		return p
	}
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo(launchRepo)
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{
		stacked(launchRepo, 10, 3), stacked(launchRepo, 11, 3),
		stacked("other/x", 20, 3), stacked("other/x", 21, 3),
	})
	m.filterInput.SetValue("pr")
	m.applyFilter()
	ps := m.section.(*PRSection)
	if !ps.forceFlat {
		t.Fatal("bare query did not flatten the board")
	}

	i11 := shownIndexRef(m, gh.PRRef{Repo: launchRepo, Number: 11})
	i21 := shownIndexRef(m, gh.PRRef{Repo: "other/x", Number: 21})
	if i11 < 0 || i21 < 0 {
		t.Fatalf("rows not shown: #11 at %d, #21 at %d", i11, i21)
	}
	if a, b := ps.unitLabelAt(i11), ps.unitLabelAt(i21); a == b {
		t.Fatalf("unitLabelAt equal across repos (%q): Stack 3 in two repos merged", a)
	}

	m.cursor = i21
	u, _ := m.Update(keyMsg("V"))
	m = u.(Model)
	var got []gh.PRRef
	for _, i := range m.sel.indices() {
		got = append(got, ps.refAt(i))
	}
	slices.SortFunc(got, func(a, b gh.PRRef) int { return a.Number - b.Number })
	if want := refsIn("other/x", 20, 21); !slices.Equal(got, want) {
		t.Fatalf("V once on other/x#21 selected %v, want %v", got, want)
	}
}

func TestOtherRepoRowsRenderNewestFirstWhenArrivingOldestFirst(t *testing.T) {
	_, _, cross := repoGroupFixture()
	slices.SortFunc(cross, func(a, b gh.PR) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	m := repoGroupModel(t, cross)
	m.repoFold["c/d"] = false
	review, open, _ := repoGroupFixture()
	m.paintSections(review, nil, open, cross, "me", true, false)
	want := []string{"owner/repo#01", "owner/repo#02", "owner/repo#03", "c/d#09", "c/d#08", "stop:a/b"}
	if got := boardLayout(m); !slices.Equal(got, want) {
		t.Fatalf("layout = %v, want %v", got, want)
	}
}
