package gh

import (
	"encoding/json"
	"errors"
)

var ErrNoRepo = errors.New("not in a GitHub repo")

// CurrentRepo resolves owner/name for dir, or ErrNoRepo.
func CurrentRepo(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "repo", "view", "--json", "nameWithOwner")
	if err != nil {
		return "", ErrNoRepo
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
