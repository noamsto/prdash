package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/gh"
)

type fakeChecksSource struct {
	got [][]int // numbers passed to each FetchChecks call
	ret map[int][]gh.Check
}

func (f *fakeChecksSource) FetchChecks(nums []int) (map[int][]gh.Check, error) {
	f.got = append(f.got, append([]int(nil), nums...))
	return f.ret, nil
}

func pending() []gh.Check { return []gh.Check{{State: "PENDING"}} }
func passing() []gh.Check { return []gh.Check{{State: "SUCCESS"}} }

func pollModel(t *testing.T, fc *fakeChecksSource, prs []gh.PR) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.SetChecksSource(fc)
	m.setPRs(prs)
	// NewModel starts mid-launch-fetch; a poll only beats once that settled.
	m.refreshing = false
	m.polling = true
	return m
}

// rowIndexOf finds the shown index of a PR number — the sort makes positions
// unpredictable from the input order.
func rowIndexOf(t *testing.T, ps *PRSection, number int) int {
	t.Helper()
	for i := 0; i < ps.Len(); i++ {
		if ps.prAt(i).Number == number {
			return i
		}
	}
	t.Fatalf("PR #%d is not on the board", number)
	return -1
}

// The regression this change exists for. backgroundRefresh flags m.refreshing and
// starts the spinner; a rollup-only beat must do neither — it is silent, and it
// never reaches the list backend.
func TestPollBeatIsSilentAndSkipsTheList(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{{Number: 1, StatusCheckRollup: pending()}})

	u, cmd := m.Update(checksPollMsg{})
	if cmd == nil {
		t.Fatal("a beat with running checks must produce commands")
	}
	if u.(Model).refreshing {
		t.Error("a checks beat must not flag a refresh (that is the list-refetch path)")
	}
	if u.(Model).spinning {
		t.Error("a checks beat must not spin the header")
	}
}

func TestPollAsksOnlyForRunningRows(t *testing.T) {
	fc := &fakeChecksSource{}
	m := pollModel(t, fc, []gh.PR{
		{Number: 1, StatusCheckRollup: passing()},
		{Number: 2, StatusCheckRollup: pending()},
		{Number: 3, StatusCheckRollup: pending()},
	})
	cmd := m.pollChecksCmd()
	if cmd == nil {
		t.Fatal("want a command while checks are running")
	}
	if _, ok := cmd().(checksFetchedMsg); !ok {
		t.Fatal("the beat should route through the checks source")
	}
	if len(fc.got) != 1 {
		t.Fatalf("want one call, got %d", len(fc.got))
	}
	want := map[int]bool{2: true, 3: true}
	for _, n := range fc.got[0] {
		if !want[n] {
			t.Errorf("asked for #%d, which has no running check", n)
		}
	}
	if len(fc.got[0]) != 2 {
		t.Errorf("asked for %v, want exactly the two running PRs", fc.got[0])
	}
}

func TestPollChecksCmdNilWithoutSource(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: pending()}})
	if cmd := m.pollChecksCmd(); cmd != nil {
		t.Error("no checks source: the beat must not fire a command")
	}
}

func TestChecksFetchedUpdatesRowWithoutReordering(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{
		{Number: 1, Author: author("me"), StatusCheckRollup: pending()},
		{Number: 2, Author: author("me"), StatusCheckRollup: passing()},
	})
	ps := m.section.(*PRSection)
	var before []int
	for i := 0; i < ps.Len(); i++ {
		before = append(before, ps.prAt(i).Number)
	}

	// #1's checks fail — the actionability sort would hoist it if a beat re-sorted.
	u, _ := m.Update(checksFetchedMsg{checks: map[int][]gh.Check{1: {{State: "FAILURE"}}}})
	ps = u.(Model).section.(*PRSection)
	for i := 0; i < ps.Len(); i++ {
		if ps.prAt(i).Number != before[i] {
			t.Fatalf("row order changed: #%d at index %d, was #%d", ps.prAt(i).Number, i, before[i])
		}
	}
	if got := ps.prAt(rowIndexOf(t, ps, 1)).CIState(); got != "fail" {
		t.Errorf("polled rollup not applied: CIState %q, want fail", got)
	}
}

func TestChecksPollDelayHotForOwnPR(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{
		{Number: 1, Author: author("me"), StatusCheckRollup: pending()},
	})
	m.viewerLogin = "me"
	if got := m.checksPollDelay(); got != pollIntervalHot {
		t.Errorf("own PR should poll hot: got %v, want %v", got, pollIntervalHot)
	}
}

func TestChecksPollDelayColdForOtherPeoplesPRs(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{
		{Number: 1, Author: author("someone"), StatusCheckRollup: passing()},
		{Number: 2, Author: author("someone"), StatusCheckRollup: pending()},
	})
	m.viewerLogin = "me"
	// Sit on the settled row, so the churning one is neither mine nor focused.
	m.cursor = rowIndexOf(t, m.section.(*PRSection), 1)
	if got := m.checksPollDelay(); got != pollIntervalCold {
		t.Errorf("other people's CI should poll cold: got %v, want %v", got, pollIntervalCold)
	}
}

// The row you are looking at is hot even when it is someone else's — that is the
// PR you are waiting on right now.
func TestChecksPollDelayHotForCursorRow(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{
		{Number: 1, Author: author("someone"), StatusCheckRollup: pending()},
	})
	m.viewerLogin = "me"
	m.cursor = 0
	if got := m.checksPollDelay(); got != pollIntervalHot {
		t.Errorf("cursor row should poll hot: got %v, want %v", got, pollIntervalHot)
	}
}

// The tier only bites on relaunch: within a session m.fresh already suppresses a
// second fetch. Other people's PRs ride pollIntervalCold, mine stay at 60s.
func TestDetailFreshTTLTiersByAuthor(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{
		{Number: 1, Author: author("me")},
		{Number: 2, Author: author("someone")},
	})
	m.viewerLogin = "me"
	ps := m.section.(*PRSection)
	if got := m.detailFreshTTL(ps, 1); got != launchFreshTTL {
		t.Errorf("own PR TTL = %v, want %v", got, launchFreshTTL)
	}
	if got := m.detailFreshTTL(ps, 2); got != pollIntervalCold {
		t.Errorf("other's PR TTL = %v, want %v", got, pollIntervalCold)
	}
}

// With no viewer login resolved yet, nothing is "mine" — everything is cold
// rather than everything being hot.
func TestDetailFreshTTLColdWithoutViewer(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{{Number: 1, Author: author("me")}})
	if got := m.detailFreshTTL(m.section.(*PRSection), 1); got != pollIntervalCold {
		t.Errorf("unknown viewer TTL = %v, want %v", got, pollIntervalCold)
	}
}

// One request per cold cursor row, not two: the detail fetch carries the threads.
func TestCursorFetchIsASingleRequest(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{
		7: {ReviewThreads: []gh.ReviewThread{{Path: "main.go", Line: 3}}},
	}}
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.SetDetailSource(fd)
	m.setPRs([]gh.PR{{Number: 7}})

	cmd := m.detailCmdForCursor()
	if cmd == nil {
		t.Fatal("a cold cursor row must fetch")
	}
	msg, ok := cmd().(detailsBatchMsg)
	if !ok {
		t.Fatalf("cursor fetch should be the batched detail request, got %T", cmd())
	}
	if len(fd.got) != 1 {
		t.Fatalf("want exactly one request for the cursor row, got %d", len(fd.got))
	}
	if len(msg.details[7].ReviewThreads) != 1 {
		t.Error("review threads should arrive with the detail, not in a second fetch")
	}
}

// TestPollPausesAfterIdleBeats walks pollIdleBeats consecutive beats with no key
// handled between them: each one must still fetch, since the counter hasn't
// crossed the threshold yet. The next beat crosses it and must skip the fetch
// while keeping the tick loop alive (self-resuming: see checksPollMsg).
func TestPollPausesAfterIdleBeats(t *testing.T) {
	fc := &fakeChecksSource{}
	m := pollModel(t, fc, []gh.PR{{Number: 1, StatusCheckRollup: pending()}})

	for i := range pollIdleBeats {
		u, cmd := m.Update(checksPollMsg{})
		m = u.(Model)
		if cmd == nil {
			t.Fatalf("beat %d: want a command while checks are running", i)
		}
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) != 2 {
			t.Fatalf("beat %d: want a fetch+reschedule batch, got %T", i, cmd())
		}
		batch[0]() // pollChecksCmd, per tea.Batch(m.pollChecksCmd(), checksPollTick(...)) call order
	}
	if len(fc.got) != pollIdleBeats {
		t.Fatalf("want %d fetches before going idle, got %d", pollIdleBeats, len(fc.got))
	}

	// The threshold beat: idle now, so it must reschedule without fetching. Do not
	// call cmd() here — on the idle path it is the raw checksPollTick closure and
	// invoking it blocks for the poll interval.
	u, cmd := m.Update(checksPollMsg{})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("idle beat must still reschedule the tick, not kill the loop")
	}
	if !m.polling {
		t.Fatal("loop should stay alive while idle, waiting for input")
	}
	if len(fc.got) != pollIdleBeats {
		t.Error("idle beat must not fetch")
	}
}

// TestKeyInputResetsIdleCounter checks the resume half: a handled key zeroes the
// counter, so the very next beat fetches again.
func TestKeyInputResetsIdleCounter(t *testing.T) {
	fc := &fakeChecksSource{}
	m := pollModel(t, fc, []gh.PR{{Number: 1, StatusCheckRollup: pending()}})
	m.pollQuietBeats = pollIdleBeats // already idle

	u, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	if m.pollQuietBeats != 0 {
		t.Fatalf("handled key should reset the idle counter, got %d", m.pollQuietBeats)
	}

	_, cmd := m.Update(checksPollMsg{})
	if cmd == nil {
		t.Fatal("want a command while checks are running")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("resumed poll should fetch again, got %T", cmd())
	}
}

// TestCtrlRRefreshesAndResumesWhileIdle: ctrl+r is itself a handled key, so besides
// forcing its own refresh it must also resume a paused poll loop.
func TestCtrlRRefreshesAndResumesWhileIdle(t *testing.T) {
	fc := &fakeChecksSource{}
	m := pollModel(t, fc, []gh.PR{{Number: 1, StatusCheckRollup: pending()}})
	m.pollQuietBeats = pollIdleBeats // idle

	// Do not invoke cmd(): backgroundRefresh's fetch closure dereferences m.prSource,
	// which pollModel never sets.
	u, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("ctrl+r must always refresh")
	}
	if !m.refreshing {
		t.Error("ctrl+r should trigger backgroundRefresh")
	}
	if m.pollQuietBeats != 0 {
		t.Error("ctrl+r is a handled key: it should resume a paused poll too")
	}
}

// TestThemePollUnaffectedByIdlePause proves the checks-poll idle gate doesn't
// reach themePollMsg — the theme watch and rate-limit countdown keep ticking.
func TestThemePollUnaffectedByIdlePause(t *testing.T) {
	writeState(t, "") // no state file: themestate.ModTime errors, the case early-returns

	m := pollModel(t, &fakeChecksSource{}, []gh.PR{{Number: 1, StatusCheckRollup: pending()}})
	m.pollQuietBeats = pollIdleBeats // idle

	_, cmd := m.Update(themePollMsg{lastMod: m.themeModTime})
	if cmd == nil {
		t.Fatal("theme watch tick must keep rearming regardless of checks-poll idle state")
	}
}
