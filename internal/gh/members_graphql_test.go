package gh

import "testing"

func TestMapAssignableUsers(t *testing.T) {
	nodes := []qlUser{
		{Login: "alice", Name: "Alice A"},
		{Login: "bob", Name: ""},
	}
	got := mapAssignableUsers(nodes)
	if len(got) != 2 || got[0].Login != "alice" || got[0].Name != "Alice A" ||
		got[1].Login != "bob" || got[1].Name != "" {
		t.Errorf("users = %+v, want [alice/Alice A, bob/(empty)]", got)
	}
}

func TestMapAssignableUsersEmpty(t *testing.T) {
	if got := mapAssignableUsers(nil); len(got) != 0 {
		t.Errorf("users = %+v, want empty slice", got)
	}
}
