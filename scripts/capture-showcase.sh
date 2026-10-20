#!/bin/sh
# Screenshots the frames scripts/showcase.mjs wrote, at 2x, and joins each
# scene into docs/media/<scene>.gif, a still of its last frame, and an mp4.
set -e
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
cd "$(dirname "$0")/.."
node scripts/showcase.mjs
M=docs/media
F=$M/frames
GROUND="#0a0e2a"
shot() { perl -e 'alarm 90; exec @ARGV' "$CHROME" --headless=new --hide-scrollbars --disable-gpu --force-device-scale-factor=2 --window-size="$3" --screenshot="$PWD/$2" "file://$PWD/$1" >/dev/null 2>&1 || shot_retry "$@"; }
shot_retry() { pkill -f "Google Chrome --headless" 2>/dev/null; sleep 1; "$CHROME" --headless=new --hide-scrollbars --disable-gpu --force-device-scale-factor=2 --window-size="$3" --screenshot="$PWD/$2" "file://$PWD/$1" >/dev/null 2>&1; }
trim() { magick "$1" -fuzz 1% -trim +repage -bordercolor "$GROUND" -border 28 "$1"; }

scenes=${*:-$(node -e 'const s=require("./docs/media/frames/scenes.json");console.log(Object.keys(s).join(" "))')}
for scene in $scenes; do
  n=$(node -e "console.log(require('./docs/media/frames/scenes.json')['$scene'].frames)")
  size=960,1000; case "$scene" in phone|dashboard) size=1300,1000;; telegram) size=420,700;; esac
  i=0; files=""
  while [ $i -lt $n ]; do
    shot $F/$scene-$i.html $F/$scene-$i.png $size
    trim $F/$scene-$i.png
    files="$files $F/$scene-$i.png"
    i=$((i+1))
  done
  # One canvas per scene, so a GIF holds still while the terminal fills.
  w=0; h=0
  for f in $files; do
    set -- $(magick identify -format "%w %h" "$f"); [ "$1" -gt "$w" ] && w=$1; [ "$2" -gt "$h" ] && h=$2
  done
  for f in $files; do magick "$f" -background "$GROUND" -gravity north -extent "${w}x${h}" "$f"; done
  # The last frame stays longer, so the eye can read the result before the loop.
  last=$F/$scene-$((n-1)).png
  magick -delay 110 -loop 0 $files -delay 300 $last -layers Optimize $M/$scene.gif
  cp $last $M/$scene.png
  if command -v ffmpeg >/dev/null; then
    list=$F/$scene.txt; : > $list
    for f in $files; do printf "file '%s'\nduration 1.2\n" "$PWD/$f" >> $list; done
    printf "file '%s'\nduration 3\nfile '%s'\n" "$PWD/$last" "$PWD/$last" >> $list
    ffmpeg -y -loglevel error -f concat -safe 0 -i $list -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p" -r 30 $M/$scene.mp4
  fi
  echo "$scene: $n frames"
done
[ $# -eq 0 ] && rm -rf $F
ls -la $M | awk '{print $5, $9}'
