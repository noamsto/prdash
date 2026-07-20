package gh

import (
	"strings"
	"testing"
)

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
		"number,title,author,statusCheckRollup,reviewDecision,labels,assignees,headRefName,baseRefName,url,updatedAt,mergedAt,closedAt,isDraft,state,body,autoMergeRequest",
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
		"https://example.com/build/123":                                       "", // external StatusContext-style target
		"":                                                                    "",
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

func TestChecksDedupesByName(t *testing.T) {
	p := PR{StatusCheckRollup: []Check{
		{Name: "ci", State: "SUCCESS", StartedAt: "2026-06-24T12:54:09Z"},
		{Name: "ci", State: "FAILURE", StartedAt: "2026-06-24T12:54:12Z"}, // newer wins
		{Name: "lint", State: "SUCCESS", StartedAt: "2026-06-24T12:00:00Z"},
		{Context: "external/a", State: "PENDING"}, // unnamed: kept as-is
		{Context: "external/b", State: "SUCCESS"},
	}}
	got := p.Checks()
	if len(got) != 4 {
		t.Fatalf("want 4 deduped checks, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Label() == "ci" && c.Result() != "fail" {
			t.Errorf("ci should keep the newer (failing) run, got %q", c.Result())
		}
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

func TestParsePRsReadsMergeAndCloseTimes(t *testing.T) {
	raw := []byte(`[{"number":1,"mergedAt":"2026-07-10T09:00:00Z","closedAt":"2026-07-10T09:00:00Z"},
	                {"number":2,"mergedAt":null,"closedAt":"2026-07-11T12:30:00Z"}]`)
	prs, err := ParsePRs(raw)
	if err != nil {
		t.Fatalf("ParsePRs: %v", err)
	}
	if prs[0].MergedAt.IsZero() {
		t.Errorf("PR #1 MergedAt should be parsed, got zero")
	}
	if !prs[1].MergedAt.IsZero() {
		t.Errorf("PR #2 has null mergedAt, want zero time, got %v", prs[1].MergedAt)
	}
	if prs[1].ClosedAt.IsZero() {
		t.Errorf("PR #2 ClosedAt should be parsed, got zero")
	}
}

func TestCheckIsExternal(t *testing.T) {
	if !(Check{Context: "ci/ext"}).IsExternal() {
		t.Fatal("StatusContext should be external")
	}
	if (Check{Name: "build"}).IsExternal() {
		t.Fatal("named CheckRun is not external")
	}
	if (Check{WorkflowName: "CI"}).IsExternal() {
		t.Fatal("workflow CheckRun is not external")
	}
}

func TestCheckURL(t *testing.T) {
	// A CheckRun exposes its page as detailsUrl.
	if got := (Check{Name: "build", DetailsUrl: "https://d/1"}).URL(); got != "https://d/1" {
		t.Fatalf("CheckRun URL = %q, want detailsUrl", got)
	}
	// An external StatusContext exposes it as targetUrl instead.
	if got := (Check{Context: "ci/ext", TargetUrl: "https://t/2"}).URL(); got != "https://t/2" {
		t.Fatalf("StatusContext URL = %q, want targetUrl", got)
	}
	if got := (Check{}).URL(); got != "" {
		t.Fatalf("URL with neither field = %q, want empty", got)
	}
}

func TestPRListArgsRequestsTimestamps(t *testing.T) {
	args := PRListArgs("is:merged", 20)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "mergedAt") || !strings.Contains(joined, "closedAt") {
		t.Fatalf("PRListArgs must request mergedAt,closedAt: %q", joined)
	}
}

func TestPRListArgsIncludesAutoMergeRequest(t *testing.T) {
	args := PRListArgs("is:open author:@me", 20)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "autoMergeRequest") {
		t.Fatalf("args missing autoMergeRequest field: %v", args)
	}
}

func TestFetchPRsParsesAutoMergeRequest(t *testing.T) {
	f := &fakeRunner{out: []byte(`[
		{"number":7,"title":"armed","autoMergeRequest":{"mergeMethod":"SQUASH"}},
		{"number":8,"title":"not armed","autoMergeRequest":null}
	]`)}
	prs, err := FetchPRs(f, "/repo", "is:open", 20)
	if err != nil {
		t.Fatalf("FetchPRs: %v", err)
	}
	if !prs[0].AutoMergeEnabled() {
		t.Errorf("PR 7 should have auto-merge enabled: %+v", prs[0])
	}
	if prs[1].AutoMergeEnabled() {
		t.Errorf("PR 8 should not have auto-merge enabled: %+v", prs[1])
	}
}
