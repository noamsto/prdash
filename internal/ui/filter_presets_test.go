package ui

import "testing"

func TestSearchFor(t *testing.T) {
	cases := []struct{ state, body, want string }{
		{"open", "author:@me", "is:open author:@me"},
		{"merged", "author:@me", "is:merged author:@me"},
		{"open", "", "is:open"},
		{"closed", "", "is:closed"},
	}
	for _, c := range cases {
		if got := searchFor(c.state, c.body); got != c.want {
			t.Errorf("searchFor(%q,%q)=%q want %q", c.state, c.body, got, c.want)
		}
	}
}

func TestSplitState(t *testing.T) {
	cases := []struct{ in, state, body string }{
		{"is:open author:@me", "open", "author:@me"},
		{"is:merged author:@me", "merged", "author:@me"},
		{"is:open", "open", ""},
		{"is:closed", "closed", ""},
		{"author:@me", "open", "author:@me"}, // no state token → default open
	}
	for _, c := range cases {
		state, body := splitState(c.in, prStates)
		if state != c.state || body != c.body {
			t.Errorf("splitState(%q)=(%q,%q) want (%q,%q)", c.in, state, body, c.state, c.body)
		}
	}
}

func TestNextStateWraps(t *testing.T) {
	if got := nextState("open", prStates); got != "merged" {
		t.Fatalf("nextState(open)=%q want merged", got)
	}
	if got := nextState("closed", prStates); got != "open" {
		t.Fatalf("nextState(closed)=%q want open (wrap)", got)
	}
	if got := nextState("bogus", prStates); got != "open" {
		t.Fatalf("nextState(bogus)=%q want open (fallback)", got)
	}
}

func TestFilterPresetCycleWraps(t *testing.T) {
	p := defaultPresets
	if p[0].name != "mine" {
		t.Fatalf("first preset should be mine, got %q", p[0].name)
	}
	for i := range p {
		want := (i + 1) % len(p)
		if got := nextPreset(i, defaultPresets); got != want {
			t.Fatalf("nextPreset(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestStatesFor(t *testing.T) {
	if got := statesFor("issue"); len(got) != 2 || got[0] != "open" || got[1] != "closed" {
		t.Errorf("issue states = %v", got)
	}
	if got := statesFor("pr"); len(got) != 3 {
		t.Errorf("pr states = %v", got)
	}
}

func TestNextStateIssueWraps(t *testing.T) {
	st := statesFor("issue")
	if got := nextState("open", st); got != "closed" {
		t.Errorf("open -> %q, want closed", got)
	}
	if got := nextState("closed", st); got != "open" {
		t.Errorf("closed -> %q, want open (wrap)", got)
	}
}

func TestIssueMinePreset(t *testing.T) {
	ps := presetsFor("issue")
	i := presetIndexFor("assignee:@me", ps)
	if i < 0 || ps[i].name != "mine" {
		t.Errorf("issue mine preset not found: idx=%d", i)
	}
}
