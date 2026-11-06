package tools

import (
	"regexp"
	"strings"
)

// globRegexp compiles a glob to regexp source: ** spans directories, * and ? do not.
func globRegexp(glob string) string {
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
