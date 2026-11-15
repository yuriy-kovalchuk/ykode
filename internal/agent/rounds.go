package agent

// outOfRounds says this round is the last one the ask gets; a limit of 0 means there is none.
func (a *Agent) outOfRounds(turn int) bool {
	return a.opts.MaxTurns > 0 && turn >= a.opts.MaxTurns
}
