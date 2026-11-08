package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeRunsSource records every call. Commands run on their own goroutines in
// collectMsgs, so the counters sit behind a mutex.
type fakeRunsSource struct {
	mu        sync.Mutex
	runs      []gh.WorkflowRun
	jobs      map[int64][]gh.RunJob
	listCalls int
	jobCalls  []int64
}

func (f *fakeRunsSource) ListRuns(int) ([]gh.WorkflowRun, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	raw, err := json.Marshal(f.runs)
	return slices.Clone(f.runs), raw, err
}

func (f *fakeRunsSource) ListRunJobs(id int64) ([]gh.RunJob, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobCalls = append(f.jobCalls, id)
	return f.jobs[id], len(f.jobs[id]), nil
}

func (f *fakeRunsSource) lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}

func (f *fakeRunsSource) jobIDs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.jobCalls)
}

// collectMsgs runs cmd's tree and returns every message it produced. A
// tea.Tick leaf blocks for its full delay, so a leaf that does not answer
// quickly is skipped.
func collectMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return []tea.Msg{msg}
		}
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(t, c)...)
		}
		return out
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

func msgOf[T any](t *testing.T, msgs []tea.Msg) T {
	t.Helper()
	for _, msg := range msgs {
		if got, ok := msg.(T); ok {
			return got
		}
	}
	var zero T
	t.Fatalf("no %T among %v", zero, msgs)
	return zero
}

// runsBoard walks to the Runs board through Tab and lands a requested fetch.
func runsBoard(t *testing.T, src *fakeRunsSource, runs ...gh.WorkflowRun) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.SetRunsSource(src)
	m.width, m.height = 100, 40
	for range 2 {
		m, _ = update(m, keyMsg("tab"))
	}
	if m.mode != "run" {
		t.Fatalf("setup: mode = %q, want run", m.mode)
	}
	m, _ = update(m, runsFetchedMsg{runs: slices.Clone(runs), replace: true})
	return m
}

func pollResult(m Model, runs ...gh.WorkflowRun) runsFetchedMsg {
	return runsFetchedMsg{runs: runs, poll: true, seq: m.runsFetchSeq}
}

func shownRunIDs(m Model) []int64 { return m.section.(*RunSection).shownIDs() }

func finished(r gh.WorkflowRun, conclusion string) gh.WorkflowRun {
	r.Status, r.Conclusion = "completed", conclusion
	r.UpdatedAt = r.UpdatedAt.Add(time.Minute)
	return r
}

func TestTabCyclesPRsIssuesRunsPRs(t *testing.T) {
	keys := func(a map[string]action.Action) string { return strings.Join(slices.Sorted(maps.Keys(a)), " ") }
	m := NewModel("/repo", "is:open", nil)
	seen := map[string]bool{keys(m.actions): true}
	stops := []struct {
		mode    string
		actions map[string]action.Action
	}{
		{"issue", action.DefaultIssueActions()},
		{"run", action.DefaultRunActions()},
		{"pr", action.DefaultPRActions()},
	}
	for _, stop := range stops {
		m, _ = update(m, keyMsg("tab"))
		if m.mode != stop.mode || m.section.Kind() != stop.mode {
			t.Fatalf("mode = %q, kind = %q, want %q", m.mode, m.section.Kind(), stop.mode)
		}
		if got, want := keys(m.actions), keys(stop.actions); got != want {
			t.Errorf("%s actions = %q, want %q", stop.mode, got, want)
		}
		seen[keys(m.actions)] = true
	}
	if len(seen) != 3 {
		t.Errorf("want three distinct action sets, got %v", seen)
	}
}

func TestPRFilterSurvivesFullTabCycle(t *testing.T) {
	m := NewModel("/repo", "is:merged author:@me", nil)
	for range 3 {
		m, _ = update(m, keyMsg("tab"))
	}
	if m.mode != "pr" || m.state != "merged" || m.body != "author:@me" || m.filter != "is:merged author:@me" {
		t.Errorf("mode=%q state=%q body=%q filter=%q", m.mode, m.state, m.body, m.filter)
	}
}

func TestRunStateSurvivesFullTabCycle(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{})
	m, _ = update(m, keyMsg("s"))
	for range 3 {
		m, _ = update(m, keyMsg("tab"))
	}
	if m.mode != "run" || m.state != "active" || m.section.(*RunSection).state != "active" {
		t.Errorf("mode=%q state=%q", m.mode, m.state)
	}
}

func TestRunPollBeatRefetchesAndUpdatesRow(t *testing.T) {
	live := testRun(5, "ci", "in_progress", "", time.Minute)
	done := finished(live, "success")
	src := &fakeRunsSource{runs: []gh.WorkflowRun{done}}
	m := runsBoard(t, src, live)
	m.spinning = false // the spinner the Tab started gets no tick here to stop it

	m, cmd := update(m, runsPollMsg{})
	if m.refreshing || m.spinning {
		t.Error("a poll beat must be silent")
	}
	got := msgOf[runsFetchedMsg](t, collectMsgs(t, cmd))
	if src.lists() != 1 || !got.poll || got.replace || got.seq != m.runsFetchSeq {
		t.Fatalf("beat fetch: calls=%d msg=%+v", src.lists(), got)
	}

	m, _ = update(m, got)
	rs := m.section.(*RunSection)
	if s := rs.runAt(0).Status; s != "completed" {
		t.Fatalf("status = %q, want completed", s)
	}
	if row := plainRow(rs, 0, 80, RowOpts{Now: time.Now()}); !strings.Contains(row, "✓") {
		t.Errorf("row %q lacks the success glyph", row)
	}
}

func TestRunPollRefreshesFocusedJobs(t *testing.T) {
	live := testRun(5, "ci", "in_progress", "", time.Minute)
	src := &fakeRunsSource{jobs: map[int64][]gh.RunJob{5: {{ID: 1, Name: "build", Status: "in_progress"}}}}
	m := runsBoard(t, src, live)

	m, cmd := update(m, pollResult(m, live))
	got := msgOf[runJobsMsg](t, collectMsgs(t, cmd))
	if ids := src.jobIDs(); !slices.Equal(ids, []int64{5}) {
		t.Fatalf("job calls = %v, want one for run 5", ids)
	}

	m, _ = update(m, got)
	if e := m.runJobs[5]; len(e.jobs) != 1 || e.jobs[0].Status != "in_progress" || !e.runUpdated.Equal(live.UpdatedAt) {
		t.Errorf("entry = %+v", e)
	}
}

func TestRunJobsSkipFinishedAndRetryAfterError(t *testing.T) {
	run := finished(testRun(5, "ci", "", "", time.Minute), "success")
	m := runsBoard(t, &fakeRunsSource{}, run)

	if m.runJobsCmdForCursor() == nil {
		t.Fatal("a run with no entry must be fetched")
	}
	m, _ = update(m, runJobsMsg{runID: 5, jobs: []gh.RunJob{{ID: 1}}, runUpdated: run.UpdatedAt})
	if m.runJobsCmdForCursor() != nil {
		t.Error("a finished run whose UpdatedAt did not move must not be refetched")
	}

	m, _ = update(m, runJobsMsg{runID: 5, runUpdated: run.UpdatedAt, err: errors.New("boom")})
	if e := m.runJobs[5]; len(e.jobs) != 1 || e.err == nil {
		t.Errorf("a failure keeps the painted jobs and records the error: %+v", e)
	}
	if m.runJobsCmdForCursor() == nil {
		t.Error("a failed fetch must retry on the next settle")
	}
}

func TestRunStateKeyIsClientSide(t *testing.T) {
	src := &fakeRunsSource{}
	running := testRun(1, "a", "in_progress", "", time.Minute)
	failedRun := finished(testRun(2, "b", "", "", 2*time.Minute), "failure")
	passedRun := finished(testRun(3, "c", "", "", 3*time.Minute), "success")
	droppedRun := finished(testRun(4, "d", "", "", 4*time.Minute), "success")
	m := runsBoard(t, src, running, failedRun, passedRun, droppedRun)
	m, _ = update(m, pollResult(m, running, failedRun, passedRun))
	if !m.section.(*RunSection).isHeld(4) {
		t.Fatal("setup: run 4 should be held")
	}

	steps := []struct {
		state string
		want  []int64
	}{
		{"active", []int64{1}},
		{"failed", []int64{2}},
		{"recent", []int64{1, 2, 3}},
	}
	for _, step := range steps {
		var cmd tea.Cmd
		m, cmd = update(m, keyMsg("s"))
		collectMsgs(t, cmd)
		if m.state != step.state {
			t.Fatalf("state = %q, want %q", m.state, step.state)
		}
		if got := shownRunIDs(m); !slices.Equal(got, step.want) {
			t.Errorf("%s shows %v, want %v", step.state, got, step.want)
		}
		if m.refreshing {
			t.Errorf("%s: s must not flag a refresh", step.state)
		}
	}
	if rs := m.section.(*RunSection); rs.isHeld(4) || len(rs.runs) != 3 {
		t.Errorf("s must clear held rows: held=%v runs=%d", rs.held, len(rs.runs))
	}
	if n := src.lists(); n != 0 {
		t.Errorf("s made %d ListRuns calls, want none", n)
	}
}

func TestRunEmptyHints(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{})
	for _, want := range []string{"No runs.", "No active runs.", "No failed runs."} {
		if !strings.Contains(m.vp.View(), want) {
			t.Errorf("state %q: view %q lacks %q", m.state, m.vp.View(), want)
		}
		m, _ = update(m, keyMsg("s"))
	}
}

func TestRunBoardRefusesMultiSelect(t *testing.T) {
	for _, key := range []string{"space", "V"} {
		t.Run(key, func(t *testing.T) {
			m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
			var msg tea.Msg = keyMsg(key)
			if key == "space" {
				msg = tea.KeyPressMsg{Code: tea.KeySpace}
			}
			m, _ = update(m, msg)
			if m.sel.count() != 0 {
				t.Error("a run row must not be selectable")
			}
			if m.actionStatus == nil || !strings.Contains(m.actionStatus.fail, "multi-selected") {
				t.Errorf("status = %+v, want the multi-select hint", m.actionStatus)
			}
		})
	}
}

func TestRunPollSkippedWhileLogOpen(t *testing.T) {
	src := &fakeRunsSource{}
	m := runsBoard(t, src, testRun(1, "a", "in_progress", "", time.Minute))
	m.logView = true

	_, cmd := update(m, runsPollMsg{})
	if cmd == nil {
		t.Fatal("the loop must stay alive while a log is open")
	}
	collectMsgs(t, cmd)
	if n := src.lists(); n != 0 {
		t.Errorf("a beat under a log view made %d ListRuns calls", n)
	}
}

func TestRunResultDoesNotDisturbLogView(t *testing.T) {
	first := testRun(1, "a", "in_progress", "", time.Minute)
	m := runsBoard(t, &fakeRunsSource{}, first)
	m.logView, m.logCursor, m.runPolling = true, 7, false
	m.vp.SetContent("LOG LINE")

	m, cmd := update(m, pollResult(m, first, testRun(2, "b", "in_progress", "", time.Second)))
	if !strings.Contains(m.vp.View(), "LOG LINE") {
		t.Errorf("viewport repainted under the log view: %q", m.vp.View())
	}
	if m.logCursor != 7 {
		t.Errorf("logCursor = %d, want 7", m.logCursor)
	}
	if m.section.Len() != 1 {
		t.Errorf("rows = %d: the result must wait for the log to close", m.section.Len())
	}
	if cmd == nil || !m.runPolling {
		t.Error("the first result after entering the board must still arm the loop")
	}
}

func TestRunPollDelayTable(t *testing.T) {
	now := runsNow
	healthy := gh.RateSnapshot{Limit: 5000, Remaining: 4000, Reset: now.Add(time.Hour)}
	warn := gh.RateSnapshot{Limit: 5000, Remaining: 1000, Reset: now.Add(time.Hour)}
	crit := gh.RateSnapshot{Limit: 5000, Remaining: 400, Reset: now.Add(time.Hour)}
	rolled := gh.RateSnapshot{Limit: 5000, Remaining: 1, Reset: now.Add(-time.Minute)}

	cases := []struct {
		name          string
		active, quiet bool
		rate          gh.RateSnapshot
		want          time.Duration
		paused        bool
	}{
		{name: "active, keys", active: true, rate: healthy, want: 20 * time.Second},
		{name: "idle, keys", rate: healthy, want: time.Minute},
		{name: "active, quiet", active: true, quiet: true, rate: healthy, want: time.Minute},
		{name: "idle, quiet", quiet: true, rate: healthy, want: 2 * time.Minute},
		{name: "no reading", active: true, want: 20 * time.Second},
		{name: "under warn", active: true, rate: warn, want: 2 * time.Minute},
		{name: "under crit", active: true, rate: crit, want: 2 * time.Minute, paused: true},
		{name: "rolled-over window", active: true, rate: rolled, want: 20 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runPollDelay(c.active, c.quiet, c.rate, now); got != c.want {
				t.Errorf("delay = %v, want %v", got, c.want)
			}
			if got := runPollPaused(c.rate, now); got != c.paused {
				t.Errorf("paused = %v, want %v", got, c.paused)
			}
		})
	}
}

func TestRunPollKeepsGoingWhenQuiet(t *testing.T) {
	src := &fakeRunsSource{}
	m := runsBoard(t, src, testRun(1, "a", "in_progress", "", time.Minute))
	now := time.Now()
	m.now = func() time.Time { return now }
	m.lastKeyAt = now.Add(-time.Hour)

	if got := m.runsPollDelay(); got != runPollIdle {
		t.Errorf("delay = %v, want the quiet active cadence", got)
	}
	_, cmd := update(m, runsPollMsg{})
	msgOf[runsFetchedMsg](t, collectMsgs(t, cmd))
	if src.lists() != 1 {
		t.Errorf("a quiet beat must still fetch, got %d calls", src.lists())
	}
}

func TestRunPollDeferredWhenBusy(t *testing.T) {
	cases := map[string]func(*Model){
		"refreshing": func(m *Model) { m.refreshing = true },
		"filtering":  func(m *Model) { m.filtering = true },
		"picker":     func(m *Model) { m.showPicker = true },
		"confirm":    func(m *Model) { m.pending = &action.Action{} },
		"rate critical": func(m *Model) {
			m.rate = gh.RateSnapshot{Limit: 5000, Remaining: 1, Reset: time.Now().Add(time.Hour)}
		},
	}
	for name, prep := range cases {
		t.Run(name, func(t *testing.T) {
			src := &fakeRunsSource{}
			m := runsBoard(t, src, testRun(1, "a", "in_progress", "", time.Minute))
			prep(&m)
			_, cmd := update(m, runsPollMsg{})
			if cmd == nil {
				t.Fatal("a deferred beat must re-arm")
			}
			collectMsgs(t, cmd)
			if n := src.lists(); n != 0 {
				t.Errorf("%d ListRuns calls, want none", n)
			}
		})
	}
}

func TestRunPollStopsOffBoard(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
	if !m.runPolling {
		t.Fatal("setup: the first result should arm the loop")
	}
	m, _ = update(m, keyMsg("tab"))

	m, cmd := update(m, runsPollMsg{})
	if cmd != nil || m.runPolling {
		t.Errorf("off the board the loop must retire: cmd=%v polling=%v", cmd != nil, m.runPolling)
	}
}

func TestStaleRunPollResultDropped(t *testing.T) {
	old := testRun(1, "a", "in_progress", "", time.Minute)
	m := runsBoard(t, &fakeRunsSource{}, old)
	m.cache = cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m.refreshing = true
	m.runsFetchSeq++

	stale := finished(old, "success")
	m, _ = update(m, runsFetchedMsg{runs: []gh.WorkflowRun{stale}, raw: []byte("[]"), poll: true, seq: m.runsFetchSeq - 1})
	if got := m.section.(*RunSection).runAt(0).Status; got != "in_progress" {
		t.Errorf("stale beat regressed the board: status = %q", got)
	}
	if !m.refreshing {
		t.Error("a stale beat must not clear a requested fetch's refreshing")
	}
	if !m.cacheFresh(runsKey(m.repo)) {
		t.Error("a stale beat is still cached")
	}

	m, _ = update(m, runsFailedMsg{err: errors.New("boom"), poll: true, seq: m.runsFetchSeq - 1})
	if m.actionStatus != nil || m.emptyNotice != "" || !m.refreshing {
		t.Errorf("a stale failed beat must change nothing: %+v %q", m.actionStatus, m.emptyNotice)
	}
}

func TestPollResultKeepsRequestedRefreshing(t *testing.T) {
	run := testRun(1, "a", "in_progress", "", time.Minute)
	m := runsBoard(t, &fakeRunsSource{}, run)
	m.refreshing = true

	m, _ = update(m, pollResult(m, run))
	if !m.refreshing {
		t.Error("a poll result must not clear refreshing")
	}
	m, _ = update(m, runsFailedMsg{err: errors.New("boom"), poll: true, seq: m.runsFetchSeq})
	if !m.refreshing {
		t.Error("a poll failure must not clear refreshing")
	}
}

func TestRunPrewarmIsCacheOnly(t *testing.T) {
	m, _ := launchModel(t)
	m.refreshing, m.loaded = false, false
	section := m.section

	m, cmd := update(m, runsFetchedMsg{runs: []gh.WorkflowRun{testRun(1, "a", "in_progress", "", time.Minute)}, raw: []byte("[]"), replace: true})
	if cmd != nil {
		t.Error("a prewarm must emit nothing")
	}
	if m.section != section || m.section.Len() != 0 || m.refreshing || m.loaded {
		t.Errorf("prewarm touched the PR board: len=%d refreshing=%v loaded=%v", m.section.Len(), m.refreshing, m.loaded)
	}
	if !m.cacheFresh(runsKey(m.repo)) {
		t.Error("the prewarm should have landed in the cache")
	}

	m.refreshing = true
	m, _ = update(m, runsFetchedMsg{raw: []byte("[]"), replace: true})
	if !m.refreshing {
		t.Error("a prewarm must not clear the PR board's refreshing")
	}
}

func TestRunPrewarmFailureDropped(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.refreshing = true

	m, _ = update(m, runsFailedMsg{err: errors.New("boom")})
	if !m.refreshing || m.loaded || m.err != nil || m.emptyNotice != "" || m.actionStatus != nil {
		t.Errorf("refreshing=%v loaded=%v err=%v notice=%q status=%+v", m.refreshing, m.loaded, m.err, m.emptyNotice, m.actionStatus)
	}
}

func TestLaunchFetchCmdsAddsRunsOnlyWhenSourceSet(t *testing.T) {
	m, c := launchModel(t)
	_ = countLaunchSources(&m)
	base := len(m.launchFetchCmds())

	src := &fakeRunsSource{}
	m.SetRunsSource(src)
	cmds := m.launchFetchCmds()
	if len(cmds) != base+1 {
		t.Fatalf("with a source: %d cmds, want %d", len(cmds), base+1)
	}
	if _, ok := cmds[len(cmds)-1]().(runsFetchedMsg); !ok || src.lists() != 1 {
		t.Errorf("last launch cmd should be the runs prewarm, calls = %d", src.lists())
	}

	c.Set(runsKey(m.repo), json.RawMessage("[]"))
	if got := len(m.launchFetchCmds()); got != base {
		t.Errorf("fresh cache: %d cmds, want %d", got, base)
	}
}

func TestHydrateRepaintsCachedRunsOnEnteringBoard(t *testing.T) {
	m, c := launchModel(t)
	m.SetRunsSource(&fakeRunsSource{})
	m.width, m.height = 100, 40
	raw, err := json.Marshal([]gh.WorkflowRun{testRun(9, "cached", "completed", "success", time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	c.Set(runsKey(m.repo), raw)

	for range 2 {
		m, _ = update(m, keyMsg("tab"))
	}
	if got := shownRunIDs(m); !slices.Equal(got, []int64{9}) || !m.loaded {
		t.Errorf("shown = %v, loaded = %v", got, m.loaded)
	}
	if !strings.Contains(ansi.Strip(m.vp.View()), "cached") {
		t.Errorf("view lacks the cached row: %q", m.vp.View())
	}
}

func TestEnteringBoardOnCacheMissDropsPreviousRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.SetRunsSource(&fakeRunsSource{})
	m, _ = update(m, prsFetchedMsg{filter: m.filter, prs: []gh.PR{{Number: 1, Title: "stale-pr-row"}}})

	for range 2 {
		m, _ = update(m, keyMsg("tab"))
	}
	view := ansi.Strip(m.vp.View())
	if strings.Contains(view, "stale-pr-row") || !strings.Contains(view, "Loading") {
		t.Errorf("a miss should show Loading…, got %q", view)
	}
}

func TestPollDropsRowAsHeld(t *testing.T) {
	kept := testRun(1, "a", "in_progress", "", time.Minute)
	dropped := testRun(2, "b", "in_progress", "", 2*time.Minute)
	m := runsBoard(t, &fakeRunsSource{}, kept, dropped)

	m, _ = update(m, pollResult(m, kept))
	rs := m.section.(*RunSection)
	if rs.Len() != 2 || !rs.isHeld(2) || rs.isHeld(1) {
		t.Fatalf("rows=%d held=%v", rs.Len(), rs.held)
	}
	if rs.live(dropped) {
		t.Error("a held run is frozen, not live")
	}
	if held, _ := m.heldTag(slices.Index(rs.shownIDs(), 2)); !held {
		t.Error("heldTag should report the dropped row")
	}

	m, _ = update(m, pollResult(m, kept, dropped))
	if rs := m.section.(*RunSection); rs.isHeld(2) {
		t.Error("a held row the list returns again stops being held")
	}
}

func TestHeldRunsCappedNeverCursor(t *testing.T) {
	var runs []gh.WorkflowRun
	for id := int64(1); id <= runHeldMax+10; id++ {
		runs = append(runs, finished(testRun(id, fmt.Sprintf("wf%d", id), "", "", time.Duration(1000-id)*time.Minute), "success"))
	}
	m := runsBoard(t, &fakeRunsSource{}, runs...)
	m.cursor = m.section.Len() - 1 // the oldest row, run 1

	m, _ = update(m, pollResult(m, finished(testRun(1000, "new", "", "", 0), "success")))
	rs := m.section.(*RunSection)
	if len(rs.held) != runHeldMax {
		t.Errorf("held = %d, want %d", len(rs.held), runHeldMax)
	}
	if !rs.isHeld(1) {
		t.Error("the cursor's run must survive the cap")
	}
	if rs.isHeld(2) || slices.Contains(rs.shownIDs(), 2) {
		t.Error("the oldest non-cursor row should have been dropped")
	}
	if id := rs.runAt(m.cursor).ID; id != 1 {
		t.Errorf("cursor on run %d, want 1", id)
	}
}

func TestRunFinishingStaysInActiveView(t *testing.T) {
	running := testRun(1, "a", "in_progress", "", time.Minute)
	m := runsBoard(t, &fakeRunsSource{}, running, finished(testRun(2, "b", "", "", 2*time.Minute), "success"))
	m, _ = update(m, keyMsg("s"))
	if got := shownRunIDs(m); !slices.Equal(got, []int64{1}) {
		t.Fatalf("setup: active shows %v", got)
	}

	m, _ = update(m, pollResult(m, finished(running, "success")))
	rs := m.section.(*RunSection)
	if got := rs.shownIDs(); !slices.Equal(got, []int64{1}) {
		t.Fatalf("active shows %v, want the finished run kept", got)
	}
	if s := rs.runAt(0).Status; s != "completed" {
		t.Errorf("status = %q, want completed", s)
	}
}

func TestCtrlRClearsHeldRuns(t *testing.T) {
	kept := testRun(1, "a", "in_progress", "", time.Minute)
	src := &fakeRunsSource{runs: []gh.WorkflowRun{kept}}
	m := runsBoard(t, src, kept, testRun(2, "b", "in_progress", "", 2*time.Minute))
	m, _ = update(m, pollResult(m, kept))
	seq := m.runsFetchSeq

	m, cmd := update(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.runsFetchSeq != seq+1 || !m.refreshing {
		t.Fatalf("ctrl+r: seq=%d refreshing=%v", m.runsFetchSeq, m.refreshing)
	}
	got := msgOf[runsFetchedMsg](t, collectMsgs(t, cmd))
	if !got.replace || got.poll {
		t.Fatalf("ctrl+r result = %+v", got)
	}

	m, _ = update(m, got)
	rs := m.section.(*RunSection)
	if !slices.Equal(rs.shownIDs(), []int64{1}) || rs.isHeld(2) {
		t.Errorf("shown=%v held=%v", rs.shownIDs(), rs.held)
	}
	if m.refreshing {
		t.Error("the requested result clears refreshing")
	}
}

func TestRunCursorFollowsRunAcrossRepaint(t *testing.T) {
	r1 := finished(testRun(1, "a", "", "", 3*time.Minute), "success")
	r2 := finished(testRun(2, "b", "", "", 2*time.Minute), "success")
	r3 := finished(testRun(3, "c", "", "", time.Minute), "success")
	m := runsBoard(t, &fakeRunsSource{}, r1, r2, r3)
	m.cursor = 1 // run 2

	newest := finished(testRun(4, "d", "", "", 0), "success")
	m, _ = update(m, pollResult(m, r1, r2, r3, newest))
	if id := m.section.(*RunSection).runAt(m.cursor).ID; id != 2 {
		t.Fatalf("after a new row, cursor on run %d, want 2", id)
	}

	m, _ = update(m, runsFetchedMsg{runs: []gh.WorkflowRun{r1, r3, newest}, replace: true})
	if id := m.section.(*RunSection).runAt(m.cursor).ID; id != 1 {
		t.Errorf("after losing run 2, cursor on run %d, want its neighbour below (1)", id)
	}
}

func TestRunsFetchFailedKeepsRows(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
	m.runPolling = false

	m, cmd := update(m, runsFailedMsg{err: errors.New("boom")})
	if m.section.Len() != 1 || m.err != nil || m.emptyNotice != "" {
		t.Errorf("rows=%d err=%v notice=%q", m.section.Len(), m.err, m.emptyNotice)
	}
	if m.actionStatus == nil || !strings.Contains(m.actionStatus.fail, "Runs refresh failed: boom") {
		t.Errorf("status = %+v", m.actionStatus)
	}
	if cmd == nil || !m.runPolling {
		t.Error("polling must continue so the board self-heals")
	}
}

func TestRunsFetchFailedEmptyShowsNoticeNotBlank(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{})
	m.runPolling = false

	m, cmd := update(m, runsFailedMsg{err: errors.New("boom")})
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "owner/repo") || !strings.Contains(out, "Couldn't load runs: boom") || strings.Contains(out, "Error:") {
		t.Fatalf("render = %q", out)
	}
	if m.err != nil || !m.loaded || cmd == nil {
		t.Errorf("err=%v loaded=%v", m.err, m.loaded)
	}

	m, _ = update(m, runsFetchedMsg{})
	if m.emptyNotice != "" {
		t.Errorf("a successful fetch must clear the notice, got %q", m.emptyNotice)
	}
}

func TestActionsDisabledShowsNotice(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{})
	m.runPolling = false

	m, _ = update(m, runsFailedMsg{err: fmt.Errorf("list runs: %w", gh.ErrActionsDisabled)})
	if !strings.Contains(ansi.Strip(m.render()), "GitHub Actions is disabled or unavailable") {
		t.Fatalf("render = %q", ansi.Strip(m.render()))
	}
	if m.err != nil || !m.loaded || !m.runPolling {
		t.Errorf("err=%v loaded=%v polling=%v", m.err, m.loaded, m.runPolling)
	}
}

func TestForeignFetchFailedDoesNotBlankRunBoard(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
	m.refreshing = true

	m, _ = update(m, fetchFailedMsg{err: errors.New("viewer boom")})
	if m.err != nil || !m.refreshing || m.section.Len() != 1 {
		t.Fatalf("err=%v refreshing=%v rows=%d", m.err, m.refreshing, m.section.Len())
	}
	if m.actionStatus == nil || !strings.Contains(m.actionStatus.fail, "viewer boom") {
		t.Errorf("status = %+v, want a transient hint", m.actionStatus)
	}

	before := m.actionStatus
	m, _ = update(m, fetchFailedMsg{err: errors.New("pr boom"), mode: "pr", filter: "is:open"})
	if m.err != nil || !m.refreshing || m.actionStatus != before {
		t.Errorf("another board's failure must be dropped: err=%v refreshing=%v status=%+v", m.err, m.refreshing, m.actionStatus)
	}
}

func TestRunsFailureClearsRefreshing(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{})
	m.refreshing = true

	m, _ = update(m, runsFailedMsg{err: errors.New("boom")})
	if m.refreshing {
		t.Error("a requested failure must clear refreshing")
	}
}

func TestRunElapsedAdvancesOnThemeBeat(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
	base := time.Now()
	m.now = func() time.Time { return base }
	m.rowGen++
	m.renderList()
	before := m.vp.View()

	m.now = func() time.Time { return base.Add(10 * time.Minute) }
	m, _ = update(m, themePollMsg{})
	if m.vp.View() == before {
		t.Error("a live run's elapsed time should repaint on the theme beat")
	}

	m.logView = true
	m.vp.SetContent("LOG LINE")
	m, _ = update(m, themePollMsg{})
	if !strings.Contains(m.vp.View(), "LOG LINE") {
		t.Errorf("the theme beat repainted under the log view: %q", m.vp.View())
	}
}

func TestActionsDisabledWithRowsShowsHint(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
	m.runPolling = false

	m, cmd := update(m, runsFailedMsg{err: fmt.Errorf("list runs: %w", gh.ErrActionsDisabled)})
	if m.emptyNotice != "" {
		t.Errorf("a board with rows must not use the empty notice, got %q", m.emptyNotice)
	}
	if m.actionStatus == nil || !strings.Contains(m.actionStatus.fail, "Runs unavailable") {
		t.Errorf("status = %+v, want a transient hint", m.actionStatus)
	}
	if cmd == nil || !m.runPolling || m.section.Len() != 1 {
		t.Errorf("cmd=%v polling=%v rows=%d", cmd != nil, m.runPolling, m.section.Len())
	}
}

func TestHeldRunWithoutJobsIsFetchedOnce(t *testing.T) {
	kept := testRun(1, "a", "in_progress", "", time.Minute)
	dropped := finished(testRun(2, "b", "", "", 2*time.Minute), "success")
	m := runsBoard(t, &fakeRunsSource{}, kept, dropped)
	m, _ = update(m, pollResult(m, kept))
	rs := m.section.(*RunSection)
	m.cursor = slices.Index(rs.shownIDs(), 2)
	if !rs.isHeld(2) || m.cursor < 0 {
		t.Fatalf("setup: held=%v cursor=%d", rs.held, m.cursor)
	}

	if m.runJobsCmdForCursor() == nil {
		t.Fatal("a held run with no jobs entry must be fetched, or its preview loads forever")
	}
	m, _ = update(m, runJobsMsg{runID: 2, jobs: []gh.RunJob{{ID: 1}}, runUpdated: dropped.UpdatedAt})
	if m.runJobsCmdForCursor() != nil {
		t.Error("a held run is frozen once its jobs are loaded")
	}
}

// heldRunBoard is a two-row board whose second row (run 2, finished) the poll
// dropped, with the cursor on it.
func heldRunBoard(t *testing.T, src *fakeRunsSource) Model {
	t.Helper()
	kept := testRun(1, "a", "in_progress", "", time.Minute)
	dropped := finished(testRun(2, "b", "", "", 2*time.Minute), "success")
	m := runsBoard(t, src, kept, dropped)
	m, _ = update(m, pollResult(m, kept))
	rs := m.section.(*RunSection)
	m.cursor = slices.Index(rs.shownIDs(), 2)
	if !rs.isHeld(2) || m.cursor < 0 {
		t.Fatalf("setup: held=%v cursor=%d", rs.held, m.cursor)
	}
	return m
}

func TestHeldRunErrOnlyJobsRetries(t *testing.T) {
	t.Run("error-only entry retries", func(t *testing.T) {
		m := heldRunBoard(t, &fakeRunsSource{})
		m, _ = update(m, runJobsMsg{runID: 2, err: errors.New("boom")})
		if e := m.runJobs[2]; len(e.jobs) != 0 || e.err == nil {
			t.Fatalf("setup: entry = %+v", e)
		}
		if m.runJobsCmdForCursor() == nil {
			t.Error("a held run whose only jobs fetch failed must be fetched again")
		}
	})

	t.Run("loaded entry stays frozen", func(t *testing.T) {
		m := heldRunBoard(t, &fakeRunsSource{})
		m, _ = update(m, runJobsMsg{runID: 2, jobs: []gh.RunJob{{ID: 1}}, runUpdated: m.section.(*RunSection).runAt(m.cursor).UpdatedAt})
		if m.runJobsCmdForCursor() != nil {
			t.Error("a held run is frozen once its jobs are loaded")
		}
	})

	// A failure that persists must not turn every keypress into a fetch: only
	// the settled cursor (the debounce carrying the current seq) retries.
	t.Run("a persistent failure does not refetch per keypress", func(t *testing.T) {
		src := &fakeRunsSource{}
		m := heldRunBoard(t, src)
		m, _ = update(m, runJobsMsg{runID: 2, err: errors.New("boom")})

		for _, key := range []string{"l", "h", "alt+j", "alt+k"} {
			var cmd tea.Cmd
			m, cmd = update(m, keyMsg(key))
			collectMsgs(t, cmd)
			if ids := src.jobIDs(); len(ids) != 0 {
				t.Fatalf("%q fetched jobs: %v", key, ids)
			}
		}

		m, up := update(m, keyMsg("k"))
		stale := msgOf[detailDebounceMsg](t, collectMsgs(t, up))
		m, down := update(m, keyMsg("j"))
		settled := msgOf[detailDebounceMsg](t, collectMsgs(t, down))
		if m.cursor != slices.Index(m.section.(*RunSection).shownIDs(), 2) {
			t.Fatalf("setup: cursor %d is not back on the held run", m.cursor)
		}

		m, cmd := update(m, stale)
		collectMsgs(t, cmd)
		if ids := src.jobIDs(); len(ids) != 0 {
			t.Fatalf("a superseded debounce fetched jobs: %v", ids)
		}
		_, cmd = update(m, settled)
		collectMsgs(t, cmd)
		if ids := src.jobIDs(); len(ids) > 1 {
			t.Errorf("one settle fetched %d times: %v", len(ids), ids)
		}
	})
}

func TestRunFilterFetchesJobsForNewCursorRow(t *testing.T) {
	alpha := finished(testRun(1, "alpha", "", "", 2*time.Minute), "success")
	beta := finished(testRun(2, "beta", "", "", time.Minute), "success")
	src := &fakeRunsSource{jobs: map[int64][]gh.RunJob{2: {{ID: 9, Name: "build"}}}}
	m := runsBoard(t, src, alpha, beta)
	m = withJobs(m, alpha, gh.RunJob{ID: 8})
	m.cursor = slices.Index(shownRunIDs(m), 1)

	m, _ = update(m, keyMsg("/"))
	var cmd tea.Cmd
	for _, r := range "workflow:beta" {
		m, cmd = update(m, keyMsg(string(r)))
	}
	if ids := shownRunIDs(m); !slices.Equal(ids, []int64{2}) {
		t.Fatalf("setup: shown %v", ids)
	}
	deb := msgOf[detailDebounceMsg](t, collectMsgs(t, cmd))
	m, cmd = update(m, deb)
	m, _ = update(m, msgOf[runJobsMsg](t, collectMsgs(t, cmd)))
	if len(m.runJobs[2].jobs) != 1 {
		t.Errorf("jobs for the filtered-to row were not fetched: %+v", m.runJobs[2])
	}

	m, _ = update(m, keyMsg("enter"))
	m, cmd = update(m, keyMsg("esc"))
	if m.filterInput.Value() != "" || m.section.Len() != 2 {
		t.Fatalf("setup: filter %q rows %d", m.filterInput.Value(), m.section.Len())
	}
	if cmd == nil {
		t.Error("clearing the filter on the Runs board must schedule a detail fetch")
	}
}

func TestRunPollBeatRearmsFetchAndTick(t *testing.T) {
	m := runsBoard(t, &fakeRunsSource{}, testRun(1, "a", "in_progress", "", time.Minute))
	m.spinning = false

	_, cmd := update(m, runsPollMsg{})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("a beat must return the fetch and the next tick, got %T %d", batch, len(batch))
	}
}

func TestActiveRunWithJobsEntryIsRefetchedByListResult(t *testing.T) {
	live := testRun(5, "ci", "in_progress", "", time.Minute)
	src := &fakeRunsSource{jobs: map[int64][]gh.RunJob{5: {{ID: 1, Name: "build", Status: "in_progress"}}}}
	m := runsBoard(t, src, live)
	m = withJobs(m, live, gh.RunJob{ID: 1, Name: "build", Status: "queued"})

	_, cmd := update(m, pollResult(m, live))
	collectMsgs(t, cmd)
	if ids := src.jobIDs(); !slices.Equal(ids, []int64{5}) {
		t.Errorf("job calls = %v, want one refetch for run 5", ids)
	}
}

func TestPROnlyKeysChangeNothingOnRunRow(t *testing.T) {
	run := finished(testRun(7, "ci", "", "", time.Minute), "failure")
	src := &fakeRunsSource{}
	m := runsBoard(t, src, run)
	ms, as := &fakeMutationSource{}, &fakeActionsSource{}
	m.SetMutationSource(ms)
	m.SetActionsSource(as)
	listsBefore := src.lists()

	for _, k := range []string{"R", "D", "g", "p"} {
		next, cmd := update(m, keyMsg(k))
		collectMsgs(t, cmd)
		if next.pending != nil || next.showPicker || next.hideDrafts != m.hideDrafts || next.byAuthor != m.byAuthor {
			t.Errorf("%q changed state: pending=%v picker=%v drafts=%v byAuthor=%v", k, next.pending, next.showPicker, next.hideDrafts, next.byAuthor)
		}
	}
	if n := len(ms.mergeCalls) + len(ms.approveCalls) + len(ms.markReadyCalls) + len(as.rerunFailedCalls) + len(as.jobLogCalls); n != 0 {
		t.Errorf("%d source calls", n)
	}
	if src.lists() != listsBefore || len(src.jobIDs()) != 0 {
		t.Error("a key reached the runs source")
	}
}
