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
		state, body := splitState(c.in)
		if state != c.state || body != c.body {
			t.Errorf("splitState(%q)=(%q,%q) want (%q,%q)", c.in, state, body, c.state, c.body)
		}
	}
}

func TestNextStateWraps(t *testing.T) {
	if got := nextState("open"); got != "merged" {
		t.Fatalf("nextState(open)=%q want merged", got)
	}
	if got := nextState("closed"); got != "open" {
		t.Fatalf("nextState(closed)=%q want open (wrap)", got)
	}
	if got := nextState("bogus"); got != "open" {
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
		if got := nextPreset(i); got != want {
			t.Fatalf("nextPreset(%d) = %d, want %d", i, got, want)
		}
	}
}
