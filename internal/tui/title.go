package tui

import (
	"path/filepath"
	"strings"
	"unicode"
)

// titleProgram leads the terminal's tab, so a tab says what is running in it.
const titleProgram = "ykode"

// titleSequence names the terminal's window and its tab. OSC 0 names both: OSC 2, which a terminal
// is free to show only in the window, would leave the tab alone in the terminals that name a tab by
// its icon name.
func titleSequence(root string) string {
	return "\x1b]0;" + onlyPrintable(windowTitle(root)) + "\x07"
}

// windowTitle is the program and the folder the project starts in. The folder is named rather than
// spelled out as a path, because a tab is a handful of characters wide and everything above the name
// is the part the reader already chose.
func windowTitle(root string) string {
	if root == "" {
		return titleProgram
	}
	return titleProgram + ": " + filepath.Base(root)
}

// onlyPrintable keeps a folder name that was built to attack the terminal from ending the title early:
// a BEL or an ESC in it would close the sequence and the rest of the name would arrive as escape
// sequences of its own.
func onlyPrintable(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)
}
