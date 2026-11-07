// Command ykode is a terminal coding agent for an OpenAI-compatible server such as llama-server.
package main

import (
	"fmt"
	"os"

	"github.com/yuriy-kovalchuk/ykode/internal/app"
)

// version is stamped at build time with -X main.version=...
var version = "dev"

func main() {
	if err := app.Run(os.Args[1:], version); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
		os.Exit(1)
	}
}
