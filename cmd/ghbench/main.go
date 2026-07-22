// Command ghbench compares prdash's PR-list backends: the gh-CLI path
// (gh.CLISource) against the in-process githubv4 path (gh.GraphSource). It
// times both and, with -parity, diffs their parsed output to prove the
// githubv4 mapping matches what `gh pr list --json` produces. Spike code.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noamsto/prdash/internal/gh"
)

func main() {
	repo := flag.String("repo", "", "owner/name (default: gh repo view)")
	search := flag.String("search", "is:open", "search filter (as passed to gh pr list --search)")
	limit := flag.Int("limit", 30, "max PRs")
	n := flag.Int("n", 10, "timed iterations per path")
	parity := flag.Bool("parity", false, "diff CLI vs githubv4 PR-list output instead of timing")
	detailParity := flag.Bool("detail-parity", false, "diff gh pr view vs batched githubv4 detail")
	detailBench := flag.Bool("detail-bench", false, "time batched githubv4 detail vs N gh pr view subprocesses")
	issueParity := flag.Bool("issue-parity", false, "diff gh issue list/view vs githubv4 FetchIssues/FetchIssueDetail")
	viewerParity := flag.Bool("viewer-parity", false, "diff gh api user vs githubv4 FetchViewer")
	membersParity := flag.Bool("members-parity", false, "diff gh api graphql assignableUsers vs githubv4 FetchAssignableUsers")
	runsParity := flag.Bool("runs-parity", false, "diff gh run list vs the native REST ListRunsForBranch")
	branch := flag.String("branch", "", "branch for -runs-parity (default: current git branch)")
	flag.Parse()

	dir, _ := os.Getwd()
	runner := gh.ExecRunner{}
	if *repo == "" {
		r, err := gh.CurrentRepo(runner, dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "repo detect:", err)
			os.Exit(1)
		}
		*repo = r
	}
	token := strings.TrimSpace(mustRun("gh", "auth", "token"))

	graph := gh.NewGraphSource(token, *repo)

	fmt.Printf("repo=%s  search=%q  limit=%d\n\n", *repo, *search, *limit)

	// CLISource is bound to the current dir; the bench targets arbitrary repos,
	// so force gh at *repo with -R (harmless when *repo is the cwd repo) to keep
	// both sides querying the same PRs. This is the same code path CLISource runs,
	// plus one flag.
	cliAt := func() ([]gh.PR, error) {
		out, err := exec.Command("gh", append(gh.PRListArgs(*search, *limit), "-R", *repo)...).Output()
		if err != nil {
			return nil, err
		}
		return gh.ParsePRs(out)
	}

	if *parity {
		runParity(cliAt, graph, *search, *limit)
		return
	}
	if *detailParity {
		runDetailParity(graph, *repo, *search, *limit)
		return
	}
	if *detailBench {
		runDetailBench(graph, *repo, *search, *limit)
		return
	}
	if *issueParity {
		runIssueParity(graph, *repo, *search, *limit)
		return
	}
	if *viewerParity {
		runViewerParity(runner, dir, graph)
		return
	}
	if *membersParity {
		runMembersParity(runner, dir, graph, *repo)
		return
	}
	if *runsParity {
		b := *branch
		if b == "" {
			out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
			if err != nil {
				fmt.Fprintln(os.Stderr, "detect branch:", err)
				os.Exit(1)
			}
			b = strings.TrimSpace(string(out))
		}
		runRunsParity(graph, *repo, b)
		return
	}

	spawnFloor := func() (int, error) { return 0, exec.Command("gh", "--version").Run() }
	ghPath := func() (int, error) {
		prs, err := cliAt()
		return len(prs), err
	}
	v4Path := func() (int, error) {
		prs, _, err := graph.FetchPRs(*search, *limit)
		return len(prs), err
	}
	bench("gh --version (spawn floor)", *n, spawnFloor)
	bench("gh pr list (-R, current path)", *n, ghPath)
	bench("gh.GraphSource (githubv4)", *n, v4Path)
}

// runDetailParity diffs batched githubv4 detail against `gh pr view` for the
// first few PRs of the list, on the fields the UI renders.
func runDetailParity(graph gh.GraphSource, repo, search string, limit int) {
	prs, _, err := graph.FetchPRs(search, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}
	var nums []int
	for _, p := range prs {
		if len(nums) >= 8 {
			break
		}
		nums = append(nums, p.Number)
	}
	batch, _, err := graph.FetchDetails(nums)
	if err != nil {
		fmt.Fprintln(os.Stderr, "batch detail:", err)
		os.Exit(1)
	}
	fmt.Printf("comparing detail for %d PRs\n", len(nums))
	diffs := 0
	for _, n := range nums {
		out, err := exec.Command("gh", append(gh.PRViewArgs(n), "-R", repo)...).Output()
		if err != nil {
			fmt.Printf("  #%d: gh pr view failed: %v\n", n, err)
			diffs++
			continue
		}
		want, err := gh.ParsePRDetail(out)
		if err != nil {
			fmt.Printf("  #%d: parse gh detail: %v\n", n, err)
			diffs++
			continue
		}
		got := batch[n]
		if s := detailSig(got); s != detailSig(want) {
			fmt.Printf("  #%d mismatch\n    gh:  %s\n    v4:  %s\n", n, detailSig(want), s)
			diffs++
		}
	}
	if diffs == 0 {
		fmt.Println("DETAIL PARITY OK: every PR matches on rendered fields")
	} else {
		fmt.Printf("\n%d difference(s)\n", diffs)
	}
}

// runDetailBench compares one batched githubv4 detail request against fetching
// the same PRs' detail via one `gh pr view` subprocess each (the old settle-path
// cost). The prefetch window is 5–6 PRs, so that is the realistic batch size.
func runDetailBench(graph gh.GraphSource, repo, search string, limit int) {
	prs, _, err := graph.FetchPRs(search, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}
	var nums []int
	for _, p := range prs {
		if len(nums) >= 6 {
			break
		}
		nums = append(nums, p.Number)
	}
	fmt.Printf("detail for %d PRs (prefetch-window size)\n\n", len(nums))

	// Old settle path: the cursor + window fetched as concurrent tea.Cmds, i.e.
	// N `gh pr view` subprocesses forking at once.
	bench(fmt.Sprintf("%d× gh pr view concurrent (old)", len(nums)), 6, func() (int, error) {
		var wg sync.WaitGroup
		errs := make([]error, len(nums))
		for i, n := range nums {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = exec.Command("gh", append(gh.PRViewArgs(n), "-R", repo)...).Run()
			}()
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				return 0, e
			}
		}
		return len(nums), nil
	})

	// New settle path: cursor detail on its own + the rest batched, run
	// concurrently — two HTTP round trips, zero subprocesses.
	bench("githubv4 cursor + batched rest", 6, func() (int, error) {
		var wg sync.WaitGroup
		var e1, e2 error
		wg.Add(2)
		go func() { defer wg.Done(); _, _, e1 = graph.FetchDetails(nums[:1]) }()
		go func() { defer wg.Done(); _, _, e2 = graph.FetchDetails(nums[1:]) }()
		wg.Wait()
		if e1 != nil {
			return 0, e1
		}
		return len(nums), e2
	})
}

func detailSig(d gh.PRDetail) string {
	var reviewers []string
	for _, r := range d.ReviewRequests {
		reviewers = append(reviewers, r.Login)
	}
	sort.Strings(reviewers)
	ds := d.Diffstat()
	return fmt.Sprintf("comments=%d reviews=%d latest=%d merge=%s mergeable=%s draft=%v reqs=%v files=%d +%d-%d",
		len(d.Comments), len(d.Reviews), len(d.LatestReviews), d.MergeStateStatus, d.Mergeable, d.IsDraft,
		reviewers, ds.Files, ds.Additions, ds.Deletions)
}

// runParity fetches via both sources and reports the first field differences,
// after sorting by PR number and normalizing check ordering.
func runParity(cli func() ([]gh.PR, error), graph gh.PRSource, search string, limit int) {
	a, err := cli()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli:", err)
		os.Exit(1)
	}
	b, _, err := graph.FetchPRs(search, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "graph:", err)
		os.Exit(1)
	}
	byNum := func(prs []gh.PR) map[int]gh.PR {
		m := map[int]gh.PR{}
		for _, p := range prs {
			sort.Slice(p.StatusCheckRollup, func(i, j int) bool {
				return p.StatusCheckRollup[i].Label() < p.StatusCheckRollup[j].Label()
			})
			m[p.Number] = p
		}
		return m
	}
	ma, mb := byNum(a), byNum(b)
	fmt.Printf("cli PRs=%d  githubv4 PRs=%d\n", len(ma), len(mb))
	diffs := 0
	for num, pa := range ma {
		pb, ok := mb[num]
		if !ok {
			fmt.Printf("  #%d: only in cli\n", num)
			diffs++
			continue
		}
		// CIState/Checks are what the UI actually renders off the rollup, so
		// compare those semantics plus the scalar fields.
		if pa.CIState() != pb.CIState() {
			fmt.Printf("  #%d: CIState cli=%s v4=%s\n", num, pa.CIState(), pb.CIState())
			diffs++
		}
		if !scalarEqual(pa, pb) {
			fmt.Printf("  #%d: scalar mismatch\n    cli: %s\n    v4:  %s\n", num, scalars(pa), scalars(pb))
			diffs++
		}
	}
	if diffs == 0 {
		fmt.Println("PARITY OK: every PR matches on scalars + CI state")
	} else {
		fmt.Printf("\n%d difference(s)\n", diffs)
	}
}

func scalarEqual(a, b gh.PR) bool {
	return a.Title == b.Title && a.Author.Login == b.Author.Login &&
		a.ReviewDecision == b.ReviewDecision && a.HeadRefName == b.HeadRefName &&
		a.BaseRefName == b.BaseRefName && a.IsDraft == b.IsDraft && a.State == b.State &&
		a.AutoMergeEnabled() == b.AutoMergeEnabled() && reflect.DeepEqual(labelSet(a), labelSet(b))
}

func labelSet(p gh.PR) []string {
	var s []string
	for _, l := range p.Labels {
		s = append(s, l.Name)
	}
	sort.Strings(s)
	return s
}

func scalars(p gh.PR) string {
	return fmt.Sprintf("title=%q author=%s review=%s draft=%v state=%s automerge=%v labels=%v",
		p.Title, p.Author.Login, p.ReviewDecision, p.IsDraft, p.State, p.AutoMergeEnabled(), labelSet(p))
}

// runIssueParity diffs the githubv4 issue list against `gh issue list`, then
// samples a few issues and diffs githubv4 issue detail against `gh issue view`.
func runIssueParity(graph gh.GraphSource, repo, search string, limit int) {
	cliOut, err := exec.Command("gh", append(gh.IssueListArgs(search, limit), "-R", repo)...).Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli issue list:", err)
		os.Exit(1)
	}
	a, err := gh.ParseIssues(cliOut)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse cli issues:", err)
		os.Exit(1)
	}
	b, _, err := graph.FetchIssues(search, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "graph issue list:", err)
		os.Exit(1)
	}
	byNum := func(is []gh.Issue) map[int]gh.Issue {
		m := map[int]gh.Issue{}
		for _, i := range is {
			m[i.Number] = i
		}
		return m
	}
	ma, mb := byNum(a), byNum(b)
	fmt.Printf("cli issues=%d  githubv4 issues=%d\n", len(ma), len(mb))
	diffs := 0
	for num, ia := range ma {
		ib, ok := mb[num]
		if !ok {
			fmt.Printf("  #%d: only in cli\n", num)
			diffs++
			continue
		}
		if !issueScalarEqual(ia, ib) {
			fmt.Printf("  #%d: scalar mismatch\n    cli: %s\n    v4:  %s\n", num, issueScalars(ia), issueScalars(ib))
			diffs++
		}
	}
	if diffs == 0 {
		fmt.Println("ISSUE LIST PARITY OK: every issue matches on scalars")
	} else {
		fmt.Printf("\n%d list difference(s)\n", diffs)
	}

	var nums []int
	for _, i := range b {
		if len(nums) >= 5 {
			break
		}
		nums = append(nums, i.Number)
	}
	fmt.Printf("\ncomparing detail for %d issues\n", len(nums))
	detailDiffs := 0
	for _, n := range nums {
		out, err := exec.Command("gh", append(gh.IssueViewArgs(n), "-R", repo)...).Output()
		if err != nil {
			fmt.Printf("  #%d: gh issue view failed: %v\n", n, err)
			detailDiffs++
			continue
		}
		want, err := gh.ParseIssueDetail(out)
		if err != nil {
			fmt.Printf("  #%d: parse gh detail: %v\n", n, err)
			detailDiffs++
			continue
		}
		got, _, err := graph.FetchIssueDetail(n)
		if err != nil {
			fmt.Printf("  #%d: graph detail: %v\n", n, err)
			detailDiffs++
			continue
		}
		if got.Body != want.Body {
			fmt.Printf("  #%d: body mismatch\n    cli len=%d\n    v4  len=%d\n", n, len(want.Body), len(got.Body))
			detailDiffs++
		}
	}
	if detailDiffs == 0 {
		fmt.Println("ISSUE DETAIL PARITY OK: every issue body matches")
	} else {
		fmt.Printf("\n%d detail difference(s)\n", detailDiffs)
	}
}

func issueScalarEqual(a, b gh.Issue) bool {
	return a.Title == b.Title && a.Author.Login == b.Author.Login && a.URL == b.URL &&
		reflect.DeepEqual(issueLabelSet(a), issueLabelSet(b)) &&
		reflect.DeepEqual(issueAssigneeSet(a), issueAssigneeSet(b))
}

func issueLabelSet(i gh.Issue) []string {
	var s []string
	for _, l := range i.Labels {
		s = append(s, l.Name)
	}
	sort.Strings(s)
	return s
}

func issueAssigneeSet(i gh.Issue) []string {
	var s []string
	for _, a := range i.Assignees {
		s = append(s, a.Login)
	}
	sort.Strings(s)
	return s
}

func issueScalars(i gh.Issue) string {
	return fmt.Sprintf("title=%q author=%s labels=%v assignees=%v",
		i.Title, i.Author.Login, issueLabelSet(i), issueAssigneeSet(i))
}

// runViewerParity diffs the githubv4 viewer login against `gh api user`.
func runViewerParity(runner gh.Runner, dir string, graph gh.GraphSource) {
	want, err := gh.FetchViewerLogin(runner, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli viewer:", err)
		os.Exit(1)
	}
	got, err := graph.FetchViewer()
	if err != nil {
		fmt.Fprintln(os.Stderr, "graph viewer:", err)
		os.Exit(1)
	}
	fmt.Printf("cli login=%q  githubv4 login=%q\n", want, got)
	if got == want {
		fmt.Println("VIEWER PARITY OK")
	} else {
		fmt.Println("VIEWER MISMATCH")
	}
}

// runMembersParity diffs the githubv4 assignable-users list against
// `gh api graphql` with assignableUsersQuery.
func runMembersParity(runner gh.Runner, dir string, graph gh.GraphSource, repo string) {
	want, err := gh.FetchAssignableUsers(runner, dir, repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli members:", err)
		os.Exit(1)
	}
	got, _, err := graph.FetchAssignableUsers()
	if err != nil {
		fmt.Fprintln(os.Stderr, "graph members:", err)
		os.Exit(1)
	}
	byLogin := func(us []gh.User) map[string]gh.User {
		m := map[string]gh.User{}
		for _, u := range us {
			m[u.Login] = u
		}
		return m
	}
	ma, mb := byLogin(want), byLogin(got)
	fmt.Printf("cli members=%d  githubv4 members=%d\n", len(ma), len(mb))
	diffs := 0
	for login, ua := range ma {
		ub, ok := mb[login]
		if !ok {
			fmt.Printf("  %s: only in cli\n", login)
			diffs++
			continue
		}
		if ua.Name != ub.Name {
			fmt.Printf("  %s: name mismatch cli=%q v4=%q\n", login, ua.Name, ub.Name)
			diffs++
		}
	}
	for login := range mb {
		if _, ok := ma[login]; !ok {
			fmt.Printf("  %s: only in githubv4\n", login)
			diffs++
		}
	}
	if diffs == 0 {
		fmt.Println("MEMBERS PARITY OK: every assignable user matches")
	} else {
		fmt.Printf("\n%d difference(s)\n", diffs)
	}
}

func bench(name string, n int, fn func() (int, error)) {
	sz, err := fn() // warmup + count sample
	if err != nil {
		fmt.Printf("%-30s  FAILED: %v\n", name, err)
		return
	}
	durs := make([]time.Duration, 0, n)
	for range n {
		t := time.Now()
		if _, err := fn(); err != nil {
			fmt.Printf("%-30s  FAILED: %v\n", name, err)
			return
		}
		durs = append(durs, time.Since(t))
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	var sum time.Duration
	for _, d := range durs {
		sum += d
	}
	p := func(q float64) time.Duration { return durs[min(int(q*float64(n)), n-1)] }
	fmt.Printf("%-30s  min %6s  med %6s  mean %6s  p90 %6s  max %6s   (%d PRs)\n",
		name, r(durs[0]), r(p(0.5)), r(sum/time.Duration(n)), r(p(0.9)), r(durs[n-1]), sz)
}

func r(d time.Duration) time.Duration { return d.Round(time.Millisecond) }

// runRunsParity diffs `gh run list --branch <b> -L 20 --json
// databaseId,conclusion,headSha` against the native REST
// GraphSource.ListRunsForBranch, field-by-field, read-only (never reruns
// anything).
func runRunsParity(graph gh.GraphSource, repo, branch string) {
	out, err := exec.Command("gh", "run", "list", "--repo", repo, "--branch", branch, "-L", "20",
		"--json", "databaseId,conclusion,headSha").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli run list:", err)
		os.Exit(1)
	}
	var cliRuns []struct {
		DatabaseID int64  `json:"databaseId"`
		Conclusion string `json:"conclusion"`
		HeadSha    string `json:"headSha"`
	}
	if err := json.Unmarshal(out, &cliRuns); err != nil {
		fmt.Fprintln(os.Stderr, "parse cli run list:", err)
		os.Exit(1)
	}
	native, err := graph.ListRunsForBranch(branch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "native run list:", err)
		os.Exit(1)
	}
	fmt.Printf("branch=%s  cli runs=%d  native runs=%d\n", branch, len(cliRuns), len(native))
	diffs := 0
	for i := range cliRuns {
		if i >= len(native) {
			fmt.Printf("  [%d]: only in cli: %+v\n", i, cliRuns[i])
			diffs++
			continue
		}
		c, n := cliRuns[i], native[i]
		if c.DatabaseID != n.ID || c.Conclusion != n.Conclusion || c.HeadSha != n.HeadSHA {
			fmt.Printf("  [%d]: mismatch\n    cli:    id=%d conclusion=%q headSha=%q\n    native: id=%d conclusion=%q headSha=%q\n",
				i, c.DatabaseID, c.Conclusion, c.HeadSha, n.ID, n.Conclusion, n.HeadSHA)
			diffs++
		}
	}
	if len(native) > len(cliRuns) {
		for i := len(cliRuns); i < len(native); i++ {
			fmt.Printf("  [%d]: only in native: %+v\n", i, native[i])
			diffs++
		}
	}
	if diffs == 0 {
		fmt.Println("RUNS PARITY OK: every run matches on id/conclusion/headSha, same order")
	} else {
		fmt.Printf("\n%d difference(s)\n", diffs)
	}
}

func mustRun(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
	return string(out)
}
