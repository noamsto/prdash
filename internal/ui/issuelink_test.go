package ui

import (
	"slices"
	"testing"
)

func TestLinkedIssueArgv(t *testing.T) {
	const prURL = "https://github.com/noamsto/prdash/pull/117"
	for _, tc := range []struct {
		name, ticket, prURL string
		want                []string
	}{
		// GitHub id: the owner/repo prefix comes from the PR's own URL, so no
		// repo plumbing is needed.
		{"github", "#213", prURL,
			[]string{"xdg-open", "https://github.com/noamsto/prdash/issues/213"}},
		{"github other repo", "#7", "https://github.com/noamsto/lazytmux/pull/236",
			[]string{"xdg-open", "https://github.com/noamsto/lazytmux/issues/7"}},
		// Linear id: delegate wholesale, so no workspace slug is ever needed.
		{"linear", "ENG-7659", prURL,
			[]string{"linear", "issue", "view", "-w", "ENG-7659"}},
		{"linear short team", "PD-8", prURL,
			[]string{"linear", "issue", "view", "-w", "PD-8"}},
		// Nothing to open.
		{"no ticket", "", prURL, nil},
		// A URL with no /pull/ segment can't locate an issue: the issue board
		// (where O is unbound) and an empty URL both land here.
		{"issue board url", "#213", "https://github.com/noamsto/prdash/issues/133", nil},
		{"empty url", "#213", "", nil},
	} {
		got := linkedIssueArgv("linux", tc.ticket, tc.prURL)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: linkedIssueArgv(%q, %q) = %v, want %v",
				tc.name, tc.ticket, tc.prURL, got, tc.want)
		}
	}
}

func TestLinkedIssueArgvUsesDarwinOpener(t *testing.T) {
	got := linkedIssueArgv("darwin", "#213", "https://github.com/noamsto/prdash/pull/117")
	want := []string{"open", "https://github.com/noamsto/prdash/issues/213"}
	if !slices.Equal(got, want) {
		t.Errorf("linkedIssueArgv(darwin) = %v, want %v", got, want)
	}
}

// The Linear arm must not depend on the host opener: the CLI opens the browser
// itself, so goos is irrelevant there.
func TestLinkedIssueArgvLinearIgnoresGOOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		got := linkedIssueArgv(goos, "ENG-1", "https://github.com/o/r/pull/1")
		want := []string{"linear", "issue", "view", "-w", "ENG-1"}
		if !slices.Equal(got, want) {
			t.Errorf("goos=%s: got %v, want %v", goos, got, want)
		}
	}
}
