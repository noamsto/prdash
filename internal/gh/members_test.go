package gh

import "testing"

func TestFetchAssignableUsersParses(t *testing.T) {
	resp := `{"data":{"repository":{"assignableUsers":{"nodes":[
		{"login":"alice","name":"Alice A"},
		{"login":"bob","name":""}]}}}}`
	users, err := FetchAssignableUsers(&fakeRunner{out: []byte(resp)}, "/repo", "noamsto/prdash")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Login != "alice" || users[0].Name != "Alice A" {
		t.Fatalf("parsed wrong: %+v", users)
	}
}

func TestFetchAssignableUsersBadRepo(t *testing.T) {
	if _, err := FetchAssignableUsers(&fakeRunner{out: []byte("{}")}, "/repo", "norepo"); err == nil {
		t.Fatal("expected error for repo without owner/name")
	}
}
