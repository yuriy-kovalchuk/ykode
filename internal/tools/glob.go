package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// glob matches the slash paths the walkers produce. The two rules the model relies on are
// older than this code: a pattern without "/" names a file at any depth, and a pattern with
// "/" may start at any directory boundary, so "src/*.go" finds "internal/src/a.go".
type glob struct {
	patterns []string
}

// newGlob compiles a pattern. An empty one is a caller bug, not a match-everything.
func newGlob(pattern string) (*glob, error) {
	pattern = strings.TrimPrefix(pattern, "./")
	if strings.TrimSpace(pattern) == "" {
		return nil, errors.New("empty pattern")
	}
	if !doublestar.ValidatePattern(pattern) {
		return nil, fmt.Errorf("bad pattern %q", pattern)
	}
	if strings.Contains(pattern, "/") {
		return &glob{patterns: []string{pattern, "**/" + pattern}}, nil
	}
	return &glob{patterns: []string{"**/" + pattern, pattern}}, nil
}

// match reports whether a walked path is one the pattern names.
func (g *glob) match(path string) bool {
	for _, pattern := range g.patterns {
		if ok, err := doublestar.Match(pattern, path); err == nil && ok {
			return true
		}
	}
	return false
}
