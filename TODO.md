# TODO

Open work only. When an item is done, delete it from this file in the same change. Undecided
ideas live in `ideas.md`.

Last full scan: 2026-11-08. `go build`, `go vet` and `go test ./...` pass.
Effort: XS < 30 min · S ≈ 1–2 h · M ≈ half a day · L ≈ a day or more.

## Rules for the work

- One ID per change, with a test that fails before the fix.
- Every `go test` gets `-timeout` (mutation checks `30s`, restore files in `try/finally`).
- Live and pty tests get a hard deadline: poll for exit, then `SIGKILL`. A killed run is a
  failure, never a pass.
- `go test -race ./...` for anything that touches parallel tools, the TUI or shared maps.

## Open

Measured against pi in `~/Developer/testing-ykode/BASELINE.md`. Rerun `./bench.py --agents ykode`
(`--repeat 5 --cache asis` on CUDA) after each item.

- **V3 · M · The project's check runs when the model is done.** A `check` command (user settings, or
  a trusted project's) that ykode runs itself when the model answers; a failure goes back as the next
  message. The model stops spending turns on verifying, and the check is always the right one. Needs
  the S1 trust rules: an untrusted project cannot set it.
- **V4 · S · Format after edits.** Run the configured formatter (e.g. `gofmt -w`) on files an edit
  touched, and report it in the result: this model often writes a tab plus a space (seen in both
  agents), and fixing it costs ykode extra edits — 3 of the 4 slow task-03 runs on 2026-11-07 were
  that repair, always inside escaped JSON in `edits[]`. Idea 4 in `ideas.md`.
- **B3 · S · A build is not a test.** Task 10 (`go-feature-two-files`) fails when the model writes
  `" <" + u.Email + ""` instead of `+ ">"`, runs `go build ./...`, sees exit 0 and answers "Build
  passes". Measured 2026-11-07/08 on Qwen3.6: the slip happens under pi too (1 of ~24 runs); prompt
  wording does not move it (a "reread character by character" line made it worse, 3/5, and the model
  "fixed" the line by deleting the string); a top-level `old_text`/`new_text` form that avoids JSON
  escaping went untested, because the model kept using `edits[]`. The fixture has no test of its own
  (the hidden one is copied in after the run), so naming a test command cannot help there. Left to
  try: V3, or offering only raw-text edit parameters for a one-change edit and measuring tasks 10, 03
  and 04 (the rename relies on several edits per call).
- **B4 · S · A small window blames the prompt for the margin.** `--ctx 6000` with the default 4096
  safety margin fails with "conversation is about 2146 tokens but the server context is 6000", which
  reads as though room were spare. What ran out is the reply: 6000 - 2146 - 4096 is negative, and the
  margin ate it. Same reason a stub at `n_ctx = 8192` gives every thinking level from `minimal` to
  `max` the same 926-token thinking budget - on a small window the margin, not the level, decides how
  much thinking fits. *Do:* name what ran out and what would help (`--ctx`, a smaller `safety_margin`,
  `/compact`). Whether the margin should scale with a small window (`min(4096, ctx/4)`) is an open
  question, not a decision; measure it on a small model first. *Test:* the error names the margin and
  the options; a `--ctx` just above prompt+margin still sends a request with `minAnswerTok` left for
  the answer.
- **Later:** explore sub-agent, MCP, goreleaser/Homebrew, hash-chained audit log. Image input
  and the rest are in `ideas.md`.

## Measured, do not retry blindly

- Narrowing the verify-nudge words (old C14: drop `add`, `read`, `file`…) made
  `answer-from-file` fail: the model answered "add" from the function's name without reading.
  The nudge stays as it is.
- The system prompt is pi's shape, not a list of task rules (F1, 2026-11-08). Each rule fixed one
  task and broke another: "say so in your answer" padded task 05's answers, N1's "just that value"
  then dropped the file name `read-the-note` needs, "follow a project file even if the code suggests
  otherwise" made the model trust a stale README (5 of 29 task-05 runs), "then verify" brought
  throwaway programs and "verify once" was the patch for those. The pi-style prompt, without any of
  them: 55/55 against 54/55, 24.7 s against 26.8 s a run, 9.2k against 11.9k prompt tokens
  (`testing-ykode` runs `20261108-021409` and `-023702`). It checks its work less often (task 03 in
  2 of 5 runs, task 09 in 1 of 5) without failing a task; if that starts to cost passes, V3 is the
  answer, not a rule. `TestTheTaskRulesStayOut` names the rules, so one comes back by decision.
- `chat_template_kwargs.reasoning_effort` is not sent: the Qwen template throws on a value it does not
  know, and `thinking_budget_tokens` already sets the depth on any llama.cpp server.
- Removing "do not write throwaway programs" (2026-11-07, task 03 ×5) brought one 89 s run of `go run -`
  and `/tmp` test files back; task 10 did not improve. The rule stays.
- Trimming a path description to "relative to the project root" made the model write the root's own path
  without its leading slash (3 of 5 task-11 runs, 0 in 397 runs before). The description now gives an
  example (`src/main.go`), and the path tools read such a path as the root anyway.
