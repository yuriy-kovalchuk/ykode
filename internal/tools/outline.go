package tools

import (
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

const (
	// declChars caps one declaration line so a dense file cannot blow the budget.
	declChars = 90
	// maxDecls caps the list itself.
	maxDecls = 80
)

// declarationRe finds declarations in languages with no parser here.
var declarationRe = regexp.MustCompile(`^\s{0,6}(func|def|class|interface|impl|module|namespace|struct|enum|type|export (function|class|const|let|type)|pub (fn|struct|trait|class)|fn|sub|task)\b`)

// outlineText lists a file's declarations with their line ranges.
func outlineText(name string, lines []string) string {
	if strings.HasSuffix(name, ".go") {
		if out, ok := goOutline(lines); ok {
			return out
		}
	}
	return keywordOutline(lines)
}

// goOutline parses the file as Go; anything that does not parse falls back to the keyword scan.
func goOutline(lines []string) (string, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "f.go", strings.Join(lines, "\n"), parser.SkipObjectResolution)
	if err != nil {
		return "", false
	}
	head := func(pos token.Pos) string {
		line := fset.Position(pos).Line
		if line >= 1 && line <= len(lines) {
			return cutOneLine(lines[line-1])
		}
		return ""
	}

	var b strings.Builder
	count := 0
	for _, decl := range file.Decls {
		if count == maxDecls {
			fmt.Fprintf(&b, "[%d declarations, list cut; read_file without outline for the rest]\n", count)
			break
		}
		count++
		fmt.Fprintf(&b, "lines %d-%d: %s\n",
			fset.Position(decl.Pos()).Line, fset.Position(decl.End()).Line, head(decl.Pos()))
	}
	if count == 0 {
		return "no declarations found", true
	}
	return strings.TrimSuffix(b.String(), "\n"), true
}

// keywordOutline scans for declaration keywords, which is as good as it gets without a parser.
func keywordOutline(lines []string) string {
	var b strings.Builder
	count := 0
	for n, line := range lines {
		if !declarationRe.MatchString(line) {
			continue
		}
		if count == maxDecls {
			fmt.Fprintf(&b, "[%d declarations, list cut; read_file without outline for the rest]", count)
			break
		}
		count++
		fmt.Fprintf(&b, "line %d: %s\n", n+1, cutOneLine(line))
	}
	if count == 0 {
		return "no declarations found"
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func cutOneLine(line string) string {
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > declChars {
		return strings.TrimRight(string(r[:declChars]), " ") + "..."
	}
	return line
}
