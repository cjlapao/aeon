package main

// triage_guard_test.go — S51 T4 (P4, TM2/TM6/TM8): the triage's management
// claims made mechanical, in the mould of the roadmap (RM), automation (AN)
// and environment (EV) guard sets in this directory. Positive halves first,
// negatives one subtest per non-human door, every guard with a canary that
// proves it can FAIL, the synthetic-violation test last. No existing guard
// body is edited here, and no guard passes on an empty walk. S29's original
// D1/D2/D5/N3/D6 tests are pinned by T0's hashes in guard 6 — siblings add,
// they never touch.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageWritesPerson are the five writes a person alone may do (TM6:
// editing, priority, duplicates, validation attempts and hand-offs belong
// to the person at the endpoint). `triage.file` is deliberately NOT in this
// set — it is the ONE triage write an agent actor may ever reach, and only
// through T5's bug_file twin; guard 1 says so in both directions.
var triageWritesPerson = []protocol.MethodName{
	protocol.MethodTriageUpdate, protocol.MethodTriageSetPriority,
	protocol.MethodTriageConfirmDuplicate, protocol.MethodTriageValidate,
	protocol.MethodTriageHandoff,
}

// triageReads are the seven reads (list/show/suggest from S29;
// board/history/vocabulary/health from T2/T3).
var triageReads = []protocol.MethodName{
	protocol.MethodTriageList, protocol.MethodTriageShow, protocol.MethodTriageSuggest,
	protocol.MethodTriageBoard, protocol.MethodTriageHistory,
	protocol.MethodTriageVocabulary, protocol.MethodTriageHealth,
}

func triageWriteSet() map[protocol.MethodName]bool {
	set := map[protocol.MethodName]bool{}
	for _, m := range triageWritesPerson {
		set[m] = true
	}
	return set
}

// ── guard 1: the human door, and the ONE file-shaped hole in it ────────────

func TestTriageWritesAreAHumanDoorExceptFile(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, m := range triageWritesPerson {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a fully wired daemon — the app would have no door", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize — a triage write may not precede negotiation", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — a triage write is a product method, not a diagnostic", m)
		}
	}

	t.Run("narrow", func(t *testing.T) {
		for _, bad := range narrowTriageOffenses(narrow.Actions()) {
			t.Error(bad)
		}
		s := narrow.BoundaryStatement()
		for _, m := range append(append([]protocol.MethodName{}, triageWritesPerson...), protocol.MethodTriageFile) {
			if strings.Contains(s, string(m)) {
				t.Errorf("the boundary statement names %q — the statement accounts for what is ABSENT, and a triage write named there would claim a projection", m)
			}
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		// NOTHING triage-shaped rides the agent-services enumeration —
		// including triage.file. An agent reaches the one lawful write
		// through the bug_file TWIN (a tool, T5), never by a socket door
		// enumerated for agents.
		for _, bad := range agentSvcTriageOffenses(protocol.AgentServiceMethods()) {
			t.Error(bad)
		}
	})

	t.Run("tools-registry", func(t *testing.T) {
		var names []string
		for name := range tools.Registry() {
			names = append(names, name)
		}
		for _, bad := range toolsTriageWriteOffenses(names) {
			t.Error(bad)
		}
	})

	t.Run("records-catalog", func(t *testing.T) {
		for _, bad := range recordsTriageWriteOffenses(records.Catalog()) {
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
		for _, bad := range triageTextOffenses(files) {
			t.Error(bad)
		}
	})
}

func narrowTriageOffenses(actions []narrow.Action) []string {
	var bad []string
	writes := triageWriteSet()
	for _, a := range actions {
		if writes[a.SocketMethod] {
			bad = append(bad, "action "+a.Name+" projects the person write "+string(a.SocketMethod)+" (TM6)")
		}
		for _, w := range narrow.ForbiddenOperationalControl {
			if strings.Contains(strings.ToLower(a.Name), w) && strings.HasPrefix(string(a.SocketMethod), "triage.") {
				bad = append(bad, "action "+a.Name+" projects "+string(a.SocketMethod)+" under the forbidden word "+w+" (S19 R6)")
			}
		}
	}
	return bad
}

func agentSvcTriageOffenses(methods []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range methods {
		if strings.HasPrefix(string(m.Name), "triage.") {
			bad = append(bad, "agent-service method "+string(m.Name)+" exposes a triage door to every agent — the one lawful write rides the bug_file twin, not an enumerated door (TM6/D7)")
		}
	}
	return bad
}

// triageWriteToolName is guard 1's tool-registry predicate. T5's tools
// (bug_list, bug_show, bug_file) match NONE of these WRITE verbs by design:
// two are reads and the third is filing — the only write D7 ever allows,
// and it is named after the ACT, never the door it twins.
var triageWriteToolName = regexp.MustCompile(`^(triage_(update|set_priority|priority|confirm_duplicate|duplicate|validate|handoff)|bug_(update|set_priority|priority|confirm_duplicate|duplicate|validate|handoff))$`)

func toolsTriageWriteOffenses(names []string) []string {
	var bad []string
	for _, name := range names {
		if triageWriteToolName.MatchString(name) {
			bad = append(bad, "tool "+name+" is named after a triage person write — a second door onto the person's act (TM6)")
		}
	}
	return bad
}

func recordsTriageWriteOffenses(specs []records.Spec) []string {
	var bad []string
	writes := triageWriteSet()
	for _, spec := range specs {
		if writes[protocol.MethodName(spec.Method)] {
			bad = append(bad, "catalog spec "+spec.Name+" twins the person write "+spec.Method+" (TM6)")
		}
		if spec.Method == string(protocol.MethodTriageFile) && spec.Name != "bug_file" {
			bad = append(bad, "catalog spec "+spec.Name+" twins triage.file but is not the LISTED bug_file twin — the one agent-reachable write has exactly one name (D7/TM6)")
		}
	}
	return bad
}

// triageLiterals is every way a person write door's name appears in source:
// the wire literal and its Go constant. triage.file is IN this list on
// purpose: the four door packages may not even speak of it — the bug_file
// twin reaches it through the socket's own dispatch, not by naming it in
// those packages. (T5's tool spec lives in the catalog and is asserted by
// name above, not by text here.)
var triageLiterals = func() [][2]string {
	out := make([][2]string, 0, 6)
	for _, m := range append(append([]protocol.MethodName{}, triageWritesPerson...), protocol.MethodTriageFile) {
		lit := string(m)
		camel := ""
		for _, part := range strings.Split(lit, ".") {
			for _, word := range strings.Split(part, "-") {
				camel += strings.ToUpper(word[:1]) + word[1:]
			}
		}
		out = append(out, [2]string{lit, "Method" + camel})
	}
	return out
}()

func triageTextOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		for _, lit := range triageLiterals {
			if strings.Contains(src, `"`+lit[0]+`"`) || strings.Contains(src, lit[1]) {
				bad = append(bad, filepath.ToSlash(path)+" names the triage write "+lit[0]+" — the four door packages must not even speak of it (TM6)")
			}
		}
	}
	return bad
}

// ── guard 2: no agent-reachable path sets duplicate, priority, state or the edit ──

// triageAgentName is the second net under guard 1's: even a NAME that
// reads like a person act must not stand on an agent-reachable surface —
// reads (bug_list, bug_show) and the one filing (bug_file) match nothing
// here.
var triageAgentName = regexp.MustCompile(`(?i)(triage|bug).*(priority|duplicate|validate|handoff|update)`)

func triageAgentMethodOffenses(methods []protocol.MethodName, reachable func(protocol.MethodName) bool) []string {
	var bad []string
	writes := triageWriteSet()
	seen := false
	for _, m := range methods {
		if !strings.HasPrefix(string(m), "triage.") {
			continue
		}
		seen = true
		if reachable(m) && (writes[m] || m == protocol.MethodTriageFile) {
			bad = append(bad, "an agent-reachable surface reaches "+string(m)+" — priority, duplicates, validation, hand-off and editing belong to a person, and the one lawful filing rides the bug_file twin (D5/N3, D2, TM1/TM6)")
		}
	}
	if len(methods) > 0 && !seen && reachable == nil {
		bad = append(bad, "the triage enumeration came back empty on a surface that had methods — the walk is blind")
	}
	return bad
}

func triageAgentNameOffenses(names []string) []string {
	var bad []string
	for _, name := range names {
		if triageAgentName.MatchString(name) {
			bad = append(bad, "agent-reachable name "+name+" reads like a triage person act — no door by that shape may exist (N3)")
		}
	}
	return bad
}

func TestNoAgentReachablePathSetsDuplicateOrPriority(t *testing.T) {
	// The four agent-reachable surfaces, enumerated exhaustively — the
	// same set guard 1 walks, asserted from the D5/N3 direction: no
	// method and no NAME reaching an agent may look like priority,
	// duplicate, validate, hand-off or update.
	var svcMethods []protocol.MethodName
	for _, m := range protocol.AgentServiceMethods() {
		svcMethods = append(svcMethods, m.Name)
	}
	reachSvc := func(protocol.MethodName) bool { return true }
	for _, bad := range triageAgentMethodOffenses(svcMethods, reachSvc) {
		t.Error(bad)
	}
	var narrowMethods []protocol.MethodName
	var toolNames []string
	for name := range tools.Registry() {
		toolNames = append(toolNames, name)
	}
	for _, a := range narrow.Actions() {
		narrowMethods = append(narrowMethods, a.SocketMethod)
	}
	reachNarrow := func(protocol.MethodName) bool { return true }
	for _, bad := range triageAgentMethodOffenses(narrowMethods, reachNarrow) {
		t.Error(bad)
	}
	for _, bad := range triageAgentNameOffenses(toolNames) {
		t.Error(bad)
	}
	var catalogMethods []protocol.MethodName
	var specNames []string
	for _, s := range records.Catalog() {
		catalogMethods = append(catalogMethods, protocol.MethodName(s.Method))
		specNames = append(specNames, s.Name)
	}
	reachCatalog := func(m protocol.MethodName) bool { return m != protocol.MethodTriageFile } // the LISTED bug_file twin is guard 1's exemption
	for _, bad := range triageAgentMethodOffenses(catalogMethods, reachCatalog) {
		t.Error(bad)
	}
	for _, bad := range triageAgentNameOffenses(specNames) {
		t.Error(bad)
	}

	// And one behavioural witness at the ONE door an agent actor can law-
	// fully stand behind: an agent-actor filing leaves exactly ONE audit
	// row — `file` — and the row it filed is unvalidated, unknown-priority
	// and unlinked. The spy (the audit) shows only Create; no second op
	// rides the filing.
	st := triageGuardStore(t)
	in := store.NewBugRow{ComponentRoot: "/guard/root", Title: "an agent claim", Body: "filed through the one lawful door", FiledBy: "researcher"}
	bug, err := st.CreateBugAudited(in, store.BugAudit{
		Op: "file", Actor: store.BugActor{Kind: "agent", ID: "researcher"},
		Summary: "filed bug through the door",
	})
	if err != nil {
		t.Fatalf("agent filing: %v", err)
	}
	rows, err := st.BugChangeList(context.Background(), bug.ID, 0)
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	if len(rows) != 1 || rows[0].Op != "file" {
		t.Fatalf("the agent door left %d audit rows — only Create, one row, ever (D7/TM2)", len(rows))
	}
	if bug.ValidationState != "unvalidated" || bug.PriorityState != "unknown" || bug.DuplicateOf != nil || bug.RoadmapItemID != nil {
		t.Fatalf("an agent filing moved state (validation %q, priority %q) — the door files a CLAIM (D7), never a decision (D2/D5)", bug.ValidationState, bug.PriorityState)
	}
}

// ── guard 3: the audit's actor law lives in Go AND in the DB ───────────────

func TestBugChangeAgentActorOnlyForFile(t *testing.T) {
	// Go side: the one expression.
	agent := store.BugActor{Kind: "agent", ID: "researcher"}
	if !agent.ValidFor("file") {
		t.Error("an agent actor was refused for op file — D7's one lawful act")
	}
	for _, op := range []string{"update", "priority", "duplicate", "validate", "handoff", "transition"} {
		if agent.ValidFor(op) {
			t.Errorf("Go accepted an agent actor for op %q — TM2 admits agents on file alone", op)
		}
	}
	// DB side: the CHECK is real, not a comment — spoken through the
	// store's own one-transaction seam, which is the only honest way a
	// sibling test reaches the database.
	st := triageGuardStore(t)
	probe := func(op, kind string) error {
		return st.BugWithTx(func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO bug_change (bug_id, op, summary, actor_kind, actor_id, occurred_at) VALUES (1, ?, 'guard probe', ?, 'researcher', '2026-11-04T12:00:00Z')`, op, kind)
			return err
		})
	}
	if err := probe("update", "agent"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("the DB accepted an agent actor on op update (err %v) — TM2's net is the database, not convention", err)
	}
	if err := probe("file", "agent"); err != nil {
		t.Errorf("the DB refused an agent actor on op file: %v — D7 must survive even a broken door", err)
	}
}

// ── guard 4: bug rows only through the door; no delete; audit append-only ──

// bugRowSQL sees every way a bug or its audit gets written. bug_vec is an
// index, not a record — its churn is not a write of the claim.
var bugRowSQL = regexp.MustCompile(`(?i)(INSERT INTO|UPDATE)\s+bug(_change)?\b`)

// bugStoreWriters are the *store.Store write methods: whatever touches a
// bug row touches it through one of these, and only internal/store (the
// definitions) and internal/triage (the DOOR's service) may call them. The
// daemon and the reactor call the SERVICE (triage.Triage's audited set),
// never these — which is what the walk proves, file by file.
var bugStoreWriters = regexp.MustCompile(`\.(CreateBug|CreateBugAudited|TransitionBug|TransitionBugAudited|SetBugPriority|SetBugPriorityAudited|SetBugDuplicateOf|SetBugDuplicateOfAudited|SetBugRoadmapItemID|SetBugRoadmapItemIDAudited|UpdateBugFieldsAudited|BugChangeAppend|BugWithTx)\(`)

func bugRowWriteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		rel := strings.TrimPrefix(p, "../../internal/")
		inStore := strings.HasPrefix(rel, "store/")
		inDoor := strings.HasPrefix(rel, "triage/")
		if strings.HasSuffix(p, "_test.go") {
			continue // this walk guards production doors; tests carry their own fixtures
		}
		if bugRowSQL.MatchString(src) && !inStore {
			bad = append(bad, p+" writes bug or bug_change rows with its own SQL — those tables are written only through the store writers (TM2)")
		}
		if m := bugStoreWriters.FindString(src); m != "" && !inStore && !inDoor {
			bad = append(bad, p+" calls the store writer "+m+" directly — the daemon and the reactor call the door's SERVICE, never the writer (TM2)")
		}
	}
	return bad
}

func TestBugRowsAreWrittenOnlyThroughTheDoor(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range bugRowWriteOffenses(files) {
		t.Error(bad)
	}
}

var bugDeleteSQL = regexp.MustCompile(`(?i)DELETE\s+FROM\s+bug(_change)?\b|UPDATE\s+bug_change\b`)

func bugDeleteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if m := bugDeleteSQL.FindString(src); m != "" {
			bad = append(bad, filepath.ToSlash(path)+" contains "+m+" — bug_change is append-only and a bug row is never deleted by code (TM2)")
		}
	}
	return bad
}

func TestNoBugDeletePath(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range bugDeleteOffenses(files) {
		t.Error(bad)
	}
}

func TestBugChangeIsAppendOnly(t *testing.T) {
	// The migrations themselves: the refusal triggers must exist, and no
	// migration may ever have carried a DELETE FROM bug*.
	matches, err := filepath.Glob(filepath.Join("..", "..", "internal", "store", "migrations", "*.sql"))
	if err != nil || len(matches) < 40 {
		t.Fatalf("the migration walk saw %d files (err %v) — no conclusion without the full set", len(matches), err)
	}
	sawUpdateTrigger, sawDeleteTrigger := false, false
	for _, m := range matches {
		src, rerr := os.ReadFile(m)
		if rerr != nil {
			t.Fatalf("read %s: %v", m, rerr)
		}
		text := string(src)
		if regexp.MustCompile(`(?i)DELETE\s+FROM\s+bug\b|DELETE\s+FROM\s+bug_change`).MatchString(text) {
			t.Errorf("%s deletes bug rows — no migration ever may", filepath.Base(m))
		}
		if strings.Contains(text, "bug_change_no_update") && strings.Contains(text, "RAISE(ABORT") {
			sawUpdateTrigger = true
		}
		if strings.Contains(text, "bug_change_no_delete") && strings.Contains(text, "RAISE(ABORT") {
			sawDeleteTrigger = true
		}
	}
	if !sawUpdateTrigger || !sawDeleteTrigger {
		t.Error("the bug_change append-only refusal triggers are not in the migrations — the DB promise was never made (TM2)")
	}
	// And live: the triggers fire on this store.
	st := triageGuardStore(t)
	seed := st.BugWithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO bug_change (bug_id, op, summary, actor_kind, actor_id, occurred_at) VALUES (2, 'file', 'guard seed', 'user', '42', '2026-11-04T12:00:00Z')`)
		return err
	})
	if seed != nil {
		t.Fatalf("seed audit row: %v", seed)
	}
	upd := st.BugWithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE bug_change SET summary = 'rewritten history' WHERE bug_id = 2`)
		return err
	})
	if upd == nil || !strings.Contains(upd.Error(), "append-only") {
		t.Errorf("bug_change accepted an UPDATE (err %v) — the trigger is fiction", upd)
	}
	del := st.BugWithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM bug_change WHERE bug_id = 2`)
		return err
	})
	if del == nil || !strings.Contains(del.Error(), "append-only") {
		t.Errorf("bug_change accepted a DELETE (err %v) — the trigger is fiction", del)
	}
}

// ── guard 5: the validation blueprint never falls back to a named literal ──

// triageFallback walks the TRIAGE dispatch plane (daemon/triage*.go,
// non-test). The settings declaration's DEFAULT (internal/config) and the
// seeded standard row are the only spellings of the name; a dispatch that
// spells it is a fallback waiting to fire (TM4).
var triageFallback = regexp.MustCompile(`"brainstorm"`)

func triageFallbackOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		base := filepath.Base(p)
		if !strings.HasPrefix(base, "triage") || !strings.Contains(p, "/daemon/") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		if m := triageFallback.FindString(src); m != "" {
			bad = append(bad, p+" carries the literal "+m+" — the validation choice lives in the settings declaration's DEFAULT, and a dispatch that spells it is the invisible run TM4 deletes")
		}
	}
	return bad
}

func TestValidationBlueprintNeverFallsBack(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal", "daemon")}, nil)
	seen := 0
	for path := range files {
		if strings.HasPrefix(filepath.Base(path), "triage") && !strings.HasSuffix(path, "_test.go") {
			seen++
		}
	}
	if seen < 4 {
		t.Fatalf("the walk saw %d triage dispatch files — too few to conclude anything", seen)
	}
	for _, bad := range triageFallbackOffenses(files) {
		t.Error(bad)
	}
}

// ── guard 6: S29's originals still hold, byte for byte (T0's pins) ─────────

// triagePinnedS29 are T0's file-level hashes of the four S29 test files —
// the D1/D2/D5/D6/D7 originals T4's siblings ADD beside but never edit.
// Their RE-RUN is the internal/triage suite itself (the dated command is in
// T4's Notes); this guard pins they are UNTOUCHED.
var triagePinnedS29 = map[string]string{
	"transitions_test.go": "e0556d7ea6eaa86e0dd0b48974700dfe634c4b85f6e10f52b4804661c18f684d",
	"dedupe_test.go":      "537637d8b6d629062b34b85806f7d9a38e8d2787242fa990f080dbe50f02e8da",
	"store_test.go":       "14b8e65a1f84bbe2802b6c7c9e115073a5e2d1dfe43ba25fed4c057f8d7b1211",
	"file_golden_test.go": "97959d7e2cc0f59ef0614161675d8407c5cc16b89183164cd841a9c1d4d82260",
}

func TestD1D2D6StillHold(t *testing.T) {
	for name, want := range triagePinnedS29 {
		src, err := os.ReadFile(filepath.Join("..", "..", "internal", "triage", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sum := sha256.Sum256(src)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("S29's %s changed (hash %s, pinned %s) — S51 adds siblings, it never edits the originals that carry D1/D2/D5/D6/D7", name, got, want)
		}
	}
	// D1 re-lived through the live machine: once validated, nothing
	// returns — the SAME service every door stands behind.
	st := triageGuardStore(t)
	tr := triage.New(st)
	person := triage.Actor{Kind: "user", ID: "42"}
	bug, ref := tr.CreateAudited("/guard/root", triage.NewBug{Title: "a validated claim", Body: "the body", FiledBy: "person"}, person)
	if ref != nil {
		t.Fatalf("file: %s", ref.Reason)
	}
	if _, ref := tr.Transition(bug.ID, triage.ValidationReproduced); ref != nil {
		t.Fatalf("validate: %s", ref.Reason)
	}
	if _, ref := tr.Transition(bug.ID, triage.ValidationUnvalidated); ref == nil {
		t.Error("a validated bug moved back to unvalidated — D1 is dead")
	}
}

// ── guard 7: every method is classified (S47) ─────────────────────────────

func TestEveryTriageMethodIsClassified(t *testing.T) {
	all := append(append(append([]protocol.MethodName{}, triageWritesPerson...), protocol.MethodTriageFile), triageReads...)
	// S51 T5: the twin stands too — a WRITE (it files), and the ONE triage
	// door whose classification must NOT claim humanDoorOnly, because the
	// bug_file catalog twin reaches it through the tool door. Marking it
	// humanDoorOnly would make S47's absence proof a lie; the claim stays
	// with triage.file (TM6).
	all = append(all, protocol.MethodTriageFileAs)
	for _, m := range all {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Errorf("%s is unclassified — the surface table must answer for every triage door (S47)", m)
			continue
		}
		isWrite := m == protocol.MethodTriageFile || m == protocol.MethodTriageFileAs || triageWriteSet()[m]
		if isWrite {
			if s.Kind != protocol.SurfaceWrite && s.Kind != protocol.SurfaceControl {
				t.Errorf("%s is classified %q — a triage write is write or control; a write that reads like a READ is how TM6 leaks", m, s.Kind)
			}
			if m == protocol.MethodTriageFileAs && s.HumanDoorOnly {
				t.Errorf("triage.fileAs claims humanDoorOnly — FALSE by construction: bug_file reaches it through the catalog/tool door (S51 T5, TM6)")
			}
			if m != protocol.MethodTriageFile && m != protocol.MethodTriageFileAs && !s.HumanDoorOnly {
				t.Errorf("%s is not humanDoorOnly — the classification must claim what guard 1 proves", m)
			}
		} else if s.Kind != protocol.SurfaceRead {
			t.Errorf("%s is classified %q — it reads and must say so", m, s.Kind)
		}
	}
}

// ── guard 8: the guards have teeth ────────────────────────────────────────

func TestTriageGuardsHaveTeeth(t *testing.T) {
	t.Run("narrow", func(t *testing.T) {
		fake := []narrow.Action{{Name: "triage_update", SocketMethod: protocol.MethodTriageUpdate}}
		if len(narrowTriageOffenses(fake)) == 0 {
			t.Error("a fake narrow action projecting triage.update passed the narrow guard")
		}
	})
	t.Run("agent-services", func(t *testing.T) {
		fake := []protocol.AgentServiceMethod{{Name: protocol.MethodTriageFile}}
		if len(agentSvcTriageOffenses(fake)) == 0 {
			t.Error("a fake agent-service method triage.file passed — the enumerated net is blind")
		}
	})
	t.Run("tools", func(t *testing.T) {
		fake := []string{"bug_validate", "triage_set_priority"}
		if len(toolsTriageWriteOffenses(fake)) != 2 {
			t.Error("fake tools named after triage person writes passed the tool guard")
		}
		fakeOK := []string{"bug_list", "bug_show", "bug_file"}
		if len(toolsTriageWriteOffenses(fakeOK)) != 0 {
			t.Error("the guard failed T5's READ and FILING names — the predicate is over-broad and will fail T5")
		}
	})
	t.Run("records", func(t *testing.T) {
		fake := []records.Spec{{Name: "bug_set_priority", Method: "triage.set-priority", Mutates: true}}
		if len(recordsTriageWriteOffenses(fake)) == 0 {
			t.Error("a fake catalog spec twinned to triage.setPriority passed the catalog guard")
		}
		fakeWrong := []records.Spec{{Name: "triage_file", Method: "triage.file", Mutates: true}}
		if len(recordsTriageWriteOffenses(fakeWrong)) == 0 {
			t.Error("a spec twins triage.file under a name that is NOT bug_file and passed — the one-hole claim needs the name")
		}
		fakeOK := []records.Spec{{Name: "bug_file", Method: "triage.file", Mutates: true}}
		if len(recordsTriageWriteOffenses(fakeOK)) != 0 {
			t.Error("the guard failed the LISTED bug_file exemption")
		}
	})
	t.Run("agent-methods", func(t *testing.T) {
		fake := []protocol.MethodName{protocol.MethodTriageConfirmDuplicate}
		if len(triageAgentMethodOffenses(fake, func(protocol.MethodName) bool { return true })) == 0 {
			t.Error("a fake agent-reachable surface reaching triage.confirmDuplicate passed")
		}
	})
	t.Run("agent-names", func(t *testing.T) {
		fake := []string{"bug_confirm_duplicate"}
		if len(triageAgentNameOffenses(fake)) == 0 {
			t.Error("a fake agent-reachable name reading like duplicate-confirmation passed")
		}
	})
	t.Run("door-text", func(t *testing.T) {
		fake := map[string]string{"../../internal/tools/fake.go": `const x = "triage.confirm-duplicate"`}
		if len(triageTextOffenses(fake)) == 0 {
			t.Error("a fake file under internal/tools naming triage.confirmDuplicate passed the text guard")
		}
	})
	t.Run("row-writer", func(t *testing.T) {
		fake := map[string]string{"../../internal/runtime/fake.go": "UPDATE bug SET validation_state = 'reproduced'"}
		if len(bugRowWriteOffenses(fake)) == 0 {
			t.Error("a fake UPDATE bug in internal/runtime passed the one-door guard")
		}
		fakeWriter := map[string]string{"../../internal/loop/fake.go": "s.SetBugPriorityAudited(id, root, state, label, a)"}
		if len(bugRowWriteOffenses(fakeWriter)) == 0 {
			t.Error("a fake direct caller of the store writer outside the door's service passed")
		}
	})
	t.Run("delete", func(t *testing.T) {
		fake := map[string]string{"../../internal/store/fake.go": "DELETE FROM bug_change WHERE bug_id = 7"}
		if len(bugDeleteOffenses(fake)) == 0 {
			t.Error("a fake DELETE FROM bug_change passed the append-only guard")
		}
	})
	t.Run("fallback", func(t *testing.T) {
		fake := map[string]string{"../../internal/daemon/triage_sneak.go": `if name == "" { name = "brainstorm" }`}
		if len(triageFallbackOffenses(fake)) == 0 {
			t.Error("a fake literal \"brainstorm\" in the triage dispatch passed the no-fallback guard")
		}
	})
	t.Run("pins", func(t *testing.T) {
		if len(triagePinnedS29) != 4 {
			t.Fatalf("the pin table holds %d files, want 4 — guard 6 would pass on a partial walk", len(triagePinnedS29))
		}
	})
}

// ── fixture helper ────────────────────────────────────────────────────────

func triageGuardStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateComponent(store.NewComponent{Name: "guard", Slug: "guard", Root: "/guard/root"}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	return st
}
