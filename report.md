# ykode — evaluation

Scope: the whole module — one command, ten internal packages, roughly thirty thousand lines including
tests. Built, vetted, formatting-checked, tested (plain and with the race detector), measured for
coverage, linted, cross-compiled for another operating system, and read end to end: the agent loop,
the tool layer, the transport, the settings layers, the session log, the screen. Findings marked as
reproduced were confirmed by throwaway tests written in the project's own style, then deleted.

Numbers: tests green with and without the race detector. Coverage — agent ninety-two, transport
ninety-three, session ninety, settings ninety, screen ninety, tool layer eighty-six, wiring thirty.
Total eighty-eight.

Nothing here is a crisis. What the codebase has instead is a large number of carefully-commented
decisions that quietly close doors, a context budget that throws away its only honest input whenever
it most needs it, a screen that will get slow in precisely the situation it was tuned for, and a
settings layer that has grown into six copies of itself.

---

## Reproduced defects

### A file read whose first line overflows the result budget panics the program

`internal/tools/read_file.go`. When the very first line of the window does not fit the remaining
budget, the cut length is computed as `t.aim() - b.Len() - 64`. Once the header line has filled the
buffer, that expression goes negative and is handed to `cutUTF8`, which slices out of range. A panic,
in a tool whose contract is "whatever went wrong becomes result text the model can act on".

Reproduced with four short lines and a result budget of `22` bytes:

```
panic: runtime error: slice bounds out of range [:-576]
  tools.cutUTF8(...)                       result.go:56
  tools.(*ReadFile).Run(...)               read_file.go:135
```

**How reachable it is, corrected after measuring.** Not reachable through the shipped configuration,
which the first draft of this report claimed. The result budget comes from one function,
`ResultBytesFor`, and that function floors it at four thousand eight hundred bytes whatever the
server reports, so the ceiling every cut uses never falls below about four thousand three hundred —
far above the length of the header line that would have to fill it first. The panic needs a caller that
sets a budget below about one hundred and fifty bytes, which only a test does today.

So the honest shape of the finding is: a latent trap rather than a live crash. The branch was written to
save a model reading a minified asset, it is one subtraction from being a crash, and the next caller
that computes a budget by hand — the report lists three places that already derive that number
independently — can walk straight into it. It is worth fixing for exactly that reason, and worth not
being described as a production crash.



### The settings screen can commit a value into the wrong setting

`internal/tui/settingsscreen.go`. `begin` opened the row under the cursor for typing but stored no row
identity; `typing` asked `s.current()` — the cursor *now* — which key to write, so enter wrote the typed
text into whichever row the cursor stood on at that moment. Values are validated against the kind of the
row they land on, so a number typed for one knob and committed into another knob of the same kind is
accepted and saved. **Reproduced and fixed**: the value now carries the key of the row it was started on.
Without the fix the test writes `max_turns: 41` into `context.safety_margin`.

**The route the first draft blamed does not exist.** It said the branch watcher, the spinner tick and a
run's events could move the cursor mid-typing. They cannot: the settings screen consumes key messages
only, so every other message falls through untouched, and while a value is being typed the keys go to
the text input rather than to the rows — an arrow key types instead of moving. Checked by holding the
screen open with a value typed in and pushing each of those messages at it. The cursor only ever moves
on an explicit arrow when nothing is being edited, which makes this latent rather than a live data
loss — still worth closing, because the safety of the write rests on nothing but that accident.

### Pinning the context window has no effect

`internal/agent/options.go` declares `CtxPinned` — "the window was fixed by a flag: a model switch
keeps it". `internal/agent/models.go` honours it. Nothing anywhere in the program assigns it; the only
assignments are in a test. `--ctx 8000` followed by a model switch on a server whose model reports a
different window silently replaces the number the user typed.

### The one real measurement of the conversation is discarded on every turn that matters

The budget layer learns the true prompt size from the server and keeps it as an anchor, so a turn's
cost is the anchor plus only what moved since. The comment is explicit about why: "the error of a byte
ratio is not paid again over the whole conversation every turn".

The anchor is zeroed by every path that makes room in the conversation:

- `internal/agent/compact.go` — every fold;
- `internal/agent/history.go` — every message the trim loop drops;
- `internal/agent/turn.go` — the overflow retry, which is the path that had to *learn* the estimate
  was wrong;
- `internal/agent/agent.go` — reset and switch-conversation.

A new ask does not clear it; only the above do. The only case that keeps it is a round-trip that ends
with a tool call, answered exactly and in order. So on each turn where the estimate decides whether to
fold, and on every turn after a fold, a drop or an overflow, the conversation is re-measured from
scratch out of a bytes-per-token guess.
The fold decision in a long session is always taken on a guess, and the comment describing the
mechanism that would make it exact describes a mechanism that does not run.

### The session log is append-only in principle and whole-copy in practice

`internal/session/session.go` defines a change record with a "replace the message at this position"
field, and the reader applies it correctly. Nothing in the program ever writes that field. What the
agent writes instead, from three call sites — the trim loop, the overflow path, and the history repair
— is a rewrite record holding the entire conversation serialised again.

One pass of the trim loop therefore appends the whole conversation to the file. A session that folds
and trims a dozen times carries a dozen full copies of itself, each containing the full text of every
file the model read or wrote. The stated reason for the append-only design is that a resume must read
the conversation back byte for byte so the server's prompt cache still matches; the mechanism chosen
for the most common mutation defeats the file size and the read cost while preserving the letter of the
principle. The partial record the format already supports, and already knows how to replay, is the fix.

### A settings file the program writes for itself reports a derived path as a user choice

`internal/config/presets.go` seeds a file whose keys all repeat the built-in defaults, and
`internal/config/catalogue.go` suppresses the "came from the file" mark whenever a value equals the
default. Every seeded key reports as unset except one, because `command_log_dir` is seeded empty and
the loader replaces an empty directory with `<home>/logs`:

```
keep_sessions          value="720h"      from=default
max_tokens             value="no cap"    from=default
tools.command_log_dir  value=".../logs"  from=file      ← the only one
```

The settings table therefore tells a user they chose a directory they never mentioned. The cause is a
key seeded with nothing in it, which is the one shape that is both a value in the file and no choice at
all. Fixed by leaving that key out of the seed and by not counting an empty scalar as a key the file
set, so a file already on disk is read correctly too.

The deeper gap this exposed was the way back: a file states a choice by naming a key, so absence is the
only thing on disk that means "use the default", and there was no way to reach absence. Writing an empty
value was refused, and the refusal said to delete the line by hand. `Set` now takes `unset` (or nothing)
as "take the key out", emptying a section the key leaves with nothing in it.

**A claim the first draft made and the code denies.** It said the screen writes a retention knob in the
whole-number form the loader then refuses, so that `keep_sessions: 30` produced a settings file the next
start could not read. It does not: both retention knobs are declared as durations, and `Set` rejects a
bare number with `keep_sessions wants a length of time, like 720h` before anything is written. The
whole-number kind is declared and never used — dead, not wrong. **The dead kind is now gone**, and a
test writes and reads back every knob the screen offers, so a future kind that is declared and never
accepted by the loader fails that test rather than reaching a user.

The rest of the claim did reproduce, and it stays reported: a key the loader cannot parse, or one
outside the schema, fails the start with the yaml error quoted
(`field typo_key not found in type config.fileSettings`). It is reported and the program exits, but a
single mistyped line in a settings file is a dead start rather than a warning next to the settings that
did read, and a settings file the program wrote itself is the likeliest place for one.

**I tried the lenient reading, and the tests said it is a decision.** Making an unknown key a notice
instead of a failure broke a test with the name `TestTheReplyBudgetHasOneName`, which exists to stop
`reserve_output` - a retired second name for `max_tokens` - from reading as though it had taken effect,
and three cases of `TestABadSettingIsRefusedByNameAndFile`. Those are pinned choices, so I reverted the
change rather than rewrite the tests. What was fixed is the part that was genuinely poor: a type error
in the file used to be reported as `yaml: unmarshal errors: line 1: cannot unmarshal !!str` plenty into
int`, naming no setting at all. It now leads with the key - `max_turns: yaml: ...` and
`tools.read_lines: yaml: ...` - by looking the key up from the line number yaml gives, so the dead
start at least says which line to edit.

### A model switch from the screen races the run

`internal/tui/modelscreen.go` opens the model picker; the picker's rows are delivered one message at a
time, and unlike `/sessions`, `/compact`, `/reset`, `/settings` and `/think`, this path has no check
that a turn is idle. Choosing a row calls `UseModel` → `SetEndpoint`, which assigns the agent's
transport client, its model id and its context size, while the run's goroutine reads all three to
build the next request. The comment on `SetEndpoint` states the precondition — "it is called between
turns, never during one" — and this call site contradicts it.

The same shape appears twice more, where the gate does hold but belongs to the caller rather than to
the agent: the switch-conversation path and the tune-options path. Both are reachable from a front end
without the agent ever refusing. The race detector cannot see any of this, because no test overlaps
the two paths.

### A server error written as a plain string becomes a decode failure

`internal/llm/types.go` declares a stream chunk's `error` field as a struct. Verified against the
package's own types that `{"error":"upstream model not found"}` fails the unmarshal. The user reads
"decode stream chunk: json: cannot unmarshal string into Go value of type …", and the much more useful
path — the matcher that explains a model is still loading — never runs, because it only inspects HTTP
bodies.

### A timings chunk erases the counts a usage chunk already brought

Reproduced against the package's own types. `internal/llm/stream.go` assigns a timings chunk over the
figures it holds wholesale, and accepts a usage block only while no prompt count has been seen. A server
that sends usage and then a timings chunk carrying speeds but no counts therefore ends the turn with no
counts at all:

```
usage first (5000 prompt, 4000 cached, 20 out), then timings without counts
→ prompt=0 cached=0 out=0
```

The same gate loses a cached-only count (`cached-without-total → prompt=0`), and the byte-ratio learner
then rejects the resulting figure by its sanity range silently. The consequence is no window budgeting,
no cache percentage and an empty `/ctx` on a server that was reporting perfectly well, with nothing said
to the user about why.

### One-shot mode cannot change a file, and says so only to the log stream

`internal/cli/view.go` refuses every confirmation unconditionally. As a default for a scripted run
that is right, but it means `-p` can never write or edit anything, and the model is told only "the user
refused this tool call", with no hint of the way out. The escape is the yes flag, which also allows
shell commands; there is no setting that allows file edits while keeping commands asked about. The
refusal notice goes to the log stream, so a caller capturing the answer stream never learns why
nothing happened.

### The module does not build for windows, and nothing would notice

`internal/tools/exec.go` sets `Setpgid` and calls `syscall.Kill`, both unix-only; a bare build for
windows fails. The release target in the Makefile names three targets, all unix. The tool's own
description string names the shell for the machine it was compiled on, so it would also promise
behaviour the code cannot deliver. Either the process-group code needs its other half, or a build
constraint and one line in the requirements section should say the module is unix-only.

### Formatting has not been run, and no linter is part of the checks

`gofmt -l` reports `internal/app/options.go`. The Makefile has a formatting target but does not
include formatting or a linter in `check`, so both drift.

### A zero-length file is tracked in the tree

`internal/tui/a` — empty, committed, from a commit named with a single letter.

---

## Architectural problems

### The screen object owns everything and reaches into everything

The model type holds the conversation handle, the resolved settings, the tool layer's display state,
the transcript, the scroll position, three separate overlays, the branch watcher, the run's cancel
function and the frame throttle. It calls the agent, the settings writer, the session helpers and the
transport directly — it constructs a transport client itself, in `internal/tui/settings.go`.

The agent has one clean boundary pointing outward, the event sink, and none pointing inward: it is
driven by direct calls in both directions. The cost is already visible. The scripted front end cannot
ask a question, because the question path was shaped around the screen's box. No screen behaviour can
be tested without a live agent. A second front end — a web view, an editor host, a batch harness —
would reconstruct the tangle.

### The screen re-derives the conversation it is shown

`internal/tui/resume.go` walks the saved message list and re-parses it into transcript entries:
recognising the fold marker, the ykode-note marker, the environment marker, re-attaching a tool answer
to a call whose row was trimmed away. Those markers are the agent's private format; the screen holds
its own copies of the strings and its own idea of what they mean, and nothing fails when the two
disagree.

The events the agent already emits are a complete description of a session. Replaying saved messages
through the path the live run uses would remove both the duplication and the drift.

### The agent is fifteen files over one struct with twenty-seven fields

That struct holds the loop state, the plan nag counters, the read and written file lists, the fold
bookkeeping, the byte-ratio estimate and its anchor, the per-ask environment note, the session-failure
flag, the waiting-ask queue, the published usage and its mutex, the context-work label and its mutex,
and the cached tool-description size. Each subsystem's state sits in the middle of the shared struct,
so changing any one of them means reading all of them to know what may be touched, and the ownership
rules — which fields belong to the run's goroutine, which may be read elsewhere — live entirely in
comments.

Grouping the meter, anchor and estimates into one type with its own lock, and the fold bookkeeping into
another, would make several of the races below unrepresentable rather than merely not-yet-written.

### The command surface lives in four places and one lookup table

A slash command may be answered by the agent, by the screen, by an overlay, or by some combination. The
one table that lists them all carries the name, the note, the argument shape and whether it waits for
the turn. Some paths consult the table, some bypass it, and `CommandIsSafeDuringATurn` answers "safe"
for any name it does not recognise — so a command that has never been described defaults to the
permissive answer, and the model picker is the live proof of what that costs.

One owner per command — name, handler, waiting rule and output shape in one place — removes the class
of problem the picker is an instance of.

### Six hand-maintained lists of the same settings

The defaults, the file shape, the withheld-key list for untrusted projects, the typed knob table, the
catalogue the screen prints, and the live-knob map that decides what a running session takes up at
once. Six lists of the same keys, agreeing by luck. The provenance bug, the unset-versus-zero
confusion and the two grammars for one duration are three drifts already present between them, all
reproduced above.

**Partly collapsed, and the rest measured as not worth collapsing.** The typed knob table and the
catalogue the screen prints were already one list when I checked. The live-knob map in the screen was a
hand-written list of six of the same keys and is now a column of that one table (`knob.live`, read
through `LiveKnobs`), so a key cannot be live in one place and missing in the other; a test asserts the
column and the table agree. The defaults already feed the seeded file (`builtinSettings` reads
`defaults()`), so the file cannot contradict them, and the withheld-key list is deliberately not the
same list as the knobs - most knobs are safe in a project file and a few are not, and that judgement is
the point of it.

What was added instead is a check the lists could not make themselves: every key the screen is willing
to write is written, read back, taken back and read again in a test. That is the drift that actually
bit - the screen writing a settings file the next start refuses to parse - and it is now caught for
every knob, at any kind, without keeping a sixth list.

### The session format grows without a version

A fold record, a change record and a boundary each add fields an older build cannot know, and the file
carries no marker. An older build reading a newer file cannot tell a field it does not recognise from a
line cut off mid-write, and its recovery for a cut line is to keep what it has and continue — so a
format change degrades silently into a shorter conversation rather than an error.

### No extension seam, though everything around one is ready

The tool interface is complete, the registry keeps registration order deliberately for cache stability,
the sink interface is clean, and the event vocabulary covers a run. But the tool set is a literal in
`internal/tools/tool.go` and the model's view of it is frozen at construction. The gap between
"extensible" and "hard-coded" here is one function and one settings key wide, which is worth naming
because the surrounding design was plainly built with that door in mind.

### The tool descriptions cost about six thousand bytes in every request

The screen reports this honestly, and the tool order is frozen specifically so a server's cached prefix
keeps matching — a good decision. Six thousand bytes is paid every round-trip and is a few percent of a
small window. The descriptions are well written; they are also reference prose for a reader who has
already read them, while the tool layer already knows how to word a call for a person in far fewer
words.

---

## Shared state without an agreement

The agent has no lock. Its own comments state the rule: the history belongs to the goroutine
answering; read the conversation between turns. The discipline is real and mostly respected — usage is
published behind a mutex, the plan list is behind a mutex, the waiting-ask queue is behind a mutex, the
transcript's row cache is only touched by the screen's goroutine — which is exactly why the exceptions
matter.

- ~~The run's cancel function has two owners, no lock, on the escape hatch.~~ **Checked and clean**:
  every path that touches it - starting a run, the key handler that cancels, and the clear when the run
  reports back - runs inside the screen's single `Update`. The run's goroutine only posts a message
  through the hub and never touches the field.
- `Conversation()` copied the slice but not the messages, and `sanitizeHistory` rewrites tool-call
  arguments inside the stored messages in place, so a reader holding the copy could see strings change
  underneath it. **Reproduced and fixed** - the copy takes the call list with it. A message coming
  through `add` is repaired before it is stored, so the case that reaches it is a conversation that came
  back from a file, which is exactly what the screen draws.
- The tool layer's options are written by the settings screen and read by a tool, with a comment
  asserting that nothing reads them during a turn. The refusal lived in one caller and nowhere else.
  The agent refuses now, so a front end cannot reach the options under a request being built from them.
- The idle watchdog is a timer, an unguarded "something has been read" flag and a cancel, mutated by
  the reading goroutine and fired by the timer's own.
- Several agent methods mutate state a front end reads — switch endpoint, tune options, switch model,
  switch thinking level, switch conversation, reset. All are described as front-end helpers; none
  checks that no turn is running.
- The detail a finished call hands the screen is guarded by the tool's class, which is the right
  instinct, but the class that means "may run beside others" is not excluded: the shell tool runs
  concurrently and keeps no state, so the guard finds nothing today. Nothing would stop a future
  tool that runs concurrently *and* keeps state.

The cheapest structural fix is not more mutexes. It is one rule enforced in code — a turn-running flag
these methods refuse when it is set — so the comments stop being load-bearing.

**Done, and one claim in this list was wrong.** The rule is now in the agent: a turn is claimed when it
starts and a second one is refused, and `SetEndpoint`, `Tune`, `SetModel`, `SetThinkingLevel`, `Reset`
and `ContinueFrom` refuse while it is held rather than trusting their caller. A fold claims the flag only
when nobody owns it, so one called from inside a turn leaves it for that turn to release - without that
the guard would release a run it never started. Enforcing the rule turned up a race the detector had
never seen: every event carries the number of the round-trip in flight, written by the goroutine
answering and read by any notice from elsewhere, and the refusal made that read deliberate. It is
atomic now.

The model picker was said to lack the idle check. **It does not**: it is declared as a command that
waits, and the gate refuses it mid-turn - checked by holding a turn open and typing each command.
What was real is what made the claim plausible: the gate lives in the screen, so the agent itself
accepted anything, and a settings change typed mid-turn was refused by neither.

---

## Where the design closes doors it did not have to

- ~~The yes flag can only be turned on, and there is no way to allow file edits while keeping commands
  asked about.~~ **Checked and clean on both counts.** The knob is on the settings screen and in
  `settings.yaml`, so it turns off; a change through the screen writes `assume_yes: false` and the next
  start reads it. And the allow list already does the finer thing: an entry naming `write_file` lets the
  writes through while `exec_command` is still asked about, which is what a probe of that exact
  configuration showed. What is true is narrower than the claim - there is no way to turn the flag off
  for one run from the command line, since the flag is one-way by design - which is a small ergonomic
  gap rather than a missing capability.
- **Answering "always" once to an edit or a write allows that tool for the rest of the run, for any
  file.** The key remembered from the prompt is the tool's name, not the path. **Left as it is, and
  narrowed where it could be without changing what a person is agreeing to.** An existing test pins the
  tool-level remembering on purpose, with a name that says so: the question asked on screen names the
  tool, so the answer given covers the tool - quietly deciding it covered one file would be reading a
  narrower consent into a wider question than the one that was asked. What was added is the narrow form
  at the other end: an entry in `settings.yaml` may now name one file (`edit_file:src/a.go`), it matches
  that file however the path is spelled, and it does not reach its neighbours. That is the granularity
  the report asked for, available to whoever writes it down, without silently changing what clicking
  "always" means.
- **Whether a command needs asking about is decided by a list of shell punctuation.** The list is a
  good approximation of a shell and not the shell. A path containing a space makes a plain command look
  chained and forces an unnecessary question; anything the list misses makes a chained command look
  plain — and those are the lines that then get remembered as always-allowed.
- ~~A leading `cd` into the project root is stripped including inside quotes, losing the quoting.~~
  **Checked and clean.** The whole match, delimiter included, is taken off - `cd '/my project' && ls`
  becomes `ls`, not `'my project' && ls` - and a quoted directory that is not the root is left exactly
  as it was, so `cd '/other place' && ls` is untouched. Checked by driving the function with quoted
  paths containing spaces, matching and not matching the root, and with a quoted argument further in.
- ~~Saving an endpoint or a knob from the screen marks the project's settings file as trusted.~~
  **Reproduced and fixed.** Declining a cloned project's withheld keys left the file unaccepted, and the
  first `/settings` change into it recorded the file as accepted whole: held keys went from two to none
  across one save of `max_turns`, and the cloned `base_url` and `assume_yes` took effect on the next
  start with nothing ever accepted. The acceptance is now withheld when the file still asks for an
  unsafe key the change itself did not write.
- **Trust is keyed by directory path, not by project.** Two checkouts of one repository, or a directory
  reused after a version-control reset, carry the trust across without being looked at again.
- **A trusted project sets conversation retention for the whole machine**, because every project's
  conversations live in one folder.
- **Every command the model runs is given the home directory in its environment.** The shell is openly
  documented as unconfined, which is honest; the documentation could also say where the boundary does
  sit — dot-files in the home directory are readable by every command the model runs.
- **Two notions of the project root.** The file tools work through a directory handle and follow the
  root as it moves; command execution works from a recorded string. The confinement itself was verified
  to hold — a symbolic link inside the project pointing outside is refused by the operating system's
  own handle check, both for reading and for writing — so this is not a hole. It is a second
  implementation of "here" that the command layer did not need to have.
- **Instruction files are cut by a plain prefix trim.** The cap that scales with the window is well
  argued, and the cut tells the model to read the rest. What it cannot say is which section it stopped
  inside, so a project whose rules are cut mid-section leaves the model following half a rule with no
  hint of which one.
- **The system prompt lists the tools by hand.** The question tool is registered at run time, only when
  the front end can answer. The list and the registry cannot see each other, so the prompt can name a
  tool the model was not given — a mismatch the project's own comments record having caused trouble.
- **Retired settings are still parsed, and unknown keys are a hard error.** Four keys that do nothing
  are accepted; a file carrying a key the program deleted fails to load at all. Deleting a key is
  therefore a breaking change to every file that mentions it.
- **`/model` with an argument asks the server for its model list twice** — once to answer and once to
  resolve — two round-trips to a server that may be starting a model.
- **A fold's request is sent twice** when the model answers the first with a tool call, which is common
  for small models, and the second re-reads the whole prefix. Tools could be refused on the first
  attempt for a server that has already done this once.

---

## Slowness and memory, each with its fix

The screen is measured and careful, and it shows: transcript rows are cached and rebuilt only from the
first entry that changed; markdown blocks are laid out once; syntax colouring is chunked and cached at
three granularities; the transcript window copies rows rather than measuring them; streaming is
throttled to thirty frames a second; command output arrives through a fixed window with the overflow
spilled to a file. This is better than most. What is not bounded is history.

- **Every transcript entry ever drawn stays in memory for the life of the run**, with its rendered rows
  and the full text of every change under a tool row. Nothing ages out and nothing is capped. **Partly
  fixed, partly declined**: what one change costs is now capped (see the byte cap below), which is the
  part that could reach megabytes. Folding entries the reader has scrolled past is **not done**, and the
  measurement says why it is not urgent: a redraw of the cached rows costs about 10 nanoseconds with
  nothing changed, because only the blocks marked stale are drawn again, and taking the 80 rows a screen
  shows costs about the same. Entries therefore cost memory, not time, and the amount is bounded by what
  a session actually says rather than by a file that can grow without limit.
- **A resumed conversation is replayed into the transcript in full at start-up**, on the screen's own
  goroutine, before the first keystroke. **Left as it is**, with a measured reason: the replay builds
  rows once, and reading them again costs nothing, so the cost is a one-time start-up charge against a
  conversation the notice tells the user to scroll up and read. Lazy replay from the oldest visible row
  would put a moving window in the middle of the one component where a mistake loses the reader's
  position in what they were reading.
- **The token estimate walks the whole conversation on every call**, and is called four times per
  round-trip plus once per message inside the trim loop. The tool-description size is memoised and the
  history size is not, for no visible reason. **Fixed**: the bytes are kept as the conversation changes.
  They are trusted only while the message count still matches, so a path that rebuilds the conversation
  cannot leave a stale total driving the fold decision and the response cap.
- **Every slash command that prints a list rebuilds the catalogue and re-reads both settings files from
  disk**, and the settings screen recomputes its header wrapping on every keystroke. **Measured and
  declined**: a catalogue build is about 7 microseconds, and it happens when a command asks for a list,
  not per keystroke or per frame. A cache invalidated on write would add a stale-settings hazard to save
  a rounding error.
- **The thinking-rate estimator appends a timing sample per chunk and trims from the front by
  re-slicing.** *Fix:* a fixed ring. **Checked and left alone**: the window is the last second of
  samples, so it holds what a stream sends in one second rather than growing with the reply, and
  `p.window = p.window[i:]` copies nothing - it moves a slice header. A ring would add an index to
  maintain for no measurable gain.
- **A fold is allowed a summary of a fixed six thousand characters**, which does not scale with the
  window the way every other cap in the same file does; on a small window it is a large part of the room
  the fold was trying to free. **Left as it is**: the summary's *token* budget already scales
  (`compactRoom` is an eighth of the window, floored), and the character cap only ever truncates a
  runaway answer to that budget. Making the second number scale would change what a fold keeps on small
  windows, which is a behaviour change rather than a fix.
- **The request refuses to send when the estimate passes the ceiling, and the ceiling is derived from
  the same estimate.** The reactive path then corrects the estimate after the server has refused the
  request already — a deliberate design choice, and an expensive one, since each correction costs a
  full round-trip of server time.

---

## Practices that invite drift

- **Comments do the work of types.** Several invariants a type, a lock or a constructor would enforce
  are stated in prose: only tools that never run beside each other may keep display state; nothing reads
  the options while a turn is in flight; read the conversation between turns, not while one runs. The
  comments are unusually good and unusually load-bearing, and each is a rule a future change can break
  with no tool noticing.
- **A tool failure becomes prose and is then re-read as prose.** The marker at the front of a failure
  string is parsed by the plan restorer, by the transcript's failure state, by the call row's colour and
  by the exit-status reader. The tool layer already has one place where a result is returned as both a
  message for the model and a structure for the screen; the same split everywhere would let the marker
  go away.
- **Two constructors exist for the agent**, differing in whether the question tool is attached, and
  neither is documented as the canonical one. The wiring uses the one that attaches it; anything built
  through the other behaves differently in a way invisible from the call site.
- **Start-up reads the settings twice**, once before the trust question and once after, and always seeds
  the settings file and walks both retention folders before doing anything the user asked for. A
  read-only home directory turns an ordinary run into an error before the first request.
- **The pruning loops stop at the first entry they cannot stat**, leaving everything after it in the
  folder forever.
- **One byte cap is computed in three places** — the tool layer's scope, the command tool, and the agent
  when it formats a result — each subtracting its own slack from the same budget. They currently agree,
  by three separate pieces of arithmetic.
- **Write results to the terminal are never checked**, and the linter that would say so is not part of
  the checks. The scripted answer stream is the worst place for it: piping into something that stops
  reading early fails every write, reports nothing, and exits zero.
- **Terminal detection is a mode-bit test rather than asking the terminal**, which calls `/dev/null` a
  terminal and would accept a character device that is not one.
- **The trust question reads one line from standard input with no timeout**, after having established the
  stream is a terminal — a terminal nobody is answering hangs start-up indefinitely.

---

## Corrections made to this report while verifying it

Five claims in the first draft did not survive being checked against the code, and one was inverted. They
are recorded rather than quietly deleted, because a reader acting on this report needs to know which
findings were tested and which were not.

- **Wrong, and it was the headline finding of the first draft** — that a call's arguments are validated
  and then the unvalidated bytes are handed to the tool. They are not. `repair` writes the unwrapped
  bytes back into the message before anything else reads it, and the unwrap therefore reaches the tool,
  the confirmation text, the permission key and the path targets. Verified end to end through a stub
  server and a tool that prints the bytes it was handed:

  ```
  server sends arguments as  "{\"path\":\"a.go\"}"
  bytes the tool received: {"path":"a.go"}
  ```

  The three discarded return values the finding was built on are real, and each discards the value
  because the work was already done elsewhere — which is worth a comment at each site so the next reader
  does not reach the same wrong conclusion.
- **Wrong** — that a tool call written as text inside an answer runs without a confirmation. It goes
  through the same runner as a real call and is asked about the same way.
- **Wrong** — that the trim loop protects the oldest message because a three-character fragment of an
  appended note matches it. The comparison is exact, the note is not involved, and the protection is what
  the code intends.
- **Wrong** — that a plan step whose text contains another status marker is restored twice. A line can
  match one marker only.
- **Overstated** — that the conversation is re-measured at the start of every ask. A new ask keeps the
  anchor; only a fold, a drop, an overflow, a reset or a conversation switch discards it. Still every
  turn that matters, not every turn.
- **Understated** — the transport counting bug. A timings chunk does not merely fail to merge with a usage
  block; it overwrites the counts already gathered, which is a stronger defect than the one first written
  up.
- **Inverted** — a model switch was reported as silently changing the sampling sent to the server. It is
  the reverse: the switch leaves the previous model's sampling in place and never picks up the new
  model's card.
- **Held up under testing** — the file reader's panic, the settings screen's row identity, the unused
  pinned-window option, the session log's whole-conversation rewrites, the seeded file's false
  provenance, the duration grammar mismatch, the model picker racing a turn, the stream error that cannot
  be a string, the tool-call ordering, the failed windows build, the unformatted file and the tracked
  empty file.
- **Reachability overstated, then measured** — the file reader's panic is real but not reachable through
  any configuration the program can produce: the one function that supplies that budget floors it at four
  thousand eight hundred bytes. It is a latent trap for a future caller, not a live crash. The defect and
  the fix both stand; the severity does not.
- **Checked and clean** — symlink confinement through the project root is refused by the operating
  system's directory-handle check for both reads and writes; the file listing's truncation count is
  correct; the manually triggered fold does run the history repair; and the idle watchdog's flag is not
  raced — the timer callback touches only the cancel and the timer, never that flag.
- **Called a bug and is a decision** — the order tool calls come back in. An existing test pins it with a
  comment explaining the intent. The fix was reverted rather than the test rewritten: a test that
  documents a deliberate choice is evidence about the design, and it outranks a finding.

## Smaller defects

- The oldest message the trim loop refuses to delete is the one whose text equals the current ask. The
  match is by exact content, and the ask may carry a note appended to it, so any first ask longer than
  three characters is matched by a three-character fragment of that note and becomes permanently
  undeletable in the oldest part of the history.
- The repetition guard counts whole lines, so a reply that repeats itself inside one long line is not
  caught — and the guard is armed for the answer as well as the thinking, so a model that legitimately
  repeats one long sentence ten times in an answer has the reply cancelled and its partial text
  discarded.
- A tool-call fragment whose index arrives out of arrival order is stitched into the right call but
  handed back in arrival order. **Not a defect on inspection**: an existing test pins that ordering with a
  comment saying the call that started first comes back first, which is a deliberate choice about what a
  server's interleaving means. Recorded here because the first draft called it a bug and it is not one.
- A stream that ends with no `[DONE]` and no finish reason is reported as possibly cut short. That is
  right for a dropped connection and wrong for servers that simply never send either marker, and the run
  cannot tell the two apart.
- The idle watchdog's "has anything been read yet" flag is written by the reading goroutine and read
  after the stream ends to decide which timeout to report; it is the one field in the transport whose
  value decides what a user reads, with no owner.
- The room a fold may use is derived from the window in one function and charged against it in another,
  with the reserve applied on both sides of the comparison. The arithmetic works out; it reads as
  double-counted, which makes it hard to change safely.
- The trim loop can never drop the recap, the current ask, or the newest call, so a conversation whose
  remaining turns are individually huge cannot be made to fit at all; the request is then refused with
  advice to reset, which is the right answer but arrives after the work of trying.
- The retry path treats a refused connection as retryable, waits once, and reports only the second
  failure — on a cold server the first attempt's reason, which is the interesting one, is discarded.
- ~~Switching the model updates the model id, the window and the transport client, but not the sampling
  or the send-reasoning flag, which were resolved from the *previous* model's card at start-up and are
  never looked up again.~~ **Reproduced and fixed.** A probe held two cards and switched: the window
  moved, the id moved, and the temperature sent was still the model that had been left. The run loop now
  carries the per-model cards a settings file wrote (`Options.Cards`), and a switch takes the card of the
  model it is moving to. A model with no card keeps what is in use rather than dropping to empty, because
  nothing is known about it and the server's default is not necessarily what had been working. A settings
  change mid-run refreshes the cards too, so a `/model` after editing the file sees the edit.
- ~~A model the server does not list carries no window, and the switch path then keeps the previous
  window rather than reporting the size as unknown.~~ **Kept as it is, and made explicit in the line the
  user reads.** An unknown window switches the budget off entirely - the history then grows until the
  server refuses, which is the worse failure - so the previous window is carried over. The message now
  says which number is being kept and that the server does not list the model, so the carry-over is not
  mistaken for the model's own size.

---

## What this codebase does well, so the above reads in proportion

- Test density and quality are high. The tests exercise truncated streams, half-written files, cut-off
  calls, resume across a fold, and the argument shapes models actually send, rather than happy paths.
  Every defect above was found by reading and then confirmed by writing a test in the project's own
  style, which took minutes each time — that is a sign of a testable design at the unit level.
- Every non-obvious decision carries a comment saying what went wrong before it was made. That is rare,
  and it is the single most valuable asset in the repository.
- The event sink is a genuinely clean boundary: the agent prints nothing itself, and both front ends are
  consumers of one stream.
- Streaming is treated as the only mode. Timings, cache statistics and speculative-decoding acceptance
  are used properly. The prompt cache is respected deliberately and thoughtfully — tool order frozen,
  environment notes moved onto the newest message specifically so the cached prefix does not move. That
  last decision is the kind most agents get wrong.
- Command output is bounded in memory by construction, with the whole text spilled to a file and a
  pointer handed to the model.
- File confinement is done with the operating system's own directory-handle machinery rather than string
  comparison, and it was verified to hold against a symbolic-link escape.
- The settings-trust model — withholding dangerous keys from an untrusted project's file, hashing the
  file so any edit re-asks — is better than what most tools in this space ship.
- Screen drawing is performance-conscious at the level of the block, the frame, the grammar and the row,
  and the comments say what was measured.

---

## The order to work in

First, the panic in the file reader's byte-cut guard — latent rather than live, as corrected above, and
one function wide.

Next, the cheap and dangerous ones: the settings screen committing into the wrong row, the duration the
screen writes that the loader refuses, the settings value that cannot be cleared, the pinned window
being overwritten, the command-lookup default of safe, the stream error field that cannot be a string,
the usage counts that zero the prompt size.

Then the budget's memory. Keep the server's real count across folds, drops, trims and new asks instead
of discarding it, and make the history byte total incremental. Those two changes are what make the rest
of the context machinery honest rather than approximate.

Then the session log: write the partial change record that already has a reader, and stop appending
whole conversations.

Then the seams that only prose protects: one owner for the cancel function, a turn-running guard on the
between-turns methods, one place that decides whether a command waits, one constructor for the agent,
and one list of the settings that generates the other five.

Then the transcript's unbounded growth, which is what a heavy user will feel first.

Then the leftovers: the tracked empty file, the unformatted file, the unused helpers, the retired keys,
the linter and the formatter in the checks, and a decision about whether the module is meant to build
anywhere other than unix.
