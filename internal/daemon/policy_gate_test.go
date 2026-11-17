package daemon

// The production policy-gate tests (loop T9). Two claims live here that the
// runtime's own tests CANNOT make: that the daemon's table boots with a gate
// wired (a nil gate is silently unenforced — "production always wires one"
// must therefore be proven, not commented), and that the adapter decides via
// `policy.Evaluate` over the STORED set and lands refusals in `policy_event`.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

func TestProductionDaemonWiresThePolicyGate(t *testing.T) {
	// nil gate ⇒ silently no enforcement — which is EXACTLY why the
	// store-backed daemon must be shown wired. `aeon serve` always passes a
	// store (it opens before the daemon is built), so this assertion IS the
	// production claim. The storeless daemon stays the test mode it was: nil
	// gate, no enforcement, no panic.
	s := recoveryStoreBound(t)
	d := New(Options{Store: s})
	if d.Table.PolicyGate == nil {
		t.Fatal("the store-backed daemon's table has NO policy gate — a step could dispatch without a " +
			"verdict, and nil means silently unenforced (D11/PL9)")
	}
	// T13: the READ window is wired at the same site over the same bridge —
	// reads of one store, one gate, so the app is never shown a set the
	// enforcement is not using. A nil here would answer the app's window
	// with ErrStoreUnavailable on a daemon that has a store: a lying read.
	if d.Table.PolicyStore == nil {
		t.Fatal("the store-backed daemon's table has NO policy read window — the four policy.* reads " +
			"would answer store-unavailable while the gate decides from the very same store (T13/R4)")
	}
	if d0 := New(Options{}); d0.Table.PolicyGate != nil || d0.Table.PolicyStore != nil {
		t.Error("the storeless (test-mode) daemon grew a gate or a read window; with no store the read fails closed on " +
			"every call and pre-S10 daemons would stop running")
	}
}

// DEF-030's first trigger: the cap fires once at daemon start. This is the
// call site retention.go promises exists; without it the 90-day cap is a
// number with no hands.
func TestTrimPolicyEventsAtStartupTrimsOnceAndSpeaks(t *testing.T) {
	s := recoveryStoreBound(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	boundary := policy.NewStore(newPolicyRepo(s))
	if rej := boundary.Add(ctx, policy.Policy{
		ID: "no-bash", Type: policy.ToolAccess, Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}, now); rej != nil {
		t.Fatalf("seeding the deny: %s", rej.Reason)
	}
	v := policy.Verdict{Subject: "agent:boss", Type: policy.ToolAccess,
		By: &policy.Policy{ID: "no-bash", Type: policy.ToolAccess, Target: policy.Target{Kind: policy.AllAgents},
			Action: policy.Deny, Source: policy.Operator}}
	// One row past the cap (91 days), one inside it.
	for _, at := range []time.Time{now.Add(-91 * 24 * time.Hour), now.Add(-time.Hour)} {
		if rej := boundary.RecordRefusal(ctx, "agent:boss", v, at); rej != nil {
			t.Fatalf("seeding a refusal: %s", rej.Reason)
		}
	}

	if got := TrimPolicyEventsAtStartup(ctx, s, discardLogger{}, now); got != 1 {
		t.Errorf("the startup trim took %d rows, want exactly the one past the 90-day cap", got)
	}
	if _, rows, rej := boundary.ReportEvents(ctx, "", 10); rej != nil || len(rows) != 1 {
		t.Errorf("after the trim the log holds %d rows (rej %v), want the one inside the cap", len(rows), rej)
	}
	// Idempotent: the second boot trims nothing.
	if got := TrimPolicyEventsAtStartup(ctx, s, discardLogger{}, now); got != 0 {
		t.Errorf("the second startup trim took %d rows, want 0", got)
	}
	// The storeless shape says so rather than panicking.
	if got := TrimPolicyEventsAtStartup(ctx, nil, nil, now); got != 0 {
		t.Errorf("trimming a nil store returned %d, want 0", got)
	}
}

func TestStorePolicyGateDecidesFromTheStoredSet(t *testing.T) {
	// The gate reads through the write boundary (every stored row re-parsed
	// through the closed set), so seeding via the SAME boundary proves the
	// whole read: add → decide.
	s := recoveryStoreBound(t)
	ctx := context.Background()
	addAt := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	decideAt := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	boundary := policy.NewStore(newPolicyRepo(s))
	if rej := boundary.Add(ctx, policy.Policy{
		ID:     "allow-read",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Allow,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"read","state":"allow"}`),
	}, addAt); rej != nil {
		t.Fatalf("seed the allow: %s", rej.Reason)
	}

	gate := NewPolicyGate(s, nil)

	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, decideAt)
	if !dec.Allowed {
		t.Fatalf("the stored operator allow did not permit its own tool: %q", dec.Sentence)
	}
	if !strings.HasPrefix(dec.Sentence, "allowed: agent:boss") {
		t.Errorf("the allow sentence is %q, want T4's rendering of the allow", dec.Sentence)
	}

	// Everything else is PL5: nothing matched, so nothing is permitted —
	// including a tool the step's grant list carries.
	dec = gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash"}, decideAt)
	if dec.Allowed {
		t.Fatal("bash was permitted by a policy set that only names read — the default deny must deny")
	}
	if !strings.HasPrefix(dec.Sentence, "refused: agent:boss") {
		t.Errorf("the deny sentence is %q", dec.Sentence)
	}
	if !strings.Contains(dec.Sentence, "default deny (PL5)") {
		t.Errorf("the deny sentence does not name the default deny: %q", dec.Sentence)
	}

	// An agent the stored allow does not name gets nothing from it.
	dec = gate.Decide(runtime.PolicyGateRequest{Agent: "other", Tool: "read"}, decideAt)
	if dec.Allowed {
		t.Error("the agent-scoped allow permitted a different agent")
	}
}

func TestStorePolicyGateRecordsRefusals(t *testing.T) {
	// The refusal half of the adapter: a denial goes through the boundary's
	// RecordRefusal — the audit row's subject is the verdict's, the detail is
	// T4's sentence (the same rendering the model was handed) — and an allow
	// records nothing (an audit row calling a permission a refusal is the row
	// that lies).
	s := recoveryStoreBound(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	boundary := policy.NewStore(newPolicyRepo(s))
	deny := policy.Policy{
		ID:     "no-bash",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}
	if rej := boundary.Add(ctx, deny, at.Add(-time.Hour)); rej != nil {
		t.Fatalf("seed the deny: %s", rej.Reason)
	}
	expected := policy.Evaluate([]policy.Policy{deny}, policy.Request{
		Type: policy.ToolAccess, Agent: "boss", Tool: "bash",
	}, at).Sentence()

	gate := NewPolicyGate(s, nil)
	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash"}, at)
	if dec.Allowed {
		t.Fatal("the deny did not deny")
	}
	if dec.Sentence != expected {
		t.Errorf("the model-facing sentence diverged from T4's rendering:\n got: %q\nwant: %q", dec.Sentence, expected)
	}

	rows, err := s.PolicyEventQuery(ctx, "agent:boss", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d refusal rows, want exactly 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Outcome != "refusal" {
		t.Errorf("outcome %q, want refusal", row.Outcome)
	}
	if row.Detail != expected {
		t.Errorf("the recorded detail is %q, want the sentence byte-equal (the one formatter, T4)", expected)
	}
	if row.PolicyID != "no-bash" {
		t.Errorf("the row names policy %q, want the deny that fired", row.PolicyID)
	}

	// The allowed call added no audit row.
	if dec = gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, at); dec.Allowed {
		t.Fatal("read should be default-denied here (only bash is named)")
	}
	// (That refusal adds its own row for the default deny — prove the count
	// moves only with refusals, and that the read refusal is the PL5 one with
	// no policy to name.)
	rows, err = s.PolicyEventQuery(ctx, "agent:boss", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows after a second refusal, want 2", len(rows))
	}
	for _, r := range rows {
		if r.PolicyID == "" && !strings.Contains(r.Detail, "default deny (PL5)") {
			t.Errorf("the row that names no policy is not the default deny: %+v", r)
		}
	}
}

func TestRepoFuncsBridgeForwardsEveryHook(t *testing.T) {
	// The daemon-side half of the RepoFuncs bridge: every hook forwards to the
	// real store and the row shapes cross field for field. The policy package
	// pins the bridge's forwarding contract against fakes
	// (TestRepoFuncsBridgeForwardsEveryCall); this pins the CONVERSIONS
	// against the real table — the row that silently loses a field across
	// policy.Row ⇄ store.PolicyRow is the policy that vanishes from the
	// enforcement set without anyone noticing. It is also the surface T12/T13
	// will reuse, so a break here breaks the CLI and the socket reads too.
	s := recoveryStoreBound(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	repo := policy.NewStore(newPolicyRepo(s))

	p := policy.Policy{
		ID:     "no-bash",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Deny,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}
	if rej := repo.Add(ctx, p, now); rej != nil {
		t.Fatalf("Add: %s", rej.Reason)
	}

	got, rej := repo.List(ctx, now)
	if rej != nil {
		t.Fatalf("List: %s", rej.Reason)
	}
	if len(got) != 1 || got[0].ID != "no-bash" || got[0].Target.Kind != policy.AgentKind ||
		got[0].Target.ID != "boss" || string(got[0].Action) != "deny" || string(got[0].Source) != "operator" ||
		got[0].Spec == nil || string(got[0].Spec) == "{}" {
		t.Fatalf("the row lost a field crossing the seam: %+v", got[0])
	}

	// Acceptance: recorded, read back through the same bridge.
	target := policy.Target{Kind: policy.AgentKind, ID: "boss"}
	if rej := repo.RecordAcceptance(ctx, "no-bash", target, "boss", now); rej != nil {
		t.Fatalf("RecordAcceptance: %s", rej.Reason)
	}
	if rej := repo.RecordAcceptance(ctx, "no-bash", target, "boss", now.Add(time.Hour)); rej != nil {
		t.Fatalf("re-record (must be idempotent): %s", rej.Reason)
	}
	as, rej := repo.AcceptancesFor(ctx, target)
	if rej != nil {
		t.Fatalf("AcceptancesFor: %s", rej.Reason)
	}
	if len(as) != 1 || as[0].AcceptedBy != "boss" {
		t.Fatalf("acceptances came back as %+v", as)
	}

	// Event: written only through the boundary (the sole-writer grep stands),
	// read back, stats'd, and trimmed — all through this bridge.
	v := policy.Evaluate(got, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}, now)
	if v.Allowed {
		t.Fatal("the seeded deny did not deny")
	}
	if rej := repo.RecordRefusal(ctx, v.Subject, v, now); rej != nil {
		t.Fatalf("RecordRefusal: %s", rej.Reason)
	}
	report, rows, rej := repo.ReportEvents(ctx, "agent:boss", 10)
	if rej != nil {
		t.Fatalf("ReportEvents: %s", rej.Reason)
	}
	if len(rows) != 1 || rows[0].Detail != v.Sentence() || report.Depth < 1 {
		t.Fatalf("the event row lost a field crossing the seam: rows=%+v depth=%d", rows, report.Depth)
	}
	// The trim is the AGE cap, not an ad-hoc cutoff: nothing goes while the
	// row is inside 90 days, and one instant past the cap takes it.
	if n, err := repo.TrimEvents(ctx, now); err != nil || n != 0 {
		t.Fatalf("TrimEvents inside the cap: n=%d err=%v, want 0 rows gone (PL11's cap, not a purge)", n, err)
	}
	if n, err := repo.TrimEvents(ctx, now.Add(91*24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("TrimEvents past the 90-day cap: n=%d err=%v, want the row taken", n, err)
	}
	if ok, err := repo.Remove(ctx, "no-bash"); err != nil || !ok {
		t.Fatalf("Remove: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.Remove(ctx, "no-bash"); err != nil || ok {
		t.Errorf("second Remove: ok=%v err=%v, want false/nil", ok, err)
	}
}

func TestStorePolicyGateFailsClosedWhenTheSetCannotBeRead(t *testing.T) {
	// The read failed ⇒ no allow can be vouched for ⇒ nothing is permitted,
	// rendered by the engine's own default-deny line (the adapter invents no
	// prose). A bridge whose ListAll errors stands in for the unreadable set;
	// no EventInsert hook is wired, so the test also proves the read-failure
	// path does not reach for an audit row through a store that just failed.
	gate := &storePolicyGate{
		boundary: policy.NewStore(policy.RepoFuncs{
			ListAll: func(context.Context) ([]policy.Row, error) {
				return nil, errors.New("the disk broke")
			},
		}),
		log: discardLogger{},
	}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, at)
	if dec.Allowed {
		t.Fatal("an unreadable policy set permitted a tool — a guard that fails open is not a guard")
	}
	if !strings.Contains(dec.Sentence, "default deny (PL5)") {
		t.Errorf("the fail-closed sentence is %q, want the engine's default-deny rendering", dec.Sentence)
	}
}

func TestStorePolicyGateRefusalStandsWhenTheAuditRowFails(t *testing.T) {
	// The audit trail is EVIDENCE, not a permission check: a refusal whose
	// event row cannot be written still refuses (and still says why to the
	// model). The inverse — "audit write failed, let it through" — would make
	// enforcement conditional on a second table being writable, and a disk
	// full of refusals would quietly become a disk full of permissions.
	//
	// Fixture: the REAL bridge over a real store (the ruleset reads for real),
	// with ONLY the EventInsert hook poisoned.
	s := recoveryStoreBound(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	seed := policy.NewStore(newPolicyRepo(s))
	if rej := seed.Add(ctx, policy.Policy{
		ID:     "no-bash",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Deny,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}, at.Add(-time.Hour)); rej != nil {
		t.Fatalf("seed the deny: %s", rej.Reason)
	}

	repo := newPolicyRepo(s)
	repo.EventInsert = func(context.Context, string, string, string, string, time.Time) error {
		return errors.New("the audit write died")
	}
	gate := &storePolicyGate{boundary: policy.NewStore(repo), log: discardLogger{}}

	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash"}, at)
	if dec.Allowed {
		t.Fatal("a refusal whose audit row failed became an ALLOW — enforcement must never depend on the audit")
	}
	if !strings.Contains(dec.Sentence, "no-bash") {
		t.Errorf("the model-visible refusal is %q, want the deny rule's own sentence", dec.Sentence)
	}
}

func TestNewPolicyRepoBridgesStoreErrorsVerbatim(t *testing.T) {
	// The bridge swallows nothing: every hook that touches the store returns
	// its error, so the boundary sees an unreadable set (and fails closed)
	// rather than an empty one (and permits). A bridge that turned errors into
	// empty results would be the fail-OPEN bug wearing a helper's clothes.
	//
	// The instrument is a cancelled context against the live store: every
	// store method runs its query with that context, so each hook faces a real
	// store error on every call. (A closed store is NOT the instrument —
	// store.Close nils the pools, and the methods panic on the nil handle;
	// that is the store's own lifecycle contract, tested in internal/store.)
	s := recoveryStoreBound(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := newPolicyRepo(s)
	// Only the three hooks that MAP rows have an error branch to swallow
	// (`if err != nil { return nil, err }` before building the slice); the
	// write hooks return the store method's error as a single expression and
	// cannot swallow one, so they are not pretended to be tested here.
	if _, err := repo.ListAll(ctx); err == nil {
		t.Error("ListAll over a failing store returned nil — the boundary would read an EMPTY set and permit")
	}
	if _, err := repo.AcceptanceList(ctx, "loop", "l"); err == nil {
		t.Error("AcceptanceList over a failing store returned nil")
	}
	if _, err := repo.EventQuery(ctx, "agent:boss", 10); err == nil {
		t.Error("EventQuery over a failing store returned nil — the report would claim an empty audit trail")
	}
}

// ── S11 T10: the command question, the ask shape, command rules in the frozen set ──

// commandRulePolicy builds one command_access rule in the shape the write
// boundary accepts (state mirrors action, operator source). It is only ever
// seeded through the real boundary below, so the row the gate decides against
// is a row Add itself would have accepted.
func commandRulePolicy(id, agent, pattern string, action policy.Action) policy.Policy {
	return policy.Policy{
		ID:     id,
		Type:   policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: action,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"command":"` + pattern + `","state":"` + string(action) + `"}`),
	}
}

// toolRulePolicy is the tool_access twin.
func toolRulePolicy(id, agent, tool string, action policy.Action) policy.Policy {
	return policy.Policy{
		ID:     id,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: action,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"` + tool + `","state":"` + string(action) + `"}`),
	}
}

// seedRules writes the rules through the real write boundary — the same door
// `aeon policy` opens (PL15) — so every gate decision below is decided
// against stored rows, not test shapes.
func seedRules(t *testing.T, s *store.Store, at time.Time, ps ...policy.Policy) {
	t.Helper()
	boundary := policy.NewStore(newPolicyRepo(s))
	for _, p := range ps {
		if rej := boundary.Add(context.Background(), p, at); rej != nil {
			t.Fatalf("seeding %s: %s", p.ID, rej.Reason)
		}
	}
}

func TestPolicyGateAsksTheCommandQuestion(t *testing.T) {
	// The routing side of the ONE selection rule (S11): a bash block WITH a
	// command is governed by the command_access rules. The command deny
	// fires on its own pattern and the sentence names the command rule —
	// only the command evaluator can name it, so the naming IS the routing
	// proof. Everything else keeps asking the tool question: the tool allow
	// permits read, and the command rule cannot touch it (another type's
	// rule is inert for the request).
	s := recoveryStoreBound(t)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	cmdDeny := commandRulePolicy("no-force-push", "boss", "git push*", policy.Deny)
	seedRules(t, s, at, cmdDeny, toolRulePolicy("allow-read", "boss", "read", policy.Allow))

	expected := policy.Evaluate([]policy.Policy{cmdDeny},
		policy.Request{Type: policy.CommandAccess, Agent: "boss", Command: "git push --force"}, at).Sentence()

	gate := NewPolicyGate(s, nil)

	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash", Command: "git push --force"}, at)
	if dec.Allowed || dec.Asked {
		t.Fatalf("the stored command deny did not deny its own pattern: %+v", dec)
	}
	if dec.Sentence != expected {
		t.Errorf("the command refusal diverged from the engine's rendering:\n got: %q\nwant: %q", dec.Sentence, expected)
	}
	if !strings.Contains(dec.Sentence, "no-force-push") {
		t.Errorf("the refusal does not name the command rule that fired: %q", dec.Sentence)
	}

	// The tool question still answers everything else.
	if d := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, at); !d.Allowed || d.Asked {
		t.Errorf("the tool allow did not permit read: %+v", d)
	}
	if d := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read", Command: "git push --force"}, at); !d.Allowed {
		t.Errorf("a command on a NON-bash tool flipped the question — the ONE rule flips for bash blocks only: %+v", d)
	}
}

func TestPolicyGateFallsBackToTheToolQuestion(t *testing.T) {
	// The fallback side of the ONE rule, with its two fail-closed truths and
	// its one governing flip:
	//
	//   - bash WITH a command asks the COMMAND question. With no command rule
	//     permitting the call, the default deny answers — and the sentence
	//     must name NOTHING: a sentence naming the stored tool deny would
	//     mean the tool question had been asked instead. The tool deny cannot
	//     leak a permission to a question it does not govern, and nothing
	//     else vouches for the call (PL5).
	//   - bash with an EMPTY command asks the TOOL question, and there a
	//     broad command allow cannot rescue the tool deny: it is another
	//     question's answer, inert. The naming is the routing proof here too.
	//   - with a command present and a command rule matching it, THE COMMAND
	//     RULE GOVERNS — the pinned consequence of the ONE rule and the
	//     corpus's own model (the opencode agents govern bash with command
	//     patterns, not a tool toggle; the tool rule governs the
	//     commandless call, G20).
	t.Run("bash with a command and no matching command rule fails closed", func(t *testing.T) {
		s := recoveryStoreBound(t)
		at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		seedRules(t, s, at, toolRulePolicy("no-bash", "boss", "bash", policy.Deny))

		gate := NewPolicyGate(s, nil)
		dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash", Command: "ls"}, at)
		if dec.Allowed || dec.Asked {
			t.Fatalf("bash reached a permitted verdict with nothing permitting it: %+v", dec)
		}
		wantPL5 := policy.Evaluate(nil,
			policy.Request{Type: policy.CommandAccess, Agent: "boss", Command: "ls"}, at).Sentence()
		if dec.Sentence != wantPL5 {
			t.Errorf("the no-match denial is %q, want the command question's default-deny rendering %q", dec.Sentence, wantPL5)
		}
		if strings.Contains(dec.Sentence, "no-bash") {
			t.Errorf("the denial names the tool rule — the TOOL question was asked, not the command question: %q", dec.Sentence)
		}
	})

	t.Run("empty command asks the tool question; a command allow cannot rescue the tool deny", func(t *testing.T) {
		s := recoveryStoreBound(t)
		at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		seedRules(t, s, at,
			toolRulePolicy("no-bash", "boss", "bash", policy.Deny),
			commandRulePolicy("allow-any-command", "boss", "*", policy.Allow))

		gate := NewPolicyGate(s, nil)
		dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash"}, at)
		if dec.Allowed || dec.Asked {
			t.Fatalf("the command allow rescued a tool deny on the tool question: %+v", dec)
		}
		wantToolDeny := policy.Evaluate([]policy.Policy{toolRulePolicy("no-bash", "boss", "bash", policy.Deny)},
			policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}, at).Sentence()
		if dec.Sentence != wantToolDeny || !strings.Contains(dec.Sentence, "no-bash") {
			t.Errorf("the fallback denial is %q, want the tool deny's own sentence %q (naming no-bash — the routing proof)", dec.Sentence, wantToolDeny)
		}
	})

	t.Run("a matching command rule governs a bash block that carries a command", func(t *testing.T) {
		s := recoveryStoreBound(t)
		at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		cmdAllow := commandRulePolicy("allow-any-command", "boss", "*", policy.Allow)
		seedRules(t, s, at, toolRulePolicy("no-bash", "boss", "bash", policy.Deny), cmdAllow)

		gate := NewPolicyGate(s, nil)
		dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash", Command: "ls"}, at)
		if !dec.Allowed {
			t.Fatalf("the command question did not decide the call it was asked about: %+v", dec)
		}
		wantAllow := policy.Evaluate([]policy.Policy{cmdAllow},
			policy.Request{Type: policy.CommandAccess, Agent: "boss", Command: "ls"}, at).Sentence()
		if dec.Sentence != wantAllow || !strings.Contains(dec.Sentence, "allow-any-command") {
			t.Errorf("the command allow did not govern the bash call as its own sentence %q: %q", wantAllow, dec.Sentence)
		}
	})
}

// TestPolicyGateAsksInTheCanonicalToolName is the SEC-051 close (routed to
// T19): the gate's question carries the tool's CANONICAL name, so deny rows
// are not name-blind across the hidden write_file alias. The two discriminating
// cases (each is a verdict that FLIPS under the fix — an undiscriminating
// assertion here would be the default deny answering for itself):
//
//   - a deny row naming "write" DENIES a call spelled "write_file". Before
//     the canonicalization the deny missed the literal and PL5's default
//     deny answered with a different sentence, naming no rule; the naming is
//     the proof the operator's row fired.
//   - an allow row naming "write" PERMITS the alias spelling (before: PL5
//     denied — same capability, two answers, name-dependent).
//
// And the dispatcher invariant stands: this changes only the QUESTION, never
// what runs — both spellings are the same Tool in the toolset (pinned in
// tools by TestWriteFileAliasStillResolves and TestGrantingBothWriteNamesOffersTheToolOnce).
func TestPolicyGateAsksInTheCanonicalToolName(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	t.Run("a deny on write reaches a call spelled write_file", func(t *testing.T) {
		s := recoveryStoreBound(t)
		seedRules(t, s, at, toolRulePolicy("no-writes", "boss", "write", policy.Deny))
		gate := NewPolicyGate(s, nil)
		dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "write_file"}, at)
		if dec.Allowed || dec.Asked {
			t.Fatalf("a deny on write let the alias through: %+v", dec)
		}
		if !strings.Contains(dec.Sentence, "no-writes") {
			t.Errorf("the denial does not name the operator's row — it read as the default deny, not the canonicalized one: %q", dec.Sentence)
		}
	})

	t.Run("an allow on write answers the alias spelling", func(t *testing.T) {
		s := recoveryStoreBound(t)
		seedRules(t, s, at, toolRulePolicy("yes-write", "boss", "write", policy.Allow))
		gate := NewPolicyGate(s, nil)
		if d := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "write_file"}, at); !d.Allowed {
			t.Fatalf("the same capability answered differently by spelling: %+v", d)
		}
		// The honest spelling answers identically — one capability, one answer.
		if d := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "write"}, at); !d.Allowed {
			t.Fatalf("the honest spelling lost its allow: %+v", d)
		}
	})

	t.Run("an unknown name passes through and fails closed", func(t *testing.T) {
		s := recoveryStoreBound(t)
		seedRules(t, s, at, toolRulePolicy("yes-write", "boss", "write", policy.Allow))
		gate := NewPolicyGate(s, nil)
		if d := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "no-such-tool"}, at); d.Allowed {
			t.Fatalf("an unknown tool name reached a permitted verdict: %+v", d)
		}
	})
}

// TestPolicyGateToolRuleMatrixAcrossBothSpellings is the SEC-059 close,
// extending the SEC-051 block above to the ROW side of the comparison. T19
// canonicalized the request (gateQuestion asks honestly); the row stayed
// literal, so a rule AUTHORED under the hidden alias was inert in every
// direction: the deny never bit (allow "write" + deny "write_file" ALLOWED —
// PL4's deny-wins silently lost), the ask never routed to a human, the allow
// never granted the legacy pinned blueprints — while `policy why`, which
// passes the caller's spelling raw, claimed the row fired. Four quadrants ×
// both request spellings, plus the parity pin: for every cell, the engine's
// own rendering over the SAME rows — exactly what both why surfaces print
// (List, then Evaluate, then Sentence) — equals what the gate enforced, under
// every combination of row spelling and request spelling. why = enforcement,
// structurally, because both judge through the one Match the canonicalizer
// was wired into.
func TestPolicyGateToolRuleMatrixAcrossBothSpellings(t *testing.T) {
	at := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	spellings := []string{"write", "write_file"}

	// whySide is what `policy why` and policy.why render: Evaluate the stored
	// set on the request, print Sentence(). The why surfaces pass the
	// caller's spelling THROUGH RAW — after SEC-059 the two-sided Match makes
	// that identical to what the gate enforced, for every spelling.
	whySide := func(rows []policy.Policy, tool string) string {
		return policy.Evaluate(rows, policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: tool}, at).Sentence()
	}

	cases := []struct {
		name        string
		rows        []policy.Policy
		wantAllowed bool
		wantAsked   bool
		ruleID      string // the deciding row the sentence must name
	}{
		{
			name:        "(a) a deny row authored under the alias bites",
			rows:        []policy.Policy{toolRulePolicy("no-writes-alias", "boss", "write_file", policy.Deny)},
			wantAllowed: false, wantAsked: false, ruleID: "no-writes-alias",
		},
		{
			name: "(b) allow write plus deny write_file denies — PL4 deny-wins restored",
			rows: []policy.Policy{
				toolRulePolicy("yes-write", "boss", "write", policy.Allow),
				toolRulePolicy("no-writes-alias", "boss", "write_file", policy.Deny),
			},
			wantAllowed: false, wantAsked: false, ruleID: "no-writes-alias",
		},
		{
			name:        "(c) an ask row under the alias routes to a human",
			rows:        []policy.Policy{toolRulePolicy("ask-writes-alias", "boss", "write_file", policy.Ask)},
			wantAllowed: false, wantAsked: true, ruleID: "ask-writes-alias",
		},
		{
			name:        "(d) an allow row under the alias grants the legacy blueprint",
			rows:        []policy.Policy{toolRulePolicy("yes-write-alias", "boss", "write_file", policy.Allow)},
			wantAllowed: true, wantAsked: false, ruleID: "yes-write-alias",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := recoveryStoreBound(t)
			seedRules(t, s, at, c.rows...)
			gate := NewPolicyGate(s, nil)
			for _, spell := range spellings {
				dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: spell}, at)
				if dec.Allowed != c.wantAllowed || dec.Asked != c.wantAsked {
					t.Fatalf("request %q: Allowed=%v Asked=%v, want Allowed=%v Asked=%v — sentence %q",
						spell, dec.Allowed, dec.Asked, c.wantAllowed, c.wantAsked, dec.Sentence)
				}
				if !strings.Contains(dec.Sentence, c.ruleID) {
					t.Errorf("request %q: the decision does not name the deciding row: %q", spell, dec.Sentence)
				}
				if c.wantAsked && dec.RuleID != c.ruleID {
					t.Errorf("request %q: the ask names rule %q, want %q — a question that does not name its rule is unauditable (QAP-12)",
						spell, dec.RuleID, c.ruleID)
				}
				// (e) The parity pin: the engine over the SAME rows, asked
				// with EITHER raw spelling (the why surfaces never canonicalize
				// the caller's input — the Match does it for both sides),
				// says exactly what the gate enforced.
				for _, raw := range spellings {
					if got := whySide(c.rows, raw); got != dec.Sentence {
						t.Errorf("why ≠ enforcement: gate asked %q, why asked %q:\n gate: %q\n why:  %q", spell, raw, dec.Sentence, got)
					}
				}
			}
		})
	}
}

func TestPolicyGateAskWritesNoAuditRow(t *testing.T) {
	// A question is not a refusal (S11): the ask decision carries the
	// question shape — Asked, the deciding rule's ID and subject (QAP-12:
	// the question that does not name its rule is unauditable), and the
	// command asked about — and the policy_event table stays EMPTY, even
	// with the attribution (model-emitted, step-attributed) that would
	// otherwise buy two rows. The genuine deny beside it still buys both:
	// the audit records refusals, and a row calling a question a refusal is
	// the row that lies.
	s := recoveryStoreBound(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	askRule := commandRulePolicy("confirm-push", "boss", "git push*", policy.Ask)
	denyRule := commandRulePolicy("no-wipe", "boss", "git push --force*", policy.Deny)
	seedRules(t, s, at, askRule, denyRule)
	gate := NewPolicyGate(s, nil)

	askReq := runtime.PolicyGateRequest{Agent: "boss", Tool: "bash", Command: "git push origin main", StepID: 5, ModelRequested: true}
	dec := gate.Decide(askReq, at)
	if !dec.Asked || dec.Allowed {
		t.Fatalf("the ask rule did not route its own pattern to a question: %+v", dec)
	}
	if dec.RuleID != "confirm-push" || dec.RuleSubject != "agent:boss" || dec.Command != askReq.Command {
		t.Errorf("the question does not name itself: rule %q subject %q command %q, want confirm-push/agent:boss/%q",
			dec.RuleID, dec.RuleSubject, dec.Command, askReq.Command)
	}
	wantAsk := policy.Evaluate([]policy.Policy{askRule},
		policy.Request{Type: policy.CommandAccess, Agent: "boss", Command: askReq.Command}, at).Sentence()
	if dec.Sentence != wantAsk || !strings.HasPrefix(dec.Sentence, "asked: agent:boss") {
		t.Errorf("the ask sentence diverged from the ONE formatter:\n got: %q\nwant: %q", dec.Sentence, wantAsk)
	}
	if rows, err := s.PolicyEventQuery(ctx, "agent:boss", 10); err != nil || len(rows) != 0 {
		t.Errorf("the ask wrote %d audit row(s), want 0 — a question is not a refusal (err %v): %+v", len(rows), err, rows)
	}

	// The genuine deny in the same setup (deny outranks ask): exactly one
	// refusal row, and — attributed and model-emitted — one disagreement row.
	dec2 := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash", Command: "git push --force", StepID: 5, ModelRequested: true}, at)
	if dec2.Allowed || dec2.Asked {
		t.Fatalf("the deny did not outrank the ask: %+v", dec2)
	}
	if !strings.Contains(dec2.Sentence, "no-wipe") {
		t.Errorf("the deny sentence does not name the deny that fired: %q", dec2.Sentence)
	}
	rows, err := s.PolicyEventQuery(ctx, "agent:boss", 10)
	if err != nil {
		t.Fatal(err)
	}
	var refusal, disagreement int
	for _, r := range rows {
		switch r.Outcome {
		case "refusal":
			refusal++
		case "disagreement":
			disagreement++
		}
	}
	if refusal != 1 || disagreement != 1 {
		t.Fatalf("the deny wrote refusal=%d disagreement=%d, want 1 and 1: %+v", refusal, disagreement, rows)
	}
}

func TestPolicyGateCommandRulesReachTheFrozenSetAndItsDigest(t *testing.T) {
	// D12 is only honest if command rules ride the SAME pass-through as tool
	// rules: PolicyEntriesAt's ONE fetch carries them (no branch there names
	// a type — and none was added for S11), the digest MOVES when a command
	// row is removed (a digest blind to command rules would let command
	// rules change under a replay unnoticed), and a replay gate over the
	// fetched entries still decides the command question. This test is the
	// verification step 3 asks for: PolicyEntryFrom, PolicyEntriesAt and
	// daemon.go's wiring are unchanged, and command rows flow through them
	// unchanged.
	s := recoveryStoreBound(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	seedRules(t, s, at,
		toolRulePolicy("allow-read", "boss", "read", policy.Allow),
		commandRulePolicy("no-force-push", "boss", "git push*", policy.Deny))

	entries, rej := PolicyEntriesAt(ctx, s, at)
	if rej != nil {
		t.Fatalf("PolicyEntriesAt: %s", rej.Reason)
	}
	if len(entries) != 2 {
		t.Fatalf("the fetch carried %d entries, want both rules: %+v", len(entries), entries)
	}
	var without []runtime.PolicyEntry
	sawCommand := false
	for _, e := range entries {
		if e.Type == "command_access" && e.ID == "no-force-push" {
			sawCommand = true
			if !strings.Contains(string(e.Spec), "git push*") {
				t.Errorf("the frozen entry lost its spec: %s", e.Spec)
			}
			continue
		}
		without = append(without, e)
	}
	if !sawCommand {
		t.Fatalf("the fetched set carries no command_access entry — command rules never reached the manifest shape: %+v", entries)
	}
	if runtime.PoliciesHashOf(entries) == runtime.PoliciesHashOf(without) {
		t.Error("PoliciesHashOf ignored the command row — the frozen digest must move with the set it digests")
	}

	// And the frozen replay decides against those entries, command question
	// included, without ever re-querying the store.
	g := NewStepPolicyGate(s, nil, runtime.FrozenManifest{Entries: entries})
	if d := g.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "bash", Command: "git push --force"}, at); d.Allowed || !strings.Contains(d.Sentence, "no-force-push") {
		t.Errorf("the frozen command deny did not decide the replay: %+v", d)
	}
}

// TestStepGateDecidesAgainstTheFrozenSet is the frozen-set pattern
// (policy_replay_test.go's shapes) with a command_access row among the
// frozen entries: (a) a command rule REVOKED after the simulated crash
// cannot move the replayed decision — D12 byte-for-byte, with the live
// store demonstrated to answer differently once the rule is gone; (b) a
// digest mismatch still poisons to the engine's default-deny rendering with
// ZERO policy_event rows and ZERO live reads (DEF-079, now proven for the
// command shape too); (c) inside (b), the honest pair still replays — so
// what fired in (b) is the comparison, never a gate that denies everything.
func TestStepGateDecidesAgainstTheFrozenSet(t *testing.T) {
	t.Run("revoked command rule does not move the replay", func(t *testing.T) {
		s := recoveryStoreBound(t)
		ctx := context.Background()
		at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		// The live store, opposite to the frozen answer once the command
		// rule is gone: bash itself is allowed by a tool rule, so only a
		// gate carrying the frozen command deny explains a refusal below.
		seedRules(t, s, at,
			toolRulePolicy("yes-bash", "test-handler", "bash", policy.Allow),
			commandRulePolicy("no-force-push", "test-handler", "git push --force*", policy.Deny))

		entries, rej := PolicyEntriesAt(ctx, s, at)
		if rej != nil {
			t.Fatalf("PolicyEntriesAt: %s", rej.Reason)
		}
		manifest := runtime.FrozenManifest{Entries: entries, Hash: runtime.PoliciesHashOf(entries), Frozen: true}
		req := runtime.PolicyGateRequest{Agent: "test-handler", Tool: "bash", Command: "git push --force", StepID: 3, ModelRequested: true}

		// The step's pre-crash decision, taken live.
		baseline := NewPolicyGate(s, nil).Decide(req, at)
		if baseline.Allowed || !strings.Contains(baseline.Sentence, "no-force-push") {
			t.Fatalf("the pre-crash live decision is not the command denial meant to be replayed: %+v", baseline)
		}

		// The crash, and the overnight drift: the command rule is REVOKED
		// through the operator's door (Remove — what `aeon policy remove`
		// reaches). policy_event's FK is ON DELETE SET NULL, so the delete
		// lands and the audit keeps its rows.
		ok, err := policy.NewStore(newPolicyRepo(s)).Remove(ctx, "no-force-push")
		if err != nil || !ok {
			t.Fatalf("revoking the live command rule: ok=%v err=%v", ok, err)
		}
		// Non-vacuity: the live store, asked NOW, answers without naming the
		// rule (no command rule survives) — so the naming below can only
		// come from the frozen set, never from a re-query.
		if live := NewPolicyGate(s, nil).Decide(req, at); strings.Contains(live.Sentence, "no-force-push") {
			t.Fatal("the revoked rule still decides the live store — the revocation did not land")
		}

		repo, reads, writes := countingAuditBridge(t, s)
		w := &wire{}
		g := newStepPolicyGateWithBoundary(policy.NewStore(repo), w, manifest)
		d := g.Decide(req, at)
		if d.Allowed {
			t.Fatal("the replayed step was allowed by the very gate holding the frozen command deny — D12 lost to the revoked rule")
		}
		if d.Sentence != baseline.Sentence || !strings.Contains(d.Sentence, "no-force-push") {
			t.Errorf("the replayed decision differs from the pre-crash one:\n got:  %q\nwant: %q", d.Sentence, baseline.Sentence)
		}
		if *reads != 0 {
			t.Errorf("the frozen replay consulted the live policy table %d time(s); the set the step saw is never re-queried (D12)", *reads)
		}
		// A READABLE frozen set audits LIVE: both writes are attempted
		// through the live boundary. Their parent rule row is gone (the
		// revocation), so the FK refuses the inserts, the adapter SAYS so,
		// and the denial still stands — T9's discipline, exercised for real
		// by the revocation instead of by a poisoned row.
		if *writes != 2 {
			t.Errorf("the replayed refusal attempted %d audit write(s), want 2 (refusal + disagreement, live)", *writes)
		}
		if !strings.Contains(w.logLines(), "was NOT recorded") {
			t.Errorf("the FK-refused audit write on the revoked rule was not spoken: %s", w.logLines())
		}
	})

	t.Run("digest mismatch poisons to deny with zero policy_event rows", func(t *testing.T) {
		s := recoveryStoreBound(t)
		ctx := context.Background()
		at := time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)
		seedRules(t, s, at,
			toolRulePolicy("yes-bash", "tampered-handler", "bash", policy.Allow),
			commandRulePolicy("no-force-push", "tampered-handler", "git push --force*", policy.Deny))
		pristine, rej := PolicyEntriesAt(ctx, s, at)
		if rej != nil {
			t.Fatalf("PolicyEntriesAt: %s", rej.Reason)
		}
		recorded := runtime.PoliciesHashOf(pristine)

		// The tampered set: the command deny rewritten to a well-formed
		// OPERATOR allow — it decodes perfectly (vocabulary, pair, spec), so
		// only the recomputed digest can catch it.
		tampered := make([]runtime.PolicyEntry, 0, len(pristine))
		for _, e := range pristine {
			if e.Type == "command_access" {
				e.Action = "allow"
				e.Spec = json.RawMessage(`{"command":"git push --force*","state":"allow"}`)
			}
			tampered = append(tampered, e)
		}

		req := runtime.PolicyGateRequest{Agent: "tampered-handler", Tool: "bash", Command: "git push --force", StepID: 3, ModelRequested: true}
		wantDeny := policy.Evaluate(nil, policy.Request{
			Type: policy.CommandAccess, Agent: "tampered-handler", Tool: "bash", Command: "git push --force",
		}, at).Sentence()

		repo, reads, writes := countingAuditBridge(t, s)
		w := &wire{}
		g := newStepPolicyGateWithBoundary(policy.NewStore(repo), w,
			runtime.FrozenManifest{Entries: tampered, Hash: recorded, Frozen: true})
		d := g.Decide(req, at)
		if d.Allowed || d.Asked {
			t.Fatalf("the digest-contradicted command set was honoured: %+v", d)
		}
		if d.Sentence != wantDeny {
			t.Errorf("the poisoned gate did not answer with the engine's default-deny sentence:\n got:  %q\nwant: %q", d.Sentence, wantDeny)
		}
		if *reads != 0 || *writes != 0 {
			t.Errorf("the poisoned path touched the live table %d time(s) and the audit %d time(s); DEF-079 says NEITHER happens", *reads, *writes)
		}
		if rows, err := s.PolicyEventQuery(ctx, "agent:tampered-handler", 10); err != nil || len(rows) != 0 {
			t.Errorf("the poisoned path wrote %d policy_event row(s), want zero (err %v): %+v", len(rows), err, rows)
		}
		if !strings.Contains(w.logLines(), "does not match the set it carries") {
			t.Errorf("the mismatch was not logged: %s", w.logLines())
		}

		// (c) The positive control, same store: the HONEST pair (pristine
		// entries + their real digest) still replays the command deny — so
		// what denied above was the comparison, not a gate that denies
		// everything.
		repo3, _, _ := countingAuditBridge(t, s)
		w3 := &wire{}
		g3 := newStepPolicyGateWithBoundary(policy.NewStore(repo3), w3,
			runtime.FrozenManifest{Entries: pristine, Hash: recorded, Frozen: true})
		if d3 := g3.Decide(req, at); d3.Allowed || !strings.Contains(d3.Sentence, "no-force-push") {
			t.Errorf("the honest pair did not replay the frozen command deny: %+v", d3)
		}
		if strings.Contains(w3.logLines(), "does not match") {
			t.Errorf("the honest pair was accused of a mismatch: %s", w3.logLines())
		}
	})
}

// TestPolicySubjectBindsToTheCorpusAgent — PL5's binding (T10). The step
// arrives under the dispatch name; the corpus rows are keyed by the agent
// the rewrite declared. A gate that asks under the wrong name is a gate
// that enforces nothing the operator authored — and the default deny makes
// it LOOK enforced while the handler gets blocked and an operator-authored
// ALLOW for the corpus agent sits inert.
func TestPolicySubjectBindsToTheCorpusAgent(t *testing.T) {
	s := recoveryStoreBound(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	boundary := policy.NewStore(newPolicyRepo(s))
	if rej := boundary.Add(ctx, policy.Policy{
		ID:     "allow-read-loop",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "golang-engineer-loop"},
		Action: policy.Allow,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"read","state":"allow"}`),
	}, at); rej != nil {
		t.Fatalf("seed the corpus-agent allow: %s", rej.Reason)
	}
	gate := NewPolicyGate(s, nil)

	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "golang-engineer", Tool: "read"}, at)
	if !dec.Allowed {
		t.Fatalf("the corpus-agent allow never reached its own handler: %q — the gate asked under %q while the row lives under %q (PL5 binding, T10)", dec.Sentence, "golang-engineer", "golang-engineer-loop")
	}
	// The binding is a SUBJECT translation, not a permission: bash is still
	// the default deny for the same handler.
	dec = gate.Decide(runtime.PolicyGateRequest{Agent: "golang-engineer", Tool: "bash"}, at)
	if dec.Allowed || !strings.Contains(dec.Sentence, "default deny (PL5)") {
		t.Errorf("binding widened the grant: %q", dec.Sentence)
	}
	// A name the registry does not know binds to itself — presets and
	// operator-authored agents keep their own rows.
	if got := bindPolicySubject("boss"); got != "boss" {
		t.Errorf("an unregistered agent bound to %q — it must keep its own subject", got)
	}
	if got := bindPolicySubject("default"); got != "default" {
		t.Errorf("the preset bound to %q — presets carry no corpus agent and must ask under their own name", got)
	}
}

// The amended PL5 on the real gate: with permissions.unmatched at its default ("ask"), a tool call or command
// no rule covers is a QUESTION; "deny" restores the strict wall; and a matched rule is never softened.
func TestGateAsksForUnmatchedCallsUnderTheDefaultSetting(t *testing.T) {
	s := recoveryStoreBound(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	boundary := policy.NewStore(newPolicyRepo(s))
	if rej := boundary.Add(ctx, policy.Policy{
		ID: "deny-write", Type: policy.ToolAccess, Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"write","state":"deny"}`),
	}, at); rej != nil {
		t.Fatalf("seed: %s", rej.Reason)
	}
	settings := config.NewStore(newSettingsRepo(s), config.Options{})
	gate := NewPolicyGate(s, nil, WithUnmatchedSetting(settings))

	dec := gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, at)
	if !dec.Asked || dec.Allowed || !strings.Contains(dec.Sentence, "no rule covers this") {
		t.Fatalf("an unmatched tool call must ask under the default setting: %+v", dec)
	}
	dec = gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "write"}, at)
	if dec.Asked || dec.Allowed {
		t.Fatalf("a matched deny must still deny: %+v", dec)
	}
	if _, _, err := settings.Set(ctx, "permissions.unmatched", "deny", "user:1"); err != nil {
		t.Fatal(err)
	}
	dec = gate.Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, at)
	if dec.Asked || dec.Allowed || !strings.Contains(dec.Sentence, "default deny (PL5)") {
		t.Fatalf("permissions.unmatched=deny must restore the strict default: %+v", dec)
	}
	// No settings reader at all: strict, never a silent widening.
	if dec := NewPolicyGate(s, nil).Decide(runtime.PolicyGateRequest{Agent: "boss", Tool: "read"}, at); dec.Asked {
		t.Fatalf("a gate with no settings must keep the strict default: %+v", dec)
	}
}
