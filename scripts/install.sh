#!/usr/bin/env sh
# hookwise installer — downloads the latest released binary for your platform.
#
#   curl -fsSL https://raw.githubusercontent.com/vishnujayvel/hookwise/main/scripts/install.sh | sh
#
# Env overrides:
#   HOOKWISE_VERSION   pin a version (e.g. v0.1.0); default: latest release
#   HOOKWISE_BIN_DIR   install dir; default: ~/.local/bin
set -eu

REPO="vishnujayvel/hookwise"
BIN_DIR="${HOOKWISE_BIN_DIR:-$HOME/.local/bin}"

err() { printf 'error: %s\n' "$1" >&2; exit 1; }

# --- detect platform -------------------------------------------------------
os="$(uname -s)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) err "unsupported OS: $os (only darwin and linux have prebuilt binaries)" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) err "unsupported arch: $arch" ;;
esac

# --- resolve version -------------------------------------------------------
tag="${HOOKWISE_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name":' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "$tag" ] || err "could not determine latest release tag (no published release yet?)"
fi
version="${tag#v}"

archive="hookwise_${version}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$tag/$archive"

# --- download + extract ----------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

printf 'Downloading hookwise %s (%s/%s)...\n' "$tag" "$os" "$arch"
curl -fsSL "$url" -o "$tmp/$archive" || err "download failed: $url"

# Best-effort checksum verification (non-fatal if tooling is absent).
if curl -fsSL "https://github.com/$REPO/releases/download/$tag/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then sumcmd="sha256sum";
  elif command -v shasum >/dev/null 2>&1; then sumcmd="shasum -a 256";
  else sumcmd=""; fi
  if [ -n "$sumcmd" ]; then
    want="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
    got="$(cd "$tmp" && $sumcmd "$archive" | awk '{print $1}')"
    [ -z "$want" ] || [ "$want" = "$got" ] || err "checksum mismatch for $archive"
  fi
fi

tar -xzf "$tmp/$archive" -C "$tmp" hookwise || err "failed to extract hookwise from $archive"

# --- install ---------------------------------------------------------------
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/hookwise" "$BIN_DIR/hookwise" 2>/dev/null \
  || { cp "$tmp/hookwise" "$BIN_DIR/hookwise" && chmod 0755 "$BIN_DIR/hookwise"; }

printf 'Installed hookwise to %s/hookwise\n' "$BIN_DIR"

case ":$PATH:" in
  *":$BIN_DIR:"*) "$BIN_DIR/hookwise" --version ;;
  # shellcheck disable=SC2016 # $PATH is intentionally literal in the printed hint
  *) printf '\nNote: %s is not on your PATH. Add it:\n  export PATH="%s:$PATH"\n' "$BIN_DIR" "$BIN_DIR" ;;
esac
