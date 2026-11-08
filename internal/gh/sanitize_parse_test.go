package gh

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type payloadRow struct {
	name    string
	value   string
	visible []string
	gone    []string
	legit   bool
}

var payloadRows = []payloadRow{
	{name: "SGR", value: "\x1b[31mred\x1b[0m", visible: []string{"red"}},
	{name: "OSC8", value: "\x1b]8;;https://evil.example\x07label\x1b]8;;\x07", visible: []string{"label"}, gone: []string{"evil.example"}},
	{name: "OSC52", value: "\x1b]52;c;SEVMTE8=\x07shown", visible: []string{"shown"}, gone: []string{"SEVMTE8"}},
	{name: "CSI cursor", value: "\x1b[2J\x1b[Hcleared", visible: []string{"cleared"}},
	{name: "BEL", value: "a\x07b", visible: []string{"a", "b"}},
	{name: "CR", value: "left\rright", visible: []string{"left", "right"}},
	{name: "newline", value: "top\nbottom", visible: []string{"top", "bottom"}},
	{name: "mixed", value: "\x1b[1mbold\x1b[0m \x1b]8;;https://evil.example\x07link\x1b]8;;\x07\x07\r\nend", visible: []string{"bold", "link", "end"}, gone: []string{"evil.example"}},
	{name: "CJK", value: "日本語タイトル", legit: true},
	{name: "ZWJ emoji", value: "👨\u200D👩\u200D👧 family", legit: true},
	{name: "RTL with RLM", value: "שלום\u200Fabc\u200E", legit: true},
}

type parsedField struct {
	name      string
	got       string
	multiline bool
}

func (r payloadRow) check(t *testing.T, fields []parsedField) {
	t.Helper()
	for _, f := range fields {
		if r.legit {
			if f.got != r.value {
				t.Errorf("%s = %q, want unchanged %q", f.name, f.got, r.value)
			}
			continue
		}
		assertNoControls(t, f.name, f.got, f.multiline)
		if f.multiline && r.name == "newline" && f.got != r.value {
			t.Errorf("%s = %q, want newlines kept: %q", f.name, f.got, r.value)
		}
		for _, v := range r.visible {
			if !strings.Contains(f.got, v) {
				t.Errorf("%s = %q lost visible text %q", f.name, f.got, v)
			}
		}
		for _, g := range r.gone {
			if strings.Contains(f.got, g) {
				t.Errorf("%s = %q still holds %q", f.name, f.got, g)
			}
		}
	}
}

func jstr(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func prFields(prs []PR) []parsedField {
	p := prs[0]
	return []parsedField{
		{"pr.title", p.Title, false},
		{"pr.body", p.Body, true},
		{"pr.headRefName", p.HeadRefName, false},
		{"pr.baseRefName", p.BaseRefName, false},
		{"pr.author", p.Author.Login, false},
		{"pr.label", p.Labels[0].Name, false},
		{"pr.assignee", p.Assignees[0].Login, false},
		{"check.name", p.StatusCheckRollup[0].Name, false},
		{"check.detailsUrl", p.StatusCheckRollup[0].DetailsUrl, false},
		{"check.workflow", p.StatusCheckRollup[0].WorkflowName, false},
		{"status.context", p.StatusCheckRollup[1].Context, false},
		{"status.targetUrl", p.StatusCheckRollup[1].TargetUrl, false},
	}
}

func TestParseSanitizesFetchPRs(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"data":{"search":{"nodes":[{"number":1,"title":`+v+`,"body":`+v+
				`,"headRefName":`+v+`,"baseRefName":`+v+
				`,"author":{"login":`+v+`,"__typename":"User"}`+
				`,"labels":{"nodes":[{"name":`+v+`}]},"assignees":{"nodes":[{"login":`+v+`}]}`+
				`,"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[`+
				`{"__typename":"CheckRun","name":`+v+`,"detailsUrl":`+v+`,"checkSuite":{"workflowRun":{"workflow":{"name":`+v+`}}}},`+
				`{"__typename":"StatusContext","context":`+v+`,"targetUrl":`+v+`}]}}}}]}}]}}}`)
			prs, raw, err := testQuerySource(srv, "o/r").FetchPRs("is:open", 10)
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, prFields(prs))

			var cached []PR
			if err := json.Unmarshal(raw, &cached); err != nil {
				t.Fatal(err)
			}
			row.check(t, prFields(cached))
		})
	}
}

func TestParseSanitizesFetchIssues(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"data":{"search":{"nodes":[{"number":1,"title":`+v+
				`,"author":{"login":`+v+`,"__typename":"User"},"labels":{"nodes":[{"name":`+v+`}]}}]}}}`)
			issues, _, err := testQuerySource(srv, "o/r").FetchIssues("is:open", 10)
			if err != nil {
				t.Fatal(err)
			}
			i := issues[0]
			row.check(t, []parsedField{
				{"issue.title", i.Title, false},
				{"issue.author", i.Author.Login, false},
				{"issue.label", i.Labels[0].Name, false},
			})
		})
	}
}

func TestParseSanitizesChecks(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			body := `{"data":{"repository":{"pr1":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[` +
				`{"__typename":"CheckRun","name":` + v + `},{"__typename":"StatusContext","context":` + v + `}]}}}}]}}}}}`
			got, err := parseChecks([]byte(body), []int{1})
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{
				{"check.name", got[1][0].Name, false},
				{"check.context", got[1][1].Context, false},
			})
		})
	}
}

func TestParseSanitizesDetails(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			body := `{"data":{"repository":{"pr1":{` +
				`"comments":{"nodes":[{"author":{"login":` + v + `},"body":` + v + `}]},` +
				`"reviews":{"nodes":[{"author":{"login":` + v + `},"body":` + v + `}]},` +
				`"reviewRequests":{"nodes":[{"requestedReviewer":{"login":` + v + `}}]},` +
				`"files":{"nodes":[{"path":` + v + `}]},` +
				`"reviewThreads":{"nodes":[{"path":` + v + `,"comments":{"nodes":[{"author":{"login":` + v + `},"body":` + v + `,"diffHunk":` + v + `}]}}]}` +
				`}}}}`
			details, raws, err := parseDetails([]byte(body), []int{1})
			if err != nil {
				t.Fatal(err)
			}
			d := details[1]
			fields := func(d PRDetail) []parsedField {
				th := d.ReviewThreads[0]
				return []parsedField{
					{"comment.login", d.Comments[0].Author.Login, false},
					{"comment.body", d.Comments[0].Body, true},
					{"review.body", d.Reviews[0].Body, true},
					{"review.login", d.Reviews[0].Author.Login, false},
					{"reviewRequest.login", d.ReviewRequests[0].Login, false},
					{"file.path", d.Files[0].Path, false},
					{"thread.path", th.Path, false},
					{"threadComment.author", th.Comments[0].Author, false},
					{"threadComment.body", th.Comments[0].Body, true},
					{"threadComment.diffHunk", th.Comments[0].DiffHunk, true},
				}
			}
			row.check(t, fields(d))

			var cached PRDetail
			if err := json.Unmarshal(raws[1], &cached); err != nil {
				t.Fatal(err)
			}
			row.check(t, fields(cached))
		})
	}
}

func TestParseSanitizesRuns(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"workflow_runs":[{"id":1,"name":`+v+`,"display_title":`+v+
				`,"head_branch":`+v+`,"actor":{"login":`+v+`},"event":`+v+`}]}`)
			runs, _, err := testSource(srv, "o/r", "tok").ListRuns(10)
			if err != nil {
				t.Fatal(err)
			}
			r := runs[0]
			row.check(t, []parsedField{
				{"run.name", r.Name, false},
				{"run.displayTitle", r.DisplayTitle, false},
				{"run.headBranch", r.HeadBranch, false},
				{"run.actor", r.Actor, false},
				{"run.event", r.Event, false},
			})
		})
	}
}

func TestParseSanitizesRunsForBranch(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"workflow_runs":[{"id":1,"conclusion":`+v+`,"head_sha":`+v+`}]}`)
			runs, err := testSource(srv, "o/r", "tok").ListRunsForBranch("main")
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{
				{"run.conclusion", runs[0].Conclusion, false},
				{"run.headSHA", runs[0].HeadSHA, false},
			})
		})
	}
}

func TestParseSanitizesIssueDetail(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"data":{"repository":{"issue":{"body":`+v+`}}}}`)
			d, raw, err := testQuerySource(srv, "o/r").FetchIssueDetail(1)
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{{"issue.body", d.Body, true}})

			var cached IssueDetail
			if err := json.Unmarshal(raw, &cached); err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{{"issue.cachedBody", cached.Body, true}})
		})
	}
}

func TestParseSanitizesViewer(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			srv := jsonServer(t, `{"data":{"viewer":{"login":`+jstr(t, row.value)+`}}}`)
			login, err := testQuerySource(srv, "o/r").FetchViewer()
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{{"viewer.login", login, false}})
		})
	}
}

func TestParseSanitizesRunJobs(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"total_count":1,"jobs":[{"id":1,"name":`+v+`,"steps":[{"name":`+v+`}]}]}`)
			jobs, _, err := testSource(srv, "o/r", "tok").ListRunJobs(1)
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{
				{"job.name", jobs[0].Name, false},
				{"step.name", jobs[0].Steps[0].Name, false},
			})
		})
	}
}

func TestParseSanitizesStack(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `[{"number":1,"base":{"ref":`+v+`},"pull_requests":[]}]`)
			st, ok, err := testSource(srv, "o/r", "tok").FetchStackForPR(1)
			if err != nil || !ok {
				t.Fatalf("FetchStackForPR = %v, %v", ok, err)
			}
			row.check(t, []parsedField{{"stack.base", st.BaseRefName, false}})

			res, err := decodeStackMerge([]byte(`{"status":"queued","details":{"message":` + v + `}}`))
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{{"merge.message", res.Message, false}})
		})
	}
}

func TestParseSanitizesAssignableUsers(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			srv := jsonServer(t, `{"data":{"repository":{"assignableUsers":{"nodes":[{"login":`+v+`,"name":`+v+`}]}}}}`)
			users, _, err := testQuerySource(srv, "o/r").FetchAssignableUsers()
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{
				{"user.login", users[0].Login, false},
				{"user.name", users[0].Name, false},
			})
		})
	}
}

func TestParseSanitizesStates(t *testing.T) {
	for _, row := range payloadRows {
		t.Run(row.name, func(t *testing.T) {
			v := jstr(t, row.value)
			got, err := parseStates([]byte(`{"data":{"repository":{"n1":{"state":`+v+`}}}}`), []int{1})
			if err != nil {
				t.Fatal(err)
			}
			row.check(t, []parsedField{{"state", got[1].State, false}})
		})
	}
}

func TestErrorBodySanitized(t *testing.T) {
	const evil = "\x1b]8;;https://evil.example\x07boom\x1b]8;;\x07\x07\r\nnext"

	t.Run("parseDetails errors", func(t *testing.T) {
		body := `{"errors":[{"message":` + jstr(t, evil) + `}]}`
		_, _, err := parseDetails([]byte(body), []int{1})
		if err == nil {
			t.Fatal("want error")
		}
		assertNoControls(t, "err", err.Error(), false)
		if !strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "evil.example") {
			t.Errorf("err = %q", err)
		}
	})

	t.Run("listRunsPage non-200 body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(evil))
		}))
		defer srv.Close()
		_, err := testSource(srv, "o/r", "tok").listRunsPage(10, "")
		if err == nil {
			t.Fatal("want error")
		}
		assertNoControls(t, "err", err.Error(), false)
		if !strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "evil.example") {
			t.Errorf("err = %q", err)
		}
	})

	t.Run("typed client errors reply", func(t *testing.T) {
		srv := jsonServer(t, `{"errors":[{"message":`+jstr(t, evil)+`}],"data":null}`)
		_, _, err := testQuerySource(srv, "o/r").FetchPRs("is:open", 10)
		if err == nil {
			t.Fatal("want error")
		}
		assertNoControls(t, "err", err.Error(), false)
		if !strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "evil.example") {
			t.Errorf("err = %q", err)
		}
		if errors.Unwrap(err) == nil {
			t.Errorf("err %v does not keep the original", err)
		}
	})

	t.Run("IssuesDisabled still matches", func(t *testing.T) {
		err := sanitizeErr(errors.New("repo \x1b[31mhas disabled issues\x07"))
		if !IssuesDisabled(err) {
			t.Errorf("IssuesDisabled(%q) = false", err)
		}
	})
}
