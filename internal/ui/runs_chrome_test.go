package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

// boardAt walks to the named board through Tab, with one row on each.
func boardAt(t *testing.T, mode string, w, h int) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("some-long-org-name/a-rather-long-repository-name-here")
	m.SetRunsSource(&fakeRunsSource{})
	m.width, m.height = w, h
	m.setPRs([]gh.PR{{Number: 1, Title: "a pr", Author: author("me")}})
	m.loaded = true
	for m.mode != mode {
		m, _ = update(m, keyMsg("tab"))
		if m.mode == "issue" {
			m.section.(*IssueSection).SetIssues([]gh.Issue{{Number: 20, Title: "an issue"}})
			m.loaded = true
		}
	}
	if mode == "run" {
		m, _ = update(m, runsFetchedMsg{replace: true, runs: []gh.WorkflowRun{
			testRun(3, "CI", "in_progress", "", 2*time.Minute),
			testRun(2, "Lint", "completed", "failure", time.Hour),
		}})
	}
	m.renderList()
	return m
}

func renderLines(m Model) []string {
	return strings.Split(ansi.Strip(m.render()), "\n")
}

func TestHeaderShowsThreeBoards(t *testing.T) {
	names := map[string]string{"pr": "PRs", "issue": "Issues", "run": "Runs"}
	for mode, active := range names {
		wide := ansi.Strip(boardAt(t, mode, 120, 30).header())
		for _, n := range names {
			if !strings.Contains(wide, n) {
				t.Errorf("%s at 120: header missing %q: %q", mode, n, wide)
			}
		}
		for _, withSel := range []bool{false, true} {
			for _, w := range []int{40, 30} {
				if w == 30 && withSel {
					continue
				}
				m := boardAt(t, mode, w, 24)
				if withSel {
					m.sel.toggle(0)
				}
				if h := ansi.Strip(m.header()); !strings.Contains(h, active) {
					t.Errorf("%s w=%d sel=%v: active board %q missing: %q", mode, w, withSel, active, h)
				}
				for i, l := range renderLines(m) {
					if got := lipgloss.Width(l); got > w {
						t.Fatalf("%s w=%d sel=%v: line %d is %d wide: %q", mode, w, withSel, i, got, l)
					}
				}
			}
		}
	}
}

func TestModeSegmentsCompactWidths(t *testing.T) {
	for mode, want := range map[string]int{"pr": 13, "issue": 16, "run": 14} {
		if got := lipgloss.Width(modeSegmentsCompact(mode)); got != want {
			t.Errorf("%s: compact width %d, want %d", mode, got, want)
		}
	}
}

func TestPRLegendNamesRuns(t *testing.T) {
	for _, mode := range []string{"pr", "issue"} {
		out := ansi.Strip(legendModel(mode, 160, 60).legendView())
		if !strings.Contains(out, "PRs/Issues/Runs") {
			t.Errorf("%s legend does not read PRs/Issues/Runs:\n%s", mode, out)
		}
	}
}

func TestRunLegendOffersOnlyRunActions(t *testing.T) {
	for _, w := range []int{160, 80} {
		out := ansi.Strip(legendModel("run", w, 60).legendView())
		for _, want := range []string{"logs", "rerun failed", "PRs/Issues/Runs"} {
			if !strings.Contains(out, want) {
				t.Errorf("w=%d: run legend missing %q:\n%s", w, want, out)
			}
		}
		for _, bad := range []string{"merge", "approve", "worktree", "select"} {
			if strings.Contains(out, bad) {
				t.Errorf("w=%d: run legend mentions %q:\n%s", w, bad, out)
			}
		}
	}
}

func TestRunStatusBarSaysLogs(t *testing.T) {
	bar := ansi.Strip(boardAt(t, "run", 140, 30).statusBar())
	for _, want := range []string{"↵:logs", "PRs/Issues/Runs", "r:rerun", "o:open"} {
		if !strings.Contains(bar, want) {
			t.Errorf("run status bar missing %q: %q", want, bar)
		}
	}
	for _, bad := range []string{"worktree", "select"} {
		if strings.Contains(bar, bad) {
			t.Errorf("run status bar mentions %q: %q", bad, bar)
		}
	}
	if pr := ansi.Strip(boardAt(t, "pr", 140, 30).statusBar()); !strings.Contains(pr, "↵:worktree") || !strings.Contains(pr, "PRs/Issues/Runs") {
		t.Errorf("PR status bar changed unexpectedly: %q", pr)
	}
}

func TestRunListTitle(t *testing.T) {
	m := boardAt(t, "run", 120, 30)
	if got := ansi.Strip(m.listTitle()); !strings.Contains(got, "runs · recent · 2") {
		t.Errorf("listTitle = %q", got)
	}
	m, _ = update(m, keyMsg("s"))
	if got := ansi.Strip(m.listTitle()); !strings.Contains(got, "runs · active · 1") {
		t.Errorf("listTitle after s = %q", got)
	}
}

func TestRunBoardFitsAtEverySize(t *testing.T) {
	check := func(t *testing.T, m Model, w, h int) {
		t.Helper()
		lines := renderLines(m)
		if len(lines) > h {
			t.Fatalf("%dx%d: height %d exceeds terminal", w, h, len(lines))
		}
		for i, l := range lines {
			if got := lipgloss.Width(l); got > w {
				t.Fatalf("%dx%d: line %d is %d wide: %q", w, h, i, got, l)
			}
		}
	}
	for _, sz := range [][2]int{{160, 40}, {120, 30}, {100, 24}, {80, 24}, {50, 20}, {30, 16}} {
		w, h := sz[0], sz[1]
		m := boardAt(t, "run", w, h)
		// Row 1 finishing on the next poll drops row 0 as held and leaves a finished row.
		m, _ = update(m, pollResult(m, finished(testRun(2, "Lint", "completed", "failure", time.Hour), "success")))
		m = withJobs(m, m.section.(*RunSection).runAt(0), runningJob(9, "build"))
		m.renderList()
		check(t, m, w, h)
	}
	m := boardAt(t, "run", 160, 40)
	for _, sz := range [][2]int{{120, 30}, {60, 20}, {30, 16}, {160, 40}, {80, 24}} {
		m = resize(m, sz[0], sz[1])
		check(t, m, sz[0], sz[1])
	}
}
