package main

import (
	"fmt"
	"strings"
)

// pressureTasks run in a small window, so shortening, trimming and compaction actually happen.
func pressureTasks() []task {
	return []task{
		{
			// a re-read of a window that was shortened must bring the text back, not "already shown"
			name:   "reread-after-shortening",
			prompt: "read lines 1-60 of src/big.go, then read src/a.go, src/b.go and src/c.go, then say in one line what helper40 returns",
			fixture: map[string]string{
				"src/big.go": bigGo(220),
				"src/a.go":   "package src\n\nconst aValue = 1\n",
				"src/b.go":   "package src\n\nconst bValue = 2\n",
				"src/c.go":   "package src\n\nconst cValue = 3\n",
			},
			ctx:      4000,
			maxTurns: 8,
			check: func(_ string, ran *transcript, _ error) (bool, string) {
				if !wholeNumberIn(ran.answer, "40") {
					return false, "lost what its first read held: " + quote(ran.answer)
				}
				return true, fmt.Sprintf("%d reads of big.go", ran.countTool("read_file"))
			},
		},
		{
			// compaction folds the turns that carried the request: its exact words must survive
			name:     "exact-words-after-compaction",
			prompt:   "read src/file0.txt to src/file7.txt one file per step, never two in the same step, then create done.txt whose only line is exactly: KEEP-these-WORDS-7",
			fixture:  manyFiles(8, 40),
			ctx:      4000,
			maxTurns: 14,
			check: func(project string, _ *transcript, _ error) (bool, string) {
				if got := strings.TrimSpace(readBack(project, "done.txt")); got != "KEEP-these-WORDS-7" {
					return false, "done.txt = " + quote(got)
				}
				return true, "the exact wording survived"
			},
		},
	}
}

// wholeNumberIn says the answer names this number as a number of its own.
func wholeNumberIn(answer, number string) bool {
	for _, n := range wholeNumbers.FindAllString(answer, -1) {
		if n == number {
			return true
		}
	}
	return false
}

// manyFiles is src/file0.txt... with enough lines each that reading them all fills a small window.
func manyFiles(files, lines int) map[string]string {
	fixture := map[string]string{}
	for f := range files {
		var b strings.Builder
		for l := 1; l <= lines; l++ {
			fmt.Fprintf(&b, "file %d line %d carries enough text to cost something in the window\n", f, l)
		}
		fixture[fmt.Sprintf("src/file%d.txt", f)] = b.String()
	}
	return fixture
}
