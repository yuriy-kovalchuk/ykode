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
	callSeq       int          // numbers the r1, r2... ids the summaries share
	prompt        string       // the system message, built once so the cached prefix never moves
	branch        string       // the branch the prompt was built on, which the screen can show at once
	task          string       // the prompt being answered, which compaction keeps word for word
	environment   string       // the date and git state the last ask carried, so an ask that changes nothing repeats nothing
	readFiles     []string     // files the folded turns read, oldest first
	modifiedFiles []string     // and the ones they wrote or edited
	compactedAt   int          // tool calls in the history when it was last folded; -1 = never
	perms         *permissions // what the user has already agreed to keep allowing
	sessionBroken atomic.Bool  // the session log failed once: say so once, not per message
	turn          int          // the round-trip currently running, carried on every event
	history       []llm.Message
	seenThrough   int // how far the history has reached the model: a result past it was never read
	meter         meter
	specBytes     int         // what the tool list costs every request, counted once
	savedRatio    float64     // the byte ratio the session log last heard of
	sincePlan     int         // tool calls since the model last touched its plan
	planReminded  bool        // the stale-plan line has been said for this plan already
	usageMu       sync.Mutex  // the numbers a front end reads while a turn runs elsewhere
	report        Usage       // what the conversation cost as of the last finished turn
	work          contextWork // what this ask had to do to fit the conversation in, for a front end to say so
	waitingMu     sync.Mutex  // asks typed while a turn ran, taken between round-trips
	waiting       []string    // what the user typed into a run that is still going
}

func New(client *llm.Client, registry *tools.Registry, model string, opts Options, sink Sink) *Agent {
	if sink == nil {
		sink = nullSink{}
	}
	if registry == nil {
		registry = tools.NewRegistry() // a run with no tools still answers questions about the names it met
	}
	a := &Agent{client: client, tools: registry, model: model, opts: opts, sink: sink}
	if _, canAnswer := sink.(Asker); canAnswer {
		// the question is answered by the same thing that draws the run, so the tool is offered only
		// when there is somebody at the other end of it to answer
		registry.Add(tools.NewAskUser(asksTo{a}))
	}
	a.meter = newMeter()
	a.publishUsage()
	a.perms = newPermissions(opts.Allow)
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
	a.startOver()
}

// startOver empties the conversation without writing anything to the log: a run coming back to another
// saved conversation leaves the one it was in standing as it was, so it can be come back to again.
func (a *Agent) startOver() {
	a.history = []llm.Message{{Role: "system", Content: a.prompt}}
	a.seenThrough = len(a.history) // only the prompt is there, and it is not sent as a turn
	a.meter.clearCost()
	a.compactedAt = -1
	a.readFiles, a.modifiedFiles = nil, nil
	a.environment = ""
	a.opts.Plan.Clear() // the steps belong to the conversation that went
	a.work.started()    // nothing has had to make room in the conversation that starts now
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

// Learned hands a run that is coming back what the earlier one had learned about its server: the
// same conversation is then measured the same way, and what fitted before is not trimmed now.
func (a *Agent) Learned(bytesPerTok float64) {
	if bytesPerTok >= minBytesPerTok && bytesPerTok <= maxBytesPerTok {
		a.meter.bytesPerTok, a.savedRatio = bytesPerTok, bytesPerTok
	}
}

// Resume continues where a saved conversation stopped.
func (a *Agent) Resume(messages []llm.Message) {
	a.history = append(a.history, messages...)
	a.seenThrough = len(a.history) // the run that wrote these sent them to its model
	// the new calls continue the saved numbering, or a fresh result would take an id that recall
	// already points at, and the store would answer with someone else's output
	a.callSeq = highestCallID(a.history)
	a.restoreFiles(a.history)
	a.restorePlan(a.history)
	a.meter.anchorTokens = 0 // the count the saved run had is not this run's
	a.environment = lastEnvironment(a.history)
	if hasRecap(a.history) {
		// the conversation was folded before it was saved: the next fold waits for new work to pile up,
		// as it would have in the run that folded it, instead of coming at the first sign of pressure
		a.compactedAt = min(callBlocks(a.history), compactKeep)
	}
	// the conversation is not trimmed here: nothing has been asked of the window yet, and this path skips
	// the shortening and folding a running conversation gets first. The first prepareTurn does all three,
	// and chat repairs what a server would not take.
	a.publishUsage()
}

// hasRecap says a conversation begins with what a fold left.
func hasRecap(history []llm.Message) bool {
	for _, m := range history {
		if _, ok := recapOf(m); ok {
			return true
		}
	}
	return false
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
	a.add(llm.Message{Role: "user", Content: NoteMark + text})
}

// NoteMark opens a message ykode wrote to the model in the user's place. Without it the model takes
// the note for something the user typed - and a model reading this very repository has answered one
// as "a string from your own codebase, a slip" and carried on as if nothing had been said.
const NoteMark = "[ykode, the program running this session, not the user] "
