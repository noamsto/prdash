# Dynamic light/dark theme Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** prdash follows the system light/dark signal (`~/.local/state/theme-state.json`) live, rendering in Catppuccin Mocha (dark) or Latte (light).

**Architecture:** Keep the package-level lipgloss style globals; add `applyTheme(Theme)` that reassigns them in place. Read the mode from the state file with a `detectTheme()` that mirrors lazytmux's picker; live-follow via an mtime-guarded `tea.Tick` poll that calls `applyTheme` + `preview.SetMode` when the file changes. The glamour preview mirrors the same global-swap pattern (`SetMode`).

**Tech Stack:** Go 1.26.3, charm.land/bubbletea/v2, charm.land/lipgloss/v2, charm.land/glamour/v2. No new dependencies.

## Global Constraints

- **No new dependencies.** charm + stdlib only.
- **Go 1.26.3** (`go.mod`).
- **Latte hexes must match** nix-config `home/desktop/theme/palette.nix` `config.theme.palette.light` verbatim (WCAG-AA adjusted).
- **Default to `"dark"`** on any state-file error (missing, unreadable, malformed, empty `theme`).
- **Single-goroutine assumption:** `applyTheme`/`SetMode` mutate package globals and take no locks; only ever called from `init()`, `InitTheme` (pre-`Run`), and the `Update` loop.
- **Test hygiene:** any test that mutates the palette globals ends with `t.Cleanup(func() { applyTheme(Mocha()) })` (and `preview.SetMode("dark")` where preview is touched). State-file tests use `t.Setenv("XDG_STATE_HOME", t.TempDir())`. No `t.Parallel()` in these tests.

---

### Task 1: Latte palette + themeFor + applyTheme

**Files:**
- Modify: `internal/ui/theme.go` (the `var theme = Mocha()` + `var (...)` style block, lines ~33–78)
- Test: `internal/ui/theme_test.go`

**Interfaces:**
- Produces: `Latte() Theme`, `themeFor(mode string) Theme`, `applyTheme(t Theme)`. The ~18 style globals (`titleStyle`, `accentStyle`, …, `runBadgeStyle`, `passBadgeStyle`, `failBadgeStyle`) become bare declarations populated by `applyTheme`; `init()` calls `applyTheme(Mocha())`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/theme_test.go`:

```go
func TestLattePaletteIsLight(t *testing.T) {
	l := Latte()
	if l.Accent != "#8839ef" {
		t.Errorf("latte accent = %q, want #8839ef", l.Accent)
	}
	if l.Base != "#eff1f5" {
		t.Errorf("latte base = %q, want light #eff1f5", l.Base)
	}
	if l.Text == Mocha().Text {
		t.Error("latte text must differ from mocha text")
	}
	if len(l.Author) != len(Mocha().Author) {
		t.Errorf("latte author rotation len = %d, want %d", len(l.Author), len(Mocha().Author))
	}
}

func TestThemeFor(t *testing.T) {
	if themeFor("light").Accent != Latte().Accent {
		t.Error(`themeFor("light") should be Latte`)
	}
	if themeFor("dark").Accent != Mocha().Accent {
		t.Error(`themeFor("dark") should be Mocha`)
	}
	if themeFor("").Accent != Mocha().Accent {
		t.Error(`themeFor("") should default to Mocha`)
	}
}

func TestApplyThemeReassignsGlobals(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })
	applyTheme(Latte())
	if theme.Accent != Latte().Accent {
		t.Errorf("applyTheme did not swap the active palette: %q", theme.Accent)
	}
	latteRender := accentStyle.Render("x")
	applyTheme(Mocha())
	if latteRender == accentStyle.Render("x") {
		t.Error("accentStyle must render differently under Latte vs Mocha")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestLattePaletteIsLight|TestThemeFor|TestApplyThemeReassignsGlobals' -v`
Expected: FAIL to compile — `undefined: Latte`, `undefined: themeFor`, `undefined: applyTheme`.

- [ ] **Step 3: Add `Latte` and `themeFor` after `Mocha()`**

In `internal/ui/theme.go`, immediately after the `Mocha()` function:

```go
// Latte is the Catppuccin Latte flavor — light mode. Accents are the WCAG-AA
// adjusted values from nix-config palette.nix, so prdash matches the desktop.
func Latte() Theme {
	return Theme{
		Accent: "#8839ef", Header: "#8839ef", Focus: "#0480b3", Select: "#b84a9e",
		Text: "#4c4f69", Meta: "#6c6f85", Rule: "#acb0be", RowBg: "#ccd0da",
		Pass: "#358023", Fail: "#d20f39", Pending: "#996b00", Draft: "#c24b00",
		Section: "#1a7d8f", Base: "#eff1f5",
		Author: []string{
			"#5a6ad4", "#147076", "#c0364a",
			"#a85847", "#b54545", "#1e66f5",
		},
	}
}

// themeFor maps a mode string ("light"/"dark") to its palette; unknown → Mocha.
func themeFor(mode string) Theme {
	if mode == "light" {
		return Latte()
	}
	return Mocha()
}
```

- [ ] **Step 4: Convert the style globals to `applyTheme`-populated declarations**

In `internal/ui/theme.go`, replace the current block:

```go
// theme is the active palette. A future toggle reassigns this.
var theme = Mocha()

var (
	titleStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	accentStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	dimStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sepStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	passStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pass))
	failStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Fail))
	pendStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pending))
	selMarkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Select))
	focusBarStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))
	headerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Header)).Bold(true)
	statusBarStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sectionLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Section)).Bold(true)
	draftTagStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Draft))

	// refreshStyle marks ambient background revalidation — brighter than dim but
	// unfilled, so it stays distinct from the mauve running-action badge.
	refreshStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))

	// Filled status badges: dark base text on a bright role-color fill, so an
	// action's outcome reads as a distinct chip against the dim header.
	badgeBase      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Base)).Bold(true).Padding(0, 1)
	runBadgeStyle  = badgeBase.Background(lipgloss.Color(theme.Accent))
	passBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Pass))
	failBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Fail))
)
```

with:

```go
// theme is the active palette; applyTheme reassigns it and every derived style.
var theme Theme

var (
	titleStyle        lipgloss.Style
	accentStyle       lipgloss.Style
	dimStyle          lipgloss.Style
	sepStyle          lipgloss.Style
	passStyle         lipgloss.Style
	failStyle         lipgloss.Style
	pendStyle         lipgloss.Style
	selMarkStyle      lipgloss.Style
	focusBarStyle     lipgloss.Style
	headerStyle       lipgloss.Style
	statusBarStyle    lipgloss.Style
	sectionLabelStyle lipgloss.Style
	draftTagStyle     lipgloss.Style
	refreshStyle      lipgloss.Style // ambient revalidation; brighter than dim, unfilled
	badgeBase         lipgloss.Style // dark base text on a bright role-color fill
	runBadgeStyle     lipgloss.Style
	passBadgeStyle    lipgloss.Style
	failBadgeStyle    lipgloss.Style
)

// applyTheme swaps the active palette and rebuilds every derived style var. Safe
// without a lock: only called from init(), InitTheme (before the program runs),
// and the single-goroutine Update loop.
func applyTheme(t Theme) {
	theme = t
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	passStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pass))
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Fail))
	pendStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pending))
	selMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Select))
	focusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Header)).Bold(true)
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sectionLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Section)).Bold(true)
	draftTagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Draft))
	refreshStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))
	badgeBase = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Base)).Bold(true).Padding(0, 1)
	runBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Accent))
	passBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Pass))
	failBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Fail))
}

func init() { applyTheme(Mocha()) }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestLattePaletteIsLight|TestThemeFor|TestApplyThemeReassignsGlobals' -v`
Expected: PASS. Then `go test ./internal/ui/` — the whole ui suite still passes (globals unchanged for default Mocha).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/theme.go internal/ui/theme_test.go
git commit -m "feat(ui): add Latte palette + applyTheme palette swap (#16)"
```

---

### Task 2: detectTheme + themeStatePath + statModTime

**Files:**
- Create: `internal/ui/theme_state.go`
- Test: `internal/ui/theme_state_test.go`

**Interfaces:**
- Produces: `themeStatePath() string`, `detectTheme() string` (`"light"`/`"dark"`, default `"dark"`), `statModTime(path string) (time.Time, error)`, and a `writeState(t *testing.T, body string)` test helper reused by Task 4.

- [ ] **Step 1: Write the failing test + helper**

Create `internal/ui/theme_state_test.go`:

```go
package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// writeState points XDG_STATE_HOME at a temp dir and writes theme-state.json.
// An empty body writes no file (simulating a missing state file).
func writeState(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "theme-state.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectTheme(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"missing file", "", "dark"},
		{"malformed", "{not json", "dark"},
		{"empty theme", `{"theme":""}`, "dark"},
		{"light", `{"theme":"light","version":1}`, "light"},
		{"dark", `{"theme":"dark","version":1}`, "dark"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeState(t, tc.body)
			if got := detectTheme(); got != tc.want {
				t.Errorf("detectTheme() = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ -run TestDetectTheme -v`
Expected: FAIL to compile — `undefined: detectTheme`.

- [ ] **Step 3: Create `internal/ui/theme_state.go`**

```go
package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// themeStatePath resolves the system theme-state file, honoring XDG_STATE_HOME.
// This is the signal theme-toggle.sh writes and lazytmux's picker reads.
func themeStatePath() string {
	xdg := os.Getenv("XDG_STATE_HOME")
	if xdg == "" {
		xdg = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(xdg, "theme-state.json")
}

// detectTheme reports "light" or "dark" from the system theme-state file,
// defaulting to "dark" on any error. Mirrors lazytmux's picker.
func detectTheme() string {
	data, err := os.ReadFile(themeStatePath())
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

// statModTime returns the mtime of path, or a zero time and error if absent.
func statModTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/ -run TestDetectTheme -v`
Expected: PASS (all 5 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/theme_state.go internal/ui/theme_state_test.go
git commit -m "feat(ui): read system theme mode from theme-state.json (#16)"
```

---

### Task 3: theme-aware glamour preview (SetMode)

**Files:**
- Modify: `internal/preview/theme.go`
- Modify: `internal/preview/render.go:37` (`WithStyles`) + add `SetMode`
- Test: `internal/preview/render_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `preview.SetMode(mode string)` swaps the active glamour style and flushes `rendererByWidth`/`outputByKey`. `Render(md string, width int)` signature unchanged.

- [ ] **Step 1: Write the failing test**

Append to `internal/preview/render_test.go`:

```go
func TestSetModeChangesOutputAndFlushes(t *testing.T) {
	t.Cleanup(func() { SetMode("dark") })
	const md = "# Hello\n\nsome **bold** text"

	SetMode("dark")
	dark, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}
	before := renderMisses

	SetMode("light")
	light, err := Render(md, 60)
	if err != nil {
		t.Fatal(err)
	}
	if dark == light {
		t.Error("light and dark render of the same markdown must differ")
	}
	if renderMisses != before+1 {
		t.Errorf("SetMode should flush caches so Render misses once: misses=%d want=%d",
			renderMisses, before+1)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/preview/ -run TestSetModeChangesOutputAndFlushes -v`
Expected: FAIL to compile — `undefined: SetMode`.

- [ ] **Step 3: Add the light + active styles**

Replace `internal/preview/theme.go` with:

```go
package preview

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// darkStyle/lightStyle are glamour's built-in chroma styles. We deliberately do
// NOT post-process rendered output (no pipe-stripping), so tables render intact.
var (
	darkStyle  ansi.StyleConfig = styles.DarkStyleConfig
	lightStyle ansi.StyleConfig = styles.LightStyleConfig
)

// activeStyle is what Render builds renderers from; SetMode swaps it.
var activeStyle = darkStyle
```

- [ ] **Step 4: Build renderers from `activeStyle` and add `SetMode`**

In `internal/preview/render.go`, change line 37 from:

```go
			glamour.WithStyles(darkStyle),
```

to:

```go
			glamour.WithStyles(activeStyle),
```

and add at the end of the file:

```go
// SetMode swaps the active glamour style and flushes the memoized renderers and
// output, so the next Render rebuilds against the new palette. Same single
// goroutine as Render (the bubbletea View loop), so no lock.
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

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/preview/ -run TestSetModeChangesOutputAndFlushes -v`
Expected: PASS. Then `go test ./internal/preview/` — existing render/timeline tests still pass (default mode is dark, unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/preview/theme.go internal/preview/render.go internal/preview/render_test.go
git commit -m "feat(preview): theme-aware glamour style via SetMode (#16)"
```

---

### Task 4: Model watcher wiring + main bootstrap

**Files:**
- Modify: `internal/ui/prlist.go` — Model struct (fields), `NewModel` unchanged, `Init` (lines ~550–559), `Update` (add case near `spinnerTickMsg` ~634), and new `InitTheme` + `themeWatchTick` + `themePollMsg` near the tick helpers (~391–410)
- Modify: `main.go` — call `m.InitTheme()` before `Run`
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `applyTheme`, `themeFor` (Task 1); `detectTheme`, `themeStatePath`, `statModTime`, `writeState` (Task 2); `preview.SetMode` (Task 3).
- Produces: Model fields `themeMode string`, `themeModTime time.Time`; `(*Model).InitTheme()`; `themePollMsg`; `themeWatchTick(lastMod time.Time) tea.Cmd`; a `themePollMsg` case in `Update`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/prlist_test.go` (add `"time"` and `"github.com/noamsto/prdash/internal/preview"` to its imports):

```go
func TestInitThemeAppliesMode(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()); preview.SetMode("dark") })
	writeState(t, `{"theme":"light","version":1}`)
	m := NewModel("/repo", "is:open", nil)
	m.InitTheme()
	if m.themeMode != "light" {
		t.Errorf("themeMode = %q, want light", m.themeMode)
	}
	if theme.Accent != Latte().Accent {
		t.Errorf("InitTheme should apply Latte globals, accent=%q", theme.Accent)
	}
}

func TestThemePollAppliesChange(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()); preview.SetMode("dark") })
	writeState(t, `{"theme":"light","version":1}`)
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.themeMode = "dark" // pretend we started dark
	// zero lastMod differs from the file's real mtime → forces a re-read.
	u, _ := m.Update(themePollMsg{lastMod: time.Time{}})
	if got := u.(Model).themeMode; got != "light" {
		t.Errorf("poll should flip mode to light, got %q", got)
	}
	if theme.Accent != Latte().Accent {
		t.Errorf("poll should apply Latte globals, accent=%q", theme.Accent)
	}
}

func TestThemePollNoChangeWhenMtimeSame(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })
	writeState(t, `{"theme":"light","version":1}`)
	mod, err := statModTime(themeStatePath())
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.themeMode = "dark"
	u, _ := m.Update(themePollMsg{lastMod: mod}) // same mtime → skip the read
	if got := u.(Model).themeMode; got != "dark" {
		t.Errorf("poll with unchanged mtime must not change mode, got %q", got)
	}
	if theme.Accent != Mocha().Accent {
		t.Errorf("globals should stay Mocha, accent=%q", theme.Accent)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestInitThemeAppliesMode|TestThemePoll' -v`
Expected: FAIL to compile — `undefined: m.InitTheme`, `undefined: themePollMsg`, `m.themeMode undefined`.

- [ ] **Step 3: Add the Model fields**

In `internal/ui/prlist.go`, in the `Model` struct (after `pendingExec`):

```go
	themeMode    string    // "light"|"dark"; active palette mode
	themeModTime time.Time // last-seen mtime of the theme-state file
```

- [ ] **Step 4: Add `InitTheme`, `themePollMsg`, and `themeWatchTick`**

In `internal/ui/prlist.go`, after `checksPollTick` (~line 410):

```go
// InitTheme reads the system theme mode, applies the matching palette, and seeds
// the watch mtime. Called from main before the program starts, so the first frame
// paints in the right palette. NOT called from NewModel, so tests keep the default
// Mocha globals regardless of the machine's live theme.
func (m *Model) InitTheme() {
	m.themeMode = detectTheme()
	applyTheme(themeFor(m.themeMode))
	preview.SetMode(m.themeMode)
	m.themeModTime, _ = statModTime(themeStatePath())
}

// themePollMsg fires the theme-watch beat. lastMod is the state-file mtime seen
// when the tick was armed, so the handler skips the read when nothing changed.
type themePollMsg struct{ lastMod time.Time }

// themeWatchTick re-arms ~every second. Unlike the other ticks it runs for the
// program's lifetime — the system theme can change at any time.
func themeWatchTick(lastMod time.Time) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return themePollMsg{lastMod: lastMod}
	})
}
```

Add the preview import to `internal/ui/prlist.go`'s import block:

```go
	"github.com/noamsto/prdash/internal/preview"
```

- [ ] **Step 5: Start the watch in `Init` and handle `themePollMsg` in `Update`**

In `Init()` (~line 553), add `themeWatchTick` to the batch:

```go
	return tea.Batch(
		m.mineFetchCmd(),
		m.fetchCmd("is:open"),
		m.fetchMembersCmd(),
		spinnerTick(),
		themeWatchTick(m.themeModTime),
	)
```

In `Update`, add a case (place it next to the `spinnerTickMsg` case, ~line 634):

```go
	case themePollMsg:
		mod, err := statModTime(themeStatePath())
		if err != nil || mod.Equal(msg.lastMod) {
			return m, themeWatchTick(msg.lastMod) // gone or unchanged: keep watching
		}
		m.themeModTime = mod
		if mode := detectTheme(); mode != m.themeMode {
			m.themeMode = mode
			applyTheme(themeFor(mode))
			preview.SetMode(mode)
			m.renderList()
		}
		return m, themeWatchTick(mod)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestInitThemeAppliesMode|TestThemePoll' -v`
Expected: PASS (all three). Then `go test ./internal/ui/` — full ui suite still green.

- [ ] **Step 7: Bootstrap the theme in `main.go`**

In `main.go`, after `m.SetRepo(repo)` and before `m.Hydrate()`:

```go
	m.InitTheme()
```

- [ ] **Step 8: Verify the whole build + suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds; all packages PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go main.go
git commit -m "feat(ui): live-follow system light/dark via mtime poll (#16)"
```

---

### Task 5: End-to-end verification

**Files:** none (manual verification).

- [ ] **Step 1: Build the binary**

Run: `go build -o prdash .`
Expected: builds clean.

- [ ] **Step 2: Confirm current mode**

Run: `cat "${XDG_STATE_HOME:-$HOME/.local/state}/theme-state.json"`
Note the current `theme` value (light/dark).

- [ ] **Step 3: Run prdash in a repo and observe the palette**

In a GitHub repo with `gh` authenticated, run `./prdash`. Confirm the board renders in the palette matching the current mode (Latte accents `#8839ef`/light background-friendly on light, Mocha mauve on dark). Open a PR preview (Enter) and confirm the markdown preview matches the mode.

- [ ] **Step 4: Toggle the theme live and watch prdash follow**

With prdash still open, in another pane trigger the toggle (the Hyprland keybind, or run `theme-toggle.sh`). Within ~1s the prdash board — header, row accents, borders, and the preview — should flip to the other flavor without a restart. Toggle back and confirm it returns.

- [ ] **Step 5: Confirm standalone default**

Run `env XDG_STATE_HOME=/nonexistent ./prdash` in a repo. It should start (defaulting to Mocha/dark) with no error from the missing state file.
