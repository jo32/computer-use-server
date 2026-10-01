#!/bin/sh
# ReadyRig CLI installer for macOS and Linux. Does not start a service or change permissions.
set -eu

usage() {
  cat <<'EOF'
Install ReadyRig CLI:
  curl -fsSL https://readyrig.getmegaportal.com/install.sh | sh
  curl -fsSL https://readyrig.getmegaportal.com/install.sh | sh -s -- --version 0.7.0

Options:
  --version VERSION   Stable release version (default: latest)
  --install-dir PATH  Destination directory (default: ~/.local/bin)
  --repo OWNER/REPO   GitHub release repository (default: jo32/readyrig)
  --help              Show this help

Environment: READYRIG_VERSION, READYRIG_INSTALL_DIR, READYRIG_INSTALL_REPO
EOF
}
fail() { printf 'ReadyRig: %s\n' "$*" >&2; exit 1; }

version=${READYRIG_VERSION:-latest}
install_dir=${READYRIG_INSTALL_DIR:-${HOME:?HOME must be set}/.local/bin}
repo=${READYRIG_INSTALL_REPO:-jo32/readyrig}
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version|--install-dir|--repo)
      [ "$#" -ge 2 ] || fail "$1 requires a value"
      case "$1" in --version) version=$2;; --install-dir) install_dir=$2;; --repo) repo=$2;; esac
      shift 2;;
    --help|-h) usage; exit 0;;
    *) fail "Unknown option: $1";;
  esac
done
printf '%s\n' "$repo" | LC_ALL=C grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || fail 'Repository must be OWNER/REPO'
[ -n "$install_dir" ] || fail 'Install directory cannot be empty'
case "$install_dir" in /*) ;; *) install_dir="$(pwd)/$install_dir";; esac

case "$(uname -s)" in Darwin) platform=darwin;; Linux) platform=linux;; *) fail 'Supported systems: macOS and Linux';; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) fail 'Supported architectures: arm64 and amd64';; esac
command -v curl >/dev/null 2>&1 || fail 'curl is required'
if command -v sha256sum >/dev/null 2>&1; then
  hash_file() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  hash_file() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  fail 'sha256sum or shasum is required to verify the download'
fi
download() { curl --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 300 --retry 3 -fsSL "$1" -o "$2"; }

if [ "$version" = latest ]; then
  # Pin one tag before downloading the binary and checksums; never mix releases.
  latest=$(curl --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 60 --retry 3 -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") || fail 'Cannot find the latest release'
  prefix="https://github.com/$repo/releases/tag/"
  case "$latest" in "$prefix"*) version=${latest#"$prefix"};; *) fail 'Unexpected latest-release URL';; esac
fi
version=${version#v}
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail 'Version must be a stable x.y.z release'
asset="readyrig-web-$platform-$arch"
base="https://github.com/$repo/releases/download/v$version"
printf 'Installing ReadyRig %s (%s/%s)\n' "$version" "$platform" "$arch"
mkdir -p "$install_dir"
install_dir=$(CDPATH= cd "$install_dir" && pwd -P)
[ ! -d "$install_dir/readyrig" ] || fail 'Destination readyrig is a directory'
temp=$(mktemp -d "$install_dir/.readyrig-install.XXXXXX")
trap 'rm -rf "$temp"' 0
trap 'exit 1' 1 2 15
download "$base/SHA256SUMS" "$temp/SHA256SUMS" || fail 'Cannot download release checksums'
download "$base/$asset" "$temp/readyrig" || fail 'Cannot download the CLI binary'
expected=$(awk -v asset="$asset" '$2 == asset {print $1}' "$temp/SHA256SUMS")
printf '%s\n' "$expected" | LC_ALL=C grep -Eq '^[a-fA-F0-9]{64}$' || fail 'Release has no unique valid SHA-256 checksum for this binary'
actual=$(hash_file "$temp/readyrig")
[ "$(printf '%s' "$actual" | tr 'A-F' 'a-f')" = "$(printf '%s' "$expected" | tr 'A-F' 'a-f')" ] || fail 'Download checksum mismatch; existing installation was kept'
chmod 755 "$temp/readyrig"
if ! "$temp/readyrig" help > "$temp/help.txt" 2>&1 || ! grep -Fq 'config set' "$temp/help.txt"; then
  fail 'This release does not provide the CLI commands; select a newer CLI release with --version'
fi
# The staging directory shares the destination filesystem, so replacement is atomic.
mv -f "$temp/readyrig" "$install_dir/readyrig"
printf '\nInstalled: %s/readyrig\n' "$install_dir"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Add this directory to PATH in your shell profile: %s\n' "$install_dir";;
esac
printf '\nNext steps:\n  readyrig init --workspace ~/agent_workspace --allow-shell --no-chrome\n  readyrig serve\n\nRun readyrig help for commands.\n'
