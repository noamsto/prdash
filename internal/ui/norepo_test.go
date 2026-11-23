package ui

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// noRepoSources stands in for every repo-scoped source and fails the test on
// any call one would make: outside a repo none of them may run.
type noRepoSources struct{ t *testing.T }

func (f noRepoSources) FetchPRs(filter string, _ int) ([]gh.PR, []byte, error) {
	f.t.Errorf("FetchPRs(%q) called outside a repo", filter)
	return nil, nil, errors.New("no repo")
}

func (f noRepoSources) FetchIssues(filter string, _ int) ([]gh.Issue, []byte, error) {
	f.t.Errorf("FetchIssues(%q) called outside a repo", filter)
	return nil, nil, errors.New("no repo")
}

func (f noRepoSources) FetchIssueDetail(n int) (gh.IssueDetail, []byte, error) {
	f.t.Errorf("FetchIssueDetail(%d) called outside a repo", n)
	return gh.IssueDetail{}, nil, errors.New("no repo")
}

func (f noRepoSources) ListRuns(int) ([]gh.WorkflowRun, []byte, error) {
	f.t.Error("ListRuns called outside a repo")
	return nil, nil, errors.New("no repo")
}

func (f noRepoSources) ListRunJobs(id int64) ([]gh.RunJob, int, error) {
	f.t.Errorf("ListRunJobs(%d) called outside a repo", id)
	return nil, 0, errors.New("no repo")
}

func (f noRepoSources) FetchAssignableUsers(repo string) ([]gh.User, []byte, error) {
	if repo == "" {
		f.t.Error(`FetchAssignableUsers("") called outside a repo`)
		return nil, nil, errors.New("no repo")
	}
	return nil, []byte("[]"), nil
}

// noRepoFixture is cross-repo rows in two repos; b/two is the more recently
// updated, and #7 is the viewer's own PR.
func noRepoFixture() []gh.PR {
	return []gh.PR{
		repoPR("b/two", 7, "me", "2026-03-01"),
		repoPR("a/one", 3, "zed", "2026-01-01"),
	}
}

// noRepoModel is a launch outside any checkout: empty repo, the fail-on-call
// fake behind every repo-scoped seam, plus a recording cross source and a
// viewer source answering "me".
func noRepoModel(t *testing.T) (Model, *fakeCrossSource, *fakeViewerSource, *cache.Cache) {
	t.Helper()
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel(t.TempDir(), "is:open", c)
	m.SetRepo("")
	fake := noRepoSources{t}
	m.SetPRSource(fake)
	m.SetIssueSource(fake)
	m.SetIssueDetailSource(fake)
	m.SetRunsSource(fake)
	m.SetMembersSource(fake)
	cross := &fakeCrossSource{prs: noRepoFixture()}
	m.SetCrossRepoSource(cross)
	viewer := &fakeViewerSource{login: "me"}
	m.SetViewerSource(viewer)
	m.width, m.height = 100, 40
	return m, cross, viewer, c
}

// runLaunch executes every launch fetch and feeds the results back through
// Update, returning the model and the messages delivered.
func runLaunch(t *testing.T, m Model) (Model, []tea.Msg) {
	t.Helper()
	var msgs []tea.Msg
	for _, cmd := range m.launchFetchCmds() {
		msgs = append(msgs, collectMsgs(t, cmd)...)
	}
	for _, msg := range msgs {
		m, _ = update(m, msg)
	}
	return m, msgs
}

func TestNoRepoLaunchFetchesOnlyCrossRepo(t *testing.T) {
	m, cross, viewer, _ := noRepoModel(t)

	m, _ = runLaunch(t, m)

	if n := cross.callCount(); n != 1 {
		t.Fatalf("cross fetches = %d, want 1", n)
	}
	if !slices.Equal(cross.filters, []string{crossRepoFilter}) || !slices.Equal(cross.limits, []int{crossRepoLimit}) {
		t.Errorf("cross calls = %v/%v, want [%q]/[%d]", cross.filters, cross.limits, crossRepoFilter, crossRepoLimit)
	}
	if viewer.calls != 1 {
		t.Errorf("viewer fetches = %d, want 1", viewer.calls)
	}
	if !m.loaded || m.err != nil {
		t.Fatalf("loaded = %v, err = %v; want a painted board", m.loaded, m.err)
	}
	want := []string{"b/two#07", "a/one#03"}
	if got := boardLayout(m); !slices.Equal(got, want) {
		t.Errorf("layout = %v, want %v", got, want)
	}
}

func TestNoRepoLaunchWithoutCrossSourceLandsEmptyBoard(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	m.SetCrossRepoSource(nil)

	m, _ = runLaunch(t, m)

	if !m.loaded || m.err != nil {
		t.Fatalf("loaded = %v, err = %v; want an empty painted board", m.loaded, m.err)
	}
	if n := m.section.Len(); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestNoRepoLaunchGateKeys(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	crossKey := prKey("", crossRepoFilter, crossRepoLimit)
	if got := m.launchGateKeys(); !slices.Equal(got, []string{crossKey}) {
		t.Fatalf("launchGateKeys = %v, want only the cross key", got)
	}
	m.SetCrossRepoSource(nil)
	if got := m.launchGateKeys(); !slices.Equal(got, []string{crossKey}) {
		t.Fatalf("launchGateKeys without a cross source = %v, want only the cross key", got)
	}

	in := NewModel("/repo", "is:open", nil)
	in.SetRepo("owner/repo")
	in.SetCrossRepoSource(&fakeCrossSource{})
	inRepo := slices.Concat(in.launchGateKeys(), in.issueLaunchGateKeys(),
		[]string{membersKey(in.repo), runsKey(in.repo)})
	if slices.Contains(inRepo, crossKey) {
		t.Errorf("no-repo cross key %q collides with an in-repo key in %v", crossKey, inRepo)
	}
}

func TestNoRepoLaunchSkipsFreshCrossCache(t *testing.T) {
	m, cross, _, c := noRepoModel(t)
	c.Set(prKey("", crossRepoFilter, crossRepoLimit), mustJSON(t, noRepoFixture()))
	c.Set(viewerKey(), json.RawMessage(`"me"`))
	m.Hydrate()

	m, msgs := runLaunch(t, m)

	if n := cross.callCount(); n != 0 {
		t.Errorf("cross fetches = %d, want 0 with a fresh cache", n)
	}
	msgOf[fetchSkippedMsg](t, msgs)
}

func TestNoRepoFetchCachesOnlyCrossKey(t *testing.T) {
	m, _, _, c := noRepoModel(t)

	runLaunch(t, m)

	if _, ok := c.Get(prKey("", crossRepoFilter, crossRepoLimit)); !ok {
		t.Error("cross key not cached")
	}
	for _, key := range []string{
		prKey("", searchFor("pr", "open", reviewBody), defaultLimit),
		prKey("", searchFor("pr", "open", reviewedBody), defaultLimit),
		prKey("", "is:open", openListLimit),
	} {
		if _, ok := c.Get(key); ok {
			t.Errorf("launch-scoped key %q cached outside a repo", key)
		}
	}
}

func TestNoRepoHydratePaintsCrossCache(t *testing.T) {
	t.Run("cross key cached", func(t *testing.T) {
		m, _, _, c := noRepoModel(t)
		c.Set(prKey("", crossRepoFilter, crossRepoLimit), mustJSON(t, noRepoFixture()))
		m.Hydrate()
		if !m.loaded {
			t.Fatal("loaded = false with the cross key cached")
		}
		if !m.section.(*PRSection).repoGrouped() {
			t.Error("repoGrouped = false on the hydrated no-repo board")
		}
		want := []string{"b/two#07", "a/one#03"}
		if got := boardLayout(m); !slices.Equal(got, want) {
			t.Errorf("layout = %v, want %v", got, want)
		}
	})
	t.Run("no cross key", func(t *testing.T) {
		m, _, _, c := noRepoModel(t)
		c.Set(prKey("", "is:open", openListLimit), mustJSON(t, noRepoFixture()))
		m.Hydrate()
		if m.loaded {
			t.Error("loaded = true without the cross key")
		}
	})
}

func TestNoRepoViewerArrivalRepaints(t *testing.T) {
	m, cross, _, _ := noRepoModel(t)
	m, _ = update(m, collectMsgs(t, m.sectionsFetchCmd(true))[0])
	if cross.callCount() != 1 || !m.loaded {
		t.Fatalf("setup: cross calls = %d, loaded = %v", cross.callCount(), m.loaded)
	}
	mine := gh.PRRef{Repo: "b/two", Number: 7}
	if got := m.section.(*PRSection).cats[mine]; got != "Others" {
		t.Fatalf("setup: #7 category = %q before the viewer, want Others", got)
	}

	m, _ = update(m, viewerFetchedMsg{login: "me"})

	ps := m.section.(*PRSection)
	if ps.viewer != "me" {
		t.Fatalf("section viewer = %q, want me", ps.viewer)
	}
	if got := ps.cats[mine]; got != "Mine" {
		t.Errorf("#7 category = %q after the viewer arrived, want Mine", got)
	}
	if got := boardLayout(m); !slices.Equal(got, []string{"b/two#07", "a/one#03"}) {
		t.Errorf("layout after viewer = %v", got)
	}
}

func TestNoRepoCrossFailureIsBoardError(t *testing.T) {
	m, cross, _, _ := noRepoModel(t)
	cross.err = errors.New("search exploded")

	m, msgs := runLaunch(t, m)

	msgOf[fetchFailedMsg](t, msgs)
	if m.err == nil || !strings.Contains(m.err.Error(), "search exploded") {
		t.Errorf("err = %v, want the cross failure as the board error", m.err)
	}
	if m.actionStatus != nil && strings.Contains(m.actionStatus.fail, "Other repos:") {
		t.Errorf("status = %q, want no Other repos hint", m.actionStatus.fail)
	}
}
