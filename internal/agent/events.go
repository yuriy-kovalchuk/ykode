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
	ToolDraft                  // a piece of a tool call still being written, Call says which
	Progress                   // the server's speeds while a reply streams, in Stats
	Steered                    // an ask typed mid-run joined the conversation, Text holds it
	Summary                    // the older turns were folded into this text, in its own lines
	Cleared                    // the conversation was forgotten; a front end drops what it reported
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
	case ToolDraft:
		return "tool_draft"
	case Progress:
		return "progress"
	case Steered:
		return "steered"
	case Summary:
		return "summary"
	case Cleared:
		return "cleared"
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
	Call   int // on a ToolDraft: which call of the reply, as the server numbers them
}

// Decision answers a Confirm event.
type Decision int

const (
	Refuse Decision = iota
	Allow
	AllowAlways // yes, and remember it: the same tool or command prefix goes unasked from now on
)

// Sink receives every event of a run; the agent prints nothing itself. Its reply is used for Confirm only.
type Sink interface {
	Emit(Event) Decision
}

// nullSink discards a run and refuses anything it is asked about.
type nullSink struct{}

func (nullSink) Emit(Event) Decision { return Refuse }

// Stats summarises one model round-trip for the user.
type Stats struct {
	FinishReason   string
	PromptTokens   int
	CachedTokens   int
	OutputTokens   int
	PrefillTokSec  float64
	DecodeTokSec   float64
	DraftN         int // tokens a draft model proposed, when the server has one
	DraftNAccepted int
	Estimated      bool // the speeds were measured by the client, not reported by the server
}
