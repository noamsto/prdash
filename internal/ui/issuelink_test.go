package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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
			[]string{"linear", "issue", "url", "ENG-7659"}},
		{"linear short team", "PD-8", prURL,
			[]string{"linear", "issue", "url", "PD-8"}},
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
		want := []string{"linear", "issue", "url", "ENG-1"}
		if !slices.Equal(got, want) {
			t.Errorf("goos=%s: got %v, want %v", goos, got, want)
		}
	}
}

// The Linear path is two steps — resolve, then open — and the resolve step is
// where a wrong subcommand hides: `issue view -w` builds a perfectly good argv
// but exits 1 with "workspace is not set", which a detached spawn swallows.
// Stub both binaries so the whole chain is asserted, not just the argv.
func TestOpenLinkedIssueResolvesLinearThenOpens(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "opened.txt")
	const url = "https://linear.app/factify/issue/ENG-7659/must-differ-guard"
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("linear", "#!/bin/sh\nprintf '%s\\n' '"+url+"'\n")
	write(browserArgv(runtime.GOOS)[0], "#!/bin/sh\nprintf '%s\\n' \"$@\" >> "+rec+"\n")
	t.Setenv("PATH", dir)

	if err := openLinkedIssue(linkedIssueArgv(runtime.GOOS, "ENG-7659", "")); err != nil {
		t.Fatalf("resolve+open failed: %v", err)
	}
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(rec); err == nil && strings.TrimSpace(string(b)) == url {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(rec)
	t.Fatalf("opener never got the resolved URL, got %q", b)
}

// A resolver that fails must report, not settle green. This is the exact shape
// of the shipped bug: the argv was valid, the command exited non-zero.
func TestOpenLinkedIssueSurfacesResolverFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "linear"),
		[]byte("#!/bin/sh\necho 'workspace is not set' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	err := openLinkedIssue(linkedIssueArgv(runtime.GOOS, "ENG-7659", ""))
	if err == nil {
		t.Fatal("a resolver exiting non-zero must return an error, not report success")
	}
	if !strings.Contains(err.Error(), "linear issue url") {
		t.Errorf("error = %v, want it to name the command that failed", err)
	}
}

// An empty resolver stdout is not a URL, and must not reach the opener.
func TestOpenLinkedIssueRejectsEmptyURL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "linear"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if err := openLinkedIssue(linkedIssueArgv(runtime.GOOS, "ENG-7659", "")); err == nil {
		t.Fatal("empty resolver output must be an error")
	}
}
