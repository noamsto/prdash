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

// clipboardText is the payload an OSC52 copy action writes for one PR.
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
		return func() tea.Msg { print(action.OSC52(text)); return nil }
	case "rerun-failed":
		r, dir, branch, label := m.runner, m.dir, v.HeadRefName, a.Label
		m.actionStatus = &actionStat{label: label}
		return tea.Batch(func() tea.Msg {
			return actionDoneMsg{label: label, err: action.RerunFailed(r, dir, branch)}
		}, m.startSpinner())
	default: // argv (e.g. gh pr merge)
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			return nil
		}
		r, dir, label := m.runner, m.dir, a.Label
		m.actionStatus = &actionStat{label: label}
		return tea.Batch(func() tea.Msg {
			_, err := r.Run(dir, argv[1:]...) // argv[0]=="gh"
			return actionDoneMsg{label: label, err: err}
		}, m.startSpinner())
	}
}

// actionStat is an inline action's transient progress, surfaced by the header.
type actionStat struct {
	label string
	done  bool
	err   error
}

// actionRunning reports whether an inline action is still in flight.
func (m Model) actionRunning() bool {
	return m.actionStatus != nil && !m.actionStatus.done
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

// startBulk runs a per-selected action, first prompting when it would open more
// than bulkWarnThreshold worktrees.
func (m *Model) startBulk(a action.Action) tea.Cmd {
	if a.ExitsTUI && len(m.selectedOrCursor()) > bulkWarnThreshold {
		m.pending = &a
		return nil
	}
	return m.runBulk(a)
}

// runBulk applies a per-selected action to each selected row (or the cursor row
// if none selected), writing one handoff line each, then quits if exits-tui.
func (m *Model) runBulk(a action.Action) tea.Cmd {
	idx := m.sel.indices()
	if len(idx) == 0 {
		idx = []int{m.cursor}
	}
	for _, i := range idx {
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
		}
	}
	if a.ExitsTUI {
		return tea.Quit
	}
	return nil
}
