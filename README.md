# prdash

A fast terminal dashboard for triaging GitHub pull requests, built on
[Bubble Tea](https://github.com/charmbracelet/bubbletea). It lists a repo's open
PRs, surfaces the one thing each needs (merge conflict, failing checks, missing
reviewers, ready-to-merge), and turns the focused PR into a worktree with one
key.

![prdash browsing charmbracelet/bubbletea: cached instant paint, triage preview, expanded PR view](demo/board.gif)

## Features

- **Dense board** of open PRs with CI, review, and conflict/behind glyphs, drafts
  demoted, grouped by author outside the "mine" view.
- **Triage preview** — the focused PR's recommended next action, check rollup,
  requested reviewers, and a folded comment timeline (rendered markdown).
- **Fuzzy find** (`/`) narrows any board to the rows you type.
- **Runs board** — repo-wide GitHub Actions workflow runs, running and queued
  first, live status and job preview.
- **Action palette** (`a`) runs against the focused PR or a multi-selected set —
  merge, rerun failed checks, update branch, mark ready, copy URL/branch, open in
  browser, and open a git worktree.
- **Stale-while-revalidate caching** — the PR list, per-PR detail, and the
  assignable-members list are cached to disk (`$XDG_STATE_HOME/prdash`) and
  painted instantly on launch, then refreshed in the background. Each board's
  section halves are pre-warmed at startup, too.

## Install

Run in any GitHub repo checkout (requires the [`gh`](https://cli.github.com) CLI,
authenticated):

```sh
nix run github:noamsto/prdash
# or
go build -o prdash . && ./prdash
```

## Keys

### Board (PR / issue list)

| Key | Action |
|-----|--------|
| `↑↓` / `j` `k` | move the cursor |
| `→` / `l` | open the expanded PR view (PR mode only) |
| `⇥` (Tab) | cycle PRs → Issues → Runs |
| `p` | unfold all comments in the preview's timeline (PR only) |
| `z` | maximize the preview; `ctrl+j` / `ctrl+k` scroll it |
| `s` | cycle state (open · merged · closed · …) |
| `F` | filter by author (PR only) |
| `R` | assign reviewers (PR only) |
| `D` | toggle drafts (PR only) |
| `g` | group the default board by author instead of Review requested / Mine / Others (PR only) |
| `e` | fold / unfold the focused repo group (default PR board); the current repo's group stays expanded |
| `/` | fuzzy find · `space` select · `V` select cluster → category → all → none |
| `a` | actions menu · `?` legend · `q` quit · `ctrl+c` quits from anywhere, prompts and overlays included |

Actions on the focused PR (or the whole selection):

| Key | Action |
|-----|--------|
| `enter` | open a git worktree |
| `W` | open a worktree per selected item (prompts first past four) |
| `m` merge (squash) · `S` merge stack up to here · `r` rerun checks · `u` update branch · `M` mark ready | PR only |
| `y` copy # · `Y` copy URL · `b` copy branch · `o` open in browser | |

### Runs board

| Key | Action |
|-----|--------|
| `↑↓` / `j` `k` | move the cursor |
| `←` `→` / `h` `l` | focus a job in the preview |
| `enter` | view the focused job's logs |
| `r` | rerun failed jobs |
| `o` | open in browser |
| `Y` / `b` | copy URL / branch |
| `s` | cycle recent · active · failed |
| `/` | filter with `branch:`, `workflow:`, `event:`, `actor:` or `@login`, plus fuzzy text |

The board loads the 50 most recent runs, plus any in-progress or queued runs
beyond them, so `active` shows those even when they are older than the window.
Multi-select (`space`, `V`) is off on this board.

### Expanded PR view (`→` / `l`)

| Key | Action |
|-----|--------|
| `⇥` / `→` / `l` | next tab · `shift+⇥` / `←` / `h` previous tab |
| `1` `2` `3` `4` | jump to Conversation / Reviews / Checks / Diff |
| `j` `k` / `↑↓` | scroll (moves the check cursor on the Checks tab) |
| `<` `>` (or `,` `.`) | scroll the diff horizontally |
| `J` / `K` | next / previous PR |
| `r` / `R` | rerun the hovered / all failed checks (Checks tab) |
| `enter` | open a worktree · `esc` (or `←` on the first tab) back to list · `q` quit |

The copy actions (`y` `Y` `b`) grab the whole selection at once when multiple
items are selected with `space`.

The footer is a compact one-line status bar. `ctrl+e` unfolds it into a docked
panel listing every action available for the focused row (or, with a selection,
the batch-capable ones); `?` shows the full keymap. On a wide terminal the
preview shows beside the list, with the footer under it.
Scroll the preview with `alt+j` / `alt+k`, or with the mouse wheel while the
pointer is over the visible preview pane.

## Repo groups

The default PR board (state `open`, no typed qualifier) groups PRs by
repository. The current repo's group comes first, with its Review
requested / Mine / Others sections (or by author under `g`). It always leads
the board and is always expanded; with no open PRs of its own it shows its
header with a muted `no open PRs` line. Below it, one
group per other repo where you have an open PR that `involves:@me`
(author, reviewer, assignee, commenter, mentioned), newest activity first.
Every group is expanded: its rows sit under its header, and a blank line
separates the groups.

A group header is `owner/repo  N · <stage counts>` with a rule filling the
rest of the line. The `owner/` prefix stays dim so the repo names — the part
that differs — read apart at a glance. The stage glyphs:

| Glyph | Bucket |
|-------|--------|
| `✗` | failing checks or changes requested |
| the running-checks clock | checks running |
| `●` | awaiting review |
| `⧉` | blocked on its stack parent |
| `◌` | draft |
| `✓` | approved with no failing or running checks (merge state isn't known until you focus a PR) |
| `•` | anything else |

Counts come from the list rows alone. prdash fetches a PR's detail and checks
for the rows it shows, so folding a group takes its PRs out of that work.

- `e` folds a group to its header line, or unfolds it again; `enter` on a
  folded header unfolds it. Other actions on a folded header do nothing.
  Folding drops that group's selection. The current repo's group never
  folds: `e` on it is a no-op with a status hint.
- `/` matches PRs inside folded groups, and reveals the group when you clear or
  leave the filter on a match.
- **Worktrees on another repo:** `enter`, `W` and `X` use the checkout at
  `$PRDASH_REPOS_ROOT/<owner>/<repo>` (default `~/git`), only if its `origin` is
  that repo. Otherwise prdash shows a notice with the path it tried and does
  nothing.
- **Scope:** the grouped view is the default open board only. The state cycle
  `s` (merged, closed…) and typed qualifiers (`@login`, `is:`, `author:`…) show
  the current repo only, as before. The Issues and Runs boards are current-repo
  only.
- **Search:** the cross-repo search is one GraphQL search per refresh, cached
  and painted on launch like the main list. It takes the 100 most recently
  updated matching PRs, so repos past that are not shown.

## Worktree handoff

`enter` and `W` (bulk) open git worktrees, which must run in the parent shell so
the tmux window can switch. prdash supports two modes:

- **Standalone** — with no orchestrator, prdash exits the alt-screen and runs the
  command itself (`wt switch <branch>`, or `wt switch pr:N` when the branch isn't
  fetched locally or the PR comes from a fork), so the worktree opens directly.
- **Orchestrated** — when `PRDASH_ACTION_FILE` is set, prdash appends the chosen
  command to that file and quits; a wrapper (e.g. the lazytmux popup binding)
  reads and runs it. This is how the `prefix + p` popup integration works.

On another repo's PR, `enter` and `W` run `wt -C <checkout> switch …` in that
repo's local checkout (see [Repo groups](#repo-groups)).

## Opening URLs

`o` opens the PR, issue, or check in your browser. When `$BROWSER` is set,
prdash runs `$BROWSER <url>` instead of `open`/`xdg-open`. Inside a lazytmux
mirrored session, `BROWSER=og-open` routes the open to the controlling host.
