package gh

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

var prFields = []string{
	"number", "title", "author", "statusCheckRollup", "reviewDecision",
	"labels", "assignees", "headRefName", "baseRefName", "url", "updatedAt", "isDraft", "state",
}

type Check struct {
	State        string `json:"state"`
	Conclusion   string `json:"conclusion"`
	Name         string `json:"name"`         // CheckRun
	WorkflowName string `json:"workflowName"` // CheckRun
	Context      string `json:"context"`      // StatusContext (no name)
	DetailsUrl   string `json:"detailsUrl"`   // CheckRun: …/actions/runs/<run>/job/<job>
	StartedAt    string `json:"startedAt"`    // CheckRun start time (RFC3339); newest run wins on dedup
}

// JobID extracts the Actions job ID from DetailsUrl so a single check can be
// rerun (gh run rerun --job). Empty for external StatusContext checks, which
// carry a targetUrl with no /job/ segment and aren't job-rerunnable.
func (c Check) JobID() string {
	_, after, ok := strings.Cut(c.DetailsUrl, "/job/")
	if !ok {
		return ""
	}
	return after
}

// Label is the display name for a check, handling the CheckRun/StatusContext union.
func (c Check) Label() string {
	switch {
	case c.Name != "":
		return c.Name
	case c.WorkflowName != "":
		return c.WorkflowName
	default:
		return c.Context
	}
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"` // 6-hex, no leading '#'
}

type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	ReviewDecision    string  `json:"reviewDecision"`
	StatusCheckRollup []Check `json:"statusCheckRollup"`
	Labels            []Label `json:"labels"`
	Assignees         []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	HeadRefName string    `json:"headRefName"`
	BaseRefName string    `json:"baseRefName"`
	URL         string    `json:"url"`
	UpdatedAt   time.Time `json:"updatedAt"`
	IsDraft     bool      `json:"isDraft"`
	State       string    `json:"state"` // OPEN | CLOSED | MERGED
}

// IsMerged reports whether the PR landed — its status mark and color differ from
// an open PR's CI rollup (merged is terminal; the checks no longer matter).
func (p PR) IsMerged() bool { return p.State == "MERGED" }

func PRListArgs(filter string, limit int) []string {
	return []string{
		"pr", "list", "--search", filter,
		"-L", strconv.Itoa(limit), "--json", strings.Join(prFields, ","),
	}
}

func FetchPRs(r Runner, dir, filter string, limit int) ([]PR, error) {
	out, err := r.Run(dir, PRListArgs(filter, limit)...)
	if err != nil {
		return nil, err
	}
	return ParsePRs(out)
}

func ParsePRs(b []byte) ([]PR, error) {
	var prs []PR
	if err := json.Unmarshal(b, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// Checks dedupes the rollup to one entry per named check, keeping the most
// recently started run — GitHub lists every re-run, which otherwise shows the
// same check (and inflates the failing count) twice. Unnamed StatusContext
// entries are never merged.
func (p PR) Checks() []Check {
	idx := map[string]int{}
	var out []Check
	for _, c := range p.StatusCheckRollup {
		l := c.Label()
		if l != "" {
			if i, ok := idx[l]; ok {
				if c.StartedAt > out[i].StartedAt {
					out[i] = c
				}
				continue
			}
			idx[l] = len(out)
		}
		out = append(out, c)
	}
	return out
}

func (p PR) CIState() string {
	if len(p.StatusCheckRollup) == 0 {
		return "none"
	}
	pending, failed := false, false
	for _, c := range p.StatusCheckRollup {
		switch c.Result() {
		case "fail":
			failed = true
		case "pending":
			pending = true
		}
	}
	switch {
	case failed:
		return "fail"
	case pending:
		return "pending"
	default:
		return "pass"
	}
}

// Result collapses one check to pass/fail/pending, resolving state→conclusion.
func (c Check) Result() string {
	s := c.State
	if s == "" {
		s = c.Conclusion
	}
	switch s {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
		return "fail"
	case "PENDING", "QUEUED", "IN_PROGRESS", "":
		return "pending"
	default:
		return "pass"
	}
}
