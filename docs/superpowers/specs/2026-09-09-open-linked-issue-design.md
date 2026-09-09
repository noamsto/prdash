# Open the linked issue with `O`

Issue: [#133](https://github.com/noamsto/prdash/issues/133)

## Problem

A PR row already shows the ticket its head branch names — `#213` for a personal
repo, `ENG-7659` for a Linear-tracked one. `o` opens the PR; nothing opens the
ticket, so reading it means copying the id and leaving the TUI.

## Design

`O` opens the ticket the row already displays. It reuses `ticketID()`, so what
`O` opens is always what the ticket column shows — the two can't diverge.

| Ticket | `O` does |
|---|---|
| `#213` | build `https://github.com/<owner>/<repo>/issues/213`, open via `openURL` |
| `ENG-7659` | `linear issue view -w ENG-7659`, spawned detached |
| none parsed | failure badge: `✗ no linked issue` |
| Linear id, no `linear` on PATH | failure badge: `✗ linear not found — can't open ENG-7659` |
| mixed selection, rows without a ticket | `✓ Open linked issue ×2 · 1 skipped` |
| mixed selection, opener missing on any row | `✗ linear not found — can't open ENG-7659 · 2 opened` |

Binding: `O`, PR board only, `Scope: "per-selected"` (matching `o`), label
`Open linked issue`, `Native: "open-issue"`.

A hint reports through `actionStat.err`+`fail`, never `ok`: `ok` is the
success wording and renders a green `✓`, so carrying a failure in it would
paint an actionable error as success.

The two failure modes are not equivalent. A branch that names no ticket is
benign — nothing is wrong, there is simply nothing to open — so it only ever
demotes the wording to a count. A missing opener is a configuration error the
user has to act on, so it takes the fail arm *even when other rows opened
successfully*; otherwise the one reason worth reading is the one that gets
dropped. The rows that did resolve still open, and the badge discloses them.
The message is taken from the first failing row in ascending order, so it does
not depend on selection order. Because `O` is per-selected, a partly
resolvable selection must also name the remainder — the bulk runner clears the
selection on success, so an unreported skip is unrecoverable as well as
invisible.

The issue board leaves `O` unbound: a row there *is* the issue, so `O` would
duplicate `o`.

## Why delegate Linear to the CLI

The authoritative Linear URL is `https://linear.app/<urlKey>/issue/ENG-7659`.
The `urlKey` (`factify`) is not derivable from a branch name — that yields the
*team* key `ENG` — and the workspace-less `https://linear.app/issue/ENG-7659`
is unverifiable: it returns a byte-identical 27040-byte SPA shell to the
form that includes it, so any resolution happens client-side after auth.

`linear issue view -w <id>` sidesteps URL construction entirely. It is
authoritative, needs no workspace config and no `LINEAR_API_KEY` (the CLI holds
its own credentials), and stays correct for someone with several workspaces.

Rejected:

- **Hardcode `factify`** — wrong for every other user, and the repo is public.
- **Env var / `git config prdash.linearWorkspace`** — both still need the CLI
  (or a manual paste) to learn the slug in the first place, so they defer the
  dependency rather than removing it, and add a staleness question.
- **Linear GraphQL client** — a new API client and a new secret to obtain one
  static string.
- **`closingIssuesReferences` on the PR query** — authoritative for GitHub but
  blind to Linear, and it can name an issue other than the id on screen.
- **Deriving the slug from the git remote** — org `factify-inc` vs urlKey
  `factify` is coincidence, not a rule.

## Implementation notes

`Command.Argv` is wired only to the `ExitsTUI` path (`queueExit` + `tea.Quit`,
`actions.go:124`), so an `Argv` action would quit the TUI on `O`. `O` is
therefore a `Native` handler mirroring `open-web`, spawning with
`cmd.Start()` and reaping in a goroutine exactly as `openURL` does — the TUI
must not block on the CLI's ~0.65s startup.

`Vars` gains `Ticket string` — the derived id, `""` when the branch names none —
filled by `PRSection.VarsAt`.

Also touched: the footer legend and `actionOrder` (`prlist.go:2843`, `:2909`),
and the Board archetype line in `KEYMAP.md`.

## Out of scope

- A `git config prdash.linearWorkspace` fallback for machines without the CLI.
  A missing CLI is a defined, actionable error, not a silent 404.
- Harvesting the urlKey from PR bodies that already contain Linear links.
- `-a` (open in Linear.app) as a separate binding.

## Testing

- `ticketID` → URL/argv mapping, table-driven: GitHub id, Linear id, no id.
- The two status-hint paths: no ticket parsed; Linear id with no `linear` on PATH.
- `linkedIssueArgv` split out like `browserArgv`/`clipboardArgv`, so the command
  choice is asserted without spawning anything.
