package ui

import (
	"errors"
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

func TestParseJobLogStripsANSI(t *testing.T) {
	// CI output often embeds SGR color codes; the viewer applies its own theme,
	// so the stored line must be plain text (a leaked escape corrupts rendering).
	raw := []byte("build\tRun tests\t2024-01-02T03:04:05Z \x1b[31mFAIL\x1b[0m foo_test.go\n")
	steps := parseJobLog(raw, true)
	if len(steps) != 1 || len(steps[0].lines) != 1 {
		t.Fatalf("steps = %+v", steps)
	}
	if got := steps[0].lines[0]; got != "FAIL foo_test.go" {
		t.Fatalf("ANSI not stripped: %q", got)
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
	m.SetActionsSource(&fakeActionsSource{})
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "test", DetailsUrl: "https://github.com/x/actions/runs/1/job/99"},
	}}})
	m.expanded = true
	m.expandedTab = tabChecks
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

// TestLogFetchedStaleIgnored pins the async staleness guard: a late fetch for a
// different job or the wrong log variant must not clobber the active viewer.
func TestLogFetchedStaleIgnored(t *testing.T) {
	m := logViewModel(t)
	u, _ := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model) // log view open for job "99", loading

	// A fetch for a different job is dropped.
	u, _ = m.Update(logFetchedMsg{job: "77", all: false, raw: []byte("build\tS\t2024-01-02T03:04:05Z x\n")})
	m = u.(Model)
	if len(m.logSteps) != 0 || !m.logLoading {
		t.Fatalf("stale job fetch applied: steps=%+v loading=%v", m.logSteps, m.logLoading)
	}
	if _, ok := m.logCache[logCacheKey("77", false)]; ok {
		t.Fatal("stale job fetch should not populate the cache")
	}

	// A fetch for the wrong variant (full log while showing failed-only) is dropped.
	u, _ = m.Update(logFetchedMsg{job: "99", all: true, raw: []byte("build\tS\t2024-01-02T03:04:05Z y\n")})
	if s := u.(Model).logSteps; len(s) != 0 {
		t.Fatalf("wrong-variant fetch applied: %+v", s)
	}

	// The matching fetch populates.
	u, _ = m.Update(logFetchedMsg{job: "99", all: false, raw: []byte("build\tRun\t2024-01-02T03:04:05Z z\n")})
	if s := u.(Model).logSteps; len(s) != 1 {
		t.Fatalf("matching fetch should populate, got %+v", s)
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

// TestLogBodyCacheInvalidatesOnContentChange guards the styled-line cache: new
// log content (via setLogSteps) must not serve stale cached lines.
func TestLogBodyCacheInvalidatesOnContentChange(t *testing.T) {
	m := logViewModel(t)
	m.setLogSteps([]logStep{{name: "step", lines: []string{"alpha-line"}}})
	if !strings.Contains(ansi.Strip(m.renderLogBody(80)), "alpha-line") {
		t.Fatal("first render missing alpha-line")
	}
	m.setLogSteps([]logStep{{name: "step", lines: []string{"bravo-line"}}})
	out := ansi.Strip(m.renderLogBody(80))
	if strings.Contains(out, "alpha-line") {
		t.Error("stale cached log line survived a content change")
	}
	if !strings.Contains(out, "bravo-line") {
		t.Error("new log line not rendered after content change")
	}
}

// TestLogBodyCursorMoveKeepsLines: moving the cursor reuses cached lines but
// still emits every line (only the gutter shifts).
func TestLogBodyCursorMoveKeepsLines(t *testing.T) {
	m := logViewModel(t)
	m.setLogSteps([]logStep{{name: "step", lines: []string{"line-one", "line-two", "line-three"}}})
	_ = m.renderLogBody(80) // populate the cache at cursor 0
	m.logCursor = len(m.logLines) - 1
	out := ansi.Strip(m.renderLogBody(80))
	for _, want := range []string{"line-one", "line-two", "line-three"} {
		if !strings.Contains(out, want) {
			t.Errorf("log line %q missing after cursor move", want)
		}
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

func TestRenderLogBodyErrorTruncates(t *testing.T) {
	m := logViewModel(t)
	m.logErr = errors.New(strings.Repeat("x", 300)) // long gh stderr
	out := strings.TrimRight(m.renderLogBody(20), "\n")
	if w := ansi.StringWidth(out); w > 20 {
		t.Fatalf("error line width %d exceeds box width 20: %q", w, ansi.Strip(out))
	}
}

func TestCopyStepOutOfRange(t *testing.T) {
	// A line whose step index is beyond the steps slice must copy "" (like a
	// stale steps/lines pairing) rather than panic.
	steps := []logStep{{name: "A", lines: []string{"x"}}}
	lines := []logLine{{text: "x", step: 5}}
	if got := copyStep(steps, lines, 0); got != "" {
		t.Fatalf("out-of-range step should copy empty, got %q", got)
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

func loadedLogModel(t *testing.T) Model {
	t.Helper()
	m := logViewModel(t)
	m.logView = true
	m.logJobID = "99"
	m.logSteps = []logStep{{name: "Run tests", lines: []string{"x", "y"}}}
	m.logLines = flattenLog(m.logSteps) // [header, x, y]
	m.setLogContent()
	return m
}

// TestLogViewSurvivesBackgroundRefresh guards the regression where a background
// poll completing while the log view is open repainted the PR list into the
// shared viewport, bleeding list rows into the log box.
func TestLogViewSurvivesBackgroundRefresh(t *testing.T) {
	m := loadedLogModel(t)
	u, _ := m.Update(prsFetchedMsg{filter: "", prs: []gh.PR{
		{Number: 7, Title: "some other pr"},
		{Number: 8, Title: "and another"},
	}})
	m = u.(Model)
	if body := ansi.Strip(m.render()); !strings.Contains(body, "Run tests") {
		t.Fatalf("log content lost after background refresh: %q", body)
	}
}

// TestLogViewSurvivesResize guards the regression where a WindowSizeMsg while
// the log view is open reflowed the Checks tab under it instead of the log.
func TestLogViewSurvivesResize(t *testing.T) {
	m := loadedLogModel(t)
	for _, w := range []int{40, 200, 60} {
		u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		m = u.(Model)
		if got, want := m.vp.Width(), max(1, m.expandedBoxWidth()-2); got != want {
			t.Fatalf("width %d: viewport width %d, want %d", w, got, want)
		}
		if body := ansi.Strip(m.render()); !strings.Contains(body, "Run tests") {
			t.Fatalf("log content lost after resize to %d: %q", w, body)
		}
	}
}

func TestLogViewCursorMoves(t *testing.T) {
	m := loadedLogModel(t)
	u, _ := m.updateLogView(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	if m.logCursor != 1 {
		t.Fatalf("j should move cursor to 1, got %d", m.logCursor)
	}
	u, _ = m.updateLogView(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if u.(Model).logCursor != 0 {
		t.Fatal("k should move cursor back to 0")
	}
}

func TestLogViewEscClosesView(t *testing.T) {
	m := loadedLogModel(t)
	u, _ := m.updateLogView(tea.KeyPressMsg{Code: tea.KeyEscape})
	if u.(Model).logView {
		t.Fatal("esc should close the log view")
	}
}

func TestLogViewToggleRefetches(t *testing.T) {
	m := loadedLogModel(t)
	u, cmd := m.updateLogView(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = u.(Model)
	if !m.logShowAll || !m.logLoading || cmd == nil {
		t.Fatalf("a should switch to full log and refetch: all=%v loading=%v", m.logShowAll, m.logLoading)
	}
}

func TestLogViewCopyStep(t *testing.T) {
	m := loadedLogModel(t)
	m.logCursor = 1 // "x", in step "Run tests"
	u, _ := m.updateLogView(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.actionStatus == nil {
		t.Fatal("s should set a copy status")
	}
}

func TestCheckTabOpenSetsStatus(t *testing.T) {
	m := logViewModel(t) // hovered check has a DetailsUrl
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = u.(Model)
	if m.actionStatus == nil || cmd == nil {
		t.Fatal("o on the Checks tab should open the check URL and set a status")
	}
	if m.logView {
		t.Fatal("o should not enter the log view")
	}
}

func TestCheckTabCopyURL(t *testing.T) {
	m := logViewModel(t)
	u, _ := m.updateExpanded(tea.KeyPressMsg{Code: 'Y', Text: "Y"})
	if u.(Model).actionStatus == nil {
		t.Fatal("Y on the Checks tab should copy the check URL")
	}
}

func TestChecksFooterShowsLogKeys(t *testing.T) {
	m := logViewModel(t)
	foot := m.expandedFooter()
	for _, want := range []string{"logs", "open", "rerun"} {
		if !strings.Contains(foot, want) {
			t.Fatalf("Checks footer missing %q: %q", want, foot)
		}
	}
}

func TestExternalCheckEnterOpensBrowser(t *testing.T) {
	m := logViewModel(t)
	// Replace the check with an external one (no /job/ in the URL → JobID "").
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Context: "ci/ext", DetailsUrl: "https://ci.example.com/build/7"},
	}}})
	m.expanded, m.expandedTab, m.checkCursor = true, tabChecks, 0
	m.renderExpanded()
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if m.logView {
		t.Fatal("external check has no job logs; enter should not open the log view")
	}
	if m.actionStatus == nil || cmd == nil {
		t.Fatal("external check enter should open the browser with a status")
	}
}

// TestExternalCheckTargetURLOpens covers the real StatusContext shape: the URL
// arrives as targetUrl (not detailsUrl), so enter/o/Y must still find it.
func TestExternalCheckTargetURLOpens(t *testing.T) {
	m := logViewModel(t)
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Context: "ci/ext", TargetUrl: "https://ci.example.com/build/7"},
	}}})
	m.expanded, m.expandedTab, m.checkCursor = true, tabChecks, 0
	m.renderExpanded()
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if m.logView {
		t.Fatal("external check should not open the log view")
	}
	if m.actionStatus == nil || cmd == nil {
		t.Fatal("external check with targetUrl should open the browser")
	}
	if m.actionStatus.err != nil {
		t.Fatalf("expected an open status, not a no-URL error: %v", m.actionStatus.err)
	}
}

func TestLogViewHidesFooterOnSmallWindow(t *testing.T) {
	m := logViewModel(t)
	m.width, m.height = 90, 14 // below footerMinHeight
	m.logView = true
	m.logLabel = "build"
	m.logSteps = []logStep{{name: "Run tests", lines: []string{"line one"}}}
	m.logLines = flattenLog(m.logSteps)
	m.setLogContent()

	out := m.logViewRender()
	if strings.Contains(out, "esc back") {
		t.Fatalf("small window should not render the log footer: %q", out)
	}
	if lines := strings.Count(out, "\n") + 1; lines > m.height {
		t.Fatalf("log view output has %d lines, exceeds terminal height %d", lines, m.height)
	}

	m.width, m.height = 90, 30
	out = m.logViewRender()
	if !strings.Contains(out, "esc back") {
		t.Fatalf("large window should render the log footer: %q", out)
	}
}

func TestLogViewLegendTogglesAndDismisses(t *testing.T) {
	m := logViewModel(t)
	m.width, m.height = 90, 14 // footer hidden; ? is the only way to see keys
	m.logView = true
	m.logLabel = "build"
	m.logSteps = []logStep{{name: "Run tests", lines: []string{"line one"}}}
	m.logLines = flattenLog(m.logSteps)
	m.setLogContent()

	u, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = u.(Model)
	if !m.showLegend {
		t.Fatal("? should open the log-view legend")
	}
	out := m.render()
	if !strings.Contains(out, "step") {
		t.Fatalf("log legend should list its own keys: %q", out)
	}

	u, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = u.(Model)
	if m.showLegend {
		t.Fatal("a key should close the log-view legend")
	}
}
