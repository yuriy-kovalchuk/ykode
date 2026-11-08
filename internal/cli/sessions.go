package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

const (
	sessionsLimit = 20             // conversations one listing shows
	whenFormat    = "02 Jan 15:04" // when a conversation was last written to
)

// Sessions answers `ykode sessions`: the conversations that can be resumed, newest first, with what
// each was about - because an id is a timestamp nobody remembers.
func Sessions(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ykode sessions", flag.ContinueOnError)
	limit := fs.Int("limit", sessionsLimit, "how many conversations to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := filepath.Join(home.Dir(), "sessions")
	list, err := session.Describe(dir, *limit)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintf(out, "nothing saved in %s: a session saves as it goes\n", dir)
		return nil
	}
	fmt.Fprintf(out, "%d conversations in %s\n", len(list), dir)
	fmt.Fprintln(out, "come back to one with: ykode --continue <id>, or --continue last; enough of an id to be unique is enough")
	fmt.Fprintln(out)
	for _, c := range list {
		fmt.Fprintf(out, "  %s  %s  %3d msgs  %s\n", c.ID, c.Used.Format(whenFormat), c.Messages, filepath.Base(c.Root))
		fmt.Fprintf(out, "    %s\n", c.First)
	}
	return nil
}
