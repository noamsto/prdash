package ui

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// bigRunID is past MaxInt32: a run ID is a 64-bit number.
const bigRunID = int64(1)<<33 + 5

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func resize(m Model, w, h int) Model {
	m, _ = update(m, tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func withJobs(m Model, run gh.WorkflowRun, jobs ...gh.RunJob) Model {
	m, _ = update(m, runJobsMsg{runID: run.ID, jobs: jobs, total: len(jobs), runUpdated: run.UpdatedAt})
	return m
}

// previewBoard is a Runs board wide enough for the side preview.
func previewBoard(t *testing.T, runs ...gh.WorkflowRun) Model {
	t.Helper()
	m := runsBoard(t, &fakeRunsSource{}, runs...)
	m = resize(m, 160, 40)
	if !m.layout().ShowSide {
		t.Fatal("setup: the side preview is not visible")
	}
	return m
}

func previewText(m Model) string {
	head, body := m.previewParts()
	return ansi.Strip(head + "\n" + body)
}

func runningJob(id int64, name string) gh.RunJob {
	return gh.RunJob{ID: id, Name: name, Status: "in_progress", StartedAt: stamp(time.Now().Add(-2 * time.Minute)),
		Steps: []gh.RunJobStep{{Name: "Set up job", Status: "completed"}, {Name: "Run tests", Status: "in_progress"}}}
}

func doneJob(id int64, name, conclusion string, took time.Duration) gh.RunJob {
	start := time.Now().Add(-time.Hour)
	return gh.RunJob{ID: id, Name: name, Status: "completed", Conclusion: conclusion,
		StartedAt: stamp(start), CompletedAt: stamp(start.Add(took))}
}

func TestRunPreviewShowsJobsAndRunningJob(t *testing.T) {
	run := testRun(7, "deploy", "in_progress", "", 3*time.Minute)
	run.HeadSHA = "0123456789abcdef"
	m := withJobs(previewBoard(t, run), run, doneJob(1, "lint", "success", 90*time.Second), runningJob(2, "unit-tests"))

	out := previewText(m)
	for _, want := range []string{"deploy", "#7", "feat/runs-board", "0123456", "Jobs  1/2 done", "lint", "took 1m 30s",
		"unit-tests", "2m", "↳ Run tests", ciRunningGlyph} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "01234567") {
		t.Errorf("the SHA must be cut to 7 bytes:\n%s", out)
	}
}

func TestRunPreviewStates(t *testing.T) {
	run := testRun(7, "deploy", "completed", "success", 3*time.Minute)
	run.HeadSHA = "" // fixtures carry short or empty SHAs
	base := previewBoard(t, run)

	if out := previewText(base); !strings.Contains(out, "loading jobs…") {
		t.Errorf("no entry yet:\n%s", out)
	}
	failed, _ := update(base, runJobsMsg{runID: 7, err: errors.New("boom")})
	if out := previewText(failed); !strings.Contains(out, "jobs failed: boom") {
		t.Errorf("failed fetch:\n%s", out)
	}
	if out := previewText(withJobs(base, run)); !strings.Contains(out, "no jobs yet") {
		t.Errorf("empty:\n%s", out)
	}
	more, _ := update(base, runJobsMsg{runID: 7, jobs: []gh.RunJob{doneJob(1, "a", "success", time.Minute), doneJob(2, "b", "success", time.Minute)},
		total: 5, runUpdated: run.UpdatedAt})
	if out := previewText(more); !strings.Contains(out, "… and 3 more on GitHub") || !strings.Contains(out, "2/5 done") {
		t.Errorf("truncated list:\n%s", out)
	}
}

func TestRunPreviewPicksUpPollStatus(t *testing.T) {
	run := testRun(7, "deploy", "in_progress", "", 3*time.Minute)
	m := previewBoard(t, run)
	m, _ = update(m, runJobsMsg{runID: 7, jobs: []gh.RunJob{runningJob(2, "unit-tests")}, total: 1, runUpdated: run.UpdatedAt})
	if out := previewText(m); !strings.Contains(out, ciRunningGlyph) || !strings.Contains(out, "↳ Run tests") {
		t.Fatalf("running job:\n%s", out)
	}

	m, _ = update(m, runJobsMsg{runID: 7, jobs: []gh.RunJob{doneJob(2, "unit-tests", "success", 4*time.Minute)}, total: 1, runUpdated: run.UpdatedAt})
	out := previewText(m)
	if strings.Contains(out, "↳") || !strings.Contains(out, "took 4m 0s") {
		t.Errorf("finished job:\n%s", out)
	}
}

func TestRunPreviewNarrow(t *testing.T) {
	run := testRun(7, "a-very-long-workflow-name-that-needs-clipping", "in_progress", "", 3*time.Minute)
	run.HeadSHA = "0123456789abcdef"
	run.DisplayTitle = strings.Repeat("a long commit subject ", 6)
	for _, w := range []int{50, 30} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := runsBoard(t, &fakeRunsSource{}, run)
			m = resize(m, w, 24)
			if m.layout().ShowSide {
				t.Fatal("setup: expected a single-column layout")
			}
			m = withJobs(m, run, doneJob(1, "a-job-with-a-very-long-name-indeed", "failure", 90*time.Second), runningJob(2, "unit-tests-on-another-long-name"))
			m, _ = update(m, keyMsg("z"))
			if !m.previewMax {
				t.Fatal("z did not maximize the preview")
			}
			out := m.render()
			lines := strings.Split(out, "\n")
			for i, ln := range lines {
				if got := lipgloss.Width(ln); got > w {
					t.Errorf("line %d is %d cells wide, terminal is %d: %q", i, got, w, ansi.Strip(ln))
				}
			}
			if len(lines) > 24 {
				t.Errorf("%d lines in a 24-row terminal", len(lines))
			}
			if !strings.Contains(ansi.Strip(out), "Jobs") {
				t.Errorf("the jobs section is missing:\n%s", ansi.Strip(out))
			}
		})
	}
}

func TestRunJobFocusDefault(t *testing.T) {
	run := testRun(7, "ci", "in_progress", "", time.Minute)
	m := NewModel("/repo", "is:open", nil)
	ok1, ok2 := doneJob(1, "a", "success", time.Minute), doneJob(2, "b", "success", time.Minute)
	bad, live := doneJob(3, "c", "failure", time.Minute), runningJob(4, "d")
	for _, tc := range []struct {
		name string
		jobs []gh.RunJob
		want int
	}{
		{"failed beats running", []gh.RunJob{ok1, live, bad, ok2}, 2},
		{"running beats first", []gh.RunJob{ok1, ok2, live}, 2},
		{"first otherwise", []gh.RunJob{ok1, ok2}, 0},
		{"none", nil, -1},
	} {
		if got := m.focusedRunJob(run, tc.jobs); got != tc.want {
			t.Errorf("%s: focus = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func focusedLine(m Model) string {
	for ln := range strings.SplitSeq(previewText(m), "\n") {
		if strings.Contains(ln, focusBarGlyph) && !strings.Contains(ln, "feat/") {
			return ln
		}
	}
	return ""
}

func TestRunJobFocusMovesAndSurvivesRepaint(t *testing.T) {
	run := finished(testRun(7, "ci", "", "", time.Minute), "success")
	jobs := []gh.RunJob{doneJob(1, "alpha", "success", time.Minute), doneJob(2, "beta", "success", time.Minute), doneJob(3, "gamma", "success", time.Minute)}
	m := withJobs(previewBoard(t, run), run, jobs...)

	if !strings.Contains(focusedLine(m), "alpha") {
		t.Fatalf("default focus: %q", focusedLine(m))
	}
	m, _ = update(m, keyMsg("l"))
	if !strings.Contains(focusedLine(m), "beta") {
		t.Errorf("l: %q", focusedLine(m))
	}
	m, _ = update(m, pollResult(m, run)) // a repaint must not reset it
	if !strings.Contains(focusedLine(m), "beta") {
		t.Errorf("after a repaint: %q", focusedLine(m))
	}
	for range 5 {
		m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if !strings.Contains(focusedLine(m), "gamma") {
		t.Errorf("clamp at the end: %q", focusedLine(m))
	}
	for range 5 {
		m, _ = update(m, keyMsg("h"))
	}
	if !strings.Contains(focusedLine(m), "alpha") {
		t.Errorf("clamp at the start: %q", focusedLine(m))
	}

	narrow := resize(m, 100, 40)
	narrow, _ = update(narrow, keyMsg("l"))
	if narrow.runFocusJob != 1 {
		t.Errorf("focus moved with no preview visible: job %d", narrow.runFocusJob)
	}
}

func TestRunJobFocusScrollsIntoView(t *testing.T) {
	run := finished(testRun(7, "ci", "", "", time.Minute), "success")
	var jobs []gh.RunJob
	for i := range 40 {
		jobs = append(jobs, doneJob(int64(i+1), fmt.Sprintf("job-%02d", i), "success", time.Minute))
	}
	m := withJobs(previewBoard(t, run), run, jobs...)

	visible := func() bool {
		head, body := m.previewParts()
		rows := strings.Split(ansi.Strip(dropLines(body, m.previewOffset)), "\n")
		for i, ln := range rows {
			if strings.Contains(ln, focusBarGlyph) {
				return i < m.previewBodyRows(head)
			}
		}
		return false
	}
	for i := 1; i < len(jobs); i++ {
		m, _ = update(m, keyMsg("l"))
		if !visible() {
			t.Fatalf("job %d is off screen (offset %d)", i, m.previewOffset)
		}
	}
	if m.previewOffset == 0 {
		t.Error("the list never scrolled")
	}
	for range len(jobs) {
		m, _ = update(m, keyMsg("h"))
		if !visible() {
			t.Fatalf("moving up lost the focused job (offset %d)", m.previewOffset)
		}
	}
	if m.previewOffset != 0 {
		t.Errorf("back on the first job the offset = %d, want 0", m.previewOffset)
	}
}

func TestRerunFailedJobsCallsSource(t *testing.T) {
	run := finished(testRun(bigRunID, "ci", "", "", time.Minute), "failure")
	m := runsBoard(t, &fakeRunsSource{}, run)
	fs := &fakeActionsSource{}
	m.SetActionsSource(fs)

	m, cmd := update(m, keyMsg("r"))
	if done, ok := driveBulk(t, cmd).(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("want a successful actionDoneMsg, got %+v", done)
	}
	if !slices.Equal(fs.rerunFailedCalls, []int64{bigRunID}) {
		t.Errorf("rerunFailedCalls = %v, want [%d]", fs.rerunFailedCalls, bigRunID)
	}
	if m.actionStatus == nil || !m.actionStatus.refresh || !m.actionStatus.rerunCI {
		t.Errorf("status = %+v, want refresh and rerunCI", m.actionStatus)
	}
}

func TestRerunRefusesActiveAndGreenAndHeld(t *testing.T) {
	active := testRun(1, "ci", "in_progress", "", time.Minute)
	green := finished(testRun(2, "ci", "", "", time.Minute), "success")
	dropped := finished(testRun(3, "ci", "", "", time.Minute), "failure")

	for name, tc := range map[string]struct {
		run  gh.WorkflowRun
		want string
	}{
		"active": {active, "still in progress"},
		"green":  {green, "No failed jobs"},
		"held":   {dropped, "ctrl+r"},
	} {
		t.Run(name, func(t *testing.T) {
			m := runsBoard(t, &fakeRunsSource{}, tc.run)
			fs := &fakeActionsSource{}
			m.SetActionsSource(fs)
			if name == "held" {
				other := testRun(9, "other", "in_progress", "", time.Second)
				m, _ = update(m, pollResult(m, other))
				if !m.section.(*RunSection).isHeld(3) {
					t.Fatal("setup: the run was not held")
				}
				m.cursor = slices.Index(shownRunIDs(m), 3)
			}
			m, _ = update(m, keyMsg("r"))
			if len(fs.rerunFailedCalls) != 0 {
				t.Errorf("rerun called: %v", fs.rerunFailedCalls)
			}
			if m.actionStatus == nil || !strings.Contains(m.actionStatus.fail, tc.want) {
				t.Errorf("status = %+v, want a hint containing %q", m.actionStatus, tc.want)
			}
		})
	}
}

func TestViewLogsCallsJobLog(t *testing.T) {
	run := finished(testRun(7, "ci", "", "", time.Minute), "failure")
	for _, tc := range []struct {
		name       string
		job        gh.RunJob
		failedOnly bool
	}{
		{"failed job reads failed steps", doneJob(11, "build", "failure", time.Minute), true},
		{"green job reads the whole log", doneJob(12, "lint", "success", time.Minute), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := withJobs(runsBoard(t, &fakeRunsSource{}, run), run, tc.job)
			fs := &fakeActionsSource{jobLogOut: []byte("build\tstep\t2024-01-01T00:00:00Z hello\n")}
			m.SetActionsSource(fs)

			m, cmd := update(m, keyMsg("enter"))
			if !m.logView || m.logLabel != tc.job.Name {
				t.Fatalf("logView = %v label = %q", m.logView, m.logLabel)
			}
			msg := msgOf[logFetchedMsg](t, collectMsgs(t, cmd))
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			if len(fs.jobLogCalls) != 1 || fs.jobLogCalls[0].jobID != tc.job.ID || fs.jobLogCalls[0].failedOnly != tc.failedOnly {
				t.Errorf("jobLogCalls = %+v, want job %d failedOnly=%v", fs.jobLogCalls, tc.job.ID, tc.failedOnly)
			}
		})
	}
}

func TestViewLogsRefusesUnfinishedJob(t *testing.T) {
	run := testRun(7, "ci", "in_progress", "", time.Minute)
	for name, jobs := range map[string][]gh.RunJob{
		"running":  {runningJob(1, "build")},
		"unloaded": nil,
	} {
		t.Run(name, func(t *testing.T) {
			m := runsBoard(t, &fakeRunsSource{}, run)
			if jobs != nil {
				m = withJobs(m, run, jobs...)
			}
			fs := &fakeActionsSource{}
			m.SetActionsSource(fs)

			m, _ = update(m, keyMsg("enter"))
			if m.logView || len(fs.jobLogCalls) != 0 {
				t.Errorf("logView = %v, calls = %v: no log may open", m.logView, fs.jobLogCalls)
			}
			if m.actionStatus == nil || !m.actionStatus.settled {
				t.Errorf("status = %+v, want a hint", m.actionStatus)
			}
		})
	}
}

func TestOpenInBrowserOpensRunURL(t *testing.T) {
	run := testRun(7, "ci", "completed", "success", time.Minute)
	run.HTMLURL = "https://example.test/owner/repo/actions/runs/7"
	dir := t.TempDir()
	out := filepath.Join(dir, "url")
	stub := filepath.Join(dir, "browser")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > '" + out + "'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil { //nolint:gosec // the stub must be executable
		t.Fatal(err)
	}
	t.Setenv("BROWSER", stub)

	m := runsBoard(t, &fakeRunsSource{}, run)
	_, cmd := update(m, keyMsg("o"))
	if done, ok := driveBulk(t, cmd).(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("want a successful actionDoneMsg, got %+v", done)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := os.ReadFile(out)
		if err == nil && len(got) > 0 {
			if string(got) != run.HTMLURL {
				t.Fatalf("opened %q, want %q", got, run.HTMLURL)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stub never recorded a URL: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLogEscReturnsToBoard(t *testing.T) {
	run := finished(testRun(7, "deploy-pipeline", "", "", time.Minute), "failure")
	m := withJobs(runsBoard(t, &fakeRunsSource{}, run), run, doneJob(11, "build", "failure", time.Minute))
	m.SetActionsSource(&fakeActionsSource{})

	m, _ = update(m, keyMsg("enter"))
	if !m.logView {
		t.Fatal("setup: no log view")
	}
	m, _ = update(m, keyMsg("esc"))
	if m.logView || m.expanded {
		t.Errorf("logView = %v expanded = %v, want the board", m.logView, m.expanded)
	}
	if !strings.Contains(ansi.Strip(m.vp.View()), "deploy-pipeline") {
		t.Errorf("the list was not repainted:\n%s", ansi.Strip(m.vp.View()))
	}
}

func TestNoPROrIssueActionReachableFromRunRow(t *testing.T) {
	run := finished(testRun(7, "ci", "", "", time.Minute), "failure")
	src := &fakeRunsSource{}
	m := runsBoard(t, src, run)
	ms, as := &fakeMutationSource{}, &fakeActionsSource{}
	m.SetMutationSource(ms)
	m.SetActionsSource(as)
	listsBefore := src.lists()

	runKeys := slices.Collect(maps.Keys(action.DefaultRunActions()))
	var foreign []string
	for _, set := range []map[string]action.Action{action.DefaultPRActions(), action.DefaultIssueActions()} {
		for k := range set {
			if _, ok := action.DefaultRunActions()[k]; !ok && !slices.Contains(foreign, k) {
				foreign = append(foreign, k)
			}
		}
	}
	foreign = append(foreign, "A", "M") // the PR actions built per row, not in the default map
	slices.Sort(foreign)

	for _, k := range foreign {
		t.Run(k, func(t *testing.T) {
			next, cmd := update(m, keyMsg(k))
			if next.pending != nil || next.showPicker || next.expanded || next.logView {
				t.Errorf("%q opened a prompt or view", k)
			}
			if cmd != nil {
				for _, msg := range collectMsgs(t, cmd) {
					switch msg.(type) {
					case actionDoneMsg, switchPreflightMsg:
						t.Errorf("%q ran an action: %T", k, msg)
					}
				}
			}
		})
	}
	if n := len(ms.mergeCalls) + len(ms.approveCalls) + len(ms.autoMergeCalls) + len(ms.markReadyCalls) +
		len(ms.updateBranchCalls) + len(ms.stackMergeCalls) + len(as.rerunFailedCalls) + len(as.jobLogCalls); n != 0 {
		t.Errorf("%d source calls from PR-only keys", n)
	}
	if src.lists() != listsBefore || len(src.jobIDs()) != 0 {
		t.Error("a PR-only key reached the runs source")
	}

	var got []string
	for _, a := range filterActions(m.actions, "") {
		got = append(got, a.Key)
	}
	slices.Sort(got)
	slices.Sort(runKeys)
	if !slices.Equal(got, runKeys) {
		t.Errorf("palette keys = %v, want %v", got, runKeys)
	}
}

func focusInView(m Model) bool {
	head, body := m.previewParts()
	for i, ln := range strings.Split(ansi.Strip(dropLines(body, m.previewOffset)), "\n") {
		if strings.Contains(ln, focusBarGlyph) {
			return i < m.previewBodyRows(head)
		}
	}
	return false
}

func TestRunDefaultFocusScrolledIntoViewOnFirstJobs(t *testing.T) {
	run := finished(testRun(7, "ci", "", "", time.Minute), "failure")
	var jobs []gh.RunJob
	for i := range 60 {
		conclusion := "success"
		if i == 50 {
			conclusion = "failure"
		}
		jobs = append(jobs, doneJob(int64(i+1), fmt.Sprintf("job-%02d", i), conclusion, time.Minute))
	}
	m := withJobs(previewBoard(t, run), run, jobs...)
	if !focusInView(m) {
		t.Fatalf("the default focused job is off screen (offset %d)", m.previewOffset)
	}

	m, _ = update(m, keyMsg("alt+k"))
	m, _ = update(m, keyMsg("alt+k"))
	offset := m.previewOffset
	m = withJobs(m, run, jobs...)
	if m.previewOffset != offset {
		t.Errorf("a poll repaint moved the user's scroll: %d -> %d", offset, m.previewOffset)
	}
}

func TestRunMoveCursorRevealsFocusedJob(t *testing.T) {
	first := finished(testRun(7, "ci", "", "", time.Minute), "success")
	second := finished(testRun(8, "deploy", "", "", time.Hour), "failure")
	var jobs []gh.RunJob
	for i := range 60 {
		conclusion := "success"
		if i == 50 {
			conclusion = "failure"
		}
		jobs = append(jobs, doneJob(int64(i+1), fmt.Sprintf("job-%02d", i), conclusion, time.Minute))
	}
	m := withJobs(previewBoard(t, first, second), second, jobs...)

	m, _ = update(m, keyMsg("j"))
	if !focusInView(m) {
		t.Fatalf("moving onto the run left the focused job off screen (offset %d)", m.previewOffset)
	}
}

// focusJobs is 60 jobs; the one at failAt failed (none when negative), which is
// where the default focus lands.
func focusJobs(failAt int) []gh.RunJob {
	jobs := make([]gh.RunJob, 0, 60)
	for i := range 60 {
		conclusion := "success"
		if i == failAt {
			conclusion = "failure"
		}
		jobs = append(jobs, doneJob(int64(i+1), fmt.Sprintf("job-%02d", i), conclusion, time.Minute))
	}
	return jobs
}

func shortJobs(prefix string) []gh.RunJob {
	jobs := make([]gh.RunJob, 0, 3)
	for i := range 3 {
		jobs = append(jobs, doneJob(int64(i+1), fmt.Sprintf("%s-%d", prefix, i), "success", time.Minute))
	}
	return jobs
}

func failedRun(id int64, name string, age time.Duration) gh.WorkflowRun {
	return finished(testRun(id, name, "", "", age), "failure")
}

func typeFilter(m Model, query string) Model {
	m, _ = update(m, keyMsg("/"))
	for _, r := range query {
		m, _ = update(m, keyMsg(string(r)))
	}
	return m
}

func onRun(t *testing.T, m Model, id int64) {
	t.Helper()
	if _, run, ok := m.cursorRun(); !ok || run.ID != id {
		t.Fatalf("setup: cursor on run %d (ok=%v), want %d", run.ID, ok, id)
	}
}

func TestRunPollMovingDefaultFocusStaysVisible(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	m := withJobs(previewBoard(t, run), run, focusJobs(-1)...)
	if m.previewOffset != 0 || !focusInView(m) {
		t.Fatalf("setup: focus 0 not shown at offset %d", m.previewOffset)
	}

	m = withJobs(m, run, focusJobs(50)...)
	if !focusInView(m) {
		t.Errorf("the default focus moved to job 50 and is off screen (offset %d)", m.previewOffset)
	}
}

func TestRunStateCycleRevealsFocus(t *testing.T) {
	t.Run("to_short_run", func(t *testing.T) {
		x := testRun(1, "xray", "in_progress", "", time.Minute)
		y := failedRun(2, "yank", 2*time.Minute)
		m := withJobs(previewBoard(t, x, y), x, shortJobs("xjob")...)
		m = withJobs(m, y, focusJobs(50)...)
		m, _ = update(m, keyMsg("j"))
		onRun(t, m, y.ID)
		if m.previewOffset == 0 || !focusInView(m) {
			t.Fatalf("setup: focus 50 not scrolled into view (offset %d)", m.previewOffset)
		}

		m, _ = update(m, keyMsg("s"))
		onRun(t, m, x.ID)
		if m.previewOffset != 0 || !strings.Contains(ansi.Strip(m.previewScrolled()), "xjob-0") {
			t.Errorf("active view: offset %d, want 0 with the run's jobs showing:\n%s", m.previewOffset, ansi.Strip(m.previewScrolled()))
		}
	})
	t.Run("to_long_run", func(t *testing.T) {
		x := finished(testRun(1, "xray", "", "", time.Minute), "success")
		y := failedRun(2, "yank", 2*time.Minute)
		m := withJobs(previewBoard(t, x, y), x, shortJobs("xjob")...)
		m = withJobs(m, y, focusJobs(50)...)
		onRun(t, m, x.ID)

		m, _ = update(m, keyMsg("s")) // active: nothing is running
		m, _ = update(m, keyMsg("s"))
		onRun(t, m, y.ID)
		if m.state != "failed" || !focusInView(m) {
			t.Errorf("failed view (state %q): the focused job is off screen (offset %d)", m.state, m.previewOffset)
		}
	})
}

func TestRunFilterRevealsFocusAndNeverBlank(t *testing.T) {
	t.Run("to_long_run", func(t *testing.T) {
		x := finished(testRun(1, "xray", "", "", time.Minute), "success")
		y := failedRun(2, "yank", 2*time.Minute)
		m := withJobs(previewBoard(t, x, y), x, shortJobs("xjob")...)
		m = withJobs(m, y, focusJobs(50)...)
		onRun(t, m, x.ID)

		m = typeFilter(m, "workflow:yank")
		onRun(t, m, y.ID)
		if !focusInView(m) {
			t.Errorf("filtering to the long run left the focused job off screen (offset %d)", m.previewOffset)
		}
	})
	t.Run("to_short_run", func(t *testing.T) {
		x := finished(testRun(1, "xray", "", "", 2*time.Minute), "success")
		y := failedRun(2, "yank", time.Minute)
		m := withJobs(previewBoard(t, x, y), x, shortJobs("xjob")...)
		m = withJobs(m, y, focusJobs(50)...)
		onRun(t, m, y.ID)
		if m.previewOffset == 0 {
			t.Fatal("setup: the long run's preview is not scrolled")
		}

		m = typeFilter(m, "workflow:xray")
		onRun(t, m, x.ID)
		if m.previewOffset != 0 || !strings.Contains(ansi.Strip(m.previewScrolled()), "xjob-0") {
			t.Errorf("filtering to the short run: offset %d, want 0 with its jobs showing:\n%s", m.previewOffset, ansi.Strip(m.previewScrolled()))
		}
	})
}

func TestRunFilterEscClearRevealsFocus(t *testing.T) {
	x := finished(testRun(1, "xray", "", "", 2*time.Minute), "success")
	y := failedRun(2, "yank", time.Minute)
	m := withJobs(previewBoard(t, x, y), y, focusJobs(50)...)
	m = withJobs(m, x, shortJobs("xjob")...)
	m, _ = update(m, keyMsg("j"))
	onRun(t, m, x.ID)

	m = typeFilter(m, "workflow:xray")
	m, _ = update(m, keyMsg("enter"))
	onRun(t, m, x.ID)
	if m.filtering || m.previewOffset != 0 {
		t.Fatalf("setup: filtering=%v offset=%d", m.filtering, m.previewOffset)
	}

	m, _ = update(m, keyMsg("esc"))
	onRun(t, m, y.ID)
	if !focusInView(m) {
		t.Errorf("clearing the filter left the focused job off screen (offset %d)", m.previewOffset)
	}
}

func TestRunRefreshDroppingAnchorRevealsFocus(t *testing.T) {
	a := testRun(1, "alpha", "in_progress", "", time.Minute)
	b := failedRun(2, "bravo", 2*time.Minute)
	m := withJobs(previewBoard(t, a, b), b, focusJobs(50)...)
	onRun(t, m, a.ID)

	m, _ = update(m, runsFetchedMsg{runs: []gh.WorkflowRun{b}, replace: true})
	onRun(t, m, b.ID)
	if !focusInView(m) {
		t.Errorf("the refresh moved the cursor onto a run whose focused job is off screen (offset %d)", m.previewOffset)
	}
}

func TestRunBoardReentryRevealsFocus(t *testing.T) {
	y := failedRun(1, "yank", time.Minute)
	w := failedRun(2, "whisky", 2*time.Minute)
	m := withJobs(previewBoard(t, y, w), w, focusJobs(50)...)
	m = withJobs(m, y, focusJobs(50)...)
	m, _ = update(m, keyMsg("j"))
	onRun(t, m, w.ID)
	m, _ = update(m, keyMsg("alt+j"))
	if m.previewOffset == 0 {
		t.Fatal("setup: the other run's preview is not scrolled")
	}

	for range 3 {
		m, _ = update(m, keyMsg("tab"))
	}
	m, _ = update(m, runsFetchedMsg{runs: []gh.WorkflowRun{y, w}, replace: true})
	if m.mode != "run" || len(m.runJobs[y.ID].jobs) != 60 {
		t.Fatalf("setup: mode %q, cached jobs %d", m.mode, len(m.runJobs[y.ID].jobs))
	}
	onRun(t, m, y.ID)
	if !focusInView(m) || m.previewOffset == 0 {
		t.Errorf("re-entering the board left the focused job off screen (offset %d)", m.previewOffset)
	}
}

func TestRunResizeRevealsFocus(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	t.Run("widen", func(t *testing.T) {
		m := runsBoard(t, &fakeRunsSource{}, run)
		m = withJobs(m, run, focusJobs(50)...)
		if m.previewShown() {
			t.Fatal("setup: the preview is already shown")
		}

		m = resize(m, 160, 40)
		if !m.previewShown() {
			t.Fatal("setup: widening did not show the preview")
		}
		if !focusInView(m) {
			t.Errorf("widening left the focused job off screen (offset %d)", m.previewOffset)
		}
	})
	t.Run("shrink", func(t *testing.T) {
		m := withJobs(previewBoard(t, run), run, focusJobs(50)...)
		if !focusInView(m) {
			t.Fatal("setup: the focused job is not in view")
		}

		m = resize(m, 160, 14)
		if !m.previewShown() {
			t.Fatal("setup: shrinking hid the preview")
		}
		if !focusInView(m) {
			t.Errorf("shrinking the height left the focused job off screen (offset %d)", m.previewOffset)
		}
	})
}

// scrollAway moves the preview up until the focused job leaves the window.
func scrollAway(t *testing.T, m Model) Model {
	t.Helper()
	for range 2 {
		m, _ = update(m, keyMsg("alt+k"))
	}
	if focusInView(m) || m.previewOffset == 0 {
		t.Fatalf("setup: focus still in view or no scroll left (offset %d)", m.previewOffset)
	}
	return m
}

func TestRunResizeKeepsDeliberateScroll(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	m := scrollAway(t, withJobs(previewBoard(t, run), run, focusJobs(50)...))
	offset := m.previewOffset

	for _, size := range [][2]int{{170, 40}, {170, 36}} {
		m = resize(m, size[0], size[1])
		if m.previewOffset != offset || focusInView(m) {
			t.Errorf("resize to %v: offset %d -> %d, focus in view %v; want the scroll kept", size, offset, m.previewOffset, focusInView(m))
		}
	}
}

func TestRunZKeepsDeliberateScroll(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	// inMax reports whether the focus shows with the preview maximized, which
	// is the shorter pane while the keys panel docks beside the side preview.
	inMax := func(m Model) bool {
		m.previewMax = true
		return focusInView(m)
	}
	loaded := withJobs(previewBoard(t, run), run, focusJobs(50)...)
	if !focusInView(loaded) || inMax(loaded) {
		t.Fatalf("setup: focus must show in the side pane and not maximized (offset %d)", loaded.previewOffset)
	}

	t.Run("out_of_view", func(t *testing.T) {
		m := scrollAway(t, loaded)
		if inMax(m) {
			t.Fatal("setup: the focus shows in the maximized pane")
		}
		offset := m.previewOffset

		for i := range 2 {
			m, _ = update(m, keyMsg("z"))
			head, body := m.previewParts()
			want := min(offset, max(lipgloss.Height(body)-m.previewBodyRows(head), 0))
			if m.previewOffset != want || focusInView(m) {
				t.Errorf("z #%d (max=%v): offset %d, focus in view %v; want offset %d, focus out", i+1, m.previewMax, m.previewOffset, focusInView(m), want)
			}
		}
	})
	t.Run("visible_then_hidden_by_smaller_pane", func(t *testing.T) {
		m, _ := update(loaded, keyMsg("z"))
		if !m.previewMax || !focusInView(m) {
			t.Errorf("z: max=%v, focus in view %v (offset %d); the focus was visible before and must follow", m.previewMax, focusInView(m), m.previewOffset)
		}
	})
}

func TestRunClampedMoveKeepsFocusInView(t *testing.T) {
	a := testRun(1, "alpha", "in_progress", "", time.Minute)
	b := failedRun(2, "bravo", 2*time.Minute)
	t.Run("single_run", func(t *testing.T) {
		m := withJobs(previewBoard(t, b), b, focusJobs(50)...)
		if m.section.Len() != 1 || m.previewOffset == 0 || !focusInView(m) {
			t.Fatalf("setup: rows %d, offset %d, in view %v", m.section.Len(), m.previewOffset, focusInView(m))
		}

		m, _ = update(m, keyMsg("j"))
		if !focusInView(m) {
			t.Errorf("a clamped j left the focused job off screen (offset %d)", m.previewOffset)
		}
	})
	t.Run("last_row", func(t *testing.T) {
		m := withJobs(previewBoard(t, a, b), b, focusJobs(50)...)
		m, _ = update(m, keyMsg("j"))
		onRun(t, m, b.ID)
		if m.previewOffset == 0 || !focusInView(m) {
			t.Fatalf("setup: offset %d, in view %v", m.previewOffset, focusInView(m))
		}

		m, _ = update(m, keyMsg("j"))
		onRun(t, m, b.ID)
		if !focusInView(m) {
			t.Errorf("a clamped j on the last row left the focused job off screen (offset %d)", m.previewOffset)
		}
	})
}

func TestRunJobsRefreshShiftingSpanKeepsFocusInView(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	// Jobs above the failed one start running, each gaining a "↳ step" line, so
	// the failed job's span moves down while it stays the focus.
	shifted := focusJobs(50)
	for i := range 5 {
		shifted[i] = runningJob(int64(i+1), fmt.Sprintf("job-%02d", i))
	}
	t.Run("focus_in_view", func(t *testing.T) {
		m := withJobs(previewBoard(t, run), run, focusJobs(50)...)
		if !focusInView(m) {
			t.Fatal("setup: the focused job is not in view")
		}

		m = withJobs(m, run, shifted...)
		if !focusInView(m) {
			t.Errorf("the focused job's span moved and it is off screen (offset %d)", m.previewOffset)
		}
	})
	t.Run("scrolled_away", func(t *testing.T) {
		m := scrollAway(t, withJobs(previewBoard(t, run), run, focusJobs(50)...))
		offset := m.previewOffset

		m = withJobs(m, run, shifted...)
		if m.previewOffset != offset {
			t.Errorf("a jobs refresh moved the user's scroll: %d -> %d", offset, m.previewOffset)
		}
	})
}

// maxPreviewOffset is the furthest the preview can scroll right now.
func maxPreviewOffset(m Model) int {
	head, body := m.previewParts()
	return max(0, lipgloss.Height(body)-m.previewBodyRows(head))
}

func TestRunResizeTallerClampsOffset(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	m := resize(withJobs(previewBoard(t, run), run, focusJobs(50)...), 160, 30)
	for range 100 {
		m, _ = update(m, keyMsg("alt+j"))
	}
	if m.previewOffset == 0 || m.previewOffset != maxPreviewOffset(m) || !focusInView(m) {
		t.Fatalf("setup: offset %d, max %d, focus in view %v", m.previewOffset, maxPreviewOffset(m), focusInView(m))
	}

	m = resize(m, 160, 60)
	if got, limit := m.previewOffset, maxPreviewOffset(m); got > limit {
		t.Errorf("a taller terminal left the offset at %d, past its maximum %d", got, limit)
	}
}

func TestRunJobsRefreshShrinkingBodyClampsOffset(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	// Running jobs above the failed one carry a "↳ step" line each; finishing
	// them shrinks the body while the failed job stays the focus.
	running := focusJobs(50)
	for i := range 5 {
		running[i] = runningJob(int64(i+1), fmt.Sprintf("job-%02d", i))
	}
	m := withJobs(previewBoard(t, run), run, running...)
	for range 100 {
		m, _ = update(m, keyMsg("alt+j"))
	}
	if m.previewOffset == 0 || m.previewOffset != maxPreviewOffset(m) || !focusInView(m) {
		t.Fatalf("setup: offset %d, max %d, focus in view %v", m.previewOffset, maxPreviewOffset(m), focusInView(m))
	}

	m = withJobs(m, run, focusJobs(50)...)
	if !focusInView(m) {
		t.Fatalf("setup: the focus left the view (offset %d)", m.previewOffset)
	}
	if got, limit := m.previewOffset, maxPreviewOffset(m); got > limit {
		t.Errorf("a refresh that shrank the body left the offset at %d, past its maximum %d", got, limit)
	}
}

func TestRunFilterNoMatchThenClearRevealsFocus(t *testing.T) {
	run := failedRun(7, "ci", time.Minute)
	m := withJobs(previewBoard(t, run), run, focusJobs(50)...)
	if m.previewOffset == 0 || !focusInView(m) {
		t.Fatalf("setup: offset %d, focus in view %v", m.previewOffset, focusInView(m))
	}

	m = typeFilter(m, "workflow:nomatch")
	m, _ = update(m, keyMsg("enter"))
	if _, _, ok := m.cursorRun(); ok || m.filtering {
		t.Fatalf("setup: a run is under the cursor (%v) or still filtering (%v)", ok, m.filtering)
	}
	m, _ = update(m, keyMsg("alt+k"))

	m, _ = update(m, keyMsg("esc"))
	onRun(t, m, run.ID)
	if !focusInView(m) {
		t.Errorf("clearing a no-match filter left the focused job off screen (offset %d)", m.previewOffset)
	}
}
