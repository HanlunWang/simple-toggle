#!/bin/bash
# <switch.name> Dark Mode
# <switch.icon> sf:moon.fill
# <switch.type> toggle
#
# Toggles the system appearance via AppleScript. The first invocation
# triggers a one-time Automation permission prompt (System Events).
# External changes (Control Center, auto-switching) are picked up by
# the 5-second status poll.
case "$1" in
  on)
    osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to true'
    ;;
  off)
    osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to false'
    ;;
  status)
    v="$(osascript -e 'tell application "System Events" to tell appearance preferences to get dark mode' 2>/dev/null)"
    [ "$v" = "true" ] && echo on || echo off
    ;;
esac
