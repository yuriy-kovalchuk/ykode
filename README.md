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
conversation has cost, `/reset` clears the history, `/exit` leaves. `Ctrl-C`
during a reply cancels that request and returns to the prompt.

## Tools

`list_dir`, `find_files`, `search_text`, `read_file`, `write_file`, `edit_file`,
`exec_command`. Every truncated result says how to continue — a `read_file`
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
budget stops guessing. A gateway failure or a model still loading on a router
gets one retry; a request the server rejected on its merits is never replayed.

## Staying inside the project

File tools go through a single `*os.Root` opened at `--root`. A path outside it
is possible but asked about, per call, showing the resolved absolute path —
`--yes` does not skip that. `exec_command` runs `sh -c` in the root with a minimal
environment (`PATH`, `HOME`, `TERM`, `LANG`) rather than inheriting ykode's.

## Development

```
make all     # build, vet, test
make test    # go test ./... -count=1
make cover   # with coverage per package
make fmt     # gofmt -w .
```
