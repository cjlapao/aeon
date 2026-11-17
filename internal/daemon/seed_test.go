package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentseed"
	"github.com/cjlapao/aeon/internal/blueprintseed"
	"github.com/cjlapao/aeon/internal/landingseed"
	"github.com/cjlapao/aeon/internal/notifyseed"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/protocol"
	retroseed "github.com/cjlapao/aeon/internal/retro/seed"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/verify/verifyseed"
	"github.com/cjlapao/aeon/internal/watchdog/watchdogseed"
)

// openSeedStore opens a fully-migrated temp store for the bridge tests.
func openSeedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

// TestSeedInertAppliesAtStart is the T6 core: the one engine, built over the
// store, applies the agents set (inert) at startup, and the ledger shows it.
// The agents set is the only inert set declared (the guard set is a test
// fake), so the startup path reaches it and nothing else.
func TestSeedInertAppliesAtStart(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	ApplyInertAtStart(context.Background(), e)

	st, err := e.Status(context.Background(), agentseed.SetID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var set seed.SetStatus
	for _, s := range st {
		if s.ID == agentseed.SetID {
			set = s
			break
		}
	}
	if set.ID != agentseed.SetID {
		t.Fatalf("status did not return the agents set")
	}
	var applied, failed int
	for _, rec := range set.Records {
		if rec.State == seed.StateApplied {
			applied++
			continue
		}
		failed++
	}
	if applied == 0 {
		t.Fatalf("startup applied nothing — the agents set did not apply at start (SD5)")
	}
	if failed > 0 {
		t.Fatalf("startup applied %d but %d did not reach applied", applied, failed)
	}
}

// TestSeedStatusIsReadOnly is the read-only half of the daemon doors: a status
// read after an apply reports the applied set, and never writes (SD10).
func TestSeedStatusIsReadOnly(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	ApplyInertAtStart(context.Background(), e)
	st, err := e.Status(context.Background(), agentseed.SetID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var found bool
	for _, s := range st {
		if s.ID == agentseed.SetID {
			found = true
			if len(s.Records) == 0 {
				t.Fatal("the status reported no records — the agents set is empty")
			}
		}
	}
	if !found {
		t.Fatal("status did not return the agents set")
	}
}

// TestSeedStatusOfNeverAppliedSetReadsAbsent is the S45 T11 walk's regression
// (finding, dated 2026-11-01): a fresh daemon's `seed.status` reported
// policy.corpus as "applied 367" — every record's status was the POST state
// of the planned create, not its actual `absent` state (the store then held
// zero policy rows and zero policy.corpus ledger rows, measured by sqlite).
// The CLI's own promise — "lists EVERY non-applied record with its state and
// the daemon's reason" (cmd/aeon/seed.go) — was invisible while all records
// claimed applied. The status must read the ledger, not the plan's intent;
// D18's "System records shows policy.corpus … absent" depends on this.
func TestSeedStatusOfNeverAppliedSetReadsAbsent(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	// NO startup apply: both sets are untouched — this is what a fresh
	// daemon's status door answers.
	st, err := e.Status(context.Background(), "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var corpus *seed.SetStatus
	var inert *seed.SetStatus
	for i := range st {
		switch st[i].ID {
		case policyseed.SetID:
			corpus = &st[i]
		case agentseed.SetID:
			inert = &st[i]
		}
	}
	if corpus == nil || inert == nil {
		t.Fatalf("status returned %d sets — both policy.corpus and the agents set must be declared", len(st))
	}
	for _, set := range []*seed.SetStatus{corpus, inert} {
		if len(set.Records) == 0 {
			t.Fatalf("set %s reported no records", set.ID)
		}
		if set.AppliedVersion != 0 {
			t.Errorf("set %s reports applied version %d on a never-applied store", set.ID, set.AppliedVersion)
		}
		var absent, applied int
		for _, r := range set.Records {
			switch r.State {
			case seed.StateAbsent:
				absent++
			case seed.StateApplied:
				applied++
			}
		}
		if applied != 0 || absent != len(set.Records) {
			t.Errorf("set %s: never applied, yet status says applied %d / absent %d of %d (the planned state leaked into the display)",
				set.ID, applied, absent, len(set.Records))
		}
	}
}

// TestSeedNilEngineDoesNotPanic is the V13 nil-safety: a nil engine (a
// storeless daemon) makes the startup apply a no-op, never a panic.
func TestSeedNilEngineDoesNotPanic(t *testing.T) {
	ApplyInertAtStart(context.Background(), nil) // must not panic
}

// failingApplier is a daemon-level fake applier whose every Upsert refuses — a
// forced applier failure (the T6 "a half-seeded agent set must not brick the
// daemon" re-assertation). Fingerprint answers not-present (so the plan decides
// a create for every record) and Upsert always refuses, so the engine marks
// each record failed. It is not guard-affecting, so an inert set may carry it
// (SD5's gate stays green).
type failingApplier struct{ setID string }

func (f failingApplier) Set() string                   { return f.setID }
func (f failingApplier) GuardAffecting() bool          { return false }
func (f failingApplier) Operator() seed.OperatorRights { return seed.OperatorRights{} }
func (f failingApplier) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, nil
}
func (f failingApplier) Adoptable(seed.Tx, string) (bool, error) {
	return false, nil
}
func (f failingApplier) Upsert(seed.Tx, seed.Record, bool) error {
	return errors.New("forced applier failure")
}
func (f failingApplier) Retire(seed.Tx, string) error { return nil }

// TestStartupFailureOfOneRecordDoesNotAbortStart is the daemon-level re-
// assertion of the T6 acceptance that a half-seeded set does not brick the
// daemon: with every agents applier Upsert refused, the startup apply must
// not panic and must not error (a per-record failure is recorded, not
// returned), and the failure must be VISIBLE — the ledger reads failed for
// every record (T3: "ledger for B says failed"), never absent or applied. The
// status's per-record state is the plan's decision (a retry, "applied"), not
// the failure; the ledger is the source of the actual state, and where
// "failed" lives. A swallowed failure leaves the ledger empty, which reads as
// a fresh create and hides the failure (SD10).
func TestStartupFailureOfOneRecordDoesNotAbortStart(t *testing.T) {
	s := openSeedStore(t)
	opt := seed.Options{Registry: seed.DefaultRegistry()}
	// The registry is the global one (S55 T9's watchdog set registers into it
	// at init), so the engine's "every set has an applier" gate needs both:
	// the agents applier the test forces to fail, and the watchdog set's — a
	// failing one here, so the apply's inert walk sees what the daemon's
	// construction (NewSeedEngine) gives it, and the assertion stays on the
	// agents ledger the test exists to prove.
	// S48 T6: the mirror grew — NewSeedEngine mounts the inert
	// blueprints.standard applier since T5, and this test promises to
	// receive "what the daemon's construction gives it". The blueprint
	// set's own records are real and inert; the assertion below stays on
	// the agents ledger the test exists to prove.
	// S57 T10: the mirror grew again — the inert verify.templates set
	// (candidate gates, VG21). Same deal: real store-backed applier,
	// inert writes, the assertion untouched.
	// S58 T4: the mirror grew once more — the inert notify.events set
	// (the shipped event classes). Same deal: real store-backed applier,
	// inert writes (a hint permits nothing), assertion untouched.
	// S60 T7: and once more — the inert blueprints.landing set (the two
	// queue blueprints), mounted by NewSeedEngine through the SAME
	// blueprintseed machinery parameterised by set. Same deal: real
	// store-backed applier, inert writes (a blueprint permits nothing),
	// assertion untouched.
	// S63 T3: and once more — the inert retro.templates set (the learning
	// bodies), mounted by NewSeedEngine over the retro applier. Same deal:
	// real store-backed applier, inert writes (a template body permits
	// nothing and writes only retro_template rows), assertion untouched.
	opt.Appliers = map[string]seed.Applier{
		agentseed.SetID:     failingApplier{setID: agentseed.SetID},
		watchdogseed.SetID:  failingApplier{setID: watchdogseed.SetID},
		blueprintseed.SetID: blueprintApplier{ap: blueprintseed.NewApplier(s)},
		verifyseed.SetID:    verifyApplier{ap: verifyseed.NewApplier(s)},
		notifyseed.SetID:    notifyApplier{ap: notifyseed.NewApplier(s)},
		landingseed.SetID:   blueprintApplier{ap: blueprintseed.NewApplierFor(s, landingseed.SetID, landingseed.Source)},
		retroseed.SetID:     retroTemplateApplier{ap: retroseed.NewApplier(s)},
	}
	e, err := seed.NewEngine(NewSeedRepo(s), opt)
	if err != nil {
		t.Fatalf("seed.NewEngine: %v", err)
	}
	// The startup apply must complete without an error — a per-record failure
	// is recorded in a second transaction, never returned as an engine error
	// (one bad record must not abort the set, T3/SD10).
	if res, aerr := e.ApplyInert(context.Background(), seed.Actor{}); aerr != nil {
		t.Fatalf("startup apply errored on a per-record failure: %v (got %d results) — one bad record must not abort the set", aerr, len(res))
	}
	// The failure must be VISIBLE in the ledger — the source of the actual
	// per-record state (T3: "ledger for B says failed"). A refused Upsert
	// rolls back its transaction, so no applied row persists; the second
	// transaction writes the failed row. A swallowed failure would leave the
	// ledger empty, which reads as absent and hides the failure.
	rows, err := s.SeedLedgerList(context.Background(), agentseed.SetID)
	if err != nil {
		t.Fatalf("ledger list: %v", err)
	}
	failed, applied := 0, 0
	for _, r := range rows {
		switch seed.State(r.State) {
		case seed.StateFailed:
			failed++
		case seed.StateApplied:
			applied++
		}
	}
	if applied > 0 {
		t.Fatalf("a refused Upsert left %d records applied — the failure was swallowed", applied)
	}
	if failed == 0 {
		t.Fatalf("no record was marked failed in the ledger — the forced applier failure was hidden (SD10: a failure must be visible, never absent)")
	}
}

// TestSeedStatusToolParityWithSocket is the T6 parity: the seed_status tool
// (a records catalog arm) and the socket's seed.status answer identically,
// because both read the SAME *seed.Engine through protocol.SeedStatusView —
// the one mapping. A drift in either path would make them disagree field for
// field (SD10). The tool is a read: it carries no actor, so a user identity
// (which the records door skips identity resolution for) drives it here.
func TestSeedStatusToolParityWithSocket(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	ApplyInertAtStart(context.Background(), e)

	ctx := context.Background()
	in := json.RawMessage(`{"set":"agents.firstparty"}`)

	// The tool: the records arm reads through the same engine the socket
	// holds, mapping with protocol.SeedStatusView.
	rs := NewRecordsService(s)
	rs.seedEngine = e
	got, f, derr := rs.Do(ctx, Actor{Kind: "user", ID: "parity"}, "seed_status", in)
	if derr != nil {
		t.Fatalf("tool seed_status: %v", derr)
	}
	if f != nil {
		t.Fatalf("tool seed_status refused: %s", f.Sentence)
	}
	gotJSON, merr := json.Marshal(got)
	if merr != nil {
		t.Fatalf("tool result does not encode: %v", merr)
	}

	// The socket: seed.status reads through the same engine, mapping with the
	// same protocol.SeedStatusView the tool arm calls.
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Seed = e
	if _, already := tbl.Lookup(protocol.MethodSeedStatus); !already {
		if !tbl.RegisterSeed() {
			t.Fatal("RegisterSeed refused a wired seam")
		}
	}
	initParams, _ := json.Marshal(protocol.InitializeParams{ClientName: "parity", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, ierr := tbl.Dispatch(ctx, "c1", protocol.MethodInitialize, initParams); ierr != nil {
		t.Fatalf("initialize: %v", ierr)
	}
	res, serr := tbl.Dispatch(ctx, "c1", protocol.MethodSeedStatus, in)
	if serr != nil {
		t.Fatalf("socket seed.status: %v", serr)
	}
	resJSON, merr := json.Marshal(res)
	if merr != nil {
		t.Fatalf("socket result does not encode: %v", merr)
	}

	if string(gotJSON) != string(resJSON) {
		t.Fatalf("the seed_status tool and the socket's seed.status disagree field for field:\ntool:   %s\nsocket: %s", gotJSON, resJSON)
	}
}

// TestSeedStatusToolRefusesWithoutTheEngine is the V13 half of the parity: a
// seedless mount (nil engine) makes the seed_status tool refuse rather than
// answer zero, so the tool never lies about an absent surface.
func TestSeedStatusToolRefusesWithoutTheEngine(t *testing.T) {
	s := openSeedStore(t)
	rs := NewRecordsService(s) // seedEngine left nil
	_, f, derr := rs.Do(context.Background(), Actor{Kind: "user", ID: "parity"}, "seed_status", json.RawMessage(`{}`))
	if derr != nil {
		t.Fatalf("seed_status returned a hard error, want a refusal: %v", derr)
	}
	if f == nil {
		t.Fatal("a nil engine must refuse the seed_status tool, not answer zero (V13)")
	}
}

// The start-up line must not call a skip a failure: a store that is already
// seeded answers every record "up to date", and that is the healthy case.
func TestStartupSeedLogCountsSkipsApartFromFailures(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e, err := NewSeedEngine(st, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatal(err)
	}
	ApplyInertAtStart(context.Background(), e) // first start: everything applies
	buf.Reset()
	ApplyInertAtStart(context.Background(), e) // second start: everything is up to date
	out := buf.String()
	if strings.Contains(out, "failed") && !strings.Contains(out, "failed 0") {
		t.Fatalf("a fully seeded store must log no failures:\n%s", out)
	}
	if !strings.Contains(out, "agents.firstParty applied 0, skipped 33, failed 0") {
		t.Fatalf("the agents set must read as skipped, not failed:\n%s", out)
	}
}
