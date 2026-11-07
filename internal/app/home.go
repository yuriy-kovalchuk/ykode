package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// PrepareHome makes the per-user directory ready: the settings file a first run never wrote,
// and no leftovers from runs that finished long ago (spilled command output, the result dirs
// of finished runs, audit months past their retention).
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
	n, err = pruneConversations(cfg)
	removed("conversation(s)", n, err)
	n, err = audit.PruneOldFiles(cfg.AuditDir, cfg.KeepAudit)
	removed("audit file(s)", n, err)
}

// pruneConversations ages out saved conversations and, with each one, the tool results that run
// saved for recall: a result is only reachable by the conversation that names its id, so the two
// survive exactly as long as each other.
func pruneConversations(cfg config.Config) (int, error) {
	removed, alive, err := pruneSessions(cfg)
	if err != nil {
		return removed, err
	}
	orphans, err := pruneOrphanResults(filepath.Dir(cfg.ResultDir), alive)
	return removed + orphans, err
}

// pruneSessions removes conversations past their keep, with their results, and names the ones kept.
func pruneSessions(cfg config.Config) (int, map[string]bool, error) {
	entries, err := os.ReadDir(cfg.SessionDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) { // nothing saved yet is not an error
		return 0, nil, fmt.Errorf("clean old conversations in %s: %w", cfg.SessionDir, err)
	}
	removed, alive := 0, map[string]bool{}
	for _, e := range entries {
		id, isSession := strings.CutSuffix(e.Name(), ".jsonl")
		if !isSession {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return removed, alive, fmt.Errorf("stat %s: %w", e.Name(), err)
		}
		if id == cfg.Session || time.Since(info.ModTime()) < cfg.KeepSessions { // --continue just picked it
			alive[id] = true
			continue
		}
		if err := os.Remove(filepath.Join(cfg.SessionDir, e.Name())); err != nil {
			return removed, alive, fmt.Errorf("remove %s: %w", e.Name(), err)
		}
		if err := os.RemoveAll(filepath.Join(filepath.Dir(cfg.ResultDir), id)); err != nil {
			return removed, alive, fmt.Errorf("remove the results of %s: %w", e.Name(), err)
		}
		removed++
	}
	return removed, alive, nil
}

// pruneOrphanResults removes result dirs whose conversation is gone: nothing can name their ids.
func pruneOrphanResults(results string, alive map[string]bool) (int, error) {
	dirs, err := os.ReadDir(results)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("clean unreachable results in %s: %w", results, err)
	}
	removed := 0
	for _, d := range dirs {
		if !d.IsDir() || !strings.HasPrefix(d.Name(), "s") || alive[d.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(results, d.Name())); err != nil {
			return removed, fmt.Errorf("remove %s: %w", d.Name(), err)
		}
		removed++
	}
	return removed, nil
}
