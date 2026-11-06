#!/bin/sh
# install.sh builds ykode from the repo and puts it in ~/.local/bin (override with YKODE_INSTALL_DIR).
set -eu

REPO=${YKODE_REPO:-https://github.com/yuriy-kovalchuk/ykode}
DEST=${YKODE_INSTALL_DIR:-$HOME/.local/bin}

command -v go >/dev/null || { echo "ykode needs the Go toolchain to build" >&2; exit 1; }
command -v git >/dev/null || { echo "ykode's installer needs git" >&2; exit 1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

git clone --depth 1 "$REPO" "$tmp/ykode"
cd "$tmp/ykode"
version=$(git describe --tags --always 2>/dev/null || echo dev)
CGO_ENABLED=0 go build -ldflags "-X main.version=$version" -o "$tmp/ykode.bin" ./cmd/ykode

mkdir -p "$DEST"
install -m 0755 "$tmp/ykode.bin" "$DEST/ykode"
echo "installed $DEST/ykode ($version); make sure $DEST is on your PATH"
