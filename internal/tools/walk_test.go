package tools

import (
	"strings"
	"testing"
)

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
		rules := (&ignoreRules{}).with(".", c.pattern)
		if got := rules.ignored(c.path, c.isDir); got != c.want {
			t.Errorf("%q vs %q (dir=%v) = %v, want %v", c.pattern, c.path, c.isDir, got, c.want)
		}
	}
}

func TestNegationAndNestedGitignore(t *testing.T) {
	rules := (&ignoreRules{}).with(".", "*.log\n!important.log\n")
	if !rules.ignored("other.log", false) {
		t.Error("*.log should ignore other.log")
	}
	if rules.ignored("important.log", false) {
		t.Error("!important.log should un-ignore it")
	}

	nested := rules.with("sub", "!*.log\n")
	if nested.ignored("sub/a.log", false) {
		t.Error("sub/.gitignore should override the outer one")
	}
	if !nested.ignored("a.log", false) {
		t.Error("the outer rule should still apply outside sub/")
	}
	if rules.ignored("sub/a.log", false) != true {
		t.Error("the outer rule should still apply inside sub/")
	}
	// adding a .gitignore to one branch must not change what another branch sees
	sibling := rules.with("other", "!*.log\n")
	if !sibling.ignored("sub/a.log", false) {
		t.Error("a !rule under other/ un-ignored a file under sub/")
	}
	if !rules.ignored("other/a.log", false) {
		t.Error("the outer set was changed by adding rules to a branch")
	}
}

func TestACommentOrABlankLineIgnoresNothing(t *testing.T) {
	rules := (&ignoreRules{}).with(".", "# build/\n\n   \n\n")
	for _, path := range []string{"build", "build/", "build/x.o"} {
		if rules.ignored(path, strings.HasSuffix(path, "/")) {
			t.Errorf("%q was ignored by a comment", path)
		}
	}
}

func TestWalkSkipsGitWithoutARule(t *testing.T) {
	rules := (&ignoreRules{}).with(".", "")
	if rules.ignored("main.go", false) {
		t.Error("an empty .gitignore ignores nothing")
	}
	if (&ignoreRules{}).ignored("main.go", false) {
		t.Error("no .gitignore at all should ignore nothing")
	}
}
