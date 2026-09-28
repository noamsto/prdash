package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// statesFields is the narrowest selection that answers "what is this row's
// real state now?": the state plus the timestamps a held row's tag and sort
// key need, nothing else.
const statesFields = "__typename ... on PullRequest{state mergedAt closedAt} ... on Issue{state closedAt}"

// FetchStates looks up the current state of every number in one aliased
// request. This is the held-row lookup: a departed or stale row's real state
// without a full list refetch.
func (s GraphSource) FetchStates(numbers []int) (map[int]ItemState, error) {
	if len(numbers) == 0 {
		return map[int]ItemState{}, nil
	}
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return nil, fmt.Errorf("bad repo %q", s.repo)
	}
	reqBody, err := json.Marshal(map[string]any{
		"query":     buildStatesQuery(numbers),
		"variables": map[string]string{"owner": owner, "name": name},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, githubGraphQLURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("graphql states: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return parseStates(body, numbers)
}

// buildStatesQuery aliases one issueOrPullRequest(number:) per number under a
// single repository query, mirroring buildChecksQuery. Numbers are ints, so
// inlining them is injection-safe.
func buildStatesQuery(numbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
	for _, n := range numbers {
		fmt.Fprintf(&b, "n%d:issueOrPullRequest(number:%d){%s}", n, n, statesFields)
	}
	b.WriteString("}}")
	return b.String()
}

// qlState is one issueOrPullRequest lookup result. PR and Issue share no
// field names beyond state, so both fragments flatten into one struct and
// __typename says which half is populated.
type qlState struct {
	Typename string     `json:"__typename"`
	State    string     `json:"state"`
	MergedAt *time.Time `json:"mergedAt"`
	ClosedAt *time.Time `json:"closedAt"`
}

// parseStates maps the aliased response back per number. An alias resolving
// to null (deleted, transferred) is simply absent from the result. Partial
// GraphQL errors alongside data still yield the numbers that resolved; it
// errors only when errors is present and repository is null/absent.
func parseStates(body []byte, numbers []int) (map[int]ItemState, error) {
	var resp struct {
		Data struct {
			Repository map[string]*qlState `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse states: %w", err)
	}
	if len(resp.Errors) > 0 && resp.Data.Repository == nil {
		return nil, fmt.Errorf("graphql states: %s", resp.Errors[0].Message)
	}
	out := make(map[int]ItemState, len(numbers))
	for _, n := range numbers {
		ql, ok := resp.Data.Repository[fmt.Sprintf("n%d", n)]
		if !ok || ql == nil {
			continue
		}
		st := ItemState{State: ql.State}
		if ql.MergedAt != nil {
			st.MergedAt = *ql.MergedAt
		}
		if ql.ClosedAt != nil {
			st.ClosedAt = *ql.ClosedAt
		}
		out[n] = st
	}
	return out, nil
}
