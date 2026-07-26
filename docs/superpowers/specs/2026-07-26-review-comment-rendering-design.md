# Design: readable review comments with diff context — issue #55

Review threads landed in #52. They fetch and display, but bodies are unreadable
and carry no code context. This design fixes both, in two PRs.

## Findings that shape the design

These came from a throwaway spike rendering the four real thread bodies on
`factify-inc/mono#2936` (Cursor BugBot + chatgpt-codex-connector) through
`preview.Render`. They are the reason the design is smaller than it first looked.

**Glamour already strips the hostile HTML.** No sanitizer is needed. Verified
dropped: `<!-- DESCRIPTION START/END -->`, `<!-- BUGBOT_BUG_ID: … -->`,
`<!-- LOCATIONS START … END -->`, `<details>`/`<summary>` (collapsed to plain
text), and Cursor's ~600-character base64 `<div><a><picture>` "Fix in Cursor"
button.

**Two gaps glamour cannot close by configuration.**

1. Heading prefixes print literally — `### Advisory lost after commit retry`.
   Stock `styles.DarkStyleConfig` sets `H3.Prefix = "### "`. Reachable: `H1`–`H6`
   are `ansi.StyleBlock` fields on `ansi.StyleConfig`.
2. Inline images and links print their destination —
   `P1 Badge https://img.shields.io/badge/P1-orange?style=flat`. This is **not**
   style-configurable: `ansi/image.go` gates URL emission on `ImageElement.TextOnly`
   and `ansi/link.go` on `LinkElement.SkipHref`, and `ansi/elements.go` sets both
   only when `isInsideTable(node)`. Setting `Image.Format`/`ImageText.Format` to
   `""` removes the `Image: ` label but not the URL. A markdown pre-pass is the
   only lever.

**Summary lines are a different problem.** Glamour emits multi-line styled
blocks; the Overview `THREADS` block and the Diff tab's per-thread head line each
need exactly one line of plain text. That is a separate function, not a `Render`
mode.

## Architecture

Two seams, both in `internal/preview`, so the Conversation tab benefits from the
same fix:

```
body ──┬─► Render(md, w)         ─► glamour ─► styled multi-line block  (full body)
       │     └─ prePass(md)
       └─► PlainTitle(body)      ─► one plain line                      (summary)
```

`prePass` runs inside `Render`, before glamour. Callers do not opt in — the
Conversation tab has the identical badge/URL noise today, and fixing it at the
shared seam fixes both surfaces at once.

`PlainTitle` is exported alongside `Render`. `firstLine` in `threads_render.go`
has three call sites, and they are not all summaries:

| Site | Context | Ends up as |
|---|---|---|
| `:35` `renderThreadsSummary` | Overview one-line-per-thread list | `PlainTitle` — genuinely a summary |
| `:84` `renderFileThreads` head body | Diff tab comment body | full `Render` (PR B) |
| `:87` `renderFileThreads` reply body | Diff tab reply body | full `Render` (PR B) |

PR A points all three at `PlainTitle` and deletes `firstLine`; that alone makes
the Diff tab readable prose instead of raw markdown, even while still one line.
PR B then replaces `:84`/`:87` with full `Render` bodies, leaving `PlainTitle`
used only at `:35`.

## Components

### `internal/preview/theme.go` — heading prefixes

Wrap the stock configs rather than hand-rolling a style: copy
`styles.DarkStyleConfig`/`LightStyleConfig`, then for `H1`–`H6` set
`Prefix = ""` and `Bold = true`. Keeps every other stock decision (including
tables, which the existing comment in this file warns not to break).

### `internal/preview` — `prePass`

Unexported. Two transforms, in order:

- strip image syntax entirely: `![alt](url)` → `` (removes badges)
- collapse links to their text: `[text](url)` → `text`

Regex-based. Markdown link/image syntax does not nest in these bodies, and a
full CommonMark inline walk to delete two node types is not warranted.

OSC 8 hyperlinks are emitted by glamour from the AST, so links remain clickable
in kitty after the pre-pass — only the printed URL text goes away.

### `internal/preview` — `PlainTitle`

`PlainTitle(body string) string` returns the first line that is non-empty after
distillation, or `""`. Per candidate line, in order: strip images, collapse links
to text, strip HTML tags, trim leading `#` and `>` markers, strip emphasis and
backtick runs, collapse whitespace, trim.

Verified against the real bodies:

| Input first line | Output |
|---|---|
| `### Advisory lost after commit retry` | `Advisory lost after commit retry` |
| `**<sub><sub>![P1 Badge](https://…)</sub></sub>  Handle nullable sibling values without aborting persistence**` | `Handle nullable sibling values without aborting persistence` |
| `**<sub><sub>![P2 Badge](https://…)</sub></sub>  Normalize currency-formatted property values before comparison**` | `Normalize currency-formatted property values before comparison` |

The "first non-empty *after* distillation" rule matters: a body whose first line
is only a badge distills to `""` and must fall through to the next line rather
than yielding a blank summary.

### `internal/gh/threads.go` — `diffHunk`

Add `diffHunk` to the `comments` selection in `reviewThreadsQuery` and a
`DiffHunk string` field to `ThreadComment`, populated in `ParseReviewThreads`.
The query already selects `comments(first:100)`, so this adds no request.

`threadsSchemaVer` goes `v1` → `v2` in `internal/ui/preview.go`, so cached
`v1` responses (no `diffHunk`) are a clean miss rather than threads that silently
render without code.

### `internal/ui/threads_render.go` — layout

`renderThreadsSummary` (Overview, **PR A**): swap `firstLine` →
`preview.PlainTitle`. No other change; this block stays a compact
one-line-per-thread list.

`renderFileThreads` (Diff tab) changes in both PRs. **PR A** swaps its two
`firstLine` uses for `PlainTitle` — still one line per body, but distilled rather
than raw. **PR B** then restructures it to render, per unresolved thread:

1. the existing `L<line>  author  ● unresolved` head line
2. the head comment's `DiffHunk` in a gutter-prefixed block, dim, with `+`/`-`
   lines styled by `passStyle`/`failStyle`
3. the head comment's **full** body via `preview.Render` — no longer one line
4. replies, each as author line + full rendered body

Full bodies, not clamped. The Diff tab is already a scrollable viewport, so a
long bot finding makes the pane longer rather than hiding what you opened it to
read.

Bot footers (`Useful? React with 👍 / 👎`, `Reviewed by Cursor Bugbot for
commit …`) are **kept**. Stripping them means pattern-matching each vendor's
boilerplate, which drifts silently as vendors change format; they are two quiet
lines at the end of a scrollable pane.

## Error handling

`Render` already returns `(string, error)` and `renderDiscussionItem` falls back
to the raw markdown on failure. `renderFileThreads` follows that precedent: on a
render error, fall back to the raw body rather than dropping the comment.

`PlainTitle` cannot fail — it is string transforms with a `""` floor. A thread
whose every line distills to empty yields `""`, and the summary line renders the
location and author with no title rather than a blank row.

An absent `DiffHunk` (empty string — a thread on a file GitHub no longer returns
a hunk for) renders the thread with no hunk block, not an empty bordered box.

## Testing

`internal/preview`:

- `PlainTitle` table test with the four real 2936 first-lines as fixtures, plus
  the badge-only-first-line case that must fall through, plus empty input
- `prePass` table test: image stripped, link collapsed to text, text with neither
  passes through unchanged
- an end-to-end `Render` assertion that a body containing `### h`, a badge image,
  and an HTML comment produces output with none of `###`, `img.shields.io`, or
  `<!--`

All of the above is PR A. PR B adds:

`internal/gh`:

- extend the existing `ParseReviewThreads` fixture with `diffHunk` and assert it
  lands on the parsed comment

`internal/ui`:

- `renderFileThreads` assertion that a two-paragraph body renders more than one
  line of prose (the regression `firstLine` caused)
- `renderFileThreads` assertion that a thread with an empty `DiffHunk` renders
  without a hunk block

## Staging

**PR A — rendering.** `theme.go` heading prefixes, `Render` pre-pass,
`PlainTitle` replacing all three `firstLine` call sites, delete `firstLine`. No
GraphQL change, no cache bump. Fixes what is visibly broken on screen today.

**PR B — diff context.** `diffHunk` selection and parse, `threadsSchemaVer`
v1→v2, the `renderFileThreads` hunk block and full-body layout (which retires
`PlainTitle` from the two Diff-tab body sites, leaving it only in the Overview
summary).

A lands first so the hunk layout in B can be judged against correctly rendered
comments rather than raw markdown.
