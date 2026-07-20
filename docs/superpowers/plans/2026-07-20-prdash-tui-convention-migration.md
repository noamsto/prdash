# prdash TUI Convention Migration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring prdash into conformance with the TUI Interaction Convention as a *board* archetype — always-visible filter bar, spine-consistent nav/help/quit keys — without touching its single-key action vocabulary.

**Architecture:** prdash is a single Bubble Tea v2 `Model` in `internal/ui/prlist.go`. Key handling is a modal `switch msg.String()` in `Update`; rendering is `render()` → `board()`. This plan makes surgical, behavior-level changes to those two paths plus the help overlay; it does **not** rewrite the raw `switch` onto `key.Binding` (that migration is deferred, per spec). The convention doc `KEYMAP.md` is authored first as the shared reference.

**Tech Stack:** Go 1.26, `charm.land/bubbletea/v2`, `charm.land/bubbles/v2` (`textinput`, `viewport`), `charm.land/lipgloss/v2`. Tests are table/`render()`-string assertions driven via `keyMsg(...)` and `newTestModelWithRows(t)` in `internal/ui/*_test.go`.

## Global Constraints

- Bubble Tea **v2** API (`tea.KeyPressMsg`, `msg.String()` matching). No v1 `KeyMsg`.
- Do **not** change prdash's action keys (`m A r y Y b o W u M s f D R V z p space tab` and the data-driven `m.actions` table). Board archetype keeps them single-key.
- prdash stays a **board**: `/` focuses the filter (confirmed 2026-07-20); typing does not filter unless the filter is focused.
- Height math is sensitive (recent commits fixed refresh-bleed / layout sweep). Any change that adds a rendered row MUST re-run `go test ./internal/ui/ -run 'Layout|Sweep|Overflow'` and reconcile expectations.
- Spec of record: `docs/superpowers/specs/2026-07-20-tui-interaction-convention-design.md`.

## Decomposition note

This is **one of several per-repo plans**. It covers `KEYMAP.md` (unit 0) + the prdash migration only. wtc, lazytmux, tmux-remux, and aeye are separate modules/repos and get their own plans authored at execution time. Nothing here spans repos.

## File Structure

- `KEYMAP.md` (new, repo root) — canonical convention doc; other repos link to it. (Recommended canonical home is lazytmux; placed in prdash for this first pass — a later `git mv` can relocate it. Confirm before Task 1.)
- `internal/ui/prlist.go` (modify) — `Update` base-board key switch (~1385–1471), `render()` (1482–1512), `board()` (1518–1534), new `filterBar()` method; `NewModel` init if needed.
- `internal/ui/prlist_test.go` (modify) — extend `keyMsg` with `ctrl+j`, `ctrl+k`, `alt+j`, `alt+k`, `f1`; add behavior tests.
- `internal/ui/filter_test.go` or `prlist_test.go` (modify) — always-visible-bar and two-stage-esc tests.

---

### Task 1: Author the KEYMAP.md convention doc

**Files:**
- Create: `KEYMAP.md`

This is a documentation deliverable distilled from the approved spec — no test cycle.

- [ ] **Step 1: Write `KEYMAP.md`** containing, verbatim in spirit from the spec: the generating rule; the three archetype tables (picker / board / viewer); the shared-spine table with the `q`/`?` and viewer exemptions; the "filter presentation (always visible)" table; and the per-app archetype assignment (prdash & wtc = board, lazytmux & tmux-remux = picker, aeye = viewer). Keep it end-user facing (what keys do), not implementation detail. Link back to the spec for rationale.

- [ ] **Step 2: Commit**

```bash
git add KEYMAP.md
git commit -m "docs(tui): add KEYMAP.md convention (canonical reference)"
```

---

### Task 2: Move preview-scroll to `alt+j/k`; add `ctrl+j/k` as selection nav

**Files:**
- Modify: `internal/ui/prlist.go` — base-board key switch (cases at 1392–1397 for `ctrl+j`/`ctrl+k`; down/up cases at 1445–1452)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.previewScrollBy(delta int)` (prlist.go:285), `m.moveCursor(delta int)` (prlist.go:185), `m.debounceDetailCmd()` (prlist.go:973).
- Produces: no new symbols; behavioral change only.

- [ ] **Step 1: Extend `keyMsg` test helper** in `internal/ui/prlist_test.go` (in the `switch s` at :986) with the new keys:

```go
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "alt+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModAlt}
	case "alt+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModAlt}
	case "f1":
		return tea.KeyPressMsg{Code: tea.KeyF1}
```

- [ ] **Step 2: Write the failing test**

```go
func TestCtrlJKMovesSelectionAltJKScrollsPreview(t *testing.T) {
	m := newTestModelWithRows(t)
	start := m.cursor
	u, _ := m.Update(keyMsg("ctrl+j"))
	m = u.(Model)
	if m.cursor != start+1 {
		t.Fatalf("ctrl+j should move selection down: cursor=%d want=%d", m.cursor, start+1)
	}
	u, _ = m.Update(keyMsg("ctrl+k"))
	m = u.(Model)
	if m.cursor != start {
		t.Fatalf("ctrl+k should move selection up: cursor=%d want=%d", m.cursor, start)
	}
	// alt+j/alt+k drive the preview offset, not the cursor.
	before := m.cursor
	u, _ = m.Update(keyMsg("alt+j"))
	m = u.(Model)
	if m.cursor != before {
		t.Fatalf("alt+j must not move the cursor: cursor=%d want=%d", m.cursor, before)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestCtrlJKMovesSelectionAltJKScrollsPreview -v`
Expected: FAIL (currently `ctrl+j` scrolls preview, leaving cursor unchanged).

- [ ] **Step 4: Implement** — in the base-board switch of `Update`, replace the `ctrl+j`/`ctrl+k` cases (1392–1397) with `alt+j`/`alt+k` for preview scroll, and fold `ctrl+j`/`ctrl+k` into the existing selection-move cases (1445–1452):

```go
		case "alt+j":
			m.previewScrollBy(1)
			return m, nil
		case "alt+k":
			m.previewScrollBy(-1)
			return m, nil
```

and change the movement cases to:

```go
		case "down", "j", "ctrl+j":
			m.moveCursor(1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
		case "up", "k", "ctrl+k":
			m.moveCursor(-1)
			m.detailSeq++
			return m, m.debounceDetailCmd()
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestCtrlJKMovesSelectionAltJKScrollsPreview -v`
Expected: PASS

- [ ] **Step 6: Guard the palette's `ctrl+j/k`** — confirm the actions-palette handler still moves its own selection (cases at 1343/1348 `up/ctrl+k`, `down/ctrl+j`), unaffected by this change.

Run: `go test ./internal/ui/ -run 'Action|Palette' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): ctrl+j/k move selection, alt+j/k scroll preview (spine nav)"
```

---

### Task 3: Add `F1` as the universal help key (keep `?` alias)

**Files:**
- Modify: `internal/ui/prlist.go` — base-board switch `?` case (1424–1426)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.showLegend bool` (prlist.go:59).
- Produces: no new symbols.

- [ ] **Step 1: Write the failing test**

```go
func TestF1OpensLegendLikeQuestionMark(t *testing.T) {
	m := newTestModelWithRows(t)
	u, _ := m.Update(keyMsg("f1"))
	if !u.(Model).showLegend {
		t.Fatal("f1 should open the legend overlay")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestF1OpensLegendLikeQuestionMark -v`
Expected: FAIL (`f1` falls through to the `m.actions` default and does nothing).

- [ ] **Step 3: Implement** — change the `?` case (1424) to also match `f1`:

```go
		case "?", "f1":
			m.showLegend = true
			return m, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestF1OpensLegendLikeQuestionMark -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): F1 opens the keymap overlay (spine help key)"
```

---

### Task 4: Always-visible filter bar (blurred until `/`)

**Files:**
- Modify: `internal/ui/prlist.go` — new `filterBar()` method; `render()` (delete the `if m.filtering` branch, 1489–1497); `board()` (insert bar after header in each return, 1522–1533)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.filterInput textinput.Model` (:53), `m.filtering bool`, `m.omniSuggestDropdown() string` (:411), `m.header() string`, `m.renderMain() string`.
- Produces: `func (m Model) filterBar() string` — the always-rendered filter row (placeholder when blurred, live input + dropdown when focused).

- [ ] **Step 1: Write the failing test**

```go
func TestFilterBarAlwaysVisible(t *testing.T) {
	m := newTestModelWithRows(t)
	// Blurred board: the filter prompt is visible even without pressing '/'.
	if !strings.Contains(m.render(), "/") {
		t.Fatalf("filter bar should be visible on the blurred board: %q", m.render())
	}
	// Focusing keeps it visible and accepts input.
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("x"))
	m = u.(Model)
	if m.filterInput.Value() != "x" {
		t.Fatalf("focused filter should capture typing: %q", m.filterInput.Value())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestFilterBarAlwaysVisible -v`
Expected: FAIL (blurred board renders no filter row today).

- [ ] **Step 3: Add the `filterBar()` method** near `render()` in `prlist.go`:

```go
// filterBar is the always-visible search row. When blurred it shows the prompt
// as a hint; when focused it shows the live query plus any @-suggestion dropdown.
func (m Model) filterBar() string {
	if m.filtering {
		bar := m.filterInput.View()
		if dd := m.omniSuggestDropdown(); dd != "" {
			return bar + "\n" + dd
		}
		if m.mode == "pr" {
			return bar + "\n" + dimStyle.Render(truncate("@user · is: · text", max(1, m.width)))
		}
		return bar
	}
	// Blurred: show the prompt + placeholder as a dim hint so the bar is always present.
	return dimStyle.Render(truncate("/ filter (@user, is:, text)", max(1, m.width)))
}
```

- [ ] **Step 4: Delete the `if m.filtering` branch in `render()`** (1489–1497) so the board path always runs:

```go
	if m.expanded {
		return m.expandedView()
	}
	// (removed: the special m.filtering top-branch — the bar now lives in board())
	board := m.board()
```

- [ ] **Step 5: Insert the bar into `board()`** — after the header in each return (1524, 1527, 1533). Example for the default return (1533):

```go
	return m.header() + "\n" + m.filterBar() + "\n" + m.renderMain() + "\n" + foot
```

Apply the same `+ "\n" + m.filterBar()` after `m.header() + "\n"` in the `previewMax` (1524) and docked (1527) returns.

- [ ] **Step 6: Run the focused test to verify it passes**

Run: `go test ./internal/ui/ -run TestFilterBarAlwaysVisible -v`
Expected: PASS

- [ ] **Step 7: Reconcile height-sensitive tests** — the bar adds one (or two, focused) rows. Run the layout regression suite and adjust `renderMain`/viewport height expectations where they subtract chrome rows:

Run: `go test ./internal/ui/ -run 'Layout|Sweep|Overflow|View' -v`
Expected: PASS (fix any height off-by-one by accounting for the new bar row in `computeLayout`/`contentHeight` and the affected test expectations). Then run the whole package: `go test ./internal/ui/`.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): always-visible filter bar (blurred until /)"
```

---

### Task 5: Two-stage `esc` on the board (filter → back → quit)

**Files:**
- Modify: `internal/ui/prlist.go` — filter-focused `esc` (PR path 1236–1239, issue path 1217–1220) and base-board switch (add `esc` case near 1427)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.filtering`, `m.filterInput` (`Value()`, `SetValue`, `Blur()`), `m.applyFilter()` (:301).
- Produces: no new symbols. Behavior contract:
  - focused → `esc` blurs, **keeps** the applied query (so single-key actions work on filtered results).
  - blurred + non-empty query → `esc` clears the query.
  - blurred + empty query → `esc` quits.

- [ ] **Step 1: Write the failing test**

```go
func TestEscTwoStageOnBoard(t *testing.T) {
	m := newTestModelWithRows(t)
	// focus + type
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("f"))
	m = u.(Model)
	// esc #1: blur but KEEP the query
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filtering {
		t.Fatal("esc should blur the focused filter")
	}
	if m.filterInput.Value() != "f" {
		t.Fatalf("esc-blur must keep the query, got %q", m.filterInput.Value())
	}
	// esc #2: clear the query (still no quit)
	u, cmd := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filterInput.Value() != "" {
		t.Fatalf("second esc should clear the query, got %q", m.filterInput.Value())
	}
	if cmd != nil {
		t.Fatal("clearing the query must not quit")
	}
	// esc #3: empty query → quit
	_, cmd = m.Update(keyMsg("esc"))
	if cmd == nil {
		t.Fatal("esc on an empty board should quit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestEscTwoStageOnBoard -v`
Expected: FAIL (base board has no `esc`; focused `esc` currently clears the query instead of keeping it).

- [ ] **Step 3: Change focused `esc` to blur-keep** — in the PR filter handler (1236–1239) and the issue filter handler (1217–1220), replace the value-clearing `esc` with:

```go
			case "esc":
				m.filtering = false
				m.filterInput.Blur() // keep the query applied so actions work on the filtered set
				return m, nil
```

- [ ] **Step 4: Add `esc` to the base-board switch** (near the `q`/`ctrl+c` case at 1427):

```go
		case "esc":
			if m.filterInput.Value() != "" {
				m.filterInput.SetValue("")
				m.applyFilter()
				return m, nil
			}
			return m, tea.Quit
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestEscTwoStageOnBoard -v`
Expected: PASS

- [ ] **Step 6: Regression** — the omni server-commit path (`enter`, 1256+) is unchanged; confirm:

Run: `go test ./internal/ui/ -run 'Omni|Filter' -v`
Expected: PASS. Then full package: `go test ./internal/ui/`.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): two-stage esc — blur (keep query) → clear → quit"
```

---

### Task 6: Searchable keymap overlay (`?`/`F1` legend filters as you type)

**Files:**
- Modify: `internal/ui/prlist.go` — legend key handling (the `showLegend` dismissal at 1359–1360) and `legendView()` (1709+)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.showLegend bool`; `legendView()` rendering.
- Produces: `m.legendQuery string` (new Model field) — the live filter over legend rows.

> This is the board's discoverability upgrade (the lazygit "searchable `?`" idea). It is independent of Tasks 2–5 and may ship separately.

- [ ] **Step 1: Add the field** `legendQuery string` to the `Model` struct (near `showLegend`, :59).

- [ ] **Step 2: Write the failing test**

```go
func TestLegendFiltersByTyping(t *testing.T) {
	m := newTestModelWithRows(t)
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("m")) // type into the legend filter
	m = u.(Model)
	if m.legendQuery != "m" {
		t.Fatalf("typing in the legend should build legendQuery, got %q", m.legendQuery)
	}
	out := m.legendView()
	if !strings.Contains(strings.ToLower(out), "merge") {
		t.Fatalf("legend filtered by 'm' should still show merge: %q", out)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestLegendFiltersByTyping -v`
Expected: FAIL (any key currently dismisses the legend at 1360; no `legendQuery`).

- [ ] **Step 4: Implement** — replace the "any key dismisses" block (1359–1360) with a handler that: `esc`/`?`/`f1` closes and resets `legendQuery`; `backspace` trims it; printable runes append to `legendQuery`; then filter the rows rendered by `legendView()` by case-insensitive substring over each row's label+description. Keep the existing grouped layout; drop rows that don't match when `legendQuery != ""`.

```go
		if m.showLegend {
			switch msg.String() {
			case "esc", "?", "f1":
				m.showLegend = false
				m.legendQuery = ""
			case "backspace":
				if r := []rune(m.legendQuery); len(r) > 0 {
					m.legendQuery = string(r[:len(r)-1])
				}
			default:
				if s := msg.String(); len(s) == 1 {
					m.legendQuery += s
				}
			}
			return m, nil
		}
```

In `legendView()`, when `m.legendQuery != ""`, filter each row by `strings.Contains(strings.ToLower(label+" "+desc), strings.ToLower(m.legendQuery))` before rendering, and show the query in the overlay title.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestLegendFiltersByTyping -v`
Expected: PASS

- [ ] **Step 6: Full package + build**

Run: `go test ./internal/ui/` then `go build ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): searchable keymap overlay (type to filter the legend)"
```

---

### Task 7: Update the in-app legend/hints copy to the new keys

**Files:**
- Modify: `internal/ui/prlist.go` — `legendView()` filters/rows (1729, 1745), `navHintsFor`/`actionOrder` hint rows (1767), `keysActionsPanel` hints (1944)
- Test: `internal/ui/prlist_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHintsMentionSpineKeys(t *testing.T) {
	m := newTestModelWithRows(t)
	u, _ := m.Update(keyMsg("?"))
	out := u.(Model).legendView()
	for _, want := range []string{"alt+j", "F1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("legend should document %q: %q", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestHintsMentionSpineKeys -v`
Expected: FAIL (legend still lists `ctrl+j/k` for scroll and only `?` for help).

- [ ] **Step 3: Implement** — in `legendView()` and the hint rows, update: preview scroll `ctrl+j/k` → `alt+j/k`; add `alt+h/l` if horizontal scroll is shown; help row shows `? / F1`. Leave action-key rows untouched.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestHintsMentionSpineKeys -v`
Expected: PASS

- [ ] **Step 5: Full package**

Run: `go test ./internal/ui/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "docs(ui): legend/hints reflect spine keys (alt scroll, F1 help)"
```

---

## Self-review notes

- **Spec coverage (prdash row):** always-visible bar (T4) ✓; live incremental fuzzy — already present via omni-filter, bar just stays visible ✓; two-stage esc (T5) ✓; preview-scroll `ctrl+j/k`→`alt+j/k`, palette `ctrl+j/k` stays (T2) ✓; searchable help + `?` alias + `F1` (T3, T6) ✓; `key.Binding` migration — **deferred by spec ("over time"), intentionally out of scope**; KEYMAP.md (T1) ✓.
- **Out of scope (deferred, tracked in spec):** full `key.Binding`/`help.Model` rewrite of `prlist.go`; the `tuikit` shared package (extract at second use); wtc/lazytmux/tmux-remux/aeye migrations (own plans).
- **Risk flag:** Task 4 changes rendered height; Step 7 of T4 exists specifically to reconcile the layout-sweep/overflow suites. Do not skip it.
