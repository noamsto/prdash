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
controller's browser. That includes a bulk `o` over several selected rows.
Outside a mirror, behavior is unchanged.

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

A small POSIX shell script, installed on PATH on every tmux-og host. Remotes
are tmux-og hosts too, so it is present on both sides.

1. Reject anything that is not `http://` or `https://`, or that contains
   whitespace (exit 2, message on stderr). Whitespace is the record separator
   below; a well-formed URL never contains it.
2. **Bridged**: when `$TMUX` and `$TMUX_PANE` are set and a control-mode client
   is attached to the pane's session
   (`list-clients -t "$TMUX_PANE" -F '#{client_control_mode}'` has a `1`),
   **append** a record ` <nonce>|<url>` to the session option `@og_open_url`
   with `set-option -a`. The nonce is `<epoch-seconds>-<pid>`, which is unique
   across concurrent calls. Exit 0.
   - The option is an append-only log, not a single value, because tmux
     reports subscription changes on a timer (about once a second). A bulk
     open overwrites a single value several times inside one tick and only
     the last URL would reach the controller. `-a` is atomic inside the tmux
     server, so concurrent `og-open` calls do not lose each other's records.
   - Cap: when the current value is already over 4096 bytes, `og-open`
     replaces it with just the new record instead of appending. Every old
     record was opened long ago. The only loss is a record appended by a
     concurrent call during that one reset, which is acceptable.
3. **Not bridged**: `unset BROWSER`, then exec the local opener (`open` on
   darwin, `xdg-open` otherwise). The unset matters: `xdg-open` itself falls
   back to `$BROWSER`, which would call `og-open` again forever.

Because the unbridged path is the plain local opener, setting
`BROWSER=og-open` is safe everywhere. The tmux config adds
`set-environment -g BROWSER og-open` so panes started by the server,
including the bridged prdash float, inherit it. `gh … --web` and other
`$BROWSER`-aware tools benefit too.

### 3. lazytmux bridge daemon — `og_open` subscription

A new `urlOpener`, built once per daemon run so its state survives
reconnects, following the `og_labels` / `og_agents` pattern
(`daemon/subscriptions.go`):

- **Subscribe** on connect and on every reconnect repair, in this order:
  1. Read the current value (`show-options -qv @og_open_url`) and mark every
     nonce in it as seen.
  2. Subscribe with `refresh-client -B 'og_open::#{@og_open_url}'`. The empty
     `what` scopes it to the control client's attached session.

  Reading first is what makes both edge cases correct. tmux re-reports the
  current value right after a subscribe, and those nonces are already seen,
  so a reconnect never replays old URLs. A record appended between the read
  and the subscribe is not seen yet, so it still opens.
- **On `%subscription-changed og_open …`**, parse the value into records
  (split on whitespace, then on the first `|`). Open each record whose nonce
  is not seen, then set seen to exactly the nonces in this value. That keeps
  seen bounded by `og-open`'s own cap. Malformed records and non-`http(s)`
  URLs are skipped: the remote is trusted the way other `@bridge_*` data is,
  but the controller never hands a `file:` or custom scheme to its opener.
- **Opening** runs off the main loop (a goroutine), through an injected
  `Config.OpenURL func(url string) error`. Production runs `open` / `xdg-open`
  and waits for it. An error goes to the daemon's existing `notifyLocal`.
  `OpenURL == nil` turns the feature off, which is the convention for the
  other injected funcs.
- If the seed read or the subscribe fails (a remote tmux older than 3.2),
  the daemon notifies once for that connection. There is no polling fallback:
  opens are events, not state.

### Data flow

```
prdash (remote) --$BROWSER--> og-open (remote)
  --set-option -a @og_open_url--> remote tmux
  --%subscription-changed og_open--> bridge daemon (controller)
  --open/xdg-open--> controller browser
```

## Error handling

- prdash shows "Opened" once `$BROWSER` starts, matching today's contract. That
  means the URL was handed off, not that a browser appeared.
- Failures on the controller (no opener, opener exits non-zero, subscription
  unsupported) surface as a local `notifyLocal` from the daemon. The remote
  cannot display anything: its only client is the control client.
- `og-open` with a bad scheme or whitespace fails locally, so prdash's
  "Open failed" badge shows.
- Latency: up to about one second, the tmux subscription timer.

## Testing

**prdash**
- Table test for `browserArgv`: BROWSER set/unset × darwin/linux. Replaces
  `TestBrowserArgv`.

**lazytmux**
- Go unit tests for `urlOpener`:
  - record parsing (several records, malformed, bad scheme, `|` inside a URL)
  - only unseen nonces open
  - seed-then-subscribe order, and nothing opens from the replayed value
  - a failed seed read does not subscribe
  - a subscription value with several records survives `controlmode.ParseLine`
- bats test for `og-open`, with `tmux` and the openers stubbed on PATH:
  - bad scheme and whitespace rejected
  - bridged: appends a record
  - bridged over the cap: replaces
  - not bridged: runs the opener with `BROWSER` unset
- Manual: from a mirrored session, `prefix + p`, `o` on a PR, then a bulk `o`
  over three rows. The controller opens each one.

## Rollout

1. lazytmux: `og-open`, the daemon subscription, and `BROWSER` in the tmux env.
2. prdash: the `$BROWSER` change. It is independently harmless: with BROWSER
   unset it is a no-op.

Either order works. The feature is live once both land, the remote is rebuilt,
and the remote's tmux server has reloaded its config.
