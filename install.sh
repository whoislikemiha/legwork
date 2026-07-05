#!/bin/sh
# Install legwork from GitHub releases. No sudo, no dependencies beyond
# curl/tar. Installs to ~/.local/bin (override with LEGWORK_INSTALL_DIR).
set -eu

REPO="whoislikemiha/legwork"
INSTALL_DIR="${LEGWORK_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *)
    echo "legwork: unsupported OS: $os (linux and darwin only)" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *)
    echo "legwork: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

url="https://github.com/$REPO/releases/latest/download/legwork_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$tmp/legwork.tar.gz"
tar -xzf "$tmp/legwork.tar.gz" -C "$tmp" legwork

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/legwork" "$INSTALL_DIR/legwork"

echo "Installed $("$INSTALL_DIR/legwork" --version) to $INSTALL_DIR/legwork"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "NOTE: $INSTALL_DIR is not on your PATH" >&2 ;;
esac
