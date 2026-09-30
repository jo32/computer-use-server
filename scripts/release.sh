#!/bin/sh
# Build release assets locally. Publishing is a separate, explicit step.
set -eu
: "${VERSION:?Set VERSION to a release version, for example 0.5.0}"
printf '%s' "$VERSION" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+$' || { printf '%s\n' 'VERSION must be a stable x.y.z release' >&2; exit 1; }
repo=${RELEASE_REPO:-jo32/readyrig}
printf '%s' "$repo" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 1
version=${VERSION#v}
out="dist/releases/$version"
mkdir -p "$out" bin
flags="-s -w -X computer-use-server/internal/buildinfo.Version=$version -X computer-use-server/internal/buildinfo.ReleaseRepo=$repo"
for target in darwin linux windows; do
  for arch in amd64 arm64; do
    suffix=''
    if [ "$target" = windows ]; then suffix='.exe'; fi
    CGO_ENABLED=0 GOOS="$target" GOARCH="$arch" go build -tags nogui -ldflags "$flags" -o "$out/readyrig-web-$target-$arch$suffix" ./cmd/adapter
  done
done
if [ "$(uname -s)" = Darwin ]; then
  native=$(go env GOARCH)
  other=amd64
  if [ "$native" = amd64 ]; then other=arm64; fi
  for arch in "$other" "$native"; do
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" MACOSX_DEPLOYMENT_TARGET=12.0 go build -tags nogui -ldflags "$flags" -o "$out/readyrig-web-darwin-$arch" ./cmd/adapter
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" MACOSX_DEPLOYMENT_TARGET=12.0 go build -ldflags "$flags" -o bin/readyrig ./cmd/adapter
    cp bin/readyrig "$out/readyrig-darwin-$arch"
    VERSION="$version" sh scripts/package-macos.sh
    ditto -c -k --norsrc --keepParent dist/ReadyRig.app "$out/readyrig-darwin-$arch.zip"
    # Previously installed updaters request their old filenames and bundle
    # layouts. Their bridge packages display ReadyRig and retain the signing ID.
    compat_dir=$(mktemp -d "$out/.compat-XXXXXX")
    trap 'rm -rf "$compat_dir"' 0
    for legacy in Readrig:readrig Relay:relay; do
      legacy_app=${legacy%:*}
      legacy_executable=${legacy#*:}
      cp bin/readyrig "$out/$legacy_executable-darwin-$arch"
      VERSION="$version" PACKAGE_APP_NAME="$legacy_app" PACKAGE_EXECUTABLE="$legacy_executable" PACKAGE_OUTPUT_DIR="$compat_dir" sh scripts/package-macos.sh
      ditto -c -k --norsrc --keepParent "$compat_dir/$legacy_app.app" "$out/$legacy_executable-darwin-$arch.zip"
    done
    rm -rf "$compat_dir"
    trap - 0
  done
fi
# Keep existing CLI installations able to discover the renamed release.
for target in darwin linux windows; do
  for arch in amd64 arm64; do
    suffix=''
    if [ "$target" = windows ]; then suffix='.exe'; fi
    for legacy in readrig relay; do
      cp "$out/readyrig-web-$target-$arch$suffix" "$out/$legacy-web-$target-$arch$suffix"
    done
  done
done
cp internal/update/MAGPIE-LICENSE.txt "$out/MAGPIE-LICENSE.txt"
python3 - "$out" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
assets = sorted(p for p in root.iterdir() if p.is_file() and p.name != 'SHA256SUMS')
with (root / 'SHA256SUMS').open('w') as f:
    for p in assets:
        f.write(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n')
PY
printf '%s\n' "Release assets: $out"
