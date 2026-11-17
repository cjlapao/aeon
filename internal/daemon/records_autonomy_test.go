package daemon

// records_autonomy_test.go — S56 T8 (AL11, AL12, 2026-11-01): the two
// propose arms walked at the door an agent actually stands at, and the
// inertness promise pinned byte for byte: a proposal is a ROW, not an
// authority.
//
// The behaviour rows the spec names, each pinned here:
//
//   - INERT (behaviour 1): the proposes are driven THROUGH A TOOL CALL and
//     the whole read surface (autonomy_list / autonomy_effective) plus the
//     grant rows are byte-identical across them; the only new row is a
//     proposal row, stamped from the step's agent, carrying the fixed inert
//     note and the copyable commit command;
//   - create-door refusals (behaviour 4): a smuggled state / conditions /
//     actor field, a missing id, a missing reason, an unknown grant id and a
//     denied subject kind are all parse- or validator-refusals with the
//     desk's own sentence — and a revoke naming a DENY row is refused with
//     the spec's sentence (behaviour 3's create half: an agent cannot
//     propose to remove a veto, because that would widen);
//   - the cap (behaviour 5): the 21st open proposal by one caller is
//     refused naming S45's constant — a counted refusal, never silence;
//   - and unwired is STATED, not faked (V13): a mount without the autonomy
//     seam names the absence rather than answering zero, and a refused
//     proposal never leaves a row (TD3).
//
// The seams this env builds are the TEST mirror of what cmd/aeon's
// deskSeams supplies for the two kinds (the daemon package may not import
// main): the desk half first, then the store half over the same real store.
// cmd/aeon's TestProposeValidatorParityWithCreate pins the PRODUCTION seam's
// bytes against proposal.create; this mirror exists so the daemon's create
// door can be walked without that import.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// autonomyProposeEnv is recordsEnv with the two handles the five arms read
// and write through — the SAME NewAutonomyService adapter the socket's
// autonomy.* methods run (one implementation, two doors), and the proposal
// doors both proposes end in (S45's create path, the shared validator).
type autonomyProposeEnv struct {
	recordsEnv
	reg *desk.Registry
}

func newAutonomyProposeEnv(t *testing.T) autonomyProposeEnv {
	t.Helper()
	e := autonomyProposeEnv{recordsEnv: newRecordsEnv(t, "")}
	e.svc.autonomyService = NewAutonomyService(e.s)
	e.reg = desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, testAutonomySeams(e.s))
	e.svc.proposals = proposalStore(e.s)
	e.svc.validateProposal = e.reg.ValidateProposal
	return e
}

// testAutonomySeams is the test mirror of cmd/aeon's production seams for
// the two kinds: Validate replaces the desk's built-in shape half with the
// full desk-then-store half (the merge rule in desk.New), AlreadyApplied and
// Apply are the commit door's job and are not exercised at the create door
// (ValidateProposal runs Validate only), so a nil half here is deliberate —
// the registry names the missing half rather than guessing it.
func testAutonomySeams(s *store.Store) desk.Seams {
	return desk.Seams{
		desk.KindAutonomyRevoke: {Validate: testAutonomyRevokeValidate(s)},
		desk.KindAutonomyDeny:   {Validate: testAutonomyDenyValidate(s)},
	}
}

func testAutonomyRevokeValidate(s *store.Store) func(context.Context, []byte) error {
	return func(_ context.Context, payload []byte) error {
		if err := desk.ValidateAutonomyRevoke(context.Background(), payload); err != nil {
			return err
		}
		var p desk.AutonomyRevokePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		g, err := s.ReadAutonomyGrant(p.ID)
		if err != nil {
			return fmt.Errorf("refused: %s is not an autonomy grant — it cannot be revoked", p.ID)
		}
		if g.State == "deny" {
			return fmt.Errorf("refused: %s is a veto — a veto is removed by a person, not proposed away", p.ID)
		}
		return nil
	}
}

func testAutonomyDenyValidate(s *store.Store) func(context.Context, []byte) error {
	return func(_ context.Context, payload []byte) error {
		if err := desk.ValidateAutonomyDeny(context.Background(), payload); err != nil {
			return err
		}
		var p desk.AutonomyDenyPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		c, err := s.ReadComponent(p.NodeID)
		if err != nil {
			return fmt.Errorf("refused: %s is not a component — a veto needs a node it can sit on", p.NodeID)
		}
		if c.ArchivedAt != "" {
			return fmt.Errorf("refused: %s is archived — its grants cannot be written", p.NodeID)
		}
		// The core's own deny validator (AL4 parity): for a veto it checks
		// that one carries no conditions and no expiry — the payload's
		// shape already forbids both, so this is the drift lock, not the
		// door (the desk half pins the subject's shape).
		if ref := autonomy.ValidateGrant(autonomy.Grant{
			NodeID:  p.NodeID,
			Subject: autonomy.Subject{Kind: p.Subject.Kind, Name: p.Subject.Name},
			State:   autonomy.StateDeny,
		}, autonomy.NodeInfo{IsRoot: c.ParentID == "", Archived: c.ArchivedAt != ""}, time.Now()); ref != nil {
			return fmt.Errorf("%s", ref.Sentence)
		}
		return nil
	}
}

// TestProposeIsInert is behaviour 1: after a propose, autonomy.effective and
// every grant row are byte-identical; only a proposal row exists. Both kinds
// are walked, each driven through the tool call an agent would make.
func TestProposeIsInert(t *testing.T) {
	e := newAutonomyProposeEnv(t)
	grant := seedAutonomyGrant(t, e.s, e.proj, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	veto := seedAutonomyGrant(t, e.s, e.proj, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.merge", State: "deny",
	})

	// The whole read surface the slice promises not to move: both tools and
	// both grant rows. EffectiveView.EvaluatedAt is a clock stamp at second
	// resolution — the only time-varying field in the view — so it is zeroed
	// on both sides and every other byte is compared as written.
	readSurface := func() []byte {
		t.Helper()
		listOut, f, err := e.do(t, "autonomy_list", `{"rootId":"`+e.proj+`","includeInactive":true}`)
		if err != nil || f != nil {
			t.Fatalf("autonomy_list: %v %+v", err, f)
		}
		effOut, f, err := e.do(t, "autonomy_effective", `{"rootId":"`+e.proj+`"}`)
		if err != nil || f != nil {
			t.Fatalf("autonomy_effective: %v %+v", err, f)
		}
		view, ok := effOut.(protocol.EffectiveView)
		if !ok {
			t.Fatalf("autonomy_effective answered %T", effOut)
		}
		view.EvaluatedAt = ""
		grantRow, err := e.s.ReadAutonomyGrant(grant.ID)
		if err != nil {
			t.Fatalf("read grant: %v", err)
		}
		vetoRow, err := e.s.ReadAutonomyGrant(veto.ID)
		if err != nil {
			t.Fatalf("read veto: %v", err)
		}
		out, err := json.Marshal(struct {
			List  protocol.AutonomyListResult `json:"list"`
			Eff   protocol.EffectiveView      `json:"effective"`
			Grant store.AutonomyGrant         `json:"grant"`
			Veto  store.AutonomyGrant         `json:"veto"`
		}{listOut.(protocol.AutonomyListResult), view, grantRow, vetoRow})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	before := readSurface()

	// — the revoke, through the tool —
	res, f, err := e.do(t, "autonomy_propose_revoke",
		`{"id":"`+grant.ID+`","reason":"the grant outlived its purpose"}`)
	if err != nil || f != nil {
		t.Fatalf("autonomy_propose_revoke: %v %+v", err, f)
	}
	pr := res.(protocol.PolicyProposeResult)
	if pr.Note != protocol.PolicyProposeInertNote {
		t.Errorf("the inert note is not the fixed sentence: %q", pr.Note)
	}
	if pr.Proposal.Kind != string(desk.KindAutonomyRevoke) ||
		pr.Proposal.CreatedByKind != "agent" || pr.Proposal.CreatedByID != e.agent ||
		pr.Proposal.Status != "proposed" {
		t.Errorf("the row is not stamped from the step as an inert proposed row: %+v", pr.Proposal)
	}
	if want := protocol.ProposalCommand(pr.Proposal.ID); pr.Proposal.Command != want {
		t.Errorf("the row carries %q, want the commit command %q", pr.Proposal.Command, want)
	}

	after := readSurface()
	if !bytes.Equal(before, after) {
		t.Errorf("a revoke propose moved the read surface:\nbefore %s\nafter  %s", before, after)
	}
	rows, err := e.svc.proposals.ListProposals("proposed")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != pr.Proposal.ID {
		t.Fatalf("the inert propose left %d proposal rows, want exactly %d", len(rows), pr.Proposal.ID)
	}

	// — the deny, through the tool, on the child —
	before = readSurface()
	res2, f, err := e.do(t, "autonomy_propose_deny",
		`{"nodeId":"`+e.comp.ID+`","subject":{"kind":"action","name":"forge.merge"},"reason":"the alpha lane must not merge unattended"}`)
	if err != nil || f != nil {
		t.Fatalf("autonomy_propose_deny: %v %+v", err, f)
	}
	pr2 := res2.(protocol.PolicyProposeResult)
	if pr2.Note != protocol.PolicyProposeInertNote || pr2.Proposal.Kind != string(desk.KindAutonomyDeny) {
		t.Errorf("the deny row is not the inert autonomy_deny row: %+v", pr2.Proposal)
	}
	if !bytes.Equal(before, readSurface()) {
		t.Errorf("a deny propose moved the read surface — a veto was created by a tool, not by a person")
	}
	rows, err = e.svc.proposals.ListProposals("proposed")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("after the deny propose there are %d proposal rows, want 2", len(rows))
	}
	// The stored payload is the desk's own shape (create/commit read one
	// shape, TD3) — not the tool input re-marshalled by accident.
	var revokeRow protocol.ProposalRow
	for _, r := range rows {
		if r.ID == pr.Proposal.ID {
			revokeRow = r
		}
	}
	if !bytes.Equal(revokeRow.Payload, []byte(`{"id":"`+grant.ID+`","reason":"the grant outlived its purpose"}`)) {
		t.Errorf("the stored revoke payload is not the input's bytes: %s", revokeRow.Payload)
	}
}

// TestProposeArmsRefuseSmugglesAndBadShapes is behaviour 4 at the create
// door (plus behaviour 3's create half): every smuggle and bad shape is
// refused with the desk's or the arm's own sentence, and a refused proposal
// never leaves a row (TD3).
func TestProposeArmsRefuseSmugglesAndBadShapes(t *testing.T) {
	e := newAutonomyProposeEnv(t)
	grant := seedAutonomyGrant(t, e.s, e.proj, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	veto := seedAutonomyGrant(t, e.s, e.proj, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.merge", State: "deny",
	})
	cases := []struct {
		tool, in, want string
	}{
		// The arm's strict decode: a field the input shape does not carry is
		// a parse refusal at the tool door (the S45 pattern — a smuggle
		// dies here, earlier than the validator). The desk's own sentence
		// for the same smuggle, and the parity with proposal.create that
		// the validator bytes carry, is pinned in cmd/aeon.
		{"autonomy_propose_revoke", `{"id":"` + grant.ID + `","reason":"ok","state":"allow"}`, `json: unknown field "state"`},
		{"autonomy_propose_revoke", `{"id":"` + grant.ID + `","reason":"ok","conditions":{"requireGates":true}}`, `json: unknown field "conditions"`},
		{"autonomy_propose_revoke", `{"id":"` + grant.ID + `","reason":"ok","actor":{"kind":"user","id":"u1"}}`, `json: unknown field "actor"`},
		{"autonomy_propose_deny", `{"nodeId":"` + e.proj + `","subject":{"kind":"action","name":"forge.merge"},"reason":"ok","state":"allow"}`, `json: unknown field "state"`},
		// The arm's own refusals (the shape decoded but is unusable).
		{"autonomy_propose_revoke", `{"reason":"no id"}`, `autonomy_propose_revoke needs the grant's id`},
		{"autonomy_propose_deny", `{"subject":{"kind":"action","name":"forge.merge"},"reason":"ok"}`, `autonomy_propose_deny needs the node's id`},
		// The desk's reason rule (one definition, both doors).
		{"autonomy_propose_revoke", `{"id":"` + grant.ID + `"}`, `autonomy_propose_revoke needs a reason`},
		{"autonomy_propose_deny", `{"nodeId":"` + e.proj + `","subject":{"kind":"action","name":"forge.merge"}}`, `autonomy_propose_deny needs a reason`},
		// The store half: the thing named must exist, and must not be a veto.
		{"autonomy_propose_revoke", `{"id":"no-such-grant","reason":"why"}`, `no-such-grant is not an autonomy grant`},
		{"autonomy_propose_revoke", `{"id":"` + veto.ID + `","reason":"remove the veto"}`, `a veto is removed by a person, not proposed away`},
		{"autonomy_propose_deny", `{"nodeId":"no-such-node","subject":{"kind":"action","name":"forge.merge"},"reason":"ok"}`, `no-such-node is not a component`},
		// The subject vocabulary (the desk's pure half).
		{"autonomy_propose_deny", `{"nodeId":"` + e.proj + `","subject":{"kind":"grant","name":"forge.merge"},"reason":"ok"}`, `a subject's kind is "action" or "class"`},
		{"autonomy_propose_deny", `{"nodeId":"` + e.proj + `","subject":{"kind":"class","name":"irreversible"},"reason":"ok"}`, `the one class is named "outward"`},
	}
	for _, tc := range cases {
		_, f, err := e.do(t, tc.tool, tc.in)
		if err != nil || f == nil {
			t.Fatalf("%s %s: %v %+v — want a refusal, not a write", tc.tool, tc.in, err, f)
		}
		if !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s %s:\n got  %q\n want a sentence containing %q", tc.tool, tc.in, f.Sentence, tc.want)
		}
	}
	// Refused means NO row (TD3): none of the refusals above wrote anything.
	rows, err := e.svc.proposals.ListProposals("proposed")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("refused proposals left %d rows — a refusal must never write: %+v", len(rows), rows)
	}
}

// TestProposeLimitPerAgent is behaviour 5: a caller may hold at most
// desk.MaxOpenProposalsPerAgent OPEN proposals — counted across kinds — and
// the refusal names the cap. The 21st is refused; it is not deleted silently.
func TestProposeLimitPerAgent(t *testing.T) {
	e := newAutonomyProposeEnv(t)
	grant := seedAutonomyGrant(t, e.s, e.proj, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	// Fill to the cap: 15 revoke + 5 deny — the cap is per caller, not per
	// kind, so the mix is the honest fill.
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		tool, in := "autonomy_propose_revoke",
			`{"id":"`+grant.ID+`","reason":"revoke filler `+strconv.Itoa(i)+`"}`
		if i%3 == 0 {
			tool, in = "autonomy_propose_deny",
				`{"nodeId":"`+e.proj+`","subject":{"kind":"action","name":"forge.merge"},"reason":"deny filler `+strconv.Itoa(i)+`"}`
		}
		if _, f, err := e.do(t, tool, in); err != nil || f != nil {
			t.Fatalf("filler %d (%s): %v %+v", i, tool, err, f)
		}
	}
	_, f, err := e.do(t, "autonomy_propose_revoke",
		`{"id":"`+grant.ID+`","reason":"one too many"}`)
	if err != nil || f == nil {
		t.Fatalf("the 21st open proposal was accepted: %v", err)
	}
	for _, want := range []string{"already holds", strconv.Itoa(desk.MaxOpenProposalsPerAgent), "per-agent cap"} {
		if !strings.Contains(f.Sentence, want) {
			t.Errorf("the cap refusal does not name %q: %s", want, f.Sentence)
		}
	}
}

// TestAutonomyArmsStateTheUnwiredSeam is V13 at the autonomy family: a mount
// without the autonomy seam (or the proposal doors) states the absence as a
// refusal naming the tool — never answers zero, never writes unvalidated.
func TestAutonomyArmsStateTheUnwiredSeam(t *testing.T) {
	e := newRecordsEnv(t, "") // nothing wired: no autonomyService, no proposals, no validator
	for _, tc := range []struct {
		tool, in, want string
	}{
		{"autonomy_list", `{"rootId":"` + e.proj + `"}`, "needs the autonomy surface"},
		{"autonomy_show", `{"id":"no-such"}`, "needs the autonomy surface"},
		{"autonomy_effective", `{"rootId":"` + e.proj + `"}`, "needs the autonomy surface"},
		{"autonomy_propose_revoke", `{"id":"g","reason":"r"}`, "the propose tools needs the proposal store"},
		{"autonomy_propose_deny", `{"nodeId":"` + e.proj + `","subject":{"kind":"action","name":"forge.merge"},"reason":"r"}`, "the propose tools needs the proposal store"},
	} {
		out, f, err := e.do(t, tc.tool, tc.in)
		if err != nil || f == nil {
			t.Fatalf("%s: %v %+v — want a stated refusal", tc.tool, err, f)
		}
		if out != nil {
			t.Errorf("%s answered %v while unwired — absent is stated, never faked (V13)", tc.tool, out)
		}
		if !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s:\n got  %q\n want a sentence containing %q", tc.tool, f.Sentence, tc.want)
		}
	}
}
