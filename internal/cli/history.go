package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// History answers `ykode history [flags] [what]`: what the runs did, outside a session.
// Flags come first, then what to look for - a tool name, failed, outside, or an id like r12.
func History(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ykode history", flag.ContinueOnError)
	all := fs.Bool("all", false, "every run of this root, not only the last one")
	failed := fs.Bool("failed", false, "only calls that failed or were refused")
	outside := fs.Bool("outside", false, "only calls that reached outside the root")
	since := fs.String("since", "", "only calls in the last stretch, like 24h or 90m")
	root := fs.String("root", ".", "which project root to report on")
	asJSON := fs.Bool("json", false, "one JSON object per call")
	limit := fs.Int("limit", auditLimit, "how many calls to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	what := strings.Join(fs.Args(), " ")
	if what == "" {
		if *failed {
			what = "failed"
		}
		if *outside {
			what = "outside"
		}
	}
	window, err := time.ParseDuration(*since)
	if *since != "" && err != nil {
		return fmt.Errorf("--since %q is not a duration like 24h", *since)
	}
	settings, err := config.Load([]string{"--root", *root})
	if err != nil {
		return err
	}
	entries, damaged, err := loggedScope(settings, *all, window)
	if err != nil {
		return err
	}
	if damaged > 0 && *asJSON {
		fmt.Fprintf(os.Stderr, "ykode: %d log lines could not be read\n", damaged)
	}
	if len(entries) == 0 {
		return errors.New("nothing logged in " + settings.AuditDir)
	}
	if *asJSON {
		return loggedJSON(out, entries, what, *limit)
	}
	fmt.Fprintln(out, audit.Answer(entries, what, *limit, damaged))
	return nil
}

// loggedScope reads the run asked about: the newest one, or every run of this root, inside the window.
func loggedScope(cfg config.Config, all bool, since time.Duration) ([]audit.Entry, int, error) {
	logger := audit.New(cfg.AuditDir, "")
	var entries []audit.Entry
	var damaged int
	var err error
	if all {
		entries, damaged, err = logger.ReadRoot(cfg.Root)
	} else {
		entries, damaged, err = logger.Read(logger.LatestSession())
	}
	if err != nil || since <= 0 {
		return entries, damaged, err
	}
	cutoff := time.Now().Add(-since)
	kept := entries[:0]
	for _, e := range entries {
		if !e.Time.Before(cutoff) {
			kept = append(kept, e)
		}
	}
	return kept, damaged, nil
}

func loggedJSON(out io.Writer, entries []audit.Entry, what string, limit int) error {
	for _, e := range audit.Match(entries, what, limit) {
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(raw))
	}
	return nil
}

// auditLimit caps how many calls one history listing shows.
const auditLimit = 25
