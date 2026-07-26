package gh

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Token returns a GitHub token for api.github.com. It prefers GH_TOKEN /
// GITHUB_TOKEN from the environment — set either (e.g. via your secrets manager)
// for a fully gh-free setup — and otherwise falls back to `gh auth token`, which
// resolves gh's stored token portably on macOS and Linux (keyring or file)
// without prdash having to know where gh keeps it.
func Token() (string, error) {
	for _, env := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := strings.TrimSpace(os.Getenv(env)); t != "" {
			return t, nil
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("no GH_TOKEN/GITHUB_TOKEN set and `gh auth token` failed: %w", err)
	}
	t := strings.TrimSpace(string(out))
	if t == "" {
		return "", errors.New("no GH_TOKEN/GITHUB_TOKEN set and `gh auth token` returned empty")
	}
	return t, nil
}
