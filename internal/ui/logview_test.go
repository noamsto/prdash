package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

func TestStripTimestamp(t *testing.T) {
	in := "2024-01-02T03:04:05.1234567Z hello world"
	if got := stripTimestamp(in); got != "hello world" {
		t.Fatalf("stripTimestamp = %q", got)
	}
	if got := stripTimestamp("no timestamp here"); got != "no timestamp here" {
		t.Fatalf("non-timestamp line altered: %q", got)
	}
}

func TestParseJobLogGroupsByStep(t *testing.T) {
	raw := []byte(
		"build\tRun tests\t2024-01-02T03:04:05.0Z FAIL foo_test.go:42\n" +
			"build\tRun tests\t2024-01-02T03:04:06.0Z expected 3 got 4\n" +
			"build\tSet up job\t2024-01-02T03:04:00.0Z Requested labels\n")
	steps := parseJobLog(raw, true)
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].name != "Run tests" || !steps[0].failed {
		t.Fatalf("step 0 = %+v", steps[0])
	}
	wantLines := []string{"FAIL foo_test.go:42", "expected 3 got 4"}
	if !reflect.DeepEqual(steps[0].lines, wantLines) {
		t.Fatalf("step 0 lines = %v, want %v", steps[0].lines, wantLines)
	}
	if steps[1].name != "Set up job" {
		t.Fatalf("step 1 name = %q", steps[1].name)
	}
}

func TestParseJobLogFullNotFailed(t *testing.T) {
	raw := []byte("build\tSet up job\t2024-01-02T03:04:00.0Z ok\n")
	if steps := parseJobLog(raw, false); steps[0].failed {
		t.Fatal("full-log steps should not be marked failed")
	}
}

func TestFlattenLog(t *testing.T) {
	steps := []logStep{
		{name: "A", lines: []string{"a1", "a2"}},
		{name: "B", lines: []string{"b1"}},
	}
	lines := flattenLog(steps)
	// header A, a1, a2, header B, b1
	if len(lines) != 5 {
		t.Fatalf("want 5 flat lines, got %d", len(lines))
	}
	if !lines[0].header || lines[0].text != "A" || lines[0].step != 0 {
		t.Fatalf("line 0 = %+v", lines[0])
	}
	if lines[2].header || lines[2].text != "a2" || lines[2].step != 0 {
		t.Fatalf("line 2 = %+v", lines[2])
	}
	if lines[4].step != 1 || lines[4].text != "b1" {
		t.Fatalf("line 4 = %+v", lines[4])
	}
}

func TestClassifyLogLine(t *testing.T) {
	cases := map[string]lineKind{
		"FAIL foo_test.go":  kindError,
		"an error occurred": kindError,
		"all tests passed":  kindPass,
		"ok  	pkg  0.1s":    kindPass,
		"compiling main.go": kindPlain,
	}
	for in, want := range cases {
		if got := classifyLogLine(in); got != want {
			t.Errorf("classifyLogLine(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCopyHelpers(t *testing.T) {
	steps := []logStep{{name: "Run tests", lines: []string{"x", "y"}}}
	lines := flattenLog(steps) // [header, x, y]
	if got := copyLine(lines, 1); got != "x" {
		t.Fatalf("copyLine = %q", got)
	}
	if got := copyStep(steps, lines, 2); got != "Run tests\nx\ny" {
		t.Fatalf("copyStep = %q", got)
	}
	if got := copyWhole(steps); got != "Run tests\nx\ny" {
		t.Fatalf("copyWhole = %q", got)
	}
	if copyLine(lines, 99) != "" {
		t.Fatal("out-of-range copyLine should be empty")
	}
}

func logViewModel(t *testing.T) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{})
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "test", DetailsUrl: "https://github.com/x/actions/runs/1/job/99"},
	}}})
	m.expanded = true
	m.expandedTab = 2 // Checks
	m.checkCursor = 0
	m.renderExpanded()
	return m
}

func TestEnterOpensLogView(t *testing.T) {
	m := logViewModel(t)
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if !m.logView {
		t.Fatal("enter on a check with a job ID should open the log view")
	}
	if m.logJobID != "99" || !m.logLoading {
		t.Fatalf("logJobID=%q loading=%v", m.logJobID, m.logLoading)
	}
	if cmd == nil {
		t.Fatal("expected a fetch command")
	}
}

func TestLogFetchedPopulatesSteps(t *testing.T) {
	m := logViewModel(t)
	u, _ := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	raw := []byte("build\tRun tests\t2024-01-02T03:04:05Z FAIL x\n")
	u, _ = m.Update(logFetchedMsg{job: "99", all: false, raw: raw})
	m = u.(Model)
	if m.logLoading {
		t.Fatal("loading should clear on fetch")
	}
	if len(m.logSteps) != 1 || m.logSteps[0].name != "Run tests" {
		t.Fatalf("steps = %+v", m.logSteps)
	}
	if _, ok := m.logCache[logCacheKey("99", false)]; !ok {
		t.Fatal("fetched log should be cached")
	}
}

func TestRenderLogBody(t *testing.T) {
	m := logViewModel(t)
	m.logSteps = []logStep{{name: "Run tests", failed: true, lines: []string{"FAIL foo", "ok done"}}}
	m.logLines = flattenLog(m.logSteps)
	m.logCursor = 1 // "FAIL foo"
	out := m.renderLogBody(80)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "Run tests") || !strings.Contains(plain, "FAIL foo") {
		t.Fatalf("body missing content: %q", plain)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[1], "▎") { // cursor gutter on the focused line
		t.Fatalf("cursor gutter not on focused line: %q", lines)
	}
}

func TestRenderLogBodyLoadingAndEmpty(t *testing.T) {
	m := logViewModel(t)
	m.logLoading = true
	if !strings.Contains(ansi.Strip(m.renderLogBody(80)), "Loading") {
		t.Fatal("loading state should say Loading")
	}
	m.logLoading = false
	m.logSteps, m.logLines = nil, nil
	if !strings.Contains(ansi.Strip(m.renderLogBody(80)), "No logs") {
		t.Fatal("empty state should say No logs")
	}
}

func TestLogViewRenderDispatch(t *testing.T) {
	m := logViewModel(t)
	m.logView = true
	m.logLabel = "test (ubuntu)"
	m.logSteps = []logStep{{name: "Run tests", lines: []string{"x"}}}
	m.logLines = flattenLog(m.logSteps)
	m.setLogContent()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "test (ubuntu)") {
		t.Fatalf("log box title (check label) missing: %q", out)
	}
}
