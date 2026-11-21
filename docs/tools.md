# Tools

Six tools, in one file each. `/tools` shows what each description costs the prompt, and
`tools.disabled` ([settings](settings.md)) takes any of them off the list the model is offered.

| tool | what it does |
|---|---|
| `read_file` | a file as text, from a line offset, capped at 2000 lines by default (`tools.read_lines`). A png, jpeg, gif or webp goes to the model as an image instead, sized down for a vision encoder |
| `write_file` | creates or replaces a whole file, making parent directories |
| `edit_file` | exact replacement: each `old_text` is copied from a read and must occur once, so a wrong guess fails instead of rewriting the wrong lines |
| `exec_command` | one `sh -c` command in the project root, 60 s by default (`tools.exec_timeout_sec`), output cut to a budget that scales with the window |
| `plan` | the model's own checklist, kept in memory and shown beside the prompt |
| `ask_user` | one question, with choices, answered in the box below the bar |

## Confinement and confirmation

Every path is project-relative and confined to `--root` through one `*os.Root`: no path walks out and
no symlink lands outside it. Reads may run together; writes and commands never do. A write or a
command is confirmed one at a time unless `assume_yes`, and a path outside `--root` is always asked
about. A command is not confined the way a file tool is: it runs with your permissions, in an
environment reduced to `PATH`, `HOME`, `TERM` and `LANG`, and its output is cut to a tenth of the
window (4.8 KB at a 4k context, 51.2 KB at 128k, 12 KB when the window is unknown), head and tail
kept, with the whole of it saved under `$YKODE_HOME/logs` for 24 h (`tools.command_log_dir`,
`tools.keep_command_logs`) and its path handed to the model.

## Pictures

A vision model sees a picture three ways: `read_file` on one, `@path` in an ask (`@shot.png`,
`@~/Desktop/shot.png`, `@"a name with spaces.png"`), and `ctrl+v` with a picture on the clipboard, which
saves it under `~/.ykode/pastes` for a day and types its `@path` into the box, where it can be read and
taken out before the ask goes. A path in an ask is yours, so it may lie outside `--root`, unlike one the
model names. `ctrl+v` reads the clipboard with `osascript` on macOS, `wl-paste` on Wayland and `xclip`
elsewhere; with text there, it pastes the text. A model that reads no pictures is sent none, and the
screen says so.
