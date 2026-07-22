package ui

import (
	"path/filepath"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeIssueSource mirrors fakeDetailSource (detailbatch_test.go) for the
// issue-list seam.
type fakeIssueSource struct {
	calls []struct {
		filter string
		limit  int
	}
	issues []gh.Issue
	raw    []byte
}

func (f *fakeIssueSource) FetchIssues(filter string, limit int) ([]gh.Issue, []byte, error) {
	f.calls = append(f.calls, struct {
		filter string
		limit  int
	}{filter, limit})
	return f.issues, f.raw, nil
}

// fakeIssueDetailSource mirrors fakeDetailSource for the per-issue detail seam.
type fakeIssueDetailSource struct {
	got    []int
	detail gh.IssueDetail
	raw    []byte
}

func (f *fakeIssueDetailSource) FetchIssueDetail(number int) (gh.IssueDetail, []byte, error) {
	f.got = append(f.got, number)
	return f.detail, f.raw, nil
}

func issueSourceModel(t *testing.T) Model {
	t.Helper()
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	return m
}

func TestIssueFetchCmdUsesNativeSource(t *testing.T) {
	fs := &fakeIssueSource{
		issues: []gh.Issue{{Number: 3, Title: "native issue"}},
		raw:    []byte(`[{"number":3}]`),
	}
	m := issueSourceModel(t)
	m.SetIssueSource(fs)

	cmd := m.issueFetchCmd("is:open assignee:@me")
	if cmd == nil {
		t.Fatal("issueFetchCmd should return a command")
	}
	msg := cmd()
	got, ok := msg.(issuesFetchedMsg)
	if !ok {
		t.Fatalf("msg = %T, want issuesFetchedMsg", msg)
	}
	if len(got.issues) != 1 || got.issues[0].Number != 3 || got.issues[0].Title != "native issue" {
		t.Errorf("issues = %+v, want the fake source's issue", got.issues)
	}
	if got.filter != "is:open assignee:@me" {
		t.Errorf("filter = %q, want the requested filter echoed back", got.filter)
	}
	if len(fs.calls) != 1 || fs.calls[0].filter != "is:open assignee:@me" || fs.calls[0].limit != defaultLimit {
		t.Errorf("source called with %+v, want one call for the filter at defaultLimit", fs.calls)
	}
}

func TestFetchIssueDetailCmdUsesNativeSource(t *testing.T) {
	fds := &fakeIssueDetailSource{
		detail: gh.IssueDetail{Body: "native body"},
		raw:    []byte(`{"body":"native body"}`),
	}
	m := issueSourceModel(t)
	m.SetIssueDetailSource(fds)

	cmd := m.fetchIssueDetailCmd(42)
	if cmd == nil {
		t.Fatal("fetchIssueDetailCmd should return a command")
	}
	msg := cmd()
	got, ok := msg.(issueDetailMsg)
	if !ok {
		t.Fatalf("msg = %T, want issueDetailMsg", msg)
	}
	if got.number != 42 || got.detail.Body != "native body" {
		t.Errorf("detail = %+v, want number=42 body=%q", got, "native body")
	}
	if len(fds.got) != 1 || fds.got[0] != 42 {
		t.Errorf("source called with %v, want one call for 42", fds.got)
	}
}
