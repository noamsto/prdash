package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FetchReviewThreads runs reviewThreadsQuery — the same document the gh CLI path
// passed to `gh api graphql` — directly against the GraphQL endpoint. GitHub's
// response envelope is the `{"data":…}` shape ParseReviewThreads already reads,
// so the raw bytes round-trip through the threads cache unchanged.
func (s GraphSource) FetchReviewThreads(number int) ([]ReviewThread, []byte, error) {
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return nil, nil, fmt.Errorf("bad repo %q", s.repo)
	}
	reqBody, err := json.Marshal(map[string]any{
		"query":     reviewThreadsQuery,
		"variables": map[string]any{"owner": owner, "repo": name, "num": number},
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, githubGraphQLURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("graphql threads: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	ts, err := ParseReviewThreads(body)
	if err != nil {
		return nil, nil, err
	}
	return ts, body, nil
}
