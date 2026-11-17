// The CLI commit door for the two restrictive autonomy kinds (S56 T8),
// tested in-process over a REAL temp store — the policy verbs' idiom
// (agent_test.go): XDG_STATE_HOME at a throwaway, openAgentStore for the
// test's handle, and the seams' own per-call openStoreDirectly() opening
// the same file, WAL serialising the two handles.
//
// What is under test is the commit body's identity discipline, not the
// shape validation (the daemon's records arms own that,
// internal/daemon/records_autonomy_test.go): behaviour 2 (the actor on the
// grant row and the change row is the COMMITTER — the person at the door —
// never the agent who proposed, and the veto's proposed_by keeps the
// author), behaviour 3 (a row that names a veto is refused at re-validation
// — a veto is removed by a person, not proposed away — the grant untouched,
// the row still proposed with the refusal recorded), and behaviour 4 (TD3:
// create door and commit door run the SAME validator —
// deskEffects.ValidateProposal — so a smuggled payload is refused with the
// same sentence at both, byte for byte).
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

// commitEnv is the whole world a committed autonomy decision stands on: a
// throwaway home, the store, a root and a child of it (a veto may sit on
// either).
type commitEnv struct {
	t    *testing.T
	st   *store.Store
	proj string
	comp string
}

func newCommitEnv(t *testing.T) commitEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	p, err := st.CreateComponent(store.NewComponent{Name: "org group", Slug: "org-group", Root: "/repo/org-group"})
	if err != nil || !p.OK() {
		t.Fatalf("group: %v %v", err, p.Reason)
	}
	c, err := st.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha", ParentID: p.Component.ID})
	if err != nil || !c.OK() {
		t.Fatalf("component: %v %v", err, c.Reason)
	}
	return commitEnv{t: t, st: st, proj: p.Component.ID, comp: c.Component.ID}
}

// seedGrant writes a grant (allow) or veto (deny) directly through the
// store's own door — the same door a person at the socket would — and
// fails the test if the store refuses the fixture.
func (e commitEnv) seedGrant(nodeID, subjectKind, subject, state, byID string) store.AutonomyGrant {
	e.t.Helper()
	g, ref, err := e.st.CreateAutonomyGrant(store.Actor{Kind: "user", ID: byID}, store.NewAutonomyGrant{
		NodeID: nodeID, SubjectKind: subjectKind, Subject: subject, State: state,
	})
	if err != nil {
		e.t.Fatalf("seed %s grant: %v", state, err)
	}
	if ref != nil {
		e.t.Fatalf("seed %s grant refused: %s", state, ref.Sentence)
	}
	return g
}

// propose writes the inert row the way an agent's tool does — authored by
// the agent, kind and payload exactly as proposed — and returns it.
func (e commitEnv) propose(kind, payload string) store.Proposal {
	e.t.Helper()
	row, err := e.st.CreateProposal(kind, payload, store.Actor{Kind: "agent", ID: "implementer"},
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		e.t.Fatalf("create %s proposal: %v", kind, err)
	}
	return row
}

func (e commitEnv) commit(id int64, payload string, byID string) (desk.CommitResult, error) {
	e.t.Helper()
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

// TestCommitAppliesWithCommitterActor is T8 behaviour 2, both kinds: the
// committer (the person who decided) is the actor on the store write, and
// the proposal's author rides on proposed_by — the two identities are
// different people, and conflating them is the failure the audit exists to
// catch.
func TestCommitAppliesWithCommitterActor(t *testing.T) {
	e := newCommitEnv(t)

	// autonomy_revoke: a live allow grant, an agent's proposal, a person's commit.
	g := e.seedGrant(e.proj, "action", "forge.merge", "allow", "peer-9000")
	payload := `{"id":"` + g.ID + `","reason":"the merge window is closed"}`
	row := e.propose(string(desk.KindAutonomyRevoke), payload)

	res, err := e.commit(row.ID, payload, "peer-1000")
	if err != nil {
		t.Fatalf("commit the revoke: %v", err)
	}
	if !res.EffectApplied {
		t.Fatalf("the revoke effect did not apply: %+v", res)
	}

	// The revoked_* trio is the COMMITTER, not the agent who filed it.
	after, err := e.st.ReadAutonomyGrant(g.ID)
	if err != nil {
		t.Fatalf("re-read grant: %v", err)
	}
	if after.RevokedAt == nil || after.RevokedByKind == nil || *after.RevokedByKind != "user" ||
		after.RevokedByID == nil || *after.RevokedByID != "peer-1000" {
		t.Fatalf("revoked trio is not the committer: %+v", after)
	}
	if after.RevokedReason == nil || *after.RevokedReason != "the merge window is closed" {
		t.Fatalf("revoked reason %v", after.RevokedReason)
	}

	// Exactly one revoke change row, the committer's actor, and an EMPTY
	// proposed_by — a revoke stamps its reason on the grant row, and the
	// audit row's proposed_by is for the identity a veto carries.
	changes, err := e.st.AutonomyChanges(store.AutonomyChangeFilter{GrantID: g.ID})
	if err != nil {
		t.Fatalf("read changes: %v", err)
	}
	revokes := 0
	for _, ch := range changes {
		if ch.ChangeKind != "revoke" {
			continue
		}
		revokes++
		if ch.ActorKind != "user" || ch.ActorID != "peer-1000" {
			t.Fatalf("revoke change actor %s/%s, want the committer user/peer-1000: %+v", ch.ActorKind, ch.ActorID, ch)
		}
		if ch.ProposedBy != "" {
			t.Fatalf("revoke change proposed_by %q, want empty (the reason rides on the grant row): %+v", ch.ProposedBy, ch)
		}
	}
	if revokes != 1 {
		t.Fatalf("want exactly one revoke change row, got %d: %+v", revokes, changes)
	}

	// The proposal row is committed BY the person; the author stays in
	// created_by_* (the row's own stamp, which the Decider read).
	committed, ok, err := e.st.GetProposal(row.ID)
	if err != nil || !ok {
		t.Fatalf("re-read proposal: %v %v", ok, err)
	}
	if committed.Status != "committed" || committed.DecidedByKind != "user" || committed.DecidedByID != "peer-1000" {
		t.Fatalf("proposal row %+v, want committed by user/peer-1000", committed)
	}
	if committed.CreatedByKind != "agent" || committed.CreatedByID != "implementer" {
		t.Fatalf("author stamp moved: %+v", committed)
	}

	// autonomy_deny: the same commit body stamps a veto on a CHILD with the
	// committer as the row's author and the proposal's author as proposed_by.
	denyPayload := `{"nodeId":"` + e.comp + `","subject":{"kind":"action","name":"forge.merge"},"reason":"alpha is not allowed to merge yet"}`
	denyRow := e.propose(string(desk.KindAutonomyDeny), denyPayload)
	if _, err := e.commit(denyRow.ID, denyPayload, "peer-2000"); err != nil {
		t.Fatalf("commit the veto: %v", err)
	}

	vetos, err := e.st.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: e.proj, State: "deny"})
	if err != nil {
		t.Fatalf("list vetoes: %v", err)
	}
	if len(vetos) != 1 {
		t.Fatalf("want the one veto, got %d: %+v", len(vetos), vetos)
	}
	v := vetos[0]
	if v.NodeID != e.comp {
		t.Fatalf("veto node %q, want the child %q", v.NodeID, e.comp)
	}
	if v.CreatedByKind != "user" || v.CreatedByID != "peer-2000" {
		t.Fatalf("veto author is not the committer: %+v", v)
	}
	if v.ProposedBy != "agent:implementer" {
		t.Fatalf("veto proposed_by %q, want the proposal's author agent:implementer", v.ProposedBy)
	}
	if v.Note != "" {
		t.Fatalf("veto note %q, want empty (a veto carries no note, SEC-033)", v.Note)
	}
	if v.ExpiresAt != nil {
		t.Fatalf("veto expiry %v, want none (a veto with an expiry is not a veto)", v.ExpiresAt)
	}

	vetoChanges, err := e.st.AutonomyChanges(store.AutonomyChangeFilter{GrantID: v.ID})
	if err != nil {
		t.Fatalf("read veto changes: %v", err)
	}
	if len(vetoChanges) != 1 || vetoChanges[0].ChangeKind != "deny" ||
		vetoChanges[0].ActorKind != "user" || vetoChanges[0].ActorID != "peer-2000" ||
		vetoChanges[0].ProposedBy != "agent:implementer" {
		t.Fatalf("veto change rows %+v, want one 'deny' row by the committer with the author proposed_by", vetoChanges)
	}
}

// TestAgentCannotProposeToRemoveAVeto is T8 behaviour 3: an agent may not
// get a veto removed by proposing it away — a veto is a person's standing
// authority, and removing it would widen. The create door refuses it (the
// daemon's records arms pin the bytes); this is the commit door's backstop:
// a row that names a veto is refused at re-validation with the same
// sentence, the veto stays standing, and the row stays proposed with the
// refusal recorded for a human to reconsider.
func TestAgentCannotProposeToRemoveAVeto(t *testing.T) {
	e := newCommitEnv(t)
	veto := e.seedGrant(e.proj, "action", "forge.merge", "deny", "peer-9000")

	payload := `{"id":"` + veto.ID + `","reason":"the freeze is over"}`
	row := e.propose(string(desk.KindAutonomyRevoke), payload)

	_, err := e.commit(row.ID, payload, "peer-1000")
	if err == nil {
		t.Fatalf("committing a revoke of a veto succeeded — a veto is removed by a person, not proposed away")
	}
	if !strings.Contains(err.Error(), "a veto is removed by a person, not proposed away") {
		t.Fatalf("refusal %q, want the veto sentence", err)
	}

	// The veto is untouched and still standing.
	after, err := e.st.ReadAutonomyGrant(veto.ID)
	if err != nil {
		t.Fatalf("re-read veto: %v", err)
	}
	if after.RevokedAt != nil {
		t.Fatalf("the veto was revoked: %+v", after)
	}

	// The row stays proposed, with the refusal recorded for a human.
	kept, ok, err := e.st.GetProposal(row.ID)
	if err != nil || !ok {
		t.Fatalf("re-read row: %v %v", ok, err)
	}
	if kept.Status != "proposed" {
		t.Fatalf("row status %q after a refused commit, want proposed (retryable, not lost)", kept.Status)
	}
	if !strings.Contains(kept.AppliedError, "a veto is removed by a person, not proposed away") {
		t.Fatalf("applied_error %q, want the veto sentence recorded for the human", kept.AppliedError)
	}
}

// TestProposeValidatorParityWithCreate is T8 behaviour 4 (TD3): the create
// door and the commit door run the SAME validator — deskEffects.
// ValidateProposal is the exact function both doors reach through the same
// registry — so the bytes cannot drift. The battery checks each shape's
// answer through it; the commit half then commits a representative bad row
// and asserts the commit refusal carries the validator's sentence
// VERBATIM — the create door wraps err.Error() the same way, which is why
// the parity is structural rather than re-pinned per sentence.
func TestProposeValidatorParityWithCreate(t *testing.T) {
	e := newCommitEnv(t)
	g := e.seedGrant(e.proj, "action", "forge.merge", "allow", "peer-9000")

	cases := []struct {
		kind    string
		payload string
		phrase  string // want in the refusal; empty means the payload passes
	}{
		{string(desk.KindAutonomyRevoke), `{"id":"` + g.ID + `","reason":"done"}`, ""},
		{string(desk.KindAutonomyRevoke), `{"id":"` + g.ID + `","reason":"done","state":"revoke"}`, `json: unknown field "state"`},
		{string(desk.KindAutonomyRevoke), `{"id":"","reason":"done"}`, "needs the grant's id"},
		{string(desk.KindAutonomyRevoke), `{"id":"` + g.ID + `"}`, "needs a reason"},
		{string(desk.KindAutonomyRevoke), `{"id":"ag-nope","reason":"done"}`, "is not an autonomy grant"},
		{string(desk.KindAutonomyDeny), `{"nodeId":"` + e.comp + `","subject":{"kind":"action","name":"forge.merge"},"reason":"freeze"}`, ""},
		{string(desk.KindAutonomyDeny), `{"nodeId":"` + e.comp + `","subject":{"kind":"action","name":"forge.merge"},"reason":"freeze","conditions":{"requireGates":true}}`, `json: unknown field "conditions"`},
		{string(desk.KindAutonomyDeny), `{"nodeId":"` + e.comp + `","subject":{"kind":"bogus","name":"forge.merge"},"reason":"freeze"}`, `is "action" or "class"`},
	}
	for _, tc := range cases {
		err := deskEffects.ValidateProposal(context.Background(), tc.kind, tc.payload)
		if tc.phrase == "" {
			if err != nil {
				t.Fatalf("ValidateProposal(%s, %s) = %v, want a pass", tc.kind, tc.payload, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.phrase) {
			t.Fatalf("ValidateProposal(%s, %s) = %v, want a refusal containing %q", tc.kind, tc.payload, err, tc.phrase)
		}
	}

	// The commit door runs the same function: a bad row that reached the
	// store (a create door that predated the seam, or a bypass) is refused at
	// re-validation with the validator's sentence verbatim.
	bad := `{"id":"` + g.ID + `","reason":"done","state":"revoke"}`
	wantErr := deskEffects.ValidateProposal(context.Background(), string(desk.KindAutonomyRevoke), bad)
	if wantErr == nil {
		t.Fatalf("precondition: the smuggled payload must fail the shared validator")
	}
	row := e.propose(string(desk.KindAutonomyRevoke), bad)
	_, err := e.commit(row.ID, bad, "peer-1000")
	if err == nil {
		t.Fatalf("committing the smuggled row succeeded")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("commit refusal %q, want the validator's sentence verbatim %q", err, wantErr)
	}

	// Nothing was applied: the grant is still standing.
	if live, err := e.st.ReadAutonomyGrant(g.ID); err != nil || live.RevokedAt != nil {
		t.Fatalf("the smuggled row applied something: %v %+v", err, live)
	}
}
