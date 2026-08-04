# Visualizing GitHub stacked PRs

Issue: #89 · Depends on #88 for the query fields

## Problem

GitHub ships stacked pull requests, and prdash cannot see them. A stack is a
chain of PRs where each one's base is the previous one's head, so it has two
properties the board currently cannot express: a **merge order** (only the
bottom is mergeable) and a **dependency** (a PR at position 3 is blocked by
positions 1 and 2 regardless of its own CI and review state).

Without this, a stacked PR looks like an ordinary one whose base branch happens
to be unfamiliar, and the operator can arm auto-merge on something that cannot
merge for reasons the board never showed.

## The data is free

`PullRequest.stack { number size baseRefName }` and
`PullRequest.stackEntry { position }` are object fields with **scalar leaves,
not connections**. Measured against `factify-inc/mono`: a 60-PR search page
carrying these *and* `additions` / `deletions` / `changedFiles` costs **1
point**.

`stack.entries` is a connection and is deliberately **not** queried. It is also
unnecessary: every open PR in a stack is already in the list result, so grouping
by `stack.number` and ordering by `stackEntry.position` reconstructs the chain
locally with no extra cost.

The four fields land in `qlPR` under #88, alongside the diffstat and the
`schemaVer` v4 → v5 bump, so this work needs no query change.

## The grouping conflict

The live stack in `factify-inc/mono`:

```
stack 3074 · size 2 · base main
  pos 1  #3065  agents/spicedb-rel-migrate-88ee  → base main
  pos 2  #3073  eng-7452-share-one-archer-…      → base agents/spicedb-rel-migrate-88ee
```

**#3065 is in Review requested. #3073 is in Others, as a draft.** Stacks span
categories. No sort rule fixes this — it is a grouping conflict, and it has to
be resolved by moving rows between categories or by not drawing the chain at
all.

## Behavior

### Hoisting

The whole chain renders in the category of its **base PR** (lowest visible
position). Consequence: a draft can be pulled into `Review requested` because
its parent is there. Accepted — a draft mid-chain is the next thing that will
need reviewing, not noise.

The base PR **is** the root and renders as an ordinary row. There is no header
row: `size` is the visible count, and `baseRefName` is already on the root's
preview identity line, so a header would spend a row restating both.

### Chain rendering

```
✓ ●  ⚠  ⧉  #3065  feat(infra): add deploy-time SpiceDB migrator
◌ ●      ├─ #3073  feat(services): enforce plan ownership
◌ ●      ╰─ #3099  feat(services): archer id migration
```

- Root takes `⧉` in the tree column. **Not** `┌─`: a line connector has to align
  vertically with the `╰─` below it, and a glyph makes no alignment promise it
  cannot keep.
- Children take `├─`, last child `╰─`.
- **No indent.** The glyph carries the structure, so every `#number` stays on
  one column and no title width is spent.

Two things worth recording, because both are easy to get wrong:

- No unindented box-drawing is *strictly* accurate for a chain — `├─`/`╰─` is
  sibling notation, and a true chain needs a staircase where each link indents
  past its parent. This is a chosen compromise: `├─`/`╰─` marks where the stack
  **ends**, which a repeated `╰─` cannot, and position ordering plus the `⧉`
  root carry the sequence.
- **At `size: 2`, correct chain notation and incorrect sibling notation render
  identically.** Any fixture that exercises this needs **at least three links**
  or it cannot fail.

### Ordering and filtering

- **A stack orders as one unit.** It is positioned by its base PR under #88's
  number-descending rule, and drafts inside it keep their position instead of
  sinking. The chain is the unit; `rankDraft` demotion does not apply within it.
- **`D` cannot hide a stacked draft.** Removing a link would leave `╰─` claiming
  to be the tip when it is not, so the toggle skips stacked drafts.
- **Merged links get reported.** Once a link merges it leaves an `is:open`
  board, so the visible chain is shorter than `size`. The root carries a dim
  `⧉+N` when the two disagree — a tree that silently shortens is a lie. If
  *position 1* is the one that merged, the lowest visible member becomes the
  root.

### Triage

`position` yields a blocker rule with no extra data: a PR at `position > 1`
whose parent is unmerged cannot merge. `triage.Compute` names it — "blocked on
#3065".

This belongs in the card, **not** on the row, where `╰─` already says it.
Repeating it as a row cost one line per stacked PR to restate a glyph.

Interaction with the existing blocker precedence: stack-blocked ranks above CI
and review states, because no amount of green makes a blocked PR mergeable.

### The stack number is never shown

`stack.number` is `3074`, drawn from the repo's shared issue/PR sequence — but
**3074 is neither a PR nor an issue** (both lookups return NOT_FOUND),
`PullRequestStack` exposes no `url`, and no repository-level field fetches a
stack by number. Rendering `#3074` would promise a page that 404s.

If two stacks ever land in one category, their base branches already distinguish
them in the preview.

## Approach

| Approach | Idea | Verdict |
|---|---|---|
| **A. Hoist to the base PR's category, draw the chain** | Chain and merge order visible in place | **Chosen** — the dependency is the point; seeing it is worth a draft appearing one category early |
| B. Badge members in place (`⧉2/2`) | Nothing reorders; categories stay pure | Rejected — the chain is never drawn, so you infer it from two badges in different groups, and nothing shows the blocker |
| C. Dedicated `Stacks` group above the categories | Stacks never split | Rejected — a stacked PR you are asked to review disappears from `Review requested`, which is the board's primary job |
| D. Badge now, decide grouping later | Defers commitment | Rejected — the grouping question is answerable now, and the deferred state still needs most of the same code |

Local reconstruction (group by `stack.number`, order by `stackEntry.position`)
is chosen over querying `stack.entries` because the members are already present
and the connection would add real cost for data we hold.

## Testing

- **Every stack fixture has three or more links.** A two-link fixture passes
  against sibling notation and is therefore worthless here.
- Hoisting: a chain whose members span three categories renders once, under the
  base PR's category, and appears in no other.
- Root selection: with position 1 absent, position 2 becomes the root and
  carries `⧉+1`.
- `size` vs visible count: equal → no marker; unequal → `⧉+N`.
- `D` with a stacked draft leaves the chain intact; `D` with an unstacked draft
  hides it.
- Ordering: a stack stays contiguous and keeps position order under the
  number-descending cluster rule, including when a middle link is a draft.
- `triage.Compute`: `position > 1` with an unmerged parent outranks a failing-CI
  blocker.
- `V` on a row inside a stack selects the chain (the tightest-unit rule from
  #88).

## Out of scope

- Stack-aware bulk merge (merge the whole chain bottom-up in one action).
- Reordering or restacking from prdash.
- `stack.entries` — deliberately unqueried, as above.
