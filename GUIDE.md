# ykode — work guide

This is the work order. `REVIEW.md` holds the evidence behind it; where the two differ, this file
wins. Written against commit `ef8f5e2`. Line numbers are for that commit and drift as you edit, so
find things by name, not by number.

## Decisions already made by the owner

- The **plan tool stays**, with its panel.
- The **`/settings` screen stays**, with the server/model screen it opens.
- The **TUI is the product**. Its screens, colours, diffs, markdown, syntax colouring, live call
  drafts, footer, branch watcher and tab title all stay. A step that makes the screen worse is wrong.
- The benchmark (`bench.py`) is retired. Nothing here depends on it. `--json` goes; `-p` stays.
- Old saved sessions must still load.
- **Per-project settings stay and grow**: a `.ykode/settings.yaml` in the project overrides the
  global file, `/create-local-setting` makes one, and `/settings` writes to it when it exists
  (step B2).

## What goes, and what it saves

| step | what | code | tests |
|---|---|---|---|
| A1 | `read_file`'s memory of what it showed (bug B1) | ~60 | ~120 |
| B1 | audit log, `/history`, `ykode history` | ~1,170 | ~1,030 |
| B2 | *adds* local settings per project (a feature, not a removal) | +~120 | +~150 |
| B3 | line prompt (`--no-tui`), `--json`, `ykode sessions` | ~310 | ~150 |
| B4 | sandbox, `/undo`, `read_file` outline, legacy `settings.json` | ~250 | ~250 |
| B5 | `find_files`, `search_text`, the loop guard | ~1,160 | ~990 |
| C | shorten + trim replaced by fold + one emergency drop | ~650 | ~1,200 |
| D | verify nudge, wrap-up warning | ~80 | ~250 |

About 3,500 of 17,989 lines of code (20%) and 3,800 of 19,314 lines of tests, net of what B2 adds. Two dependencies go
(`doublestar`, `go-gitignore`).

---

## Rules for every step

1. **One step, one commit.** Do the steps in the order written. Do not start a step while the
   previous one is red.
2. **The check**, run from the repo root after every step:
   ```sh
   gofmt -l . ; go build ./... && go vet ./... && go test ./... -count=1 -timeout 180s
   ```
   `gofmt -l` must print nothing. For steps A2, B5 and C also run
   `go test -race ./internal/agent/ ./internal/tui/ -count=1 -timeout 300s`.
3. **How to remove a feature.** Delete the files the step names. Run `go build ./...`. For every
   error, delete the use — the field, the argument, the `case`, the call. Never bring the feature
   back to make an error go away. Repeat until it builds. Then run `go vet ./...`, which compiles
   the tests, and do the same there.
4. **Tests.** A test of a removed feature is deleted whole. A test of something that stays is fixed
   so it still tests it. Never delete an assertion about kept behaviour to get to green. If you
   cannot tell which kind a test is, read its name and its first comment: they say what it protects.
5. **Settings files must keep loading.** `settings.yaml` is read with `KnownFields(true)`
   (`internal/config/settings.go`), so a key the struct no longer has is a startup error. When a
   step retires a setting: keep its field in `fileSettings`, move it under a comment
   `// retired: still read so an old file loads, no longer used`, and remove it everywhere else
   (`Config`, `defaults()`, `builtinSettings()`, `applySettings`, `knobs()` in `catalogue.go`).
6. **The TUI's four lists must agree** after any step that removes a command, key or setting:
   - `agent.Commands()` in `internal/agent/commands.go` (feeds `/help` and tab completion),
   - `keys` in `internal/tui/bottom.go` (what `?` shows),
   - `knobs()` in `internal/config/catalogue.go` and `liveKnobs` in `internal/tui/settingsscreen.go`,
   - the matching rows and paragraphs of `README.md`.
7. **Docs.** Each step ends by removing what it removed from `README.md` and, if an item there is
   now done or moot, from `TODO.md`.
8. **Commit message** in the repo's style: a lower-case type, a colon, one sentence saying what
   changed for the user. Example: `refactor: the audit log and /history are gone`.
9. **Stop and report** instead of guessing when: a kept TUI test fails and you cannot see why; a
   step needs a file this guide does not mention to change by more than a few lines; the check was
   green before your step and you cannot get it green after.
10. Do not touch `~/.ykode`. Do not delete `bin/`; the owner will.

Step 0: run the check before changing anything and note that it is green.

---

## A · Bug fixes first

### A1 · `read_file` forgets nothing it showed (bug B1)

**Why:** after an old result is shortened, the same read answers "that window is already in front
of you" although the text is gone. The memory is keyed on the tool's raw output, but the
conversation stores `strings.TrimSpace` of it, so the two never match and the entry is never
released. Deleting the memory is the fix.

**Do, in `internal/tools/read_file.go`:**
- remove the fields `mu` and `served` from `ReadFile`, and `served: map[string]bool{}` from
  `NewReadFile`;
- remove `alreadyShown`, `Forget`, `ForgetAll`;
- in `Run`, the outline branch returns `outline` directly, and the end of `Run` returns
  `b.String()` directly (delete both `if t.alreadyShown(...)` blocks);
- drop the `sync` import.

**Do, elsewhere:**
- `internal/tools/tool.go`: delete `Shown`, `Forgetter`, `resultKey`, and the `crypto/sha256` and
  `encoding/hex` imports if nothing else uses them.
- `internal/agent/history.go`: in `forget`, delete everything that looks up the tool and calls
  `Forget`; keep the `a.repeats.forgetID` part for now (B5 removes it). Delete `forgetAll`.
- `internal/agent/agent.go`: delete the `a.forgetAll()` call in `startOver`.
- `internal/agent/class.go`: `worthGuarding` becomes
  `return t.Class() == tools.ClassReads && t.Name() != "read_file"`.

**Tests:** delete the tests of the memory (`unchanged since you read` in
`tools/read_file_test.go`, `TestAReReadAfterShorteningBringsTheTextBack` in
`agent/shrink_test.go`). Add this one to `internal/tools/read_file_test.go`, adapting the helper
names to the ones that file already uses:

```go
// a second read of the same window returns the same text: the tool keeps no memory of the first
func TestReadingTheSameWindowTwiceReturnsTheTextTwice(t *testing.T) {
	// build a ReadFile over a temp dir holding a 40-line file, as the other tests here do
	first, err := tool.Run(context.Background(), args)
	if err != nil { t.Fatal(err) }
	second, err := tool.Run(context.Background(), args)
	if err != nil { t.Fatal(err) }
	if first != second { t.Errorf("the second read differs from the first:\n%s", second) }
}
```

**Done when:** `grep -rn "alreadyShown\|Forgetter\|unchanged since" internal` prints nothing.

### A2 · A resume only loads

**Why:** `Agent.Resume` (`internal/agent/agent.go`) calls `sanitizeHistory()` and `trimHistory()`
itself. That trims a conversation before the user has typed anything, by different rules than a
running session uses, and its notices are lost because the screen is not up yet. The first request
already repairs and makes room.

**Do:** delete the two lines `a.sanitizeHistory()` and `a.trimHistory()` from `Resume`. Nothing else.

**Tests:** in `internal/agent/resume_prefix_test.go` and `sessions_test.go`, a test that expects
trimming to have happened *inside* `Resume` must now call `a.prepareTurn(ctx, 1)` (or run one
`Ask` against the stub server) before it looks. Add:

```go
// coming back to a conversation changes nothing in it and writes nothing to its log
func TestResumeLeavesTheConversationAsItWasSaved(t *testing.T) {
	// build an agent with a session log in t.TempDir(), a small CtxSize, and messages
	// large enough to be over the pressure mark; note the log file's size; call Resume
	// assert: a.Conversation()[1:] equals the messages given, and the log file did not grow
}
```

### A3 · Four small TUI defects

1. **`/compact` that folded nothing says a negative number.** `agent.CompactReport`
   (`commands.go`) prints "about N tokens smaller" for any N. Make it return
   `"compact: nothing was folded\n"` when `freed <= 0`.
2. **A setting change resets the thinking level.** `applyLive` in
   `internal/tui/settingsscreen.go` always calls `m.agent.SetThinkingLevel(now.ThinkingLevel)`, so
   a level chosen with `/think` is lost when any other live setting is changed. Call it only when
   `now.ThinkingLevel != before.ThinkingLevel`.
3. **`esc` pressed twice prints the cancelling notice twice.** `cancelRun` in
   `internal/tui/run.go`: return early when `m.cancel == nil`, and set `m.cancel = nil` after
   calling it.
4. **The plan reminder nags.** `planReminder` in `internal/agent/parallel.go` fires every 6 calls
   for as long as a plan has open steps: in the owner's logs it was added to a tool result about
   50 times against 38 `plan` calls. Remind **once** per plan: add a field `planReminded bool` to
   `Agent`; set it when the reminder is returned; clear it where `sincePlan` is reset to 0 because
   the model called `plan` or the plan is settled or empty; return `""` while it is set.

Each gets a test beside the existing ones for that function.

---

## B · Removals

### B1 · Audit log and `/history`

The session file already records every call with its arguments and result. This removes the second
record and its viewer. **It removes one TUI screen (`/history`).** If the owner has said to keep
that screen, skip B1 entirely and go to B2.

**Delete:** `internal/audit/` (whole directory), `internal/cli/history.go`,
`internal/cli/history_test.go`, `internal/tui/historyscreen.go`,
`internal/tui/historyscreen_test.go`.

**Then edit:**
- `internal/agent/audit.go`: delete `record`, `targetPaths`, `History`, `Logged`, `logged` and the
  `audit` and `crypto/sha256` imports. Keep `describe`, `oneLine`, `detailOf`. Rename the file to
  `describe.go` and its test file to match, keeping only the tests of what is left.
- `internal/agent/execute.go`: delete every `a.record(...)` call. Then delete what only they used:
  `argsText`, `start`, `status` and the `switch` that sets it, and the `time` import. Keep
  `detailOf(tool).Change` on the `ToolEnd` event — the diff view reads it.
- `internal/agent/agent.go`: delete the field `auditBroken`.
- `internal/agent/options.go`: delete `Options.Audit`.
- `internal/agent/sessions.go`: delete the line `a.opts.Audit.ContinueAs(id)`.
- `internal/agent/commands.go`: delete the `/history` entry in `Commands()`, the `case "/history"`,
  and `historyLimit`. `internal/agent/history.go`: delete `splitAll`.
- `internal/tui/run.go`: delete the `if name == "/history"` block and the `audit` import.
- `internal/app/run.go`: delete the `history` subcommand at the top of `Run`, `auditLogger`, and
  `agentOpts.Audit`. `internal/app/home.go`: delete the `audit.PruneOldFiles` lines.
- `internal/config`: retire `audit` and `audit_retention_days` by rule 5. Delete `AuditDir`,
  `LogAudit`, `KeepAudit`, `defaultAuditKeep`, the `ykode history` usage line in `Load`.
- `internal/tools/tool.go`: in `Detail`, delete `ExitCode`, `TimedOut`, `LogPath` and `Diff` **only
  if** the build shows nothing reads them after the above; `Change` stays.
- `README.md`: the "Sessions and the audit log" section keeps its session half; drop the audit
  half, the `ykode history` examples, and `/history` wherever it is listed.

**Done when:** `grep -rni "audit" internal --include='*.go'` shows only the two retired yaml fields.

### B2 · Local settings per project (a feature)

**What the owner asked for.** The global `~/.ykode/settings.yaml` is the default. A project can
have its own `<root>/.ykode/settings.yaml` that overrides it — a different server address or model
for that project — without touching the global file. `/create-local-setting` creates it. When it
exists, `/settings` reads and writes **it**; when it does not, `/settings` writes the global file
and **always says so**.

**What already exists — do not rebuild it.**
- The project file is already read and layered over the global one: `readSettings` and `applyFile`
  in `internal/config/settings.go`, `projectSettingsPath` in `internal/config/trust.go`.
- A project file ykode did not write is already distrusted: keys such as `base_url`, `model`,
  `api_key`, `assume_yes` are held back (`stripUnsafe`) until the user accepts the file at startup
  (`cli.ReportHeld`), and the acceptance is stored as a hash in `~/.ykode/trusted.json`
  (`TrustProject`). **Keep all of it.** It is what stops a cloned repository from pointing ykode at
  someone else's server. The new rule is only: a file ykode itself writes is trusted as written.

**What is missing today.** `config.Set` and `config.SaveEndpoint` always write the global file;
nothing creates the project file; nothing tells the user which file a change lands in.

**Do, in `internal/config`:**

1. `trust.go`: export the path and add a presence check.
   ```go
   // LocalSettings is where a project keeps its own settings: <root>/.ykode/settings.yaml.
   func LocalSettings(root string) string { return projectSettingsPath(root) }

   // HasLocal says the project has a settings file of its own.
   func HasLocal(root string) bool {
   	_, err := os.Stat(LocalSettings(root))
   	return err == nil
   }

   // SettingsTarget is the file a change is written to: the project's when it has one, else the
   // user's own. The bool says it is the project's.
   func SettingsTarget(root string) (path string, local bool) {
   	if HasLocal(root) {
   		return LocalSettings(root), true
   	}
   	return home.Settings(), false
   }
   ```
2. `endpoint.go`: `readSettingsDoc` and `writeSettingsDoc` take the path as a parameter instead of
   calling `home.Settings()`. A missing **global** file starts from `builtinSettings()` as now; a
   missing **local** file is an error (it is only written once it exists).
3. After every write to a local file, trust what was written, so the next start does not ask about
   a file ykode itself just changed:
   ```go
   // trustWritten records the project file as it now stands as accepted.
   func trustWritten(root string) error {
   	raw, err := os.ReadFile(LocalSettings(root))
   	if err != nil {
   		return err
   	}
   	sum := sha256.Sum256(raw)
   	return TrustProject(root, hex.EncodeToString(sum[:]))
   }
   ```
4. `change.go`: `Set(root, key, value string) (path string, local bool, err error)` — resolve the
   file with `SettingsTarget(root)`, read, change, write as now, then `trustWritten(root)` when
   `local`. `endpoint.go`: `SaveEndpoint(root, baseURL, apiKey, model string) (string, bool, error)`
   the same way. The call in `internal/app/run.go` (`askForServer`, the first-run screen) passes
   `cfg.Root` like the others, so a project with a local file gets its server saved there.
5. New file `local.go`:
   ```go
   // CreateLocal makes <root>/.ykode/settings.yaml holding the server and model this run uses, as a
   // starting point to edit, and records it as trusted. It refuses to write over one that exists.
   func CreateLocal(root, baseURL, model string) (string, error) {
   	path := LocalSettings(root)
   	if _, err := os.Stat(path); err == nil {
   		return path, fmt.Errorf("%s is already there: /settings changes it", path)
   	}
   	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
   		return path, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
   	}
   	body := "# ykode settings for this project. A key here overrides ~/.ykode/settings.yaml;\n" +
   		"# a key left out keeps the global value. /settings edits this file.\n" +
   		"base_url: " + baseURL + "\n"
   	if model != "" {
   		body += "model: " + model + "\n"
   	}
   	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
   		return path, fmt.Errorf("write %s: %w", path, err)
   	}
   	return path, trustWritten(root)
   }
   ```
   Write the values through `yaml.Marshal` of a small struct rather than by string concatenation if
   a URL or model id could need quoting; the comment header stays as text above it.
6. `catalogue.go`: add a source `FromProject = "project file"` beside `FromFile`. `fileKeys`
   returns two sets (global, project) and `where` answers `FromProject` when the project file sets
   the key. The settings screen then shows, per row, which file decided it. `ModelFromFile` is true
   for either file.

**Do, in the TUI:**

7. `internal/agent/commands.go`, `Commands()`: add after `/settings`
   `{Name: "/create-local-setting", Note: "make .ykode/settings.yaml for this project; /settings then changes it instead of the global file", Waits: true}`
   and change the `/settings` note to say it writes the project's file when there is one.
8. `internal/tui/run.go`, in `submit`, before the `/settings` block:
   ```go
   if name == "/create-local-setting" {
   	m.createLocal()
   	m.refresh()
   	return m, nil
   }
   ```
   and in `settings.go`:
   ```go
   // createLocal gives the project a settings file of its own, starting from the server and model
   // this run is using, and reads the settings again so the screen shows where each value comes from.
   func (m *model) createLocal() {
   	if m.running {
   		m.log.notice("that waits for the turn to finish: press esc to stop this one first")
   		return
   	}
   	path, err := config.CreateLocal(m.cfg.Root, m.cfg.BaseURL, m.agent.Model())
   	if err != nil {
   		m.err = err.Error()
   		return
   	}
   	if fresh, err := m.cfg.Reload(); err == nil {
   		m.cfg = fresh
   	}
   	m.log.notice("created " + shortenHome(path) + ": /settings now changes this project only. " +
   		"Add .ykode/ to the project's .gitignore if it holds a key.")
   }
   ```
9. **The warning.** Every place a setting is written must say which file takes it, and warn when it
   is the global one. There are three:
   - the settings screen header (`settingsScreen.view` in `settingsscreen.go`). Replace the dim
     text after the title with one of
     `this project · .ykode/settings.yaml` (dim), or
     `⚠ global · ~/.ykode/settings.yaml — every project is changed · /create-local-setting makes one for this project`
     rendered with `styleWarn`. The screen learns which from `config.SettingsTarget(cfg.Root)`;
     add one row to the `room` arithmetic if the warning wraps on a narrow terminal.
   - `writeSetting` in `settingsscreen.go`: `s.note` after a save is
     `KEY saved to .ykode/settings.yaml (this project)` or
     `KEY saved globally to ~/.ykode/settings.yaml`.
   - `settingsChange` in `settings.go` (the typed form, `/settings key value`) and `applyEndpoint`
     (the server screen): the same two wordings, and for the global case a line before it:
     `no .ykode/settings.yaml in this project: this changes the global settings`.
10. `Reload` (`config.go`) already re-reads both files; nothing to change. After a local write the
    live settings are applied exactly as after a global one.

**Tests** (`internal/config/local_test.go`, with `home.Reset()` and `YKODE_HOME` set to a temp dir
as the other config tests do):
- with no local file, `Set` writes the global file and reports `local == false`; the project
  folder is not created;
- `CreateLocal` writes the file, refuses a second time, and `Load` afterwards holds back nothing
  (`HeldKeys` empty) — it is trusted;
- with a local file, `Set("base_url", …)` changes the local file, leaves the global file's bytes
  unchanged, and `Load` returns the new value with nothing held back;
- a local file edited by hand afterwards is held back again (the existing trust test, kept);
- a value in the local file wins over the global one, and a key only in the global file still
  applies;
- `Catalogue()` reports `FromProject` for a key the local file sets.

In `internal/tui`: the settings screen's view contains the warning text when there is no local
file and does not when there is; `/create-local-setting` leaves a notice and a file.

**README:** in the Settings section, replace the trust paragraph's first sentence with a short
description of local settings: what the file is, `/create-local-setting`, that `/settings` writes
to it when present and warns when it writes globally, and that a project file you did not create
through ykode is still asked about once. Add `/create-local-setting` to the command table.

### B3 · One front end, plus `-p`

**Delete:** `internal/cli/repl.go`, `repl_test.go`, `json.go`, `json_test.go`, `confirm.go`,
`sessions.go`, `sessions_test.go`.

**Then edit:**
- `internal/cli/view.go`: delete the `confirm` field and parameter (`NewView(out, log)`), and the
  `Ask` method — without it `agent.New` does not offer `ask_user` to a `-p` run, which is right:
  nobody is there to answer. The `Confirm` case becomes
  `fmt.Fprintf(v.log, "  !! %s needs a yes: run with --yes\n", ev.Tool); return agent.Refuse`.
- `internal/app/frontend.go`: `FrontEnd` keeps `View` and `hub`. `NewFrontEnd` builds the hub when
  `cfg.TUI`, else `cli.NewView(os.Stdout, os.Stderr)`. `Run` has two cases: `cfg.Prompt != ""` →
  `answerOnce`; otherwise `tui.Run`. `answerOnce` loses its `events` parameter. Delete
  `readsStdinLineByLine`.
- `internal/app/run.go`: delete the `sessions` subcommand. After `cfg.TUI` is computed add:
  ```go
  if !cfg.TUI && cfg.Prompt == "" {
  	return errors.New("ykode needs a terminal; use -p TASK to answer one task without one")
  }
  ```
  In `connect`, the banner goes to `os.Stdout` always when `!cfg.TUI` (delete the `cfg.JSON` case).
- `internal/config`: delete the `json` and `no-tui` flags, `flagValues.JSON`, `flagValues.NoTUI`,
  `Config.JSON`. In `normalize`: `if c.Prompt != "" { c.TUI = false }`. Fix the usage line in `Load`.
- `internal/agent/sessions.go`: the last row of `Sessions()` stays as it is.
- `README.md`: remove `--no-tui` and `--json` from the options table and every sentence about the
  line prompt or JSON lines. `/sessions` on the screen is the way to list conversations.

### B4 · Sandbox, `/undo`, outline, legacy JSON settings

**Sandbox.** Delete `internal/tools/sandbox.go` and `sandbox_test.go`. In `exec.go` delete the
`Sandbox` field and the `if t.Sandbox` block. Delete `tools.Options.Sandbox`, `Config.Sandbox`, the
line in `app/options.go`. Retire the `sandbox` setting by rule 5. In `README.md` delete the
`sandbox: true` paragraph; the paragraph before it, which says `exec_command` is not confined,
stays.

**`/undo`.** Delete `internal/tools/undo.go` and `undo_test.go`. In `paths.go` delete the `undo`
field of `scope` and `pushUndo`; delete the two `t.pushUndo(...)` calls (`edit_file.go`,
`write_file.go`) and, in `write_file.go`, whatever variable only that call read. Delete
`tools.Options.Undo` and the two assignments in `BuiltIn`; `agent.Undoer`, `Options.Undo`,
`HasUndo`; the `/undo` entry and `case` in `commands.go`; the `undo` parameter of `app.Options` and
its argument in `app/run.go`; the `/undo force` block in `tui/run.go`. `tui/confirm.go` **stays**:
`/reset` still asks. `README.md`: delete the `/undo` paragraph and its mentions.

**Outline.** Delete `internal/tools/outline.go`. In `read_file.go` delete `readInput.Outline`, the
`"outline"` schema property, the `if in.Outline` block, and the last sentence of `Description()`.
In `describe.go` delete the `if in.Outline` lines. Delete the outline tests. `README.md`: delete
the sentence about `outline: true`.

**Legacy `settings.json`.** In `internal/config/settings.go`, `settingsIn(dir)` returns
`filepath.Join(dir, "settings.yaml")` and nothing else (it serves both the global and the project
folder). Delete `blockStyle` in `endpoint.go` and both calls to it (`endpoint.go`, `change.go`). Delete the tests of the JSON fallback. `README.md`: delete "a folder that still holds the
old `settings.json`…".

### B5 · `find_files`, `search_text` and the loop guard

**Why:** 59 of 2,838 calls in the owner's sessions (2%); the model searches with `rg`, `grep`,
`find` and `ls` through `exec_command`. The loop guard exists only for these two tools and never
fired. This step changes what the model is offered, so the wording below matters: use it exactly.

**Delete:** in `internal/tools/`: `find_files.go`, `search_text.go`, `search_ripgrep.go`,
`search_walk.go`, `search_result.go`, `walk.go`, `gitignore.go`, `glob.go` and each one's
`_test.go`; `internal/agent/loopguard.go`, `loopguard_test.go`.

**Three things those files defined that kept code needs — move them first:**
- `oneLine` (in `search_result.go`) → move the function unchanged into `internal/tools/result.go`.
- `maxScannedFileBytes` (in `walk.go`) → `const maxScannedFileBytes = 1 << 20` in `read_file.go`,
  with the comment "read_file refuses a file larger than this".
- `searchInput` and `findInput` are used by `describe.go`; delete those two `case`s there instead.

**Then edit:**
- `internal/tools/tool.go`: `BuiltIn` builds `read, write, edit, run, plan`; delete `find`,
  `search`, `Options.SearchLimit`.
- `internal/tools/read_file.go`, new wording for four messages:
  - no such file → `read_file %q: no such file; list the directory with exec_command (ls)`
  - a directory → `read_file %q is a directory; list it with exec_command (ls)`
  - too big → `read_file %q is %.1f MB; use exec_command with grep -n to find a spot, or sed -n '1,200p'`
  - long file hint → `[a long file: to find a line, grep -n PATTERN %q with exec_command is faster than reading on]`
- `internal/tools/edit_file.go` `Description()`: "…Each old_text is copied from read_file output
  and occurs once in the file. All edits apply or none."
- `internal/agent/prompt.go`, `SystemPrompt`: delete the `find_files` and `search_text` lines of
  the tool list, and replace the first guideline with:
  ```
  - Find and search with exec_command (rg, grep, find, ls); read one file with read_file. When the
    work takes several commands, plan them first and run them together in one exec_command, each
    under an "=== name ===" line.
  ```
- `internal/agent/verify.go` `verifyNote`: "…verify it now with read_file or exec_command, then
  answer again." (D removes this note altogether; keep the build green until then.)
- `internal/agent`: delete the field `repeats` and `newLoopGuard()`; every `a.repeats.…` line
  (`agent.go`, `turn.go` including the whole `if a.repeats.isStuck()` block, `commands.go`,
  `execute.go` including both `if worthGuarding(tool)` blocks); `worthGuarding` in `class.go`;
  `forget` in `history.go` and its callers (it is empty now). In `shrink.go` delete the two map
  entries for the removed tools.
- `internal/tui/call.go`: delete the `search_text`/`find_files` case, `foundWord`, `countSaid`.
- `internal/config`: retire `tools.search_limit` by rule 5; delete `SearchLimit`,
  `defaultSearchLimit`; in `normalize` the check becomes `if c.ReadLines < 1`.
- `go mod tidy` — `doublestar` and `go-gitignore` leave `go.mod`.
- Tests: `agent/prompt_test.go` holds byte budgets for the tool specs and the system prompt; lower
  them to the new sizes plus 100 bytes. `agent/class_test.go`: drop the removed tools.
- `README.md`: Requirements loses the `rg`/`fd` line; the Tools section names five tools plus
  `ask_user`; delete the `search_text` modes paragraph and the "several reads at once" paragraph's
  mention of the two tools.

Old sessions hold calls to the removed tools. They still load and draw: an unknown tool name
already reads as `ClassRuns` (`class.go`) and `tools.Describe` falls through to `brief`.

**Done when:** `grep -rn "find_files\|search_text\|loopGuard\|repeats\." internal --include='*.go'`
prints nothing outside test fixtures of old sessions.

---

## C · One way to make room

Read all of C before editing. It is the only step that adds logic.

### What exists

Over 5/6 of the ceiling, `prepareTurn` (`internal/agent/turn.go`) runs three things in order:
`shrinkWhatAged` (replaces old results, old write arguments and old thinking with markers),
`Compact` (the model summarises the older turns), `trimHistory` (blanks more results, then drops
whole calls). Each rewrites history at a different place. The summary is written from a re-rendered
transcript in which results are already markers, cut to 2,000 bytes a message, with no thinking in
it — so it summarises almost nothing (REVIEW.md B3). The fallback shortens results the model has
not read (B4). The estimate swings ±9% between requests (B5).

### What replaces it

1. Nothing is shortened. A message stays as it was sent until it is folded away.
2. Over the pressure mark, **fold**: one request that sends the conversation exactly as the server
   already holds it, plus one instruction asking for the summary. The server's prompt cache covers
   the prefix, and the summariser reads the real results, calls and thinking.
3. Over the ceiling after that, **drop** the oldest messages, whole, until it fits.
4. The estimate starts from the server's own count of the last request.

The marks do not change: `ceiling()` and `pressureMark()` in `budget.go` stay; `trimGoal()` goes.

### C1 · The estimate

In `Agent` (`agent.go`) add `anchorTokens, anchorBytes, anchorLen int`.

`estimateTokens` (`history.go`) becomes:

```go
// estimateTokens is what the next request will cost: the server's own count of the last one, plus
// what was added since at the byte ratio that count implied. With no count yet, the ratio prices all.
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

In `chat` (`turn.go`), right after `a.meter.observe(stats, a.sendingBytes())`:

```go
if stats.PromptTokens > 0 && len(req.Tools) > 0 { // a request without tools is not a normal one
	a.anchorTokens, a.anchorBytes, a.anchorLen = stats.PromptTokens, a.sendingBytes(), len(a.history)
}
```

Set `a.anchorTokens = 0` in `startOver`, in `Resume`, after a fold replaces history, and after a
drop. The ratio keeps being learned and saved exactly as now (`Learned`, `savedRatio`).

### C2 · The fold

Rewrite `Compact` in `compact.go`. `compactCut`, `dropping`, `callBlocks`, `foldOverdue`,
`canCompact`, `keepSummary`, `checkSummary` and everything in `files.go` stay as they are.

```go
func (a *Agent) Compact(ctx context.Context) int {
	a.sanitizeHistory()
	before := a.estimateTokens()
	start, task := a.compactCut()
	if start < 2 {
		a.compactedAt = callBlocks(a.history)
		a.tell("too little older work to fold")
		return 0
	}
	summary, err := a.summarise(ctx, start)
	if err != nil {
		a.compactedAt = callBlocks(a.history) // the next try waits for new work
		a.tell("the older turns could not be summarised, so they stay as they are: " + err.Error())
		return 0
	}
	dropped := a.dropping(start, task)
	if len(summary) >= historyBytes(dropped) {
		a.compactedAt = callBlocks(a.history)
		a.tell("the older turns are already smaller than a summary of them: nothing was folded")
		return 0
	}
	a.noteFiles(dropped)
	kept := []llm.Message{{Role: "user", Content: FoldMark + "\n" + summary + a.fileBlocks() + a.planBlock()}}
	if task > 0 {
		kept = append(kept, a.history[task]) // the job in hand, word for word
	}
	a.history = append(a.history[:1:1], append(kept, a.history[start:]...)...)
	a.compactedAt, a.anchorTokens = callBlocks(a.history), 0
	a.work.made(roomFolded(len(dropped)))
	a.emit(Event{Kind: Summary, Text: summary})
	a.reportSave(a.opts.Session.Folded(session.Fold{Kept: a.history[1:], Summary: summary,
		ReadFiles: a.readOnlyFiles(), ModifiedFiles: a.modifiedFiles, Dropped: len(dropped), TokensBefore: before}))
	a.publishUsage()
	return before - a.estimateTokens()
}
```

`summariseWithModel` becomes `summarise`:

```go
// summarise asks for the summary of history[:start] by sending exactly those messages, as every
// request before it sent them, and one instruction after them. The server already holds that
// prefix, so the request costs the summary and little else.
func (a *Agent) summarise(ctx context.Context, start int) (string, error) {
	prefix := a.history[:start]
	if a.opts.CtxSize > 0 {
		rate := max(a.meter.bytesPerTok, minBytesPerTok)
		sent := a.sendingBytes() - historyBytes(a.history[start:]) + len(compactPrompt)
		if int(float64(sent)/rate)+compactMaxTokens > a.usable() {
			return "", fmt.Errorf("they are larger than the window")
		}
	}
	messages := append(append([]llm.Message{}, prefix...), llm.Message{Role: "user", Content: NoteMark + compactPrompt})
	var why error
	// first exactly as a normal request, so the cached prefix matches; then with tools refused,
	// for a model that answered the first one with a call
	for _, choice := range []string{"auto", "none"} {
		turn, err := a.client.ChatStream(ctx, llm.ChatRequest{
			Model: a.model, Messages: messages, Tools: a.specs(), ToolChoice: choice,
			MaxTokens: compactMaxTokens, Sampling: a.opts.NoThink,
			ChatTemplateKwargs: map[string]any{"enable_thinking": false},
		}, nil)
		if err != nil {
			return "", err
		}
		summary := keepSummary(turn.Message.Content)
		if len(turn.Message.ToolCalls) > 0 {
			why = fmt.Errorf("the model called a tool instead of writing the summary")
			continue
		}
		if why = checkSummary(summary); why == nil {
			a.tell(fmt.Sprintf("summary written: the server had %d of the %d prompt tokens cached",
				turn.Timings.CacheN, turn.Timings.PromptTokens()))
			return summary, nil
		}
	}
	return "", why
}
```

`compactPrompt` becomes one instruction (merge the old `compactPrompt` and `compactAsk`). Keep the
six sections and the rules word for word, with three changes:
- first line: "The conversation above is about to leave your context window. Write the memory you
  will carry on from: the summary below is all that is kept of it, plus the newest messages."
- the rule about `<previous-summary>` becomes: "If the conversation above begins with an earlier
  summary, update it instead of starting over: keep what is still true, move finished work to Done,
  drop what no longer matters, add what changed since."
- last line: "Write the summary now, starting with \"## Goal\". Do not call a tool."

Delete from `compact.go`: `compactAsk`, `compactLineCap`, `compactArgCap`, `foldNote` use,
`foldFreed`, `shrinkForCompaction`, `transcriptOf`, `cutText`, `callLine`. Delete the `fold` field
of `Agent` and the `foldNote` type in `files.go`.

### C3 · The emergency drop

In `history.go`, replace `trimHistory`, `trimResults`, `firstUnseen`, `trimmed`,
`dropOldestToolCall`, `dropOldestExchange`, `anotherTurnFollows` and `trimMarker` with:

```go
// makeRoom is the last resort: when the conversation is over the ceiling even after a fold had its
// turn, the oldest messages go, whole, until it fits.
func (a *Agent) makeRoom() {
	if a.opts.CtxSize <= 0 {
		return
	}
	dropped := 0
	for a.estimateTokens() > a.ceiling() && a.dropOldestUnit() {
		dropped++
	}
	if dropped == 0 {
		return
	}
	a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
	a.work.made(roomDropped(dropped))
	a.tell(fmt.Sprintf("dropped %s to fit the context", counted(dropped, "older message", "older messages")))
}

// dropOldestUnit removes the oldest message that may go: a call goes with its replies. What stays
// whatever happens: the system prompt, a fold's recap, the ask being answered, and the newest call
// with everything after it.
func (a *Agent) dropOldestUnit() bool {
	tail := len(a.history)
	for i := len(a.history) - 1; i > 0; i-- {
		if m := a.history[i]; m.Role == "user" || (m.Role == "assistant" && len(m.ToolCalls) > 0) {
			tail = i
			break
		}
	}
	for i := 1; i < tail; i++ {
		m := a.history[i]
		if _, recap := recapOf(m); recap || m.Role == "tool" || (m.Role == "user" && m.Content == a.task) {
			continue
		}
		end := i + 1
		for end < tail && a.history[end].Role == "tool" {
			end++
		}
		a.history = append(a.history[:i:i], a.history[end:]...)
		a.anchorTokens = 0
		return true
	}
	return false
}
```

### C4 · Wiring

`prepareTurn` (`turn.go`), after the loop that takes the waiting asks:

```go
if a.underPressure() && a.canCompact() && a.foldOverdue() {
	a.tell("the conversation is out of room: asking the model to summarise the older turns")
	a.work.made(roomFolding)
	a.Compact(ctx)
}
a.makeRoom()
a.publishUsage()
```

Move `underPressure` from `shrink.go` to `budget.go`.

The overflow branch in `Ask` (`llm.IsOverflow(err) && !overflowed`) becomes:

```go
overflowed = true
a.tell("the server says the conversation does not fit its window: making room and asking again")
a.meter.bytesPerTok = max(a.meter.bytesPerTok*3/4, minBytesPerTok)
a.anchorTokens = 0
if a.Compact(ctx) <= 0 {
	for i := 0; i < 8 && a.dropOldestUnit(); i++ {
	}
	a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
}
continue
```

`Resume` (`agent.go`) ends with `a.anchorTokens = 0` and `a.publishUsage()`; `restorePlan` and
`restoreFiles` stay.

### C5 · What is deleted

- `internal/agent/shrink.go` and `shrink_test.go`, whole. Before deleting, check the build for
  anything else that used `repliesFor`, `ranToTheEnd`, `summaryFloor`, `shrinkMark`, `keepResults`;
  delete the use, not the deletion.
- `internal/agent/agent.go`: `revised` and `removed`.
- `internal/agent/contextwork.go`: `roomShortened` and `roomTrimmed`.
- `internal/session/session.go`: the **writers** `Revise` and `Remove`. **Keep** the `change` type,
  `apply`, and the branch of `load` and of `describeLine` (`catalogue.go`) that reads `revised` and
  `removed` lines: old files have them and must load. `Learned`, `Rewritten`, `Folded`, `Cleared`,
  `Append` stay.
- `internal/tools/store.go`, `store_test.go`; `tools.Options.Store`; `agent.Options.Store`; the
  `store` parameter of `app.Options` and of `Agent.ContinueFrom`; `tools.NewResultStore` in
  `app/run.go`; `Config.ResultDir` and its line in `normalize`; in `app/home.go` the
  `os.RemoveAll(...ResultDir...)` in `pruneSessions` and all of `pruneOrphanResults` (then
  `pruneConversations` is just `pruneSessions`, without the `alive` map); in
  `tui/sessionscreen.go` `continueWith` loses `where`.
- `CallID` (`r1`, `r2`…) on tool messages stays. It costs nothing and old files carry it.

### C6 · Tests

Delete `resume_prefix_test.go`'s tests of revisions, and every test of shortening or trimming in
`history_test.go`, `compact_test.go`, `budget_test.go`. Keep the tests of `compactCut`, of the
marks, of `sanitizeHistory`, of the file lists. Write these first; each must fail before C and
pass after. The stub server in `agent_test.go` records every request body, which is what they read.

1. **A fold sends the conversation it summarises.** Fill a history past the pressure mark, run
   `prepareTurn`. The stub's first request has, as its messages, exactly `history[:start]` followed
   by one user message beginning with `NoteMark`; it carries the `tools` array.
2. **Nothing is shortened.** After `prepareTurn` on a history just under the pressure mark, every
   message is byte-for-byte what it was.
3. **A fold never touches the newest call.** After a fold, the last assistant message with tool
   calls and its replies are byte-for-byte what they were.
4. **A summary that is a tool call is retried without tools, then refused.** Stub answers with a
   tool call twice: `Compact` returns 0, history is unchanged, two requests were made, the second
   with `"tool_choice":"none"`.
5. **The drop keeps what it must.** History of system, recap, the task, ten call blocks, over the
   ceiling: after `makeRoom` the recap, the task and the last call block are still there, and the
   estimate is under the ceiling.
6. **The estimate follows the server's count.** After a stub reply reporting `prompt_n` 1000, with
   200 bytes added, `estimateTokens()` is 1000 + 200/ratio ± 10.
7. **An old log still loads.** A session file containing `revised` and `removed` lines (copy the
   fixture from the existing `session_test.go`) loads to the same messages as before.
8. **A resume writes nothing** (from A2) still passes.

### C7 · Check it on the real server

After C is green, the owner should run one long session and read the notice a fold prints:
`summary written: the server had X of the Y prompt tokens cached`. X close to Y means the fold is
nearly free. X near zero means the server re-read everything: the fold still works, and the cause
is worth finding (the first thing to compare is the body of the fold request against the request
before it).

### C8 · Docs

Rewrite README's "Context and cost" list as two steps (fold over 5/6 of the ceiling; drop over the
ceiling) and delete everything about shortened results, `~/.ykode/results`, the "unchanged since"
line, and "trimmed N tool results". In the footer paragraph the things an ask may do to fit are
now `summarising older turns`, `folded N older turns`, `dropped N older messages`.

---

## D · Two nudges go

In all 21 saved sessions the verify nudge fired twice and the wrap-up warning three times. Both
put words in the user's mouth on a guess.

**Verify nudge.** Delete from `internal/agent/verify.go`: `verifyNote`, `projectWorkRe`,
`looksLikeProjectWork`, `noToolsRe`, `forbidsTools` (keep `textCallRe`, `hasTextCall`,
`withoutTextCalls`; move them into `textcalls.go` and delete `verify.go`). In `Ask` (`turn.go`)
delete the `if !nudged && !usedTools && …` block and the variables `nudged` and `forceTool`;
`request()` loses its `forceTool` parameter and the `"required"` branch. Delete `Options.NoVerify`,
`Config.NoVerify`, the `--no-verify` flag; retire the `verify` setting by rule 5; remove `verify`
from `liveKnobs` and `NoVerify` from `applyLive` (`tui/settingsscreen.go`).

**Wrap-up warning.** In `internal/agent/rounds.go` delete `wrapUpShare`, `wrapUpMinTurns`,
`wrapUpTurn`, `warnNearLimit`, and the `a.warnNearLimit(turn)` call in `Ask`. `outOfRounds` and
`outOfTurns` stay: a run that uses all its rounds is still asked where the work stands.

**These stay**, because they fired and they work: running a tool call the model wrote as text (14
times), continuing an empty reply (6), continuing a reply cut at `max_tokens`, the "stopped
halfway" nudge (4), the repeated-line detector.

README: delete `--no-verify`, the `verify` setting, and the sentence about the model being "told to
start finishing".

---

## E · Last pass

1. `go mod tidy`; `go build ./...`; the check.
2. Rule 6: read `Commands()`, the `keys` list, `knobs()` and `liveKnobs` once more against what
   exists. `/help` and `?` must not name anything removed.
3. Start the binary in a terminal and walk this list. Each line must work:
   - type a prompt, watch thinking, an answer, a tool row, a diff under an edit;
   - `/help`, `/tools`, `/ctx`, `/usage`, `/model`, `/sessions`, `/settings`, `/plan`, `/think high`,
     `/compact`, `/reset` (asks first);
   - in a project with no `.ykode` folder, `/settings` shows the global warning; after
     `/create-local-setting` it names the project file, a change lands there, and
     `~/.ykode/settings.yaml` is unchanged;
   - `?` shows the keys; `tab` completes `/se` ; `ctrl+p` walks earlier prompts;
   - `esc` during a reply stops it, once, with one notice;
   - quit, then `ykode --continue last`: the conversation is drawn and nothing is trimmed on load;
   - `ykode -p "say hi" --yes` prints an answer and exits.
4. `README.md` read top to bottom against the binary. `TODO.md`: delete T1 (the two walkers are
   gone), the `SEC` lines about the sandbox, `B4`'s `/compact` hint if it no longer applies, and
   anything that names a removed feature.
5. Tell the owner: `~/.ykode/audit` and `~/.ykode/results` are no longer read or written and can be
   deleted by hand; `bin/` holds 163 MB of old binaries.

---

## Not in this guide — ask the owner first

| what | lines | why it is here and not above |
|---|---|---|
| `ask_user` | 267 + 579 | used twice in 21 sessions, but it is a TUI feature |
| typing into a running turn (steering) | ~60 + 130 | unknown whether the owner uses it |
| allow-list and confirm bar | 130 + 175 | the owner runs `assume_yes: true`, others may not |
| stripping a leading `cd <root> &&` from commands | ~45 | it changes what the row shows |
| stream idle timeout while the server reads a cold prompt (REVIEW.md B6) | — | needs a timing on the owner's server |
| thinking shown in full in the transcript | — | the model writes ~18× more thinking than answer; collapsing finished thinking to its last lines would be a design change to the screen |
