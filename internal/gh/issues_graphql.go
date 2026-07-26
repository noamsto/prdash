package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"
)

func (s GraphSource) FetchIssues(filter string, limit int) ([]Issue, []byte, error) {
	issues, err := s.queryIssues(context.Background(), filter, limit)
	if err != nil {
		return nil, nil, err
	}
	// The cache stores the marshalled []Issue; the hydrate path (json.Unmarshal
	// into []Issue) round-trips it back unchanged.
	raw, err := json.Marshal(issues)
	if err != nil {
		return nil, nil, err
	}
	return issues, raw, nil
}

// qlIssue is the githubv4 shape of one search result, covering exactly the
// fields mapped into gh.Issue.
type qlIssue struct {
	Number    int
	Title     string
	URL       string `graphql:"url"`
	UpdatedAt githubv4.DateTime
	Author    struct {
		Login    string
		Typename string `graphql:"__typename"`
	}
	Labels struct {
		Nodes []struct{ Name, Color string }
	} `graphql:"labels(first: 20)"`
	Assignees struct {
		Nodes []struct{ Login string }
	} `graphql:"assignees(first: 20)"`
}

func (s GraphSource) queryIssues(ctx context.Context, filter string, limit int) ([]Issue, error) {
	var q struct {
		Search struct {
			Nodes []struct {
				Issue qlIssue `graphql:"... on Issue"`
			}
		} `graphql:"search(query: $q, type: ISSUE, first: $limit)"`
	}
	// gh issue list --search implicitly scopes to the repo and to issues; the
	// raw search API needs both qualifiers spelled out.
	vars := map[string]any{
		"q":     githubv4.String(fmt.Sprintf("repo:%s is:issue %s", s.repo, filter)),
		"limit": githubv4.Int(limit),
	}
	if err := s.client.Query(ctx, &q, vars); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(q.Search.Nodes))
	for _, n := range q.Search.Nodes {
		issues = append(issues, mapIssue(n.Issue))
	}
	return issues, nil
}

func mapIssue(g qlIssue) Issue {
	i := Issue{
		Number:    g.Number,
		Title:     g.Title,
		URL:       g.URL,
		UpdatedAt: g.UpdatedAt.Time,
	}
	// gh reports GitHub App actors (dependabot, etc.) with an "app/" prefix;
	// the GraphQL Bot actor's login has none, so add it to match.
	i.Author.Login = g.Author.Login
	if g.Author.Typename == "Bot" && g.Author.Login != "" {
		i.Author.Login = "app/" + g.Author.Login
	}
	for _, l := range g.Labels.Nodes {
		i.Labels = append(i.Labels, Label{Name: l.Name, Color: l.Color})
	}
	for _, a := range g.Assignees.Nodes {
		i.Assignees = append(i.Assignees, struct {
			Login string `json:"login"`
		}{Login: a.Login})
	}
	return i
}

// FetchIssueDetail mirrors `gh issue view --json body`.
func (s GraphSource) FetchIssueDetail(number int) (IssueDetail, []byte, error) {
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return IssueDetail{}, nil, fmt.Errorf("bad repo %q", s.repo)
	}
	var q struct {
		Repository struct {
			Issue struct {
				Body string
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]any{
		"owner":  githubv4.String(owner),
		"name":   githubv4.String(name),
		"number": githubv4.Int(number),
	}
	if err := s.client.Query(context.Background(), &q, vars); err != nil {
		return IssueDetail{}, nil, err
	}
	d := IssueDetail{Body: q.Repository.Issue.Body}
	raw, err := json.Marshal(d)
	if err != nil {
		return IssueDetail{}, nil, err
	}
	return d, raw, nil
}
