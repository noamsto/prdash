package action

import "encoding/base64"

// OSC52 returns the terminal escape that copies s to the system clipboard.
// Survives the tmux popup / SSH when the terminal has clipboard passthrough.
func OSC52(s string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07"
}
