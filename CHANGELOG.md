# Changelog

What changed in ykode, newest first, in the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
shape. The release notes are cut from this file: before tagging `vX.Y.Z`, move the `[Unreleased]`
entries under a new `[X.Y.Z] - YYYY-MM-DD` heading and start `[Unreleased]` again, because `cd.yml`
refuses a tag with no section or an empty one rather than publishing notes nobody wrote. No version
has been tagged yet, so everything below describes what `main` does today.

## [Unreleased]

### Added

- The agent loop: a model answers, asks for tools, the loop runs again, and its thinking is shown
  apart from its answer. One streaming path — `/v1/chat/completions` over SSE against an
  OpenAI-compatible server, normally a local `llama-server`, and nothing else.
- Six tools, one file each: `read_file`, `write_file`, `edit_file`, `exec_command`, `plan`,
  `ask_user`. Every path is project-relative and confined to `--root` through one `*os.Root`; reads
  may overlap, writes and commands run one at a time.
- The full-screen front end: transcript, overlays, code colouring, the key list, scrolling without
  grabbing the pointer — and `-p`, the same run with no screen.
- Saved conversations: append-only on disk, resumable byte for byte. `/sessions` picks one and this
  run continues it.
- Budgets measured against the server's context window: room for a reply, a tool-result cut that
  scales with it, folding of older turns (`/compact`), and `/ctx` plus `/usage` to see what the
  window and the server's cache are doing.
- Settings in `~/.ykode/settings.yaml` and a project's `.ykode/settings.yaml`, then `$LOCAL_LLM_URL`
  and `$LOCAL_LLM_MODEL`, then flags. A key nothing reads is refused by name and line, and `/settings`
  lists and edits every knob.
- Permissions: a write or a command is confirmed one at a time, `assume_yes` stops asking about
  writes inside the project, and `permissions.allow` names tools or one file.
- A reply that stops after its thinking, with no tool call and no answer, is dropped and the same
  request is sent again, up to twice for each stall; the history never keeps one, since a model that
  sees such a turn in its history makes more of them.
- Pictures: `read_file` on a png, jpeg, gif or webp sizes it down and sends it to a model that takes
  images.
- Commands: `/help`, `/usage`, `/ctx`, `/compact`, `/sessions`, `/model`, `/think`, `/plan`, `/tools`,
  `/settings`, `/create-local-setting`, `/reset`, `/exit`.
- Build and install: `make build` (one binary, version stamped from `git describe`), `make install`
  and `make uninstall` (`PURGE=1` also removes `~/.ykode`), `sh scripts/install.sh`
  (`--add-to-path`), `make check` as the gate, `make audit` for dependency updates and CVEs.
- A release pipeline: CI runs `make build check`, `govulncheck` and a cross-build of the three
  shipped targets; a `v*` tag publishes `linux/amd64`, `linux/arm64` and `darwin/arm64` binaries with
  `SHA256SUMS` and keyless cosign signatures as a GitHub Release. Dependabot bumps modules and
  Actions weekly, and `make install-hooks` makes git check commit subjects and tag names.

### Changed

- The screen runs on Bubble Tea v2, Lip Gloss v2 and Bubbles v2. Colours live in one palette, a dark
  and a light one, picked from the background the terminal reports when the screen opens; a session
  already on screen is redrawn in the palette that answer chose. In the prompt box, a row the text
  fills to the edge puts the cursor on a row of its own below it.
- `ctrl+enter` starts a new line, as `shift+enter` does, where the terminal speaks the kitty keyboard
  protocol or modifyOtherKeys; elsewhere it still arrives as `enter`. The screen and the server
  screen switch those protocols off before they draw and on the way out, as they do pointer tracking.
- The key list `?` and `ctrl+/` open is grouped — prompt, edit, scroll, session — one row per group
  with its name in a column, wrapped into even rows when the screen is narrower.
- `theme` setting, applied at once: `auto` (the default), `ykode-dark`, `ykode-light`, `terminal`
  (the terminal's sixteen colours), and `dracula`, `nord`, `solarized-dark`, `solarized-light`,
  `gruvbox-dark`, `gruvbox-light`, `catppuccin-mocha`, `catppuccin-latte`, `tokyo-night`, `one-dark`,
  `monokai`, which repaint the terminal's background while the screen is up.

### Removed

- The API key. No request carries an `Authorization` header, and no key is read from a flag, a file
  or the environment: the only address ykode connects to is the one you configure.
