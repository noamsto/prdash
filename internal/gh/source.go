package gh

// PRSource fetches a PR list for a search filter, returning the parsed PRs and
// the JSON bytes to persist in the on-disk cache. It is the seam the refresh
// path fetches through, so the gh-CLI and githubv4 backends are swappable and
// A/B-testable without the UI knowing which one is live. The []byte must
// round-trip through ParsePRs so a cache entry written by either source stays
// readable by the other.
type PRSource interface {
	FetchPRs(filter string, limit int) (prs []PR, raw []byte, err error)
}

// DetailSource fetches per-PR detail for a set of PR numbers in one shot,
// returning the mapped details plus the JSON bytes to cache per number (which
// must round-trip through the detail hydrate path). A batched backend collapses
// the prefetch window's N `gh pr view` subprocesses into a single request.
type DetailSource interface {
	FetchDetails(numbers []int) (details map[int]PRDetail, raw map[int][]byte, err error)
}

// IssueSource fetches an issue list for a search filter, mirroring PRSource:
// the []byte must round-trip through ParseIssues so a cache entry written by
// either backend stays readable by the other.
type IssueSource interface {
	FetchIssues(filter string, limit int) (issues []Issue, raw []byte, err error)
}

// IssueDetailSource fetches a single issue's detail (currently just Body). It
// isn't batched like DetailSource — issue detail is already fetched lazily one
// at a time, so there's no N-subprocess cost to collapse.
type IssueDetailSource interface {
	FetchIssueDetail(number int) (detail IssueDetail, raw []byte, err error)
}

// ViewerSource fetches the authenticated user's login, mirroring
// `gh api user --jq .login`.
type ViewerSource interface {
	FetchViewer() (login string, err error)
}

// MembersSource fetches the assignable-users list for the current repo. The
// []byte must round-trip through the members cache hydrate path (which
// unmarshals directly into []User) so a cache entry written by either backend
// stays readable by the other.
type MembersSource interface {
	FetchAssignableUsers() (users []User, raw []byte, err error)
}

// MutationSource performs the PR-mutating actions (merge, auto-merge,
// mark-ready, update-branch, request-reviewers) via githubv4, replacing the
// argv-templated `gh` CLI commands in internal/action/defaults.go. Every
// method takes the PR's GraphQL node ID (gh.PR.ID), not its number — mutation
// inputs require it. RequestReviews takes the full desired reviewer login set
// and always replaces (union:false); an empty set is the valid "remove all
// reviewers" encoding and callers must still fire it, not skip it.
type MutationSource interface {
	MergePR(prID string) error
	EnableAutoMerge(prID string) error
	MarkReady(prID string) error
	UpdateBranch(prID string) error
	RequestReviews(prID string, logins []string) error
}

// WorkflowRun is one run entry from ActionsSource.ListRunsForBranch, mirroring
// the fields `gh run list --json databaseId,conclusion,headSha` returns.
type WorkflowRun struct {
	ID         int64
	Conclusion string
	HeadSHA    string
}

// ActionsSource performs the Actions rerun/job-log operations via REST,
// replacing the `gh run` subprocess calls in internal/action/builtin.go.
// RunID/JobID are the REST API's numeric IDs (gh's CLI --json databaseId).
// nil at the action boundary ⇒ the existing gh.Runner path.
type ActionsSource interface {
	// ListRunsForBranch lists the most recent runs for branch, newest first,
	// mirroring `gh run list --branch <b> -L 20 --json databaseId,conclusion,headSha`.
	ListRunsForBranch(branch string) ([]WorkflowRun, error)
	// RerunFailedJobs reruns the failed jobs of runID, mirroring
	// `gh run rerun <id> --failed`.
	RerunFailedJobs(runID int64) error
	// RerunJob reruns a single job (and its dependents), mirroring
	// `gh run rerun --job <id>`.
	RerunJob(jobID int64) error
	// JobLog fetches jobID's log, filtered to failed steps when failedOnly,
	// tab-delimited in the same "job\tstep\ttimestamp content" shape
	// `gh run view --log[-failed]` emits so it round-trips through the
	// existing parseJobLog consumer unchanged.
	JobLog(jobID int64, failedOnly bool) ([]byte, error)
}

// CLISource is the original path: shell out to `gh pr list --json`.
type CLISource struct {
	R   Runner
	Dir string
}

func (s CLISource) FetchPRs(filter string, limit int) ([]PR, []byte, error) {
	raw, err := s.R.Run(s.Dir, PRListArgs(filter, limit)...)
	if err != nil {
		return nil, nil, err
	}
	prs, err := ParsePRs(raw)
	if err != nil {
		return nil, nil, err
	}
	return prs, raw, nil
}
