package main

// automation_guard_test.go — S49 T5 (AN2, AN3, AN6, AN14; sibling of S40's
// AU1 guards). The slice's security claims made mechanical, in the mould of
// the loop-control (P4), blueprint (BP12) and settings (CS9) guards in this
// directory: positive halves first, negatives one subtest per non-human
// door, every guard with a canary that proves it can FAIL. No existing guard
// body is edited here; the two AU1 bodies amended by S49 itself are pinned
// and LISTED below, and TestAU1OriginalGuardsAreByteIdentical makes the
// listing the build's fact rather than the review's memory.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// automationWrites is the eleven: S20's clock/webhook controls and add,
// T4's update and two archive doors, S40's event create/update/enable/
// disable — the whole of both families' write surface, one list.
var automationWrites = []protocol.MethodName{
	protocol.MethodTriggerAdd, protocol.MethodTriggerUpdate,
	protocol.MethodTriggerPause, protocol.MethodTriggerResume, protocol.MethodTriggerFireNow,
	protocol.MethodAutomationTriggerCreate, protocol.MethodAutomationTriggerUpdate,
	protocol.MethodAutomationTriggerEnable, protocol.MethodAutomationTriggerDisable,
	protocol.MethodAutomationArchive, protocol.MethodAutomationUnarchive,
}

// automationReads are T3's ten — the unified plane's reads, classified with
// the writes in guard 7 (AN7: no automation surface, read or write, belongs
// on a narrow or agent projection).
var automationReads = []protocol.MethodName{
	protocol.MethodAutomationList, protocol.MethodAutomationShow,
	protocol.MethodAutomationFires, protocol.MethodAutomationRuns,
	protocol.MethodAutomationHealth, protocol.MethodAutomationVocabulary,
	protocol.MethodAutomationPreview, protocol.MethodAutomationTest,
	protocol.MethodAutomationDryRun, protocol.MethodAutomationChanges,
}

func automationWriteSet() map[protocol.MethodName]bool {
	set := map[protocol.MethodName]bool{}
	for _, m := range automationWrites {
		set[m] = true
	}
	return set
}

// ── guard 1: the human door, positive and six ways negative ───────────────

// TestAutomationWritesAreAHumanDoorOnBothFamilies is the positive half (all
// eleven registered on a fully wired daemon, RequiresInitialize, none Debug
// — the app HAS its door, P34) and the negative half, one subtest per
// non-human door: the narrow projection and the boundary statement, the
// agent-services enumeration, the tool registry, the records catalog, and
// the text of the four packages that own those doors.
func TestAutomationWritesAreAHumanDoorOnBothFamilies(t *testing.T) {
	d := blueprintGuardDaemon(t) // the fully wired daemon (store, seams)
	for _, m := range automationWrites {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a fully wired daemon — the app would have no door", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize — an automation write may not precede negotiation", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — an automation write is a product method, not a diagnostic", m)
		}
	}

	t.Run("narrow", func(t *testing.T) {
		for _, bad := range narrowAutomationOffenses(narrow.Actions()) {
			t.Error(bad)
		}
		s := narrow.BoundaryStatement()
		for _, m := range automationWrites {
			if strings.Contains(s, string(m)) {
				t.Errorf("the boundary statement names %q — the statement is the surface's account of what is ABSENT", m)
			}
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		writes := automationWriteSet()
		for _, m := range protocol.AgentServiceMethods() {
			if writes[m.Name] {
				t.Errorf("%s is an agent-service method — automation writes are a person's act (AU1/AN1)", m.Name)
			}
		}
	})

	t.Run("tools-registry", func(t *testing.T) {
		var names []string
		for name := range tools.Registry() {
			names = append(names, name)
		}
		for _, bad := range toolsAutomationWriteOffenses(names) {
			t.Error(bad)
		}
	})

	t.Run("records-catalog", func(t *testing.T) {
		for _, bad := range recordsAutomationWriteOffenses(records.Catalog()) {
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
		for _, bad := range automationTextOffenses(files) {
			t.Error(bad)
		}
	})
}

// narrowAutomationOffenses is guard 1's narrow predicate over the action
// list — split from the collection so the canary can feed it fakes.
func narrowAutomationOffenses(actions []narrow.Action) []string {
	var bad []string
	writes := automationWriteSet()
	for _, a := range actions {
		if writes[a.SocketMethod] {
			bad = append(bad, "action "+a.Name+" projects the person write "+string(a.SocketMethod)+" (AU1/AN1)")
		}
		// S19's permanence, automation shape: no forbidden operational word
		// rides an automation method's projection either.
		for _, w := range narrow.ForbiddenOperationalControl {
			if strings.Contains(strings.ToLower(a.Name), w) && strings.HasPrefix(string(a.SocketMethod), "automation.") {
				bad = append(bad, "action "+a.Name+" projects "+string(a.SocketMethod)+" under the forbidden word "+w+" (S19 R6)")
			}
		}
	}
	return bad
}

// automationWriteToolName is guard 1's tool-registry predicate: no tool is
// named after an automation WRITE verb. T7's five tools (automation_list,
// automation_show, automation_fires, automation_health,
// automation_propose_disable) match none of these verbs — the reads are
// reads, and the disable is a PROPOSAL, which is what the word "propose"
// says the tool never is.
var automationWriteToolName = regexp.MustCompile(`^(trigger|automation)_(add|create|update|enable|disable|pause|resume|fire|firenow|archive|unarchive)$`)

func toolsAutomationWriteOffenses(names []string) []string {
	var bad []string
	for _, name := range names {
		if automationWriteToolName.MatchString(name) {
			bad = append(bad, "tool "+name+" is named after an automation write — a second door onto the person's act (AU1/AN1)")
		}
	}
	return bad
}

func recordsAutomationWriteOffenses(specs []records.Spec) []string {
	var bad []string
	writes := automationWriteSet()
	for _, spec := range specs {
		if writes[protocol.MethodName(spec.Method)] {
			// The ONLY exemption (T7's design): an inert PROPOSAL filing
			// twin — a name containing "propose", never the write itself.
			if strings.Contains(spec.Name, "propose") {
				continue
			}
			bad = append(bad, "catalog spec "+spec.Name+" twines the person write "+spec.Method+" (AU1/AN1)")
		}
	}
	return bad
}

// automationLiterals is every way a write door's name can appear in source:
// the wire literal and its Go constant.
var automationLiterals = func() [][2]string {
	out := make([][2]string, 0, len(automationWrites))
	for _, m := range automationWrites {
		lit := string(m)
		parts := strings.Split(lit, ".")
		camel := ""
		for _, p := range parts {
			camel += strings.ToUpper(p[:1]) + p[1:]
		}
		out = append(out, [2]string{lit, "Method" + camel})
	}
	return out
}()

func automationTextOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		for _, lit := range automationLiterals {
			if strings.Contains(src, `"`+lit[0]+`"`) || strings.Contains(src, lit[1]) {
				bad = append(bad, filepath.ToSlash(path)+" names the automation write "+lit[0]+" — the four door packages must not even speak of it (AN7)")
			}
		}
	}
	return bad
}

// ── guard 2: AU1's original bodies, pinned and listed ─────────────────────

// TestAU1OriginalGuardsAreByteIdentical re-verifies T0's digests over the
// SAME extraction methods T0 used (the function-body extract for the two
// test bodies; the forbidden-list block for the catalog guard), and lists —
// rather than hides — S49's two amendments to one of them.
//
// The listing: T0 pinned TestEveryTriggerWriteRefusesAnAgentBeforeTouchingT
// heStore at 423fb888825ae426a2382ead7752c248e567525c343f092a1f82b953707f386
// d (the tree at 25a304b0). T4 (commit e8e9a693) amended it ONCE, for the
// one reason the amendment language allows: the -31892/-31893 pair gives
// the two honest refusals their own codes, so the assertion names the codes
// instead of dressing them as ErrBadParams; the refusal the guard EXISTS for
// — refused FIRST, in the person sentence, the seam never called — is
// asserted to survive below, and the amended body is pinned to the byte.
// TestTheTriggerTableEnforcesItsShape and the catalog forbidden-list block
// were never touched: their T0 digests still hold byte for byte (T1's
// amendments to that FILE hit its other tests, not this body).
func TestAU1OriginalGuardsAreByteIdentical(t *testing.T) {
	originals := []struct {
		path, name string // name: "block:<start>" for a line block
		want       string
	}{
		{"../../internal/protocol/triggers_write_test.go", "TestEveryTriggerWriteRefusesAnAgentBeforeTouchingTheStore",
			"567239e863e5446e48522e179aeb02e2a90796b2fc8eb235f410ccd9953aa395"}, // amended by e8e9a693, listed above
		{"../../internal/store/automation_schema_test.go", "TestTheTriggerTableEnforcesItsShape",
			"7196e6af9f6c24f86333e71dc2c44a0fd3bfcf46af54018019485ae4a75d657c"},
	}
	for _, p := range originals {
		src, err := os.ReadFile(p.path)
		if err != nil {
			t.Fatalf("%s: %v", p.path, err)
		}
		body, err := extractFuncBody(string(src), p.name)
		if err != nil {
			t.Fatalf("%s: %v", p.path, err)
		}
		if got := shaHex([]byte(body)); got != p.want {
			t.Errorf("%s body of %s changed since the pin (want %s, got %s) — a deliberate amendment must be LISTED here, at the guard, and in the task Notes", p.path, p.name, p.want, got)
		}
	}
	// The refusal the pinned body exists for survives the amendment:
	// person-sentence first, and the codes are the named pair.
	locked, err := os.ReadFile("../../internal/protocol/triggers_write_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(locked), `"written by a person"`) ||
		!strings.Contains(string(locked), "ErrAutomationRefused") ||
		!strings.Contains(string(locked), "ErrAutomationNoActor") {
		t.Error("the AU1 socket lock no longer asserts the person sentence with the named codes — that is a relaxation, not an amendment")
	}
	// Lock 1: the catalog's forbidden-list block, byte for byte since T0.
	src, err := os.ReadFile("../../internal/records/catalog_test.go")
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "\tforbiddenWholeCatalog := []string{"
	i := strings.Index(string(src), anchor)
	if i < 0 {
		t.Fatal("the forbiddenWholeCatalog block is gone from catalog_test.go — lock 1 has no body left")
	}
	blk := string(src)[i:]
	if end := strings.Index(blk, "\n\t}\n"); end >= 0 {
		blk = blk[:end+len("\n\t}\n")]
	}
	if got := shaHex([]byte(blk)); got != "4c7600d5e4e3497b9480e27e1539711fc82c565ca35df900530c73e5541d900a" {
		t.Errorf("the catalog forbidden-list block changed since T0 (got %s) — the words decide/accept/approve/trigger/mcp are the whole-catalog ban; changing it is an amendment LISTED at this guard", got)
	}
}

// extractFuncBody slices `func NAME(` through the next line that is exactly
// "}" — the extraction T0 used for its digests.
func extractFuncBody(src, name string) (string, error) {
	i := strings.Index(src, "func "+name)
	if i < 0 {
		return "", errors.New("function " + name + " not found")
	}
	j := strings.Index(src[i:], "\n}\n")
	if j < 0 {
		return "", errors.New("function " + name + " is unterminated")
	}
	return src[i : i+j+3], nil
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ── guard 2b: the three locks, now proven for the S20 family ──────────────

// TestAU1LocksNowHoldForSchedules re-runs the three AU1 locks — no tool,
// the socket refusal, the store refusal before any SQL plus the DB CHECK —
// with the S20 schedule family as the subject. Before S49 they guarded
// event triggers only; AU1's whole claim is that the locks are one rule, so
// each one is now demonstrated on a schedule row too.
func TestAU1LocksNowHoldForSchedules(t *testing.T) {
	t.Run("lock1-no-tool-names-trigger", func(t *testing.T) {
		for name := range tools.Registry() {
			if strings.Contains(strings.ToLower(name), "trigger") {
				t.Errorf("tool %q names trigger — S19's whole-catalog forbidden word must not ride any tool (AU1 lock 1)", name)
			}
		}
	})

	t.Run("lock2-socket-refusal", func(t *testing.T) {
		fake := guardAutomationWrite{}
		mk := func(actor protocol.Actor) *protocol.Table {
			tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
			tbl.AutomationWrite = fake
			tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor { return actor }
			tbl.RegisterAutomationWrites()
			initializeForGuard(t, tbl, "c1") // the gate FIRST: a handshake, then the actor verdict
			return tbl
		}
		raw := json.RawMessage(`{"source":"schedule","id":"guard-row","reason":"guard"}`)
		cases := []struct {
			who  protocol.Actor
			want *protocol.Error
		}{
			{protocol.Actor{Kind: "user"}, protocol.ErrAutomationNoActor}, // a "person" nobody can resolve: the honest absence
			{protocol.Actor{Kind: "agent", ID: "brainstormer"}, protocol.ErrAutomationRefused}, // named non-person
		}
		for _, tc := range cases {
			tbl := mk(tc.who)
			for _, m := range []protocol.MethodName{protocol.MethodTriggerUpdate, protocol.MethodAutomationArchive} {
				_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
				var perr *protocol.Error
				if !errors.As(err, &perr) || perr.Code != tc.want.Code {
					t.Errorf("%s as %q: got %v, want %s", m, tc.who.Kind, err, tc.want)
					continue
				}
				if !strings.Contains(perr.Data.What, "written by a person") {
					t.Errorf("%s as %q: Data.What %q must carry the person sentence (AU1 lock 2)", m, tc.who.Kind, perr.Data.What)
				}
			}
		}
	})

	t.Run("lock3-store-refuses-before-SQL-and-the-CHECK-holds", func(t *testing.T) {
		dir := t.TempDir()
		st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close() // harmless twice; the raw-SQL proof below closes it first
		ctx := context.Background()
		row := store.TriggerScheduleRow{ID: "guard-lock3", Kind: "schedule", Blueprint: "guard", Expr: "0 7 * * *", Zone: "UTC"}
		for _, a := range []store.Actor{{Kind: "agent", ID: "brainstormer"}, {}} {
			if _, _, err := st.SaveTriggerScheduleAs(ctx, a, row, "update", "guard"); err == nil ||
				!strings.Contains(err.Error(), "written by a person") {
				t.Errorf("SaveTriggerScheduleAs as %+v: got %v, want the person refusal", a, err)
			}
		}
		// Raw SQL next: what the CHECK enforces no Go code needs to believe.
		// Close the store first and speak to the file directly, the way
		// the blueprint guard's append-only proof does.
		st.Close()
		db, err := sql.Open("sqlite3", filepath.Join(dir, "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM trigger_schedule WHERE id = 'guard-lock3'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("the refused write reached the row — the store refused AFTER SQL, which is what the lock forbids")
		}
		if _, err := db.Exec(`INSERT INTO trigger_schedule (id, kind, blueprint, zone, created_at, created_by_kind) VALUES ('guard-check','schedule','guard','UTC','2026-11-03T00:00:00Z','agent')`); err == nil ||
			!strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("the created_by_kind CHECK let an agent through (or is gone): %v", err)
		}
	})
}

type guardAutomationWrite struct{}

func (guardAutomationWrite) TriggerUpdate(context.Context, protocol.Actor, protocol.TriggerUpdateParams) (protocol.TriggerUpdateResult, error) {
	return protocol.TriggerUpdateResult{}, nil
}
func (guardAutomationWrite) Archive(context.Context, protocol.Actor, protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	return protocol.AutomationArchiveResult{}, nil
}
func (guardAutomationWrite) Unarchive(context.Context, protocol.Actor, protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	return protocol.AutomationArchiveResult{}, nil
}

// ── guard 3: triggers are never deleted ───────────────────────────────────

var triggerRowDelete = regexp.MustCompile(`DELETE FROM (trigger_schedule|event_trigger|trigger_fire|event_trigger_fire|automation_change)`)
var storeDeleterMethod = regexp.MustCompile(`func \(s \*Store\) [A-Za-z]*(Delete|Remove|Purge|Drop)[A-Za-z]*(Trigger|Automation|Schedule|Event)`)

func TestTriggersAreNeverDeleted(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range triggerDeleteOffenses(files) {
		t.Error(bad)
	}
	// The append-only promise is schema, not code: live proof on a real
	// migration-complete store — the audit table refuses UPDATE and DELETE
	// with its own sentence.
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	person := store.Actor{Kind: "user", ID: "guard"}
	if err := st.RecordAutomationAudit(context.Background(), person, store.AutomationChange{
		Source: "schedule", TriggerID: "guard-row", Op: "create", Summary: "the canary row",
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE automation_change SET summary = 'rewritten history' WHERE trigger_id = 'guard-row'`); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE automation_change: want the append-only refusal, got %v", err)
	}
	if _, err := db.Exec(`DELETE FROM automation_change WHERE trigger_id = 'guard-row'`); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE FROM automation_change: want the append-only refusal, got %v", err)
	}
}

func triggerDeleteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		if triggerRowDelete.MatchString(src) {
			bad = append(bad, p+" deletes an automation row — the tables retire through archive, never through DELETE (AN6)")
		}
		if storeDeleterMethod.MatchString(src) {
			bad = append(bad, p+" names a deleting store method over the trigger tables — there is no delete door and no delete accessor (AU4)")
		}
	}
	return bad
}

// ── guard 4: triggers are never seeded (AN2) ──────────────────────────────

var seedTriggerWrite = regexp.MustCompile(`INSERT INTO (trigger_schedule|event_trigger)`)

func TestTriggersAreNeverSeeded(t *testing.T) {
	reg := seed.DefaultRegistry()
	sets := reg.All()
	if len(sets) < 4 {
		t.Fatalf("the seed registry holds %d sets — the walk read almost nothing", len(sets))
	}
	for _, s := range sets {
		low := strings.ToLower(s.ID)
		if strings.Contains(low, "trigger") || strings.Contains(low, "automation") {
			t.Errorf("seed set %q is named after an automation — AN2: nothing seeds a trigger", s.ID)
		}
	}
	var seen int
	err := filepath.WalkDir(filepath.Join("..", "..", "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(filepath.ToSlash(path), "seed") ||
			!strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		seen++
		if seedTriggerWrite.MatchString(string(src)) {
			t.Errorf("%s seeds a trigger row — AN2: the one write service is the only writer, and a seed is not a person", filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 3 {
		t.Fatalf("the seed walk read %d files — a grep over nothing proves nothing", seen)
	}
}

// ── guard 5: the rows are written only through the one write service ─────

// automationWriterCalls names the actor-taking writers T4 introduced plus
// S40's event writers: the functions through which every automation row in
// the database must pass.
// The receiver names are the store handle (s/st/Store) — the SAME method
// names ride the trigger.Scheduler's HumanWriter assertion and the protocol
// seams, and those doors are other rules' business; this one watches the
// store's own writers.
var automationWriterCalls = regexp.MustCompile(`\b(s|st|Store)\.(SaveTriggerScheduleAs|ArchiveAutomation|UnarchiveAutomation|RecordAutomationAudit|CreateEventTrigger|UpdateEventTrigger|SetEventTriggerEnabled)\(`)

var automationRawTableWrite = regexp.MustCompile(`(INSERT INTO (trigger_schedule|event_trigger)|UPDATE (trigger_schedule|event_trigger)\b)`)

func TestTriggerRowsAreWrittenOnlyThroughTheOneWriteService(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range oneWriterOffenses(files) {
		t.Error(bad)
	}
}

func oneWriterOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := strings.TrimPrefix(filepath.ToSlash(path), "../../")
		switch {
		case strings.HasPrefix(p, "internal/store/"):
			// The store owns its own tables — writers called inside the
			// package (and its own SQL) are the one door, not a second one.
			continue
		case strings.HasPrefix(p, "internal/daemon/automation_write.go"),
			strings.HasPrefix(p, "cmd/aeon/trigger.go"):
			// The named write service, and the CLI door T5's spec names as
			// riding it. (The CLI dialled the socket at T0; the entry is
			// harmless if the file never calls — the walk sees either way.)
			continue
		}
		if automationWriterCalls.MatchString(src) {
			bad = append(bad, p+" calls a store trigger writer outside the one write service — the rule is one file, T4 (AN3: one stamp, one door)")
		}
		for _, line := range strings.Split(src, "\n") {
			if automationRawTableWrite.MatchString(line) {
				bad = append(bad, p+" writes an automation table with raw SQL outside internal/store — the schema is the store's to name")
				break
			}
		}
	}
	return bad
}

// ── guard 6: attendedness and outward are not moved ───────────────────────

// engineFiles are the byte-frozen engines (AN14), pinned at T0's digests —
// measured 2026-11-03 at 25a304b0 and unchanged through S49.
var engineFiles = map[string]string{
	"../../internal/trigger/clock.go":         "e88d1ddd2dbc3521e6a5819d6f40f5ef99f2426f40cb1784ada520a9333a2567",
	"../../internal/trigger/overlap.go":       "f9c86882d984d70f6342e14d168cf0ebbe694f74ebcfc220ce2ebb1b1b9aa0bd",
	"../../internal/trigger/reconcile.go":     "73e740fdb700e5df0e88bb943a237813680a9b9c26ca9beddb14f6cef7a7fc11",
	"../../internal/trigger/dst.go":           "304de8ccbd628f26b17aed0d3dd8746bd1cbf640e4d86360c25f0989a5335db9",
	"../../internal/automation/dispatcher.go": "6b46a1f68dbbf0108c7991b3f5f9f47334a751b703ccb3b290626bc050bf26b7",
	"../../internal/automation/starter.go":    "01aaa214d14ba9da5016ec13445088238dc1e8669e876bfdf5d91f3581fdea87",
	"../../internal/automation/outcome.go":    "5aeb87f0c2c904d96b57bd89df172608fd59adcb06554d9663aa97e70ba48c01",
}

func TestAttendednessAndOutwardAreNotMoved(t *testing.T) {
	for path, want := range engineFiles {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if got := shaHex(b); got != want {
			t.Errorf("%s changed hash (want %s, got %s) — the engines are byte-frozen under S49 (AN14); a change to one is the AN14 amendment ceremony, not an edit", path, want[:12], got[:12])
		}
	}
	// The S40 T4 pinned-row assertion (AU2/G14: an event start is
	// unattended, AllowOutward false) still asserts what T0 pinned.
	src, err := os.ReadFile("../../internal/daemon/automation_test.go")
	if err != nil {
		t.Fatal(err)
	}
	body, err := extractFuncBody(string(src), "TestAnApprovedGoalStartsAnUnattendedLoopEndToEnd")
	if err != nil {
		t.Fatal(err)
	}
	if got := shaHex([]byte(body)); got != "e2177c2169f4b7f6fd601f1b8a757115cf8c621992df35f6726aca0a71cf6c49" {
		t.Errorf("S40 T4's pinned-row assertion changed body (got %s) — amendments are LISTED at this guard", got)
	}
	if !strings.Contains(body, "AllowOutward") || !strings.Contains(body, "ttended") {
		t.Error("the pinned assertion no longer asserts attendedness/outward — a relaxation, not an amendment")
	}
	// The start doors' assignments: in both fire files every Attended
	// assignment is the locally declared false; no true rides either file.
	for _, name := range []string{"../../internal/daemon/trigger_fire.go", "../../internal/daemon/event_fire.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "Attended:") && !strings.Contains(line, "&attended") {
				t.Errorf("%s assigns Attended somewhere but &attended: %s", name, strings.TrimSpace(line))
			}
			if strings.Contains(line, "attended :=") && !strings.Contains(line, "attended := false") {
				t.Errorf("%s declares attended non-false: %s", name, strings.TrimSpace(line))
			}
		}
	}
}

// ── guard 7: every automation method is classified (S47 is merged) ───────

func TestEveryAutomationMethodIsClassified(t *testing.T) {
	for _, m := range automationWrites {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Errorf("%s is unclassified — the surface table must answer for every automation door (S47)", m)
			continue
		}
		if s.Kind != protocol.SurfaceWrite && s.Kind != protocol.SurfaceControl {
			t.Errorf("%s is classified %q — every automation write is write or control (the switch on the row the write made); a write that reads like a READ is how AU1 leaks", m, s.Kind)
		}
		if !s.HumanDoorOnly {
			t.Errorf("%s is not humanDoorOnly — the classification must claim what the guards here prove", m)
		}
	}
	for _, m := range automationReads {
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

// ── guard 8: no secret value can reach an automation output (C17) ────────

// automationPlaneFiles are the sources that answer automation doors; the
// engine's own single os.Getenv (the webhook HMAC, internal/trigger/
// webhook.go) is not on this plane and its own comment pins it.
func TestNoSecretValueCanReachAnyAutomationOutput(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "internal", "protocol"),
		filepath.Join("..", "..", "internal", "daemon"),
		filepath.Join("..", "..", "internal", "store"),
		filepath.Join("..", "..", "internal", "automationview"),
	}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range secretHygieneOffenses(files) {
		t.Error(bad)
	}
}

func automationPlane(rel string) bool {
	base := filepath.Base(rel)
	return (strings.HasPrefix(base, "automation") || strings.HasPrefix(base, "trigger")) &&
		strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go") &&
		!strings.Contains(rel, "webhook.go")
}

func secretHygieneOffenses(files map[string]string) []string {
	var bad []string
	seenHelper := 0
	for path, src := range files {
		p := filepath.ToSlash(path)
		if !automationPlane(p) {
			continue
		}
		if strings.Contains(src, "os.Getenv(") {
			bad = append(bad, p+" reads an env VALUE on the automation plane — the plane answers with NAMES and presence, never values (C17)")
		}
		if strings.Contains(src, "os.LookupEnv(") {
			if !strings.HasSuffix(p, "daemon/automation_read.go") {
				bad = append(bad, p+" consults the environment outside the single presence helper")
				continue
			}
			// The helper's body: presence only, returning a bool.
			i := strings.Index(src, "EnvSet: func(name string) bool {")
			if i < 0 {
				bad = append(bad, p+" calls LookupEnv but the EnvSet presence helper is gone or changed shape")
				continue
			}
			end := strings.Index(src[i:], "\n\t\t},")
			body := src[i : i+end]
			if !strings.Contains(body, "os.LookupEnv(") || !strings.Contains(body, "return ok") {
				bad = append(bad, p+" — the EnvSet helper no longer answers presence alone")
			}
			seenHelper++
		}
	}
	if seenHelper != 1 {
		bad = append(bad, "the single presence helper was seen "+strconv.Itoa(seenHelper)+" times — exactly one automation-plane file may consult the environment")
	}
	return bad
}

// ── guard 9: the guards have teeth ────────────────────────────────────────

func TestAutomationGuardsHaveTeeth(t *testing.T) {
	t.Run("narrow", func(t *testing.T) {
		fake := []narrow.Action{{Name: "trigger_add", SocketMethod: protocol.MethodTriggerAdd}}
		if len(narrowAutomationOffenses(fake)) == 0 {
			t.Error("a fake narrow action projecting trigger.add passed the narrow guard")
		}
	})
	t.Run("tools", func(t *testing.T) {
		fake := []string{"automation_archive"}
		if len(toolsAutomationWriteOffenses(fake)) == 0 {
			t.Error("a fake tool named after an automation write passed the tool guard")
		}
	})
	t.Run("records", func(t *testing.T) {
		fake := []records.Spec{{Name: "automation_trigger_enable", Method: "automation.trigger.enable", Mutates: true}}
		if len(recordsAutomationWriteOffenses(fake)) == 0 {
			t.Error("a fake catalog spec twinned to automation.trigger.enable passed the catalog guard")
		}
		fakePropose := []records.Spec{{Name: "automation_propose_disable", Method: "automation.trigger.disable", Mutates: true}}
		if len(recordsAutomationWriteOffenses(fakePropose)) != 0 {
			t.Error("the guard failed even the LISTED propose-twin exemption — the predicate is over-broad and will fail T7")
		}
	})
	t.Run("door-text", func(t *testing.T) {
		fake := map[string]string{"../../internal/tools/fake.go": "const x = \"automation.archive\""}
		if len(automationTextOffenses(fake)) == 0 {
			t.Error("a fake file under internal/tools naming automation.archive passed the text guard")
		}
	})
	t.Run("delete", func(t *testing.T) {
		fake := map[string]string{"../../internal/daemon/fake.go": "db.Exec(`DELETE FROM event_trigger WHERE id = x`)"}
		if len(triggerDeleteOffenses(fake)) == 0 {
			t.Error("a fake DELETE FROM event_trigger caller passed the never-delete guard")
		}
	})
	t.Run("one-writer", func(t *testing.T) {
		fake := map[string]string{"../../internal/runtime/fake.go": "n, c, err := s.SaveTriggerScheduleAs(ctx, a, r, \"update\", \"sneak\")"}
		if len(oneWriterOffenses(fake)) == 0 {
			t.Error("a fake direct caller of the store writer outside the write service passed")
		}
	})
	t.Run("seeds", func(t *testing.T) {
		if !seedTriggerWrite.MatchString("INSERT INTO trigger_schedule (id) VALUES ('sneak')") {
			t.Error("the seed-walk predicate cannot even see a fake seed INSERT")
		}
	})
	t.Run("secret", func(t *testing.T) {
		fake := map[string]string{"../../internal/daemon/automation_sneak.go": "v := os.Getenv(row.SecretEnv); return v"}
		if len(secretHygieneOffenses(fake)) == 0 {
			t.Error("a fake env-VALUE read on the automation plane passed the C17 guard (and the helper-count line too, which is fine — the read is what matters)")
		}
	})
}
