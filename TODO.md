# TODO

Open work only. When an item is fixed, delete it from this file in the same commit (the
commit message names its ID). Undecided ideas live in `ideas.md`.

Last full scan: 2026-11-07 at `26287ec`. `go build`, `go vet` and `go test -race ./...` pass.
Effort: XS < 30 min · S ≈ 1–2 h · M ≈ half a day · L ≈ a day or more.

## Rules for the work

- One ID per commit, with a test that fails before the fix.
- Every `go test` gets `-timeout` (mutation checks `30s`, restore files in `try/finally`).
- Live and pty tests get a hard deadline: poll for exit, then `SIGKILL`. A killed run is a
  failure, never a pass.
- `go test -race ./...` for anything that touches parallel tools, the TUI or shared maps.

## Order


5. B13, then the rest of the cleanup
6. Evals E1–E7, then F1 (new system prompt), then F2, F3

## Security


## Bugs

- **B13 · S · `/undo` destroys manual edits.** `internal/tools/undo.go` writes the old bytes
  back without checking the file and with `0644`. → Store the hash of what the tool wrote;
  refuse when the file differs (`/undo force` overrides); keep the file's mode.

## Cleanup

| ID | Effort | Where | Problem → fix |
|---|---|---|---|
| C1 | XS | `config` → `agent/budget.go` | The `safety_margin` setting is never used; the budget uses the constant `ctxSafetyTok`. → Pass the setting through. |
| C2 | XS | `llm/client.go` | `headerTimeout` is unused. → Use it as `ResponseHeaderTimeout`, or delete it. |
| C3 | XS | `config/settings.go` | `reserve_output` and `max_tokens` set the same value. → Keep one. |
| C5 | S | `agent/budget.go` `trimHistory` | Blanks results without storing them and may blank the newest ones. → Store first; never touch the last `keepResults`. |
| C6 | S | `agent/compact.go` `transcriptOf` | The summary request has no total cap and can overflow the window. → Cap at ~40% of the window, dropping the oldest lines first. |
| C7 | XS | `agent/commands.go` `/compact` | Runs on `context.Background()` and always reports "tool results shortened". → Take the caller's ctx; report what happened. |
| C8 | S | `llm/client.go` | The 2-minute silence limit is fixed; plain llama-server can stay silent longer on a cold prompt. → A `stream_idle_sec` setting. |
| C11 | XS | `config/config.go` | Session ids have one-second resolution, so two runs can collide. → Add milliseconds or a random suffix. |
| C12 | S | `session` | A session does not record its root, so `--continue` in another project mixes them. → A header line `{root, model, created}`; refuse or ask on a mismatch. |
| C13 | S | `audit` `matching` | Every history read parses every month. → Newest first; stop when the limit is filled. |
| C14 | XS | `agent/agent.go` `projectWorkRe` | Matches everyday words (`add`, `read`, `file`). → Narrow it, or drop it after F1; measure. |

## Evals (`cmd/eval`)

- **E1 · XS** ykode's own flags only work after `--`. → One flag set; document `make eval ARGS=…`.
- **E2 · M** Only `get-the-old-result-back` runs under context pressure. → Add at least a
  stale-read task (catches B1) and a long task that compacts and needs the exact original
  wording (catches B8). Report shrinks, compactions, recalls and repeated commands.
- **E3 · M** One run per task at temperature 0.7, with no comparison. → `--repeat N`; save
  each run to `~/.ykode/evals/<time>.json` with model, commit and effective settings;
  `--baseline last` marks regressions.
- **E4 · S** The eval loads the user's `settings.json`. → Start from built-in defaults;
  `--use-settings` opts back in.
- **E5 · S** Checks that pass wrongly: `count-matches` accepts any answer containing "7";
  `refuse-outside` passes without the model ever trying the read. → Whole-number match;
  require an attempted and refused call.
- **E6 · S** Not measured yet: tool calls written as text, re-reads of unchanged ranges,
  answer length, verify nudges, recall vs re-run, max-turn stops. → Count them per task.
- **E7 · M** Every task is a toy. → A cross-package rename checked with `go build ./...`, a
  "where is X configured" task in 50+ files with decoys, a bug whose cause is in another
  file, and a two-prompt session.
- **E8 · XS** `read-the-note` fails since S3: its check wants the note to beat the code, which
  is the rule S3 removed from the prompt. → decide what the task measures: "names the file that
  states the rule", or "follows a note over the user's question"; the current prompt supports
  the first, not the second.

## Features

- **F1 · S** Adopt the system prompt below, behind a flag at first; compare with E3 on
  Qwen3.6-35B-A3B.
- **F2 · M** OS sandbox for `exec_command` (`sandbox-exec` on macOS, `bwrap` on Linux): writes
  only inside the root, no network, opt-in.
- **F3 · S** Per-model sampling in `settings.json` (temperature, top_p, top_k, min_p,
  presence_penalty), separate for thinking and no-think. Qwen loops are better fixed here
  than with more prompt text.
- **Later:** explore sub-agent, MCP, goreleaser/Homebrew, hash-chained audit log. Image
  input and the rest are in `ideas.md`.

## System prompt draft (for F1)

Targets, most used first: Qwen3.6-35B-A3B (MoE, ~3B active, needs the explicit order and the
anti-loop rules most), Qwen3.8-27B (dense), Qwen3.8-Flash-Next (256K context; slow cold
prefill on Strata, so the prompt must stay cache-friendly).

Design notes:

- No list of tools: Qwen's template already renders every tool spec into the system message.
  Rules about a single tool belong in its description.
- The fixed text comes first and the per-session values last, so the longest possible
  prefix is shared across sessions. Built once and never changed mid-session (B14).
- No "think step by step": the thinking mode already does it. The prompt bounds exploration
  instead.
- An explicit reply format prevents Markdown recaps. "Never write a tool call as text" stops
  `<tool_call>` blocks from appearing in answers.
- About 550 tokens. Drop any rule the evals show is not earning its place.

`{{…}}` is filled in once at session start; a section whose value is empty is left out,
tags included. `{{date}}` is the date only (a time would change the prompt). `{{git}}` is the
branch and whether the tree is dirty, or `not a git repository`. The instructions are
`~/.ykode/AGENTS.md`, then `<root>/AGENTS.md` or `CLAUDE.md`, each capped at 8 KB.

```text
You are ykode, a coding agent. You work on one project by calling tools, and you reply to the user in plain text.

<workflow>
1. Locate: find_files and search_text. Search before reading whole files.
2. Read: read_file, only the part you need.
3. Change: edit_file for existing files, write_file for new files or full rewrites.
4. Verify: exec_command with the narrowest build or test that covers the change.
Stop exploring as soon as you know enough to act. Do not re-read a file you have already read unless it changed.
</workflow>

<rules>
- If the user asks a question, read what you need and answer it. Change files only when the user asks for a change.
- Do what was asked and nothing more: no unrelated refactors, renames, comments or reformatting.
- When a project file states a rule about the code (a value that must stay, a name to keep), say so in your answer and name the file. The user's request comes first.
- Paths are relative to the project root. A path outside it needs the user's approval for each call.
- Put tool calls that do not depend on each other in the same turn.
- Make tool calls only through the tool interface. Never write a tool call in your reply text.
- When a result says it was cut, follow its hint (an offset, a saved log path) instead of guessing the rest.
- An older result may be replaced by a short summary with an id such as r12. Use recall for command or search output; read files again with read_file.
- If a tool fails, read the error, fix the arguments and retry once. If it fails again, stop and explain what went wrong.
- If the user refuses a tool call, do not repeat it. Find another way, or ask.
- Commands get no input: use non-interactive flags, and never start editors, pagers or servers that do not exit.
- Do not run destructive or outward-facing commands (rm -rf, git push, git reset --hard, git checkout -- ., git clean, package installs, uploads) unless the user asked for them. Do not commit unless asked.
- Never invent file contents, APIs or command output. If you have not read it, read it or say you do not know.
</rules>

<reply>
When the task is done, reply without any tool call:
- what you changed (file names) and how you verified it, or why you could not;
- 1 to 5 short lines of plain text: no headings, no emoji, no step-by-step recap;
- in the language the user wrote in.
</reply>

<environment>
project root: {{root}}
os: {{os}}/{{arch}}
shell for exec_command: sh
date: {{date}}
git: {{git}}
</environment>

<project_instructions path="{{instructions_path}}">
The project's own instructions follow. Follow them unless they conflict with the user's request or with the rules above.
{{instructions}}
</project_instructions>
```

What to compare against the current prompt (with E3 and E6): tasks passed, tool rounds per
task, re-reads of unchanged files, tool calls written as text, answers over 5 lines, and
verify nudges fired.
