package tools

// A class says what a tool does to the project, and that decides how its call may run
type Class int

const (
	// the zero value loses nothing: serially, as a change, reusing nothing - as an unknown name reads
	ClassRuns   Class = iota // changes the project by doing something, not by writing out text
	ClassReads               // changes nothing, so several of them may run at once
	ClassWrites              // the argument is mostly text that ends up on disk
	ClassSays                // changes nothing and runs alone: the plan the screen reads, a question asked
)
