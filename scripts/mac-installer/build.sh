#!/usr/bin/env bash
# Builds "Install corgi.app": a small Mac app that installs the corgi command
# line with install.sh, starts the agent, and pairs the Corgi app from the App
# Store with it - so nobody has to open a terminal. The store app cannot do
# this itself: it runs in the sandbox.
#
# Output: resources/mac/Install-corgi.dmg, signed with the Developer ID and
# notarized. It holds no corgi binary: it fetches the newest release when it
# runs, so it is rebuilt only when this folder changes.
#
#   scripts/mac-installer/build.sh            build, sign, notarize, staple
#   NOTARIZE=0 scripts/mac-installer/build.sh  build and sign only
set -euo pipefail
cd "$(dirname "$0")/../.."
SRC=scripts/mac-installer
OUT=resources/mac/Install-corgi.dmg
IDENTITY="${SIGN_IDENTITY:-$(security find-identity -v -p codesigning | grep -o '"Developer ID Application[^"]*"' | head -1 | tr -d '"')}"
[ -n "$IDENTITY" ] || { echo "no Developer ID Application certificate in the keychain"; exit 1; }
kc() { security find-generic-password -a "$USER" -s "$1" -w; }

WORK="$(mktemp -d)"
APP="$WORK/dmg/Install corgi.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$WORK/icon.iconset" resources/mac

echo "==> compiling"
for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos13.0" "$SRC/InstallCorgi.swift" -o "$WORK/InstallCorgi-$arch"
done
lipo -create -output "$APP/Contents/MacOS/InstallCorgi" "$WORK/InstallCorgi-arm64" "$WORK/InstallCorgi-x86_64"
cp "$SRC/Info.plist" "$APP/Contents/Info.plist"

echo "==> icon"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" resources/corgi.png --out "$WORK/icon.iconset/icon_${size}x${size}.png" >/dev/null
  sips -z "$((size * 2))" "$((size * 2))" resources/corgi.png --out "$WORK/icon.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$WORK/icon.iconset" -o "$APP/Contents/Resources/AppIcon.icns"

echo "==> signing as $IDENTITY"
codesign --force --options runtime --timestamp --sign "$IDENTITY" "$APP"
codesign --verify --strict "$APP"

echo "==> dmg"
hdiutil create -quiet -volname "Install corgi" -srcfolder "$WORK/dmg" -ov -format UDZO "$OUT"
codesign --force --timestamp --sign "$IDENTITY" "$OUT"

if [ "${NOTARIZE:-1}" = "1" ]; then
  echo "==> notarizing"
  kc asc-api-key | base64 -d > "$WORK/asc.p8"
  xcrun notarytool submit "$OUT" --key "$WORK/asc.p8" --key-id "$(kc asc-api-key-id)" --issuer "$(kc asc-api-issuer-id)" --wait | tee "$WORK/notary.log"
  : > "$WORK/asc.p8"
  grep -q "status: Accepted" "$WORK/notary.log" || { echo "not accepted"; exit 1; }
  xcrun stapler staple "$OUT"
  spctl --assess --type open --context context:primary-signature -v "$OUT"
fi
echo "built $OUT ($(du -h "$OUT" | cut -f1))"
