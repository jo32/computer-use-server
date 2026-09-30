#!/bin/sh
set -eu
bundle='dist/Relay.app/Contents'
version=${VERSION:-dev}
identity=${SIGN_IDENTITY:--}
case "$version" in *[!0-9A-Za-z.+-]*|'') printf '%s\n' 'Invalid VERSION' >&2; exit 1;; esac
mkdir -p "$bundle/MacOS" "$bundle/Resources"
cp bin/relay "$bundle/MacOS/relay"
cp internal/server/assets/MAGPIE-LICENSE.txt "$bundle/Resources/MAGPIE-LICENSE.txt"
cat > "$bundle/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleName</key><string>Relay</string><key>CFBundleDisplayName</key><string>Relay</string><key>CFBundleIdentifier</key><string>dev.local.relay</string><key>CFBundleExecutable</key><string>relay</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>${version#v}</string><key>CFBundleVersion</key><string>${version#v}</string><key>LSMinimumSystemVersion</key><string>12.0</string><key>NSHighResolutionCapable</key><true/><key>NSHumanReadableCopyright</key><string>Relay Local Agent Adapter</string></dict></plist>
PLIST
codesign --force --deep --sign "$identity" 'dist/Relay.app'
codesign --verify --deep --strict 'dist/Relay.app'
printf '%s\n' "Built dist/Relay.app ($version)"
