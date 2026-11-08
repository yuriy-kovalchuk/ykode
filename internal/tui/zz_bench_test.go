package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
)

var zzTime = time.Now()

func zzLog(calls int) transcript {
	var log transcript
	for i := 0; i < calls; i++ {
		log.user("a prompt that asks for something reasonable in this project", zzTime)
		log.toolStart("edit_file", `{"path":"internal/tui/rows.go","edits":[{"oldText":"x","newText":"y"}]}`)
		log.toolEnd("edit_file", `{"path":"internal/tui/rows.go"}`, editResult)
	}
	log.answer("the answer the model wrote, with **shape** in it")
	return log
}

func BenchmarkZZParts(b *testing.B) {
	for _, calls := range []int{50, 300, 1200} {
		log := zzLog(calls)
		content := log.render(90, false)
		view := viewport.New(90, 30)
		rows := strings.Split(content, "\n")
		b.Run(strings.Repeat(".", calls), func(b *testing.B) {
			b.Logf("%d calls: %d rows, %d bytes", calls, len(rows), len(content))
			b.Run("render", func(b *testing.B) {
				for b.Loop() {
					_ = log.render(90, false)
				}
			})
			b.Run("SetContent", func(b *testing.B) {
				for b.Loop() {
					view.SetContent(content)
				}
			})
			b.Run("View", func(b *testing.B) {
				view.SetContent(content)
				view.GotoBottom()
				for b.Loop() {
					_ = view.View()
				}
			})
			b.Run("joinWindowOnly", func(b *testing.B) {
				for b.Loop() {
					end := len(rows)
					_ = strings.Join(rows[end-30:end], "\n")
				}
			})
		})
	}
}

func BenchmarkZZDraft(b *testing.B) {
	body := strings.Repeat("func example() {\n\treturn nil\n}\n\n", 3000) // ~60 KB file
	args := `{"path":"big.go","content":"` + strings.ReplaceAll(body, "\n", `\n`) + `"}`
	d := draft{call: 1, tool: "write_file", args: args}
	b.ResetTimer()
	for b.Loop() {
		_, _, _ = d.preview()
	}
}
