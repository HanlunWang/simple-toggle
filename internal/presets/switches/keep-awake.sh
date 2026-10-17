#!/bin/bash
# <switch.name> Keep Awake
# <switch.icon> ☕️
# <switch.type> daemon
# <switch.param> key=mode type=select label=Mode default=d options="Display only=d|System only=is|Display & system=dis"
# <switch.param> key=duration type=number label="Duration (min)" default=0 min=0 max=1440 presets="Unlimited=0|1 h=60|2 h=120|4 h=240|8 h=480" hint="0 = no limit"
# <switch.menubar> mode=add icon=☕️ countdown=on
#
# Daemon contract: OpenToggle launches `<script> run` and holds the
# process; turning the switch off sends SIGTERM. `exec` replaces this
# shell so the signal reaches caffeinate directly. With -t the process
# exits naturally (exit 0) and the app resets the switch.
#
# Parameters arrive as environment variables: $SWITCH_MODE, $SWITCH_DURATION
# caffeinate: -d prevent display sleep / -i prevent idle sleep /
#             -s prevent system sleep (effective on AC power only)
ARGS="-${SWITCH_MODE:-d}"
if [ "${SWITCH_DURATION:-0}" -gt 0 ]; then
  ARGS="$ARGS -t $((SWITCH_DURATION * 60))"
fi
exec caffeinate $ARGS
