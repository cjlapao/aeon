package main

// memory_proposal_test.go — S52 T4 (LM7, LM8, TD3, TD4): the
// learning_retract kind, tested where the kind lives — the create door,
// the commit door, and the two rows they move (the proposal row and the
// learning row). What each test pins, in the order:
//
//   - the create door refuses a nonexistent id, a zero id, a blank or
//     too-long reason and a smuggled field BEFORE a byte is stored, and
//     the door's refusal and validateLearningRetractProposal carry the
//     SAME sentence — one function behind both doors (TD3);
//   - an ask at rest is INERT, measured against the plane itself: the row
//     live, the audit career only its add, the payload stored byte for
//     byte as filed;
//   - the commit STAMPS through learnings' own Retract — reason verbatim,
//     the COMMITTER as actor on the stamp, the audit line and nothing
//     else, the proposer staying in the proposal's created_by_* (LM8);
//   - the crash-retry answers AlreadyApplied and completes WITHOUT a
//     second audit line (TD4); a re-commit of a retracted row after the
//     fact is refused by Validate, not silently re-stamped;
//   - and an apply whose context carries no committer, or no proposal id,
//     refuses rather than stamping with a guessed actor — LM8's audit gap
//     closed fail-closed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type memPropEnv struct {
	t  *testing.T
	st *store.Store
	d  *daemon.Daemon
	l  *learnings.Learnings
}

func newMemPropEnv(t *testing.T) memPropEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	// The agent scope "atlas" the seed files into must be a REAL agent —
	// the plane checks the slug against S43's table before it files (LM1
	// keys every readable scope on an identity that exists).
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Name: "Atlas", Slug: "atlas", Description: "d", Prompt: "p", Version: "1"}, "2026-11-04T00:00:00Z"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return memPropEnv{t: t, st: st, d: d, l: learnings.New(st)}
}

func (e memPropEnv) initialize(conn string) {
	e.t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "memory", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		e.t.Fatal(err)
	}
}

// seedMemory files one live learning through the plane (a person's own
// add — the ask below asks to take THIS back) and returns its id.
func (e memPropEnv) seedMemory(body string) int64 {
	e.t.Helper()
	got, ref := e.l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "atlas", Body: body, Provenance: learnings.ProvenanceObserved}, learnings.Actor{Kind: "user", ID: "person"})
	if ref != nil {
		e.t.Fatalf("seed: %s", ref.Reason)
	}
	return got.ID
}

func memRetractPayload(t *testing.T, id int64, reason string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"id": id, "reason": reason})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e memPropEnv) createViaSocket(conn, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": "learning_retract", "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

func (e memPropEnv) commit(id int64, payload, byID string) (desk.CommitResult, error) {
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

func (e memPropEnv) history(id int64) []learnings.Change {
	e.t.Helper()
	rows, err := e.l.History(id)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestRetractCreateRefusesWhatTheLearningPlaneRefuses(t *testing.T) {
	e := newMemPropEnv(t)
	e.initialize("c-refuse")
	live := e.seedMemory("the deploy gate counts flake retries, not the first failure")
	cases := []struct {
		name    string
		payload string
		wants   []string
	}{
		{"nonexistent id", memRetractPayload(t, 424242, "no such row"), []string{"no learning 424242 exists", "learnings.board"}},
		{"zero id", memRetractPayload(t, 0, "nothing"), []string{"positive id"}},
		{"blank reason", memRetractPayload(t, live, "   "), []string{"reason"}},
		{"too-long reason", memRetractPayload(t, live, strings.Repeat("r", 257)), []string{"reason"}},
		{"smuggled actor", `{"id":` + fmt.Sprint(live) + `,"reason":"why","actor":"ghost"}`, []string{`unknown field "actor"`}},
		{"smuggled state", `{"id":` + fmt.Sprint(live) + `,"reason":"why","retractedAt":"2026-11-04T00:00:00Z"}`, []string{"unknown field"}},
	}
	before := e.history(live)
	for _, c := range cases {
		if _, err := e.createViaSocket("c-refuse", c.payload); err == nil {
			t.Errorf("%s: the create door stored a nothing-ask", c.name)
			continue
		} else {
			msg := err.Error()
			var perr *protocol.Error
			if errors.As(err, &perr) {
				msg = perr.Data.What
			}
			for _, w := range c.wants {
				if !strings.Contains(msg, w) {
					t.Errorf("%s: refusal %q does not name %q", c.name, msg, w)
				}
			}
		}
	}
	// Nothing was written and nothing was stamped or audited: every
	// refusal died above the store's write half.
	if got := e.history(live); len(got) != len(before) {
		t.Errorf("the refused asks wrote audit lines: %d → %d", len(before), len(got))
	}
}

// TestRetractCreateAndCommitValidatorsAreOneFunction is TD3 for the new
// kind: the door's refusal and validateLearningRetractProposal carry the
// SAME bytes, because one function stands behind both — and the store
// halves (it exists; it is not already retracted) are that function's, so
// the commit door re-runs them verbatim.
func TestRetractCreateAndCommitValidatorsAreOneFunction(t *testing.T) {
	e := newMemPropEnv(t)
	e.initialize("c-one")
	bad := memRetractPayload(t, 424242, "a row that never existed")
	_, doorErr := e.createViaSocket("c-one", bad)
	if doorErr == nil {
		t.Fatal("the create door accepted an ask about nothing")
	}
	direct := validateLearningRetractProposal([]byte(bad))
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
	// The already-retracted half is the validator's other store fact: the
	// person retracts by hand, and the standing ask for the same row now
	// refuses BOTH ways.
	gone := e.seedMemory("a rule the person already took back")
	if _, _, ref := e.l.Retract(gone, "superseded by the deploy rewrite", learnings.Actor{Kind: "user", ID: "person"}); ref != nil {
		t.Fatalf("seed retract: %+v", ref)
	}
	if err := validateLearningRetractProposal([]byte(memRetractPayload(t, gone, "again, please"))); err == nil || !strings.Contains(err.Error(), "already retracted") {
		t.Fatalf("the validator on an already-retracted row = %v, want the TD3 refusal", err)
	}
	if _, err := e.createViaSocket("c-one", memRetractPayload(t, gone, "again, please")); err == nil {
		t.Fatal("the create door stored an ask the validator refuses")
	}
}

func TestRetractAskIsInertAtRest(t *testing.T) {
	e := newMemPropEnv(t)
	e.initialize("c-inert")
	id := e.seedMemory("the flaky-suite note that deserves to be taken back")
	payload := memRetractPayload(t, id, "superseded once the cache race was fixed")
	row, err := e.createViaSocket("c-inert", payload)
	if err != nil {
		t.Fatalf("an honest ask should stand at the door: %v", err)
	}
	if row.Status != "proposed" {
		t.Fatalf("staged status = %q, want proposed", row.Status)
	}
	// Byte for byte: the payload the ask carried is the payload stored.
	var stored desk.LearningRetractProposal
	if err := json.Unmarshal(row.Payload, &stored); err != nil {
		t.Fatalf("stored payload %s does not decode: %v", row.Payload, err)
	}
	if stored.ID != id || stored.Reason != "superseded once the cache race was fixed" {
		t.Errorf("stored %+v, want id %d and the reason verbatim", stored, id)
	}
	// The learning lives on, unstamped, unaudited beyond its add.
	lr, found, err := e.l.Show(id)
	if err != nil || !found {
		t.Fatalf("show: %v %v", found, err)
	}
	if lr.Retracted || lr.RetractedReason != "" {
		t.Errorf("an ASK stamped the row (retracted=%v reason=%q) — an ask is not an act (LM7)", lr.Retracted, lr.RetractedReason)
	}
	if hist := e.history(id); len(hist) != 1 || hist[0].Op != "add" {
		t.Errorf("audit career at rest = %+v, want only the add", hist)
	}
}

// TestRetractCommitStampsTheCommitter is LM8 made visible: the commit
// stamps through learnings' own Retract with the COMMITTER as actor — the
// stamp, the reason, the ONE learning_change line (op retract, actor
// user/u-commit, the plane's own frozen sentence) — while the proposer
// stays exactly where LM8 put them: the proposal's created_by.
func TestRetractCommitStampsTheCommitter(t *testing.T) {
	e := newMemPropEnv(t)
	e.initialize("c-commit")
	id := e.seedMemory("a stale note a person agreed to take back")
	payload := memRetractPayload(t, id, "the flake was the cache, fixed in the retry rewrite")
	row, err := e.createViaSocket("c-commit", payload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row.ID, payload, "u-commit")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.Row.Status != "committed" || !res.EffectApplied {
		t.Fatalf("commit answer = %q applied=%v, want committed and the effect marked applied", res.Row.Status, res.EffectApplied)
	}
	lr, found, err := e.l.Show(id)
	if err != nil || !found {
		t.Fatalf("show after commit: %v %v", found, err)
	}
	if !lr.Retracted || lr.RetractedReason != "the flake was the cache, fixed in the retry rewrite" {
		t.Errorf("stamp after commit = retracted=%v reason=%q, want stamped with the ask's reason verbatim", lr.Retracted, lr.RetractedReason)
	}
	hist := e.history(id)
	if len(hist) != 2 || hist[1].Op != "retract" {
		t.Fatalf("audit career after commit = %+v, want add + exactly one retract", hist)
	}
	if hist[1].ActorKind != "user" || hist[1].ActorID != "u-commit" {
		t.Errorf("retract line actor = %s/%s, want user/u-commit — the COMMITTER is the actor, never the proposer (LM8)", hist[1].ActorKind, hist[1].ActorID)
	}
}

// TestRetractCrashRetryCompletesWithoutASecondLine is TD4 at the seam:
// the stamp is already on the row — the crash's exact footprint (effect
// landed, row update did not) — and seamAlreadyAppliedLearningRetract
// answers TRUE, which is the answer decide.go completes the commit on
// WITHOUT re-running Apply: no second stamp, no second audit line. The
// gate is answered here, at the seam, not re-enacted through desk.
func TestRetractCrashRetryCompletesWithoutASecondLine(t *testing.T) {
	e := newMemPropEnv(t)
	e.initialize("c-retry")
	id := e.seedMemory("a note taken back between two heartbeats")
	payload := memRetractPayload(t, id, "applied once; the retry must not double-stamp")
	row, err := e.createViaSocket("c-retry", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.commit(row.ID, payload, "u-crash"); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	applied, err := seamAlreadyAppliedLearningRetract(context.Background(), []byte(payload))
	if err != nil || !applied {
		t.Fatalf("the retry gate answered %v/%v, want true — a retry would re-run Apply and double-stamp", applied, err)
	}
	if hist := e.history(id); len(hist) != 2 || hist[1].Op != "retract" {
		t.Errorf("audit career at the retry gate = %+v, want add + ONE retract (TD4)", hist)
	}
	// And the gate reads facts, not optimism: a live row's ask is NOT
	// already applied — true-by-default would silently complete asks
	// nobody honoured.
	other := e.seedMemory("a note that stands")
	applied, err = seamAlreadyAppliedLearningRetract(context.Background(), []byte(memRetractPayload(t, other, "take it back")))
	if err != nil || applied {
		t.Fatalf("the gate said already-applied to a LIVE row (%v/%v) — TD4's gate reads the stamp, never hopes", applied, err)
	}
}

func TestRetractApplyRefusesADoorWithNoCommitter(t *testing.T) {
	e := newMemPropEnv(t)
	id := e.seedMemory("a note that must never be stamped by a guessed actor")
	payload := []byte(memRetractPayload(t, id, "why"))

	// No decider at all.
	if err := seamApplyLearningRetract(context.Background(), nil, payload); err == nil || !strings.Contains(err.Error(), "committer") {
		t.Fatalf("no-committer apply = %v, want the committer refusal", err)
	}
	// A decider with no proposal id: the trace link is not optional.
	noID := desk.WithCommitActor(desk.WithDecider(context.Background(), desk.Decider{Kind: "user", ID: "u-x"}), desk.Actor{Kind: "user", ID: "u-x"})
	if err := seamApplyLearningRetract(noID, nil, payload); err == nil || !strings.Contains(err.Error(), "proposal") {
		t.Fatalf("no-proposal-id apply = %v, want the id refusal", err)
	}
	// Both refusals wrote nothing.
	if hist := e.history(id); len(hist) != 1 || hist[0].Op != "add" {
		t.Errorf("a refused apply still wrote: %+v", hist)
	}
}
