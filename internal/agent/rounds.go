package agent

import "fmt"

const (
	// wrapUpShare: the last fifth of the rounds is where the model is told to start finishing.
	wrapUpShare = 5
	// wrapUpMinTurns is the smallest limit worth a warning; below it the warning would be the limit.
	wrapUpMinTurns = 5
)

// outOfRounds says this round is the last one the ask gets; a limit of 0 means there is none.
func (a *Agent) outOfRounds(turn int) bool {
	return a.opts.MaxTurns > 0 && turn >= a.opts.MaxTurns
}

// wrapUpTurn is the round after which the model is warned, or 0 when no warning applies.
func wrapUpTurn(maxTurns int) int {
	if maxTurns < wrapUpMinTurns {
		return 0
	}
	return maxTurns - maxTurns/wrapUpShare
}

// warnNearLimit tells the model, once, that the rounds are running out. A model that thinks deeply
// keeps widening a review ("one more check") and never turns to the answer until it is cut off:
// told in time, it spends the rounds left on finishing instead of on the next thing to look at.
func (a *Agent) warnNearLimit(turn int) {
	if at := wrapUpTurn(a.opts.MaxTurns); at == 0 || turn != at {
		return
	}
	left := a.opts.MaxTurns - turn
	a.tell(fmt.Sprintf("%d of %d tool rounds used: asking the model to wrap up", turn, a.opts.MaxTurns))
	a.modelNote(fmt.Sprintf("(You have used %d of the %d tool rounds this request gets; %d are left. "+
		"Stop widening the search: finish only the checks that decide the answer, then write the answer.)",
		turn, a.opts.MaxTurns, left))
}
