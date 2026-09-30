#!/bin/sh
set -eu
app_name=${PACKAGE_APP_NAME:-ReadyRig}
executable=${PACKAGE_EXECUTABLE:-readyrig}
package_dir=${PACKAGE_OUTPUT_DIR:-dist}
case "$app_name:$executable" in *[!A-Za-z0-9:]*|:*|*:) printf '%s\n' 'Invalid package name' >&2; exit 1;; esac
bundle="$package_dir/$app_name.app/Contents"
version=${VERSION:-dev}
identity=${SIGN_IDENTITY:--}
case "$version" in *[!0-9A-Za-z.+-]*|'') printf '%s\n' 'Invalid VERSION' >&2; exit 1;; esac
mkdir -p "$bundle/MacOS" "$bundle/Resources"
go run ./cmd/iconbuild
iconutil -c icns dist/readyrig.iconset -o "$bundle/Resources/readyrig.icns"
cp bin/readyrig "$bundle/MacOS/$executable"
cp internal/server/assets/MAGPIE-LICENSE.txt "$bundle/Resources/MAGPIE-LICENSE.txt"
cat > "$bundle/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleName</key><string>ReadyRig</string><key>CFBundleDisplayName</key><string>ReadyRig</string><key>CFBundleIdentifier</key><string>dev.local.relay</string><key>CFBundleExecutable</key><string>$executable</string><key>CFBundleIconFile</key><string>readyrig.icns</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>${version#v}</string><key>CFBundleVersion</key><string>${version#v}</string><key>LSMinimumSystemVersion</key><string>12.0</string><key>NSHighResolutionCapable</key><true/><key>NSHumanReadableCopyright</key><string>ReadyRig Local Agent Adapter</string></dict></plist>
PLIST
codesign --force --deep --sign "$identity" "$package_dir/$app_name.app"
codesign --verify --deep --strict "$package_dir/$app_name.app"
printf '%s\n' "Built $package_dir/$app_name.app ($version)"
