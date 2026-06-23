package action

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/noamsto/prdash/internal/gh"
)

// OSC52 returns the terminal escape that copies s to the system clipboard.
// Survives the tmux popup / SSH when the terminal has clipboard passthrough.
func OSC52(s string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07"
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
