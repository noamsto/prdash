package gh

import (
	"encoding/json"
	"fmt"
	"strings"
)

type User struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

const assignableUsersQuery = `query($owner:String!,$name:String!){repository(owner:$owner,name:$name){assignableUsers(first:100){nodes{login name}}}}`

// FetchAssignableUsers returns the users GitHub permits as reviewers/assignees
// for repo (owner/name).
func FetchAssignableUsers(r Runner, dir, repo string) ([]User, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("bad repo %q", repo)
	}
	out, err := r.Run(dir, "api", "graphql",
		"-f", "query="+assignableUsersQuery,
		"-F", "owner="+owner, "-F", "name="+name)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Repository struct {
				AssignableUsers struct {
					Nodes []User `json:"nodes"`
				} `json:"assignableUsers"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse assignable users: %w", err)
	}
	return resp.Data.Repository.AssignableUsers.Nodes, nil
}
