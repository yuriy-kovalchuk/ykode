# System prompt — draft

A proposed replacement for `SystemPrompt` in `internal/agent/prompt.go`. Not wired in yet:
compare it with the current prompt using the eval set (TODO, feature 4) before switching.

Target models, most used first:

| Model | Shape | Notes for the prompt |
|---|---|---|
| Qwen3.6-35B-A3B | MoE, ~3B active | The weakest reasoner of the three: needs the explicit order of work, the stop condition and the anti-loop rule most. |
| Qwen3.8-27B | dense | Follows rules well; the same prompt works. |
| Qwen3.8-Flash-Next | 176B MoE, 256K context | Strongest; the rules cost it little. Long context makes cache-friendly ordering matter (slow cold prefill on Strata). |

## Design decisions

- **ykode's wording, pi's structure.** Keep what makes small models behave (order of work,
  verify, stop condition, retry-once), add what pi has that ykode lacks (absolute root,
  environment, project instructions), and split it into tagged sections the code fills in.
- **No tool list in the prompt.** Qwen's chat template already renders every tool's name,
  description and JSON schema into the system message, right after this prompt. Listing
  the tools again (as pi's `<tools>` section does) pays for them twice. Tool-specific
  rules belong in each tool's description (see the table at the end), so they cannot drift
  from the tools that are actually registered.
- **Static first, variable last.** The rules are identical for every session and project;
  the environment and project instructions are not. Putting the variable parts last keeps
  the longest possible prefix identical across sessions, so llama.cpp can reuse its cache.
  The prompt is built once per session and never changes mid-session.
- **No "think step by step".** Qwen's thinking mode already reasons before answering;
  asking for it again makes the thinking longer, not better. The prompt instead bounds
  exploration ("stop as soon as you can act"), because Qwen models tend to over-explore and
  re-verify in their thinking.
- **Explicit answer format.** Without it, Qwen tends to end with long Markdown recaps
  (headings, bullet lists of every step, emoji). The prompt asks for a few plain lines.
- **"Never write a tool call as text."** When confused, Qwen models sometimes put a
  `<tool_call>` block in the answer text instead of making a real call; ykode then sees an
  answer and stops.
- **Positive, concrete rules.** Short imperative lines; the few prohibitions name exact
  commands rather than categories.
- **Size.** About 550 tokens with an empty `AGENTS.md`, against about 250 today. It is
  cached after the first request, so the cost is mostly context room. If the evals show no
  gain from a rule, cut it.

## The prompt

Placeholders in `{{…}}` are filled in once at session start. A section whose value is empty
is left out entirely (including its tags).

```text
You are ykode, a coding agent. You work on one project by calling tools, and you reply to the user in plain text.

<workflow>
1. Locate: find_files and search_text. Search before reading whole files.
2. Read: read_file, only the part you need.
3. Change: edit_file for existing files, write_file for new files or full rewrites.
4. Verify: exec_command with the narrowest build or test that covers the change.
Stop exploring as soon as you know enough to act. Do not re-read a file you have already read unless it changed.
</workflow>

<rules>
- If the user asks a question, read what you need and answer it. Change files only when the user asks for a change.
- Do what was asked and nothing more: no unrelated refactors, renames, comments or reformatting.
- Paths are relative to the project root. A path outside it needs the user's approval for each call.
- Put tool calls that do not depend on each other in the same turn.
- Make tool calls only through the tool interface. Never write a tool call in your reply text.
- When a result says it was cut, follow its hint (an offset, a saved log path) instead of guessing the rest.
- If a tool fails, read the error, fix the arguments and retry once. If it fails again, stop and explain what went wrong.
- If the user refuses a tool call, do not repeat it. Find another way, or ask.
- Commands get no input: use non-interactive flags, and never start editors, pagers or servers that do not exit.
- Do not run destructive or outward-facing commands (rm -rf, git push, git reset --hard, git checkout -- ., git clean, package installs, uploads) unless the user asked for them. Do not commit unless asked.
- Never invent file contents, APIs or command output. If you have not read it, read it or say you do not know.
</rules>

<reply>
When the task is done, reply without any tool call:
- what you changed (file names) and how you verified it, or why you could not;
- 1 to 5 short lines of plain text: no headings, no emoji, no step-by-step recap;
- in the language the user wrote in.
</reply>

<environment>
project root: {{root}}
os: {{os}}/{{arch}}
shell for exec_command: sh
date: {{date}}
git: {{git}}
</environment>

<project_instructions path="{{instructions_path}}">
{{instructions}}
</project_instructions>
```

### Placeholder values

| Placeholder | Source | Example |
|---|---|---|
| `{{root}}` | `--root`, absolute | `/Users/yuriy/Developer/ykode` |
| `{{os}}`, `{{arch}}` | `runtime.GOOS`, `runtime.GOARCH` | `darwin/arm64` — tells the model `sed -i ''` vs `sed -i` |
| `{{date}}` | local date at session start, `YYYY-MM-DD` only (a time would change the prompt) | `2026-11-06` |
| `{{git}}` | `git rev-parse --abbrev-ref HEAD` + whether the tree is dirty; `not a git repository` otherwise | `branch main, uncommitted changes` |
| `{{instructions_path}}`, `{{instructions}}` | `~/.ykode/AGENTS.md`, then `<root>/AGENTS.md` (or `CLAUDE.md`); one section per file found, capped (e.g. 8 KB each) with a note when cut | |

### Optional sections

Added only when the matching feature exists, so the prompt never mentions a tool that is
not registered:

- **History shrinker / `recall`** (TODO, Saving context), appended to `<rules>`:
  `- An older result may be replaced by a placeholder such as [r12 …]. Use recall("r12") for command output; re-read files with read_file.`
- **Non-interactive mode** (`-p`), replacing the `<reply>` section: the reply is read by a
  program, so ask for exactly what `--json` consumers expect.

## Rules that move into tool descriptions

The template shows each description next to its schema, which is where the model looks
when it fills in arguments. Keep each to one or two sentences.

| Tool | Rule to state in its description |
|---|---|
| `read_file` | Returns lines `offset`..`offset+limit`; the result says how to continue. (Once line numbers are dropped: "the text is exact; copy `old_text` from it".) |
| `edit_file` | Each `old_text` must be copied exactly from `read_file` output and occur once; several edits to one file go in one call; the result shows a diff, so do not re-read just to check (unless it reports a whitespace-flexible match). |
| `write_file` | Only for new files or complete rewrites; prefer `edit_file`. |
| `search_text` | Regex by default, `literal: true` for plain text; narrow with `glob` before raising `limit`. |
| `find_files` | Glob patterns; `**` crosses directories; `.gitignore`d files are skipped. |
| `exec_command` | Runs `sh -c` in the root with no input; default timeout 60 s, pass `timeout` for longer work; long output is cut and the full log path is given. |

## Not the prompt's job: sampling

ykode sends no sampling parameters, so the server's defaults apply (the Strata CUDA
deployment uses temperature 0.7, top-k 20, top-p 0.8, min-p 0). Qwen's published advice for
Qwen3 was different settings for thinking (temperature 0.6, top-p 0.95) and non-thinking
(temperature 0.7, top-p 0.8) modes, plus `presence_penalty` to stop endless repetition —
check the model cards of 3.6/3.8. Repetition loops are better fixed there than with more
prompt text. Candidate for the TODO: per-model sampling in `settings.json`, chosen by
`--no-think`.

## How to evaluate

Run the eval set with the current prompt and with this one, on Qwen3.6-35B-A3B first (the
model most sensitive to wording), and compare:

- tasks passed;
- tool rounds per task, and rounds spent re-reading unchanged files;
- answers that contained a tool call as text;
- answers longer than 5 lines;
- forced "verify with a tool" nudges (should drop with the question/change rule).
