# ykode review — bugs, context handling, optimizations, reduction plan

> **The plan in §4–§7 of this file is superseded by `GUIDE.md`**, which reflects the owner's later
> decisions (plan tool, `/settings` screen, TUI screens and per-project settings stay). §1–§3 —
> the bugs, how the context code behaves, the optimizations — still stand.

Written 2026-11-14 against commit `ef8f5e2` (clean tree). Line numbers are for that commit.
This file is a work order for an agent. Nothing in the code was changed while writing it.

How to read the confidence tags:

- **[VERIFIED]** — reproduced with a test in a scratch copy of the repo, or read directly out of the
  saved session logs in `~/.ykode/sessions`.
- **[READ]** — follows from reading the code; not executed.
- **[HYPOTHESIS]** — plausible, needs a measurement before acting.

Sizes used throughout: 17,989 lines of non-test Go, 19,314 lines of tests, 163 MB of stale binaries
in `bin/` (untracked).

---

## 0. The five things that matter most

1. **`read_file` refuses to re-send text that was shortened out of the context** (B1). Verified.
   The model is told "that window is already in front of you" when it is not.
2. **The resume bug the owner noticed is real, was caused by the reasoning strip not being saved,
   and was fixed today in `b67844e`** (B2). Old session files still trigger it once. Three smaller
   resume defects remain.
3. **The fold (compaction) summarises stubs, not content** (B3). Shortening runs before folding, so
   the summariser reads `[ykode shortened this …]` markers and never sees results or thinking.
4. **The fallback path of a fold shortens results the model has not read yet** (B4). Verified.
5. **Context management has three overlapping mechanisms where one would do** (§3, §5 phase 2).
   Replacing shorten + trim + fold with fold + one emergency drop deletes B1, B3, B4 and about
   650 lines, and is cheaper in wall-clock on a local server.

Real usage, all 21 saved sessions (2,838 tool calls) — this drives the reduction plan:

| tool | calls | share |
|---|---|---|
| `exec_command` | 1,884 | 66.4% |
| `edit_file` | 389 | 13.7% |
| `read_file` | 357 | 12.6% |
| `write_file` | 109 | 3.8% |
| `plan` | 38 | 1.3% |
| `search_text` | 30 | 1.1% |
| `find_files` | 29 | 1.0% |
| `ask_user` | 2 | 0.07% |

Notes ykode sent the model in the user's place, all sessions: tool call written as text 14, empty
reply 6, stopped halfway 4, wrap-up warning 3, verify nudge 2, out of rounds 2. The loop guard
("you already asked for the same …") fired 0 times. The plan reminder was appended to a tool result
about 50 times in the two newest sessions, against 38 `plan` calls in all sessions.

---

## 1. Bugs

### B1 · `read_file` memory is never released — HIGH — [VERIFIED]

**Where:** `internal/tools/read_file.go:165,187-207`, `internal/agent/execute.go:70`,
`internal/agent/history.go:162-181`, `internal/tools/result.go:35`.

**What happens:** `ReadFile.Run` records the result in `served` keyed on the SHA of the text it
returns, which ends in `"\n"`. `runTool` then stores `tools.FormatResult(out, …)`, and
`FormatResult` does `strings.TrimSpace(out)`. When the result is later shortened, trimmed, dropped or
folded, `Agent.forget` calls `Forget(Shown{Text: m.Content})` with the **trimmed** text. The hashes
never match, so the entry is never deleted. `planReminder` (`parallel.go:22,67`) appending to a
result breaks the match the same way.

**Effect:** after any shortening, reading the same window of an unchanged file answers
`unchanged since you read lines 1-40; that window is already in front of you…` while the text is
gone. The shorten marker itself tells the model "read the range you need again"
(`shrink.go:94`), so ykode instructs the model to do the one thing that will not work.

**Why the existing test misses it:** `TestAReReadAfterShorteningBringsTheTextBack`
(`shrink_test.go:395`) puts the raw `reader.Run` output into history, skipping `runTool`.

**Repro (fails on `ef8f5e2`):**

```go
// internal/agent — uses newStoringAgent, writeLines, stubServer, sse from the existing tests
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
	if a.shrinkOldResults(0) == 0 { t.Fatal("not shortened") }
	call.ID = "call2"
	a.history = append(a.history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}})
	a.runToolCalls(context.Background(), []llm.ToolCall{call})
	if strings.Contains(a.history[len(a.history)-1].Content, "unchanged since you read") {
		t.Error("text is gone from the conversation and the tool still refuses to resend it")
	}
}
```

**Fix, pick one:**

- *Preferred (less is more):* delete the memory. Remove `served`, `alreadyShown`, `Forget`,
  `ForgetAll`, the `Forgetter` and `Shown` types (`tool.go:75-93`), `Agent.forget`/`forgetAll`
  (`history.go:160-193`) and the `worthGuarding` special case (`class.go:33-39`). A repeated read
  costs tokens; a refused read costs the task. In the saved logs the "unchanged since" reply never
  appears, so the saving it buys is unmeasured.
- *Minimal:* key the memory on the call id (`map[callID]key`) instead of on the text, and delete by
  id in `Forget`.

### B2 · Resume: reasoning strip not persisted — FIXED in `b67844e`, residue remains — [VERIFIED]

**The owner's symptom:** "resuming a session seems to trigger shortened older tool output and the
older thinking".

**Root cause (now fixed):** the owner's settings have `send_reasoning: true`, so every assistant
message carries its thinking in `WireReasoning`. `shrinkOldReasoning` cleared it in memory but, until
`b67844e` (2026-11-14 15:30), did not call `a.revised(i)`, so the session log kept the thinking.
Each `--continue` reloaded all of it. Replaying `~/.ykode/sessions/s20261114-124122.306.jsonl`:

| log line | event | messages | bytes | of which reasoning | estimate (tokens) |
|---|---|---|---|---|---|
| 797 | resume | 565 | 780,194 | 423,998 | 244,779 |
| 965 | resume | 633 | 855,058 | 460,638 | 253,189 |
| 1143 | resume | 720 | 912,141 | 443,976 | 286,209 |
| 1369 | resume | 644 | 836,011 | 410,062 | 256,471 |

The window is 250,000 (ceiling 225,000, pressure 187,500). Every resume loaded over the ceiling, so
`Resume → trimHistory` (`agent.go:147`) blanked every remaining tool result (27–43 each time) and
dropped whole tool calls (12, 17, 78 and 22 `removed` lines). That file holds 638 revisions,
129 removals and **no fold at all**: the emergency brake did the work the fold exists for. The first
ask after resume then stripped the thinking again in memory, which is the "older thinking" half of
the symptom. In the whole session the model wrote 822 KB of thinking against 45 KB of answers.

**What is still wrong after the fix:**

- **B2a · Old session files stay inconsistent.** A session written before `b67844e` reloads its
  thinking once more on its next resume. *Fix:* in `Resume`, before anything is measured, apply the
  reasoning policy unconditionally (clear `WireReasoning` on all but the messages the policy keeps).
  That heals old files and makes resume independent of what the log happened to record.
- **B2b · `Resume` calls `trimHistory()` directly** (`agent.go:147`), skipping the shorten and fold
  stages a live run goes through in `prepareTurn` (`turn.go:205-219`). A conversation that ended
  between the pressure mark and the ceiling gets a different rewrite on resume than it would have
  had by staying open, before the user has typed anything, and the notices fire before the screen is
  up. *Fix:* delete the `sanitizeHistory`/`trimHistory` calls from `Resume`; the first `prepareTurn`
  and `request` already do both.
- **B2c · Footer says "shortened old output" for thinking-only housekeeping.** `shrinkWhatAged`
  sets `roomShortened` whenever `freed > 0` (`shrink.go:31-33`), including when only thinking went.
  `shrinkNotice` was taught to stay quiet about that; the footer label was not.
- **B2d · The resumed transcript draws shorten markers as results.** `tui/resume.go:67` passes
  `m.Content` to `toolEnd`, so every shortened call row reads `[ykode shortened this …` and every
  shortened edit shows `{"_ykode": …}` arguments through `tools.Describe`. [READ]

### B3 · The fold summarises stubs — HIGH — [READ]

**Where:** `turn.go:205-218`, `compact.go:86,215-237`.

`prepareTurn` runs `shrinkWhatAged` first and `Compact` second. `Compact` builds the summariser's
input with `transcriptOf(dropped)`, which reads `m.Content` **after** shortening. All but the two
newest results are already `[ykode shortened this exec_command output (it was N bytes)…; it began:
"<120 chars>"]`. On top of that:

- each message is cut to 2,000 bytes (`compactLineCap`), each call argument to 200 (`compactArgCap`);
- thinking is not in the transcript at all (`transcriptOf` reads only `Content`), and this model
  writes about 18 times more thinking than answer text;
- file-write arguments older than the newest two are already `{"_ykode": …}` (`shrinkOldEdits`).

The summary is therefore written from the user's prompts, the model's short answers and call names.
What was learned from results is lost before the summariser is asked. This is the most likely cause
of "bad folding".

**Fix:** fold before shortening, or drop shortening (§5 phase 2). See O1 for how to make the fold
read the real conversation for free.

### B4 · Fold fallback shortens results the model has not read — HIGH — [VERIFIED]

**Where:** `compact.go:72-76,87-93,157-160`.

When `compactCut` returns `start < 2` (everything older than the newest calls is smaller than
`KeepRecentTokens`) or the summary request fails, `Compact` calls `shrinkForCompaction`, which calls
`shrinkOldResults(0)` — keep **zero**. `prepareTurn` runs right before the request that would show
the model its newest tool results, so those are shortened unread.

Probe (ctx 8,000, three call blocks, the last one just returned): after `prepareTurn` all three
results, including the unread one, were shortened; estimate 7,263 → 2,139.

Two more defects on the same path:

- the `start < 2` branch does not set `compactedAt`, so every following turn under pressure calls
  `Compact` again and repeats the notice;
- `foldFreed` writes a `folded` boundary to the session log even when nothing was folded
  (`compact.go:118-126`), copying the whole kept conversation into the file each time.

**Fix:** never shorten at or after `firstUnseen()`; set `compactedAt` on every exit; write the
boundary only when a summary replaced something. All three disappear with §5 phase 2.

### B5 · Token estimate swings with every request — MEDIUM — [VERIFIED in logs]

**Where:** `usage.go:60-73`, `history.go:13-16`.

`bytesPerTok` is replaced by the ratio of the **last** request, with no smoothing. In the newest log
it moves between 3.13 and 3.69. It falls right after a shortening (3.43 → 3.13, 3.53 → 3.15,
3.41 → 3.16) because per-message template overhead stops being diluted by content, and
`+ 4*len(history)` underestimates that overhead. A falling ratio raises the estimate by about 10%
just after room was made, which pushes the conversation back toward the marks it was just moved
away from. The overflow correction (`turn.go:62`) is not written to the log.

**Fix:** O2.

### B6 · Stream idle timeout can kill a legitimate cold prompt — MEDIUM — [HYPOTHESIS]

**Where:** `llm/client.go:140-143`.

The idle watchdog's context also covers waiting for response headers. llama.cpp sends nothing while
it reads the prompt. After any rewrite of old history the cache is gone, and a 100k-token prompt at a
few hundred tokens per second exceeds `stream_idle_sec` (120). The request then fails with "server
sent nothing for 2m0s", is retried once and fails again. Every shorten, trim and fold creates exactly
this situation. *Check:* time a cold 150k prompt on the owner's server. *Fix if confirmed:* do not
arm the watchdog until the first byte arrives, or scale the first-byte allowance with the prompt
size.

### B7 · Smaller defects — [READ]

| id | where | what |
|---|---|---|
| B7a | `session.go:42`, `types.go:19` | Each assistant record stores the thinking twice (`reasoning_content` and `thinking`). Every `Revise` writes the whole message again, thinking included. The newest file is 4.2 MB for a 480 KB conversation. |
| B7b | `history.go:24` | `trimResults(1, firstUnseen(), trimGoal())` has no `keepResults`: when shortening was not enough it also blanks the two results the model is working from, aiming for half the window. |
| B7c | `history.go:28-36` | `dropOldestToolCall` drops calls from the ask in progress before any prose from finished asks goes. |
| B7d | `compact.go:297-306` | The summary request sends no sampling settings and bypasses `a.chat`, so it has no loop guard and no usage accounting. |
| B7e | `~/.ykode/settings.yaml` | The file contains `permissions.allow: [""]`. `entryKey` ignores it, but something wrote an empty entry: check `config/change.go` `addToList`. |
| B7f | `store.go:38` | `Save` uses `O_EXCL`; a second save of the same id fails and the summary silently degrades to "run it again". Reachable when a log lost a revision (pre-`b67844e` files). |

---

## 2. What the context code does today (for orientation)

Marks, for window `C` and reserve `R = min(safety_margin, C/4)` (`budget.go:68-85`):
ceiling `= min(0.9C, C − R − 64)`, pressure `= 5/6 ceiling`, trim goal `= 5/9 ceiling`.
For the owner (C = 250,000, margin 16,000): ceiling 225,000, pressure 187,500, trim goal 125,000.

Per round-trip (`prepareTurn`, `turn.go:196-222`):

1. over pressure → `shrinkWhatAged(2)`: strip all thinking but the newest 2, replace all results but
   the newest 2 with a marker, replace write/edit arguments but the newest 2 with a marker;
2. still over pressure, more than 2 call blocks, 6 new blocks since the last fold → `Compact`;
3. `trimHistory`: over pressure → blank results to the trim goal; over ceiling → drop whole calls,
   then whole exchanges; then blank unread results if still over.

Each of the three rewrites history at a different place, so each invalidates the server's prompt
cache from that place. In the newest log, step 1 took the estimate from 190,771 to 85,428 in one
go: after it the server reads 85k tokens cold, and a fold a few turns later makes it read again.

---

## 3. Optimizations

### O1 · Fold on the cached prefix — biggest win — [HYPOTHESIS, strong]

Today the fold sends a fresh two-message request containing a re-rendered transcript of up to 40% of
the window (`compact.go:236`): 100k tokens for the owner, none of it cached, read cold, to produce
1,500 tokens.

Instead send the conversation as it stands — same system prompt, same tools, same messages up to the
cut — and append one user message with the summary instructions, `tool_choice: "none"`. The server
already holds that prefix, so the request costs the summary's generation and nothing else, and the
summariser reads the real results and calls. This removes `transcriptOf`, `cutText`, `callLine`
and the two caps, and fixes B3 by construction. Keep `checkSummary`.

Requires that nothing rewrote the prefix first, i.e. that shortening is gone (§5 phase 2).

### O2 · Anchor the estimate on the server's own count — [READ]

Keep `lastPromptTokens + lastOutputTokens` from the newest reply and the history length at that
moment. Estimate = that count + bytes added since ÷ ratio. The ratio then only prices the newest
messages, so its error stops mattering, and B5 goes away. On resume, persist that one number (or the
ratio, as now) with the session. `meter` loses the per-request ratio replacement.

### O3 · One reasoning policy, applied at a fixed point — [HYPOTHESIS]

Today thinking is kept until pressure, then all of it is cut at once. Decide instead when a message
is added: thinking is sent back only for assistant messages of the ask in progress, and cleared when
the next user ask starts. The prefix then changes once per ask at a known place rather than at an
arbitrary moment mid-task, and thinking never grows to half the window. Measure against the bench
first: whether the model works worse without older thinking is unknown. If the chat template itself
drops older `reasoning_content`, this costs nothing.

### O4 · Session log — [READ]

- Store thinking once (drop `reasoning_content` from the record, rebuild `WireReasoning` on load
  from the policy in O3).
- With phase 2 done, the log needs only: message lines, `cleared`, `folded` (with kept messages).
  `Revise`, `Remove`, `Rewritten`, `Learned` and `change.apply` go (`session.go:80-89,146-172,
  270-280`), and so does the matching branch in `catalogue.go:99-106`.

### O5 · Smaller — [READ]

- `estimateTokens` → `sendingBytes` → `specs()` rebuilds every tool's description and schema on each
  call, and is called once per message inside `trimResults` and the drop loops. Cache the spec byte
  count; it cannot change mid-session (`specs.go:61`).
- Every `Revise`/`Remove`/`Append` opens, stats and closes the session file (`session.go:175-199`).
  A shortening of 244 messages is 244 open/close cycles. Hold the file open, or batch.
- `withEnvironment` in `Ask` (`turn.go:314`) runs two `git` subprocesses per ask, with a 2 s timeout
  each, before the request is sent.
- `bin/` holds 17 binaries, 163 MB. `make clean`, then keep one.

---

## 4. Reduction plan — principles

- The core is: stream a chat, run four tools (`exec_command`, `read_file`, `edit_file`,
  `write_file`), keep the conversation inside the window, save it, draw it.
- A feature stays if the logs show it used, or a bench result depends on it. Everything else goes.
- Delete tests with the feature. Do not keep a test for removed behaviour.
- One phase per commit series; `go build ./... && go vet ./... && go test ./... -count=1 -timeout 120s`
  green after each step; `-race` for anything touching `parallel.go` or `tui/`.
- After each phase rerun the bench (`~/Developer/testing-ykode/bench.py --agents ykode`) and compare
  with `BASELINE.md`. Confirm first which flags the bench uses (`-p`, `--json`, `--yes`,
  `--no-think`…); those must survive.
- Update `README.md` and `TODO.md` in the same change as each removal.

Expected result: about 18.0k → 9.5–10.5k lines of code, 19.3k → about 10k lines of tests, 3 fewer
direct dependencies (chroma, doublestar, go-gitignore).

---

## 5. Reduction plan — phases

Line counts are `code + tests` for the files named.

### Phase 1 — delete features nobody uses (low risk, no behaviour the model sees changes except 1c, 1d)

**1a · Audit log and `/history` — 1,171 + 1,028**
Remove `internal/audit/` whole, `cli/history.go`, `tui/historyscreen.go`, `Agent.record` and
`History`/`Logged`/`logged` in `agent/audit.go` (keep `describe`, `oneLine`, `detailOf` if still
used), the `history` subcommand in `app/run.go:29`, `auditLogger`, `audit.PruneOldFiles` in
`app/home.go`, settings `audit`, `audit_retention_days`, and `Options.Audit`.
*Why:* the session log already holds every call, its arguments and its result. A second
append-only record with redaction, retention and its own reader is a compliance feature in a
single-user local tool. `Detail.ExitCode/TimedOut/LogPath` exist only to feed it — check what the
TUI still reads (`Detail.Change` is used by the diff view; keep that).

**1b · Overlay screens → plain text — 1,075 + 648**
Remove `tui/helpscreen.go`, `toolsscreen.go`, `report.go`, `modelscreen.go`, `sessionscreen.go`,
`list.go`, `overlay.go`, `confirm.go` and the `picker` field and `pickerMsg` path in `tui/model.go`.
`Agent.Command` already returns text for every one of these commands (`commands.go:124-179`); print
it with `transcript.output`. Drop `/reset` and `/undo force` confirmation boxes.
With the session picker gone, delete `Agent.ContinueFrom` and `startOver`'s second caller
(`agent/sessions.go:50-75`); `--continue` at startup remains the one way back.

**1c · `find_files`, `search_text` and the loop guard — 1,162 + 991**
Remove `tools/find_files.go`, `search_text.go`, `search_ripgrep.go`, `search_walk.go`,
`search_result.go`, `walk.go`, `gitignore.go`, `glob.go`, and `agent/loopguard.go` with its uses in
`execute.go:48-69`, `turn.go:141-144`, `commands.go:175`.
*Why:* 59 calls out of 2,838 (2.1%); the model uses `rg`, `grep`, `find` and `ls` through
`exec_command`. The loop guard only guards these two tools (`worthGuarding`, `class.go:33`) and has
never fired. Two dependencies go, and about 1.5 KB leaves every request.
*Also edit:* the system prompt tool list and the first guideline (`prompt.go:15-27`), `verifyNote`
(`verify.go:8`), the `read_file` error texts that point at `find_files`/`search_text`
(`read_file.go:93,99,103,161,166`), `edit_file`'s description, `tools.Describe`, `tui/call.go:70`.
*Risk:* this changes what the model is offered. Bench before and after; the TODO records that prompt
wording moves pass rates. If a task regresses, restore `search_text` only, on ripgrep only, without
the pure-Go walker.

**1d · `plan` tool, panel and reminders — 424 + 512, plus about 120 in `agent/`**
Remove `tools/plan.go`, `tui/planpanel.go`, `planReminder` and `sincePlan` (`parallel.go:28-53`),
`planBlock`/`restorePlan` and the `<plan>` tags (`files.go:137-172`), `planReminderForPause` and the
plan clause in `stoppedHalfway` (`turn.go:171-192`), `Options.Plan`, `/plan`, `ctrl+o`.
*Why:* 38 calls in 21 sessions, and the reminder was appended to results about 50 times in two sessions —
it nags far more often than the tool is used, and each reminder also breaks B1's hash.
*Risk:* same as 1c; bench it.

**1e · Settings screen, catalogue, in-place YAML writer — 1,122 + 740**
Remove `tui/settingsscreen.go`, `tui/settings.go`, `config/catalogue.go`, `config/change.go`,
`config/normalize.go` (check what else calls it), `Agent.Tune`. `/settings` prints the path of
`settings.yaml`. Keep `/model` and `/think`, which already change the run without the screen.
Drop the legacy `settings.json` reader.

**1f · Setup wizard — 366 + 261**
Remove `tui/setup.go`, `tui/probe.go`, `config/endpoint.go`, `askForServer` and the two call sites
in `app/run.go:42-56`. An unreachable server is an error naming `--base-url` and `settings.yaml`.

**1g · Project settings file and trust — 103 + 126, plus the merge logic in `config/settings.go`**
Remove `<root>/.ykode/settings.yaml`, `config/trust.go`, `cli/trust.go`, `trusted.json`,
`cli.ReportHeld` and the double `config.Load` in `startupConfig`. One settings file, flags over it.

**1h · Small removals**

| what | files | lines | why |
|---|---|---|---|
| syntax highlighting | `tui/highlight.go`, `codeblock_test.go`, chroma dependency | 284 + 266 | the largest dependency; fences still render as code blocks |
| sandbox | `tools/sandbox.go`, `Exec.Sandbox`, setting | 53 + 80 | off in the owner's settings, and `(allow default)` on macOS is not isolation (TODO `SEC`) |
| `/undo` | `tools/undo.go`, `pushUndo`, `Undoer`, `HasUndo` | 74 + 173 | git does this |
| `read_file` outline | `tools/outline.go`, `outline` parameter | 90 + 0 | no test file of its own; removing it shrinks the tool spec |
| client-side speed estimate | `agent/pace.go`, `Stats.Estimated` | 100 + 95 | only for servers that send no timings; show nothing instead |
| branch watcher, tab title | `tui/branchwatch.go`, `tui/title.go` | 137 + 189 | polls HEAD every 5 s for a footer word |
| line REPL | `cli/repl.go`, `--no-tui` interactive mode | 135 + 56 | keep `cli.View`/`cli.JSONWriter` for `-p` and `--json`, which the bench needs |
| `ykode sessions` subcommand | `cli/sessions.go` | 43 + 69 | `/sessions` prints the same list |
| result store | `tools/store.go`, `Options.Store`, `ResultDir`, orphan pruning in `app/home.go:84-101` | 62 + 87 | goes with phase 2; cut `exec_command` output already has its own log file |
| mouse mode | `mouseOn`, `wantMouse`, `ctrl+t`, `mouse` setting | — | check size first; optional |

Phase 1 total: about 6,400 lines of code and 5,300 lines of tests.

### Phase 2 — one context mechanism instead of three (medium risk, highest value)

Target design:

1. **Estimate** from the server's last count (O2).
2. **Thinking** kept for the ask in progress only (O3), or — if the bench says older thinking
   matters — kept until the fold and summarised with everything else.
3. **Fold** when the estimate passes one mark. Suggested: `C − R − keepRecent`, where
   `keepRecent = min(20000, C/5)` as now. The fold runs on the cached prefix (O1), keeps the newest
   `keepRecent` tokens verbatim, never cuts between a call and its results, and keeps the current
   ask's user message word for word. This is `compactCut` and `Compact` minus the transcript code.
4. **Emergency** only when the server reports overflow or the fold fails: drop the oldest whole
   turns until the request fits, and say so. One function.
5. **Resume** loads and does nothing else; the first request applies 2–4 like any other.

Deleted by this: `agent/shrink.go` whole (226), `trimResults`, `trimmed`, `firstUnseen`, `forget`,
`forgetAll` in `history.go` (about 90), `shrinkForCompaction`, `transcriptOf`, `cutText`, `callLine`
in `compact.go` (about 90), `tools/store.go` (62), `tools.Forgetter`/`Shown`, the `read_file`
memory (about 40), `agent/contextwork.go` down to one label, `session.Log.Revise/Remove/Rewritten/
Learned` and `change` (about 70), the `Learned`/`savedRatio` plumbing in `agent.go`, `turn.go:286-289`
and `app/run.go:86`. About 650 lines of code and 1,200 of tests (`shrink_test.go` 562,
`resume_prefix_test.go` 196, parts of `history_test.go` and `compact_test.go`).

Bugs removed by construction: B1, B2b, B2c, B2d, B3, B4, B5, B7a–c, B7f.

Tests to write first (each fails today or pins the new contract):

- the B1 repro above (if the memory is kept until this phase);
- a fold's request body starts with the same bytes as the request before it (cache prefix kept);
- a fold never touches a result newer than the last assistant message;
- resume of a log written by the old format (with `revised`/`removed` lines) still loads — keep the
  reader for old lines one release, or state in the README that old sessions are not resumable;
- resume issues no log writes and no notices before the first ask.

*Risk:* a fold costs one generation (1,500 tokens) where shortening cost none. On a local server
that is cheaper than the cold re-read shortening causes, but confirm with the bench's wall-clock on a
long task. Small windows (8k–16k) fold often; check `--ctx 8192` still completes a multi-file task.

### Phase 3 — nudges: measure, then cut (needs the bench)

All of these put words in the user's mouth. Counts are across all saved sessions.

| nudge | where | fired | recommendation |
|---|---|---|---|
| tool call written as text → run it | `textcalls.go`, `turn.go:68-98` | 14 | **keep**; this model does it. The "ask once to call it properly" branch (`turn.go:93-98`) can go if every parseable call is run |
| empty reply → continue | `turn.go:114-121` | 6 | keep |
| cut at `max_tokens` → continue | `turn.go:102-113` | — | keep |
| stopped halfway (regex on the last line) | `turn.go:122-128,155-192` | 4 | measure; English-only heuristics |
| wrap-up warning at 80% of rounds | `rounds.go` | 3 | measure; candidate to cut with `max_turns` raised |
| verify nudge (regex on the prompt) | `verify.go`, `turn.go:130-135` | 2 | TODO says narrowing it failed one task; measure removal as a whole, not narrowing |
| repeated-line detector | `stuckreply.go` | 0 notes | keep; 45 lines, and nothing else stops a looping local model |
| `cd <root> &&` stripping | `execute.go:128-173` | — | cut when `assume_yes` is the normal mode; it exists for allow-list matching |

### Phase 4 — optional, owner's call

| what | lines | note |
|---|---|---|
| `ask_user` | 267 + 579 | 2 calls ever. The model can ask in its answer. Removing it also removes `Asker`, `askMsg`, and the registry mutation in `agent.New` |
| live tool-call drafts | 276 + 112 (`tui/draft.go`) | shows a call while it is being written; pleasant on long writes, not needed |
| steering mid-run | 53 + 128 + TUI waiting row | check whether the owner uses it |
| permissions allow-list and confirm bar | `permissions.go` 130 + 175 | the owner runs `assume_yes: true`; could reduce to "ask or don't" |
| environment note per ask | `environment.go` 69 + 202 | could keep the date only and drop both git calls |
| seven thinking levels | `llm/thinking.go` | three would do; tiny |
| `ideas.md`, README | 272, 538 | README describes many of the features above at length; cut to match |

---

## 6. Order of work

1. B1 (one-line-class fix or delete the memory) and B2a/B2b — small, independent, user-visible.
2. Phase 1a, 1b, 1e, 1f, 1g, 1h — none changes what the model sees. Bench once at the end.
3. Phase 1c and 1d — each changes the tool list. Bench after each, separately.
4. Phase 2, tests first.
5. Phase 3 with bench runs; Phase 4 after asking the owner.

## 7. Questions for the owner before starting

1. Which flags does `bench.py` pass to ykode? (`-p`, `--json`, `--yes`, others.) Those are fixed
   points for phase 1h.
2. Is the audit log used for anything outside ykode? If yes, 1a becomes "keep `audit.Append`, drop
   the readers and screens".
3. Should old session files stay resumable after phase 2?
4. Is `send_reasoning: true` a measured choice for this model, or a default that stuck? It decides
   O3.
5. Phase 4 items: keep or cut, one by one.

## 8. What was not reviewed in depth

`internal/tui` rendering (`markdown.go`, `diff.go`, `wrap.go`, `rows.go`, `bottom.go`, `status.go`,
`keys.go`), `internal/config/settings.go` merge logic, and `scripts/install.sh` were skimmed for
structure only. No bug claims are made about them here.
