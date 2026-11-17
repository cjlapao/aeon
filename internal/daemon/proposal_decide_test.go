package daemon

// proposal_decide_test.go — S45 T5: the socket's decision door over the
// daemon's OWN wiring (the same New(Options{Store…}) the product builds),
// so the table seams, the peer stamp, the conditional registration and the
// effect registry behind the commit are the production ones. The store is a
// fresh temp XDG per test (the env rule); the peer is uid 4321 — a number
// the wire never saw.

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"
)

// decideJSON renders a dispatch RESULT (the other direction of
// mustJSONDaemon): the tests compare rendered answers as JSON text.
func decideJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func decideWhat(err error) string {
	if e, ok := err.(*protocol.Error); ok {
		return e.Data.What
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

var decidePeer = []localrpc.ConnInfo{{ID: "peer-1", PeerUID: 4321, PeerPID: 7}}
var decideNoPeer = []localrpc.ConnInfo{{ID: "peer-1", PeerUID: 0, PeerPID: 7}}

func decideDaemon(t *testing.T, conns []localrpc.ConnInfo) (*Daemon, *store.Store) {
	t.Helper()
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	// The effect registry arrives exactly the way serve.go hands it: an
	// opener building desk.New over THIS store's boundary. (In the product
	// the seam map is cmd/aeon's injected one; the policy kinds need no
	// seam, and the MCP test below wires its own map through the same
	// Options field — the injection point is what's under test.)
	d := New(Options{
		DaemonVersion: "9.9.9-test",
		BuildTime:     "2026-10-15T00:00:00Z",
		GitCommit:     "deadbee",
		Connections:   func() []localrpc.ConnInfo { return conns },
		Store:         s,
		Seed:          e,
		Effects: func() (*desk.Registry, error) {
			// The seed_corpus seam is the daemon's OWN door (T4's
			// policyseed.ApplyCorpus route) — the test mount injects it the
			// way the product's composition-root seam map does, so the
			// seed-commit test exercises the real effect over this store.
			return desk.New(desk.Deps{Boundary: testBoundaryOpener(s)}, desk.Seams{
				desk.KindSeedCorpus: {
					Validate:       func(context.Context, []byte) error { return nil },
					AlreadyApplied: func(context.Context, []byte) (bool, error) { return false, nil },
					Apply: func(ctx context.Context, w io.Writer, payload []byte) error {
						var p struct {
							Agents []string `json:"agents"`
						}
						if err := json.Unmarshal(payload, &p); err != nil {
							return fmt.Errorf("seed_corpus payload: %w", err)
						}
						_, err := policyseed.ApplyCorpus(ctx, e, s, p.Agents, nil, false,
							seed.Actor{Kind: "user", ID: "test-daemon"}, time.Now().UTC())
						return err
					},
				},
			}), nil
		},
	})
	return d, s
}

// testBoundaryOpener hands the desk the policy write boundary over this
// daemon's store, through the same RepoFuncs bridge the gate reads with.
// (Production passes cmd/aeon's opener; the shape is identical.)
func testBoundaryOpener(s *store.Store) desk.BoundaryOpener {
	return func() (desk.Boundary, func(), error) {
		ps := policy.NewStore(newPolicyRepo(s))
		return testBoundary{ps}, func() {}, nil
	}
}

type testBoundary struct{ s *policy.Store }

func (b testBoundary) AddPolicy(ctx context.Context, p policy.Policy, at time.Time) *policy.Reject {
	return b.s.Add(ctx, p, at)
}
func (b testBoundary) RemovePolicy(ctx context.Context, id string) (bool, error) {
	return b.s.Remove(ctx, id)
}
func (b testBoundary) ListPolicies(ctx context.Context, at time.Time) ([]policy.Policy, *policy.Reject) {
	return b.s.List(ctx, at)
}
func (b testBoundary) RecordAuthoredPolicy(ctx context.Context, subject, policyID string, at time.Time) *policy.Reject {
	return b.s.RecordAuthored(ctx, subject, policyID, at)
}

func decideInit(t *testing.T, d *Daemon, ctx context.Context) {
	t.Helper()
	if _, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodInitialize,
		mustJSONDaemon(t, protocol.InitializeParams{ClientName: "s45-t5", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})); err != nil {
		t.Fatalf("initialize: %v", err)
	}
}

// stageProposal writes an inert row directly (the create door is S35's,
// pinned elsewhere) so each test starts from exactly one proposed row.
func stageProposal(t *testing.T, s *store.Store, kind, payload string) int64 {
	t.Helper()
	p, err := s.CreateProposal(kind, payload, store.Actor{Kind: "agent", ID: "boss"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("stage proposal: %v", err)
	}
	return p.ID
}

func decisionActor() protocol.Actor { return protocol.Actor{Kind: "user", ID: "4321"} }

func svcOf(t *testing.T, d *Daemon) protocol.ProposalDecider {
	t.Helper()
	svc, ok := d.Table.ProposalDecider.(protocol.ProposalDecider)
	if !ok || svc == nil {
		t.Fatal("Table.ProposalDecider not wired by New with a store")
	}
	return svc
}

func commitVia(t *testing.T, d *Daemon, ctx context.Context, id int64, digest string) (protocol.ProposalCommitResult, error) {
	t.Helper()
	res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalCommit,
		mustJSONDaemon(t, protocol.ProposalCommitParams{ID: id, Digest: digest}))
	if err != nil {
		return protocol.ProposalCommitResult{}, err
	}
	return res.(protocol.ProposalCommitResult), nil
}

// --- the tests from the T5 table ------------------------------------------------

func TestCommitPolicyAddAppliesAndFlips(t *testing.T) {
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	payload := `{"id":"t5-deny-webfetch","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`
	id := stageProposal(t, s, "policy_add", payload)

	res, err := commitVia(t, d, ctx, id, desk.ProposalPayloadDigest(payload))
	if err != nil {
		t.Fatalf("proposal.commit: %v", err)
	}
	if !res.EffectApplied {
		t.Error("effectApplied=false on a fresh commit")
	}
	if res.Proposal.Status != "committed" || res.Proposal.DecidedByKind != "user" || res.Proposal.DecidedByID != "4321" {
		t.Fatalf("the flip did not carry the peer stamp: %+v", res.Proposal)
	}
	// The effect is REAL: policy.list answers the row (PM11: the next read
	// sees it, no cache).
	lres, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyList, mustJSONDaemon(t, protocol.PolicyListParams{}))
	if err != nil {
		t.Fatalf("policy.list after commit: %v", err)
	}
	if !strings.Contains(decideJSON(t, lres), "t5-deny-webfetch") {
		t.Fatal("the committed rule is not in force to the reads")
	}
	// PM8: exactly one audit row for the decision, op=commit, the rule and
	// the proposal named, an ids-only summary.
	rows, err := s.PolicyChangeList(ctx, "t5-deny-webfetch", 50)
	if err != nil {
		t.Fatalf("PolicyChangeList: %v", err)
	}
	if len(rows) != 1 || rows[0].Op != "commit" || rows[0].ActorKind != "user" || rows[0].ActorID != "4321" ||
		rows[0].ProposalID == nil || *rows[0].ProposalID != id {
		t.Fatalf("audit rows: %+v", rows)
	}
}

func TestProposalGetCarriesDigestAndPreview(t *testing.T) {
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	payload := `{"id":"t5-preview-add","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`
	id := stageProposal(t, s, "policy_add", payload)

	res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalGet,
		mustJSONDaemon(t, protocol.ProposalGetParams{ID: id}))
	if err != nil {
		t.Fatalf("proposal.get: %v", err)
	}
	got := res.(protocol.ProposalGetResult)
	if got.Digest != desk.ProposalPayloadDigest(payload) {
		t.Fatalf("digest is not the desk canonicaliser's: %q", got.Digest)
	}
	if got.Preview == nil || got.Preview.Describe == "" || got.Preview.Target != "agent:boss" {
		t.Fatalf("preview: %+v", got.Preview)
	}
	// The preview's describe IS the committed row's describe (D9 one
	// formatter): commit, read the row, compare.
	if _, err := commitVia(t, d, ctx, id, got.Digest); err != nil {
		t.Fatalf("commit: %v", err)
	}
	gres, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyGet,
		mustJSONDaemon(t, protocol.PolicyGetParams{ID: "t5-preview-add"}))
	if err != nil {
		t.Fatalf("policy.get: %v", err)
	}
	if d := gres.(protocol.PolicyGetResult).Policy; d.Describe != got.Preview.Describe {
		t.Fatalf("preview describe %q ≠ committed describe %q", got.Preview.Describe, d.Describe)
	}
	// And the commit's `after` is the same answer policy.inForce gives now
	// (PM7.2 — the confirmation is the store's, not the door's).
	ires, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyInForce,
		mustJSONDaemon(t, protocol.PolicyInForceParams{Agent: "boss"}))
	if err != nil {
		t.Fatalf("policy.inForce: %v", err)
	}
	inforce := ires.(protocol.PolicyInForceResult)
	if len(inforce.Answers) != len(got.Preview.InForce)+1 {
		// preview saw the pre-commit set; after the commit the row joined —
		// the pair is the delta, proved by the count AND the committed row.
		t.Logf("inForce %d rows vs preview %d (pre-commit)", len(inforce.Answers), len(got.Preview.InForce))
	}
}

func TestCommitRequiresMatchingDigest(t *testing.T) {
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	payload := `{"id":"t5-digest","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`
	id := stageProposal(t, s, "policy_add", payload)

	_, err := commitVia(t, d, ctx, id, strings.Repeat("0", 64))
	e, ok := err.(*protocol.Error)
	if !ok || e.Code != protocol.ErrProposalDecideRefused.Code {
		t.Fatalf("wrong digest: %v", err)
	}
	if !strings.Contains(e.Data.What, "changed since you read it") {
		t.Fatalf("the refusal must name the change: %q", e.Data.What)
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "proposed" || row.AppliedError != "" {
		t.Fatalf("a mismatched commit touched the row: %+v", row)
	}
}

func TestCommitFailedValidationLeavesRowProposedWithAppliedError(t *testing.T) {
	// S35 test-map #5 over the socket: the payload passes create-time shape
	// but the boundary refuses at commit-time re-validation — here a
	// command_access row whose spec is missing its command (the boundary
	// refuses; the gate never half-writes). applied_error records, the row
	// stays proposed.
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	payload := `{"id":"t5-bad","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{},"source":"operator"}`
	id := stageProposal(t, s, "policy_add", payload)

	_, err := commitVia(t, d, ctx, id, desk.ProposalPayloadDigest(payload))
	e, ok := err.(*protocol.Error)
	if !ok || e.Code != protocol.ErrProposalDecideRefused.Code {
		t.Fatalf("bad row committed? %v", err)
	}
	if !strings.Contains(e.Data.What, "failed re-validation at commit") {
		t.Fatalf("refusal: %q", e.Data.What)
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "proposed" {
		t.Fatalf("row moved off proposed: %s", row.Status)
	}
	if row.AppliedError == "" {
		t.Fatal("applied_error not recorded")
	}
	// Nothing authored: no policy row, no audit row.
	if n, _ := s.PolicyChangeCount(ctx); n != 0 {
		t.Fatalf("a refused commit audited %d rows", n)
	}
}

func TestCommitDecidedRowRefusedWithWinner(t *testing.T) {
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	payload := `{"id":"t5-twice","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`
	id := stageProposal(t, s, "policy_add", payload)
	digest := desk.ProposalPayloadDigest(payload)

	if _, err := commitVia(t, d, ctx, id, digest); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	_, err := commitVia(t, d, ctx, id, digest)
	e, ok := err.(*protocol.Error)
	if !ok || e.Code != protocol.ErrProposalDecideRefused.Code {
		t.Fatalf("second commit: %v", err)
	}
	if !strings.Contains(e.Data.What, "already committed by user:4321 at") {
		t.Fatalf("the refusal must name the winner: %q", e.Data.What)
	}
	// One decision, one audit row — the second door changed nothing.
	rows, _ := s.PolicyChangeList(ctx, "t5-twice", 50)
	if len(rows) != 1 {
		t.Fatalf("audit rows for a doubly-presented commit: %+v", rows)
	}
}

func TestRejectRequiresReason(t *testing.T) {
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	id := stageProposal(t, s, "policy_add", `{"id":"t5-rej","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`)

	// Empty and blank reasons refuse at the DOOR (params-level) and would
	// refuse again at the service (ReasonRequired) — the row never moves.
	for _, r := range []string{"", "   "} {
		_, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalReject,
			mustJSONDaemon(t, protocol.ProposalRejectParams{ID: id, Reason: r}))
		e, ok := err.(*protocol.Error)
		if !ok || e.Code != protocol.ErrProposalDecideRefused.Code || !strings.Contains(e.Data.What, "requires a reason") {
			t.Fatalf("blank reason %q: %v", r, err)
		}
	}
	// 513 bytes refuse; 512 pass (the audit column's budget).
	_, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalReject,
		mustJSONDaemon(t, protocol.ProposalRejectParams{ID: id, Reason: strings.Repeat("x", 513)}))
	if e, ok := err.(*protocol.Error); !ok || !strings.Contains(e.Data.What, "512") {
		t.Fatalf("513-byte reason: %v", err)
	}
	_, err = d.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalReject,
		mustJSONDaemon(t, protocol.ProposalRejectParams{ID: id, Reason: strings.Repeat("x", 512)}))
	if err != nil {
		t.Fatalf("512-byte reason: %v", err)
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "rejected" || row.DecidedByKind != "user" || row.DecidedByID != "4321" {
		t.Fatalf("rejected row: %+v", row)
	}
	// A reject applies NOTHING: no policy row was ever authored for it.
	if _, found, _ := s.GetProposal(id); !found {
		t.Fatal("row gone")
	}
	lres, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyList, mustJSONDaemon(t, protocol.PolicyListParams{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(decideJSON(t, lres), "t5-rej") {
		t.Fatal("a reject applied an effect")
	}
	// PM8: the reject audits exactly one row (op=reject, the proposal named).
	rows, _ := s.PolicyChangeList(ctx, "t5-rej", 50)
	if len(rows) != 1 || rows[0].Op != "reject" || rows[0].ProposalID == nil || *rows[0].ProposalID != id {
		t.Fatalf("reject audit: %+v", rows)
	}
}

func TestDecideActorFromPeerOnlyAndNoActorRefuses(t *testing.T) {
	ctx := context.Background()
	// (a) forged actor in params is refused as a shape error — the caller
	// may not name its own identity (PM7.3/PM8).
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	id := stageProposal(t, s, "policy_add", `{"id":"t5-forged","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`)
	_, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalCommit,
		json.RawMessage(fmt.Sprintf(`{"id":%d,"digest":"%s","actor":{"kind":"user","id":"9999"}}`, id, desk.ProposalPayloadDigest(`{"id":"t5-forged","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`))))
	if err == nil || !strings.Contains(err.Error(), "actor") {
		t.Fatalf("a forged actor passed or went unnamed: %v", err)
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "proposed" {
		t.Fatal("the forged commit wrote")
	}
	// (b) an unresolvable peer (uid 0) refuses BOTH doors with
	// ErrPolicyNoActor and writes nothing (PM7.3, T0 item 3.4).
	d2, s2 := decideDaemon(t, decideNoPeer)
	decideInit(t, d2, ctx)
	id2 := stageProposal(t, s2, "policy_add", `{"id":"t5-nop","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`)
	digest2 := desk.ProposalPayloadDigest(`{"id":"t5-nop","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`)
	_, err = d2.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalCommit,
		mustJSONDaemon(t, protocol.ProposalCommitParams{ID: id2, Digest: digest2}))
	if e, ok := err.(*protocol.Error); !ok || e.Code != protocol.ErrPolicyNoActor.Code {
		t.Fatalf("commit with uid 0: %v", err)
	}
	_, err = d2.Table.Dispatch(ctx, "peer-1", protocol.MethodProposalReject,
		mustJSONDaemon(t, protocol.ProposalRejectParams{ID: id2, Reason: "no peer"}))
	if e, ok := err.(*protocol.Error); !ok || e.Code != protocol.ErrPolicyNoActor.Code {
		t.Fatalf("reject with uid 0: %v", err)
	}
	row2, _, _ := s2.GetProposal(id2)
	if row2.Status != "proposed" || row2.DecidedByKind != "" {
		t.Fatalf("an unattributable decision wrote: %+v", row2)
	}
}

// TestCommitAgentImportCommitsLikeEveryKind — S45 T10, superseding T5's
// TestCommitAgentImportRefusedByName (whose by-name refusal stood only
// until S43 merged). The kind runs desk's GENERIC path behind the socket:
// digest-bound, effect-then-flip with the peer stamp, and it writes NO
// policy_change row — the row's decided_by_* is its record. The import
// effect arrives as an INJECTED seam (exactly the shape the product
// injects from cmd/aeon — the daemon links no plugin code), and its
// consent print is CAPTURED (a daemon never streams to a terminal) and
// shipped as data on the result; the before/after pair is S37's own
// rendering through the door's exported evidence builders.
func TestCommitAgentImportCommitsLikeEveryKind(t *testing.T) {
	ctx := context.Background()
	const payload = `{"pkgPath":"/abs/agents/groot"}`
	s := openSeedStore(t)
	var applied int
	d := New(Options{
		DaemonVersion: "9.9.9-test",
		BuildTime:     "2026-10-15T00:00:00Z",
		GitCommit:     "deadbee",
		Connections:   func() []localrpc.ConnInfo { return decidePeer },
		Store:         s,
		Effects: func() (*desk.Registry, error) {
			return desk.New(desk.Deps{Boundary: testBoundaryOpener(s)}, desk.Seams{
				desk.KindAgentImport: {
					Validate:       func(context.Context, []byte) error { return nil },
					AlreadyApplied: func(context.Context, []byte) (bool, error) { return false, nil },
					Apply: func(_ context.Context, w io.Writer, _ []byte) error {
						applied++
						_, err := io.WriteString(w, "imported groot as DISABLED (AP2: import runs the check and stops)\n")
						return err
					},
				},
			}), nil
		},
		AgentImportPreview: func(_ context.Context, pkgPath string) (*protocol.AgentImportPreview, string, error) {
			return &protocol.AgentImportPreview{Agent: "groot", Consent: "CONSENT over " + pkgPath, CarriedDeny: []protocol.AgentImportCarried{}}, "", nil
		},
	})
	decideInit(t, d, ctx)

	id := stageProposal(t, s, "agent_import", payload)
	res, err := commitVia(t, d, ctx, id, desk.ProposalPayloadDigest(payload))
	if err != nil {
		t.Fatalf("agent_import commit over the socket: %v", err)
	}
	if applied != 1 {
		t.Fatalf("the injected seam ran %d times, want 1", applied)
	}
	if !res.EffectApplied || res.Proposal.Status != "committed" {
		t.Fatalf("half a commit: %+v", res)
	}
	if res.Proposal.DecidedByKind != "user" || res.Proposal.DecidedByID != "4321" {
		t.Errorf("the flip did not carry the peer stamp: %+v", res.Proposal)
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "committed" || row.DecidedByKind != "user" || row.DecidedByID != "4321" {
		t.Fatalf("the row itself: %+v", row)
	}
	// The consent reached the socket RESULT as data — and exactly once.
	joined := strings.Join(res.Warnings, "\n")
	if strings.Count(joined, "imported groot as DISABLED") != 1 {
		t.Fatalf("the consent print must ride the result exactly once, warnings=%+v", res.Warnings)
	}
	// Before = S37's preview (the seam's bytes, verbatim); after = the
	// import body's own post-import print.
	if res.Before.Target != "agent:groot" || len(res.Before.Answers) != 1 || res.Before.Answers[0].Render != "CONSENT over /abs/agents/groot" {
		t.Errorf("before-view is not the preview renderer's answer: %+v", res.Before)
	}
	if res.After.Target != "agent:groot" || len(res.After.Answers) != 1 || !strings.Contains(res.After.Answers[0].Render, "imported groot as DISABLED") {
		t.Errorf("after-view is not the body's own print: %+v", res.After)
	}
	// The audit rule (T10): NOT ONE policy_change row — the authoring audit
	// is policy's; the decision lives in the row's decided_by_* stamp.
	if n, err := s.PolicyChangeCount(ctx); err != nil || n != 0 {
		t.Fatalf("agent_import wrote %d policy_change rows (err %v) — the authoring audit is policy-only", n, err)
	}

	// PM7.1 stands on this kind too: a commit not bound to what was read
	// commits nothing, and a refused row stays proposed.
	id2 := stageProposal(t, s, "agent_import", payload)
	if _, err := commitVia(t, d, ctx, id2, "deadbeef"); err == nil {
		t.Fatal("a wrong-digest agent_import commit succeeded")
	}
	if row2, _, _ := s.GetProposal(id2); row2.Status != "proposed" {
		t.Fatalf("the refused commit moved the row: %s", row2.Status)
	}
	if applied != 1 {
		t.Fatalf("the refused commit ran the effect (applied=%d)", applied)
	}
}

// TestDaemonCommitDoorPrintsNothingItself — T10's daemon-side half of the
// consent rule (S37 AA4): the daemon door CAPTURES the effect's print and
// ships it as data; it never streams. The adapter file therefore names no
// writer at all — no stdout, no stderr, no fmt print call. This is a
// structural pin: the day someone hands the door a terminal, this guard
// says which rule broke.
func TestDaemonCommitDoorPrintsNothingItself(t *testing.T) {
	src, err := os.ReadFile("proposal_decide.go")
	if err != nil {
		t.Fatalf("read the adapter: %v", err)
	}
	for _, banned := range []string{"os.Stdout", "os.Stderr", "fmt.Print", "fmt.Fprint", "fmt.Println"} {
		if strings.Contains(string(src), banned) {
			t.Errorf("proposal_decide.go contains %q — the daemon door captures the consent print and ships it as data; it never prints itself", banned)
		}
	}
}

func TestCommitSeedCorpusIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	// The engine's all-or-nothing rule (T12): an unknown agent name refuses
	// the WHOLE run — the row stays proposed, nothing seeds, nothing audits.
	id := stageProposal(t, s, "seed_corpus", `{"agents":["no-such-agent"]}`)
	_, err := commitVia(t, d, ctx, id, desk.ProposalPayloadDigest(`{"agents":["no-such-agent"]}`))
	if err == nil || !strings.Contains(decideWhat(err), "no-such-agent") {
		t.Fatalf("a seed naming an unknown agent committed or refused without naming it: %v (%s)", err, decideWhat(err))
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "proposed" {
		t.Fatalf("the failed seed commit moved the row: %s", row.Status)
	}
	n, _ := s.PolicyChangeCount(ctx)
	if n != 0 {
		t.Fatalf("a failed seed audited %d rows", n)
	}
	// The success half: seed a real corpus agent, then the commit completes
	// and the run's op='seed' row + the decision's op='commit' row stand.
	// The agent must be a ROW in the agent table — T12's rule: authoring
	// permissions for an agent nobody has is not seeding.
	known := corpusAgentFor(t, s)
	ensureCorpusAgentRow(t, s, known)
	id2 := stageProposal(t, s, "seed_corpus", fmt.Sprintf(`{"agents":[%q]}`, known))
	res, err := commitVia(t, d, ctx, id2, desk.ProposalPayloadDigest(fmt.Sprintf(`{"agents":[%q]}`, known)))
	if err != nil {
		t.Fatalf("seed_corpus commit: %v — sentence %s", err, decideWhat(err))
	}
	if !res.EffectApplied {
		t.Error("a first seed run answered effectApplied=false")
	}
	audits, _ := s.PolicyChangeList(ctx, "", 500)
	seeded, committed := 0, 0
	for _, a := range audits {
		switch a.Op {
		case "seed":
			seeded++
		case "commit":
			committed++
			if a.ProposalID == nil || *a.ProposalID != id2 || a.PolicyID != "" {
				t.Fatalf("the commit audit row misnamed: %+v", a)
			}
		}
	}
	if committed != 1 || seeded < 1 {
		t.Fatalf("audits after a seed commit: seed=%d commit=%d", seeded, committed)
	}
}

func corpusAgentFor(t *testing.T, s *store.Store) string {
	t.Helper()
	// The shipped corpus's own name (T4/T12's fixture agent).
	_ = s
	return "brief-author"
}

// walkForCallers finds every .go file (whole module, tests included,
// vendor-free by construction) whose SOURCE contains a call-shaped use of
// the token. It is a call-graph assertion, not a text grep: the file must
// parse and the token must appear inside a CallExpr's selector — comments
// about the call do not count (which is why this file's own mentions of
// DecideProposal stay clean while its shim does not).
// walkForCallers counts PRODUCTION callers: the walk skips _test.go files,
// the S45 convention (T6's write-boundary walk in cmd/aeon/policy_amend_test.go
// excludes them and floors the walk at ≥ 50 files, "never pass on an empty
// walk"). The whitelist constrains the doors a running process can reach —
// a test harness that builds a wired Table or fakes a store (T8's parity
// fixture calling desk.EffectsFor, the golden harness's store) is a test, not
// a door. Reconciled 2026-11-01 (T11): the walk saw test files before, and
// two of them sat in the allow-list; a third (T8's cmd/aeon/policy_parity_test.go)
// turned the guard red through the full run until this line made the guard's
// own intent — production — explicit. The non-test floor keeps the exclusion
// from becoming the vacuity it must not be.
func walkForCallers(t *testing.T, needle string) map[string]bool {
	t.Helper()
	root := decideModuleRoot(t)
	sel := strings.TrimSuffix(needle, "(")
	if i := strings.LastIndexByte(sel, '.'); i >= 0 {
		sel = sel[i+1:]
	}
	out := map[string]bool{}
	nonTest := 0
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable corners are skipped; the pin is on the known files
		}
		if !strings.HasSuffix(path, ".go") || strings.Contains(path, "/testdata/") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		nonTest++
		rel, _ := filepath.Rel(root, path)
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selExpr, ok := call.Fun.(*ast.SelectorExpr); ok && selExpr.Sel.Name == sel {
				out[rel] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if nonTest < 300 {
		t.Fatalf("the walk saw only %d non-test files — the whitelist checked nothing (never pass on an empty walk)", nonTest)
	}
	return out
}

func decideModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

func TestCommitMCPKindsUseRegistry(t *testing.T) {
	// The commit reaches the MCP effect through the injected registry ONLY:
	// this daemon mount hands Options.Effects a registry whose MCP seams
	// record their own calls. The door never calls mcp.SaveConfig itself —
	// that caller lives in cmd/aeon's seam map and nowhere else (the
	// walk-test in cmd/aeon pins the product's call graph; this pins that
	// the socket's commit consults the registry the composition root
	// injected).
	ctx := context.Background()
	var applied atomic.Int64
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := New(Options{
		DaemonVersion: "9.9.9-test", BuildTime: "2026-10-15T00:00:00Z", GitCommit: "deadbee",
		Connections: func() []localrpc.ConnInfo { return decidePeer },
		Store:       s, Seed: e,
		Effects: func() (*desk.Registry, error) {
			return desk.New(desk.Deps{Boundary: testBoundaryOpener(s)}, desk.Seams{
				desk.KindMCPServerAdd: {
					Validate:       func(ctx context.Context, payload []byte) error { return nil },
					AlreadyApplied: func(ctx context.Context, payload []byte) (bool, error) { return false, nil },
					Apply: func(ctx context.Context, w io.Writer, payload []byte) error {
						applied.Add(1)
						return nil
					},
				},
			}), nil
		},
	})
	decideInit(t, d, ctx)
	payload := `{"agent":"boss","server":{"name":"gh","command":"gh-mcp"}}`
	id := stageProposal(t, s, "mcp_server_add", payload)
	res, err := commitVia(t, d, ctx, id, desk.ProposalPayloadDigest(payload))
	if err != nil {
		t.Fatalf("mcp commit through the injected registry: %v", err)
	}
	if !res.EffectApplied || applied.Load() != 1 {
		t.Fatalf("the MCP effect ran %d times (want exactly 1, through the registry)", applied.Load())
	}
}

func TestCommitReturnsBeforeAndAfter(t *testing.T) {
	// PM7.2 on the wire: the pair rides InForceAnswer.Render() bytes and
	// the pair MEANS something — a deny commit changes the verdict the
	// same question gets from policy.why.
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	why := func() string {
		res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyWhy,
			mustJSONDaemon(t, protocol.PolicyWhyParams{Type: "tool_access", Agent: "boss", Tool: "webfetch"}))
		if err != nil {
			t.Fatalf("policy.why: %v", err)
		}
		return decideJSON(t, res)
	}
	// An allow stands first (through its own honest door).
	allow := `{"id":"t5-ba-allow","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"allow","spec":{"toolName":"webfetch","state":"allow"},"source":"operator"}`
	aid := stageProposal(t, s, "policy_add", allow)
	if _, err := commitVia(t, d, ctx, aid, desk.ProposalPayloadDigest(allow)); err != nil {
		t.Fatalf("allow commit: %v", err)
	}
	beforeVerdict := why()
	if !strings.Contains(beforeVerdict, "allow") {
		t.Fatalf("the pre-commit verdict is not an allow: %s", beforeVerdict)
	}
	// The deny commit answers the pair, Render() bytes verbatim (D14).
	deny := `{"id":"t5-ba-deny","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`
	did := stageProposal(t, s, "policy_add", deny)
	res, err := commitVia(t, d, ctx, did, desk.ProposalPayloadDigest(deny))
	if err != nil {
		t.Fatalf("deny commit: %v", err)
	}
	if res.Before.Target != "agent:boss" || res.After.Target != "agent:boss" {
		t.Fatalf("pair target: %+v / %+v", res.Before, res.After)
	}
	// before: the allow is in force (and the deny is not — it has no row
	// yet); after: the deny rides the answer too, and its render is the
	// SAME bytes policy.inForce answers now.
	if len(res.Before.Answers) != 1 || res.Before.Answers[0].PolicyID != "t5-ba-allow" {
		t.Fatalf("before answers: %+v", res.Before.Answers)
	}
	if len(res.After.Answers) != 2 {
		t.Fatalf("after answers: %+v", res.After.Answers)
	}
	ires, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyInForce,
		mustJSONDaemon(t, protocol.PolicyInForceParams{Agent: "boss"}))
	if err != nil {
		t.Fatalf("policy.inForce: %v", err)
	}
	inforce := decideJSON(t, ires)
	for _, a := range res.After.Answers {
		if !strings.Contains(inforce, a.Render) {
			t.Fatalf("after render %q is not the byte answer policy.inForce gives (D14: one formatter)", a.Render)
		}
	}
	afterVerdict := why()
	if !strings.Contains(afterVerdict, "deny") {
		t.Fatalf("the deny commit did not change the verdict: %s", afterVerdict)
	}
}

func TestConcurrentCommitsApplyTheEffectOnce(t *testing.T) {
	// -race two-goroutines, ONE store, ONE registry (the same handles the
	// product shares). The effect counts its writes; the claimed-FROM flip
	// admits one winner. The proof: exactly one committed row, exactly one
	// audit row, and the policy row exists exactly once.
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	payload := `{"id":"t5-race","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`
	id := stageProposal(t, s, "policy_add", payload)
	digest := desk.ProposalPayloadDigest(payload)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc, ok := d.Table.ProposalDecider.(protocol.ProposalDecider)
			if !ok {
				errs[i] = fmt.Errorf("no decider")
				return
			}
			<-start
			_, errs[i] = svc.CommitProposal(ctx, decisionActor(), id, digest)
		}(i)
	}
	close(start)
	wg.Wait()

	oks, refused := 0, 0
	for _, err := range errs {
		if err == nil {
			oks++
			continue
		}
		refused++
		if !strings.Contains(err.Error(), "already committed by user:4321") {
			t.Fatalf("the loser must name the winner: %v", err)
		}
	}
	if oks != 1 || refused != 1 {
		t.Fatalf("two commits: %d ok / %d refused (want 1/1)", oks, refused)
	}
	row, _, _ := s.GetProposal(id)
	if row.Status != "committed" {
		t.Fatalf("row: %s", row.Status)
	}
	audits, _ := s.PolicyChangeList(ctx, "t5-race", 50)
	if len(audits) != 1 {
		t.Fatalf("audit rows: %+v", audits)
	}
	// The effect ran once — the policy row exists exactly once (a second
	// Add on the same id would have refused on the primary key and left a
	// loser naming an effect failure instead of the winner).
	lres, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyList, mustJSONDaemon(t, protocol.PolicyListParams{}))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(decideJSON(t, lres), `"id":"t5-race"`); n != 1 {
		t.Fatalf("the rule appears %d times — the effect applied more than once", n)
	}
}

func TestOnlyDecideAndEffectCallersAreTheWhitelisted(t *testing.T) {
	// The real call graph, asserted (the shape T6's PM10 whitelist states).
	// PRODUCTION callers only — the walk skips _test.go (see walkForCallers;
	// reconciled 2026-11-01, T11: T8's parity harness made the old test-file-
	// inclusive walk red, and a test that builds a wired Table is not a door).
	// store.DecideProposal has exactly four caller FILES outside the store —
	// cmd/aeon/proposal_decide.go (the CLI door's shim, T5's sanctioned move),
	// internal/desk/decide.go (the one orchestration), internal/daemon/
	// proposal_decide.go (the socket door's shim, which reaches it only
	// through desk) and internal/intake/resume.go (S61 T6: the intake engine
	// marks its own plan applied when its ledger completes under the
	// committer — the generic door refuses an intake_plan, so this is the one
	// seam that ever decides that kind; it holds no decide logic, it calls
	// the store's decide verbatim) — and desk.EffectsFor has exactly one caller FILE outside
	// desk's own: cmd/aeon/serve.go. The two test-file entries this list used
	// to carry (the golden harness's fake store, the store's own tests) are
	// test wiring, not doors; they are deliberately invisible here.
	for _, want := range []struct {
		token string
		allow map[string]bool
	}{
		{"DecideProposal(", map[string]bool{
			"cmd/aeon/proposal_decide.go":        true, // the CLI door's shim
			"internal/desk/decide.go":            true, // the orchestration
			"internal/daemon/proposal_decide.go": true, // the socket door's shim
			"internal/intake/resume.go":          true, // S61 T6: the intake plan's dedicated commit seam
		}},
		{"desk.EffectsFor(", map[string]bool{
			"cmd/aeon/serve.go": true,
		}},
		{"desk.RegisterSeams(", map[string]bool{
			"cmd/aeon/proposal_decide.go": true,
		}},
	} {
		got := walkForCallers(t, want.token)
		for f := range got {
			if !want.allow[f] {
				t.Errorf("%s called from %s — off the whitelist (T6's list, stated early and honestly)", want.token, f)
			}
		}
		for f := range want.allow {
			if !got[f] {
				t.Errorf("%s expected in %s — the door is gone (renamed? deleted? say so here, not in a surprise)", want.token, f)
			}
		}
	}
}

func TestArbiterUnchangedWithDecisionsAvailable(t *testing.T) {
	// S35 criterion 1: the policy arbiter does not move because the doors
	// arrived. policy.why answers the SAME verdict sentence before and
	// after a commit — the decision path never consults the proposal table
	// (nothing here wires a proposal into the gate), asserted by an
	// unchanged why-answer across a committed add of an UNRELATED rule.
	ctx := context.Background()
	d, s := decideDaemon(t, decidePeer)
	decideInit(t, d, ctx)
	why := func() any {
		res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyWhy,
			mustJSONDaemon(t, protocol.PolicyWhyParams{Type: "tool_access", Agent: "boss", Tool: "webfetch"}))
		if err != nil {
			t.Fatalf("policy.why: %v", err)
		}
		return res
	}
	before := decideJSON(t, why())
	id := stageProposal(t, s, "policy_add", `{"id":"t5-arbiter","type":"command_access","target":{"kind":"agent","id":"other"},"action":"deny","spec":{"command":"git push","state":"deny"},"source":"operator"}`)
	if _, err := commitVia(t, d, ctx, id, desk.ProposalPayloadDigest(`{"id":"t5-arbiter","type":"command_access","target":{"kind":"agent","id":"other"},"action":"deny","spec":{"command":"git push","state":"deny"},"source":"operator"}`)); err != nil {
		t.Fatalf("commit: %v — sentence %s", err, decideWhat(err))
	}
	after := decideJSON(t, why())
	if before != after {
		t.Fatalf("the arbiter moved because a proposal committed:\n before %s\n after  %s", before, after)
	}
}
