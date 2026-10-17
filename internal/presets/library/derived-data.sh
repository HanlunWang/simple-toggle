#!/bin/bash
# <switch.name> Clear DerivedData
# <switch.icon> lucide:trash-2
# <switch.type> action
# <switch.description> Delete Xcode's build cache to free disk space
#
# Removes everything under ~/Library/Developer/Xcode/DerivedData. Xcode
# rebuilds what it needs on the next build. Quit Xcode first.
DIR="$HOME/Library/Developer/Xcode/DerivedData"
[ -d "$DIR" ] || { echo "nothing to clear"; exit 0; }
SIZE="$(du -sh "$DIR" 2>/dev/null | cut -f1)"
rm -rf "${DIR:?}"/*
echo "freed ${SIZE:-0}"
