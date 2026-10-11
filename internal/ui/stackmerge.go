package ui

import (
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// stackBoardRow is one loaded-board PR as the stack-merge load needs it,
// snapshotted on the Update goroutine so the async load never reads the Model.
type stackBoardRow struct {
	pr             gh.PR
	mergeable, mss string // mergeState(pr, m.detail[n], ok)
	mutableErr     error  // m.mutable(pr)
}

// stackRange is the membership from the bottom up to and including target,
// located by number so a renumbering or a merged-layer offset can never pick
// the wrong PR. Merged layers need nothing and are dropped.
func stackRange(target int, st gh.Stack) ([]gh.StackMember, error) {
	for i, mem := range st.Members {
		if mem.Number != target {
			continue
		}
		if mem.State != "OPEN" {
			return nil, fmt.Errorf("PR #%d is not open", target)
		}
		var out []gh.StackMember
		for _, below := range st.Members[:i+1] {
			if below.State != "MERGED" {
				out = append(out, below)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("PR #%d is no longer in this stack — ctrl+r to refresh", target)
}

// stackNeedsDetail lists the layers whose mergeability the board cannot vouch
// for: off the board, or on it with an unresolved reading.
func stackNeedsDetail(members []gh.StackMember, board map[int]stackBoardRow) []int {
	var need []int
	for _, mem := range members {
		row, ok := board[mem.Number]
		if !ok || mergeUnresolved(row.mergeable) {
			need = append(need, mem.Number)
		}
	}
	return need
}

// checkStackLayers applies merge-squash's client-side checks to every layer,
// bottom first, refusing with the first offender. fresh readings override the
// board's (the merge-squash refetch rule); a layer with no reading at all is
// unresolved, never clean.
func checkStackLayers(members []gh.StackMember, board map[int]stackBoardRow, fresh map[int]gh.PRDetail) ([]gh.PR, error) {
	layers := make([]gh.PR, 0, len(members))
	for _, mem := range members {
		row, onBoard := board[mem.Number]
		if mem.State == "CLOSED" {
			return nil, fmt.Errorf("PR #%d is not open", mem.Number)
		}
		if onBoard && row.mutableErr != nil {
			return nil, row.mutableErr
		}
		if mem.IsDraft {
			return nil, fmt.Errorf("PR #%d is a draft", mem.Number)
		}

		var mergeable, mss string
		if onBoard {
			mergeable, mss = row.mergeable, row.mss
		}
		if d, ok := fresh[mem.Number]; ok {
			mergeable, mss = d.Mergeable, d.MergeStateStatus
		}
		if mergeConflicted(mergeable, mss) {
			return nil, fmt.Errorf("PR #%d has conflicts", mem.Number)
		}
		if mergeUnresolved(mergeable) {
			return nil, fmt.Errorf("PR #%d merge state is still pending — try again shortly", mem.Number)
		}

		if onBoard {
			layers = append(layers, row.pr)
			continue
		}
		layers = append(layers, gh.PR{Number: mem.Number, State: mem.State, IsDraft: mem.IsDraft})
	}
	return layers, nil
}

// stackMergePlan is the snapshot a confirmed prompt promises: later refreshes
// cannot change what it names or what is reported merged.
type stackMergePlan struct {
	target     int
	layers     []gh.PR // bottom to top, merged layers already dropped
	mergeQueue bool
}

func (p *stackMergePlan) numbers() []int {
	nums := make([]int, len(p.layers))
	for i, l := range p.layers {
		nums[i] = l.Number
	}
	return nums
}

func (p *stackMergePlan) question() string {
	noun := "PRs"
	if len(p.layers) == 1 {
		noun = "PR"
	}
	return fmt.Sprintf("Merge stack up to #%d (%d %s: %s)?", p.target, len(p.layers), noun, numList(p.numbers()))
}

// loadStackPlan is the off-goroutine body of the S press: it reads only its
// arguments. details may be nil; layers needing a reading then fall to "still
// pending", as they do when the refetch fails.
func loadStackPlan(t gh.PR, board map[int]stackBoardRow, stacks gh.StackSource, details gh.DetailSource) (*stackMergePlan, error) {
	st, ok, err := stacks.FetchStackForPR(t.Number)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", stackMergeFailedText, err)
	}
	if !ok {
		return nil, fmt.Errorf("PR #%d is no longer in a stack — ctrl+r to refresh", t.Number)
	}
	members, err := stackRange(t.Number, st)
	if err != nil {
		return nil, err
	}

	var fresh map[int]gh.PRDetail
	if need := stackNeedsDetail(members, board); len(need) > 0 && details != nil {
		// An error leaves fresh empty: unknown is never clean.
		fresh, _, _ = details.FetchDetails(need)
	}
	layers, err := checkStackLayers(members, board, fresh)
	if err != nil {
		return nil, err
	}

	// Detection failure falls back to a direct merge, as gh-stack does; a base
	// that really has a queue makes the server reject it and that is the settle.
	queue, err := stacks.BaseUsesMergeQueue(st.BaseRefName)
	return &stackMergePlan{target: t.Number, layers: layers, mergeQueue: queue && err == nil}, nil
}

const (
	stackMergeFailedText  = "Stack merge failed"
	stackMergeRunningText = "Stack merge still running on GitHub — check the PRs"
	stackMergeNoStartText = "Stack merge did not start"
)

// Polling is bounded by a beat count rather than a deadline, so a test drives
// it by feeding results with no clock.
const (
	stackMergePollEvery = 2 * time.Second
	stackMergePollBeats = 90
)

// stackMergeStep is what the run decided to do next.
type stackMergeStep int

const (
	stackMergePoll   stackMergeStep = iota // probe the merge again after stackMergePollEvery
	stackMergeSettle                       // the run is over; done holds the settle
)

// stackMergeRun drives one confirmed plan from submit to settle.
type stackMergeRun struct {
	id      int // matches beat msgs to this run
	plan    *stackMergePlan
	uuid    string
	beats   int
	lastErr error // the latest beat's probe error, nil when that beat read fine

	// stat is the run's own badge state; see cascadeRun.stat for why it is not
	// read back from m.actionStatus.
	stat *actionStat
	done actionDoneMsg // the settle, valid once the step is stackMergeSettle
}

func (r *stackMergeRun) onSubmitted(res gh.StackMergeResult, err error) stackMergeStep {
	if err != nil {
		return r.fail(fmt.Errorf("%s: %w", stackMergeFailedText, err))
	}
	return r.resolve(res)
}

func (r *stackMergeRun) onPolled(res gh.StackMergeResult, err error) stackMergeStep {
	r.beats++
	r.lastErr = err
	step := stackMergePoll
	if err == nil {
		step = r.resolve(res)
	}
	if step != stackMergePoll || r.beats < stackMergePollBeats {
		return step
	}
	if err != nil {
		return r.fail(fmt.Errorf("%s (last check: %w)", stackMergeRunningText, err))
	}
	return r.failText(stackMergeRunningText)
}

// resolve maps one submit or probe answer to the next step. An unknown status
// keeps polling: unknown is not done.
func (r *stackMergeRun) resolve(res gh.StackMergeResult) stackMergeStep {
	n := len(r.plan.layers)
	switch res.Status {
	case gh.StackMergeMerged:
		r.done = actionDoneMsg{stackMerge: true, merged: r.plan.layers, ok: fmt.Sprintf("Stack merged ×%d", n)}
		return stackMergeSettle
	case gh.StackMergeEnqueued:
		r.done = actionDoneMsg{stackMerge: true, ok: fmt.Sprintf("Stack queued ×%d", n)}
		return stackMergeSettle
	case gh.StackMergeFailed:
		return r.fail(fmt.Errorf("%s: %s — nothing merged", stackMergeFailedText, res.Message))
	case gh.StackMergePending:
		if res.UUID == "" {
			return r.failText(stackMergeNoStartText)
		}
		r.uuid = res.UUID
	}
	return stackMergePoll
}

func (r *stackMergeRun) fail(err error) stackMergeStep {
	r.done = actionDoneMsg{stackMerge: true, err: err, fail: err.Error()}
	return stackMergeSettle
}

// failText settles on a badge text shown as written.
func (r *stackMergeRun) failText(text string) stackMergeStep {
	r.done = actionDoneMsg{stackMerge: true, err: errors.New(text), fail: text}
	return stackMergeSettle
}

// startStackMerge is the S press: a refusal settles at once, otherwise the plan
// loads off the Update goroutine and opens the prompt when it lands.
func (m *Model) startStackMerge(a action.Action) tea.Cmd {
	// Silent, like the live-cascade guard: any status written here would steal
	// the live load's or run's badge.
	if m.stackLoad != nil || m.pendingStackMerge != nil || m.stackMerge != nil {
		return nil
	}
	ps, ok := m.section.(*PRSection)
	if !ok || ps.Len() == 0 {
		return nil
	}
	t := ps.prAt(m.cursor)
	if t.Stack == nil {
		return m.hint(fmt.Sprintf("PR #%d is not in a stack", t.Number))
	}
	if err := m.mutable(t); err != nil {
		m.actionStatus = &actionStat{fail: err.Error(), err: err, settled: true}
		return clearStatusCmd()
	}
	if m.stackSource == nil {
		return m.hint("Stack merge unavailable")
	}

	board := make(map[int]stackBoardRow, ps.Len())
	for i := range ps.Len() {
		p := ps.prAt(i)
		d, hasDetail := m.detail[p.Number]
		mergeable, mss := mergeState(p, d, hasDetail)
		board[p.Number] = stackBoardRow{pr: p, mergeable: mergeable, mss: mss, mutableErr: m.mutable(p)}
	}
	m.stackLoadSeq++
	m.stackLoad = &actionStat{run: "Loading stack"}
	m.actionStatus = m.stackLoad
	seq, stacks, details := m.stackLoadSeq, m.stackSource, m.detailSource
	return tea.Batch(func() tea.Msg {
		plan, err := loadStackPlan(t, board, stacks, details)
		return stackPlanLoadedMsg{seq: seq, a: a, plan: plan, err: err}
	}, m.startSpinner())
}

// settleStackLoad replaces the loading badge directly, since statusHint is a
// no-op while it runs, and only while it is still the one showing.
func (m *Model) settleStackLoad(load *actionStat, text string) tea.Cmd {
	if m.actionStatus != load {
		return nil
	}
	m.actionStatus = &actionStat{err: errors.New(text), fail: text, settled: true}
	return clearStatusCmd()
}

// runStackMerge submits a confirmed plan. The run owns its stat for the reason
// cascadeRun.stat gives; nums and refresh cover every layer.
func (m *Model) runStackMerge(a action.Action, plan *stackMergePlan) tea.Cmd {
	nums := plan.numbers()
	stat := statForBulk(a, len(plan.layers))
	stat.refresh = true
	stat.nums = nums
	m.stackRunID++
	m.stackMerge = &stackMergeRun{id: m.stackRunID, plan: plan, stat: stat}
	m.actionStatus = stat
	m.invalidateLaunchCache(nums...)
	m.clearSelection() // the run consumes the selection, exactly as runBulkNative does
	id, src, target, queue := m.stackRunID, m.mutationSource, plan.target, plan.mergeQueue
	return tea.Batch(func() tea.Msg {
		res, err := src.MergeStack(target, queue)
		return stackMergeSubmittedMsg{id: id, res: res, err: err}
	}, m.startSpinner())
}

// stackMergeDispatch carries the run's decision forward: the next poll beat,
// or the settle. The settle re-asserts the run's stat for the reason
// cascadeSettleCmd does.
func (m *Model) stackMergeDispatch(step stackMergeStep) tea.Cmd {
	r := m.stackMerge
	switch step {
	case stackMergePoll:
		r.stat.run = fmt.Sprintf("Merging stack ×%d · %d/%d", len(r.plan.layers), r.beats+1, stackMergePollBeats)
		id := r.id
		return tea.Tick(stackMergePollEvery, func(time.Time) tea.Msg { return stackMergeProbeMsg{id: id} })
	default: // stackMergeSettle
		m.actionStatus = r.stat
		done := r.done
		return func() tea.Msg { return done }
	}
}

// stackMergeProbeCmd fires the beat's single StackMergeStatus call.
func (m *Model) stackMergeProbeCmd() tea.Cmd {
	r := m.stackMerge
	id, src, target, uuid := r.id, m.mutationSource, r.plan.target, r.uuid
	return func() tea.Msg {
		res, err := src.StackMergeStatus(target, uuid)
		return stackMergePolledMsg{id: id, res: res, err: err}
	}
}
