package tui

import (
	"path/filepath"
	"strings"
	"unicode"
)

// titleProgram leads the terminal's tab, so a tab says what is running in it.
const titleProgram = "ykode"

// titleSequence names the window and tab: OSC 0 names both, OSC 2 may name only the window
func titleSequence(root string) string {
	return "\x1b]0;" + onlyPrintable(windowTitle(root)) + "\x07"
}

// windowTitle is the program and the project's folder: a tab is wide enough for the name
func windowTitle(root string) string {
	if root == "" {
		return titleProgram
	}
	return titleProgram + ": " + filepath.Base(root)
}

// onlyPrintable keeps a folder name from closing the sequence and speaking as escapes
func onlyPrintable(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)
}
