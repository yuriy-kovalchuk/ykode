# Changelog

What changed in ykode, newest first, in the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
shape. The release notes are cut from this file: before tagging `vX.Y.Z`, move the `[Unreleased]`
entries under a new `[X.Y.Z] - YYYY-MM-DD` heading and start `[Unreleased]` again, because `cd.yml`
refuses a tag with no section or an empty one rather than publishing notes nobody wrote.

## [Unreleased]

## [0.1.0] - 2026-11-21

The first release of ykode, an experimental terminal coding agent for local inference engines,
written in Go as a learning project.

### Added

- **The agent loop** over one streaming path: `/v1/chat/completions` with tool calls over SSE, against
  `llama-server` or another OpenAI-compatible local server. No provider SDK, no API key, no telemetry.
- **Six tools**: `read_file`, `write_file`, `edit_file`, `exec_command`, `plan` and `ask_user`. File
  access is confined to the project root, and writes and commands are confirmed unless allowed.
- **A full-screen terminal UI** with themes, code colouring and markdown, plus `-p TASK` for one task
  with no screen.
- **Context management for small windows**: budgets measured against the server's window, tool output
  cut to fit, older turns folded into a summary (`/compact`), and byte-stable requests so the server's
  prompt cache keeps hitting.
- **Saved conversations** that resume exactly as they were sent (`--continue`, `/sessions`).
- **Settings** in a global and a per-project file, edited from the `/settings` screen.
- **Pictures** for vision models: `read_file` on an image, `@path` in an ask, and `ctrl+v` from the
  clipboard.
- **llama.cpp extras**: the window read from `/props`, prompt reading progress, and the server's cache
  saved and restored across runs (`save_server_cache`).
- **One static binary** for Linux (amd64, arm64) and macOS (arm64), with `SHA256SUMS` and cosign
  signatures on every release.

[Unreleased]: https://github.com/yuriy-kovalchuk/ykode/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/yuriy-kovalchuk/ykode/releases/tag/v0.1.0
