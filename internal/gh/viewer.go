package gh

import "strings"

// FetchViewerLogin returns the authenticated user's login. The value is identical
// across every repo on a host, so callers cache it host-scoped and indefinitely.
func FetchViewerLogin(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
