package gh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNoRepo = errors.New("not in a GitHub repo")

// RepoFromGit resolves owner/name from the origin remote of the git repo at dir,
// without invoking gh. Returns ErrNoRepo when dir isn't a git repo or its origin
// isn't a github.com remote, or a distinct error if the underlying git call times
// out or otherwise fails to run. (git — not gh — is a legitimate dependency:
// prdash only runs inside a repo.)
func RepoFromGit(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := newBoundedCmd(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git remote get-url origin timed out after %s: %w", execTimeout, ctx.Err())
		}
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
	_, after, found := strings.Cut(s, "github.com")
	if !found {
		return "", false
	}
	rest := strings.TrimLeft(after, ":/")
	owner, name, ok := strings.Cut(rest, "/")
	if !ok || owner == "" || name == "" {
		return "", false
	}
	if j := strings.IndexByte(name, '/'); j >= 0 {
		name = name[:j]
	}
	return owner + "/" + name, true
}

// DefaultReposRoot is the directory under which checkouts are looked up by owner/name.
func DefaultReposRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "git")
}

// CheckoutError reports that no matching local checkout of Repo exists at Path.
// Origin is the repo the checkout at Path actually points at, if it is a different one.
// Err is why Path's origin could not be read, other than it having no GitHub origin.
type CheckoutError struct {
	Repo   string
	Path   string
	Origin string
	Err    error
}

func (e *CheckoutError) Error() string {
	switch {
	case e.Err != nil:
		return fmt.Sprintf("cannot read origin of %s: %v", e.Path, e.Err)
	case e.Origin == "":
		return fmt.Sprintf("no local checkout of %s at %s", e.Repo, e.Path)
	}
	return fmt.Sprintf("%s is a checkout of %s, not %s", e.Path, e.Origin, e.Repo)
}

func (e *CheckoutError) Unwrap() error { return e.Err }

// CheckoutFor returns the path of the checkout of repo (owner/name) under root,
// which must have a github origin for repo. The origin match is case-insensitive.
func CheckoutFor(root, repo string) (string, error) {
	owner, name, _ := strings.Cut(repo, "/")
	dir := filepath.Join(root, owner, name)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", &CheckoutError{Repo: repo, Path: dir}
	}
	got, err := RepoFromGit(dir)
	if errors.Is(err, ErrNoRepo) {
		return "", &CheckoutError{Repo: repo, Path: dir}
	}
	if err != nil {
		return "", &CheckoutError{Repo: repo, Path: dir, Err: err}
	}
	if !strings.EqualFold(got, repo) {
		return "", &CheckoutError{Repo: repo, Path: dir, Origin: got}
	}
	return dir, nil
}
