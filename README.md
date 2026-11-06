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

## In the session

Type a request. `/help` lists the commands, `/usage` reports what the
conversation has cost, `/compact` shortens the older tool results, `/reset`
clears the history, `/exit` leaves. `Ctrl-C` during a reply cancels that
request and returns to the prompt.

## Tools

`find_files`, `search_text`, `read_file`, `write_file`, `edit_file`,
`exec_command`, `recall`. `find_files` lists one directory with `depth: 1`;
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

## Context and cost

Each turn's timing line reports the server's own prompt, cached and generated
token counts, and `/usage` adds them up. Those numbers also calibrate the
estimate used to shrink `max_tokens` and trim old tool output, so the context
budget stops guessing.

Past 3/4 of the server's window, ykode stops paying for output the model has
already used: older tool results are replaced with a summary the code writes —
which call it was, how big it was, how it began — and the full text is kept in
`~/.ykode/results/<run>/r7.txt` for `recall {"id":"r7"}` to hand back. The two
newest results are never shortened, and a `read_file` of a window the model
already has, of a file that has not changed, comes back as one line instead of
the file. Result sizes scale with the window: 4.8 KB on a 4k context, 24 KB on a
128k one. A gateway failure or a model still loading on a router
gets one retry; a request the server rejected on its merits is never replayed.

## Staying inside the project

File tools go through a single `*os.Root` opened at `--root`. A path outside it
is possible but asked about, per call, showing the resolved absolute path —
`--yes` does not skip that. `exec_command` runs `sh -c` in the root with a minimal
environment (`PATH`, `HOME`, `TERM`, `LANG`) rather than inheriting ykode's.

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
