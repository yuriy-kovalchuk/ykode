# ykode

ykode is a learning project: a terminal coding agent written in Go, built to understand how coding
agents work by building one from the ground up. It is experimental. Settings keys and the
saved-conversation format may still change between 0.x releases, and the input box, the
scrolling and the code colouring are where rough edges still show.

## What it is

An agent for **local** inference engines: llama.cpp's `llama-server` and strata so far. It speaks
`/v1/chat/completions` with tool calls over SSE, and nothing else. There is no cloud integration, no
provider SDK, no account and no telemetry. The only address it connects to is the one you configure.

The loop is simple. The model answers, asks for tools, the tools run, and the loop goes again until
the model answers in plain text. Its thinking is shown apart from its answer.

## Why Go

The point of the project is to see how far plain Go gets you. That shape of agent usually ships with a
Node runtime, a package manager, a bundler and a dependency tree, all to move bytes between a socket
and a terminal. ykode is one static binary:

- HTTP, JSON and SSE come from the standard library;
- the screen is Bubble Tea, Bubbles and Lip Gloss, code colouring is chroma, markdown is goldmark, and
  settings are yaml.v3, and that is the whole dependency list;
- `make build` is the whole build, with CGO off, and the result runs with nothing installed beside it.

Most of what is worth learning lives in the parts a local model makes hard: fitting a conversation into
a small context window, keeping every request byte-stable so the server's prompt cache keeps hitting,
folding old turns into a summary, and coping with models that write tool calls as text or stop after
thinking.

## Quick start

You need a unix system, Go 1.27 or newer, and a server that takes tool calls (`llama-server --jinja`,
plus `--mmproj` for pictures).

```sh
git clone https://github.com/yuriy-kovalchuk/ykode && cd ykode
make install                     # bin/ykode into ~/.local/bin
llama-server --jinja --host 127.0.0.1 --port 8080 -m model.gguf &
ykode                            # asks for the server on a first run
ykode -p "what does main.go do?" # one task on stdout, no screen
ykode --continue last            # pick the last conversation up again
```

The default server is `http://127.0.0.1:8080/v1`. The context window is read from the server; 16k
tokens is the practical floor for a coding session, and 32k or more is comfortable. `/settings`
changes everything else, `/help` lists the commands and `?` lists the keys.

## Documentation

| document | what is in it |
|---|---|
| [Installing](docs/install.md) | building from source, release binaries and checking their signatures, the server's flags |
| [Settings](docs/settings.md) | where settings come from, every setting, the flags, the server's cache across runs, themes |
| [Tools](docs/tools.md) | the six tools, how files and commands are confined and confirmed, pictures |
| [Commands and keys](docs/usage.md) | how a turn runs, every slash command and every key |
| [Developing](docs/development.md) | make targets, evals, releasing |
| [Changelog](CHANGELOG.md) | what changed, release by release |

## Deliberately not here

- **No cloud provider and no auth token.** No request carries an `Authorization` header, and no key is
  read from a flag, a variable or a file.
- **No plugins and no MCP.** The callable tools are the six in [Tools](docs/tools.md).
- **No OS sandbox.** The confinement is the project root plus your confirmation, and a command reaches
  whatever your permissions reach, including the network. To fence commands in, run ykode inside a
  container with only the project mounted.
- **No Windows port.** A command runs in its own process group so that cancelling it takes its
  children too, which relies on `setpgid` and `kill(-pid)`.

## Licence

MIT, see `LICENSE`.
