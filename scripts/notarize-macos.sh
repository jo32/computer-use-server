#!/bin/bash
set -euo pipefail
auth=()
if [[ -n "${NOTARY_PROFILE:-}" ]]; then
  auth=(--keychain-profile "$NOTARY_PROFILE")
  if [[ -n "${NOTARY_KEYCHAIN:-}" ]]; then auth+=(--keychain "$NOTARY_KEYCHAIN"); fi
else
  : "${NOTARY_KEY_PATH:?Set NOTARY_PROFILE or NOTARY_KEY_PATH for Apple notarization}"
  : "${NOTARY_KEY_ID:?Set NOTARY_KEY_ID for Apple notarization}"
  : "${NOTARY_ISSUER_ID:?Set NOTARY_ISSUER_ID for the App Store Connect team API key}"
  [[ -f "$NOTARY_KEY_PATH" ]] || { printf '%s\n' 'Notarization API key file not found' >&2; exit 1; }
  auth=(--key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID")
fi
if [[ "${1:-}" == --check-credentials ]]; then exit 0; fi

apps=${1:?Usage: notarize-macos.sh APP_DIRECTORY RELEASE_DIRECTORY}
release=${2:?Release directory required}
work="$apps/notarization"
packet="$work/ReadyRig"
mkdir -p "$packet/apps" "$packet/binaries"
for arch in arm64 amd64; do cp -R "$apps/$arch" "$packet/apps/"; done
for binary in "$release/"*darwin*; do
  [[ "$binary" != *.zip ]] || continue
  cp "$binary" "$packet/binaries/"
done
archive="$work/ReadyRig-notarization.zip"
ditto -c -k --keepParent "$packet" "$archive"
printf '%s\n' 'Submitting signed Mac apps and binaries to Apple for notarization'
xcrun notarytool submit "$archive" "${auth[@]}" --wait --timeout 30m --output-format json > "$work/result.json"
submission=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$work/result.json")
xcrun notarytool log "$submission" "${auth[@]}" "$work/log.json"
python3 - "$work/result.json" "$work/log.json" <<'PY'
import json,sys
result=json.load(open(sys.argv[1]))
log=json.load(open(sys.argv[2]))
print('Apple notarization:',result['status'],'·',result['id'])
if result['status']!='Accepted':
    for issue in log.get('issues') or []: print(issue.get('path',''),issue.get('message',''))
    sys.exit('Apple notarization did not accept this release')
for issue in log.get('issues') or []: print(issue.get('severity',''),issue.get('message',''))
PY
for arch in arm64 amd64; do
  for app in "$apps/$arch/"*.app; do
    xcrun stapler staple "$app"
    xcrun stapler validate "$app"
    codesign --verify --deep --strict "$app"
    spctl --assess --type execute --verbose=2 "$app"
  done
done
