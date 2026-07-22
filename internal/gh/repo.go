package gh

import (
	"encoding/json"
	"errors"
	"fmt"
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

// CurrentRepo resolves owner/name for dir. It returns ErrNoRepo when dir isn't a
// git repo or has no GitHub remote; auth/network failures surface as the real
// gh error so they can be told apart from "just cd somewhere else".
func CurrentRepo(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "repo", "view", "--json", "nameWithOwner")
	if err != nil {
		if isNoRepo(err) {
			return "", ErrNoRepo
		}
		return "", fmt.Errorf("gh repo view: %w", err)
	}
	return parseRepo(out)
}

// isNoRepo tells a "you're not in a GitHub repo" failure (git absent, or no
// GitHub remote) from auth/network trouble, matching gh/git's stderr wording.
func isNoRepo(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "not a git repository") ||
		strings.Contains(msg, "none of the git remotes") ||
		strings.Contains(msg, "no git remotes found")
}

func parseRepo(b []byte) (string, error) {
	var v struct {
		NameWithOwner string `json:"nameWithOwner"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	if v.NameWithOwner == "" {
		return "", ErrNoRepo
	}
	return v.NameWithOwner, nil
}
