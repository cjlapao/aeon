package main

// history_coverage_test.go — S53 T1's cross-package pin. coverage.floor.statement
// is S25's own P32 sentence, VERBATIM (HV1): the statement the History screen
// and history.coverage print must be the statement `aeon history`'s usage block
// already prints, byte for byte — not a re-phrasing that drifts one comma per
// release. The pin reads the CLI's own source (the file that holds the usage
// text) and demands it CONTAINs the constant internal/history serves, so the
// sentence is one string with two callers, exactly like every other
// one-formatter pair in this house.

import (
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/history"
)

func TestFloorStatementIsVerbatim(t *testing.T) {
	raw, err := os.ReadFile("history.go")
	if err != nil {
		t.Fatalf("read the history CLI source: %v", err)
	}
	usage := string(raw)
	if !strings.Contains(usage, history.FloorStatement) {
		t.Errorf("the usage block no longer contains internal/history's FloorStatement verbatim\n  (%q)\n— the sentence the coverage view states and the sentence `aeon history` prints are becoming two sentences", history.FloorStatement)
	}
}
