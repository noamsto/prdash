package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

type fakeChecksSource struct {
	got [][]gh.PRRef // refs passed to each FetchChecks call
	ret map[gh.PRRef][]gh.Check
}

func (f *fakeChecksSource) FetchChecks(refs []gh.PRRef) (map[gh.PRRef][]gh.Check, error) {
	f.got = append(f.got, append([]gh.PRRef(nil), refs...))
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
	want := map[gh.PRRef]bool{{Repo: "owner/repo", Number: 2}: true, {Repo: "owner/repo", Number: 3}: true}
	for _, r := range fc.got[0] {
		if !want[r] {
			t.Errorf("asked for %v, which has no running check", r)
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
	u, _ := m.Update(checksFetchedMsg{checks: map[gh.PRRef][]gh.Check{m.launchRef(1): {{State: "FAILURE"}}}})
	ps = u.(Model).section.(*PRSection)
	if len(before) != ps.Len() {
		t.Fatalf("row count changed: %d -> %d", len(before), ps.Len())
	}
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
	if got := m.detailFreshTTL(ps, m.launchRef(1)); got != launchFreshTTL {
		t.Errorf("own PR TTL = %v, want %v", got, launchFreshTTL)
	}
	if got := m.detailFreshTTL(ps, m.launchRef(2)); got != pollIntervalCold {
		t.Errorf("other's PR TTL = %v, want %v", got, pollIntervalCold)
	}
}

// With no viewer login resolved yet, nothing is "mine" — everything is cold
// rather than everything being hot.
func TestDetailFreshTTLColdWithoutViewer(t *testing.T) {
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{{Number: 1, Author: author("me")}})
	if got := m.detailFreshTTL(m.section.(*PRSection), m.launchRef(1)); got != pollIntervalCold {
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
	if len(msg.details[m.launchRef(7)].ReviewThreads) != 1 {
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

// settleModel has #1..#3 running with a detail cached while CI ran (UNSTABLE),
// the state the poll leaves behind once the rollups turn green.
func settleModel(t *testing.T, fd *fakeDetailSource) Model {
	t.Helper()
	m := pollModel(t, &fakeChecksSource{}, []gh.PR{
		{Number: 1, StatusCheckRollup: pending()},
		{Number: 2, StatusCheckRollup: pending()},
		{Number: 3, StatusCheckRollup: pending()},
	})
	m.SetDetailSource(fd)
	m.width, m.height = 100, 30
	for n := 1; n <= 3; n++ {
		m.detail[m.launchRef(n)] = gh.PRDetail{MergeStateStatus: "UNSTABLE"}
		m.fresh[m.launchRef(n)] = true
	}
	return m
}

// batchedMsgs flattens a possibly batched cmd into the messages it yields.
func batchedMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	b, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range b {
		out = append(out, batchedMsgs(c)...)
	}
	return out
}

func TestSettledChecksRefetchDetailOncePerBeat(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{
		1: {MergeStateStatus: "CLEAN"}, 2: {MergeStateStatus: "CLEAN"},
	}}
	m := settleModel(t, fd)

	u, cmd := m.Update(checksFetchedMsg{checks: map[gh.PRRef][]gh.Check{
		m.launchRef(1): passing(), m.launchRef(2): passing(), m.launchRef(3): pending(),
	}})
	m = u.(Model)
	for _, n := range []int{1, 2} {
		if m.fresh[m.launchRef(n)] {
			t.Errorf("#%d settled but its detail is still marked fresh", n)
		}
	}
	if !m.fresh[m.launchRef(3)] {
		t.Error("#3 is still running; its detail must stay fresh")
	}

	var details []tea.Msg
	for _, msg := range batchedMsgs(cmd) {
		if _, ok := msg.(detailsBatchMsg); ok {
			details = append(details, msg)
		}
	}
	if len(fd.got) != 1 {
		t.Fatalf("want exactly one FetchDetails call for the beat, got %d: %v", len(fd.got), fd.got)
	}
	if got := fd.got[0]; len(got) != 2 || !slices.Contains(got, m.launchRef(1)) || !slices.Contains(got, m.launchRef(2)) {
		t.Fatalf("FetchDetails refs = %v, want #1 and #2", got)
	}

	for _, msg := range details {
		u, _ = m.Update(msg)
		m = u.(Model)
	}
	m.cursor = rowIndexOf(t, m.section.(*PRSection), 1)
	card, ok := m.cursorCard()
	if !ok {
		t.Fatal("cursor PR has no cached detail")
	}
	if card.Kind != triage.KindReady {
		t.Errorf("card = %v %q after checks settled and detail refreshed, want KindReady", card.Kind, card.Headline)
	}
}

func TestUnresolvedSettleRefetchStaysUnfreshAndRetries(t *testing.T) {
	fd := &sequencedDetailSource{readings: []gh.PRDetail{
		{MergeStateStatus: "UNSTABLE"}, {MergeStateStatus: "CLEAN"},
	}}
	m := settleModel(t, &fakeDetailSource{})
	m.SetDetailSource(fd)
	ref := m.launchRef(1)

	u, cmd := m.Update(checksFetchedMsg{checks: map[gh.PRRef][]gh.Check{
		ref: passing(), m.launchRef(2): pending(), m.launchRef(3): pending(),
	}})
	m = u.(Model)
	for _, msg := range batchedMsgs(cmd) {
		u, _ = m.Update(msg)
		m = u.(Model)
	}
	if m.fresh[ref] {
		t.Fatal("unresolved UNSTABLE refetch was marked fresh; it would never be retried")
	}

	m.cursor = rowIndexOf(t, m.section.(*PRSection), 1)
	retry := m.detailCmdForCursor()
	if retry == nil {
		t.Fatal("cursor visit did not refetch the unresolved detail")
	}
	u, _ = m.Update(retry())
	m = u.(Model)
	if fd.calls != 2 {
		t.Fatalf("FetchDetails calls = %d, want 2", fd.calls)
	}
	if !m.fresh[ref] {
		t.Error("resolved CLEAN detail should be fresh")
	}
	card, ok := m.cursorCard()
	if !ok || card.Kind != triage.KindReady {
		t.Errorf("card = %v, want KindReady", card.Kind)
	}
}

// settleOnce drives one settle beat for PR 1 and applies the refetch results.
func settleOnce(t *testing.T, m Model) Model {
	t.Helper()
	u, cmd := m.Update(checksFetchedMsg{checks: map[gh.PRRef][]gh.Check{
		m.launchRef(1): passing(), m.launchRef(2): pending(), m.launchRef(3): pending(),
	}})
	m = u.(Model)
	for _, msg := range batchedMsgs(cmd) {
		u, _ = m.Update(msg)
		m = u.(Model)
	}
	return m
}

func TestUnresolvedSettleRetryBypassesOlderDiskEntry(t *testing.T) {
	fd := &sequencedDetailSource{readings: []gh.PRDetail{
		{MergeStateStatus: "UNSTABLE"}, {MergeStateStatus: "CLEAN"},
	}}
	m := batchModel(t, &fakeDetailSource{}, []gh.PR{
		{Number: 1, StatusCheckRollup: pending()},
		{Number: 2, StatusCheckRollup: pending()},
		{Number: 3, StatusCheckRollup: pending()},
	})
	m.SetDetailSource(fd)
	m.width, m.height = 100, 30
	m.polling = true
	ref := m.launchRef(1)
	key := detailKey(ref.Repo, ref.Number)
	m.cache.Set(key, json.RawMessage(`{"mergeStateStatus":"UNSTABLE"}`))
	if !m.cache.Fresh(key, time.Hour) {
		t.Fatal("setup: seeded cache entry should be fresh")
	}
	m.fresh[ref] = true

	m = settleOnce(t, m)
	if m.fresh[ref] || m.cache.Fresh(key, time.Hour) {
		t.Fatal("unresolved settle read left the detail fresh in memory or on disk")
	}
	m.cursor = rowIndexOf(t, m.section.(*PRSection), 1)
	if m.detailCmdForCursor() == nil {
		t.Fatal("cursor visit did not refetch despite the older disk entry")
	}
}

func TestLegitimateUnstableOutsideSettleIsFreshAndCached(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{5: {MergeStateStatus: "UNSTABLE"}}}
	m := batchModel(t, fd, []gh.PR{{Number: 5, StatusCheckRollup: []gh.Check{{State: "FAILURE"}}}})
	ref := m.launchRef(5)
	m.cursor = rowIndexOf(t, m.section.(*PRSection), 5)

	u, _ := m.Update(m.batchDetailCmd([]gh.PRRef{ref})())
	m = u.(Model)
	if !m.fresh[ref] || !m.cache.Fresh(detailKey(ref.Repo, ref.Number), time.Hour) {
		t.Fatal("UNSTABLE detail from a normal fetch should be fresh and cached")
	}
	calls := len(fd.got)
	for i := 0; i < 2; i++ {
		m.detailSeq = i + 1
		u, cmd := m.Update(detailDebounceMsg{seq: i + 1})
		m = u.(Model)
		batchedMsgs(cmd)
	}
	if len(fd.got) != calls {
		t.Errorf("cursor visits refetched a fresh UNSTABLE PR: %d extra FetchDetails calls", len(fd.got)-calls)
	}
}

func TestStillUnresolvedAfterRetryBecomesFresh(t *testing.T) {
	fd := &sequencedDetailSource{readings: []gh.PRDetail{{MergeStateStatus: "UNSTABLE"}}}
	m := settleModel(t, &fakeDetailSource{})
	m.SetDetailSource(fd)
	ref := m.launchRef(1)

	m = settleOnce(t, m)
	if m.fresh[ref] {
		t.Fatal("settle read should stay unfresh for one retry")
	}
	m.cursor = rowIndexOf(t, m.section.(*PRSection), 1)
	retry := m.detailCmdForCursor()
	if retry == nil {
		t.Fatal("no retry issued")
	}
	u, _ := m.Update(retry())
	m = u.(Model)
	if !m.fresh[ref] {
		t.Fatal("still-UNSTABLE retry should be fresh")
	}
	if m.detailCmdForCursor() != nil {
		t.Error("cursor refetches again after the one retry")
	}
}

func TestNoSettleNoDetailFetch(t *testing.T) {
	fd := &fakeDetailSource{}
	m := settleModel(t, fd)
	_, cmd := m.Update(checksFetchedMsg{checks: map[gh.PRRef][]gh.Check{m.launchRef(1): pending()}})
	batchedMsgs(cmd)
	if len(fd.got) != 0 {
		t.Errorf("no PR settled, yet FetchDetails ran: %v", fd.got)
	}
}

func TestExpandedOverviewRefreshesOnDetailsBatch(t *testing.T) {
	m := settleModel(t, &fakeDetailSource{})
	ps := m.section.(*PRSection)
	ps.ApplyChecks(map[gh.PRRef][]gh.Check{m.launchRef(1): passing(), m.launchRef(2): passing(), m.launchRef(3): passing()})
	m.cursor = rowIndexOf(t, ps, 1)
	m.expanded = true
	m.expandedTab = tabOverview
	m.reflowExpanded()

	u, _ := m.Update(detailsBatchMsg{details: map[gh.PRRef]gh.PRDetail{
		m.launchRef(1): {MergeStateStatus: "CLEAN"},
	}})
	got := ansi.Strip(u.(Model).vp.GetContent())
	if !strings.Contains(got, "Ready to merge") {
		t.Errorf("expanded Overview did not re-render with the fresh detail:\n%s", got)
	}
}
