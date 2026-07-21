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
