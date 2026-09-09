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
| none parsed | status: `no linked issue` |
| Linear id, no `linear` on PATH | status: `install the linear CLI to open ENG-7659` |

Binding: `O`, PR board only, `Scope: "per-selected"` (matching `o`), label
`Open linked issue`, `Native: "open-issue"`.

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
