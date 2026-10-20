# Architecture

SimpleToggle is one Go binary. Depending on how it is started it is the menu bar app, a command-line client, or an MCP server. The engine and everything around it live in `internal/` and know nothing about the UI toolkit; `app/` is the only package that imports [mygo](https://github.com/egoist/mygo).

```
               ┌──────────────────────── simpletoggle (one binary) ────────────────────────┐
 menu bar,     │  app/                  mygo UI: tray, panel, workbench, settings, onboarding │
 windows  ───► │      │ commands / events (posted back to the main thread)                   │
               │      ▼                                                                       │
               │  internal/engine  ◄── internal/automation ◄── internal/platform/darwin      │
               │   registry · state machine · runner · daemon supervision · polling · scenes │
               │      │                                                                       │
               │      ▼                                                                       │
               │  internal/contract   internal/store (JSON)   internal/keys                  │
               │                                                                              │
               │  internal/api  ◄──── HTTP 127.0.0.1:43737 (token) ───────────────────┐      │
               └───────▲──────────────────────────────────────────────────────────────┼──────┘
                       │                                                              │
   simpletoggle <command>  (CLI) ──── HTTP ───────────────────────────────────────────┘
   simpletoggle mcp        (MCP over stdio) ──── HTTP ──► same API
```

## Run modes

| Invocation | Mode | Notes |
|---|---|---|
| no arguments (Finder, login item) | GUI | single instance; a second launch hands its arguments to the first |
| `simpletoggle gui` | GUI | from a terminal the app is started with `open -b`, so it keeps its bundle identity |
| `simpletoggle <command>` | CLI | a plain HTTP client of the running app |
| `simpletoggle mcp` | MCP | stdio server that calls the same HTTP API |
| `simpletoggle validate <file>` | offline | contract lint, no running app needed |

## Packages

```
cmd/simpletoggle/          main: picks the mode and wires dependencies
internal/contract/         header parsing, lint, header rewriting (workbench inspector)
internal/keys/             key specs (f15, cmd+shift+k, mouse:middle)
internal/engine/           registry, state machine, runner, daemon supervision, PID ledger,
                           polling, scenes, pending AI changes, event bus
internal/runner/           process groups, timeouts, output capture
internal/automation/       triggers, rule evaluation, conditions, conflict policy, rate limits
internal/store/            state.json, runs.jsonl, usage statistics
internal/api/              HTTP routes (/v1 compatible, /v2), token, request sources, SSE
internal/cli/              CLI commands
internal/mcp/              MCP tools (go-sdk)
internal/integrations/     one-click MCP setup for Claude Code, Cursor and Codex
internal/presets/          bundled switches and the script library
internal/platform/darwin/  purego bindings: CGEvent, Accessibility, CoreWLAN, NSWorkspace, power
app/                       the UI
```

Dependencies only point down: `app` → `engine` / `automation` / `store` → `contract` / `keys` / `platform`. `internal/arch` has a test that fails if any `internal/` package imports mygo.

## Engine

**States.** `toggle` and `daemon`: off ⇄ pending ⇄ on, with error and unknown. `action`: idle → running → done or failed → idle.

- Each switch has its own serial queue, so on, off, run and status never overlap on one switch; different switches don't block each other.
- Polling uses a generation counter: every user action bumps it, and a poll that started before the action is discarded when it returns, so a slow `status` can never overwrite what you just did. Polling runs every 5 s while the panel is open and every 30 s in the background.

**Processes.** Every script runs in its own process group. Stopping sends SIGTERM to the group and SIGKILL after 3 s, which also cleans up anything the script spawned. Daemons are recorded in a PID ledger (`daemons.json`); at launch, leftovers from a crash are stopped after checking the process start time, so a reused PID is never killed by mistake. A small guard process (`simpletoggle __guard`) stops daemons right away if the app itself crashes. Toggle commands and actions time out after 30 s by default; daemons don't. Up to 64 KB of output is captured per run, and the last 8 KB is kept in the run history.

**Environment.** `SWITCH_<KEY>` for each parameter, `SIMPLETOGGLE_BIN`, `SIMPLETOGGLE_TRIGGER` (manual, shortcut, scene, automation, ai, cli), global variables from Settings, and Homebrew's `bin` directories on `PATH`.

**Events.** The engine exposes commands (`SetSwitch`, `Run`, `ActivateScene`, `SetParam`, …) and one event stream. The UI, the API's server-sent events, notifications and automations all consume that stream instead of calling each other.

## Scenes and automations

A scene runs its steps in order and records the state of each member before it starts; with *restore on second click* another click puts them back. A scene counts as active only while all its toggle and daemon members are still where it put them.

| Trigger | Source |
|---|---|
| Time | engine timer, woken just before the next match |
| Wi-Fi | network fingerprint (default route, gateway, DHCP server, subnet, search domains; no permission), or SSID through CoreWLAN (needs Location) |
| Power | IOKit notifications, with a 30 s fallback check |
| Apps | NSWorkspace launch and quit notifications |
| Displays, login / wake / unlock, files, shortcuts | system notifications, file watching, global shortcuts |

Rules can be limited to days and times. By default a switch you changed by hand is left alone by rules for 10 minutes. A rule fires at most once every 5 s, and a rule that flips the same switch back and forth more than 3 times in a minute is paused with a notification.

## Storage

- Scripts are plain files in `~/.config/simple-toggle/switches/` (configurable) and are reloaded when they change.
- App data lives in `~/Library/Application Support/SimpleToggle/`: `state.json` (settings, metadata, parameter values, desired states, scenes, automations, pending changes; written atomically, kept aside and rebuilt if corrupted), `runs.jsonl` (run history, trimmed to the retention period) and `usage.json` (daily usage, 400 days).
- `~/.config/simple-toggle/api.json` (mode 0600) holds the API token, which survives restarts, and while the app runs also its port and PID.

## API

The server listens on `127.0.0.1:43737` only. Every request except `ping` and `validate` needs `Authorization: Bearer <token>`, and requests with a foreign `Host` header are rejected. `/v1` covers switches and scripts; `/v2` adds actions, run history, scenes, automations, pending changes and `GET /v2/events` (server-sent events). Every request carries its source (UI, CLI, MCP, AI client), which drives AI permissions and shows up in Activity. Script changes from AI clients become pending changes that the user approves in the workbench.

## Releases and updates

`Scripts/release.sh` checks the signing identity, notarization profile and update key, runs the tests, builds a universal, signed and notarized app and disk image with `go tool mygo build`, and verifies the result. With `--upload` it creates a draft GitHub Release. The app checks GitHub Releases for updates and verifies each update with an Ed25519 signature before installing it.

## Tests

- `internal/contract`: golden tests over the bundled scripts.
- `internal/engine`: state machine, generation counter, timeouts, process-group cleanup, scenes, pending changes.
- `internal/api`, `internal/cli`: end-to-end tests against a real engine in a temporary home.
- `app/`: headless UI tests with mygo's `ui.Tester`. Set `OT_SNAPSHOT_DIR` to render screenshots of every screen with `go test -run TestSnapshots ./app/`.
- CI runs gofmt, `go vet`, `go test -race` and lints every bundled script on macOS.
