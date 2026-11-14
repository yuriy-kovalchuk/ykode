package tools

// A class says what a tool does to the project. That one answer decides the rest of how a call runs:
// whether calls may overlap, whether what was read before still holds the answer, whether a repeat
// can be answered from the result the model already has, and whether the arguments can be dropped
// once their text is on disk.
type Class int

const (
	// The zero value is the reading that loses nothing: serially, as a change to the project,
	// reusing nothing. It is what a name the run no longer offers reads as.
	ClassRuns   Class = iota // changes the project by doing something, not by writing out text
	ClassReads               // changes nothing, so several of them may run at once
	ClassWrites              // the argument is mostly text that ends up on disk
	ClassSays                // changes nothing and runs alone: the plan the screen reads, a question asked
)
