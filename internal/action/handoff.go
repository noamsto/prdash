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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(key + "\t" + string(j) + "\n")
	return err
}
