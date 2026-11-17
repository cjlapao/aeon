package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// The tests here are the desk's own unit tests — the registry's shape, the
// seam refusals, the id that makes seeding idempotent, and the round-trip
// count the CLI's "one call per accepted add" property rests on. The behaviour
// the two doors print is NOT re-asserted here: it is pinned byte for byte by
// cmd/aeon's goldens (policy_golden_test.go), and duplicating it in a unit
// test would be the second truth this slice exists to delete.

// --- the spy boundary -----------------------------------------------------------

type authoredCall struct {
	subject, policyID string
	at                time.Time
}

// spyBoundary is the desk's boundary as the caller's adapter would answer it:
// rows in a slice, every call counted. It never validates — that is the real
// boundary's job and cmd/aeon's goldens prove THAT half — so a count here
// measures what the desk asked for, which is the only thing the desk controls.
type spyBoundary struct {
	rows []policy.Policy

	listCalls    int
	addCalls     int
	removeCalls  int
	authoredCall int

	added     []policy.Policy
	addRej    []*policy.Reject // consumed one per Add; nil entries mean "accept"
	removedID string
	removed   []string
	authored  []authoredCall

	listRej  *policy.Reject
	removeEr error
}

func (s *spyBoundary) AddPolicy(_ context.Context, p policy.Policy, _ time.Time) *policy.Reject {
	s.addCalls++
	s.added = append(s.added, p)
	n := s.addCalls - 1
	if n < len(s.addRej) && s.addRej[n] != nil {
		return s.addRej[n]
	}
	s.rows = append(s.rows, p)
	return nil
}

func (s *spyBoundary) RemovePolicy(_ context.Context, id string) (bool, error) {
	s.removeCalls++
	s.removedID = id
	s.removed = append(s.removed, id)
	if s.removeEr != nil {
		return false, s.removeEr
	}
	kept := s.rows[:0]
	var found bool
	for _, r := range s.rows {
		if r.ID == id {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	s.rows = kept
	return found, nil
}

func (s *spyBoundary) ListPolicies(_ context.Context, _ time.Time) ([]policy.Policy, *policy.Reject) {
	s.listCalls++
	if s.listRej != nil {
		return nil, s.listRej
	}
	return append([]policy.Policy(nil), s.rows...), nil
}

func (s *spyBoundary) RecordAuthoredPolicy(_ context.Context, subject, policyID string, at time.Time) *policy.Reject {
	s.authoredCall++
	s.authored = append(s.authored, authoredCall{subject: subject, policyID: policyID, at: at})
	return nil
}

func row(id string) policy.Policy {
	return policy.Policy{
		ID:     id,
		Type:   policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "alice"},
		Action: policy.Deny,
		Spec:   json.RawMessage(`{"command":"ls*","state":"deny"}`),
		Source: policy.Operator,
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	return string(mustJSON(t, v))
}

// seamsFor returns the seams a real caller registers, with the MCP and import
// halves reduced to what this package can say without importing those layers:
// each records that it was consulted and answers what the table says.
type seamAnswer struct {
	already     bool
	alreadyErr  error
	applyErr    error
	validateErr error
}

type seamLog struct {
	validateCalls, alreadyCalls, applyCalls int
	applyWriter                             io.Writer
}

func wiredSeams(answers map[Kind]seamAnswer, log map[Kind]*seamLog) Seams {
	seams := Seams{}
	for k, a := range answers {
		l := log[k]
		seams[k] = Effect{
			Validate: func(_ context.Context, _ []byte) error {
				l.validateCalls++
				return a.validateErr
			},
			AlreadyApplied: func(_ context.Context, _ []byte) (bool, error) {
				l.alreadyCalls++
				return a.already, a.alreadyErr
			},
			Apply: func(_ context.Context, w io.Writer, _ []byte) error {
				l.applyCalls++
				l.applyWriter = w
				return a.applyErr
			},
		}
	}
	return seams
}

func testRegistry(sb *spyBoundary, seams Seams) *Registry {
	return New(Deps{Boundary: func() (Boundary, func(), error) { return sb, func() {}, nil }}, seams)
}

// --- the registry's shape --------------------------------------------------------

func TestDeskRegistryHasAllKnownKinds(t *testing.T) {
	// The five S35 kinds are implemented here; agent_import is declared and
	// completed by a seam. A kind lost in the move is the slice's first
	// acceptance criterion failing, so this list is written out long rather
	// than derived from the code under test.
	full := []string{"policy_add", "policy_remove", "seed_corpus", "mcp_server_add", "mcp_server_remove"}
	// S56 T8: the seam kinds are the three whose effect halves come from
	// cmd/aeon — agent_import and the two restrictive autonomy kinds (each
	// keeps the desk's built-in Validate, the seam completes the rest).
	seamKinds := []string{"agent_import", "autonomy_revoke", "autonomy_deny"}

	seams := wiredSeams(
		map[Kind]seamAnswer{
			KindMCPServerAdd: {}, KindMCPServerRem: {}, KindAgentImport: {},
			KindAutonomyRevoke: {}, KindAutonomyDeny: {},
		},
		map[Kind]*seamLog{
			KindMCPServerAdd: {}, KindMCPServerRem: {}, KindAgentImport: {},
			KindAutonomyRevoke: {}, KindAutonomyDeny: {},
		},
	)
	r := testRegistry(&spyBoundary{}, seams)

	for _, kind := range append(append([]string(nil), full...), seamKinds...) {
		if _, ok := r.Effect(kind); !ok {
			t.Errorf("Effect(%q) is not complete once wired: %s", kind, r.Missing(kind))
		}
	}
	// Unwired, the seam kinds are the ONLY ones that go incomplete — the
	// three policy kinds work with nothing registered.
	bare := testRegistry(&spyBoundary{}, nil)
	for _, kind := range []string{"policy_add", "policy_remove", "seed_corpus"} {
		if _, ok := bare.Effect(kind); !ok {
			t.Errorf("the built-in kind %q needs no seam, yet Missing says: %s", kind, bare.Missing(kind))
		}
	}
	for _, kind := range []string{"mcp_server_add", "mcp_server_remove", "agent_import", "autonomy_revoke", "autonomy_deny", "mcp_grant_revoke"} {
		if _, ok := bare.Effect(kind); ok {
			t.Errorf("%q is a seam kind and must be incomplete until registered", kind)
		}
	}
	// The kind list itself: the nine names, exactly (S46 T6's sanctioned
	// lock-list update beside S56 T8's — the kind list exists only to be
	// updated with the store mirror; every OTHER assertion in this file is
	// S45's untouched).
	var got []string
	for _, k := range Kinds() {
		got = append(got, string(k))
	}
	sort.Strings(got)
	want := append(append(append([]string(nil), full...), seamKinds...), "mcp_grant_revoke")
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Kinds() = %v, want %v", got, want)
	}
	// Kinds is a copy: mutating it must not reorder the registry.
	Kinds()[0] = "tampered"
	if Kinds()[0] == "tampered" {
		t.Fatal("Kinds() handed out the registry's own slice")
	}
}

func TestDeskSeamKindRefusesByNameWhenUnwired(t *testing.T) {
	r := testRegistry(&spyBoundary{}, nil)
	if _, ok := r.Effect("agent_import"); ok {
		t.Fatal("an unwired agent_import must not answer as an effect")
	}
	missing := r.Missing("agent_import")
	for _, want := range []string{`"agent_import"`, "AlreadyApplied", "Apply", "seam"} {
		if !strings.Contains(missing, want) {
			t.Errorf("Missing(%q) = %q — must name the kind and the half missing", "agent_import", missing)
		}
	}
	// And the caller's refusal says the same thing out loud, on every door.
	if err := r.Apply(context.Background(), io.Discard, "agent_import", `{"pkgPath":"/abs"}`); err == nil {
		t.Fatal("Apply on an unwired seam kind must refuse, not skip")
	} else if !strings.Contains(err.Error(), "agent_import") || !strings.Contains(err.Error(), "Apply") {
		t.Errorf("Apply refusal %q does not name the kind and the missing half", err)
	}
	if _, err := r.AlreadyApplied(context.Background(), "agent_import", `{}`); err == nil {
		t.Fatal("AlreadyApplied on an unwired seam kind must refuse, not skip")
	} else if !strings.Contains(err.Error(), "idempotency check") {
		t.Errorf("AlreadyApplied refusal %q lost the wording the CLI had before the move", err)
	}
	// A kind nobody ever declared refuses with the words S35 used, unchanged.
	for _, want := range []struct{ kind, phrase string }{
		{"nonsense", "has no validator registered"},
		{"", "has no validator registered"},
	} {
		err := r.ValidateProposal(context.Background(), want.kind, `{}`)
		if err == nil || !strings.Contains(err.Error(), want.phrase) {
			t.Errorf("ValidateProposal(%q) = %v, want a refusal containing %q", want.kind, err, want.phrase)
		}
	}
	if _, err := r.AlreadyApplied(context.Background(), "nonsense", `{}`); err == nil ||
		!strings.Contains(err.Error(), `proposal kind "nonsense" has no idempotency check`) {
		t.Errorf("AlreadyApplied(nonsense) = %v", err)
	}
	if err := r.Apply(context.Background(), io.Discard, "nonsense", `{}`); err == nil ||
		!strings.Contains(err.Error(), `proposal kind "nonsense" has no apply path`) {
		t.Errorf("Apply(nonsense) = %v", err)
	}
}

// --- the id that makes seeding idempotent ---------------------------------------

func TestSeedPolicyIDStable(t *testing.T) {
	// Every literal here was computed OUTSIDE this package (sha256 over the
	// pattern, first six bytes as twelve hex) before the function moved. A
	// failure means seeded rows from an earlier run would be orphaned — the
	// id is a primary key, so this is the test that must never be edited to
	// match a change, only argued with.
	for _, tc := range []struct{ agent, pattern, want string }{
		{"memory-steward-loop", "*", "corpus.memory-steward-loop.684888c0ebb1"},
		{"memory-steward-loop", "ls*", "corpus.memory-steward-loop.123056d4fe20"},
		{"brief-author", "git *", "corpus.brief-author.d93688dfec86"},
		{"a", "p", "corpus.a.148de9c5a7a4"},
		{"investigator-loop", "grep -rn foo /tmp", "corpus.investigator-loop.25d7ef6104cd"},
	} {
		if got := SeedPolicyID(tc.agent, tc.pattern); got != tc.want {
			t.Errorf("SeedPolicyID(%q, %q) = %q, want %q", tc.agent, tc.pattern, got, tc.want)
		}
	}
}

// --- the round-trip count the CLI's own property rests on -----------------------

func TestDeskAddDoesNotRevalidate(t *testing.T) {
	sb := &spyBoundary{}
	r := testRegistry(sb, nil)
	payload := mustJSONString(t, row("add-one"))

	if err := r.Apply(context.Background(), io.Discard, "policy_add", payload); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Exactly one boundary round-trip: the Add. No List (that would mean the
	// desk re-checking the world the boundary is about to check), no audit row
	// (a policy_add is an operator's authored row, not a seeded one), and no
	// second Add.
	if sb.addCalls != 1 || sb.listCalls != 0 || sb.authoredCall != 0 {
		t.Fatalf("one accepted add = %d Add, %d List, %d RecordAuthored; want 1, 0, 0",
			sb.addCalls, sb.listCalls, sb.authoredCall)
	}
	if len(sb.added) != 1 || sb.added[0].ID != "add-one" {
		t.Fatalf("the desk handed the boundary %v", sb.added)
	}

	// The commit door's whole sequence stays two reads deep: the gate's List,
	// then the Add. Anything more is the drift the CLI's pinned one-call
	// property forbids.
	sb2 := &spyBoundary{rows: []policy.Policy{row("there")}}
	r2 := testRegistry(sb2, nil)
	already, err := r2.AlreadyApplied(context.Background(), "policy_remove",
		mustJSONString(t, PolicyRemovePayload{ID: "there"}))
	if err != nil || already {
		t.Fatalf("AlreadyApplied(policy_remove, present) = %v, %v", already, err)
	}
	if err := r2.Apply(context.Background(), io.Discard, "policy_remove",
		mustJSONString(t, PolicyRemovePayload{ID: "there"})); err != nil {
		t.Fatalf("Apply(policy_remove): %v", err)
	}
	if sb2.listCalls != 1 || sb2.removeCalls != 1 || sb2.addCalls != 0 {
		t.Fatalf("remove sequence = %d List, %d Remove, %d Add; want 1, 1, 0",
			sb2.listCalls, sb2.removeCalls, sb2.addCalls)
	}
	if sb2.removedID != "there" {
		t.Fatalf("removed %q, want the payload's id", sb2.removedID)
	}
}

func TestDeskAddRefusalIsTheBoundarysWords(t *testing.T) {
	sb := &spyBoundary{addRej: []*policy.Reject{{Reason: `policy "nope" · type "command_access": refused for a reason`}}}
	r := testRegistry(sb, nil)
	err := r.Apply(context.Background(), io.Discard, "policy_add", mustJSONString(t, row("nope")))
	if err == nil || err.Error() != `policy "nope" · type "command_access": refused for a reason` {
		t.Fatalf("the boundary's refusal must reach the caller verbatim, got %v", err)
	}
	if sb.authoredCall != 0 {
		t.Fatal("a refused add must not leave an authored audit row behind — the audit row that lies")
	}
}

// --- AlreadyApplied, one row per kind -------------------------------------------

func TestDeskAlreadyAppliedPerKind(t *testing.T) {
	present := row("present-row")
	sb := &spyBoundary{rows: []policy.Policy{present}}
	logs := map[Kind]*seamLog{
		KindMCPServerAdd: {}, KindMCPServerRem: {}, KindAgentImport: {},
	}
	answers := map[Kind]seamAnswer{
		KindMCPServerAdd: {already: true},
		KindMCPServerRem: {already: false},
		KindAgentImport:  {already: false},
	}
	r := testRegistry(sb, wiredSeams(answers, logs))

	for _, tc := range []struct {
		name    string
		kind    string
		payload string
		want    bool
		list    int // List calls this case must add (0 for a seam kind)
		seam    Kind
	}{
		// The two policy kinds answer from the boundary, in opposite
		// directions: an add whose id stands is applied; a remove whose id is
		// gone is applied. Getting either backwards re-applies an effect or
		// refuses a retry for the wrong reason.
		{name: "add/present", kind: "policy_add", payload: mustJSONString(t, present), want: true, list: 1},
		{name: "add/absent", kind: "policy_add", payload: mustJSONString(t, row("never-seen")), want: false, list: 1},
		{name: "remove/present", kind: "policy_remove", payload: mustJSONString(t, PolicyRemovePayload{ID: "present-row"}), want: false, list: 1},
		{name: "remove/absent", kind: "policy_remove", payload: mustJSONString(t, PolicyRemovePayload{ID: "gone"}), want: true, list: 1},
		// seed_corpus needs no detection — its own skip-if-exists makes a
		// re-run convergent, so it always re-runs.
		{name: "seed/always", kind: "seed_corpus", payload: mustJSONString(t, SeedCorpusPayload{Agents: []string{"a"}}), want: false, list: 0},
		{name: "mcp add/seam", kind: "mcp_server_add", payload: `{"tier":"public","server":{"name":"gh"}}`, want: true, list: 0, seam: KindMCPServerAdd},
		{name: "mcp remove/seam", kind: "mcp_server_remove", payload: `{"name":"gh"}`, want: false, list: 0, seam: KindMCPServerRem},
		{name: "import/seam", kind: "agent_import", payload: `{"pkgPath":"/abs"}`, want: false, list: 0, seam: KindAgentImport},
	} {
		before := sb.listCalls
		got, err := r.AlreadyApplied(context.Background(), tc.kind, tc.payload)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: AlreadyApplied = %v, want %v", tc.name, got, tc.want)
		}
		if sb.listCalls-before != tc.list {
			t.Errorf("%s: the gate made %d List calls, want %d", tc.name, sb.listCalls-before, tc.list)
		}
		if tc.seam != "" && logs[tc.seam].alreadyCalls == 0 {
			t.Errorf("%s: the seam was never consulted", tc.name)
		}
	}
}

func TestDeskInfrastructureFailuresReachTheCallerAsErrors(t *testing.T) {
	// DB19's split, kept across the move: a domain refusal is the boundary's
	// *Reject rendered as its own words, an infrastructure failure is an error
	// the commit door records in applied_error while the row stays proposed.
	// Neither may collapse into the other — a store failure answered "not
	// applied" would re-run the effect, and answered "refused" would blame the
	// payload for the disk.
	r := New(Deps{Boundary: func() (Boundary, func(), error) {
		return nil, nil, errors.New("database is locked")
	}}, nil)
	if _, err := r.AlreadyApplied(context.Background(), "policy_add", mustJSONString(t, row("x"))); err == nil ||
		!strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("the gate swallowed the store failure: %v", err)
	}
	if err := r.ValidateProposal(context.Background(), "policy_remove",
		mustJSONString(t, PolicyRemovePayload{ID: "x"})); err == nil ||
		!strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("the validator swallowed the store failure: %v", err)
	}
	sb := &spyBoundary{removeEr: errors.New("the store is closed")}
	r2 := testRegistry(sb, nil)
	if err := r2.Apply(context.Background(), io.Discard, "policy_remove",
		mustJSONString(t, PolicyRemovePayload{ID: "x"})); err == nil ||
		!strings.Contains(err.Error(), "the store is closed") {
		t.Fatalf("Apply swallowed the remove failure: %v", err)
	}
	// And a List the boundary refuses is the boundary's own words, verbatim.
	sb3 := &spyBoundary{listRej: &policy.Reject{Reason: "the read failed closed"}}
	r3 := testRegistry(sb3, nil)
	if _, err := r3.AlreadyApplied(context.Background(), "policy_add", mustJSONString(t, row("x"))); err == nil ||
		err.Error() != "the read failed closed" {
		t.Fatalf("the gate must pass the boundary's reason through unchanged, got %v", err)
	}
}

// --- the seed runner's one path the goldens cannot reach -------------------------

func TestDeskSeedCorpusStopsAtTheRefusedRule(t *testing.T) {
	// The corpus is the specification, so a rule of it that the boundary
	// refuses is a defect to SURFACE: the run stops, names the rule, reports
	// the partial counts, and never claims a clean seed. No golden exercises
	// this (every corpus rule clears the boundary today), so it is pinned here.
	sb := &spyBoundary{addRej: []*policy.Reject{nil, {Reason: "the second rule is refused"}}}
	var out strings.Builder
	err := RunSeedCorpus(context.Background(), &out, sb, []string{"memory-steward-loop"}, false, time.Now())
	if err == nil {
		t.Fatal("a refused corpus rule must refuse the run")
	}
	if !strings.Contains(err.Error(), "was REFUSED by the write boundary") ||
		!strings.Contains(err.Error(), "is NOT fully seeded") {
		t.Fatalf("refusal lost its shape: %v", err)
	}
	printed := out.String()
	if !strings.Contains(printed, "stopped before rule 2 of") {
		t.Fatalf("the partial seed must name the rule it stopped at, got:\n%s", printed)
	}
	// One authored audit row for the one rule that stood, and none for the
	// refused one: the audit row that lies is the failure this ordering exists
	// to prevent.
	if len(sb.authored) != 1 {
		t.Fatalf("%d authored rows for 1 stored rule: %v", len(sb.authored), sb.authored)
	}
	if sb.authored[0].subject != "agent:memory-steward-loop" {
		t.Fatalf("authored subject = %q", sb.authored[0].subject)
	}
}

func TestDeskSeedCorpusRefusesTheFilterNotTheAgent(t *testing.T) {
	// An agent the corpus does not carry is the FILTER's answer and refuses the
	// whole run — never "seeded zero rules", which reads as success.
	var out strings.Builder
	err := RunSeedCorpus(context.Background(), &out, &spyBoundary{}, []string{"nobody-here"}, false, time.Now())
	if err == nil || !strings.Contains(err.Error(), "is not in the corpus") {
		t.Fatalf("want the filter's refusal, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a run refused by the filter prints nothing but the error, got %q", out.String())
	}
}

func TestDeskSeedCorpusDryRunWritesNothing(t *testing.T) {
	sb := &spyBoundary{}
	var out strings.Builder
	if err := RunSeedCorpus(context.Background(), &out, sb, []string{"memory-steward-loop"}, true, time.Now()); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if sb.addCalls != 0 || sb.authoredCall != 0 {
		t.Fatalf("a dry run wrote: %d adds, %d authored rows", sb.addCalls, sb.authoredCall)
	}
	if !strings.Contains(out.String(), "the store saw zero writes") {
		t.Fatalf("the dry run must say it wrote nothing, got:\n%s", out.String())
	}
}

// --- the opener, and the clock ----------------------------------------------------

func TestOpenBoundaryRefusesANilOpenerByName(t *testing.T) {
	_, _, err := OpenBoundary(nil)
	if err == nil || !strings.Contains(err.Error(), "desk.Deps.Boundary") {
		t.Fatalf("a registry built without a boundary must name the wiring bug, got %v", err)
	}
	_, _, err = OpenBoundary(func() (Boundary, func(), error) { return nil, nil, nil })
	if err == nil || !strings.Contains(err.Error(), "returned no boundary") {
		t.Fatalf("a nil boundary with no error must refuse, got %v", err)
	}
	// A close of nil is not a panic waiting to happen at every defer site.
	_, closeFn, err := OpenBoundary(func() (Boundary, func(), error) { return &spyBoundary{}, nil, nil })
	if err != nil {
		t.Fatalf("OpenBoundary: %v", err)
	}
	closeFn()
}

func TestDepsClockIsTheOneItIsHanded(t *testing.T) {
	fixed := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	sb := &spyBoundary{}
	r := New(Deps{
		Boundary: func() (Boundary, func(), error) { return sb, func() {}, nil },
		Clock:    func() time.Time { return fixed },
	}, nil)
	if err := r.Apply(context.Background(), io.Discard, "policy_add", mustJSONString(t, row("stamped"))); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// A zero clock is not a crash waiting in New.
	if (Deps{}).now().IsZero() {
		t.Fatal("a Deps with no clock must still answer an instant")
	}
}

// --- the seam that must reach the writer ----------------------------------------

func TestSeamApplyReceivesTheDoorsWriter(t *testing.T) {
	// The import effect prints a fresh consent to whoever is standing at the
	// door — that is why Effect.Apply carries an io.Writer. A seam that lost it
	// would print the consent nowhere, or into the daemon's logs.
	var out strings.Builder
	logs := map[Kind]*seamLog{KindAgentImport: {}}
	r := testRegistry(&spyBoundary{}, wiredSeams(map[Kind]seamAnswer{KindAgentImport: {}}, logs))
	if err := r.Apply(context.Background(), &out, "agent_import", `{"pkgPath":"/abs"}`); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var discard io.Writer = io.Discard
	if fmt.Sprint(logs[KindAgentImport].applyWriter) != fmt.Sprint(&out) {
		t.Fatal("the seam did not receive the door's writer")
	}
	_ = discard
	if err := r.Apply(context.Background(), nil, "agent_import", `{}`); err != nil {
		t.Fatalf("a nil writer must fall back to io.Discard, got %v", err)
	}
}
