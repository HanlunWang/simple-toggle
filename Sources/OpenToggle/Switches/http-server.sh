#!/bin/bash
# <switch.name> Local HTTP Server
# <switch.icon> sf:globe
# <switch.type> daemon
# <switch.param> key=port type=number label=Port default=8000 min=1024 max=65535 presets="8000|8080|3000|5500"
# <switch.param> key=dir type=text label=Directory default=~ hint="Directory to serve"
# <switch.menubar> mode=add icon=sf:globe
#
# Serves a local directory over HTTP using Python's built-in server.
# Handy for sharing files on the LAN or previewing static sites.
# Disabled by default: enable it in the script manager when needed.
DIR="${SWITCH_DIR:-~}"
DIR="${DIR/#\~/$HOME}"
exec python3 -m http.server "${SWITCH_PORT:-8000}" --directory "$DIR"
