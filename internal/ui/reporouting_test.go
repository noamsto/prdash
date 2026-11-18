package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

// routingRow describes one of two rows sharing PR number 7 across repos.
type routingRow struct {
	repo, id, branch string
	stacked          bool
}

var (
	launchRow = routingRow{repo: "owner/repo", id: "L7", branch: "feat/l"}
	otherRow  = routingRow{repo: "other/x", id: "O7", branch: "feat/o", stacked: true}
)

func (r routingRow) ref() gh.PRRef { return gh.PRRef{Repo: r.repo, Number: 7} }

func (r routingRow) url() string { return "https://github.com/" + r.repo + "/pull/7" }

func (r routingRow) pr() gh.PR {
	p := gh.PR{
		ID: r.id, Repo: r.repo, Number: 7, Title: "same number " + r.repo, State: "OPEN",
		HeadRefName: r.branch, Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN", URL: r.url(),
		StatusCheckRollup: []gh.Check{
			{State: "FAILURE", Name: "test", DetailsUrl: "https://github.com/" + r.repo + "/actions/runs/1/job/99"},
			{State: "PENDING", Name: "build"},
		},
	}
	p.Author.Login = "alice"
	if r.stacked {
		p.Stack, p.StackPosition = &gh.PRStack{Number: 7, Size: 1}, 1
	}
	return p
}

type routingFakes struct {
	detail  *fakeDetailSource
	checks  *fakeChecksSource
	mut     *fakeMutationSource
	stacks  *fakeStackSource
	actions *fakeActionsSource
	members *fakeMembersSource
}

// routingModel paints both rows on a flat board launched in owner/repo, with
// the cursor on cursorRow and every source replaced by a recording fake.
func routingModel(t *testing.T, cursorRow routingRow) (Model, *routingFakes) {
	t.Helper()
	fk := &routingFakes{
		detail: &fakeDetailSource{ret: map[int]gh.PRDetail{7: {Mergeable: "MERGEABLE", MergeStateStatus: "CLEAN"}}},
		checks: &fakeChecksSource{},
		mut:    &fakeMutationSource{},
		stacks: &fakeStackSource{found: true, st: gh.Stack{BaseRefName: "main", Members: openMembers(7)}},
		actions: &fakeActionsSource{
			runs: []gh.WorkflowRun{{ID: 200, Conclusion: "failure", HeadSHA: "abc"}},
		},
		members: &fakeMembersSource{users: []gh.User{{Login: "bob"}}},
	}
	m := NewModel(t.TempDir(), "is:open", cache.Open(filepath.Join(t.TempDir(), "c.json")))
	m.SetRepo("owner/repo")
	m.viewerLogin = "me"
	m.width, m.height = 120, 40
	m.SetPRSource(stubSource{}) // the post-mutation refetch fires through it
	m.SetDetailSource(fk.detail)
	m.SetChecksSource(fk.checks)
	m.SetMutationSource(fk.mut)
	m.SetStackSource(fk.stacks)
	m.SetActionsSource(fk.actions)
	m.SetMembersSource(fk.members)
	m.setPRs([]gh.PR{launchRow.pr(), otherRow.pr()})
	m.refreshing = false
	m.polling = true
	cursorOnRepoPR(t, &m, cursorRow.repo, 7)
	return m, fk
}

func cursorOnRepoPR(t *testing.T, m *Model, repo string, n int) {
	t.Helper()
	ps := m.section.(*PRSection)
	i := shownRepoPR(ps, repo, n)
	if i < 0 {
		t.Fatalf("%s#%d not on the board", repo, n)
	}
	m.cursor = i
}

// shownRepoPR is the shown index of repo#n, or -1.
func shownRepoPR(ps *PRSection, repo string, n int) int {
	for i := range ps.Len() {
		if p := ps.prAt(i); p.Repo == repo && p.Number == n {
			return i
		}
	}
	return -1
}

// TestRowRepoRoutesEverySource: two rows share number 7 in different repos;
// every per-row source call must carry the cursor row's repo (and its node ID
// and branch), never the launch repo's.
func TestRowRepoRoutesEverySource(t *testing.T) {
	for _, row := range []routingRow{otherRow, launchRow} {
		t.Run("cursor on "+row.repo, func(t *testing.T) {
			want := row.ref()
			unwanted := otherRow.ref()
			if row == otherRow {
				unwanted = launchRow.ref()
			}

			t.Run("detail", func(t *testing.T) {
				m, fk := routingModel(t, row)
				cmd := m.detailCmdForCursor()
				if cmd == nil {
					t.Fatal("cold cursor detail should fetch")
				}
				msg := cmd()
				if !slices.Equal(flatRefs(fk.detail.got), []gh.PRRef{want}) {
					t.Errorf("FetchDetails refs = %v, want [%v]", fk.detail.got, want)
				}
				m, _ = update(m, msg)
				if _, ok := m.detail[want]; !ok {
					t.Errorf("detail[%v] missing after the batch landed", want)
				}
				if _, ok := m.detail[unwanted]; ok {
					t.Errorf("detail[%v] set by the other row's fetch", unwanted)
				}
				if _, ok := m.cache.Get(detailKey(want.Repo, 7)); !ok {
					t.Errorf("detail not cached under detailKey(%q, 7)", want.Repo)
				}
				if _, ok := m.cache.Get(detailKey(unwanted.Repo, 7)); ok {
					t.Errorf("detail cached under detailKey(%q, 7), the other row's key", unwanted.Repo)
				}
			})

			t.Run("merge", func(t *testing.T) {
				m, fk := routingModel(t, row)
				msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["m"]))
				if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
					t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
				}
				if !slices.Equal(fk.mut.mergeCalls, []string{row.id}) {
					t.Errorf("mergeCalls = %v, want [%s]", fk.mut.mergeCalls, row.id)
				}
			})

			t.Run("stack merge", func(t *testing.T) {
				if !row.stacked {
					t.Skip("only the other repo's row is stacked")
				}
				m, fk := routingModel(t, row)
				m, load := pressS(t, m)
				m, _ = update(m, load)
				if !slices.Equal(fk.stacks.refs, []gh.PRRef{want}) {
					t.Errorf("FetchStackForPR refs = %v, want [%v]", fk.stacks.refs, want)
				}
				if m.pendingStackMerge == nil {
					t.Fatalf("the landed load should open the prompt: status=%+v", m.actionStatus)
				}
				m, cmd := update(m, keyMsg("y"))
				update(m, head(t, cmd))
				if len(fk.mut.stackMergeCalls) != 1 || fk.mut.stackMergeCalls[0].ref != want {
					t.Errorf("MergeStack calls = %+v, want one for %v", fk.mut.stackMergeCalls, want)
				}
			})

			t.Run("rerun failed", func(t *testing.T) {
				m, fk := routingModel(t, row)
				msg := driveBulk(t, m.runAction(action.DefaultPRActions()["r"]))
				if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
					t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
				}
				if len(fk.actions.repos) == 0 || slices.ContainsFunc(fk.actions.repos, func(r string) bool { return r != row.repo }) {
					t.Errorf("repos = %v, want every call on %s", fk.actions.repos, row.repo)
				}
				if !slices.Equal(fk.actions.branches, []string{row.branch}) {
					t.Errorf("ListRunsForBranch branches = %v, want [%s]", fk.actions.branches, row.branch)
				}
			})

			t.Run("reviewers", func(t *testing.T) {
				m, fk := routingModel(t, row)
				_, cmd := update(m, keyMsg("R"))
				if cmd == nil {
					t.Fatal("R should fetch the assignable users")
				}
				if _, ok := cmd().(membersFetchedMsg); !ok {
					t.Fatal("R's command should land a membersFetchedMsg")
				}
				if !slices.Equal(fk.members.repos, []string{row.repo}) {
					t.Errorf("FetchAssignableUsers repos = %v, want [%s]", fk.members.repos, row.repo)
				}
			})

			t.Run("copy and vars", func(t *testing.T) {
				m, _ := routingModel(t, row)
				if got := m.copyPayload("copy-url"); got != row.url() {
					t.Errorf("Y payload = %q, want %q", got, row.url())
				}
				v, ok := m.cursorVars()
				if !ok {
					t.Fatal("cursorVars should resolve on a shown row")
				}
				if v.Repo != row.repo {
					t.Errorf("cursorVars().Repo = %q, want %q", v.Repo, row.repo)
				}
			})

			t.Run("checks poll", func(t *testing.T) {
				m, fk := routingModel(t, row)
				cmd := m.pollChecksCmd()
				if cmd == nil {
					t.Fatal("rows with running checks should poll")
				}
				if _, ok := cmd().(checksFetchedMsg); !ok {
					t.Fatal("the beat should route through the checks source")
				}
				if len(fk.checks.got) != 1 {
					t.Fatalf("FetchChecks calls = %d, want 1", len(fk.checks.got))
				}
				got := slices.Clone(fk.checks.got[0])
				slices.SortFunc(got, func(a, b gh.PRRef) int { return strings.Compare(a.Repo, b.Repo) })
				if wantRefs := []gh.PRRef{otherRow.ref(), launchRow.ref()}; !slices.Equal(got, wantRefs) {
					t.Errorf("FetchChecks refs = %v, want each row under its own repo %v", got, wantRefs)
				}
			})

			t.Run("job log", func(t *testing.T) {
				m, fk := routingModel(t, row)
				hoverFailingCheck(t, &m)
				u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
				m = u.(Model)
				if !m.logView || cmd == nil {
					t.Fatal("enter on a check with a job ID should open the log view")
				}
				if _, ok := head(t, cmd).(logFetchedMsg); !ok {
					t.Fatal("the log command should land a logFetchedMsg")
				}
				if !slices.Equal(fk.actions.repos, []string{row.repo}) {
					t.Errorf("JobLog repos = %v, want [%s]", fk.actions.repos, row.repo)
				}
				if len(fk.actions.jobLogCalls) != 1 || fk.actions.jobLogCalls[0].jobID != 99 {
					t.Errorf("jobLogCalls = %+v, want one for job 99", fk.actions.jobLogCalls)
				}
			})

			t.Run("rerun hovered check", func(t *testing.T) {
				m, fk := routingModel(t, row)
				hoverFailingCheck(t, &m)
				_, cmd := m.updateExpanded(tea.KeyPressMsg{Code: 'r', Text: "r"})
				if done, ok := driveBulk(t, cmd).(actionDoneMsg); !ok || done.err != nil {
					t.Fatalf("msg = %+v, want a successful actionDoneMsg", done)
				}
				if !slices.Equal(fk.actions.repos, []string{row.repo}) {
					t.Errorf("RerunJob repos = %v, want [%s]", fk.actions.repos, row.repo)
				}
			})
		})
	}
}

func flatRefs(calls [][]gh.PRRef) []gh.PRRef {
	var out []gh.PRRef
	for _, c := range calls {
		out = append(out, c...)
	}
	return out
}

// hoverFailingCheck opens the cursor row's expanded Checks tab on its job-99 check.
func hoverFailingCheck(t *testing.T, m *Model) {
	t.Helper()
	ps := m.section.(*PRSection)
	idx := slices.IndexFunc(ps.prAt(m.cursor).Checks(), func(c gh.Check) bool { return c.JobID() == "99" })
	if idx < 0 {
		t.Fatal("cursor row has no job-99 check")
	}
	m.expanded, m.expandedTab, m.checkCursor = true, tabChecks, idx
	m.renderExpanded()
}

func stackedRepoPR(repo string, n, pos int) gh.PR {
	return gh.PR{
		Repo: repo, Number: n, Title: "stack row", State: "OPEN",
		Stack: &gh.PRStack{Number: 3, Size: 2}, StackPosition: pos,
	}
}

// TestStacksDoNotCrossRepos: stack numbers are only unique within a repo, so
// two repos' "stack 3" must stay separate stacks, flat and under a bare query.
func TestStacksDoNotCrossRepos(t *testing.T) {
	prs := []gh.PR{
		stackedRepoPR("owner/repo", 10, 1), stackedRepoPR("owner/repo", 11, 2),
		stackedRepoPR("other/x", 20, 1), stackedRepoPR("other/x", 21, 2),
	}
	ref := func(repo string, n int) gh.PRRef { return gh.PRRef{Repo: repo, Number: n} }

	check := func(t *testing.T, m Model) {
		t.Helper()
		ps := m.section.(*PRSection)
		if ps.Len() != 4 {
			t.Fatalf("shown rows = %d, want 4", ps.Len())
		}
		for _, w := range []struct {
			repo       string
			n, rootNum int
			tree       string
		}{
			{"owner/repo", 10, 10, stackRootGlyph}, {"owner/repo", 11, 10, stackLastGlyph},
			{"other/x", 20, 20, stackRootGlyph}, {"other/x", 21, 20, stackLastGlyph},
		} {
			if got := ps.stackRoots[ref(w.repo, w.n)]; got != w.rootNum {
				t.Errorf("stackRoots[%s#%d] = %d, want %d", w.repo, w.n, got, w.rootNum)
			}
			if got := ps.stackTrees[ref(w.repo, w.n)]; got != w.tree {
				t.Errorf("stackTrees[%s#%d] = %q, want %q", w.repo, w.n, got, w.tree)
			}
		}
		for _, w := range []struct {
			repo   string
			n      int
			parent int
		}{{"owner/repo", 11, 10}, {"other/x", 21, 20}} {
			i := shownRepoPR(ps, w.repo, w.n)
			if got := ps.stackParentNumber(i); got != w.parent {
				t.Errorf("stackParentNumber(%s#%d) = %d, want %d", w.repo, w.n, got, w.parent)
			}
		}
		i := shownRepoPR(ps, "other/x", 21)
		card := triage.Preliminary(ps.prAt(i), "me", ps.stackParentNumber(i))
		if card.Headline != "Blocked on #20" {
			t.Errorf("other/x#21 headline = %q, want %q", card.Headline, "Blocked on #20")
		}
	}

	newModel := func() Model {
		m := NewModel(t.TempDir(), "is:open", nil)
		m.SetRepo("owner/repo")
		m.setPRs(prs)
		return m
	}

	t.Run("flat", func(t *testing.T) { check(t, newModel()) })
	t.Run("bare query", func(t *testing.T) {
		m := newModel()
		m.filterInput.SetValue("stack")
		m.applyFilter()
		if !m.section.(*PRSection).forceFlat {
			t.Fatal("a bare query should force the flat board")
		}
		check(t, m)
	})
}
