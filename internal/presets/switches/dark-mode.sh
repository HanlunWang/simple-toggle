#!/bin/bash
# <switch.name> Dark Mode
# <switch.icon> sf:moon.fill
# <switch.type> toggle
#
# Toggles the system appearance via AppleScript. The first time you
# switch it, macOS asks once to let SimpleToggle control System Events.
# `status` only reads the appearance setting, so polling needs no
# permission; external changes (Control Center, auto-switching) are
# picked up by the status poll.
case "$1" in
  on)
    osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to true'
    ;;
  off)
    osascript -e 'tell application "System Events" to tell appearance preferences to set dark mode to false'
    ;;
  status)
    [ "$(defaults read -g AppleInterfaceStyle 2>/dev/null)" = "Dark" ] && echo on || echo off
    ;;
esac
