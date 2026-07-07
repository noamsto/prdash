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

## Styles struct on the Model

Collapse the package-global `theme` var and the ~18 `lipgloss.Style` vars into a
`Styles` struct built from a `Theme`:

```go
type Styles struct {
	title, accent, dim, sep, pass, fail, pend, selMark, focusBar,
	header, statusBar, sectionLabel, draftTag, refresh,
	runBadge, passBadge, failBadge lipgloss.Style
	theme Theme // retained for authorStyle/labelChip/ciGlyph lookups
}

func newStyles(t Theme) Styles { /* build every style from t */ }
```

- `Model` gains `styles Styles` and `themeMode string`. `NewModel` calls
  `detectTheme()` → `newStyles(themeFor(mode))`.
- The theme-reading free functions become methods on `Styles`:
  `authorStyle(login)`, `labelChip(name, hex)`, `ciGlyph(state)`, `metaLine(...)`,
  `reviewStateLabel(state)`. `lightText` stays a pure free function (no theme).
- Render call sites in `card.go`, `section.go`, `expanded.go`, `preview.go`,
  `prlist.go`, `actionview.go` switch from `titleStyle` → `m.styles.title`, etc.
  Render helpers that don't already receive the model take `Styles` (or a
  `*Styles`) as a parameter.

This removes all mutable global theme state; `Styles` is a value threaded from the
Model. Larger diff than reassigning globals, but fully testable and isolation-clean.

### Section rendering note

`PRSection` and the other `section.go` renderers currently reach for the package
globals. They gain a `Styles` parameter on their render entry points (the Model
passes `m.styles`). Where a type stores rendered strings, rendering happens in the
Model's `renderList()` path where `m.styles` is in scope, so no `Styles` needs to
be stored on `PRSection` itself — confirm during implementation and, if a stored
`Styles` is unavoidable, set it in the same place `SetHideDrafts` is called.

## Live-follow trigger — mtime-guarded poll tick

`theme-toggle.sh` has no push channel to prdash, so prdash pulls. A dedicated
tick, modeled on the existing `spinnerTick`/`checksPollTick`:

```go
type themeChangedMsg struct{ mode string }

// themeWatchTick re-arms ~every second. It carries the last-seen mtime so a tick
// only re-reads the file when it actually changed.
func themeWatchTick(lastMod time.Time) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return themePollMsg{lastMod: lastMod}
	})
}
```

`Init()` starts the watch (seeded with the mtime read at construction). On
`themePollMsg`, `Update` `os.Stat`s the file:

- stat error or unchanged mtime → re-arm `themeWatchTick(lastMod)`, nothing else.
- mtime changed → `detectTheme()`; if the mode differs from `m.themeMode`, emit /
  handle `themeChangedMsg{mode}`; always re-arm with the new mtime.

On `themeChangedMsg`, `Update`:
1. `m.themeMode = mode`
2. `m.styles = newStyles(themeFor(mode))`
3. tell `internal/preview` the mode (so its next render uses the right style)
4. `m.renderList()`

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
renderers by width only. Make the mode an explicit input (no global theme state):

- `theme.go`: expose both `styles.DarkStyleConfig` and `styles.LightStyleConfig`
  via a `styleFor(mode string) ansi.StyleConfig` lookup.
- `render.go`: `Render(md string, width int, mode string)`. Key `rendererByWidth`
  by `(mode, width)` and `outputByKey` by `(mode, width, body)`. A mode flip simply
  misses into fresh cache keys — no flush needed. `renderMisses` accounting
  unchanged.
- `timeline.go` and the `internal/ui` callers thread `mode` through (from
  `m.themeMode`).

## Testing

- `theme_test.go`: `newStyles(Latte())` differs from `newStyles(Mocha())` on
  representative roles; `themeFor` maps `"light"→Latte`, everything else→Mocha.
- `detectTheme` table: missing file → `"dark"`; malformed JSON → `"dark"`; empty
  `theme` → `"dark"`; `"light"`/`"dark"` parse through. Use a temp `XDG_STATE_HOME`.
- `Update` handling: a `themeChangedMsg{"light"}` rebuilds `m.styles` to the Latte
  set; a `themePollMsg` with unchanged mtime does not.
- Preview: `Render` with `"light"` vs `"dark"` produces different ANSI and both are
  cached independently (miss count increments once per new key).
- Existing snapshot/render tests gain the `mode` parameter (default `"dark"` keeps
  current expectations).

## Rollout

Single PR against `main` from `feat/16-dynamic-theme`. The refactor (Styles struct)
and the feature (detect + follow + Latte) land together since the struct is a
prerequisite for a clean toggle.
