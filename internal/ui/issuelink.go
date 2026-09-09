package ui

import "strings"

// linkedIssueArgv is the command that opens the issue a PR row names, or nil
// when there is nothing openable. A GitHub id becomes a URL for the host opener;
// a Linear id is handed to the linear CLI, which knows the workspace this
// machine is authenticated to — the urlKey is not derivable from a branch name.
func linkedIssueArgv(goos, ticket, prURL string) []string {
	if ticket == "" {
		return nil
	}
	if num, ok := strings.CutPrefix(ticket, "#"); ok {
		url, ok := prIssueURL(prURL, num)
		if !ok {
			return nil
		}
		return append(browserArgv(goos), url)
	}
	return []string{"linear", "issue", "view", "-w", ticket}
}

// prIssueURL rewrites a PR URL into a sibling issue URL, so the owner/repo pair
// rides along on the row we already have instead of needing separate plumbing.
func prIssueURL(prURL, num string) (string, bool) {
	base, _, ok := strings.Cut(prURL, "/pull/")
	if !ok {
		return "", false
	}
	return base + "/issues/" + num, true
}
