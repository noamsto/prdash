package main

import (
	"context"
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

	repo, err := gh.RepoFromGit(dir)
	if err != nil {
		body := "Not inside a GitHub repository.\n\ncd into a repo with a github.com origin remote, then run prdash again."
		if !errors.Is(err, gh.ErrNoRepo) {
			body = fmt.Sprintf("Could not resolve the GitHub repo.\n\n%s", err)
		}
		ui.RunNotice("prdash", body)
		os.Exit(1)
	}

	// prdash talks to GitHub over githubv4/REST, so a token is mandatory. It
	// comes from GH_TOKEN/GITHUB_TOKEN or, failing that, `gh auth token`.
	tok, err := gh.Token()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			ui.RunNotice("prdash", fmt.Sprintf("%s\n\nSet GH_TOKEN or GITHUB_TOKEN to bypass `gh auth token` entirely.", err))
		} else {
			fmt.Fprintln(os.Stderr, "prdash:", err)
			fmt.Fprintln(os.Stderr, "Set GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`.")
		}
		os.Exit(1)
	}

	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	c := cache.Open(filepath.Join(stateDir, "prdash", "results-cache.json"))

	m := ui.NewModel(dir, "is:open", c)
	m.SetRepo(repo)
	if os.Getenv("PRDASH_FRAME") == "1" {
		m.SetOuterFrame(true)
	}
	gs := gh.NewGraphSource(tok, repo)
	m.SetPRSource(gs)
	m.SetDetailSource(gs)
	m.SetChecksSource(gs)
	m.SetIssueSource(gs)
	m.SetIssueDetailSource(gs)
	m.SetViewerSource(gs)
	m.SetMembersSource(gs)
	m.SetMutationSource(gs)
	m.SetActionsSource(gs)
	m.SetRateSource(gs)
	m.InitTheme()
	m.Hydrate()

	final, err := tea.NewProgram(m).Run()
	c.Flush() // persist any debounced cache writes before we exit
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
