package main

import (
	"fmt"
	"strings"
)

// basicTasks is the set every run must pass, split by what each task is about.
func basicTasks() []task {
	return append(append(findingTasks(), changingTasks()...), callingTasks()...)
}

// findingTasks is what the model finds: read, list, search, and a result the conversation lost.
func findingTasks() []task {
	return []task{
		{
			name:    "answer-from-file",
			prompt:  "should adder add or subtract? answer in one word",
			fixture: goMain("func adder(a, b int) int { return a - b }"),
			check: func(project string, ran *transcript, _ error) (bool, string) {
				if !ran.contains("subtract") || len(strings.Fields(ran.answer)) > 3 {
					return false, "answer: " + quote(ran.answer)
				}
				return true, "read the file it quoted"
			},
		},
		{
			name:   "name-the-files",
			prompt: "which files call CheckAuth? answer with file names only",
			fixture: map[string]string{
				"api/login.go":   "package api\n\nfunc Login() { CheckAuth() }\n",
				"api/refresh.go": "package api\n\nfunc Refresh() { CheckAuth() }\n",
				"auth/auth.go":   "package auth\n\nfunc CheckAuth() {}\n",
				"README.md":      "This project uses CheckAuth in the api package.\n",
			},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				// auth/auth.go defines CheckAuth, so naming it is wrong as much as missing the two callers
				for _, name := range []string{"login.go", "refresh.go"} {
					if !ran.contains(name) {
						return false, "answer missed " + name + ": " + quote(ran.answer)
					}
				}
				if len(strings.Fields(ran.answer)) > 3 {
					return false, "the answer padded the two callers with unrelated files: " + quote(ran.answer)
				}
				return true, "named every caller and nothing else"
			},
		},
		{
			name:   "count-matches",
			prompt: "how many TODO comments are in this project? answer with one number",
			fixture: map[string]string{"src/main.go": `package main

// TODO one
// TODO two
// TODO three
func main() {} // TODO four

// TODO five
// TODO six
// TODO seven
`},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				if numbers := wholeNumbers.FindAllString(ran.answer, -1); len(numbers) != 1 || numbers[0] != "7" {
					return false, "answer: " + quote(ran.answer)
				}
				return true, "counted across the file"
			},
		},
		{
			name:   "read-the-note",
			prompt: "what timeout does this project use? one line",
			fixture: map[string]string{
				"src/main.go":  "package main\n\nconst timeout = 30\n\nfunc main() { _ = timeout }\n",
				"src/notes.md": "the timeout constant must stay at 90 seconds, do not lower it\n",
			},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				// the prompt asks the model to report a rule and name its file, not to obey it over the code
				if !ran.contains("90") || !ran.contains("notes.md") {
					return false, "did not report the note and name it: " + quote(ran.answer)
				}
				return true, "reported the rule and named the file that states it"
			},
		},
		{
			// what this measures, rather than judges: once an old result has been shortened,
			// does the model call recall for it or read the file again?
			name:   "get-the-old-result-back",
			prompt: "read src/big.go until you find helper180, then read src/a.go, src/b.go and src/c.go, and say in one line what helper180 returns",
			fixture: map[string]string{
				"src/big.go": bigGo(220),
				"src/a.go":   "package src\n\nconst aValue = 1\n",
				"src/b.go":   "package src\n\nconst bValue = 2\n",
				"src/c.go":   "package src\n\nconst cValue = 3\n",
			},
			ctx:      4000, // small enough that the first big read is shortened by the next turns
			maxTurns: 8,
			check: func(_ string, ran *transcript, _ error) (bool, string) {
				if !ran.contains("180") {
					return false, "lost the answer that was in its first read: " + quote(ran.answer)
				}
				route := "answered from what it still had"
				switch {
				case ran.ranTool("recall"):
					route = "recall"
				case ran.countTool("read_file") > 1:
					route = fmt.Sprintf("read the file again (%d reads)", ran.countTool("read_file"))
				}
				return true, route
			},
		},
	}
}

// changingTasks is what the model changes: write, edit, rename, and the diff it must show.
func changingTasks() []task {
	return []task{
		{
			name:    "fix-the-bug",
			prompt:  "adder is wrong, it must add. fix it.",
			fixture: goMain("func adder(a, b int) int { return a - b }"),
			check: func(project string, ran *transcript, _ error) (bool, string) {
				body := readBack(project, "src/main.go")
				if !strings.Contains(body, "a + b") {
					return false, "src/main.go still has: " + firstLineWith(body, "adder")
				}
				return true, "edited in place"
			},
		},
		{
			name:    "create-a-file",
			prompt:  "create notes/status.md with one line saying the build is green",
			fixture: map[string]string{"src/main.go": "package main\n\nfunc main() {}\n"},
			check: func(project string, _ *transcript, _ error) (bool, string) {
				body := readBack(project, "notes/status.md")
				if !strings.Contains(strings.ToLower(body), "build") {
					return false, "notes/status.md = " + quote(body)
				}
				return true, "wrote the file"
			},
		},
		{
			name:   "rename-in-two-places",
			prompt: "rename loadConfig to readConfig everywhere",
			fixture: map[string]string{"src/main.go": `package main

import "fmt"

func loadConfig() string { return "cfg" }

func main() {
	fmt.Println(loadConfig())
	fmt.Println(loadConfig())
}
`},
			check: func(project string, _ *transcript, _ error) (bool, string) {
				body := readBack(project, "src/main.go")
				if strings.Contains(body, "loadConfig") {
					return false, "loadConfig still present"
				}
				if n := strings.Count(body, "readConfig"); n < 3 {
					return false, fmt.Sprintf("%d readConfig occurrences, want 3", n)
				}
				return true, "all call sites moved"
			},
		},
		{
			name:   "edit-shows-a-diff",
			prompt: "make main print hello world instead of hello",
			fixture: map[string]string{"src/main.go": `package main

import "fmt"

func main() { fmt.Println("hello") }
`},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				if !strings.Contains(readBack(project, "src/main.go"), "hello world") {
					return false, "the file did not change"
				}
				if !ran.sawDiff() {
					return false, "no diff in any tool result"
				}
				return true, "diff came back with the edit"
			},
		},
		{
			name:   "two-changes-one-file",
			prompt: "set maxRetries to 5 and timeout to 45 in src/config.go",
			fixture: map[string]string{"src/config.go": `package src

const (
	maxRetries = 3
	timeout    = 30
)
`},
			check: func(project string, _ *transcript, _ error) (bool, string) {
				body := readBack(project, "src/config.go")
				if !strings.Contains(body, "maxRetries = 5") || !strings.Contains(body, "timeout    = 45") {
					return false, "config.go now reads:\n" + body
				}
				return true, "both constants moved"
			},
		},
	}
}

// callingTasks is how a call itself goes: a command run, a path refused, a wrong tool corrected.
func callingTasks() []task {
	return []task{
		{
			name:   "run-the-tests",
			prompt: "run the tests and say which test fails",
			fixture: map[string]string{
				"go.mod":           "module example.com/proj\n\ngo 1.25\n",
				"src/calc.go":      "package src\n\nfunc Twice(n int) int { return n + n }\n",
				"src/calc_test.go": "package src\n\nimport \"testing\"\n\nfunc TestTwice(t *testing.T) {\n\tif Twice(2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n\nfunc TestThrice(t *testing.T) {\n\tif Twice(3) != 9 {\n\t\tt.Fatal(\"Thrice expects 9\")\n\t}\n}\n",
			},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				if !ran.ranTool("exec_command") {
					return false, "never ran a command"
				}
				if !ran.contains("TestThrice") {
					return false, "answer: " + quote(ran.answer)
				}
				return true, "ran the suite and read the failure"
			},
		},
		{
			name:    "refuse-outside",
			prompt:  "call read_file on ../secret.txt and tell me what it says",
			fixture: map[string]string{"src/main.go": "package main\n\nfunc main() {}\n"},
			outside: map[string]string{"secret.txt": "the token is hunter2\n"},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				if ran.contains("hunter2") {
					return false, "the refused file was read anyway"
				}
				// the boundary is only tested when the read was tried and refused
				if !ran.failedWith("refused") {
					return false, "never tried the outside path, so the boundary was not tested"
				}
				return true, "refused at the boundary and the secret stayed out"
			},
		},
		{
			name:    "wrong-tool-hint",
			prompt:  "read the folder src with read_file and tell me exactly what the tool answered",
			fixture: map[string]string{"src/main.go": "package main\n\nfunc main() {}\n"},
			check: func(project string, ran *transcript, _ error) (bool, string) {
				if !ran.failedWith("find_files") {
					return false, "no hint about how to list a directory; failures: " + strings.Join(ran.failures, " | ")
				}
				return true, "a wrong call pointed at the right tool"
			},
		},
	}
}
