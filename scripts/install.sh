#!/bin/sh
# install.sh builds ykode from the repo and puts it in ~/.local/bin (override with YKODE_INSTALL_DIR).
# Pass --add-to-path to have the PATH line appended to your shell's startup file; without it the
# exact line is printed for you to paste.
set -eu

REPO=${YKODE_REPO:-https://github.com/yuriy-kovalchuk/ykode}
DEST=${YKODE_INSTALL_DIR:-$HOME/.local/bin}
ADD_TO_PATH=0

for arg in "$@"; do
	case "$arg" in
	--add-to-path) ADD_TO_PATH=1 ;;
	*) echo "ykode install: unknown argument $arg (only --add-to-path)" >&2; exit 2 ;;
	esac
done

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
echo "installed $DEST/ykode ($version)"
echo "settings and sessions live in ${YKODE_HOME:-$HOME/.ykode}, written on the first run"

# Which startup file, and which line, depends on the shell that asked for this.
shell=${SHELL:-/bin/sh}
shell=${shell##*/}
case "$shell" in
zsh) rc=$HOME/.zshrc; line="export PATH=\"$DEST:\$PATH\"" ;;
bash) rc=$HOME/.bashrc; line="export PATH=\"$DEST:\$PATH\"" ;;
fish) rc=$HOME/.config/fish/config.fish; line="fish_add_path \"$DEST\"" ;;
*) rc=$HOME/.profile; line="export PATH=\"$DEST:\$PATH\"" ;;
esac

case ":$PATH:" in
*":$DEST:"*) exit 0 ;; # already reachable, nothing to say
esac

echo "$DEST is not on your PATH."
if [ "$ADD_TO_PATH" -eq 0 ]; then
	echo "add it to $rc:"
	echo "    $line"
	exit 0
fi

mkdir -p "$(dirname "$rc")"
if grep -qF -- "$line" "$rc" 2>/dev/null; then
	echo "$rc already has that line; left it alone"
	exit 0
fi
printf '%s\n' "$line" >>"$rc"
echo "added it to $rc; open a new shell, or source it"
