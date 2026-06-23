# prdash Redesign — Phase B: Dynamic triage card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the side preview into a dynamic triage card that leads with the focused PR's top merge-blocker (driven by GitHub's `mergeStateStatus`) plus the one-key fix, instead of a passive comment dump.

**Architecture:** Extend the per-PR detail fetch with `mergeStateStatus`/`mergeable`/`reviewRequests`/`files` and the list fetch with `isDraft`; teach `gh.Check` to carry a display name. A new pure `internal/triage` package computes a ranked `Card` from a PR + its detail via the spec's priority ladder (table-tested, no UI). The UI renders that card at the top of the side pane (the comment timeline stays beneath it until Phase C moves it to the expanded view), and two new actions — `u` (update-branch) and overlay-only Mark-ready — back the card's suggestions.

**Tech Stack:** Go 1.26, bubbletea v1, lipgloss. Spec: `docs/superpowers/specs/2026-06-23-prdash-tui-redesign-design.md` (§"The triage ladder", §"Data", §"Action set"). Builds on Phase A.

---

## Conventions

- Work on `feat/redesign-phase-b` (already created from `main`). TDD; one logical change per commit. After each task: `go build ./... && go vet ./... && gofmt -l .` clean.
- The triage card is built from the **focused PR (list data)** + its **detail fetch** — never the bulk list for merge-state (per spec §2).

## File structure (Phase B)

- `internal/gh/prs.go` — **modify.** Add `isDraft` to `prFields`; add `IsDraft` to `PR`; extend `Check` with name fields + a `Label()` helper.
- `internal/gh/prview.go` — **modify.** Extend `PRViewArgs` + `PRDetail` with `mergeStateStatus`, `mergeable`, `reviewRequests`, `files`; parse `latestReviews`.
- `internal/gh/prview_test.go` — **modify.** Assert the new fields parse.
- `internal/triage/triage.go` — **new.** `Card`, `Kind`, `Compute(pr, detail) Card`.
- `internal/triage/triage_test.go` — **new.** Table tests over the ladder.
- `internal/ui/card.go` — **new.** `renderCard(triage.Card, width int) string`.
- `internal/ui/card_test.go` — **new.**
- `internal/ui/preview.go` — **modify.** `previewPane` leads with the card, timeline beneath.
- `internal/action/defaults.go` — **modify.** Add `u` (update-branch) + `ready` (Mark ready) to `DefaultPRActions`.
- `internal/action/defaults_test.go` — **modify.** Assert the new actions.

---

## Task 1: Extend `gh` data (Check names, isDraft, detail merge-state)

**Files:**
- Modify: `internal/gh/prs.go`, `internal/gh/prview.go`
- Modify: `internal/gh/prview_test.go`

- [ ] **Step 1: Write failing tests for the new parsing**

Add to `internal/gh/prview_test.go`:

```go
func TestParsePRDetailMergeState(t *testing.T) {
	d, err := ParsePRDetail([]byte(`{
		"mergeStateStatus":"BLOCKED","mergeable":"MERGEABLE","isDraft":false,
		"reviewRequests":[{"login":"octocat"}],
		"files":[{"path":"a.go","additions":10,"deletions":2},{"path":"b.go","additions":1,"deletions":1}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.MergeStateStatus != "BLOCKED" || d.Mergeable != "MERGEABLE" {
		t.Fatalf("merge state not parsed: %+v", d)
	}
	if len(d.ReviewRequests) != 1 || d.ReviewRequests[0].Login != "octocat" {
		t.Fatalf("review requests: %+v", d.ReviewRequests)
	}
	if d.Diffstat().Files != 2 || d.Diffstat().Additions != 11 || d.Diffstat().Deletions != 3 {
		t.Fatalf("diffstat: %+v", d.Diffstat())
	}
}

func TestCheckLabel(t *testing.T) {
	if got := (Check{Name: "test (ubuntu)"}).Label(); got != "test (ubuntu)" {
		t.Errorf("name-first: %q", got)
	}
	if got := (Check{WorkflowName: "CI"}).Label(); got != "CI" {
		t.Errorf("workflow fallback: %q", got)
	}
	if got := (Check{Context: "ci/circleci"}).Label(); got != "ci/circleci" {
		t.Errorf("context fallback: %q", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/gh/ -run 'TestParsePRDetailMergeState|TestCheckLabel'`
Expected: FAIL — undefined fields/methods.

- [ ] **Step 3: Extend `Check` + `PR` in `internal/gh/prs.go`**

Replace the `Check` struct and add `Label()`; add `IsDraft` to `PR`; add `isDraft` to `prFields`:

```go
var prFields = []string{
	"number", "title", "author", "statusCheckRollup", "reviewDecision",
	"labels", "assignees", "headRefName", "baseRefName", "url", "updatedAt", "isDraft",
}

type Check struct {
	State        string `json:"state"`
	Conclusion   string `json:"conclusion"`
	Name         string `json:"name"`         // CheckRun
	WorkflowName string `json:"workflowName"` // CheckRun
	Context      string `json:"context"`      // StatusContext (no name)
}

// Label is the display name for a check, handling the CheckRun/StatusContext union.
func (c Check) Label() string {
	switch {
	case c.Name != "":
		return c.Name
	case c.WorkflowName != "":
		return c.WorkflowName
	default:
		return c.Context
	}
}
```

Add `IsDraft bool` to the `PR` struct (after `UpdatedAt`):

```go
	UpdatedAt   time.Time `json:"updatedAt"`
	IsDraft     bool      `json:"isDraft"`
```

(Note: `CIState()` already resolves state→conclusion; leave it unchanged.)

- [ ] **Step 4: Extend `PRDetail` + `PRViewArgs` in `internal/gh/prview.go`**

```go
type ReviewRequest struct {
	Login string `json:"login"`
}

type DiffFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type Diffstat struct {
	Files, Additions, Deletions int
}

type PRDetail struct {
	Comments         []Comment       `json:"comments"`
	Reviews          []Review        `json:"reviews"`
	LatestReviews    []Review        `json:"latestReviews"`
	MergeStateStatus string          `json:"mergeStateStatus"`
	Mergeable        string          `json:"mergeable"`
	IsDraft          bool            `json:"isDraft"`
	ReviewRequests   []ReviewRequest `json:"reviewRequests"`
	Files            []DiffFile      `json:"files"`
}

// Diffstat aggregates the per-file changes into totals for the card/Diff tab.
func (d PRDetail) Diffstat() Diffstat {
	s := Diffstat{Files: len(d.Files)}
	for _, f := range d.Files {
		s.Additions += f.Additions
		s.Deletions += f.Deletions
	}
	return s
}

func PRViewArgs(number int) []string {
	return []string{"pr", "view", strconv.Itoa(number), "--json",
		"comments,reviews,latestReviews,mergeStateStatus,mergeable,isDraft,reviewRequests,files"}
}
```

- [ ] **Step 5: Run, verify pass**

Run: `go test ./internal/gh/ -v`
Expected: PASS (new tests + existing `TestParsePRDetail`, `TestCheckLabel`, all gh tests).

- [ ] **Step 6: Commit**

```bash
git add internal/gh/
git commit -m "feat(gh): merge-state + diffstat + reviewRequests on detail; check Label() + isDraft"
```

---

## Task 2: `internal/triage` — the ranked card (pure, table-tested)

**Files:**
- Create: `internal/triage/triage.go`, `internal/triage/triage_test.go`

- [ ] **Step 1: Write the failing table test**

`internal/triage/triage_test.go`:

```go
package triage

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func pr(rollup ...gh.Check) gh.PR { return gh.PR{Number: 1, StatusCheckRollup: rollup} }

func TestLadderPriority(t *testing.T) {
	fail := gh.Check{State: "FAILURE"}
	pass := gh.Check{State: "SUCCESS"}
	cases := []struct {
		name   string
		p      gh.PR
		d      gh.PRDetail
		want   Kind
	}{
		{"draft wins over everything", gh.PR{IsDraft: true, StatusCheckRollup: []gh.Check{fail}},
			gh.PRDetail{MergeStateStatus: "DIRTY"}, KindDraft},
		{"conflict", pr(pass), gh.PRDetail{MergeStateStatus: "DIRTY"}, KindConflict},
		{"conflict via mergeable", pr(pass), gh.PRDetail{Mergeable: "CONFLICTING"}, KindConflict},
		{"failing checks", pr(pass, fail), gh.PRDetail{MergeStateStatus: "BLOCKED"}, KindChecksFailing},
		{"changes requested", pr(pass), gh.PR{ReviewDecision: "CHANGES_REQUESTED"}.detail(), KindChangesRequested},
		{"behind base", pr(pass), gh.PRDetail{MergeStateStatus: "BEHIND"}, KindBehind},
		{"awaiting review", gh.PR{ReviewDecision: "REVIEW_REQUIRED", StatusCheckRollup: []gh.Check{pass}},
			gh.PRDetail{MergeStateStatus: "BLOCKED", ReviewRequests: []gh.ReviewRequest{{Login: "x"}}}, KindAwaitingReview},
		{"pending", pr(gh.Check{State: "PENDING"}), gh.PRDetail{MergeStateStatus: "UNSTABLE"}, KindChecksRunning},
		{"ready", pr(pass), gh.PRDetail{MergeStateStatus: "CLEAN"}, KindReady},
		{"unknown", pr(pass), gh.PRDetail{MergeStateStatus: "UNKNOWN"}, KindPending},
	}
	for _, c := range cases {
		if got := Compute(c.p, c.d).Kind; got != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFailingChecksListed(t *testing.T) {
	card := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}, gh.Check{State: "SUCCESS", Name: "build"}),
		gh.PRDetail{MergeStateStatus: "BLOCKED"})
	if card.ActionKey != "r" {
		t.Errorf("failing-checks action = %q, want r", card.ActionKey)
	}
	if len(card.Lines) == 0 || card.Lines[0] != "lint" {
		t.Errorf("expected failing check 'lint' listed: %+v", card.Lines)
	}
}
```

Add this helper at the bottom of the test file (keeps the table readable):

```go
// detail attaches a review decision via a throwaway PR→detail combiner used only in tests.
func (p gh.PR) detail() gh.PRDetail { return gh.PRDetail{} } // overridden below
```

Wait — that won't compile (can't add methods to gh.PR from another package). Replace the `"changes requested"` case line with an inline construction instead:

```go
		{"changes requested",
			gh.PR{Number: 1, ReviewDecision: "CHANGES_REQUESTED", StatusCheckRollup: []gh.Check{pass}},
			gh.PRDetail{MergeStateStatus: "BLOCKED"}, KindChangesRequested},
```

and delete the bogus `detail()` helper. (The other cases already pass `gh.PR{...}` / `pr(...)` directly.)

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/triage/`
Expected: FAIL — undefined `Compute`, `Kind`, `Card`, the `Kind*` constants.

- [ ] **Step 3: Implement `internal/triage/triage.go`**

```go
package triage

import (
	"fmt"

	"github.com/noamsto/prdash/internal/gh"
)

type Kind int

const (
	KindFallback Kind = iota
	KindDraft
	KindConflict
	KindChecksFailing
	KindChangesRequested
	KindBehind
	KindBlocked
	KindAwaitingReview
	KindChecksRunning
	KindReady
	KindPending
)

// Card is the triage summary for one PR: the top blocker, its one-key fix, and
// which expanded tab to deep-link into.
type Card struct {
	Kind     Kind
	Headline string
	Lines    []string // e.g. the failing check names
	ActionKey   string // key the user presses to act ("" if none)
	ActionLabel string
	JumpTab     string // "" | "checks" | "reviews" | "conversation"
}

// Compute returns the highest-priority triage card for pr given its detail.
// Merge-state comes from detail (reliable per-PR); checks come from the PR rollup.
func Compute(pr gh.PR, d gh.PRDetail) Card {
	mss := d.MergeStateStatus
	failing := checksByState(pr, "fail")
	pending := checksByState(pr, "pending")

	switch {
	case pr.IsDraft || mss == "DRAFT":
		return Card{Kind: KindDraft, Headline: "Draft — not ready",
			ActionKey: "a", ActionLabel: "Mark ready"}
	case mss == "DIRTY" || d.Mergeable == "CONFLICTING":
		return Card{Kind: KindConflict, Headline: "Conflicts with base",
			ActionKey: "enter", ActionLabel: "worktree to resolve"}
	case len(failing) > 0:
		return Card{Kind: KindChecksFailing,
			Headline: fmt.Sprintf("%d checks failing", len(failing)), Lines: failing,
			ActionKey: "r", ActionLabel: "rerun failed", JumpTab: "checks"}
	case pr.ReviewDecision == "CHANGES_REQUESTED":
		return Card{Kind: KindChangesRequested, Headline: "Changes requested",
			ActionKey: "enter", ActionLabel: "worktree to address", JumpTab: "reviews"}
	case mss == "BEHIND":
		return Card{Kind: KindBehind, Headline: "Behind base",
			ActionKey: "u", ActionLabel: "update branch"}
	case mss == "BLOCKED" && pr.ReviewDecision != "REVIEW_REQUIRED":
		return Card{Kind: KindBlocked, Headline: "Blocked by branch protection",
			JumpTab: "conversation"}
	case pr.ReviewDecision == "REVIEW_REQUIRED":
		return Card{Kind: KindAwaitingReview, Headline: awaitingHeadline(d),
			JumpTab: "reviews"}
	case len(pending) > 0 || mss == "UNSTABLE":
		return Card{Kind: KindChecksRunning, Headline: "Checks running…",
			Lines: pending, JumpTab: "checks"}
	case mss == "CLEAN" || mss == "HAS_HOOKS":
		return Card{Kind: KindReady, Headline: "Ready to merge",
			ActionKey: "m", ActionLabel: "merge (squash)"}
	case mss == "UNKNOWN" || mss == "":
		return Card{Kind: KindPending, Headline: "Merge state pending…"}
	default:
		return Card{Kind: KindFallback, Headline: "", JumpTab: "conversation"}
	}
}

func checksByState(pr gh.PR, want string) []string {
	var out []string
	for _, c := range pr.StatusCheckRollup {
		if checkState(c) == want {
			out = append(out, c.Label())
		}
	}
	return out
}

// checkState collapses one rollup entry, mirroring gh.PR.CIState's vocabulary.
func checkState(c gh.Check) string {
	s := c.State
	if s == "" {
		s = c.Conclusion
	}
	switch s {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
		return "fail"
	case "PENDING", "QUEUED", "IN_PROGRESS", "":
		return "pending"
	default:
		return "pass"
	}
}

func awaitingHeadline(d gh.PRDetail) string {
	if len(d.ReviewRequests) > 0 {
		return "Waiting on @" + d.ReviewRequests[0].Login
	}
	return "Awaiting review"
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/triage/ -v`
Expected: PASS (TestLadderPriority all cases, TestFailingChecksListed).

- [ ] **Step 5: Commit**

```bash
git add internal/triage/
git commit -m "feat(triage): mergeStateStatus-driven priority ladder (pure, table-tested)"
```

---

## Task 3: Render the triage card in the side pane

**Files:**
- Create: `internal/ui/card.go`, `internal/ui/card_test.go`
- Modify: `internal/ui/preview.go`

- [ ] **Step 1: Write the failing test**

`internal/ui/card_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/triage"
)

func TestRenderCardShowsHeadlineAndAction(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Lines: []string{"lint", "e2e"}, ActionKey: "r", ActionLabel: "rerun failed"}
	out := renderCard(c, 40)
	if !strings.Contains(out, "2 checks failing") {
		t.Fatalf("headline missing: %q", out)
	}
	if !strings.Contains(out, "lint") || !strings.Contains(out, "e2e") {
		t.Fatalf("failing checks missing: %q", out)
	}
	if !strings.Contains(out, "r") || !strings.Contains(out, "rerun failed") {
		t.Fatalf("suggested action missing: %q", out)
	}
}

func TestRenderCardReadyNoAction(t *testing.T) {
	out := renderCard(triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}, 40)
	if !strings.Contains(out, "Ready to merge") {
		t.Fatalf("headline missing: %q", out)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/ui/ -run TestRenderCard`
Expected: FAIL — `renderCard` undefined.

- [ ] **Step 3: Implement `internal/ui/card.go`**

```go
package ui

import (
	"strings"

	"github.com/noamsto/prdash/internal/triage"
)

// cardGlyph picks a leading glyph + style for the card's kind.
func cardGlyph(k triage.Kind) string {
	switch k {
	case triage.KindReady:
		return passStyle.Render("✓")
	case triage.KindChecksFailing, triage.KindConflict, triage.KindChangesRequested:
		return failStyle.Render("✗")
	case triage.KindChecksRunning, triage.KindPending:
		return pendStyle.Render("●")
	default:
		return dimStyle.Render("•")
	}
}

// renderCard renders the triage card: glyph + headline, any detail lines, and
// the suggested action. Empty headline (fallback) renders nothing.
func renderCard(c triage.Card, width int) string {
	if c.Headline == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(cardGlyph(c.Kind) + " " + headerStyle.Render(c.Headline) + "\n")
	for _, l := range c.Lines {
		b.WriteString("  " + failStyle.Render(truncate(l, width-2)) + "\n")
	}
	if c.ActionKey != "" {
		b.WriteString(dimStyle.Render(c.ActionLabel+" → ") + accentStyle.Render(c.ActionKey) + "\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/ui/ -run TestRenderCard -v`
Expected: PASS.

- [ ] **Step 5: Lead the side pane with the card**

In `internal/ui/preview.go`, change `previewPane` to put the card on top and the timeline beneath (separated by a blank line). Add the `triage` import.

```go
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return "Loading preview…"
	}
	w := m.previewWidth()
	card := renderCard(triage.Compute(m.section.prAt(m.cursor), d), w)
	timeline := renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)
	if card == "" {
		return timeline
	}
	return card + "\n" + timeline
}
```

This needs the focused `gh.PR` (triage uses the PR's rollup + draft + reviewDecision, which live on the list item, not in `gh.PRDetail`). Add a small accessor to the PR section. In `internal/ui/section.go`, add to `PRSection`:

```go
// prAt returns the gh.PR at shown-row i (for triage, which needs list fields).
func (s *PRSection) prAt(i int) gh.PR { return s.prs[s.shown[i]] }
```

But `m.section` is the `Section` interface, which has no `prAt`. Rather than widen the interface (issues have no PR), type-assert in `previewPane`:

```go
	var card string
	if ps, ok := m.section.(*PRSection); ok {
		card = renderCard(triage.Compute(ps.prAt(m.cursor), d), w)
	}
	timeline := renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)
	if card == "" {
		return timeline
	}
	return card + "\n" + timeline
```

Use this type-assert version (not the `m.section.prAt` line above). Keep the `prAt` method on `*PRSection`.

- [ ] **Step 6: Run UI suite + build**

Run: `go test ./internal/ui/ -v && go build ./...`
Expected: PASS; all pre-existing UI tests still green.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/card.go internal/ui/card_test.go internal/ui/preview.go internal/ui/section.go
git commit -m "feat(ui): lead the side pane with the triage card"
```

---

## Task 4: `u` update-branch + Mark-ready actions

**Files:**
- Modify: `internal/action/defaults.go`, `internal/action/defaults_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/action/defaults_test.go`:

```go
func TestDefaultsHaveUpdateAndReady(t *testing.T) {
	d := DefaultPRActions()
	u := d["u"]
	if u.Command.Argv[0] != "gh" || u.Command.Argv[1] != "pr" || u.Command.Argv[2] != "update-branch" {
		t.Fatalf("u must be gh pr update-branch: %+v", u.Command.Argv)
	}
	if u.ExitsTUI {
		t.Fatal("update-branch is inline, not exits-tui")
	}
	ready := d["ready"]
	if ready.Label != "Mark ready" || ready.Command.Argv[2] != "ready" {
		t.Fatalf("ready action wrong: %+v", ready)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/action/ -run TestDefaultsHaveUpdateAndReady`
Expected: FAIL — keys absent.

- [ ] **Step 3: Add the actions to `DefaultPRActions` in `internal/action/defaults.go`**

Add these two entries to the returned map:

```go
		"u": {Key: "u", Label: "Update branch",
			Command: Command{Argv: []string{"gh", "pr", "update-branch", "{{.Number}}"}}, Scope: "single"},
		"ready": {Key: "ready", Label: "Mark ready",
			Command: Command{Argv: []string{"gh", "pr", "ready", "{{.Number}}"}}, Scope: "single"},
```

(`u` is bound as a top-level key by the existing `Update` dispatch — pressing `u` looks it up in `m.actions`. `ready` has no single-char top-level key, so it's reachable only through the `a` action overlay, matching the spec's "overlay-only Mark-ready".)

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/action/ -v`
Expected: PASS (new test + existing `TestDefaultsHaveEnterAndExits`, `TestDefaultsHaveBulkW`).

- [ ] **Step 5: Build + manual confirm the card's action keys resolve**

Run: `go build ./... && go vet ./... && gofmt -l .`
The triage card suggests `r`/`u`/`m`/`enter`/`a` — all now exist in `DefaultPRActions` (or are built-in keys). No card suggests a key that isn't bound.

- [ ] **Step 6: Commit**

```bash
git add internal/action/
git commit -m "feat(action): update-branch (u) + mark-ready (overlay)"
```

---

## Task 5: Live smoke test

**Files:** none (manual verification)

- [ ] **Step 1: Build + run against PRs in varied states**

Build (`go build -o /tmp/prdash-b .`) and run in a wide terminal against a repo with PRs that have failing CI, pending CI, and clean states. Verify the side card leads with the right blocker per PR (failing → lists checks + `r`; clean → "Ready to merge" + `m`; draft → "Mark ready"), the timeline still shows beneath, and `UNKNOWN`/freshly-pushed PRs show "Merge state pending…" not a wrong blocker.

- [ ] **Step 2: Verify `u` works**

On a behind-base PR, press `u`; confirm `gh pr update-branch` runs (or surfaces its error) without leaving the TUI.

- [ ] **Step 3: Commit any polish from observations**

```bash
git add -A
git commit -m "polish(ui): triage card tuning from live test"
```

---

## Self-review (done)

- **Spec coverage (Phase B slice):** `mergeStateStatus`-driven ladder ✓ (T2), detail-fetch as the merge-state source ✓ (T1, §2 of spec), per-check names via the CheckRun/StatusContext union ✓ (T1 `Check.Label`), card leads the side pane ✓ (T3), suggested actions reference only bound keys ✓ (T4), `u`/Mark-ready added ✓ (T4), `UNKNOWN` → neutral "pending" ✓ (T2). Deferred to Phase C: the expanded tabbed view + deep-link from the card's `JumpTab` (the field is populated now, consumed in C); the diffstat is parsed (T1 `Diffstat()`) and surfaces in C's Diff tab.
- **Placeholders:** none — every code step shows code; T5 is explicit manual verification.
- **Type consistency:** `gh.Check.Label()`, `gh.PR.IsDraft`, `gh.PRDetail.{MergeStateStatus,Mergeable,ReviewRequests,Files,Diffstat()}`, `triage.{Kind,Card,Compute}`, `ui.renderCard`, `PRSection.prAt`, action keys `u`/`ready` consistent across tasks. The card's `JumpTab` is set in T2 and intentionally unused until Phase C.

## Next

Phase C (expanded tabbed view) consumes `Card.JumpTab` (deep-link), `PRDetail.LatestReviews` (Reviews tab), `PRDetail.Diffstat()`/`Files` (Diff tab), and `Check.Label()` (Checks tab) — all established here.
