package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// assertNoControls fails on any rune < 0x20 (except \n/\t when multiline), 0x7F,
// or U+0080-U+009F; one-line fields also reject U+2028/U+2029.
func assertNoControls(t *testing.T, field, got string, multiline bool) {
	t.Helper()
	for _, r := range got {
		switch {
		case multiline && (r == '\n' || r == '\t'):
		case r < 0x20, r == 0x7F, r >= 0x80 && r <= 0x9F:
			t.Errorf("%s contains control rune %U: %q", field, r, got)
			return
		case !multiline && (r == 0x2028 || r == 0x2029):
			t.Errorf("%s contains line separator %U: %q", field, r, got)
			return
		}
	}
}

// evilJSON is a JSON string literal body as a pre-filter cache would hold it.
const evilJSON = `\u001b]8;;https://evil.example\u0007title\u001b]8;;\u0007\u0007\r\n`

func hydrateModel(t *testing.T) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", cache.Open(filepath.Join(t.TempDir(), "c.json")))
	m.SetRepo("x")
	return m
}

func TestHydrateSanitizesCachedPRs(t *testing.T) {
	m := hydrateModel(t)
	m.cache.Set(prKey(m.repo, "f", 10), []byte(`[{"number":1,"title":"`+evilJSON+`","headRefName":"`+evilJSON+`"}]`))
	prs, ok := m.cachedPRs("f", 10)
	if !ok || len(prs) != 1 {
		t.Fatalf("cachedPRs = %v, %v", prs, ok)
	}
	assertNoControls(t, "title", prs[0].Title, false)
	assertNoControls(t, "headRefName", prs[0].HeadRefName, false)
	if !strings.Contains(prs[0].Title, "title") {
		t.Errorf("title = %q", prs[0].Title)
	}
}

func TestHydrateSanitizesCachedIssues(t *testing.T) {
	m := hydrateModel(t)
	m.cache.Set(issueKey(m.repo, "f", 10), []byte(`[{"number":1,"title":"`+evilJSON+`"}]`))
	is, ok := m.cachedIssues("f", 10)
	if !ok || len(is) != 1 {
		t.Fatalf("cachedIssues = %v, %v", is, ok)
	}
	assertNoControls(t, "title", is[0].Title, false)
}

func TestHydrateSanitizesCachedRuns(t *testing.T) {
	m := hydrateModel(t)
	m.cache.Set(runsKey(m.repo), []byte(`[{"ID":1,"Name":"`+evilJSON+`","DisplayTitle":"`+evilJSON+`"}]`))
	runs, ok := m.cachedRuns()
	if !ok || len(runs) != 1 {
		t.Fatalf("cachedRuns = %v, %v", runs, ok)
	}
	assertNoControls(t, "Name", runs[0].Name, false)
	assertNoControls(t, "DisplayTitle", runs[0].DisplayTitle, false)
}

func TestHydrateSanitizesCachedPRDetail(t *testing.T) {
	m := hydrateModel(t)
	m.setPRs([]gh.PR{{Number: 4}})
	m.cache.Set(detailKey(m.repo, 4), []byte(`{"comments":[{"body":"`+evilJSON+`"}],"reviews":[{"body":"`+evilJSON+`","state":"`+evilJSON+`"}]}`))
	m.hydrateDetail()
	d, ok := m.detail[4]
	if !ok || len(d.Comments) != 1 || len(d.Reviews) != 1 {
		t.Fatalf("detail = %+v, %v", d, ok)
	}
	assertNoControls(t, "comment body", d.Comments[0].Body, true)
	assertNoControls(t, "review body", d.Reviews[0].Body, true)
	assertNoControls(t, "review state", d.Reviews[0].State, false)
}

func TestHydrateSanitizesCachedIssueDetail(t *testing.T) {
	m := hydrateModel(t)
	s := NewIssueSection("")
	s.SetIssues([]gh.Issue{{Number: 5}})
	m.section = s
	m.cache.Set(issueDetailKey(m.repo, 5), []byte(`{"body":"`+evilJSON+`"}`))
	m.hydrateIssueDetail()
	d, ok := m.issueDetail[5]
	if !ok {
		t.Fatal("issue detail not hydrated")
	}
	assertNoControls(t, "issue body", d.Body, true)
}

func TestHydrateSanitizesCachedMembers(t *testing.T) {
	m := hydrateModel(t)
	m.cache.Set(membersKey(m.repo), []byte(`[{"login":"`+evilJSON+`","name":"`+evilJSON+`"}]`))
	m.hydrateMembers()
	if len(m.members) != 1 {
		t.Fatalf("members = %+v", m.members)
	}
	assertNoControls(t, "login", m.members[0].Login, false)
	assertNoControls(t, "name", m.members[0].Name, false)
}

func TestHydrateSanitizesCachedViewer(t *testing.T) {
	m := hydrateModel(t)
	m.cache.Set(viewerKey(), []byte(`"`+evilJSON+`"`))
	m.hydrateViewer()
	if m.viewerLogin == "" {
		t.Fatal("viewer not hydrated")
	}
	assertNoControls(t, "viewerLogin", m.viewerLogin, false)
}
