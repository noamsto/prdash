package ui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// linearCLI resolves a Linear ticket to its URL. `issue view -w` would open the
// browser itself, but it refuses to run without a configured workspace, while
// `issue url` reads the workspace from the stored credentials — so prdash asks
// for the URL and opens it the same way it opens everything else.
var linearCLI = []string{"linear", "issue", "url"}

// linkedIssueArgv is the command that gets a PR row's linked issue on screen, or
// nil when there is nothing openable. A GitHub id becomes a URL for the host
// opener; a Linear id becomes the resolver above, because the workspace urlKey
// the real URL needs is not derivable from a branch name.
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
	return append(append([]string{}, linearCLI...), ticket)
}

// openLinkedIssue runs what linkedIssueArgv produced. A host-opener argv is
// spawned and left alone; the resolver's stdout is a URL that still has to be
// opened, and its exit status is the only place a bad ticket or an unset
// workspace shows up.
func openLinkedIssue(argv []string) error {
	if argv[0] != linearCLI[0] {
		return spawnDetached(argv)
	}
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.Join(linearCLI, " "), err)
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return errors.New(strings.Join(linearCLI, " ") + " returned no URL")
	}
	return openURL(url)
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
