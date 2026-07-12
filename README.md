# prdash

A fast terminal dashboard for triaging GitHub pull requests, built on
[Bubble Tea](https://github.com/charmbracelet/bubbletea). It lists a repo's open
PRs, surfaces the one thing each needs (merge conflict, failing checks, missing
reviewers, ready-to-merge), and turns the focused PR into a worktree with one
key.

## Features

- **Dense board** of open PRs with CI, review, and conflict/behind glyphs, drafts
  demoted, grouped by author outside the "mine" view.
- **Triage preview** — the focused PR's recommended next action, check rollup,
  requested reviewers, and a folded comment timeline (rendered markdown).
- **Filter presets** (`f` cycles mine / review-requested / all) plus an author
  picker (`F`) and a fuzzy find (`/`).
- **Actions** run against the focused PR or a multi-selected set — merge, rerun
  failed checks, update branch, mark ready, copy URL/branch, open in browser, and
  open a git worktree.
- **Stale-while-revalidate caching** — the PR list, per-PR detail, and the
  assignable-members list are cached to disk (`$XDG_STATE_HOME/prdash`) and
  painted instantly on launch, then refreshed in the background. All filter
  presets are pre-warmed at startup so `f` is instant.

## Install

Run in any GitHub repo checkout (requires the [`gh`](https://cli.github.com) CLI,
authenticated):

```sh
nix run git+ssh://git@github.com/noamsto/prdash      # flake (private repo → git+ssh)
# or
go build -o prdash . && ./prdash
```

## Keys

### Board (PR / issue list)

| Key | Action |
|-----|--------|
| `↑↓` / `j` `k` | move the cursor |
| `→` / `l` | open the expanded PR view (PR mode only) |
| `⇥` (Tab) | toggle PRs ⟷ Issues |
| `p` | unfold all comments in the preview's timeline (PR only) |
| `z` | maximize the preview; `ctrl+j` / `ctrl+k` scroll it |
| `f` | cycle filter presets (mine · review-requested · all) |
| `s` | cycle state (open · merged · closed · …) |
| `F` | filter by author (PR only) |
| `R` | assign reviewers (PR only) |
| `D` | toggle drafts (PR only) |
| `/` | fuzzy find · `space` select · `V` select all |
| `a` | actions menu · `?` legend · `q` (or `ctrl+c`) quit |

Actions on the focused PR (or the whole selection):

| Key | Action |
|-----|--------|
| `enter` | open a git worktree |
| `W` | open a worktree per selected item (prompts first past four) |
| `m` merge (squash) · `r` rerun checks · `u` update branch · `M` mark ready | PR only |
| `y` copy # · `Y` copy URL · `b` copy branch · `o` open in browser | |

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

On a wide terminal the preview shows beside the list and a keys/actions panel
docks under it; narrow terminals drop the preview and show a compact status bar.

## Worktree handoff

`enter` and `W` (bulk) open git worktrees, which must run in the parent shell so
the tmux window can switch. prdash supports two modes:

- **Standalone** — with no orchestrator, prdash exits the alt-screen and runs the
  command itself (`wt switch pr:N`), so the worktree opens directly.
- **Orchestrated** — when `PRDASH_ACTION_FILE` is set, prdash appends the chosen
  command to that file and quits; a wrapper (e.g. the lazytmux popup binding)
  reads and runs it. This is how the `prefix + p` popup integration works.
