package ui

import (
	"errors"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// withStat correlates a hand-built result with the status shown now, as the
// producer of a real result would have.
func withStat(m Model, msg actionDoneMsg) actionDoneMsg {
	msg.stat = m.actionStatus
	return msg
}

// clearFor is the dwell timer of the status shown now.
func clearFor(m Model) actionClearMsg {
	return actionClearMsg{stat: m.actionStatus}
}

// pairPRs is two open, mergeable PRs, #1 and #2.
func pairPRs() []gh.PR {
	return []gh.PR{mergeablePR(1, "alice"), mergeablePR(2, "alice")}
}

// startOn runs a on PR n's row, as a press there would, and returns its command
// with the running status it put up.
func startOn(t *testing.T, m *Model, n int, a action.Action) (tea.Cmd, *actionStat) {
	t.Helper()
	i := shownIndex(*m, n)
	if i < 0 {
		t.Fatalf("PR #%d is not shown", n)
	}
	m.cursor = i
	cmd := m.runBulk(a)
	if cmd == nil || m.actionStatus == nil || m.actionStatus.settled {
		t.Fatalf("starting %q on #%d: cmd=%v status=%+v, want a running action", a.Key, n, cmd, m.actionStatus)
	}
	return cmd, m.actionStatus
}

// result runs cmd to completion and returns the actionDoneMsg it produced.
func result(t *testing.T, cmd tea.Cmd) actionDoneMsg {
	t.Helper()
	done, ok := driveBulk(t, cmd).(actionDoneMsg)
	if !ok {
		t.Fatal("the command produced no actionDoneMsg")
	}
	return done
}

func deliver(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestActionDoneSupersededLeavesRunningStatusAlone(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"failure", errors.New("boom")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, fs := mutationModel(t, pairPRs())
			cmdA, _ := startOn(t, &m, 1, action.DefaultPRActions()["u"])
			fs.err = tt.err
			msgA := result(t, cmdA)
			fs.err = nil
			_, b := startOn(t, &m, 2, action.DefaultPRActions()["L"])
			wantOK, wantFail := b.ok, b.fail

			m, _ = deliver(t, m, msgA)

			if m.actionStatus != b {
				t.Fatalf("status = %+v, want B's still shown", m.actionStatus)
			}
			if b.settled || b.err != nil || b.ok != wantOK || b.fail != wantFail {
				t.Errorf("B = %+v, want it still running with ok %q fail %q", b, wantOK, wantFail)
			}
		})
	}
}

func TestActionDoneSideEffectsUseOwnAction(t *testing.T) {
	t.Run("optimistic patch and freshness", func(t *testing.T) {
		pr1, pr2 := mergeablePR(1, "alice"), mergeablePR(2, "alice")
		pr1.IsDraft = true
		m, _ := mutationModel(t, []gh.PR{pr1, pr2})
		cmdA, _ := startOn(t, &m, 1, action.ReadyAction(true))
		msgA := result(t, cmdA)
		startOn(t, &m, 2, action.ReadyAction(false))
		m.fresh[1], m.fresh[2] = true, true

		m, _ = deliver(t, m, msgA)

		ps := m.section.(*PRSection)
		if findPR(t, ps, 1).IsDraft {
			t.Error("#1 is still a draft, want A's mark-ready patched onto it")
		}
		if findPR(t, ps, 2).IsDraft {
			t.Error("#2 became a draft, want B's rows untouched by A's result")
		}
		if m.fresh[1] {
			t.Error("#1 kept its freshness, want A's own rows invalidated")
		}
		if !m.fresh[2] {
			t.Error("#2 lost its freshness, want B's rows left alone")
		}
	})

	t.Run("CI re-run stamp", func(t *testing.T) {
		pr2 := mergeablePR(2, "alice")
		pr2.IsDraft = true
		m, _ := mutationModel(t, []gh.PR{mergeablePR(1, "alice"), pr2})
		cmdA, _ := startOn(t, &m, 1, action.DefaultPRActions()["u"])
		msgA := result(t, cmdA)
		startOn(t, &m, 2, action.ReadyAction(true))

		m, _ = deliver(t, m, msgA)

		if _, ok := m.ciRerun[1]; !ok {
			t.Error("ciRerun[1] missing, want A's update-branch stamped")
		}
		if _, ok := m.ciRerun[2]; ok {
			t.Error("ciRerun[2] set, want B's rows left alone")
		}
	})
}

func TestActionDoneSupersededMergeFailureInvalidatesOwnNums(t *testing.T) {
	fd := &fakeDetailSource{}
	m, fs := mutationModelWithDetail(t, pairPRs(), fd)
	merge := action.DefaultPRActions()["m"]
	cmdA, _ := startOn(t, &m, 1, merge)
	fs.err = errors.New("boom")
	msgA := result(t, cmdA)
	fs.err = nil
	if msgA.err == nil {
		t.Fatal("setup: A's merge should have failed")
	}
	startOn(t, &m, 2, merge)
	m.fresh[1], m.fresh[2] = true, true
	fetched := len(fd.got)

	m, cmd := deliver(t, m, msgA)
	collectMsgs(t, cmd)

	if m.fresh[1] {
		t.Error("#1 kept its freshness, want A's failed merge to invalidate it")
	}
	if !m.fresh[2] {
		t.Error("#2 lost its freshness, want B's rows left alone")
	}
	if got := fd.got[fetched:]; len(got) != 1 || !slices.Equal(got[0], []int{1}) {
		t.Errorf("detail fetches = %v, want one for [1]", got)
	}
}

func TestActionClearDwellOnlyClearsOwnStatus(t *testing.T) {
	prev := statusDwell
	statusDwell = time.Millisecond
	t.Cleanup(func() { statusDwell = prev })

	// No Refresh, so A's settle holds only its dwell timer.
	plain := action.Action{Key: "x", Label: "Mark ready", Command: action.Command{Native: "mark-ready"}, Scope: "per-selected"}
	pr1 := mergeablePR(1, "alice")
	pr1.IsDraft = true
	m, _ := mutationModel(t, []gh.PR{pr1})
	cmdA, a := startOn(t, &m, 1, plain)
	m, cmd := deliver(t, m, result(t, cmdA))
	clearA := msgOf[actionClearMsg](t, collectMsgs(t, cmd))

	cmd = m.hint("hint")
	b := m.actionStatus
	clearB := msgOf[actionClearMsg](t, collectMsgs(t, cmd))
	if clearA.stat != a || clearB.stat != b || a == b {
		t.Fatalf("setup: timers carry %p and %p, want A's %p and B's %p", clearA.stat, clearB.stat, a, b)
	}

	m, _ = deliver(t, m, clearA)
	if m.actionStatus != b {
		t.Fatalf("status = %+v after A's timer, want B's still shown", m.actionStatus)
	}
	m, _ = deliver(t, m, clearB)
	if m.actionStatus != nil {
		t.Errorf("status = %+v after B's own timer, want it cleared", m.actionStatus)
	}
}

// cascadeAtSettle drives a 3-link stack whose middle link fails to the point
// where the run's settle message is about to be delivered.
func cascadeAtSettle(t *testing.T) (Model, actionDoneMsg) {
	t.Helper()
	pr1 := stackedPR(1, 1, 3)
	pr1.ID, pr1.Mergeable, pr1.MergeStateStatus = "n1", "MERGEABLE", "BEHIND"
	pr2 := stackedPR(1, 2, 3) // no ID: the stale-cache guard fails this link
	pr3 := stackedPR(1, 3, 3)
	pr3.ID = "n3"

	m := NewModel("/repo", "is:open", nil)
	m.SetMutationSource(&fakeMutationSource{})
	m.SetDetailSource(&sequencedDetailSource{readings: []gh.PRDetail{{Mergeable: "MERGEABLE", MergeStateStatus: "BEHIND"}}})
	m.setPRs([]gh.PR{pr3, pr2, pr1})
	m.sel.toggle(0) // #1, the bottom of the stack

	if cmd := m.startBulk(m.actions["u"]); cmd != nil || !m.pendingCascade.cascades() {
		t.Fatalf("expected a stacked cascade prompt, got cmd=%v plan=%+v", cmd, m.pendingCascade)
	}
	m, done := driveCascadeToSettle(t, m, m.confirmAnswer(true))
	if done.err == nil || !slices.Equal(done.partial, []int{1}) || done.stat != m.cascade.stat {
		t.Fatalf("setup: settle = %+v, want a failed run that landed #1, carrying its own stat", done)
	}
	return m, done
}

func TestCascadeSettleForeignStatusShown(t *testing.T) {
	m, done := cascadeAtSettle(t)
	foreign := &actionStat{run: "Copying", ok: "Copied", fail: "Copy failed"}
	m.actionStatus = foreign
	m.fresh[1] = true

	m, _ = deliver(t, m, done)

	if m.cascade != nil {
		t.Error("m.cascade still set, want the settle to free the run")
	}
	if foreign.settled || foreign.err != nil {
		t.Errorf("foreign status = %+v, want it still running with no error", foreign)
	}
	if !done.stat.settled {
		t.Error("the cascade's own status is not settled")
	}
	if m.fresh[1] {
		t.Error("#1 kept its freshness, want the landed link invalidated")
	}
}

func TestCascadeSettleNilStatus(t *testing.T) {
	m, done := cascadeAtSettle(t)
	m.actionStatus = nil

	m, _ = deliver(t, m, done)

	if m.cascade != nil {
		t.Error("m.cascade still set, want the settle to free the run")
	}
}

// stackMergeAtSettle runs a confirmed stack merge through a pending submit and
// a poll answering final, to the point where the run's settle is about to be
// delivered.
func stackMergeAtSettle(t *testing.T, final gh.StackMergeResult) (Model, actionDoneMsg, *fakeStackDetails) {
	t.Helper()
	m, fs, _, details := promptedStackMerge(t)
	fs.stackMergeResult = gh.StackMergeResult{Status: gh.StackMergePending, UUID: "u1"}
	fs.stackPollResults = []gh.StackMergeResult{final}

	m, cmd := update(m, keyMsg("y"))
	m, _ = update(m, head(t, cmd))
	run := m.stackMerge
	if run == nil {
		t.Fatal("setup: the pending answer should keep the run polling")
	}
	m, cmd = update(m, stackMergeProbeMsg{id: run.id})
	m, cmd = update(m, cmd())
	done, ok := cmd().(actionDoneMsg)
	if !ok || !done.stackMerge || done.stat != run.stat {
		t.Fatalf("setup: the poll should settle the run with its own stat, got %+v", done)
	}
	return m, done, details
}

func TestStackMergeSettleForeignStatusShown(t *testing.T) {
	m, done, _ := stackMergeAtSettle(t, gh.StackMergeResult{Status: gh.StackMergeMerged})
	foreign := &actionStat{run: "Copying", ok: "Copied", fail: "Copy failed"}
	m.actionStatus = foreign

	m, _ = deliver(t, m, done)

	if m.stackMerge != nil {
		t.Error("m.stackMerge still set, want the settle to free the run")
	}
	if foreign.settled || foreign.err != nil {
		t.Errorf("foreign status = %+v, want it still running with no error", foreign)
	}
}

func TestStackMergeSettleFailedWhileStatusNil(t *testing.T) {
	m, done, details := stackMergeAtSettle(t, gh.StackMergeResult{Status: gh.StackMergeFailed, Message: "checks failed"})
	if done.err == nil {
		t.Fatal("setup: the poll should have failed the run")
	}
	layers := []int{140, 141, 142}
	for _, n := range layers {
		m.fresh[n] = true
	}
	m.actionStatus = nil
	fetched := len(details.calls)

	m, cmd := deliver(t, m, done)
	collectMsgs(t, cmd)

	if m.stackMerge != nil {
		t.Error("m.stackMerge still set, want the settle to free the run")
	}
	for _, n := range layers {
		if m.fresh[n] {
			t.Errorf("#%d kept its freshness, want the run's layers invalidated", n)
		}
	}
	if got := details.calls[fetched:]; len(got) != 1 || !slices.Equal(got[0], layers) {
		t.Errorf("detail fetches = %v, want one for %v", got, layers)
	}
}

func TestActionDoneUncorrelatedLeavesStatusAlone(t *testing.T) {
	// A running mark-ready on #1, whose own result never arrives here.
	running := func(t *testing.T) (Model, *actionStat) {
		t.Helper()
		pr1 := mergeablePR(1, "alice")
		pr1.IsDraft = true
		m, _ := mutationModel(t, []gh.PR{pr1, mergeablePR(2, "alice")})
		_, stat := startOn(t, &m, 1, action.ReadyAction(true))
		m.fresh[1] = true
		return m, stat
	}

	t.Run("settles nothing", func(t *testing.T) {
		m, stat := running(t)

		m, cmd := deliver(t, m, actionDoneMsg{err: errors.New("boom"), fail: "other failure"})

		if m.actionStatus != stat || stat.settled || stat.err != nil || stat.fail == "other failure" {
			t.Errorf("status = %+v, want it still running and untouched", stat)
		}
		if !findPR(t, m.section.(*PRSection), 1).IsDraft || !m.fresh[1] {
			t.Error("the running action's side effects ran for a message that is not its own")
		}
		if cmd != nil {
			t.Error("an uncorrelated result scheduled work, want none")
		}
	})

	t.Run("frees a flagged run", func(t *testing.T) {
		m, stat := running(t)
		m.cascade, m.stackMerge = &cascadeRun{}, &stackMergeRun{}

		m, _ = deliver(t, m, actionDoneMsg{cascade: true, stackMerge: true})

		if m.cascade != nil || m.stackMerge != nil {
			t.Errorf("cascade = %v, stackMerge = %v, want both freed", m.cascade, m.stackMerge)
		}
		if stat.settled {
			t.Error("the running status settled")
		}
	})

	t.Run("reconciles merged rows", func(t *testing.T) {
		m, stat := running(t)
		m.fresh[2] = true

		m, cmd := deliver(t, m, actionDoneMsg{merged: []gh.PR{findPR(t, m.section.(*PRSection), 2)}})

		if _, ok := m.sessionMerged[2]; !ok {
			t.Error("#2 not marked session-merged")
		}
		if m.fresh[2] {
			t.Error("#2 kept its freshness, want the merged row invalidated")
		}
		if _, ok := listFetchMsg(t, cmd).(sectionsFetchedMsg); !ok {
			t.Error("no list refetch after merged rows")
		}
		if stat.settled {
			t.Error("the running status settled")
		}
	})
}
