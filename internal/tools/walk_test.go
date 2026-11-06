package tools

import "testing"

func TestIgnoreRulesFollowGitSemantics(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		isDir   bool
		want    bool
	}{
		{"node_modules/", "node_modules", true, true},
		{"node_modules/", "node_modules/x.go", false, true}, // a dir rule covers its contents
		{"build/", "build", false, false},                   // but not a file of the same name
		{"*.log", "a.log", false, true},
		{"*.log", "deep/a.log", false, true},
		{"/root-only.txt", "root-only.txt", false, true},
		{"/root-only.txt", "sub/root-only.txt", false, false},
		{"src/*.go", "src/a.go", false, true},
		{"src/*.go", "src/deep/a.go", false, false}, // an inner slash anchors the pattern
		{"**/vendor/*.go", "a/b/vendor/x.go", false, true},
		{"*.o", "keep.o", false, true},
	}
	for _, c := range cases {
		rules := parseIgnoreRules(c.pattern)
		if got := ignored(c.path, ignoreSet{{dir: ".", rules: rules}}, c.isDir); got != c.want {
			t.Errorf("%q vs %q (dir=%v) = %v, want %v", c.pattern, c.path, c.isDir, got, c.want)
		}
	}
}

func TestNegationAndNestedGitignore(t *testing.T) {
	outer := parseIgnoreRules("*.log\n!important.log\n")
	if !ignored("other.log", ignoreSet{{dir: ".", rules: outer}}, false) {
		t.Error("*.log should ignore other.log")
	}
	if ignored("important.log", ignoreSet{{dir: ".", rules: outer}}, false) {
		t.Error("!important.log should un-ignore it")
	}

	inner := parseIgnoreRules("!*.log\n")
	nested := ignoreSet{{dir: ".", rules: outer}, {dir: "sub", rules: inner}}
	if ignored("sub/a.log", nested, false) {
		t.Error("sub/.gitignore should override the outer one")
	}
	if !ignored("a.log", nested, false) {
		t.Error("the outer rule should still apply outside sub/")
	}
}

func TestWalkSkipsGitWithoutARule(t *testing.T) {
	rules := parseIgnoreRules("")
	set := ignoreSet{{dir: ".", rules: rules}}
	if ignored("main.go", set, false) {
		t.Error("an empty .gitignore ignores nothing")
	}
}
