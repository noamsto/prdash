package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// clipboardArgv returns the argv for a native clipboard writer that reads the
// payload on stdin, or nil when none is available (callers then fall back to
// OSC 52). See clipboardArgvFor and the copy case in runAction for why native
// tools are preferred here.
func clipboardArgv() []string {
	return clipboardArgvFor(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY"), os.Getenv("DISPLAY"),
		func(name string) string { p, _ := exec.LookPath(name); return p })
}

// clipboardArgvFor picks a native clipboard-writer argv from the display
// environment. lookPath resolves a tool to its absolute path, or "" if absent.
// Priority: Wayland (wl-copy) before X11 (xclip, then xsel); pbcopy on macOS.
func clipboardArgvFor(goos, wayland, display string, lookPath func(string) string) []string {
	if goos == "darwin" {
		if p := lookPath("pbcopy"); p != "" {
			return []string{p}
		}
		return nil
	}
	if wayland != "" {
		if p := lookPath("wl-copy"); p != "" {
			return []string{p}
		}
	}
	if display != "" {
		if p := lookPath("xclip"); p != "" {
			return []string{p, "-selection", "clipboard"}
		}
		if p := lookPath("xsel"); p != "" {
			return []string{p, "--clipboard", "--input"}
		}
	}
	return nil
}

// writeClipboard runs a native clipboard writer, feeding text on stdin.
func writeClipboard(argv []string, text string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
