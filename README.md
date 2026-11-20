# ykode

An experimental terminal coding agent for **local** inference engines: llama.cpp's `llama-server`
and strata so far. It speaks `/v1/chat/completions` with tool calls over SSE, and nothing else. No
cloud integration is planned: no provider SDK, no account, no telemetry, and the only address it
connects to is the one you configure.

The loop is pi-shaped. A model answers, asks for tools, the loop runs again, and its thinking is
shown apart from its answer. A reply that ends after its thinking, with no call and no answer, is
not kept: the same request is sent again, at most twice per stall, because a kept one teaches the
model that a turn may end there. What this repository adds is the toolchain: that shape usually ships
with a Node runtime, a package manager, a bundler and a dependency tree, to move bytes between a
socket and a terminal. ykode is one Go binary on the standard library's HTTP, JSON and SSE, and
`make build` is the whole build. It is experimental: settings keys and the saved-conversation format
may still change, no version is tagged yet, and the input box, the scrolling and the code colouring
are where a rough edge still shows.

## Build and install

You need a unix system and Go 1.27 or newer. No Windows port is planned: a command runs in its own
process group so that cancelling it takes its children too, which is a `setpgid` and `kill(-pid)`
thing.

```sh
git clone https://github.com/yuriy-kovalchuk/ykode && cd ykode
make build    # bin/ykode: CGO off, -trimpath, version stamped from git describe
make install  # into ~/.local/bin
ykode --version
```

`sh scripts/install.sh` clones and installs without cloning by hand (`--add-to-path` appends the
PATH line). A tagged release needs no toolchain at all: it publishes the three cross-built binaries —
`ykode-linux-amd64` (Linux x86-64), `ykode-linux-arm64` (Linux ARM64) and `ykode-darwin-arm64` (Apple
silicon) — with `SHA256SUMS` and a cosign signature per binary, on the Releases page, which is empty
until the first `v*` tag. Checking a download you did not build yourself:

```sh
cosign verify-blob ykode-darwin-arm64-v0.1.0 \
  --signature ykode-darwin-arm64-v0.1.0.sig \
  --certificate ykode-darwin-arm64-v0.1.0.pem \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/yuriy-kovalchuk/ykode/\.github/workflows/cd\.yml@refs/tags/v'
```

`make check` is the gate (vet, gofmt, tests, lint), `make audit` reports dependency updates and known
CVEs through Trivy in Docker, `make release` cross-builds the three shipped targets into `dist/`,
`make install-hooks` makes git check your commit subjects and tag names, and `make uninstall` takes
`PURGE=1` to remove `~/.ykode` as well.

The default server is `http://127.0.0.1:8080/v1`, which is what `llama-server --host 127.0.0.1
--port 8080` answers. That server needs `--jinja` before it takes a tool call, and `--mmproj` with a
vision encoder before it can see a picture. A small window is the one hard limit, because everything
ykode budgets (the room for a reply, the tool-result cut, folding and compaction) is measured against
it: 16k tokens is the practical floor for a coding session, 32k or more is comfortable.

## Settings

Precedence, lowest first: built-in defaults, `~/.ykode/settings.yaml`, the project's
`.ykode/settings.yaml` (`/create-local-setting` makes one, and it then wins for that project), the
variables `$LOCAL_LLM_URL` and `$LOCAL_LLM_MODEL`, then flags. A key nothing reads is refused by name
and line at startup, so a typo fails loudly instead of being ignored. `/settings` lists every knob and
edits it on screen.

| setting | default | what it does |
|---|---|---|
| `base_url` | `http://127.0.0.1:8080/v1` | the server ykode asks; a first session asks for it |
| `model` | the first one the server lists | model id |
| `thinking_level` | `medium` | `off` `minimal` `low` `medium` `high` `xhigh` `max` |
| `max_turns` | `100` | round-trips one ask may take before it is stopped as runaway |
| `max_tokens` | none | a cap on one reply; unset leaves it the room the window has left |
| `context.safety_margin` | `4096` | tokens kept free for the reply, never more than a quarter of the window |
| `stream_idle_sec` | `120` | silence before a stream counts as dead; a cold prompt wants more |
| `assume_yes` | `false` | do not ask about writes inside the project |
| `permissions.allow` | empty | never ask about these: a tool name, or `exec:go test` |
| `keep_sessions` | `720h` | how long a saved conversation survives |

The same knobs from the command line: `--base-url`, `--model`, `--root`, `--max-turns`,
`--max-tokens`, `--ctx` (a window for a server that never reports one), `--thinking`, `--no-think`,
`--yes`. Two that are not knobs: `-p TASK` answers one task on stdout and exits, which is what makes
it scriptable, and `--continue ID` resumes a saved conversation (`last`, or enough of its id).
Everything ykode keeps lives under `$YKODE_HOME`, default `~/.ykode`.

Requests stay byte-stable: the system prompt and the tool list never change mid-session, and news
about the date or the git state travels on the newest ask, so the server's prompt cache keeps hitting.

## Tools

Six tools, in one file each. `/tools` shows what each description costs the prompt.

| tool | what it does |
|---|---|
| `read_file` | a file as text, from a line offset, capped at 2000 lines by default (`tools.read_lines`). A png, jpeg, gif or webp goes to the model as an image instead, sized down for a vision encoder |
| `write_file` | creates or replaces a whole file, making parent directories |
| `edit_file` | exact replacement: each `old_text` is copied from a read and must occur once, so a wrong guess fails instead of rewriting the wrong lines |
| `exec_command` | one `sh -c` command in the project root, 60 s by default (`tools.exec_timeout_sec`), output cut to a budget that scales with the window |
| `plan` | the model's own checklist, kept in memory and shown beside the prompt |
| `ask_user` | one question, with choices, answered in the box below the bar |

Every path is project-relative and confined to `--root` through one `*os.Root`: no path walks out and
no symlink lands outside it. Reads may run together; writes and commands never do. A write or a
command is confirmed one at a time unless `assume_yes`, and a path outside `--root` is always asked
about. A command is not confined the way a file tool is: it runs with your permissions, in an
environment reduced to `PATH`, `HOME`, `TERM` and `LANG`, and its output is cut to a tenth of the
window (4.8 KB at a 4k context, 51.2 KB at 128k, 12 KB when the window is unknown), head and tail
kept, with the whole of it saved under `$YKODE_HOME/logs` for 24 h (`tools.command_log_dir`,
`tools.keep_command_logs`) and its path handed to the model.

## Commands

| command | what it does |
|---|---|
| `/help` | the commands, narrowed by typing |
| `/usage` | what the conversation has cost the server |
| `/ctx` | how full the window is, and what the server is caching |
| `/compact` | fold the older turns into a summary the model writes |
| `/sessions` | the conversations saved for this project; pick one and this run continues it |
| `/model [id]` | list this server's models, or switch to one |
| `/think [level]` | how deeply the model thinks |
| `/plan` | the checklist as the model last wrote it |
| `/tools` | what the model may call, and what each description costs |
| `/settings [list \| key] [value]` | the knobs: `list` shows every one, a key explains it, a key and a value change it |
| `/create-local-setting` | make `.ykode/settings.yaml` for this project |
| `/reset` | forget the conversation; the screen asks first |
| `/exit`, `/quit` | quit |

## Keys

| key | what it does |
|---|---|
| `enter` | send |
| `ctrl+j`, `alt+enter` | start a new line. `shift+enter` too where the terminal reports the modifier: most cannot tell `ctrl+enter` from `enter`, so it is not offered |
| `ctrl+p`, `ctrl+n` | walk earlier prompts, so a wrong ask is edited rather than repeated |
| `tab` | complete a command |
| `esc`, `ctrl+c` | stop a run; at an idle prompt `ctrl+c` quits |
| `pgup`, `pgdown`, `ctrl+u`, `ctrl+d`, `up`, `down` | scroll the transcript, by a page or half a page; the arrows are the cursor while the box holds more than one line |
| `end` | return to the live end |
| `ctrl+o` | fold the plan |
| `?`, `ctrl+/` | list the keys |

## Deliberately not here

- **No cloud provider**, and **no auth token**: no request carries an `Authorization` header, and no
  key is read from a flag, a variable or a file.
- **No plugins and no MCP**: the callable tools are the six above.
- **No OS sandbox**: the confinement is the project root plus the confirmation, and a command reaches
  whatever your permissions reach, including the network. A jailed `exec_command` is on the ideas
  list, not in the code.
- **No pointer grabbing**: the screen never takes the mouse and always leaves the terminal clean.

## Releasing

A release is a `vX.Y.Z` tag, and it is prepared in a commit before the tag: move the `[Unreleased]`
entries of `CHANGELOG.md` under a new `[X.Y.Z] - YYYY-MM-DD` heading and start `[Unreleased]` again.
Then `git tag vX.Y.Z && git push origin vX.Y.Z`. CD re-runs the CI gate, refuses a tag that is not
semver or has no `CHANGELOG.md` section, and publishes the three binaries with their `SHA256SUMS` and
cosign signatures as a GitHub Release whose notes are that changelog section. There is no `:latest`
and no rolling artifact: a release is a tag, and the newest tag is the newest release.

## Licence

MIT, see `LICENSE`.
