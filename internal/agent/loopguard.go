package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// read_file is left out: it detects repeats by content, which also notices an edited file.
var guardedTools = map[string]bool{"find_files": true, "search_text": true}

const (
	repeatWarnAt = 3 // the third identical ask is told plainly that the next one ends the run
	repeatStopAt = 4 // the fourth means the model is stuck: stop and answer from what it has
)

// loopGuard answers a repeated search from the result the model still has in context.
type loopGuard struct {
	mu    sync.Mutex
	runs  map[string]loopRun // call key -> the call that answered it, and where
	asks  map[string]int     // call key -> how often it has been asked, counting this one
	byID  map[string]string  // result id -> call key, so a result leaving the conversation releases it
	stuck bool               // some call was asked repeatStopAt times: the run should end
}

// loopRun is the first call of a kind: the model can be pointed at its result instead of getting a copy.
type loopRun struct {
	turn int
	id   string
}

func newLoopGuard() *loopGuard {
	g := &loopGuard{}
	g.changed()
	return g
}

// claim reserves the call before it runs, so an identical parallel call is caught too.
func (g *loopGuard) claim(key, id string, turn int) (earlier *loopRun, times int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asks[key]++
	times = g.asks[key]
	if first, seen := g.runs[key]; seen {
		if times >= repeatStopAt {
			g.stuck = true
		}
		return &first, times
	}
	g.runs[key] = loopRun{turn: turn, id: id}
	g.byID[id] = key
	return nil, times
}

// release forgets a call that failed: it answered nothing, so trying it again is not a repeat.
func (g *loopGuard) release(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run, ok := g.runs[key]; ok {
		delete(g.byID, run.id)
	}
	delete(g.runs, key)
	delete(g.asks, key)
}

// forgetID releases a call whose result left the context: asking again is then the only way back.
func (g *loopGuard) forgetID(id string) {
	g.mu.Lock()
	key, ok := g.byID[id]
	g.mu.Unlock()
	if ok {
		g.release(key)
	}
}

// changed forgets every claim: after a change, an earlier result may no longer be true.
func (g *loopGuard) changed() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = map[string]loopRun{}
	g.asks = map[string]int{}
	g.byID = map[string]string{}
	g.stuck = false
}

// isStuck says a call was repeated so often that more turns will not help.
func (g *loopGuard) isStuck() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stuck
}

// reply is what the model is told instead of running the call again.
func (g *loopGuard) reply(name string, earlier loopRun, times int) string {
	text := fmt.Sprintf("you already asked for the same %s in turn %d and this is attempt %d: the answer has not changed. "+
		"its result is %s, still above in this conversation - read something new, or answer from what you have.",
		name, earlier.turn, times, earlier.id)
	if times >= repeatWarnAt {
		text += " asking for it once more ends the run with whatever answer you can give."
	}
	return text
}

// repeatKey identifies one call by tool and arguments; spacing the model varies does not make it new.
func repeatKey(call llm.ToolCall) string {
	return call.Function.Name + " " + strings.Join(strings.Fields(string(call.Function.Arguments)), " ")
}
