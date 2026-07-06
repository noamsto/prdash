package action

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/noamsto/prdash/internal/gh"
)

// RerunCheck reruns a single Actions job (one check) by its job ID.
func RerunCheck(r gh.Runner, dir, jobID string) error {
	_, err := r.Run(dir, "run", "rerun", "--job", jobID)
	return err
}

func RerunFailed(r gh.Runner, dir, branch string) error {
	out, err := r.Run(dir, "run", "list", "--branch", branch, "-L", "1", "--json", "databaseId")
	if err != nil {
		return err
	}
	var runs []struct {
		DatabaseID int `json:"databaseId"`
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("no runs for branch %s", branch)
	}
	_, err = r.Run(dir, "run", "rerun", strconv.Itoa(runs[0].DatabaseID), "--failed")
	return err
}
