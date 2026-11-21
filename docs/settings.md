# Settings

Precedence, lowest first: built-in defaults, `~/.ykode/settings.yaml`, the project's
`.ykode/settings.yaml` (`/create-local-setting` makes one, and it then wins for that project), the
variables `$LOCAL_LLM_URL` and `$LOCAL_LLM_MODEL`, then flags. A key nothing reads is refused by name
and line at startup, so a typo fails loudly instead of being ignored. Everything ykode keeps lives under
`$YKODE_HOME`, default `~/.ykode`.

## The settings screen

`/settings` lists every knob, grouped (model, context, tools, permissions, sessions, appearance) with
whether it applies now or at the next start, and opens the editor its kind wants: a switch turns over on
enter, a choice lists its options (the thinking levels with their budgets, the themes with their colours)
and `←`/`→` steps through them, the tools list is a checklist, a typed list adds entries and takes one
off with `d`, and a number or a length of time is typed and checked before it is written. `r` takes any
setting back to its default.

## Every knob

| setting | default | what it does |
|---|---|---|
| `base_url` | `http://127.0.0.1:8080/v1` | the server ykode asks; a first session asks for it |
| `model` | the first one the server lists | model id |
| `thinking_level` | `medium` | `off` `minimal` `low` `medium` `high` `xhigh` `max` |
| `theme` | `auto` | the screen's colours; see [themes](#themes) |
| `max_turns` | `100` | round-trips one ask may take before it is stopped as runaway |
| `max_tokens` | none | a cap on one reply; unset leaves it the room the window has left |
| `context.safety_margin` | `4096` | tokens kept free for the reply, never more than a quarter of the window |
| `stream_idle_sec` | `120` | silence before a stream counts as dead; a cold prompt wants more |
| `assume_yes` | `false` | do not ask about writes inside the project |
| `permissions.allow` | empty | never ask about these: a tool name, or `exec:go test` |
| `save_server_cache` | `false` | llama.cpp writes this conversation's cache to its disk on leaving and loads it on `--continue`; see [below](#the-servers-cache-across-runs) |
| `tools.disabled` | empty | tools the model is not offered, e.g. `exec_command` for a read-only project |
| `keep_sessions` | `720h` | how long a saved conversation survives |

## Flags

The same knobs from the command line: `--base-url`, `--model`, `--root`, `--max-turns`,
`--max-tokens`, `--ctx` (a window that overrides what the server reports), `--thinking`, `--no-think`,
`--yes`. Two that are not knobs: `-p TASK` answers one task on stdout and exits, which is what makes
it scriptable, and `--continue ID` resumes a saved conversation (`last`, or enough of its id).

## The server's cache across runs

A llama.cpp server keeps the conversation it last read, so the next request reads only what is new.
`--continue` after a restart of either side reads the whole conversation again, which takes minutes on
a long one. With `save_server_cache: true`, leaving ykode asks the server to save its slot as
`ykode-<session>.bin`, and `--continue` loads it back before the first request. The server must run with
`--slot-save-path DIR`, and with a single slot, since ykode cannot tell which of several holds the
conversation; a long conversation's file is gigabytes. A refusal is said and costs nothing but the
cold read.

## Themes

`theme: auto` draws with ykode's own dark or light palette, picked from the background the terminal
reports. `ykode-dark` and `ykode-light` force one of them, and `terminal` uses only the terminal's
sixteen colours, so the screen follows the scheme the terminal already has. The others are the
well-known schemes — `dracula`, `nord`, `solarized-dark`, `solarized-light`, `gruvbox-dark`,
`gruvbox-light`, `catppuccin-mocha`, `catppuccin-latte`, `tokyo-night`, `one-dark`, `monokai` — and
each repaints the terminal's background and text colour while the screen is up, so it looks the same on
any terminal; leaving puts the terminal's own colours back. A theme applies at once: on `/settings`,
←/→ on the `theme` row steps through them.
