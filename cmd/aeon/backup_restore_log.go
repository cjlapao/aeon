package main

// backup_restore_log.go — T7's restore log (BK9 step 8):
// <state dir>/restore-<ts>.log, beside the pre-restore dir — the record of
// the attempt, where the undo lives. Plain text, one line per step:
// timestamp, step, fact, tab-separated.
//
// The honesty guard: the log carries basenames and counts and nothing
// else — never a path, never row content, never a secret. So the writer
// screens every line, and the file it writes contains no / at all: a line
// that would carry a path is a refusal, not a redaction.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
)

// logTimeFormat is the instant on a log line and in the file name: UTC,
// the same shape the pre-restore dir carries, no colon, no slash.
const logTimeFormat = "20060102T150405"

// writeRestoreLog renders plan.Log to <stateDir>/restore-<ts>.log (0644)
// and returns the full path. It refuses a step or a fact that carries a
// /, a \ or a newline — the log names its own closed step set and
// basenames only — so the file it writes can contain no path.
func writeRestoreLog(stateDir string, plan backup.RestorePlan, at time.Time) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "restore %s\n", filepath.Base(plan.BackupPath))
	for _, e := range plan.Log {
		if strings.ContainsAny(e.Step, "/\\\n") {
			return "", fmt.Errorf("restore log refused: step %q carries a path or a newline — the log names its own steps only", e.Step)
		}
		if strings.ContainsAny(e.Fact, "/\\\n") {
			return "", fmt.Errorf("restore log refused: the %s fact carries a path or a newline — facts are basenames and counts only", e.Step)
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\n", e.At.UTC().Format("2006-01-02T15:04:05Z"), e.Step, e.Fact)
	}
	name := "restore-" + at.UTC().Format(logTimeFormat) + "Z.log"
	full := filepath.Join(stateDir, name)
	if err := os.WriteFile(full, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("the restore log will not be written: %w", err)
	}
	return full, nil
}
