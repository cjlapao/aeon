// gate_reactor_test.go — S57 T7's daemon half, at the reactor's real
// door: the subscription sits on the terminal fan-out (the one point a
// mid-rework red never reaches), the reason door takes the engine's
// shape and rejects the T1 golden's, the filing dedupes per subject
// through the real ledger and stamps the store's defaults, and the
// watch's forge is the merge action's forge — one factory, one identity.

package daemon

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/gatewatch"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/triage"
)

// productionCallSites walks the production tree (the package's parent,
// the graph-service guard's own scope) and names every file whose source
// carries the needle — the widening-guard idiom this package already
// holds.
func productionCallSites(t *testing.T, needle string) []string {
	t.Helper()
	var sites []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(src), needle) {
			sites = append(sites, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sites
}

// settle is the reactor's goroutine window: the dispatch is a goroutine
// by design (a gate reaction must not delay the finished step), so a
// negative assertion waits for it to have had its chance.
const settle = 300 * time.Millisecond

// runGit runs one git command in dir and returns its combined output.
func runGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *gateReactorFixture) gateFilings(t *testing.T) int {
	t.Helper()
	all, err := f.s.ListGateFilings()
	if err != nil {
		t.Fatalf("read the filing ledger: %v", err)
	}
	return len(all)
}

func (f *gateReactorFixture) bugs(t *testing.T) []triage.Bug {
	t.Helper()
	bugs, err := f.d.Table.Triage.List(f.root, triage.ListFilter{})
	if err != nil {
		t.Fatalf("list the bugs: %v", err)
	}
	return bugs
}

// emitBlocked fires the door with the engine's own shape: the reason is
// a *BlockedReason, exactly what blockTo puts on the payload.
func emitBlocked(f *gateReactorFixture, loopID string, reason loop.BlockedReason) {
	f.d.publishLoop(protocol.KindLoopBlocked, map[string]any{
		"loopId": loopID,
		"status": "blocked",
		"reason": &reason,
	})
}

// TestGateReactorIsOnTheTerminalFanOut — the door's placement, pinned at
// the source: exactly one production call site, in the file the engine's
// terminal emission lands in, strictly after the triage terminal and
// before the closing publish. The order is part of the contract — the
// gate reactor must see the same fields the triage reactor did, and the
// wire must close the fan-out after it.
func TestGateReactorIsOnTheTerminalFanOut(t *testing.T) {
	sites := productionCallSites(t, "d.onGateTerminal(")
	if len(sites) != 1 || !strings.HasSuffix(sites[0], "blueprint.go") {
		t.Fatalf("the reactor's production call sites = %v, want exactly one, in blueprint.go — the terminal fan-out is the door", sites)
	}
	src, err := os.ReadFile("blueprint.go")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	at := func(needle string) int {
		for i, l := range lines {
			if strings.Contains(l, needle) {
				return i
			}
		}
		return -1
	}
	triageAt := at("d.onTriageTerminal(")
	gateAt := at("d.onGateTerminal(")
	publishAt := -1
	for i := gateAt + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "d.publish(") {
			publishAt = i
			break
		}
	}
	if triageAt == -1 || gateAt == -1 || gateAt <= triageAt || publishAt == -1 || publishAt < gateAt {
		t.Errorf("the fan-out's order is triage@%d, gate@%d, publish@%d — the gate reactor sits strictly between the triage terminal and the closing publish", triageAt, gateAt, publishAt)
	}
}

// TestMidReworkRedFilesNothing — the reactor's discrimination, at the
// real door: a mid-rework red is not a terminal with a gate reason, so
// nothing on the mid-rework path files — no reason, a string reason (the
// T1 golden's shape), or a non-gate blocked reason all stand with zero
// filings. The gate terminals file, and file once.
func TestMidReworkRedFilesNothing(t *testing.T) {
	t.Run("a finished loop files nothing", func(t *testing.T) {
		f := newGateReactorFixture(t)
		id := f.pinLoop(t, "gr-done")
		f.d.publishLoop(protocol.KindLoopFinished, map[string]any{"loopId": id, "status": "finished"})
		time.Sleep(settle)
		if n, b := f.gateFilings(t), len(f.bugs(t)); n != 0 || b != 0 {
			t.Errorf("a finished loop filed: %d filings, %d bugs — a finished loop is not a gate failure", n, b)
		}
	})

	t.Run("a string reason files nothing (the T1 golden shape)", func(t *testing.T) {
		f := newGateReactorFixture(t)
		id := f.pinLoop(t, "gr-string")
		f.emit(id, "blocked", "verify failed on the gate") // the golden's string door
		time.Sleep(settle)
		if n, b := f.gateFilings(t), len(f.bugs(t)); n != 0 || b != 0 {
			t.Errorf("a string reason filed: %d filings, %d bugs — the string is the golden's shape, not a gate failure", n, b)
		}
	})

	t.Run("a non-gate blocked reason files nothing", func(t *testing.T) {
		f := newGateReactorFixture(t)
		id := f.pinLoop(t, "gr-other")
		emitBlocked(f, id, loop.BlockedReason{Kind: "cap-hit", Detail: "the round cap stood"})
		time.Sleep(settle)
		if n, b := f.gateFilings(t), len(f.bugs(t)); n != 0 || b != 0 {
			t.Errorf("a cap-hit terminal filed: %d filings, %d bugs — the discrimination is closed to the gate kinds", n, b)
		}
	})

	t.Run("the verify-failed terminal files exactly one bug", func(t *testing.T) {
		f := newGateReactorFixture(t)
		id := f.pinLoop(t, "gr-red")
		emitBlocked(f, id, loop.BlockedReason{Kind: "verify-failed", Detail: "the gate row failed"})
		await(t, "the gate filing to stand", func() bool {
			return f.gateFilings(t) == 1 && len(f.bugs(t)) == 1
		})
		bug := f.bugs(t)[0]
		if bug.Title != "Gate red: verify failed on gr" {
			t.Errorf("title = %q, want the fixed template with the component's slug", bug.Title)
		}
	})

	t.Run("the exhaustion terminal files the exhausted bug", func(t *testing.T) {
		f := newGateReactorFixture(t)
		id := f.pinLoop(t, "gr-exhausted")
		emitBlocked(f, id, loop.BlockedReason{Kind: "exhaustion", Action: action.VerifyRun, Detail: "the rework round failed a third time"})
		await(t, "the gate filing to stand", func() bool {
			return f.gateFilings(t) == 1 && len(f.bugs(t)) == 1
		})
		bug := f.bugs(t)[0]
		if bug.Title != "Gate red: verify exhausted on gr" {
			t.Errorf("title = %q, want the exhaustion template (the rework gave up, not the gate)", bug.Title)
		}
	})
}

// TestFilingIsDedupedPerSubject — the VG16 cycle at the daemon's own
// door: the first filing of a subject files the bug and stamps the claim;
// a repeat of the same subject files NOTHING (the bug already stands)
// and only bumps the count. A different subject stands on its own.
func TestFilingIsDedupedPerSubject(t *testing.T) {
	f := newGateReactorFixture(t)
	subject := gatewatch.SubjectVerify(f.root)
	facts := gatewatch.Facts{Kind: gatewatch.KindVerify, ComponentRoot: f.root, LoopSlug: "gr-red", Summary: "1 failed, 2 passed (3 gates)"}

	bugID, filed, err := f.d.fileGateBug(context.Background(), gatewatch.KindVerify, subject, f.root, facts)
	if err != nil || !filed || bugID == 0 {
		t.Fatalf("first filing: id=%d filed=%v err=%v, want a new bug stamped", bugID, filed, err)
	}
	againID, againFiled, err := f.d.fileGateBug(context.Background(), gatewatch.KindVerify, subject, f.root, facts)
	if err != nil || againFiled || againID != 0 {
		t.Fatalf("second filing: id=%d filed=%v err=%v, want nothing filed (the bug already stands)", againID, againFiled, err)
	}
	if bugs := f.bugs(t); len(bugs) != 1 {
		t.Fatalf("bugs after two filings of one subject: %d, want exactly one", len(bugs))
	}
	g, err := f.s.GateFilingBySubject(subject)
	if err != nil {
		t.Fatalf("read the claim: %v", err)
	}
	if g.Count != 2 || g.BugID == nil || *g.BugID != bugID {
		t.Errorf("claim = count %d, bug %v — want the count bumped twice and the first bug named", g.Count, g.BugID)
	}

	// A different subject (the refusal's, per root plus PR) files its own.
	refusal := gatewatch.SubjectMergeRefused(f.root, 7)
	id2, filed2, err := f.d.fileGateBug(context.Background(), gatewatch.KindMergeRefused, refusal, f.root,
		gatewatch.Facts{Kind: gatewatch.KindMergeRefused, ComponentRoot: f.root, PR: 7, CheckName: "ci/build", Detail: "the check is not green"})
	if err != nil || !filed2 || id2 == 0 || id2 == bugID {
		t.Fatalf("the second subject: id=%d filed=%v — a new subject files its own bug", id2, filed2)
	}
	if bugs := f.bugs(t); len(bugs) != 2 {
		t.Fatalf("bugs after the second subject: %d, want two (one per subject)", len(bugs))
	}
}

// TestFiledBugGetsStoreDefaults — the filing passes no label and no
// validation: the triage door's defaults stand (unvalidated, priority
// unknown, no label), and the bug's author is the gate itself.
func TestFiledBugGetsStoreDefaults(t *testing.T) {
	f := newGateReactorFixture(t)
	subject := gatewatch.SubjectVerify(f.root)
	bugID, filed, err := f.d.fileGateBug(context.Background(), gatewatch.KindVerify, subject, f.root,
		gatewatch.Facts{Kind: gatewatch.KindVerify, ComponentRoot: f.root, LoopSlug: "gr-red"})
	if err != nil || !filed || bugID == 0 {
		t.Fatalf("the filing: id=%d filed=%v err=%v", bugID, filed, err)
	}
	bug, ok, err := f.d.Table.Triage.Show(bugID)
	if err != nil || !ok {
		t.Fatalf("read the filed bug: ok=%v err=%v", ok, err)
	}
	if bug.Validation != triage.ValidationUnvalidated {
		t.Errorf("validation = %q, want the store's default (the gate files unvalidated)", bug.Validation)
	}
	if bug.PriorityState != triage.PriorityUnknown || bug.PriorityLabel != "" {
		t.Errorf("priority = %q / %q, want unknown with no label — the filing passes no label (D2)", bug.PriorityState, bug.PriorityLabel)
	}
	if bug.FiledBy != gatewatch.FiledBy {
		t.Errorf("filedBy = %q, want %q — the gate is the author", bug.FiledBy, gatewatch.FiledBy)
	}
	if bug.ComponentRoot != f.root {
		t.Errorf("componentRoot = %q, want the loop's root", bug.ComponentRoot)
	}
}

// TestWatcherUsesTheSameForgeAsTheMergeAction — the watch must forge
// through the identity the merge action did (VG5): the wiring's source
// pin (the Forge slot is the daemon's factory, in exactly one
// production place) plus the behavioural agreement — both doors answer
// the same root the same way, nil for a non-GitHub root, a forge for a
// GitHub one.
func TestWatcherUsesTheSameForgeAsTheMergeAction(t *testing.T) {
	f := newGateReactorFixture(t)
	if f.d.gateWatcher == nil {
		t.Fatal("the watcher is not built on a daemon with a store — the merge watch is a daemon service")
	}
	sites := productionCallSites(t, "Forge:    d.forgeClient")
	if len(sites) != 1 || !strings.HasSuffix(sites[0], "gatewatch_wiring.go") {
		t.Fatalf("the Forge slot's production sites = %v, want exactly one, in gatewatch_wiring.go — two factories could drift into two forges", sites)
	}

	// A root with no git origin: both doors answer nil (the outward half
	// is simply absent).
	plain := t.TempDir()
	if f.d.forgeClient(plain) != nil || f.d.gateWatcher.Forge(plain) != nil {
		t.Error("a non-GitHub root resolved a forge on one door but not the other — the identities have drifted")
	}

	// A root whose origin is a GitHub remote: both doors answer a forge.
	repo := t.TempDir()
	if out, err := runGit(t, repo, "init", "-q"); err != nil {
		t.Fatalf("git init the scenario repo: %v (%s)", err, out)
	}
	if out, err := runGit(t, repo, "remote", "add", "origin", "https://github.com/o/r.git"); err != nil {
		t.Fatalf("git remote add: %v (%s)", err, out)
	}
	if f.d.forgeClient(repo) == nil || f.d.gateWatcher.Forge(repo) == nil {
		t.Error("a GitHub root resolved nil on one door but not the other — the identities have drifted")
	}
}
