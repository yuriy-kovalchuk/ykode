# Ideas

Improvements worth considering for ykode. Not committed work: an idea moves to `TODO.md`
(or straight into code) once it is decided. Bugs and fixes live in `TODO.md` too.

ykode only ever talks to self-hosted servers (llama.cpp, Strata), so ideas may rely on
features of those servers that a cloud-compatible agent could not use.

Format per idea: why, design, effort, open questions. Effort: XS < 30 min · S ≈ 1–2 h ·
M ≈ half a day · L ≈ a day or more.

References below are to what the tree holds now. Things the reduction (`f24211f`, `1aab3fb`)
deleted — `find_files`, `search_text`, `outline`, `recall` and the write store, `/undo`, the
audit log, the sandbox, `--json` — are named as history, not as code to build on.

---

## 1. Repository map

**Status:** proposed · **Effort:** L (Go only), more for other languages

A table of contents of the whole project: every file and what it defines — types, functions,
signatures — without bodies. A few thousand tokens describe a project that would take 100k+ to read.

**Why.** Without a map the model starts blind: list, search, open the wrong file, search again.
With one it goes to the right file and knows the real signatures, so it stops inventing APIs.
On small models that exploration is where the run most often loops.

**Design.**

- `go/parser` + `go/ast` over the files a `.gitignore` walk keeps, collecting definitions only.
  `outline`, which did this per file, was retired with the reduction, so the parser pass is new work.
- Rank without a graph at first: exported before unexported, files imported by many packages first
  (count imports in `ImportsOnly` mode), tests last, boost paths named in the conversation.
  A reference graph and PageRank only if the simple ranking is not enough.
- A **tool**, never part of the system prompt: the map would go stale as files change, and
  regenerating it would throw away the whole prompt cache. Registered at session start like any tool.
- Budget scaled with the window like a result: ~1k tokens at 32k, ~6k at 256k. Cut the
  lowest-ranked entries first and say how many were left out.
- Cache in the process by path + mtime + size, so a repeat call only re-parses what changed.

**Open.** Does the model call it on its own, or should ykode attach a small map to the first turn
of a request? Measure before deciding. And map size against benefit on a 32k window.

**Other languages.** tree-sitter covers 100+ but needs cgo, which breaks the single pure-Go binary.
Options: `universal-ctags` as an optional external binary (as `rg` and `fd` are today), per-language
regexes for definitions as the fallback, or `documentSymbol` from a language server (idea 3), which
gives the same map with no parsing in ykode at all.

---

## 2. Reuse llama.cpp features a cloud agent cannot

**Status:** proposed · **Effort:** S–M each · check which of these Strata's front end supports

1. **Save and restore the prompt cache** (`--slot-save-path`): `--continue` reloads the processed
   prompt instead of reading it again, which is minutes per resume on a large model.
2. **Pin a session to a slot** (`id_slot`): an eval run next to an interactive one stops evicting
   the other's cache.
3. **Exact counts from `/tokenize`**, at least to calibrate the bytes-per-token estimate.
4. **Grammar-constrained tool calls**: removes malformed calls from small models at the source.
5. **The chat template and settings from `/props`**: whether the template wants thinking back, and
   the true window, instead of what a setting claims.
6. **Prompt-processing progress** where the server reports it: a real bar for a long cold prompt,
   instead of raising `stream_idle_sec` by hand.

---

## 3. Feedback after an edit: hooks first, then a language server

**Status:** proposed · **Effort:** S (hooks) · L (LSP client) · M (what the client then gives)

**Why.** A model learns about a compile error one `exec_command` round-trip after the edit, if it
remembers to build at all. Small models are worst at exactly what a language server knows: does this
compile, where is this defined, who calls it, what is its signature. ykode itself stays free of any
language knowledge: every checker and server is an external program named in the user's settings.

**Phase 1 — hooks, no LSP code.** Commands from the user's own `settings.yaml` run after
`edit_file`/`write_file`, matched by file pattern (`**/*.go` → `gopls check {files}`, `**/*.py` →
`pyright --outputjson {files}`, `tsc --noEmit`, `cargo check --message-format short`). The smart
parts are generic: run once before the edit and once after, and append to the result only the
diagnostics that are new, errors first, plus a count of the rest; batch a turn's edits into one run;
cap the output like any result and leave the whole of it in the command log, which the result already
names for `exec_command` to read; a timeout per hook. Recognise the `file:line:col: message` shape
rather than write per-language parsers.

**Phase 2 — a protocol-only LSP client.** JSON-RPC over stdio with `Content-Length` framing and the
handful of message types used, defined locally, no library. Configured per language, never hard-coded:
`{"go": {"match": "**/*.go", "command": ["gopls"]}}`. Started lazily in the background the first time
a matching file is read or edited (indexing takes seconds to minutes), one process per language per
session, `initialize` with the root, `didOpen`/`didChange` after a write, `publishDiagnostics`
debounced ~300 ms and capped ~5 s. One tool, taking a symbol **name** and not a position, because
small models are bad at positions: `code_nav(definition | references | signature | symbols)` returning
short `path:line  signature` lines, project before dependencies, capped. Registered at session start
only if a configured server's command is on `PATH` — a tool appearing mid-session costs the cache.

**Phase 3 — what falls out of phase 2.** The repository map for every language through
`documentSymbol` (idea 1's Go pass becomes the fallback), and a safe rename: `textDocument/rename`
returns a workspace edit, applied through the same path as `edit_file`.

**Safety.** Hooks and servers come only from the **user's** settings, never an untrusted project's:
they are arbitrary code, and a language server may run project code (`go list`, build scripts). Say so
in the README and in the trust prompt.

**Open.** Is phase 1 enough? Measure turns spent on build errors before building phase 2. And
`workspace/symbol` quality varies by server: fall back to `grep -n` through `exec_command` when it
comes back empty.

---

## 4. Git checkpoint per request

**Status:** proposed · **Effort:** M

Before a request that may change files, record the working tree — e.g. a commit on a hidden ref such
as `refs/ykode/checkpoints/<session>/<n>`, made with a temporary index so the user's index and branch
are untouched — and let `/undo` restore a whole request at once, including files `exec_command`
changed. `/undo` and the write store it used to have were retired with the reduction, so this is a new
mechanism rather than an extension of one, and it only applies inside a git repository.

---

## 5. Pictures

**Status:** partly built · **Effort:** M left

Built (README has the detail): `read_file` answers a picture as an attachment, `internal/images`
sizes it for a vision encoder, and the server itself says whether the model takes pictures.

Open, in the order they are worth doing:

- `@path/to/screenshot.png` in the ask box, and a clipboard paste. The read path was built first
  because a path is what pi puts in; a real paste needs a per-OS clipboard reader.
- A setting for a proxy that loads a model it does not list: a switch keeps the capability of the
  model it left, which is right until the first such switch.
- Other modalities (pdf, audio) are not a plumbing gap: `/v1/chat/completions` has no content part
  for them. Convert to text with `exec_command`.
- Pixels on screen (kitty/iterm2) is its own subsystem and buys the model nothing.

---

## 6. A switch per tool

**Status:** proposed · **Effort:** S

Per-project settings are built (`/create-local-setting`, `<root>/.ykode/settings.yaml`, and the trust
rules that decide which keys a project may set). What is left is dropping a tool from the list the
model is shown: no `exec_command` in a deployment repo, no `plan` in a small one. A `/tools` switch,
decided at session start — the tool list never changes mid-session, and a tool that appears later
costs the cache everything before it. Open: per session or remembered in the project file.

---

## 7. Themes

**Status:** proposed · **Effort:** S

The screen's colours are built once in `internal/tui/palette.go` (`styleDim`, `styleThink`,
`styleBad`, …), so a theme is a map of those names onto colours in the settings file, with the
terminal's own capabilities deciding between 16, 256 and true colour. A light terminal is the reason
to want it: faint-on-white is unreadable today. Open: whether the syntax colours of a code block
(`internal/tui/markdown.go`, `internal/tui/highlight.go`) follow the same map or stay a separate
choice.

---

## 8. The OS sandbox that was retired

**Status:** to decide · **Effort:** M

`exec_command` used to run inside an OS jail: `sandbox-exec` on macOS with a seatbelt profile that
denied the network and every write outside the project, the temp folder and the user's cache; `bwrap
--ro-bind / --unshare-net` on Linux. It went with the reduction (`f24211f`), and `internal/tools/sandbox.go`
and its test are in that commit's parent if it is wanted back.

Two things it would answer today, both from the same gap: `HOME` is passed to a model-run command, so
`cat ~/.ykode/settings.yaml` reaches a token written into the settings file (`api_key_env` is the way
that keeps the secret out of any file); and `settings.yaml` used to accept `sandbox:` and `audit:` and
say nothing when it ignored them — those keys are refused like any unknown one now, so nobody reads
`sandbox: true` as a protection that is not there. What is left to decide is the jail itself: bring it
back behind a knob, or say plainly in the README that a command has the whole of `$HOME` and the network.

---

## Measured, do not retry blindly

- The system prompt is pi's shape, not a list of task rules (2026-11-08). Each rule fixed one task
  and broke another: "say so in your answer" padded task 05's answers, "just that value" then
  dropped the file name `read-the-note` needs, "follow a project file even if the code suggests
  otherwise" made the model trust a stale README (5 of 29 task-05 runs), "then verify" brought
  throwaway programs and "verify once" was the patch for those. The pi-style prompt, without any of
  them: 55/55 against 54/55, 24.7 s against 26.8 s a run, 9.2k against 11.9k prompt tokens
  (`testing-ykode` runs `20261108-021409` and `-023702`). It checks its work less often (task 03 in
  2 of 5 runs, task 09 in 1 of 5) without failing a task; if that starts to cost passes, the answer
  is running the project's check itself, not another rule. `TestTheTaskRulesStayOut` names the rules,
  so one comes back by decision.
- `chat_template_kwargs.reasoning_effort` is not sent: the Qwen template throws on a value it does
  not know, and `thinking_budget_tokens` already sets the depth on any llama.cpp server.
- Removing "do not write throwaway programs" (2026-11-07, task 03 ×5) brought one 89 s run of
  `go run -` and `/tmp` test files back; task 10 did not improve. The rule stays.
- Trimming a path description to "relative to the project root" made the model write the root's own
  path without its leading slash (3 of 5 task-11 runs, 0 in 397 runs before). The description now
  gives an example (`src/main.go`), and the path tools read such a path as the root anyway.
- `find_files` and `search_text` went with the reduction (2026-11-09), and two things came of it: the
  two dependencies they pulled had no test of their own, so `go mod tidy` dropped them with the code
  and nothing complained — which is why the unused check is part of `check` now; and the loop guard's
  "same call repeats" rule was not what kept a rewritten file's old listing short, its test still
  passing with the guard deleted because the fold had already replaced the listing. That test went
  when a second read of the same window started sending the text again: no memory of what was shown,
  so nothing to be told twice.
