#!/bin/sh
# Makes web/public's logo and icons from assets/branding/logo.jpg. It runs
# inside the container make branding starts, which has ImageMagick.
set -eu

SRC=/code/assets/branding/logo.jpg
OUT=/code/web/public

# The picture reads at a few hundred pixels but not at sixteen, so the icons
# are cut from the gopher's face on the same blue rather than shrunk from the
# whole. Geometry is of the 1024 pixel source: width x height + left + top.
FACE=300x300+245+190

for width in 128 256 512; do
  magick "$SRC" -resize "${width}x${width}" -strip -quality 85 "$OUT/logo-${width}.webp"
done

face() { magick "$SRC" -crop "$FACE" +repage -resize "$1x$1" -strip "$2"; }
face 180 "$OUT/apple-touch-icon.png"
face 192 "$OUT/icon-192.png"
face 512 "$OUT/icon-512.png"
face 32 "$OUT/favicon-32.png"
magick "$SRC" -crop "$FACE" +repage -resize 96x96 -strip -quality 90 "$OUT/mark-96.webp"
magick "$SRC" -crop "$FACE" +repage -define icon:auto-resize=48,32,16 "$OUT/favicon.ico"
