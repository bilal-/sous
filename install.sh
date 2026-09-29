#!/bin/sh
# Install sous from a GitHub release into ~/.local/bin, then run `sous setup`.
#
#   curl -fsSL https://raw.githubusercontent.com/bilal-/sous/main/install.sh | sh
#   SOUS_VERSION=v0.1.0 ... | sh     # pin a version (default: latest)
#   SOUS_BIN=~/bin ... | sh          # install elsewhere
set -eu

repo="bilal-/sous"
bin="${SOUS_BIN:-$HOME/.local/bin}"
version="${SOUS_VERSION:-}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "sous: unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in darwin|linux) ;; *) echo "sous: unsupported OS: $os" >&2; exit 1 ;; esac

if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')"
  [ -n "$version" ] || { echo "sous: could not determine latest release" >&2; exit 1; }
fi
plain="${version#v}"
asset="sous_${plain}_${os}_${arch}.tar.gz"
url="https://github.com/$repo/releases/download/$version/$asset"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "sous: downloading $version ($os/$arch)"
curl -fsSL "$url" -o "$tmp/$asset"
curl -fsSL "https://github.com/$repo/releases/download/$version/checksums.txt" -o "$tmp/checksums.txt"
(cd "$tmp" && grep " $asset\$" checksums.txt | { command -v sha256sum >/dev/null && sha256sum -c - || shasum -a 256 -c -; } >/dev/null) \
  || { echo "sous: checksum mismatch for $asset" >&2; exit 1; }
tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$bin"
install -m 0755 "$tmp/sous" "$bin/sous"
echo "sous: installed $("$bin/sous" version) to $bin/sous"

case ":$PATH:" in *":$bin:"*) ;; *) echo "sous: add $bin to your PATH" ;; esac
"$bin/sous" setup
