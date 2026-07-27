# OpenToggle

**Turn any script into a managed menu bar switch on macOS.**

English | [简体中文](README.zh-CN.md)

OpenToggle renders each of your automation scripts as a first-class switch in the macOS menu bar — a name, a status light, and a toggle. Turning a switch on starts the automation; turning it off stops it; state survives restarts. Write the automation in any language, declare a few metadata directives, and OpenToggle handles the process lifecycle, status polling, parameter UI, and persistence.

## Why

Everyday macOS automations — keep the display awake, jiggle a key to stay "active", flip a proxy, show hidden files — each traditionally require a separate single-purpose utility crowding the menu bar. Script-to-menu-bar tools exist (SwiftBar, xbar, Hammerspoon), but their model is *script → printed output → display*. None of them offer a first-class **switch abstraction**: managed on/off semantics, process lifecycle, and state persistence have to be hand-rolled per plugin.

OpenToggle's model is *script → managed switch*. The contract is the product.

|  | SwiftBar / xbar | Hammerspoon | **OpenToggle** |
|---|---|---|---|
| Mental model | print output → display | Lua automation framework | script → managed switch |
| On/off semantics | hand-rolled per plugin | hand-rolled in Lua | built-in |
| Process lifecycle | manual (pid files, pkill) | manual | spawn / SIGTERM / orphan cleanup |
| State persistence | manual | manual | automatic, restored on launch |
| Parameter UI | none | DIY | declarative (dropdown / number / presets / text) |

## Features

- **Two switch forms** — imperative `toggle` (on/off/status commands) and long-running `daemon` (spawn & hold, SIGTERM to stop)
- **Declarative parameters** — dropdowns, bounded numbers with preset buttons, free text; values injected as environment variables; live-restart on change
- **Menu bar presence** — a switch can add its own status item (or replace the app icon) while active, with an optional per-second countdown
- **State persistence** — desired states stored and re-applied at launch; daemons re-spawned, toggles re-checked before re-applying
- **Script manager GUI** — sidebar CRUD, metadata form with per-field documentation, icon picker (emoji / SF Symbols), dark code editor with an auto-generated read-only contract header; lossless round-trip editing of hand-written scripts
- **Safety** — orphan cleanup on quit, natural daemon exit (exit 0) resets the switch, unsaved-change guards on every exit path, delete moves to Trash
- **Localized** — English and Simplified Chinese, follows the system language, switchable at runtime

## Requirements

- macOS 14+
- Swift toolchain (build from source; app bundle distribution is on the roadmap)

## Getting Started

```bash
git clone git@github.com:HanlunWang/open-toggle.git
cd open-toggle
swift run
```

A switch icon appears in the menu bar. First launch seeds a library of ready-to-use switches into the scripts directory (each installed once; deleting one won't resurrect it):

| Switch | Form | What it does | Default |
|---|---|---|---|
| ☕️ Keep Awake | daemon | `caffeinate` with a mode dropdown and 1/2/4/8 h duration presets; menu bar countdown | on |
| 👁️ Show Hidden Files | toggle | Finder hidden-file visibility | on |
| 🌙 Dark Mode | toggle | System appearance (one-time Automation permission prompt) | on |
| 🔇 Mute Audio | toggle | System output mute | on |
| ▦ Hide Desktop Icons | toggle | Clean desktop for screen sharing / recording | on |
| ⬒ Dock Auto-Hide | toggle | Dock auto-hiding | on |
| 📶 Wi-Fi | toggle | Wi-Fi power (device auto-detected) | deactivated |
| 🌐 Local HTTP Server | daemon | `python3 -m http.server` with port + directory parameters | deactivated |
| 🛡 Web Proxy | toggle | System HTTP/HTTPS proxy with service / host / port parameters | deactivated |

Deactivated switches stay out of the menu bar until you enable them (eye icon in the script manager sidebar). Create your own via **Manage Scripts**, or drop a script into the directory and hit reload.

## Script Contract

A switch is a single executable script in `~/.config/open-toggle/switches/`, carrying metadata as `# <switch.*>` directive comments within the first 40 lines.

### Directives

| Directive | Required | Description |
|---|---|---|
| `<switch.name>` | yes | Display name |
| `<switch.icon>` | no | Emoji, or SF Symbols name prefixed `sf:` (e.g. `sf:cup.and.saucer.fill`) |
| `<switch.type>` | no | `toggle` (default) or `daemon` |
| `<switch.param>` | no | One parameter per line; see below |
| `<switch.menubar>` | no | Menu bar presence while on; see below |

### Invocation protocol

**`toggle`** — imperative, for idempotent operations:

```
<script> on       # turn on
<script> off      # turn off
<script> status   # print "on" or "off" to stdout; polled every 5 s
```

A non-zero exit code from `status` marks the state *unknown*; a non-zero exit from `on`/`off` marks it *error*.

**`daemon`** — long-running foreground process:

```
<script> run      # spawned and held by the app
```

Off sends SIGTERM. Use `exec` so the signal reaches your process directly. Exit 0 is treated as natural completion and resets the switch; a non-zero exit marks it *error*.

### Parameters

```bash
# <switch.param> key=mode type=select label=Mode default=d options="Display only=d|Display & system=dis"
# <switch.param> key=duration type=number label="Duration (min)" default=0 min=0 max=1440 presets="Unlimited=0|1 h=60|2 h=120"
# <switch.param> key=note type=text label=Note hint="free text"
```

| Attribute | Applies to | Description |
|---|---|---|
| `key` | all | Identifier; injected as `SWITCH_<KEY>` (uppercased, non-alphanumerics → `_`) |
| `type` | all | `select` \| `number` \| `text` |
| `label` | all | Display label in the panel |
| `default` | all | Initial value; for `select`, defaults to the first option's value |
| `options` | select | `label=value` pairs separated by `\|` |
| `presets` | number, text | Quick-pick buttons, same `label=value\|…` format; coexist with the input field |
| `min` / `max` | number | Inclusive bounds |
| `hint` | all | Placeholder (text) or tooltip (others) |

Quote any attribute value containing spaces. Values are injected as environment variables on every invocation; changing a parameter while a switch is on restarts it with the new environment.

### Menu bar presence

```bash
# <switch.menubar> mode=add icon=☕️ countdown=on
```

- `mode=add` — dedicated status item while on; its menu offers a direct turn-off action
- `mode=replace` — temporarily replaces the app's own menu bar icon
- `icon` — emoji or `sf:<symbol-name>`
- `countdown=on` — shows remaining time (refreshed every second); requires a `number` parameter with key `duration` in minutes, where `0` means unlimited

### Execution details

- Scripts are `chmod 755` on save; files without the executable bit fall back to `/bin/sh`
- State model: `on` / `off` / `error` / `unknown`
- Desired states persist in `UserDefaults` and are re-applied at launch: daemons re-spawn; toggles check `status` first and only run `on` if needed
- All held daemon processes are terminated when the app quits
- Saving a script whose switch is currently on restarts it with the new content

## Architecture

```
Sources/OpenToggle/
├── OpenToggleApp.swift        # MenuBarExtra entry, manager window, termination guards
├── MenuView.swift             # Panel: status lights, toggles, expandable parameter controls
├── ManagerView.swift          # Manager window: sidebar CRUD, metadata form, code editor
├── IconPicker.swift           # Emoji / SF Symbols picker grids
├── StatusBarController.swift  # Script-declared status items (icon + countdown + menu)
├── SwitchModel.swift          # Contract parsing (directives, params, menubar)
├── SwitchManager.swift        # Registry, process lifecycle, polling, persistence
├── ScriptRunner.swift         # Process wrapper (commands / daemon spawn, env injection)
├── Localization.swift         # Runtime-switchable string tables (en, zh-Hans)
└── ExampleScripts.swift       # Seeded example switches
```

## Roadmap

- App bundle packaging, notarization, Homebrew cask
- Accessibility-dependent examples (keyboard jiggler via CGEvent)
- Manifest-style contract (TOML) as an alternative to comment headers
- Scheduling, conditional triggers, switch dependencies

## License

MIT
