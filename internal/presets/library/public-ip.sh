#!/bin/bash
# <switch.name> Copy Public IP
# <switch.icon> lucide:copy
# <switch.type> action
# <switch.description> Look up your public IP address and copy it to the clipboard
#
# Sends one HTTPS request to api.ipify.org (change SERVICE to use another).
SERVICE="https://api.ipify.org"
IP="$(curl -fsS --max-time 8 "$SERVICE")" || { echo "lookup failed"; exit 1; }
printf %s "$IP" | pbcopy
echo "$IP (copied)"
