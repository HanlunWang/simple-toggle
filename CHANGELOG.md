# Changelog

## 2.0.2

- Automatic updates now work: SimpleToggle checks a little after launch and every 6 hours, installs verified updates in the background and uses them at the next launch. Check Now in Settings installs right away and offers a restart. Turning off automatic updates also stops the check
- 2.0.0 and 2.0.1 cannot update themselves: download this version once from the Releases page, or run `brew upgrade --cask --greedy simpletoggle`

## 2.0.1

- The app inside the disk image now carries its notarization ticket, so it opens on a Mac that is offline

## 2.0.0

The first release of SimpleToggle.

- **New design** — a quiet black-and-white interface where one signal color marks what is on or running, with dot-matrix status marks, Geist type and a new icon
- **Buttons** — a new `action` switch type that runs once per press and shows progress and the result
- **Workbench** — edit scripts in a code editor whose header stays in sync with an inspector; test them with live output; browse the run history and a library of ready-made scripts
- **Scenes** — set several switches in one click, and restore them with another
- **Automations** — run switches or scenes by time, Wi-Fi network, power, apps, displays, login / wake / unlock, file changes or shortcuts
- **Global shortcuts** for switches, scenes, the panel and the workbench
- **Activity** — usage statistics (uses per day, a calendar heat map, time of week, sources, switches by use) and every run with its source, duration, exit code and output
- **Safe to quit** — quitting explains what happens to the switches that are on; if SimpleToggle crashes, long-running switches are stopped right away and turned back on at the next launch
- **Settings** — themes, six palettes, panel layout and density, menu bar icon styles, notifications, and more
- **AI** — permissions for AI agents, approval of script changes from AI with a diff, one-click setup for Claude Code, Cursor and Codex, and new MCP tools
- **Deep links** — `simpletoggle://` for Shortcuts, Raycast and Alfred
- **Automatic updates**, verified before they are installed
