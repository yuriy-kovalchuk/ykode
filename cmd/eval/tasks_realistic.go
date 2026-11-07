package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const goMod = "module example.com/proj\n\ngo 1.25\n"

// realisticTasks span packages, hide the answer among decoys, or take two requests: closer to real work.
func realisticTasks() []task {
	return []task{
		{
			name:   "cross-package-rename",
			prompt: "rename the function SumAll to Total everywhere in this project",
			fixture: map[string]string{
				"go.mod":                goMod,
				"pkg/mathx/sum.go":      "package mathx\n\n// SumAll adds every value.\nfunc SumAll(values ...int) int {\n\ttotal := 0\n\tfor _, v := range values {\n\t\ttotal += v\n\t}\n\treturn total\n}\n",
				"pkg/report/report.go":  "package report\n\nimport \"example.com/proj/pkg/mathx\"\n\nfunc Line(values []int) int { return mathx.SumAll(values...) }\n",
				"cmd/app/main.go":       "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/proj/pkg/mathx\"\n)\n\nfunc main() { fmt.Println(mathx.SumAll(1, 2, 3)) }\n",
				"pkg/mathx/sum_test.go": "package mathx\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) {\n\tif SumAll(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
			},
			maxTurns: 12,
			check: func(project string, _ *transcript, _ error) (bool, string) {
				if out, err := goRun(project, "build", "./..."); err != nil {
					return false, "go build fails: " + flat(out, 200)
				}
				if out, _ := exec.Command("grep", "-rl", "SumAll", project).Output(); len(out) > 0 {
					return false, "SumAll is still in: " + flat(string(out), 200)
				}
				return true, "renamed across three packages and it builds"
			},
		},
		{
			name:     "where-is-it-configured",
			prompt:   "where is the maximum number of upload retries configured, and what is it? answer with the file path and the value",
			fixture:  decoyProject(),
			maxTurns: 10,
			check: func(_ string, ran *transcript, _ error) (bool, string) {
				if !ran.contains("config/limits.go") || !wholeNumberIn(ran.answer, "4") {
					return false, "answer: " + quote(ran.answer)
				}
				if ran.contains("old.yaml") || wholeNumberIn(ran.answer, "9") {
					return false, "fell for a decoy: " + quote(ran.answer)
				}
				return true, "found the real setting among the decoys"
			},
		},
		{
			name:   "bug-in-another-file",
			prompt: "the tests in pkg/price fail. fix the cause without changing any test",
			fixture: map[string]string{
				"go.mod":                   goMod,
				"pkg/price/price.go":       "package price\n\nimport \"example.com/proj/pkg/discount\"\n\n// Total is the price after the discount.\nfunc Total(cents int) int { return discount.Apply(cents) }\n",
				"pkg/price/price_test.go":  priceTest,
				"pkg/discount/discount.go": "package discount\n\n// Apply takes 10% off.\nfunc Apply(cents int) int { return cents - cents/100 }\n",
			},
			maxTurns: 12,
			check: func(project string, _ *transcript, _ error) (bool, string) {
				if readBack(project, "pkg/price/price_test.go") != priceTest {
					return false, "the test was changed"
				}
				if out, err := goRun(project, "test", "./..."); err != nil {
					return false, "go test still fails: " + flat(out, 200)
				}
				return true, "fixed the cause in the other package"
			},
		},
		{
			name:     "two-prompt-session",
			prompt:   "add a function Double(n int) int to src/mathx.go that returns n*2",
			followUp: "now do the same for Triple, returning n*3",
			fixture:  map[string]string{"go.mod": goMod, "src/mathx.go": "package src\n"},
			maxTurns: 8,
			check: func(project string, _ *transcript, _ error) (bool, string) {
				body := readBack(project, "src/mathx.go")
				if !strings.Contains(body, "func Double(") || !strings.Contains(body, "func Triple(") {
					return false, "src/mathx.go = " + quote(body)
				}
				if out, err := goRun(project, "build", "./..."); err != nil {
					return false, "go build fails: " + flat(out, 200)
				}
				return true, "the second request built on the first"
			},
		},
	}
}

const priceTest = "package price\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) {\n\tif got := Total(1000); got != 900 {\n\t\tt.Fatalf(\"Total(1000) = %d, want 900\", got)\n\t}\n}\n"

// decoyProject hides one real setting among 50 files of similar names, a stale README and a test value.
func decoyProject() map[string]string {
	files := map[string]string{
		"go.mod":                goMod,
		"config/limits.go":      "package config\n\n// MaxUploadRetries is how often a failed upload is tried again.\nconst MaxUploadRetries = 4\n",
		"README.md":             "Upload retries are configured in config/old.yaml.\n",
		"upload/upload_test.go": "package upload\n\n// a test pretends the limit is higher\nvar testRetries = 9\n",
	}
	for i := range 50 {
		files[fmt.Sprintf("internal/part%02d/part.go", i)] =
			fmt.Sprintf("package part%02d\n\n// retry handling for downloads lives elsewhere\nconst maxDownloadRetry%d = %d\n", i, i, i%3+1)
	}
	return files
}

// goRun runs the go tool in the project with a deadline, for checks that build or test it.
func goRun(project string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = project
	out, err := cmd.CombinedOutput()
	return string(out), err
}
