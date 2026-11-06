# TODO

Collected from a review against pi (a mature agent). ykode is a POC: the goal is
to stay small and tuned for local models, not to reach feature parity.

## Roadmap to the TUI

Rule: before the TUI, build what changes the agent core, its data, or the
interface between the agent and the screen. Anything that is only about display waits.

| Step | Items | Why now |
|---|---|---|
| 1 | All of **Fixes**, project instructions (feature 3) | Quick wins; Strata needs the timeout fix. |
| 2 | **Installation** + `~/.ykode` + settings file (feature 2) | Sessions, logs, the allowlist and settings all need a home. |
| 3 | **Event stream** (feature 1) + **non-interactive mode / evals** (feature 4) | The real prerequisite for a TUI; the evals catch what the refactor breaks. |
| 4 | **Saving context** (history shrinker + `recall`) + **compaction** (feature 6) | They change history and tool results; measured with the evals. |
| 5 | **Sessions** (5), **audit log** + `/history`, **permissions** (7), **`/undo`** (8) | Core state the TUI will only display. |
| 6 | **Library swaps** | Easier while the code is small and before the TUI depends on the diff output. |
| 7 | **TUI** (9), then queued messages (10) and runtime commands (11) | Built once, on top of the above. |

Independent of the UI, any time: parallel read-only tools (12), the `exec_command` sandbox (13).

## Fixes

- [ ] **Count tool specs in the context budget.** `promptTokens` (`internal/agent/budget.go`)
      only counts history; the 7 tool schemas (~1–1.5k tokens) are not counted against a
      256-token safety margin, so a nearly full context can overflow the server.
- [ ] **Trim in one big step, not one message per turn.** `trimHistory` stops as soon as the
      estimate drops below 75%, so near the limit every turn blanks one more old result and
      breaks llama.cpp's prompt cache from that point on. Once over the threshold, trim down
      to ~50%.
- [ ] **Use real token counts.** Calibrate the estimate with `Timings.PromptTokens()` from
      the last turn plus an estimate for the new messages, instead of bytes/3 every turn.
- [ ] **Replace the 10-minute `http.Client.Timeout`.** It covers the whole stream and cuts
      long generations. Use a timeout for the response headers plus an idle timeout
      (no bytes for N seconds), and cancel through `ctx`. Urgent for Strata: Flash-Next
      reads an uncached prompt at ~100 tok/s, so a 60k-token cold prompt alone takes ~10 min.
      Strata sends `: keep-alive` comments while it reads the prompt, so an idle timeout works.
- [ ] **Make the "verify with a tool" nudge optional.** The `projectWorkWords` substring
      check matches "address", "profile", etc. Put it behind a flag, or match whole words.
- [ ] **API key support.** Send `Authorization: Bearer $LOCAL_LLM_API_KEY` when it is set.
- [ ] **Explain the shell gap in the README.** File tools are confined by `os.Root`;
      `exec_command` is not (`Exec.Targets` returns nil), and `--yes` makes it unprompted.
- [ ] **Per-model setting to send reasoning back** for chat templates that expect thinking
      from earlier tool-calling turns.

## Features

1. [ ] **Agent emits events instead of printing.** `internal/agent` writes to the terminal
       itself (`fmt.Fprintf(a.log, …)`) and confirmation is a blocking `ConfirmFunc`
       callback. Replace both with a typed event stream: turn started, reasoning/content
       delta, tool started/finished, confirmation requested (answered via a reply channel),
       stats, error. The current CLI becomes one consumer of those events; non-interactive
       mode and the TUI are the next two. Leave room for several tools running at once and
       for user messages arriving mid-run.
2. [ ] **Installation and the `~/.ykode` folder.** See [Installation](#installation).
3. [ ] **Load project instructions**: `AGENTS.md` / `CLAUDE.md` from the root into the
       system prompt.
4. [ ] **Non-interactive mode + eval set**: `ykode -p "task" --yes --json`, and a dozen
       scripted tasks on fixture repos that report pass/fail, turns and tokens. Use it to
       tune the prompt and tool descriptions with numbers. `--json` prints the events from
       feature 1 as JSON lines.
5. [ ] **Session persistence**: append-only JSONL under `~/.ykode/sessions/`, `--continue`.
6. [ ] **Real compaction**: at the threshold, ask the model for a structured summary
       (goal, files touched, decisions, next step) and replace older turns with it;
       `/compact` command.
7. [ ] **"Always allow" on confirmation**: `[y/N/a]` — allow this tool or this command
       prefix (e.g. `go test`) for the rest of the session; a permanent allowlist in
       `~/.ykode/settings.json`.
8. [ ] **`/undo`**: keep a stack of pre-edit contents (edit/write already read them), or a
       git stash/checkpoint before each request.
9. [ ] **TUI.** Input box with history and multi-line paste, streamed thinking/answer,
       tool calls as collapsible rows, confirmation dialog, status bar (model, context use,
       cache hit rate, tok/s). Candidate: Bubble Tea. Built on feature 1's events.
10. [ ] **Queue messages typed while the agent works** (steering / follow-up). Needs the TUI.
11. [ ] **Runtime commands**: `/model`, `/ctx` (token usage, cache hit rate), `/tools`,
        `/think on|off`. Show speculative-decoding acceptance from Strata's
        `timings.draft_n` / `draft_n_accepted` in the stats. Display only: build them in
        the TUI.
12. [ ] **Run read-only tools in parallel** (`read_file`, `search_text`, `find_files`,
        `list_dir`) when several arrive in one turn. Independent of the UI.
13. [ ] **Optional OS sandbox for `exec_command`**: `sandbox-exec` on macOS, `bwrap` on
        Linux; writes only inside the root, network off. Independent of the UI.
14. [ ] **Later, only when needed**: explore sub-agent, image input for vision models, MCP.

~~Line editing (readline)~~ — dropped: the TUI's input box replaces it.

## Installation

Goal: one command builds ykode, puts it on the `PATH`, and creates `~/.ykode`.

- [ ] **`make install`** (and a `scripts/install.sh` that does the same for people without
      make):
      - build with `go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)"`;
      - install the binary to `$PREFIX/bin` (default `~/.local/bin`, overridable with
        `PREFIX=...`), no `sudo`;
      - if that directory is not on `PATH`, print the exact line to add for the user's shell
        (`zsh`, `bash`, `fish`) instead of editing their rc file silently; an opt-in
        `--add-to-path` flag may append it, once, to the right rc file;
      - create `~/.ykode` (mode `0700`) and its layout below, without overwriting anything
        that is already there;
      - print the installed version and where everything went.
- [ ] **`make uninstall`**: remove the binary; leave `~/.ykode` unless `PURGE=1`.
- [ ] **`ykode --version`** (from the `-X main.version` above).
- [ ] **Folder layout** (`$YKODE_HOME` overrides `~/.ykode`):

      ```
      ~/.ykode/
        settings.json   # user settings, written with the defaults on first install (JSON has no comments)
        sessions/       # JSONL sessions (feature 5)
        logs/           # spilled exec_command output, instead of os.TempDir()
        audit/          # tool-call audit log, YYYY-MM.jsonl
        AGENTS.md       # optional global instructions, loaded before the project's (feature 3)
      ```

- [ ] **Settings file** `~/.ykode/settings.json`, plus an optional per-project
      `<root>/.ykode/settings.json`. Precedence, highest first: flag → env var → project
      settings → user settings → built-in default. Holds `base_url`, `model`, `api_key`
      (or better `api_key_env`, the name of an env var), `max_tokens`, `max_turns`,
      `think`, the permission allowlist (feature 7), and per-model overrides (e.g. send
      reasoning back, context size). Reject unknown keys with a clear error.
      - Create the file with `0600`, since it may hold an API key.
      - `ykode` also creates `~/.ykode` on first run if it is missing, so the binary works
        without `make install` (e.g. after `go install`).
- [ ] **Move spill logs** from `os.TempDir()` to `~/.ykode/logs/`, keeping the 24 h prune.
- [ ] **README**: an Install section (`make install`, `go install …@latest`, PATH hint,
      folder layout, settings precedence).
- [ ] Later: release binaries (goreleaser) and a Homebrew tap.

## Saving context (cheap / small models)

Biggest wins first. Measure each with the eval set (feature 4).

- [ ] **Drop line numbers from `read_file`.** `edit_file` matches by text, not line number,
      so the `%4d\t` prefix costs ~2–3 tokens per line for nothing, and small models
      sometimes copy it into `old_text`. Keep a header like `lines 120-240 of 900`.
- [ ] **Smaller default read window**: 400 → ~150 lines, pushing the model to
      `search_text` first.
- [ ] **Outline mode**: `read_file` with `outline: true` (or a `file_outline` tool) returns
      only declarations with line ranges — `go/ast` for Go, a regex/ctags fallback for others.
- [ ] **History shrinker: move old tool results out of the history, reversibly.** One module
      that replaces `trimHistory` (which throws data away today). Runs only when the context
      crosses the threshold, in one batch (down to ~50%), so the prompt cache breaks once.
      - **Every result is seen once, in full.** Only *older* results are replaced by a
        placeholder.
      - **Keyed by a unique call ID** (`r12`), never by the command: `go test ./...` runs many
        times with different output. The command is the label. The audit log uses the same
        ID (see [Audit log](#audit-log)).
      - **Command output** (`exec_command`, `search_text`, `find_files`, `list_dir`) goes to
        `~/.ykode/sessions/<session>/results/r12.txt`. The history keeps a summary built by
        code, not by the model, e.g.
        ``[r12] exec `go test ./...` → exit 1 · 8.2 KB · FAIL TestParse, TestRender · recall("r12")``
        (exit code, size, and the lines matching `FAIL`/`error`/`panic`).
      - **File reads are not stored**: the file is the source of truth, and a stored copy goes
        stale after the next edit. Placeholder: `read_file main.go lines 1-200 (removed;
        re-read if needed)`. When a file is re-read or edited, older reads of it get this
        placeholder first.
      - **Unchanged re-reads**: if the same range of an unchanged file is read again, answer
        `(unchanged since your read in r7)` instead of the text.
      - **Old tool-call arguments too**: `write_file.content` and `edit_file` texts live in
        the assistant's tool calls; replace them with `(N bytes written, see r9)`.
      - **`recall(id, pattern?, offset?, limit?)` tool**: returns a stored output, or only the
        lines matching `pattern` with a little context. A dedicated tool, because the store
        is outside the project root and `read_file` would ask for approval every time.
        Costs ~80 tokens of tool spec per request.
      - Stored results live with the session, so `--continue` restores the IDs.
      - **Measure with the evals**: how often the model calls `recall`, how often it re-runs a
        command instead, and how often a task fails after an eviction. If small models
        ignore `recall`, invest in better summaries rather than the tool.
- [ ] **Clean `exec_command` output**: strip ANSI codes, keep only the last frame of `\r`
      progress lines, collapse repeated lines (`[previous line repeated 40×]`).
- [ ] **Scale result budgets with the context window.** 12 KB per result (~3.5k tokens) is a
      lot in a 32k window; derive `maxResultBytes` from `--ctx` (e.g. ctx/10).
- [ ] **`search_text` output modes**: `files` (paths only) and `count`, a per-file match cap,
      and group matches under one file header instead of repeating the path.
- [ ] **Fewer, shorter tool specs**: merge `list_dir` into `find_files` (depth 1), shorten
      descriptions; measure spec size and print it at startup.
- [ ] **Keep the prompt prefix stable**: never change the system prompt or tool order
      mid-session; append notes as new messages. (llama-server: `--cache-reuse 256` lets the
      cache survive small shifts.)
- [ ] **Explore sub-agent (later)**: run a throwaway sub-conversation for "find where X
      happens" that returns only the conclusion. Costs time locally, saves main context.

## Audit log

Every tool call the agent makes is recorded, for auditing and for review with a command.
Separate from the session history: never trimmed, compacted or rewritten, it survives
`/reset`, and it records calls that were refused or failed.

- [ ] **Append-only JSONL** at `~/.ykode/audit/YYYY-MM.jsonl` (one file per month, mode
      `0600`, folder `0700`), one line per tool call written *before* the tool runs
      (`status: "started"`) and one when it ends, so a crash still leaves a trace. Fields:
      - `time`, `duration_ms`, `id` (the same `r12` as the history shrinker), `session`,
        `turn`, `root`, `model`;
      - `tool`, `args` (as the model sent them), resolved target paths, `outside_root`;
      - `decision`: `auto` (no confirmation needed), `yes_flag` (`--yes`), `allowlist` (which
        rule), `user_yes`, `user_no`;
      - `status`: `ok`, `error`, `refused`, `cancelled`, `timeout`; `exit_code` for commands;
      - `result_bytes`, `result_sha256`, and the path of the stored output when there is one;
      - for `edit_file`/`write_file`: the diff (capped), which also feeds `/undo`.
- [ ] **Redaction**: before writing, mask values that look like secrets (`*_KEY=`,
      `*_TOKEN=`, `Authorization:`, `-----BEGIN … PRIVATE KEY-----`) in args and diffs.
- [ ] **`/history` in the session**: this session's tool calls as a numbered table (time,
      tool, short args, decision, status, duration). Filters: `/history exec`,
      `/history --failed`, `/history --all` (every session for this root).
      `/history r12` shows one call in full, including its output if it was stored.
      In the TUI, the same thing as a panel.
- [ ] **`ykode history` subcommand** for use outside a session, with the same filters plus
      `--since`, `--root` and `--json`, e.g. `ykode history --since 24h exec`.
- [ ] **Retention**: 6 months by default (`audit_retention_days` in settings), pruned at
      startup like the spill logs. Never delete the current month.
- [ ] **Setting to turn it off** (`audit: false`); on by default.
- [ ] Later: chain each line with the previous line's hash so edits to the log are
      detectable.

## Libraries to adopt

Rule: use a well-known library when it replaces our own code and pulls in little or
nothing. Stay on the standard library for `internal/llm`: the tolerance hacks
(string-wrapped arguments, missing ids, `reasoning_content`, `timings`,
`chat_template_kwargs`) would fight an SDK such as openai-go.

Checked with `go get` on 2026-11-06 (then reverted):

- [ ] **`github.com/aymanbagabas/go-udiff` v0.4.1** (no dependencies) — replace the
      hand-written diff in `internal/tools/diff.go`: `editScript`, `renderHunks`,
      `hunkRanges` (~110 of its 155 lines). It uses the Myers algorithm instead of an
      O(n·m) table.
      - Use `udiff.Strings` + `udiff.ToUnifiedDiff(..., 3)` and drop the `---`/`+++` header lines.
      - Keep our wrappers: the "large change" summary, `capDiff`, and turning CRLF into LF before diffing.
      - `write_file` still needs a line count (`diffSplit`); keep it as a small helper.
      - Keep the existing `diff_test.go` tests as they are; they must still pass.
- [ ] **`github.com/bmatcuk/doublestar/v4` v4.10.4** (no dependencies) — replace
      `internal/tools/glob.go` (`globRegexp`) and `compileGlob` in `find_files.go`.
      - Keep today's behaviour: a pattern without `/` matches the base name at any depth.
        A pattern with `/` matches from any directory boundary: try
        `doublestar.Match("**/"+p, path)`.
      - Reject bad patterns up front with `doublestar.ValidatePattern`.
      - `search_text`'s Go fallback uses the same matcher.
- [ ] **`github.com/sabhiram/go-gitignore`** (no dependencies, last release 2021) — replace
      the rule parsing in `internal/tools/gitignore.go`. Keep `ignoreSet`, one compiled
      `*GitIgnore` per directory, because nested `.gitignore` files are our job.
      - The deepest `.gitignore` with an opinion wins. `MatchesPathHow` returns the matching
        pattern (nil = no opinion), which tells "not matched" apart from "un-ignored by `!`".
      - Patterns ending in `/` only match directories: pass a directory's path with a trailing
        `/`, and check that a file with the same name is not ignored.
      - Run the cases in `walk_test.go` unchanged; they are the spec.
      - Fallback if it fails those cases: go-git's `plumbing/format/gitignore`. It handles
        nested directories and `isDir` correctly, but pulls in `go-billy`.
      - Bigger option: require `rg`/`fd` and delete the pure-Go fallback (walk, gitignore and
        glob, ~280 lines plus tests).
- [ ] **`github.com/invopop/jsonschema` v0.14.0** — generate tool schemas from the input
      structs, so the schema and the decoder can't drift. **Heaviest of the four:** it pulls
      in 4 dependencies (`bahlo/generic-list-go`, `buger/jsonparser`, `pb33f/ordered-map/v2`,
      `go.yaml.in/yaml/v4` at a release candidate). Decide whether that is worth it before
      adopting.
      - Reflector: `DoNotReference`, `ExpandedStruct`, `AllowAdditionalProperties`,
        `RequiredFromJSONSchemaTags`. Clear `$schema` and `$id`; small models don't need them.
      - Put descriptions in `jsonschema_description` tags (commas inside `jsonschema:"..."`
        must be escaped).
      - Defaults and limits come from Go constants (`defaultReadLimit`, `maxExecTimeout`, …).
        Set them in a `JSONSchemaExtend` method, not as literal numbers in tags, so they stay
        in sync. Use `default`/`maximum` keywords; llama.cpp turns integer `maximum` into
        its grammar.
      - `edit_file`'s input struct has alias fields (`old_string`, `new_string`) that the
        schema must not advertise, and `edits` is a `json.RawMessage`. Give it a separate
        schema-only struct.
      - Print the schema size before and after; the generated one must not be larger.
- [ ] `github.com/chzyer/readline` (or `ergochat/readline`) — for line editing; later
      Bubble Tea if a real TUI is ever wanted.
- [ ] Rule for later additions: one dependency per real gain. Before adding, check
      `go mod graph` for what it pulls in transitively.
