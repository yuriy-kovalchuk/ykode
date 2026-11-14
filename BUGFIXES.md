# ykode — bug-fix guide

Bug fixes only. No feature is removed and none is added. Evidence for each bug is in `REVIEW.md` §1.
Written against commit `ef8f5e2`; find things by name, line numbers drift.
`GUIDE.md` is a different, larger plan (code reduction). **Do not follow it for this work.**

## Rules

1. One fix, one commit, in the order below.
2. After each fix, from the repo root:
   ```sh
   gofmt -l . ; go build ./... && go vet ./... && go test ./... -count=1 -timeout 180s
   ```
   `gofmt -l` prints nothing. For F1–F5 also
   `go test -race ./internal/agent/ ./internal/tui/ -count=1 -timeout 300s`.
3. Write the failing test first, then the fix.
4. A test that fails because it asserted the old, buggy behaviour is updated to assert the new one.
   Never delete an assertion about behaviour that is still wanted. The tests known to need updating
   are listed under each fix.
5. Stop and report if the check cannot be made green.

Status of this guide: F1–F4 were applied in a scratch copy and built. The tools package passed.
In `internal/agent` the eight tests named below failed and still need the updates described;
that updating was **not** finished or verified. F5–F9 were not tried.

---

## F1 · `read_file` refuses to re-send text that left the context — HIGH

**Bug.** `ReadFile` remembers what it showed, keyed on its raw output (ends in `\n`). The
conversation stores `strings.TrimSpace` of it (`tools.FormatResult`), sometimes with the plan
reminder appended. `Agent.forget` passes the stored text, the hashes never match, the memory is
never released. After a result is shortened, the same read answers "that window is already in front
of you".

**Fix.**
- `internal/tools/read_file.go`: in `alreadyShown` use `resultKey(strings.TrimSpace(text))`; in
  `Forget` use `resultKey(strings.TrimSpace(shown.Text))`.
- `internal/agent/parallel.go`: move the reminder string into a constant `planNag` and return it
  from `planReminder`.
- `internal/agent/history.go`, `forget`:
  `f.Forget(tools.Shown{CallID: m.CallID, Text: strings.TrimSuffix(m.Content, planNag)})`.

**Test** (`internal/agent/shrink_test.go`; replaces `TestAReReadAfterShorteningBringsTheTextBack`,
which skipped the real path):

```go
func TestRereadAfterShrinkThroughTheRealPath(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	dir := t.TempDir()
	name := writeLines(t, dir, "big.txt", 40)
	a, done := newStoringAgent(t, stub, Options{CtxSize: 8000}, &captureSink{}, dir)
	defer done()
	args := json.RawMessage(`{"path":"` + name + `"}`)
	call := llm.ToolCall{ID: "call1", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: args}}
	a.history = append(a.history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}})
	a.runToolCalls(context.Background(), []llm.ToolCall{call})
	a.history = append(a.history, llm.Message{Role: "assistant", Content: "read"}) // the model has seen it
	if a.shrinkOldResults(0) == 0 {
		t.Fatal("not shortened")
	}
	call.ID = "call2"
	a.history = append(a.history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}})
	a.runToolCalls(context.Background(), []llm.ToolCall{call})
	if strings.Contains(a.history[len(a.history)-1].Content, "unchanged since you read") {
		t.Error("the text is gone from the conversation and the tool still refuses to resend it")
	}
}
```

## F2 · A resume trims before anyone asked — HIGH

**Bug.** `Agent.Resume` (`internal/agent/agent.go`) calls `sanitizeHistory()` and `trimHistory()`.
That skips the shorten and fold stages a running session uses, so a conversation that loads over
the pressure mark has results blanked and calls dropped at once. With a session file written before
commit `b67844e` (thinking not saved as stripped) it loads far over the window and loses dozens of
calls.

**Fix.** Delete those two lines from `Resume`. The first `prepareTurn` strips old thinking first,
then folds, then trims; `request` repairs the history.

**Tests to update.** `TestAResumeThatOverflowsTheWindowIsTrimmed` (`repair_test.go`): after
`a.Resume(saved)` call `a.prepareTurn(context.Background(), 1)` before the assertions.
`TestAResumedConversationSaysNothingAboutShortening` (`shrink_test.go`): see F3.
**New test:** after `Resume`, `a.Conversation()[1:]` equals the messages given and the session file
did not grow.

## F3 · The fold summarises markers instead of content — HIGH

**Bug.** `prepareTurn` (`internal/agent/turn.go`) shortens first and folds second, so
`transcriptOf` hands the summariser `[ykode shortened this …]` in place of results. Thinking is not
in the transcript at all.

**Fix.**
- In `prepareTurn`, swap the two blocks: first
  `if a.underPressure() && a.canCompact() && a.foldOverdue() { … a.Compact(ctx) }`, then
  `if a.underPressure() { … a.shrinkWhatAged(keepResults) … }`, then `a.trimHistory()`.
- `internal/agent/compact.go`, `transcriptOf`: for an assistant message with non-empty
  `m.Reasoning`, write `thinking: ` + `cutText(m.Reasoning, compactLineCap)` before its text and
  calls. `Reasoning` is still in memory after `WireReasoning` was stripped.
- `summariseWithModel`: add `Sampling: a.opts.NoThink` to the request.

**Tests to update.** `TestAnOldResultIsSummarisedAndStillReachable` and
`TestAResumedConversationSaysNothingAboutShortening` (`shrink_test.go`) run with a stub that cannot
write a summary, so the fold now fails first and its fallback does the shortening with a different
notice. Either give the stub a valid summary reply for the fold request, or make the history too
short to fold (two call blocks or fewer) so only the shortening runs.
**New test:** a history past the pressure mark with one large result; the fold's request body
contains text from inside that result, and no `[ykode shortened this`.

## F4 · Results the model has not read are shortened — HIGH

**Bug.** `shrinkOldResults` counts its `keep` over all tool messages, so with several parallel
results just returned, some unread ones are shortened; the fold's fallback
(`shrinkForCompaction`) calls it with `keep` 0 and shortens all of them.

**Fix** (`internal/agent/shrink.go`, `compact.go`):
- `shrinkOldResults`: collect positions only from `a.history[:a.firstUnseen()]`.
- `shrinkOldEdits`: skip a site whose `reply >= a.firstUnseen()`.
- `shrinkForCompaction`: `a.shrinkOldResults(compactKeep)` instead of `0`.
- `Compact`, the `start < 2` branch: set `a.compactedAt = callBlocks(a.history)` so it is not
  retried every turn.
- `foldFreed`: when `note.summary == ""` return without writing a `folded` boundary.

**Tests to update.** These build a history that ends in tool messages and call the shrink
functions directly; those messages now count as unread. Append
`llm.Message{Role: "assistant", Content: "ok"}` to the fixture before shrinking:
`TestAnInterruptedWriteKeepsItsArguments` (`carryon_test.go`),
`TestTheNewestWritesAndTheFailedOnesKeepTheirText`,
`TestTwoCallsWithTheSameServerIDPointAtTheirOwnReply` (`shrink_test.go`),
`TestCompactShortensTheRestAndSaysHowMuch`, `TestCompactionRepublishesWhatTheConversationCosts`
(`compact_test.go`). The last two may also need more than two call blocks now that the fallback
keeps two.
**New test:** three call blocks, the last just returned, over the pressure mark; after
`prepareTurn` the last block's results are byte-for-byte unchanged.

## F5 · The token estimate swings between requests — MEDIUM

**Bug.** The estimate is all bytes ÷ the byte ratio of the last request. The ratio moves between
3.13 and 3.69 in the owner's logs and drops right after a shortening, which raises the estimate
just after room was made.

**Fix.** Add `anchorTokens, anchorBytes, anchorLen int` to `Agent`.

```go
func (a *Agent) estimateTokens() int {
	rate := max(a.meter.bytesPerTok, minBytesPerTok)
	bytes := a.sendingBytes()
	if a.anchorTokens > 0 {
		est := a.anchorTokens + int(float64(bytes-a.anchorBytes)/rate) + 4*(len(a.history)-a.anchorLen)
		if est > 0 {
			return est
		}
	}
	return int(float64(bytes)/rate) + 4*len(a.history)
}
```

In `chat` (`turn.go`) after `a.meter.observe(...)`:
`if stats.PromptTokens > 0 && len(req.Tools) > 0 { a.anchorTokens, a.anchorBytes, a.anchorLen = stats.PromptTokens, a.sendingBytes(), len(a.history) }`.
Set `a.anchorTokens = 0` in `startOver`, in `Resume`, in `shrinkWhatAged` when `freed > 0`, after
`Compact` replaces history, and in `trimHistory` when it trimmed or dropped anything.

**Test.** After a stub reply reporting 1,000 prompt tokens and 200 bytes added,
`estimateTokens()` is 1,000 + 200/ratio, within 10.

## F6 · `trimResults` blanks the two results the model is working from — MEDIUM

**Bug.** `trimHistory` calls `trimResults(1, a.firstUnseen(), a.trimGoal())`, with no `keepResults`.

**Fix** (`history.go`). Stop the first pass before the newest `keepResults` read tool messages:
walk back from `firstUnseen()` counting tool messages and pass that index as `to`. The last-resort
pass at the end of `trimHistory` stays as it is.

## F7 · Small TUI defects — LOW

1. `agent.CompactReport` (`commands.go`): return `"compact: nothing was folded\n"` when `freed <= 0`.
2. `applyLive` (`tui/settingsscreen.go`): call `SetThinkingLevel` only when
   `now.ThinkingLevel != before.ThinkingLevel`; today any live setting change undoes `/think`.
3. `cancelRun` (`tui/run.go`): return early when `m.cancel == nil` and set it to nil after calling,
   so a second `esc` prints no second notice.
4. `shrinkWhatAged` (`shrink.go`): set `roomShortened` only when `freed-thinking > 0`, to match
   `shrinkNotice`; today the footer says "shortened old output" for thinking alone.
5. `planReminder` (`parallel.go`): remind once per plan, not every 6 calls. Add
   `planReminded bool` to `Agent`; set it when the reminder is returned; clear it wherever
   `sincePlan` is reset because the model called `plan` or the plan is empty or settled.
6. `replay` (`tui/resume.go`): when a tool message's content starts with `[ykode shortened this`,
   pass `""` as the result to `toolEnd`, so the row shows the call and not the marker. Export the
   marker from `agent` for this.

## F8 · Smaller defects — LOW

1. `config/change.go`, `addToList`: refuse an empty value. The owner's file holds
   `permissions.allow: [""]`.
2. `tools/store.go`, `Save`: when the file already exists, return its path instead of an error.
3. `session/session.go`: every assistant record stores the thinking twice (`reasoning_content` and
   `thinking`). Store it once: when the two are equal write only `thinking` plus `"sent": true`,
   and on load set `WireReasoning = Thinking` when `sent` is true. Old files have both fields and
   load unchanged.

## F9 · Stream timeout while the server reads a cold prompt — check first

`llm/client.go`: the idle watchdog also covers the wait for the first byte. After a fold the server
re-reads the prompt cold and sends nothing meanwhile; a large prompt on a slow server can exceed
`stream_idle_sec` (120). **Check before fixing:** does the owner ever see "server sent nothing for
2m0s" right after a fold or a shortening? If yes: give `newIdleWatchdog` a separate first-byte
allowance of 10 minutes, and have `pokeReader` switch the window to `c.Idle` on the first read.

---

## Not covered

- The order in which whole calls are dropped at the ceiling (`dropOldestToolCall` before prose) is
  a design choice, left as is.
- `tui/markdown.go`, `diff.go`, `wrap.go`, `draft.go`, `list.go` were not reviewed for bugs.
