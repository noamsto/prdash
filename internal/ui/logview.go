package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
		// CI job logs carry raw ANSI SGR codes; strip them so truncation counts
		// real width and the viewer's own theme coloring isn't corrupted (a
		// truncated escape can also bleed into the surrounding lipgloss style).
		content = ansi.Strip(content)
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
	if step := lines[cursor].step; step >= 0 && step < len(steps) {
		s := steps[step]
		return s.name + "\n" + strings.Join(s.lines, "\n")
	}
	return ""
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
// have no job log, so enter opens the check's page in the browser instead.
func (m Model) enterLogView() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok {
		return m, nil
	}
	if c.IsExternal() { // StatusContext: no job log — open its page in the browser
		return m.openHoveredCheck()
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
	spin := m.startSpinner() // capture before the return copies m (persists m.spinning)
	return m, tea.Batch(m.fetchJobLogCmd(m.logJobID, false), spin)
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

// logBoxHeight is the OUTER height of the log box: the frame minus the header
// and footer (the log view has no metadata line).
func (m Model) logBoxHeight() int {
	h := m.height - 1
	if showFooter(m.width, m.height) {
		h-- // reserve the footer's row
	}
	if h < 3 {
		h = 3
	}
	return h
}

// setLogContent (re)fills the viewport with the rendered log at the current
// geometry and keeps the cursor line on screen.
func (m *Model) setLogContent() {
	w := m.expandedBoxWidth() - 2
	rows := m.logBoxHeight() - 2
	if w < 1 {
		w = 1
	}
	if rows < 1 {
		rows = 1
	}
	m.vp.SetWidth(w)
	m.vp.SetHeight(rows)
	m.vp.SetContent(m.renderLogBody(w))
	m.keepLogCursorVisible()
}

// keepLogCursorVisible scrolls the viewport so the cursor line stays in view.
// One logLine renders to exactly one display line (each is truncated to width),
// so the cursor index is its display row.
func (m *Model) keepLogCursorVisible() {
	h := m.vp.Height()
	off := m.vp.YOffset()
	switch {
	case m.logCursor < off:
		m.vp.SetYOffset(m.logCursor)
	case m.logCursor >= off+h:
		m.vp.SetYOffset(m.logCursor - h + 1)
	}
}

// renderLogBody paints the flattened log: dim step headers (red for failed
// steps), content lines colored by classifyLogLine, cursor line gutter-marked.
func (m Model) renderLogBody(w int) string {
	switch {
	case m.logLoading:
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return dimStyle.Render("  " + frame + " Loading…")
	case m.logErr != nil:
		return failStyle.Render("  " + truncate(m.logErr.Error(), w-2))
	case len(m.logLines) == 0:
		return dimStyle.Render("  No logs.")
	}
	var b strings.Builder
	for i, ln := range m.logLines {
		gutter := "  "
		if i == m.logCursor {
			gutter = focusBarStyle.Render("▎") + " "
		}
		text := truncate(ln.text, w-2)
		var styled string
		switch {
		case ln.header:
			if m.logSteps[ln.step].failed {
				styled = failStyle.Bold(true).Render(text)
			} else {
				styled = dimStyle.Render(text)
			}
		default:
			switch classifyLogLine(ln.text) {
			case kindError:
				styled = failStyle.Render(text)
			case kindPass:
				styled = passStyle.Render(text)
			default:
				styled = titleStyle.Render(text)
			}
		}
		b.WriteString(gutter + styled + "\n")
	}
	return b.String()
}

// updateLogView handles keys while the check-log sub-view is open.
func (m Model) updateLogView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "h", "left":
		m.logView = false
		m.renderExpanded() // restore the Checks tab into the viewport
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.logCursor < len(m.logLines)-1 {
			m.logCursor++
			m.setLogContent()
		}
		return m, nil
	case "k", "up":
		if m.logCursor > 0 {
			m.logCursor--
			m.setLogContent()
		}
		return m, nil
	case "a": // toggle failed-only ↔ full job log
		if m.logJobID == "" { // pending check with no job yet — nothing to fetch
			return m, nil
		}
		m.logShowAll = !m.logShowAll
		m.logCursor = 0
		m.logErr = nil
		if steps, hit := m.logCache[logCacheKey(m.logJobID, m.logShowAll)]; hit {
			m.logLoading = false
			m.setLogSteps(steps)
			return m, nil
		}
		m.logLoading = true
		m.logSteps, m.logLines = nil, nil
		m.setLogContent()
		spin := m.startSpinner() // capture before the return copies m (persists m.spinning)
		return m, tea.Batch(m.fetchJobLogCmd(m.logJobID, m.logShowAll), spin)
	case "y":
		return m.copyLogText(copyLine(m.logLines, m.logCursor), "Copied line")
	case "s":
		return m.copyLogText(copyStep(m.logSteps, m.logLines, m.logCursor), "Copied step")
	case "Y":
		return m.copyLogText(copyWhole(m.logSteps), "Copied log")
	}
	return m, nil
}

// copyLogText copies text via the native clipboard tool, falling back to OSC 52,
// mirroring runAction's copy path. Returns the mutated model so callers avoid the
// return-value evaluation-order trap.
func (m Model) copyLogText(text, ok string) (tea.Model, tea.Cmd) {
	if text == "" {
		return m, nil
	}
	if argv := clipboardArgv(); argv != nil {
		m.actionStatus = &actionStat{run: "Copying", ok: ok, fail: "Copy failed"}
		spin := m.startSpinner() // capture before the return copies m (persists m.spinning)
		return m, tea.Batch(func() tea.Msg {
			return actionDoneMsg{err: writeClipboard(argv, text)}
		}, spin)
	}
	m.actionStatus = &actionStat{ok: ok, fail: "Copy failed", settled: true}
	return m, tea.Batch(tea.SetClipboard(text), clearStatusCmd())
}

// openHoveredCheck opens the hovered check's details URL in the browser.
func (m Model) openHoveredCheck() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok {
		return m, nil
	}
	url := c.URL()
	if url == "" {
		m.actionStatus = &actionStat{fail: "no URL for this check", settled: true,
			err: fmt.Errorf("check %q has no URL", c.Label())}
		return m, clearStatusCmd()
	}
	m.actionStatus = &actionStat{run: "Opening", ok: "Opened in browser", fail: "Open failed"}
	spin := m.startSpinner() // capture before the return copies m (persists m.spinning)
	return m, tea.Batch(func() tea.Msg {
		return actionDoneMsg{err: openURL(url)}
	}, spin)
}

// copyHoveredCheckURL copies the hovered check's details URL.
func (m Model) copyHoveredCheckURL() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok || c.URL() == "" {
		return m, nil
	}
	return m.copyLogText(c.URL(), "Copied URL")
}

// logFooter is the log view's key hint line; `a` toggles the log scope.
func (m Model) logFooter() string {
	word := "all steps"
	if m.logShowAll {
		word = "failed only"
	}
	return "  j/k move · y line · s step · Y all · a " + word + " · esc back"
}

// logViewRender is the full-screen log view: header, the log framed in a titled
// box (the check label as title), and the key hint line — centered like the
// expanded view.
func (m Model) logViewRender() string {
	n := 0
	if v, ok := m.cursorVars(); ok {
		n = v.Number
	}
	bw := m.expandedBoxWidth()
	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, n))
	head += m.statusBadge()
	box := titledBox(m.vp.View(), bw, m.logBoxHeight(), m.logLabel)
	parts := []string{head, box}
	if showFooter(m.width, m.height) {
		parts = append(parts, statusBarStyle.Render(m.logFooter()))
	}
	out := strings.Join(parts, "\n")
	if bw < m.width {
		out = indentLines(out, (m.width-bw)/2)
	}
	return out
}
