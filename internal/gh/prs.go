package gh

import (
	"strings"
	"time"
)

type Check struct {
	State        string `json:"state"`
	Conclusion   string `json:"conclusion"`
	Name         string `json:"name"`         // CheckRun
	WorkflowName string `json:"workflowName"` // CheckRun
	Context      string `json:"context"`      // StatusContext (no name)
	DetailsUrl   string `json:"detailsUrl"`   // CheckRun: …/actions/runs/<run>/job/<job>
	TargetUrl    string `json:"targetUrl"`    // StatusContext: external CI's page (CheckRuns leave this empty)
	StartedAt    string `json:"startedAt"`    // CheckRun start time (RFC3339); newest run wins on dedup
}

// URL is the check's web page: a CheckRun's detailsUrl, or an external
// StatusContext's targetUrl (the two halves of the union expose it under
// different fields).
func (c Check) URL() string {
	if c.DetailsUrl != "" {
		return c.DetailsUrl
	}
	return c.TargetUrl
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

// IsExternal reports whether this check is a StatusContext (external CI) rather
// than a GitHub Actions CheckRun. External checks have no name/workflow and no
// job log — only a targetUrl (see URL) to open in the browser.
func (c Check) IsExternal() bool {
	return c.Name == "" && c.WorkflowName == ""
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"` // 6-hex, no leading '#'
}

// AutoMergeRequest mirrors GitHub's non-null-when-armed autoMergeRequest
// object; only the merge method is currently surfaced in the UI.
type AutoMergeRequest struct {
	MergeMethod string `json:"mergeMethod"` // MERGE | SQUASH | REBASE
}

type PR struct {
	// ID is the GraphQL node ID (e.g. "PR_kwDOA..."), populated only on the
	// GraphSource list path — every mutation input needs it as pullRequestId.
	// Empty on the gh-CLI path, which mutates by number instead.
	ID     string `json:"id"`
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
	HeadRefName      string            `json:"headRefName"`
	BaseRefName      string            `json:"baseRefName"`
	URL              string            `json:"url"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	MergedAt         time.Time         `json:"mergedAt"` // zero unless State == MERGED
	ClosedAt         time.Time         `json:"closedAt"` // zero while OPEN; set for MERGED and CLOSED
	IsDraft          bool              `json:"isDraft"`
	State            string            `json:"state"` // OPEN | CLOSED | MERGED
	Body             string            `json:"body"`
	AutoMergeRequest *AutoMergeRequest `json:"autoMergeRequest"`
}

// IsMerged reports whether the PR landed — its status mark and color differ from
// an open PR's CI rollup (merged is terminal; the checks no longer matter).
func (p PR) IsMerged() bool { return p.State == "MERGED" }

// AutoMergeEnabled reports whether GitHub auto-merge is armed on this PR —
// autoMergeRequest is null unless a merge has been queued to land once checks
// and reviews are satisfied. Gated to OPEN because the field can linger
// non-null after merge/close, which would otherwise contradict a terminal
// PR's own merged/closed state everywhere this is displayed.
func (p PR) AutoMergeEnabled() bool { return p.State == "OPEN" && p.AutoMergeRequest != nil }

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
