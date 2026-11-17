package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeProposalStore struct {
	rows       []ProposalRow
	nextID     int64
	created    int
	discardErr error
	discardID  int64
}

func (f *fakeProposalStore) CreateProposal(_ context.Context, kind string, payload json.RawMessage, byKind, byID string) (ProposalRow, error) {
	f.created++
	f.nextID++
	row := ProposalRow{ID: f.nextID, Kind: kind, Payload: payload, Status: "proposed",
		CreatedAt: "2026-10-29T00:00:00Z", CreatedByKind: byKind, CreatedByID: byID}
	f.rows = append([]ProposalRow{row}, f.rows...) // newest first, matching ListProposals' own contract
	return row, nil
}

func (f *fakeProposalStore) ListProposals(status string) ([]ProposalRow, error) {
	if status == "" {
		return f.rows, nil
	}
	var out []ProposalRow
	for _, r := range f.rows {
		if r.Status == status {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeProposalStore) DiscardProposal(_ context.Context, id int64, _, _ string) error {
	f.discardID = id
	return f.discardErr
}

func proposalTable(store *fakeProposalStore, validate func(ctx context.Context, kind, payload string) error) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Proposals = store
	tbl.ValidateProposal = validate
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	return tbl
}

func callProposal(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	meth, ok := tbl.Lookup(m)
	if !ok {
		t.Fatalf("%s is not registered", m)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return meth.Handle(context.Background(), nil, raw)
}

func TestProposalCreateValidatesBeforeWriting(t *testing.T) {
	store := &fakeProposalStore{}
	refusal := errors.New("mcp: server \"github\" is configured twice (public and public) — the same server cannot be two tiers or two agents' (MC12)")
	tbl := proposalTable(store, func(context.Context, string, string) error { return refusal })
	_, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "mcp_server_add", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("a payload T2's registry refuses must never produce a row")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrProposalRefused.Code {
		t.Fatalf("want ErrProposalRefused, got %v", err)
	}
	if e.Data.What != refusal.Error() {
		t.Fatalf("the refusal reason must be the validator's, verbatim: got %q, want %q", e.Data.What, refusal.Error())
	}
	if store.created != 0 {
		t.Fatalf("a refused payload reached the store (%d creates)", store.created)
	}
}

func TestProposalCreateReturnsCommitCommand(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := proposalTable(store, func(context.Context, string, string) error { return nil })
	res, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: "policy_add", Payload: json.RawMessage(`{"id":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := res.(ProposalRow)
	if !ok {
		t.Fatalf("result is %T, want ProposalRow", res)
	}
	want := "aeon proposal commit 1"
	if row.Command != want {
		t.Fatalf("command = %q, want %q", row.Command, want)
	}
	if row.CreatedByKind != "user" || row.CreatedByID != "1000" {
		t.Fatalf("actor not stamped from ActorFor: %+v", row)
	}
}

func TestProposalListNewestFirst(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := proposalTable(store, func(context.Context, string, string) error { return nil })
	for _, kind := range []string{"policy_add", "policy_remove", "seed_corpus"} {
		if _, err := callProposal(t, tbl, MethodProposalCreate, proposalCreateParams{Kind: kind, Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := callProposal(t, tbl, MethodProposalList, proposalListParams{})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]any)["proposals"].([]ProposalRow)
	if len(out) != 3 {
		t.Fatalf("got %d rows, want 3", len(out))
	}
	if out[0].ID != 3 || out[1].ID != 2 || out[2].ID != 1 {
		t.Fatalf("not newest-first: ids %d,%d,%d", out[0].ID, out[1].ID, out[2].ID)
	}
	for _, r := range out {
		if r.Command == "" {
			t.Errorf("row %d carries no command", r.ID)
		}
	}
}

func TestProposalDiscardGuarded(t *testing.T) {
	store := &fakeProposalStore{discardErr: errors.New("already committed by 1000 at 2026-10-29T15:10:00Z")}
	tbl := proposalTable(store, nil)
	res, err := callProposal(t, tbl, MethodProposalDiscard, proposalDiscardParams{ID: 7})
	if err != nil {
		t.Fatalf("a guarded refusal must be a RESULT, not an RPC error: %v", err)
	}
	out := res.(map[string]any)
	if out["ok"] != false {
		t.Fatalf("result = %v, want ok:false", out)
	}
	if out["error"] != store.discardErr.Error() {
		t.Fatalf("error = %v, want the store's guarded error verbatim: %v", out["error"], store.discardErr)
	}
	if store.discardID != 7 {
		t.Fatalf("discard called with id %d, want 7", store.discardID)
	}
}

func TestProposalDiscardSuccess(t *testing.T) {
	store := &fakeProposalStore{}
	tbl := proposalTable(store, nil)
	res, err := callProposal(t, tbl, MethodProposalDiscard, proposalDiscardParams{ID: 9})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]any)
	if out["id"] != int64(9) || out["status"] != "discarded" {
		t.Fatalf("result = %v, want {id:9, status:discarded}", out)
	}
}

// TestNoProposalCommitMethodExists is criterion 7 / test map #8, standalone —
// T6 later generalizes this into the full whitelist, but brief §7's rule
// must hold on its own before that amendment exists.
func TestNoProposalCommitMethodExists(t *testing.T) {
	// S45 T5 supersession (PM1, operator's ruling of 2026-10-30): the
	// human door is no longer CLI-ONLY — the socket carries proposal.get/
	// commit/reject as the CONDITIONAL human doors, which is exactly the
	// registration this body forbids. Skipped at its first line, body
	// unedited (the original bytes are history); the living statement is
	// TestProposalCommitExistsOnlyAsAHumanDoor beside it. Pre-skip body
	// sha256: b1f552d6e2637e7708c143e1ae3da93c782c740d5dbc32d9a4b47238a9acc451
	// (recorded in docs/aeon/S45-policy-management/tasks/T5.md Notes).
	t.Skip("superseded by S45 T5/T6 (PM1): the doors exist on the socket now — see the sibling test beside this one")
	tbl := newTestTable(t)
	for _, name := range tbl.Methods() {
		if name == "proposal.commit" || name == "proposal.reject" {
			t.Fatalf("%q is registered — commit/reject are the human-only CLI door (T5); this name must never exist on the socket table", name)
		}
	}
}

// TestProposalCommitExistsOnlyAsAHumanDoor is the S45 T5 sibling of the
// skipped body above — PM1's amended ruling, stated as the original stated
// the old one: the decision doors are HUMAN doors, and what made the CLI
// the human door (an attributable operator, nothing an agent can reach) is
// now enforced as the CONDITION on the socket registration instead. The
// three properties this test pins are the whole of that condition:
//
//  1. the names exist IFF Table.ProposalDecider is wired — an unwired
//     table (no store behind the daemon, the narrow projections, every
//     agent-reachable surface) does not carry them at all: Dispatch
//     answers ErrUnknownMethod, the same refusal as a name that never
//     existed;
//  2. RequiresInitialize is true — no decision before the handshake;
//  3. Debug is false — they are product surface, not diagnosis.
//
// The actor discipline (peer-stamped, never a param) is not re-pinned
// here: TestProposalDecideActorFromTableNotParams owns it.
func TestProposalCommitExistsOnlyAsAHumanDoor(t *testing.T) {
	// (1) Absent without the service — measured, not asserted.
	plain := newTestTable(t)
	for _, name := range []MethodName{MethodProposalGet, MethodProposalCommit, MethodProposalReject} {
		if _, ok := plain.Lookup(name); ok {
			t.Fatalf("%q is registered without ProposalDecider — an unwired table must answer it as unknown (PM1)", name)
		}
		if _, err := plain.Dispatch(context.Background(), "peer-x", name, json.RawMessage(`{"id":1,"digest":"0","reason":"r"}`)); err == nil {
			t.Fatalf("%q dispatched without a service", name)
		} else if _, isUnknown := err.(ErrUnknownMethod); !isUnknown {
			t.Fatalf("%q without a service: %v, want ErrUnknownMethod", name, err)
		}
	}
	// (1–3) Present, initialised, non-debug with the service wired.
	wired := newTestTable(t)
	wired.ProposalDecider = &decideStub{}
	if !wired.RegisterProposalDecider() {
		t.Fatal("RegisterProposalDecider answered false with a wired service")
	}
	for _, name := range []MethodName{MethodProposalGet, MethodProposalCommit, MethodProposalReject} {
		m, ok := wired.Lookup(name)
		if !ok {
			t.Fatalf("%q missing after registration", name)
		}
		if !m.RequiresInitialize {
			t.Errorf("%q: RequiresInitialize false — a decision before the handshake is not the human door", name)
		}
		if m.Debug {
			t.Errorf("%q: Debug true — the decision doors are product surface (S19 must project them)", name)
		}
	}
	// And registration is idempotent-honest: nil service registers nothing.
	none := newTestTable(t)
	if none.RegisterProposalDecider() {
		t.Fatal("RegisterProposalDecider registered names with a nil service")
	}
}
