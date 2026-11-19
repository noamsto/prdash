package ui

import (
	"errors"
	"fmt"
	"slices"
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

// checkStackLayers applies merge-squash's client-side checks, plus a draft
// check, to every layer, bottom first, refusing with the first offender. fresh readings override the
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
	repo       string // the stack's repo; target and layers are numbered within it
	target     int
	layers     []gh.PR // bottom to top, merged layers already dropped
	mergeQueue bool
}

func memberNumbers(members []gh.StackMember) []int {
	nums := make([]int, len(members))
	for i, mem := range members {
		nums[i] = mem.Number
	}
	return nums
}

func (p *stackMergePlan) numbers() []int {
	nums := make([]int, len(p.layers))
	for i, l := range p.layers {
		nums[i] = l.Number
	}
	return nums
}

func (p *stackMergePlan) refs() []gh.PRRef {
	refs := make([]gh.PRRef, len(p.layers))
	for i, l := range p.layers {
		refs[i] = gh.PRRef{Repo: p.repo, Number: l.Number}
	}
	return refs
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
// pending", as does a layer the refetch answers without.
func loadStackPlan(t gh.PRRef, board map[int]stackBoardRow, stacks gh.StackSource, details gh.DetailSource) (*stackMergePlan, error) {
	st, ok, err := stacks.FetchStackForPR(t)
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
		refs := make([]gh.PRRef, len(need))
		for i, n := range need {
			refs[i] = gh.PRRef{Repo: t.Repo, Number: n}
		}
		got, _, err := details.FetchDetails(refs)
		if err != nil {
			return nil, fmt.Errorf("%s: fetch layer state: %w", stackMergeFailedText, err)
		}
		fresh = make(map[int]gh.PRDetail, len(got))
		for r, d := range got {
			fresh[r.Number] = d
		}
	}
	layers, err := checkStackLayers(members, board, fresh)
	if err != nil {
		return nil, err
	}
	for i := range layers {
		layers[i].Repo = t.Repo // an off-board layer is built from its member number alone
	}

	// Detection failure refuses rather than guessing: a direct merge onto a base
	// that really has a queue would bypass it.
	queue, err := stacks.BaseUsesMergeQueue(t.Repo, st.BaseRefName)
	if err != nil {
		return nil, fmt.Errorf("%s: couldn't check the base branch's merge queue: %w", stackMergeFailedText, err)
	}
	return &stackMergePlan{repo: t.Repo, target: t.Number, layers: layers, mergeQueue: queue}, nil
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
	var changed stackChangedError
	if errors.As(err, &changed) {
		return r.failText(changed.Error())
	}
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

// resolve maps one submit or probe answer to the next step. Any non-terminal
// status, pending or unrecognised, polls on the uuid it carries or the one
// already held; with neither there is nothing to poll, so it never started.
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
	}
	if res.UUID != "" {
		r.uuid = res.UUID
	}
	if r.uuid == "" {
		return r.failText(stackMergeNoStartText)
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

// stackChangedError is the submit refusing a plan whose stack membership moved
// since the prompt; it settles as written, not as a failed merge.
type stackChangedError struct{ key string }

func (e stackChangedError) Error() string {
	return fmt.Sprintf("Stack changed since the prompt — press %s again", e.key)
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
		return clearStatusCmd(m.actionStatus)
	}
	if m.stackSource == nil {
		return m.hint("Stack merge unavailable")
	}

	// Stack members are numbered within one repo, so the board names only t's.
	board := make(map[int]stackBoardRow, ps.Len())
	for i := range ps.Len() {
		p := ps.prAt(i)
		if ps.isFoldStop(i) || p.Repo != t.Repo {
			continue
		}
		d, hasDetail := m.detail[p.Ref()]
		mergeable, mss := mergeState(p, d, hasDetail)
		board[p.Number] = stackBoardRow{pr: p, mergeable: mergeable, mss: mss, mutableErr: m.mutable(p)}
	}
	m.stackLoadSeq++
	m.stackLoad = &actionStat{run: "Loading stack"}
	m.actionStatus = m.stackLoad
	seq, keys, stacks, details, ref := m.stackLoadSeq, m.keyPresses, m.stackSource, m.detailSource, t.Ref()
	return tea.Batch(func() tea.Msg {
		plan, err := loadStackPlan(ref, board, stacks, details)
		return stackPlanLoadedMsg{seq: seq, keys: keys, a: a, plan: plan, err: err}
	}, m.startSpinner())
}

// settleStackLoad replaces the loading badge directly, since statusHint is a
// no-op while it runs, and only while it is still the one showing.
func (m *Model) settleStackLoad(load *actionStat, text string) tea.Cmd {
	if m.actionStatus != load {
		return nil
	}
	m.actionStatus = &actionStat{err: errors.New(text), fail: text, settled: true}
	return clearStatusCmd(m.actionStatus)
}

// runStackMerge submits a confirmed plan. The run owns its stat for the reason
// cascadeRun.stat gives; refs and refresh cover every layer. The membership is
// re-read first: the prompt may have sat open while the stack changed, and the
// server merges whatever is below target now, not what the prompt named.
func (m *Model) runStackMerge(a action.Action, plan *stackMergePlan) tea.Cmd {
	nums := plan.numbers()
	stat := statForBulk(a, len(plan.layers))
	stat.refresh = true
	stat.refs = plan.refs()
	m.stackRunID++
	m.stackMerge = &stackMergeRun{id: m.stackRunID, plan: plan, stat: stat}
	m.actionStatus = stat
	m.invalidateLaunchCache(stat.refs...)
	m.clearSelection() // the run consumes the selection, exactly as runBulkNative does
	id, src, stacks, target, queue := m.stackRunID, m.mutationSource, m.stackSource, plan.target, plan.mergeQueue
	ref := gh.PRRef{Repo: plan.repo, Number: target}
	return tea.Batch(func() tea.Msg {
		st, ok, err := stacks.FetchStackForPR(ref)
		if err != nil {
			return stackMergeSubmittedMsg{id: id, err: err}
		}
		members, err := stackRange(target, st)
		if !ok || err != nil || !slices.Equal(memberNumbers(members), nums) {
			return stackMergeSubmittedMsg{id: id, err: stackChangedError{key: a.Key}}
		}
		res, err := src.MergeStack(ref, queue)
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
		done.stat = r.stat
		return func() tea.Msg { return done }
	}
}

// stackMergeProbeCmd fires the beat's single StackMergeStatus call.
func (m *Model) stackMergeProbeCmd() tea.Cmd {
	r := m.stackMerge
	id, src, ref, uuid := r.id, m.mutationSource, gh.PRRef{Repo: r.plan.repo, Number: r.plan.target}, r.uuid
	return func() tea.Msg {
		res, err := src.StackMergeStatus(ref, uuid)
		return stackMergePolledMsg{id: id, res: res, err: err}
	}
}
