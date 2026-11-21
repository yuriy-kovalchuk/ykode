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
| `/think [level]` | how deeply the model thinks |
| `/plan` | the checklist as the model last wrote it |
| `/tools` | what the model may call, and what each description costs |
| `/settings [list \| key] [value]` | the knobs: `list` shows every one, a key explains it, a key and a value change it |
| `/create-local-setting` | make `.ykode/settings.yaml` for this project |
| `/reset` | forget the conversation; the screen asks first |
| `/exit`, `/quit` | quit |

## Keys

| key | what it does |
|---|---|
| `enter` | send |
| `ctrl+j`, `alt+enter` | start a new line, in every terminal. `ctrl+enter` and `shift+enter` too where the terminal speaks the kitty keyboard protocol or xterm's modifyOtherKeys (kitty, Ghostty, WezTerm, foot, iTerm2, recent xterm); elsewhere they arrive as a plain `enter` and send |
| `ctrl+p`, `ctrl+n` | walk earlier prompts, so a wrong ask is edited rather than repeated |
| `tab` | complete a command |
| `ctrl+v` | paste the clipboard's picture as an `@path`, or its text |
| `esc`, `ctrl+c` | stop a run; at an idle prompt `ctrl+c` quits |
| `pgup`, `pgdown`, `ctrl+u`, `ctrl+d`, `up`, `down` | scroll the transcript, by a page or half a page; the arrows are the cursor while the box holds more than one line |
| `end` | return to the live end |
| `ctrl+o` | fold the plan |
| `?`, `ctrl+/` | list the keys |
