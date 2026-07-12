package ui

import (
	"slices"
	"testing"
)

func TestClipboardArgvFor(t *testing.T) {
	// have builds a lookPath stub that resolves only the named tools.
	have := func(tools ...string) func(string) string {
		return func(name string) string {
			if slices.Contains(tools, name) {
				return "/usr/bin/" + name
			}
			return ""
		}
	}
	tests := []struct {
		name    string
		goos    string
		wayland string
		display string
		look    func(string) string
		want    []string
	}{
		{"darwin uses pbcopy", "darwin", "", "", have("pbcopy"), []string{"/usr/bin/pbcopy"}},
		{"darwin without pbcopy falls back to OSC52", "darwin", "", "", have(), nil},
		{"wayland prefers wl-copy", "linux", "wayland-1", "", have("wl-copy"), []string{"/usr/bin/wl-copy"}},
		{"wl-copy wins over xclip", "linux", "wayland-1", ":0", have("wl-copy", "xclip"), []string{"/usr/bin/wl-copy"}},
		{"wayland set but wl-copy missing falls through to X11", "linux", "wayland-1", ":0", have("xclip"), []string{"/usr/bin/xclip", "-selection", "clipboard"}},
		{"x11 uses xclip", "linux", "", ":0", have("xclip"), []string{"/usr/bin/xclip", "-selection", "clipboard"}},
		{"x11 uses xsel when no xclip", "linux", "", ":0", have("xsel"), []string{"/usr/bin/xsel", "--clipboard", "--input"}},
		{"xclip wins over xsel", "linux", "", ":0", have("xclip", "xsel"), []string{"/usr/bin/xclip", "-selection", "clipboard"}},
		{"no display, no tools falls back to OSC52", "linux", "", "", have("wl-copy", "xclip"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clipboardArgvFor(tt.goos, tt.wayland, tt.display, tt.look)
			if !slices.Equal(got, tt.want) {
				t.Errorf("clipboardArgvFor(%q, %q, %q) = %v, want %v", tt.goos, tt.wayland, tt.display, got, tt.want)
			}
		})
	}
}
