package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

var runsNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func testRun(id int64, name, status, conclusion string, age time.Duration) gh.WorkflowRun {
	t := time.Now().Add(-age)
	return gh.WorkflowRun{
		ID: id, Name: name, DisplayTitle: "Fix the flaky integration test", RunNumber: int(id),
		Status: status, Conclusion: conclusion, HeadBranch: "feat/runs-board", Event: "pull_request",
		Actor: "octocat", CreatedAt: t, UpdatedAt: t, RunStartedAt: t, HTMLURL: "https://example.test/run",
	}
}

func runSection(state string, runs ...gh.WorkflowRun) *RunSection {
	s := NewRunSection("", state)
	s.SetRuns(runs, nil)
	return s
}

func plainRow(s *RunSection, i, w int, o RowOpts) string {
	o.Width = w
	return ansi.Strip(s.RenderRow(i, o))
}

func TestRunRowShowsAllColumns(t *testing.T) {
	s := runSection("recent", testRun(7, "CI", "completed", "success", 3*time.Hour))
	row := s.RenderRow(0, RowOpts{Width: 80})
	if got := lipgloss.Width(row); got > 80 {
		t.Fatalf("row is %d cells, want <= 80", got)
	}
	plain := ansi.Strip(row)
	for _, want := range []string{"CI", "feat/runs-board", "pr", "octocat", "3h", "✓", "Fix the flaky"} {
		if !strings.Contains(plain, want) {
			t.Errorf("row %q missing %q", plain, want)
		}
	}
}

func TestRunRowNarrowKeepsStatusWorkflowTime(t *testing.T) {
	s := runSection("recent", testRun(7, "Continuous Integration Pipeline", "completed", "failure", 3*time.Hour))
	for _, w := range []int{24, 40, 60} {
		row := s.RenderRow(0, RowOpts{Width: w})
		if got := lipgloss.Width(row); got > w {
			t.Errorf("width %d: row is %d cells", w, got)
		}
		plain := ansi.Strip(row)
		for _, want := range []string{"✗", "Continuous", "3h"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width %d: row %q missing %q", w, plain, want)
			}
		}
	}
}

func TestRunRowActiveVersusFinished(t *testing.T) {
	running := testRun(1, "CI", "in_progress", "", time.Minute)
	running.RunStartedAt = runsNow.Add(-90 * time.Second)
	queued := testRun(2, "CI", "queued", "", time.Minute)
	queued.RunStartedAt = time.Time{}
	queued.CreatedAt = runsNow.Add(-5 * time.Minute)
	done := testRun(3, "CI", "completed", "success", time.Hour)
	s := runSection("active", running, queued, done)
	s.SetShown(allIdx(3))
	o := RowOpts{Width: 80, Now: runsNow}
	rows := map[int64]string{}
	for i := range s.Len() {
		rows[s.runAt(i).ID] = plainRow(s, i, 80, o)
	}
	if r := rows[1]; !strings.Contains(r, ciRunningGlyph) || !strings.Contains(r, "1m") {
		t.Errorf("running row %q lacks glyph or elapsed", r)
	}
	if r := rows[2]; !strings.Contains(r, runQueuedGlyph) || !strings.Contains(r, "5m") {
		t.Errorf("queued row %q lacks glyph or elapsed", r)
	}
	if r := rows[3]; strings.Contains(r, ciRunningGlyph) || strings.Contains(r, runQueuedGlyph) || !strings.Contains(r, "1h") {
		t.Errorf("finished row %q should show age and no live glyph", r)
	}
}

func TestRunRowHeldShowsAgeNotElapsed(t *testing.T) {
	r := testRun(1, "CI", "in_progress", "", 2*time.Hour)
	r.RunStartedAt = runsNow.Add(-90 * time.Second)
	s := NewRunSection("", "recent")
	s.SetRuns([]gh.WorkflowRun{r}, map[int64]bool{1: true})
	row := plainRow(s, 0, 80, RowOpts{Now: runsNow})
	if !strings.Contains(row, "2h") || strings.Contains(row, "1m") {
		t.Errorf("held row %q should show age, not elapsed", row)
	}
	if s.live(r) {
		t.Error("a held run is not live")
	}
}

func TestRunGroupingActiveFirst(t *testing.T) {
	s := runSection("recent",
		testRun(1, "CI", "completed", "success", time.Hour),
		testRun(2, "CI", "in_progress", "", 2*time.Hour),
		testRun(3, "CI", "completed", "failure", 30*time.Minute),
	)
	if !s.isGrouped() {
		t.Fatal("recent view with a live run should group")
	}
	var order []int64
	var labels []string
	for i := range s.Len() {
		order = append(order, s.runAt(i).ID)
		labels = append(labels, s.groupLabelAt(i))
	}
	if !slices.Equal(order, []int64{2, 3, 1}) || !slices.Equal(labels, []string{"Active", "Recent", "Recent"}) {
		t.Errorf("order %v labels %v", order, labels)
	}

	if flat := runSection("recent", testRun(1, "CI", "completed", "success", time.Hour)); flat.isGrouped() {
		t.Error("no live run: not grouped")
	}
	active := runSection("active", testRun(2, "CI", "in_progress", "", time.Hour))
	if active.isGrouped() {
		t.Error("active state is flat")
	}
}

func TestRunMatchQuery(t *testing.T) {
	a := testRun(1, "CI", "completed", "success", time.Hour)
	a.HeadBranch, a.Actor, a.Event = "main", "alice", "push"
	b := testRun(2, "Deploy", "completed", "failure", 2*time.Hour)
	b.HeadBranch, b.Actor, b.Event = "feat/x", "bob", "schedule"
	s := runSection("recent", a, b)
	ids := func(q string) []int64 {
		var out []int64
		for _, ix := range s.matchQuery(q) {
			out = append(out, s.runs[ix].ID)
		}
		return out
	}
	for _, tc := range []struct {
		query     string
		want      []int64
		forceFlat bool
	}{
		{"branch:main", []int64{1}, false},
		{"workflow:depl", []int64{2}, false},
		{"event:sched", []int64{2}, false},
		{"actor:ALI", []int64{1}, false},
		{"@bob", []int64{2}, false},
		{"branch:feat @bob", []int64{2}, false},
		{"deploy", []int64{2}, true},
		{"foo:bar", nil, true},
		{"", []int64{1, 2}, false},
	} {
		if got := ids(tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.query, got, tc.want)
		}
		if s.forceFlat != tc.forceFlat {
			t.Errorf("%q: forceFlat %v, want %v", tc.query, s.forceFlat, tc.forceFlat)
		}
	}
}

func TestRunStateFiltersAndPins(t *testing.T) {
	running := testRun(1, "CI", "in_progress", "", time.Minute)
	failed := testRun(2, "CI", "completed", "failure", time.Hour)
	ok := testRun(3, "CI", "completed", "success", 2*time.Hour)
	s := runSection("active", running, failed, ok)
	count := func() int { return len(s.matchQuery("")) }

	if got := count(); got != 1 {
		t.Fatalf("active shows %d rows, want 1", got)
	}
	// The run finishes in a later poll; it stays because the view pinned it.
	finished := running
	finished.Status, finished.Conclusion = "completed", "success"
	s.SetRuns([]gh.WorkflowRun{finished, failed, ok}, nil)
	if got := count(); got != 1 {
		t.Fatalf("pinned run should survive finishing, got %d rows", got)
	}
	s.resetView()
	if got := count(); got != 0 {
		t.Fatalf("resetView should drop the pin, got %d rows", got)
	}

	s.SetState("failed")
	if got := s.matchQuery(""); len(got) != 1 || s.runs[got[0]].ID != 2 {
		t.Fatalf("failed view got %v", got)
	}
}

func TestRunSectionVars(t *testing.T) {
	r := testRun(9, "CI", "completed", "success", time.Hour)
	s := runSection("recent", r)
	v := s.VarsAt(0)
	if v.Number != 9 || v.Title != r.DisplayTitle || v.Author != "octocat" || v.URL != r.HTMLURL ||
		v.HeadRefName != r.HeadBranch || v.Branch != r.HeadBranch {
		t.Errorf("vars = %+v", v)
	}
}

func TestRunSectionIsNotNumbered(t *testing.T) {
	var s Section = NewRunSection("", "recent")
	if _, ok := s.(numbered); ok {
		t.Error("RunSection must not implement numbered")
	}
}

func TestRunStatusGlyph(t *testing.T) {
	for _, tc := range []struct{ status, conclusion, want string }{
		{"in_progress", "", ciRunningGlyph},
		{"queued", "", runQueuedGlyph},
		{"completed", "success", "✓"},
		{"completed", "timed_out", "✗"},
		{"completed", "cancelled", runCancelledGlyph},
		{"completed", "skipped", "·"},
	} {
		if got := ansi.Strip(runStatusGlyph(tc.status, tc.conclusion)); got != tc.want {
			t.Errorf("%s/%s = %q, want %q", tc.status, tc.conclusion, got, tc.want)
		}
	}
}

func TestThemeRunAccent(t *testing.T) {
	if Mocha().Run != "#89b4fa" || Latte().Run != "#1e66f5" {
		t.Errorf("run accent: mocha %q latte %q", Mocha().Run, Latte().Run)
	}
	t.Cleanup(func() { applyTheme(Mocha()) })
	mocha := runAccentStyle.Render("x")
	applyTheme(Latte())
	if runAccentStyle.Render("x") == mocha {
		t.Error("runAccentStyle must follow the theme")
	}
}
