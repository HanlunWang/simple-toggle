#!/bin/bash
# <switch.name> OrbStack
# <switch.icon> lucide:container
# <switch.type> toggle
# <switch.description> Start or stop the OrbStack container engine
#
# Needs the `orb` command that comes with OrbStack (brew install orbstack).
PATH="$PATH:$HOME/.orbstack/bin"
command -v orb >/dev/null || { echo "orb not found"; exit 1; }
case "$1" in
  on)     orb start ;;
  off)    orb stop ;;
  status) [ "$(orb status 2>/dev/null)" = "Running" ] && echo on || echo off ;;
esac
