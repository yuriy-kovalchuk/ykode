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
	client          *llm.Client
	tools           *tools.Registry
	model           string
	opts            Options
	sink            Sink
	callSeq         int          // numbers the r1, r2... ids the summaries share
	prompt          string       // the system message, built once so the cached prefix never moves
	branch          string       // the branch the prompt was built on, which the screen can show at once
	task            string       // the prompt being answered, which compaction keeps word for word
	environment     string       // the date and git state the last ask carried, so an ask that changes nothing repeats nothing
	readFiles       []string     // files the folded turns read, oldest first
	modifiedFiles   []string     // and the ones they wrote or edited
	compactedAt     int          // tool calls in the history when it was last folded; -1 = never
	perms           *permissions // what the user has already agreed to keep allowing
	sessionBroken   atomic.Bool  // the session log failed once: say so once, not per message
	unfinishedNoted atomic.Bool  // a reply that ended without saying it was finished: said once per run
	turnRunning     atomic.Bool  // a round-trip is in flight: the history belongs to the goroutine answering
	turn            atomic.Int64 // the round-trip currently running, carried on every event: a notice from elsewhere reads it
	history         []llm.Message
	histBytes       int // the conversation's bytes, kept as it changes: the estimate runs on every call
	histCount       int // how many messages that total was measured over, so a stale total is caught
	meter           meter
	specBytes       int         // what the tool list costs every request, counted once
	savedRatio      float64     // the byte ratio the session log last heard of
	usageMu         sync.Mutex  // the numbers a front end reads while a turn runs elsewhere
	report          Usage       // what the conversation cost as of the last finished turn
	work            contextWork // what this ask had to do to fit the conversation in, for a front end to say so
	waitingMu       sync.Mutex  // asks typed while a turn ran, taken between round-trips
	waiting         []string    // what the user typed into a run that is still going
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
		// the ask tool is offered only when there is somebody at the other end to answer it
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
	if a.refuseDuringATurn("forgetting the conversation") {
		return
	}
	if len(a.history) > 1 {
		// the saved conversation starts over too, or --continue hands back what was just cleared
		a.reportSave(a.opts.Session.Cleared())
	}
	a.startOver()
}

// startOver empties the conversation without touching the log, leaving the saved one to return to
func (a *Agent) startOver() {
	a.history = []llm.Message{{Role: "system", Content: a.prompt}}
	a.recountBytes()
	a.meter.clearCost()
	a.compactedAt = -1
	a.readFiles, a.modifiedFiles = nil, nil
	a.environment = ""
	a.opts.Plan.Clear() // the steps belong to the conversation that went
	a.work.started()    // nothing has had to make room in the conversation that starts now
	a.dropWaiting()     // an ask typed into the old conversation does not steer the new one
	a.publishUsage()
}

// turnBusy says a round-trip is in flight, and the history belongs to the goroutine answering it
func (a *Agent) turnBusy() bool { return a.turnRunning.Load() }

// refuseDuringATurn stops a between-turns change mid-turn, in words a user can act on
func (a *Agent) refuseDuringATurn(what string) bool {
	if !a.turnBusy() {
		return false
	}
	a.tell("a turn is running: " + what + " waits for it to finish (esc stops it)")
	return true
}

// reportSave says once per run that the session log will not write: it is a fact about the run
func (a *Agent) reportSave(err error) {
	if err == nil || a.sessionBroken.Swap(true) {
		return
	}
	a.tell(err.Error())
}

func (a *Agent) add(m llm.Message) {
	m = storable(m)
	a.histBytes += messageBytes(m)
	a.histCount++
	a.history = append(a.history, m)
	a.reportSave(a.opts.Session.Append(m))
}

// Learned gives a returning run what the last learned about its server, so it measures the same way
func (a *Agent) Learned(bytesPerTok float64) {
	if bytesPerTok >= minBytesPerTok && bytesPerTok <= maxBytesPerTok {
		a.meter.bytesPerTok, a.savedRatio = bytesPerTok, bytesPerTok
	}
}

// Resume continues where a saved conversation stopped.
func (a *Agent) Resume(messages []llm.Message) {
	a.history = append(a.history, messages...)
	a.recountBytes()
	// new calls continue the saved numbering, so a result id never names someone else's output
	a.callSeq = highestCallID(a.history)
	a.restoreFiles(a.history)
	a.restorePlan(a.history)
	a.meter.anchorTokens = 0 // the count the saved run had is not this run's
	a.environment = lastEnvironment(a.history)
	if hasRecap(a.history) {
		// a conversation folded before saving waits for new work before folding again, as the old run would
		a.compactedAt = min(callBlocks(a.history), compactKeep)
	}
	// no trimming here: nothing has been asked of the window yet, and prepareTurn does all of it first
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

func (a *Agent) tell(text string) { a.emit(Event{Kind: Notice, Turn: a.turnNo(), Text: text}) }

func (a *Agent) emit(ev Event) Decision {
	if ev.Turn == 0 {
		ev.Turn = a.turnNo()
	}
	return a.sink.Emit(ev)
}

// turnNo is the round-trip now running, 0 between turns, so a notice can say which turn it is
func (a *Agent) turnNo() int { return int(a.turn.Load()) }

// note keeps the history honest when a turn ends without an answer.
func (a *Agent) note(text string) {
	a.add(llm.Message{Role: "assistant", Content: text})
}
