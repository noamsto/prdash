# Board + preview cleanup: state on the row, detail in the preview

Issue: #88 · Closes #62

## Problem

The two-line row has become the place everything goes. Line 1 carries the glyph
gutter, number and title; line 2 carries the author, label chips and the head
branch. That was a reasonable way to fit more in, but it costs half the board's
vertical density to show three things the preview pane is already better at, and
it leaves the row without the one number a reviewer actually wants before
opening anything: how big is this.

Meanwhile the ordering reshuffles. `prRank` sorts by actionability, so a CI run
finishing moves rows under the cursor — the complaint in #62.

And the draft marker contradicts the row's own grammar: every other indicator is
a left-gutter glyph, but draft is a `[draft]` tag trailing the title.

## Behavior

### What moves

| Element | Today | After |
|---|---|---|
| Labels | Row line 2, chips | Preview identity block, chip row |
| Branch | Row line 2, dim | Preview identity line, as `main ← head` |
| Author | Row line 2 | Row column, tinted, clustered |
| Draft | `[draft]` trailing the title | Gutter cell 1, overriding CI |
| Diffstat | Absent | New column |
| Ticket id | Absent | New column, after the title |

Line 2 has no content left, so **`TwoLine` is deleted** — the field comes out of
`RowOpts`, `Layout` and `rowKey`, and `renderItemRow` loses its entire
second-line branch (~45 lines).

### Row layout

```
rail │ ci rv am fl │ tree │ #num │ title… │ ticket │ author │ diffstat │ age
  1      2  2  2  2     3      6     1fr       10       17         12     4
```

61 fixed cells before the title gets any.

- **Cell 1 (CI)** is overridden by terminal and draft state, extending the
  precedent `mergedMark()` / `closedMark()` already set. A draft shows a dim
  draft-PR glyph. The row still dims and `rankDraft` demotion is unchanged.
  Cost: a draft's CI rollup stops showing on the row. Accepted — drafts are
  demoted anyway, and the preview still has it.
- **Diffstat** renders `+412 -18`, colour on the numbers only, abbreviated at
  1000 (`+1.6k`). Its width is computed across the shown set like
  `columnWidths`, so it does not jitter row to row.
- **The tree column sits after the glyph gutter, not before it.** Placing it
  first shifts the state glyphs on any row that uses it, which makes the board
  read as several competing left edges. After the gutter, every row's glyphs
  hold the same four columns.

  Only #89 populates this column; #88 **reserves** it, spending 3 cells that
  render blank until stacks land. The alternative — adding the column in #89 —
  would shift every responsive breakpoint below, so reserving buys breakpoint
  stability with 3 cells. If #89 is abandoned, reclaiming them is a one-line
  change to the column list.

### Row separation

**Nothing** — no hairline, no zebra striping. Single-line rows make a per-row
hairline cost roughly 50% of the board's vertical capacity (~18 visible PRs
becomes ~9), which is the opposite of what this change is for. gh-dash can
afford one because its rows are already two lines.

The category rules and the author clusters chunk the list, and the cursor row
keeps its `RowBg` fill plus the focus rail. Zebra striping was evaluated and
rejected as unnecessary at one line per row; note for any future attempt that it
would need row parity folded into `rowKey`, or the row cache serves the wrong
tint after a scroll.

### Ordering (closes #62)

Within each category: cluster by author, clusters led by their highest PR
number, rows inside a cluster by number descending, drafts last.

`groupByCategory` returns early at `section.go:243`, so `groupByAuthor` never
runs on a categorized board. Making it run per-category composes the two
functions that already exist rather than adding a third ordering concept.

`groupByAuthor`'s existing "best member rank" cluster ordering is replaced by
highest-PR-number. **This is a deliberate trade**: urgency stops being visible
in position and reads only from the gutter glyphs. In exchange nothing moves
when CI lands, cluster position becomes muscle memory, and the cursor survives a
refresh. The categories still encode coarse urgency, which is what makes the
trade acceptable.

### Ticket id

Parsed from `HeadRefName`, already in the list payload — no new fields, no API
cost. Two patterns, tried in order:

| Order | Pattern | Yields |
|---|---|---|
| 1 | `^(feat\|fix\|refactor\|chore\|docs)/(\d+)-` | `#213` |
| 2 | `^([a-z]{2,6})-(\d+)-` | `ENG-7726` (uppercased) |

Both shapes come from this machine's documented branch conventions rather than
guesses about the world.

Two traps, both load-bearing:

- Pattern 2 **must** require `\d+`, or `eng-emmett-graph-assurance` yields a
  bogus `ENG-EMMETT`.
- Pattern 2 **must** deny the commit-type words (`feat fix chore docs refactor
  perf test build ci style revert`), or `fix-123-typo` — a branch with no ticket
  at all — parses as `FIX-123`.

**The column goes after the title, not before the number.** On a sampled board
4 of 14 branches yield nothing (`agents/…` and `cursor/…` have no id by
construction), so blanks are common. Before the number, a blank is an empty
10-cell slot *and* every `#number` gets pushed off the gutter. After the title,
blanks land against the title's already-ragged right edge, where the eye expects
irregularity: a gap before an aligned column is a hole, a gap after ragged text
is invisible.

No configuration. prdash has no config file — one env var, `PRDASH_ACTION_FILE`,
existing for tests — and the failure mode here is a blank cell. If it misfires
in practice, `PRDASH_TICKET_RE` follows that precedent in one line.

### Filter bar

Always boxed: rounded border, leading search glyph, three rows. Focused shows
the cursor and a live `17→4` match count right-aligned inside the box; blurred
with a query shows the query in accent plus `esc clears`; blurred and empty
shows a dim placeholder.

`filterBarRows()` already measures off the render, so `contentHeight` adjusts
without a second source of truth.

### Preview

`sectionRule` becomes `sectionHeader`: a glyph, Title Case name, and underline
in one accent. The `─────` rule and the uppercasing both go. Body text stays
dim; colour appears only on data — CI verdicts, diffstat, chips, author hue.

The pane paints its *scaffolding* today (uppercase sapphire label plus a rule on
every block); this makes it paint only its *content*.

Identity block gains, in order: the `#num` + title line, then
`author · main ← head · age`, then the label chip row. Labels get no section
header — they are identity metadata, not a section.

Section glyphs land as named constants carrying `// nerd:` hints for blocker,
checks, review, threads, latest and description; the actual glyph values are set
by the operator per the icon convention.

Co-authors surface here via `assignees`, already fetched at `first: 20`. True
`Co-authored-by` trailers live in commit messages and would need a nested
connection, so they are out of scope.

### Responsive degradation

Same mechanism `computeLayout` already applies to panes via `ShowSide` /
`ShowPanel` / `ShowFooter`, extended to columns. Widths are **list** cells, not
terminal cells — once the preview drops, the list gets the whole terminal back,
so the lower steps fire less often than the numbers suggest.

| List cells | Sheds | Why this one next |
|---|---|---|
| ≥ 92 | nothing | Everything fits |
| 80–91 | diffstat → `±430` | Magnitude survives; the split is detail the preview has |
| 70–79 | author → initials | The hue already carries identity; text is confirmation |
| 62–69 | ticket dropped | Reference data, and blank a third of the time |
| < 62 | diffstat dropped | Below this the title needs every cell |

**The title and the glyph gutter never shed.** Those two are what the board is
for; everything else is affordance.

At wide widths the author column is the **full login**: `asaf-s-factify` and
`assaflavi` both reduce to `AS`, and telling two people apart by hue alone is
fragile. Initials appear only under the ladder, where the alternative is no
author at all.

### V select-group

Cycles **cluster → category → all → none**: the author cluster the cursor
occupies, then its category, then everything, then nothing. Extends the existing
cycle rather than replacing it.

#89 makes the first step stack-aware — inside a stack it selects the chain
instead of the cluster — so the rule is "tightest unit the cursor occupies".
Implementing the first step as a lookup of that unit, rather than hardcoding
"cluster", keeps #89 from having to rework it.

### Narrow terminals

Below the preview threshold, labels and branch are reachable only via `→`.
`statusBar` gains one dim segment carrying the focused PR's head branch,
truncated **from the left** so the distinctive tail survives. It is what the
copy and worktree actions operate on, so it earns permanent visibility; labels
stay behind `→`.

## Data

`Additions`, `Deletions` and `ChangedFiles` join `qlPR` and `gh.PR`. They are
leaf scalars on the search node, so the GraphQL cost is unchanged — measured at
1 point for a 60-PR page. The four stack fields for #89 land in the same commit
for the same reason.

**`schemaVer` goes v4 → v5.** Cached `[]PR` JSON has none of these fields, so
without the bump every cached list paints `+0 -0` until it expires.

## Approach

| Approach | Idea | Verdict |
|---|---|---|
| **A. Delete `TwoLine`, widen the column set** | Row becomes single-line; a responsive ladder sheds columns by list width | **Chosen** — the second line existed only to hold what now lives in the preview; keeping it would mean an empty line 2 |
| B. Keep `TwoLine`, move only labels | Smaller diff | Rejected — leaves a line holding one field, and does not fix ordering, draft placement, or the missing diffstat |
| C. Configurable column set | Operator picks columns | Rejected — YAGNI, and prdash has no config surface to hang it on |

For the ordering, `groupByAuthor` is reused with its cluster key swapped rather
than replaced, so the "clusters are contiguous, cursor walks them top to bottom"
invariant it already guarantees carries over untouched.

## Testing

The render tests are differential rather than golden (see the note in
`gridhints_test.go`), so each phase updates assertions instead of frozen output.

- `renderItemRow`: single-line only; draft overrides cell 1; diffstat width is
  stable across a set with mixed magnitudes; abbreviation at exactly 1000.
- Ticket parsing: table-driven over the real branch shapes, **including**
  `eng-emmett-graph-assurance` → empty and `fix-123-typo` → empty. Those two are
  the whole point of the regex constraints; without them the tests pass on a
  broken parser.
- Ordering: clusters contiguous, led by highest number, drafts last, and a
  property that a CI-state change produces an identical row order — that is the
  #62 regression.
- `layout_sweep_regression_test.go` gains the column ladder: every breakpoint
  renders without overflow, and the title never reaches zero width.
- The filter box's extra two rows must not desynchronise `contentHeight` from
  `filterBarRows()`.
- Row separation: a board of N rows renders N lines, not 2N — a guard against a
  separator being reintroduced by accident.

## Out of scope

- Stacked PRs (#89), though this spec reserves the tree column and lands the
  fields.
- Reversible auto-merge and mark-ready (#90), which touches the same optimistic
  paint path for the draft glyph.
- `closingIssuesReferences` as a ticket-id fallback: it is a nested connection
  and therefore not free. Revisit only if the blanks prove annoying.
- Avatars. `avatarUrl` is a free leaf but there is no field distinguishing a
  generated identicon, and rendering images needs kitty-graphics or sixel
  passthrough through tmux, which smears on scroll. Tracked separately if ever.
