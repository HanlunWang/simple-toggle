---
name: simpletoggle
description: Control SimpleToggle (macOS menu bar switch manager) and author switch scripts. Use when the user asks to turn switches on/off, run an action, activate a scene, adjust switch parameters, or create/edit a SimpleToggle switch script (keep awake, proxy, flush DNS, any script-backed toggle). Scripts must follow the SimpleToggle contract and pass `simpletoggle validate`.
---

# SimpleToggle

SimpleToggle turns user scripts into managed menu bar switches on macOS. Talk to it through the `simpletoggle` CLI (the app's own binary) or its MCP server (`simpletoggle mcp`, same operations as tools). Prefer the CLI in a shell. The app must be running for every command except `validate`, `help` and `version`.

SimpleToggle was called OpenToggle up to 0.5 (command `opentoggle`, same script contract without `action` and `lucide:` icons). This skill is for SimpleToggle 2 and later.

## Operating switches

```bash
simpletoggle list                      # all switches: state (on/off/error/unknown), type, enabled
simpletoggle on keep-awake             # .sh suffix optional; works on hidden switches too
simpletoggle off keep-awake
simpletoggle state keep-awake          # prints just the state
simpletoggle run flush-dns             # run an action switch, prints its output
simpletoggle params keep-awake         # parameter keys, current values, options
simpletoggle set keep-awake mode=dis duration=120   # restarts a running switch with new values
simpletoggle enable wifi               # show in the panel
simpletoggle disable wifi              # hide from the panel — visibility only: a RUNNING
                                     # switch keeps running; run `off` first to stop it
simpletoggle cat keep-awake            # print script source
simpletoggle log [id] [--failed]       # recent runs: source, duration, exit code, output
simpletoggle scenes                    # list scenes (● = active)
simpletoggle scene "Focus"             # activate a scene, or restore it when active
simpletoggle rm my-switch              # delete (moves script to Trash)
```

Add `--json` for machine-readable output.

### What you may do

The user decides in Settings → AI & Integrations what AI agents may do: read, flip switches / run actions / activate scenes, write scripts, delete scripts. By default deleting is **not** allowed and writing scripts **needs the user's approval**:

- `simpletoggle add` / `put` print `pending approval (chg-N): …` — nothing is written yet. Tell the user that SimpleToggle is showing the change for review; check the outcome later with `GET /v2/changes/chg-N` or the MCP tool `simpletoggle_change_status` (`pending`, `approved`, `rejected`, `failed`). Do not retry the write while it is pending.
- A `403` whose message says the settings do not allow it means exactly that: ask the user to allow it, do not look for a way around it (for example by editing files in the scripts folder directly).

Every call you make is recorded in the app's Activity with you as the source.

## Authoring switch scripts

A switch is one executable script carrying metadata as `# <switch.*>` directive comments in its first 40 lines.

### Workflow (always follow this)

1. Write the script to a temp file.
2. `simpletoggle validate <file>` — fix every **error**; address warnings when reasonable.
3. Install: `simpletoggle add <file>` (new switch; file name derives from `<switch.name>`) or `simpletoggle put <id> <file>` (replace an existing one). Both lint again and reject errors. This usually waits for the user's approval (see above).
4. Once installed, verify: `simpletoggle list`, then `simpletoggle on <id>` / `simpletoggle run <id>` and `simpletoggle state <id>`.

### Contract

```bash
#!/bin/bash
# <switch.name> My Switch                    # required: display name
# <switch.icon> lucide:coffee                # lucide:<name> , an emoji, or sf:<symbol> (mapped)
# <switch.type> toggle                       # toggle (default) | daemon | action
# <switch.description> One sentence          # optional, shown in the library and tooltips
# <switch.param> key=mode type=select label=Mode default=a options="Label A=a|Label B=b"
# <switch.param> key=duration type=number label="Duration (min)" default=0 min=0 max=1440 presets="Unlimited=0|1 h=60"
# <switch.param> key=note type=text label=Note hint="placeholder text"
# <switch.param> key=nudge type=key label=Key default=f15
# <switch.menubar> mode=add icon=lucide:coffee countdown=on
```

**toggle** (idempotent on / off): invoked as `<script> on`, `<script> off`, `<script> status`. `status` must print `on` or `off` to stdout; it is polled every 5 s (30 s in the background). Non-zero exit from `status` → *unknown*; from `on`/`off` → *error*.

**daemon** (long-running): invoked as `<script> run`. Start the foreground process with `exec` so SIGTERM reaches it. Exit 0 = natural completion (the switch resets); non-zero = *error*.

**action** (one shot): invoked as `<script>` with **no arguments** — do not write a `case "$1"` for on/off. Exit 0 = success, non-zero = failure; the last line of output is shown as the result. Use it for things that have no on/off state: flush DNS, copy the public IP, clear caches.

**Icons**: prefer `lucide:<name>` (any icon from lucide.dev, e.g. `lucide:wifi`, `lucide:timer`). Emoji work everywhere. `sf:<symbol>` is mapped to a Lucide icon; an unmapped one shows a generic icon with a lint warning.

**Parameters** are injected as environment variables on every invocation: `key=mode` → `$SWITCH_MODE` (uppercase, non-alphanumerics become `_`). `options`/`presets` use `label=value|label=value`; quote attribute values containing spaces. `presets` renders quick-pick buttons for number/text params. Changing a value restarts a running switch.

**Key params & sending keys**: `type=key` renders a key picker. The value is a key spec — `f15`, `cmd+shift+k`, `mouse:middle` (modifiers: cmd/shift/opt/ctrl/fn). Send it with the built-in synthesizer instead of osascript:

```bash
"${SIMPLETOGGLE_BIN:-simpletoggle}" press "$SWITCH_NUDGE"
```

`$SIMPLETOGGLE_BIN` (absolute binary path) is injected into every script, so this works with a minimal PATH. `press` needs a one-time Accessibility grant; it exits non-zero when unauthorized — fail fast so the switch shows *error* instead of silently doing nothing. `$SIMPLETOGGLE_TRIGGER` tells the script who started it (`manual`, `shortcut`, `scene`, `automation`, `ai`, `cli`, `restore`…).

**Menu bar**: `mode=add` shows a dedicated status item while on; `mode=replace` swaps the app icon. `countdown=on` requires a number param with `key=duration` (minutes, 0 = unlimited).

Scripts are `chmod 755` on install. Saving over a running switch restarts it with the new content. Homebrew's `bin` directories are added to `PATH`.

### Conventions

- Prefer zero-permission mechanisms (`defaults`, `caffeinate`, `networksetup`, `osascript` volume). Note in a comment when a script triggers a permission prompt (e.g. System Events automation).
- Keep `status` fast (<1 s) and side-effect free. Keep output small — it is shown in the panel and the run log, not a log channel.
- **Daemon signal discipline** — the app stops a daemon with SIGTERM to its whole process group (SIGKILL after 3 s). Pick one of two patterns:
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
- **Never leave system-state helpers unowned.** Anything that holds system state while running (`caffeinate` assertions, proxies enabled in `on`, servers) must be released on every exit path. The app journals daemon PIDs and sweeps leftovers of a crashed instance at launch, but scripts should not rely on that.
- `simpletoggle doctor` reports running daemons, flags orphans, and counts `caffeinate` sleep assertions — use it when diagnosing "the screen stays awake" or "the switch looks off but something is still running".
- Good references: the scripts in the app's Library (Workbench → Library) and the user's own scripts (`simpletoggle cat <id>`).

## Scenes and automations

Scenes are one-click groups of steps (`on` / `off` / `run`), created by the user in the workbench; you can list and activate them (`simpletoggle scenes`, `simpletoggle scene <name>`, MCP `simpletoggle_list_scenes` / `simpletoggle_activate_scene`). Automation rules (time, Wi-Fi, power, app, display, wake, file, shortcut → switch or scene) are read-only for agents (`GET /v2/automations`, MCP `simpletoggle_list_automations`); suggest rules to the user instead of trying to create them.

## HTTP API (advanced)

The app serves a local control API on `127.0.0.1:43737`. Writes need the token from `~/.config/simple-toggle/api.json` (the CLI and MCP read it automatically). `/v1` is unchanged from OpenToggle 0.5 (`GET /v1/switches`, `POST /v1/switches/{id}/on`, `PUT /v1/switches/{id}/script`, `POST /v1/scripts`, `POST /v1/validate`, …); 2.0 adds `/v2` (`POST /v2/switches/{id}/run`, `GET /v2/runs`, `GET /v2/events` (SSE), `/v2/scenes`, `/v2/automations`, `/v2/changes`). Use the API directly only when the CLI is unavailable.
