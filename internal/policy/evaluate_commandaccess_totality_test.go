package policy_test

// Totality of the CommandAccess engine, swept rather than sampled (PL6).
//
// Split out of evaluate_commandaccess_test.go by the file-length rule, not by
// a difference of subject: the corpus-as-specification suite and this sweep are
// the same claim seen from two ends — every rule the corpus carries decides the
// right way (that file), and nothing at all — malformed, out-of-vocabulary, or
// 10,000 rows deep — ever fails to decide (this one).
//
// ════════════════════════════════════════════════════════════════════════════
// TOTALITY: NO COMMAND POLICY PANICS
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── totality: no command policy panics ───────────────────────────────────────

func TestNoCommandPolicyPanics(t *testing.T) {
	// Fuzz-style total-input sweep over everything the store can hand back and
	// everything a hand-rolled path could build, evaluated over every
	// adversarial command. Deterministic rather than a `Fuzz*` target on
	// purpose: the shape space here is a bounded product the corpus and these
	// tables enumerate exhaustively, and a seed corpus that drifts from the
	// corpus would be a second truth (pattern_test.go's FuzzMatchPattern is the
	// place random bytes meet the matcher). Every call runs under recover and
	// names its own inputs, so a panic is a red line, not a red process.
	//
	// The invariants checked on EVERY verdict:
	//
	//   - nothing panics — a panic here is a daemon that stops (PL6);
	//   - a permission is never unnamed: Allowed implies By is a CommandAccess
	//     allow of the request's own type;
	//   - DefaultDeny and Unimplemented are never both true, and Reason is
	//     non-empty exactly on the unimplemented path (prose belongs to those
	//     two refusals; everywhere else it is verdict.go's);
	//   - Subject and Type are the request's on every return path;
	//   - CommandAccess NEVER answers Unimplemented — the placeholder is gone
	//     and no row shape resurrects it;
	//   - OverriddenAllow implies a named deny that beat it;
	//   - the same inputs give the same verdict (D5's purity, checked by
	//     re-running rather than by reading the source).
	specs := []json.RawMessage{
		nil, json.RawMessage(``), json.RawMessage(`   `),
		json.RawMessage(`{"command":"git push*",`), json.RawMessage(`{"command":`),
		json.RawMessage(`null`), json.RawMessage(`42`), json.RawMessage(`"git push"`),
		json.RawMessage(`["git push","git pull"]`),
		json.RawMessage(`{"command":"git push*","state":"deny","segment":2}`),
		json.RawMessage(`{"cmd":"git push*","state":"deny"}`),
		json.RawMessage(`{"command":"pwd","state":"deny"}{"command":"ls","state":"deny"}`),
		json.RawMessage(`{"command":"pwd","state":"allow","state":"deny"}`),
		json.RawMessage(`{"secret":"hunter2-in-a-spec"}`),
		json.RawMessage(`{"command":"","state":"allow"}`),
		json.RawMessage(`{"command":"   ","state":"deny"}`),
		json.RawMessage(`{"command":"**","state":"deny"}`),
		json.RawMessage(`{"command":" git push","state":"deny"}`),
		json.RawMessage(`{"command":"git push ","state":"deny"}`),
		json.RawMessage(`{"command":"` + strings.Repeat("*", 400) + `","state":"deny"}`),
		json.RawMessage(`{"command":"\u0000\u00ff","state":"deny"}`),
		json.RawMessage(`{"command":"git push*","state":"ASK"}`),
		json.RawMessage(`{"command":"git push*","state":"ask"}`),
		json.RawMessage(`{"command":{"nested":"deep"},"state":"deny"}`),
	}
	actions := []policy.Action{policy.Allow, policy.Deny, policy.Action("ask"), policy.Action("permit"),
		policy.Action(""), policy.Action("ALLOW"), policy.Action("deny ")}
	targets := []policy.Target{
		{Kind: policy.AgentKind, ID: "main"},
		{Kind: policy.AgentKind},                     // names nobody: unconstructable via ParseTarget
		{Kind: policy.AllAgents},                     //
		{Kind: policy.TargetKind("global"), ID: "x"}, // a stale row from an older vocabulary
		{Kind: policy.LoopKind, ID: "l1"},
		{Kind: policy.LaneTypeKind, ID: "review"},
	}
	dead := evalAt.Add(-time.Hour)
	alive := evalAt.Add(time.Hour)
	expiries := []*time.Time{nil, &dead, &alive}

	overLimit := strings.Join(append(repeat("git push", policy.MaxSegments+1), "echo done"), " && ")
	commands := []string{
		"", " ", ";", " && ", "\n", "\t", ";;", "&&",
		"git push origin main", "cd sub && git push origin", "ls; git push", "true || git push",
		"echo a & b | c", "git commit -m 'x; y'", "rm -rf /", "bash -c \"git push\"",
		"\x00\xff", "é", strings.Repeat("a", 5000), strings.Repeat("*", 2000), overLimit,
	}

	var rows []policy.Policy
	for si, spec := range specs {
		for ai, action := range actions {
			for ti, target := range targets {
				for ei, notAfter := range expiries {
					p := policy.Policy{
						ID: fmt.Sprintf("panic-sweep/%d-%d-%d-%d", si, ai, ti, ei), Type: policy.CommandAccess,
						Target: target, Action: action, Spec: spec, Source: policy.Operator,
					}
					if notAfter != nil {
						instant := *notAfter
						p.NotAfter = &instant
					}
					rows = append(rows, p)
				}
			}
		}
	}
	if len(rows) == 0 {
		t.Fatal("the sweep built no rows; it would pass vacuously")
	}

	requests := func(command string) []policy.Request {
		return []policy.Request{
			{Type: policy.CommandAccess, Agent: "main", Command: command},
			{Type: policy.CommandAccess, Agent: "main", Loop: "l1", Command: command},
			{Type: policy.CommandAccess, Command: command}, // agent-less
			{Type: policy.ToolAccess, Agent: "main", Tool: "bash", Command: command},
			{}, // the zero request: a type nobody typed
		}
	}

	// The foreign-type row, so the sweep also sees another type's rules mixed
	// into a CommandAccess question (they are inert, and must stay inert).
	foreign := policy.Policy{
		ID: "foreign-tool-access", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "main"}, Action: policy.Allow,
		Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`), Source: policy.Operator,
	}

	// Three shapes of policy set, in the order a real caller's sets get bigger:
	// nothing (with another type's row in it, so "empty for this question" is
	// tested rather than "empty"), one row, the whole malformed table at once,
	// and a pathological 10,000-row set — the case the seed design's fuzz list
	// names, kept because a guard that is only sound for small inputs is a
	// guard that gets bypassed by an input.
	whole := rows
	huge := make([]policy.Policy, 0, 10000)
	for i := range 10000 {
		huge = append(huge, policy.Policy{
			ID: fmt.Sprintf("huge/%05d", i), Type: policy.CommandAccess,
			Target: policy.Target{Kind: policy.AgentKind, ID: "main"},
			Action: policy.Deny, Spec: json.RawMessage(`{"command":"git push*","state":"deny"}`),
			Source: policy.Operator,
		})
	}
	sets := [][]policy.Policy{{foreign}, whole, huge}

	calls := 0
	// The wide sweep: every adversarial command × every request shape × the
	// three set shapes.
	for _, command := range commands {
		for _, req := range requests(command) {
			for _, set := range sets {
				checkVerdict(t, &calls, set, req, command)
			}
		}
	}
	// The deep sweep: every row alone — the shape a corrupt single row arrives
	// in — against the commands that exercise the matcher, the splitter and the
	// inertness arms. One request shape, because the wide sweep already covers
	// the request space and this is where the row space gets its turn.
	soloCommands := []string{"", " ", ";", "git push origin main", "cd sub && git push origin",
		"rm -rf /", "\x00\xff", overLimit}
	soloReq := policy.Request{Type: policy.CommandAccess, Agent: "main", Command: ""}
	for i := range rows {
		for _, command := range soloCommands {
			soloReq.Command = command
			checkVerdict(t, &calls, []policy.Policy{rows[i]}, soloReq, command)
		}
	}
	if calls < 10000 {
		t.Errorf("only %d calls ran; the sweep is too narrow to be called total", calls)
	}
	t.Logf("totality sweep: %d Evaluate calls over %d rows, %d commands, no panic", calls, len(rows), len(commands))
}

// checkVerdict runs one Evaluate under recover and holds the answer to every
// invariant in the docblock above. It names the full input on any failure: a
// red line that cannot be reproduced is a red line nobody fixes.
func checkVerdict(tb testing.TB, calls *int, ps []policy.Policy, req policy.Request, command string) {
	tb.Helper()
	*calls++
	var v policy.Verdict
	func() {
		defer func() {
			if r := recover(); r != nil {
				tb.Errorf("Evaluate PANICKED on type %q agent %q command %q (%d rows): %v",
					req.Type, req.Agent, truncate(command), len(ps), r)
			}
		}()
		v = policy.Evaluate(ps, req, evalAt)
	}()

	shape := shapeOf(v)
	if v.Allowed && (v.By == nil || v.By.Type != req.Type || v.By.Action != policy.Allow) {
		tb.Errorf("PERMITTED without a matching allow of the request's own type: type %q command %q → %+v "+
			"(PL1/PL5: a permission nobody can name is not a permission)", req.Type, truncate(command), shape)
	}
	if v.OverriddenAllow != nil && (v.Allowed || v.By == nil || v.OverriddenAllow.Action != policy.Allow) {
		tb.Errorf("OverriddenAllow=%q on a verdict that is not a named denial: %+v", v.OverriddenAllow.ID, shape)
	}
	if v.Allowed && v.DefaultDeny {
		tb.Errorf("a verdict claims both permitted and default-denied: %+v", shape)
	}
	if v.DefaultDeny && v.Unimplemented {
		tb.Errorf("a verdict claims both default-deny and could-not-determine: %+v (T11 renders these apart)", shape)
	}
	if v.Unimplemented && strings.TrimSpace(v.Reason) == "" {
		tb.Errorf("an unimplemented verdict carries no Reason: %+v", shape)
	}
	if !v.Unimplemented && v.Reason != "" {
		tb.Errorf("Reason %q on a decided verdict: prose belongs to verdict.go", v.Reason)
	}
	if want := req.Subject(); v.Subject != want {
		tb.Errorf("Subject = %q, want %q: the sentence must say who it refused without the caller re-supplying "+
			"a request (command %q)", v.Subject, want, truncate(command))
	}
	if v.Type != req.Type {
		tb.Errorf("Type = %q, want %q", v.Type, req.Type)
	}
	if req.Type == policy.CommandAccess && v.Unimplemented {
		tb.Errorf("CommandAccess refused to decide on command %q over %d rows: %+v — this type decides now, "+
			"and could-not-determine is the placeholder's answer, which S11 replaced", truncate(command), len(ps), shape)
	}

	// Determinism (D5): the same inputs, the same answer.
	again := policy.Evaluate(ps, req, evalAt)
	if shapeOf(again) != shape || again.Reason != v.Reason || again.Subject != v.Subject || again.Type != v.Type {
		tb.Errorf("Evaluate is not determined on type %q command %q: %+v then %+v",
			req.Type, truncate(command), shape, shapeOf(again))
	}
}

// truncate keeps a failure line readable: the adversarial commands include
// 5000-byte strings, and a log line is not where they belong.
func truncate(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[:60] + fmt.Sprintf("…(%d bytes)", len(s))
}
