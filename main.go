package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/ui"
)

func main() {
	dir, _ := os.Getwd()
	runner := gh.ExecRunner{}

	repo, err := gh.CurrentRepo(runner, dir)
	if err != nil {
		if errors.Is(err, gh.ErrNoRepo) {
			ui.RunNotice("prdash", "Not inside a GitHub repository.\n\ncd into a repo with a GitHub remote, then run prdash again.")
		} else {
			ui.RunNotice("prdash", "Couldn't reach GitHub:\n\n"+err.Error())
		}
		os.Exit(1)
	}

	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	c := cache.Open(filepath.Join(stateDir, "prdash", "results-cache.json"))

	m := ui.NewModel(dir, "is:open author:@me", c)
	m.SetRunner(runner)
	m.SetRepo(repo)
	m.InitTheme()
	m.Hydrate()

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Standalone fallback: with no orchestrator handoff sink, exits-TUI actions
	// (open worktree) queue their command here to run once the alt-screen is gone.
	if fm, ok := final.(ui.Model); ok {
		for _, argv := range fm.PendingExec() {
			if err := runExit(dir, argv); err != nil {
				fmt.Fprintln(os.Stderr, "prdash:", err)
			}
		}
	}
}

// runExit runs one queued exits-TUI command with the terminal attached, so an
// interactive tool (wt switch) can prompt and its tmux hook can navigate.
func runExit(dir string, argv []string) error {
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
