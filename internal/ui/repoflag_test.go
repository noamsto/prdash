package ui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func remoteEntries(t *testing.T, path string) []action.HandoffEntry {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []action.HandoffEntry
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		_, js, _ := strings.Cut(line, "\t")
		if strings.HasPrefix(js, "[") {
			continue
		}
		var e action.HandoffEntry
		if err := json.Unmarshal([]byte(js), &e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestPathFreeEnterWritesRemoteEntry(t *testing.T) {
	m, handoff, calls := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetPathFree(true)
	m, cmd := pressEnter(t, m, repoRowIndex(t, m, "nope/y", 3))

	if !isQuit(cmd) {
		t.Fatal("enter did not quit")
	}
	if len(*calls) != 0 {
		t.Errorf("preflight called: %+v", *calls)
	}
	got := remoteEntries(t, handoff)
	want := action.HandoffEntry{Repo: "nope/y", Kind: "pr", Target: "feat/c", PR: 3, Args: []string{"switch", "feat/c"}, Key: "enter"}
	if len(got) != 1 || got[0].Repo != want.Repo || got[0].Kind != want.Kind || got[0].Target != want.Target ||
		got[0].PR != want.PR || strings.Join(got[0].Args, " ") != "switch feat/c" || got[0].Key != want.Key {
		t.Errorf("entries = %+v, want [%+v]", got, want)
	}
}

func TestPathFreeOffKeepsNotice(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m, cmd := pressEnter(t, m, repoRowIndex(t, m, "nope/y", 3))
	if isQuit(cmd) || m.switchNotice == "" {
		t.Fatalf("want notice and no quit, notice = %q", m.switchNotice)
	}
	if _, err := os.Stat(handoff); err == nil {
		t.Error("handoff file written")
	}
}

func TestPathFreeCrossRepoPRUsesPRRef(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetPathFree(true)
	m.setPRs([]gh.PR{{Repo: "nope/y", Number: 9, HeadRefName: "main", IsCrossRepository: true}})
	m.renderList()
	m, _ = pressEnter(t, m, 0)
	got := remoteEntries(t, handoff)
	if len(got) != 1 || got[0].Target != "pr:9" {
		t.Errorf("entries = %+v, want target pr:9", got)
	}
}

func TestPathFreeBulk(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetPathFree(true)
	m.setPRs([]gh.PR{
		{Repo: "nope/y", Number: 3, HeadRefName: "feat/c"},
		{Repo: "nope/y", Number: 4, HeadRefName: "feat/d"},
	})
	m.renderList()
	m.sel.toggle(0)
	m.sel.toggle(1)
	cmd := m.runBulk(m.actions["W"])
	if !isQuit(cmd) {
		t.Fatal("bulk did not quit")
	}
	if got := remoteEntries(t, handoff); len(got) != 2 {
		t.Errorf("entries = %+v, want 2", got)
	}
}

func TestPathFreeMixedBulkBlockedWritesNothing(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetPathFree(true)
	preflightSwitchFn = func(string, string) (gh.SwitchPreflight, error) {
		return gh.SwitchPreflight{Path: "/wt", Occupant: "other", Remedy: "fix"}, nil
	}
	m.setPRs([]gh.PR{
		{Repo: "nope/y", Number: 3, HeadRefName: "feat/c"},
		{Repo: "other/x", Number: 2, HeadRefName: "feat/b"},
		{Repo: "other/x", Number: 5, HeadRefName: "feat/e"},
	})
	m.renderList()
	m.sel.toggle(0)
	m.sel.toggle(1)
	m.sel.toggle(2)
	cmd := m.runBulk(m.actions["W"])
	msg := firstBatchMsg(t, cmd)
	u, _ := m.Update(msg)
	if _, err := os.Stat(handoff); err == nil {
		t.Error("handoff written despite blocked preflight")
	}
	if !strings.Contains(u.(Model).switchNotice, "No switches were queued") {
		t.Errorf("notice = %q", u.(Model).switchNotice)
	}
}

func TestLaunchCheckoutRemoteNeverUsesDir(t *testing.T) {
	m, _, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	sibling := t.TempDir()
	checkoutForFn = func(_, repo string) (string, error) { return sibling, nil }
	m.SetLaunchCheckout(false)
	if dir, _ := m.checkoutDir("owner/repo"); dir != sibling {
		t.Errorf("dir = %q, want sibling %q", dir, sibling)
	}
	m.SetLaunchCheckout(true)
	if dir, _ := m.checkoutDir("owner/repo"); dir != m.dir {
		t.Errorf("dir = %q, want m.dir", dir)
	}
}

func TestRemoteLaunchRepoBoards(t *testing.T) {
	is := &fakeIssueSource{}
	rs := &fakeRunsSource{}
	m := NewModel(t.TempDir(), "is:open", nil)
	m.SetRepo("owner/name")
	m.SetLaunchCheckout(false)
	m.SetIssueSource(is)
	m.SetRunsSource(rs)
	m.width, m.height = 100, 40
	if m.noRepo() {
		t.Fatal("a named repo is not the all-repos board")
	}

	m, cmd := update(m, keyMsg("tab"))
	if m.mode != "issue" {
		t.Fatalf("mode = %q, want issue", m.mode)
	}
	for _, msg := range collectMsgs(t, cmd) {
		m, _ = update(m, msg)
	}
	if len(is.calls) == 0 {
		t.Error("issue board never fetched issues for the named repo")
	}

	m, cmd = update(m, keyMsg("tab"))
	if m.mode != "run" {
		t.Fatalf("mode = %q, want run", m.mode)
	}
	for _, msg := range collectMsgs(t, cmd) {
		m, _ = update(m, msg)
	}
	if rs.lists() == 0 {
		t.Error("runs board never listed runs for the named repo")
	}
	if m.actionStatus != nil && strings.Contains(m.actionStatus.fail, "start prdash") {
		t.Errorf("repo-needed hint shown for a named repo: %q", m.actionStatus.fail)
	}
}

func TestPathFreeUnsafeHeadRefUsesPRRef(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetPathFree(true)
	m.setPRs([]gh.PR{{Repo: "nope/y", Number: 9, HeadRefName: "-xtouch"}})
	m.renderList()
	m, _ = pressEnter(t, m, 0)
	got := remoteEntries(t, handoff)
	if len(got) != 1 || got[0].Target != "pr:9" || strings.Join(got[0].Args, " ") != "switch pr:9" {
		t.Errorf("entries = %+v, want target pr:9", got)
	}
}

func TestPathFreeLaunchRepoPR(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetRepo("owner/name")
	m.SetLaunchCheckout(false)
	m.SetPathFree(true)
	checkoutForFn = func(_, repo string) (string, error) { return "", &gh.CheckoutError{Repo: repo, Path: missingCheckout} }
	m.setPRs([]gh.PR{{Repo: "owner/name", Number: 8, HeadRefName: "feat/l"}})
	m.renderList()
	m, cmd := pressEnter(t, m, 0)
	if !isQuit(cmd) {
		t.Fatal("enter did not quit")
	}
	got := remoteEntries(t, handoff)
	if len(got) != 1 || got[0].Repo != "owner/name" || got[0].Kind != "pr" || got[0].PR != 8 ||
		got[0].Target != "feat/l" || strings.Join(got[0].Args, " ") != "switch feat/l" || got[0].Key != "enter" {
		t.Errorf("entries = %+v", got)
	}
}

func TestPathFreeIssueRow(t *testing.T) {
	m, handoff, _ := worktreeRepoModel(t, t.TempDir(), "OPEN")
	m.SetPathFree(true)
	m.SetRepo("nope/y")
	m.SetLaunchCheckout(false)
	m.SetIssueSource(&fakeIssueSource{})
	m, _ = update(m, keyMsg("tab"))
	if m.mode != "issue" {
		t.Fatalf("mode = %q, want issue", m.mode)
	}
	m.setIssues([]gh.Issue{{Number: 5, Title: "Fix bug"}})
	m.renderList()
	m, cmd := pressEnter(t, m, 0)
	if !isQuit(cmd) {
		t.Fatal("enter did not quit")
	}
	got := remoteEntries(t, handoff)
	if len(got) != 1 || got[0].Repo != "nope/y" || got[0].Kind != "issue" || got[0].PR != 0 ||
		got[0].Target != "feat/5-fix-bug" || strings.Join(got[0].Args, " ") != "switch -c feat/5-fix-bug" || got[0].Key != "enter" {
		t.Errorf("entries = %+v", got)
	}
}

func TestPathFreeMixedBulkWritesArgvAndEntry(t *testing.T) {
	checkout := t.TempDir()
	m, handoff, _ := worktreeRepoModel(t, checkout, "OPEN")
	m.SetPathFree(true)
	m.setPRs([]gh.PR{
		{Repo: "other/x", Number: 2, HeadRefName: "feat/b"},
		{Repo: "nope/y", Number: 3, HeadRefName: "feat/c"},
	})
	m.renderList()
	m.sel.toggle(0)
	m.sel.toggle(1)
	cmd := m.runBulk(m.actions["W"])
	m.Update(firstBatchMsg(t, cmd))
	b, err := os.ReadFile(handoff)
	if err != nil {
		t.Fatal(err)
	}
	var argvs [][]string
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		_, js, _ := strings.Cut(line, "\t")
		var a []string
		if strings.HasPrefix(js, "[") && json.Unmarshal([]byte(js), &a) == nil {
			argvs = append(argvs, a)
		}
	}
	if len(argvs) != 1 || strings.Join(argvs[0], " ") != "wt -C "+checkout+" switch pr:2" {
		t.Errorf("argv lines = %q", argvs)
	}
	got := remoteEntries(t, handoff)
	if len(got) != 1 || got[0].Repo != "nope/y" || got[0].Kind != "pr" || got[0].PR != 3 ||
		got[0].Target != "feat/c" || strings.Join(got[0].Args, " ") != "switch feat/c" || got[0].Key != "W" {
		t.Errorf("entries = %+v", got)
	}
}

func TestSwitchOnlyNoticeLaunchRepoRemote(t *testing.T) {
	m := NewModel(t.TempDir(), "is:open", nil)
	m.SetRepo("owner/name")
	m.SetLaunchCheckout(false)
	if got := m.switchOnlyNotice("x", "Owner/Name", ""); got != "x needs prdash started in owner/name's checkout" {
		t.Errorf("notice = %q", got)
	}
	if got := m.switchOnlyNotice("x", "other/y", ""); got != "x needs prdash started in owner/name's checkout" {
		t.Errorf("notice = %q", got)
	}
}
