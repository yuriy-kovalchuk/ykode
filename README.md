# ykode

A small terminal coding agent that talks to an OpenAI-compatible chat server —
normally a local [llama.cpp](https://github.com/ggml-org/llama.cpp) `llama-server`.
Go standard library only: no SDK, one streaming path, and the model's thinking is
shown separately from its answer.

It keeps a conversation, calls tools (find, search, read, write, edit, run), and
confines file access to the project root it started in.

## Requirements

- Go 1.25 or newer.
- A server speaking `/v1/chat/completions` with tool calling over SSE. For
  `llama-server`, tool calling needs `--jinja`.
- Optional: `rg` and `fd`. `search_text` and `find_files` use them when installed
  (faster, `.gitignore`-aware) and fall back to pure Go when they are not.

## Build and run

```
make build
./bin/ykode --root ~/code/project
make run ARGS="--base-url https://host/v1 --model Qwen3.6-35B-A3B-GSQ-hybrid"
```

## Options

| flag | env | default | meaning |
|---|---|---|---|
| `--base-url` | `LOCAL_LLM_URL` | `http://127.0.0.1:8080/v1` | server base, `/v1` included |
| `--model` | `LOCAL_LLM_MODEL` | first id from `/models` | model id |
| `--root` | — | current directory | project root the file tools live inside |
| `--ctx` | — | read from the server | context window in tokens |
| `--max-tokens` | — | `4096` | per-response cap, shrunk to fit the window |
| `--max-turns` | — | `25` | tool rounds per request |
| `--no-think` | — | thinking on | skips the thinking phase (`chat_template_kwargs`) |
| `--yes` | — | off | skip in-project confirmations |
| `-p TASK` | — | — | answer one task and exit (pipeable; add `--json` for event lines) |
| `--continue ID` | — | — | resume a saved conversation: `last`, or an id like `s20260102-150405` |
| `--tui` | — | off | full-screen session: scrolling transcript, input box, status bar |
| `--version` | — | — | print the version and exit |

## In the session

Type a request. `/help` lists the commands, `/usage` reports what the
conversation has cost, `/compact` folds the older turns into a summary the model
writes, `/history [what]` shows what this run has done (a tool name, `failed`,
`outside`, one call by id like `r12`, or `--all` for every run of this root),
`/undo` puts the most recent file change back, `/reset` clears the history,
`/exit` leaves. `Ctrl-C` during a reply cancels that request and
returns to the prompt.

## The full-screen session

`--tui` replaces the line prompt with an alternate-screen session: the
transcript scrolls, the thinking and the answer arrive as one block each, and
one line at the bottom reports what the session costs.

```
ykode --tui                      # or --tui --continue last
```

| key | what it does |
|---|---|
| `enter` | sends the prompt |
| `ctrl+j`, `alt+enter`, `shift+enter` | starts a new line; a paste keeps its own newlines |
| `up`, `down` | walks the prompts you sent this session (with an empty box) |
| `esc`, `ctrl+c` | stops the running turn; `ctrl+c` twice leaves |
| `ctrl+o` | shows and hides the result text of each tool call |
| `pgup`, `ctrl+u`, `pgdown`, `ctrl+d` | move the transcript |
| `y`, `a`, `n` | answers a question about a change: yes, always, no — `n` is the default |

Typing while the model works is allowed: the prompt is queued and goes in the
moment the turn ends. The commands of `In the session` work there too, except
that `/reset`, `/compact` and `/undo` wait for the running turn rather than
racing it.

The bottom line reads `model │ ctx used/total % │ cache % │ tok/s`, and adds
`working` (and how many prompts are queued) while a turn runs. The cache
percentage is llama.cpp's: how much of the request it already had. `--tui` and
`-p`/`--json` are mutually exclusive — one draws a screen, the other writes
stdout.

## Tools

`find_files`, `search_text`, `read_file`, `write_file`, `edit_file`,
`exec_command`, and `recall` — which only appears once a result has actually been
shortened, because a tool description costs tokens in every request.
`find_files` lists one directory with `depth: 1`;
`read_file` answers with `outline: true` when a big file only needs its shape.
Every truncated result says how to continue — a `read_file`
window reports the next `offset`, a cut `exec_command` log reports the saved file.
`edit_file` takes several `edits[]` in one call and matches them atomically, so a
bad pair never applies half an edit. `edit_file` and `write_file` answer with a
short diff, and every path they name is one the next call can use: project-relative
inside the root, absolute only for an approved outside path.

`search_text` answers in three sizes: matching lines (default), `mode="count"`
(matches per file) and `mode="files_with_matches"` (names only). The two summaries
cost a fraction of the tokens when the question is *where*, not *what*.

When a turn asks for several reads at once, `find_files`, `search_text`, `read_file`
and `recall` run at the same time and their replies still come back in the order the
model asked for. Anything that changes something — a write, an edit, a command —
still runs one at a time, in order.

## Context and cost

Each turn's timing line reports the server's own prompt, cached and generated
token counts, and `/usage` adds them up. Those numbers also calibrate the
estimate used to shrink `max_tokens` and trim old tool output, so the context
budget stops guessing.

Past 3/4 of the server's window, ykode stops paying for output the model has
already used. In order, cheapest first:

1. Older tool results are replaced with a summary the code writes — which call it
   was, how big it was, how it began — and the full text is kept in
   `~/.ykode/results/<run>/r7.txt` for `recall {"id":"r7"}` to hand back. The two
   newest results are never shortened, and a `read_file` of a window the model
   already has, of a file that has not changed, comes back as one line.
2. If that was not enough, the older turns are folded into a summary the **model**
   writes (goal, files touched, decisions, next step) and the turns it summarised
   are dropped. This happens once per session, and `/compact` asks for it directly.
3. Past 9/10 of the window the emergency brake applies: whole tool calls and their
   replies are removed, oldest first.

Result sizes scale with the window: 4.8 KB on a 4k context, 24 KB on a 128k one. A gateway failure or a model still loading on a router
gets one retry; a request the server rejected on its merits is never replayed.

## Sessions and the audit log

Every conversation is appended to `$YKODE_HOME/sessions/<id>.jsonl`, one line per
message, and every tool call is appended to `$YKODE_HOME/audit/YYYY-MM.jsonl`
before it runs and again after, including the calls that were refused. The audit
log is never trimmed, compacted or reset — it is the record of what actually
happened, with the file each call touched, a command's exit code, the size and
fingerprint of each result, and `OUTSIDE` when a call reached past the root.
Anything that reads like a key, token or password is masked on the way in.
Months past their retention (180 days, `audit_retention_days`) are removed at
startup, never the current one, and `"audit": false` stops the log being written.

`/history` answers the same questions inside a session. Outside one:

```sh
ykode history                      # the last run, call by call
ykode history --all exec           # every command of this root
ykode history --failed --since 24h # what went wrong today
ykode history --all r12            # one call in full, with its output path
ykode history --json --all         # the same, one JSON object per call
```

Flags come first, then what to look for.

`/undo` takes the most recent change off a small stack of snapshots that
`write_file` and `edit_file` leave behind: a file that was edited goes back to the
exact bytes it held, and a file that was created is removed. It is a stack, so a
second `/undo` undoes the change before that.

`--continue last` (or an id) starts where a previous run stopped, and the resumed
run keeps the same id, so its results and its log stay with its conversation. A
conversation bigger than the window sheds its oldest turns and keeps the newest.

## Staying inside the project

File tools go through a single `*os.Root` opened at `--root`. A path outside it
is possible but asked about, per call, showing the resolved absolute path —
`--yes` does not skip that, and neither does an "always". `exec_command` runs
`sh -c` in the root with a minimal environment (`PATH`, `HOME`, `TERM`, `LANG`)
rather than inheriting ykode's.

A confirmation is `[y/N/a]`. `a` means yes, and stop asking about **that tool**
(`edit_file`) or **that command prefix** (`exec:go test` covers `go test ./...`
but not `go mod tidy`) for the rest of the run. The same entries in
`settings.json` are allowed from the start:

```json
{ "permissions": { "allow": ["edit_file", "exec:go test"] } }
```

## Install

```sh
make install                      # builds and copies to ~/.local/bin
make install INSTALL_DIR=/usr/local/bin
sh scripts/install.sh             # clone-and-build installer, same destination
make uninstall                    # remove the binary; keeps ~/.ykode
make uninstall PURGE=1            # also removes ~/.ykode
ykode --version                   # git tag the binary was built from
make release                      # linux/amd64, linux/arm64, darwin/arm64 into dist/
```

`~/.local/bin` is not on `PATH` by default in every shell; add it if `ykode` is not found.

## Settings

Settings resolve in this order, later winning: built-in defaults, `~/.ykode/settings.json`,
`<root>/.ykode/settings.json`, environment variables, flags.

```json
{
  "base_url": "http://127.0.0.1:8080/v1",
  "model": "Qwen3.6-35B-A3B-GSQ-hybrid",
  "max_turns": 25,
  "thinking": true,
  "verify": true,
  "assume_yes": false,
  "audit": true,
  "audit_retention_days": 180,
  "context": { "reserve_output": 4096, "safety_margin": 256 },
  "tools": {
    "read_lines": 150,
    "search_limit": 100,
    "exec_timeout_sec": 60,
    "command_log_dir": "",
    "keep_command_logs": "24h"
  },
  "models": { "Qwen3.6-35B-A3B-GSQ-hybrid": { "send_reasoning": false } }
}
```

`reserve_output` is the token budget for one reply, `safety_margin` what is kept free beside
it, and `send_reasoning` tells ykode to repeat earlier thinking for a chat template that
expects it back from tool-calling turns. The per-user directory is `~/.ykode`, or
`$YKODE_HOME` when that is set; command logs a cut `exec_command` left behind live in
`~/.ykode/logs`, and shortened tool results in `~/.ykode/results`; both are pruned at
startup after `keep_command_logs`.

## What is confined, and what is not

The file tools are opened through a single `*os.Root`, so a path can neither walk out of
`--root` nor land on a symlink that points outside it. A tool call naming a file that is
outside the root is shown to you and confirmed per call; the answer is never remembered.

`exec_command` is **not** inside that boundary. It runs `sh -c` with the working directory
set to `--root`, so `cat ../../secrets` or `rm -rf ~` is possible. Its environment is
scrubbed to `PATH`, `HOME`, `TERM` and `LANG`, so a command run by the model cannot read
`LOCAL_LLM_API_KEY` or anything else from the environment ykode was started with, but it can
still read and write anywhere your user can. `--yes` skips the confirmation for it too.

So: run ykode as a user with access only to what you are willing to let a shell touch, or in
a container. Closing this gap means an OS-level sandbox (`sandbox-exec` on macOS, `bwrap` on
Linux), which is on the roadmap.

## Development

```
make all     # build, vet, test
make test    # go test ./... -count=1
make cover   # with coverage per package
make fmt     # gofmt -w .
```
