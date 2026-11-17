package main

// # The S31 guards (T9)
//
// Two claims the slice ships on, machine-pinned BEFORE anything can
// accidentally break them:
//
//   - **LW8** — terminals and steers belong to the person on the socket.
//     The narrow HTTP surface (S19's projection) may never carry a
//     `terminal.*` or `step.steer*` method: a browser token buying a shell
//     or a steer is not a projection of the socket, it is a different
//     product with different rules. S19 is unbuilt, so this grep pairs
//     with the pins exactly as `TestNoPolicyAuthoringReachesTheNarrowSurface`
//     does: the method-name string literals may exist only in the two
//     files that OWN them (`internal/protocol/method.go`, the one method
//     table, and `internal/terminal/terminal.go`, the two notification
//     constants). The day S19 adds a handler that spells a terminal
//     method — dotted or quoted — it fails here first. The pin it leans
//     on: `ForceOff("api")` in `internal/bootstrap/register.go` (S1's
//     `TestAPIIsPinnedOff` keeps the pin itself honest).
//   - **I6** — `PROTOCOL_VERSION` stays 1: every addition in S31 is an
//     optional field or a new method, which is the definition of a
//     non-breaking change. A bump here means someone broke forward
//     compatibility rather than remembering to update a constant.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestNoTerminalOrSteerMethodReachesAnyButTheOneTable(t *testing.T) {
	// The method names, quoted exactly as they cross the wire. The steer
	// set deliberately excludes the EVENT kinds (step.steer.queued /
	// .cancelled / .delivered, step.tool.skipped, step.route.changed):
	// those are stream vocabulary the renderer prints, not surface a
	// projection can expose. `"step.steer.cancel"` closes its quote right
	// after `cancel`, so `step.steer.cancelled` cannot match.
	terminalMethodRef := regexp.MustCompile(`"terminal\.(open|input|resize|attach|detach|close|list|output|exit)"`)
	steerMethodRef := regexp.MustCompile(`"step\.steer(\.cancel|s)?"`)

	// The two owners. A third file spelling these names is a second copy
	// of the method table — or a projection — and both are the thing LW8
	// refuses.
	allowed := map[string]bool{
		"internal/protocol/method.go":   true, // the ONE method table
		"internal/terminal/terminal.go": true, // the two notification constants
	}

	var walked, offenders int
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs (dist/, node_modules) hold no Go code we own
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := strings.TrimPrefix(filepath.ToSlash(path), "../../")
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		walked++
		if allowed[rel] {
			return nil
		}
		if terminalMethodRef.Match(src) || steerMethodRef.Match(src) {
			t.Errorf("%s names a terminal or steer method on the wire: LW8 keeps them on the socket's "+
				"one table (internal/protocol/method.go) — a narrow-surface or second-table appearance "+
				"is the ruling refused, not a detail", rel)
			offenders++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The walk must actually have walked: a guard that matched zero files
	// guards zero claims.
	if walked < 100 {
		t.Fatalf("the walk reached %d files; it is not reaching the tree", walked)
	}
	if offenders > 0 {
		t.Fatalf("%d offending file(s) named above", offenders)
	}
}

func TestProtocolVersionStaysOne(t *testing.T) {
	// The history this pin carries: S31's argument (optional fields, new
	// methods, additive kinds) was all ADDITIVE and left version 1 at 1.
	// S38 T2 moved it to 2 against the BREAKING column of the rule beside the
	// constant — OL1 renamed `project.*` methods to `component.*` and renamed
	// `projectRoot`→`componentRoot` across the scope-carrying params and
	// results; nothing shipped version 1, so MinSupportedVersion moved with
	// it. A later bump past 2 belongs in a slice that argues it, not here.
	if protocol.PROTOCOL_VERSION != 2 {
		t.Fatalf("PROTOCOL_VERSION = %d; S31's additions were additive and S38 T2's rename is the "+
			"argued bump (design.md OL1, version.go's comment) — anything beyond 2 means something "+
			"stopped being additive without an argument", protocol.PROTOCOL_VERSION)
	}
}
