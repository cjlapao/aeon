package main

// S48 T8 — the slice's claims made mechanical, in the S45 T6 / S46 T7 / S47 T9
// method: sibling tests in this package, NO existing guard edited, each
// negative predicate canaried by TestBlueprintGuardsHaveTeeth. Seven guards:
// the human door (positive + six negative subtests), the one write path, the
// no-runtime-file-read rule, the append-only history, the surface
// classification, the one validator — and the teeth.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3" // the raw handle the history drill needs (same CGO driver GORM opens)

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

var blueprintWriteMethods = []protocol.MethodName{
	protocol.MethodBlueprintSave, protocol.MethodBlueprintDuplicate,
	protocol.MethodBlueprintDelete, protocol.MethodBlueprintRevert,
}

var blueprintReadMethods = []protocol.MethodName{
	protocol.MethodBlueprintUsage, protocol.MethodBlueprintHistory,
	protocol.MethodBlueprintRevision, protocol.MethodBlueprintDiff,
	protocol.MethodBlueprintLint,
}

// blueprintWriteNeedles is what must NEVER appear in the four non-person
// packages: the dotted wire names and their Go constant spellings (T0's
// inventory: the constants are how a smuggler reaches the table without
// spelling the dot).
var blueprintWriteNeedles = []string{
	`"blueprint.save"`, `"blueprint.duplicate"`, `"blueprint.delete"`, `"blueprint.revert"`,
	"MethodBlueprintSave", "MethodBlueprintDuplicate", "MethodBlueprintDelete", "MethodBlueprintRevert",
}

// blueprintWriteToolNames are the tool names a blueprint write must never
// wear in the registry.
var blueprintWriteToolNames = []string{"blueprint_save", "blueprint_delete", "blueprint_revert", "blueprint_duplicate"}

// blueprintProposeTwins names the records-catalog specs that may twin to a
// blueprint WRITE method. Empty today (T8 lands before T10); T10 adds the
// one propose-only twin here by NAME — an unlisted twin is an offense, and
// the canary feeds exactly that shape to prove the predicate has teeth.
var blueprintProposeTwins = map[string]bool{
	// S48 T10 (2026-11-03): the ONE propose-only twin, registered by name
	// as the comment required. It twins to blueprint.propose (a new method,
	// not a write door), files the INERT blueprint_save row, and remains
	// the family's sole catalog Mutates:true spec — any OTHER blueprint
	// spec twinned to a write method is still an offense.
	"blueprint_propose": true,
}

func blueprintGuardDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return daemon.New(daemon.Options{Store: st})
}

// ── guard 1: the human door, positive and six ways negative ───────────────────

func TestBlueprintWritesAreAHumanDoor(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, m := range blueprintWriteMethods {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a wired daemon — the person's door vanished", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize — a write on an unnegotiated connection is unthinkable", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — the human-door exclusion must never rest on a flag", m)
		}
	}
	// `save` keeps TODAY's registration (the spec's pin): registered, gated,
	// not Debug — the same three answers T0 recorded, unchanged by S48.
	if _, ok := d.Table.Lookup(protocol.MethodBlueprintSave); !ok {
		t.Fatal("blueprint.save lost its registration")
	}

	// The absent half (V13): unwired seams leave the four ABSENT, not refusing.
	bare := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if bare.RegisterBlueprintManage() {
		t.Fatal("RegisterBlueprintManage succeeded on the unwired table — registered-and-refusing is V13's forbidden shape")
	}
	// The THREE NEW ones are the V13 half; `save` keeps today's registration
	// (the spec's pin) — it has been unconditionally registered since T0.
	for _, m := range []protocol.MethodName{protocol.MethodBlueprintDuplicate, protocol.MethodBlueprintDelete, protocol.MethodBlueprintRevert} {
		if _, ok := bare.Lookup(m); ok {
			t.Errorf("%s is registered without the write service", m)
		}
		if _, err := bare.Dispatch(context.Background(), "c-bp-absent", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Errorf("%s answered %v, want ErrUnknownMethod", m, err)
		}
	}
	if _, ok := bare.Lookup(protocol.MethodBlueprintSave); !ok {
		t.Error("blueprint.save lost today's unconditional registration — the write doors are new, the save door is old")
	}

	rootsFour := []string{
		filepath.Join("..", "..", "internal", "narrow"),
		filepath.Join("..", "..", "internal", "agentsvc"),
		filepath.Join("..", "..", "internal", "tools"),
		filepath.Join("..", "..", "internal", "records"),
	}

	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		if bad := blueprintWritesOnNarrow(actions); len(bad) != 0 {
			t.Errorf("the narrow surface projects blueprint writes through %s — a key-holder projected onto the write path is BP2's refusal made mechanical", strings.Join(bad, ", "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := blueprintWritesOnAgentServices(ms); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying a blueprint write is the side door BP2 keeps shut", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		if bad := blueprintWriteToolsInRegistry(reg); len(bad) != 0 {
			t.Errorf("the tool registry carries %s — no agent holds a blueprint write tool; the only blueprint-family twins are T10's read and propose specs", strings.Join(bad, ", "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := blueprintCatalogWriteTwins(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — a blueprint write method has no agent-reachable twin except the ONE named propose-only spec", strings.Join(bad, "\n"))
		}
	})

	t.Run("SourceLiterals", func(t *testing.T) {
		bad := mcpLiteralGrep(rootsFour, blueprintWriteNeedles, nil)
		if len(bad) == 0 {
			if seen := walkedSources(rootsFour, nil); len(seen) == 0 {
				t.Fatal("the walk saw no non-test files — the grep checked nothing")
			}
		} else {
			t.Errorf("the four non-person packages name the write vocabulary: %s", strings.Join(bad, "\n"))
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		if bad := blueprintNamesInBoundary(stmt); len(bad) != 0 {
			t.Errorf("the projection's rendered boundary statement lists %s — the surface DISPLAYS a door its table must not hold", strings.Join(bad, ", "))
		}
	})
}

func blueprintWritesOnNarrow(actions []narrow.Action) []string {
	var bad []string
	want := map[protocol.MethodName]bool{}
	for _, m := range blueprintWriteMethods {
		want[m] = true
	}
	for _, a := range actions {
		if want[a.SocketMethod] {
			bad = append(bad, a.Name+"→"+string(a.SocketMethod))
		}
	}
	return bad
}

func blueprintWritesOnAgentServices(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	want := map[protocol.MethodName]bool{}
	for _, m := range blueprintWriteMethods {
		want[m] = true
	}
	for _, m := range ms {
		if want[m.Name] {
			bad = append(bad, string(m.Name))
		}
	}
	return bad
}

func blueprintWriteToolsInRegistry(reg map[string]tools.Tool) []string {
	var bad []string
	for name := range reg {
		for _, w := range blueprintWriteToolNames {
			if name == w {
				bad = append(bad, name)
			}
		}
	}
	return bad
}

func blueprintCatalogWriteTwins(specs []records.Spec) []string {
	var bad []string
	want := map[string]bool{}
	for _, m := range blueprintWriteMethods {
		want[string(m)] = true
	}
	for _, s := range specs {
		if want[s.Method] && !blueprintProposeTwins[s.Name] {
			bad = append(bad, s.Name+" twin of "+s.Method)
		}
	}
	return bad
}

func blueprintNamesInBoundary(stmt string) []string {
	var bad []string
	lower := strings.ToLower(stmt)
	for _, m := range blueprintWriteMethods {
		if strings.Contains(lower, strings.ToLower(string(m))) {
			bad = append(bad, string(m))
		}
	}
	return bad
}

// ── guard 2: the one write path ────────────────────────────────────────────────

// blueprintWritePathAllowed names every non-test file allowed to reach the
// blueprint rows: the two store files holding the single write path's SQL
// (the S48 T1 pair — the spec named blueprint.go; T1's SQL lives in the
// blueprint_records.go beside it, recorded as a deviation), the daemon's
// write service, the seed applier, and the adoption importer.
var blueprintWritePathAllowed = map[string]bool{
	"internal/store/blueprint.go":         true,
	"internal/store/blueprint_records.go": true,
	"internal/daemon/blueprint_write.go":  true,
	"internal/blueprintseed/applier.go":   true,
	"cmd/aeon/blueprint_adopt.go":         true,
}

var blueprintWritePathNeedles = []string{
	"store.BlueprintPutTx(", "store.BlueprintDeleteTx(",
	"INSERT INTO blueprint (", "UPDATE blueprint ", "DELETE FROM blueprint ",
}

func TestBlueprintRowsAreWrittenOnlyThroughTheOneWritePath(t *testing.T) {
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	// The walk must have SEEN the allowed files, or the allow-list could be
	// satisfied by their absence.
	for named := range blueprintWritePathAllowed {
		found := false
		for path := range files {
			if strings.HasSuffix(filepath.ToSlash(path), named) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the walk never saw %s — the allow-list is asserting nothing about a file it cannot find", named)
		}
	}
	if bad := blueprintWritePathOffenses(files); len(bad) != 0 {
		t.Errorf("blueprint rows are reached outside the one write path:\n%s", strings.Join(bad, "\n"))
	}
}

func blueprintWritePathOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		slash := filepath.ToSlash(path)
		allowed := false
		for named := range blueprintWritePathAllowed {
			if strings.HasSuffix(slash, named) {
				allowed = true
				break
			}
		}
		if allowed {
			continue
		}
		for _, needle := range blueprintWritePathNeedles {
			if strings.Contains(src, needle) {
				bad = append(bad, slash+" calls the write path directly ("+needle+")")
			}
		}
	}
	return bad
}

// ── guard 3: no runtime read of a blueprint file ───────────────────────────────

// blueprintFileReadAllowed names every non-test file allowed to touch the
// legacy <store>/blueprints/ mirror: the definition site in the blueprint
// package, the adoption importer — and doctor.go, a recorded deviation: the
// CLI reporting door LISTS the mirror to say it is a mirror (the mirror
// rule is that anything rendered is regenerable; a report that cannot see
// the mirror cannot report drift). The daemon tree (internal/daemon,
// internal/runtime) carries ZERO of these needles — that is the runtime half.
var blueprintFileReadAllowed = map[string]bool{
	"internal/blueprint/standard.go": true,
	"cmd/aeon/blueprint_adopt.go":    true,
	"cmd/aeon/doctor.go":             true,
	// The legacy `path` verb, still standing until T11 retires it —
	// T11 MUST drop this line, and the guard then holds the spec's
	// exact allow-list. The runtime half below never exempted it.
	"cmd/aeon/blueprint.go": true,
}

var blueprintFileReadNeedles = []string{
	"LegacyResolve(", "LegacyNamedPath(", "LegacyNames(",
	`filepath.Join(dir, "blueprints"`,
}

// blueprintRuntimeRoots are the trees where NO blueprint-file symbol may
// appear in non-test code at all — the runtime never reads what the rows hold.
var blueprintRuntimeRoots = []string{
	filepath.Join("..", "..", "internal", "daemon"),
	filepath.Join("..", "..", "internal", "runtime"),
}

func TestNoRuntimeReadOfABlueprintFile(t *testing.T) {
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	if bad := blueprintFileReadOffenses(files); len(bad) != 0 {
		t.Errorf("blueprint files are read outside the named doors:\n%s", strings.Join(bad, "\n"))
	}
	// The runtime half, strictly: the daemon and the runtime do not even
	// name the mirror directory.
	for _, root := range blueprintRuntimeRoots {
		for path, src := range walkedSources([]string{root}, nil) {
			if strings.Contains(src, `"blueprints"`) || strings.Contains(src, "LegacyResolve(") ||
				strings.Contains(src, "LegacyNamedPath(") || strings.Contains(src, "LegacyNames(") {
				t.Errorf("%s — the runtime reads what the rows hold (BP5: the store is the source of truth; the files are a disposable mirror)", filepath.ToSlash(path))
			}
		}
	}
	// UseEmbeddedForOfflineTools callers ⊆ the named CLI verbs (+ the two
	// definition sites it is defined at).
	for path, src := range files {
		called := false
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue // prose about the swap is not a call of it
			}
			if strings.Contains(trimmed, "UseEmbeddedForOfflineTools()") {
				called = true
			}
		}
		if !called {
			continue
		}
		slash := filepath.ToSlash(path)
		switch {
		case strings.HasSuffix(slash, "cmd/aeon/blueprint.go"): // the named verb
		case strings.HasSuffix(slash, "internal/blueprint/source.go"),
			strings.HasSuffix(slash, "internal/handler/source.go"): // the definitions
		case strings.HasSuffix(slash, "internal/daemon/blueprint_source.go"):
			// the ONE installer's re-arm (T3's recorded rule: last installer
			// wins; production installs exactly once — the swap back to
			// embedded before SetSource is what keeps many-test processes honest)
		default:
			t.Errorf("%s calls UseEmbeddedForOfflineTools — only the named CLI verbs swap the source (T4's rule)", slash)
		}
	}
}

func blueprintFileReadOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		slash := filepath.ToSlash(path)
		allowed := false
		for named := range blueprintFileReadAllowed {
			if strings.HasSuffix(slash, named) {
				allowed = true
				break
			}
		}
		if allowed {
			continue
		}
		for _, needle := range blueprintFileReadNeedles {
			if strings.Contains(src, needle) {
				bad = append(bad, slash+" reads blueprint files ("+needle+")")
			}
		}
	}
	return bad
}

// ── guard 4: the history is never rewritten ────────────────────────────────────

var blueprintHistoryRewrite = regexp.MustCompile(`(?i)(UPDATE\s+blueprint_revision|DELETE\s+FROM\s+blueprint_revision)`)

func TestBlueprintHistoryIsNeverRewritten(t *testing.T) {
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	if bad := blueprintHistoryOffenses(files); len(bad) != 0 {
		t.Errorf("non-test code rewrites the append-only history:\n%s", strings.Join(bad, "\n"))
	}
	// The trigger is present in the shipped DDL (BP7's teeth at the schema
	// level) — the migration file itself is the one source.
	ddl, err := os.ReadFile(filepath.Join("..", "..", "internal", "store", "migrations", "20261102153836_blueprint.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"blueprint_revision_no_update", "blueprint_revision_no_delete", "RAISE(ABORT"} {
		if !strings.Contains(string(ddl), want) {
			t.Errorf("the blueprint migration lost %q — the append-only claim rests on those triggers", want)
		}
	}
	// And the triggers have teeth on a migrated database: a raw UPDATE and a
	// raw DELETE are both refused at the SQL level.
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "state.db")
	st.Close()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO blueprint (name, version, content, content_hash, origin, created_at, updated_at, updated_by)
		VALUES ('guard-four', '1.0.0', 'id: guard-four', '0000000000000000000000000000000000000000000000000000000000000000', 'user', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'user:1')`); err != nil {
		t.Fatalf("seeding the history row failed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO blueprint_revision (name, rev, version, op, note, actor_kind, actor_id, content, content_hash, occurred_at)
		VALUES ('guard-four', 1, '1.0.0', 'create', '', 'user', '1', 'id: guard-four', '0000000000000000000000000000000000000000000000000000000000000000', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seeding the history row failed: %v", err)
	}
	if _, err := db.Exec(`UPDATE blueprint_revision SET note = 'rewritten' WHERE name = 'guard-four'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE blueprint_revision: want the append-only refusal, got %v", err)
	}
	if _, err := db.Exec(`DELETE FROM blueprint_revision WHERE name = 'guard-four'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE FROM blueprint_revision: want the append-only refusal, got %v", err)
	}
}

func blueprintHistoryOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if strings.HasSuffix(filepath.ToSlash(path), "internal/store/migrations/20261102153836_blueprint.sql") {
			continue // the triggers' own DDL names what it refuses
		}
		if blueprintHistoryRewrite.MatchString(src) {
			bad = append(bad, filepath.ToSlash(path)+" rewrites blueprint_revision")
		}
	}
	return bad
}

// ── guard 5: every blueprint write is classified as a write (S47 is merged) ───

func TestEveryBlueprintWriteMethodIsClassifiedAsAWrite(t *testing.T) {
	d := blueprintGuardDaemon(t)
	byName := map[protocol.MethodName]protocol.SurfaceEntry{}
	for _, e := range d.Table.Surface() {
		byName[e.Method] = e
	}
	for _, m := range blueprintWriteMethods {
		e, ok := byName[m]
		if !ok {
			t.Fatalf("%s is absent from the surface answer of a fully wired daemon", m)
		}
		if e.Kind != protocol.SurfaceWrite {
			t.Errorf("%s is classified %q — a write that reads like a read is how BP2 leaks", m, e.Kind)
		}
		if !e.HumanDoorOnly {
			t.Errorf("%s is not marked humanDoorOnly — the classification must claim what the guards prove", m)
		}
	}
	for _, m := range blueprintReadMethods {
		e, ok := byName[m]
		if !ok {
			t.Fatalf("%s is absent from the surface answer of a fully wired daemon", m)
		}
		if e.Kind != protocol.SurfaceRead {
			t.Errorf("%s is classified %q — it reads and must say so", m, e.Kind)
		}
	}
}

// ── guard 6: Analyze is the only blueprint validator ──────────────────────────

// blueprintAnalyzeAllowed names every non-test file that may call the one
// validator: the daemon's validate seam, its read service (show's report),
// its write path, and the two CLI doors (the validate verb, the importer).
var blueprintAnalyzeAllowed = map[string]bool{
	// S48 T11 (BP12): the validate seam moved OUT of the daemon's blueprint
	// wiring into blueprint_doors.go, so the CLI's offline door and the
	// socket door answer from ONE implementation over their own source. The
	// entry follows the seam; it is a door that relocated, not a judgment
	// that multiplied — the exactly-one-call-site count below is untouched.
	"internal/daemon/blueprint_doors.go": true, // the validate seam (T11)
	"internal/daemon/blueprint_read.go":  true, // show's byte-equal report
	"internal/daemon/blueprint_write.go": true, // the write path's one judgment
	"cmd/aeon/blueprint.go":              true, // the validate verb
	"cmd/aeon/blueprint_adopt.go":        true, // the importer's candidate check
	// S48 T9 (2026-11-03): the blueprint_save proposal seam — it RE-RUNS
	// the write path's judgment (the same Analyze, the same standard-mode
	// options) so the create door and the commit door refuse alike (TD3).
	// It is a caller of the one validator, never a second validator: the
	// judgment below still pins blueprint_write.go's call-site count at
	// exactly one, and this entry adds a door, not a judgment.
	"cmd/aeon/blueprint_proposal_seams.go": true,
	// S49 T3 (2026-11-03, named here 2026-11-03 as a fix-forward — the door
	// landed with the S49 merge, this map never followed it): the automation
	// health read runs the STORE ROW through the same Analyze to answer
	// "is the blueprint this automation points at still valid" — a reader
	// of the one judgment, the blueprint_read.go shape over a different
	// caller. Adds a door, not a judgment.
	"internal/daemon/automation_health.go": true,
	// S49 T4 (2026-11-03): trigger.update's door re-runs the SAME
	// judgment so a row edited toward a broken blueprint refuses before the
	// write, exactly as a start would — a caller of the one validator, never
	// a second validator (the health line above is the same shape).
	"internal/daemon/automation_write.go": true, // the update door's second opinion (T4)
	// S50 T3 (2026-11-04, RM3): the roadmap dispatch resolves its blueprint
	// SETTING by reading the named row through the same Analyze (the same
	// standard-mode options) before anything starts or pins — a caller of
	// the one validator, never a second validator, and its refusal is the
	// point: a choice that fails the judgment names its setting and starts
	// nothing. Adds a door, not a judgment.
	"internal/daemon/roadmap_settings.go": true,
}

func TestAnalyzeIsTheOnlyBlueprintValidator(t *testing.T) {
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	if bad := blueprintValidatorOffenses(files); len(bad) != 0 {
		t.Errorf("a second blueprint validator appeared:\n%s", strings.Join(bad, "\n"))
	}
	// "Exactly once per accepted write" — the write path holds a single
	// call site; a second judgment in the save flow would be a second truth.
	once := 0
	for path, src := range files {
		if strings.HasSuffix(filepath.ToSlash(path), "internal/daemon/blueprint_write.go") {
			once = strings.Count(src, "blueprint.Analyze(")
		}
	}
	if once != 1 {
		t.Errorf("the write path calls Analyze %d times — exactly one judgment per accepted write (the runtime-spy form the spec sketch suggested has no DI seam on Analyze; the count of the one call site plus the daemon's save tests pin the same fact)", once)
	}
}

func blueprintValidatorOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		slash := filepath.ToSlash(path)
		if strings.HasSuffix(slash, "internal/blueprint/analyze.go") {
			continue // the one validator's definition
		}
		allowed := false
		for named := range blueprintAnalyzeAllowed {
			if strings.HasSuffix(slash, named) {
				allowed = true
				break
			}
		}
		if !allowed && strings.Contains(src, "blueprint.Analyze(") {
			bad = append(bad, slash+" calls Analyze — outside the named doors a second caller is a second judgment")
		}
		if !strings.Contains(slash, "internal/blueprint/") && strings.Contains(src, "blueprint.Report{") {
			bad = append(bad, slash+" constructs a blueprint.Report — only the blueprint package produces reports; everyone else carries Analyze's")
		}
	}
	return bad
}

// ── guard 7: the teeth ─────────────────────────────────────────────────────────

func TestBlueprintGuardsHaveTeeth(t *testing.T) {
	t.Run("NarrowCanary", func(t *testing.T) {
		bad := blueprintWritesOnNarrow([]narrow.Action{{Name: "aeon_blueprint_save", SocketMethod: protocol.MethodBlueprintSave}})
		if len(bad) != 1 {
			t.Fatal("the narrow predicate let a projected blueprint.save through — the guard is decorative")
		}
	})
	t.Run("AgentServicesCanary", func(t *testing.T) {
		bad := blueprintWritesOnAgentServices([]protocol.AgentServiceMethod{{Name: protocol.MethodBlueprintDelete}})
		if len(bad) != 1 {
			t.Fatal("the enumeration predicate let blueprint.delete through")
		}
	})
	t.Run("ToolRegistryCanary", func(t *testing.T) {
		bad := blueprintWriteToolsInRegistry(map[string]tools.Tool{"blueprint_revert": tools.Read()})
		if len(bad) != 1 {
			t.Fatal("the registry predicate let blueprint_revert through")
		}
	})
	t.Run("CatalogCanary", func(t *testing.T) {
		bad := blueprintCatalogWriteTwins([]records.Spec{{Name: "blueprint_delete_now", Method: "blueprint.delete", Family: "blueprint", Mutates: true}})
		if len(bad) != 1 {
			t.Fatal("the catalog predicate let a spec twinned to blueprint.delete through")
		}
	})
	t.Run("LiteralCanary", func(t *testing.T) {
		bad := mcpLiteralGrep(nil, blueprintWriteNeedles, map[string]string{
			filepath.Join("..", "..", "internal", "tools", "fake_smuggled.go"): `package tools // "blueprint.revert" smuggled in`,
		})
		if len(bad) != 1 {
			t.Fatal("the literal grep missed the planted needle")
		}
	})
	t.Run("WritePathCanary", func(t *testing.T) {
		bad := blueprintWritePathOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "runtime", "fake_put.go"): `package runtime // store.BlueprintPutTx(tx, row, rev, refs)`,
		})
		if len(bad) != 1 {
			t.Fatal("the write-path predicate let a fake BlueprintPutTx caller in internal/runtime through")
		}
	})
	t.Run("FileReadCanary", func(t *testing.T) {
		bad := blueprintFileReadOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "daemon", "fake_read.go"): `package daemon // raw, _ := os.ReadFile(filepath.Join(dir, "blueprints", name))`,
		})
		if len(bad) != 1 {
			t.Fatal("the file-read predicate let a fake blueprints-dir read in internal/daemon through")
		}
	})
	t.Run("HistoryCanary", func(t *testing.T) {
		bad := blueprintHistoryOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "store", "fake_hist.go"): `package store // db.Exec("UPDATE blueprint_revision SET note = 'x'")`,
		})
		if len(bad) != 1 {
			t.Fatal("the history predicate let a fake UPDATE blueprint_revision through")
		}
	})
	t.Run("ValidatorCanary", func(t *testing.T) {
		bad := blueprintValidatorOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "runtime", "fake_validate.go"): `package runtime // r := blueprint.Analyze(raw, opts)`,
		})
		if len(bad) != 1 {
			t.Fatal("the validator predicate let a second Analyze caller through")
		}
		bad2 := blueprintValidatorOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "daemon", "fake_report.go"): `package daemon // return blueprint.Report{Valid: true}`,
		})
		if len(bad2) != 1 {
			t.Fatal("the validator predicate let a second Report producer through")
		}
	})
}
