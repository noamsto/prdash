# Shared theme-state library — Design

Extract the `theme-state.json` light/dark detector into one small Go module
consumed by every TUI in the fleet. Donor is prdash, whose implementation is the
only one with live reload and tests.

## Problem

`detectTheme()` — resolve `$XDG_STATE_HOME/theme-state.json`, parse
`{"theme": "light"|"dark"}`, default `"dark"` on any error — exists **six times
across four repos**:

| # | Location | Shape |
|---|---|---|
| 1 | `prdash/internal/ui/theme_state.go` | `themeStatePath` + `detectTheme` + `statModTime`; **mtime live reload** (`prlist.go:121`, `:1448`); unit-tested |
| 2 | `tmux-remux/internal/picker/theme.go:74` | `detectFlavour()`, read-once |
| 3 | `lazytmux/picker/splash/theme.go` | `detectTheme()`, read-once, unit-tested |
| 4 | `lazytmux/picker/statusline/claude.go` | `detectTheme()`, read-once |
| 5 | `lazytmux/picker/tui.go` | inline read, read-once |
| 6 | `aeye/theme.go` | `detectTheme()`, read-once |

Copies 3 and 6 are **byte-identical function bodies**, differing only in the doc
comment. Three of the six live inside lazytmux.

The copies are hand-synced, and the code says so: prdash's comment reads
*"Mirrors lazytmux's picker"*; lazytmux's reads *"same contract as the shell
scripts and the picker"*. A contract maintained by cross-referencing comments in
four repos is a library that has not been written yet.

The contract also extends beyond Go: `theme-toggle.sh` writes the file. So the
schema is already fleet-wide and already load-bearing — this change gives it one
Go implementation rather than inventing a new contract.

### Capability drift

Only prdash reloads on change. The other five read once at startup, so toggling
the system theme leaves them stale until restart. Adopting prdash's version is
therefore not merely deduplication — it is a capability upgrade for four apps.

## Non-goals

Each was evaluated against the fleet and rejected. Recorded so they are not
re-litigated.

| Rejected candidate | Why |
|---|---|
| **tmux `@thm_*` palette resolver** (`tmux-remux/internal/picker/theme.go:31,96`) | **One** adopter. aeye's `show-options` reads `@claude_img_axis` (pane geometry); lazytmux's reads `-w` window state (enrichcard). Neither is theme. prdash explicitly refuses terminal-theme inheritance (`internal/ui/theme.go:13`). |
| **Theme palettes** (prdash `Theme`, tmux-remux `Theme`) | Two unrelated systems with different sources of truth and different vocabularies — prdash's roles are domain terms (`Issue`, `Draft`, `Pass`, `Author[]`), tmux-remux's are Catppuccin names. wtc, aeye, lazytmux have no theme system at all, just inline `lipgloss.Color`. |
| **WCAG contrast math** (prdash `relativeLuminance`, `contrastRatio`, `lightText`, `chipReadableAsText`) | Pure, testable, zero-dependency — but **one** adopter. Library at second use. |
| **Clipboard / OSC 52** (`aeye/clipboard.go`, `prdash/internal/ui/clipboard.go`) | Different problems: aeye copies image *files* by MIME type via osascript pasteboard classes; prdash copies *text* to a stdin writer. Real overlap is ~15 lines of tool selection, and the two diverge on `DISPLAY` gating and `xsel`. A shared API would need `(goos, wayland, display, mime, lookPath)` — a signature as large as the body. The OSC 52 half is already covered by the existing `charmbracelet/x/ansi` dependency; see Prior art. |
| **tmux client substrate** (`tmux-remux/internal/tmux/client.go`) | Genuine (~120 lines, `Run` + env synthesis + no-server detection + format parsing), but a tmux-remux ↔ lazytmux affair — prdash invokes **zero** tmux subcommands. Published Go clients were reviewed and rejected on quality (see Prior art); if extracted, extract ours. Defer; own decision. |
| **Worktree ops** (`wtc/internal/git/worktree.go`, `prdash/internal/gh/localgit.go`) | Genuine — `RemoveWorktree` is duplicated, and wtc has a *tested* `ParseWorktreesPorcelain` that prdash lacks while carrying an open worktree bug (#45). Not solved by go-git (see Prior art). The strongest remaining candidate after this one. Deferred to keep this change single-purpose. |
| **Keymap / help spine** | Not an extraction — wtc and tmux-remux already use `bubbles/key` + `help` directly. Shipping the convention's binding tables is new code implementing `2026-07-20-tui-interaction-convention-design.md`, and belongs to that spec. |

Also out of scope: changing any app's palette values, changing prdash's
no-terminal-theme-inheritance decision, and migrating the shell scripts.

## Prior art — is this a reinvention?

Checked before committing to write anything.

**This extraction: no.** The contract is `$XDG_STATE_HOME/theme-state.json`, a
file written by our own `theme-toggle.sh`. No third-party library can read a
private schema, so there is nothing off-the-shelf to adopt in its place.

**The signal it carries: arguably yes — and that is a decision, not an
oversight.** The freedesktop standard for "does the user prefer dark?" is the
XDG Desktop Portal setting `org.freedesktop.appearance` / `color-scheme`
(`0` = no preference, `1` = prefer dark, `2` = prefer light), read over D-Bus
from `org.freedesktop.portal.Settings.Read`. It is what GTK apps, Chrome, and
Ghostty honor. There is no established Go wrapper; it would be a direct
`godbus/dbus` call.

We keep `theme-state.json` as the source of truth, deliberately:

- It requires no D-Bus session and no portal backend. Tiling-WM setups routinely
  lack `xdg-desktop-portal-gtk`, and the portal returns nothing without one.
- `theme-toggle.sh` owns the write, so the toggle is ours to trigger from
  anywhere — including over SSH and inside containers, where the portal is absent.
- The contract already spans shell, Go, and nix-config's `palette.nix`. The
  portal would add a second source of truth rather than replace the first.
- Zero dependencies, versus a D-Bus client.

Should this be revisited, the natural shape is `Detect()` preferring the portal
and falling back to the file — which the proposed API accommodates without a
signature change. Out of scope here.

### Deferred candidates, re-checked against upstream

Published alternatives exist for two of the three, but neither survives a quality
review. Measured 2026-07-30:

| Library | Stars | Importers | Last push | Notes |
|---|---|---|---|---|
| `jubnzv/go-tmux` | 54 | 20, several forks of itself | 2025-05-11 | Best of the set |
| `GianlucaP106/gotmux` | 47 | negligible | 2025-10-22 | v0.5.0, 5 open issues |
| `wricardo/gomux` | 38 | — | 2025-01-26 | Created 2014 |
| `rafi/jig` | 3 | — | 2024-09-24 | An application, not a library |
| `owenthereal/tmux` | 2 | — | 2026-01-10 | Personal scratch repo |
| `atomicstack/gotmuxcc` | 0 | — | 2026-06-29 | Control-mode; zero validation |
| `aymanbagabas/go-osc52` | 48 | — | 2023-06-21 | Three years stale |

- **tmux client — keep the hand-rolled one.** The strongest candidate is 54 stars,
  fourteen months stale, and its importers are small personal sessionizer and
  dotfile tools (plus forks of itself). `tmux-remux/internal/tmux/client.go` is
  185 lines with integration scaffolding (`testutil/tmuxserver.go`) and encodes
  two details a generic client is unlikely to handle: `isNoServerStderr`
  (distinguishing "no server running" from a real failure) and
  `withSynthesizedTmuxEnv`. tmux's CLI is a stable target, so those 185 lines
  carry near-zero maintenance. `gotmuxcc` at zero stars is not a candidate to
  replace lazytmux's control-mode bridge. If this is ever extracted, extract
  *ours*.
- **Worktree ops — not a reinvention; the candidate stands.** go-git's worktree
  support lives in `x/plumbing/worktree`, is flagged experimental, and implements
  only `add` and `remove` — no `list`, no `prune`, no lock/move. wtc needs
  precisely `list` (`ParseWorktreesPorcelain`) and `prune` (`PruneWorktrees`).
  Shelling out to `git worktree list --porcelain` remains correct.
- **Clipboard / OSC 52 — already covered; no new dependency wanted.**
  `charmbracelet/x/ansi` is already a *direct* dependency and ships `clipboard.go`
  with `SetSystemClipboard`, `SetPrimaryClipboard`, `RequestClipboard`, and
  `ResetClipboard`, so OSC 52 needs nothing added. Nor is #20 an OSC 52 encoding
  problem: `internal/ui/actions.go:129` records the actual cause — tmux 3.6 drops
  OSC 52 sent from a popup, fixed in tmux 3.7 (tmux issue 4797) — and the code
  deliberately prefers native tools until then. No library action applies.

## What gets extracted

The donor is prdash's `internal/ui/theme_state.go` — the superset. Three
behaviors, ~40 lines:

1. **Path resolution** — `$XDG_STATE_HOME`, falling back to `$HOME/.local/state`,
   then `theme-state.json`.
2. **Detection** — read, JSON-decode `{"theme": string}`, return `"light"` or
   `"dark"`; default `"dark"` on missing file, unreadable file, malformed JSON,
   or empty `theme` value.
3. **Change detection** — mtime of the state file, so callers can poll and
   reload. Currently prdash-only.

### API

```go
package themestate

// Path returns the resolved theme-state file path, honoring XDG_STATE_HOME.
func Path() string

// Detect reports "light" or "dark", defaulting to "dark" on any error.
func Detect() string

// ModTime returns the state file's mtime, or a zero time and an error if absent.
func ModTime() (time.Time, error)
```

Three package-level functions over a struct: there is no per-instance state, and
every existing call site is a bare function call. `Detect()` returns a `string`
rather than an enum because all six current implementations return `string` and
every consumer switches on `"light"`; introducing a type would force churn at
each call site for no correctness gain at this size.

`ModTime` is exposed rather than a `Watch`/callback helper because the two
plausible reload strategies differ — prdash polls mtime inside its Bubble Tea
tick (`prlist.go:1217`, `themeWatchTick`), while a `Watch` goroutine would impose
a lifecycle the apps do not share. Polling policy stays with the app; only the
mtime read is shared.

`ModTime` drops the donor's path parameter: `statModTime(path)` is called at
`prlist.go:1008` and `:1441` as `statModTime(themeStatePath())`, and every other
call site would do the same, so the argument only offers the caller a way to get
it wrong. `Path()` stays exported for callers that need the path itself.

The file's real schema carries a `version` field alongside `theme` (see
`internal/ui/theme_state_test.go:27`). The decoder targets only `theme` and
ignores the rest, which is the existing behavior in all six copies and must be
preserved — the module must not tighten decoding into an error on unknown fields.

### Canonical home

A new module, `github.com/noamsto/themestate`.

Not inside an application repo: five consumers should not version against an
application's release cadence, and exporting it from prdash or lazytmux would
make their `internal/` boundaries meaningless. The convention spec already noted
this multi-repo reality (`2026-07-20-tui-interaction-convention-design.md:200`)
— "no single branch or PR can span them" — and a standalone module is what makes
that tractable.

## Per-app migration impact

One PR per repo. No repo's behavior changes except where noted as a gain.

| App | Change | Behavior |
|---|---|---|
| **prdash** | Delete `internal/ui/theme_state.go`; call `themestate.Detect`/`ModTime`. `prlist.go:121` mtime field and the reload at `:1448` keep working unchanged. | No change (donor) |
| **aeye** | Delete `detectTheme` from `theme.go`; call `themestate.Detect`. | No change; live reload now available |
| **tmux-remux** | Replace `detectFlavour()` (`theme.go:74`) with `themestate.Detect`. Leave `readTmuxOpts` and `color()` in place — explicitly not extracted. | No change |
| **lazytmux** | Collapse three copies (`splash/theme.go`, `statusline/claude.go`, `tui.go`) onto the module. | No change; internal 3→1 |
| **wtc** | None. Has no theme detection today. Optional later adoption. | — |

lazytmux is the largest single win: three copies in one repo become one import.

## Testing

The module carries the tests. Five cases port directly from
`prdash/internal/ui/theme_state_test.go:23` (lazytmux's `splash/theme_test.go`
covers a subset of the same ground):

- Missing file → `"dark"`
- Malformed JSON (`{not json`) → `"dark"`
- Empty `{"theme":""}` → `"dark"`
- `{"theme":"light","version":1}` → `"light"`
- `{"theme":"dark","version":1}` → `"dark"`

Four are new — they cover the behavior the donor has but never asserted:

- `XDG_STATE_HOME` set → `Path()` honors it
- `XDG_STATE_HOME` unset → `Path()` falls back to `$HOME/.local/state`
- `ModTime` on a missing file → zero time and a non-nil error
- `ModTime` after a rewrite → advances

Tests set `XDG_STATE_HOME` to a `t.TempDir()`, matching the existing pattern in
`prdash/internal/ui/theme_state_test.go`.

Per-app PRs delete the corresponding duplicate tests; each app keeps whatever
integration coverage it already has around theme *application*, which is
untouched.

## Success criteria

- One implementation of the `theme-state.json` contract in Go.
- `rg 'theme-state\.json' --type go` across the five repos matches only the
  module and its tests.
- prdash renders identically before and after; its live reload still works when
  the file is toggled.
- aeye, tmux-remux, and lazytmux pick up a theme toggle without restart, or
  explicitly opt out by not polling.
- No repo gains a dependency on another application repo.

## Risks

- **Five repos, five PRs, no atomic landing.** Each app keeps its local copy
  until its own PR merges, so the fleet is briefly mixed. Harmless — the copies
  are behaviorally identical, so a partially-migrated fleet behaves exactly as it
  does today.
- **A sixth repo to maintain.** Mitigated by scope: three functions, one file,
  a contract already frozen by `theme-toggle.sh`.

## Related

- `2026-07-20-tui-interaction-convention-design.md` — the fleet-wide interaction
  convention; its "library at second use" rule (line 200) is the precedent this
  follows.
- Deferred candidate: worktree ops (`wtc/internal/git`, `prdash/internal/gh/localgit.go`),
  possibly touching prdash #45.
- A Go→Rust rewrite of the fleet was evaluated and declined on 2026-07-30. The
  deciding evidence was the audit above: total genuinely-shared substrate across
  the five TUIs is on the order of a few hundred lines, which cannot amortize a
  ~24.5k-line rewrite.
