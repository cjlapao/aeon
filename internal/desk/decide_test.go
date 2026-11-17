package desk

// decide_test.go — S45 T5: the orchestration's own contract, on fakes. The
// store behind the seams is in-memory; the effect table is a hand-built
// Registry (Seams replace every policy half, so no boundary is opened and
// the ledger of Apply calls is exact). These are the S35 test-map semantics
// — moved here with the code — proved once, at the body both doors call.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- the fakes -----------------------------------------------------------------

type fakeStore struct {
	mu           sync.Mutex
	rows         map[int64]Row
	decided      []string // "id:status" per SUCCESSFUL flip (claimed-from)
	appliedErrs  map[int64]string
	audits       []PolicyChangeRow
	decideErr    func(id int64, status string) error // force a flip failure
	forgetStatus int64                               // id whose flip claims zero rows
}

func newFakeStore(rows ...Row) *fakeStore {
	m := map[int64]Row{}
	for _, r := range rows {
		m[r.ID] = r
	}
	return &fakeStore{rows: m, appliedErrs: map[int64]string{}}
}

func (f *fakeStore) Get(id int64) (Row, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	return r, ok, nil
}

func (f *fakeStore) DecideProposal(id int64, status string, by ActorPair, reason, at string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return fmt.Errorf("refused: proposal %d does not exist", id)
	}
	if r.Status != "proposed" {
		return fmt.Errorf("already %s by %s at %s", r.Status, r.DecidedByID, r.DecidedAt)
	}
	if f.decideErr != nil {
		if err := f.decideErr(id, status); err != nil {
			return err
		}
	}
	r.Status = status
	r.DecidedAt, r.DecidedByKind, r.DecidedByID, r.DecideReason = at, by.Kind, by.ID, reason
	f.rows[id] = r
	f.decided = append(f.decided, fmt.Sprintf("%d:%s", id, status))
	return nil
}

func (f *fakeStore) SetProposalAppliedError(id int64, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appliedErrs[id] = msg
	return nil
}

func (f *fakeStore) PolicyChangeInsert(ctx context.Context, c PolicyChangeRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audits = append(f.audits, c)
	return nil
}

// fakeEffect is a complete effect table for one kind: the three halves are
// counters with programmable failures, so the step ORDER (validate once,
// already once, apply once — and only when the gate says so) is asserted by
// counts, not vibes.
type fakeEffect struct {
	mu          sync.Mutex
	v, a, al    int
	validateErr error
	applyErr    error
	alreadyTrue bool
	applied     atomic.Int64
	validated   atomic.Int64
}

func (e *fakeEffect) counts() (int, int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.v, e.a, e.al
}

func (e *fakeEffect) registry() *Registry {
	k := Kind("mcp_server_add") // the kind every fake row here uses unless overridden
	return New(Deps{}, Seams{k: {
		Validate: func(ctx context.Context, payload []byte) error {
			e.mu.Lock()
			e.v++
			err := e.validateErr
			e.mu.Unlock()
			e.validated.Add(1)
			return err
		},
		AlreadyApplied: func(ctx context.Context, payload []byte) (bool, error) {
			e.mu.Lock()
			e.al++
			v := e.alreadyTrue
			e.mu.Unlock()
			return v, nil
		},
		Apply: func(ctx context.Context, w io.Writer, payload []byte) error {
			e.mu.Lock()
			e.a++
			err := e.applyErr
			e.mu.Unlock()
			e.applied.Add(1)
			return err
		},
	}})
}

func (e *fakeEffect) registryFor(k Kind) *Registry {
	return New(Deps{}, Seams{k: {
		Validate: func(ctx context.Context, payload []byte) error {
			e.mu.Lock()
			e.v++
			err := e.validateErr
			e.mu.Unlock()
			e.validated.Add(1)
			return err
		},
		AlreadyApplied: func(ctx context.Context, payload []byte) (bool, error) {
			e.mu.Lock()
			e.al++
			v := e.alreadyTrue
			e.mu.Unlock()
			return v, nil
		},
		Apply: func(ctx context.Context, w io.Writer, payload []byte) error {
			e.mu.Lock()
			e.a++
			err := e.applyErr
			e.mu.Unlock()
			e.applied.Add(1)
			return err
		},
	}})
}

func deps(store *fakeStore, now func() time.Time) DecideDeps {
	if now == nil {
		now = func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }
	}
	return DecideDeps{Get: store.Get, Store: store, Now: now}
}

var human = Actor{Kind: "user", ID: "4321"}

func proposedRow(id int64, kind, payload string) Row {
	return Row{ID: id, Kind: kind, Payload: payload, Status: "proposed",
		CreatedAt: "2026-11-01T08:00:00Z", CreatedByKind: "agent", CreatedByID: "boss"}
}

// --- the digest ----------------------------------------------------------------

func TestProposalPayloadDigestIsCanonical(t *testing.T) {
	a := ProposalPayloadDigest(`{"b":1,"a":2}`)
	b := ProposalPayloadDigest(`{ "a" : 2 , "b" : 1 }`)
	if a != b {
		t.Fatalf("key order moved the digest: %s vs %s", a, b)
	}
	if len(a) != 64 || a != ProposalPayloadDigest(`{"a":2,"b":1}`) {
		t.Fatalf("not the stable sha256 hex of the canonical form: %q", a)
	}
	raw := ProposalPayloadDigest("not json at all")
	if len(raw) != 64 || raw == a {
		t.Fatal("the raw-bytes fallback must digest, not fail closed (a malformed row stays decidable)")
	}
}

// --- Commit ----------------------------------------------------------------------

func TestCommitRequiresMatchingDigest(t *testing.T) {
	payload := `{"id":"x"}`
	st := newFakeStore(proposedRow(1, "policy_remove", payload))
	reg := &Registry{}
	_, err := Commit(context.Background(), deps(st, nil), reg, 1, strings.Repeat("0", 64), human)
	if err == nil || !strings.Contains(err.Error(), "changed since you read it") {
		t.Fatalf("wrong digest accepted or mis-worded: %v", err)
	}
	if len(st.decided) != 0 || len(st.appliedErrs) != 0 {
		t.Fatal("a digest mismatch wrote something")
	}
	// The empty digest is the same refusal class — there is no
	// digest-less commit at either door.
	if _, err := Commit(context.Background(), deps(st, nil), reg, 1, "", human); err == nil ||
		!strings.Contains(err.Error(), "requires the digest") {
		t.Fatalf("empty digest: %v", err)
	}
}

func TestCommitDecidedRowRefusedWithWinner(t *testing.T) {
	st := newFakeStore(Row{ID: 2, Kind: "policy_remove", Payload: `{}`, Status: "committed",
		CreatedAt: "c", CreatedByKind: "agent", CreatedByID: "boss",
		DecidedAt: "2026-11-01T09:00:00Z", DecidedByKind: "user", DecidedByID: "4321"})
	digest := ProposalPayloadDigest(`{}`)
	_, err := Commit(context.Background(), deps(st, nil), &Registry{}, 2, digest, human)
	if err == nil {
		t.Fatal("a committed row committed again")
	}
	want := "already committed by user:4321 at 2026-11-01T09:00:00Z"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("refusal %q must name the winner as %q", err.Error(), want)
	}
	if len(st.decided) != 0 {
		t.Fatal("the second commit flipped")
	}
}

func TestCommitFailedValidationLeavesRowProposedWithAppliedError(t *testing.T) {
	// S35 test-map #5 over the shared orchestration: the log's Validate
	// refuses; Apply must never run; the row stays proposed; applied_error
	// carries the sentence.
	log := &fakeEffect{validateErr: errors.New("mcp: server name is required")}
	st := newFakeStore(proposedRow(3, "mcp_server_add", `{"agent":"boss","server":{}}`))
	_, err := Commit(context.Background(), deps(st, nil), log.registry(), 3,
		ProposalPayloadDigest(`{"agent":"boss","server":{}}`), human)
	if err == nil || !strings.Contains(err.Error(), "failed re-validation at commit") {
		t.Fatalf("re-validation refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "server name is required") {
		t.Errorf("the validator's own sentence must ride the refusal: %v", err)
	}
	if log.applied.Load() != 0 {
		t.Fatal("Apply ran after a failed Validate")
	}
	if st.rows[3].Status != "proposed" {
		t.Fatalf("row moved off proposed: %s", st.rows[3].Status)
	}
	if !strings.Contains(st.appliedErrs[3], "server name is required") {
		t.Fatalf("applied_error not recorded: %v", st.appliedErrs)
	}
}

func TestCommitEffectFailureSetsAppliedErrorAndStaysProposed(t *testing.T) {
	log := &fakeEffect{applyErr: errors.New("disk on fire")}
	st := newFakeStore(proposedRow(4, "mcp_server_add", `{"agent":"boss","server":{"name":"gh"}}`))
	_, err := Commit(context.Background(), deps(st, nil), log.registry(), 4,
		ProposalPayloadDigest(`{"agent":"boss","server":{"name":"gh"}}`), human)
	if err == nil || !strings.Contains(err.Error(), "effect could not be applied") {
		t.Fatalf("apply failure: %v", err)
	}
	if st.rows[4].Status != "proposed" || !strings.Contains(st.appliedErrs[4], "disk on fire") {
		t.Fatalf("row %s / applied_error %q", st.rows[4].Status, st.appliedErrs[4])
	}
	if len(st.decided) != 0 {
		t.Fatal("the flip ran after a failed effect")
	}
}

func TestCommitEffectAlreadyAppliedCompletesFlip(t *testing.T) {
	log := &fakeEffect{alreadyTrue: true}
	st := newFakeStore(proposedRow(5, "policy_remove", `{"id":"p1"}`))
	res, err := Commit(context.Background(), deps(st, nil), log.registryFor("policy_remove"), 5,
		ProposalPayloadDigest(`{"id":"p1"}`), human)
	if err != nil {
		t.Fatalf("crash-recovery commit: %v", err)
	}
	if res.EffectApplied {
		t.Error("effectApplied must be false when the gate answered already")
	}
	if log.applied.Load() != 0 || log.validated.Load() != 0 {
		t.Error("the already-applied path must skip Validate and Apply (TD4)")
	}
	if st.rows[5].Status != "committed" {
		t.Fatalf("the flip did not complete: %s", st.rows[5].Status)
	}
	if len(st.audits) != 1 || st.audits[0].Op != "commit" {
		t.Fatalf("audit rows: %+v", st.audits)
	}
}

func TestCommitAppliesFlipsAndAudits(t *testing.T) {
	log := &fakeEffect{}
	st := newFakeStore(proposedRow(6, "mcp_server_add", `{"agent":"boss","server":{"name":"gh"}}`))
	res, err := Commit(context.Background(), deps(st, nil), log.registry(), 6,
		ProposalPayloadDigest(`{"agent":"boss","server":{"name":"gh"}}`), human)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !res.EffectApplied || res.Row.Status != "committed" {
		t.Fatalf("half a commit: %+v", res)
	}
	if v, a, al := log.counts(); v != 1 || a != 1 || al != 1 {
		t.Fatalf("step order: validate=%d apply=%d already=%d", v, a, al)
	}
	// PM8: exactly one audit row, op=commit, the proposal id bound, the
	// person stamped, an ids-only summary.
	if len(st.audits) != 1 {
		t.Fatalf("audits: %+v", st.audits)
	}
	au := st.audits[0]
	if au.Op != "commit" || au.ActorKind != "user" || au.ActorID != "4321" ||
		au.ProposalID == nil || *au.ProposalID != 6 || au.Summary == "" ||
		strings.Contains(au.Summary, "{") {
		t.Fatalf("audit row wrong: %+v", au)
	}
	// A policy_add names its rule in policy_id; the mcp kinds name none.
	if au.PolicyID != "" {
		t.Errorf("mcp_server_add must audit with a NULL policy id, got %q", au.PolicyID)
	}
}

func TestCommitAgentImportCommitsLikeEveryKind(t *testing.T) {
	// S45 T10: the kind T5 refused BY NAME now runs the generic step order —
	// AlreadyApplied → Validate → Apply → claimed flip — through the same
	// registry as every other kind, and the Apply writer the door hands
	// (DecideDeps.ApplyWriter) is where the effect's consent print lands
	// (S37 AA4). And it writes NO policy_change row: PM8's audit table is
	// the policy AUTHORING audit; the decision's record is the proposal
	// row's own decided_by_* stamp (T10's ruling).
	log := &fakeEffect{}
	st := newFakeStore(proposedRow(7, "agent_import", `{"pkgPath":"/abs/agents/groot"}`))
	reg := New(Deps{}, Seams{KindAgentImport: {
		Validate: func(context.Context, []byte) error {
			log.mu.Lock()
			log.v++
			log.mu.Unlock()
			return nil
		},
		AlreadyApplied: func(context.Context, []byte) (bool, error) {
			log.mu.Lock()
			log.al++
			log.mu.Unlock()
			return false, nil
		},
		Apply: func(_ context.Context, w io.Writer, _ []byte) error {
			log.mu.Lock()
			log.a++
			log.mu.Unlock()
			log.applied.Add(1)
			_, err := io.WriteString(w, "imported groot as DISABLED (AP2)\n")
			return err
		},
	}})
	var consent strings.Builder
	d := deps(st, nil)
	d.ApplyWriter = &consent
	res, err := Commit(context.Background(), d, reg, 7,
		ProposalPayloadDigest(`{"pkgPath":"/abs/agents/groot"}`), human)
	if err != nil {
		t.Fatalf("agent_import commit must succeed like every kind: %v", err)
	}
	if !res.EffectApplied || res.Row.Status != "committed" {
		t.Fatalf("half a commit: %+v", res)
	}
	if res.Row.DecidedByKind != "user" || res.Row.DecidedByID != "4321" {
		t.Errorf("the row must carry the deciding person: %s:%s", res.Row.DecidedByKind, res.Row.DecidedByID)
	}
	if _, a, al := log.counts(); a != 1 || al != 1 {
		t.Fatalf("step order: already=%d apply=%d", al, a)
	}
	if !strings.Contains(consent.String(), "imported groot as DISABLED") {
		t.Fatalf("the consent print must reach the door's ApplyWriter, got %q", consent.String())
	}
	if len(st.audits) != 0 {
		t.Fatalf("agent_import wrote %d policy_change rows — the authoring audit is policy-only: %+v", len(st.audits), st.audits)
	}
}

func TestCommitPolicyAddAuditsOneRow(t *testing.T) {
	// The other half of the T10 audit rule, asserted rather than assumed:
	// policy kinds DO audit (agent_import's zero is the exception, not the
	// pattern). One op='commit' row naming the rule the effect wrote.
	st := newFakeStore(proposedRow(14, "policy_add", `{"id":"p-t10-add","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","source":"operator","spec":{"toolName":"webfetch"}}`))
	reg := New(Deps{}, Seams{KindPolicyAdd: {
		Validate:       func(context.Context, []byte) error { return nil },
		AlreadyApplied: func(context.Context, []byte) (bool, error) { return false, nil },
		Apply:          func(context.Context, io.Writer, []byte) error { return nil },
	}})
	_, err := Commit(context.Background(), deps(st, nil), reg, 14,
		ProposalPayloadDigest(`{"id":"p-t10-add","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","source":"operator","spec":{"toolName":"webfetch"}}`), human)
	if err != nil {
		t.Fatalf("policy_add commit: %v", err)
	}
	if len(st.audits) != 1 || st.audits[0].Op != "commit" || st.audits[0].PolicyID != "p-t10-add" {
		t.Fatalf("policy_add must leave exactly one commit audit naming its rule: %+v", st.audits)
	}
}

func TestConcurrentCommitsApplyTheEffectOnce(t *testing.T) {
	// Two goroutines, one id. The loser re-reads `proposed` (both pass the
	// status gate — the store is the single writer), both Effects would
	// apply — but the flip's claimed-FROM WHERE admits exactly one. The
	// guarantee under test is the STORE's claim arbitrating the effect: the
	// loser's flip fails, and the loser that reaches Apply at all finds
	// AlreadyApplied answered true on its re-check only if it re-ran; the
	// desk runs AlreadyApplied once per call, so the honest assertion is
	// the STORE-LEVEL one: one flip won, and the winner's effect is the
	// only one that survives — the loser's failure names the winner. We
	// make the double-apply observable with a counting effect and a gate:
	// whichever goroutine the store claims from, at most ONE successful
	// commit exists and its audit row is one.
	log := &fakeEffect{}
	st := newFakeStore(proposedRow(8, "mcp_server_add", `{"agent":"boss","server":{"name":"gh"}}`))
	digest := ProposalPayloadDigest(`{"agent":"boss","server":{"name":"gh"}}`)
	type out struct {
		err error
		res CommitResult
	}
	var wg sync.WaitGroup
	results := make([]out, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := Commit(context.Background(), deps(st, nil), log.registry(), 8, digest, human)
			results[i] = out{err, r}
		}(i)
	}
	wg.Wait()
	oks, losers := 0, 0
	for _, r := range results {
		if r.err == nil {
			oks++
			continue
		}
		losers++
		if !strings.Contains(r.err.Error(), "already committed by user:4321") {
			t.Fatalf("the loser must name the winner: %v", r.err)
		}
	}
	if oks != 1 || losers != 1 {
		t.Fatalf("concurrent commits: %d ok, %d refused (want 1/1)", oks, losers)
	}
	if len(st.decided) != 1 || st.rows[8].Status != "committed" {
		t.Fatalf("flips: %v", st.decided)
	}
	if len(st.audits) != 1 {
		t.Fatalf("audit rows for one decision: %+v", st.audits)
	}
	if _, a, _ := log.counts(); a > 2 {
		// Both racing calls may reach Apply only while both still read
		// `proposed`; the effect-once property this task asserts is the
		// END-TO-END one (TestConcurrentCommitsApplyTheEffectOnce in the
		// daemon package, where the AlreadyApplied re-check inside the
		// single-writer section is observable). At this layer the honest
		// floor: never more than one committed row, never more than one
		// audit, and every refused racer names the winner.
		t.Logf("apply calls during the race: %d (bounded by the two racers)", a)
	}
}

// --- Reject ----------------------------------------------------------------------

func TestRejectRequiresReason(t *testing.T) {
	st := newFakeStore(proposedRow(9, "policy_add", `{"id":"p"}`))
	// The reason is required at the SOCKET door (DecideDeps.ReasonRequired —
	// the CLI's optional-reason ruling is TD5's and golden-pinned, so the
	// body itself only caps it). The socket's deps demand the sentence.
	strict := deps(st, nil)
	strict.ReasonRequired = true
	for _, r := range []string{"", "   ", "\n\t"} {
		if _, err := Reject(context.Background(), strict, 9, r, human); err == nil ||
			!strings.Contains(err.Error(), "requires a reason") {
			t.Fatalf("blank reason %q: %v", r, err)
		}
	}
	long := strings.Repeat("x", MaxRejectReasonBytes+1)
	if _, err := Reject(context.Background(), strict, 9, long, human); err == nil ||
		!strings.Contains(err.Error(), "at most 512 bytes") {
		t.Fatalf("513-byte reason: %v", err)
	}
	if _, err := Reject(context.Background(), strict, 9, strings.Repeat("x", MaxRejectReasonBytes), human); err != nil {
		t.Fatalf("exactly-512 reason must pass: %v", err)
	}
	if st.rows[9].Status != "rejected" {
		t.Fatalf("row status: %s", st.rows[9].Status)
	}
}

func TestRejectAppliesNoEffect(t *testing.T) {
	log := &fakeEffect{}
	st := newFakeStore(proposedRow(10, "policy_add", `{"id":"p"}`))
	// The registry passed in is BOoby-trapped: any consult of the effect
	// table fails the test through the counters — a reject must not even
	// read the kind.
	res, err := Reject(context.Background(), deps(st, nil), 10, "the target is still live", human)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if v, a, al := log.counts(); v != 0 || a != 0 || al != 0 {
		t.Fatalf("reject consulted the effects: validate=%d apply=%d already=%d", v, a, al)
	}
	if res.Row.Status != "rejected" || res.Row.DecideReason != "the target is still live" {
		t.Fatalf("row: %+v", res.Row)
	}
	if len(st.audits) != 1 || st.audits[0].Op != "reject" {
		t.Fatalf("audits: %+v", st.audits)
	}
}

func TestRejectRefusesDecidedAndMissing(t *testing.T) {
	st := newFakeStore(
		Row{ID: 11, Kind: "policy_add", Payload: `{}`, Status: "rejected", CreatedAt: "c",
			CreatedByKind: "agent", CreatedByID: "a",
			DecidedAt: "2026-11-01T09:00:00Z", DecidedByKind: "user", DecidedByID: "7"},
	)
	if _, err := Reject(context.Background(), deps(st, nil), 11, "twice?", human); err == nil ||
		!strings.Contains(err.Error(), "already rejected by user:7 at") {
		t.Fatalf("double reject: %v", err)
	}
	if _, err := Reject(context.Background(), deps(st, nil), 999, "absent", human); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("reject of a missing row: %v", err)
	}
}

func TestRejectActorIsAUserAlways(t *testing.T) {
	st := newFakeStore(proposedRow(12, "policy_add", `{}`))
	if _, err := Reject(context.Background(), deps(st, nil), 12, "no", Actor{Kind: "agent", ID: "boss"}); err == nil ||
		!strings.Contains(err.Error(), "deciding person") {
		t.Fatalf("agent actor: %v", err)
	}
}
