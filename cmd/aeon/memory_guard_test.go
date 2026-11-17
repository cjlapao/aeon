package main

// memory_guard_test.go — S52 T3 (P4, LM2/LM3/LM4/LM7/LM8): the memory
// management claims made mechanical, in the mould of the triage (TM),
// roadmap (RM) and environment (EV) guard sets in this directory. Positive
// halves first, negatives one subtest per non-human door, every predicate a
// pure function with a canary in TestMemoryGuardsHaveTeeth, the S28/S29
// pins re-hashed here (T0 re-read the bodies; T3 owns the hash mechanism),
// the synthetic-violation test last. No existing guard body is edited here,
// and no guard passes on an empty walk. The task text assumed S28 had left
// a memory_guard_test.go to grow siblings beside — it had not (fact drift,
// recorded in T3's Notes); this file is that name, born now, holding the
// whole S52 guard set.
//
// The claims, in one breath: retraction is the human door's act and reaches
// no other surface; an agent actor can only ever ADD (observed or assumed)
// and both the Go rule and the DB CHECK say so; relayed stays the hooks'
// own provenance; every default reader carries the one shared retraction
// predicate and the word for it lives in exactly one package; learning rows
// are written only through the store's named writers, nothing deletes them,
// and the audit is append-only by trigger the DB actually enforces; an
// agent's own-scope read never sees another agent's memory; every door is
// classified and the two retraction doors claim the human door with this
// file's proof as warrant.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// memoryRetractDoors are the two acts a person alone may do (LM8: the peer
// stamps the actor; agents are refused by name and get T4's proposal kind).
var memoryRetractDoors = []protocol.MethodName{
	protocol.MethodLearningsRetract, protocol.MethodLearningsRestore,
}

// memoryReads are the seven reads — the four S28 ones plus S52's three.
var memoryReads = []protocol.MethodName{
	protocol.MethodLearningsList, protocol.MethodLearningsShow, protocol.MethodLearningsQuery,
	protocol.MethodLearningsBoard, protocol.MethodLearningsHistory,
	protocol.MethodLearningsVocabulary, protocol.MethodLearningsForAgent,
}

// memoryWrites are the four writes (add and embed ride S28; retract and
// restore are S52's).
var memoryWrites = []protocol.MethodName{
	protocol.MethodLearningsAdd, protocol.MethodLearningsEmbed,
	protocol.MethodLearningsRetract, protocol.MethodLearningsRestore,
}

func memoryRetractSet() map[protocol.MethodName]bool {
	set := map[protocol.MethodName]bool{}
	for _, m := range memoryRetractDoors {
		set[m] = true
	}
	return set
}

// ── guard 1: the retraction doors are the human door, five ways negative ────

func TestMemoryRetractionIsAHumanDoor(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for _, m := range memoryRetractDoors {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a fully wired daemon — the app would have no door", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize — a retraction may not precede negotiation", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — taking a memory back is a product act, not a diagnostic", m)
		}
	}

	t.Run("narrow", func(t *testing.T) {
		for _, bad := range narrowMemoryOffenses(narrow.Actions()) {
			t.Error(bad)
		}
		s := narrow.BoundaryStatement()
		for _, m := range memoryRetractDoors {
			if strings.Contains(s, string(m)) {
				t.Errorf("the boundary statement names %q — a retraction projected through the narrow door would put taking a memory back a protocol hop from a non-person (LM8)", m)
			}
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		// NOTHING learnings-shaped rides the agent-services enumeration —
		// an agent reaches its own memory through T4's tools, never an
		// enumerated socket door, and the retraction doors reach nobody
		// but the person.
		for _, bad := range agentSvcMemoryOffenses(protocol.AgentServiceMethods()) {
			t.Error(bad)
		}
	})

	t.Run("tools-registry", func(t *testing.T) {
		var names []string
		for name := range tools.Registry() {
			names = append(names, name)
		}
		for _, bad := range toolsMemoryOffenses(names) {
			t.Error(bad)
		}
	})

	t.Run("records-catalog", func(t *testing.T) {
		for _, bad := range recordsMemoryOffenses(records.Catalog()) {
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
		for _, bad := range memoryTextOffenses(files) {
			t.Error(bad)
		}
	})
}

func narrowMemoryOffenses(actions []narrow.Action) []string {
	var bad []string
	retracts := memoryRetractSet()
	for _, a := range actions {
		if retracts[a.SocketMethod] {
			bad = append(bad, "action "+a.Name+" projects the retraction door "+string(a.SocketMethod)+" — retraction never reaches the narrow surface (LM8)")
		}
		for _, w := range narrow.ForbiddenOperationalControl {
			if strings.Contains(strings.ToLower(a.Name), w) && strings.HasPrefix(string(a.SocketMethod), "learnings.") {
				bad = append(bad, "action "+a.Name+" projects "+string(a.SocketMethod)+" under the forbidden word "+w+" (S19 R6)")
			}
		}
	}
	return bad
}

func agentSvcMemoryOffenses(methods []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range methods {
		if strings.HasPrefix(string(m.Name), "learnings.") {
			bad = append(bad, "agent-service method "+string(m.Name)+" exposes a learnings door to every agent — the person's memory doors stay the person's, and an agent's own view arrives as a tool over its own rows (LM8/LM5)")
		}
	}
	return bad
}

// memoryToolName guards the tool registry against the two acts an agent
// must never hold. T4's own-scope tools (reads, the self-log add, the
// propose-retract ASK) match NONE of these by design: the propose kind is
// a proposal, not a tool named after the act — a tool that retracted was
// LM8 inverted, and one that relayed was M1 inverted.
var memoryToolName = regexp.MustCompile(`^(learnings|memory)_(retract|restore|relay|add_told|say_told)$`)

func toolsMemoryOffenses(names []string) []string {
	var bad []string
	for _, name := range names {
		if memoryToolName.MatchString(name) {
			bad = append(bad, "tool "+name+" is named after an act no agent may hold — retract/restore are the human door's and relayed is the hooks' (LM8, LM4, M1)")
		}
	}
	return bad
}

func recordsMemoryOffenses(specs []records.Spec) []string {
	var bad []string
	retracts := memoryRetractSet()
	for _, spec := range specs {
		if retracts[protocol.MethodName(spec.Method)] {
			bad = append(bad, "catalog spec "+spec.Name+" twins the retraction door "+spec.Method+" — a tool twin of LM8's act would make the human door a revolving one (LM8)")
		}
		if memoryToolName.MatchString(spec.Name) {
			bad = append(bad, "catalog spec "+spec.Name+" is named after an act no agent may hold (LM8, M1)")
		}
	}
	return bad
}

// memoryLiterals is every way the retraction doors' names appear in source:
// the wire literal and its Go constant. The four door packages may not even
// speak of them.
var memoryLiterals = [][2]string{
	{"learnings.retract", "MethodLearningsRetract"},
	{"learnings.restore", "MethodLearningsRestore"},
}

func memoryTextOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		for _, lit := range memoryLiterals {
			if strings.Contains(src, `"`+lit[0]+`"`) || strings.Contains(src, lit[1]) {
				bad = append(bad, filepath.ToSlash(path)+" names the retraction door "+lit[0]+" — the four door packages must not even speak of it (LM8)")
			}
		}
	}
	return bad
}

// ── guard 2: an agent actor may only ADD observed or assumed ────────────────

func TestAgentActorMayOnlyAddObservedOrAssumed(t *testing.T) {
	// The door walk first: today NO agent-reachable surface reaches
	// learnings.add or the Append methods at all — the enumeration says
	// so, and T4's tools will change the census but never the rule, which
	// the behavioural half below pins for every actor shape.
	for _, bad := range agentSvcMemoryOffenses(protocol.AgentServiceMethods()) {
		t.Error(bad)
	}
	for _, bad := range narrowMemoryOffenses(narrow.Actions()) {
		t.Error(bad)
	}

	// Go side, behavioural: whatever a door claims, the domain refuses the
	// impossible pairings — an agent actor cannot produce told (a person's
	// word) or relayed (the channel's fact), in any scope, with any tags.
	l := learnings.New(memoryGuardStore(t))
	agent := learnings.Actor{Kind: "agent", ID: "loop-1"}
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "golang-engineer", Body: "an agent claiming to have been told", Provenance: learnings.ProvenanceTold}, agent); ref == nil {
		t.Error("an agent actor produced a told learning — LM4 is dead: agents do not speak with a person's voice")
	}
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "golang-engineer", Body: "an agent filing a relayed row", Provenance: learnings.ProvenanceRelayed}, agent); ref == nil {
		t.Error("an agent actor produced a relayed learning — M1's write protection is the door, not politeness")
	}
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "golang-engineer", Body: "an agent observed a thing", Provenance: learnings.ProvenanceObserved}, agent); ref != nil {
		t.Errorf("an agent actor was refused its own observed learning: %s — M3's self-log dies here", ref.Reason)
	}
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "golang-engineer", Body: "an agent assumed a thing", Provenance: learnings.ProvenanceAssumed}, agent); ref != nil {
		t.Errorf("an agent actor was refused its own assumed learning: %s", ref.Reason)
	}

	// DB side: the CHECK is real, not a comment — spoken through the
	// store's own one-transaction seam (the triage guard's BugWithTx
	// precedent), which is the only honest way a sibling test reaches the
	// database.
	st := memoryGuardStore(t)
	probe := func(op, kind string) error {
		return st.LearningWithTx(func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO learning_change (learning_id, op, summary, actor_kind, actor_id, occurred_at) VALUES (1, ?, 'guard probe', ?, 'golang-engineer', '2026-11-04T12:00:00Z')`, op, kind)
			return err
		})
	}
	if err := probe("retract", "agent"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("the DB accepted an agent actor on op retract (err %v) — LM3's net is the database, not convention", err)
	}
	if err := probe("restore", "agent"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("the DB accepted an agent actor on op restore (err %v)", err)
	}
	if err := probe("add", "agent"); err != nil {
		t.Errorf("the DB refused an agent actor on op add: %v — T4's self-log must survive even a broken door", err)
	}
	// Go and DB agree: the domain refuses the agent's retract/restore too
	// (the human door), so both halves of the promise say one thing.
	if _, _, ref := l.Retract(1, "an agent trying to retract", agent); ref == nil {
		t.Error("the domain accepted an agent retraction — LM8 and the CHECK disagree")
	}
}

// ── guard 3: relayed is still write-protected ───────────────────────────────

// relayedCallerAllowance is the closed set of production files that may
// CALL the hooks' write. The hooks themselves are daemon-internal by name;
// the definition lives in internal/learnings (matched without a leading
// dot, so the walk cannot see it — deliberate: the walk counts CALLERS).
// Today the set is empty because the board relay's resume wiring (S14's
// completion handler) has not landed — when it does, its file joins this
// list in the same commit that wires it, and no other file may.
var relayedCallerAllowance = map[string]bool{
	"internal/daemon/learnings_dispatch.go":   true,
	"internal/daemon/learnings_board.go":      true,
	"internal/daemon/triage_learning_feed.go": true,
}

var relayedCallerSQL = regexp.MustCompile(`\.AppendRelayed(As)?\(`)

func relayedCallerOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		if strings.HasSuffix(p, "_test.go") {
			continue // tests exercise the refusal; production doors are what this guards
		}
		rel := strings.TrimPrefix(p, "../../")
		if relayedCallerSQL.MatchString(src) && !relayedCallerAllowance[rel] {
			bad = append(bad, p+" calls the relayed-write hook — relayed is the hooks' own provenance, and the CLI, the socket and every tool are refused it (M1, LM4)")
		}
	}
	return bad
}

func TestRelayedIsStillWriteProtected(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range relayedCallerOffenses(files) {
		t.Error(bad)
	}
	// And live: S28's protection, re-run through the doors the public
	// actually holds. AppendAs — the path every public door funnels
	// through — refuses relayed from a person and from an agent alike;
	// only the hook's own name carries it, and the hook refuses a
	// non-system actor by name.
	l := learnings.New(memoryGuardStore(t))
	if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopePerson, ScopeKey: "me", Body: "a person filing relayed", Provenance: learnings.ProvenanceRelayed}, learnings.Actor{Kind: "user", ID: "42"}); ref == nil {
		t.Error("AppendAs accepted a relayed write from a person — the board channel is watched by the daemon, not typed by anyone")
	}
	if _, ref := l.AppendRelayedAs(learnings.AppendInput{Scope: learnings.ScopePerson, ScopeKey: "me", Body: "relay through the wrong actor", Provenance: learnings.ProvenanceRelayed}, learnings.Actor{Kind: "agent", ID: "loop-1"}); ref == nil {
		t.Error("AppendRelayedAs accepted a non-system actor — LM4's hook actor is {system, …} or nobody")
	}
}

// ── guard 4: every default reader carries the one shared predicate ──────────

// retractedWordOffenses enforces the predicate's HOME: the word for the
// stamp is the store's — the one package that owns SQL (the SQL-placement
// guard already pins that) — and the phrase `retracted_at IS NULL` may
// appear exactly ONCE across the package's production files, at the
// learningLiveSQL constant every default reader composes. A second spelling
// is a second truth about what live means.
var retractedWordSQL = regexp.MustCompile(`retracted_at IS NULL`)

func retractedWordOffenses(files map[string]string) []string {
	var bad []string
	phraseTotal := 0
	phraseFiles := 0
	for path, src := range files {
		p := filepath.ToSlash(path)
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		n := len(retractedWordSQL.FindAllString(src, -1))
		if !strings.Contains(p, "/internal/store/") {
			// Comment lines may name the column — a comment is prose
			// about the stamp, not a spelling of SQL. Only CODE that
			// re-spells the predicate is an offense.
			for _, line := range strings.Split(src, "\n") {
				t := strings.TrimSpace(line)
				if strings.Contains(t, "retracted_at") && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "*") {
					bad = append(bad, p+" spells retracted_at in code outside the store — the predicate is composed by the store's one constant, never re-spelled by a caller (LM2)")
					break
				}
			}
			continue
		}
		if n > 0 {
			phraseTotal += n
			phraseFiles++
		}
	}
	if phraseTotal == 0 {
		bad = append(bad, "no file spells the live predicate at all — the walk is blind or the constant is gone")
	}
	if phraseTotal > 1 || phraseFiles > 1 {
		bad = append(bad, fmt.Sprintf("the live predicate is spelled %d time(s) across %d file(s) — LM2's shared predicate is ONE constant, and a grep guard passes only while it stays one", phraseTotal, phraseFiles))
	}
	return bad
}

func TestEveryLearningReaderExcludesRetracted(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range retractedWordOffenses(files) {
		t.Error(bad)
	}

	// And behavioural, through every default reader the public holds:
	// List, Query, Board lose the stamped row the moment the stamp lands;
	// Show and the explicit includeRetracted read keep it, flagged; the
	// store-level read composes the same predicate (it IS the predicate);
	// and Restore reverses all of it.
	st := memoryGuardStore(t)
	l := learnings.New(st)
	person := learnings.Actor{Kind: "user", ID: "42"}
	row, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopePerson, ScopeKey: "me", Body: "guard: retract me", Provenance: learnings.ProvenanceTold}, person)
	if ref != nil {
		t.Fatalf("seed: %s", ref.Reason)
	}
	if _, _, ref := l.Retract(row.ID, "guard: it was not true", person); ref != nil {
		t.Fatalf("retract: %s", ref.Reason)
	}
	live, err := l.List(learnings.ScopePerson, "me")
	if err != nil || len(live) != 0 {
		t.Fatalf("List still shows a retracted learning (%d rows, err %v) — LM2's predicate is not optional", len(live), err)
	}
	ctx := context.Background()
	q, err := l.Query(ctx, learnings.QueryFilter{Scope: learnings.ScopePerson})
	if err != nil || len(q) != 0 {
		t.Fatalf("Query still returns a retracted learning (%d rows, err %v)", len(q), err)
	}
	board, err := l.Board(learnings.BoardFilter{})
	if err != nil || len(board) != 0 {
		t.Fatalf("the board still carries a retracted row (%d, err %v) — the board is a default reader too", len(board), err)
	}
	all, err := l.ListOpts(learnings.ScopePerson, "me", true)
	if err != nil || len(all) != 1 || !all[0].Retracted {
		t.Fatalf("includeRetracted=false-by-default honesty failed: %d rows (err %v) — the explicit read must SHOW the stamped row", len(all), err)
	}
	if _, err := st.ListLearnings(store.LearningFilters{Scope: "person", ScopeKey: "me", IncludeRetracted: true}); err != nil {
		t.Fatalf("store-level explicit read: %v", err)
	}
	if got, ok, _ := l.Show(row.ID); !ok || !got.Retracted {
		t.Error("Show lost the row — retracted is shown, flagged, never silent (LM2)")
	}
	if _, _, ref := l.Restore(row.ID, person); ref != nil {
		t.Fatalf("restore: %s", ref.Reason)
	}
	if live, err := l.List(learnings.ScopePerson, "me"); err != nil || len(live) != 1 {
		t.Fatalf("the restored learning did not come back to List (%d rows, err %v) — restore reverses or it is not restore", len(live), err)
	}
}

// ── guard 5: rows only through the door; no delete; audit append-only ───────

// learningRowSQL sees every way a learning or its audit gets written by
// hand. The learning_vec tables are an index, not a record — and the
// interpolation keeps the real SQL a secret of DropLearningVectors' string,
// which the DELETE regex cannot match (its target is a quoted backtick, not
// the word learning).
var learningRowSQL = regexp.MustCompile(`(?i)(INSERT INTO|UPDATE)\s+learning(_change)?\b`)

// learningStoreWriters are the *store.Store write methods. Whatever touches
// a learning row touches it through one of these, and only internal/store
// (the definitions) and internal/learnings (the door's service) may call
// them; the daemon calls the SERVICE, never these.
var learningStoreWriters = regexp.MustCompile(`\.(CreateLearningAudited|CreateLearning|SetLearningRetraction|MigrateLearningAgentScopeKeys|LearningWithTx)\(`)

func learningRowWriteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		p := filepath.ToSlash(path)
		if strings.HasSuffix(p, "_test.go") {
			continue // this walk guards production doors; tests carry their own fixtures
		}
		rel := strings.TrimPrefix(p, "../../internal/")
		inStore := strings.HasPrefix(rel, "store/")
		inDoor := strings.HasPrefix(rel, "learnings/")
		if learningRowSQL.MatchString(src) && !inStore {
			bad = append(bad, p+" writes learning or learning_change rows with its own SQL — those tables are written only through the store writers (LM3)")
		}
		if m := learningStoreWriters.FindString(src); m != "" && !inStore && !inDoor {
			bad = append(bad, p+" calls the store writer "+m+" directly — the daemon and the reactor call the door's SERVICE, never the writer (LM3)")
		}
	}
	return bad
}

func TestLearningRowsAreWrittenOnlyThroughTheDoor(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range learningRowWriteOffenses(files) {
		t.Error(bad)
	}
}

var learningDeleteSQL = regexp.MustCompile(`(?i)DELETE\s+FROM\s+learning(_change)?\b|UPDATE\s+learning_change\b`)

func learningDeleteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if m := learningDeleteSQL.FindString(src); m != "" {
			bad = append(bad, filepath.ToSlash(path)+" contains "+m+" — a learning is never deleted by code and learning_change is append-only (LM2, LM3)")
		}
	}
	return bad
}

func TestNoLearningDeletePath(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "internal")}, nil)
	if len(files) < 50 {
		t.Fatalf("the walk saw %d files — far too few to conclude anything", len(files))
	}
	for _, bad := range learningDeleteOffenses(files) {
		t.Error(bad)
	}
	// No migration ever deletes or rewrites one either.
	matches, err := filepath.Glob(filepath.Join("..", "..", "internal", "store", "migrations", "*.sql"))
	if err != nil || len(matches) < 47 {
		t.Fatalf("the migration walk saw %d files (err %v) — no conclusion without the full set", len(matches), err)
	}
	for _, m := range matches {
		src, rerr := os.ReadFile(m)
		if rerr != nil {
			t.Fatalf("read %s: %v", m, rerr)
		}
		if regexp.MustCompile(`(?i)DELETE\s+FROM\s+learning\b|DELETE\s+FROM\s+learning_change|UPDATE\s+learning_change`).MatchString(string(src)) {
			t.Errorf("%s deletes or rewrites learning rows — no migration ever may (LM2: the body survives the stamp)", filepath.Base(m))
		}
	}
}

func TestLearningChangeIsAppendOnly(t *testing.T) {
	// The migrations themselves carry the refusal triggers.
	matches, err := filepath.Glob(filepath.Join("..", "..", "internal", "store", "migrations", "*.sql"))
	if err != nil || len(matches) < 47 {
		t.Fatalf("the migration walk saw %d files (err %v) — no conclusion without the full set", len(matches), err)
	}
	sawUpdateTrigger, sawDeleteTrigger := false, false
	for _, m := range matches {
		src, rerr := os.ReadFile(m)
		if rerr != nil {
			t.Fatalf("read %s: %v", m, rerr)
		}
		text := string(src)
		if strings.Contains(text, "learning_change_no_update") && strings.Contains(text, "RAISE(ABORT") {
			sawUpdateTrigger = true
		}
		if strings.Contains(text, "learning_change_no_delete") && strings.Contains(text, "RAISE(ABORT") {
			sawDeleteTrigger = true
		}
	}
	if !sawUpdateTrigger || !sawDeleteTrigger {
		t.Error("the learning_change append-only refusal triggers are not in the migrations — the DB promise was never made (LM3)")
	}
	// And live: the triggers fire on this store.
	st := memoryGuardStore(t)
	seed := st.LearningWithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO learning_change (learning_id, op, summary, actor_kind, actor_id, occurred_at) VALUES (2, 'add', 'guard seed', 'user', '42', '2026-11-04T12:00:00Z')`)
		return err
	})
	if seed != nil {
		t.Fatalf("seed audit row: %v", seed)
	}
	upd := st.LearningWithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE learning_change SET summary = 'rewritten history' WHERE learning_id = 2`)
		return err
	})
	if upd == nil || !strings.Contains(upd.Error(), "append-only") {
		t.Errorf("learning_change accepted an UPDATE (err %v) — the trigger is fiction", upd)
	}
	del := st.LearningWithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM learning_change WHERE learning_id = 2`)
		return err
	})
	if del == nil || !strings.Contains(del.Error(), "append-only") {
		t.Errorf("learning_change accepted a DELETE (err %v) — the trigger is fiction", del)
	}
}

// ── guard 6: an agent's own-scope reads never cross agents ──────────────────

func TestAgentScopedReadsNeverCrossAgents(t *testing.T) {
	st := memoryGuardStore(t)
	l := learnings.New(st)
	person := learnings.Actor{Kind: "user", ID: "42"}
	mine := func(slug, body string, prov learnings.Provenance) {
		if _, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: slug, Body: body, Provenance: prov}, person); ref != nil {
			t.Fatalf("seed %s: %s", slug, ref.Reason)
		}
	}
	mine("golang-engineer", "the other agent's private lesson", learnings.ProvenanceObserved)
	mine("golang-engineer", "the other agent's second lesson", learnings.ProvenanceTold)
	mine("imported-agent", "my own lesson", learnings.ProvenanceObserved)

	ctx := context.Background()
	hits, ref := l.ForAgent(ctx, "imported-agent", "", 20)
	if ref != nil {
		t.Fatalf("forAgent: %+v", ref)
	}
	for _, h := range hits {
		if h.Learning.Scope == learnings.ScopeAgent && h.Learning.ScopeKey != "imported-agent" {
			t.Errorf("forAgent(imported-agent) returned another agent's row (scope key %q) — MC2's own-scope law applies to memory too", h.Learning.ScopeKey)
		}
	}
	// The refusal names the slug asked for and leaks nothing else — the
	// other agent's bodies appear in no error text, hint or reason.
	if _, ref := l.ForAgent(ctx, "ghost-agent", "", 20); ref == nil {
		t.Error("forAgent accepted an unknown slug — LM1's authority is AgentBySlug or nothing")
	} else {
		if !strings.Contains(ref.Reason, "ghost-agent") {
			t.Errorf("the refusal does not name the slug it refused: %s", ref.Reason)
		}
		if strings.Contains(ref.Reason, "private lesson") {
			t.Error("another agent's learning body appears in a refusal's text — an error that quotes memory is a leak (MC2 by way of LM5)")
		}
	}
}

// ── guard 7: every memory method is classified (S47) ───────────────────────

func TestEveryMemoryMethodIsClassified(t *testing.T) {
	all := append(append([]protocol.MethodName{}, memoryWrites...), memoryReads...)
	for _, m := range all {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Errorf("%s is unclassified — the surface table must answer for every learnings door (S47)", m)
			continue
		}
		switch {
		case memoryRetractSet()[m]:
			if s.Kind != protocol.SurfaceWrite && s.Kind != protocol.SurfaceControl {
				t.Errorf("%s is classified %q — a retraction is a write; a write that reads like a READ is how LM8 leaks", m, s.Kind)
			}
			if !s.HumanDoorOnly {
				t.Errorf("%s does not claim humanDoorOnly — the classification must claim what guard 1 proves (S51 T4's precedent: the claim rides the proof)", m)
			}
		case m == protocol.MethodLearningsAdd || m == protocol.MethodLearningsEmbed:
			if s.Kind != protocol.SurfaceWrite && s.Kind != protocol.SurfaceControl {
				t.Errorf("%s is classified %q — it writes and must say so", m, s.Kind)
			}
			// add carries NO humanDoorOnly claim, and that absence is
			// itself true: T4 gives agents a self-log path to the same
			// rows through their own door. Marking add would make S47's
			// absence proof a lie.
			if m == protocol.MethodLearningsAdd && s.HumanDoorOnly {
				t.Errorf("%s claims humanDoorOnly — FALSE by construction: agents self-log through T4's tool (LM8 keeps the ACTS human, not the table)", m)
			}
		default:
			if s.Kind != protocol.SurfaceRead {
				t.Errorf("%s is classified %q — it reads and must say so", m, s.Kind)
			}
		}
	}
}

// ── guard 8: S28's and S29's originals still hold (T3's re-hash) ────────────

// memoryPinnedFiles are T3's re-hash of the originals T0 re-read verbatim
// on 93eeea0b (T0's record: "the re-hash mechanism is T3's job"). Four of
// the five are byte-identical to that base (git diff 93eeea0b..HEAD proves
// it; the hashes below are today's and must stay). store_test.go carries
// T1's fixture growth — the agent-row seeders beside the pinned bodies —
// which T1's Notes record as its deviation 1; its CURRENT bytes pin from
// here, so any FURTHER edit fails this guard and demands a new dated
// re-pin, the way every sibling does it.
//
// 2026-11-06 (S73 T3 — the dated re-pin the paragraph above promises):
// 207368e5… → 9267a030…. The edit is ONE line, inside the `openLearnings`
// fixture and nowhere near a pinned body: `s.CreateAgent(…)` became
// `s.CreateAgentAudited(ctx, actor, …)`, because S73 AR3 deleted the
// un-audited writer rather than leave a twin beside it, so every caller has
// to name a person. The file's own fixture comment — "the bodies are
// untouched, hash-pinned as they were; only the fixture grows to the new
// authority" — is S52 T1's sentence for exactly this shape of change, and the
// nine named bodies below are still in the file (grep `func TestAgentScope…`
// et al. answered 1 each). The actor the fixture passes is `user:1000`, the
// value the file's own S28 assertions already use.
var memoryPinnedFiles = map[string]string{
	"../../internal/learnings/store_test.go":             "9267a0309e033c92",
	"../../internal/learnings/embed_test.go":             "26936955424e410f",
	"../../internal/learnings/dispatch_boundary_test.go": "0b98168ebac3ef82",
	"../../internal/daemon/learnings_board_test.go":      "e184117649268379",
	"../../internal/daemon/triage_learning_feed_test.go": "66751ff4c73e2834",
}

// memoryPinnedBodies are T0's named assertions — the M1 ordering and
// write-protection tests, M3's same-call-sequence test, S29's four feed
// proofs. Their BODIES are what the file hashes above pin; this list makes
// a rename or deletion a named failure rather than a hash diff nobody
// reads.
var memoryPinnedBodies = []string{
	"TestAgentScopeValidatedNotFKed",
	"TestRevokedPluginAgentRefused",
	"TestRelaxedIsWriteProtected",
	"TestEveryResultCarriesProvenanceAndStrength",
	"TestTagFilterNeverDependsOnEmbeddingRoute",
	"TestAttemptConclusionFiledAsObservedLearning",
	"TestFeedIsProjectScopedNeverAgentScoped",
	"TestAnswerFiledAsRelayedLearning",
	"TestEmbeddingNeverChoosesDispatch",
}

func TestS28AndS29MemoryPinsStillHold(t *testing.T) {
	for name, want := range memoryPinnedFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sum := sha256.Sum256(src)
		if got := hex.EncodeToString(sum[:]); !strings.HasPrefix(got, want) {
			t.Errorf("pinned file %s changed (hash %s, pinned prefix %s) — S52 adds siblings, it never edits the originals that carry M1, M3 and S29's feed laws", name, got, want)
		}
	}
	// walkedSources skips test files (it walks production doors), so the
	// named bodies are looked for in the pinned files themselves — which
	// their hashes already froze. A name vanishing from the frozen bytes is
	// impossible while the hash holds; this walk is what turns the hash
	// list and the law list into the same promise.
	for _, body := range memoryPinnedBodies {
		found := false
		for name := range memoryPinnedFiles {
			src, rerr := os.ReadFile(name)
			if rerr != nil {
				t.Fatalf("read %s: %v", name, rerr)
			}
			if strings.Contains(string(src), "func "+body+"(") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pinned test %s no longer exists in the pinned files — deleting a pinned assertion is deleting its law", body)
		}
	}
}

// ── guard 9: the guards have teeth ──────────────────────────────────────────

func TestMemoryGuardsHaveTeeth(t *testing.T) {
	t.Run("narrow", func(t *testing.T) {
		fake := []narrow.Action{{Name: "learnings_retract", SocketMethod: protocol.MethodLearningsRetract}}
		if len(narrowMemoryOffenses(fake)) == 0 {
			t.Error("a fake narrow action projecting learnings.retract passed the narrow guard")
		}
	})
	t.Run("agent-services", func(t *testing.T) {
		fake := []protocol.AgentServiceMethod{{Name: protocol.MethodLearningsForAgent}}
		if len(agentSvcMemoryOffenses(fake)) == 0 {
			t.Error("a fake agent-service method learnings.forAgent passed — the enumerated net is blind")
		}
	})
	t.Run("tools", func(t *testing.T) {
		fake := []string{"learnings_retract", "memory_restore", "learnings_relay"}
		if len(toolsMemoryOffenses(fake)) != 3 {
			t.Error("fake tools named after acts no agent may hold passed the tool guard")
		}
		fakeOK := []string{"learnings_mine_list", "learnings_log_own", "learnings_propose_retract", "bash", "bug_file"}
		if len(toolsMemoryOffenses(fakeOK)) != 0 {
			t.Error("the guard failed T4's READ / SELF-LOG / PROPOSE shapes — the predicate is over-broad and will fail T4")
		}
	})
	t.Run("records", func(t *testing.T) {
		fake := []records.Spec{{Name: "learning_retract", Method: "learnings.retract", Mutates: true}}
		if len(recordsMemoryOffenses(fake)) == 0 {
			t.Error("a fake catalog spec twinned to learnings.retract passed the catalog guard")
		}
		fakeOK := []records.Spec{{Name: "learnings_mine_list", Method: "learnings.list", Mutates: false}}
		if len(recordsMemoryOffenses(fakeOK)) != 0 {
			t.Error("the guard failed an own-view READ twin T4 will ship")
		}
	})
	t.Run("door-text", func(t *testing.T) {
		fake := map[string]string{"../../internal/tools/fake.go": `const x = "learnings.retract"`}
		if len(memoryTextOffenses(fake)) == 0 {
			t.Error("a fake file under internal/tools naming learnings.retract passed the text guard")
		}
	})
	t.Run("relayed-caller", func(t *testing.T) {
		fake := map[string]string{"../../internal/loop/fake.go": "l.AppendRelayedAs(in, actor)"}
		if len(relayedCallerOffenses(fake)) == 0 {
			t.Error("a fake AppendRelayedAs caller outside the daemon's hooks passed M1's walk")
		}
		fakeCLI := map[string]string{"../../cmd/aeon/fake.go": "l.AppendRelayedAs(in, actor)"}
		if len(relayedCallerOffenses(fakeCLI)) == 0 {
			t.Error("a fake relayed call from the CLI passed — the public doors cannot reach relayed")
		}
	})
	t.Run("predicate-home", func(t *testing.T) {
		fake := map[string]string{
			"../../internal/store/learnings.go": "const learningLiveSQL = ` AND retracted_at IS NULL`",
			"../../internal/store/second.go":    "const alsoHere = ` AND retracted_at IS NULL`",
		}
		if len(retractedWordOffenses(fake)) == 0 {
			t.Error("a SECOND spelling of the live predicate inside the store passed — LM2's ONE predicate is a countable fact")
		}
		fakeOutside := map[string]string{"../../internal/learnings/fake.go": "WHERE retracted_at IS NULL"}
		if len(retractedWordOffenses(fakeOutside)) == 0 {
			t.Error("a caller spelling retracted_at outside the store passed — SQL lives in one package")
		}
	})
	t.Run("row-writer", func(t *testing.T) {
		fake := map[string]string{"../../internal/runtime/fake.go": "UPDATE learning SET body = 'edited' WHERE id = 7"}
		if len(learningRowWriteOffenses(fake)) == 0 {
			t.Error("a fake UPDATE learning in internal/runtime passed the one-door guard")
		}
		fakeWriter := map[string]string{"../../internal/loop/fake.go": "s.SetLearningRetraction(id, true, reason, k, i, ch)"}
		if len(learningRowWriteOffenses(fakeWriter)) == 0 {
			t.Error("a fake direct caller of the store writer outside the door's service passed")
		}
	})
	t.Run("delete", func(t *testing.T) {
		fake := map[string]string{"../../internal/store/fake.go": "DELETE FROM learning_change WHERE learning_id = 7"}
		if len(learningDeleteOffenses(fake)) == 0 {
			t.Error("a fake DELETE FROM learning_change passed the append-only guard")
		}
		fakeVec := map[string]string{"../../internal/store/fake.go": "DELETE FROM learning_vec_10e2 WHERE learning_id = 7"}
		if len(learningDeleteOffenses(fakeVec)) != 0 {
			t.Error("the delete guard flagged a learning_vec index write — the regex must spare the index, LM5's hygiene depends on it")
		}
	})
	t.Run("pins", func(t *testing.T) {
		if len(memoryPinnedFiles) != 5 {
			t.Fatalf("the pin table holds %d files, want 5 — guard 8 would pass on a partial walk", len(memoryPinnedFiles))
		}
		if len(memoryPinnedBodies) != 9 {
			t.Fatalf("the body list holds %d names, want 9 — T0 recorded nine", len(memoryPinnedBodies))
		}
	})
}

// ── fixture helper ──────────────────────────────────────────────────────────

// memoryGuardStore opens a fresh daemon-shaped store and seeds the two
// agent rows the domain's slug authority needs — the same seeders
// internal/learnings' openLearnings uses (LM1: scope keys are validated
// against real agent rows, so a guard that seeds scopes must seed agents).
func memoryGuardStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, slug := range []string{"golang-engineer", "imported-agent"} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: slug, Name: slug}, "2026-11-04T00:00:00Z"); err != nil {
			t.Fatalf("seed agent %s: %v", slug, err)
		}
	}
	return st
}
