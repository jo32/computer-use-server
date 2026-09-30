#!/bin/sh
# Build release assets locally. Publishing is a separate, explicit step.
set -eu
: "${VERSION:?Set VERSION to a release version, for example 0.5.0}"
printf '%s' "$VERSION" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+$' || { printf '%s\n' 'VERSION must be a stable x.y.z release' >&2; exit 1; }
repo=${RELEASE_REPO:-jo32/computer-use-server}
printf '%s' "$repo" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || exit 1
version=${VERSION#v}
out="dist/releases/$version"
mkdir -p "$out" bin
flags="-s -w -X computer-use-server/internal/buildinfo.Version=$version -X computer-use-server/internal/buildinfo.ReleaseRepo=$repo"
for target in darwin linux windows; do
  for arch in amd64 arm64; do
    suffix=''
    if [ "$target" = windows ]; then suffix='.exe'; fi
    CGO_ENABLED=0 GOOS="$target" GOARCH="$arch" go build -tags nogui -ldflags "$flags" -o "$out/relay-web-$target-$arch$suffix" ./cmd/adapter
  done
done
if [ "$(uname -s)" = Darwin ]; then
  native=$(go env GOARCH)
  other=amd64
  if [ "$native" = amd64 ]; then other=arm64; fi
  for arch in "$other" "$native"; do
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" MACOSX_DEPLOYMENT_TARGET=12.0 go build -tags nogui -ldflags "$flags" -o "$out/relay-web-darwin-$arch" ./cmd/adapter
    CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" MACOSX_DEPLOYMENT_TARGET=12.0 go build -ldflags "$flags" -o bin/relay ./cmd/adapter
    cp bin/relay "$out/relay-darwin-$arch"
    VERSION="$version" sh scripts/package-macos.sh
    ditto -c -k --keepParent dist/Relay.app "$out/relay-darwin-$arch.zip"
  done
fi
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
