package gh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseGitHubRemote(t *testing.T) {
	ok := map[string]string{
		"git@github.com:noamsto/prdash.git":                          "noamsto/prdash",
		"git@github.com:noamsto/prdash":                              "noamsto/prdash",
		"https://github.com/noamsto/prdash.git":                      "noamsto/prdash",
		"https://github.com/noamsto/prdash":                          "noamsto/prdash",
		"ssh://git@github.com/noamsto/prdash.git":                    "noamsto/prdash",
		"https://x-access-token:TOKEN@github.com/noamsto/prdash.git": "noamsto/prdash",
		"git@github.com:factify-inc/mono.git\n":                      "factify-inc/mono",
	}
	for in, want := range ok {
		got, ok := parseGitHubRemote(in)
		if !ok || got != want {
			t.Errorf("parseGitHubRemote(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}

	for _, in := range []string{
		"git@gitlab.com:x/y.git",
		"https://example.com/a/b",
		"github.com/onlyowner",
		"",
	} {
		if got, ok := parseGitHubRemote(in); ok {
			t.Errorf("parseGitHubRemote(%q) = %q,true; want _,false", in, got)
		}
	}
}

// fakeBinOnPath writes an executable script named name to a temp dir and
// prepends it to PATH for the test's duration, so exec.Command(name, ...)
// resolves to it instead of (or in place of) the real binary.
func fakeBinOnPath(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRepoFromGitTimesOutOnStalledGit(t *testing.T) {
	old := execTimeout
	execTimeout = 20 * time.Millisecond
	t.Cleanup(func() { execTimeout = old })
	// exec, not a bare sleep: replaces the shell image instead of forking, so
	// this test doesn't depend on newBoundedCmd's WaitDelay to finish quickly
	// (that's defense in depth for the general/forking case, not this one).
	fakeBinOnPath(t, "git", "exec sleep 30\n")

	_, err := RepoFromGit(t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RepoFromGit() = %v, want a deadline-exceeded error", err)
	}
}
