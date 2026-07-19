package gh

import (
	"strings"
	"testing"
)

func TestFetchViewerLogin(t *testing.T) {
	r := &fakeRunner{out: []byte("octocat\n")}
	login, err := FetchViewerLogin(r, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if login != "octocat" {
		t.Fatalf("login = %q, want octocat", login)
	}
	if got := strings.Join(r.gotArgs, " "); got != "api user --jq .login" {
		t.Fatalf("argv = %q", got)
	}
}
