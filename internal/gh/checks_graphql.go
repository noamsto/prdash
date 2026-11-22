package gh

import (
	"encoding/json"
	"time"
)

// checksFields is the narrowest selection that answers "did CI move?": the last
// commit's rollup, nothing else. It deliberately omits every list field the
// board renders — a poll reuses the rows it already has.
const checksFields = "commits(last:1){nodes{commit{statusCheckRollup{contexts(first:100){nodes{__typename" +
	" ... on CheckRun{name conclusion detailsUrl startedAt completedAt checkSuite{workflowRun{workflow{name}}}}" +
	" ... on StatusContext{context state targetUrl}}}}}}}"

// checksSpec batches the CI poll: one aliased pullRequest per PR costs one
// point for the whole batch, while a full list refetch costs a point per 25 rows
// per nested connection it selects. 50 PRs × 100 contexts stays far below the
// node limit.
var checksSpec = batchSpec{
	label: "checks", aliasPrefix: "pr", field: "pullRequest",
	selection: checksFields, maxPRs: 50, strict: true,
}

// FetchChecks fetches just the status-check rollup, batching every repo into
// one aliased request per chunk. A failing repo leaves the others' results in
// the map and joins its error into err.
func (s GraphSource) FetchChecks(refs []PRRef) (map[PRRef][]Check, error) {
	out := make(map[PRRef][]Check, len(refs))
	err := s.fetchAliased(checksSpec, refs, func(ref PRRef, raw json.RawMessage) error {
		var ql qlChecks
		if err := json.Unmarshal(raw, &ql); err != nil {
			return err
		}
		out[ref] = mapChecks(ql)
		return nil
	})
	return out, err
}

// qlCheckContext is one statusCheckRollup context in JSON form. CheckRun and
// StatusContext share no field names, so both fragments flatten into one struct
// and __typename says which half is populated.
type qlCheckContext struct {
	Typename    string     `json:"__typename"`
	Name        string     `json:"name"`
	Conclusion  string     `json:"conclusion"`
	DetailsURL  string     `json:"detailsUrl"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	CheckSuite  struct {
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
				if n.CompletedAt != nil {
					c.CompletedAt = n.CompletedAt.Format(time.RFC3339)
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
	sanitize(&checks)
	return checks
}
