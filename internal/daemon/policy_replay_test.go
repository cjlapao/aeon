package daemon

// The T10 daemon-side tests. The named runtime test proves the DECISION
// (gateForStep consults the manifest); this file proves the PRODUCTION
// MACHINERY: daemon.New's wiring pair, the StepRunner fetch, the frozen-set
// adapter (decode fail-closed, live audit), and — end to end through
// d.Recover — that a recovered step re-run against a mutated store STILL
// refuses what its manifest froze (D12 made real, receipts and marker files
// and all).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// ── fixtures ────────────────────────────────────────────────────────────────

func denyBashEntry(id string) runtime.PolicyEntry {
	return runtime.PolicyEntry{ID: id, Type: "tool_access", TargetKind: "all-agents",
		Action: "deny", Source: "operator", Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`)}
}

func storeBashPolicy(t *testing.T, s *store.Store, id string, action policy.Action, at time.Time) {
	t.Helper()
	storeBashPolicyExpiring(t, s, id, action, at, nil)
}

// storeBashPolicyExpiring seeds one policy, optionally already EXPIRED
// (notAfter before the eval instant). The expired row is the drift story:
// policy_event.policy_id carries a FOREIGN KEY to policy(id), so a replay
// that audits a refusal by a policy whose row was DELETED cannot write the
// row at all (the adapter logs it, the denial stands — T9's discipline).
// The honest overnight mutation is expiry: the row stays (a store that
// deletes history is the audit that lies), it stops matching, and the audit
// trail keeps its FK.
func storeBashPolicyExpiring(t *testing.T, s *store.Store, id string, action policy.Action, at time.Time, notAfter *time.Time) {
	t.Helper()
	boundary := policy.NewStore(newPolicyRepo(s))
	p := policy.Policy{ID: id, Type: policy.ToolAccess, Target: policy.Target{Kind: policy.AllAgents},
		Action: action, Source: policy.Operator, NotAfter: notAfter,
		Spec: json.RawMessage(`{"toolName":"bash","state":"` + string(action) + `"}`)}
	if rej := boundary.Add(context.Background(), p, at); rej != nil {
		t.Fatalf("seeding %s (%s): %s", id, action, rej.Reason)
	}
}

// All-agent command rules, the S11 counterparts of the two fixtures above.
// A bash block that carries a parsable command is decided by the COMMAND
// question (T11's ONE selection rule, daemon/policy_gate.go), so these are
// the rules that actually govern the seeded `touch marker` block; the
// tool_access fixtures keep governing the commandless half of the world.
func allAgentsCommandEntry(id, pattern string, action policy.Action) runtime.PolicyEntry {
	return runtime.PolicyEntry{ID: id, Type: "command_access", TargetKind: "all-agents",
		Action: string(action), Source: "operator",
		Spec: json.RawMessage(`{"command":"` + pattern + `","state":"` + string(action) + `"}`)}
}

func storeCommandPolicy(t *testing.T, s *store.Store, id, pattern string, action policy.Action, at time.Time, notAfter *time.Time) {
	t.Helper()
	boundary := policy.NewStore(newPolicyRepo(s))
	p := policy.Policy{ID: id, Type: policy.CommandAccess, Target: policy.Target{Kind: policy.AllAgents},
		Action: action, Source: policy.Operator, NotAfter: notAfter,
		Spec: json.RawMessage(`{"command":"` + pattern + `","state":"` + string(action) + `"}`)}
	if rej := boundary.Add(context.Background(), p, at); rej != nil {
		t.Fatalf("seeding command policy %s (%s): %s", id, action, rej.Reason)
	}
}

// seedFrozenRecoveryStep seeds a running step with a pending bash tool_use
// and a snapshot whose manifest froze `policies` — compiled through the
// PRODUCTION StepRunner (pass-through of the evaluated set), so the frozen
// entries and the hash are production output, not test shapes.
func seedFrozenRecoveryStep(t *testing.T, s *store.Store, root string, policies []runtime.PolicyEntry) (int64, string) {
	t.Helper()
	slug := "recovery"
	if _, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Goal: "test"}); err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewStep{Agent: "test", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	_, specs, err := (StepRunner{}).BuildTools([]string{"bash"}, root, "test", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	// The gate question's Agent is the STEP'S OWN identity as the policy
	// engine sees it: runTool asks with Handler.Name, so the name must be
	// one the boundary accepts as an id (SEC-013: no colons — the ad-hoc
	// mint is "step-ad-hoc" with a dash for exactly this reason, DEF-041).
	// Blueprint steps carry real names; this seed names its handler the way
	// a blueprint would.
	handler := runtime.DefaultHandler("test", []string{"bash"})
	handler.Name = "test"
	compiled := (StepRunner{}).Compile(context.Background(), runtime.CompileInput{
		Handler: handler, Granted: []string{"bash"},
		Route:         model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root, ToolSpecs: specs,
	}, runtime.ContextDeps{Policies: policies})
	if !compiled.OK {
		t.Fatal(compiled.Reason)
	}
	compiled.Request.StepID = step.ID
	msgs := compiled.Request.Messages
	msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: []model.Block{
		{Kind: model.BlockToolUse, ID: "u1", Name: "bash", Input: json.RawMessage(`{"command":"touch marker"}`)},
	}})
	raw, err := json.Marshal(protocol.StepSnapshot{ContextManifest: compiled.Request.Manifest,
		Request: compiled.Request, LoopSlug: slug})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRunningCheckpoint(context.Background(), step.ID, msgs, raw, model.Cost{}); err != nil {
		t.Fatal(err)
	}
	return step.ID, compiled.Request.Manifest.PoliciesHash
}

// ── the wiring pair (extends T9's claim: the gate arrives as a PAIR) ────────

func TestProductionDaemonWiresThePolicyPair(t *testing.T) {
	// T9 pinned the gate; T10 makes it a triple: live gate, frozen-set
	// constructor, and the once-per-step fetch. Any one present without the
	// others is a wiring defect (live-gate-only replays against today's
	// table), so the assertion is all-three-or-none on both sides of the
	// store condition.
	s := recoveryStoreBound(t)
	d := New(Options{Store: s})
	if d.Table.PolicyGate == nil {
		t.Fatal("the store-backed daemon's table has NO policy gate (D11/PL9)")
	}
	if d.Table.PolicySet == nil {
		t.Fatal("the store-backed daemon's table has NO frozen-set constructor — a recovered step would " +
			"replay against the live store, which is D12's drift")
	}
	runner, ok := d.Table.Runner.(StepRunner)
	if !ok {
		t.Fatalf("the table's runner is %T, not StepRunner — the fetch cannot be pinned", d.Table.Runner)
	}
	if runner.PolicyList == nil {
		t.Fatal("the store-backed runner has NO PolicyList — first-assembly manifests would freeze nothing " +
			"while the store holds rules (the step runs uninformed, PL9's informing half silently absent)")
	}
	d0 := New(Options{})
	if d0.Table.PolicyGate != nil || d0.Table.PolicySet != nil {
		t.Error("the storeless daemon grew a gate pair; with no store every read fails closed and pre-S10 " +
			"daemons would stop running")
	}
	if r0, ok := d0.Table.Runner.(StepRunner); !ok || r0.PolicyList != nil {
		t.Error("the storeless runner grew a policy fetch")
	}
}

// ── the frozen-set adapter, unit level ──────────────────────────────────────

func TestStepPolicyGateDecidesFromTheFrozenSetNotTheStore(t *testing.T) {
	// NewStepPolicyGate: pinned non-empty ⇒ the set answers, and the AUDIT
	// stays live (refusal + disagreement rows land in the real store).
	// Pinned empty ⇒ the live store answers (pre-S10 fallback). A poisoned
	// entry ⇒ PL5 for every question, even one the store would allow.
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	// The store, deliberately OPPOSITE to the frozen set: bash is allowed
	// today. no-bash's row is present but EXPIRED (the row the audit's FK
	// will reference; see storeBashPolicyExpiring's why).
	expiredAt := at.Add(-time.Minute)
	storeBashPolicy(t, s, "yes-bash", policy.Allow, at)
	storeBashPolicyExpiring(t, s, "no-bash", policy.Deny, at, &expiredAt)

	w := &wire{}
	g := NewStepPolicyGate(s, w, runtime.FrozenManifest{Entries: []runtime.PolicyEntry{denyBashEntry("no-bash")}})
	dec := g.Decide(runtime.PolicyGateRequest{Agent: "test-handler", Tool: "bash", StepID: 3, ModelRequested: true}, at)
	if dec.Allowed {
		t.Fatal("the frozen deny lost to the store's allow — the replay gate consulted the store (D12)")
	}
	if !strings.HasPrefix(dec.Sentence, "refused:") {
		t.Errorf("the frozen denial did not render the sentence: %q", dec.Sentence)
	}
	rows, err := s.PolicyEventQuery(context.Background(), "agent:test-handler", 10)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, r := range rows {
		kinds[r.Outcome] = true
		if r.PolicyID != "no-bash" {
			t.Errorf("the row names %q, want the frozen policy that fired", r.PolicyID)
		}
	}
	if !kinds["refusal"] || !kinds["disagreement"] {
		t.Errorf("the frozen denial wrote kinds %v; the adapter must record the refusal AND (model-emitted, "+
			"attributed) the disagreement — through the LIVE store, not the frozen set", kinds)
	}

	// Pinned empty ⇒ live store answers. The store here denies (a fresh
	// deny row appears); the same question on the frozen gate above allowed
	// nothing only because the frozen set said so.
	storeBashPolicy(t, s, "no-bash-live", policy.Deny, at.Add(time.Second))
	live := NewStepPolicyGate(s, w, runtime.FrozenManifest{})
	if d := live.Decide(runtime.PolicyGateRequest{Agent: "other-handler", Tool: "bash"}, at.Add(time.Hour)); d.Allowed {
		t.Error("the live-fallback gate allowed what the STORE denies — pinned-empty lost the live read")
	}

	// A poisoned entry PL5s the whole gate — even questions the (allowing)
	// store would pass. Dropping the bad row is dropping a possible Deny.
	broken := NewStepPolicyGate(s, w, runtime.FrozenManifest{Entries: []runtime.PolicyEntry{{ID: "junk", Type: "not-a-type",
		TargetKind: "all-agents", Action: "deny", Source: "operator", Spec: json.RawMessage(`{}`)}}})
	if d := broken.Decide(runtime.PolicyGateRequest{Agent: "test-handler", Tool: "read"}, at); d.Allowed {
		t.Error("a frozen set with an engine-rejected entry was ALLOWED — a tampered manifest must fail closed " +
			"as a whole, never drop the row that broke it")
	}
	if !strings.Contains(w.logLines(), "could not be read") {
		t.Errorf("the poisoned-set denial was not logged: %s", w.logLines())
	}
}

// The SEC-042 plant, made permanent. The security record reproduced this
// class THREE times (T6's on-disk row, T9's forged entries, the QA probe's
// frozen-manifest path): decodeFrozenSet takes every manifest entry through
// policy.DecodePolicy, and until T19 that decoder checked each field's
// VOCABULARY but never the action↔source PAIR — so a well-formed
// {"action":"allow","source":"agent"} entry was honoured on replay, and an
// agent-authored manifest edit could hand itself any permission. This is the
// three-times-reproduced attack as a test: the same all-or-nothing poison as
// the broken-kind case above, arrived at through the pair instead of through
// an unknown kind.
func TestStepPolicyGatePoisonsOnAForgedAllowFromANonOperatorSource(t *testing.T) {
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	// The live store deliberately would ALLOW bash: the frozen gate answers
	// from the manifest (D12), so what it decides here is the forged entry's
	// doing alone — and a poisoned gate must deny even MORE than the store
	// would, never less.
	storeBashPolicy(t, s, "yes-bash", policy.Allow, at)

	w := &wire{}
	forged := NewStepPolicyGate(s, w, runtime.FrozenManifest{Entries: []runtime.PolicyEntry{{ID: "forged-self-grant", Type: "tool_access",
		TargetKind: "agent", TargetID: "boss", Action: "allow", Source: "agent",
		Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`)}}})
	d := forged.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash"}, at)
	if d.Allowed {
		t.Fatal("the forged allow/agent manifest entry was honoured on replay — SEC-042's plant, " +
			"unreproduced only because DecodePolicy now refuses the pair")
	}
	// The engine's own default-deny sentence — compared against what the
	// formatter RENDERS for this request (D9's discipline: never a
	// transcription of the sentence, the sentence itself), not invented prose.
	wantDeny := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}, at).Sentence()
	if d.Sentence != wantDeny {
		t.Errorf("the poisoned gate did not answer with the engine's default-deny sentence:\n got  %q\nwant  %q", d.Sentence, wantDeny)
	}
	logged := w.logLines()
	if !strings.Contains(logged, "could not be read") || !strings.Contains(logged, "forged-self-grant") {
		t.Errorf("the poisoning was not logged, or does not name the entry that poisoned it: %s", logged)
	}

	// The positive control (T10's plant discipline — a gate that poisoned
	// EVERYTHING would pass the assertion above too): the legitimate
	// direction from the same non-operator source — an agent-authored DENY —
	// still decodes and still replays exactly as it did before T19. The
	// pair-check closes the self-grant; it must not break the narrowings PL1
	// exists to welcome.
	w2 := &wire{}
	genuine := NewStepPolicyGate(s, w2, runtime.FrozenManifest{Entries: []runtime.PolicyEntry{{ID: "agent-no-bash", Type: "tool_access",
		TargetKind: "agent", TargetID: "boss", Action: "deny", Source: "agent",
		Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`)}}})
	d = genuine.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash"}, at)
	if d.Allowed {
		t.Error("the genuine deny/agent frozen set permitted its own tool — the set was not replayed")
	}
	if !strings.Contains(d.Sentence, "agent-no-bash") {
		t.Errorf("the frozen deny did not fire as the set's own (the sentence should name it): %q", d.Sentence)
	}
	if strings.Contains(w2.logLines(), "could not be read") {
		t.Errorf("a legitimate non-operator deny poisoned the gate — PL1's safe direction must replay: %s", w2.logLines())
	}
}

// ── DEF-039: the digest is RECOMPUTED and COMPARED at load ─────────────────

// countingBridge is the production RepoFuncs bridge with a counter on the ONE
// read of the live policy table (ListAll). The mismatch path must leave it at
// zero: re-deciding a half-updated record against today's store is the drift
// D12 forbids, so "no live read" is a measured zero, not a reading of code.
func countingBridge(t *testing.T, s *store.Store) (policy.RepoFuncs, *int) {
	t.Helper()
	repo := newPolicyRepo(s)
	reads := 0
	inner := repo.ListAll
	repo.ListAll = func(ctx context.Context) ([]policy.Row, error) {
		reads++
		return inner(ctx)
	}
	return repo, &reads
}

// frozenTamperedAllow is the tampered entry the digest exists for: a
// well-formed operator allow of bash where the step froze a deny. It DECODES
// perfectly — vocabulary, pair, spec all legal — so only the recomputed
// digest can catch it.
func frozenTamperedAllow() []runtime.PolicyEntry {
	return []runtime.PolicyEntry{{ID: "no-bash", Type: "tool_access", TargetKind: "all-agents",
		Action: "allow", Source: "operator", Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`)}}
}

func TestFrozenSetContradictingItsRecordedDigestDeniesEverything(t *testing.T) {
	// The unit half of DEF-039: NewStepPolicyGate recomputes
	// runtime.PoliciesHashOf over the entries it is handed and compares it to
	// the digest the manifest recorded, BEFORE decodeFrozenSet. On mismatch
	// every Decide answers the engine's default-deny sentence, the Infof
	// names BOTH digests, and the live policy table is never read. The live
	// store here allows bash on purpose: a path that trusted the tampered
	// entries or fell back to the store would ALLOW, so the denial can only
	// come from the comparison.
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	storeBashPolicy(t, s, "yes-bash", policy.Allow, at)

	pristine := []runtime.PolicyEntry{denyBashEntry("no-bash")}
	recorded := runtime.PoliciesHashOf(pristine) // what assembly minted beside the set
	tampered := frozenTamperedAllow()
	gotHash := runtime.PoliciesHashOf(tampered)

	repo, liveReads := countingBridge(t, s)
	w := &wire{}
	g := newStepPolicyGateWithBoundary(policy.NewStore(repo), w,
		runtime.FrozenManifest{Entries: tampered, Hash: recorded})

	// EVERY question is denied, twice over: the tampered tool and an
	// unrelated one (a mismatch poisons the gate; it does not just flip the
	// one entry the tamperor touched).
	for _, tool := range []string{"bash", "read"} {
		d := g.Decide(runtime.PolicyGateRequest{Agent: "test-handler", Tool: tool, StepID: 3, ModelRequested: true}, at)
		wantDeny := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "test-handler", Tool: tool}, at).Sentence()
		if d.Allowed || d.Sentence != wantDeny {
			t.Errorf("Decide(%s) = {Allowed:%v Sentence:%q}, want the engine's default-deny sentence %q — "+
				"a half-updated governance record must deny as an unreadable set (PL5), never half-decide",
				tool, d.Allowed, d.Sentence, wantDeny)
		}
	}
	if *liveReads != 0 {
		t.Errorf("the mismatch path read the live policy table %d time(s) — DEF-039's fix is deny-all via "+
			"Evaluate(nil,...), never silently re-deciding against the live store", *liveReads)
	}
	logged := w.logLines()
	if !strings.Contains(logged, "does not match the set it carries") || !strings.Contains(logged, recorded) || !strings.Contains(logged, gotHash) {
		t.Errorf("the mismatch Infof must name BOTH digests (recorded %s, recomputed %s); log: %s", recorded, gotHash, logged)
	}

	// A garbage digest denies the same way — even over the PRISTINE set, so
	// what fired is the comparison, not the decode (the set decodes fine).
	w2 := &wire{}
	g2 := newStepPolicyGateWithBoundary(policy.NewStore(repo), w2,
		runtime.FrozenManifest{Entries: pristine, Hash: "deadbeef"})
	if d := g2.Decide(runtime.PolicyGateRequest{Agent: "test-handler", Tool: "bash"}, at); d.Allowed {
		t.Error("a garbage policiesHash was accepted — truncation/corruption of the digest must deny everything")
	}
	// SEC-066: "deadbeef" is not the 64-lowercase-hex shape PoliciesHashOf
	// mints, so it is NEVER echoed back — the recorded value arrived from a
	// stored manifest a same-user process can rewrite. The Infof names the
	// trusted recomputed digest and flags the recorded one BY SHAPE.
	if l := w2.logLines(); !strings.Contains(l, "not a hex digest") || !strings.Contains(l, recorded) {
		t.Errorf("the malformed-digest Infof must name the recomputed digest (%s) and flag the recorded one "+
			"as 'not a hex digest' (SEC-066); log: %s", recorded, l)
	}
	if strings.Contains(w2.logLines(), "deadbeef") {
		t.Errorf("the malformed recorded digest was echoed into the log — the hex64 shape gate is the point "+
			"(SEC-066): %s", w2.logLines())
	}

	// The positive control: the HONEST pair (pristine entries + their real
	// digest) passes the comparison and still replays the frozen deny, while
	// the store would allow. Without this, "mismatch denies" could mean
	// "the gate denies everything".
	w3 := &wire{}
	g3 := newStepPolicyGateWithBoundary(policy.NewStore(repo), w3,
		runtime.FrozenManifest{Entries: pristine, Hash: recorded})
	d := g3.Decide(runtime.PolicyGateRequest{Agent: "test-handler", Tool: "bash", StepID: 3, ModelRequested: true}, at)
	if d.Allowed || !strings.Contains(d.Sentence, "no-bash") {
		t.Errorf("the matching digest did not pass the comparison through to a faithful replay: %+v", d)
	}
	if strings.Contains(w3.logLines(), "does not match") {
		t.Errorf("the honest pair was accused of a mismatch: %s", w3.logLines())
	}

	// The deliberate empty-hash rule, as a test and not a guess: a hand-built
	// manifest has no digest on record to contradict, so its entries replay
	// AS-IS. The store still allows bash, so the deny below can only come
	// from the entries themselves — and zero live reads stays the invariant.
	w4 := &wire{}
	g4 := newStepPolicyGateWithBoundary(policy.NewStore(repo), w4,
		runtime.FrozenManifest{Entries: pristine, Hash: ""})
	d = g4.Decide(runtime.PolicyGateRequest{Agent: "test-handler", Tool: "bash", StepID: 3, ModelRequested: true}, at)
	if d.Allowed || !strings.Contains(d.Sentence, "no-bash") {
		t.Errorf("entries with no recorded hash did not replay against those entries: %+v — the empty-hash "+
			"rule is deliberate (no digest on record to contradict) and must not become a mismatch", d)
	}
	if strings.Contains(w4.logLines(), "does not match") {
		t.Errorf("an empty recorded hash was treated as a mismatch: %s", w4.logLines())
	}
	if *liveReads != 0 {
		t.Errorf("the frozen replay paths read the live policy table %d time(s) in total; the boundary "+
			"listens ONLY on the live/pre-S10 fallback", *liveReads)
	}
}

func TestRecoveredStepWithAHalfUpdatedManifestDeniesEverything(t *testing.T) {
	// DEF-039 end to end through d.Recover and the REAL store: the stored
	// step.context_manifest is tampered — the frozen entries rewritten while
	// policiesHash is left exactly as assembly minted it — and recovery must
	// deny everything. The live store is the tamperor's dream (a bash allow,
	// the deny row removed), so the denial cannot come from the store, and
	// the untouched control elsewhere in this file proves bash CAN run: only
	// a recomputed-and-compared digest explains a refusal here.
	base := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(snap *protocol.StepSnapshot)
		// wantRecordedLogged is what the mismatch Infof must say about the
		// RECORDED digest: a genuine 64-hex value is quoted back (it passed
		// the SEC-066 shape gate), a malformed one is named by shape only.
		wantRecordedLogged string
		// wantEchoed records whether the malformed value must NOT appear in
		// the log at all (SEC-066); empty means no such extra check.
		wantNotEchoed string
	}{
		{"half-updated entries, digest left alone", func(snap *protocol.StepSnapshot) {
			snap.ContextManifest.Policies = frozenTamperedAllow()
		}, "", ""},
		{"garbage digest, entries untouched", func(snap *protocol.StepSnapshot) {
			snap.ContextManifest.PoliciesHash = "deadbeef"
		}, "not a hex digest", "deadbeef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := recoveryStoreBound(t)
			root := t.TempDir()
			storeBashPolicy(t, s, "no-bash", policy.Deny, base)
			id, hash := seedFrozenRecoveryStep(t, s, root, []runtime.PolicyEntry{denyBashEntry("no-bash")})

			// The overnight mutation: today's LIVE set allows bash, so a
			// fallback to the store (or trust in tampered entries) runs the
			// marker.
			if ok, err := policy.NewStore(newPolicyRepo(s)).Remove(context.Background(), "no-bash"); err != nil || !ok {
				t.Fatal("the drift: the deny was not removable")
			}
			storeBashPolicy(t, s, "yes-bash", policy.Allow, base.Add(time.Minute))

			// Tamper the STORED manifest through the same checkpoint path the
			// runtime writes it, leaving every other field byte-identical.
			st, err := s.ReadStep(id)
			if err != nil {
				t.Fatal(err)
			}
			var snap protocol.StepSnapshot
			if err := json.Unmarshal(st.ContextManifestRaw, &snap); err != nil {
				t.Fatal(err)
			}
			if snap.ContextManifest.PoliciesHash != hash {
				t.Fatalf("the stored manifest does not carry the digest assembly minted (%s → %s) — the "+
					"tamper premise is broken before the tamper is even applied", hash, snap.ContextManifest.PoliciesHash)
			}
			tc.mutate(&snap)
			recorded, recomputed := snap.ContextManifest.PoliciesHash, runtime.PoliciesHashOf(snap.ContextManifest.Policies)
			raw, err := json.Marshal(snap)
			if err != nil {
				t.Fatal(err)
			}
			trans := s.ReadTranscript(id)
			if !trans.OK {
				t.Fatalf("the seeded transcript is unreadable: %s", trans.Reason())
			}
			if err := s.SaveRunningCheckpoint(context.Background(), id, trans.Messages, raw, model.Cost{}); err != nil {
				t.Fatal(err)
			}

			d, w := recoveryDaemon(s)
			sum, err := d.Recover(context.Background(), false)
			if err != nil || sum.Recovered != 1 {
				t.Fatalf("summary=%+v err=%v\n%s", sum, err, w.logLines())
			}
			if markerExists(root) {
				t.Fatal("THE TAMPERED MANIFEST LET BASH RUN — the digest was trusted, not proved (DEF-039)")
			}
			l := w.logLines()
			if !strings.Contains(l, "does not match the set it carries") || !strings.Contains(l, recomputed) {
				t.Errorf("recovery did not log the mismatch naming the recomputed digest (%s):\n%s", recomputed, l)
			}
			if tc.wantRecordedLogged == "" {
				// The recorded value is a genuine hex64 digest: it passed the
				// SEC-066 shape gate and is quoted back verbatim.
				if !strings.Contains(l, recorded) {
					t.Errorf("recovery did not name the recorded digest (%s) in the mismatch Infof:\n%s", recorded, l)
				}
			} else if !strings.Contains(l, tc.wantRecordedLogged) {
				t.Errorf("recovery did not flag the recorded digest as malformed (%q expected in the log):\n%s",
					tc.wantRecordedLogged, l)
			}
			if tc.wantNotEchoed != "" && strings.Contains(l, tc.wantNotEchoed) {
				t.Errorf("the malformed recorded digest was echoed into the operator's log (SEC-066): %s", l)
			}
			// Every Decide denied with the engine's default-deny sentence —
			// read it off the transcript the recovery committed, byte for
			// byte (never a paraphrase, D9).
			wantDeny := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "test", Tool: "bash"}, base).Sentence()
			var sawDenial bool
			for _, m := range s.ReadTranscript(id).Messages {
				for _, b := range m.Content {
					if b.Kind == model.BlockToolResult && b.ToolUseID == "u1" {
						if !b.IsError || b.Content != wantDeny {
							t.Errorf("the tampered replay answered u1 with {IsError:%v Content:%q}, want the "+
								"default-deny sentence %q", b.IsError, b.Content, wantDeny)
						}
						sawDenial = true
					}
				}
			}
			if !sawDenial {
				t.Error("u1 was never answered with a tool_result — the step did not route through the frozen gate")
			}
			// The record is left alone: recovery denied; it did not rewrite
			// the digest to match anything (replay never re-decides the past,
			// including in the record layer).
			st2, err := s.ReadStep(id)
			if err != nil {
				t.Fatal(err)
			}
			var snap2 protocol.StepSnapshot
			if err := json.Unmarshal(st2.ContextManifestRaw, &snap2); err != nil {
				t.Fatal(err)
			}
			if snap2.ContextManifest.PoliciesHash != recorded {
				t.Errorf("recovery rewrote the stored digest (%s → %s)", recorded, snap2.ContextManifest.PoliciesHash)
			}
		})
	}
}

// ── the StepRunner fetch ────────────────────────────────────────────────────

func TestCompileFreezesTheEvaluatedSetOnce(t *testing.T) {
	// The pass-through fetch: exactly ONE PolicyList call per Compile, the
	// entries land in the manifest (frozen, hashed), and a read failure
	// FAILS the construction — a step that cannot say what binds it must not
	// run claiming to.
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	storeBashPolicy(t, s, "no-bash", policy.Deny, at)

	calls := 0
	r := StepRunner{PolicyList: func(c context.Context, when time.Time) ([]runtime.PolicyEntry, error) {
		calls++
		entries, rej := PolicyEntriesAt(c, s, when)
		if rej != nil {
			return nil, errors.New(rej.Reason)
		}
		return entries, nil
	}}
	root := t.TempDir()
	_, specs, err := r.BuildTools([]string{"bash"}, root, "test", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	in := runtime.CompileInput{Handler: runtime.DefaultHandler("test", []string{"bash"}),
		Granted: []string{"bash"}, Route: model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root, ToolSpecs: specs}

	compiled := r.Compile(context.Background(), in, runtime.ContextDeps{})
	if !compiled.OK {
		t.Fatalf("compile refused: %s", compiled.Reason)
	}
	if calls != 1 {
		t.Errorf("PolicyList was consulted %d times, want exactly 1 — two fetches are two instants the set "+
			"could disagree with itself (D12)", calls)
	}
	if len(compiled.Request.Manifest.Policies) != 1 || compiled.Request.Manifest.Policies[0].ID != "no-bash" {
		t.Fatalf("the manifest froze %+v, want the one stored deny", compiled.Request.Manifest.Policies)
	}
	if compiled.Request.Manifest.PoliciesHash != runtime.PoliciesHashOf(compiled.Request.Manifest.Policies) {
		t.Error("the frozen set and its digest do not match — the audit hash must be recomputable from the set")
	}

	// The read failure is a CONSTRUCTION failure.
	failing := StepRunner{PolicyList: func(context.Context, time.Time) ([]runtime.PolicyEntry, error) {
		return nil, errors.New("the policy table is unreadable")
	}}
	bad := failing.Compile(context.Background(), in, runtime.ContextDeps{})
	if bad.OK || !strings.Contains(bad.Reason, "unreadable") || !strings.Contains(bad.Reason, "blind") {
		t.Errorf("an unreadable policy set assembled the step anyway (ok=%v, reason=%q) — it must fail "+
			"construction, fail-closed", bad.OK, bad.Reason)
	}

	// The zero runner (tests, storeless) fetches nothing and freezes nothing.
	zero := (StepRunner{}).Compile(context.Background(), in, runtime.ContextDeps{})
	if !zero.OK || len(zero.Request.Manifest.Policies) != 0 {
		t.Errorf("the zero runner froze %+v; with no policy source the manifest stays honestly empty",
			zero.Request.Manifest.Policies)
	}
}

// ── the E2E replay honesty proof ────────────────────────────────────────────

type spyForwardGate struct {
	mu    sync.Mutex
	saw   []runtime.PolicyGateRequest
	inner runtime.PolicyGate
}

func (g *spyForwardGate) Decide(req runtime.PolicyGateRequest, at time.Time) runtime.PolicyGateDecision {
	g.mu.Lock()
	g.saw = append(g.saw, req)
	g.mu.Unlock()
	if g.inner != nil {
		return g.inner.Decide(req, at)
	}
	// Default DENY: the test's store is empty (PL5 would deny anyway), and
	// an ALLOWING spy would prove forwarding while letting the tool run —
	// which is exactly what "the spy's decision was not obeyed" must catch.
	// The text is deliberately the spy's OWN words, not the engine's: the
	// refusal sentence belongs to verdict.go alone (TestSentenceIsTheOnly
	// FormatterInTheModule), and a spy that mimics it would be a second
	// copy of a thing that must have one.
	return runtime.PolicyGateDecision{Allowed: false, Sentence: "spy gate: the test refused this"}
}

func markerExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, "marker"))
	return err == nil
}

func TestRecoveredStepIsJudgedByItsFrozenSetNotTodaysStore(t *testing.T) {
	// THE end-to-end claim (D12), through d.Recover and the REAL store:
	//
	//	step compiled under set A (frozen deny) → store mutated to set B
	//	(deny gone, allow added) → recovery re-runs the step → bash is STILL
	//	refused, the marker never appears, the audit shows the frozen policy
	//	named under both kinds, and the stored manifest hash is byte-unchanged.
	//
	// Then the two mirror controls that make the assertion load-bearing:
	//	a manifest WITHOUT a frozen set falls back to the live store (deny
	//	there refuses); the same live store without the deny lets bash run.
	// Without those, "still refused" could just mean "nothing ever runs".
	base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	// (1) Frozen deny beats today's allow. The step is compiled with set A
	// {no-bash} — which IS the stored set at seed time. The overnight
	// mutation: no-bash expires (row kept, matching stopped) and a bash
	// allow joins. Today the call is allowed; the replay must still refuse.
	s1 := recoveryStoreBound(t)
	root1 := t.TempDir()
	// The S11 world: the seeded `touch marker` block carries a parsable
	// command, so the gate asks the COMMAND question (T11's ONE rule) and
	// command_access rules govern it. The drift story is unchanged: frozen
	// deny vs a today-store that would allow.
	storeCommandPolicy(t, s1, "no-bash", "touch*", policy.Deny, base, nil)
	id1, hash1 := seedFrozenRecoveryStep(t, s1, root1, []runtime.PolicyEntry{allAgentsCommandEntry("no-bash", "touch*", policy.Deny)})
	if ok, err := func() (bool, error) {
		b := policy.NewStore(newPolicyRepo(s1))
		return b.Remove(context.Background(), "no-bash")
	}(); err != nil || !ok {
		t.Fatal("the drift: the deny was not removable")
	}
	expiredAt := time.Now().Add(-time.Minute)
	storeCommandPolicy(t, s1, "no-bash", "touch*", policy.Deny, time.Now(), &expiredAt)
	storeCommandPolicy(t, s1, "yes-bash", "touch*", policy.Allow, time.Now(), nil)
	d1, w1 := recoveryDaemon(s1)
	sum, err := d1.Recover(context.Background(), false)
	if err != nil || sum.Recovered != 1 {
		t.Fatalf("summary=%+v err=%v\n%s", sum, err, w1.logLines())
	}
	if markerExists(root1) {
		t.Fatal("THE RECOVERED STEP EXECUTED THE FROZEN-DENIED BASH — recovery consulted today's store, " +
			"not the manifest (D12)")
	}
	rows, err := s1.PolicyEventQuery(context.Background(), "agent:test", 10)
	if err != nil {
		t.Fatal(err)
	}
	var sawDisagreement bool
	for _, r := range rows {
		if r.Outcome == "disagreement" && r.PolicyID == "no-bash" {
			sawDisagreement = true
		}
	}
	if !sawDisagreement {
		st1p, _ := s1.ReadStep(id1)
		t.Errorf("the replayed refusal wrote no disagreement naming the frozen policy: %+v (step=%+v)\ndaemon log:\n%s",
			rows, st1p, w1.logLines())
	}
	st1, err := s1.ReadStep(id1)
	if err != nil {
		t.Fatal(err)
	}
	var snap protocol.StepSnapshot
	if err := json.Unmarshal(st1.ContextManifestRaw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.ContextManifest.PoliciesHash != hash1 {
		t.Errorf("recovery re-hashed the frozen set (%s → %s) — replay must not re-decide the past, "+
			"including in the record layer", hash1, snap.ContextManifest.PoliciesHash)
	}

	// (2) Pre-S10 manifest (no frozen set) → LIVE store decides: deny there
	// still refuses. This is the fallback the seed demands, and it proves
	// (1) was not "the gate always denies". A command-carrying block asks
	// the command question, so the governing live deny is a command rule.
	s2 := recoveryStoreBound(t)
	root2 := t.TempDir()
	storeCommandPolicy(t, s2, "no-bash-live", "touch*", policy.Deny, base, nil)
	id2, _ := seedFrozenRecoveryStep(t, s2, root2, nil)
	d2, _ := recoveryDaemon(s2)
	if sum, err := d2.Recover(context.Background(), false); err != nil || sum.Recovered != 1 {
		t.Fatalf("fallback summary=%+v err=%v", sum, err)
	}
	if markerExists(root2) {
		t.Fatal("a manifest-free step ran bash that the LIVE store denies — the pre-S10 fallback stopped " +
			"consulting the store (D12 the other way)")
	}
	_ = id2

	// (3) Control: the same step shape with NOTHING denying anywhere runs
	// bash for real (the marker proves the harness can execute — (1)'s
	// absence is a refusal, not a dead tool). The governing allow for a
	// command-carrying block is a command rule (S11's ONE rule).
	s3 := recoveryStoreBound(t)
	root3 := t.TempDir()
	id3, _ := seedFrozenRecoveryStep(t, s3, root3, nil)
	_ = id3
	storeCommandPolicy(t, s3, "yes-bash-live", "touch*", policy.Allow, base, nil)
	d3, _ := recoveryDaemon(s3)
	if sum, err := d3.Recover(context.Background(), false); err != nil || sum.Recovered != 1 {
		t.Fatalf("control summary=%+v err=%v", sum, err)
	}
	if !markerExists(root3) {
		t.Fatal("the control step never executed bash — the marker assertions above certify nothing")
	}
}

// ── DEF-042 plant: a zero-policy step is FROZEN, not pre-S10 ───────────────

// seedStepWithRunner is seedFrozenRecoveryStep with the ASSEMBLY made
// explicit: the step is compiled through the given StepRunner (which may
// carry the production PolicyList fetch) over the given ContextDeps, exactly
// as protocol/step.go does it — so what the manifest ends up claiming about
// the policy set is production output of Compile + AssembleContext, not a
// test-authored shape.
func seedStepWithRunner(t *testing.T, s *store.Store, root string, runner StepRunner, deps runtime.ContextDeps) (int64, runtime.ContextManifest) {
	t.Helper()
	slug := "recovery"
	if _, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Goal: "test"}); err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewStep{Agent: "test", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	_, specs, err := (StepRunner{}).BuildTools([]string{"bash"}, root, "test", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	handler := runtime.DefaultHandler("test", []string{"bash"})
	handler.Name = "test"
	compiled := runner.Compile(context.Background(), runtime.CompileInput{
		Handler: handler, Granted: []string{"bash"},
		Route:         model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root, ToolSpecs: specs,
	}, deps)
	if !compiled.OK {
		t.Fatal(compiled.Reason)
	}
	compiled.Request.StepID = step.ID
	msgs := compiled.Request.Messages
	msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: []model.Block{
		{Kind: model.BlockToolUse, ID: "u1", Name: "bash", Input: json.RawMessage(`{"command":"touch marker"}`)},
	}})
	raw, err := json.Marshal(protocol.StepSnapshot{ContextManifest: compiled.Request.Manifest,
		Request: compiled.Request, LoopSlug: slug})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRunningCheckpoint(context.Background(), step.ID, msgs, raw, model.Cost{}); err != nil {
		t.Fatal(err)
	}
	return step.ID, compiled.Request.Manifest
}

func TestZeroPolicyStepRecoveryStaysFrozenNotPreS10(t *testing.T) {
	// THE DEF-042 plant, as the finding prescribes it: compile and suspend a
	// step over a store holding ZERO policies (the normal state on a fresh
	// install, not a corner), have an operator add an allow for the tool the
	// step's next iteration wants — through the boundary, not by hand — and
	// Recover. The call must STILL be refused: the step ran under
	// default-deny, and a replay that re-decides that past against today's
	// table executes a call the original step was refused. This is the
	// escape direction of D12's prohibition: the one that GRANTS a
	// permission.
	base := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	s := recoveryStoreBound(t)
	root := t.TempDir()

	// The store holds zero policies — a fresh install. Production shape
	// (protocol/step.go:336): assembly is handed NO set, the daemon fetches.
	fetches := 0
	runner := StepRunner{PolicyList: func(c context.Context, when time.Time) ([]runtime.PolicyEntry, error) {
		fetches++
		entries, rej := PolicyEntriesAt(c, s, when)
		if rej != nil {
			return nil, errors.New(rej.Reason)
		}
		return entries, nil
	}}
	id, manifest := seedStepWithRunner(t, s, root, runner, runtime.ContextDeps{})
	if fetches != 1 {
		t.Fatalf("PolicyList was consulted %d time(s), want exactly 1 — the step must see one instant (D12)", fetches)
	}
	// The claim the manifest now has to be able to make (DEF-042): a set WAS
	// consulted and found empty. Nothing is frozen — the copy stays nothing
	// (freezePolicyEntries' nil rule, pinned in the runtime package) — but
	// the marker says the emptiness is a fact about the world, and that is
	// what routes the replay to the frozen branch.
	if !manifest.PoliciesFrozen {
		t.Error("a step assembled over a consulted (empty) store carries NO PoliciesFrozen marker — " +
			"this is the byte-identical-to-pre-S10 shape that reopened the live fallback (DEF-042)")
	}
	if manifest.Policies != nil || manifest.PoliciesHash != "" {
		t.Errorf("an empty assembly froze %+v / hash %q; freezePolicyEntries must still return nil for "+
			"the empty set — the marker carries the claim, not an empty-but-present slice",
			manifest.Policies, manifest.PoliciesHash)
	}

	// The overnight mutation: the operator adds an allow for the command the
	// step's pending block carries while the step is suspended. Today's set
	// WOULD grant that call (a command-carrying bash block is governed by
	// command rules, S11's ONE rule — a tool allow is inert for it).
	storeCommandPolicy(t, s, "yes-bash", "touch*", policy.Allow, base, nil)

	d, w := recoveryDaemon(s)
	sum, err := d.Recover(context.Background(), false)
	if err != nil || sum.Recovered != 1 {
		t.Fatalf("summary=%+v err=%v\n%s", sum, err, w.logLines())
	}
	if markerExists(root) {
		t.Fatal("THE ZERO-POLICY STEP RE-DECIDED ITS OWN PAST — recovery fell back to the LIVE store and " +
			"executed the bash that the step's frozen empty set refuses (DEF-042)")
	}
	// The denial is audited like every other refusal.
	rows, err := s.PolicyEventQuery(context.Background(), "agent:test", 10)
	if err != nil {
		t.Fatal(err)
	}
	var sawRefusal bool
	for _, r := range rows {
		if r.Outcome == "refusal" {
			sawRefusal = true
		}
	}
	if !sawRefusal {
		t.Errorf("the replayed denial of the zero-policy step wrote no policy_event refusal row: %+v\ndaemon log:\n%s",
			rows, w.logLines())
	}
	// The model was answered with the ENGINE's default-deny sentence — the
	// one formatter, read off the formatter itself (D9), not invented prose.
	wantDeny := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "test", Tool: "bash"}, base).Sentence()
	var sawDenial bool
	for _, m := range s.ReadTranscript(id).Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "u1" {
				if !b.IsError || b.Content != wantDeny {
					t.Errorf("u1 was answered {IsError:%v Content:%q}, want the default-deny sentence %q",
						b.IsError, b.Content, wantDeny)
				}
				sawDenial = true
			}
		}
	}
	if !sawDenial {
		t.Error("u1 was never answered with a tool_result — the zero-policy replay did not route through a gate")
	}

	// The counterfactual that keeps the assertion above load-bearing: the
	// SAME step shape assembled with NO policy source at all (markerless,
	// genuinely pre-S10) keeps the live fallback — bash runs when the store
	// allows it. Without this, "still refused" could mean "nothing ever
	// runs".
	s2 := recoveryStoreBound(t)
	root2 := t.TempDir()
	// The governing allow for a command-carrying block is a command rule
	// (S11's ONE rule): the live store's command allow must actually run
	// the bash for "still refused" above to mean "frozen", not "dead".
	storeCommandPolicy(t, s2, "yes-bash-live", "touch*", policy.Allow, base, nil)
	idc, mc := seedStepWithRunner(t, s2, root2, StepRunner{}, runtime.ContextDeps{})
	if mc.PoliciesFrozen {
		t.Errorf("the pre-S10 control claimed a consultation it never made (step %d) — the marker would "+
			"silence the live fallback and the assertion below would be a frozen-empty denial, not a grant", idc)
	}
	d2, w2 := recoveryDaemon(s2)
	if sum, err := d2.Recover(context.Background(), false); err != nil || sum.Recovered != 1 {
		t.Fatalf("control summary=%+v err=%v\n%s", sum, err, w2.logLines())
	}
	if !markerExists(root2) {
		t.Fatal("the pre-S10 control never executed bash — the marker assertion above certifies nothing")
	}
}

func TestRecoverStepForwardsTheGateWithAttribution(t *testing.T) {
	// DEF-034's recover site, behaviourally: swap the table's live gate for
	// a spy BEFORE recovery and prove the production recover.go forwarded a
	// NON-NIL gate — and that the runtime filled the trusted attribution
	// fields (the row's step id, ModelRequested) on the question it asked.
	s := recoveryStoreBound(t)
	root := t.TempDir()
	id, _ := seedFrozenRecoveryStep(t, s, root, nil)
	// Live store empty ⇒ the forwarded gate must be consulted before ANY
	// PL5 denial; if recover.go dropped the forward (nil gate), the spy
	// stays silent and bash runs.
	d, _ := recoveryDaemon(s)
	spy := &spyForwardGate{}
	d.Table.PolicyGate = spy
	if sum, err := d.Recover(context.Background(), false); err != nil || sum.Recovered != 1 {
		t.Fatalf("summary=%+v err=%v", sum, err)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.saw) == 0 {
		t.Fatal("RecoverStep's RunOpts carried NO gate (or a nil one) — the recover site dropped the " +
			"forward, and with nil the runtime dispatches unenforced")
	}
	got := spy.saw[0]
	if got.StepID != id {
		t.Errorf("the gate was asked about a call attributed to step %d, want %d — disagreements charge "+
			"the row that ran them", got.StepID, id)
	}
	if !got.ModelRequested {
		t.Error("the recovery re-run asked the gate with ModelRequested=false — a re-run IS a model-emitted " +
			"attempt; its refusal belongs in the disagreement telemetry")
	}
	if markerExists(root) {
		t.Error("the gate was consulted yet bash ran — the spy's decision was not obeyed")
	}
}

// ── DEF-042: the fetch guard is the nil test, not the len test ──────────────

func TestCompileFetchesOnlyWhenNoSetWasSupplied(t *testing.T) {
	// The fixture escape (DEF-042): `len(deps.Policies) == 0` read a
	// SUPPLIED empty set as "no opinion" and overrode it with a live fetch,
	// so "frozen empty, do not go look" was inexpressible and a production
	// zero-policy step's manifest was indistinguishable from pre-S10. The
	// guard is a nil test now: supplied means answered.
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	storeBashPolicy(t, s, "no-bash", policy.Deny, at)

	fetches := 0
	r := StepRunner{PolicyList: func(c context.Context, when time.Time) ([]runtime.PolicyEntry, error) {
		fetches++
		entries, rej := PolicyEntriesAt(c, s, when)
		if rej != nil {
			return nil, errors.New(rej.Reason)
		}
		return entries, nil
	}}
	root := t.TempDir()
	_, specs, err := r.BuildTools([]string{"bash"}, root, "test", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	in := runtime.CompileInput{Handler: runtime.DefaultHandler("test", []string{"bash"}),
		Granted: []string{"bash"}, Route: model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		WorkspaceRoot: root, ToolSpecs: specs}

	// A supplied empty set is an answer, not an absence: zero fetches, and
	// the manifest records "consulted, found empty".
	supplied := r.Compile(context.Background(), in, runtime.ContextDeps{Policies: []runtime.PolicyEntry{}})
	if !supplied.OK {
		t.Fatalf("compile refused: %s", supplied.Reason)
	}
	if fetches != 0 {
		t.Errorf("PolicyList was consulted %d time(s) despite an explicit empty set being supplied — the daemon "+
			"overrode frozen-empty with a live read (DEF-042)", fetches)
	}
	if !supplied.Request.Manifest.PoliciesFrozen {
		t.Error("the supplied empty set left PoliciesFrozen false — the manifest cannot claim a consultation " +
			"that AssembleContext was plainly handed")
	}
	if supplied.Request.Manifest.Policies != nil {
		t.Errorf("an empty supplied set froze to %+v; nil is the only honest empty (freezePolicyEntries' rule)",
			supplied.Request.Manifest.Policies)
	}

	// Nothing supplied ⇒ the fetch happens, once, and the set freezes with
	// its marker (protocol/step.go's production call passes ContextDeps{}).
	nothing := r.Compile(context.Background(), in, runtime.ContextDeps{})
	if !nothing.OK {
		t.Fatalf("compile refused: %s", nothing.Reason)
	}
	if fetches != 1 {
		t.Errorf("PolicyList was consulted %d time(s), want exactly 1 — nothing supplied must fetch once (D12)", fetches)
	}
	if !nothing.Request.Manifest.PoliciesFrozen || len(nothing.Request.Manifest.Policies) != 1 ||
		nothing.Request.Manifest.Policies[0].ID != "no-bash" {
		t.Errorf("the fetched set did not freeze with its marker: frozen=%v policies=%+v",
			nothing.Request.Manifest.PoliciesFrozen, nothing.Request.Manifest.Policies)
	}

	// A storeless assembly does not lie: no source, no marker — that
	// manifest keeps the live fallback because it genuinely predates any
	// consultation (the residual the fix knowingly leaves).
	zero := (StepRunner{}).Compile(context.Background(), in, runtime.ContextDeps{})
	if !zero.OK || zero.Request.Manifest.PoliciesFrozen {
		t.Errorf("the zero runner (never a policy source) produced ok=%v marker=%v; absence must not claim "+
			"a consultation it did not make", zero.OK, zero.Request.Manifest.PoliciesFrozen)
	}
}

// ── DEF-042 + DEF-079: the frozen-empty gate and the two counting halves ────

// countingAuditBridge is the production RepoFuncs bridge with counters on the
// TWO ways the live policy tables can be touched: ListAll (the live set read)
// and EventInsert (the audit write). Same instrument for both, per T20's
// non-vacuity discipline: the counter that must stay at zero and the counter
// that must move are measured together, so "no live read" and "no audit row"
// are measured zeros, never readings of the code.
func countingAuditBridge(t *testing.T, s *store.Store) (policy.RepoFuncs, *int, *int) {
	t.Helper()
	repo := newPolicyRepo(s)
	reads, writes := 0, 0
	innerList := repo.ListAll
	repo.ListAll = func(ctx context.Context) ([]policy.Row, error) {
		reads++
		return innerList(ctx)
	}
	innerInsert := repo.EventInsert
	repo.EventInsert = func(ctx context.Context, kind, subject, policyID, detail string, at time.Time) error {
		writes++
		return innerInsert(ctx, kind, subject, policyID, detail, at)
	}
	return repo, &reads, &writes
}

func TestFrozenEmptyGateDeniesWithoutTouchingTheLiveStore(t *testing.T) {
	// DEF-042's unit half and DEF-079's measured truth, side by side on the
	// same counting bridge: a READABLE frozen EMPTY set is decided over that
	// empty set by the engine itself — zero policy-table reads, the engine's
	// default-deny sentence (never invented prose, never a literal deny
	// smuggled in), and the audit STILL written — while the poisoned
	// (digest-contradicted) set touches NEITHER the table NOR the audit.
	// The live store allows bash throughout: every denial below can only
	// come from the frozen branch, and the pre-S10 case takes the read
	// counter off zero so the zeros above mean "did not read", not "the
	// instrument never counts".
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	storeBashPolicy(t, s, "yes-bash", policy.Allow, at)

	req := runtime.PolicyGateRequest{Agent: "test-handler", Tool: "bash", StepID: 3, ModelRequested: true}
	wantDeny := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "test-handler", Tool: "bash"}, at).Sentence()

	// (1) Frozen-empty ⇒ the engine's default-deny over the empty set, zero
	// live reads, and BOTH audit rows still land (a readable set audits live).
	repo, reads, writes := countingAuditBridge(t, s)
	w := &wire{}
	g := newStepPolicyGateWithBoundary(policy.NewStore(repo), w, runtime.FrozenManifest{Frozen: true})
	d := g.Decide(req, at)
	if d.Allowed {
		t.Fatal("a frozen-empty step was ALLOWED by the live store's permission — the marker did not route " +
			"the gate to the frozen branch (DEF-042)")
	}
	if d.Sentence != wantDeny {
		t.Errorf("the frozen-empty set answered with %q, want the engine's default-deny sentence %q — empty "+
			"must be Evaluate over the empty set (verdict.go, the ONE formatter), never a hand-written deny",
			d.Sentence, wantDeny)
	}
	if *reads != 0 {
		t.Errorf("the frozen-empty path read the live policy table %d time(s); a step governed by nothing "+
			"keeps that answer after a crash without asking today's table", *reads)
	}
	if *writes != 2 {
		t.Errorf("the frozen-empty denial wrote %d audit row(s), want 2 (refusal + disagreement) — a readable "+
			"frozen set audits LIVE; only the poisoned path is silent", *writes)
	}

	// (2) Counter non-vacuity + the preserved fallback: the genuinely
	// pre-S10 shape (no marker, no entries) consults the live store exactly
	// once and is answered by it — the store ALLOWS, so the gate must allow.
	repo2, reads2, _ := countingAuditBridge(t, s)
	g2 := newStepPolicyGateWithBoundary(policy.NewStore(repo2), &wire{}, runtime.FrozenManifest{})
	if d2 := g2.Decide(req, at); !d2.Allowed {
		t.Error("the pre-S10 fallback no longer lets the LIVE store answer (the store allows bash) — the " +
			"live path broke while fixing the frozen one")
	}
	if *reads2 != 1 {
		t.Errorf("the pre-S10 gate read the live table %d time(s), want 1 — and it is THIS case that keeps "+
			"the zero in (1) meaningful", *reads2)
	}

	// (3) DEF-079, pinned: the poisoned gate denies from the early return
	// and touches neither the table nor the audit — the doc comment says
	// NEITHER happens; this is the measurement behind that sentence.
	pristine := []runtime.PolicyEntry{denyBashEntry("no-bash")}
	repo3, reads3, writes3 := countingAuditBridge(t, s)
	w3 := &wire{}
	g3 := newStepPolicyGateWithBoundary(policy.NewStore(repo3), w3,
		runtime.FrozenManifest{Entries: frozenTamperedAllow(), Hash: runtime.PoliciesHashOf(pristine), Frozen: true})
	if d3 := g3.Decide(req, at); d3.Allowed {
		t.Fatal("the digest-contradicted set was allowed — the poisoned gate lost its deny (DEF-039 regression)")
	}
	if *reads3 != 0 || *writes3 != 0 {
		t.Errorf("the poisoned path touched the live store %d time(s) and the audit %d time(s); the "+
			"storePolicyGate doc comment (DEF-079) says NEITHER happens — that sentence is pinned here",
			*reads3, *writes3)
	}
	if strings.Contains(w3.logLines(), "was NOT recorded") {
		t.Errorf("the poisoned path attempted an audit and logged the failure — the early return writes nothing: %s", w3.logLines())
	}
}
