package ui

import (
	"reflect"
	"testing"
)

func TestStripTimestamp(t *testing.T) {
	in := "2024-01-02T03:04:05.1234567Z hello world"
	if got := stripTimestamp(in); got != "hello world" {
		t.Fatalf("stripTimestamp = %q", got)
	}
	if got := stripTimestamp("no timestamp here"); got != "no timestamp here" {
		t.Fatalf("non-timestamp line altered: %q", got)
	}
}

func TestParseJobLogGroupsByStep(t *testing.T) {
	raw := []byte(
		"build\tRun tests\t2024-01-02T03:04:05.0Z FAIL foo_test.go:42\n" +
			"build\tRun tests\t2024-01-02T03:04:06.0Z expected 3 got 4\n" +
			"build\tSet up job\t2024-01-02T03:04:00.0Z Requested labels\n")
	steps := parseJobLog(raw, true)
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].name != "Run tests" || !steps[0].failed {
		t.Fatalf("step 0 = %+v", steps[0])
	}
	wantLines := []string{"FAIL foo_test.go:42", "expected 3 got 4"}
	if !reflect.DeepEqual(steps[0].lines, wantLines) {
		t.Fatalf("step 0 lines = %v, want %v", steps[0].lines, wantLines)
	}
	if steps[1].name != "Set up job" {
		t.Fatalf("step 1 name = %q", steps[1].name)
	}
}

func TestParseJobLogFullNotFailed(t *testing.T) {
	raw := []byte("build\tSet up job\t2024-01-02T03:04:00.0Z ok\n")
	if steps := parseJobLog(raw, false); steps[0].failed {
		t.Fatal("full-log steps should not be marked failed")
	}
}
