package gh

import "testing"

type fakeRunner struct {
	gotArgs []string
	out     []byte
}

func (f *fakeRunner) Run(_ string, args ...string) ([]byte, error) {
	f.gotArgs = args
	return f.out, nil
}

func TestPRListArgs(t *testing.T) {
	args := PRListArgs("is:open author:@me", 20)
	want := []string{
		"pr", "list", "--search", "is:open author:@me",
		"-L", "20", "--json",
		"number,title,author,statusCheckRollup,reviewDecision,labels,assignees,headRefName,baseRefName,url,updatedAt,isDraft",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestFetchPRsParses(t *testing.T) {
	f := &fakeRunner{out: []byte(`[
		{"number":7,"title":"hi","author":{"login":"noam"},
		 "statusCheckRollup":[{"state":"SUCCESS"}],"headRefName":"feat/x"}
	]`)}
	prs, err := FetchPRs(f, "/repo", "is:open", 20)
	if err != nil {
		t.Fatalf("FetchPRs: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 7 || prs[0].Author.Login != "noam" {
		t.Fatalf("parsed = %+v", prs)
	}
}

func TestCIState(t *testing.T) {
	cases := []struct {
		name   string
		rollup []Check
		want   string
	}{
		{"empty", nil, "none"},
		{"all pass", []Check{{State: "SUCCESS"}, {State: "SUCCESS"}}, "pass"},
		{"one fail", []Check{{State: "SUCCESS"}, {State: "FAILURE"}}, "fail"},
		{"pending", []Check{{State: "SUCCESS"}, {State: "PENDING"}}, "pending"},
	}
	for _, c := range cases {
		if got := (PR{StatusCheckRollup: c.rollup}).CIState(); got != c.want {
			t.Errorf("%s: CIState = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCheckJobID(t *testing.T) {
	cases := map[string]string{
		"https://github.com/cli/cli/actions/runs/28238190155/job/83658069205": "83658069205",
		"https://example.com/build/123": "", // external StatusContext-style target
		"":                              "",
	}
	for url, want := range cases {
		if got := (Check{DetailsUrl: url}).JobID(); got != want {
			t.Errorf("JobID(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestLabelColorParses(t *testing.T) {
	prs, err := ParsePRs([]byte(`[{"number":1,"labels":[{"name":"bug","color":"d73a4a"}]}]`))
	if err != nil {
		t.Fatalf("ParsePRs: %v", err)
	}
	if got := prs[0].Labels[0].Color; got != "d73a4a" {
		t.Errorf("label color = %q, want d73a4a", got)
	}
}

func TestCheckResult(t *testing.T) {
	cases := map[string]string{"SUCCESS": "pass", "FAILURE": "fail", "PENDING": "pending", "": "pending"}
	for state, want := range cases {
		if got := (Check{State: state}).Result(); got != want {
			t.Errorf("Check{State:%q}.Result() = %q, want %q", state, got, want)
		}
	}
	if got := (Check{Conclusion: "FAILURE"}).Result(); got != "fail" {
		t.Errorf("conclusion fallback: %q", got)
	}
}
