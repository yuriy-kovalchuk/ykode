# TODO

Open work only. When an item is done, delete it from this file in the same change. Undecided
ideas live in `ideas.md`.

Last full scan: 2026-11-07. `go build`, `go vet` and `go test -race ./...` pass.
Effort: XS < 30 min · S ≈ 1–2 h · M ≈ half a day · L ≈ a day or more.

## Rules for the work

- One ID per change, with a test that fails before the fix.
- Every `go test` gets `-timeout` (mutation checks `30s`, restore files in `try/finally`).
- Live and pty tests get a hard deadline: poll for exit, then `SIGKILL`. A killed run is a
  failure, never a pass.
- `go test -race ./...` for anything that touches parallel tools, the TUI or shared maps.

## Open

From the pi benchmark (`~/Developer/testing-ykode/BASELINE.md`, round 2): ykode now ties or beats pi
on search and simple edits; it loses where it verifies without a plan. Rerun `./bench.py --agents ykode`
after each item.

- **N1 · done in code, one line left to write.** Both prompts now say *follow* a project file's rule
  "even if the code suggests otherwise, and name that file when it is the reason for your answer", and
  add "Answer in the form the user asked for…", pinned by `TestBothPromptsHoldTheRequestedAnswerFormat`.
  Measured on Strata: 05 0/5 → 5/5, 07 2/5 → 5/5, full suite `runs/20261107-195613` 55/55 (was 47/55),
  21.6 s, 5.2 req, 201 answer chars. Left: the round-6 line in `BASELINE.md`.
  Lesson for F1: `cmd/eval`'s `read-the-note` check (`tasks.go:207`) demands the *answer* name the file
  that states the rule, so an unconditional "say so in your answer" and a blanket "no note" both break
  something — only the scoped wording satisfies both (eval 3/3, bench 5/5).
- **T1 · S · Response ceiling from the context, not a constant.** `defaultMaxTokens = 4096`
  (`internal/config/config.go:23`) caps one reply and thinking is billed against the same number, so a
  long think leaves nothing for the tool call: on an enriched task-11 prompt, turns 4 and 6 spent the
  whole 4096 (≈12.4k chars of thinking) and the `write_file` arguments were cut — `finish=length`, the
  "split it into two calls" note, two wasted turns. Make the default "no cap" (0 → field omitted; it is
  already `omitempty`, `internal/llm/types.go:62`, and `normalize()` stops rejecting `<1`) and clamp to
  the room the window has left, which `budget.go:40` already computes. Raise `defaultSafety = 256`
  (`config.go:31`) toward pi's reserve of 16384 (`~/Developer/pi/packages/coding-agent/src/core/settings-defaults.ts:11`);
  pi itself sends `max_tokens` ≈ `ctx − input − max(4096, 15% input)`
  (`~/Developer/pi/packages/ai/src/api/simple-options.ts:26-35`). Keep the `finishLength` handling: the
  window can still cut a reply. Tests: 0 omits the field; an over-large value clamps to the room; a cut
  call is never run. Benchmark note: until T1–T3 land, the pi columns are not like-for-like — pi had a
  real thinking budget and effectively no output ceiling.
  → **done**: the default is no cap (`max_tokens: 0`); a known window clamps every reply to
  `ctx − used − margin`, and `defaultSafety` went 256 → 4096 so an uncapped reply has room for a whole
  answer. With no cap and no known window the field is omitted. `internal/agent/budget_test.go` pins both.
- **T2 · S · Thinking gets its own budget.** ykode's only control is thinking on/off (`--no-think`);
  reasoning arrives in the same response (`internal/llm/types.go:18`) and shares `max_tokens`.
  llama-server takes `thinking_budget_tokens` per request (alias `reasoning_budget_tokens`) — checked on
  Strata: 40 cut reasoning to 208 chars, no error. Reference implementation: `pi-llama-cpp`
  `src/managers/events.ts:157-169` with `src/constants.ts:52-60` (`off 0 · minimal 1024 · low 2048 ·
  medium 8192 · high 16384 · xhigh 32768 · max = send nothing`). Send `reasoning_effort` in
  `chat_template_kwargs` too — ykode already uses that map for `enable_thinking`, and Strata's Qwen
  template defaults to `xhigh` ("think carefully, consider plausible alternatives"), which is why
  thinking reaches 12k chars.
  → **done**: `thinking_budget_tokens` goes on the request (`internal/llm/thinking.go` holds the
  level→budget table) and the budget is capped to `max_tokens − 1024` so thinking can never starve the
  answer. Deliberately *not* sent: `chat_template_kwargs.reasoning_effort` — the Qwen template throws on a
  value it does not know, and the budget already sets the depth on any llama.cpp server. Never let the budget starve the answer; pi keeps 1024 tokens for it
  (`simple-options.ts:83,103`).
- **T3 · S · Thinking levels, the same names pi uses.** `off · minimal · low · medium · high · xhigh ·
  max`, default `medium` (`packages/coding-agent/src/core/defaults.ts:3`), clamped to what the model
  supports. Settings key `thinking_level` replaces the `thinking` bool, `--no-think` becomes
  `--thinking off`, and `/thinking` cycles the level with Ctrl+S to save it, as pi does. Per-model
  sampling already has a home: `models.<id>.thinking` / `no_think`. Needs T2. This is what lets the
  benchmark compare the same thing: pi ran its default `medium` (8192-token budget) while ykode ran
  uncapped thinking inside 4096.
  → **done**: the levels exist in settings (`thinking_level`), flags (`--thinking LEVEL`, `--no-think` is
  shorthand for `off`) and `/think [level]`, all validated by `llm.ThinkingLevelValue`. **Breaking**: the
  `thinking: true|false` settings key is gone; the strict unknown-key error names it. Left out on purpose:
  pi's Ctrl-S save and a level-cycling binding — `/think` sets the level for the run, the settings file
  makes it permanent.
- **B1 · XS · The session recorder chokes on a cut-off tool call.** A reply cut at `max_tokens` mid-call
  reaches the recorder and logs `session: json: error calling MarshalJSON for type *jsontext.Value:
  unexpected end of JSON input` (enriched task-11 run, turn 4). The agent refuses to run the call
  (`internal/agent/agent.go:182-188`) but appends the message with truncated raw arguments anyway. Record
  cut-off arguments as text; test that a truncated call survives both recording and replay.
  → **done**: `storable()` (`internal/agent/repair.go`) replaces a cut call's arguments with `{}` on the
  copy that goes into history and the session log, before either encoder sees the broken bytes; the
  original still reaches `toolCallsRunnable`, so a cut call is still never run. Pinned by
  `TestACutOffToolCallIsStillSaved`.

V1 and V2 are done: Flash-Next on Strata follows "verify once" every time, Qwen3.6 on CUDA only
sometimes (BASELINE.md rounds 3–4). V3 and V4 do not depend on the model obeying.

- **V3 · M · The project's check runs when the model is done.** A `check` command (user settings, or
  a trusted project's) that ykode runs itself when the model answers; a failure goes back as the next
  message. The model stops spending turns on verifying, and the check is always the right one. Needs
  the S1 trust rules: an untrusted project cannot set it.
- **V4 · S · Format after edits.** Run the configured formatter (e.g. `gofmt -w`) on files an edit
  touched, and report it in the result: this model often writes a tab plus a space (seen in both
  agents), and fixing it costs ykode extra edits. Idea 4 in `ideas.md`.
- **F1 · S · Pick the system prompt.** The candidate is `newSystemPrompt` in
  `internal/agent/newprompt.go`, behind `--new-prompt`: fixed rules first and the session's
  environment last (cache-friendly), no tool list (Qwen's template already renders the tools),
  an explicit reply format. Run `make eval ARGS="--repeat 5"`, then
  `make eval ARGS="--repeat 5 --new-prompt --baseline last"`, on Qwen3.6-35B-A3B first, then
  Qwen3.8-27B and Qwen3.8-Flash-Next. Compare pass rates, turns, tokens and the counters
  (call-as-text, unchanged-reread, long-answer, nudge). Keep the winner as `SystemPrompt`;
  delete the other and the flag.
- **B2 · S · A run that repeats itself never notices.** CUDA round 7, task 05: 164 s, 25 requests, 24
  `search_text` calls, and the run stopped at `--max-turns (25)` with an **empty answer** — the bench
  counted 8 identical repeated calls. The same task passed 5/5 on Strata (round 6), so the looping is the
  small model's, but ykode watched it repeat and said nothing. ykode already counts repeats for the eval
  (`cmd/eval`) and remembers every call for `/history`; use that: after the same call (tool + arguments)
  is asked a third time, tell the model in-turn that it is repeating and what it has already learned, and
  if it repeats again, stop with the best answer so far instead of burning the whole turn budget. Test: a
  stub server that returns the same search call each turn asserts the notice arrives before turn 25 and
  that the run ends with an answer, not a `--max-turns` error. Same task on CUDA, `--repeat 5`, is the
  benchmark check.

- **S2 · M · Settings in YAML, user and project.** `~/.ykode/settings.json` (`internal/home/home.go:47`)
  and `<root>/.ykode/settings.json` (`internal/config/trust.go:18`) become `.yaml`; `fileSettings`
  (`internal/config/settings.go:19-52`, ~25 keys) keeps its pointer-means-set semantics with `yaml` tags,
  and an unknown key stays an error naming the file and the key (README:291) — `gopkg.in/yaml.v3` with
  `KnownFields(true)`, the one dependency this needs, because the point is comments: a file that can say
  *why* `sandbox: true` is set. Machine-written records stay JSON (`trusted.json`, sessions, audit).
  Migration: `.yaml` wins, a legacy `.json` is still read for one release with one warning, then removed;
  the trust record is a hash of the file's bytes (`trust.go:26-29`), so every existing acceptance is
  asked about once. `SeedSettings` (`settings.go:82`) and the README's six mentions (205, 230, 235-236,
  238, 313, 331) move too. Tests: YAML fixtures across `settings_test.go` (599 lines) and
  `config_test.go` (354); one per case — legacy read, new wins over old, unknown key named.
  → **done**: both layers are YAML (`settings.yaml`), decoded by `yaml.v3` with `KnownFields(true)`, so an
  unknown key still names the file and the key. A folder holding only the old `settings.json` is read for
  one more release; when both are there, YAML wins. Machine records (sessions, audit, `trusted.json`, eval
  records) stay JSONL/JSON — they are ykode's, not yours. Seeded defaults and the README example are YAML;
  `~/.ykode/settings.json` was converted to `settings.yaml` (`.bak` left beside it).
- **Later:** explore sub-agent, MCP, goreleaser/Homebrew, hash-chained audit log. Image input
  and the rest are in `ideas.md`.

## Measured, do not retry blindly

- Narrowing the verify-nudge words (old C14: drop `add`, `read`, `file`…) made
  `answer-from-file` fail: the model answered "add" from the function's name without reading.
  The nudge stays as it is until F1 decides whether the new prompt makes it unnecessary.
