// Package agent runs the loop between the model and the tools until the model answers in text.
package agent

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// finishLength is the finish reason a server sends when it stopped at max_tokens.
const finishLength = "length"

type Agent struct {
	client        *llm.Client
	tools         *tools.Registry
	model         string
	opts          Options
	sink          Sink
	callSeq       int          // numbers the r1, r2... ids that the summaries and the audit log share
	prompt        string       // the system message, built once so the cached prefix never moves
	branch        string       // the branch the prompt was built on, which the screen can show at once
	task          string       // the prompt being answered, which compaction keeps word for word
	environment   string       // the date and git state the last ask carried, so an ask that changes nothing repeats nothing
	fold          foldNote     // what the fold in progress did, until the log has it
	readFiles     []string     // files the folded turns read, oldest first
	modifiedFiles []string     // and the ones they wrote or edited
	compactedAt   int          // tool calls in the history when it was last folded; -1 = never
	auditBroken   atomic.Bool  // the audit log failed once: say so once, not per call
	perms         *permissions // what the user has already agreed to keep allowing
	repeats       *loopGuard   // calls this run has already answered, so a repeat costs one line
	sessionBroken atomic.Bool  // the session log failed once: say so once, not per message
	turn          int          // the round-trip currently running, carried on every event
	history       []llm.Message
	meter         meter
	usageMu       sync.Mutex // the numbers a front end reads while a turn runs elsewhere
	report        Usage      // what the conversation cost as of the last finished turn
	waitingMu     sync.Mutex // asks typed while a turn ran, taken between round-trips
	waiting       []string   // what the user typed into a run that is still going
}

func New(client *llm.Client, registry *tools.Registry, model string, opts Options, sink Sink) *Agent {
	if sink == nil {
		sink = nullSink{}
	}
	a := &Agent{client: client, tools: registry, model: model, opts: opts, sink: sink}
	a.meter = newMeter()
	a.publishUsage()
	a.perms = newPermissions(opts.Allow)
	a.repeats = newLoopGuard()
	a.prompt, a.branch = withInstructions(withEnvironment(SystemPrompt, opts.Root), opts.Instructions), GitBranch(opts.Root)
	a.Reset()
	return a
}

// Reset keeps the system prompt and forgets the rest of the conversation; the learned byte ratio stays.
func (a *Agent) Reset() {
	if len(a.history) > 1 {
		// the saved conversation starts over too, or --continue hands back what was just cleared
		a.reportSave(a.opts.Session.Cleared())
	}
	a.history = []llm.Message{{Role: "system", Content: a.prompt}}
	a.meter.clearCost()
	a.compactedAt = -1
	a.readFiles, a.modifiedFiles = nil, nil
	a.environment = ""
	a.opts.Plan.Clear() // the steps belong to the conversation that went
	a.repeats.changed() // nothing the model saw before is in front of it any more
	a.forgetAll()       // the tools forget what they handed over too: there is nothing in front of the model
	a.dropWaiting()     // an ask typed into the old conversation does not steer the new one
	a.publishUsage()
}

// reportSave passes on the one thing a session log can answer with, and says it only once per run:
// a log that will not write is a fact about the run, not about one call.
func (a *Agent) reportSave(err error) {
	if err == nil || a.sessionBroken.Swap(true) {
		return
	}
	a.tell(err.Error())
}

func (a *Agent) add(m llm.Message) {
	m = storable(m)
	a.history = append(a.history, m)
	a.reportSave(a.opts.Session.Append(m))
}

// Resume continues where a saved conversation stopped.
func (a *Agent) Resume(messages []llm.Message) {
	a.history = append(a.history, messages...)
	// the new calls continue the saved numbering, or a fresh result would take an id that recall
	// already points at, and the store would answer with someone else's output
	a.callSeq = highestCallID(a.history)
	a.restoreFiles(a.history)
	a.restorePlan(a.history)
	a.environment = lastEnvironment(a.history)
	a.sanitizeHistory()
	a.trimHistory()
	a.publishUsage()
}

// highestCallID is the largest rN a conversation already holds; 0 when it holds none.
func highestCallID(messages []llm.Message) int {
	highest := 0
	for _, m := range messages {
		if !strings.HasPrefix(m.CallID, "r") {
			continue
		}
		if n, err := strconv.Atoi(m.CallID[1:]); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

func (a *Agent) tell(text string) { a.emit(Event{Kind: Notice, Turn: a.turn, Text: text}) }

func (a *Agent) emit(ev Event) Decision {
	if ev.Turn == 0 {
		ev.Turn = a.turn
	}
	return a.sink.Emit(ev)
}

// note keeps the history honest when a turn ends without an answer.
func (a *Agent) note(text string) {
	a.add(llm.Message{Role: "assistant", Content: text})
}

// modelNote answers the model in the user's voice: a request whose last line is an assistant
// message is continued by the server as prefill instead of answered.
func (a *Agent) modelNote(text string) {
	a.add(llm.Message{Role: "user", Content: text})
}
