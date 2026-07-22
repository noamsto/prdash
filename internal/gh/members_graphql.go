package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"
)

// qlUser is the githubv4 shape of one assignableUsers node, matching
// assignableUsersQuery's `nodes{login name}` (members.go).
type qlUser struct {
	Login string
	Name  string
}

// FetchAssignableUsers mirrors `gh api graphql` with assignableUsersQuery
// (members.go), returning the users GitHub permits as reviewers/assignees for
// the repo. raw mirrors the shape the members cache stores (a marshaled
// []User, per hydrateMembers) so a cache entry written by either backend
// stays readable by the other.
func (s GraphSource) FetchAssignableUsers() ([]User, []byte, error) {
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return nil, nil, fmt.Errorf("bad repo %q", s.repo)
	}
	var q struct {
		Repository struct {
			AssignableUsers struct {
				Nodes []qlUser
			} `graphql:"assignableUsers(first: 100)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	vars := map[string]any{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}
	if err := s.client.Query(context.Background(), &q, vars); err != nil {
		return nil, nil, err
	}
	users := mapAssignableUsers(q.Repository.AssignableUsers.Nodes)
	raw, err := json.Marshal(users)
	if err != nil {
		return nil, nil, err
	}
	return users, raw, nil
}

func mapAssignableUsers(nodes []qlUser) []User {
	users := make([]User, 0, len(nodes))
	for _, n := range nodes {
		users = append(users, User(n))
	}
	return users
}
