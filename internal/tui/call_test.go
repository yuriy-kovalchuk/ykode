package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

// a read says what it read, on one row: the file it opened is not printed into the session
func TestAReadShowsItsRowAndNotTheFile(t *testing.T) {
	var log transcript
	log.root = "/work/playground"
	args := `{"path":"/work/playground/toy_llm/train.py"}`
	body := "toy_llm/train.py: lines 1-200 of 200\n" + strings.Repeat("a line of the file\n", 200)
	log.toolStart("read_file", args)
	log.toolEnd("read_file", args, body)

	rows := log.rows(80)
	if len(rows) != 1 {
		t.Fatalf("the call took %d rows, want one:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	row := stripCodes(rows[0])
	if !strings.Contains(row, "toy_llm/train.py") {
		t.Errorf("the row does not name the file it read: %q", row)
	}
	if strings.Contains(row, "/work/playground") {
		t.Errorf("the row names the whole disk, not the project: %q", row)
	}
	if strings.Contains(row, "a line of the file") {
		t.Errorf("the row holds the file's text: %q", row)
	}
}

// a finished call keeps no body: the row holds a sentence, the rest goes back where it came from
func TestACallDoesNotKeepWhatItRead(t *testing.T) {
	var log transcript
	log.root = "/work"
	args := `{"path":"/work/big.go"}`
	body := "read big.go: lines 1-9000 of 9000\n" + strings.Repeat("x", 300<<10)
	log.toolStart("read_file", args)
	log.toolEnd("read_file", args, body)

	held := heldBytes(log.entries[0])
	if held > 1<<10 {
		t.Errorf("a finished call holds %d bytes of a 300 KB answer", held)
	}
}

// a change keeps the rows the screen shows, counts what it left out, and is drawn under the row
func TestAChangeIsRowsUnderTheRow(t *testing.T) {
	var log transcript
	log.root = "/work"
	args := `{"path":"/work/a.go","edits":[{"old_text":"one","new_text":"two"}]}`
	result := "edited a.go (12 -> 12 bytes, 1 replacement)\n@@ -1,3 +1,3 @@\n package main\n-one\n+two\n var x int\n"
	log.toolStart("edit_file", args)
	log.toolEnd("edit_file", args, result)

	view := strings.Join(log.rows(80), "\n")
	if !strings.Contains(stripCodes(view), "+ two") {
		t.Errorf("the change is not drawn under the row:\n%s", view)
	}
	if !strings.Contains(stripCodes(view), "2 - one") || !strings.Contains(stripCodes(view), "3   var x int") {
		t.Errorf("the change does not say where in the file it happened:\n%s", view)
	}
	if !strings.Contains(stripCodes(view), "· +1 -1") {
		t.Errorf("the row does not count the change:\n%s", view)
	}
	if held := heldBytes(log.entries[0]); held > 8<<10 {
		t.Errorf("a finished call holds %d bytes", held)
	}
}

// the screen draws the whole change a call handed over, not the short copy the model was sent
func TestTheWholeChangeIsDrawnWhenTheToolHandsItOver(t *testing.T) {
	var log transcript
	log.root = "/work"
	args := `{"path":"/work/big.go","content":"a whole file"}`
	var whole strings.Builder
	whole.WriteString("@@ -0,0 +1,400 @@\n")
	for i := 0; i < 400; i++ {
		whole.WriteString("+line of the new file " + itoa(i) + "\n")
	}
	log.toolStart("write_file", args)
	log.changed("write_file", args, "created big.go (9000 bytes, 400 lines)", whole.String())

	rows := log.rows(120)
	if len(rows) != 401 {
		t.Fatalf("a change of 400 lines took %d rows, want its row and 400", len(rows))
	}
	if head := stripCodes(rows[0]); !strings.Contains(head, "created big.go") || !strings.Contains(head, "+400") {
		t.Errorf("the row does not say what was made and how much: %q", head)
	}
	if last := stripCodes(rows[400]); !strings.Contains(last, "400 + line of the new file 399") {
		t.Errorf("the last line of the change is not the last row: %q", last)
	}
}

// a non-zero command says its exit and shows the last lines, which is where the error is
func TestACommandThatFailedSaysWhy(t *testing.T) {
	var log transcript
	log.root = "/work"
	args := `{"command":"python3 toy_llm/train.py"}`
	result := "Traceback (most recent call last):\n  File \"toy_llm/train.py\", line 8, in <module>\n    main()\nTypeError: 'NoneType' is not iterable\n[exit status 1]"
	log.toolStart("exec_command", args)
	log.toolEnd("exec_command", args, result)

	view := strings.Join(log.rows(100), "\n")
	if !strings.Contains(view, "exit 1") {
		t.Errorf("the row does not say the command failed: %q", view)
	}
	if !strings.Contains(view, "TypeError") {
		t.Errorf("the line that explains the failure is not on the screen: %q", view)
	}
	if !strings.Contains(view, "python3 toy_llm/train.py") {
		t.Errorf("the row lost the command it ran: %q", view)
	}
}

// a call whose arguments could not be read still says what it did, in the tool's own words
func TestACallWithUnreadableArgumentsStillReadsWell(t *testing.T) {
	var log transcript
	log.root = "/work"
	args := `{"path": "/work/a.go", "edits": [{"old_text": "one` + "\n" + `", "new_text": "two"}]}`
	log.toolStart("edit_file", args)
	log.toolEnd("edit_file", args, "edited a.go (10 -> 10 bytes, 1 replacement)")
	view := stripCodes(strings.Join(log.rows(100), "\n"))
	if strings.Contains(view, "{\"path\"") {
		t.Errorf("the row writes the arguments as JSON: %q", view)
	}
	if !strings.Contains(view, "edited a.go") {
		t.Errorf("the row lost what the call did: %q", view)
	}
}

// heldBytes is what a row still costs the run once its call is done.
func heldBytes(e entry) int { return len(e.text) + len(e.args) + heldCall(e.call) }

// itoa is the count the tests write with
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// a long command is cut to its row, and ctrl+r shows it whole under the row, then folds it back
func TestALongCommandIsShownWholeOnlyWhenAsked(t *testing.T) {
	var log transcript
	log.root = "/work"
	command := "go test ./internal/agent/ -count=1 -run 'TheFoldAsks|Refused|Ladder' -v " + strings.Repeat("-args x ", 20) + "&& echo tail-end"
	args, _ := json.Marshal(map[string]string{"command": command})
	log.toolStart("exec_command", string(args))
	log.toolEnd("exec_command", string(args), "ok\n[exit status 0]")

	if view := stripCodes(strings.Join(log.rows(100), "\n")); strings.Contains(view, "tail-end") {
		t.Fatalf("the command is whole before anyone asked:\n%s", view)
	}
	log.showWhole(true)
	view := stripCodes(strings.Join(log.rows(100), "\n"))
	if !strings.Contains(view, "tail-end") || !strings.Contains(view, "-run 'TheFoldAsks|Refused|Ladder'") {
		t.Errorf("the whole command is not under its row:\n%s", view)
	}
	log.showWhole(false)
	if view := stripCodes(strings.Join(log.rows(100), "\n")); strings.Contains(view, "tail-end") {
		t.Errorf("folding back left the command whole:\n%s", view)
	}
}

// a command the row already shows whole is not repeated, and a multi-line one keeps its line breaks
func TestAWholeCommandRepeatsNothingAndKeepsItsLines(t *testing.T) {
	var log transcript
	log.toolStart("exec_command", `{"command":"go vet ./..."}`)
	log.toolEnd("exec_command", `{"command":"go vet ./..."}`, "exit 0, no output")
	script := `{"command":"cat <<'EOF' > notes.txt\nfirst line\nsecond line\nEOF"}`
	log.toolStart("exec_command", script)
	log.toolEnd("exec_command", script, "exit 0, no output")
	log.showWhole(true)

	view := stripCodes(strings.Join(log.rows(100), "\n"))
	if strings.Count(view, "go vet ./...") != 1 {
		t.Errorf("a command that fits its row was shown twice:\n%s", view)
	}
	for _, line := range []string{"│ cat <<'EOF' > notes.txt", "│ first line", "│ second line", "│ EOF"} {
		if !strings.Contains(view, line) {
			t.Errorf("the heredoc lost the line %q:\n%s", line, view)
		}
	}
}
