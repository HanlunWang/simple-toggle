#!/bin/bash
# <switch.name> Pomodoro
# <switch.icon> lucide:timer
# <switch.type> daemon
# <switch.description> A focus timer that notifies you when the time is up
# <switch.param> key=duration type=number label="Minutes" default=25 min=1 max=180 presets="25 min=25|50 min=50|90 min=90"
# <switch.menubar> mode=add icon=lucide:timer countdown=on
#
# Daemon contract: SimpleToggle runs `<script> run` and holds the process.
# Turning the switch off sends SIGTERM, which ends the timer quietly; when
# the time is up the script notifies and exits 0, which resets the switch.
MINUTES="${SWITCH_DURATION:-25}"
sleep $((MINUTES * 60)) &
TIMER=$!
trap 'kill $TIMER 2>/dev/null; exit 0' TERM INT
wait $TIMER
osascript -e "display notification \"$MINUTES minutes are up\" with title \"Pomodoro\" sound name \"Glass\""
