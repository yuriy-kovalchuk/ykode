# Migrating the screen to Bubble Tea v2

Decided, not started. `internal/tui` stays on `github.com/charmbracelet/*` v1 until someone takes
this whole change in one pass: one package, two `tea.Model`s, no way to run v1 and v2 side by side.

## Why

- **The Enter chords.** `internal/tui/model.go:76` binds newline to `ctrl+j`, `alt+enter`,
  `shift+enter`. `ctrl+enter` cannot be added on v1: its parser has no modifier beyond `alt`, and
  the encodings that do carry it arrive as the unexported `unknownCSISequenceMsg`
  (`bubbletea@v1.3.10/key.go:548`), so a `"ctrl+enter"` entry in that `SetKeys` list is a string
  nothing can ever produce. v2 decodes them as ordinary keys and asks the terminal for the
  protocol itself, on every render, with the pop on the way out.
- **v1 is a finished line.** `go list -m -versions` ends at `v1.3.10`. The development line is
  v2 (`v2.1.2`), with bubbles `v2.2.1` and lipgloss `v2.0.5`.
- **Themes ride along.** Sized S when it was still an idea, and their first edit is the same nine
  colour pairs this pass already rewrites, so the palette is touched once instead of twice.

## Measured, with the probes still on disk

`/tmp/keyprobe` (v1) and `/tmp/v2keys`, `/tmp/v2probe` (v2) feed bytes to a real `tea.Program` and
print what the model receives.

| keys | bytes | v1.3.10 | v2.1.2 |
|---|---|---|---|
| Enter | `\r` | `"enter"` | `"enter"` |
| Ctrl+J | `\n` | `"ctrl+j"` | `"ctrl+j"` |
| Alt+Enter | `\x1b\r` | `"alt+enter"` | `"alt+enter"` |
| kitty Ctrl+Enter | `\x1b[13;5u` | `unknownCSISequenceMsg` | `"ctrl+enter"` |
| kitty Shift+Enter | `\x1b[13;2u` | `unknownCSISequenceMsg` | `"shift+enter"` |
| xterm modifyOtherKeys Ctrl+Enter | `\x1b[27;5;13~` | `unknownCSISequenceMsg` | `"ctrl+enter"` |

v2's renderer writes `SetModifyOtherKeys2` and `PushKittyKeyboard(flags)` on the first render and
`ResetModifyOtherKeys` plus `PopKittyKeyboard` on close, and `keyboardEnhancementsFlags` starts at
`flags = 1`, so basic disambiguation is on without the app asking
(`bubbletea/v2@v2.1.2/cursed_renderer.go:436-465, 599, 977`). Nothing in ykode has to write a
keyboard sequence to get the chords — the terminal still has to support them.

Import paths move: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`.
`go get github.com/charmbracelet/bubbletea/v2` now fails with "module declares its path as
charm.land/bubbletea/v2". `tea.Msg` becomes an alias of `ultraviolet.Event`, pulling
`charmbracelet/ultraviolet`, `x/termios`, `x/windows`.

## The shape changes, against this tree

`internal/tui` is the only package that touches the screen: 37 non-test files, 364 `tea.X`
references over 44 identifiers, 161 `lipgloss.X` references over 12 identifiers, 4 bubbles imports.

- **`View() string` → `View() tea.View`** — two models: `draw.go:47` (main screen) and
  `setup.go:200` (the server wizard). `tea.NewView(body)`; the setup screen is also drawn *inside*
  the main screen, so `draw.go:49` takes `m.modal.View().Content`.
- **Alt screen** — `tea.WithAltScreen()` is gone: `tui.go:174` and `setup.go:40` lose the option and
  each `View` sets `v.AltScreen = true` instead. Tests that call `run` with a fake terminal will now
  see alt-screen and keyboard-protocol sequences in the writer.
- **Keys** — `tea.KeyMsg` is an interface (`String()`, `Key()`), the values are `KeyPressMsg` /
  `KeyReleaseMsg`, and `Key{Text, Mod, Code, ShiftedCode, BaseCode, IsRepeat}`. `String()` keeps the
  v1 spelling, so the string comparisons across `keys.go`, `list.go`, `settingsscreen.go` survive
  untouched. Key *fields* are read in a handful of places (25) and those change.
- **Mouse** — `MouseAction`/`MouseButton*` and the `Action`/`Button` fields are gone: `scroll.go:12`
  switches on `tea.MouseWheelMsg` and `Button: tea.MouseWheelUp/Down`; `model.go`'s intercept stays
  on the `tea.MouseMsg` interface so a click still reaches nothing.
- **Paste** — a bracketed paste is `tea.PasteMsg{Content}`, not a key, so it never reaches
  `handleKey`; the box has to be fed from `Update`. `bubbles/v2` textarea's own paste path came back
  as `"one"` for `"one\ntwo"` in the probe, which contradicts the README's "a paste keeps its own
  newlines" — **verify before trusting it**, and expect to insert the content yourself.
- **Styling** — lipgloss v2 `Render` always emits escapes; the profile only ever applies at a
  writer. `AdaptiveColor` moves to `charm.land/lipgloss/v2/compat` and takes `color.Color`, so the
  nine palette entries become `compat.AdaptiveColor{Dark: lipgloss.Color("#A78BFA"), …}` (a
  two-line `shade(dark, light)` helper keeps the table readable). `HasDarkBackground()` becomes the
  compat var, `SetColorProfile`/`ColorProfile` are gone and their place is `compat.Profile` plus
  `github.com/charmbracelet/colorprofile`. The reset lipgloss writes is `\x1b[m`, not `\x1b[0m`,
  and `wrap.go:43 styleReset` is compared against rendered output by `markdown_test.go:183` and
  `diff_test.go:63,66`, so it moves with it. Underline and strike are still one escape per
  character, so `markdown.go`'s hand-written splice stays.
- **bubbles v2** — `viewport.New(1, 1)` becomes `viewport.New(viewport.WithWidth(1),
  viewport.WithHeight(1))`; `YOffset`, `XOffset`, `Width`, `Height` are methods; `textinput` and
  `textarea` widths go through `SetWidth`. Call sites: `list.go:65,187,227,228,248`,
  `settingsscreen.go:39`, `setup.go:82,93`. `Blink()`, `WithStyle`, `Placeholder`, `Prompt`,
  `ShowLineNumbers`, `EchoMode`, `KeyMap` all survive. Components gain `Cursor() *tea.Cursor` and
  `SetVirtualCursor`; the virtual caret is still what the component draws, so the layout stays.

A first pass that only swaps the imports produced 27 real errors and nothing surprising: palette
(10), list (5), markdown (1), scroll (5), settingsscreen (1), setup (4), tui (1).

## The cost is the tests, not the screen

36 test files drive the screen. Roughly 90 of them build a key by hand:

| v1 | v2 |
|---|---|
| `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("timer")}` | `tea.KeyPressMsg{Text: "timer"}` (verified: the box takes all of it, `String()` is `"timer"`) |
| `tea.KeyMsg{Type: tea.KeyEnter}` | `tea.KeyPressMsg{Code: tea.KeyEnter}` |
| `tea.KeyMsg{Type: tea.KeyCtrlP}` | `tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}` |
| `tea.KeyMsg{Type: tea.KeyCtrlUnderscore}` | `tea.KeyPressMsg{Code: '_', Mod: tea.ModCtrl}` → `"ctrl+_"` |
| `tea.KeyMsg{Type: tea.KeyUp / KeyEsc / KeyBackspace / KeyEnd / KeyTab}` | `tea.KeyPressMsg{Code: tea.KeyUp …}` |
| `tea.KeyMsg{Type: tea.KeyRunes, Runes: …, Paste: true}` | `tea.PasteMsg{Content: …}` — and it is no longer a key |
| `tea.MouseMsg{Action: Press, Button: MouseButtonWheelUp}` | `tea.MouseWheelMsg{Button: tea.MouseWheelUp}` |
| `lipgloss.SetColorProfile(termenv.TrueColor)` | `compat.Profile = colorprofile.TrueColor` (a var; restore in cleanup) |

The harder one: v1 tests ran at the Ascii profile, so `m.View()` was plain text and assertions read
as plain text. In v2 the styled spans carry escapes, so content assertions need either a helper that
strips SGR for the tests that are about text (`ansi.Strip` from `x/ansi`, which is already a
dependency) or updated expectations, with the raw view kept wherever the styling *is* the subject
(`palette_test.go`, `diff_test.go`, `highlight_test.go`).

## Themes, in the same pass

The colours really are one map, which is what makes both jobs cheap to do together:

- `internal/tui/palette.go`: 9 dark/light shade pairs and 20 styles built from them, referenced 205
  times outside tests and 35 inside them. The styles therefore stay package-level variables and a
  theme re-derives them; threading a palette through the screen is 205 edits for the same result.
- 30 `NewStyle()` calls run at init: 20 in `palette.go`, 3 in `settingsscreen.go:166-168`, one in
  `overlay.go:24`, 4 in `setup.go:192-197`, and the four diff grounds behind `diffSides`'s
  `sync.OnceValue` (`palette.go:76`). Every one is rebuilt by the theme or the screen is half
  themed, and `overlay.go` plus the setup wizard are the ones easy to leave out.
- `setup.go:192-197` picks raw ANSI `6` and `1` instead of palette roles, so the wizard is already
  off-palette; those four become rows in the table.
- `config` hands over the rest: one `knob` row (`catalogue.go:41`) with
  `options: []string{"auto", "dark", "light"}` brings the ←/→ picker, the value check, the strict
  loader refusing a bad name, and with `live: true` the `applies now` label and the hook in
  `applyLive` (`settingsscreen.go:353`). Plus `Config.Theme`, a preset, a `fileSettings` field.

Design: `internal/tui/theme.go` holds the table of roles per theme and one `applyTheme(name)` that
fills the colours and rebuilds the styles. `newModel` calls it before the first draw, and so does
`Setup`, which runs as its own program before any model exists; `applyLive` calls it again when the
knob moves. `diffSides` loses its `sync.OnceValue` — it caches on the terminal's background, which
is what a theme overrides — and `HasDarkBackground` moves inside `applyTheme("auto")` so an explicit
theme beats detection. Swapping styles mid-session is safe because every style read is on the
screen's own goroutine (`applyLive` included), which is worth one line in AGENTS.md next to the
other single-owner rules; `internal/cli` never styles, since only `internal/tui` imports lipgloss.

Two decisions this plan will not make for you: how many built-ins (`auto dark light` at least,
`mono` for a terminal with no colour at all), and whether a theme may repaint the four code roles —
`README.md:181-183` says those four are ykode's own on purpose, so painting them means rewriting
those lines too. A user-supplied palette file is a second feature (colour parsing, a schema, a
screen to edit it, and a `format` story when its shape changes): not in this pass.

Tests pin the mechanism, not the taste: a named theme lands its pair on a role, `theme: marmalade`
is refused with the key named, and a live change repaints something the screen already shows. The
shades themselves — nine light shades, four diff grounds, four code roles — are judged by an eye in
a real terminal, so budget for that as separate work, not as a test.

## Steps, in the order that keeps the compiler useful

1. `go get charm.land/bubbletea/v2@v2.1.2 charm.land/bubbles/v2@v2.2.1 charm.land/lipgloss/v2@v2.0.5`.
2. Swap the three import paths across `internal/tui` (59 files) — nothing else imports them.
3. The 27 production fixes above, `go build ./internal/tui` as the loop.
4. Decide the keyboard-protocol belt: `tui.go:178 mouseOff` is the existing "leave the terminal
   clean" guard. v2 pops on a normal exit, panic and ctx cancel, but a killed program leaves
   `>1u`/modifyOtherKeys on, which changes how the shell reads keys. Mirroring the mouse belt (pop
   kitty, reset modifyOtherKeys) is two more sequences and belongs in the same helper.
5. `go vet ./internal/tui`, then the mechanical test key table above.
6. Choose the view-assertion approach and run `go test ./internal/tui -count=1` to the end; this is
   where the days go.
7. Themes, while the palette is still in hand: `theme.go` (the table and `applyTheme`), the
   `theme` knob with its preset, the four `setup.go` colours becoming roles, `diffSides` losing its
   cache, `applyLive` taking the change, and the mechanism tests above.
8. Add `ctrl+enter` and `shift+enter` to `InsertNewline.SetKeys`, cover them by feeding raw
   `\x1b[13;5u` and `\x1b[13;2u` through the fake terminal, as the pointer tests already feed escape
   bytes, and prove a plain `\r` still sends.
9. Docs: README's shortcut row (`README.md:117`) loses the "most terminals cannot tell ctrl+enter
   from enter" sentence and names the terminals that can; README's colours section (145, and 181-183
   if the code roles become themeable) says what `theme` changes; AGENTS.md's dependency line, its
   terminal-hygiene invariant and its single-owner rules gain the keyboard protocol and the
   styles-rebuilt-on-the-screen-goroutine rule.
10. `make check`, then `make release`'s three targets — v2 pulls a Windows file, and the repo is
    unix-only by rule, so confirm nothing about the build tags changes.

## Effort and risk

Production code is a half day: 27 fixes, all known. Themes add about a day of wiring — settings, the
screen, the mechanism tests — on top. Tests are the bulk, one to two days with the gate as the loop,
and the view-assertion decision is the part that can go wrong quietly. What cannot be verified from
here is the actual terminal: cursor position, alt-screen restore, the keyboard pop on exit, whether a
pasted newline survives, and every shade of every theme. A real-terminal pass over the README's
shortcut list and over each theme belongs at the end, not as an assumption.

## Until then

`ctrl+j` is a newline everywhere, forever — `\n` cannot be confused with anything. `alt+enter` is
bound today and a terminal can be told to send `ctrl+enter` as it: in Ghostty,
`keybind = ctrl+enter=text:\x1b\r` in `~/.config/ghostty/config`, checked with
`ghostty +validate-config`. That is the whole reason this plan has no urgency beyond v1 being dead.
