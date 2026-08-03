# Optimistic Auto-Merge + Approve Glyphs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On successful `A` (auto-merge) or `L` (approve), paint the row glyph immediately — do not wait for a list refetch.

**Architecture:** Record `Command.Native` on `actionStat`. On successful `actionDoneMsg`, patch the in-memory `gh.PR` (and detail, for approve) for each `actionStatus.nums` entry, bump `rowGen`, repaint. Enable `Refresh: true` on default `A` so the server still reconciles. Failure (`err != nil`) patches nothing — including partial bulk failures, matching today's all-or-nothing refresh gate.

**Tech Stack:** Go, Bubble Tea `Update`, plain `go test`.

**Spec:** `docs/superpowers/specs/2026-08-03-board-prefetch-select-optimistic-design.md` (Feature 3)

## Global Constraints

- Optimistic paint **only on success** — never on keypress.
- No sticky map; live list refresh overwrites the optimistic fields.
- Do not invent per-PR success lists; patch all `nums` iff `err == nil`.
- Do not add disable-auto-merge.
- Run: `go test ./internal/ui/ -run 'Optimistic|AutoMerge.*Action|Approve.*Action|DefaultPRActions' -count=1` and `go test ./internal/action/ -count=1`

---

### Task 1: `Refresh: true` on default Auto-merge

**Files:**
- Modify: `internal/action/defaults.go` (`"A"` entry)
- Test: `internal/action/defaults_test.go` (extend)

**Interfaces:**
- Consumes: `action.Action.Refresh`
- Produces: `DefaultPRActions()["A"].Refresh == true`

- [ ] **Step 1: Write the failing test**

Append to `internal/action/defaults_test.go`:

```go
func TestAutoMergeActionRefreshes(t *testing.T) {
	a := DefaultPRActions()["A"]
	if !a.Refresh {
		t.Fatal(`default "A" (auto-merge) must Refresh so the board reconciles after arming`)
	}
}

func TestApproveActionStillRefreshes(t *testing.T) {
	a := DefaultPRActions()["L"]
	if !a.Refresh {
		t.Fatal(`default "L" (approve) must keep Refresh`)
	}
}
```

- [ ] **Step 2: Run — expect fail**

Run: `go test ./internal/action/ -run 'TestAutoMergeActionRefreshes|TestApproveActionStillRefreshes' -count=1`

Expected: `TestAutoMergeActionRefreshes` FAIL.

- [ ] **Step 3: Set Refresh on A**

In `internal/action/defaults.go`, change the `"A"` entry to:

```go
		"A": {Key: "A", Label: "Auto-merge (squash)",
			Command: Command{Native: "auto-merge-squash"},
			Scope:   "per-selected", ConfirmOthers: true, Refresh: true,
			Progress: "Enabling auto-merge", Past: "Auto-merge on", Fail: "Auto-merge failed"},
```

- [ ] **Step 4: Run — expect pass**

Run: `go test ./internal/action/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/action/defaults.go internal/action/defaults_test.go
git commit -m "$(cat <<'EOF'
fix(action): refresh the board after enabling auto-merge

EOF
)"
```

---

### Task 2: Patch PRs on successful `actionDoneMsg`

**Files:**
- Modify: `internal/ui/actions.go` (`actionStat`, `statFor`)
- Modify: `internal/ui/section.go` (add `updatePR`)
- Modify: `internal/ui/prlist.go` (`actionDoneMsg` handler — call optimistic apply)
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: `actionStat.nums`, `actionStat` native marker, `m.detail`, `m.viewerLogin`
- Produces:
  - `actionStat.native string` set from `a.Command.Native` in `statFor`
  - `func (s *PRSection) updatePR(number int, fn func(*gh.PR)) bool`
  - `func (m *Model) applyOptimisticAction()` — patches in-memory state when native is auto-merge or approve

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/actions_test.go`:

```go
func TestOptimisticAutoMergePaintsGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN", Title: "x", Author: author("alice"),
	}})
	m.width, m.height = 120, 40
	m.actionStatus = statFor(action.DefaultPRActions()["A"])
	m.actionStatus.nums = []int{13}
	m.actionStatus.refresh = true

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	ps := m.section.(*PRSection)
	p := ps.prAt(0)
	if !p.AutoMergeEnabled() {
		t.Fatal("successful auto-merge must arm AutoMergeRequest on the in-memory PR")
	}
	m.renderList()
	if !strings.Contains(m.rowText[0], autoMergeGlyph(true)) {
		t.Fatalf("row should show auto-merge glyph immediately:\n%s", m.rowText[0])
	}
}

func TestFailedAutoMergeDoesNotPaintGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{Number: 13, ID: "pr13node", State: "OPEN"}})
	m.actionStatus = statFor(action.DefaultPRActions()["A"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{err: errors.New("boom")})
	m = u.(Model)
	if m.section.(*PRSection).prAt(0).AutoMergeEnabled() {
		t.Fatal("failed auto-merge must leave AutoMergeRequest unset")
	}
}

func TestOptimisticApprovePaintsReviewGlyph(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN",
		ReviewDecision: "REVIEW_REQUIRED", Title: "x",
	}})
	m.width, m.height = 120, 40
	m.viewerLogin = "me"
	m.detail[13] = gh.PRDetail{} // empty reviews → upsert path
	m.actionStatus = statFor(action.DefaultPRActions()["L"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	if got := m.section.(*PRSection).prAt(0).ReviewDecision; got != "APPROVED" {
		t.Fatalf("ReviewDecision = %q, want APPROVED", got)
	}
	d := m.detail[13]
	found := false
	for _, r := range d.LatestReviews {
		if r.Author.Login == "me" && r.State == "APPROVED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("detail LatestReviews should carry viewer APPROVED, got %+v", d.LatestReviews)
	}
	m.renderList()
	if !strings.Contains(m.rowText[0], reviewApprovedGlyph) {
		t.Fatalf("row should show approved glyph immediately:\n%s", m.rowText[0])
	}
}

func TestFailedApproveDoesNotPaint(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{{
		Number: 13, ID: "pr13node", State: "OPEN", ReviewDecision: "REVIEW_REQUIRED",
	}})
	m.actionStatus = statFor(action.DefaultPRActions()["L"])
	m.actionStatus.nums = []int{13}

	u, _ := m.Update(actionDoneMsg{err: errors.New("boom")})
	m = u.(Model)
	if got := m.section.(*PRSection).prAt(0).ReviewDecision; got != "REVIEW_REQUIRED" {
		t.Fatalf("failed approve must leave ReviewDecision, got %q", got)
	}
}

func TestOptimisticAutoMergeBulkPatchesAllNums(t *testing.T) {
	m, _ := mutationModel(t, []gh.PR{
		{Number: 1, ID: "n1", State: "OPEN"},
		{Number: 2, ID: "n2", State: "OPEN"},
	})
	m.actionStatus = statFor(action.DefaultPRActions()["A"])
	m.actionStatus.nums = []int{1, 2}

	u, _ := m.Update(actionDoneMsg{})
	m = u.(Model)
	ps := m.section.(*PRSection)
	for i := 0; i < ps.Len(); i++ {
		if !ps.prAt(i).AutoMergeEnabled() {
			t.Fatalf("PR #%d should be armed after bulk success", ps.prAt(i).Number)
		}
	}
}
```

Implementer notes:
- Use whatever `Author` / `author()` pattern `mutationModel` and neighboring tests already use — do not invent a new author shape if one exists in the file.
- Import `strings` if the test file does not already.
- `reviewApprovedGlyph` is package-private in `ui` — tests in `package ui` can reference it.

- [ ] **Step 2: Run — expect fail**

Run: `go test ./internal/ui/ -run 'TestOptimistic|TestFailedAutoMerge|TestFailedApprove' -count=1`

Expected: FAIL (no optimistic patch yet). `TestFailed*` may pass already.

- [ ] **Step 3: Implement**

**`actionStat` + `statFor`** in `internal/ui/actions.go`:

```go
type actionStat struct {
	run     string
	ok      string
	fail    string
	settled bool
	err     error
	refresh bool
	rerunCI bool
	nums    []int
	merged  []gh.PR
	native  string // a.Command.Native — drives optimistic row patches on success
}

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
	return &actionStat{run: run, ok: ok, fail: fail, rerunCI: rerunsCI(a), native: a.Command.Native}
}
```

Hand-built `actionStat{...}` sites (expanded rerun, etc.) leave `native` empty — fine.

**`updatePR`** on `PRSection` in `internal/ui/section.go` (near `ApplyChecks`):

```go
// updatePR applies fn to the stored PR with the given number. ok is false when
// the board does not hold it.
func (s *PRSection) updatePR(number int, fn func(*gh.PR)) bool {
	for i := range s.prs {
		if s.prs[i].Number == number {
			fn(&s.prs[i])
			return true
		}
	}
	return false
}
```

**`applyOptimisticAction`** on `Model` in `internal/ui/prlist.go` (or `actions.go`):

```go
// applyOptimisticAction paints mutation results onto the in-memory board as
// soon as the request succeeds, so glyphs do not wait on backgroundRefresh.
func (m *Model) applyOptimisticAction() {
	if m.actionStatus == nil {
		return
	}
	ps, ok := m.section.(*PRSection)
	if !ok {
		return
	}
	switch m.actionStatus.native {
	case "auto-merge-squash":
		for _, n := range m.actionStatus.nums {
			ps.updatePR(n, func(p *gh.PR) {
				p.AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
			})
		}
	case "approve":
		for _, n := range m.actionStatus.nums {
			ps.updatePR(n, func(p *gh.PR) {
				p.ReviewDecision = "APPROVED"
			})
			if m.viewerLogin == "" {
				continue
			}
			d := m.detail[n]
			replaced := false
			for i := range d.LatestReviews {
				if d.LatestReviews[i].Author.Login == m.viewerLogin {
					d.LatestReviews[i].State = "APPROVED"
					replaced = true
					break
				}
			}
			if !replaced {
				var r gh.Review
				r.Author.Login = m.viewerLogin
				r.State = "APPROVED"
				d.LatestReviews = append(d.LatestReviews, r)
			}
			m.detail[n] = d
		}
	default:
		return
	}
	m.rowGen++
	m.renderList()
}
```

**`actionDoneMsg` handler** — call it when `msg.err == nil`, before the refresh branch:

```go
		if msg.err == nil {
			landed := time.Now()
			for _, p := range m.actionStatus.merged {
				p.State, p.MergedAt = "MERGED", landed
				m.mergedSticky[p.Number] = p
				delete(m.ciRerun, p.Number)
			}
			m.applyOptimisticAction()
		}
```

Keep the existing `refresh` / `rerunCI` branches as they are.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestOptimistic|TestFailedAutoMerge|TestFailedApprove|TestUpdateBranchStamps|TestMergeDoesNot' -count=1`

Expected: PASS. Also: `go test ./internal/ui/ -count=1` and `go test ./internal/action/ -count=1`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/actions.go internal/ui/section.go internal/ui/prlist.go internal/ui/actions_test.go
git commit -m "$(cat <<'EOF'
feat(ui): paint auto-merge and approve glyphs on mutation success

Patch the in-memory PR (and detail for approve) as soon as actionDoneMsg
succeeds so the row glyph does not wait on backgroundRefresh.

EOF
)"
```
