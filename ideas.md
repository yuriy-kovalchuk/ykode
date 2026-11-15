# Ideas

Improvements worth considering for ykode. Not committed work: an idea moves to `TODO.md`
(or straight into code) once it is decided. Bugs and fixes live in `TODO.md` too.

ykode only ever talks to self-hosted servers (llama.cpp, Strata), so ideas may rely on
features of those servers that a cloud-compatible agent could not use.

Format per idea: why, design, effort, open questions. Effort: XS < 30 min · S ≈ 1–2 h ·
M ≈ half a day · L ≈ a day or more.

---

## 1. Repository map

**Status:** proposed · **Effort:** L (Go only), more for other languages

A compact table of contents of the whole project: every file and what it defines (types,
functions, method signatures), without bodies. A few thousand tokens describe a project
that would take 100k+ to read.

```
internal/agent/agent.go
  type Agent struct
  func New(client *llm.Client, registry *tools.Registry, model string, opts Options, sink Sink) *Agent
  func (a *Agent) Ask(ctx context.Context, prompt string) error
internal/agent/compact.go
  func (a *Agent) Compact(ctx context.Context) int
internal/tools/edit_file.go
  func locate(current, old string) (start, end int, fuzzy bool, err error)
```

### Why

- Without a map the model starts blind: list, search, open the wrong file, search again.
  Each step is a round-trip and context; with Qwen3.6-35B-A3B this exploration is where it
  most often gets lost or loops.
- With a map it goes straight to the right file, and knows real signatures, so it stops
  inventing APIs.

### Reference: how Aider does it

1. Parse every file with tree-sitter; collect definitions and references.
2. Build a graph: file A → file B when A uses something B defines.
3. Rank with PageRank, boosting files mentioned in the conversation or being edited.
4. Fill a token budget (1k by default) with the highest-ranked definitions.

### Design for ykode

- **Go first, standard library only:** `go/parser` + `go/ast`, the same code as
  `read_file outline: true` (`internal/tools/outline.go`), run over all files that pass the
  `.gitignore` walk.
- **Ranking without a graph at first:** exported before unexported; files imported by many
  packages first (count imports with `go/parser`'s `ImportsOnly` mode); tests last; boost
  paths mentioned in the conversation. A real reference graph (PageRank) later, if the
  simple ranking is not good enough.
- **A tool, not part of the system prompt:** `repo_map(path?, query?)` — whole project, one
  folder, or definitions matching a name. A map in the system prompt would go stale as files
  change, and regenerating it would invalidate the whole prompt cache (minutes of re-reading
  on Flash-Next). The tool list stays fixed for the session.
- **Budget:** scaled to the window like tool results, about 2–4% of it: ~1k tokens at 32k,
  ~6k at Strata's 256k. Cut lowest-ranked entries first and say how many were left out.
- **Cache inside the process:** keyed by file path + mtime + size, so repeated calls only
  re-parse changed files.
- **System prompt:** one line in the order of work: "Start with repo_map when you do not
  know where something is."

### Other languages

- tree-sitter covers 100+ languages but needs cgo, which breaks the pure-Go single binary.
  Options: a pure-Go tree-sitter port (check maturity), `universal-ctags` as an optional
  external binary (like `rg`/`fd` today), or per-language regexes for definitions
  (`def`, `class`, `function`, `fn`, `func`) as the fallback.

- **With a language server (idea 6, phase 3):** `documentSymbol` gives the same map for
  every language without tree-sitter; this Go-only version becomes the fallback.

### Open questions

- Does the model actually call `repo_map` on its own, or should ykode attach a small map
  automatically to the first turn of each request? Measure with the evals.
- Map size vs. benefit on 32k windows.

---

## 2. Reuse llama.cpp features cloud agents cannot

**Status:** proposed · **Effort:** S–M each · check which of these Strata's front end supports

1. **Save and restore the prompt cache** (llama-server slot save/restore, `--slot-save-path`):
   `--continue` reloads the processed prompt instead of re-reading it. On Flash-Next that
   saves minutes per resume.
2. **Pin a session to a slot** (`id_slot`): parallel sessions, or an eval run next to an
   interactive one, stop evicting each other's cache.
3. **Exact token counts** from `/tokenize`, at least to calibrate the estimate.
4. **Grammar-constrained tool calls** (JSON schema / grammar enforced by the server): removes
   malformed calls from small models at the source.
5. **Read the chat template and settings from `/props`:** detect "send reasoning back" or
   the true context size instead of relying on settings.
6. **Prompt-processing progress** where supported: a real progress bar for a long cold
   prompt, instead of raising `stream_idle_sec` by hand.

---

## 3. Git checkpoint per request

**Status:** proposed · **Effort:** M

Before each request that may change files, record the working tree (e.g. a commit on a
hidden ref such as `refs/ykode/checkpoints/<session>/<n>`, made with a temporary index so
the user's index and branch are untouched). `/undo` then restores the whole request's
changes at once, including files changed by `exec_command`, which today's undo stack misses.
Aider commits every change for the same reason.

Open question: only inside git repositories; keep the current undo stack as the fallback.

---

## 4. Hooks after edits

**Status:** proposed · **Effort:** M

Commands from `settings.json` run after `edit_file`/`write_file`, matched by file pattern
(e.g. `*.go` → `gofmt -l {file} && go vet ./{dir}`). Their output is appended to the tool
result, so the model sees compile errors right after an edit without spending a turn on
`exec_command`. Runs under the same confirmation rules as `exec_command` (allowlisted in
the user's settings only, never from an untrusted project's settings). Idea 6 (LSP),
phase 1, uses this mechanism for diagnostics.

---

## 5. Image input

**Status:** proposed · **Effort:** M

Qwen3.8-Flash-Next reads images, and the Strata deployment has its vision encoder. Allow
`@path/to/screenshot.png` in a prompt (and paste in the TUI), sent as an OpenAI-style
`image_url` content part with a data URL. Only for models whose `/models` entry lists
`image` in `input_modalities`.

---

## 6. Language-server (LSP) integration, with no language knowledge in the Go code

**Status:** proposed · **Effort:** S (phase 1) · L (phase 2) · M (phase 3)

Goal: compiler-grade feedback and navigation for any language, while ykode itself knows
nothing about Go, Python, Rust or TypeScript. All language knowledge lives in external
programs (`gopls`, `pyright`, `rust-analyzer`, `typescript-language-server`, …) named in the
user's `settings.json`. ykode only speaks the protocol, or only runs commands.

### Why

- Small models are worst at what a language server knows exactly: does this compile, where
  is this defined, who calls it, what is its signature.
- Today the model learns about a compile error one `exec_command` round-trip after the edit,
  if it remembers to build at all.

### Phase 1 — diagnostics through hooks, zero LSP code (builds on idea 4)

Most of the value is diagnostics after an edit, and most toolchains have a command for it:

```json
"hooks": {
  "after_edit": [
    { "match": "**/*.go",  "run": "gopls check {files}" },
    { "match": "**/*.py",  "run": "pyright --outputjson {files}" },
    { "match": "**/*.ts",  "run": "tsc --noEmit --pretty false" },
    { "match": "**/*.rs",  "run": "cargo check --message-format short" }
  ]
}
```

"Smart" parts, all generic:

- **Only what the edit caused:** run once before the edit (cached) and once after; append to
  the edit result only diagnostics that are new, errors first, plus a count of the rest
  ("3 new errors in agent.go; 12 warnings elsewhere unchanged").
- **Batch:** several edits in one turn run the hook once, at the end of the turn.
- **Budget:** capped like other results; the full output goes to the result store (`recall`).
- **Timeout** per hook; a slow checker never blocks the turn for long.
- **Parsing:** recognise the common `file:line:col: message` shape generically; a hook may
  also declare `"format": "json-sarif"` or similar if needed. No per-language parsers.

### Phase 2 — a generic LSP client (protocol only)

A small JSON-RPC client over stdio (`Content-Length` framing; only the handful of message
types used, defined locally, no LSP library). Configured per language, never hard-coded:

```json
"lsp": {
  "go":     { "match": "**/*.go",       "command": ["gopls"] },
  "python": { "match": "**/*.py",       "command": ["pyright-langserver", "--stdio"] },
  "ts":     { "match": "**/*.{ts,tsx}", "command": ["typescript-language-server", "--stdio"] }
}
```

- **Lifecycle:** started lazily in the background the first time a matching file is read or
  edited (gopls or rust-analyzer can take seconds to minutes to index), one process per
  language per session, shut down at exit. `initialize` with the project root.
- **Sync:** after `edit_file`/`write_file`/`/undo`, send `didOpen`/`didChange`; collect
  `publishDiagnostics` and wait briefly for them to settle (debounce ~300 ms, cap ~5 s).
  Diagnostics then replace the phase-1 hook for that language.
- **Navigation by name, not by position.** Small models are bad at exact line/column
  positions, so the tool takes a symbol name and ykode resolves it with
  `workspace/symbol` (or `documentSymbol` on a given file) before asking the server:

  ```
  code_nav(action: "definition" | "references" | "signature" | "symbols", symbol?: "Agent.Compact", path?: "internal/agent")
  ```

  One tool instead of five keeps the tool definitions small. Results come back as short
  `path:line  signature` lines, capped and ranked (definitions in the project before
  dependencies, non-test before test).
- **Fixed tool list:** `code_nav` is registered at session start only if at least one
  configured server's command exists on `PATH`; it never appears mid-session (that would empty the prompt cache).

### Phase 3 — uses that fall out of phase 2

- **Repository map for every language** (idea 1): `documentSymbol` across files gives
  definitions for Python, TypeScript, Rust… with no tree-sitter and no cgo. The Go-only
  `go/ast` map becomes the fallback when no server is configured.
- **Safe rename:** `textDocument/rename` returns a workspace edit; apply it through the same
  path as `edit_file` (confirmation, undo stack, audit log, diff in the result).
- **Hover for signatures/docs** when the model is about to call an unfamiliar function.

### Safety

- Server and hook commands come only from the **user's** `settings.json`, never from a
  untrusted project's: a language server is arbitrary code.
- Language servers may run project code (build scripts, `go list`, macros); note it in the
  README and in the trust prompt.

### Prior art

opencode and Charm's Crush feed LSP diagnostics back after edits; `mcp-language-server`
exposes LSP to agents through MCP. ykode has no MCP client, so phase 2 talks LSP directly.

### Open questions

- Is phase 1 enough? Measure with the evals (tasks passed, turns spent on build errors)
  before building phase 2.
- `workspace/symbol` quality varies by server (gopls is good; some servers return little
  before indexing finishes). Fall back to `search_text` when it comes back empty.'

---

## 7. Per-project settings, and a switch for each tool

**Status:** proposed · **Effort:** S

A repository may want ykode to behave differently: no `exec_command` in a deployment repo, no
`plan` in a small one, a smaller `--ctx` against a model with a small window. The trust rules
already decide which projects are trusted, so reading one more file there is a small step.

Design: a `.ykode/settings.json` in the project root, read after the trust check, holding the
keys the user settings already have. A `/tools` command (or `--tool off plan`) drops a tool from
the registry the model is shown, so the model never sees it rather than being told no. Open
questions: which keys belong per project - a window or a safety margin would disagree with what
the server reports - and whether a switch is per session or remembered.

---

## 8. Themes

**Status:** proposed · **Effort:** S

The screen's colours are built once in `internal/tui/palette.go` (`styleDim`, `styleThink`,
`styleBad`, …), so a theme is a map of those names onto colours in the settings file, with the
terminal's own capabilities deciding whether to use 16, 256 or true colour. A light-terminal
theme is the reason to want it: faint-on-white is unreadable today. Open question: whether
syntax colours for code blocks (`markdown.go`) follow the same map or stay a separate choice.

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
- `find_files` and `search_text` went with the reduction (2026-11-09). Two things learned: the two
  dependencies they pulled (`bmatcuk/doublestar`, `sabhiram/go-gitignore`) had no test of their own,
  so `go mod tidy` dropped them with the code and nothing complained; and the loop guard's "same call
  repeats" rule was not what kept a rewritten file's old listing short - its test still passed with
  the guard deleted, because the fold had already replaced the listing. That test went when a second
  read of the same window started sending the text again: no memory of what was shown, so nothing to
  be told twice.
