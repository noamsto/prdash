package ui

import (
	"reflect"
	"testing"
)

func TestBrowserArgv(t *testing.T) {
	if got := browserArgv("darwin"); !reflect.DeepEqual(got, []string{"open"}) {
		t.Fatalf("darwin argv = %v", got)
	}
	if got := browserArgv("linux"); !reflect.DeepEqual(got, []string{"xdg-open"}) {
		t.Fatalf("linux argv = %v", got)
	}
}
