# Approve Action + Update-branch Checks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a bulk-capable `L` approve action to the PR board, and make an update-branch/rerun show its PRs' checks as in-progress instead of a stale green ✓.

**Architecture:** Approve follows the existing native-mutation seam exactly — a `GraphSource` method behind the `gh.MutationSource` interface, dispatched by `nativeMutationFn` with client-side pre-checks, bound as a `Scope:"per-selected"` default action so bulk comes free. The CI-rerun override is a `map[int]time.Time` on `Model`, applied at the two funnels where fetched PRs enter the board (`setPRs`, `setSections`), so every display site sees one consistent CI state without threading an override parameter.

**Tech Stack:** Go, `github.com/shurcooL/githubv4`, charmbracelet Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss v2.

**Spec:** `docs/superpowers/specs/2026-07-27-approve-action-design.md`
**Issue:** #58
**Branch/worktree:** `feat/58-approve-action` @ `~/Data/git/.worktrees/noamsto/prdash/feat-58-approve-action` (already created — all commands below run from that directory).

## Global Constraints

- **Mutations take the GraphQL node ID** (`gh.PR.ID`), never the PR number.
- **Pre-checks resolve synchronously**, on the calling goroutine, before returning the closure — the closure may run later from `runBulkNative`'s async batch and must close over plain values only, never `m`.
- **`p.ID == ""` is a stale-cache signal** (a cache entry written by the old gh-CLI prdash) and every native marker needing an ID must be listed in the guard at `internal/ui/actions.go:204`.
- **Never `tea.Batch` a blocking call in `Update`** — network work goes through a `tea.Cmd`.
- **Lean style:** match the file's existing conventions; comments explain non-obvious WHY only; no speculative robustness.
- **The pre-commit hook runs on commit** (`alejandra`, `deadnix`, `statix`, `typos`, whitespace). Go files are unaffected by the Nix linters but `typos` and trailing-whitespace apply.

---

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/gh/mutations_graphql.go` | `ApprovePR` GraphQL mutation | 1 |
| `internal/gh/source.go` | `MutationSource` interface + doc comment | 1 |
| `internal/gh/mutations_graphql_test.go` | Input-shape pin test | 1 |
| `internal/ui/mutationsource_test.go` | Fake + dispatch tests | 1, 2 |
| `internal/ui/actions.go` | `approve` dispatch case, pre-checks, `rerunCI` flag on `actionStat` | 2, 4 |
| `internal/action/defaults.go` | `L` binding; `u`/`r` past-tense wording | 2, 5 |
| `internal/ui/prlist.go` | Legend hint, `ciRerun` field + `applyCIRerun`, `delayedRefreshMsg` | 2, 4, 5 |
| `internal/ui/messages.go` | `delayedRefreshMsg` type | 5 |
| `internal/triage/triage.go` | `viewer` parameter, approve suggestion on awaiting-review | 3 |
| `internal/triage/triage_test.go` | Updated call sites + suppression tests | 3 |
| `internal/ui/preview.go`, `internal/ui/expanded.go` | Pass `m.viewerLogin` to triage | 3 |

---

### Task 1: `ApprovePR` mutation and seam

**Files:**
- Modify: `internal/gh/mutations_graphql.go` (append after `MarkReady`, ~line 49)
- Modify: `internal/gh/source.go:56-62` (the `MutationSource` interface)
- Modify: `internal/ui/mutationsource_test.go:17-51` (the fake)
- Test: `internal/gh/mutations_graphql_test.go`

**Interfaces:**
- Produces: `func (s GraphSource) ApprovePR(prID string) error`, and `ApprovePR(prID string) error` on the `gh.MutationSource` interface. Task 2 calls it as `src.ApprovePR(p.ID)`.
- Produces: `fakeMutationSource.approveCalls []string` — Task 2's tests assert against it.

- [ ] **Step 1: Write the failing input-shape test**

Append to `internal/gh/mutations_graphql_test.go`. This mirrors the existing tests' rationale — `s.client.Mutate` offers no fake seam, so the input construction is pinned here:

```go
func TestAddPullRequestReviewInputUsesApproveEventWithNoBody(t *testing.T) {
	event := githubv4.PullRequestReviewEventApprove
	input := githubv4.AddPullRequestReviewInput{
		PullRequestID: githubv4.ID("PR_test"),
		Event:         &event,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"event":"APPROVE"`) {
		t.Errorf("AddPullRequestReviewInput JSON = %s, want event:APPROVE", raw)
	}
	if strings.Contains(string(raw), `"body"`) {
		t.Errorf("AddPullRequestReviewInput JSON = %s, want no body — approve submits a bare review", raw)
	}
}
```

Also extend the file's header comment (lines 11-16) to name `ApprovePR`:

```go
// These tests mirror the Input construction inside MergePR, EnableAutoMerge,
// ApprovePR, and RequestReviews (mutations_graphql.go) to pin the
// destructive-path constants — squash merge method, APPROVE event, union:false —
// that s.client.Mutate gives no fake seam to observe. A future edit changing any
// of those values in the real method, without updating the mirror here, is the
// signal these tests exist to surface.
```

- [ ] **Step 2: Run it and confirm it passes trivially, then confirm the real method does not exist**

Run: `go test ./internal/gh/ -run TestAddPullRequestReviewInputUsesApproveEvent -v`
Expected: PASS (it only exercises githubv4 types).

Run: `rg -n "func \(s GraphSource\) ApprovePR" internal/gh/`
Expected: no output — the method is still missing. That absence is what Step 3 fixes; the real compile-time gate is Step 5, where the interface stops being satisfied.

- [ ] **Step 3: Add the mutation**

Append to `internal/gh/mutations_graphql.go`, after `MarkReady`:

```go
// ApprovePR submits a bare approving review, replacing `gh pr review --approve`.
// GitHub rejects approving your own PR with an opaque 422, so callers pre-check
// authorship (see nativeMutationFn) rather than letting that surface.
func (s GraphSource) ApprovePR(prID string) error {
	var m struct {
		AddPullRequestReview struct {
			PullRequestReview struct{ ID string }
		} `graphql:"addPullRequestReview(input: $input)"`
	}
	event := githubv4.PullRequestReviewEventApprove
	input := githubv4.AddPullRequestReviewInput{
		PullRequestID: githubv4.ID(prID),
		Event:         &event,
	}
	return s.client.Mutate(context.Background(), &m, input, nil)
}
```

- [ ] **Step 4: Add it to the interface**

In `internal/gh/source.go`, update the `MutationSource` doc comment and add the method:

```go
// MutationSource performs the PR-mutating actions (merge, auto-merge,
// mark-ready, update-branch, approve, request-reviewers) via githubv4. Every
// method takes the PR's GraphQL node ID (gh.PR.ID), not its number — mutation
// inputs require it. RequestReviews takes the full desired reviewer login set
// and always replaces (union:false); an empty set is the valid "remove all
// reviewers" encoding and callers must still fire it, not skip it.
type MutationSource interface {
	MergePR(prID string) error
	EnableAutoMerge(prID string) error
	MarkReady(prID string) error
	UpdateBranch(prID string) error
	ApprovePR(prID string) error
	RequestReviews(prID string, logins []string) error
}
```

- [ ] **Step 5: Run the build to see the fake break**

Run: `go build ./... && go vet ./internal/ui/`
Expected: `go build` PASSES; `go vet ./internal/ui/` FAILS with `*fakeMutationSource does not implement gh.MutationSource (missing method ApprovePR)` — the test fake is now behind the interface.

- [ ] **Step 6: Extend the fake**

In `internal/ui/mutationsource_test.go`, add the field to the struct (line 18) and the method after `UpdateBranch`:

```go
type fakeMutationSource struct {
	mergeCalls, autoMergeCalls, markReadyCalls, updateBranchCalls, approveCalls []string
	reviewCalls                                                                 []reviewCall
	err                                                                         error // returned by every call, to test failure propagation
}
```

```go
func (f *fakeMutationSource) ApprovePR(prID string) error {
	f.approveCalls = append(f.approveCalls, prID)
	return f.err
}
```

- [ ] **Step 7: Verify the tree builds and tests pass**

Run: `go build ./... && go test ./internal/gh/ ./internal/ui/`
Expected: both packages `ok`.

- [ ] **Step 8: Commit**

```bash
git add internal/gh/mutations_graphql.go internal/gh/source.go internal/gh/mutations_graphql_test.go internal/ui/mutationsource_test.go
git commit -m "feat(gh): add ApprovePR mutation to the MutationSource seam"
```

---

### Task 2: Approve dispatch, pre-checks, and the `L` binding

**Files:**
- Modify: `internal/ui/actions.go:201-234` (`nativeMutationFn`)
- Modify: `internal/action/defaults.go:3-38` (`DefaultPRActions`)
- Modify: `internal/ui/prlist.go:1968-1973` (`legendGroups` actions group)
- Test: `internal/ui/mutationsource_test.go`

**Interfaces:**
- Consumes: `gh.MutationSource.ApprovePR(prID string) error` and `fakeMutationSource.approveCalls` from Task 1.
- Produces: the `"approve"` native marker and the `"L"` default binding. Task 3's triage card points users at `L`.

- [ ] **Step 1: Write the failing dispatch tests**

Append to `internal/ui/mutationsource_test.go`. Note `mutationModel` does not set `viewerLogin`, so tests that exercise the self-approve guard set it explicitly:

```go
func TestApproveRoutesToNativeSource(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 31, ID: "pr31node", State: "OPEN"}})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.approveCalls) != 1 || fs.approveCalls[0] != "pr31node" {
		t.Errorf("approveCalls = %v, want [pr31node]", fs.approveCalls)
	}
}

// TestApproveSkipsOwnPR guards the pre-check that keeps a mixed Mine/Others bulk
// selection from half-failing on GitHub's opaque self-approval 422.
func TestApproveSkipsOwnPR(t *testing.T) {
	pr := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	pr.Author.Login = "me"
	m, fs := mutationModel(t, []gh.PR{pr})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (own PR)", msg)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none — the self-approve guard must short-circuit", fs.approveCalls)
	}
}

// TestApproveFiresWhenViewerUnknown documents the deliberate gap: with no
// resolved viewer login there is nothing to compare against, so the mutation
// fires and GitHub decides.
func TestApproveFiresWhenViewerUnknown(t *testing.T) {
	pr := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	pr.Author.Login = "me"
	m, fs := mutationModel(t, []gh.PR{pr}) // viewerLogin left ""
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %+v, want a successful actionDoneMsg", msg)
	}
	if len(fs.approveCalls) != 1 {
		t.Errorf("approveCalls = %v, want one call", fs.approveCalls)
	}
}

func TestApproveFailsWhenNotOpen(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 31, ID: "pr31node", State: "CLOSED"}})
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (closed PR)", msg)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none", fs.approveCalls)
	}
}

func TestApproveSkipsWhenNodeIDEmpty(t *testing.T) {
	m, fs := mutationModel(t, []gh.PR{{Number: 31, State: "OPEN"}}) // ID left unset
	m.viewerLogin = "me"
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	if done, ok := msg.(actionDoneMsg); !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a failed actionDoneMsg (empty node id)", msg)
	}
	if len(fs.approveCalls) != 0 {
		t.Errorf("approveCalls = %v, want none", fs.approveCalls)
	}
}

// TestApproveBulkSkipsOnlyOwnPR is the reason the guard exists: one aggregate
// failure out of two, with the other PR actually approved.
func TestApproveBulkSkipsOnlyOwnPR(t *testing.T) {
	mine := gh.PR{Number: 31, ID: "pr31node", State: "OPEN"}
	mine.Author.Login = "me"
	theirs := gh.PR{Number: 32, ID: "pr32node", State: "OPEN"}
	theirs.Author.Login = "you"
	m, fs := mutationModel(t, []gh.PR{mine, theirs})
	m.viewerLogin = "me"
	for i := 0; i < m.section.Len(); i++ {
		m.sel.toggle(i)
	}
	msg := driveBulk(t, m.runBulk(action.DefaultPRActions()["L"]))
	done, ok := msg.(actionDoneMsg)
	if !ok || done.err == nil {
		t.Fatalf("msg = %+v, want a partially-failed actionDoneMsg", msg)
	}
	if done.fail != "1 of 2 failed" {
		t.Errorf("fail = %q, want %q", done.fail, "1 of 2 failed")
	}
	if len(fs.approveCalls) != 1 || fs.approveCalls[0] != "pr32node" {
		t.Errorf("approveCalls = %v, want only [pr32node]", fs.approveCalls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run TestApprove -v`
Expected: FAIL — every case reports `msg = <nil>, want ...`, because `DefaultPRActions()["L"]` is the zero `action.Action` (no `L` key exists yet) so `runBulk` finds no native marker and returns nil.

- [ ] **Step 3: Add the dispatch case and pre-checks**

In `internal/ui/actions.go`, add `"approve"` to the empty-ID guard list:

```go
	if p.ID == "" {
		switch native {
		case "merge-squash", "auto-merge-squash", "mark-ready", "update-branch", "approve":
			err := fmt.Errorf("PR #%d node id unavailable (stale cache) — refresh and retry", p.Number)
			return func() error { return err }, true
		}
	}
```

Then add the case to the dispatch switch, after `"update-branch"`:

```go
	case "approve":
		if p.State != "OPEN" {
			err := fmt.Errorf("PR #%d is not open", p.Number)
			return func() error { return err }, true
		}
		// GitHub rejects self-approval with an opaque 422; caught here so a mixed
		// Mine/Others bulk selection reports which PRs were skipped and why.
		if m.viewerLogin != "" && p.Author.Login == m.viewerLogin {
			err := fmt.Errorf("can't approve your own PR #%d", p.Number)
			return func() error { return err }, true
		}
		return func() error { return src.ApprovePR(p.ID) }, true
```

Also extend the function's doc comment (line ~190) so the pre-check inventory stays accurate:

```go
// nativeMutationFn resolves the client-side pre-checks the research contracts
// specify (merge/auto-merge: PR state + cached mergeable; mark-ready: IsDraft;
// approve: PR state + self-approval) against live Model state on the calling
```

- [ ] **Step 4: Add the default binding**

In `internal/action/defaults.go`, add to the `DefaultPRActions` map after the `"M"` entry:

```go
		"L": {Key: "L", Label: "Approve",
			Command: Command{Native: "approve"}, Scope: "per-selected", Refresh: true,
			Confirm:  true,
			Progress: "Approving", Past: "Approved", Fail: "Approve failed"},
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run TestApprove -v`
Expected: all six PASS.

- [ ] **Step 6: Add the legend hint**

In `internal/ui/prlist.go`, the PR row of `legendGroups`' actions group:

```go
	if m.mode == "pr" {
		actions = append(actions, keyHint{"m", "merge"}, keyHint{"r", "rerun"}, keyHint{"u", "update"},
			keyHint{"M", "ready"}, keyHint{"L", "approve"})
	}
```

- [ ] **Step 7: Run the full UI + action suites**

Run: `go test ./internal/ui/ ./internal/action/`
Expected: both `ok`. If a legend snapshot/width test fails, it is asserting on the legend's rendered size — update its expectation to include the new hint rather than dropping the hint.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/actions.go internal/action/defaults.go internal/ui/prlist.go internal/ui/mutationsource_test.go
git commit -m "feat(ui): bulk-capable approve action bound to L"
```

---

### Task 3: Triage suggests approve on others' awaiting-review PRs

**Files:**
- Modify: `internal/triage/triage.go:41,53,75-77,94,100,115-116` (signatures + the two awaiting-review branches)
- Modify: `internal/ui/preview.go:334,336`
- Modify: `internal/ui/prlist.go:1917` (`cursorCard`)
- Modify: `internal/ui/expanded.go:154`
- Test: `internal/triage/triage_test.go`

**Interfaces:**
- Consumes: the `"L"` binding from Task 2 (as the card's `ActionKey`).
- Produces: `triage.Compute(pr gh.PR, d gh.PRDetail, viewer string) Card` and `triage.Preliminary(pr gh.PR, viewer string) Card`. Both are the only exported entry points; every caller must pass a viewer login (`""` when unknown).

- [ ] **Step 1: Write the failing tests**

Append to `internal/triage/triage_test.go`:

```go
// TestAwaitingReviewSuggestsApprove covers the three viewer cases: another
// author's PR gets the one-key approve, your own never does (GitHub forbids
// self-approval), and an unresolved viewer stays silent rather than guessing.
func TestAwaitingReviewSuggestsApprove(t *testing.T) {
	tests := []struct {
		name   string
		author string
		viewer string
		wantKey string
	}{
		{"others PR offers approve", "you", "me", "L"},
		{"own PR does not", "me", "me", ""},
		{"unknown viewer does not", "you", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := gh.PR{Number: 1, State: "OPEN", ReviewDecision: "REVIEW_REQUIRED"}
			pr.Author.Login = tt.author
			c := Compute(pr, gh.PRDetail{MergeStateStatus: "CLEAN"}, tt.viewer)
			if c.Kind != KindAwaitingReview {
				t.Fatalf("Kind = %v, want KindAwaitingReview", c.Kind)
			}
			if c.ActionKey != tt.wantKey {
				t.Errorf("ActionKey = %q, want %q", c.ActionKey, tt.wantKey)
			}
		})
	}
}

func TestPreliminaryAwaitingReviewSuggestsApprove(t *testing.T) {
	pr := gh.PR{Number: 1, State: "OPEN", ReviewDecision: "REVIEW_REQUIRED"}
	pr.Author.Login = "you"
	if got := Preliminary(pr, "me").ActionKey; got != "L" {
		t.Errorf("ActionKey = %q, want L", got)
	}
	pr.Author.Login = "me"
	if got := Preliminary(pr, "me").ActionKey; got != "" {
		t.Errorf("ActionKey = %q on own PR, want empty", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/triage/ -run TestAwaitingReviewSuggestsApprove -v`
Expected: FAIL to compile — `too many arguments in call to Compute`.

- [ ] **Step 3: Thread `viewer` through triage**

In `internal/triage/triage.go`, change the two exported signatures and their internal helpers:

```go
// Compute returns the highest-priority triage card for pr given its detail.
// Merge-state comes from detail (reliable per-PR); checks come from the PR rollup.
// viewer is the authenticated user's login ("" when unresolved) — it gates the
// approve suggestion, which GitHub forbids on your own PR.
func Compute(pr gh.PR, d gh.PRDetail, viewer string) Card {
	mss := d.MergeStateStatus
	failing := checksByState(pr, "fail")
	pending := checksByState(pr, "pending")

	c := computeCard(pr, d, mss, failing, pending, viewer)
	c.AutoMerge = pr.AutoMergeEnabled()
	return c
}
```

```go
func computeCard(pr gh.PR, d gh.PRDetail, mss string, failing, pending []string, viewer string) Card {
```

In `computeCard`, the `REVIEW_REQUIRED` branch becomes:

```go
	case pr.ReviewDecision == "REVIEW_REQUIRED":
		return awaitingReviewCard(awaitingHeadline(d), pr.Author.Login, viewer)
```

`Preliminary` and `preliminaryCard`:

```go
func Preliminary(pr gh.PR, viewer string) Card {
	c := preliminaryCard(pr, viewer)
	c.AutoMerge = pr.AutoMergeEnabled()
	return c
}

func preliminaryCard(pr gh.PR, viewer string) Card {
```

and its `REVIEW_REQUIRED` branch:

```go
	case pr.ReviewDecision == "REVIEW_REQUIRED":
		return awaitingReviewCard("Awaiting review", pr.Author.Login, viewer)
```

Add the shared constructor next to `awaitingHeadline`:

```go
// awaitingReviewCard offers the one-key approve only on someone else's PR —
// GitHub rejects self-approval, and an unresolved viewer ("") can't be
// distinguished from yourself, so it stays informational.
func awaitingReviewCard(headline, author, viewer string) Card {
	c := Card{Kind: KindAwaitingReview, Headline: headline, JumpTab: "reviews"}
	if viewer != "" && author != viewer {
		c.ActionKey, c.ActionLabel = "L", "approve"
	}
	return c
}
```

- [ ] **Step 4: Update the three production call sites**

`internal/ui/preview.go` (~line 334):

```go
		tc := triage.Preliminary(pr, m.viewerLogin)
		if cached {
			tc = triage.Compute(pr, d, m.viewerLogin)
		}
```

`internal/ui/prlist.go`, `cursorCard` (~line 1917):

```go
	return triage.Compute(ps.prAt(m.cursor), d, m.viewerLogin), true
```

`internal/ui/expanded.go` (~line 154):

```go
				m.expandedTab = jumpTabIndex(triage.Compute(ps.prAt(m.cursor), d, m.viewerLogin).JumpTab)
```

- [ ] **Step 5: Update the existing triage test call sites**

Run: `go test ./internal/triage/ 2>&1 | head -20`
Expected: compile errors listing each `Compute(...)`/`Preliminary(...)` call with too few arguments (13 sites).

Fix them by appending `, ""` to each existing call — `""` means "viewer unknown", which preserves every existing assertion (no card gains an `ActionKey`):

```bash
sed -i -E 's/(\bCompute\(pr, [A-Za-z0-9_.{}]+)\)/\1, "")/; s/(\bPreliminary\(pr)\)/\1, "")/' internal/triage/triage_test.go
```

That regex only covers the common shapes. After running it, re-run the build and fix any remaining call by hand — do not leave a call that passes a real login unless the test intends one.

Run: `go vet ./internal/triage/`
Expected: no output.

- [ ] **Step 6: Run all affected suites**

Run: `go build ./... && go test ./internal/triage/ ./internal/ui/`
Expected: both `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/triage/ internal/ui/preview.go internal/ui/prlist.go internal/ui/expanded.go
git commit -m "feat(triage): offer L approve on others' awaiting-review PRs"
```

---

### Task 4: Optimistic checks-in-progress after update-branch / rerun

**Files:**
- Modify: `internal/ui/prlist.go` — `Model` struct (~line 113 area), `NewModel` (~line 135), `setPRs:177`, `setSections:205`, `actionDoneMsg` handler `:1300-1320`
- Modify: `internal/ui/actions.go` — `actionStat` struct `:253`, `statFor:265`
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Produces: `Model.ciRerun map[int]time.Time`, `func (m *Model) applyCIRerun(prs []gh.PR) []gh.PR`, `func hasPendingCheck(p gh.PR) bool`, `const ciRerunWindow = 2 * time.Minute`, and `actionStat.rerunCI bool`. Task 5 reads `actionStat.rerunCI` to decide whether to schedule its delayed refresh.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/actions_test.go`:

```go
// TestUpdateBranchPaintsChecksInProgress covers the staleness this override
// exists for: GitHub keeps serving the pre-push rollup for seconds after
// update-branch, which would otherwise show a green ✓ on a branch whose checks
// are about to start over.
func TestUpdateBranchPaintsChecksInProgress(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.ciRerun[13] = time.Now().Add(ciRerunWindow)

	got := m.applyCIRerun([]gh.PR{{
		Number:            13,
		StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}},
	}})
	if got[0].CIState() != "pending" {
		t.Errorf("CIState = %q, want pending", got[0].CIState())
	}
}

func TestApplyCIRerunLeavesUnstampedPRsAlone(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now().Add(ciRerunWindow)

	got := m.applyCIRerun([]gh.PR{{
		Number:            99,
		StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}},
	}})
	if got[0].CIState() != "pass" {
		t.Errorf("CIState = %q, want pass — PR 99 was never stamped", got[0].CIState())
	}
}

// TestApplyCIRerunClearsWhenRealPendingArrives stops the override from
// outliving its usefulness: once GitHub reports work in flight, the real rollup
// is authoritative.
func TestApplyCIRerunClearsWhenRealPendingArrives(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now().Add(ciRerunWindow)

	m.applyCIRerun([]gh.PR{{
		Number:            13,
		StatusCheckRollup: []gh.Check{{Name: "build", State: "IN_PROGRESS"}},
	}})
	if _, still := m.ciRerun[13]; still {
		t.Error("ciRerun[13] survived a rollup that already reports pending")
	}
}

// TestApplyCIRerunExpires bounds the lie: a PR whose workflows never re-fire
// (path filters, no push trigger) self-corrects instead of spinning forever.
func TestApplyCIRerunExpires(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now().Add(-time.Second) // already past

	got := m.applyCIRerun([]gh.PR{{
		Number:            13,
		StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}},
	}})
	if got[0].CIState() != "pass" {
		t.Errorf("CIState = %q, want pass — the override expired", got[0].CIState())
	}
	if _, still := m.ciRerun[13]; still {
		t.Error("expired ciRerun entry was not pruned")
	}
}

// TestApplyCIRerunDoesNotMutateInput guards the shared cache: fetched PR values
// are handed out by the cache layer, so the override must copy the rollup rather
// than write through the caller's backing array.
func TestApplyCIRerunDoesNotMutateInput(t *testing.T) {
	m, _ := mutationModel(t, nil)
	m.ciRerun[13] = time.Now().Add(ciRerunWindow)

	in := []gh.PR{{Number: 13, StatusCheckRollup: []gh.Check{{Name: "build", Conclusion: "SUCCESS"}}}}
	m.applyCIRerun(in)
	if in[0].StatusCheckRollup[0].Conclusion != "SUCCESS" {
		t.Error("applyCIRerun wrote through to the caller's rollup")
	}
}

// TestUpdateBranchStampsCIRerun wires the flag end-to-end: a settled
// update-branch marks its PRs, a merge does not.
func TestUpdateBranchStampsCIRerun(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["u"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if _, ok := m.ciRerun[13]; !ok {
		t.Error("a successful update-branch must stamp ciRerun")
	}
}

func TestMergeDoesNotStampCIRerun(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["m"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if _, ok := m.ciRerun[13]; ok {
		t.Error("merge does not re-trigger checks and must not stamp ciRerun")
	}
}

func TestFailedUpdateBranchDoesNotStampCIRerun(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["u"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{err: errors.New("boom")})
	m = u.(Model)
	if _, ok := m.ciRerun[13]; ok {
		t.Error("a failed update-branch must not stamp ciRerun")
	}
}
```

Ensure the file imports `errors`, `time`, `github.com/noamsto/prdash/internal/action`, and `github.com/noamsto/prdash/internal/gh`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run 'TestApplyCIRerun|TestUpdateBranch|TestMergeDoesNot|TestFailedUpdate' -v`
Expected: FAIL to compile — `m.ciRerun undefined`, `m.applyCIRerun undefined`, `ciRerunWindow undefined`.

- [ ] **Step 3: Add the model field and the override**

In `internal/ui/prlist.go`, add to the `Model` struct next to `detail`/`fresh`:

```go
	ciRerun map[int]time.Time // PR number → expiry of its optimistic checks-in-progress state
```

In `NewModel`, alongside the other map initializers (~line 135):

```go
		detail:  map[int]gh.PRDetail{}, fresh: map[int]bool{},
		ciRerun: map[int]time.Time{},
```

Add the override itself near `setPRs`:

```go
// ciRerunWindow is how long a PR keeps its optimistic checks-in-progress state
// after an update-branch or rerun. Long enough for GitHub to queue the new runs,
// short enough that a PR whose workflows never re-fire (path filters, no push
// trigger) self-corrects instead of showing a permanent phantom spinner.
const ciRerunWindow = 2 * time.Minute

// applyCIRerun repaints the checks of PRs that just had them re-triggered as
// in-progress. GitHub keeps serving the pre-push rollup for several seconds
// after update-branch, so without this the row shows a stale ✓ for a branch
// whose checks are about to start over. Both board funnels (setPRs,
// setSections) run fetched PRs through it, so rows, preview, expanded, triage
// and filter all read one consistent CI state.
//
// Entries clear as soon as the real rollup reports work in flight, or when the
// window expires — the override never outlives evidence.
func (m *Model) applyCIRerun(prs []gh.PR) []gh.PR {
	if len(m.ciRerun) == 0 {
		return prs
	}
	now := time.Now()
	out := make([]gh.PR, len(prs))
	copy(out, prs)
	for i, p := range out {
		exp, stamped := m.ciRerun[p.Number]
		if !stamped {
			continue
		}
		if now.After(exp) || hasPendingCheck(p) {
			delete(m.ciRerun, p.Number)
			continue
		}
		// Copy before writing: these PR values come from the shared cache.
		rollup := make([]gh.Check, len(p.StatusCheckRollup))
		copy(rollup, p.StatusCheckRollup)
		for j := range rollup {
			rollup[j].State, rollup[j].Conclusion = "IN_PROGRESS", ""
		}
		out[i].StatusCheckRollup = rollup
	}
	return out
}

// hasPendingCheck reports whether the rollup already shows work in flight.
func hasPendingCheck(p gh.PR) bool {
	for _, c := range p.Checks() {
		if c.Result() == "pending" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Apply it at both funnels**

At the top of `setPRs`:

```go
func (m *Model) setPRs(prs []gh.PR) {
	prs = m.applyCIRerun(prs)
	if s, ok := m.section.(*PRSection); ok {
```

In `setSections`, just before the `if s, ok := m.section.(*PRSection); ok {` block:

```go
	all = m.applyCIRerun(all)
	if s, ok := m.section.(*PRSection); ok {
```

- [ ] **Step 5: Flag the actions that re-trigger checks**

In `internal/ui/actions.go`, add the field to `actionStat`:

```go
	refresh bool  // true when the action mutated the PR(s) → refetch on success
	rerunCI bool  // true when the action re-triggers CI → paint checks in-progress until GitHub catches up
	nums    []int // PR numbers the action touched, for detail-freshness invalidation
```

and set it in `statFor` (which `statForBulk` also funnels through, so every single and bulk path is covered):

```go
func statFor(a action.Action) *actionStat {
	run, ok, fail := a.Progress, a.Past, a.Fail
	if run == "" {
		run = a.Label
	}
	if ok == "" {
		ok = a.Label
	}
	if fail == "" {
		fail = a.Label
	}
	return &actionStat{run: run, ok: ok, fail: fail, rerunCI: rerunsCI(a)}
}

// rerunsCI reports whether an action causes GitHub to queue fresh check runs —
// update-branch pushes a merge commit, rerun-failed re-dispatches the workflows.
func rerunsCI(a action.Action) bool {
	return a.Command.Native == "update-branch" || a.Command.Builtin == "rerun-failed"
}
```

- [ ] **Step 6: Stamp on success**

In `internal/ui/prlist.go`'s `actionDoneMsg` case, inside the existing success branch:

```go
		cmds := []tea.Cmd{clearStatusCmd()}
		if msg.err == nil && m.actionStatus.refresh {
			for _, n := range m.actionStatus.nums {
				delete(m.fresh, n) // force the detail/summary to revalidate
			}
			cmds = append(cmds, m.backgroundRefresh())
		}
		if msg.err == nil && m.actionStatus.rerunCI {
			exp := time.Now().Add(ciRerunWindow)
			for _, n := range m.actionStatus.nums {
				m.ciRerun[n] = exp
			}
		}
		return m, tea.Batch(cmds...)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestApplyCIRerun|TestUpdateBranch|TestMergeDoesNot|TestFailedUpdate' -v`
Expected: all PASS.

- [ ] **Step 8: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: all packages `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/prlist.go internal/ui/actions.go internal/ui/actions_test.go
git commit -m "feat(ui): show checks as in-progress after update-branch or rerun"
```

---

### Task 5: Re-run wording and the delayed second refresh

**Files:**
- Modify: `internal/action/defaults.go` (the `r` and `u` entries)
- Modify: `internal/ui/messages.go` (add `delayedRefreshMsg`)
- Modify: `internal/ui/prlist.go` (`actionDoneMsg` handler, new `delayedRefreshMsg` case)
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: `actionStat.rerunCI` from Task 4.
- Produces: `delayedRefreshMsg` and `const ciRerunRecheck = 12 * time.Second`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/actions_test.go`. Both tests assert on behavior rather than on the batch's internal shape — `tea.Tick`'s command blocks for its full delay when called, so a test must not drive it:

```go
// TestDelayedRefreshMsgTriggersFetch is the behavior that matters: when the
// scheduled tick lands, the board refetches.
func TestDelayedRefreshMsgTriggersFetch(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.refreshing = false

	u, cmd := m.Update(delayedRefreshMsg{})
	m = u.(Model)
	if !m.refreshing {
		t.Error("delayedRefreshMsg must start a refresh")
	}
	if cmd == nil {
		t.Error("delayedRefreshMsg must return the fetch command")
	}
}

// TestRerunsCIClassifiesActions pins which actions get the follow-up refetch and
// the optimistic in-progress paint: the two that re-trigger CI, and nothing else.
func TestRerunsCIClassifiesActions(t *testing.T) {
	defaults := action.DefaultPRActions()
	for key, want := range map[string]bool{"u": true, "r": true, "m": false, "A": false, "M": false, "L": false} {
		if got := rerunsCI(defaults[key]); got != want {
			t.Errorf("rerunsCI(%q) = %v, want %v", key, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run 'TestDelayedRefresh|TestRerunsCI' -v`
Expected: FAIL to compile — `delayedRefreshMsg` undefined. (`rerunsCI` was added in Task 4; if Task 4 is not yet done, that symbol is undefined too.)

- [ ] **Step 3: Add the message and its command**

In `internal/ui/messages.go`:

```go
// delayedRefreshMsg is the second, later refetch scheduled after an action that
// re-triggers CI: the immediate refetch races GitHub's queueing and returns the
// pre-push rollup, so a follow-up is what actually brings the new runs in.
type delayedRefreshMsg struct{}
```

In `internal/ui/prlist.go`, next to `ciRerunWindow`:

```go
// ciRerunRecheck is how long to wait before the follow-up refetch — long enough
// for GitHub to have queued the re-triggered runs, well inside ciRerunWindow so
// the optimistic state is replaced by real data rather than expiring into a
// stale one.
const ciRerunRecheck = 12 * time.Second

func delayedRefreshCmd() tea.Cmd {
	return tea.Tick(ciRerunRecheck, func(time.Time) tea.Msg { return delayedRefreshMsg{} })
}
```

- [ ] **Step 4: Schedule it and handle it**

In the `actionDoneMsg` case, extend the `rerunCI` branch added in Task 4:

```go
		if msg.err == nil && m.actionStatus.rerunCI {
			exp := time.Now().Add(ciRerunWindow)
			for _, n := range m.actionStatus.nums {
				m.ciRerun[n] = exp
			}
			cmds = append(cmds, delayedRefreshCmd())
		}
```

Add the handler case next to `actionClearMsg`:

```go
	case delayedRefreshMsg:
		return m, m.backgroundRefresh()
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestDelayedRefresh|TestRerunsCI' -v`
Expected: both PASS.

- [ ] **Step 6: Update the past-tense wording**

In `internal/action/defaults.go`:

```go
		"r": {Key: "r", Label: "Rerun checks",
			Command: Command{Builtin: "rerun-failed"}, Scope: "single", Refresh: true,
			Progress: "Rerunning checks", Past: "Checks re-running", Fail: "Rerun failed"},
```

```go
		"u": {Key: "u", Label: "Update branch",
			Command: Command{Native: "update-branch"}, Scope: "per-selected", Refresh: true,
			Progress: "Updating branch", Past: "Branch updated — checks re-running", Fail: "Update failed"},
```

- [ ] **Step 7: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: all packages `ok`. If a test asserts on the old `"Checks rerun"` or `"Branch updated"` strings, update its expectation.

- [ ] **Step 8: Commit**

```bash
git add internal/action/defaults.go internal/ui/messages.go internal/ui/prlist.go internal/ui/actions_test.go
git commit -m "feat(ui): re-check CI shortly after update-branch and say so"
```

---

## Final verification

- [ ] **Run everything**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: all `ok`, no vet output.

- [ ] **Manual smoke test** (needs a real repo with an open PR you didn't author)

```bash
go run ./cmd/prdash
```

1. Cursor a PR by another author → press `L` → the confirm reads `Approve #N by <author>?` → `y` → badge shows `Approving` then `Approved`, and the review dot flips after the refresh.
2. Cursor one of your own PRs → `L` → `y` → badge shows the `can't approve your own PR #N` failure, no network call made.
3. Select two PRs (one yours, one not) with `space` → `L` → confirm reads `Approve for 2 PRs?` → badge settles to `1 of 2 failed`.
4. A PR with passing checks → `u` → badge reads `Branch updated — checks re-running`, and the row's CI glyph flips from `✓` to `●` immediately, staying there until the real runs appear.
5. `?` → the legend's actions group lists `L approve`.

- [ ] **Open the PR**

```bash
gh pr create --assignee @me --title "feat(ui): approve action + express update-branch check re-runs" --body "Closes #58"
```
