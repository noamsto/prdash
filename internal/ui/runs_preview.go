package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// lineSpan is the body lines one job occupies in the preview.
type lineSpan struct{ top, bottom int }

func (m Model) cursorRun() (*RunSection, gh.WorkflowRun, bool) {
	rs, ok := m.section.(*RunSection)
	if !ok || m.cursor < 0 || m.cursor >= rs.Len() {
		return nil, gh.WorkflowRun{}, false
	}
	return rs, rs.runAt(m.cursor), true
}

// runPreviewParts is the side pane for the focused run: the pinned head, then
// the jobs. A pane narrower than the model (the fixed narrow width) is clamped
// to the terminal so no line outruns the box.
func (m Model) runPreviewParts(rs *RunSection, w, bw int) (head, body string) {
	w = min(w, m.width-2)
	bw = min(bw, w-2)
	run := rs.runAt(m.cursor)
	head = m.runHead(rs, run, bw)
	body, _ = m.runPreviewBody(rs, run, w, bw)
	return head, body
}

// runClock is the time to age run's live jobs against: zero for a held row,
// which is frozen and would otherwise show an ever-growing elapsed time.
func (m Model) runClock(rs *RunSection, run gh.WorkflowRun) time.Time {
	if rs.isHeld(run.ID) {
		return time.Time{}
	}
	return m.clock()
}

func (m Model) runHead(rs *RunSection, run gh.WorkflowRun, bw int) string {
	glyph := runStatusGlyph(run.Status, run.Conclusion)
	num := fmt.Sprintf(" #%d", run.RunNumber)
	name := truncate(run.Name, bw-1-len(num)-1)
	lines := []string{
		glyph + " " + headerStyle.Render(name) + dimStyle.Render(num),
		titleStyle.Render(truncate(run.DisplayTitle, bw)),
	}

	timing, timingSt := ageString(run.UpdatedAt), dimStyle
	if now := m.runClock(rs, run); rs.live(run) && !now.IsZero() {
		start := run.RunStartedAt
		if start.IsZero() {
			start = run.CreatedAt
		}
		timing, timingSt = fmtElapsed(now.Sub(start)), pendStyle
	}
	var parts []string
	for _, p := range []string{run.HeadBranch, runEventLabel(run.Event), run.Actor, run.HeadSHA[:min(7, len(run.HeadSHA))]} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	sfx := " · " + timing
	meta := truncate(strings.Join(parts, " · "), bw-lipgloss.Width(sfx))
	lines = append(lines, dimStyle.Render(meta)+dimStyle.Render(" · ")+timingSt.Render(timing))
	return strings.Join(lines, "\n")
}

// runPreviewBody is the jobs section plus, per job, the lines it occupies so
// focus changes can scroll it into view.
func (m Model) runPreviewBody(rs *RunSection, run gh.WorkflowRun, w, bw int) (string, []lineSpan) {
	entry, ok := m.runJobs[run.ID]
	switch {
	case !ok:
		return dimStyle.Render("  loading jobs…"), []lineSpan{}
	case entry.err != nil && len(entry.jobs) == 0:
		return failStyle.Render("  " + truncate("jobs failed: "+entry.err.Error(), bw-2)), []lineSpan{}
	case len(entry.jobs) == 0:
		return dimStyle.Render("  no jobs yet"), []lineSpan{}
	}

	now := m.runClock(rs, run)
	focus := m.focusedRunJob(run, entry.jobs)
	done := 0
	for _, j := range entry.jobs {
		if j.Status == "completed" {
			done++
		}
	}
	lines := []string{sectionHeader(checksGlyph, fmt.Sprintf("jobs  %d/%d done", done, max(entry.total, len(entry.jobs))), w)}
	spans := make([]lineSpan, len(entry.jobs))
	for i, j := range entry.jobs {
		gutter, st := "  ", titleStyle
		if i == focus {
			gutter, st = focusBarStyle.Render(focusBarGlyph)+" ", st.Bold(true)
		}
		label, timing := timedLabel(j.Name, checkTiming(j.Check(), now), bw-4)
		spans[i].top = len(lines)
		if i == 0 {
			spans[i].top = 0 // the first job brings the section header back into view
		}
		lines = append(lines, gutter+runStatusGlyph(j.Status, j.Conclusion)+" "+st.Render(label)+timing)
		if j.Status == "in_progress" {
			if step := currentStep(j); step != "" {
				lines = append(lines, "    "+dimStyle.Render(truncate("↳ "+step, bw-4)))
			}
		}
		spans[i].bottom = len(lines) - 1
	}
	if entry.total > len(entry.jobs) {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  … and %d more on GitHub", entry.total-len(entry.jobs))))
	}
	if entry.err != nil {
		lines = append(lines, failStyle.Render("  "+truncate("jobs refresh failed: "+entry.err.Error(), bw-2)))
	}
	return strings.Join(lines, "\n"), spans
}

func currentStep(j gh.RunJob) string {
	for _, s := range j.Steps {
		if s.Status == "in_progress" {
			return s.Name
		}
	}
	return ""
}

// focusedRunJob is the index of the focused job: the stored one while it is
// still listed, else the first failed, else the first running, else the first.
// -1 when there are no jobs.
func (m Model) focusedRunJob(run gh.WorkflowRun, jobs []gh.RunJob) int {
	if len(jobs) == 0 {
		return -1
	}
	if m.runFocusRun == run.ID {
		for i, j := range jobs {
			if j.ID == m.runFocusJob {
				return i
			}
		}
	}
	for i, j := range jobs {
		if j.Status == "completed" && j.Conclusion == "failure" {
			return i
		}
	}
	for i, j := range jobs {
		if j.Status == "in_progress" {
			return i
		}
	}
	return 0
}

// moveRunJobFocus moves the focused job and scrolls it into the pane. It is a
// no-op while no preview is visible: focus must not move out of sight.
func (m *Model) moveRunJobFocus(delta int) {
	if !m.previewShown() {
		return
	}
	_, run, ok := m.cursorRun()
	if !ok {
		return
	}
	jobs := m.runJobs[run.ID].jobs
	cur := m.focusedRunJob(run, jobs)
	if cur < 0 {
		return
	}
	next := min(max(cur+delta, 0), len(jobs)-1)
	m.runFocusRun, m.runFocusJob = run.ID, jobs[next].ID
	m.scrollRunFocusIntoView()
}

func (m Model) previewShown() bool { return m.layout().ShowSide || m.previewMax }

// scrollRunFocusIntoView scrolls the focused job into the pane. It does nothing
// unless a run is under the cursor, the preview is visible and its jobs are
// loaded.
func (m *Model) scrollRunFocusIntoView() {
	if !m.previewShown() {
		return
	}
	rs, run, ok := m.cursorRun()
	if !ok {
		return
	}
	focus := m.focusedRunJob(run, m.runJobs[run.ID].jobs)
	if focus < 0 {
		return
	}
	head, _ := m.previewParts()
	w := min(m.previewWidth(), m.width-2)
	_, spans := m.runPreviewBody(rs, run, w, w-2)
	span, rows := spans[focus], m.previewBodyRows(head)
	switch {
	case span.top < m.previewOffset:
		m.previewScrollBy(span.top - m.previewOffset)
	case span.bottom >= m.previewOffset+rows:
		m.previewScrollBy(span.bottom - (m.previewOffset + rows) + 1)
	}
}

// runPreviewKey is what decides whether the Runs preview needs reconciling.
type runPreviewKey struct {
	run, job    int64 // run under the cursor and its focused job
	jobsRev     int
	shown       bool
	rows, width int
}

// runPreviewState is the key last seen plus the focused job's span under it.
type runPreviewState struct {
	runPreviewKey
	span    lineSpan
	hasSpan bool
}

// reconcileRunPreview keeps the focused job visible whenever something other
// than a deliberate alt+j/k scroll changes the preview: a different run or
// focused job reveals the focus; any other change (geometry, jobs content)
// reveals it unless it was already out of view, so a deliberate scroll away
// survives. It skips while no run is under the cursor and keeps the old key,
// so filtering back to the same run reuses its offset on purpose.
func (m *Model) reconcileRunPreview() {
	if m.mode != "run" {
		m.runPreviewSeen = runPreviewState{}
		return
	}
	rs, run, ok := m.cursorRun()
	if !ok {
		return
	}
	w := min(m.previewWidth(), m.width-2)
	key := runPreviewKey{
		run:     run.ID,
		jobsRev: m.runJobsRev,
		shown:   m.previewShown(),
		rows:    m.previewBodyRows(m.runHead(rs, run, w-2)),
		width:   w,
	}
	jobs := m.runJobs[run.ID].jobs
	focus := m.focusedRunJob(run, jobs)
	if focus >= 0 {
		key.job = jobs[focus].ID
	}
	prev := m.runPreviewSeen
	if key == prev.runPreviewKey {
		return
	}

	switch {
	case key.run != prev.run:
		m.previewOffset = 0
		m.scrollRunFocusIntoView()
	case key.job != prev.job:
		m.scrollRunFocusIntoView()
	default:
		// prev.rows is the previous geometry's; previewOffset is current (alt+j/k
		// may have moved it since). Spans do not depend on the offset.
		wasOutside := prev.shown && prev.hasSpan &&
			(prev.span.top < m.previewOffset || prev.span.bottom >= m.previewOffset+prev.rows)
		if wasOutside {
			m.previewScrollBy(0)
		} else {
			m.scrollRunFocusIntoView()
		}
	}

	state := runPreviewState{runPreviewKey: key}
	if focus >= 0 {
		_, spans := m.runPreviewBody(rs, run, w, w-2)
		state.span, state.hasSpan = spans[focus], true
	}
	m.runPreviewSeen = state
}

// rerunRunCmd reruns the cursor run's failed jobs, refusing a run that has
// nothing to rerun or whose state is stale.
func (m *Model) rerunRunCmd(a action.Action) tea.Cmd {
	rs, run, ok := m.cursorRun()
	if !ok {
		return nil
	}
	switch {
	case rs.isHeld(run.ID):
		return m.hint("Run is no longer on this board — ctrl+r")
	case run.Active():
		return m.hint("Run is still in progress")
	case !runRerunnable(run):
		return m.hint("No failed jobs to rerun")
	}
	src, id := m.actionsSource, run.ID
	m.actionStatus = statFor(a)
	m.actionStatus.refresh = a.Refresh
	m.actionStatus.rerunCI = true
	if m.cache != nil {
		m.cache.Invalidate(runsKey(m.repo))
	}
	return tea.Batch(func() tea.Msg {
		return actionDoneMsg{err: src.RerunFailedJobs(id)}
	}, m.startSpinner())
}

func runRerunnable(r gh.WorkflowRun) bool {
	switch r.Conclusion {
	case "failure", "timed_out", "cancelled", "startup_failure":
		return true
	}
	return false
}

// openRunJobLog opens the log of the focused job, whole for a job that did not
// fail and failed steps only for one that did.
func (m *Model) openRunJobLog() tea.Cmd {
	_, run, ok := m.cursorRun()
	if !ok {
		return nil
	}
	jobs := m.runJobs[run.ID].jobs
	i := m.focusedRunJob(run, jobs)
	if i < 0 {
		return m.hint("Jobs aren't loaded yet")
	}
	job := jobs[i]
	if job.Status != "completed" {
		return m.hint("Job hasn't finished — GitHub serves logs only for finished jobs")
	}
	return m.openJobLog(strconv.FormatInt(job.ID, 10), job.Name, job.Conclusion != "failure")
}
