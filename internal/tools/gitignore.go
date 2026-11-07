package tools

import (
	"regexp"
	"strings"
)

// ignoreSet holds the .gitignore files in effect, outermost first, since a nested one overrides.
type ignoreSet []*ignoreFile

// ignoreFile is one .gitignore and the directory it lives in.
type ignoreFile struct {
	dir   string
	rules []ignoreRule
}

// ignoreRule is one pattern line compiled to a regexp over paths relative to its directory.
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
		body := gitignoreGlobRegexp(line)
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

// ignored reports whether path is excluded; the deepest .gitignore wins, last pattern within it.
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
			// patterns end with "(/.*)?$", so a directory rule must still not match a file of the same name
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

// gitignoreGlobRegexp compiles one gitignore pattern to regexp source: ** spans directories,
// * and ? do not. It lives here now because gitignore parsing is its only user.
func gitignoreGlobRegexp(glob string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			if strings.HasPrefix(glob[i+1:], "*") {
				i++
				if strings.HasPrefix(glob[i+1:], "/") {
					i++
					b.WriteString(`([^/]*/)*`) // "**/" matches zero or more directories
					continue
				}
				b.WriteString(".*")
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			end := strings.Index(glob[i+1:], "]")
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : i+end+1]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}
