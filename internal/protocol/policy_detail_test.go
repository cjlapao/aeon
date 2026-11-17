package protocol

// The detail reads on the wire (S45 T2, PM5/PM6/PM11/D9): policy.get's
// whole row with its raw spec and two informational records; policy.list's
// filters — parsed at this boundary, applied in the ONE place inside
// internal/policy; the two empties answering with two different sentences;
// policy.vocabulary answering without needing a store at all; and the whole
// family still writing nothing (PL11's rule, re-proved across the new
// doors) while staying live off the same handle the gate decides with
// (PM11: no cache may creep between the CLI's write and the socket's read).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

var detailBoss = policy.Target{Kind: policy.AgentKind, ID: "boss"}

const detailToolSpec = `{"toolName":"bash","state":"allow"}`

// --- policy.get ----------------------------------------------------------------

// TestPolicyGetReturnsSpecAndDescribe: the whole row — the identity view,
// the raw spec untouched (here and only here), the engine's own sentence
// verbatim (D9 pin: the wire value must equal a DIRECT call to
// policy.Describe, so the protocol can never be assembling it), the row's
// own createdAt bytes, and the two informational records.
func TestPolicyGetReturnsSpecAndDescribe(t *testing.T) {
	tbl, store, fx := policyTable(t)
	addPolicyAt(t, store, "grant-bash", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "")
	if rej := store.RecordAcceptance(context.Background(), "grant-bash", detailBoss, "ops", time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)); rej != nil {
		t.Fatalf("seeding acceptance: %s", rej.Reason)
	}
	if rej := store.RecordAuthored(context.Background(), "agent:boss", "grant-bash", time.Date(2026, 10, 1, 8, 0, 1, 0, time.UTC)); rej != nil {
		t.Fatalf("seeding authored audit row: %s", rej.Reason)
	}
	fx.spy.reset()

	res := dispatchPolicy[PolicyGetResult](t, tbl, MethodPolicyGet, PolicyGetParams{ID: "grant-bash"})
	d := res.Policy
	if d.ID != "grant-bash" || d.Type != "tool_access" || d.Target != "agent:boss" ||
		d.Action != "allow" || d.Source != "operator" || d.NotAfter != nil {
		t.Errorf("identity view = %+v, want the stored rule rendered", d)
	}
	// The raw spec, byte for byte — the same bytes the write boundary
	// validated (T4's edit round-trips them; anything prettified here
	// would break that).
	if string(d.Spec) != detailToolSpec {
		t.Errorf("spec = %s, want the seeded bytes %s", d.Spec, detailToolSpec)
	}
	// D9's pin: the wire's describe EQUALS a direct call to the ONE
	// formatter. This is the line that makes "the protocol assembled its
	// own sentence" a build-time-impossible drift caught on the run.
	wantSentence := `allows tool "bash" for agent "boss" — authored by operator`
	if d.Describe != wantSentence {
		t.Errorf("describe = %q, want %q", d.Describe, wantSentence)
	}
	if live := policy.Describe(mustListedPolicy(t, store, "grant-bash")); d.Describe != live {
		t.Errorf("the wire drifted from the engine's own mouth: wire %q, engine %q", d.Describe, live)
	}
	// createdAt is the row's written-at instant rendered, byte for byte.
	if d.CreatedAt == "" || d.CreatedAt != fx.rows["grant-bash"].CreatedAt {
		t.Errorf("createdAt = %q, want the row's own %q", d.CreatedAt, fx.rows["grant-bash"].CreatedAt)
	}
	// The two informational records (PL10: nothing enforcement-side
	// consults them; the read side may say who was told and whether the
	// audit carries the authoring row).
	if d.AcceptedBy != "ops" || d.AcceptedAt == "" {
		t.Errorf("acceptance record = (%q, %q), want the seeded (ops, <instant>)", d.AcceptedBy, d.AcceptedAt)
	}
	if !d.Authored {
		t.Error("authored = false beside a stored RecordAuthored row — the detail lost the audit it was promised")
	}
	if *fx.spy != (policySpy{}) {
		t.Errorf("a detail read wrote to the boundary: %+v", *fx.spy)
	}
}

// TestPolicyGetUnknownNamesNearest: the typed refusal (-31840) names the id
// it could not find AND up to three nearest ids — a typo turns into a pick,
// not a re-guess. Never an empty success, never -31400 (the read answered
// cleanly; this is absence, not refusal).
func TestPolicyGetUnknownNamesNearest(t *testing.T) {
	tbl, store, _ := policyTable(t)
	addPolicyAt(t, store, "grant-bash", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "")
	addPolicyAt(t, store, "grant-bash-2", policy.ToolAccess, detailBoss, policy.Deny, `{"toolName":"bash","state":"deny"}`, "")
	addPolicyAt(t, store, "grant-read", policy.ToolAccess, detailBoss, policy.Allow, `{"toolName":"read","state":"allow"}`, "")
	addPolicyAt(t, store, "zzz-far", policy.ToolAccess, detailBoss, policy.Deny, `{"toolName":"zzz","state":"deny"}`, "")

	aeon := mustAeonCode(t, tbl, MethodPolicyGet, PolicyGetParams{ID: "grant-bashhh"}, ErrPolicyUnknown.Code)
	if aeon.Message != "no such policy" || aeon.Data.Fatal {
		t.Errorf("refusal shape = (%q, fatal=%v), want (\"no such policy\", fatal=false)", aeon.Message, aeon.Data.Fatal)
	}
	what := aeon.Data.What
	if !strings.Contains(what, `"grant-bashhh"`) {
		t.Errorf("the refusal does not name the id asked for: %q", what)
	}
	listed := what[strings.Index(what, "nearest ids are:"):]
	for _, near := range []string{`"grant-bash"`, `"grant-bash-2"`, `"grant-read"`} {
		if !strings.Contains(listed, near) {
			t.Errorf("the nearest list is missing %s: %q", near, listed)
		}
	}
	if strings.Contains(listed, "zzz-far") {
		t.Errorf("a far id crowded the list — the point is the CLOSEST, ≤ 3: %q", listed)
	}
	if n := strings.Count(listed, `"`) / 2; n > 3 || n == 0 {
		t.Errorf("nearest list quoted %d ids, want 1..3: %q", n, listed)
	}

	// An empty store says its own useful truth instead of an empty list.
	bare, _, _ := policyTable(t)
	aeon = mustAeonCode(t, bare, MethodPolicyGet, PolicyGetParams{ID: "anything"}, ErrPolicyUnknown.Code)
	if !strings.Contains(aeon.Data.What, "none — the table holds no policies") {
		t.Errorf("empty-store refusal = %q, want the stated emptiness", aeon.Data.What)
	}
}

// TestPolicyGetRefusesEscapedRowsFirst: a hand-edited row that escaped the
// write boundary refuses the SAME way list refuses it (-31400, the whole
// read) — through get too, and BEFORE any unknown-id answer. A row that
// could not have come through Add must not let the boundary answer "no such
// policy" while that row sits unread in the table: dropping it could drop a
// Deny.
func TestPolicyGetRefusesEscapedRowsFirst(t *testing.T) {
	tbl, store, fx := policyTable(t)
	addPolicyAt(t, store, "grant-bash", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "")
	fx.rows["hand-edited"] = policy.Row{
		ID: "hand-edited", Type: "tool_access", TargetKind: "agent", TargetID: "boss",
		Action: "deny", Source: "governor", // a source outside the closed set
		SpecJSON: `{"toolName":"rm","state":"deny"}`,
	}

	mustAeonCode(t, tbl, MethodPolicyList, nil, ErrPolicyReadRefused.Code)
	// get of a CLEAN id: the read still refuses — a half answer about a
	// rule is how a confident wrong answer is built.
	aeon := mustAeonCode(t, tbl, MethodPolicyGet, PolicyGetParams{ID: "grant-bash"}, ErrPolicyReadRefused.Code)
	if aeon.Code == ErrPolicyUnknown.Code {
		t.Fatal("an escaped row was laundered into 'unknown id' — the drop in disguise (SEC-015)")
	}
	// And get of the ESCAPED id itself is the same -31400: the boundary
	// never renders a row it cannot vouch for, and never claims absence
	// about it either.
	mustAeonCode(t, tbl, MethodPolicyGet, PolicyGetParams{ID: "hand-edited"}, ErrPolicyReadRefused.Code)

	delete(fx.rows, "hand-edited")
	res := dispatchPolicy[PolicyGetResult](t, tbl, MethodPolicyGet, PolicyGetParams{ID: "grant-bash"})
	if res.Policy.ID != "grant-bash" {
		t.Fatalf("the read stayed poisoned after the escaped row was gone: %+v", res.Policy)
	}
}

// TestPolicyReadsAreLive (PM11): rows written through the boundary — the
// CLI's door — are visible to the socket's detail reads with no pause and
// no restart. The two share the one *policy.Store handle; a cache between
// them fails here.
func TestPolicyReadsAreLive(t *testing.T) {
	tbl, store, _ := policyTable(t)
	p := policy.Policy{
		ID: "late-row", Type: policy.ToolAccess, Target: detailBoss, Action: policy.Deny,
		Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"rm","state":"deny"}`),
	}
	if rej := store.Add(context.Background(), p, time.Now().UTC()); rej != nil {
		t.Fatalf("CLI-door write: %s", rej.Reason)
	}
	res := dispatchPolicy[PolicyGetResult](t, tbl, MethodPolicyGet, PolicyGetParams{ID: "late-row"})
	if res.Policy.ID != "late-row" || res.Policy.Action != "deny" {
		t.Fatalf("policy.get did not see the fresh row: %+v", res.Policy)
	}
	if _, err := store.Remove(context.Background(), "late-row"); err != nil {
		t.Fatalf("CLI-door remove: %v", err)
	}
	mustAeonCode(t, tbl, MethodPolicyGet, PolicyGetParams{ID: "late-row"}, ErrPolicyUnknown.Code)
}

// --- policy.list's filters -------------------------------------------------------

// filterCorpus spans every filter dimension plus a row that is expired
// against the test instant.
func filterCorpus(t *testing.T, store *policy.Store) {
	t.Helper()
	addPolicyAt(t, store, "t-bash", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "")
	addPolicyAt(t, store, "t-loop", policy.ToolAccess, policy.Target{Kind: policy.LoopKind, ID: "nightly"}, policy.Ask, `{"toolName":"bash","state":"ask"}`, "")
	addPolicyAt(t, store, "c-bash", policy.CommandAccess, detailBoss, policy.Deny, `{"command":"git*","state":"deny"}`, "")
	addPolicyAt(t, store, "x-never", policy.SkillAccess, policy.Target{Kind: policy.AllAgents}, policy.Deny, `{"skill":"acme/deploy","state":"deny"}`, "")
	// Expired against the fixed instant this file installs.
	addPolicyAt(t, store, "gone", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "2020-01-01T00:00:00Z")
}

func TestPolicyListFiltersCompose(t *testing.T) {
	old := now
	defer func() { now = old }()
	fixed := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return fixed }

	tbl, store, _ := policyTable(t)
	filterCorpus(t, store)

	cases := []struct {
		name   string
		params PolicyListParams
		want   []string
	}{
		{"no params — everything, expiry included (D6/D8: absent means the audit view)",
			PolicyListParams{}, []string{"c-bash", "gone", "t-bash", "t-loop", "x-never"}},
		{"type", PolicyListParams{Type: "tool_access"}, []string{"gone", "t-bash", "t-loop"}},
		{"target, CLI spelling", PolicyListParams{Target: "agent:boss"}, []string{"c-bash", "gone", "t-bash"}},
		{"action+source (both seeded operator denials: the command row and x-never)",
			PolicyListParams{Action: "deny", Source: "operator"}, []string{"c-bash", "x-never"}},
		{"includeExpired explicitly false hides the gone row",
			PolicyListParams{IncludeExpired: ptr(false)}, []string{"c-bash", "t-bash", "t-loop", "x-never"}},
		{"includeExpired explicitly true keeps it (same as absent)",
			PolicyListParams{IncludeExpired: ptr(true)}, []string{"c-bash", "gone", "t-bash", "t-loop", "x-never"}},
		{"all four dimensions AND",
			PolicyListParams{Type: "tool_access", Target: "agent:boss", Source: "operator", Action: "allow"},
			[]string{"gone", "t-bash"}},
		{"all-agents", PolicyListParams{Target: "all-agents"}, []string{"x-never"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, c.params)
			if res.Statement != nil {
				t.Errorf("a non-empty filtered list carries a statement: %q", *res.Statement)
			}
			var got []string
			for _, v := range res.Policies {
				got = append(got, v.ID)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("ids = %v, want %v", got, c.want)
			}
			// The identity view stays the identity view: no row here may
			// carry spec bytes (the raw spec rides policy.get only).
			for _, v := range res.Policies {
				if v.Describe == "" {
					t.Errorf("row %s carries no describe — every surface owes the operator a sentence (D9)", v.ID)
				}
			}
		})
	}

	// Refusals name the value refused, through the right door:
	// closed-set parsers for type/source/action (-31400 verbatim), the
	// params door for a malformed target (-32602, spec: ErrBadParams
	// naming it).
	mustAeonCode(t, tbl, MethodPolicyList, PolicyListParams{Type: "made_up_type"}, ErrPolicyReadRefused.Code)
	mustAeonCode(t, tbl, MethodPolicyList, PolicyListParams{Action: "permit"}, ErrPolicyReadRefused.Code)
	mustAeonCode(t, tbl, MethodPolicyList, PolicyListParams{Source: "governor"}, ErrPolicyReadRefused.Code)
	// A malformed target is the PARAMS door (-32602 as plain JSON-RPC via
	// ErrBadParams, the handshake's rule for params that do not fit),
	// naming the value; the closed-set refusals above are Aeon codes.
	assertBadParams(t, tbl, PolicyListParams{Target: "agent:bad id"}, `"agent:bad id"`)
	assertBadParams(t, tbl, PolicyListParams{Target: "folder:x"}, `"folder:x"`)
}

// assertBadParams dispatches a filtered list and requires the plain
// ErrBadParams answer (never an Aeon code — the handshake test pins why)
// naming the given value.
func assertBadParams(t *testing.T, tbl *Table, params PolicyListParams, wantSub string) {
	t.Helper()
	_, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyList, mustJSON(t, params))
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("filtered list %v: want ErrBadParams, got %T (%v)", params, err, err)
	}
	if !strings.Contains(bad.Error(), wantSub) {
		t.Errorf("target refusal %q does not name %s", bad.Error(), wantSub)
	}
}

func ptr(b bool) *bool { return &b }

// TestPolicyListFilterEmptyUsesItsOwnStatement: the two empties never read
// the same — a filter that matched nothing says the FILTER matched nothing;
// the unfiltered empty corpus says D4's sentence. Exactly one of the two
// rides any answer, never both, never neither.
func TestPolicyListFilterEmptyUsesItsOwnStatement(t *testing.T) {
	old := now
	defer func() { now = old }()
	now = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }

	// Filter matches nothing in a POPULATED table: the filter's answer.
	tbl, store, _ := policyTable(t)
	filterCorpus(t, store)
	res := dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, PolicyListParams{Type: "effect_class"})
	if len(res.Policies) != 0 || res.Statement == nil || *res.Statement != PolicyListFilterEmptyStatement {
		t.Fatalf("filtered-empty = (%d rows, %v), want the filter's own statement %q",
			len(res.Policies), res.Statement, PolicyListFilterEmptyStatement)
	}

	// Hiding expiry on a table of only-gone rows is still a filter that
	// removed rows — the filter's sentence, not D4's (the corpus is NOT
	// empty; everything in it has stopped existing).
	only, onlyStore, _ := policyTable(t)
	addPolicyAt(t, onlyStore, "all-gone", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "2020-01-01T00:00:00Z")
	res = dispatchPolicy[PolicyListResult](t, only, MethodPolicyList, PolicyListParams{IncludeExpired: ptr(false)})
	if len(res.Policies) != 0 || res.Statement == nil || *res.Statement != PolicyListFilterEmptyStatement {
		t.Fatalf("all-hidden-by-expiry = (%d rows, %v), want the filter's own statement", len(res.Policies), res.Statement)
	}

	// Truly empty corpus, no filter: D4's sentence, byte for byte — and
	// never the filter's.
	bare, _, _ := policyTable(t)
	res = dispatchPolicy[PolicyListResult](t, bare, MethodPolicyList, nil)
	if res.Statement == nil || *res.Statement != PolicyListEmptyStatement {
		t.Fatalf("unfiltered empty = %v, want D4's sentence", res.Statement)
	}
	if *res.Statement == PolicyListFilterEmptyStatement {
		t.Fatal("the two empties read the same")
	}
	// And the filtered-empty answer carries no D4, the unfiltered empty no
	// filter sentence — one sentence, always exactly one.
	if strings.Contains(*res.Statement, "match this filter") {
		t.Fatal("D4's sentence grew the filter's words")
	}
}

// TestPolicyListUnchangedWithNoFilter: the pre-S45 conformance fixture
// (list-one-rule.json) is replayed against a dispatch of the same corpus —
// every field the fixture names must match byte for byte; only the
// ADDITIVE fields (describe/createdAt) may appear that the fixture never
// knew. This is the existing-client contract.
func TestPolicyListUnchangedWithNoFilter(t *testing.T) {
	raw, err := os.ReadFile("conformance/policy/list-one-rule.json")
	if err != nil {
		t.Fatalf("reading the pre-S45 fixture: %v", err)
	}
	var fixture struct {
		Response struct {
			Result PolicyListResult `json:"result"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	want := fixture.Response.Result

	tbl, store, _ := policyTable(t)
	addPolicyAt(t, store, "grant-read-tool", policy.ToolAccess, detailBoss, policy.Allow, `{"toolName":"read","state":"allow"}`, "")
	addPolicyAt(t, store, "until-halloween", policy.CommandAccess,
		policy.Target{Kind: policy.LaneTypeKind, ID: "review"}, policy.Deny, `{"command":"git push*","state":"deny"}`, "2026-11-01T00:00:00Z")

	for _, params := range []any{nil, PolicyListParams{}, json.RawMessage(`{}`)} {
		got := dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, params)
		if len(got.Policies) != len(want.Policies) {
			t.Fatalf("rows = %d, fixture says %d (params %v)", len(got.Policies), len(want.Policies), params)
		}
		for i := range want.Policies {
			w, g := want.Policies[i], got.Policies[i]
			if g.ID != w.ID || g.Type != w.Type || g.Target != w.Target ||
				g.Action != w.Action || g.Source != w.Source {
				t.Errorf("row %d identity = %+v, fixture says %+v (params %v)", i, g, w, params)
			}
			if (g.NotAfter == nil) != (w.NotAfter == nil) ||
				(g.NotAfter != nil && !g.NotAfter.Equal(*w.NotAfter)) {
				t.Errorf("row %d notAfter = %v, fixture says %v", i, g.NotAfter, w.NotAfter)
			}
			// Additive fields exist and carry truth the fixture could not
			// name — an old client ignores them, a new one gets sentences.
			if g.Describe == "" || g.CreatedAt == "" {
				t.Errorf("row %d lost an additive field: %+v", i, g)
			}
		}
		if (got.Statement == nil) != (want.Statement == nil) {
			t.Fatalf("statement presence changed: got %v, fixture %v", got.Statement, want.Statement)
		}
	}
}

// --- policy.vocabulary -------------------------------------------------------------

// TestPolicyVocabularyAnswersEveryClosedSet: the wire's sets are the
// engine's (compared to the exported All* doors, not to literals — a
// literal here would be the drift this method exists to prevent), the
// matrix carries the rule's sentence, and every compiled type has a
// descriptor. The storeless daemon answers too: refusing this with
// ErrStoreUnavailable would claim the daemon cannot state facts it ships.
func TestPolicyVocabularyAnswersEveryClosedSet(t *testing.T) {
	tbl, store, fx := policyTable(t)
	addPolicyAt(t, store, "t-bash", policy.ToolAccess, detailBoss, policy.Allow, detailToolSpec, "")
	fx.spy.reset()

	res := dispatchPolicy[PolicyVocabularyResult](t, tbl, MethodPolicyVocabulary, nil)
	v := res.Vocabulary
	eqStrs(t, "types", v.Types, stringify(policy.AllPolicyTypes()))
	eqStrs(t, "sources", v.Sources, stringify(policy.AllSources()))
	eqStrs(t, "actions", v.Actions, stringify(policy.AllActions()))
	eqStrs(t, "targetKinds", v.TargetKinds, stringify(policy.AllTargetKinds()))

	if len(v.Authorship) != len(policy.AllSources())*len(policy.AllActions()) {
		t.Fatalf("matrix cells = %d, want sources × actions", len(v.Authorship))
	}
	for _, e := range v.Authorship {
		if e.Source == "operator" && !e.Allowed {
			t.Errorf("operator/%s refused — the matrix drifted from PL1", e.Action)
		}
		if e.Source != "operator" && e.Action == "allow" {
			if e.Allowed || !strings.Contains(e.Reason, "requires an operator (PL1)") {
				t.Errorf("%s/allow = (allowed=%v, reason=%q), want the boundary's PL1 sentence", e.Source, e.Allowed, e.Reason)
			}
		}
	}
	if len(v.Specs) != len(policy.AllPolicyTypes()) {
		t.Fatalf("descriptors = %d, engine compiles %d types", len(v.Specs), len(policy.AllPolicyTypes()))
	}
	if *fx.spy != (policySpy{}) {
		t.Errorf("vocabulary wrote: %+v", *fx.spy)
	}

	bare := newTestTable(t)
	initialized(t, bare, "c1", PROTOCOL_VERSION)
	res = dispatchPolicy[PolicyVocabularyResult](t, bare, MethodPolicyVocabulary, json.RawMessage(`{}`))
	if len(res.Vocabulary.Types) != len(policy.AllPolicyTypes()) {
		t.Error("a storeless daemon refused to state its own vocabulary")
	}
	// list and get DO need the store — the honest storeless answer is
	// ErrStoreUnavailable, not an empty success.
	mustAeonCode(t, bare, MethodPolicyList, nil, ErrStoreUnavailable.Code)
	mustAeonCode(t, bare, MethodPolicyGet, PolicyGetParams{ID: "x"}, ErrStoreUnavailable.Code)
}

// --- registration + zero writes across the family ----------------------------------

// TestPolicyDetailReadsRegisteredLikeTheReads: both new methods ride the
// table with RequiresInitialize, un-Debugged (product surface, not
// diagnostics), gated before initialize like every read.
func TestPolicyDetailReadsRegisteredLikeTheReads(t *testing.T) {
	tbl := newTestTable(t)
	for _, name := range []MethodName{MethodPolicyGet, MethodPolicyVocabulary} {
		m, ok := tbl.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered (PM5: the detail reads are product surface)", name)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s answers before initialize — the handshake gate is not optional", name)
		}
		if m.Debug {
			t.Errorf("%s is Debug: S19 would mount a diagnostics door onto the narrow projection", name)
		}
		uninit := newTestTable(t)
		mustAeonCode(t, uninit, name, nil, ErrNotInitialized.Code)
	}
}

// TestPolicyReadsDoNotWrite spans the WHOLE read family — list (filtered
// and not), get (found and unknown), vocabulary — across one spy reset.
// A read is not an event; no read here records, audits, or trims (PL11,
// re-proved for the new doors).
func TestPolicyReadsDoNotWrite(t *testing.T) {
	old := now
	defer func() { now = old }()
	now = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }

	tbl, store, fx := policyTable(t)
	filterCorpus(t, store)
	fx.spy.reset()

	dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, nil)
	dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, PolicyListParams{Type: "tool_access", IncludeExpired: ptr(false)})
	mustAeonCode(t, tbl, MethodPolicyList, PolicyListParams{Type: "made_up_type"}, ErrPolicyReadRefused.Code)
	dispatchPolicy[PolicyGetResult](t, tbl, MethodPolicyGet, PolicyGetParams{ID: "t-bash"})
	mustAeonCode(t, tbl, MethodPolicyGet, PolicyGetParams{ID: "no-such-id"}, ErrPolicyUnknown.Code)
	dispatchPolicy[PolicyVocabularyResult](t, tbl, MethodPolicyVocabulary, nil)

	if *fx.spy != (policySpy{}) {
		t.Fatalf("a read wrote to the boundary (adds=%d removes=%d accepts=%d inserts=%d trims=%d) — a read audited a hypothetical",
			fx.spy.adds, fx.spy.removes, fx.spy.accepts, fx.spy.inserts, fx.spy.trims)
	}
}

// --- helpers -----------------------------------------------------------------------

func mustListedPolicy(t *testing.T, store *policy.Store, id string) policy.Policy {
	t.Helper()
	rows, rej := store.List(context.Background(), time.Now().UTC())
	if rej != nil {
		t.Fatalf("reading back for the D9 pin: %s", rej.Reason)
	}
	for _, p := range rows {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("id %q vanished between seeding and pinning", id)
	return policy.Policy{}
}

func eqStrs(t *testing.T, name string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s = %v, engine says %v", name, got, want)
	}
}

func stringify[T ~string](xs []T) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, string(x))
	}
	return out
}
