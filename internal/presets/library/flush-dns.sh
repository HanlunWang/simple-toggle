#!/bin/bash
# <switch.name> Flush DNS
# <switch.icon> lucide:rotate-cw
# <switch.type> action
# <switch.description> Clear the macOS DNS cache
#
# Action contract: SimpleToggle runs `<script>` with no arguments; exit 0
# means success and the last line of output is shown as the result.
# Flushing the resolver cache needs administrator rights, so macOS asks
# for your password once per run.
osascript -e 'do shell script "dscacheutil -flushcache; killall -HUP mDNSResponder" with administrator privileges' >/dev/null || exit 1
echo "DNS cache flushed"
