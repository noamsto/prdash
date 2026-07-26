package gh

import (
	"errors"
	"os/exec"
	"strings"
)

var ErrNoRepo = errors.New("not in a GitHub repo")

// RepoFromGit resolves owner/name from the origin remote of the git repo at dir,
// without invoking gh. Returns ErrNoRepo when dir isn't a git repo or its origin
// isn't a github.com remote. (git — not gh — is a legitimate dependency: prdash
// only runs inside a repo.)
func RepoFromGit(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", ErrNoRepo
	}
	repo, ok := parseGitHubRemote(string(out))
	if !ok {
		return "", ErrNoRepo
	}
	return repo, nil
}

// parseGitHubRemote extracts owner/name from a github.com remote URL in any of
// git's forms: git@github.com:owner/repo.git, ssh://git@github.com/owner/repo,
// https://github.com/owner/repo.git (optionally with credentials).
func parseGitHubRemote(remote string) (string, bool) {
	s := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	i := strings.Index(s, "github.com")
	if i < 0 {
		return "", false
	}
	rest := strings.TrimLeft(s[i+len("github.com"):], ":/")
	owner, name, ok := strings.Cut(rest, "/")
	if !ok || owner == "" || name == "" {
		return "", false
	}
	if j := strings.IndexByte(name, '/'); j >= 0 {
		name = name[:j]
	}
	return owner + "/" + name, true
}
