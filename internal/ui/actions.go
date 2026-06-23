package ui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func (m *Model) varsFor(p gh.PR) action.Vars {
	return action.Vars{
		Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login,
		Repo: m.repo, Branch: p.HeadRefName,
	}
}

// cursorPR returns the PR under the table cursor within the currently shown
// (filtered) slice, or nil if out of range.
func (m *Model) cursorPR() *gh.PR {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.shown) {
		return nil
	}
	return &m.shown[i]
}

// runAction executes a single-scope action against the cursor PR. exits-tui
// actions write the handoff file and quit; inline actions run via the runner.
func (m *Model) runAction(a action.Action) tea.Cmd {
	cur := m.cursorPR()
	if cur == nil {
		return nil
	}
	v := m.varsFor(*cur)

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
