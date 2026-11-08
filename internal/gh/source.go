package gh

import (
	"strings"
	"time"
)

// PRSource fetches a PR list for a search filter, returning the parsed PRs and
// the JSON bytes to persist in the on-disk cache. The []byte is the marshalled
// []PR, so the on-disk cache hydrate path round-trips it back unchanged.
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

// ChecksSource fetches just the status-check rollup for a set of PR numbers, for
// the live-CI poll. Nothing is cached: the rollup is the most perishable field on
// the board, and the poll applies it to rows it already has.
type ChecksSource interface {
	FetchChecks(numbers []int) (checks map[int][]Check, err error)
}

// ItemState is the current GitHub state of a held row, looked up by number.
// MergedAt is PRs only; zero otherwise.
type ItemState struct {
	State    string
	MergedAt time.Time
	ClosedAt time.Time
}

// StateSource looks up the current state of issues/PRs by number in one
// aliased request, for the held-row lookup: a departed row's real state
// without a full list refetch.
type StateSource interface {
	FetchStates(numbers []int) (map[int]ItemState, error)
}

// IssueSource fetches an issue list for a search filter, mirroring PRSource:
// the []byte is the marshalled []Issue for the cache hydrate round-trip.
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
// []byte is the marshalled []User for the members cache hydrate round-trip.
type MembersSource interface {
	FetchAssignableUsers() (users []User, raw []byte, err error)
}

// MutationSource performs the PR-mutating actions (merge, auto-merge,
// mark-ready, update-branch, approve, request-reviewers) via githubv4. Every
// method takes the PR's GraphQL node ID (gh.PR.ID), not its number — mutation
// inputs require it. RequestReviews takes the full desired reviewer login set
// and always replaces (union:false); an empty set is the valid "remove all
// reviewers" encoding and callers must still fire it, not skip it. MergeStack
// and StackMergeStatus are the exception: they are REST calls and take the PR
// number.
type MutationSource interface {
	MergePR(prID string) error
	EnableAutoMerge(prID string) error
	DisableAutoMerge(prID string) error
	MarkReady(prID string) error
	ConvertToDraft(prID string) error
	UpdateBranch(prID string) error
	ApprovePR(prID string) error
	RequestReviews(prID string, logins []string) error
	MergeStack(prNumber int, mergeQueue bool) (StackMergeResult, error)
	StackMergeStatus(prNumber int, uuid string) (StackMergeResult, error)
}

// StackSource looks up a PR's stacked-PR chain and whether its base branch
// is merge-queue protected, the inputs to a stack merge.
type StackSource interface {
	FetchStackForPR(prNumber int) (Stack, bool, error)
	BaseUsesMergeQueue(baseRef string) (bool, error)
}

// WorkflowRun is one run entry from ActionsSource.ListRunsForBranch, mirroring
// the fields `gh run list --json databaseId,conclusion,headSha` returns.
type WorkflowRun struct {
	ID         int64
	Conclusion string
	HeadSHA    string

	// The fields below are filled by RunsSource.ListRuns alone.
	Name, DisplayTitle                 string
	RunNumber                          int
	Status, HeadBranch, Event, Actor   string
	CreatedAt, UpdatedAt, RunStartedAt time.Time
	HTMLURL                            string
}

// Active reports whether the run is still queued or executing.
func (r WorkflowRun) Active() bool {
	return r.Status != "" && r.Status != "completed"
}

// RunJobStep is one step of a RunJob.
type RunJobStep struct {
	Name, Status string
}

// RunJob is one job of a workflow run.
type RunJob struct {
	ID                              int64
	Name, Status, Conclusion        string
	StartedAt, CompletedAt, HTMLURL string
	Steps                           []RunJobStep
}

// Check maps the job onto the Check shape so Check.Result and the check timing
// helpers work on it unchanged.
func (j RunJob) Check() Check {
	c := Check{Name: j.Name, StartedAt: j.StartedAt, CompletedAt: j.CompletedAt}
	switch j.Status {
	case "completed":
		c.Conclusion = strings.ToUpper(j.Conclusion)
	case "in_progress":
		c.State = "IN_PROGRESS"
	default:
		c.State = "QUEUED"
	}
	return c
}

// ActionsSource performs the Actions rerun/job-log operations via REST.
// RunID/JobID are the REST API's numeric IDs.
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

// RunsSource lists the repo's workflow runs and a run's jobs for the runs board.
// The []byte is the marshalled []WorkflowRun for the cache hydrate round-trip.
type RunsSource interface {
	ListRuns(limit int) ([]WorkflowRun, []byte, error)
	ListRunJobs(runID int64) ([]RunJob, int, error)
}

// RateSource reports the tightest GitHub rate-limit budget seen so far, scraped
// off the responses prdash already makes. It reports false until some response
// has advertised a budget, which is also the state a backend that never observes
// one stays in.
type RateSource interface {
	RateLimit() (RateSnapshot, bool)
}
