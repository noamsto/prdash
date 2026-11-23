package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/ui"
)

var repoFromGit = gh.RepoFromGit

// resolveRepo returns the launch repo, or "" when dir is outside a GitHub
// checkout (the all-repos board).
func resolveRepo(dir string) (string, error) {
	repo, err := repoFromGit(dir)
	if errors.Is(err, gh.ErrNoRepo) {
		return "", nil
	}
	return repo, err
}

var (
	repoOwnerRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	repoNameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func validRepoFlag(v string) bool {
	owner, name, ok := strings.Cut(v, "/")
	return ok && repoOwnerRe.MatchString(owner) && repoNameRe.MatchString(name) && name != "." && name != ".."
}

// parseRepoFlag extracts --repo from args wherever it appears. Everything else
// is ignored so a launch without --repo behaves exactly as before.
func parseRepoFlag(args []string) (repo string, given bool, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		if !strings.HasPrefix(arg, "-") || name != "repo" {
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return "", true, errors.New("--repo needs a value: owner/name")
			}
			i++
			val = args[i]
		}
		repo, given = val, true
	}
	if given && !validRepoFlag(repo) {
		return "", true, fmt.Errorf("invalid --repo %q: want owner/name", repo)
	}
	return repo, given, nil
}

// launchRepo resolves the launch repo. With a --repo flag the flag wins and
// local reports whether dir is a checkout of it; otherwise it is resolveRepo's.
func launchRepo(dir, flagRepo string) (repo string, local bool, err error) {
	if flagRepo == "" {
		repo, err = resolveRepo(dir)
		return repo, true, err
	}
	cwdRepo, err := resolveRepo(dir)
	return flagRepo, err == nil && strings.EqualFold(cwdRepo, flagRepo), nil
}

func main() {
	dir, _ := os.Getwd()

	flagRepo, pathFree, err := parseRepoFlag(os.Args[1:])
	if err != nil {
		ui.RunNotice("prdash", err.Error()) //nolint:errcheck,gosec // G104: best-effort notice before the process exits
		os.Exit(1)
	}

	repo, local, err := launchRepo(dir, flagRepo)
	if err != nil {
		ui.RunNotice("prdash", fmt.Sprintf("Could not resolve the GitHub repo.\n\n%s", err)) //nolint:errcheck,gosec // G104: best-effort notice before the process exits
		os.Exit(1)
	}

	// prdash talks to GitHub over githubv4/REST, so a token is mandatory. It
	// comes from GH_TOKEN/GITHUB_TOKEN or, failing that, `gh auth token`.
	tok, err := gh.Token()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			ui.RunNotice("prdash", fmt.Sprintf("%s\n\nSet GH_TOKEN or GITHUB_TOKEN to bypass `gh auth token` entirely.", err)) //nolint:errcheck,gosec // G104: best-effort notice before the process exits
		} else {
			ui.RunNotice("prdash", fmt.Sprintf("prdash: %s\n\nSet GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`.", err)) //nolint:errcheck,gosec // G104: best-effort notice before the process exits
		}
		os.Exit(1)
	}

	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	c := cache.Open(filepath.Join(stateDir, "prdash", "results-cache.json"))

	m := buildModel(dir, repo, gh.NewGraphSource(tok, repo), c)
	m.SetLaunchCheckout(local)
	m.SetPathFree(pathFree)

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

// buildModel wires the model for repo ("" for the all-repos board) to gs and c.
func buildModel(dir, repo string, gs gh.GraphSource, c *cache.Cache) ui.Model {
	m := ui.NewModel(dir, "is:open", c)
	m.SetRepo(repo)
	root := os.Getenv("PRDASH_REPOS_ROOT")
	if root == "" {
		root = gh.DefaultReposRoot()
	}
	m.SetReposRoot(root)
	if os.Getenv("PRDASH_FRAME") == "1" {
		m.SetOuterFrame(true)
	}
	m.SetPRSource(gs)
	m.SetDetailSource(gs)
	m.SetStackSource(gs)
	m.SetCrossRepoSource(gs)
	m.SetChecksSource(gs)
	m.SetStateSource(gs)
	m.SetIssueSource(gs)
	m.SetIssueDetailSource(gs)
	m.SetViewerSource(gs)
	m.SetMembersSource(gs)
	m.SetMutationSource(gs)
	m.SetActionsSource(gs)
	m.SetRunsSource(gs)
	m.SetRateSource(gs)
	m.InitTheme()
	m.Hydrate()
	return m
}

// runExit runs one queued exits-TUI command with the terminal attached, so an
// interactive tool (wt switch) can prompt and its tmux hook can navigate.
func runExit(dir string, argv []string) error {
	c := exec.Command(argv[0], argv[1:]...) //nolint:gosec // G204: re-execs this process's own queued argv, not untrusted input
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
