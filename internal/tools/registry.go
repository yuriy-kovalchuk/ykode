package tools

import "slices"

// Registry keeps tools in registration order so the list the model sees stays stable.
type Registry struct {
	byName  map[string]Tool
	ordered []Tool
}

func NewRegistry(all ...Tool) *Registry {
	r := &Registry{byName: make(map[string]Tool, len(all))}
	for _, t := range all {
		if _, dup := r.byName[t.Name()]; dup {
			continue
		}
		r.byName[t.Name()] = t
		r.ordered = append(r.ordered, t)
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// Add appends a tool, keeping the order the model sees; a name already there is not swapped
func (r *Registry) Add(t Tool) {
	if _, dup := r.byName[t.Name()]; dup {
		return
	}
	r.byName[t.Name()] = t
	r.ordered = append(r.ordered, t)
}

func (r *Registry) All() []Tool { return r.ordered }

// Without is the registry less the tools named, in the same order; names it does not hold are passed over
func (r *Registry) Without(names ...string) *Registry {
	keep := make([]Tool, 0, len(r.ordered))
	for _, t := range r.ordered {
		if !slices.Contains(names, t.Name()) {
			keep = append(keep, t)
		}
	}
	return NewRegistry(keep...)
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.ordered))
	for _, t := range r.ordered {
		names = append(names, t.Name())
	}
	return names
}
