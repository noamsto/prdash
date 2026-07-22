package ui

import (
	"path/filepath"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeViewerSource mirrors fakeIssueSource (issuesource_test.go) for the
// viewer-login seam.
type fakeViewerSource struct {
	calls int
	login string
}

func (f *fakeViewerSource) FetchViewer() (string, error) {
	f.calls++
	return f.login, nil
}

// fakeMembersSource mirrors fakeIssueSource for the assignable-users seam.
type fakeMembersSource struct {
	calls int
	users []gh.User
	raw   []byte
}

func (f *fakeMembersSource) FetchAssignableUsers() ([]gh.User, []byte, error) {
	f.calls++
	return f.users, f.raw, nil
}

func viewerSourceModel(t *testing.T) Model {
	t.Helper()
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	return m
}

func TestFetchViewerCmdUsesNativeSource(t *testing.T) {
	fs := &fakeViewerSource{login: "octocat"}
	m := viewerSourceModel(t)
	m.SetViewerSource(fs)

	cmd := m.fetchViewerCmd()
	if cmd == nil {
		t.Fatal("fetchViewerCmd should return a command")
	}
	msg := cmd()
	got, ok := msg.(viewerFetchedMsg)
	if !ok {
		t.Fatalf("msg = %T, want viewerFetchedMsg", msg)
	}
	if got.login != "octocat" {
		t.Errorf("login = %q, want the fake source's login", got.login)
	}
	if fs.calls != 1 {
		t.Errorf("source called %d times, want 1", fs.calls)
	}
}

func TestFetchMembersCmdUsesNativeSource(t *testing.T) {
	fs := &fakeMembersSource{
		users: []gh.User{{Login: "alice", Name: "Alice A"}},
		raw:   []byte(`[{"login":"alice","name":"Alice A"}]`),
	}
	m := viewerSourceModel(t)
	m.SetMembersSource(fs)

	cmd := m.fetchMembersCmd()
	if cmd == nil {
		t.Fatal("fetchMembersCmd should return a command")
	}
	msg := cmd()
	got, ok := msg.(membersFetchedMsg)
	if !ok {
		t.Fatalf("msg = %T, want membersFetchedMsg", msg)
	}
	if len(got.users) != 1 || got.users[0].Login != "alice" || got.users[0].Name != "Alice A" {
		t.Errorf("users = %+v, want the fake source's user", got.users)
	}
	if string(got.raw) != string(fs.raw) {
		t.Errorf("raw = %s, want %s", got.raw, fs.raw)
	}
	if fs.calls != 1 {
		t.Errorf("source called %d times, want 1", fs.calls)
	}
}
