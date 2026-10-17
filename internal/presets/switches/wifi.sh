#!/bin/bash
# <switch.name> Wi-Fi
# <switch.icon> sf:wifi
# <switch.type> toggle
#
# Toggles Wi-Fi power. The device name (usually en0) is resolved
# dynamically from the hardware port list.
# Disabled by default: enable it in the script manager when needed.
DEV="$(networksetup -listallhardwareports | awk '/Wi-Fi|AirPort/{getline; print $2; exit}')"
[ -n "$DEV" ] || exit 1
case "$1" in
  on)
    networksetup -setairportpower "$DEV" on
    ;;
  off)
    networksetup -setairportpower "$DEV" off
    ;;
  status)
    networksetup -getairportpower "$DEV" | grep -q ": On" && echo on || echo off
    ;;
esac
