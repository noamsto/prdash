# Open PRs and issues on the controlling host from a mirrored session

## Problem

In a lazytmux bridge mirror, `prefix + p` does not run prdash locally: the
`tool` ctl verb launches it **on the remote**, in the remote pane's cwd
(lazytmux `generator/render/keys.go` `prdashBind`, `daemon/ctl.go` `"tool"`).
Every prdash open path — `o` on a PR or issue (single and bulk) and `o` on a
check in the log view — goes through `openURL` (`internal/ui/browser.go`),
which runs `open`/`xdg-open` on the machine prdash runs on. On a headless
remote that fails; on a remote with a display it opens a browser nobody is
looking at. The user is at the controller.

lazytmux already met this for the enrich card and dodged it by keeping the card
local. prdash cannot stay local: it has to run where the worktree is.

## Goal

`o` in prdash, run inside a mirrored session, opens the URL in the
controller's browser. Outside a mirror, behavior is unchanged.

Non-goals: opening files or non-URL targets; any reverse socket or forward
(the bridge is outbound-ssh only, by design).

## Design

The work splits across two repos. prdash learns one standard convention;
lazytmux provides a generic remote→controller opener any tool can use.

### 1. prdash — honor `$BROWSER`

`browserArgv` gains the `BROWSER` value as an input:

```go
func browserArgv(goos, browser string) []string
```

- `browser != ""` → `[]string{browser}`
- otherwise the current OS default (`open` on darwin, `xdg-open` elsewhere)

`openURL` passes `os.Getenv("BROWSER")`. It keeps its start-then-reap shape.
No call site changes: single, bulk, and check-log opens all use `openURL`.

`$BROWSER` is treated as a single executable name or path, not a
shell-split command line or a `:`-separated list. That matches how lazytmux
will set it (`og-open`). Richer `$BROWSER` forms are out of scope.

### 2. lazytmux — `og-open <url>`

A small POSIX shell script (shellcheck-clean), installed via `home.packages`
on every tmux-og host. Remotes are tmux-og hosts too, so it is present on both
sides.

1. Reject anything that is not `http://` or `https://` (exit non-zero, message
   on stderr).
2. **Bridged**: when `$TMUX` is set and a control-mode client is attached to
   the current session (`list-clients -t <session> -F '#{client_control_mode}'`
   contains `1`), set the session option
   `@og_open_url` to `<nonce><TAB><url>`. The nonce is unique per call
   (e.g. `date +%s%N` plus `$$`). Exit 0.
3. **Not bridged**: exec the local opener (`open` on darwin, `xdg-open`
   otherwise).

Because the unbridged path is the plain local opener, setting
`BROWSER=og-open` is safe everywhere. The tmux config adds
`set-environment -g BROWSER og-open` so panes started by the server,
including the bridged prdash float, inherit it. `gh … --web` and other
`$BROWSER`-aware tools benefit too.

### 3. lazytmux bridge daemon — `og_open` subscription

This follows the `og_labels` / `og_agents` pattern (`daemon/subscriptions.go`):

- Subscribe `refresh-client -B 'og_open::#{@og_open_url}'` on connect and on
  every reconnect repair. The empty `what` scopes it to the control client's
  attached session, so an option change reports once.
- On `%subscription-changed og_open …`:
  - Parse `<nonce>\t<url>`. Drop malformed values.
  - Hold the last-seen nonce. **The value seen at subscribe time (including
    after a reconnect) only seeds that nonce and is never opened**, so a
    reconnect never replays an old URL.
  - Re-check the scheme is `http(s)`. The remote is trusted the way other
    `@bridge_*` data is, but the controller should never hand a `file:` or
    custom scheme to its opener.
  - Start the controller's opener (`open` / `xdg-open`) detached and reap it.
    On a start error or non-zero exit, send the daemon's existing local
    `notify`.
- An `%error` from the subscribe (a remote tmux older than 3.2) disables the
  feature for the connection and notifies once. There is no polling fallback:
  opens are events, not state.

### Data flow

```
prdash (remote) --$BROWSER--> og-open (remote)
  --set-option @og_open_url--> remote tmux
  --%subscription-changed og_open--> bridge daemon (controller)
  --open/xdg-open--> controller browser
```

## Error handling

- prdash shows "Opened" once `$BROWSER` starts, matching today's contract. That
  means the URL was handed off, not that a browser appeared.
- Failures on the controller (no opener, opener exits non-zero, subscription
  unsupported) surface as a local `notify` from the daemon. The remote cannot
  display anything: its only client is the control client.
- `og-open` with a bad scheme fails locally, so prdash's "Open failed" badge
  shows.

## Testing

**prdash**
- Table test for `browserArgv`: BROWSER set/unset × darwin/linux. Replaces
  `TestBrowserArgv`.

**lazytmux**
- Unit tests:
  - value parsing (good, malformed, missing tab)
  - nonce dedup
  - the subscribe-time value is seeded, not opened
  - scheme rejection
- bats test for `og-open`: bad scheme rejected; with a fake control client
  attached, the option is stamped; without one, the local opener (stubbed on
  PATH) runs.
- Manual: from a mirrored session, `prefix + p`, `o` on a PR, and confirm the
  controller's browser opens.

## Rollout

1. lazytmux: `og-open`, the daemon subscription, and `BROWSER` in the tmux env.
2. prdash: the `$BROWSER` change. It is independently harmless: with BROWSER
   unset it is a no-op.

Either order works. The feature is live once both land and the remote's tmux
server has reloaded its config.
