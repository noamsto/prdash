package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

const githubGraphQLURL = "https://api.github.com/graphql"

// graphTimeout bounds every githubv4 request so a stalled network call surfaces
// as a fetch error instead of hanging the UI on "Loading…".
const graphTimeout = 20 * time.Second

// GraphSource fetches PR data straight from GitHub's GraphQL API, skipping the
// per-call `gh` subprocess. It implements both PRSource (list) and DetailSource
// (batched per-PR detail). repo is owner/name.
type GraphSource struct {
	repo   string
	http   *http.Client     // for raw aliased detail queries
	client *githubv4.Client // for the typed list query
}

// NewGraphSource builds a GraphSource authenticated with token, as returned by
// `gh auth token`.
func NewGraphSource(token, repo string) GraphSource {
	hc := oauth2.NewClient(context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	hc.Timeout = graphTimeout
	return GraphSource{repo: repo, http: hc, client: githubv4.NewClient(hc)}
}

func (s GraphSource) FetchPRs(filter string, limit int) ([]PR, []byte, error) {
	prs, err := s.query(context.Background(), filter, limit)
	if err != nil {
		return nil, nil, err
	}
	// Cache bytes mirror `gh pr list --json` output so the hydrate path
	// (ParsePRs) round-trips whichever source wrote the entry.
	raw, err := json.Marshal(prs)
	if err != nil {
		return nil, nil, err
	}
	return prs, raw, nil
}

// qlPR is the githubv4 shape of one search result, covering exactly the fields
// in prFields. The statusCheckRollup union is flattened into []Check by mapPR.
type qlPR struct {
	Number         int
	Title          string
	Body           string
	URL            string `graphql:"url"`
	State          string
	IsDraft        bool
	HeadRefName    string
	BaseRefName    string
	ReviewDecision string
	UpdatedAt      githubv4.DateTime
	MergedAt       *githubv4.DateTime
	ClosedAt       *githubv4.DateTime
	Author         struct {
		Login    string
		Typename string `graphql:"__typename"`
	}
	AutoMergeRequest *struct{ MergeMethod string }
	Labels           struct {
		Nodes []struct{ Name, Color string }
	} `graphql:"labels(first: 20)"`
	Assignees struct {
		Nodes []struct{ Login string }
	} `graphql:"assignees(first: 20)"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *qlRollup
			}
		}
	} `graphql:"commits(last: 1)"`
}

type qlRollup struct {
	Contexts struct {
		Nodes []qlCheckNode
	} `graphql:"contexts(first: 100)"`
}

// qlCheckNode is one entry of the statusCheckRollup union. githubv4 populates
// whichever inline fragment matched; __typename says which.
type qlCheckNode struct {
	Typename string `graphql:"__typename"`
	CheckRun struct {
		Name       string
		Conclusion string
		DetailsURL string `graphql:"detailsUrl"`
		StartedAt  *githubv4.DateTime
		CheckSuite struct {
			WorkflowRun *struct {
				Workflow struct{ Name string }
			}
		}
	} `graphql:"... on CheckRun"`
	StatusContext struct {
		Context   string
		State     string
		TargetURL string `graphql:"targetUrl"`
	} `graphql:"... on StatusContext"`
}

func (s GraphSource) query(ctx context.Context, filter string, limit int) ([]PR, error) {
	var q struct {
		Search struct {
			Nodes []struct {
				PR qlPR `graphql:"... on PullRequest"`
			}
		} `graphql:"search(query: $q, type: ISSUE, first: $limit)"`
	}
	// gh pr list --search implicitly scopes to the repo and to PRs; the raw
	// search API needs both qualifiers spelled out.
	vars := map[string]any{
		"q":     githubv4.String(fmt.Sprintf("repo:%s is:pr %s", s.repo, filter)),
		"limit": githubv4.Int(limit),
	}
	if err := s.client.Query(ctx, &q, vars); err != nil {
		return nil, err
	}
	prs := make([]PR, 0, len(q.Search.Nodes))
	for _, n := range q.Search.Nodes {
		prs = append(prs, mapPR(n.PR))
	}
	return prs, nil
}

func mapPR(g qlPR) PR {
	p := PR{
		Number:         g.Number,
		Title:          g.Title,
		Body:           g.Body,
		URL:            g.URL,
		State:          g.State,
		IsDraft:        g.IsDraft,
		HeadRefName:    g.HeadRefName,
		BaseRefName:    g.BaseRefName,
		ReviewDecision: g.ReviewDecision,
		UpdatedAt:      g.UpdatedAt.Time,
	}
	// gh reports GitHub App actors (dependabot, etc.) with an "app/" prefix;
	// the GraphQL Bot actor's login has none, so add it to match.
	p.Author.Login = g.Author.Login
	if g.Author.Typename == "Bot" && g.Author.Login != "" {
		p.Author.Login = "app/" + g.Author.Login
	}
	if g.MergedAt != nil {
		p.MergedAt = g.MergedAt.Time
	}
	if g.ClosedAt != nil {
		p.ClosedAt = g.ClosedAt.Time
	}
	if g.AutoMergeRequest != nil {
		p.AutoMergeRequest = &AutoMergeRequest{MergeMethod: g.AutoMergeRequest.MergeMethod}
	}
	for _, l := range g.Labels.Nodes {
		p.Labels = append(p.Labels, Label{Name: l.Name, Color: l.Color})
	}
	for _, a := range g.Assignees.Nodes {
		p.Assignees = append(p.Assignees, struct {
			Login string `json:"login"`
		}{Login: a.Login})
	}
	p.StatusCheckRollup = mapRollup(g)
	return p
}

// mapRollup flattens the last commit's rollup union into []Check, matching the
// field layout `gh pr list --json statusCheckRollup` emits: CheckRuns carry a
// name/conclusion/workflowName, StatusContexts a context/state — the two halves
// Check.Result and Check.Label already switch on.
func mapRollup(g qlPR) []Check {
	var checks []Check
	for _, cn := range g.Commits.Nodes {
		rollup := cn.Commit.StatusCheckRollup
		if rollup == nil {
			continue
		}
		for _, n := range rollup.Contexts.Nodes {
			switch n.Typename {
			case "CheckRun":
				c := Check{
					Name:       n.CheckRun.Name,
					Conclusion: n.CheckRun.Conclusion,
					DetailsUrl: n.CheckRun.DetailsURL,
				}
				if n.CheckRun.StartedAt != nil {
					c.StartedAt = n.CheckRun.StartedAt.Format(time.RFC3339)
				}
				if wr := n.CheckRun.CheckSuite.WorkflowRun; wr != nil {
					c.WorkflowName = wr.Workflow.Name
				}
				checks = append(checks, c)
			case "StatusContext":
				checks = append(checks, Check{
					Context:   n.StatusContext.Context,
					State:     n.StatusContext.State,
					TargetUrl: n.StatusContext.TargetURL,
				})
			}
		}
	}
	return checks
}
