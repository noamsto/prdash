package ui

import (
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// runJobsEntry is one run's last jobs fetch. runUpdated is zero after a failure
// so a finished run retries on the next settle.
type runJobsEntry struct {
	jobs       []gh.RunJob
	total      int
	runUpdated time.Time
	err        error
}

const (
	runsListLimit = 50
	runsSchemaVer = "v1"
	// runHeldMax bounds the held rows: the per-second elapsed repaint walks the
	// whole list, so a long session on a busy repo must not grow it forever.
	runHeldMax = 100

	runPollActive = 20 * time.Second
	runPollIdle   = 60 * time.Second
	runPollSlow   = 2 * time.Minute
	runPollQuiet  = 10 * time.Minute
)

func runsKey(repo string) string {
	return cache.Key("runs", repo, runsListLimit, runsSchemaVer)
}

func (m *Model) cachedRuns() ([]gh.WorkflowRun, bool) {
	e, ok := m.cache.Get(runsKey(m.repo))
	if !ok {
		return []gh.WorkflowRun{}, false
	}
	var runs []gh.WorkflowRun
	if err := json.Unmarshal(e.Rows, &runs); err != nil {
		slog.Debug("runs cache unmarshal failed", "err", err)
		return []gh.WorkflowRun{}, false
	}
	return runs, true
}

// runsFetchCmd lists the repo's runs. A requested caller bumps m.runsFetchSeq
// first, so an in-flight poll beat lands stale.
func (m Model) runsFetchCmd(replace, poll bool) tea.Cmd {
	src, seq := m.runsSource, m.runsFetchSeq
	return func() tea.Msg {
		runs, raw, err := src.ListRuns(runsListLimit)
		if err != nil {
			return runsFailedMsg{err: err, poll: poll, seq: seq}
		}
		return runsFetchedMsg{runs: runs, raw: raw, replace: replace, poll: poll, seq: seq}
	}
}

func (m Model) runJobsFetchCmd(run gh.WorkflowRun) tea.Cmd {
	src, id, updated := m.runsSource, run.ID, run.UpdatedAt
	return func() tea.Msg {
		jobs, total, err := src.ListRunJobs(id)
		return runJobsMsg{runID: id, jobs: jobs, total: total, runUpdated: updated, err: err}
	}
}

// runJobsCmdForCursor refetches the focused run's jobs unless they are known
// current: a finished run whose UpdatedAt has not moved. A held run is frozen.
func (m *Model) runJobsCmdForCursor() tea.Cmd {
	rs, ok := m.section.(*RunSection)
	if !ok || m.cursor < 0 || m.cursor >= rs.Len() {
		return nil
	}
	run := rs.runAt(m.cursor)
	if rs.isHeld(run.ID) {
		return nil
	}
	if e, ok := m.runJobs[run.ID]; ok && e.runUpdated.Equal(run.UpdatedAt) && !run.Active() {
		return nil
	}
	return m.runJobsFetchCmd(run)
}

// mergeHeldRuns returns fetched plus every previous run it omitted, and the set
// of those carried rows. Past runHeldMax the oldest carried rows go, never the
// row under the cursor.
func mergeHeldRuns(prev, fetched []gh.WorkflowRun, anchor int64) ([]gh.WorkflowRun, map[int64]bool) {
	have := make(map[int64]bool, len(fetched))
	for _, r := range fetched {
		have[r.ID] = true
	}
	omitted := slices.DeleteFunc(slices.Clone(prev), func(r gh.WorkflowRun) bool { return have[r.ID] })
	slices.SortStableFunc(omitted, func(a, b gh.WorkflowRun) int { return b.CreatedAt.Compare(a.CreatedAt) })
	for i := len(omitted) - 1; i >= 0 && len(omitted) > runHeldMax; i-- {
		if omitted[i].ID != anchor {
			omitted = slices.Delete(omitted, i, i+1)
		}
	}
	held := make(map[int64]bool, len(omitted))
	for _, r := range omitted {
		held[r.ID] = true
	}
	return append(slices.Clone(fetched), omitted...), held
}

// paintRuns applies a fetched list. An unrequested paint carries omitted rows
// as held; a requested one replaces them. The cursor follows its run ID.
func (m *Model) paintRuns(runs []gh.WorkflowRun, replace bool) {
	rs, ok := m.section.(*RunSection)
	if !ok {
		return
	}
	order := rs.shownIDs()
	var anchor int64
	if !(m.cursor == 0 && m.cursorPinnedTop) && m.cursor >= 0 && m.cursor < len(order) {
		anchor = order[m.cursor]
	}
	var held map[int64]bool
	if replace {
		rs.resetView()
		m.cursorPinnedTop = false
	} else {
		runs, held = mergeHeldRuns(rs.runs, runs, anchor)
	}
	rs.SetState(m.state)
	rs.SetRuns(runs, held)
	m.applyFilter()
	m.restoreRunCursor(rs, anchor, order)
}

// restoreRunCursor is restoreCursor keyed by run ID: back on anchor, or else on
// its nearest surviving neighbour in the previous order, preferring the row
// below.
func (m *Model) restoreRunCursor(rs *RunSection, anchor int64, order []int64) {
	l := rs.Len()
	if l == 0 {
		m.cursor = 0
		return
	}
	shown := make(map[int64]int, l)
	for i, id := range rs.shownIDs() {
		shown[id] = i
	}
	if i, ok := shown[anchor]; anchor != 0 && ok {
		m.cursor = i
		return
	}
	if oldIdx := slices.Index(order, anchor); anchor != 0 && oldIdx >= 0 {
		for step := 1; step <= len(order); step++ {
			if oldIdx+step < len(order) {
				if i, ok := shown[order[oldIdx+step]]; ok {
					m.cursor = i
					return
				}
			}
			if oldIdx-step >= 0 {
				if i, ok := shown[order[oldIdx-step]]; ok {
					m.cursor = i
					return
				}
			}
		}
	}
	m.cursor = min(max(m.cursor, 0), l-1)
}

// cycleRunState advances recent → active → failed. The states are views of the
// fetched list, so this costs no list request.
func (m *Model) cycleRunState() tea.Cmd {
	rs, ok := m.section.(*RunSection)
	if !ok {
		return nil
	}
	m.state = nextState(m.state, runStates)
	m.filter = searchFor("run", m.state, "")
	rs.SetState(m.state)
	rs.dropHeld()
	m.cursor = 0
	m.applyFilter()
	return m.detailCmdForCursor()
}

// runRateFrac is the share of the budget left, false when there is no reading
// or its window has already rolled over.
func runRateFrac(s gh.RateSnapshot, now time.Time) (float64, bool) {
	if s.Limit == 0 || !s.Reset.After(now) {
		return 0, false
	}
	return float64(s.Remaining) / float64(s.Limit), true
}

// runPollDelay spaces the next beat. Quiet only slows the loop: it never stops
// while the board is open, because status has to update on its own.
func runPollDelay(active, quiet bool, s gh.RateSnapshot, now time.Time) time.Duration {
	d := runPollIdle
	switch {
	case active && !quiet:
		d = runPollActive
	case !active && quiet:
		d = runPollSlow
	}
	if frac, ok := runRateFrac(s, now); ok && frac < rateWarnFrac {
		d = max(d, runPollSlow)
	}
	return d
}

func runPollPaused(s gh.RateSnapshot, now time.Time) bool {
	frac, ok := runRateFrac(s, now)
	return ok && frac < rateCritFrac
}

func (m Model) runsPollDelay() time.Duration {
	rs, ok := m.section.(*RunSection)
	active := ok && rs.anyLive()
	now := m.clock()
	quiet := !m.lastKeyAt.IsZero() && now.Sub(m.lastKeyAt) >= runPollQuiet
	return runPollDelay(active, quiet, m.rate, now)
}

func runsPollTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return runsPollMsg{} })
}

// maybeStartRunPoll arms the poll loop while on the Runs board, unless it is
// already running (one loop only). The loop retires on its first beat off the
// board.
func (m *Model) maybeStartRunPoll() tea.Cmd {
	if m.mode != "run" || m.runPolling {
		return nil
	}
	m.runPolling = true
	return runsPollTick(m.runsPollDelay())
}
