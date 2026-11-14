# ykode

A small terminal coding agent that talks to an OpenAI-compatible chat server —
normally a local [llama.cpp](https://github.com/ggml-org/llama.cpp) `llama-server`.
No provider SDK: the wire protocol is the standard library's, one streaming path, and the model's thinking is
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
| `--max-tokens` | — | no cap | tokens per reply; with no cap it gets the room the window has left |
| `--max-turns` | — | `100` | tool rounds per request |
| `--thinking LEVEL` | — | `medium` | how deeply the model thinks: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `--no-think` | — | thinking on | shorthand for `--thinking off` |
| `--yes` | — | off | skip in-project confirmations |
| `-p TASK` | — | — | answer one task and exit (pipeable; add `--json` for event lines) |
| `--continue ID` | — | — | resume a saved conversation: `last`, or an id like `s20260102-150405.123` |
| `--no-tui` | — | full screen | a line prompt instead of the full-screen session (also when not on a terminal, or with `-p`/`--json`) |
| `--version` | — | — | print the version and exit |

## In the session

Type a request. A `/` lists the commands it could still be and `tab` completes one;
`/compact` folds the older turns into a summary the model writes, `/undo` puts the most
recent file change back, `/exit` (or `/quit`) leaves.

Several commands are answered with a box over the session instead of text in the transcript,
because a list you can look through beats a paragraph you have to re-read: `/sessions` the
saved conversations, newest first, `/model` the models the server offers, `/history [what]` the
calls this run made (a tool name, `failed`, `outside` or `--all` narrow it, and `r12` opens that
call), `/tools` the tools the model may call, `/help` the commands themselves. In any of
them: type to narrow, `↑/↓` move, `esc` gives the session back, and enter chooses — continue the
conversation, switch to the model, read a whole call, write one command in the prompt box.
A call is read inside the box, never written into the conversation: everything there is of it - the
arguments in the order the model wrote them, every line of a file it wrote or a patch it applied, the
files it touched, what it changed, what it came back with. Longer than the box, it scrolls: `↑/↓` and
`pgup/pgdn` through it, `end` straight to its last row, `←/→` to the call next to it without going back
through the list. `/ctx` and `/usage` open the same box with the figures in it, the window's fullness drawn
as a bar rather than a sentence. `/reset`, and `/undo force`, ask once before they do it: neither
can be undone.

Four commands change the run rather than the conversation:

| command | what it does |
|---|---|
| `/ctx` | opens a panel with: how full the window is, what the server had cached in the last request and in all of them, what the tool descriptions cost, and — where a draft model runs — how many of its tokens were kept |
| `/tools` | opens the list of tools the model may call, and the bytes each description adds to every request |
| `/plan` | the steps of the task the model is working through, as it last wrote them |
| `/model [id \| number]` | on its own, opens the list of models the server offers, with its window and the one in use marked, and enter switches to the one under the cursor; an id or the number from that list switches from where you stand (the window follows the model unless `--ctx` fixed one) |
| `/think [level]` | how deeply the model thinks on the turns that follow; no level prints the current one |
| `/settings [list \| key] [value]` | on its own, opens a screen over the session with **every setting** in one list: the server row opens the connect-and-pick-a-model screen a first run starts with; switches turn over on enter, choices cycle with enter or the arrows, and counts, durations and text are typed in place and checked before they are written. On the thinking level it also shows the budget each level gives (minimal 1,024 … high 16,384, xhigh 32,768, max no cap), the selected one marked, so moving between levels shows what each costs. Each change goes to `settings.yaml` at once, and the ones a running session can take up (thinking level, verify, assume_yes, mouse, max turns and tokens, the safety margin) apply immediately; the rest are marked *next run*. Between turns only. `list` prints every knob with its default and who set it, a key explains one, and a key and a value write it |

A turn owns the conversation and its own settings, so `/reset`, `/compact`, `/undo`,
`/model` and `/think` wait for the turn in flight; `/ctx`, `/tools`, `/usage`, `/history` and
`/settings` answer straight away. `Ctrl-C` during a reply cancels that request and
returns to the prompt.

## The full-screen session

A plain `ykode` opens an alternate-screen session: the transcript scrolls, the thinking and
the answer arrive as one block each, a tool call shows while the model is still writing it, and
two rows at the bottom report where the session runs and what it costs. `--no-tui` keeps the line prompt; so does
a run that is not on a terminal, and `-p`/`--json`, which write to stdout.

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
| `up`, `down` | scroll the transcript; with more than one line in the box they move the caret. A trackpad or wheel in a terminal that is not holding the mouse arrives as these keys, so they never write into the box |
| `ctrl+p`, `ctrl+n` | walk the prompts you sent this session; a line you were writing waits at the end of the walk |
| mouse wheel | scrolls the transcript, once the screen has the mouse (`ctrl+t`, or `mouse: true`); scrolled up, the rule above the status says where you are and when new output arrived below |
| `end` | back to the end of the transcript (with an empty box) |
| `ctrl+o` | folds the plan to its heading, or opens it again |
| `ctrl+t` | takes the mouse for the wheel, or gives it back so the terminal can select and copy; the screen starts without it |
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
that `/reset`, `/compact` and `/undo` wait for the running turn rather than
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
`shortened old output`, `summarising older turns`, `folded 12 older turns`, `trimmed 3 tool results`,
`dropped 1 older turn` — said while it happens, because that is what a slow round-trip makes a reader
wonder about. It starts over with the next ask. Then the server's speed, `↑` reading the prompt and `↓`
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

`find_files`, `search_text`, `read_file`, `write_file`, `edit_file`, `exec_command` and `plan`,
plus `ask_user` where someone is there to answer it. The list is fixed for the whole session: Qwen
renders it inside the system message, so a tool that appears later throws away everything the server
had cached. The descriptions cost about 3.9 KB on every request, and `ask_user` adds about 0.7 KB
where it is offered; tests keep both under their budgets.
`find_files` lists one directory with `depth: 1`;
`read_file` answers with `outline: true` when a big file only needs its shape.
Every truncated result says how to continue — a `read_file`
window reports the next `offset`, a cut `exec_command` log reports the saved file. Nothing reads a
file it cannot hold: `read_file` and `search_text` stop at 1 MB and name the tool that can (a
`search_text` over a tree skips such files, one named on `path` is refused), and `exec_command`
streams its output through a window instead of waiting for all of it.
`edit_file` takes several `edits[]` in one call and matches them atomically, so a
bad pair never applies half an edit. `edit_file` and `write_file` answer with a
short diff, and every path they name is one the next call can use: project-relative
inside the root, absolute only for an approved outside path.

`search_text` answers in three sizes: matching lines (default), `mode="count"`
(matches per file) and `mode="files_with_matches"` (names only). The two summaries
cost a fraction of the tokens when the question is *where*, not *what*. Matching
lines read `path:line:text`; a file with several matches gets its path once with the
lines indented under it, including the lines of context, because the path is the part
that repeats.

`ask_user` stops the run and puts one question to the person, with the choices the model would
act on. On the screen it appears above the box with the choices numbered, and the answer is typed:
a number picks the choice it names (the model reads the choice in words, not the number), anything
else is the answer as typed, and an empty line or `esc` says there is none, which tells the model to
make the choice itself and say what it assumed. A question offers up to six choices; on a short
terminal the ones that do not fit are counted in one line, and their numbers still answer. The line
front end asks the same way on stdin. A run writing JSON lines, or reading a pipe with nobody at the
keyboard, is not offered the tool at all:
a call nobody can answer would only stall the run.

When a turn asks for several reads at once, `find_files`, `search_text` and `read_file`
run at the same time and their replies still come back in the order the
model asked for. Anything that changes something — a write, an edit, a command —
still runs one at a time, in order.

## Context and cost

Each turn's timing line reports the server's own prompt, cached and generated
token counts, and `/usage` adds them up. Those numbers also calibrate the
estimate used to set each reply's `max_tokens` (when no cap was asked for) and to
trim old tool output, so the context budget stops guessing.

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

Past 3/4 of the server's window, ykode stops paying for output the model has
already used. In order, cheapest first:

1. Older tool results are replaced with a summary the code writes — which call it
   was, how big it was, how it began — and how to get the rest back, whichever is
   cheapest: a `read_file` says to read the range again and a `find_files` or
   `search_text` says to run it again, because the disk is the source of truth and a
   stored copy goes stale the moment the model edits a file. A command's output is the
   one thing kept, in `~/.ykode/results/<run>/r7.txt`, because running a test suite
   twice costs minutes; the summary names the file to grep, the way a cut command log
   already does. The two newest results are never shortened, and a `read_file` of a
   window the model already has, of a file that has not changed, comes back as one
   line. That pointer lasts only while the text is still in front of the model: once a
   result has been shortened, trimmed or dropped away, the same read sends the text again.
   The same pass drops the text an older `write_file` or `edit_file` sent: the
   history keeps one line saying how much went where, because the file on disk has
   the rest. A call that failed keeps its arguments, so a retry still has them.
2. If that was not enough, the older turns are folded into a summary the **model**
   writes, in fixed sections (goal, constraints, progress done / in progress / blocked,
   decisions, critical context such as exact names and error text, next steps). The newest
   work stays whole: at least the two newest calls, and back to a fifth of the window
   (at most 20k tokens) where the window is known; a cut never separates a call from its
   results. A later fold updates the earlier summary instead of starting over, and the
   summary ends with the files the session read (`<read-files>`) and changed
   (`<modified-files>`), accumulated across folds and restored on resume. `/compact`
   asks for a fold directly.
3. Past 9/10 of the window the emergency brake applies: whole tool calls and their
   replies are removed, oldest first.

Result sizes scale with the window: 4.8 KB on a 4k context, 50 KB (as pi) on a 128k one. A gateway failure or a model still loading on a router
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
ykode sessions                     # the conversations that can be resumed (--limit N)
```

Flags come first, then what to look for.

`/undo` takes the most recent change off a small stack of snapshots that
`write_file` and `edit_file` leave behind: a file that was edited goes back to the
exact bytes it held, and a file that was created is removed. It is a stack, so a
second `/undo` undoes the change before that. If the file was changed after ykode wrote it, `/undo` refuses
rather than lose that work; `/undo force` puts the old text back anyway.

`--continue last` starts the newest conversation again; `--continue <id>` picks one
by id. An id is a timestamp to the millisecond, so enough of it to be unique is
enough (`--continue 2137`), and `ykode sessions` lists what there is with what each
was about. On the screen `/sessions` opens that list as a menu: enter puts the
conversation under the cursor in this run's place - the screen draws it, the model
reads it, and what follows is saved to its file, while the conversation the run gave
up stays saved to be come back to the same way. A conversation written in another
project is only named there, with the command that continues it in its own root. The
resumed run keeps the same id, so its results and its audit entries stay with its
conversation.

The screen opens with the conversation it came back to — the asks, the calls with what they
answered, and the recap of any fold — and `ctrl+p` still walks the earlier prompts of it.

The model's thinking is saved beside each message and shown again on resume (it is never sent
back to the server unless `send_reasoning` asks for it).

A resume reads back what the last run *ended with*: a fold is written to the log with its summary,
the files it covered, how many messages it took out and what the conversation cost before it, and
the older entries stay in the file, only no longer sent. The recap the model
wrote for it is saved with the conversation, and `/reset` marks the saved log as
started over, so nothing that was put away comes back. What a resume cannot save is
the trimming a turn does to fit the window, so a conversation bigger than the window
still sheds its oldest turns and keeps the newest.

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
hand; a folder that still holds the old `settings.json` is read for one more release, and the YAML file
wins when both are there.

`/settings` is the way in from inside a session. Alone it lists every knob with what it is now, and
marks the ones someone changed with what they were and whether a flag, the environment or the file
decided it; a knob nobody chose says nothing else, so the list reads as what is unusual about this
run. `/settings thinking_level` explains one knob — what it is for, its choices, how to change it.
`/settings tools.read_lines 40` writes that key into `~/.ykode/settings.yaml`, comments and other keys
kept, and says which way it moved; the next run reads it. A value the knob cannot take is refused
before the file is touched. `api_key` is reported as set, never repeated.

A project's file is not trusted just because it is there. `<root>/.ykode/settings.yaml` may set
`max_turns`, `max_tokens`, `thinking_level`, `verify`, `context.*`, `tools.read_lines`,
`tools.search_limit`, `tools.exec_timeout_sec` and `models.*` — how the project is read and what a
turn costs. Everything else waits until you trust the project: the server it talks to, any key,
`assume_yes`, `permissions.allow`, the audit log, and where command output is written. ykode names
what it held back, asks once when someone is at the keyboard, and records the answer in
`~/.ykode/trusted.json` as project root → the hash of that file, so editing it asks again.

```yaml
base_url: http://127.0.0.1:8080/v1
api_key_env: LOCAL_LLM_API_KEY
model: Qwen3.6-35B-A3B-GSQ-hybrid
max_turns: 100
verify: true
assume_yes: false
mouse: false
audit: true
audit_retention_days: 180
keep_sessions: 720h
max_tokens: 0
thinking_level: medium
stream_idle_sec: 120
context:
  safety_margin: 4096
tools:
  read_lines: 2000
  search_limit: 50
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
keeps room of its own), `mouse` gives the screen the mouse by default — the wheel then scrolls the
transcript, and selecting text needs `shift` or `ctrl+t` to hand the mouse back), `safety_margin` what is kept free beside
it for the chat template, and `send_reasoning` tells ykode to repeat earlier thinking for a chat template that
expects it back from tool-calling turns. `thinking` and `no_think` are the sampling a model
gets with its thinking phase on and off (`temperature`, `top_p`, `top_k`, `min_p`,
`presence_penalty`); anything left out is the server's default. A server that wants a bearer token takes it from
`api_key`, or from the variable `api_key_env` names — the second keeps the secret out of
the file, and an unset variable is no error.

A saved conversation ages out after `keep_sessions` (30 days by default), and the command output
that run saved go with it: a saved file is only named by the conversation that shortened it, so
the two survive exactly as long as each other.

A key the settings file does not recognise is an error naming the file and the key, not a
silently ignored line.

The per-user directory is `~/.ykode`, or `$YKODE_HOME` when that is set; command logs a cut
`exec_command` left behind live in `~/.ykode/logs`, and shortened tool results in
`~/.ykode/results`; both are pruned at startup after `keep_command_logs`, and the audit
months after `audit_retention_days`. `$YKODE_HOME/AGENTS.md` holds rules for every project
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
still read and write anywhere your user can. `--yes` skips the confirmation for it too.

`sandbox: true` in `~/.ykode/settings.yaml` closes that gap: every command then runs under
`sandbox-exec` (macOS) or `bwrap` (Linux) with no network and write access only to the
project, the temp folder and your cache folder (where build tools keep theirs). If the
sandbox tool is missing, the command is refused rather than run unconfined. A project's own
settings file cannot turn it off. Without it, run ykode as a user with access only to what
you are willing to let a shell touch, or in a container.

## Benchmarks

The task benchmark against pi lives outside this repository, in `~/Developer/testing-ykode`
(`./bench.py --agents ykode`); its README and `BASELINE.md` say how to run and read it.

## Development

```
make all     # build, vet, test
make test    # go test ./... -count=1
make cover   # with coverage per package
make fmt     # gofmt -w .
```
