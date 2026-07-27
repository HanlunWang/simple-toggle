#!/bin/bash
# <switch.name> Mute Audio
# <switch.icon> sf:speaker.slash.fill
# <switch.type> toggle
#
# Mutes/unmutes system output volume. No permissions required.
case "$1" in
  on)
    osascript -e 'set volume output muted true'
    ;;
  off)
    osascript -e 'set volume output muted false'
    ;;
  status)
    v="$(osascript -e 'output muted of (get volume settings)' 2>/dev/null)"
    [ "$v" = "true" ] && echo on || echo off
    ;;
esac
