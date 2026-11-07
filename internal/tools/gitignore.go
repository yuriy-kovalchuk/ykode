package tools

import (
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// ignoreRules is the .gitignore set in effect for one walk. Rules from outer files come first
// and rules from inner files after, each prefixed with the directory it was read in, which is
// git's own order: the last rule that speaks about a path decides, and "!" un-ignores.
type ignoreRules struct {
	lines    []string
	compiled *gitignore.GitIgnore
}

// ignored reports whether a walked path is excluded. A directory is probed with its trailing
// slash, which is what separates "build/" (a directory rule) from a file that happens to be named build.
func (r *ignoreRules) ignored(path string, isDir bool) bool {
	if r == nil || r.compiled == nil {
		return false
	}
	if isDir {
		path += "/"
	}
	return r.compiled.MatchesPath(path)
}

// with adds one .gitignore that was found in dir. It returns a new set rather than editing this
// one, so a sibling branch of the walk does not inherit a rule that only belongs to its neighbour.
func (r *ignoreRules) with(dir, text string) *ignoreRules {
	var lines []string
	if r != nil {
		lines = append(lines, r.lines...)
	}
	lines = append(lines, scopedRules(dir, text)...)
	return &ignoreRules{lines: lines, compiled: gitignore.CompileIgnoreLines(lines...)}
}

// scopedRules turns the lines of one .gitignore into rules over the paths this walk sees.
func scopedRules(dir, text string) []string {
	var rules []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}
		negate := strings.HasPrefix(line, "!")
		pattern := strings.TrimPrefix(line, "!")
		if dir != "" && dir != "." {
			// a leading "/" anchors a rule at the file it lives in, and the prefix does that already
			pattern = dir + "/" + strings.TrimPrefix(pattern, "/")
		}
		if negate {
			pattern = "!" + pattern
		}
		rules = append(rules, pattern)
	}
	return rules
}
