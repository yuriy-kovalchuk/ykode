package agent

import (
	"fmt"
	"slices"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// efforts is what each model's template turned down, so the next request goes with a word it takes
type efforts struct {
	refused   map[string][]string // per model: the words a server refused for it
	supported map[string][]string // per model: the words a server said it takes, when it said
}

// effort is the reasoning_effort word the next request carries for a level; "" leaves the field out
func (a *Agent) effort(level string) string {
	want := llm.EffortFor(level)
	refused := a.efforts.refused[a.model]
	if !slices.Contains(refused, want) {
		return want
	}
	return llm.NearestEffort(want, a.efforts.supported[a.model], refused)
}

// learnEffort reads a refusal of the effort word, and says whether asking again with another is worth it
func (a *Agent) learnEffort(err error, sent string) bool {
	supported, rejected := llm.RejectedEffort(err)
	if !rejected || sent == "" {
		return false
	}
	if a.efforts.refused == nil {
		a.efforts.refused, a.efforts.supported = map[string][]string{}, map[string][]string{}
	}
	a.efforts.refused[a.model] = append(a.efforts.refused[a.model], sent)
	if len(supported) > 0 {
		a.efforts.supported[a.model] = supported
	}
	// each refusal rules a word out, so a model that takes none of them ends with the field left out
	if next := a.effort(a.ThinkingLevel()); next != "" {
		a.tell(fmt.Sprintf("%s does not take reasoning effort %q: asking with %q, the nearest it takes", a.model, sent, next))
	} else {
		a.tell(fmt.Sprintf("%s does not take reasoning effort %q: asking without one", a.model, sent))
	}
	return true
}
