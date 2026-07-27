#!/bin/bash
# <switch.name> Show Hidden Files
# <switch.icon> 👁️
# <switch.type> toggle
#
# Toggle contract: OpenToggle invokes `<script> on | off | status`.
# `status` must print "on" or "off" to stdout; a non-zero exit code
# marks the switch state as unknown.
case "$1" in
  on)
    defaults write com.apple.finder AppleShowAllFiles -bool true
    killall Finder
    ;;
  off)
    defaults write com.apple.finder AppleShowAllFiles -bool false
    killall Finder
    ;;
  status)
    v="$(defaults read com.apple.finder AppleShowAllFiles 2>/dev/null)"
    case "$v" in
      1|true|TRUE|YES|yes) echo on ;;
      *) echo off ;;
    esac
    ;;
esac
