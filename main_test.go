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

func TestParseRepoFlag(t *testing.T) {
	tests := []struct {
		args    []string
		repo    string
		given   bool
		wantErr bool
	}{
		{nil, "", false, false},
		{[]string{"--repo", "o/n"}, "o/n", true, false},
		{[]string{"--repo=o/n"}, "o/n", true, false},
		{[]string{"--bogus", "x", "pos"}, "", false, false},
		{[]string{"--repo", "o/n", "extra", "--other"}, "o/n", true, false},
		{[]string{"--repo", "foo"}, "", true, true},
		{[]string{"--repo=a/b/c"}, "", true, true},
		{[]string{"--repo=/x"}, "", true, true},
		{[]string{"--repo=a/"}, "", true, true},
		{[]string{"--repo="}, "", true, true},
		{[]string{"--repo"}, "", true, true},
		{[]string{"--bogus", "--repo", "o/n"}, "o/n", true, false},
		{[]string{"pos", "--repo", "o/n"}, "o/n", true, false},
		{[]string{"-repo", "o/n"}, "o/n", true, false},
		{[]string{"-repo=o/n"}, "o/n", true, false},
		{[]string{"--repo=../.."}, "", true, true},
		{[]string{"--repo=-x/y"}, "", true, true},
		{[]string{"--repo=a/.."}, "", true, true},
		{[]string{"--", "--repo", "o/n"}, "", false, false},
		{[]string{"--repo", "o/n", "--", "--repo", "x"}, "o/n", true, false},
	}
	for _, tt := range tests {
		repo, given, err := parseRepoFlag(tt.args)
		if repo != tt.repo || given != tt.given || (err != nil) != tt.wantErr {
			t.Errorf("parseRepoFlag(%q) = (%q, %v, %v)", tt.args, repo, given, err)
		}
		if err != nil && !strings.Contains(err.Error(), "owner/name") {
			t.Errorf("error %q lacks owner/name", err)
		}
	}
}

func TestLaunchRepo(t *testing.T) {
	tests := []struct {
		name      string
		git       string
		gitErr    error
		flag      string
		repo      string
		local     bool
		wantError bool
	}{
		{"no flag in checkout", "o/n", nil, "", "o/n", true, false},
		{"no flag outside", "", gh.ErrNoRepo, "", "", true, false},
		{"no flag other error", "", errors.New("bad"), "", "", true, true},
		{"flag matches checkout", "O/N", nil, "o/n", "o/n", true, false},
		{"flag differs", "x/y", nil, "o/n", "o/n", false, false},
		{"flag outside checkout", "", gh.ErrNoRepo, "o/n", "o/n", false, false},
		{"flag with git error", "", errors.New("bad"), "o/n", "o/n", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubRepoFromGit(t, tt.git, tt.gitErr)
			repo, local, err := launchRepo(t.TempDir(), tt.flag)
			if repo != tt.repo || local != tt.local || (err != nil) != tt.wantError {
				t.Errorf("launchRepo = (%q, %v, %v)", repo, local, err)
			}
		})
	}
}

func TestLaunchRepoFlagOutsideCheckout(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PRDASH_REPOS_ROOT", t.TempDir())

	c := cache.Open(filepath.Join(state, "prdash", "results-cache.json"))
	m := buildModel(t.TempDir(), "owner/name", gh.NewGraphSource("x", "owner/name"), c)
	m.SetLaunchCheckout(false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	v := next.View().Content
	if strings.Contains(v, "all repos") || !strings.Contains(v, "owner/name") {
		t.Fatalf("view is not the owner/name repo board:\n%s", v)
	}
}
