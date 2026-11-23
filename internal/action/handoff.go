package action

import (
	"encoding/json"
	"os"
)

// AppendHandoff appends one "<key>\t<argv-json>" line to the handoff file the
// tmux orchestrator reads after the popup closes.
func AppendHandoff(path, key string, argv []string) error {
	j, err := json.Marshal(argv)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: path is the handoff sink the caller chose, not untrusted input
	if err != nil {
		return err
	}
	_, werr := f.WriteString(key + "\t" + string(j) + "\n")
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// HandoffEntry is a worktree switch the orchestrator resolves itself because
// this process has no local checkout of Repo. Args is the `wt` argv without the
// leading "wt" and any -C <dir>; Target is its last element.
type HandoffEntry struct {
	Repo   string   `json:"repo"`
	Kind   string   `json:"kind"`
	Target string   `json:"target"`
	PR     int      `json:"pr,omitempty"`
	Args   []string `json:"args"`
	Key    string   `json:"key"`
}

// AppendHandoffRemote appends one "<key>\t<entry-json>" line; consumers tell it
// from an argv line by the first byte after the tab ('{' vs '[').
func AppendHandoffRemote(path string, e HandoffEntry) error {
	j, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: path is the handoff sink the caller chose, not untrusted input
	if err != nil {
		return err
	}
	_, werr := f.WriteString(e.Key + "\t" + string(j) + "\n")
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
