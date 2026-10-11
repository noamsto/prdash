package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// boardLayer is an OPEN, clean PR on board stack 7 at pos.
func boardLayer(n, pos int) gh.PR {
	return gh.PR{
		Number: n, ID: "node", State: "OPEN", Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN",
		Stack: &gh.PRStack{Number: 7, Size: 4}, StackPosition: pos,
	}
}

// stackMergeModel paints #141 and #142 (layers 2 and 3 of a four-layer stack
// whose bottom, #140, is off the board) with the cursor on #142.
func stackMergeModel(t *testing.T) (Model, *fakeMutationSource, *fakeStackSource, *fakeStackDetails) {
	t.Helper()
	m, fs := mutationModel(t, []gh.PR{boardLayer(142, 3), boardLayer(141, 2)})
	stacks := &fakeStackSource{found: true, st: gh.Stack{BaseRefName: "main", Members: openMembers(140, 141, 142, 143)}}
	details := &fakeStackDetails{ret: map[int]gh.PRDetail{140: cleanDetail()}}
	m.SetStackSource(stacks)
	m.SetDetailSource(details)
	cursorOnPR(t, &m, 142)
	return m, fs, stacks, details
}

func cursorOnPR(t *testing.T, m *Model, n int) {
	t.Helper()
	ps := m.section.(*PRSection)
	for i := range ps.Len() {
		if ps.prAt(i).Number == n {
			m.cursor = i
			return
		}
	}
	t.Fatalf("PR #%d not on the board", n)
}

// update feeds msg through Update.
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// head runs the first command of cmd's batch: the load or the MergeStack call,
// never the spinner tick batched behind it.
func head(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, c := range batch {
		if c != nil {
			return c()
		}
	}
	t.Fatal("empty batch")
	return nil
}

func pressS(t *testing.T, m Model) (Model, tea.Msg) {
	t.Helper()
	m, cmd := update(m, keyMsg("S"))
	if m.stackLoad == nil || !m.actionRunning() {
		t.Fatalf("S should start a load with a running badge: stackLoad=%v status=%+v", m.stackLoad, m.actionStatus)
	}
	msg, ok := head(t, cmd).(stackPlanLoadedMsg)
	if !ok {
		t.Fatalf("load cmd produced %T, want stackPlanLoadedMsg", msg)
	}
	return m, msg
}

// promptedStackMerge presses S and lands the load, leaving the prompt open.
func promptedStackMerge(t *testing.T) (Model, *fakeMutationSource, *fakeStackSource, *fakeStackDetails) {
	t.Helper()
	m, fs, stacks, details := stackMergeModel(t)
	m, load := pressS(t, m)
	m, _ = update(m, load)
	if m.pending == nil || m.pendingStackMerge == nil {
		t.Fatalf("the landed load should open the prompt: pending=%v plan=%v", m.pending, m.pendingStackMerge)
	}
	return m, fs, stacks, details
}

func TestStackMergeRefusesNonStackedPR(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 7, ID: "n7", State: "OPEN", Mergeable: "MERGEABLE"}})
	stacks := &fakeStackSource{}
	m.SetStackSource(stacks)

	m, _ = update(m, keyMsg("S"))

	if m.actionStatus == nil || m.actionStatus.fail != "PR #7 is not in a stack" {
		t.Fatalf("status = %+v, want the not-in-a-stack hint", m.actionStatus)
	}
	if stacks.calls != 0 || len(fs.stackMergeCalls) != 0 {
		t.Errorf("stack calls = %d, merge calls = %v; want none", stacks.calls, fs.stackMergeCalls)
	}
	if m.pending != nil || m.stackLoad != nil {
		t.Errorf("no load or prompt may start: pending=%v stackLoad=%v", m.pending, m.stackLoad)
	}
}

func TestStackMergeMiddleLayerPromptsWithOffBoardLayer(t *testing.T) {
	m, _, stacks, details := promptedStackMerge(t)

	if got, want := m.confirmQuestion(), "Merge stack up to #142 (3 PRs: #140 #141 #142)?"; got != want {
		t.Errorf("confirmQuestion = %q, want %q", got, want)
	}
	if len(details.calls) != 1 || !slices.Equal(details.calls[0], []int{140}) {
		t.Errorf("FetchDetails calls = %v, want one call for the off-board #140", details.calls)
	}
	if stacks.calls != 1 {
		t.Errorf("FetchStackForPR calls = %d, want 1", stacks.calls)
	}
	if m.actionRunning() || m.stackLoad != nil {
		t.Errorf("the loading badge must clear once the prompt opens: status=%+v", m.actionStatus)
	}
}

func TestStackMergeConfirmMergesEveryLayer(t *testing.T) {
	m, fs, _, _ := promptedStackMerge(t)
	fs.stackMergeResult = gh.StackMergeResult{Status: gh.StackMergeMerged}

	m, cmd := update(m, keyMsg("y"))
	if m.stackMerge == nil || m.pending != nil || m.pendingStackMerge != nil {
		t.Fatalf("y should start the run and consume the prompt: run=%v pending=%v plan=%v", m.stackMerge, m.pending, m.pendingStackMerge)
	}
	m, cmd = update(m, head(t, cmd))
	if !slices.Equal(fs.stackMergeCalls, []stackMergeCall{{number: 142}}) {
		t.Fatalf("MergeStack calls = %+v, want exactly one MergeStack(142, false)", fs.stackMergeCalls)
	}
	done, ok := cmd().(actionDoneMsg)
	if !ok {
		t.Fatal("a merged answer should settle")
	}
	var merged []int
	for _, p := range done.merged {
		merged = append(merged, p.Number)
	}
	if !done.stackMerge || !slices.Equal(merged, []int{140, 141, 142}) {
		t.Fatalf("settle = stackMerge %v merged %v, want true [140 141 142]", done.stackMerge, merged)
	}

	m, _ = update(m, done)
	for _, n := range []int{140, 141, 142} {
		if _, ok := m.sessionMerged[n]; !ok {
			t.Errorf("PR #%d not marked session-merged", n)
		}
	}
	if m.stackMerge != nil {
		t.Error("the run's own settle must clear m.stackMerge")
	}
	if m.actionStatus == nil || m.actionStatus.ok != "Stack merged ×3" || m.actionStatus.err != nil {
		t.Errorf("status = %+v, want settled Stack merged ×3", m.actionStatus)
	}
}

func TestStackMergeDeclineMergesNothing(t *testing.T) {
	m, fs, _, _ := promptedStackMerge(t)

	m, cmd := update(m, keyMsg("n"))

	if cmd != nil || len(fs.stackMergeCalls) != 0 {
		t.Errorf("n must not merge: cmd=%v calls=%v", cmd, fs.stackMergeCalls)
	}
	if m.pending != nil || m.pendingStackMerge != nil || m.stackMerge != nil {
		t.Errorf("n must drop the plan: pending=%v plan=%v run=%v", m.pending, m.pendingStackMerge, m.stackMerge)
	}
}

func TestStackMergeConflictedLayerRefusesWithoutPrompt(t *testing.T) {
	m, fs, _, details := stackMergeModel(t)
	details.ret[140] = gh.PRDetail{Mergeable: "CONFLICTING", MergeStateStatus: "DIRTY"}

	m, load := pressS(t, m)
	m, _ = update(m, load)

	if m.pending != nil || m.pendingStackMerge != nil {
		t.Fatalf("a refused plan must not prompt: pending=%v plan=%v", m.pending, m.pendingStackMerge)
	}
	if m.actionStatus == nil || m.actionStatus.fail != "PR #140 has conflicts" || !m.actionStatus.settled {
		t.Errorf("status = %+v, want settled PR #140 has conflicts", m.actionStatus)
	}
	if len(fs.stackMergeCalls) != 0 {
		t.Errorf("MergeStack calls = %v, want none", fs.stackMergeCalls)
	}
}

func TestStackMergePollIgnoresForeignSettleAndStaleBeats(t *testing.T) {
	m, fs, _, _ := promptedStackMerge(t)
	fs.stackMergeResult = gh.StackMergeResult{Status: gh.StackMergePending, UUID: "u1"}

	m, cmd := update(m, keyMsg("y"))
	m, _ = update(m, head(t, cmd)) // pending: the run now waits on a poll tick
	run := m.stackMerge
	if run == nil || run.uuid != "u1" {
		t.Fatalf("a pending answer should keep the run polling: run=%+v", run)
	}

	m, _ = update(m, actionDoneMsg{})
	if m.stackMerge != run {
		t.Fatal("a foreign actionDoneMsg must not clear the stack-merge run")
	}

	m, cmd = update(m, stackMergePolledMsg{id: run.id + 1, res: gh.StackMergeResult{Status: gh.StackMergeMerged}})
	if cmd != nil || m.stackMerge != run || run.beats != 0 {
		t.Fatalf("a stale-id beat must be ignored: cmd=%v run=%v beats=%d", cmd, m.stackMerge, run.beats)
	}

	fs.stackPollResults = []gh.StackMergeResult{{Status: gh.StackMergeMerged}}
	m, cmd = update(m, stackMergeProbeMsg{id: run.id})
	m, cmd = update(m, cmd())
	if !slices.Equal(fs.stackPollCalls, []stackPollCall{{number: 142, uuid: "u1"}}) {
		t.Errorf("StackMergeStatus calls = %+v, want one for #142 u1", fs.stackPollCalls)
	}
	done, ok := cmd().(actionDoneMsg)
	if !ok || !done.stackMerge || len(done.merged) != 3 {
		t.Fatalf("the polled merge should settle with every layer: %+v", done)
	}
	m, _ = update(m, done)
	if m.stackMerge != nil {
		t.Error("the run's own settle must clear it")
	}
}

func TestStackMergeLoadDroppedWhileLegendOpen(t *testing.T) {
	m, _, _, _ := stackMergeModel(t)
	m, load := pressS(t, m)
	m, _ = update(m, keyMsg("?"))

	m, _ = update(m, load)

	if m.pending != nil || m.pendingStackMerge != nil {
		t.Fatalf("a load landing over the legend must not prompt: pending=%v", m.pending)
	}
	if m.actionRunning() || m.actionStatus == nil || m.actionStatus.fail != "Stack merge cancelled — press S again" {
		t.Errorf("status = %+v, want the settled cancel hint", m.actionStatus)
	}
}

func TestStackMergeLoadDroppedWhileFiltering(t *testing.T) {
	m, _, _, _ := stackMergeModel(t)
	m, load := pressS(t, m)
	m, _ = update(m, keyMsg("/"))

	m, _ = update(m, load)

	if m.pending != nil || m.pendingStackMerge != nil {
		t.Fatalf("a load landing over the focused filter must not prompt: pending=%v", m.pending)
	}
	if m.actionStatus == nil || m.actionStatus.fail != "Stack merge cancelled — press S again" {
		t.Errorf("status = %+v, want the cancel hint", m.actionStatus)
	}
}

func TestStackMergeSecondPressWhileLiveIsIgnored(t *testing.T) {
	m, _, _, _ := stackMergeModel(t)
	m, _ = pressS(t, m)
	seq := m.stackLoadSeq
	m, cmd := update(m, keyMsg("S"))
	if cmd != nil || m.stackLoadSeq != seq {
		t.Fatalf("S during a load must not start a second one: cmd=%v seq=%d→%d", cmd, seq, m.stackLoadSeq)
	}

	m, fs, stacks, _ := promptedStackMerge(t)
	fs.stackMergeResult = gh.StackMergeResult{Status: gh.StackMergePending, UUID: "u1"}
	m, cmd = update(m, keyMsg("y"))
	m, _ = update(m, head(t, cmd))
	badge, run := m.actionStatus, m.stackMerge.stat.run

	m, cmd = update(m, keyMsg("S"))

	if cmd != nil || stacks.calls != 1 {
		t.Errorf("S during a run must not load again: cmd=%v FetchStackForPR calls=%d", cmd, stacks.calls)
	}
	if m.actionStatus != badge || badge.run != run || badge.settled {
		t.Errorf("the run's badge must be untouched: %+v", m.actionStatus)
	}
}
