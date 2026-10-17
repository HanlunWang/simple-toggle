#!/bin/bash
# <switch.name> Web Proxy
# <switch.icon> sf:network.badge.shield.half.filled
# <switch.type> toggle
# <switch.param> key=service type=select label=Service default=Wi-Fi options="Wi-Fi=Wi-Fi|Ethernet=Ethernet"
# <switch.param> key=host type=text label=Host default=127.0.0.1 presets="127.0.0.1"
# <switch.param> key=port type=number label=Port default=7890 min=1 max=65535 presets="7890|8080|1080|8888"
#
# Sets/unsets the system HTTP + HTTPS proxy for the selected network
# service. Adjust host/port to match your local proxy client.
# Disabled by default: enable it in the script manager when needed.
case "$1" in
  on)
    networksetup -setwebproxy "$SWITCH_SERVICE" "$SWITCH_HOST" "$SWITCH_PORT"
    networksetup -setsecurewebproxy "$SWITCH_SERVICE" "$SWITCH_HOST" "$SWITCH_PORT"
    ;;
  off)
    networksetup -setwebproxystate "$SWITCH_SERVICE" off
    networksetup -setsecurewebproxystate "$SWITCH_SERVICE" off
    ;;
  status)
    networksetup -getwebproxy "$SWITCH_SERVICE" 2>/dev/null | grep -q "^Enabled: Yes" && echo on || echo off
    ;;
esac
