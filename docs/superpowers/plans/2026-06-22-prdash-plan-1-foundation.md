# prdash Plan 1 — Foundation + PR list

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A runnable, lean PR dashboard for the current repo that paints rows instantly from an on-disk cache and refreshes them in the background.

**Architecture:** Pure-Go core (build `gh` argv, parse `gh --json` output, persist a stale-while-revalidate cache) under a thin bubbletea v1 UI. No GraphQL client — data comes from shelling out to `gh`. This plan delivers the read path; actions/issues/preview are Plans 2–4.

**Tech Stack:** Go 1.23, `github.com/charmbracelet/bubbletea` + `bubbles/table` + `lipgloss` (stable v1), stdlib `log/slog`, `os/exec`, `encoding/json`. Spec: `2026-06-22-prdash-design.md`.

---

## Conventions

- **Isolation:** Per the worktree rule, this is a brand-new repo; create it, then do all work on a branch (`wt switch -c feat/foundation` once the repo exists).
- **Build/test:** the repo is bootstrapped with a Nix flake + direnv devshell (Task 0), so `go` is on PATH inside the repo. Run Go as plain `go test ./...` / `go build ./...` from the repo root (direnv provides the toolchain). The module is pure-Go; no CGO.
- Module path: `github.com/noamsto/prdash`.

## File structure

- `flake.nix`, `.envrc`, `go.mod` — repo scaffold (Task 0)
- `internal/gh/runner.go` — `Runner` interface + `ExecRunner` (the only thing that touches the OS)
- `internal/gh/prs.go` — PR struct, arg builder, parser, CI-state derivation
- `internal/cache/cache.go` — persisted results cache (ported, hardened)
- `internal/ui/prlist.go` — bubbletea model: table of PRs, hydrate + refresh
- `internal/ui/messages.go` — tea messages (fetched / fetch-failed)
- `main.go` — wire repo detection, cache, model, program

---

## Task 0: Bootstrap the repo

**Files:** `flake.nix`, `.envrc`, `.gitignore`, `go.mod`, `main.go`

- [ ] **Step 1: Create the GitHub repo + local clone**

```bash
gh repo create noamsto/prdash --private --clone
cd prdash
```

- [ ] **Step 2: Scaffold Nix flake + devshell**

Invoke the `bootstrap-nix-repo` skill for a Go project (it writes `flake.nix`, `.envrc`, `.gitignore`, treefmt, git-hooks per Noam's conventions). Then:

```bash
direnv allow
go mod init github.com/noamsto/prdash
```

- [ ] **Step 3: Minimal main so it builds**

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("prdash")
}
```

- [ ] **Step 4: Verify build + commit**

Run: `go build ./... && ./prdash`
Expected: prints `prdash`.

```bash
git add -A
git commit -m "chore: bootstrap prdash (flake, devshell, module)"
```

---

## Task 1: `gh` runner + PR fetch/parse

**Files:**
- Create: `internal/gh/runner.go`, `internal/gh/prs.go`
- Test: `internal/gh/prs_test.go`

- [ ] **Step 1: Write the runner interface**

Create `internal/gh/runner.go`:

```go
package gh

import "os/exec"

// Runner runs `gh` with args in dir and returns stdout. The single OS seam,
// so the rest of the package is unit-testable with a fake.
type Runner interface {
	Run(dir string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	return cmd.Output()
}
```

- [ ] **Step 2: Write the failing test for arg-building + parsing**

Create `internal/gh/prs_test.go`:

```go
package gh

import "testing"

type fakeRunner struct {
	gotArgs []string
	out     []byte
}

func (f *fakeRunner) Run(_ string, args ...string) ([]byte, error) {
	f.gotArgs = args
	return f.out, nil
}

func TestPRListArgs(t *testing.T) {
	args := PRListArgs("is:open author:@me", 20)
	want := []string{
		"pr", "list", "--search", "is:open author:@me",
		"-L", "20", "--json",
		"number,title,author,statusCheckRollup,reviewDecision,labels,assignees,headRefName,baseRefName,url,updatedAt",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestFetchPRsParses(t *testing.T) {
	f := &fakeRunner{out: []byte(`[
		{"number":7,"title":"hi","author":{"login":"noam"},
		 "statusCheckRollup":[{"state":"SUCCESS"}],"headRefName":"feat/x"}
	]`)}
	prs, err := FetchPRs(f, "/repo", "is:open", 20)
	if err != nil {
		t.Fatalf("FetchPRs: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 7 || prs[0].Author.Login != "noam" {
		t.Fatalf("parsed = %+v", prs)
	}
}

func TestCIState(t *testing.T) {
	cases := []struct {
		name  string
		rollup []Check
		want  string
	}{
		{"empty", nil, "none"},
		{"all pass", []Check{{State: "SUCCESS"}, {State: "SUCCESS"}}, "pass"},
		{"one fail", []Check{{State: "SUCCESS"}, {State: "FAILURE"}}, "fail"},
		{"pending", []Check{{State: "SUCCESS"}, {State: "PENDING"}}, "pending"},
	}
	for _, c := range cases {
		if got := (PR{StatusCheckRollup: c.rollup}).CIState(); got != c.want {
			t.Errorf("%s: CIState = %q, want %q", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run the test, verify it fails to compile**

Run: `go test ./internal/gh/`
Expected: FAIL — `undefined: PRListArgs`, `FetchPRs`, `PR`, `Check`.

- [ ] **Step 4: Implement `internal/gh/prs.go`**

```go
package gh

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

var prFields = []string{
	"number", "title", "author", "statusCheckRollup", "reviewDecision",
	"labels", "assignees", "headRefName", "baseRefName", "url", "updatedAt",
}

type Check struct {
	State      string `json:"state"`      // CheckRun: SUCCESS/FAILURE/...
	Conclusion string `json:"conclusion"` // some rollup entries use conclusion
}

type Label struct {
	Name string `json:"name"`
}

type PR struct {
	Number            int       `json:"number"`
	Title             string    `json:"title"`
	Author            struct{ Login string `json:"login"` } `json:"author"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup []Check   `json:"statusCheckRollup"`
	Labels            []Label   `json:"labels"`
	Assignees         []struct{ Login string `json:"login"` } `json:"assignees"`
	HeadRefName       string    `json:"headRefName"`
	BaseRefName       string    `json:"baseRefName"`
	URL               string    `json:"url"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// PRListArgs builds the `gh pr list --search` invocation. --search keeps the
// full field set (incl. statusCheckRollup, verified populated under search).
func PRListArgs(filter string, limit int) []string {
	return []string{
		"pr", "list", "--search", filter,
		"-L", strconv.Itoa(limit), "--json", strings.Join(prFields, ","),
	}
}

func FetchPRs(r Runner, dir, filter string, limit int) ([]PR, error) {
	out, err := r.Run(dir, PRListArgs(filter, limit)...)
	if err != nil {
		return nil, err
	}
	return ParsePRs(out)
}

func ParsePRs(b []byte) ([]PR, error) {
	var prs []PR
	if err := json.Unmarshal(b, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// CIState collapses the check rollup into pass/fail/pending/none.
func (p PR) CIState() string {
	if len(p.StatusCheckRollup) == 0 {
		return "none"
	}
	pending, failed := false, false
	for _, c := range p.StatusCheckRollup {
		s := c.State
		if s == "" {
			s = c.Conclusion
		}
		switch s {
		case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
			failed = true
		case "PENDING", "QUEUED", "IN_PROGRESS", "":
			pending = true
		}
	}
	switch {
	case failed:
		return "fail"
	case pending:
		return "pending"
	default:
		return "pass"
	}
}
```

- [ ] **Step 5: Run tests, verify pass**

Run: `go test ./internal/gh/ -v`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/gh/
git commit -m "feat(gh): PR fetch via gh --json + CI-state derivation"
```

---

## Task 2: Results cache (ported + hardened)

**Files:**
- Create: `internal/cache/cache.go`
- Test: `internal/cache/cache_test.go`

Ports `feat/results-cache`'s store, swapping `charm.land/log` for `log/slog`, and adding the two locked fixes: full-lock save (rename-clobber) and a `schemaVer` segment in the key.

- [ ] **Step 1: Write the failing tests**

Create `internal/cache/cache_test.go`:

```go
package cache

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	return &Cache{
		entries:  map[string]Entry{},
		filePath: filepath.Join(t.TempDir(), "results-cache.json"),
	}
}

func TestRoundTrip(t *testing.T) {
	c := newTestCache(t)
	rows := json.RawMessage(`[{"number":1}]`)
	c.Set("pr:is:open\x0020\x00v1", rows)
	got, ok := c.Get("pr:is:open\x0020\x00v1")
	if !ok || string(got.Rows) != string(rows) {
		t.Fatalf("got=%q ok=%v", got.Rows, ok)
	}
}

func TestPersistsAcrossLoad(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", json.RawMessage(`[]`))
	reloaded := &Cache{entries: map[string]Entry{}, filePath: c.filePath}
	if err := reloaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := reloaded.Get("k"); !ok {
		t.Fatal("expected hit after reload")
	}
}

func TestPrunesOld(t *testing.T) {
	c := newTestCache(t)
	c.entries["fresh"] = Entry{SavedAt: time.Now()}
	c.entries["stale"] = Entry{SavedAt: time.Now().Add(-maxAge - time.Hour)}
	c.prune()
	if _, ok := c.entries["stale"]; ok {
		t.Error("stale should be pruned")
	}
	if _, ok := c.entries["fresh"]; !ok {
		t.Error("fresh should survive")
	}
}

func TestKey(t *testing.T) {
	k := Key("pr", "is:open author:@me", 20, "abc123")
	want := "pr:is:open author:@me\x0020\x00abc123"
	if k != want {
		t.Errorf("Key = %q, want %q", k, want)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/cache/`
Expected: FAIL — undefined `Cache`, `Entry`, `Key`, `maxAge`.

- [ ] **Step 3: Implement `internal/cache/cache.go`**

```go
package cache

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxAge = 7 * 24 * time.Hour

type Entry struct {
	Rows    json.RawMessage `json:"rows"`
	SavedAt time.Time       `json:"savedAt"`
}

type Cache struct {
	mu       sync.Mutex
	entries  map[string]Entry
	filePath string
}

// Key composes a cache key. schemaVer (a hash of the requested --json field set)
// makes a changed field set a clean miss rather than a corrupt hydrate.
func Key(kind, filter string, limit int, schemaVer string) string {
	return fmt.Sprintf("%s:%s\x00%d\x00%s", kind, filter, limit, schemaVer)
}

func Open(filePath string) *Cache {
	c := &Cache{entries: map[string]Entry{}, filePath: filePath}
	if err := c.load(); err != nil {
		slog.Debug("cache load failed, starting empty", "err", err)
	}
	return c
}

func (c *Cache) Get(key string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

func (c *Cache) Set(key string, rows json.RawMessage) {
	c.mu.Lock()
	c.entries[key] = Entry{Rows: rows, SavedAt: time.Now()}
	c.mu.Unlock()
	if err := c.save(); err != nil {
		slog.Debug("cache save failed", "err", err)
	}
}

func (c *Cache) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var entries map[string]Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return err
	}
	c.entries = entries
	c.prune()
	return nil
}

// prune drops entries older than maxAge. Caller holds the lock.
func (c *Cache) prune() {
	cutoff := time.Now().Add(-maxAge)
	for k, e := range c.entries {
		if e.SavedAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}

// save holds the full lock across marshal+rename so concurrent saves can't
// clobber each other via rename ordering (locked fix from feat/results-cache).
func (c *Cache) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, c.filePath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/cache/ -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cache/
git commit -m "feat(cache): persisted stale-while-revalidate store (schemaVer + locked save)"
```

---

## Task 3: PR list UI model (hydrate + render)

**Files:**
- Create: `internal/ui/messages.go`, `internal/ui/prlist.go`
- Test: `internal/ui/prlist_test.go`

- [ ] **Step 1: Messages**

Create `internal/ui/messages.go`:

```go
package ui

import "github.com/noamsto/prdash/internal/gh"

type prsFetchedMsg struct {
	prs []gh.PR
	raw []byte
}

type fetchFailedMsg struct{ err error }
```

- [ ] **Step 2: Write the failing test (hydrate paints rows)**

Create `internal/ui/prlist_test.go`:

```go
package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestSetPRsBuildsRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 7, Title: "hello", HeadRefName: "feat/x"},
		{Number: 9, Title: "world", HeadRefName: "fix/y"},
	})
	if got := len(m.table.Rows()); got != 2 {
		t.Fatalf("table rows = %d, want 2", got)
	}
	if m.table.Rows()[0][0] != "#7" {
		t.Errorf("first row number cell = %q, want #7", m.table.Rows()[0][0])
	}
}
```

- [ ] **Step 3: Run, verify fail**

Run: `go test ./internal/ui/`
Expected: FAIL — undefined `NewModel`, `setPRs`.

- [ ] **Step 4: Implement `internal/ui/prlist.go`**

```go
package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type Model struct {
	dir    string
	filter string
	cache  *cache.Cache
	table  table.Model
	prs    []gh.PR
	err    error
}

func NewModel(dir, filter string, c *cache.Cache) Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "#", Width: 6},
			{Title: "Title", Width: 50},
			{Title: "Author", Width: 14},
			{Title: "CI", Width: 8},
		}),
		table.WithFocused(true),
	)
	return Model{dir: dir, filter: filter, cache: c, table: t}
}

func (m *Model) setPRs(prs []gh.PR) {
	m.prs = prs
	rows := make([]table.Row, 0, len(prs))
	for _, p := range prs {
		rows = append(rows, table.Row{
			fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login, p.CIState(),
		})
	}
	m.table.SetRows(rows)
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case prsFetchedMsg:
		m.setPRs(msg.prs)
		if m.cache != nil && msg.raw != nil {
			m.cache.Set(cache.Key("pr", m.filter, 20, schemaVer), msg.raw)
		}
		return m, nil
	case fetchFailedMsg:
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if len(m.prs) == 0 && m.err == nil {
		return "Loading PRs…  (q to quit)"
	}
	if m.err != nil && len(m.prs) == 0 {
		return "Error: " + m.err.Error() + "  (q to quit)"
	}
	return m.table.View() + "\n(q to quit)"
}

// schemaVer is bumped whenever the requested gh --json field set changes.
const schemaVer = "v2" // v2: added assignees (for fuzzy filter)
```

- [ ] **Step 5: Run, verify pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): PR list table model with setPRs row building"
```

---

## Task 4: Hydrate-from-cache + background fetch

**Files:**
- Modify: `internal/ui/prlist.go`
- Test: `internal/ui/prlist_test.go`

- [ ] **Step 1: Failing test — hydrate from cache on Init**

Add to `internal/ui/prlist_test.go`:

```go
import (
	"encoding/json"
	"path/filepath"

	"github.com/noamsto/prdash/internal/cache"
)

func TestHydrateFromCache(t *testing.T) {
	c := &cache.Cache{} // empty; use Open against a temp file instead
	c = cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw, _ := json.Marshal([]gh.PR{{Number: 42, Title: "cached"}})
	c.Set(cache.Key("pr", "is:open", 20, schemaVer), raw) // schemaVer keeps test in sync with the const

	m := NewModel("/repo", "is:open", c)
	m.hydrate()
	if len(m.prs) != 1 || m.prs[0].Number != 42 {
		t.Fatalf("hydrate did not paint cached rows: %+v", m.prs)
	}
	if len(m.table.Rows()) != 1 {
		t.Fatal("table not painted from cache")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/ui/ -run TestHydrateFromCache`
Expected: FAIL — `m.hydrate undefined`.

- [ ] **Step 3: Implement hydrate + fetch command**

Add to `internal/ui/prlist.go`:

```go
import (
	"encoding/json"

	"github.com/noamsto/prdash/internal/gh"
)

// hydrate paints rows from the cache immediately (instant launch). No-op on miss.
func (m *Model) hydrate() {
	if m.cache == nil {
		return
	}
	e, ok := m.cache.Get(cache.Key("pr", m.filter, 20, schemaVer))
	if !ok {
		return
	}
	var prs []gh.PR
	if err := json.Unmarshal(e.Rows, &prs); err != nil {
		return
	}
	m.setPRs(prs)
}

// fetchCmd runs the live gh fetch off the UI thread.
func (m Model) fetchCmd(r gh.Runner) tea.Cmd {
	dir, filter := m.dir, m.filter
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.PRListArgs(filter, 20)...)
		if err != nil {
			return fetchFailedMsg{err}
		}
		prs, err := gh.ParsePRs(raw)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return prsFetchedMsg{prs: prs, raw: raw}
	}
}
```

Change `Init` to hydrate + kick the fetch (the runner is injected in Task 5; for now expose a setter):

```go
func (m *Model) SetRunner(r gh.Runner) { m.runner = r }
```

Add `runner gh.Runner` to the struct, and:

```go
func (m Model) Init() tea.Cmd {
	m.hydrate()
	return m.fetchCmd(m.runner)
}
```

(Note: `Init` has a value receiver; hydrate mutates — so hydrate is also called once in `main` before `tea.NewProgram`. Keep `Init` returning only the fetch command; `main` calls `m.hydrate()` on the pointer before starting the program — see Task 5.)

Revise `Init`:

```go
func (m Model) Init() tea.Cmd { return m.fetchCmd(m.runner) }
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (all UI tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): hydrate from cache + background gh fetch"
```

---

## Task 5: Wire main — repo detection + launch

**Files:**
- Modify: `main.go`
- Test: `internal/gh/repo_test.go`, `internal/gh/repo.go`

- [ ] **Step 1: Failing test — current-repo detection**

Create `internal/gh/repo.go` + `internal/gh/repo_test.go`:

```go
// repo_test.go
package gh

import "testing"

func TestParseRepoFromView(t *testing.T) {
	got, err := parseRepo([]byte(`{"nameWithOwner":"noamsto/prdash"}`))
	if err != nil || got != "noamsto/prdash" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestParseRepoEmpty(t *testing.T) {
	if _, err := parseRepo([]byte(`{}`)); err == nil {
		t.Fatal("expected error on empty repo")
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/gh/ -run TestParseRepo`
Expected: FAIL — `parseRepo` undefined.

- [ ] **Step 3: Implement repo detection**

Create `internal/gh/repo.go`:

```go
package gh

import (
	"encoding/json"
	"errors"
)

var ErrNoRepo = errors.New("not in a GitHub repo")

// CurrentRepo resolves owner/name for dir, or ErrNoRepo.
func CurrentRepo(r Runner, dir string) (string, error) {
	out, err := r.Run(dir, "repo", "view", "--json", "nameWithOwner")
	if err != nil {
		return "", ErrNoRepo
	}
	return parseRepo(out)
}

func parseRepo(b []byte) (string, error) {
	var v struct {
		NameWithOwner string `json:"nameWithOwner"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	if v.NameWithOwner == "" {
		return "", ErrNoRepo
	}
	return v.NameWithOwner, nil
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/gh/ -run TestParseRepo -v`
Expected: PASS.

- [ ] **Step 5: Wire `main.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/ui"
)

func main() {
	dir, _ := os.Getwd()
	runner := gh.ExecRunner{}

	if _, err := gh.CurrentRepo(runner, dir); err != nil {
		fmt.Fprintln(os.Stderr, "prdash: not in a GitHub repo")
		os.Exit(1)
	}

	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	c := cache.Open(filepath.Join(stateDir, "prdash", "results-cache.json"))

	m := ui.NewModel(dir, "is:open author:@me", c)
	m.SetRunner(runner)
	m.Hydrate() // paint cached rows before the program starts

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Export `Hydrate` (rename `hydrate`→`Hydrate`, update the test call) so `main` can paint before `Run()`.

- [ ] **Step 6: Build + manual smoke test**

Run from inside a GitHub repo checkout:

```bash
go build -o prdash . && ./prdash
```

Expected: PR table for `is:open author:@me`; second launch paints instantly from cache, then refreshes. Outside a repo: prints "not in a GitHub repo" and exits 1.

Verify cache file:

```bash
test -f "${XDG_STATE_HOME:-$HOME/.local/state}/prdash/results-cache.json" && echo OK
```

- [ ] **Step 7: Commit**

```bash
git add main.go internal/gh/repo.go internal/gh/repo_test.go internal/ui/prlist.go
git commit -m "feat: wire prdash launch with repo detection + cache hydration"
```

---

## Task 6: Client-side fuzzy filter (all fields)

**Files:**
- Create: `internal/ui/filter.go`, `internal/ui/filter_test.go`
- Modify: `internal/ui/prlist.go`

- [ ] **Step 1: Add the fuzzy dependency**

Run: `go get github.com/sahilm/fuzzy`

- [ ] **Step 2: Failing test for the pure filter**

Create `internal/ui/filter_test.go`:

```go
package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func sample() []gh.PR {
	a := gh.PR{Number: 7, Title: "add cache", HeadRefName: "feat/cache"}
	a.Author.Login = "noam"
	b := gh.PR{Number: 12, Title: "fix render", HeadRefName: "fix/render"}
	b.Author.Login = "dlvhdr"
	b.Assignees = []struct{ Login string `json:"login"` }{{Login: "noam"}}
	return []gh.PR{a, b}
}

func TestFilterEmptyReturnsAll(t *testing.T) {
	if got := filterPRs(sample(), ""); len(got) != 2 {
		t.Fatalf("empty query = %d rows, want 2", len(got))
	}
}

func TestFilterByNumber(t *testing.T) {
	got := filterPRs(sample(), "12")
	if len(got) == 0 || got[0].Number != 12 {
		t.Fatalf("query '12' = %+v", got)
	}
}

func TestFilterByAssignee(t *testing.T) {
	// both have author/assignee noam; "noam" should match both, not zero.
	if got := filterPRs(sample(), "noam"); len(got) != 2 {
		t.Fatalf("query 'noam' = %d, want 2", len(got))
	}
}

func TestFilterByBranch(t *testing.T) {
	got := filterPRs(sample(), "render")
	if len(got) != 1 || got[0].Number != 12 {
		t.Fatalf("query 'render' = %+v", got)
	}
}
```

- [ ] **Step 3: Run, verify fail**

Run: `go test ./internal/ui/ -run TestFilter`
Expected: FAIL — `filterPRs` undefined.

- [ ] **Step 4: Implement `internal/ui/filter.go`**

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/noamsto/prdash/internal/gh"
)

// haystack is the all-fields searchable string for one PR.
func haystack(p gh.PR) string {
	parts := []string{
		fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login,
		p.HeadRefName, p.BaseRefName, p.ReviewDecision, p.CIState(),
	}
	for _, a := range p.Assignees {
		parts = append(parts, a.Login)
	}
	for _, l := range p.Labels {
		parts = append(parts, l.Name)
	}
	return strings.Join(parts, " ")
}

// filterPRs fuzzy-matches query across all fields, ranked by score. Empty query
// returns the input unchanged (original order).
func filterPRs(prs []gh.PR, query string) []gh.PR {
	if strings.TrimSpace(query) == "" {
		return prs
	}
	hay := make([]string, len(prs))
	for i, p := range prs {
		hay[i] = haystack(p)
	}
	matches := fuzzy.Find(query, hay)
	out := make([]gh.PR, 0, len(matches))
	for _, m := range matches {
		out = append(out, prs[m.Index])
	}
	return out
}
```

- [ ] **Step 5: Run, verify pass**

Run: `go test ./internal/ui/ -run TestFilter -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Wire `/` filter mode into the model**

In `internal/ui/prlist.go`: add `github.com/charmbracelet/bubbles/textinput` import; add fields `filtering bool` and `filterInput textinput.Model`; init `filterInput` in `NewModel` (`ti := textinput.New(); ti.Prompt = "/"`). Split row-building from `setPRs`:

```go
// setPRs stores the full set, then re-applies the current filter to the table.
func (m *Model) setPRs(prs []gh.PR) {
	m.prs = prs
	m.applyFilter()
}

// applyFilter rebuilds table rows from prs filtered by the current query.
func (m *Model) applyFilter() {
	shown := filterPRs(m.prs, m.filterInput.Value())
	rows := make([]table.Row, 0, len(shown))
	for _, p := range shown {
		rows = append(rows, table.Row{
			fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login, p.CIState(),
		})
	}
	m.table.SetRows(rows)
}
```

In `Update`, handle filter mode in the `tea.KeyMsg` branch:

```go
	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.applyFilter()
				return m, nil
			case "enter":
				m.filtering = false
				m.filterInput.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.applyFilter()
			return m, cmd
		}
		switch msg.String() {
		case "/":
			m.filtering = true
			return m, m.filterInput.Focus()
		case "q", "ctrl+c":
			return m, tea.Quit
		}
```

In `View`, show the filter line when filtering:

```go
	if m.filtering {
		return m.filterInput.View() + "\n" + m.table.View()
	}
```

- [ ] **Step 7: Build + manual check**

Run: `go build -o prdash . && ./prdash`
Expected: `/` opens the filter; typing `noam`, a number, or a branch fragment narrows the list live; `esc` clears.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/ go.mod go.sum
git commit -m "feat(ui): client-side fuzzy filter across all PR fields"
```

---

## Self-review (done)

- **Spec coverage (Plan 1 slice):** gh-CLI fetch ✓ (T1), cache w/ schemaVer + locked save ✓ (T2), cwd-scoped PR list ✓ (T1/T5), CI rollup column ✓ (T1/T3), instant-launch + bg refresh ✓ (T4), no-repo handling ✓ (T5), client-side all-fields fuzzy filter ✓ (T6, `assignees` field + schemaVer v2). Deferred to later plans: filters beyond `author:@me`, actions, issues, preview, tmux packaging, optional server-search fallback on zero fuzzy matches.
- **Type consistency:** `gh.PR` (incl. `Assignees`), `cache.Key/Open/Get/Set`, `ui.NewModel/SetRunner/Hydrate/setPRs/applyFilter/filterPRs`, `schemaVer="v2"` consistent across tasks.
- **Placeholders:** none — every code step is complete.

## Next
Plan 2 (actions + action view + tmux handoff/orchestrator) once this runs.
