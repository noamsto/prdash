package ui

import (
	"reflect"
	"testing"
)

func TestBrowserArgv(t *testing.T) {
	tests := []struct {
		name, goos, browser string
		want                []string
	}{
		{"darwin default", "darwin", "", []string{"open"}},
		{"linux default", "linux", "", []string{"xdg-open"}},
		{"darwin honors BROWSER", "darwin", "og-open", []string{"og-open"}},
		{"linux honors BROWSER", "linux", "/usr/bin/firefox", []string{"/usr/bin/firefox"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := browserArgv(tt.goos, tt.browser); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("browserArgv(%q, %q) = %v, want %v", tt.goos, tt.browser, got, tt.want)
			}
		})
	}
}
