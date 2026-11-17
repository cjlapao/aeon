package main

// environment_proposal_test.go — S59 T7 (EV16, EV3, EV17, TD3, TD4): the
// environment_restrict kind, tested where the kind lives — the create door,
// the commit door, and the two rows they move (the proposal row and the
// environment row). Every assertion is about what a person and a model can
// each prove to the other:
//
//   - the create door REFUSES a widening with the validator's own sentence,
//     every loosening named, and stores nothing (an inert row that loosens is
//     still a loosening waiting for a careless commit);
//   - the same bytes come out of the create door and the validator, because
//     there is one function behind both (TD3);
//   - a filed proposal changes NOTHING — not the spec, not the revision, not
//     one audit row (EV16's inertness, EV17's "an ask is not a change");
//   - the commit tightens through the store's own CAS door, stamped with the
//     COMMITTER and naming the PROPOSAL in the audit line (two identities,
//     both recorded, neither guessed);
//   - a baseline that has moved is refused, not clobbered (EV17's lost
//     update); an ask that already landed answers "already applied" on the
//     canonical bytes and burns no revision (TD4);
//   - and an apply whose context carries no committer, or no proposal id,
//     refuses rather than writing with a guessed actor — the fail-closed half
//     the desk cannot enforce for itself.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// envPropSlug is the one environment every test in this file asks about.
const envPropSlug = "t7-proposal-env"

// envPropCapSlug is the capped row — the container backend, because that is
// the backend whose caps the validator agrees can be enforced (EV13).
const envPropCapSlug = "t7-capped-env"

type envProposalEnv struct {
	t   *testing.T
	st  *store.Store
	d   *daemon.Daemon
	id  string // the seeded host row's id
	rev int64
	// capID is the seeded container row — the one that may carry caps,
	// because a host row's caps are the lie EV13 refuses to tell.
	capID  string
	capRev int64
}

func newEnvProposalEnv(t *testing.T) envProposalEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	row, ref, err := st.CreateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewEnvironment{
		Slug: envPropSlug, Name: "T7 proposal env", Backend: "host-scrubbed",
		// No caps: execenv.Validate refuses a memory limit on a host
		// backend, so a seeded row carries only what the plane would accept
		// from a person. The capped row below is the container one.
		Spec: envPropSpec(t, []string{"PATH", "HOME", "LANG"}, nil, nil),
	})
	if ref != nil || err != nil {
		t.Fatalf("seed the environment: ref=%v err=%v", ref, err)
	}
	capRow, ref, err := st.CreateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewEnvironment{
		Slug: envPropCapSlug, Name: "T7 capped env", Backend: "container",
		Spec: envPropContainerSpec(t, []string{"PATH", "HOME", "LANG"}, envInt(512)),
	})
	if ref != nil || err != nil {
		t.Fatalf("seed the capped environment: ref=%v err=%v", ref, err)
	}
	return envProposalEnv{t: t, st: st, d: d, id: row.ID, rev: row.Revision,
		capID: capRow.ID, capRev: capRow.Revision}
}

// envPropSpec builds a spec the way a person's write reaches the store —
// parsed, canonical bytes — so the tests speak the shape the validator signs
// rather than a hand-typed JSON that could drift from it. A nil limit is
// ABSENT, which `Restricts` reads as the loosest state of all (EV16), so the
// seeded row carries its caps explicitly and a proposal has to keep them.
func envPropSpec(t *testing.T, passthrough []string, memoryMiB *int, allow []string) string {
	t.Helper()
	spec := execenv.Spec{
		Env:    execenv.EnvSection{Passthrough: passthrough},
		Home:   execenv.HomeReal,
		Limits: execenv.Limits{MemoryMiB: memoryMiB},
		Egress: execenv.Egress{Mode: "unenforced", Allow: allow},
	}
	return string(execenv.Canonical(spec))
}

// envPropContainerSpec is the one shape that may carry a cap at all:
// execenv.Validate refuses limits.memoryMiB on a host backend — the cap
// cannot be enforced there, and accepting it would be the lie EV13 refuses to
// tell — so the cap axes are exercised against a container row. Image and the
// scratch home are what that backend needs.
func envPropContainerSpec(t *testing.T, passthrough []string, memoryMiB *int) string {
	t.Helper()
	spec := execenv.Spec{
		Env:    execenv.EnvSection{Passthrough: passthrough},
		Home:   execenv.HomeScratch,
		Limits: execenv.Limits{MemoryMiB: memoryMiB},
		Egress: execenv.Egress{Mode: "unenforced"},
		Image:  &execenv.Image{Ref: "debian:bookworm-slim", Pull: execenv.PullMissing},
	}
	return string(execenv.Canonical(spec))
}

func envInt(v int) *int { return &v }

func envPropPayload(t *testing.T, id, spec string, base int64, reason string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"environmentId": id, "spec": spec, "baseRevision": base, "reason": reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func envPropInitialize(t *testing.T, d *daemon.Daemon, conn string) {
	t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "env", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		t.Fatal(err)
	}
}

func (e envProposalEnv) createViaSocket(conn, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": string(desk.KindEnvironmentRestrict), "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

func (e envProposalEnv) commit(id int64, payload, byID string) (desk.CommitResult, error) {
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

func (e envProposalEnv) environment(t *testing.T) store.Environment {
	t.Helper()
	row, err := e.st.GetEnvironment(e.id)
	if err != nil {
		t.Fatalf("read the environment: %v", err)
	}
	return row
}

func (e envProposalEnv) capped(t *testing.T) store.Environment {
	t.Helper()
	row, err := e.st.GetEnvironment(e.capID)
	if err != nil {
		t.Fatalf("read the capped environment: %v", err)
	}
	return row
}

func (e envProposalEnv) changes(t *testing.T) []store.EnvironmentChange {
	t.Helper()
	rows, err := e.st.ListChanges(store.EnvironmentChangeFilter{SubjectKind: "environment", SubjectID: e.id, Limit: 100})
	if err != nil {
		t.Fatalf("read the audit: %v", err)
	}
	return rows
}

// TestEnvironmentRestrictCreateRefusesEveryWidening is EV16 at the create
// door, in four shapes of the same crime — one name more, one name dropped
// from a cap's reach, a cap raised, egress switched on — plus the backend
// move that is not even a proposal shape. Each refusal carries the validator's
// OWN sentence with the axis named, and not one row is stored.
func TestEnvironmentRestrictCreateRefusesEveryWidening(t *testing.T) {
	e := newEnvProposalEnv(t)
	envPropInitialize(t, e.d, "c-env-widen")
	before, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	// Each case names the row it asks about: the caps live on the container
	// row because a host row may not carry them at all (EV13's honesty), and
	// `Restricts` runs BEFORE the validator, so a widening meets the axis
	// sentence rather than a backend complaint about the same field.
	cases := []struct {
		name   string
		capped bool
		spec   string
		must   []string
	}{
		{"one more name", false, envPropSpec(t, []string{"PATH", "HOME", "LANG", "SHELL"}, nil, nil),
			[]string{"WIDEN", "spec.passthrough", "SHELL"}},
		{"egress allowlist added", false, envPropSpec(t, []string{"PATH", "HOME", "LANG"}, nil, []string{"api.example.com"}),
			[]string{"WIDEN", "spec.egress", "egress.allow added"}},
		{"real home swapped for none", false, func() string {
			return strings.Replace(envPropSpec(t, []string{"PATH", "HOME", "LANG"}, nil, nil),
				`"home":"`+execenv.HomeReal+`"`, `"home":"nonsense"`, 1)
		}(), []string{"WIDEN", "spec.home"}},
		{"cap raised", true, envPropContainerSpec(t, []string{"PATH", "HOME", "LANG"}, envInt(1024)),
			[]string{"WIDEN", "spec.limits", "memoryMiB"}},
		{"cap removed", true, envPropContainerSpec(t, []string{"PATH", "HOME", "LANG"}, nil),
			[]string{"WIDEN", "spec.limits", "memoryMiB limit removed"}},
	}
	for _, tc := range cases {
		id, rev := e.id, e.rev
		if tc.capped {
			id, rev = e.capID, e.capRev
		}
		payload := envPropPayload(t, id, tc.spec, rev, "the loop would like this")
		_, err := e.createViaSocket("c-env-widen", payload)
		if err == nil {
			t.Fatalf("%s: a WIDENING became a proposal — EV16 exists to refuse exactly this", tc.name)
		}
		var perr *protocol.Error
		if !errors.As(err, &perr) || perr.Data.What == "" {
			t.Fatalf("%s: the refusal is not the refused shape with a What: %v", tc.name, err)
		}
		for _, needle := range tc.must {
			if !strings.Contains(perr.Data.What, needle) {
				t.Fatalf("%s: the refusal does not carry %q verbatim: %s", tc.name, needle, perr.Data.What)
			}
		}
		// The row did not move by one revision while being refused (EV17).
		target := e.environment(t)
		wantRev := e.rev
		if tc.capped {
			target, wantRev = e.capped(t), e.capRev
		}
		if target.Revision != wantRev {
			t.Fatalf("%s: the refused ask moved the row to revision %d (EV17)", tc.name, target.Revision)
		}
	}
	after, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("the refused creates wrote %d rows — a loosening is not inert just because it is called a proposal", len(after)-len(before))
	}
}

// TestEnvironmentRestrictCreateAndCommitValidatorsAreOneFunction is TD3 for
// the new kind: the create door's refusal and the validator's carry the same
// bytes, because there is one function behind both doors — a person reading a
// rejected proposal reads the truth the validator used, not a paraphrase.
func TestEnvironmentRestrictCreateAndCommitValidatorsAreOneFunction(t *testing.T) {
	e := newEnvProposalEnv(t)
	envPropInitialize(t, e.d, "c-env-one")
	bad := envPropPayload(t, e.id, envPropSpec(t, []string{"PATH", "HOME", "LANG", "SHELL"}, envInt(512), nil), e.rev, "widen please")
	_, doorErr := e.createViaSocket("c-env-one", bad)
	if doorErr == nil {
		t.Fatal("the create door accepted a widening")
	}
	direct := validateEnvironmentRestrictProposal([]byte(bad))
	if direct == nil {
		t.Fatal("the validator accepted what the door refused")
	}
	var perr *protocol.Error
	if !errors.As(doorErr, &perr) {
		t.Fatalf("the door did not answer the refused shape: %v", doorErr)
	}
	if perr.Data.What != direct.Error() {
		t.Fatalf("the doors drifted:\n door:  %s\n valid: %s", perr.Data.What, direct)
	}
	// And the unknown row: one function, one sentence, both doors.
	ghost := envPropPayload(t, "env-nope", envPropSpec(t, []string{"PATH"}, nil, nil), 1, "tighten")
	if err := validateEnvironmentRestrictProposal([]byte(ghost)); err == nil || !strings.Contains(err.Error(), "environment_list") {
		t.Fatalf("the validator's unknown-row refusal is %v, want one naming environment_list", err)
	}
	if _, err := e.createViaSocket("c-env-one", ghost); err == nil {
		t.Fatal("the create door stored an ask about a row that does not exist")
	}
}

// TestEnvironmentRestrictProposalIsInertUntilCommitted is EV16's positive
// half stated as a row count: a filed ask is a proposal row and NOTHING else.
// The environment's spec bytes, its revision and its audit are untouched.
func TestEnvironmentRestrictProposalIsInertUntilCommitted(t *testing.T) {
	e := newEnvProposalEnv(t)
	envPropInitialize(t, e.d, "c-env-inert")
	tight := envPropSpec(t, []string{"PATH"}, nil, nil)
	before := e.environment(t)
	changesBefore := len(e.changes(t))

	payload := envPropPayload(t, e.id, tight, e.rev, "LANG is not read by the build")
	if err := validateEnvironmentRestrictProposal([]byte(payload)); err != nil {
		t.Fatalf("a tightening ask should stand; the validator said: %v", err)
	}
	row, err := e.createViaSocket("c-env-inert", payload)
	if err != nil {
		t.Fatalf("a tightening ask should stand at the door too: %v", err)
	}
	if row.Status != "proposed" {
		t.Errorf("the row is %q, want proposed", row.Status)
	}
	after := e.environment(t)
	if after.Spec != before.Spec {
		t.Errorf("the FILED ask changed the spec\n was: %s\n now: %s — an ask constrains nothing until a person commits it (EV16)", before.Spec, after.Spec)
	}
	if after.Revision != before.Revision {
		t.Errorf("the filed ask moved the revision %d → %d", before.Revision, after.Revision)
	}
	if got := len(e.changes(t)); got != changesBefore {
		t.Errorf("the filed ask wrote %d audit rows beyond the seed's %d — the audit is for changes (EV17)", got, changesBefore)
	}
}

// TestEnvironmentRestrictCommitTightensAsTheCommitter is the whole reason the
// kind exists, read back from the two rows it moves: the environment now says
// the tighter spec at the next revision, and the audit line names BOTH the
// person who committed it and the proposal that asked.
func TestEnvironmentRestrictCommitTightensAsTheCommitter(t *testing.T) {
	e := newEnvProposalEnv(t)
	envPropInitialize(t, e.d, "c-env-commit")
	tight := envPropSpec(t, []string{"PATH"}, nil, nil)
	payload := envPropPayload(t, e.id, tight, e.rev, "LANG is not read by the build")
	if err := validateEnvironmentRestrictProposal([]byte(payload)); err != nil {
		t.Fatalf("the validator refused its own tightening: %v", err)
	}
	row, err := e.createViaSocket("c-env-commit", payload)
	if err != nil {
		t.Fatalf("the create door refused its own tightening: %v", err)
	}
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatalf("the commit failed: %v", err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q", res.Row.Status)
	}
	after := e.environment(t)
	if after.Revision != e.rev+1 {
		t.Errorf("the row is at revision %d, want %d — the committed ask must land through the plane's own write", after.Revision, e.rev+1)
	}
	want := string(execenv.Canonical(mustParseSpec(t, tight)))
	if after.Spec != want {
		t.Errorf("the row does not carry the canonical proposed spec\n got:  %s\n want: %s", after.Spec, want)
	}
	if strings.Contains(after.Spec, "LANG") {
		t.Error("LANG survived the tightening")
	}
	changes := e.changes(t)
	if len(changes) < 2 {
		t.Fatalf("the audit holds %d rows, want the seed's plus the committed tightening", len(changes))
	}
	// The audit read is ORDER BY id DESC (store/environment_change.go), so the
	// newest row is the FIRST — reading the other end is how a test asserts on
	// the seed's create row and passes on luck.
	last := changes[0]
	if last.ActorKind != "user" || last.ActorID != "u-committ" {
		t.Errorf("the audit row carries actor %s/%s — the COMMITTER writes the row, never the proposer (S59 T7)", last.ActorKind, last.ActorID)
	}
	if !strings.Contains(last.Summary, "tightened by committed proposal #") || !strings.Contains(last.Summary, "#"+strconv.FormatInt(row.ID, 10)) {
		t.Errorf("the audit line does not name the proposal that produced it: %q", last.Summary)
	}
	if !strings.Contains(last.Summary, "LANG is not read") {
		t.Errorf("the audit line lost the proposer's reason: %q", last.Summary)
	}
}

// TestEnvironmentRestrictCommitRefusesAStaleBaseline is EV17's lost update at
// the commit door: the ask was written against revision N, a person edited the
// row in the meantime, and the committed ask meets the store's own
// compare-and-set refusal instead of clobbering the edit.
func TestEnvironmentRestrictCommitRefusesAStaleBaseline(t *testing.T) {
	e := newEnvProposalEnv(t)
	envPropInitialize(t, e.d, "c-env-stale")
	tight := envPropSpec(t, []string{"PATH"}, nil, nil)
	payload := envPropPayload(t, e.id, tight, e.rev, "tighten against the old row")
	if err := validateEnvironmentRestrictProposal([]byte(payload)); err != nil {
		t.Fatalf("the validator refused its own tightening before the edit: %v", err)
	}
	row, err := e.createViaSocket("c-env-stale", payload)
	if err != nil {
		t.Fatalf("the door refused its own tightening: %v", err)
	}
	// A person tightens the row first, by hand.
	edited, ref, err := e.st.UpdateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.UpdateEnvironment{
		ID: e.id, Spec: envPropSpec(t, []string{"PATH", "HOME"}, nil, nil), ExpectedRevision: e.rev,
	})
	if ref != nil || err != nil {
		t.Fatalf("the person's edit: ref=%v err=%v", ref, err)
	}
	if edited.Revision != e.rev+1 {
		t.Fatalf("the person's edit landed at %d, want %d", edited.Revision, e.rev+1)
	}

	_, err = e.commit(row.ID, payload, "u-late")
	if err == nil {
		t.Fatal("the committed ask clobbered a person's later edit — EV17's lost update, the CAS exists to refuse it")
	}
	now := e.environment(t)
	if strings.Contains(now.Spec, `"LANG"`) || now.Revision != e.rev+1 {
		t.Errorf("the row moved to revision %d — the refused commit changed it anyway", now.Revision)
	}
	rows, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rows {
		if p.ID == row.ID && p.Status == "committed" {
			t.Error("the proposal says committed while the row was never tightened")
		}
	}
}

// TestEnvironmentRestrictAlreadyAppliedIsHonest is TD4 for the kind: an ask
// that has already landed answers "already applied" on the CANONICAL bytes —
// so a re-delivered commit completes instead of burning a revision, and a spec
// that differs only in spelling is the same effect, not a second one.
func TestEnvironmentRestrictAlreadyAppliedIsHonest(t *testing.T) {
	e := newEnvProposalEnv(t)
	tight := envPropSpec(t, []string{"PATH"}, nil, nil)
	applied, ref, err := e.st.UpdateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.UpdateEnvironment{
		ID: e.id, Spec: tight, ExpectedRevision: e.rev,
	})
	if ref != nil || err != nil {
		t.Fatalf("apply the tightening by hand: ref=%v err=%v", ref, err)
	}
	// The same effect, spelled differently: re-marshal through a map so key
	// order and spacing differ while the meaning does not.
	var raw map[string]any
	if err := json.Unmarshal([]byte(tight), &raw); err != nil {
		t.Fatal(err)
	}
	reshuffled, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	payload := envPropPayload(t, e.id, string(reshuffled), applied.Revision, "already the case")
	ok, err := seamAlreadyAppliedEnvironmentRestrict(context.Background(), []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("the already-applied gate said false on the same effect spelled differently:\n stored: %s\n asked:  %s", applied.Spec, reshuffled)
	}
	// And it says false when the row has NOT landed — the gate must not
	// paper over a real apply.
	notYet := envPropPayload(t, e.id, envPropSpec(t, []string{"PATH", "HOME"}, envInt(1024), nil), applied.Revision, "a cap nobody applied")
	ok, err = seamAlreadyAppliedEnvironmentRestrict(context.Background(), []byte(notYet))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("the already-applied gate said true on a spec the row does not carry")
	}
}

// TestEnvironmentRestrictApplyFailsClosedWithoutItsTwoIdentities is the half
// the desk cannot enforce for itself: an apply whose context carries no
// committer, or no proposal id, refuses rather than writing with a guessed
// actor — the audit gap the two channels exist to close.
func TestEnvironmentRestrictApplyFailsClosedWithoutItsTwoIdentities(t *testing.T) {
	e := newEnvProposalEnv(t)
	payload := []byte(envPropPayload(t, e.id, envPropSpec(t, []string{"PATH"}, nil, nil), e.rev, "tighten"))

	if err := seamApplyEnvironmentRestrict(context.Background(), nil, payload); err == nil ||
		!strings.Contains(err.Error(), "committer") {
		t.Errorf("an apply with no committer wrote (or failed the wrong way): %v", err)
	}
	decided := desk.WithCommitActor(desk.WithDecider(context.Background(), desk.Decider{Kind: "user", ID: "u-x"}), desk.Actor{Kind: "user", ID: "u-x"})
	if err := seamApplyEnvironmentRestrict(decided, nil, payload); err == nil ||
		!strings.Contains(err.Error(), "proposal") {
		t.Errorf("an apply with no proposal id wrote (or failed the wrong way): %v", err)
	}
	now := e.environment(t)
	if now.Revision != e.rev || strings.Contains(now.Spec, `"LANG"`) == false {
		t.Errorf("the refused applies moved the row to revision %d — a refused apply is not a changing write", now.Revision)
	}
}

// TestEnvironmentRestrictKindIsRegisteredForBothDoors pins the registry half
// of TD3 structurally: the kind has ONE entry, its Validate is the function
// the create door calls, and the Apply the commit door calls is the one in
// this file — no second write path for the plane.
func TestEnvironmentRestrictKindIsRegisteredForBothDoors(t *testing.T) {
	e := newEnvProposalEnv(t)
	entry, ok := deskEffects.Effect(string(desk.KindEnvironmentRestrict))
	if !ok {
		t.Fatal("the desk has no environment_restrict entry — the kind is unregistered and every proposal of it is inert nonsense")
	}
	if entry.Validate == nil || entry.Apply == nil {
		t.Fatal("the entry is missing Validate or Apply — one door would run without the other (TD3)")
	}
	if entry.AlreadyApplied == nil {
		t.Error("the entry has no AlreadyApplied gate — a re-delivered commit would re-run the write (TD4)")
	}
	// The proposal kind is in the store's closed list, so a row of it can
	// exist at all, and the desk's constant is the store's string.
	if string(desk.KindEnvironmentRestrict) != store.ProposalKindEnvironmentRestrict {
		t.Errorf("the desk calls the kind %q and the store %q — two spellings of one kind is how a kind gets orphaned",
			desk.KindEnvironmentRestrict, store.ProposalKindEnvironmentRestrict)
	}
	found := false
	for _, k := range store.ProposalKinds {
		if k == store.ProposalKindEnvironmentRestrict {
			found = true
		}
	}
	if !found {
		t.Error("the store's closed kind list does not name environment_restrict")
	}
	// And a row of the kind can be created and read back (the CHECK accepts it).
	if _, err := e.st.CreateProposal(store.ProposalKindEnvironmentRestrict, "{}", store.Actor{Kind: "agent", ID: "a"}, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Errorf("the store refused to hold a row of the kind: %v", err)
	}
}

func mustParseSpec(t *testing.T, s string) execenv.Spec {
	t.Helper()
	spec, err := execenv.ParseSpec([]byte(s))
	if err != nil {
		t.Fatalf("the test's own spec does not parse: %v", err)
	}
	return spec
}
