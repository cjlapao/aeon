package main

// roadmap_guard_test.go — S50 T4 (P4): the roadmap's security and honesty
// claims made mechanical, in the mould of the automation (AN), settings (CS)
// and blueprint (BP) guards in this directory. Positive halves first,
// negatives one subtest per non-human door, every guard with a canary that
// proves it can FAIL, and the synthetic-violation test at the end. No
// existing guard body is edited here, and no guard passes on an empty walk.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/tools"
)

// roadmapWrites are the eight person doors — S26's seven plus T2's update.
// RM4's claim rides guard 1: dispatch and editing are a person's act.
var roadmapWrites = []protocol.MethodName{
	protocol.MethodRoadmapAdd, protocol.MethodRoadmapUpdate, protocol.MethodRoadmapCancel,
	protocol.MethodRoadmapEstimate, protocol.MethodRoadmapPriority,
	protocol.MethodRoadmapIdeate, protocol.MethodRoadmapPlan, protocol.MethodRoadmapMaterialize,
}

// roadmapReads are the six reads, classified in guard 6 (RM6): reads may
// reach the agent later (T5); the classification row must already say so.
var roadmapReads = []protocol.MethodName{
	protocol.MethodRoadmapList, protocol.MethodRoadmapShow, protocol.MethodRoadmapBoard,
	protocol.MethodRoadmapHistory, protocol.MethodRoadmapVocabulary, protocol.MethodRoadmapHealth,
}

// roadmapGraphWrites are the graph's two edges — S27 T3's amendment,
// kept a SEPARATE list from the ladder's sealed eight precisely because
// the two planes now differ in one provable way: the edges have named
// catalog twins (roadmap_depend, roadmap_undepend — registered, never
// default-granted), the ladder has none and never will (RM8 stands whole
// for status, priority, estimate and the dispatches). 2026-11-05 (S27 T3,
// P6's note: direct, widens nothing — the twin answers the SAME service
// the person's door answers through).
var roadmapGraphWrites = []protocol.MethodName{
	protocol.MethodRoadmapDepend, protocol.MethodRoadmapUndepend,
}

// roadmapGraphTwins is the closed set of catalog tools the graph edges may
// have. A third name twinned to roadmap.depend/roadmap.undepend is refused
// by TestRoadmapGraphEdgesAreAHumanDoor, and neither twin may grow.
var roadmapGraphTwins = map[string]protocol.MethodName{
	"roadmap_depend":   protocol.MethodRoadmapDepend,
	"roadmap_undepend": protocol.MethodRoadmapUndepend,
}

func roadmapWriteSet() map[protocol.MethodName]bool {
	set := map[protocol.MethodName]bool{}
	for _, m := range roadmapWrites {
		set[m] = true
	}
	return set
}

// ── guard 1: the human door, positive and five ways negative ──────────────

func TestRoadmapWritesAreAHumanDoor(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, m := range roadmapWrites {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a fully wired daemon — the app would have no door", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize — a roadmap write may not precede negotiation", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — a roadmap write is a product method, not a diagnostic", m)
		}
	}

	t.Run("narrow", func(t *testing.T) {
		for _, bad := range narrowRoadmapOffenses(narrow.Actions()) {
			t.Error(bad)
		}
		s := narrow.BoundaryStatement()
		for _, m := range roadmapWrites {
			if strings.Contains(s, string(m)) {
				t.Errorf("the boundary statement names %q — the statement accounts for what is ABSENT, and a roadmap write named there would claim a projection", m)
			}
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		writes := roadmapWriteSet()
		for _, m := range protocol.AgentServiceMethods() {
			if writes[m.Name] {
				t.Errorf("%s is an agent-service method — roadmap dispatch and editing are a person's act (RM4)", m.Name)
			}
		}
	})

	t.Run("tools-registry", func(t *testing.T) {
		var names []string
		for name := range tools.Registry() {
			names = append(names, name)
		}
		for _, bad := range toolsRoadmapWriteOffenses(names) {
			t.Error(bad)
		}
	})

	t.Run("records-catalog", func(t *testing.T) {
		for _, bad := range recordsRoadmapWriteOffenses(records.Catalog()) {
			t.Error(bad)
		}
	})

	t.Run("door-package-text", func(t *testing.T) {
		roots := []string{
			filepath.Join("..", "..", "internal", "narrow"),
			filepath.Join("..", "..", "internal", "agentsvc"),
			filepath.Join("..", "..", "internal", "tools"),
			filepath.Join("..", "..", "internal", "records"),
		}
		files := walkedSources(roots, nil)
		if len(files) < 10 {
			t.Fatalf("the walk saw %d files — a grep that reads nothing passes forever", len(files))
		}
		for _, bad := range roadmapTextOffenses(files) {
			t.Error(bad)
		}
	})
}

func narrowRoadmapOffenses(actions []narrow.Action) []string {
	var bad []string
	writes := roadmapWriteSet()
	for _, a := range actions {
		if writes[a.SocketMethod] {
			bad = append(bad, "action "+a.Name+" projects the person write "+string(a.SocketMethod)+" (RM4)")
		}
		for _, w := range narrow.ForbiddenOperationalControl {
			if strings.Contains(strings.ToLower(a.Name), w) && strings.HasPrefix(string(a.SocketMethod), "roadmap.") {
				bad = append(bad, "action "+a.Name+" projects "+string(a.SocketMethod)+" under the forbidden word "+w+" (S19 R6)")
			}
		}
	}
	return bad
}

// roadmapWriteToolName is guard 1's tool-registry predicate. T5's tools
// (roadmap_board, roadmap_history, roadmap_show, … and the one propose-only
// roadmap_idea) match none of these WRITE verbs by design: the reads are
// reads and the idea is a proposal — which is exactly what "propose" says
// the tool never is.
var roadmapWriteToolName = regexp.MustCompile(`^roadmap_(add|update|cancel|estimate|priority|ideate|plan|materialize)$`)

func toolsRoadmapWriteOffenses(names []string) []string {
	var bad []string
	for _, name := range names {
		if roadmapWriteToolName.MatchString(name) {
			bad = append(bad, "tool "+name+" is named after a roadmap write — a second door onto the person's act (RM4)")
		}
	}
	return bad
}

func recordsRoadmapWriteOffenses(specs []records.Spec) []string {
	var bad []string
	writes := roadmapWriteSet()
	for _, spec := range specs {
		if writes[protocol.MethodName(spec.Method)] {
			// The ONE exemption (T5's design): an inert PROPOSAL filing
			// twin — a name containing "propose", never the write itself.
			if strings.Contains(spec.Name, "propose") {
				continue
			}
			bad = append(bad, "catalog spec "+spec.Name+" twins the person write "+spec.Method+" (RM4)")
		}
	}
	return bad
}

// roadmapLiterals is every way a write door's name can appear in source:
// the wire literal and its Go constant.
var roadmapLiterals = func() [][2]string {
	out := make([][2]string, 0, len(roadmapWrites))
	for _, m := range roadmapWrites {
		lit := string(m)
		camel := ""
		for _, part := range strings.Split(lit, ".") {
			camel += strings.ToUpper(part[:1]) + part[1:]
		}
		out = append(out, [2]string{lit, "Method" + camel})
	}
	return out
}()

func roadmapTextOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		for _, lit := range roadmapLiterals {
			if strings.Contains(src, `"`+lit[0]+`"`) || strings.Contains(src, lit[1]) {
				bad = append(bad, filepath.ToSlash(path)+" names the roadmap write "+lit[0]+" — the four door packages must not even speak of it (RM4)")
			}
		}
	}
	return bad
}

// TestRoadmapGraphEdgesAreAHumanDoor is S27 T3's amendment to guard 1:
// the two graph doors are person doors in exactly the shape the eight
// ladder writes are — registered on a wired daemon, RequiresInitialize,
// never Debug, absent from the narrow projection, absent from the
// agent-services enumeration — and they are the ONLY roadmap plane writes
// with catalog twins, pinned closed to the two names here while the
// ladder's eight stay absent from the catalog and the registry whole
// (guard 1 above owns that half and its regexes were NOT widened).
func TestRoadmapGraphEdgesAreAHumanDoor(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, m := range roadmapGraphWrites {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a fully wired daemon — the graph would have no door", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize — an edge may not be authored before negotiation", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — an edge is a product fact, not a diagnostic", m)
		}
	}

	t.Run("narrow", func(t *testing.T) {
		for _, a := range narrow.Actions() {
			for _, m := range roadmapGraphWrites {
				if a.SocketMethod == m {
					t.Errorf("the narrow projection carries %s — an edge is a person's ruling on intent, RM4's shape widened to the graph", m)
				}
			}
		}
		s := narrow.BoundaryStatement()
		for _, m := range roadmapGraphWrites {
			if strings.Contains(s, string(m)) {
				t.Errorf("the boundary statement names %q — the statement accounts for what is ABSENT, and an edge named there would claim a projection", m)
			}
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		graph := map[protocol.MethodName]bool{}
		for _, m := range roadmapGraphWrites {
			graph[m] = true
		}
		for _, m := range protocol.AgentServiceMethods() {
			if graph[m.Name] {
				t.Errorf("%s is an agent-service method — the machine's standing surface does not author edges", m.Name)
			}
		}
	})

	t.Run("catalog-twins-are-exactly-the-two", func(t *testing.T) {
		seen := map[string]protocol.MethodName{}
		for _, sp := range records.Catalog() {
			for _, m := range roadmapGraphWrites {
				if protocol.MethodName(sp.Method) == m {
					seen[sp.Name] = m
				}
			}
		}
		if len(seen) != len(roadmapGraphTwins) {
			t.Errorf("the graph's doors carry %d catalog twins, want exactly %d — roadmap.depend/roadmap.undepend reach agents through roadmap_depend/roadmap_undepend and nothing else (S27 T3)", len(seen), len(roadmapGraphTwins))
		}
		for name, m := range roadmapGraphTwins {
			if got, ok := seen[name]; !ok {
				t.Errorf("%s (twinned to %s) is missing from the catalog — the T3 spec's tool is not there", name, m)
			} else if got != m {
				t.Errorf("%s twinned to %s, want %s — the dot-name IS the person's door (rule 4)", name, got, m)
			}
		}
	})

	t.Run("classified", func(t *testing.T) {
		for _, m := range roadmapGraphWrites {
			s, ok := protocol.SurfaceClassified(m)
			if !ok {
				t.Errorf("%s is unclassified — the surface table must answer for every roadmap door (S47)", m)
				continue
			}
			if s.Kind != protocol.SurfaceWrite {
				t.Errorf("%s is classified %q — an edge writes a row and must say so", m, s.Kind)
			}
			// The honest inversion: NOT humanDoorOnly, because the two named
			// twins above reach these doors through the tool door — an
			// absence claim here would be false (MethodTriageFileAs's shape).
			if s.HumanDoorOnly {
				t.Errorf("%s claims humanDoorOnly while roadmap_depend/roadmap_undepend are its catalog twins — the classification would be lying", m)
			}
		}
	})

	t.Run("ladder-stays-out", func(t *testing.T) {
		// The amendment must not have softened RM8: the sealed eight reach
		// neither the catalog nor the registry, regexes still unmoved.
		for _, bad := range recordsRoadmapWriteOffenses(records.Catalog()) {
			t.Error(bad)
		}
		var names []string
		for name := range tools.Registry() {
			names = append(names, name)
		}
		for _, bad := range toolsRoadmapWriteOffenses(names) {
			t.Error(bad)
		}
	})
}

// ── guard 2: rows only through the door ────────────────────────────────────

// roadmapRowSQL sees every way a roadmap row gets written: the two tables'
// INSERT and UPDATE. (The append-only trigger lives in the migration; guard
// 4 owns the DELETE/UPDATE side of roadmap_change.)
var roadmapRowSQL = regexp.MustCompile(`(?i)(INSERT INTO|UPDATE)\s+roadmap_(item|change)\b`)

// roadmapStoreWriters are the *store.Store write methods, listed by name
// (T1/T2's audited set plus S26's originals): whatever touches a roadmap
// row touches it through one of these, and only internal/roadmap's service
// may call them. The reactor and the dispatch call the SERVICE
// (roadmap.Roadmap's TransitionAudited & co), not these — which is what the
// walk here proves, file by file.
var roadmapStoreWriters = regexp.MustCompile(`\.(CreateRoadmapItemAudited|CreateRoadmapItem|UpdateRoadmapItemAudited|TransitionRoadmapItemAudited|TransitionRoadmapItem|RoadmapItemLandingAudited|SetRoadmapItemEstimateAudited|SetRoadmapItemEstimate|SetRoadmapItemLoopAudited|SetRoadmapItemLoop|SetRoadmapItemPriorityAudited|SetRoadmapItemPriority|SetRoadmapItemArtifacts)\(`)

func roadmapRowWriteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		rel := strings.TrimPrefix(p, "../../internal/")
		inStore := strings.HasPrefix(rel, "store/")
		inService := strings.HasPrefix(rel, "roadmap/")
		if roadmapRowSQL.MatchString(src) && !inStore {
			bad = append(bad, p+" writes roadmap rows with its own SQL — roadmap_item/roadmap_change are written only through the store writers (RM4)")
		}
		if m := roadmapStoreWriters.FindString(src); m != "" && !inStore && !inService {
			bad = append(bad, p+" calls the store writer "+m+" directly — the daemon's dispatch and the reactor call the door's SERVICE, never the writer (RM4)")
		}
	}
	return bad
}

func TestRoadmapRowsAreWrittenOnlyThroughTheDoor(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range roadmapRowWriteOffenses(files) {
		t.Error(bad)
	}
}

// ── guard 3: the lifecycle machine is the machine T0 recorded ──────────────

// roadmapTransitionsHash pins the eleven edges T0 walked — golden by test
// (the rendered, sorted `from->to` lines), not by source text. Any edge
// added, removed or moved changes the digest.
const roadmapTransitionsHash = "945ffea76eb41e20162428c083636248143641f896a96b3e0f5e0dc978899118"

func roadmapTransitionsDigest(table map[roadmap.Status][]roadmap.Status) string {
	var lines []string
	for from, tos := range table {
		for _, to := range tos {
			lines = append(lines, string(from)+"->"+string(to))
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func TestNoLifecycleEdgeWasAdded(t *testing.T) {
	table := roadmap.AllowedTransitions()
	if got := roadmapTransitionsDigest(table); got != roadmapTransitionsHash {
		t.Errorf("the transition table digest is %s, want %s — S50 manages the roadmap's words and paths, never its edges", got, roadmapTransitionsHash)
	}
	if res := roadmap.ValidateTransition(roadmap.StatusRunning, roadmap.StatusCancelled); res.OK {
		t.Error("running → cancelled validated — a running materialization is cancelled by the person, not the machine (S26 R6)")
	}
	for _, term := range []roadmap.Status{roadmap.StatusDone, roadmap.StatusCancelled} {
		if out := table[term]; len(out) != 0 {
			t.Errorf("%s has out-edges %v — terminal is terminal", term, out)
		}
	}
}

// ── guard 4: the audit is append-only; nothing deletes a roadmap row ──────

var roadmapDeleteSQL = regexp.MustCompile(`(?i)DELETE\s+FROM\s+roadmap_(item|change)\b|UPDATE\s+roadmap_change\b`)

func roadmapDeleteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if m := roadmapDeleteSQL.FindString(src); m != "" {
			bad = append(bad, filepath.ToSlash(path)+" contains "+m+" — roadmap_change is append-only and roadmap rows are cancelled, never deleted (RM2/RM7)")
		}
	}
	return bad
}

func TestRoadmapChangeIsAppendOnly(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range roadmapDeleteOffenses(files) {
		t.Error(bad)
	}
}

func TestNoRoadmapDeletePathExists(t *testing.T) {
	// The migrations themselves: the trigger that REFUSES delete/update on
	// roadmap_change must exist, and no migration may ever have carried a
	// DELETE FROM roadmap_*.
	matches, err := filepath.Glob(filepath.Join("..", "..", "internal", "store", "migrations", "*.sql"))
	if err != nil || len(matches) < 40 {
		t.Fatalf("the migration walk saw %d files (err %v) — no conclusion without the full set", len(matches), err)
	}
	sawRefusalTrigger := false
	for _, m := range matches {
		src, rerr := os.ReadFile(m)
		if rerr != nil {
			t.Fatalf("read %s: %v", m, rerr)
		}
		text := string(src)
		if regexp.MustCompile(`(?i)DELETE\s+FROM\s+roadmap_`).MatchString(text) {
			t.Errorf("%s deletes roadmap rows — no migration ever may (RM7)", filepath.Base(m))
		}
		if strings.Contains(text, "roadmap_change") && strings.Contains(text, "RAISE(ABORT") {
			sawRefusalTrigger = true
		}
	}
	if !sawRefusalTrigger {
		t.Error("no migration carries the roadmap_change append-only refusal trigger — the DB promise was never made")
	}
}

// ── guard 5: dispatch never falls back to a named blueprint ───────────────

// roadmapFallback walks the ROADMAP dispatch plane (daemon/roadmap*.go,
// non-test) — the settings declaration (internal/config) and the seed are
// the only spellings of the two default names. The triage slice's own
// `brainstorm` const is S51's pre-existing fact, not this slice's road.
var roadmapFallback = regexp.MustCompile(`"(brainstorm|roadmap-plan)"`)

func roadmapFallbackOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		base := filepath.Base(p)
		if !strings.HasPrefix(base, "roadmap") || !strings.Contains(p, "/daemon/") {
			continue
		}
		if m := roadmapFallback.FindString(src); m != "" {
			bad = append(bad, p+" carries the literal "+m+" — the blueprint choices live in the settings declaration's DEFAULTS, and a dispatch that spells one is a fallback waiting to fire (RM3)")
		}
	}
	return bad
}

func TestDispatchNeverFallsBackToADefaultBlueprint(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal", "daemon")}, nil)
	seen := 0
	for path := range files {
		if strings.HasPrefix(filepath.Base(path), "roadmap") {
			seen++
		}
	}
	if seen < 3 {
		t.Fatalf("the walk saw %d roadmap dispatch files — too few to conclude anything", seen)
	}
	for _, bad := range roadmapFallbackOffenses(files) {
		t.Error(bad)
	}
}

// ── guard 6: every method is classified (S47) ─────────────────────────────

func TestEveryRoadmapMethodIsClassified(t *testing.T) {
	for _, m := range roadmapWrites {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Errorf("%s is unclassified — the surface table must answer for every roadmap door (S47)", m)
			continue
		}
		if s.Kind != protocol.SurfaceWrite && s.Kind != protocol.SurfaceControl {
			t.Errorf("%s is classified %q — a roadmap write is write or control; a write that reads like a READ is how RM4 leaks", m, s.Kind)
		}
		if !s.HumanDoorOnly {
			t.Errorf("%s is not humanDoorOnly — the classification must claim what guard 1 proves", m)
		}
	}
	for _, m := range roadmapReads {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Errorf("%s is unclassified", m)
			continue
		}
		if s.Kind != protocol.SurfaceRead {
			t.Errorf("%s is classified %q — it reads and must say so", m, s.Kind)
		}
	}
}

// ── guard 7: the guards have teeth ────────────────────────────────────────

func TestRoadmapGuardsHaveTeeth(t *testing.T) {
	t.Run("narrow", func(t *testing.T) {
		fake := []narrow.Action{{Name: "roadmap_add", SocketMethod: protocol.MethodRoadmapAdd}}
		if len(narrowRoadmapOffenses(fake)) == 0 {
			t.Error("a fake narrow action projecting roadmap.add passed the narrow guard")
		}
	})
	t.Run("tools", func(t *testing.T) {
		fake := []string{"roadmap_materialize"}
		if len(toolsRoadmapWriteOffenses(fake)) == 0 {
			t.Error("a fake tool named after a roadmap write passed the tool guard")
		}
		fakeRead := []string{"roadmap_board", "roadmap_idea"}
		if len(toolsRoadmapWriteOffenses(fakeRead)) != 0 {
			t.Error("the guard failed even T5's READ names — the predicate is over-broad and will fail T5")
		}
	})
	t.Run("records", func(t *testing.T) {
		fake := []records.Spec{{Name: "roadmap_update", Method: "roadmap.update", Mutates: true}}
		if len(recordsRoadmapWriteOffenses(fake)) == 0 {
			t.Error("a fake catalog spec twinned to roadmap.update passed the catalog guard")
		}
		fakePropose := []records.Spec{{Name: "roadmap_propose_idea", Method: "roadmap.add", Mutates: true}}
		if len(recordsRoadmapWriteOffenses(fakePropose)) != 0 {
			t.Error("the guard failed even the LISTED propose-twin exemption — the predicate is over-broad and will fail T5")
		}
	})
	t.Run("door-text", func(t *testing.T) {
		fake := map[string]string{"../../internal/tools/fake.go": `const x = "roadmap.materialize"`}
		if len(roadmapTextOffenses(fake)) == 0 {
			t.Error("a fake file under internal/tools naming roadmap.materialize passed the text guard")
		}
	})
	t.Run("row-writer", func(t *testing.T) {
		fake := map[string]string{"../../internal/runtime/fake.go": "UPDATE roadmap_item SET status = 'done'"}
		if len(roadmapRowWriteOffenses(fake)) == 0 {
			t.Error("a fake UPDATE roadmap_item in internal/runtime passed the one-road guard")
		}
		fakeWriter := map[string]string{"../../internal/daemon/fake.go": "s.UpdateRoadmapItemAudited(ctx, id, f, a)"}
		if len(roadmapRowWriteOffenses(fakeWriter)) == 0 {
			t.Error("a fake direct caller of the store writer outside the service passed")
		}
	})
	t.Run("delete", func(t *testing.T) {
		fake := map[string]string{"../../internal/store/fake.go": "DELETE FROM roadmap_change WHERE item_id = 7"}
		if len(roadmapDeleteOffenses(fake)) == 0 {
			t.Error("a fake DELETE FROM roadmap_change passed the append-only guard")
		}
	})
	t.Run("fallback", func(t *testing.T) {
		fake := map[string]string{"../../internal/daemon/roadmap_sneak.go": `if name == "" { name = "brainstorm" }`}
		if len(roadmapFallbackOffenses(fake)) == 0 {
			t.Error("a fake literal \"brainstorm\" in the roadmap dispatch passed the no-fallback guard")
		}
	})
	t.Run("edges", func(t *testing.T) {
		fake := map[roadmap.Status][]roadmap.Status{
			roadmap.StatusRunning: {roadmap.StatusDone, roadmap.StatusCancelled},
		}
		if roadmapTransitionsDigest(fake) == roadmapTransitionsHash {
			t.Error("a tampered transition table produced the pinned digest — the hash guard is blind")
		}
	})
}
