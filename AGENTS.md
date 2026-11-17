# ykode — how to work in this repo

A terminal coding agent that talks to an OpenAI-compatible chat server (normally a local
`llama-server`). One streaming path, no provider SDK, file tools confined to one project root.
`README.md` is the user-facing specification: anything a user would notice belongs there too.

## Commands

```
make build     # bin/ykode, version from `git describe` through -X main.version
make check     # vet + gofmt gate + go test ./... -count=1  — this is the gate
make cover     # coverage per package
make lint      # golangci-lint when installed; passes the gate on when it is not
make fmt       # gofmt -w .
make release   # cross-builds the three shipped targets into dist/
```

`make check` must be clean before work is called done. `lint` is deliberately outside `check`:
the gate must not fail because a machine lacks a third-party tool.

## Layout

| package | job |
|---|---|
| `cmd/ykode` | flags in, exit code out; nothing else |
| `internal/app` | one invocation end to end: settings → server → tools → agent → front end |
| `internal/config` | settings resolution and `settings.yaml`: built-ins, then file, then env, then flags |
| `internal/agent` | the loop between model and tools: turns, budget, folding, permissions, commands |
| `internal/tools` | the callable surface: read, write, edit, exec, plan, ask_user |
| `internal/llm` | `/v1/chat/completions` over `net/http`: request shapes, streaming, timings |
| `internal/session` | the conversation on disk, append-only, resumable byte for byte |
| `internal/tui` | the full-screen front end: transcript, overlays, input, status |
| `internal/cli` | the front end with no screen (`-p`): event consumer, trust prompt |
| `internal/images` | a picture shrunk and encoded for a vision encoder |
| `internal/home` | `$YKODE_HOME`, so every caller agrees on where files live |
| `internal/size` | byte counts as the words a listing would use |

The dependency direction is one way: `app` is the only package that wires the two front ends,
`tui` and `cli` only consume `agent`'s events, and `agent` uses `tools` and `llm`, which use
nothing above them. `agent`, `tools`, `llm`, `session` and `config` never import a front end.

## Invariants that break quietly

- **Requests must stay byte-stable.** A server's prompt cache matches from the first token, so the
  system prompt never changes mid-session (`agent/environment.go`), the tool list never changes
  mid-session (`agent/specs.go`), and news about the date or git state travels on the newest ask,
  never in the prompt.
- **The session log is append-only** (`session/session.go`). A change to the conversation is a line
  naming a position, not a rewrite. A resume must read back exactly what the last run sent. Lines
  that change how a stored line is read raise `format`.
- **One goroutine owns the conversation while a turn runs** (`agent/agent.go`: `turnBusy`,
  `refuseDuringATurn`). Anything that changes history between turns goes through that refusal
  rather than trusting its caller.
- **The waiting rule for commands has one owner:** the table in `agent/commands.go` says which
  commands wait for a turn, and the gate in `tui/run.go` holds every one of them. Never add a
  per-handler "is a turn running" check.
- **A key is described in one place:** the knobs table in `config/catalogue.go` feeds the screen,
  the loader's strictness and the live column. A key described anywhere else is two lists that can
  disagree — and a settings file the next start refuses to read.
- **Result budgets share one arithmetic:** `tools/result.go` (`resultCap`, `ResultBytesFor`).
  Do not re-derive a cap or its cut note in a tool.
- **A tool's class is its contract** (`tools/class.go`): reads may overlap, writes and runs are
  serial, and an unknown name reads as the safe one. A tool that keeps state about its last
  result must not be a reading one (`tools.Detailed`).
- **File access is one `*os.Root`** (`tools/paths.go`): no path walks out, no symlink lands
  outside. `exec_command` is deliberately outside that boundary; its environment stays scrubbed
  (`PATH`, `HOME`, `TERM`, `LANG`) so secrets never reach a model-run command.
- **The screen never takes the pointer** and always leaves the terminal with pointer tracking off
  (`tui/scroll.go`, `tui/tui.go`).

## House rules

- **Comments: one line, maximum.** No block of consecutive `//` lines, no paragraph comments: the
  codebase is one-line-only today, keep it that way. Say why, not what, in a single line — if the
  reasoning will not fit, the line is the conclusion and the detail goes in `README.md`, `ideas.md`
  or the commit message. No `/* */` comments.
- One component per file, named after what it holds; tests mirror the source file.
- gofmt-clean (enforced by `make check`), comments wrapped to about 110 columns.
- Unix only: no Windows port, no `_windows.go` files, no build tags. `exec_command` relies on
  `setpgid` and `kill(-pid)`.
- No provider SDK and no new dependency without a reason stated in the commit. HTTP, JSON and SSE
  are the standard library; `charmbracelet` is the screen, `chroma` the code colouring,
  `goldmark` the markdown, `yaml.v3` the settings file.
- Unreleased licence: this repo has no `LICENSE` yet. Do not add headers or files that claim one.

## Tests

`go test ./... -count=1`, 780+ tests, ~88 % coverage. Every behaviour change is covered by a test
in the test file of the component it verifies.

- Names read as sentences about the behaviour: `TestTheNullDeviceIsNotATerminal`,
  `TestATurnCannotBeStartedOverOneAlreadyRunning`.
- Never touch the developer's `~/.ykode`: point `$YKODE_HOME` at a scratch directory
  (`t.Setenv`, then `home.Reset()` when the package caches it).
- Servers are `httptest` ones driven by scripted replies; the screen is driven through a fake
  terminal (`tui.run`'s writer argument), and pointer reports are fed as real escape bytes.
- A benchmark in a package is the cost of a screen or a reply, not a micro-benchmark.

## Where a change goes

- **New tool:** a file in `internal/tools` implementing the `Tool` interface and `Class()`, added
  in `tools.BuiltIn`, with its class decision left to `agent/class.go`.
- **New slash command:** one row in `agent/commands.go` (name, argument, meaning, waits-or-not),
  handled in `tui/run.go` if it needs a screen, and covered by `agent/commands_test.go` that the
  list and the gate agree.
- **New setting:** one knob in `config/catalogue.go`, its default in `config/presets.go`, and the
  live/not-live column decided there — the settings screens read the table, so they need no edit.
- **New wire field:** `internal/llm/types.go`, and a test that a request the server has never been
  sent is only sent when it has to be: shape changes move every later token.
