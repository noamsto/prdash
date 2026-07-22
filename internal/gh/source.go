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
