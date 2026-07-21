// Command ghbench compares prdash's PR-list backends: the gh-CLI path
// (gh.CLISource) against the in-process githubv4 path (gh.GraphSource). It
// times both and, with -parity, diffs their parsed output to prove the
// githubv4 mapping matches what `gh pr list --json` produces. Spike code.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/noamsto/prdash/internal/gh"
)

func main() {
	repo := flag.String("repo", "", "owner/name (default: gh repo view)")
	search := flag.String("search", "is:open", "search filter (as passed to gh pr list --search)")
	limit := flag.Int("limit", 30, "max PRs")
	n := flag.Int("n", 10, "timed iterations per path")
	parity := flag.Bool("parity", false, "diff CLI vs githubv4 output instead of timing")
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

	graph := gh.GraphSource{Client: gh.NewGraphClient(token), Repo: *repo}

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

func mustRun(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
	return string(out)
}
