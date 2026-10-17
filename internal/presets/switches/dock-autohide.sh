#!/bin/bash
# <switch.name> Dock Auto-Hide
# <switch.icon> sf:dock.rectangle
# <switch.type> toggle
#
# Toggles Dock auto-hiding. Restarting the Dock is required for the
# setting to take effect; open windows are not affected.
case "$1" in
  on)
    defaults write com.apple.dock autohide -bool true
    killall Dock
    ;;
  off)
    defaults write com.apple.dock autohide -bool false
    killall Dock
    ;;
  status)
    v="$(defaults read com.apple.dock autohide 2>/dev/null)"
    case "$v" in
      1|true|TRUE|YES|yes) echo on ;;
      *) echo off ;;
    esac
    ;;
esac
