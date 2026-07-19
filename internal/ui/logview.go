package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// logStep is one Actions step's log: its name, whether it's a failed step
// (true for every step in --log-failed output), and its content lines.
type logStep struct {
	name   string
	failed bool
	lines  []string
}

// parseJobLog turns `gh run view --log[-failed]` output into ordered steps.
// The output is tab-delimited as job⇥step⇥<timestamp> <content>; we group by the
// step column, preserving first-seen order, and strip the leading timestamp.
func parseJobLog(raw []byte, failedOnly bool) []logStep {
	var steps []logStep
	idx := map[string]int{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		step, content := "", line
		if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 {
			step = parts[1]
			content = stripTimestamp(parts[2])
		}
		i, seen := idx[step]
		if !seen {
			i = len(steps)
			idx[step] = i
			steps = append(steps, logStep{name: step, failed: failedOnly})
		}
		steps[i].lines = append(steps[i].lines, content)
	}
	return steps
}

// stripTimestamp drops gh's leading RFC3339 timestamp ("2024-01-02T03:04:05Z ")
// from a log line, leaving the message. Lines without one are returned as-is.
func stripTimestamp(s string) string {
	i := strings.IndexByte(s, ' ')
	if i < 20 || s[4] != '-' || s[10] != 'T' {
		return s
	}
	return s[i+1:]
}

// logLine is one rendered/navigable line: either a step header or a content
// line. step indexes into the []logStep it came from (headers included), so copy
// can target the whole step from any line within it.
type logLine struct {
	text   string
	step   int
	header bool
}

func flattenLog(steps []logStep) []logLine {
	var out []logLine
	for i, s := range steps {
		out = append(out, logLine{text: s.name, step: i, header: true})
		for _, ln := range s.lines {
			out = append(out, logLine{text: ln, step: i})
		}
	}
	return out
}

type lineKind int

const (
	kindPlain lineKind = iota
	kindError
	kindPass
)

// classifyLogLine buckets a content line so the renderer can color it. Errors
// win over passes when a line somehow matches both.
func classifyLogLine(text string) lineKind {
	l := strings.ToLower(text)
	switch {
	case strings.Contains(l, "error") || strings.Contains(l, "fail") || strings.Contains(text, "✗"):
		return kindError
	case strings.Contains(l, "pass") || strings.Contains(text, "✓") || strings.HasPrefix(l, "ok"):
		return kindPass
	default:
		return kindPlain
	}
}

func copyLine(lines []logLine, cursor int) string {
	if cursor < 0 || cursor >= len(lines) {
		return ""
	}
	return lines[cursor].text
}

func copyStep(steps []logStep, lines []logLine, cursor int) string {
	if cursor < 0 || cursor >= len(lines) {
		return ""
	}
	s := steps[lines[cursor].step]
	return s.name + "\n" + strings.Join(s.lines, "\n")
}

func copyWhole(steps []logStep) string {
	var parts []string
	for _, s := range steps {
		parts = append(parts, s.name+"\n"+strings.Join(s.lines, "\n"))
	}
	return strings.Join(parts, "\n")
}

func logCacheKey(job string, all bool) string { return fmt.Sprintf("%s|%t", job, all) }

// hoveredCheck returns the check under the Checks-tab cursor.
func (m Model) hoveredCheck() (gh.Check, bool) {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return gh.Check{}, false
	}
	checks := ps.prAt(m.cursor).Checks()
	if m.checkCursor < 0 || m.checkCursor >= len(checks) {
		return gh.Check{}, false
	}
	return checks[m.checkCursor], true
}

// enterLogView opens the log sub-view for the hovered check. A cached log paints
// instantly; otherwise it kicks an async fetch. External (StatusContext) checks
// have no job log — Task 7 upgrades the notice below to open the browser.
func (m Model) enterLogView() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok {
		return m, nil
	}
	if c.IsExternal() { // StatusContext: no job log (Task 7 opens the browser here)
		m.actionStatus = &actionStat{fail: "external check — no job logs", settled: true,
			err: fmt.Errorf("external check %q has no job logs", c.Label())}
		return m, clearStatusCmd()
	}
	m.logView = true
	m.logJobID = c.JobID()
	m.logLabel = c.Label()
	m.logCursor = 0
	m.logShowAll = false
	m.logErr = nil
	if m.logJobID == "" { // Actions check with no job assigned yet (pending/queued)
		m.logLoading = false
		m.setLogSteps(nil) // renders "No logs."
		return m, nil
	}
	if steps, hit := m.logCache[logCacheKey(m.logJobID, false)]; hit {
		m.logLoading = false
		m.setLogSteps(steps)
		return m, nil
	}
	m.logLoading = true
	m.logSteps, m.logLines = nil, nil
	m.setLogContent()
	return m, tea.Batch(m.fetchJobLogCmd(m.logJobID, false), m.startSpinner())
}

// fetchJobLogCmd fetches a job log off the UI thread and reports it back.
func (m Model) fetchJobLogCmd(job string, all bool) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		out, err := action.JobLog(r, dir, job, !all)
		return logFetchedMsg{job: job, all: all, raw: out, err: err}
	}
}

// setLogSteps swaps in freshly parsed steps, clamps the cursor, and re-renders.
func (m *Model) setLogSteps(steps []logStep) {
	m.logSteps = steps
	m.logLines = flattenLog(steps)
	if m.logCursor >= len(m.logLines) {
		m.logCursor = max(0, len(m.logLines)-1)
	}
	m.setLogContent()
}

// replaced in Task 5 (render)
func (m *Model) setLogContent() {}
