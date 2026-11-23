package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

func stubRepoFromGit(t *testing.T, repo string, err error) {
	t.Helper()
	orig := repoFromGit
	repoFromGit = func(string) (string, error) { return repo, err }
	t.Cleanup(func() { repoFromGit = orig })
}

func TestResolveRepoNoRepoStarts(t *testing.T) {
	stubRepoFromGit(t, "", gh.ErrNoRepo)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PRDASH_REPOS_ROOT", t.TempDir())

	repo, err := resolveRepo(t.TempDir())
	if repo != "" || err != nil {
		t.Fatalf("resolveRepo = (%q, %v), want (\"\", nil)", repo, err)
	}

	c := cache.Open(filepath.Join(state, "prdash", "results-cache.json"))
	m := buildModel(t.TempDir(), repo, gh.NewGraphSource("x", repo), c)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := next.View().Content; !strings.Contains(v, "all repos") {
		t.Fatalf("view lacks %q:\n%s", "all repos", v)
	}
}

func TestResolveRepoOtherErrorIsNotice(t *testing.T) {
	stubRepoFromGit(t, "", errors.New("malformed remote"))
	if _, err := resolveRepo(t.TempDir()); err == nil {
		t.Fatal("resolveRepo returned nil error for a non-ErrNoRepo failure")
	}
}
