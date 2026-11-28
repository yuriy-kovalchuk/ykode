# Commands and keys

## How a turn runs

A model answers, asks for tools, the loop runs again, and its thinking is shown apart from its answer.
A reply that ends after its thinking, with no call and no answer, is not kept: the same request is sent
again, at most twice per stall, because a kept one teaches the model that a turn may end there.
Requests stay byte-stable: the system prompt and the tool list never change mid-session, and news
about the date or the git state travels on the newest ask, so the server's prompt cache keeps hitting.

## Commands

| command | what it does |
|---|---|
| `/help` | the commands, narrowed by typing |
| `/usage` | what the conversation has cost the server |
| `/ctx` | how full the window is, and what the server is caching |
| `/compact` | fold the older turns into a summary the model writes |
| `/sessions` | the conversations saved for this project; pick one and this run continues it |
| `/model [id]` | list this server's models, or switch to one |
| `/think [level]` | how deeply the model thinks, sent as `reasoning_effort`; the token cap is `thinking_budget` |
| `/plan` | the checklist as the model last wrote it |
| `/tools` | what the model may call, and what each description costs |
| `/settings [list \| key]` | the settings screen, where every knob is changed; `list` prints every one, a key explains it |
| `/create-local-setting` | make `.ykode/settings.yaml` for this project |
| `/reset` | forget the conversation; the screen asks first |
| `/exit`, `/quit` | quit |

Leaving a session that saved anything ends with the command that resumes it, alone on the last line,
so the whole line can be copied: `ykode --continue s20261127-101010 --root /path/to/project`.

## Keys

| key | what it does |
|---|---|
| `enter` | send |
| `ctrl+j`, `alt+enter` | start a new line, in every terminal. `ctrl+enter` and `shift+enter` too where the terminal speaks the kitty keyboard protocol or xterm's modifyOtherKeys (kitty, Ghostty, WezTerm, foot, iTerm2, recent xterm); elsewhere they arrive as a plain `enter` and send |
| `ctrl+p`, `ctrl+n` | walk earlier prompts, so a wrong ask is edited rather than repeated |
| `tab` | complete a command |
| `ctrl+v` | paste the clipboard's picture as an `@path`, or its text |
| a paste | goes in as typed when the box can show it; past 10 lines or 1,000 bytes it is one token, `[Pasted text #1 +42 lines]`, which the model receives as the whole text while the box and the ask's card keep the token. Delete the token to drop the paste; `ctrl+p` sends an old one again |
| `esc`, `ctrl+c` | stop a run; at an idle prompt `ctrl+c` quits |
| `pgup`, `pgdown`, `ctrl+u`, `ctrl+d`, `up`, `down` | scroll the transcript, by a page or half a page; the arrows are the cursor while the box holds more than one line |
| `end` | return to the live end |
| `ctrl+o` | fold the plan |
| `ctrl+r` | show every command whole under the row that cut it, line breaks kept; again to fold them back |
| `?`, `ctrl+/` | list the keys |
