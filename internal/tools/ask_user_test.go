package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// askedRecords what a question looked like when it reached the front end
type askedRecords struct {
	questions []Question
	answer    string
	waiting   bool // the call's context was still alive when the question arrived
}

func (a *askedRecords) Ask(ctx context.Context, q Question) string {
	a.questions = append(a.questions, q)
	a.waiting = ctx.Err() == nil
	return a.answer
}

func TestTheQuestionAndItsChoicesReachTheFrontEnd(t *testing.T) {
	front := &askedRecords{answer: "the stat poll"}
	got, err := NewAskUser(front).Run(context.Background(), []byte(`{"question":"how should the footer follow the branch?","options":["stat HEAD each 5s","a file watcher","leave it"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(front.questions) != 1 {
		t.Fatalf("the front end was asked %d times", len(front.questions))
	}
	asked := front.questions[0]
	if asked.Text != "how should the footer follow the branch?" {
		t.Errorf("what it was asked: %q", asked.Text)
	}
	if len(asked.Options) != 3 || asked.Options[1] != "a file watcher" {
		t.Errorf("the choices: %q", asked.Options)
	}
	if !front.waiting {
		t.Error("the call's own context did not reach the front end")
	}
	if !strings.Contains(got, "the stat poll") {
		t.Errorf("the model got %q", got)
	}
}

// a question the model puts no choices on is still a question
func TestAQuestionWithoutChoices(t *testing.T) {
	front := &askedRecords{answer: "src/main.go"}
	got, err := NewAskUser(front).Run(context.Background(), []byte(`{"question":"which file first?"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(front.questions[0].Options) != 0 || !strings.Contains(got, "src/main.go") {
		t.Errorf("asked %+v, model got %q", front.questions[0], got)
	}
}

// nobody answered: the run was cancelled, or nobody is watching
func TestNoAnswerIsSaidPlainlyToTheModel(t *testing.T) {
	got, err := NewAskUser(&askedRecords{}).Run(context.Background(), []byte(`{"question":"which of the two?"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, noAnswer) {
		t.Errorf("the model was told: %q", got)
	}
}

func TestAQuestionWithNothingInItIsRefused(t *testing.T) {
	front := &askedRecords{answer: "x"}
	for _, args := range []string{`{}`, `{"question":"   "}`} {
		if _, err := NewAskUser(front).Run(context.Background(), []byte(args)); err == nil {
			t.Errorf("%s was accepted", args)
		}
	}
	if len(front.questions) != 0 {
		t.Error("an empty question still reached the front end")
	}
}

// more choices than a screen holds are cut, not dropped whole: the question is still answerable
func TestTooManyChoicesAreCutToWhatFits(t *testing.T) {
	front := &askedRecords{answer: "the first"}
	args := `{"question":"which?","options":["a","b","c","d","e","f","g","h"]}`
	if _, err := NewAskUser(front).Run(context.Background(), []byte(args)); err != nil {
		t.Fatal(err)
	}
	if len(front.questions[0].Options) != maxAskOptions {
		t.Errorf("%d choices reached the screen, want %d", len(front.questions[0].Options), maxAskOptions)
	}
}

// a cancelled call lets go of the question instead of hanging the run
func TestACancelledCallStopsWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	front := &waitsUntilGone{}
	cancel()
	_, err := NewAskUser(front).Run(ctx, []byte(`{"question":"still there?"}`))
	if err != nil {
		t.Errorf("a cancelled call failed instead of reporting no answer: %v", err)
	}
	if !front.gaveUp {
		t.Error("the question was never released")
	}
}

type waitsUntilGone struct{ gaveUp bool }

// a front end that waits on the context, the way both of them do
func (f *waitsUntilGone) Ask(ctx context.Context, q Question) string {
	<-ctx.Done()
	f.gaveUp = true
	return ""
}

// the row on the screen names the question, not the raw arguments
func TestTheCallIsDescribedByItsQuestion(t *testing.T) {
	line := Describe("ask_user", `{"question":"which of these two?","options":["one","two"]}`)
	if !strings.Contains(line, "which of these two?") {
		t.Errorf("described as %q", line)
	}
}

// ask_user joins the list only when something can answer it
func TestTheQuestionIsOnlyListedWhereItCanBeAnswered(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	has := func(registry *Registry, name string) bool {
		for _, tool := range registry.All() {
			if tool.Name() == name {
				return true
			}
		}
		return false
	}
	registry := BuiltIn(root, dir, Options{})
	if has(registry, "ask_user") {
		t.Error("a plain registry lists a question nobody can answer")
	}
	registry.Add(NewAskUser(&askedRecords{}))
	if !has(registry, "ask_user") {
		t.Error("a registry with a front end does not list the question")
	}
}

// askedArgs builds one call's arguments, so the text under test can hold quotes and newlines
func askedArgs(t *testing.T, question string, options []string) []byte {
	t.Helper()
	raw, err := json.Marshal(askInput{Question: question, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// what a person reads has to be all of the question: the end of a sentence often decides the answer
func TestALongQuestionReachesTheScreenWhole(t *testing.T) {
	long := "the settings screen can stay one column that scrolls, or split into a column of keys and a " +
		"column of values, and the help for a key could sit under the key you have selected or in the " +
		"row at the bottom where the model and the context already live; the split costs a row of the " +
		"transcript but keeps every key readable without scrolling, and moving the help to the bottom " +
		"keeps the columns steady while you move between sections but puts it further from what it " +
		"describes: of those two changes, or both, or neither, which do you want?"
	front := &askedRecords{answer: "the split"}
	if _, err := NewAskUser(front).Run(context.Background(), askedArgs(t, long, nil)); err != nil {
		t.Fatal(err)
	}
	if got := front.questions[0].Text; got != long {
		t.Errorf("the screen was shown %d of %d characters of the question:\n%s", len(got), len(long), got)
	}
}

// a choice is what the model would act on, so a fragment of one is not enough to answer from
func TestALongChoiceReachesTheScreenWhole(t *testing.T) {
	choice := "keep the stat poll every five seconds, since it costs nothing on a local branch and a " +
		"file watcher would need a second goroutine and a debounce to say the same thing"
	front := &askedRecords{answer: "the first"}
	if _, err := NewAskUser(front).Run(context.Background(), askedArgs(t, "which way?", []string{choice})); err != nil {
		t.Fatal(err)
	}
	if got := front.questions[0].Options; len(got) != 1 || got[0] != choice {
		t.Errorf("the choices: %q", got)
	}
}

// what a person types back is theirs: the model reads all of it, or the run goes on having half asked
func TestALongAnswerReachesTheModelWhole(t *testing.T) {
	answer := "use the split, but keep the keys in the order the settings file lists them rather than " +
		"alphabetically, and leave the help line where it is now because moving it under the selected " +
		"column makes it jump as you move; one more thing - the settings screen should say which keys " +
		"come from the project file rather than from the user's own file, since that is the one that " +
		"surprises people when a value will not change no matter how many times they set it. One more " +
		"thing while I have you: the transcript should not repeat the same tool result twice in one run, " +
		"since that is the second thing people notice about the screen"
	front := &askedRecords{answer: answer}
	got, err := NewAskUser(front).Run(context.Background(), askedArgs(t, "which layout?", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, answer) {
		t.Errorf("the model was given %q", got)
	}
}
