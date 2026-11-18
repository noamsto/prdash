package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

const missingCheckout = "/root/nope/y"

type preflightCall struct{ dir, branch string }

// worktreeRepoModel builds a flat board of owner/repo#1, other/x#2 and nope/y#3
// launched in owner/repo, with every external seam stubbed: checkoutForFn
// resolves other/x to checkout and fails for nope/y, and preflightSwitchFn
// records its calls instead of running wt.
func worktreeRepoModel(t *testing.T, checkout string, otherState string) (m Model, handoff string, calls *[]preflightCall) {
	t.Helper()
	handoff = filepath.Join(t.TempDir(), "handoff")
	t.Setenv("PRDASH_ACTION_FILE", handoff)

	oldCheckout, oldPreflight := checkoutForFn, preflightSwitchFn
	t.Cleanup(func() { checkoutForFn, preflightSwitchFn = oldCheckout, oldPreflight })
	checkoutForFn = func(_, repo string) (string, error) {
		switch repo {
		case "other/x":
			return checkout, nil
		case "nope/y":
			return "", &gh.CheckoutError{Repo: repo, Path: missingCheckout}
		}
		t.Errorf("checkoutForFn called for %q", repo)
		return "", nil
	}
	calls = &[]preflightCall{}
	preflightSwitchFn = func(dir, branch string) (gh.SwitchPreflight, error) {
		*calls = append(*calls, preflightCall{dir, branch})
		return gh.SwitchPreflight{}, nil
	}

	m = NewModel(t.TempDir(), "is:open", nil)
	m.SetRepo("owner/repo")
	m.SetReposRoot(t.TempDir())
	m.width, m.height = 140, 40
	m.setPRs([]gh.PR{
		{Repo: "owner/repo", Number: 1, HeadRefName: "feat/a", Title: "a"},
		{Repo: "other/x", Number: 2, HeadRefName: "feat/b", Title: "b", State: otherState},
		{Repo: "nope/y", Number: 3, HeadRefName: "feat/c", Title: "c"},
	})
	m.renderList()
	return m, handoff, calls
}

func repoRowIndex(t *testing.T, m Model, repo string, n int) int {
	t.Helper()
	for i := 0; i < m.section.Len(); i++ {
		if r, ok := m.refAt(i); ok && r.Repo == repo && r.Number == n {
			return i
		}
	}
	t.Fatalf("no row %s#%d on the board", repo, n)
	return -1
}

func handoffLines(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		_, js, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("malformed handoff line %q", line)
		}
		var argv []string
		if err := json.Unmarshal([]byte(js), &argv); err != nil {
			t.Fatal(err)
		}
		out = append(out, argv)
	}
	return out
}

// notQuit fails when cmd would quit the TUI; a nil cmd is fine.
func notQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if _, quit := cmd().(tea.QuitMsg); quit {
		t.Fatal("cmd is tea.Quit, want the TUI to stay open")
	}
}

// pressEnter presses enter on the row at idx and, when that starts a preflight,
// drives it and feeds the result back like the runtime would.
func pressEnter(t *testing.T, m Model, idx int) (Model, tea.Cmd) {
	t.Helper()
	m.cursor = idx
	u, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return u.(Model), cmd
}

func TestOtherRepoWorktree(t *testing.T) {
	t.Run("enter on another repo's row switches inside its checkout", func(t *testing.T) {
		checkout := t.TempDir()
		m, handoff, calls := worktreeRepoModel(t, checkout, "OPEN")
		m, cmd := pressEnter(t, m, repoRowIndex(t, m, "other/x", 2))
		if cmd == nil {
			t.Fatal("enter returned no cmd")
		}
		u, _ := m.Update(firstBatchMsg(t, cmd))
		m = u.(Model)

		if len(*calls) != 1 || (*calls)[0].dir != checkout || (*calls)[0].branch != "feat/b" {
			t.Errorf("preflight calls = %+v, want one in %s for feat/b", *calls, checkout)
		}
		want := []string{"wt", "-C", checkout, "switch", "pr:2"}
		if got := handoffLines(t, handoff); len(got) != 1 || !slices.Equal(got[0], want) {
			t.Errorf("handoff = %v, want [%v]", got, want)
		}
	})

	t.Run("enter on a launch-repo row is unchanged", func(t *testing.T) {
		m, handoff, calls := worktreeRepoModel(t, t.TempDir(), "OPEN")
		dir := m.dir
		m, cmd := pressEnter(t, m, repoRowIndex(t, m, "owner/repo", 1))
		if cmd == nil {
			t.Fatal("enter returned no cmd")
		}
		u, _ := m.Update(firstBatchMsg(t, cmd))
		m = u.(Model)

		if len(*calls) != 1 || (*calls)[0].dir != dir {
			t.Errorf("preflight calls = %+v, want one in %s", *calls, dir)
		}
		got := handoffLines(t, handoff)
		if len(got) != 1 || len(got[0]) < 3 || !slices.Equal(got[0][:2], []string{"wt", "switch"}) {
			t.Errorf("handoff = %v, want a plain `wt switch <ref>`", got)
		}
		if len(got) == 1 && slices.Contains(got[0], "-C") {
			t.Errorf("launch-repo argv carries -C: %v", got[0])
		}
	})

	t.Run("enter on a row with no checkout shows the path and queues nothing", func(t *testing.T) {
		m, handoff, calls := worktreeRepoModel(t, t.TempDir(), "OPEN")
		m, cmd := pressEnter(t, m, repoRowIndex(t, m, "nope/y", 3))

		if !strings.Contains(m.switchNotice, missingCheckout) {
			t.Errorf("switchNotice = %q, want it to name %s", m.switchNotice, missingCheckout)
		}
		notQuit(t, cmd)
		if got := handoffLines(t, handoff); len(got) != 0 {
			t.Errorf("handoff written: %v", got)
		}
		if len(*calls) != 0 {
			t.Errorf("preflight ran: %+v", *calls)
		}
	})

	t.Run("W with one unresolved row queues nothing", func(t *testing.T) {
		m, handoff, calls := worktreeRepoModel(t, t.TempDir(), "OPEN")
		m.sel.toggle(repoRowIndex(t, m, "other/x", 2))
		m.sel.toggle(repoRowIndex(t, m, "nope/y", 3))
		cmd := m.runBulk(m.actions["W"])

		if !strings.Contains(m.switchNotice, missingCheckout) {
			t.Errorf("switchNotice = %q, want it to name %s", m.switchNotice, missingCheckout)
		}
		notQuit(t, cmd)
		if got := handoffLines(t, handoff); len(got) != 0 {
			t.Errorf("handoff written: %v", got)
		}
		if len(*calls) != 0 || len(m.PendingExec()) != 0 {
			t.Errorf("something was queued: preflight=%+v pending=%v", *calls, m.PendingExec())
		}
	})

	t.Run("X cleans up inside the row's checkout", func(t *testing.T) {
		checkout := cleanupRepo(t)
		gitIn(t, checkout, "branch", "feat/b")
		m, _, _ := worktreeRepoModel(t, checkout, "MERGED")
		// m.dir is not a git repo: cleanup aimed at it cannot find feat/b.
		m.cursor = repoRowIndex(t, m, "other/x", 2)
		cmd := m.runAction(m.actions["X"])
		if cmd == nil {
			t.Fatal("X returned no cmd")
		}
		done, ok := firstBatchMsg(t, cmd).(actionDoneMsg)
		if !ok {
			t.Fatalf("X work cmd did not return an actionDoneMsg")
		}
		if done.err != nil {
			t.Fatalf("cleanup failed: %v", done.err)
		}
		if gh.BranchExists(checkout, "feat/b") {
			t.Error("feat/b still exists in the other repo's checkout")
		}
	})

	t.Run("a rebound enter that is not a wt switch is refused on other repos", func(t *testing.T) {
		m, handoff, calls := worktreeRepoModel(t, t.TempDir(), "OPEN")
		m.actions["enter"] = action.Action{
			Key:      "enter",
			Command:  action.Command{Argv: []string{"echo", "{{.Number}}"}},
			ExitsTUI: true,
		}
		m, cmd := pressEnter(t, m, repoRowIndex(t, m, "other/x", 2))

		if !strings.Contains(m.switchNotice, "enter runs only on owner/repo rows") {
			t.Errorf("switchNotice = %q", m.switchNotice)
		}
		notQuit(t, cmd)
		if got := handoffLines(t, handoff); len(got) != 0 {
			t.Errorf("handoff written: %v", got)
		}
		if len(*calls) != 0 {
			t.Errorf("preflight ran: %+v", *calls)
		}
	})
}
