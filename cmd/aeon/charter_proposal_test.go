package main

// charter_proposal_test.go — S61 T5 (GI4): the charter_save proposal kind,
// inert at rest and a person's act at commit. The assertions are about
// rows: the create door refuses what charter.Validate refuses, with the
// SAME bytes the direct validator produces (one function, TD3); the commit
// writes ONE revision with the COMMITTER as actor, source 'proposal' and
// the proposal id as source_ref, never the proposer; a proposal changes no
// read until it is committed; and the second identical ask completes
// through AlreadyApplied without churning append-only history (TD4).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type charterPropEnv struct {
	t    *testing.T
	st   *store.Store
	d    *daemon.Daemon
	root string // the seeded root component's id — the charter's home
}

func newCharterPropEnv(t *testing.T) charterPropEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	ws := t.TempDir()
	pc, err := st.CreateComponent(store.NewComponent{Name: "charter-plane", Slug: "charter-plane", Root: ws})
	if err != nil || !pc.OK() {
		t.Fatalf("seed the root component: %v refusal=%q", err, pc.Reason)
	}
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	return charterPropEnv{t: t, st: st, d: d, root: pc.Component.ID}
}

func charterPayload(t *testing.T, rootID, bodyJSON, reason string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"rootId": rootID, "body": json.RawMessage(bodyJSON), "reason": reason})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e charterPropEnv) createViaSocket(conn, kind, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "charter", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		e.t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"kind": kind, "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

func (e charterPropEnv) commit(id int64, payload, byID string) (desk.CommitResult, error) {
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

// TestCharterSaveCreateCommitParity: a payload refused at create is refused
// by the same validator, in the same bytes, that the commit door re-runs —
// one function behind both doors (TD3). The shape refusal, the charter's
// own body sentence and the store's root sentences all travel verbatim.
func TestCharterSaveCreateCommitParity(t *testing.T) {
	e := newCharterPropEnv(t)
	for _, bad := range []struct {
		name    string
		payload string
		must    []string
	}{
		{"smuggled-actor", `{"rootId":"x","body":{"goal":"g"},"reason":"r","actor":{"kind":"user","id":"me"}}`, []string{"carries no field besides them"}},
		{"empty-goal", charterPayload(t, e.root, `{"goal":""}`, "r"), []string{"charter: goal is required"}},
		{"unknown-root", charterPayload(t, "/no-such-root", `{"goal":"a goal"}`, "r"), []string{"does not exist", "a charter belongs to a project that exists"}},
	} {
		_, doorErr := e.createViaSocket("c-parity-"+bad.name, "charter_save", bad.payload)
		if doorErr == nil {
			t.Fatalf("%s: the create door accepted it", bad.name)
		}
		direct := validateCharterSaveProposal([]byte(bad.payload))
		if direct == nil {
			t.Fatalf("%s: the validator accepted what the door refused", bad.name)
		}
		var perr *protocol.Error
		if !errors.As(doorErr, &perr) {
			t.Fatalf("%s: the door did not answer the refused shape: %v", bad.name, doorErr)
		}
		if perr.Data.What != direct.Error() {
			t.Fatalf("%s: the doors drifted:\n door:  %s\n valid: %s", bad.name, perr.Data.What, direct)
		}
		for _, needle := range bad.must {
			if !strings.Contains(perr.Data.What, needle) {
				t.Fatalf("%s: the refusal does not carry %q verbatim: %s", bad.name, needle, perr.Data.What)
			}
		}
	}
	// Nothing invalid was persisted.
	rows, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("refused creates stored %d rows", len(rows))
	}
}

// TestCharterSaveCommitUsesCommitterAsActor: the revision's actor is the
// person at the commit door; the proposer stays in created_by_*; the source
// is 'proposal' and the source_ref is the proposal's id (GI4).
func TestCharterSaveCommitUsesCommitterAsActor(t *testing.T) {
	e := newCharterPropEnv(t)
	payload := charterPayload(t, e.root, `{"goal":"ship the intake plane","stack":["go"]}`, "the charter this slice asks for")
	row, err := e.st.CreateProposal("charter_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q", res.Row.Status)
	}
	rev, err := e.st.CurrentCharter(e.root)
	if err != nil {
		t.Fatalf("the commit wrote no revision: %v", err)
	}
	if rev.ActorKind != "user" || rev.ActorID != "u-committ" {
		t.Errorf("revision actor = %s:%s, want user:u-committ — the committer, never the proposer (GI4)", rev.ActorKind, rev.ActorID)
	}
	if rev.Source != "proposal" || rev.SourceRef != fmt.Sprint(row.ID) {
		t.Errorf("revision source = %q/%q, want proposal/%d", rev.Source, rev.SourceRef, row.ID)
	}
	if !strings.Contains(rev.Body, "ship the intake plane") {
		t.Errorf("the revision body lost the proposed charter: %s", rev.Body)
	}
	again, ok, err := e.st.GetProposal(row.ID)
	if err != nil || !ok {
		t.Fatalf("read back the committed row: ok=%v err=%v", ok, err)
	}
	if again.CreatedByKind != "agent" || again.CreatedByID != "manny" {
		t.Errorf("the proposer's stamp moved: %s:%s", again.CreatedByKind, again.CreatedByID)
	}
	if again.DecidedByKind != "user" || again.DecidedByID != "u-committ" {
		t.Errorf("the decider's stamp = %s:%s, want user:u-committ", again.DecidedByKind, again.DecidedByID)
	}
}

// TestCharterSaveIsInertUntilCommitted: create writes a row and NOTHING
// else; the commit writes the one revision; and a second ask carrying the
// same body completes through AlreadyApplied with no churn — append-only
// history never repeats a byte (TD4).
func TestCharterSaveIsInertUntilCommitted(t *testing.T) {
	e := newCharterPropEnv(t)
	payload := charterPayload(t, e.root, `{"goal":"inert until a person says so"}`, "prove inertness")
	row, err := e.createViaSocket("c-inert", "charter_save", payload)
	if err != nil {
		t.Fatal(err)
	}
	svc := &charter.Service{Store: e.st}
	if revs, err := svc.Revisions(context.Background(), e.root); err != nil || len(revs) != 0 {
		t.Fatalf("create was not inert: %d revisions (err %v)", len(revs), err)
	}
	if _, err := e.commit(row.ID, payload, "u-committ"); err != nil {
		t.Fatal(err)
	}
	revs, err := svc.Revisions(context.Background(), e.root)
	if err != nil || len(revs) != 1 {
		t.Fatalf("after commit: %d revisions (err %v), want 1", len(revs), err)
	}
	// The identical ask again — TD4: the crash-retry completes, no churn.
	row2, err := e.st.CreateProposal("charter_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row2.ID, payload, "u-committ")
	if err != nil {
		t.Fatalf("the AlreadyApplied commit failed: %v", err)
	}
	if res.Row.Status != "committed" || res.EffectApplied {
		t.Fatalf("second commit: status=%q effectApplied=%v, want committed/false — TD4 completes without reapplying", res.Row.Status, res.EffectApplied)
	}
	if revs, err := svc.Revisions(context.Background(), e.root); err != nil || len(revs) != 1 {
		t.Fatalf("history churned on a re-send: %d revisions (err %v), want 1", len(revs), err)
	}
}
