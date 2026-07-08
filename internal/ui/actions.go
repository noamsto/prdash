package ui

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
)

// cursorVars returns the template vars for the row under the cursor, or
// false if the cursor is out of range. Repo is injected from the model.
func (m *Model) cursorVars() (action.Vars, bool) {
	i := m.cursor
	if i < 0 || i >= m.section.Len() {
		return action.Vars{}, false
	}
	v := m.section.VarsAt(i)
	v.Repo = m.repo
	return v, true
}

// clipboardText is the payload a copy action writes for one PR.
func clipboardText(builtin string, v action.Vars) string {
	switch builtin {
	case "copy-url":
		return v.URL
	case "copy-branch":
		return v.Branch
	case "copy-number":
		return fmt.Sprintf("#%d", v.Number)
	default:
		return ""
	}
}

// copiedLabel is the status shown after a copy action: past-tense, pluralized
// by how many rows were grabbed.
func copiedLabel(builtin string, n int) string {
	noun, plural := "URL", "URLs"
	switch builtin {
	case "copy-branch":
		noun, plural = "branch", "branches"
	case "copy-number":
		noun, plural = "PR number", "PR numbers"
	}
	if n > 1 {
		return fmt.Sprintf("Copied %d %s", n, plural)
	}
	return "Copied " + noun
}

// selectedOrCursor returns the selected row indices (sorted), or the cursor row
// when nothing is selected.
func (m *Model) selectedOrCursor() []int {
	idx := m.sel.indices()
	if len(idx) == 0 {
		return []int{m.cursor}
	}
	slices.Sort(idx)
	return idx
}

// copyPayload joins the clipboard text for every selected row (or the cursor),
// so the copy actions grab the whole selection at once.
func (m *Model) copyPayload(builtin string) string {
	var lines []string
	for _, i := range m.selectedOrCursor() {
		if i < 0 || i >= m.section.Len() {
			continue
		}
		v := m.section.VarsAt(i)
		v.Repo = m.repo
		if s := clipboardText(builtin, v); s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

// PendingExec is the list of exits-TUI commands queued to run after the program
// quits, populated only when no orchestrator handoff file is set.
func (m Model) PendingExec() [][]string { return m.pendingExec }

// queueExit routes an exits-TUI command: to the orchestrator's handoff file when
// one is set, otherwise onto pendingExec for main to exec after the TUI closes.
func (m *Model) queueExit(key string, argv []string) {
	if path := os.Getenv("PRDASH_ACTION_FILE"); path != "" {
		_ = action.AppendHandoff(path, key, argv)
		return
	}
	m.pendingExec = append(m.pendingExec, argv)
}

// runAction executes a single-scope action against the cursor row. exits-tui
// actions hand off (or queue) the command and quit; inline actions run via the runner.
func (m *Model) runAction(a action.Action) tea.Cmd {
	v, ok := m.cursorVars()
	if !ok {
		return nil
	}

	if a.ExitsTUI {
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			return nil
		}
		m.queueExit(a.Key, argv)
		return tea.Quit
	}

	switch a.Command.Builtin {
	case "copy-url", "copy-branch", "copy-number":
		text := m.copyPayload(a.Command.Builtin)
		ok := copiedLabel(a.Command.Builtin, len(m.selectedOrCursor()))
		if m.sel.count() > 0 {
			m.sel.clear() // a batch copy consumes the selection
		}
		// Prefer a native clipboard tool: tmux 3.6 drops OSC 52 sent from a popup
		// (prdash's prefix+p launch), so tea.SetClipboard silently fails there.
		// Fixed in tmux 3.7 (issue 4797) — revert to OSC 52 only once upgraded, as
		// it's the sole path that reaches the *local* clipboard over SSH. See #20.
		if argv := clipboardArgv(); argv != nil {
			m.actionStatus = &actionStat{run: "Copying", ok: ok, fail: "Copy failed"}
			return tea.Batch(func() tea.Msg {
				return actionDoneMsg{err: writeClipboard(argv, text)}
			}, m.startSpinner())
		}
		m.actionStatus = &actionStat{ok: ok, fail: "Copy failed", settled: true} // OSC 52 is fire-and-forget
		return tea.Batch(tea.SetClipboard(text), clearStatusCmd())
	case "rerun-failed":
		r, dir, branch := m.runner, m.dir, v.HeadRefName
		m.actionStatus = statFor(a)
		m.actionStatus.refresh = a.Refresh
		m.actionStatus.nums = []int{v.Number}
		return tea.Batch(func() tea.Msg {
			return actionDoneMsg{err: action.RerunFailed(r, dir, branch)}
		}, m.startSpinner())
	default: // argv (e.g. gh pr merge)
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			return nil
		}
		r, dir := m.runner, m.dir
		m.actionStatus = statFor(a)
		m.actionStatus.refresh = a.Refresh
		m.actionStatus.nums = []int{v.Number}
		return tea.Batch(func() tea.Msg {
			_, err := r.Run(dir, argv[1:]...) // argv[0]=="gh"
			return actionDoneMsg{err: err}
		}, m.startSpinner())
	}
}

// actionStat is an inline action's transient progress, surfaced by the header
// as a state-appropriate badge (gerund while running, past tense on success).
type actionStat struct {
	run     string // gerund shown while in flight
	ok      string // past tense shown on success
	fail    string // shown on failure
	settled bool
	err     error
	refresh bool  // true when the action mutated the PR(s) → refetch on success
	nums    []int // PR numbers the action touched, for detail-freshness invalidation
}

// statFor builds the running status for an action, falling back to its imperative
// label for any per-state wording the action leaves unset.
func statFor(a action.Action) *actionStat {
	run, ok, fail := a.Progress, a.Past, a.Fail
	if run == "" {
		run = a.Label
	}
	if ok == "" {
		ok = a.Label
	}
	if fail == "" {
		fail = a.Label
	}
	return &actionStat{run: run, ok: ok, fail: fail}
}

// actionRunning reports whether an inline action is still in flight.
func (m Model) actionRunning() bool {
	return m.actionStatus != nil && !m.actionStatus.settled
}

// reviewerDiff compares the currently-requested reviewers against the picked
// set, returning logins to add and to remove.
func reviewerDiff(current []string, picked map[string]bool) (add, remove []string) {
	cur := map[string]bool{}
	for _, l := range current {
		cur[l] = true
	}
	for l, on := range picked {
		if on && !cur[l] {
			add = append(add, l)
		}
	}
	for l := range cur {
		if !picked[l] {
			remove = append(remove, l)
		}
	}
	return add, remove
}

// assignReviewersCmd applies an add/remove reviewer diff to one PR, then refetches.
func (m Model) assignReviewersCmd(number int, add, remove []string) tea.Cmd {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	delete(m.fresh, number) // reviewer set changed → summary must revalidate
	r, dir := m.runner, m.dir
	args := []string{"pr", "edit", strconv.Itoa(number)}
	if len(add) > 0 {
		args = append(args, "--add-reviewer", strings.Join(add, ","))
	}
	if len(remove) > 0 {
		args = append(args, "--remove-reviewer", strings.Join(remove, ","))
	}
	fetch := m.fetchCmd(m.filter)
	return func() tea.Msg {
		if _, err := r.Run(dir, args...); err != nil {
			return fetchFailedMsg{err: err}
		}
		return fetch()
	}
}

func (m *Model) confirmAnswer(yes bool) tea.Cmd {
	a := m.pending
	m.pending = nil
	if !yes || a == nil {
		return nil
	}
	if a.Scope == "per-selected" {
		return m.runBulk(*a)
	}
	return m.runAction(*a)
}

// bulkWarnThreshold is the selection size above which a worktree fan-out prompts
// first — opening a windowful of worktrees at once is rarely intended.
const bulkWarnThreshold = 4

// startBulk runs a per-selected action, first prompting when it needs confirming
// or when it would open more than bulkWarnThreshold worktrees.
func (m *Model) startBulk(a action.Action) tea.Cmd {
	overThreshold := a.ExitsTUI && len(m.selectedOrCursor()) > bulkWarnThreshold
	if a.Confirm || overThreshold {
		m.pending = &a
		return nil
	}
	return m.runBulk(a)
}

// runBulk applies a per-selected action to each selected row (or the cursor row
// if none selected). Exits-tui actions write one handoff line each and quit;
// inline gh actions run across the selection and settle to an aggregate badge.
func (m *Model) runBulk(a action.Action) tea.Cmd {
	var argvs [][]string
	var nums []int
	for _, i := range m.selectedOrCursor() {
		if i < 0 || i >= m.section.Len() {
			continue
		}
		v := m.section.VarsAt(i)
		v.Repo = m.repo
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			continue
		}
		if a.ExitsTUI {
			m.queueExit(a.Key, argv)
		} else {
			argvs = append(argvs, argv)
			nums = append(nums, v.Number)
		}
	}
	if a.ExitsTUI {
		return tea.Quit
	}
	if len(argvs) == 0 {
		return nil
	}
	n := len(argvs)
	m.actionStatus = statForBulk(a, n)
	m.actionStatus.refresh = a.Refresh
	m.actionStatus.nums = nums
	m.sel.clear() // the batch op consumes the selection
	r, dir := m.runner, m.dir
	return tea.Batch(func() tea.Msg {
		var failed int
		for _, argv := range argvs {
			if _, err := r.Run(dir, argv[1:]...); err != nil { // argv[0]=="gh"
				failed++
			}
		}
		if failed == 0 {
			return actionDoneMsg{}
		}
		return actionDoneMsg{
			err:  fmt.Errorf("%d of %d failed", failed, n),
			fail: fmt.Sprintf("%d of %d failed", failed, n),
		}
	}, m.startSpinner())
}

// statForBulk builds the running status for a bulk action, tagging the running
// and success wording with the item count (×N) when it spans more than one PR.
func statForBulk(a action.Action, n int) *actionStat {
	s := statFor(a)
	if n > 1 {
		s.run = fmt.Sprintf("%s ×%d", s.run, n)
		s.ok = fmt.Sprintf("%s ×%d", s.ok, n)
	}
	return s
}
