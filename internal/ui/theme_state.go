package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// themeStatePath resolves the system theme-state file, honoring XDG_STATE_HOME.
// This is the signal theme-toggle.sh writes and lazytmux's picker reads.
func themeStatePath() string {
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg == "" {
		xdg = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(xdg, "theme-state.json")
}

// detectTheme reports "light" or "dark" from the system theme-state file,
// defaulting to "dark" on any error. Mirrors lazytmux's picker.
func detectTheme() string {
	data, err := os.ReadFile(themeStatePath())
	if err != nil {
		return "dark"
	}
	var cfg struct {
		Theme string `json:"theme"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Theme == "" {
		return "dark"
	}
	return cfg.Theme
}

// statModTime returns the mtime of path, or a zero time and error if absent.
func statModTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
