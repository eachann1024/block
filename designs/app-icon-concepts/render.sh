#!/bin/bash
# usage: render.sh name.svg -> name.png (1024)
for f in "$@"; do
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --hide-scrollbars \
    --default-background-color=00000000 --window-size=1024,1024 --screenshot="$PWD/${f%.svg}.png" "file://$PWD/$f" >/dev/null 2>&1
done
