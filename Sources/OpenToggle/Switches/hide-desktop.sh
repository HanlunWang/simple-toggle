#!/bin/bash
# <switch.name> Hide Desktop Icons
# <switch.icon> sf:rectangle.dashed
# <switch.type> toggle
#
# Hides all desktop icons — useful for screen sharing, recording,
# and presentations. Files stay untouched in ~/Desktop.
case "$1" in
  on)
    defaults write com.apple.finder CreateDesktop -bool false
    killall Finder
    ;;
  off)
    defaults write com.apple.finder CreateDesktop -bool true
    killall Finder
    ;;
  status)
    v="$(defaults read com.apple.finder CreateDesktop 2>/dev/null)"
    case "$v" in
      0|false|FALSE|NO|no) echo on ;;
      *) echo off ;;
    esac
    ;;
esac
