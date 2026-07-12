package gh

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrNoRepo = errors.New("not in a GitHub repo")

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
