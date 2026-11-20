# Ideas

One bullet per item: what it is, and the constraint or measurement that decides it. Not
committed work — an item decided enough to be written out gets a plan file of its own, deleted
once it is built. Effort: XS < 30 min · S ≈ 1–2 h · M ≈ half a day · L ≈ a day or more.

Things the reduction deleted (`find_files`, `search_text`, `outline`, `recall` and the write
store, `/undo`, the audit log, the sandbox, `--json`) are named as history, not as code to build on.

## Worth building

- **Repository map** (L) — every file and what it defines, no bodies: a few thousand tokens
  against 100k+ to read, and the blind list-search-open loop is where small models get lost.
  `go/parser` over the `.gitignore` walk, ranked exported-first and boosted by paths the
  conversation names; a tool, never the system prompt — a map there goes stale and regenerating
  it costs the whole prompt cache.
- **llama.cpp features a cloud agent cannot use** (S–M each) — save and restore the prompt cache
  (`--slot-save-path`) so `--continue` does not re-read a conversation; pin a session to a slot
  (`id_slot`) so an eval run stops evicting an interactive one's cache; exact counts from
  `/tokenize`; grammar-constrained tool calls; the chat template and true window from `/props`;
  real progress for a long cold prompt instead of raising `stream_idle_sec` by hand.
- **Diagnostics after an edit** (S, then L) — commands from the user's own settings run after a
  write and only the diagnostics that are new join the result, so a compile error costs no
  round-trip. Phase 2 is a protocol-only LSP client over stdio, configured per language, never
  hard-coded, with one tool that takes a symbol **name** because small models are bad at
  positions — and `documentSymbol` then gives the map for every language and a safe rename.
  Hooks and servers come only from the user's settings: they are arbitrary code.
- **Git checkpoint per request** (M) — a commit on a hidden ref made through a temporary index
  before a request that may change files, so one undo takes a whole request back, including what
  `exec_command` changed.
- **Pictures at the ask box** (M) — `@path/screenshot.png` and a clipboard paste; the read path
  is built, a paste needs a per-OS reader. A proxy that loads a model it does not list still
  needs a settings override: a switch keeps the capability of the model it left.
- **A switch per tool** (S) — drop `exec_command` or `plan` from the list the model is shown,
  decided at session start, since a tool appearing mid-session costs the cache everything after
  it. Per-project settings are already built.
- **The OS sandbox retired in `f24211f`** (M) — `sandbox-exec` with a seatbelt profile, or
  `bwrap --ro-bind / --unshare-net`, around `exec_command`; that commit's parent has the code.
  Without it a model-run command holds the whole `$HOME` and the network, so it reads every file
  you can read: the ssh keys, the browser's passwords, whatever is mounted.
- **An eval loop brought back** (M) — the same tasks run again, counting passes, seconds and prompt
  tokens; nothing like it exists in the tree any more. Without it the thinking table
  (`llm/thinking.go:14`, 1024 tokens at `minimal` to 32768 at `xhigh`) stays a guess, and so does
  whether older thinking earns the tokens it costs before that setting is on by default.
- **Budgets shown as the server gets them** (S) — `agent/budget.go:59-65` clamps every level to the
  room the reply has left, so on a small window `low` and `xhigh` are the same number while the
  settings panel and README still quote the nominal table. Showing the clamped figures is the fix;
  another clamp is not.
- **The room named where it ran out** (XS) — `agent/turn.go:150` and `agent/budget.go:49-51` say how
  full the window is but never name `reserve()`, the room held back for the answer that is what
  actually filled it, so the message reads as though the whole window were spare.

## Measured, do not retry

- The system prompt is pi's shape, not a list of task rules (2026-11-08): each rule fixed one
  task and broke another; without them 55/55 against 54/55, 24.7 s against 26.8 s a run, 9.2k
  against 11.9k prompt tokens. It checks its work less often without failing a task, and if that
  starts to cost passes the answer is running the project's check itself, not another rule.
  `TestTheTaskRulesStayOut` names the rules so one comes back by decision.
- `chat_template_kwargs.reasoning_effort` is not sent: the Qwen template throws on a value it
  does not know, and `thinking_budget_tokens` already sets the depth on any llama.cpp server.
- "Do not write throwaway programs" stays: removing it (2026-11-07, task 03 ×5) brought back an
  89 s `go run -` and `/tmp` test files, and task 10 did not improve.
- A path described only as "relative to the project root" made the model write the root without
  its leading slash (3 of 5 task-11 runs, 0 in 397 before): the description gives an example, and
  the path tools read such a path as the root anyway.
- `find_files` and `search_text` went with the reduction (2026-11-09) and their dependencies left
  `go.mod` with nothing complaining — which is why the unused check is part of `check` now. The
  loop guard's "same call repeats" rule was not what kept a stale listing short either: the fold
  had already replaced it, and its test passed with the guard deleted.
