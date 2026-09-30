#!/bin/bash
set -euo pipefail
target=${1:?Usage: sign-macos.sh TARGET [IDENTIFIER]}
identity=${SIGN_IDENTITY:--}
if [[ "${REQUIRE_DEVELOPER_ID:-0}" == 1 ]]; then
  if [[ -n "${SIGN_KEYCHAIN:-}" ]]; then
    identities=$(security find-identity -v -p codesigning "$SIGN_KEYCHAIN")
  else
    identities=$(security find-identity -v -p codesigning)
  fi
  identity=$(SIGN_REQUESTED_IDENTITY="$identity" python3 -c '
import os,re,sys
requested=os.environ["SIGN_REQUESTED_IDENTITY"]
for fingerprint,name in re.findall(r"\d+\) ([A-Fa-f0-9]{40}) \"([^\"]+)\"",sys.stdin.read()):
    if name.startswith("Developer ID Application:") and requested in (fingerprint,name):
        print(fingerprint)
        sys.exit(0)
sys.exit("A valid Developer ID Application identity is required for a release")
' <<< "$identities")
fi
if [[ "$target" == --check-identity ]]; then exit 0; fi

args=(--force --sign "$identity")
if [[ "$identity" != - ]]; then args+=(--options runtime --timestamp); fi
if [[ -n "${2:-}" ]]; then args+=(--identifier "$2"); fi
if [[ -n "${SIGN_KEYCHAIN:-}" ]]; then args+=(--keychain "$SIGN_KEYCHAIN"); fi
codesign "${args[@]}" "$target"
codesign --verify --deep --strict "$target"
