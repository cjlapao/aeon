package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// T13 carried D4's sentence to the socket (policy.list's statement). Two
// surfaces, ONE sentence: the const the CLI prints is the const the wire
// carries, pinned here where both are in scope. Drift between the two is
// two different claims about the same empty table.
func TestTheEmptyPolicyListSentenceIsOneSentenceAcrossSurfaces(t *testing.T) {
	if emptyPolicyListLine != protocol.PolicyListEmptyStatement {
		t.Errorf("the CLI prints %q and the socket carries %q — D4 is one sentence, not two",
			emptyPolicyListLine, protocol.PolicyListEmptyStatement)
	}
}

// Note: the TestMain in cli_integration_test.go owns the binary build for this
// package — these tests call the verbs in-process and via that binary. Only one
// TestMain per package is legal, so this file deliberately adds none.

// --- the spec's tests, named verbatim --------------------------------------------

// "an empty list states the default deny" (D4) — the exact sentence, not
// `0 policies`, byte-equal to the pinned constant AND to the spec's string.
func TestAnEmptyListStatesTheDefaultDeny(t *testing.T) {
	const pinned = "no policies. default is DENY (PL5) — nothing is granted until something grants it."
	if emptyPolicyListLine != pinned {
		t.Fatalf("the printed empty-list line drifted from the spec's sentence:\n got %q\nwant %q", emptyPolicyListLine, pinned)
	}

	spy := &spyRepo{}
	withSpy(t, spy)
	out := mustPolicy(t, "list")
	if out != pinned+"\n" {
		t.Errorf("empty list did not print the sentence and nothing else:\n%q", out)
	}

	// And against the REAL temp store's empty table, not only the spy.
	withTempStore(t)
	out = mustPolicy(t, "list")
	if out != pinned+"\n" {
		t.Errorf("empty list on the real temp store printed:\n%q", out)
	}

	// A non-empty list must NOT print it (the line is about emptiness, not
	// a banner).
	spy2 := &spyRepo{rows: []policy.Row{toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, "")}}
	withSpy(t, spy2)
	out = mustPolicy(t, "list")
	if strings.Contains(out, pinned) {
		t.Errorf("the default-deny line printed beside a stored policy:\n%s", out)
	}
}

// "why explains a verdict and changes nothing" — the recorder is not
// reachable from why; the spy is pinned at zero writes.
func TestWhyExplainsAVerdictAndChangesNothing(t *testing.T) {
	spy := &spyRepo{rows: []policy.Row{
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy)

	out, err := runPolicy(t, "why", "--agent", "bob", "--tool", "bash")
	if err != nil {
		t.Fatalf("why failed: %v\n%s", err, out)
	}
	// stdout carries T4's sentence, verbatim, whole.
	want := `refused: agent:bob · tool_access "no-bash" · target agent:bob · source operator · no expiry`
	if !strings.Contains(out, want) {
		t.Errorf("why did not print the verdict sentence verbatim:\ngot:\n%s\nwant substring:\n%s", out, want)
	}
	// ZERO writes: not one authoring, record or housekeeping call. Reads:
	// exactly the one List.
	if spy.calls.add != 0 || spy.calls.remove != 0 || spy.calls.accRecord != 0 ||
		spy.calls.eventInsert != 0 || spy.calls.eventTrim != 0 {
		t.Errorf("why wrote: add=%d remove=%d accRecord=%d eventInsert=%d trim=%d — why must change nothing",
			spy.calls.add, spy.calls.remove, spy.calls.accRecord, spy.calls.eventInsert, spy.calls.eventTrim)
	}
	if spy.calls.listAll != 1 || spy.calls.eventQuery != 0 || spy.calls.eventStats != 0 || spy.calls.accList != 0 {
		t.Errorf("why read the wrong set (listAll=%d eventQuery=%d eventStats=%d accList=%d) — the one read is List",
			spy.calls.listAll, spy.calls.eventQuery, spy.calls.eventStats, spy.calls.accList)
	}
}

// "why --at explores an expiry boundary" — live at T, gone at T+1ns past
// NotAfter; the output changes from the policy-deny sentence to the
// default-deny sentence.
func TestWhyAtExploresAnExpiryBoundary(t *testing.T) {
	const at = "2026-10-15T00:00:00Z"             // the demo §6 instant T
	const past = "2026-10-15T00:00:00.000000001Z" // T + 1ns == NotAfter → expired
	spy := &spyRepo{rows: []policy.Row{
		toolRow("no-fetch-today", "all-agents", "", "deny", `{"toolName":"webfetch","state":"deny"}`, past),
	}}
	withSpy(t, spy)

	live := mustPolicy(t, "why", "--agent", "bob", "--tool", "webfetch", "--at", at)
	if !strings.Contains(live, `"no-fetch-today"`) || strings.Contains(live, "default deny (PL5)") {
		t.Errorf("at T the deny should still stand and name the policy:\n%s", live)
	}

	gone := mustPolicy(t, "why", "--agent", "bob", "--tool", "webfetch", "--at", past)
	if !strings.Contains(gone, "default deny (PL5)") || strings.Contains(gone, `"no-fetch-today"`) {
		t.Errorf("at T+1ns the deny must be GONE and the default take over:\n%s", gone)
	}
	if spy.calls.add != 0 || spy.calls.eventInsert != 0 {
		t.Errorf("exploring the boundary wrote (add=%d insert=%d)", spy.calls.add, spy.calls.eventInsert)
	}

	// A malformed --at is refused as the input error it is — naming the value.
	if _, err := runPolicy(t, "why", "--agent", "bob", "--at", "yesterday"); err == nil {
		t.Error("--at accepted a non-RFC3339 instant")
	}
}

// "why renders the same sentence as the runtime refusal" — one formatter
// (T4), not two (D9): stdout is BYTE-EQUAL to policy.Evaluate(...).Sentence().
// for the same fixture, including the PL4 override clause.
func TestWhyRendersTheSameSentenceAsTheRuntimeRefusal(t *testing.T) {
	spy := &spyRepo{rows: []policy.Row{
		toolRow("grant-bash", "agent", "bob", "allow", `{"toolName":"bash","state":"allow"}`, ""),
		toolRow("no-shell-anywhere", "all-agents", "", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy)

	const atStr = "2026-10-14T12:00:00Z"
	out, err := runPolicy(t, "why", "--agent", "bob", "--tool", "bash", "--at", atStr)
	if err != nil {
		t.Fatalf("why failed: %v\n%s", err, out)
	}

	// The runtime's answer for the same fixture: exactly what T9's gate
	// hands the model — Evaluate(...).Sentence(), printed whole.
	at, perr := time.Parse(time.RFC3339Nano, atStr)
	if perr != nil {
		t.Fatal(perr)
	}
	boundary := policy.NewStore(spy)
	ps, rej := boundary.List(context.Background(), at)
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	v := policy.Evaluate(ps, policy.Request{Type: policy.ToolAccess, Agent: "bob", Tool: "bash"}, at)
	want := v.Sentence()
	if out != want+"\n" {
		t.Errorf("why did not print Sentence() byte-for-byte:\ngot:  %q\nwant: %q", out, want+"\n")
	}
	// The demo §3 half of the claim: the beaten allow is NAMED.
	if !strings.Contains(out, `(an allow exists: "grant-bash"`) || !strings.Contains(out, "denied anyway, PL4") {
		t.Errorf("the PL4 override clause is missing:\n%s", out)
	}
}

// DEF-038 at explanation: `why` asked on identity dimensions the runtime
// never fills says so, instead of explaining as if matching worked.
func TestWhyWarnsOnUnmatchableIdentityFlags(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	out, err := runPolicy(t, "why", "--agent", "bob", "--loop", "nightly", "--tool", "bash")
	if err != nil {
		t.Fatalf("why failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "S18/S20") || !strings.Contains(out, "cannot match a running step today") {
		t.Errorf("why did not warn that loop/lane-type/mcp identities never match:\n%s", out)
	}
	// The warning is ADDITIVE — the sentence still printed (first line).
	if !strings.HasPrefix(out, "refused: loop:nightly ·") {
		t.Errorf("the warning displaced the sentence:\n%s", out)
	}
	// No unmatchable flag, no warning.
	out = mustPolicy(t, "why", "--agent", "bob", "--tool", "bash")
	if strings.Contains(out, "S18/S20") {
		t.Errorf("the warning printed for a plain agent question:\n%s", out)
	}
}

// Obligation 5: unimplemented vs ordinary refusal is branched on the FIELD,
// never on the shared "refused:" prefix — the D10 note rides only the field,
// and an ordinary denial never grows it.
func TestWhyDistinguishesUnimplementedFromDenialsByField(t *testing.T) {
	spy := &spyRepo{rows: []policy.Row{{
		ID: "cap-1", Type: "cost_cap", TargetKind: "agent", TargetID: "bob",
		Action: "allow", Source: "operator", Subject: "agent:bob",
		SpecJSON: `{"token":"input","max":1000}`, CreatedAt: "2026-10-01T00:00:00Z",
	}}}
	withSpy(t, spy)

	// cost_cap's registered evaluator is T5's placeholder: the verdict has
	// Unimplemented TRUE, and the CLI's D10 note prints because of the field.
	out := mustPolicy(t, "why", "--agent", "bob", "--type", "cost_cap", "--cost", "input")
	if !strings.Contains(out, "has no evaluator yet") || !strings.Contains(out, "cannot decide this type yet") {
		t.Errorf("the unimplemented field produced no operator note:\n%s", out)
	}

	// An ordinary denial (Unimplemented false) shares the "refused: " prefix
	// but must NOT grow the D10 note — prefix matching would have.
	spy2 := &spyRepo{rows: []policy.Row{
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy2)
	out = mustPolicy(t, "why", "--agent", "bob", "--tool", "bash")
	if !strings.HasPrefix(out, "refused: ") {
		t.Fatalf("ordinary denials share the prefix (the trap this test guards):\n%s", out)
	}
	if strings.Contains(out, "cannot decide this type yet") {
		t.Errorf("the D10 note printed beside an ordinary refusal (prefix-matched instead of field-branch):\n%s", out)
	}
}

// SEC-050 (T13): the cross-type composition made visible where operators read
// truth. A command_access ALLOW runs bash despite a coexisting tool_access
// deny (the engine's type filter makes the tool rule inert for a
// command-carrying question — documented, S10). `why` now SAYS so, additively,
// beside the command's own sentence.
func TestWhyShowsTheToolDenyACommandAllowOutranks(t *testing.T) {
	cmdAllow := policy.Row{
		ID: "cmd-allow", Type: "command_access", TargetKind: "agent", TargetID: "bob",
		Action: "allow", Source: "operator", Subject: "agent:bob",
		SpecJSON: `{"command":"git push*","state":"allow"}`, CreatedAt: "2026-10-01T00:00:00Z",
	}

	// The composition itself: command allowed, tool denied by a rule.
	spy := &spyRepo{rows: []policy.Row{
		cmdAllow,
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy)
	out := mustPolicy(t, "why", "--agent", "bob", "--type", "command_access", "--tool", "bash", "--command", "git push origin main")
	if !strings.HasPrefix(out, "allowed: agent:bob ·") {
		t.Fatalf("the command question did not answer allow first (the sentence must stay whole and FIRST):\n%s", out)
	}
	if !strings.Contains(out, "SEC-050") || !strings.Contains(out, `"no-bash"`) || !strings.Contains(out, "target agent:bob") {
		t.Errorf("the outranked tool deny was not named (rule id + target):\n%s", out)
	}

	// The three negative controls, each ruling out one toothless version of
	// the note: no coexisting deny ⇒ nothing to see; an ask is not a deny; a
	// refused command never outranks anything.
	spy = &spyRepo{rows: []policy.Row{cmdAllow}}
	withSpy(t, spy)
	out = mustPolicy(t, "why", "--agent", "bob", "--type", "command_access", "--tool", "bash", "--command", "git push origin main")
	if strings.Contains(out, "SEC-050") {
		t.Errorf("the note printed with no tool rule in the set at all:\n%s", out)
	}

	spy = &spyRepo{rows: []policy.Row{
		cmdAllow,
		toolRow("ask-bash", "agent", "bob", "ask", `{"toolName":"bash","state":"ask"}`, ""),
	}}
	withSpy(t, spy)
	out = mustPolicy(t, "why", "--agent", "bob", "--type", "command_access", "--tool", "bash", "--command", "git push origin main")
	if strings.Contains(out, "SEC-050") {
		t.Errorf("the note printed for a coexisting ASK (an ask is not a deny):\n%s", out)
	}

	spy = &spyRepo{rows: []policy.Row{
		{ID: "cmd-deny", Type: "command_access", TargetKind: "agent", TargetID: "bob",
			Action: "deny", Source: "operator", Subject: "agent:bob",
			SpecJSON: `{"command":"git push*","state":"deny"}`, CreatedAt: "2026-10-01T00:00:00Z"},
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy)
	out = mustPolicy(t, "why", "--agent", "bob", "--type", "command_access", "--tool", "bash", "--command", "git push origin main")
	if !strings.HasPrefix(out, "refused: ") {
		t.Fatalf("the denied command did not answer refused:\n%s", out)
	}
	if strings.Contains(out, "SEC-050") {
		t.Errorf("the note printed beside a DENIED command (nothing was outranked):\n%s", out)
	}

	// And the note belongs to the bash question only: --type command_access
	// with no --tool asked a question the runtime would never ask (runTool
	// always carries Tool), and no composition claim is owed to it.
	spy = &spyRepo{rows: []policy.Row{
		cmdAllow,
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy)
	out = mustPolicy(t, "why", "--agent", "bob", "--type", "command_access", "--command", "git push origin main")
	if strings.Contains(out, "SEC-050") {
		t.Errorf("the note printed without --tool bash on the question:\n%s", out)
	}
}

// The same obligation's other edge (SEC-033 class, inverted): a --type OUTSIDE
// the closed set must be refused AT THE FLAG — it is not a policy type at all,
// so it may never reach Evaluate and borrow the seven's Unimplemented answer
// plus its D10 promise ("the answer changes when the slice registering this
// type's evaluator lands") for a registration no slice will ever make. The
// refusal is the boundary's own parser's, one call at flag-parse time — the
// same message `add` gives for the same value, byte-identical by delegation.
func TestWhyRefusesATypeOutsideTheClosedSetAtTheFlag(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	for _, typ := range []string{"nonsense", "tool-access"} {
		out, err := runPolicy(t, "why", "--agent", "bob", "--type", typ)
		if err == nil || !strings.Contains(err.Error(), "unknown policy type") || !strings.Contains(err.Error(), typ) {
			t.Errorf("--type %q was not refused by name: err=%v out=%s", typ, err, out)
		}
		if strings.Contains(out, "has no evaluator yet") || strings.Contains(out, whyUnimplementedNote) {
			t.Errorf("--type %q still got the D10 sentence and note (the false reassurance):\n%s", typ, out)
		}
	}
	// Flag-parse refusal, so the repo was never even asked: a refusal AFTER
	// a List would mean Evaluate still saw the non-member.
	if spy.calls.listAll != 0 {
		t.Errorf("the refused why reached the repo (%d List calls) — the refusal belongs at the flag", spy.calls.listAll)
	}
	// And the closed set itself still answers: a member with a stub evaluator
	// keeps its honest Unimplemented (pinned beside by the cost_cap case) —
	// the flag check refuses non-members only, never the seven.
	mustPolicy(t, "why", "--agent", "bob")
	if spy.calls.listAll != 1 {
		t.Errorf("the default --type (tool_access, a member) was not accepted at the flag: %d List calls", spy.calls.listAll)
	}
}

// D9 for the in-force half: every printed line ENDS with the byte-exact
// InForceAnswer.Render(); the identity columns are additive.
func TestInForcePrintsTheRenderedAnswersVerbatim(t *testing.T) {
	spy := &spyRepo{rows: []policy.Row{
		toolRow("grant-bash", "agent", "bob", "allow", `{"toolName":"bash","state":"allow"}`, ""),
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	at, err := time.Parse(time.RFC3339Nano, "2026-10-10T09:12:04Z")
	if err != nil {
		t.Fatal(err)
	}
	// Only the grant was ever acknowledged — the middle state (still
	// enforced, unacknowledged) must stay visible.
	spy.acceptances = []policy.AcceptanceRow{{
		PolicyID: "grant-bash", TargetKind: "agent", TargetID: "bob",
		AcceptedBy: "bob", AcceptedAt: at.UTC().Format(time.RFC3339Nano),
	}}
	withSpy(t, spy)

	out, rerr := runPolicy(t, "in-force", "--agent", "bob")
	if rerr != nil {
		t.Fatalf("in-force failed: %v\n%s", rerr, out)
	}
	answers, rej := policy.NewStore(spy).InForceFor(context.Background(),
		policy.Target{Kind: policy.AgentKind, ID: "bob"}, time.Now())
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(answers) {
		t.Fatalf("printed %d lines for %d answers:\n%s", len(lines), len(answers), out)
	}
	for i, a := range answers {
		if !strings.HasSuffix(lines[i], a.Render()) {
			t.Errorf("line %d does not end with Render() byte-for-byte:\ngot:  %s\nwant: %s", i, lines[i], a.Render())
		}
	}
	if !strings.Contains(out, "unacknowledged — still enforced (PL10)") {
		t.Errorf("the unacknowledged state (the valuable middle) is not visible:\n%s", out)
	}
	if !strings.Contains(out, "acknowledged by agent:bob since 2026-10-10T09:12:04Z") {
		t.Errorf("the acknowledged state lost its since-when:\n%s", out)
	}

	// Flags: one target per answer (input hygiene, not policy validation).
	if _, err := runPolicy(t, "in-force"); err == nil {
		t.Error("in-force accepted no target")
	}
	if _, err := runPolicy(t, "in-force", "--agent", "bob", "--loop", "nightly"); err == nil {
		t.Error("in-force accepted two targets")
	}
	// Empty answer is a statement, not silence.
	withSpy(t, &spyRepo{})
	out = mustPolicy(t, "in-force", "--agent", "nobody")
	if !strings.Contains(out, "nothing governs") || !strings.Contains(out, "default deny (PL5)") {
		t.Errorf("the empty in-force answer read as absence:\n%s", out)
	}
}

// list surfaces every column T6 stores, and STATES no expiry (PL7).
func TestListPrintsEveryColumnAndStatesNoExpiry(t *testing.T) {
	expiry := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	spy := &spyRepo{rows: []policy.Row{
		toolRow("no-bash", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
		{ID: "wide-til-nov", Type: "tool_access", TargetKind: "all-agents", Action: "deny",
			Source: "import", Subject: "all-agents", SpecJSON: `{"toolName":"webfetch","state":"deny"}`,
			NotAfter: expiry.Format(time.RFC3339Nano), CreatedAt: "2026-10-01T00:00:00Z"},
	}}
	withSpy(t, spy)
	out := mustPolicy(t, "list")
	for _, want := range []string{
		"no-bash", "tool_access", "agent:bob", "deny", "operator", "no expiry",
		"wide-til-nov", "all-agents", "import", "2026-11-01T00:00:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list omitted %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, emptyPolicyListLine) {
		t.Errorf("the default-deny line printed beside rows:\n%s", out)
	}
}

// TestPolicyWhyAgreesWithTheGateForBothWriteSpellings kills the QA report's
// line-37 repro (SEC-059): `policy why --tool write_file` claimed an
// alias-authored deny fired while the gate allowed the very same call —
// why ≠ enforcement, because the row side of the comparison was literal and
// the why surfaces pass --tool/tool through RAW. Rows here are authored
// through Store.Add, the real write boundary (the same door `policy add`
// opens, PL15); both spellings of the request must render byte-identical
// output naming the row that decides, because the single Match the CLI, the
// socket and the gate share now judges canonical names on both sides.
func TestPolicyWhyAgreesWithTheGateForBothWriteSpellings(t *testing.T) {
	addAliasRow := func(t *testing.T, id, tool string, action policy.Action) {
		t.Helper()
		boundary, closeFn, err := policyBoundary()
		if err != nil {
			t.Fatalf("open the boundary: %v", err)
		}
		defer closeFn()
		p := policy.Policy{
			ID: id, Type: policy.ToolAccess,
			Target: policy.Target{Kind: policy.AgentKind, ID: "main"},
			Action: action, Source: policy.Operator,
			Spec: json.RawMessage(`{"toolName":"` + tool + `","state":"` + string(action) + `"}`),
		}
		instant := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
		if rej := boundary.Add(context.Background(), p, instant); rej != nil {
			t.Fatalf("seeding %s: %s", id, rej.Reason)
		}
	}

	why := func(tool string) (string, error) {
		var b strings.Builder
		err := cmdPolicyWhy(&b, []string{"--agent", "main", "--type", "tool_access", "--tool", tool})
		return b.String(), err
	}

	t.Run("a deny authored under the alias explains itself identically both ways", func(t *testing.T) {
		withTempStore(t)
		addAliasRow(t, "no-writes-alias", "write_file", policy.Deny)
		alias, errA := why("write_file")
		honest, errH := why("write")
		if errA != nil || errH != nil {
			t.Fatalf("why must answer both spellings, not refuse either: %v / %v", errA, errH)
		}
		if alias != honest {
			t.Errorf("the two spellings rendered different bytes — why still reads the spelling, not the tool:\n--tool write_file: %q\n--tool write:      %q", alias, honest)
		}
		if !strings.Contains(alias, "refused") || !strings.Contains(alias, `"no-writes-alias"`) {
			t.Errorf("the sentence must name the alias-authored deny that now decides: %q", alias)
		}
	})

	t.Run("an allow authored under the alias grants both spellings alike", func(t *testing.T) {
		withTempStore(t)
		addAliasRow(t, "yes-write-alias", "write_file", policy.Allow)
		alias, errA := why("write_file")
		honest, errH := why("write")
		if errA != nil || errH != nil {
			t.Fatalf("why must answer both spellings, not refuse either: %v / %v", errA, errH)
		}
		if alias != honest {
			t.Errorf("the two spellings rendered different bytes:\n--tool write_file: %q\n--tool write:      %q", alias, honest)
		}
		if !strings.HasPrefix(alias, "allowed:") || !strings.Contains(alias, `"yes-write-alias"`) {
			t.Errorf("the sentence must be the allow naming the legacy row: %q", alias)
		}
	})
}
