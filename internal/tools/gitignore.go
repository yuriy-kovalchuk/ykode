package tools

import (
	"regexp"
	"strings"
)

// ignoreSet holds the .gitignore files in effect for a directory, outermost
// first, because a nested .gitignore overrides the ones above it.
type ignoreSet []*ignoreFile

// ignoreFile is one .gitignore and the directory it lives in.
type ignoreFile struct {
	dir   string
	rules []ignoreRule
}

// ignoreRule is one pattern line, compiled into a regexp over the path
// relative to ignoreFile.dir. A rule matches a path when it matches the path
// itself or any of its ancestors, so ignoring a directory hides its contents.
type ignoreRule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

func parseIgnoreRules(text string) []ignoreRule {
	var rules []ignoreRule
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negate, line = true, line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly, line = true, line[:len(line)-1]
		}
		anchored := strings.HasPrefix(line, "/")
		if anchored {
			line = strings.TrimPrefix(line, "/")
		} else if strings.Contains(line, "/") {
			anchored = true // git anchors any pattern with an inner slash
		}
		body := globRegexp(line)
		if body == "" {
			continue
		}
		if anchored {
			body = "^" + body + `(/.*)?$`
		} else {
			body = `(^|.*/)` + body + `(/.*)?$`
		}
		re, err := regexp.Compile(body)
		if err != nil {
			continue
		}
		r.re = re
		rules = append(rules, r)
	}
	return rules
}

// ignored reports whether path (root-relative, slash separated) is excluded by
// the .gitignore files in effect for it. The deepest file wins, and within one
// file the last matching pattern wins, as git does it.
func ignored(path string, ignores ignoreSet, isDir bool) bool {
	for i := len(ignores) - 1; i >= 0; i-- {
		ig := ignores[i]
		rel := strings.TrimPrefix(path, ig.dir+"/")
		if rel == path && ig.dir != "." { // path is outside this .gitignore
			continue
		}
		verdict := false
		decided := false
		for _, r := range ig.rules {
			// The patterns end with "(/.*)?$", so they already cover paths inside
			// a match. A directory-only rule must not match a file of the same
			// name, so for files it has to match an ancestor instead.
			hit := r.re.MatchString(rel)
			if r.dirOnly && !isDir {
				hit = false
				for i, c := range rel {
					if c == '/' && r.re.MatchString(rel[:i]) {
						hit = true
						break
					}
				}
			}
			if hit {
				verdict, decided = !r.negate, true // "!" un-ignores
			}
		}
		if decided {
			return verdict
		}
	}
	return false
}
