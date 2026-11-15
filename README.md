# ykode

A small terminal coding agent that talks to an OpenAI-compatible chat server —
normally a local [llama.cpp](https://github.com/ggml-org/llama.cpp) `llama-server`.
No provider SDK: the wire protocol is the standard library's, one streaming path, and the model's thinking is
shown separately from its answer.

It keeps a conversation, calls tools (find, search, read, write, edit, run), and
confines file access to the project root it started in.

## Requirements

- A unix system. ykode runs commands in their own process group so that
  cancelling a command takes its children with it, which is a `setpgid` and
  `kill(-pid)` thing; there is no port and none is planned.
- Go 1.25 or newer.
- A server speaking `/v1/chat/completions` with tool calling over SSE. For
  `llama-server`, tool calling needs `--jinja`.
- Optional: `rg` and `fd`. Searching is `grep` and `find` run through
  `exec_command`, so they only make those faster; ykode needs neither.

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
| `--max-tokens` | — | no cap | tokens per reply; with no cap it gets the room the window has left |
| `--max-turns` | — | `100` | tool rounds per request |
| `--api-key KEY` | — | — | bearer token, for a server that asks; `api_key_env` in the file is the safer way |
| `--thinking LEVEL` | — | `medium` | how deeply the model thinks: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `--no-think` | — | thinking on | shorthand for `--thinking off` |
| `--yes` | — | off | skip in-project confirmations |
| `--send-reasoning` | — | off | send earlier thinking back to the model, which some chat templates expect |
| `-p TASK` | — | — | answer one task and exit; the answer goes to stdout, so it pipes |
| `--continue ID` | — | — | resume a saved conversation: `last`, or an id like `s20260102-150405.123` |
| `--version` | — | — | print the version and exit |

## In the session

Type a request. A `/` lists the commands it could still be and `tab` completes one;
`/compact` folds the older turns into a summary the model writes, and `/exit` (or `/quit`) leaves.

Several commands are answered with a box over the session instead of text in the transcript,
because a list you can look through beats a paragraph you have to re-read: `/sessions` the
saved conversations, newest first, `/model` the models the server offers,
`/tools` the tools the model may call, `/help` the commands themselves. In any of
them: type to narrow, `↑/↓` move, `esc` gives the session back, and enter chooses — continue the
conversation, switch to the model, read a whole call, write one command in the prompt box.
A call is read inside the box, never written into the conversation: everything there is of it - the
arguments in the order the model wrote them, every line of a file it wrote or a patch it applied, the
files it touched, what it changed, what it came back with. Longer than the box, it scrolls: `↑/↓` and
`pgup/pgdn` through it, `end` straight to its last row, `←/→` to the call next to it without going back
through the list. `/ctx` and `/usage` open the same box with the figures in it, the window's fullness drawn
as a bar rather than a sentence. `/reset` asks once before it does it: what the run had
learned cannot be brought back.

Four commands change the run rather than the conversation:

| command | what it does |
|---|---|
| `/ctx` | opens a panel with: how full the window is, what the server had cached in the last request and in all of them, what the tool descriptions cost, and — where a draft model runs — how many of its tokens were kept |
| `/tools` | opens the list of tools the model may call, and the bytes each description adds to every request |
| `/plan` | the steps of the task the model is working through, as it last wrote them |
| `/model [id \| number]` | on its own, opens the list of models the server offers, with its window and the one in use marked, and enter switches to the one under the cursor; an id or the number from that list switches from where you stand (the window follows the model unless `--ctx` fixed one) |
| `/think [level]` | how deeply the model thinks on the turns that follow; no level prints the current one |
| `/create-local-setting` | make `<root>/.ykode/settings.yaml` for this project, starting from the server and model this run uses; `/settings` changes that file afterwards instead of `~/.ykode/settings.yaml`, and says which it wrote. Between turns |
| `/settings [list \| key] [value]` | on its own, opens a screen over the session with **every setting** in one list: the server row opens the connect-and-pick-a-model screen a first run starts with; switches turn over on enter, choices cycle with enter or the arrows, and counts, durations and text are typed in place and checked before they are written. On the thinking level it also shows the budget each level gives (minimal 1,024 … high 16,384, xhigh 32,768, max no cap), the selected one marked, so moving between levels shows what each costs. Each change goes to `settings.yaml` at once, and the ones a running session can take up (thinking level, assume_yes, max turns and tokens, the safety margin) apply immediately; the rest are marked *next run*. Between turns only. `list` prints every knob with its default and who set it, a key explains one, and a key and a value write it |

A turn owns the conversation and its own settings, so `/reset`, `/compact`,
`/model` and `/think` wait for the turn in flight; `/ctx`, `/tools`, `/usage` and
`/settings` answer straight away. `Ctrl-C` during a reply cancels that request and
returns to the prompt.

## The full-screen session

A plain `ykode` opens an alternate-screen session: the transcript scrolls, the thinking and
the answer arrive as one block each, a tool call shows while the model is still writing it, and
two rows at the bottom report where the session runs and what it costs. Without a terminal there is
no screen to draw: such a run has to name one task with `-p`.

The terminal's own tab is named for the project — `ykode: ykode` when it was opened in
`~/Developer/ykode` — so several sessions in one window say which is which. The name goes to the
terminal as an escape sequence, so it costs the session no rows.

```
ykode                            # or ykode --continue last
```

A model pinned in `settings.yaml` that the server does not list (left over from another endpoint) is
replaced by the server's own, and the screen says so at the top; a model named with `--model` or
`$LOCAL_LLM_MODEL` is kept, with a warning.

The first time, with no `base_url` in `~/.ykode/settings.yaml` (and none from `--base-url` or
`$LOCAL_LLM_URL`), the session opens on a setup screen: the server's URL, an optional API key, then
the model, picked from the server's own `/models`. A URL typed without `/v1` is tried with it too.
The answer is written into `settings.yaml`, which keeps your comments and other keys. The same
screen opens when the configured server does not answer, with the error on it.

| key | what it does |
|---|---|
| `enter` | sends the prompt |
| `ctrl+j`, `alt+enter`, `shift+enter` | starts a new line; a paste keeps its own newlines. `shift+enter` only where the terminal reports the modifier at all, and most terminals cannot tell `ctrl+enter` from `enter`, so it is not offered |
| `ctrl+a`, `ctrl+e` | go to the start or the end of the line the caret stands on; `home` does the same |
| `ctrl+k`, `ctrl+w` | delete from the caret to the end of the line, or the word before the caret |
| `up`, `down` | scroll the transcript; with more than one line in the box they move the caret. A trackpad scroll in a terminal that reports it as these keys moves the transcript, never the box |
| `ctrl+p`, `ctrl+n` | walk the prompts you sent this session; a line you were writing waits at the end of the walk |
| mouse wheel | scrolls the transcript, and that is all the mouse does: ykode never takes the pointer, so clicks and drags do nothing here and selecting and copying text stays with your terminal. Scrolled up, the rule above the status says where you are and when new output arrived below |
| `end` | back to the end of the transcript (with an empty box) |
| `ctrl+o` | folds the plan to its heading, or opens it again |
| `esc`, `ctrl+c` | stops the running turn; `ctrl+c` twice leaves |
| `pgup`, `pgdown` | move the transcript a page; `ctrl+u`, `ctrl+d` half a page |
| `y`, `a`, `n` | answers a question about a change, shown in a bar above the prompt with the call in words and the first lines of what it would write: yes, always, no — `n` is the default |
| `ctrl+/`, `?` | shows and hides this list of keys; `?` only when the box is empty |
| `tab`, `esc` | `tab` completes a `/command` to what the candidates share; `esc` clears the list, and stops a running turn when there is no list |

Typing while the model works is allowed: the ask goes to that run, which takes it at the next
round-trip instead of waiting for the whole turn to end. The request already in flight and the
tools already running are not interrupted. Until it is taken, an ask keeps a row of its own above
the prompt so it cannot scroll away; one still waiting when the run ended starts a turn of its own.
The commands of `In the session` work there too, except
that `/reset` and `/compact` wait for the running turn rather than
racing it.

The box a prompt is typed in grows with what is typed, to ten rows, and past that scrolls the text
inside itself; a long prompt therefore keeps the rows it wraps over on the screen. What was asked
stands in a band across the whole width, so a prompt reads apart from an answer even when it runs
over several rows. Everything else — the answer, the thinking, a call and what it changed, notices —
is read inside a margin: two columns clear of the left edge and one held back from the right, so the
nesting of an answer, its code and its changes stays visible and nothing sits against the terminal's
border. The session also starts one row below the top edge.

The footer is two rows. The first names where the session runs on the left — the project root and the
branch checked out there, written from `$HOME`, as `~/Developer/ykode (main)` — and on the right the
model and how deeply it thinks (`· high`, or `· thinking off`). The second has what the conversation
has cost on the left: `↑` prompt tokens, `↓` generated, and `R` of them served from the server's cache,
all three summed over the whole session; then `cache %` for what the last request found already cached,
`draft %` where the server ran a draft model (how much of the last reply's speculation the real model
kept), and last how full the window is, `ctx used/total %`. A screen too narrow for all of it drops the totals first and
keeps the window. What is happening now sits on the right of that row: the running prompt's clock
(`✓ done in 51s` once it finishes) and, beside it, what that ask had to do to fit the conversation in —
`summarising older turns`, `folded 12 older turns`, `dropped 3 older messages` — said while it
happens, because that is what a slow round-trip makes a reader wonder about. It starts over with the next ask. Then the server's speed, `↑` reading the prompt and `↓`
writing, both in tokens per second. A server that reports its speeds with every chunk (llama.cpp's own
`llama-server`) updates both live. One that
reports them only at the end of the reply, as Strata does, still gets a live `↓`: ykode counts the streamed text
(bytes over the bytes-per-token ratio it has learned), reads a one-second sliding window, and shows the result with a `~`; the server's exact
speed, `↑` included, replaces it when the reply ends, and a generated count the server counted is never replaced
by the one measured here. The cache percentage is llama.cpp's: how much of the request it already
had. Each prompt shows the time it was sent, and each finished one when it ended and how long it took.
The branch is read when a prompt starts, and the screen looks at HEAD every five seconds besides, so a
`git checkout` made in another window shows up without waiting for the next prompt.

An answer is drawn as the model shaped it: headings, lists, checklists, block quotes and fenced code
(language named once above the block), tables as a ruled grid whose cells wrap inside their columns
when the screen is narrower than the table, and the marks it wrote turned into the terminal's own
bold, italic, struck-out and colour. A line the model broke stays broken, so data written a value a
line reads as it was written. goldmark parses the
CommonMark; ykode draws it, so the palette and the width stay the screen's. Nothing about that changes what the model is sent — an answer is kept exactly as it
arrived, and only the drawing interprets it.

A fence that names a language is coloured by that language's grammar: chroma's hundreds of grammars,
the colours ykode's own four (the words that define, the values, the comments, what is called), so the
terminal's theme still decides what each of them looks like. A fence that names nothing, or a language
no grammar covers, is left exactly as written — guessing at a language colours prose, and a sentence
about a `case` or a `type` would read as code. A block is coloured while it is still arriving: the
lines already written are read once, and a frame reads only the line being written, so the colour
keeps up with the tokens. A closed block is read once and kept.

What streams is drawn at thirty frames a second, however fast the tokens come, and the run never waits
for a frame: what it reports is queued and reaches the screen joined. An answer is laid out a block at
a time, so a frame costs the block being written rather than the answer so far.

A turn carries on until the work is answered, not until a reply happens to end. A reply cut off at
`max_tokens` - an answer stopped mid-sentence, or thinking that used the whole budget - is asked to go
on; so is an empty one, and, once, a reply that says what it will do next and then does not do it or
leaves steps of its plan open. A tool call the model wrote out as text (`<tool_call>…`), which some
servers do not read back into a call, is run as the call it is when it names a tool ykode has, and its
markup is never drawn. When a request uses all its tool rounds (`max_turns`, 100 unless set) the
model is asked where the work stands and the screen says a message will carry it on. What ykode tells
the model in the user's place is marked as ykode's, so the model does not answer it as the user's.

For a long task the model keeps a checklist with the `plan` tool: it sends every step each time, each
pending, active (the one it is on), done or failed, so the list is never half an edit. The screen pins it
between the transcript and the prompt, where it cannot scroll away: a heading with one cell per step
(`plan ▰▰▰▱▱ 3/5`, green for done, the accent for the active one, red for failed, hollow for pending), then a row
per step, the active one turning with the spinner while the turn runs. A plan too long for the room shows the
steps around the active one and counts what it left out; a short terminal, `ctrl+o`, or a plan whose every step
has an outcome fold it to its heading — `plan ✓ 3/3 done`, or `plan ✗ 2/3` when a step failed, since a plan that
ended badly is over but did not finish. A model that works on for several calls without touching a plan
with open steps is reminded of it on the next result it reads, so the list moves with the work
instead of being ticked off all at once at the end. A plan that is over is dropped when the next prompt starts, so it does
not sit there for the rest of the session; one with steps still pending or active carries over, which is what a
stopped turn and a "carry on" leave behind. The plan is kept with the conversation: `/reset` clears it, `--continue` finds it again
in the newest plan call, and a fold carries it in its recap so the model never loses the list it is working
through. Twelve steps at most, a line each; the tool costs about 500 bytes of every request.

Each call the model makes is one row — always one row — in plain words: the tool, the file or pattern
or command it touched **from where the project starts** (`toy_llm/train.py`, not
`/Users/you/dev/playground/toy_llm/train.py`), and what came back: `lines 1-170 of 170` for a read,
`5180 → 5410 bytes, 2 replacements` for a change, `exit 1` for a command that left badly, `12 matches`
for a locate. Nothing is hidden behind a key and nothing the call read is kept for a screen that is
not going to draw it: a locate's matches and a read's file are counted on the row, not printed into
the session. A question about a change asks the same way: `write_file web/index.html · 200 lines · 1.6 KB`.

The row has no ground behind it: the glyph at its left carries the state — dim `▸` while the call
runs, green `✓` once it answered, red `✗` when it failed, `✎` while the model is still writing it —
and the tool's name is bold. The row is as wide as its words, not the screen.

A call that changed a file puts the change on the rows under its row, drawn the way a review tool
draws one: every line with its number in the file, a line that went on a red ground and one that came
on a green one, each running to the edge of the screen, and the words that differ between a line and
the one that replaced it on a stronger shade of the same ground. The grounds are picked for the
terminal the screen runs in, and the words on them stay the terminal's own colour. Unchanged lines
around a change are left as they are, a `⋯` stands between two places in the file, and the call's row
counts the change (`+12 -3`). A change is shown whole, a new file included: its rows are drawn once and
kept, so a long one costs rows to scroll past and no time per frame (three to four milliseconds per
thousand lines, once). The model is still sent the short copy; the screen is handed the whole change
beside it, up to three thousand lines. A question about an `edit_file` shows the first rows of the
change, from the call's own arguments, before the file is touched.

A command that came back non-zero, was killed at its timeout or was cancelled reads as a failure —
its red `✗` and the last lines it printed under the row, behind a quiet gutter, where the error is.

A command that prints past its budget keeps its head and most of its tail in what the model is sent,
and the whole of it is written to a log as the command makes it, under `~/.ykode/logs`, with the path
named in the result. The output is streamed, not buffered, so a build that prints forty megabytes costs the run a
window of RAM rather than the forty megabytes. The middle of it is the model's to read, not the
screen's: the result tells it `read it with exec_command, e.g. sed -n '1,80p' FILE`.

## Tools

`read_file`, `write_file`, `edit_file`, `exec_command` and `plan`,
plus `ask_user` where someone is there to answer it. The list is fixed for the whole session: Qwen
renders it inside the system message, so a tool that appears later throws away everything the server
had cached. The descriptions cost about 2.4 KB on every request, and `ask_user` adds about 0.7 KB
where it is offered; tests keep both under their budgets.
There is no search tool: locating code is `grep` or `find` through `exec_command`, and the
description says so — filter in the command itself, because a result that has to be cut off costs
the round-trip that reading the file again would have. Neither command reads `.gitignore` on its
own, so the instructions say to exclude what git ignores.
Every truncated result says how to continue — a `read_file`
window reports the next `offset`, a cut `exec_command` log reports the saved file. Nothing reads a
file it cannot hold: `read_file` stops at 1 MB and names the tool that can, and `exec_command`
streams its output through a window instead of waiting for all of it.
`edit_file` takes several `edits[]` in one call and matches them atomically, so a
bad pair never applies half an edit. `edit_file` and `write_file` answer with a
short diff, and every path they name is one the next call can use: project-relative
inside the root, absolute only for an approved outside path.

`ask_user` stops the run and puts one question to the person, with the choices the model would
act on. On the screen it appears above the box with the choices numbered, and the answer is typed:
a number picks the choice it names (the model reads the choice in words, not the number), anything
else is the answer as typed, and an empty line or `esc` says there is none, which tells the model to
make the choice itself and say what it assumed. A question offers up to six choices; on a short
terminal the ones that do not fit are counted in one line, and their numbers still answer. A `-p` run
has nobody at the keyboard, so it is not offered the tool at all:
a call nobody can answer would only stall the run.

When a turn asks for several reads at once, the `read_file` calls
run at the same time and their replies still come back in the order the
model asked for. Anything that changes something — a write, an edit, a command —
still runs one at a time, in order.

## Context and cost

Each turn's timing line reports the server's own prompt, cached and generated
token counts, and `/usage` adds them up. Those numbers also calibrate the
estimate used to set each reply's `max_tokens` (when no cap was asked for) and to
decide when to fold, so the context budget stops guessing.

What ykode does about the server's prompt cache is keep still rather than instruct: the system message
and the tool list hold nothing that changes from one run to the next, so the prefix is the same bytes in
every run and a resumed conversation is not read again. The date and whether the git tree has changes
travel with an ask instead, as a short `<environment>` note at its end, and only when they differ from
what the last ask said; a listing or a replay shows the ask without it. Within a run, folding older turns waits
until enough has changed to be worth the reset it costs, and `/ctx` reports what the server says it
found. There are no `cache_control` breakpoints, no `prompt_cache_key`, no retention setting: a local
`llama.cpp` decides what to keep from the prefix it was already handed, and asking it is a different
server's business. The one thing every request does say is which conversation it belongs to —
`X-Session-Id` carries the run's id, which is how a proxy in front of a server keeps one conversation
on the slot that already holds its cache.

ykode makes room in two steps, and nothing between them: a message stays exactly as it was sent until
it is folded away or dropped whole. A fold is asked at the last moment a fold request still fits - the
window less the summary it would write - and the drop at the last moment a reply still fits, so the
model keeps whole results until they cost the run something it actually needs to spend. Both marks, and
what a request costs, are in `/ctx`.

1. **Fold.** The older turns are replaced by a summary the **model** writes, in fixed sections (goal,
   constraints, progress done / in progress / blocked, decisions, critical context such as exact names
   and error text, next steps). The request sends the conversation exactly as the server already holds
   it — same system message, same tool list, same messages — and one instruction after them, so the
   prompt cache covers the asking and the summariser reads the real results, calls and thinking rather
   than a transcript of markers. The notice it prints says how many of the prompt tokens the server had
   cached. The newest work stays whole: at least the two newest calls - one, where the whole conversation is
   two calls and keeping two would leave nothing to fold - and back to a fifth of the window (at most
   20k tokens) where the window is known; a cut never separates a call from its results. A
   later fold updates the recap an earlier one left, because that recap is part of what gets sent. The
   summary ends with the files the session read (`<read-files>`) and changed (`<modified-files>`),
   accumulated across folds and restored on resume. `/compact` asks for a fold directly. A summary may
   cost 1500 tokens, or an eighth of the window where the window is smaller: a fold request has to fit
   the window it is making room in, and a fold that would not is refused out loud.
2. **Drop.** Past 9/10 of the window the emergency brake applies: the oldest messages go, whole — a
   call together with its replies — until the estimate fits. A fold's recap, the ask being answered and
   the newest call are never dropped. The notice names what went (`dropped 3 older messages to fit the
   context: read_file src/parser.go; exec_command go test ./...; ...`), so a model that needs one of
   them again can ask for it instead of guessing at what it forgot.

Result sizes scale with the window: 4.8 KB on a 4k context, 50 KB (as pi) on a 128k one. A gateway failure or a model still loading on a router
gets one retry; a request the server rejected on its merits is never replayed.

## Saved conversations

Every conversation is appended to `$YKODE_HOME/sessions/<id>.jsonl`, one line per
message, with every tool call and its result. It is what `--continue` reads back, and
what `/sessions` lists on the screen.

`--continue last` starts the newest conversation again; `--continue <id>` picks one
by id. An id is a timestamp to the millisecond, so enough of it to be unique is
enough (`--continue 2137`), and `/sessions` on the screen lists what there is with what each
was about. On the screen `/sessions` opens that list as a menu: enter puts the
conversation under the cursor in this run's place - the screen draws it, the model
reads it, and what follows is saved to its file, while the conversation the run gave
up stays saved to be come back to the same way. A conversation written in another
project is only named there, with the command that continues it in its own root.

The screen opens with the conversation it came back to — the asks, the calls with what they
answered, and the recap of any fold — and `ctrl+p` still walks the earlier prompts of it.

The model's thinking is saved beside each message and shown again on resume (it is never sent
back to the server unless `send_reasoning` asks for it).

A resume reads back what the last run *ended with*: a fold is written to the log with its summary,
the files it covered, how many messages it took out and what the conversation cost before it, and
the older entries stay in the file, only no longer sent. The recap the model
wrote for it is saved with the conversation, and `/reset` marks the saved log as
started over, so nothing that was put away comes back. What a resume cannot save is
the folding and dropping a turn does to fit the window, so a conversation bigger than
the window still sheds its oldest turns and keeps the newest.

## Staying inside the project

File tools go through a single `*os.Root` opened at `--root`. A path outside it
is possible but asked about, per call, showing the resolved absolute path —
`--yes` does not skip that, and neither does an "always". `exec_command` runs
`sh -c` in the root with a minimal environment (`PATH`, `HOME`, `TERM`, `LANG`)
rather than inheriting ykode's.

A confirmation answers to `y`, `a` or `n`, and `n` is the default. `a` means yes, and stop asking about **that tool**
(`edit_file`) or **that command prefix** (`exec:go test` covers `go test ./...`
but not `go mod tidy`) for the rest of the run. The same entries in
`settings.yaml` are allowed from the start:

```json
{ "permissions": { "allow": ["edit_file", "exec:go test"] } }
```

## Install

```sh
make install                      # builds and copies to ~/.local/bin
make install PREFIX=/usr/local    # or INSTALL_DIR=... for the exact directory
sh scripts/install.sh             # clone-and-build installer, same destination
make uninstall                    # remove the binary; keeps ~/.ykode
make uninstall PURGE=1            # also removes ~/.ykode
ykode --version                   # git tag the binary was built from
make release                      # linux/amd64, linux/arm64, darwin/arm64 into dist/
```

The installer prints the exact `PATH` line for the shell that ran it (zsh, bash, fish), and
with `--add-to-path` appends it to your startup file once, instead of just showing it:

```sh
sh scripts/install.sh --add-to-path
```

The first run creates `~/.ykode` (mode `0700`) and writes `~/.ykode/settings.yaml` (mode
`0600`) holding every setting at its default, so the file you would edit is already there.

## Settings

Settings resolve in this order, later winning: built-in defaults, `~/.ykode/settings.yaml`,
`<root>/.ykode/settings.yaml`, environment variables, flags. Both are YAML and meant to be edited by
hand.

`/settings` is the way in from inside a session. Alone it opens a screen whose header says which
file it writes: this project's `.ykode/settings.yaml` when it has one, or a warning that it is about
to change `~/.ykode/settings.yaml`, the file every project reads. It lists every knob with what it is
now, and marks the ones someone changed with what they were and whether a flag, the environment, the
project's file or the global one decided it; a knob nobody chose says nothing else, so the list reads
as what is unusual about this run. `/settings thinking_level` explains one knob — what it is for, its
choices, how to change it. `/settings tools.read_lines 40` writes that key into the file this project
uses, comments and other keys kept, and says which file took it and which way it moved; the next run
reads it. A value the knob cannot take is refused before the file is touched. `api_key` is reported as
set, never repeated.

A project can keep its own settings: `<root>/.ykode/settings.yaml` overrides the global file key by
key, and a key left out keeps the global value. `/create-local-setting` makes one, starting from the
server and model this run is using; after that `/settings` changes it and never the global file.
A project's file is not trusted just because it is there — only one ykode wrote is. It may set
`max_turns`, `max_tokens`, `thinking_level`, `context.*`, `tools.read_lines`,
`tools.exec_timeout_sec` and `models.*` — how the project is read and what a turn costs. Everything else waits until you trust the project: the server it talks to, any key,
`assume_yes`, `permissions.allow`, and where command output is written. ykode names
what it held back, asks once when someone is at the keyboard, and records the answer in
`~/.ykode/trusted.json` as project root → the hash of that file, so editing it asks again.

```yaml
base_url: http://127.0.0.1:8080/v1
api_key_env: LOCAL_LLM_API_KEY
model: Qwen3.6-35B-A3B-GSQ-hybrid
max_turns: 100
assume_yes: false
keep_sessions: 720h
max_tokens: 0
thinking_level: medium
stream_idle_sec: 120
context:
  safety_margin: 4096
tools:
  read_lines: 2000
  exec_timeout_sec: 60
  command_log_dir: ""
  keep_command_logs: 24h
models:
  Qwen3.6-35B-A3B-GSQ-hybrid:
    send_reasoning: false
    thinking: { temperature: 0.6, top_p: 0.95, top_k: 20, min_p: 0 }
    no_think: { temperature: 0.7, top_p: 0.8, top_k: 20, presence_penalty: 1.5 }
```

`stream_idle_sec` is how long a stream may stay silent before ykode gives up on it (raise it
when a server sends nothing while it reads a long cold prompt), `max_tokens` is the token budget for one reply
(0, the default, asks for no cap: the server's own context decides where the reply ends, and ykode never asks
for more than the window has left), `thinking_level` how deeply the model thinks
(`off` … `max`; each level asks llama.cpp for a thinking budget, and the budget is capped so the answer
keeps room of its own), `safety_margin` what is kept free beside
it for the chat template, and `send_reasoning` tells ykode to repeat earlier thinking for a chat template that
expects it back from tool-calling turns. `thinking` and `no_think` are the sampling a model
gets with its thinking phase on and off (`temperature`, `top_p`, `top_k`, `min_p`,
`presence_penalty`); anything left out is the server's default. A server that wants a bearer token takes it from
`api_key`, or from the variable `api_key_env` names — the second keeps the secret out of
the file, and an unset variable is no error.

A saved conversation ages out after `keep_sessions` (30 days by default).

A key the settings file does not recognise is an error naming the file and the key, not a
silently ignored line.

The per-user directory is `~/.ykode`, or `$YKODE_HOME` when that is set; command logs a cut
`exec_command` left behind live in `~/.ykode/logs`, pruned at startup after `keep_command_logs`. `$YKODE_HOME/AGENTS.md` holds rules for every project
you open; they are loaded before the project's own `AGENTS.md`, so a project can overrule
what you set for all of them.

## What is confined, and what is not

The file tools are opened through a single `*os.Root`, so a path can neither walk out of
`--root` nor land on a symlink that points outside it. A tool call naming a file that is
outside the root is shown to you and confirmed per call; the answer is never remembered.

`exec_command` is **not** inside that boundary. It runs `sh -c` with the working directory
set to `--root`, so `cat ../../secrets` or `rm -rf ~` is possible. Its environment is
scrubbed to `PATH`, `HOME`, `TERM` and `LANG`, so a command run by the model cannot read
`LOCAL_LLM_API_KEY` or anything else from the environment ykode was started with, but it can
still read and write anywhere your user can. `--yes` skips the confirmation for it too. So run
ykode as a user with access only to what you are willing to let a shell touch, or in a container.

## Benchmarks

The task benchmark against pi lives outside this repository, in `~/Developer/testing-ykode`; its
README and `BASELINE.md` say how to run and read it. It drives `ykode -p ... --json`, and `--json`
went with the reduction, so it has to be updated before it can score ykode again.

## Development

```
make all     # build, vet, test
make test    # go test ./... -count=1
make cover   # with coverage per package
make fmt     # gofmt -w .
```
