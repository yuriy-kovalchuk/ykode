package agent

// EventKind says what an event reports.
type EventKind int

const (
	TurnStart EventKind = iota // a model round-trip began
	Thinking                   // one streamed reasoning chunk
	Answer                     // one streamed answer chunk
	TurnEnd                    // the round-trip finished, Stats filled
	ToolStart                  // a tool call is about to run
	ToolEnd                    // a tool returned text
	ToolFail                   // a tool or the user rejected it
	Notice                     // something the run did that is not a tool: trim, nudge, cut-off
	Confirm                    // the run is waiting for a decision
)

func (k EventKind) String() string {
	switch k {
	case TurnStart:
		return "turn_start"
	case Thinking:
		return "thinking"
	case Answer:
		return "answer"
	case TurnEnd:
		return "turn_end"
	case ToolStart:
		return "tool_start"
	case ToolEnd:
		return "tool_end"
	case ToolFail:
		return "tool_fail"
	case Notice:
		return "notice"
	case Confirm:
		return "confirm"
	}
	return "unknown"
}

// Event is one step of a run. Tool events may arrive from several goroutines at once, so a
// consumer must not assume one ordering, and Emit must be safe to call concurrently.
type Event struct {
	Kind   EventKind
	Turn   int
	Tool   string
	Args   string
	Text   string
	Target []string // paths outside the project root, on a Confirm event
	Stats  Stats
}

// Decision answers a Confirm event.
type Decision int

const (
	Refuse Decision = iota
	Allow
)

// Sink receives every event of a run; the agent prints nothing itself. Its reply is used for Confirm only.
type Sink interface {
	Emit(Event) Decision
}

// nullSink discards a run and refuses anything it is asked about.
type nullSink struct{}

func (nullSink) Emit(Event) Decision { return Refuse }
