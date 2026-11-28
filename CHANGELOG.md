# Changelog

What changed in ykode, newest first, in the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
shape. The release notes are cut from this file: before tagging `vX.Y.Z`, move the `[Unreleased]`
entries under a new `[X.Y.Z] - YYYY-MM-DD` heading and start `[Unreleased]` again, because `cd.yml`
refuses a tag with no section or an empty one rather than publishing notes nobody wrote.

## [Unreleased]

### Changed

- **The thinking level sets the depth, and the cap is its own setting.** A level now reaches the server
  as `reasoning_effort` (`off` as `none`, still with `enable_thinking: false`), where before it only set a
  token budget and the depth stayed at the server's default. The token cap is the new `thinking_budget`
  (`--thinking-budget`), sent as `reasoning_budget_tokens` and `thinking_budget_tokens`; unset, thinking
  gets what the reply can spare after 1,024 tokens for the answer, where `medium` used to cap it at 8,192.
  A word a model's template refuses is asked again with the nearest one it takes.
- **The context window is read from more servers**: a model list that gives it as `-c N`,
  `--ctx-size=N`, `context_length`, `max_context_length` or `max_model_len` now sets the window, so the
  footer's fullness bar and the folding budget work on routers that list several models.
- **Leaving prints the resume command alone on its last line**, so the whole line can be copied; a project
  path that needs quoting gets single quotes, which keep a `$` or a backtick literal.

### Removed

- **`/settings KEY VALUE`**: a setting is changed on the screen a bare `/settings` opens, which applies it
  to the running session where it can. `/settings list` and `/settings KEY` still print the knobs.

### Fixed

- **A paste goes where the screen shows it**: with a settings, server or list screen open, a paste no longer
  lands in the hidden prompt box; it types into the value, URL or filter being edited instead.
- **The thinking cap does not promise a cap while thinking is off**: the `thinking_budget` row says no cap
  is sent, rather than naming one the request never carries.
- **A server error quoted in full stays readable**: the quote is cut on a whole character, so it can no
  longer end in a broken one.

### Added

- **`ctrl+r` shows commands whole**: a command cut to its row is shown in full under it, line breaks
  kept, until `ctrl+r` folds them back.
- **Code in thinking keeps its shape**: a fenced block the model writes while thinking is drawn line for
  line, indented and with its language named, instead of being run into the prose around it. It stays
  in thinking's colours: coloured code belongs to the answer.
- **A long paste is one token in the box**: past 10 lines or 1,000 bytes a paste becomes
  `[Pasted text #1 +42 lines]`, so the box stays quick; the model receives the whole text.

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
