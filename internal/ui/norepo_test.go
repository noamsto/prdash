package ui

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
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
	m, _ = update(m, msgOf[sectionsFetchedMsg](t, collectMsgs(t, m.sectionsFetchCmd(true))))
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

func typeOmni(t *testing.T, m Model, s string) (Model, []tea.Cmd) {
	t.Helper()
	var cmds []tea.Cmd
	for _, r := range s {
		var cmd tea.Cmd
		m, cmd = update(m, tea.KeyPressMsg{Code: r, Text: string(r)})
		cmds = append(cmds, cmd)
	}
	return m, cmds
}

func failText(m Model) string {
	if m.actionStatus == nil {
		return ""
	}
	return m.actionStatus.fail
}

func TestNoRepoBoardRendersGroupsOnly(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	m, _ = runLaunch(t, m)

	if got := boardLayout(m); !slices.Equal(got, []string{"b/two#07", "a/one#03"}) {
		t.Fatalf("layout = %v", got)
	}
	view := ansi.Strip(m.render())
	if strings.Contains(view, "no open PRs") {
		t.Error("view shows the empty-launch-group placeholder")
	}
	if i, j := strings.Index(view, "b/two"), strings.Index(view, "a/one"); i < 0 || j < 0 || i > j {
		t.Errorf("b/two (%d) must precede a/one (%d)", i, j)
	}
	head := ansi.Strip(m.header())
	if !strings.Contains(head, "all repos") || strings.Contains(head, "Issues") || strings.Contains(head, "Runs") {
		t.Errorf("header = %q, want all repos and only the PRs board", head)
	}

	m.cursor = 0
	before := boardLayout(m)
	m, _ = update(m, keyMsg("e"))
	if after := boardLayout(m); slices.Equal(before, after) {
		t.Errorf("e did not fold the first group: %v", after)
	}
}

func TestNoRepoRepoScopedKeysHint(t *testing.T) {
	m, cross, _, _ := noRepoModel(t)
	m, _ = runLaunch(t, m)

	for _, tc := range []struct{ key, want string }{
		{"tab", "Issues and Runs need"},
		{"shift+tab", "Issues and Runs need"},
		{"s", "merged and closed need"},
		{"g", "Grouping by author needs"},
	} {
		m.actionStatus = &actionStat{settled: true}
		var cmd tea.Cmd
		m, cmd = update(m, keyMsg(tc.key))
		if cmd == nil {
			t.Errorf("%s: no clear-status cmd", tc.key)
		}
		if m.mode != "pr" || m.state != "open" {
			t.Errorf("%s: mode/state = %s/%s, want pr/open", tc.key, m.mode, m.state)
		}
		if got := failText(m); !strings.Contains(got, tc.want) || !strings.Contains(got, "start prdash inside a GitHub checkout") {
			t.Errorf("%s: hint = %q, want %q", tc.key, got, tc.want)
		}
	}

	m, _ = update(m, keyMsg("/"))
	var cmds []tea.Cmd
	m, cmds = typeOmni(t, m, "is:merged")
	for _, cmd := range cmds {
		for _, msg := range collectMsgs(t, cmd) {
			m, _ = update(m, msg)
		}
	}
	if m.omniServer != "" || m.filter != "is:open" {
		t.Errorf("omniServer/filter = %q/%q, want the filter untouched", m.omniServer, m.filter)
	}
	if got := failText(m); !strings.Contains(got, "search qualifiers need") {
		t.Errorf("qualifier hint = %q", got)
	}
	m, _ = update(m, keyMsg("esc"))
	m, cmd := update(m, keyMsg("ctrl+r"))
	for _, msg := range collectMsgs(t, cmd) {
		m, _ = update(m, msg)
	}
	if cross.callCount() < 2 {
		t.Errorf("cross fetches = %d, want the ctrl+r refresh to use the cross source", cross.callCount())
	}
}

func TestNoRepoActionTargetsRowRepo(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	checkout := t.TempDir()
	fd := &fakeDetailSource{}
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{repoPR("other/x", 7, "me", "2026-03-01")})
	m.renderList()

	oldCheckout, oldPreflight := checkoutForFn, preflightSwitchFn
	t.Cleanup(func() { checkoutForFn, preflightSwitchFn = oldCheckout, oldPreflight })
	var gotRepo string
	checkoutForFn = func(_, repo string) (string, error) {
		gotRepo = repo
		return checkout, nil
	}
	preflightSwitchFn = func(string, string) (gh.SwitchPreflight, error) { return gh.SwitchPreflight{}, nil }
	handoff := filepath.Join(t.TempDir(), "handoff")
	t.Setenv("PRDASH_ACTION_FILE", handoff)

	m, cmd := pressEnter(t, m, 0)
	if cmd == nil {
		t.Fatal("enter returned no cmd")
	}
	update(m, firstBatchMsg(t, cmd))

	if gotRepo != "other/x" {
		t.Errorf("checkout resolved for %q, want other/x", gotRepo)
	}
	want := []string{"wt", "-C", checkout, "switch", "pr:7"}
	if got := handoffLines(t, handoff); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Errorf("handoff = %v, want [%v]", got, want)
	}

	m.detailSeq++
	if dc := m.detailCmdForCursor(); dc != nil {
		collectMsgs(t, dc)
	}
	if len(fd.got) != 1 || fd.got[0][0] != (gh.PRRef{Repo: "other/x", Number: 7}) {
		t.Errorf("detail refs = %v, want other/x#7", fd.got)
	}
}

func TestNoRepoNonSwitchExitNotice(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	m.setPRs([]gh.PR{repoPR("other/x", 7, "me", "2026-03-01")})
	m.renderList()
	oldCheckout := checkoutForFn
	t.Cleanup(func() { checkoutForFn = oldCheckout })
	checkoutForFn = func(_, _ string) (string, error) { return t.TempDir(), nil }
	m.actions["enter"] = action.Action{
		Key:      "enter",
		Command:  action.Command{Argv: []string{"echo", "{{.Number}}"}},
		ExitsTUI: true,
	}

	m, cmd := pressEnter(t, m, 0)

	notQuit(t, cmd)
	if !strings.Contains(m.switchNotice, "needs a launch repo") || strings.Contains(m.switchNotice, "on  rows") {
		t.Errorf("switchNotice = %q", m.switchNotice)
	}
}

func TestNoRepoChromeOmitsRepoScopedKeys(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	m, _ = runLaunch(t, m)

	if bar := ansi.Strip(m.statusBar()); strings.Contains(bar, "PRs/Issues/Runs") {
		t.Errorf("status bar advertises ⇥ outside a repo: %q", bar)
	}
	for _, g := range m.keyPanes() {
		for _, h := range g.hints {
			if h.key == "⇥" || h.key == "s" || h.key == "g" {
				t.Errorf("legend lists %q (%s) outside a repo", h.key, h.label)
			}
		}
	}

	in := NewModel(t.TempDir(), "is:open", nil)
	in.SetRepo("owner/repo")
	in.width, in.height = 140, 30
	if bar := ansi.Strip(in.statusBar()); !strings.Contains(bar, "PRs/Issues/Runs") {
		t.Errorf("in-repo status bar lost PRs/Issues/Runs: %q", bar)
	}
}

func TestNoRepoQualifierHintWaitsForVisibleHint(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	m, _ = runLaunch(t, m)
	m, _ = update(m, keyMsg("/"))

	m.actionStatus = &actionStat{}
	m, _ = typeOmni(t, m, "@bob")
	if got := failText(m); got != "" {
		t.Errorf("hint shown while an action runs: %q", got)
	}
	if m.omniHinted != "" {
		t.Errorf("omniHinted = %q, want empty while the hint was suppressed", m.omniHinted)
	}

	m.actionStatus = &actionStat{settled: true}
	m, _ = typeOmni(t, m, " x")
	if got := failText(m); !strings.Contains(got, "search qualifiers need") {
		t.Errorf("hint after settling = %q", got)
	}
}

func TestNoRepoFilterChromeDropsQualifiers(t *testing.T) {
	m, _, _, _ := noRepoModel(t)
	m, _ = runLaunch(t, m)

	bar := ansi.Strip(m.filterBar())
	if !strings.Contains(bar, "filter — text") || strings.Contains(bar, "@user") {
		t.Errorf("empty filter bar = %q", bar)
	}

	m, _ = update(m, keyMsg("/"))
	m, _ = typeOmni(t, m, "is:draft")
	m, _ = update(m, keyMsg("enter"))
	if bar := ansi.Strip(m.filterBar()); !strings.Contains(bar, "qualifiers need a repo") {
		t.Errorf("committed filter bar = %q", bar)
	}

	found := false
	for _, g := range m.keyPanes() {
		for _, h := range g.hints {
			if h.key == "/" {
				found = h.label == "filter (text)"
			}
		}
	}
	if !found {
		t.Errorf("legend has no hint labelled %q", "filter (text)")
	}

	in := NewModel(t.TempDir(), "is:open", nil)
	in.SetRepo("owner/repo")
	in.width, in.height = 140, 30
	if bar := ansi.Strip(in.filterBar()); !strings.Contains(bar, "filter — @user · is: · text") {
		t.Errorf("in-repo filter bar = %q", bar)
	}
}
