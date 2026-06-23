package ui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/action"
)

// cursorVars returns the template vars for the row under the table cursor, or
// false if the cursor is out of range. Repo is injected from the model.
func (m *Model) cursorVars() (action.Vars, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= m.section.Len() {
		return action.Vars{}, false
	}
	v := m.section.VarsAt(i)
	v.Repo = m.repo
	return v, true
}

// runAction executes a single-scope action against the cursor row. exits-tui
// actions write the handoff file and quit; inline actions run via the runner.
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
		if path := os.Getenv("PRDASH_ACTION_FILE"); path != "" {
			_ = action.AppendHandoff(path, a.Key, argv)
		}
		return tea.Quit
	}

	switch a.Command.Builtin {
	case "copy":
		return func() tea.Msg { print(action.OSC52(v.Branch)); return nil }
	case "rerun-failed":
		r := m.runner
		dir, branch := m.dir, v.HeadRefName
		return func() tea.Msg {
			if err := action.RerunFailed(r, dir, branch); err != nil {
				return fetchFailedMsg{err}
			}
			return nil
		}
	default: // argv (e.g. gh pr merge)
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			return nil
		}
		r := m.runner
		dir := m.dir
		return func() tea.Msg {
			if _, err := r.Run(dir, argv[1:]...); err != nil { // argv[0]=="gh"
				return fetchFailedMsg{err}
			}
			return nil
		}
	}
}

func (m *Model) confirmAnswer(yes bool) tea.Cmd {
	a := m.pending
	m.pending = nil
	if !yes || a == nil {
		return nil
	}
	return m.runAction(*a)
}

// runBulk applies a per-selected action to each selected row (or the cursor row
// if none selected), writing one handoff line each, then quits if exits-tui.
func (m *Model) runBulk(a action.Action) tea.Cmd {
	idx := m.sel.indices()
	if len(idx) == 0 {
		idx = []int{m.table.Cursor()}
	}
	path := os.Getenv("PRDASH_ACTION_FILE")
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
		if a.ExitsTUI && path != "" {
			_ = action.AppendHandoff(path, a.Key, argv)
		}
	}
	if a.ExitsTUI {
		return tea.Quit
	}
	return nil
}
