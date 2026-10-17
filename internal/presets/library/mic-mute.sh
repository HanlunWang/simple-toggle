#!/bin/bash
# <switch.name> Mute Microphone
# <switch.icon> lucide:mic-off
# <switch.type> toggle
# <switch.description> Set the input volume to zero; turning it off restores the previous level
#
# The previous input volume is kept in a file under $TMPDIR.
SAVED="${TMPDIR:-/tmp}/simpletoggle-mic-volume"
case "$1" in
  on)
    osascript -e 'input volume of (get volume settings)' > "$SAVED"
    osascript -e 'set volume input volume 0'
    ;;
  off)
    LEVEL="$(cat "$SAVED" 2>/dev/null)"
    [ -n "$LEVEL" ] && [ "$LEVEL" -gt 0 ] || LEVEL=75
    osascript -e "set volume input volume $LEVEL"
    ;;
  status)
    [ "$(osascript -e 'input volume of (get volume settings)')" = "0" ] && echo on || echo off
    ;;
esac
