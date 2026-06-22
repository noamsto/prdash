package gh

import (
	"encoding/json"
	"errors"
	"fmt"
)

var ErrNoRepo = errors.New("not in a GitHub repo")

// CurrentRepo resolves owner/name for dir. It returns ErrNoRepo only when dir
// genuinely isn't a repo; auth/network failures surface as the real gh error.
func CurrentRepo(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "repo", "view", "--json", "nameWithOwner")
	if err != nil {
		return "", fmt.Errorf("gh repo view: %w", err)
	}
	return parseRepo(out)
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
