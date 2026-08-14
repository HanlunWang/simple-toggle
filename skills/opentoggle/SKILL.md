---
name: opentoggle
description: Control OpenToggle (macOS menu bar switch manager) and author switch scripts. Use when the user asks to turn switches on/off, adjust switch parameters, or create/edit an OpenToggle switch script (keep awake, proxy, any script-backed toggle). Scripts must follow the OpenToggle contract and pass `opentoggle validate`.
---

# OpenToggle

OpenToggle renders user scripts as managed menu bar switches on macOS. You interact with it through the `opentoggle` CLI (the OpenToggle binary invoked with arguments; commonly aliased as `opentoggle`, or run via the built product at `.build/debug/OpenToggle` in the repo). The GUI app must be running for every command except `validate`.

An MCP server is also available (`opentoggle mcp`), exposing the same operations as tools; prefer the CLI when running in a shell.

## Operating switches

```bash
opentoggle list                      # all switches: state (on/off/error/unknown), type, enabled
opentoggle on keep-awake             # .sh suffix optional
opentoggle off keep-awake
opentoggle state keep-awake          # prints just the state
opentoggle params keep-awake         # parameter keys, current values, options
opentoggle set keep-awake mode=dis duration=120   # restarts a running switch with new values
opentoggle enable wifi               # show in menu bar
opentoggle disable wifi              # hide from the menu bar panel — visibility only:
                                     # a RUNNING switch keeps running (and keeps
                                     # restoring at launch); run `off` first to stop it
opentoggle cat keep-awake            # print script source
opentoggle rm my-switch              # delete (moves script to Trash)
```

Add `--json` to `list`/`params`/mutation commands for machine-readable output.

## Authoring switch scripts

A switch is one executable script carrying metadata as `# <switch.*>` directive comments in its first 40 lines.

### Workflow (always follow this)

1. Write the script to a temp file.
2. `opentoggle validate <file>` — fix every **error**; address warnings when reasonable.
3. Install: `opentoggle add <file>` (new switch; file name derives from `<switch.name>`) or `opentoggle put <id> <file>` (replace existing). Both lint again server-side and reject on errors.
4. Verify: `opentoggle list`, then `opentoggle on <id>` and `opentoggle state <id>`.

### Contract

```bash
#!/bin/bash
# <switch.name> My Switch                    # required: display name
# <switch.icon> ☕️                           # emoji or sf:<sf-symbol-name>
# <switch.type> toggle                       # toggle (default) | daemon
# <switch.param> key=mode type=select label=Mode default=a options="Label A=a|Label B=b"
# <switch.param> key=duration type=number label="Duration (min)" default=0 min=0 max=1440 presets="Unlimited=0|1 h=60"
# <switch.param> key=note type=text label=Note hint="placeholder text"
# <switch.param> key=nudge type=key label=Key default=f15
# <switch.menubar> mode=add icon=☕️ countdown=on
```

**toggle** (imperative, idempotent operations): invoked as `<script> on`, `<script> off`, `<script> status`. `status` must print `on` or `off` to stdout; it is polled every 5 s. Non-zero exit from `status` → state *unknown*; from `on`/`off` → *error*.

**daemon** (long-running): invoked as `<script> run`. Start the foreground process with `exec` so SIGTERM reaches it. Exit 0 = natural completion (switch resets); non-zero = *error*.

**Parameters** are injected as environment variables on every invocation: `key=mode` → `$SWITCH_MODE` (uppercase, non-alphanumerics become `_`). `options`/`presets` use `label=value|label=value`; quote attribute values containing spaces. `presets` renders quick-pick buttons for number/text params.

**Key params & sending keys**: `type=key` renders a key picker (capture / common keys / mouse buttons). The value is a key spec — `f15`, `cmd+shift+k`, `mouse:middle` (modifiers: cmd/shift/opt/ctrl/fn). Scripts send it with the built-in synthesizer instead of osascript:

```bash
"${OPENTOGGLE_BIN:-opentoggle}" press "$SWITCH_NUDGE"
```

`$OPENTOGGLE_BIN` (absolute binary path) is injected into every script's environment, so this works even when PATH is minimal. `press` requires a one-time Accessibility permission grant (System Settings → Privacy & Security → Accessibility); it exits non-zero when unauthorized — fail fast on that so the switch shows *error* instead of silently doing nothing.

**Menu bar**: `mode=add` shows a dedicated status item while on; `mode=replace` swaps the app icon. `countdown=on` requires a number param with `key=duration` (minutes, 0 = unlimited).

Scripts are `chmod 755` on install. Saving over a running switch restarts it with the new content.

### Conventions

- Prefer zero-permission mechanisms (`defaults`, `caffeinate`, `networksetup`, `osascript` volume). Note in a comment when a script triggers a permission prompt (e.g. System Events automation).
- Keep `status` fast (<1 s) and side-effect free. Keep stdout/stderr output small — it is not a log channel.
- **Daemon signal discipline** — the app stops a daemon with SIGTERM; pick one of two patterns:
  - *Single process*: `exec` the long-running command so the signal reaches it directly.
  - *Loop / multi-process*: trap and clean up explicitly. Run blocking waits as `sleep N & wait $!` (interruptible), and kill every helper you started in the trap:

    ```bash
    HELPER_PID=""
    cleanup() { [ -n "$HELPER_PID" ] && kill "$HELPER_PID" 2>/dev/null; pkill -P $$ 2>/dev/null; }
    trap 'cleanup; exit 0' TERM INT
    trap 'cleanup' EXIT
    caffeinate -d -w $$ & HELPER_PID=$!   # tie helpers to this process AND kill them in cleanup
    while :; do sleep "$INTERVAL" & wait $!; do_work; done
    ```
- **Never leave system-state helpers unowned.** Anything that holds system state while running (`caffeinate` sleep assertions, proxies enabled in `on`, servers) must be released on every exit path — tie process helpers to the script's lifetime (`caffeinate -w $$`) *and* kill them in the trap; belt and suspenders. The app additionally journals daemon PIDs and sweeps leftovers from a crashed previous instance at launch, but scripts should not rely on that.
- `opentoggle doctor` reports running daemons, flags orphans, and counts `caffeinate` sleep assertions — use it when diagnosing "the screen stays awake" or "the switch looks off but something is still running".
- Reference examples live in `Sources/OpenToggle/Switches/` of the repo.

## HTTP API (advanced)

The app serves a local control API on `127.0.0.1:43737` (`GET /v1/switches`, `POST /v1/switches/{id}/on`, `PUT /v1/switches/{id}/script`, `POST /v1/scripts`, `POST /v1/validate`, …). The CLI is a thin client over it; use the API directly only when the CLI is unavailable.
