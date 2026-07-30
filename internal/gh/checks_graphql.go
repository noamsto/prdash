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

// checksFields is the narrowest selection that answers "did CI move?": the last
// commit's rollup, nothing else. It deliberately omits every list field the
// board renders — a poll reuses the rows it already has.
const checksFields = "commits(last:1){nodes{commit{statusCheckRollup{contexts(first:100){nodes{__typename" +
	" ... on CheckRun{name conclusion detailsUrl startedAt checkSuite{workflowRun{workflow{name}}}}" +
	" ... on StatusContext{context state targetUrl}}}}}}}"

// FetchChecks fetches just the status-check rollup for every number in one
// aliased request. This is the CI poll's query: a full list refetch costs a
// point per 25 rows per nested connection it selects, while one aliased
// pullRequest per PR costs one point for the whole batch.
func (s GraphSource) FetchChecks(numbers []int) (map[int][]Check, error) {
	if len(numbers) == 0 {
		return map[int][]Check{}, nil
	}
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return nil, fmt.Errorf("bad repo %q", s.repo)
	}
	reqBody, err := json.Marshal(map[string]any{
		"query":     buildChecksQuery(numbers),
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
		return nil, fmt.Errorf("graphql checks: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return parseChecks(body, numbers)
}

// buildChecksQuery aliases one pullRequest(number:) per number under a single
// repository query, mirroring buildDetailQuery. Numbers are ints, so inlining
// them is injection-safe.
func buildChecksQuery(numbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
	for _, n := range numbers {
		fmt.Fprintf(&b, "pr%d:pullRequest(number:%d){%s}", n, n, checksFields)
	}
	b.WriteString("}}")
	return b.String()
}

// qlCheckContext is one statusCheckRollup context in JSON form. CheckRun and
// StatusContext share no field names, so both fragments flatten into one struct
// and __typename says which half is populated.
type qlCheckContext struct {
	Typename   string     `json:"__typename"`
	Name       string     `json:"name"`
	Conclusion string     `json:"conclusion"`
	DetailsURL string     `json:"detailsUrl"`
	StartedAt  *time.Time `json:"startedAt"`
	CheckSuite struct {
		WorkflowRun *struct {
			Workflow struct{ Name string } `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
	Context   string `json:"context"`
	State     string `json:"state"`
	TargetURL string `json:"targetUrl"`
}

type qlChecks struct {
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					Contexts struct {
						Nodes []qlCheckContext `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// parseChecks maps the aliased response back per number. A PR whose rollup is
// null yields an empty (non-nil) slice: "no checks" is an answer the poll must
// be able to apply, distinct from "this PR wasn't in the response".
func parseChecks(body []byte, numbers []int) (map[int][]Check, error) {
	var resp struct {
		Data struct {
			Repository map[string]qlChecks `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse checks: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql checks: %s", resp.Errors[0].Message)
	}
	out := make(map[int][]Check, len(numbers))
	for _, n := range numbers {
		ql, ok := resp.Data.Repository[fmt.Sprintf("pr%d", n)]
		if !ok {
			continue
		}
		out[n] = mapChecks(ql)
	}
	return out, nil
}

// mapChecks flattens the rollup union into []Check, matching the field layout
// mapRollup produces for the list query so a polled row renders identically.
func mapChecks(q qlChecks) []Check {
	checks := []Check{}
	for _, cn := range q.Commits.Nodes {
		rollup := cn.Commit.StatusCheckRollup
		if rollup == nil {
			continue
		}
		for _, n := range rollup.Contexts.Nodes {
			switch n.Typename {
			case "CheckRun":
				c := Check{
					Name:       n.Name,
					Conclusion: n.Conclusion,
					DetailsUrl: n.DetailsURL,
				}
				if n.StartedAt != nil {
					c.StartedAt = n.StartedAt.Format(time.RFC3339)
				}
				if wr := n.CheckSuite.WorkflowRun; wr != nil {
					c.WorkflowName = wr.Workflow.Name
				}
				checks = append(checks, c)
			case "StatusContext":
				checks = append(checks, Check{
					Context:   n.Context,
					State:     n.State,
					TargetUrl: n.TargetURL,
				})
			}
		}
	}
	return checks
}
