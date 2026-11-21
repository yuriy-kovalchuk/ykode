# Installing ykode

You need a unix system and Go 1.27 or newer. No Windows port is planned: a command runs in its own
process group so that cancelling it takes its children too, which is a `setpgid` and `kill(-pid)`
thing.

## From source

```sh
git clone https://github.com/yuriy-kovalchuk/ykode && cd ykode
make build    # bin/ykode: CGO off, -trimpath, version stamped from git describe
make install  # into ~/.local/bin
ykode --version
```

`sh scripts/install.sh` clones and installs without cloning by hand (`--add-to-path` appends the
PATH line). `make uninstall` takes `PURGE=1` to remove `~/.ykode` as well.

## A release binary

A tagged release needs no toolchain at all: it publishes the three cross-built binaries —
`ykode-linux-amd64` (Linux x86-64), `ykode-linux-arm64` (Linux ARM64) and `ykode-darwin-arm64` (Apple
silicon) — with `SHA256SUMS` and a cosign bundle per binary (`.sigstore.json`), on the Releases
page. Checking a download you did not build yourself:

```sh
cosign verify-blob ykode-darwin-arm64-v0.1.0 \
  --bundle ykode-darwin-arm64-v0.1.0.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/yuriy-kovalchuk/ykode/\.github/workflows/cd\.yml@refs/tags/v'
```

## The server

The default server is `http://127.0.0.1:8080/v1`, which is what `llama-server --host 127.0.0.1
--port 8080` answers. That server needs `--jinja` before it takes a tool call, and `--mmproj` with a
vision encoder before it can see a picture. ykode reads the window from `/models`, and from llama.cpp's
`/props` when the server lists one model, so a slot's real share is what it budgets for. `--ctx` overrides
both, for a server that reports no window or one that reports it wrong. A small window is the one hard limit, because everything
ykode budgets (the room for a reply, the tool-result cut, folding and compaction) is measured against
it: 16k tokens is the practical floor for a coding session, 32k or more is comfortable.

To keep the server's cache across restarts, see [the server's cache across runs](settings.md#the-servers-cache-across-runs).
