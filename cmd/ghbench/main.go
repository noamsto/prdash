// Command ghbench compares prdash's current gh-CLI hot path against an
// in-process githubv4 query fetching the equivalent field set, to measure the
// latency cost of shelling out to `gh` per request. Spike code — not shipped.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// prFields mirrors internal/gh.prFields so the gh path fetches exactly what
// prdash fetches on a refresh.
var prFields = []string{
	"number", "title", "author", "statusCheckRollup", "reviewDecision",
	"labels", "assignees", "headRefName", "baseRefName", "url", "updatedAt",
	"mergedAt", "closedAt", "isDraft", "state", "body", "autoMergeRequest",
}

// prNode is the githubv4 shape equivalent to one --json PR row, including the
// statusCheckRollup union (CheckRun | StatusContext) that dominates payload.
type prNode struct {
	Number         int
	Title          string
	Body           string
	URL            string `graphql:"url"`
	State          string
	IsDraft        bool
	HeadRefName    string
	BaseRefName    string
	ReviewDecision string
	UpdatedAt      githubv4.DateTime
	MergedAt       *githubv4.DateTime
	ClosedAt       *githubv4.DateTime
	Author         struct{ Login string }
	AutoMergeRequest *struct {
		MergeMethod string
	}
	Labels struct {
		Nodes []struct {
			Name  string
			Color string
		}
	} `graphql:"labels(first: 20)"`
	Assignees struct {
		Nodes []struct{ Login string }
	} `graphql:"assignees(first: 20)"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					Contexts struct {
						Nodes []struct {
							Typename string `graphql:"__typename"`
							CheckRun struct {
								Name       string
								Status     string
								Conclusion string
								DetailsURL string `graphql:"detailsUrl"`
								StartedAt  *githubv4.DateTime
								CheckSuite struct {
									WorkflowRun *struct {
										Workflow struct{ Name string }
									}
								}
							} `graphql:"... on CheckRun"`
							StatusContext struct {
								Context   string
								State     string
								TargetURL string `graphql:"targetUrl"`
							} `graphql:"... on StatusContext"`
						}
					} `graphql:"contexts(first: 100)"`
				}
			}
		}
	} `graphql:"commits(last: 1)"`
}

func main() {
	repo := flag.String("repo", "", "owner/name (default: gh repo view)")
	search := flag.String("search", "is:open", "search filter (as passed to gh pr list --search)")
	limit := flag.Int("limit", 30, "max PRs")
	n := flag.Int("n", 10, "timed iterations per path")
	flag.Parse()

	if *repo == "" {
		*repo = strings.TrimSpace(mustRun("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"))
	}
	token := strings.TrimSpace(mustRun("gh", "auth", "token"))

	ctx := context.Background()
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	client := githubv4.NewClient(httpClient)

	// gh pr list --search implicitly scopes to the repo and PRs; the raw search
	// API needs both qualifiers spelled out.
	searchStr := fmt.Sprintf("repo:%s is:pr %s", *repo, *search)

	fmt.Printf("repo=%s  search=%q  limit=%d  n=%d\n\n", *repo, *search, *limit, *n)

	ghPath := func() (int, error) {
		args := append([]string{"pr", "list", "--search", *search, "-L", strconv.Itoa(*limit), "--json", strings.Join(prFields, ",")}, "-R", *repo)
		cmd := exec.Command("gh", args...)
		out, err := cmd.Output()
		return len(out), err
	}

	v4Path := func() (int, error) {
		var q struct {
			Search struct {
				IssueCount int
				Nodes      []struct {
					PR prNode `graphql:"... on PullRequest"`
				}
			} `graphql:"search(query: $q, type: ISSUE, first: $limit)"`
		}
		vars := map[string]interface{}{
			"q":     githubv4.String(searchStr),
			"limit": githubv4.Int(*limit),
		}
		if err := client.Query(ctx, &q, vars); err != nil {
			return 0, err
		}
		b, _ := json.Marshal(q.Search.Nodes)
		return len(b), nil
	}

	spawnFloor := func() (int, error) {
		return 0, exec.Command("gh", "--version").Run()
	}

	bench("gh --version (spawn floor)", *n, spawnFloor)
	bench("gh pr list (current hot path)", *n, ghPath)
	bench("githubv4 search (in-process)", *n, v4Path)
}

func bench(name string, n int, fn func() (int, error)) {
	sz, err := fn() // warmup + payload sample
	if err != nil {
		fmt.Printf("%-32s  FAILED: %v\n", name, err)
		return
	}
	durs := make([]time.Duration, 0, n)
	for range n {
		t := time.Now()
		if _, err := fn(); err != nil {
			fmt.Printf("%-32s  FAILED: %v\n", name, err)
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
	fmt.Printf("%-32s  min %6s  med %6s  mean %6s  p90 %6s  max %6s   payload %d B\n",
		name, round(durs[0]), round(p(0.5)), round(sum/time.Duration(n)), round(p(0.9)), round(durs[n-1]), sz)
}

func round(d time.Duration) time.Duration { return d.Round(time.Millisecond) }

func mustRun(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
	return string(out)
}
