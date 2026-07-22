package gh

import "context"

// FetchViewer returns the authenticated user's login via the typed githubv4
// client, replacing `gh api user --jq .login`.
func (s GraphSource) FetchViewer() (string, error) {
	var q struct {
		Viewer struct {
			Login string
		}
	}
	if err := s.client.Query(context.Background(), &q, nil); err != nil {
		return "", err
	}
	return q.Viewer.Login, nil
}
