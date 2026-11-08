package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// countListFetches runs cmd's tree and counts the list-fetch results in it.
// Tick-based leaves (spinner, status clear) time out and are skipped.
func countListFetches(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case prsFetchedMsg, sectionsFetchedMsg, fetchFailedMsg:
			return 1
		case tea.BatchMsg:
			n := 0
			for _, c := range msg {
				n += countListFetches(c)
			}
			return n
		}
	case <-time.After(200 * time.Millisecond):
	}
	return 0
}

// TestMergeResultUnderAnotherActionsStatusReconciles: a merge result that lands
// while a different action's status is showing (a refused rerun on a held row
// here, which carries no refresh) must still invalidate the merged PR's detail
// and refetch the list exactly once.
func TestMergeResultUnderAnotherActionsStatusReconciles(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	m.setPRs([]gh.PR{mergeablePR(11, "alice"), mergeablePR(9, "alice")})
	m.SetMutationSource(&fakeMutationSource{})
	m.cursor = shownIndex(m, 11)
	done := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))

	m.held[9] = ""
	m.cursor = shownIndex(m, 9)
	m.runAction(action.DefaultPRActions()["r"]) // refused: #9 is held
	if m.actionStatus == nil {
		t.Fatal("test setup: want the refused rerun's status showing when the merge lands")
	}
	m.fresh[11] = true

	u, cmd := m.Update(done)
	m = u.(Model)

	if m.fresh[11] {
		t.Error("merged #11 kept its detail freshness, want it invalidated")
	}
	if got := countListFetches(cmd); got != 1 {
		t.Errorf("list refetches = %d, want exactly 1", got)
	}
}

// TestMergeResultWithOwnStatusRefetchesOnce: the ordinary merge, whose own
// status carries refresh, still refetches once and not once per branch.
func TestMergeResultWithOwnStatusRefetchesOnce(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 40
	m.SetPRSource(stubSource{})
	m.setPRs([]gh.PR{mergeablePR(11, "alice")})
	m.SetMutationSource(&fakeMutationSource{})
	m.cursor = shownIndex(m, 11)
	done := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
	if m.actionStatus == nil || !m.actionStatus.refresh {
		t.Fatalf("test setup: status = %+v, want the merge's own status with refresh", m.actionStatus)
	}
	m.fresh[11] = true

	u, cmd := m.Update(done)
	m = u.(Model)

	if m.fresh[11] {
		t.Error("merged #11 kept its detail freshness, want it invalidated")
	}
	if got := countListFetches(cmd); got != 1 {
		t.Errorf("list refetches = %d, want exactly 1", got)
	}
}
