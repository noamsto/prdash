# Dynamic light/dark theming for prdash

**Issue:** [#16](https://github.com/noamsto/prdash/issues/16)
**Date:** 2026-07-07

## Problem

prdash ships a single hardcoded Catppuccin Mocha palette (`internal/ui/theme.go`:
`var theme = Mocha()` plus ~18 package-global `lipgloss.Style` vars computed once
at init). The glamour markdown preview is likewise pinned to
`styles.DarkStyleConfig`. On a light desktop the whole board is unreadable.

The desktop already has a system-wide light/dark switch: `theme-toggle.sh`
(Hyprland keybind) writes `$XDG_STATE_HOME/theme-state.json` and pushes live
updates to fish, tmux, kitty, starship, etc. lazytmux's Go picker already follows
this signal via its own `detectTheme()`. prdash should join that system and
follow the same signal, live.

## Goal

prdash reads the system theme mode from `theme-state.json`, renders in a matching
Catppuccin flavor (Mocha dark / Latte light), and live-follows toggles while
running — without inheriting terminal colors, so it also works standalone outside
tmux.

## Non-goals

- Reading the live tmux catppuccin color options. prdash owns a bespoke palette (author-hue
  rotation, deliberate role exclusions) that is not a straight Catppuccin mapping,
  and must work outside tmux. Only the *mode* comes from the state file.
- A manual keybinding override. Auto-follow only, for now. (A future override key
  is a clean addition: set `themeMode` from a key handler and stop honoring the
  watcher — out of scope here.)
- Additional flavors (Frappé/Macchiato).

## Signal source

Mirror lazytmux's `detectTheme()` exactly:

```go
// detectTheme reports "light" or "dark" from the system theme-state file,
// defaulting to "dark" on any error. Mirrors lazytmux's picker.
func detectTheme() string {
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg == "" {
		xdg = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	data, err := os.ReadFile(filepath.Join(xdg, "theme-state.json"))
	if err != nil {
		return "dark"
	}
	var cfg struct {
		Theme string `json:"theme"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Theme == "" {
		return "dark"
	}
	return cfg.Theme
}
```

The state file shape (written by `theme-toggle.sh` via atomic `mv`):

```json
{ "theme": "light", "timestamp": "...", "failed": [], "version": 1 }
```

A companion `themeStatePath()` returns the resolved path so `detectTheme` and the
watcher agree on one location.

## Palettes

`Mocha()` is unchanged. Add `Latte()` returning the same `Theme` struct, roles
mapped to nix-config `palette.nix` **light** values (WCAG-AA-adjusted Latte — the
accents are darkened for contrast on the light base, so prdash matches the rest of
the desktop):

| Role | Mocha | Latte |
|---|---|---|
| Accent / Header (mauve) | `#cba6f7` | `#8839ef` |
| Focus (sky) | `#89dceb` | `#0480b3` |
| Select (pink) | `#f5c2e7` | `#b84a9e` |
| Text | `#cdd6f4` | `#4c4f69` |
| Meta (subtext0) | `#a6adc8` | `#6c6f85` |
| Rule (surface2) | `#585b70` | `#acb0be` |
| RowBg (surface0) | `#313244` | `#ccd0da` |
| Pass (green) | `#a6e3a1` | `#358023` |
| Fail (red) | `#f38ba8` | `#d20f39` |
| Pending (yellow) | `#f9e2af` | `#996b00` |
| Draft (peach) | `#fab387` | `#c24b00` |
| Section (sapphire) | `#74c7ec` | `#1a7d8f` |
| Base | `#1e1e2e` | `#eff1f5` |
| Author rotation | `#b4befe` `#94e2d5` `#eba0ac` `#f5e0dc` `#f2cdcd` `#89b4fa` | `#5a6ad4` `#147076` `#c0364a` `#a85847` `#b54545` `#1e66f5` |

`Base` is the canvas color, so it inverts naturally: badge text (`Base` on a bright
`Accent`/`Pass`/`Fail` fill) stays legible in both modes — dark base text on Mocha's
bright fills, light base text on Latte's darkened fills.

`themeFor(mode string) Theme` returns `Latte()` for `"light"`, else `Mocha()`.

## Switching the palette: applyTheme reassigns the globals

The theme globals are referenced at ~80 call sites across 7 files (`box.go`,
`section.go`, `prlist.go`, `picker.go`, `card.go`, `expanded.go`, `preview.go`),
including shared free helpers (`authorStyle`, `metaLine`, `ciGlyph`, `labelChip`,
`cardGlyph`, `reviewDot`, `renderTimeline`, `renderReviews`). Threading a `Styles`
value through all of them is a large, risky mechanical change for a cosmetic
feature.

Instead, **keep the package globals and reassign them in place**. `theme.go` gains
one function:

```go
// applyTheme swaps the active palette and rebuilds every derived style var.
// Called once at construction and again on every live theme change.
func applyTheme(t Theme) {
	theme = t
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	// ...every other style var, plus the badgeBase-derived run/pass/fail badges...
}
```

The ~18 style vars stay package-level `var`s (kept as declarations so zero call
sites change); `applyTheme` overwrites them. The theme-reading free helpers
(`authorStyle`, `labelChip`, `ciGlyph`, `metaLine`, `reviewStateLabel`) already
read `theme` / the style globals **at call time**, so they pick up the new palette
automatically — no signature or call-site changes anywhere.

- `Model` gains `themeMode string` and `themeModTime time.Time` (seeds the watch).
- Bootstrap lives in a new `func (m *Model) InitTheme()` — `detectTheme()`,
  `applyTheme(themeFor(mode))`, `preview.SetMode(mode)`, set `m.themeMode`, and stat
  the state file for the initial mtime. `main.go` calls it before `Run()` (next to
  the existing `SetRunner`/`SetRepo`/`Hydrate` calls), so it applies synchronously —
  no first-frame flash. **`NewModel` does not detect** — it stays theme-neutral, so
  `init()`'s default Mocha globals hold in tests and existing ANSI-exact tests are
  unaffected by the machine's live theme.
- `theme.go` gets `func init() { applyTheme(Mocha()) }` so the style globals are
  populated at package load (they become bare `var` declarations that `applyTheme`
  fills).

**Concurrency.** `applyTheme` mutates package globals, but Bubble Tea runs `Update`
and `View` on a single goroutine and `applyTheme` is only ever called from `init()`,
`InitTheme` (before the program starts), and the `Update` `themePollMsg` handler.
There is no concurrent read, so no lock is needed. This is called out because the
globals are otherwise write-once.

**Test hygiene.** Because `applyTheme` mutates process-global state, any test that
calls it (or asserts on the globals) must restore Mocha afterward:
`t.Cleanup(func() { applyTheme(Mocha()) })`. State-file tests use a temp
`XDG_STATE_HOME` via `t.Setenv`.

## Live-follow trigger — mtime-guarded poll tick

`theme-toggle.sh` has no push channel to prdash, so prdash pulls. A dedicated
tick, modeled on the existing `spinnerTick`/`checksPollTick`:

```go
// themePollMsg fires each tick, carrying the mtime observed when the tick was
// armed so the handler can skip the read when nothing changed.
type themePollMsg struct{ lastMod time.Time }

// themeWatchTick re-arms ~every second, carrying the last-seen mtime forward.
func themeWatchTick(lastMod time.Time) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return themePollMsg{lastMod: lastMod}
	})
}
```

`Init()` starts the watch via `themeWatchTick(m.themeModTime)` (the mtime seeded
by `InitTheme`). A single `themePollMsg` handler does both detection and
application inline — there is no separate "changed" message, since nothing else
triggers a theme change (an override key is out of scope). On `themePollMsg`:

- stat error, or mtime unchanged → re-arm `themeWatchTick(msg.lastMod)`, nothing
  else. (A deleted state file therefore *keeps the last mode* rather than reverting
  to dark — intended: a momentary gap during the toggle's atomic `mv` must not flash
  the board.)
- mtime changed → store the new mtime; `detectTheme()`; and if the mode differs
  from `m.themeMode`, apply it inline: `m.themeMode = mode`,
  `applyTheme(themeFor(mode))`, `preview.SetMode(mode)`, `m.renderList()`. Re-arm
  the tick with the new mtime either way.

### Why poll, not fsnotify or OSC 11

- **Atomic-rename safety.** The toggle writes via `mv tmp state` — the rename swaps
  the inode. A direct fsnotify *file* watch goes dead after the first toggle; you'd
  have to watch the parent directory and filter for the filename. Poll is
  rename-immune with no extra machinery.
- **No new dependency.** prdash's dep tree is deliberately lean (charm + a couple);
  fsnotify + a goroutine-to-Bubble-Tea bridge is disproportionate for a signal that
  changes a few times a day.
- **Idiom match.** `tea.Tick` self-re-arming cmds are the existing pattern here.
- **Cost / latency.** One `os.Stat`/sec; ≤1s lag on a toggle is imperceptible.

Unlike the other self-terminating ticks, the theme watch runs for the lifetime of
the program (the theme can change at any time).

## Preview / glamour theme-awareness

`internal/preview` currently pins `darkStyle = styles.DarkStyleConfig` and caches
renderers by width only. Mirror the ui package's global-reassignment approach so
`preview.Render`'s signature and its two ui callers stay untouched:

- `theme.go`: `var activeStyle = styles.DarkStyleConfig` plus a `lightStyle =
  styles.LightStyleConfig`, and:

  ```go
  // SetMode swaps the glamour style and flushes the memoized renderers/output,
  // so the next Render rebuilds against the new palette. Same single goroutine
  // as Render (bubbletea View loop), so no lock.
  func SetMode(mode string) {
      if mode == "light" {
          activeStyle = lightStyle
      } else {
          activeStyle = darkStyle
      }
      rendererByWidth = map[int]*glamour.TermRenderer{}
      outputByKey = map[string]string{}
  }
  ```
- `render.go`: `Render` keeps its `(md string, width int)` signature but builds the
  renderer from `activeStyle` instead of the `darkStyle` constant. No call-site
  changes in `ui/preview.go` (`renderTimeline`) or `ui/expanded.go`
  (`renderReviews`); `metaLine` there already reads the ui globals reassigned by
  `applyTheme`.

Ordering matters: the `themeChangedMsg` handler calls `preview.SetMode(mode)`
(flushes caches) **before** `renderList()`, so the re-render repopulates them under
the new palette.

## Testing

- `theme_test.go`: `Latte()` differs from `Mocha()` on representative roles
  (`Accent`, `Text`, `Base`, `Author[0]`); `themeFor` maps `"light"→Latte`,
  everything else→Mocha. `applyTheme(Latte())` then rendering with `accentStyle`
  produces different ANSI than under Mocha; each such test ends with
  `t.Cleanup(func(){ applyTheme(Mocha()) })`.
- `detectTheme` table: missing file → `"dark"`; malformed JSON → `"dark"`; empty
  `theme` → `"dark"`; `"light"`/`"dark"` parse through. Use `t.Setenv` to point
  `XDG_STATE_HOME` at a temp dir with a written state file.
- Watcher: a `themeChangedMsg{"light"}` through `Update` leaves `m.themeMode ==
  "light"` and the globals rebuilt to Latte (assert via `theme.Accent`); a
  `themePollMsg` whose `lastMod` equals the file's current mtime produces no
  `themeChangedMsg` (mode unchanged). Restore Mocha in cleanup.
- Preview: `SetMode("light")` then `Render` yields different ANSI than
  `SetMode("dark")` + `Render` for the same input, and `SetMode` flushes the cache
  (a subsequent identical `Render` is a miss — `renderMisses` increments). Restore
  `SetMode("dark")` in cleanup.
- No existing render tests change signatures — nothing is threaded, so
  `box_test.go`, `section_test.go`, `expanded_test.go` compile and pass unchanged.
  (Tests that assert exact ANSI still run under the default Mocha globals.)

## Rollout

Single PR against `main` from `feat/16-dynamic-theme`: `Latte()` + `applyTheme`,
the `preview.SetMode` swap, and the detect+watch wiring land together.
