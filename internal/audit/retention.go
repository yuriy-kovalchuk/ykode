package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PruneOldFiles deletes monthly files wholly older than keep. The current month is never
// touched, and neither is a file whose name is not a month.
func PruneOldFiles(dir string, keep time.Duration) (int, error) {
	if dir == "" {
		return 0, nil
	}
	oldest := time.Now().Add(-keep)
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, path := range names {
		month, err := time.Parse("2006-01", strings.TrimSuffix(filepath.Base(path), ".jsonl"))
		if err != nil || month.After(oldest) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("audit: %w", err)
		}
		removed++
	}
	return removed, nil
}
