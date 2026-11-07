package audit

import (
	"strings"
	"testing"
	"time"
)

func TestTheHistoryTableSkipsTheStartedLines(t *testing.T) {
	code := 2
	entries := []Entry{
		{Time: time.Unix(0, 0), ID: "r1", Tool: "read_file", Status: "started", Paths: []string{"/p/src/a.go"}},
		{Time: time.Unix(0, 0), ID: "r1", Tool: "read_file", Status: "ok", Paths: []string{"/p/src/a.go"}},
		{Time: time.Unix(1, 0), ID: "r2", Tool: "exec_command", Status: "error", ExitCode: &code, Paths: []string{"/p"}},
		{Time: time.Unix(2, 0), ID: "r3", Tool: "write_file", Status: "refused", Paths: []string{"/outside/x"}},
	}
	table := Format(entries, "", 0)
	if strings.Count(table, "\n") != 3 {
		t.Fatalf("want one line per call, got:\n%s", table)
	}
	if !strings.Contains(table, "a.go") || strings.Contains(table, "/p/src/a.go") {
		t.Errorf("paths should show as names:\n%s", table)
	}
	if !strings.Contains(table, "exit=2") {
		t.Errorf("the exit code is missing:\n%s", table)
	}

	if only := Format(entries, "failed", 0); strings.Count(only, "\n") != 2 {
		t.Errorf("failed filter:\n%s", only)
	}
	if only := Format(entries, "exec", 0); only == "" || !strings.Contains(only, "exec_command") || strings.Contains(only, "read_file") {
		t.Errorf("tool filter:\n%s", only)
	}
	if only := Format(entries, "", 1); strings.Count(only, "\n") != 1 || !strings.Contains(only, "r3") {
		t.Errorf("the limit should keep the newest:\n%s", only)
	}
}

func TestHistoryCanAskForEveryRunOfOneRoot(t *testing.T) {
	logger := New(t.TempDir(), "s1")
	for _, root := range []string{"/repo/a", "/repo/b"} {
		if err := logger.Append(Entry{ID: "r1", Tool: "read_file", Status: "ok", Root: root}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _, err := logger.ReadRoot("/repo/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Root != "/repo/a" {
		t.Fatalf("wanted only /repo/a, got %v", entries)
	}
}

func TestAFailedFilterAcceptsTheDashedForm(t *testing.T) {
	entries := []Entry{
		{ID: "r1", Tool: "exec_command", Status: "ok", Time: time.Now()},
		{ID: "r2", Tool: "edit_file", Status: "refused", Time: time.Now()},
	}
	for _, filter := range []string{"failed", "--failed"} {
		table := Format(entries, filter, 0)
		if !strings.Contains(table, "r2") || strings.Contains(table, "r1") {
			t.Errorf("%s: %q", filter, table)
		}
	}
}

func TestOneCallInFullAnswersTheID(t *testing.T) {
	exit := 2
	e := Entry{
		ID: "r9", Time: time.Now(), Tool: "exec_command", Args: `{"command":"go test"}`,
		Status: "timeout", Decision: "user_yes", ExitCode: &exit, DurationMS: 60000,
		Paths: []string{"/repo/a"}, OutputPath: "/home/.ykode/sessions/s1/results/r9.txt", ResultBytes: 8200,
	}
	detail := e.Detail()
	for _, want := range []string{"r9", "exec_command", "go test", "user_yes", "timeout", "exit 2", "r9.txt", "8200"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail is missing %q:\n%s", want, detail)
		}
	}
}

func TestAskingForAnIDAnswersOneCallOrSaysThereIsNone(t *testing.T) {
	entries := []Entry{
		{ID: "r1", Tool: "read_file", Status: "ok", Args: `{"path":"main.go"}`, Time: time.Now()},
		{ID: "r2", Tool: "exec_command", Status: "error", Args: `{"command":"go test"}`, Time: time.Now()},
	}
	if got := Answer(entries, "r2", 0, 0); !strings.Contains(got, "go test") || strings.Contains(got, "main.go") {
		t.Errorf("r2: %s", got)
	}
	if got := Answer(entries, "r7", 0, 0); !strings.Contains(got, "no call r7") {
		t.Errorf("r7: %s", got)
	}
	if got := Answer(entries, "exec", 0, 0); !strings.Contains(got, "r2") || strings.Contains(got, "r1") {
		t.Errorf("exec: %s", got)
	}
	if got := Answer(entries, "write_file", 0, 0); !strings.Contains(got, "nothing matches") {
		t.Errorf("an empty table should say so: %s", got)
	}
}
