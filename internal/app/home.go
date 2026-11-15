package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// PrepareHome makes the per-user directory ready: the settings file a first run never wrote,
// and no leftovers from runs that finished long ago (spilled command output, the result dirs
// of finished runs).
func PrepareHome(cfg config.Config) {
	if err := config.SeedSettings(); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
	}

	removed := func(what string, n int, err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "ykode:", err)
			return
		}
		if n > 0 {
			fmt.Fprintf(os.Stderr, "ykode: removed %d old %s\n", n, what)
		}
	}
	n, err := tools.PruneSpillLogs(cfg.CommandLogDir, cfg.KeepCommandLogs)
	removed("command log(s)", n, err)
	n, err = pruneSessions(cfg)
	removed("conversation(s)", n, err)
}

// pruneSessions removes the conversations past their keep. One entry that cannot be read or removed is
// passed over rather than ending the sweep: it is one conversation among many, and stopping here would
// leave everything after it on disk at every future start.
func pruneSessions(cfg config.Config) (int, error) {
	entries, err := os.ReadDir(cfg.SessionDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) { // nothing saved yet is not an error
		return 0, fmt.Errorf("clean old conversations in %s: %w", cfg.SessionDir, err)
	}
	removed, skipped := 0, 0
	for _, e := range entries {
		id, isSession := strings.CutSuffix(e.Name(), ".jsonl")
		if !isSession {
			continue
		}
		info, err := e.Info()
		if err != nil {
			skipped++
			continue
		}
		if id == cfg.Session || time.Since(info.ModTime()) < cfg.KeepSessions { // --continue just picked it
			continue
		}
		if err := os.Remove(filepath.Join(cfg.SessionDir, e.Name())); err != nil {
			skipped++
			continue
		}
		removed++
	}
	if skipped > 0 {
		return removed, fmt.Errorf("%d saved conversation(s) in %s could not be read or removed", skipped, cfg.SessionDir)
	}
	return removed, nil
}
