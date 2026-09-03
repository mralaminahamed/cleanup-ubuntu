#!/usr/bin/env bash
# Rasterise the generated SVGs. Run after assets/generate.py.
#
# Chrome is used rather than ImageMagick's own SVG renderer, which does not
# handle the gradients and filters here faithfully.
set -euo pipefail
cd "$(dirname "$0")"

shot() { # shot <svg> <w> <h> <out>
  google-chrome --headless --disable-gpu --screenshot="/tmp/render-$$.png" \
    --window-size="$2,$3" --default-background-color=00000000 --hide-scrollbars \
    "file://$PWD/$1" 2>/dev/null
  convert "/tmp/render-$$.png" -resize "$2x$3" "$4"
  rm -f "/tmp/render-$$.png"
}

shot icon.svg        256  256  icon-256.png
shot icon-macos.svg 1024 1024  icon-macos-1024.png
shot banner.svg     1280  400  banner.png
H=$(grep -o 'height="[0-9]*"' demo.svg | head -2 | tail -1 | grep -o '[0-9]*')
shot demo.svg        980 "$H"  demo.png

# macOS .iconset — exactly the sizes Apple's iconutil expects, no more.
rm -rf reclaim.iconset && mkdir -p reclaim.iconset
for s in 16 32 128 256 512; do
  convert icon-macos-1024.png -resize "${s}x${s}"         "reclaim.iconset/icon_${s}x${s}.png"
  convert icon-macos-1024.png -resize "$((s*2))x$((s*2))" "reclaim.iconset/icon_${s}x${s}@2x.png"
done
echo "rendered: icon-256.png icon-macos-1024.png banner.png demo.png reclaim.iconset/"
