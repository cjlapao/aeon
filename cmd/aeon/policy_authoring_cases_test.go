package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// "add refuses a non-operator Allow, with the source named" — and the
// refusal is T6's verbatim, not a second formulation.
func TestAddRefusesANonOperatorAllowWithTheSourceNamed(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)

	args := []string{"add", "--type", "tool_access", "--id", "reddit-self-grant",
		"--target", "agent:reddit-agent", "--action", "allow", "--source", "agent",
		"--spec", `{"toolName":"bash","state":"allow"}`}
	out, err := runPolicy(t, args...)
	if err == nil {
		t.Fatalf("a non-operator Allow was stored:\n%s", out)
	}
	if !strings.Contains(err.Error(), `source "agent"`) {
		t.Errorf("the refusal does not name the source:\n%v", err)
	}
	if !strings.Contains(err.Error(), "PL1") || !strings.Contains(err.Error(), "Deny") {
		t.Errorf("the refusal does not name PL1 and the Deny-would-be-accepted alternative:\n%v", err)
	}
	if spy.calls.add != 0 {
		t.Errorf("the refused row reached the repo (%d adds)", spy.calls.add)
	}

	// One place (D3): the CLI's printed refusal is BYTE-EQUAL to what
	// policy.Store.Add hands back when called directly with the same policy.
	direct := policy.NewStore(&spyRepo{})
	p := policy.Policy{
		ID: "reddit-self-grant", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "reddit-agent"},
		Action: policy.Allow, Source: policy.Agent,
		Spec: []byte(`{"toolName":"bash","state":"allow"}`),
	}
	rej := direct.Add(context.Background(), p, time.Now())
	if rej == nil {
		t.Fatal("the boundary itself accepted a non-operator Allow")
	}
	if err.Error() != rej.Reason {
		t.Errorf("the CLI reformulated the refusal (a second voice):\nCLI: %q\nT6:  %q", err.Error(), rej.Reason)
	}

	// The same policy as a DENY from agent is ACCEPTED — the demo §5 pair.
	if out, err := runPolicy(t, "add", "--type", "tool_access", "--id", "reddit-self-restraint",
		"--target", "agent:reddit-agent", "--action", "deny", "--source", "agent",
		"--spec", `{"toolName":"webfetch","state":"deny"}`); err != nil {
		t.Errorf("an agent's self-denial was refused (PL1 says a Deny is safe from anyone):\n%s\n%v", out, err)
	}
}

// "add does not re-validate" — the authorship rule and the validator run
// inside T6 exactly once; the CLI adds no check of its own.
func TestAddDoesNotRevalidate(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)

	// One accepted add = exactly ONE repo round-trip and nothing else. The
	// validator (T5) and the authorship rule (T6) run inside boundary.Add on
	// the way to that one write; the CLI pre-checked nothing (zero reads).
	if _, err := runPolicy(t, "add", "--type", "tool_access", "--id", "grant",
		"--target", "agent:bob", "--action", "allow",
		"--spec", `{"toolName":"bash","state":"allow"}`); err != nil {
		t.Fatalf("a legal operator add was refused: %v", err)
	}
	if spy.calls.add != 1 {
		t.Errorf("the repo saw %d writes for one add — the write boundary must be crossed exactly once", spy.calls.add)
	}
	for name, n := range map[string]int{
		"remove": spy.calls.remove, "accRecord": spy.calls.accRecord,
		"eventInsert": spy.calls.eventInsert, "trim": spy.calls.eventTrim,
		"listAll": spy.calls.listAll,
	} {
		if n != 0 {
			t.Errorf("add made an extra repo round-trip (%s=%d) — a second pass is a second validator with extra steps", name, n)
		}
	}

	// Structurally: the CLI's own source cannot re-validate. It must not
	// reference the boundary's gate machinery at all. ParsePolicyType is
	// NOT banned: `why` makes ONE call to it at flag-parse time to refuse a
	// --type that is not a policy type at all — delegation to the boundary's
	// single parser (input parsing, the header's rule), and banning the
	// identifier here would box this file out of its own closed set.
	src, err := os.ReadFile("policy.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{
		"ValidatorFor", "ParseTarget", "ParseAction", "ParseSource",
		"requires an operator", "checkIdentifier",
	} {
		if strings.Contains(string(src), banned) {
			t.Errorf("policy.go references %q — that rule lives in internal/policy/store.go and a second call site is a second drift place", banned)
		}
	}
}

// DEF-038 at authoring: rules on kinds no runtime decision carries until
// S18/S20 store with an explicit cannot-match-yet warning — the operator
// learns the gap when they write the rule, not after trusting it.
func TestAddWarnsOnUnmatchableTargetKinds(t *testing.T) {
	for _, tc := range []struct{ target, kind string }{
		{"loop:nightly", "loop"},
		{"lane-type:review", "lane-type"},
		{"mcp:github-reviewer", "mcp"},
	} {
		spy := &spyRepo{}
		withSpy(t, spy)
		out, err := runPolicy(t, "add", "--type", "tool_access", "--id", "wide-rule",
			"--target", tc.target, "--action", "deny",
			"--spec", `{"toolName":"bash","state":"deny"}`)
		if err != nil {
			t.Fatalf("%s: legal deny was refused: %v\n%s", tc.target, err, out)
		}
		if !strings.Contains(out, "cannot match yet") || !strings.Contains(out, tc.kind) {
			t.Errorf("no unmatchable-target warning for %s:\n%s", tc.target, out)
		}
	}
	// agent and all-agents DO match today — no warning may ride them.
	spy := &spyRepo{}
	withSpy(t, spy)
	out, err := runPolicy(t, "add", "--type", "tool_access", "--id", "tight-rule",
		"--target", "agent:bob", "--action", "deny", "--spec", `{"toolName":"bash","state":"deny"}`)
	if err != nil {
		t.Fatalf("agent-target deny refused: %v", err)
	}
	if strings.Contains(out, "cannot match yet") {
		t.Errorf("an agent-target rule was warned about (agent identity IS carried today):\n%s", out)
	}
}

// remove says whether a row existed — both answers, and the flag hygiene.
func TestRemoveReportsWhetherARowExisted(t *testing.T) {
	spy := &spyRepo{rows: []policy.Row{
		toolRow("doomed", "agent", "bob", "deny", `{"toolName":"bash","state":"deny"}`, ""),
	}}
	withSpy(t, spy)
	out := mustPolicy(t, "remove", "--id", "doomed")
	if !strings.Contains(out, "removed") {
		t.Errorf("an existing removal did not say so:\n%s", out)
	}
	out = mustPolicy(t, "remove", "--id", "never-existed")
	if !strings.Contains(out, "no policy called") {
		t.Errorf("a missing row did not say so (silence reads as success either way):\n%s", out)
	}
	if spy.calls.remove != 2 {
		t.Errorf("remove hit the repo %d times for two calls", spy.calls.remove)
	}
}

func TestSpecSugarSetsOnlyToolName(t *testing.T) {
	// --tool is the spec's toolName sugar; state is NOT defaulted (T5 refuses
	// a defaulted state — a policy whose state follows its action by magic is
	// the typo class the validator exists for).
	spy := &spyRepo{}
	withSpy(t, spy)
	if _, err := runPolicy(t, "add", "--type", "tool_access", "--id", "s", "--target", "agent:bob",
		"--action", "deny", "--tool", "bash"); err == nil {
		t.Error("--tool without a state was accepted (the validator must refuse it)")
	} else if !strings.Contains(err.Error(), "state") {
		t.Errorf("the refusal should quote the missing field's shape, T5-style:\n%v", err)
	}
	// --spec carries toolName itself (the demo's shape) — no --tool needed.
	if _, err := runPolicy(t, "add", "--type", "tool_access", "--id", "s2", "--target", "agent:bob",
		"--action", "deny", "--spec", `{"toolName":"bash","state":"deny"}`); err != nil {
		t.Errorf("the demo's --spec shape was refused: %v", err)
	}
	// --tool overrides the spec's toolName (last explicit word wins).
	if _, err := runPolicy(t, "add", "--type", "tool_access", "--id", "s3", "--target", "agent:bob",
		"--action", "deny", "--spec", `{"toolName":"webfetch","state":"deny"}`, "--tool", "bash"); err != nil {
		t.Errorf("--tool over --spec toolName was refused: %v", err)
	}
	if spy.calls.add != 2 {
		t.Fatalf("expected 2 stored rows, got %d", spy.calls.add)
	}
	if !strings.Contains(spy.rows[1].SpecJSON, `"toolName":"bash"`) {
		t.Errorf("--tool did not win the toolName field: %s", spy.rows[1].SpecJSON)
	}
	// An unparseable --spec is an INPUT error naming the value — not a
	// policy judgement.
	_, err := runPolicy(t, "add", "--type", "tool_access", "--id", "s4", "--target", "agent:bob",
		"--action", "deny", "--spec", "not json")
	if err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Errorf("unparseable --spec escaped as something else: %v", err)
	}
	// JSON `null` decodes into a map with NO error (leaving it nil), so it
	// escapes the unmarshal check — it must be refused like every other JSON
	// non-object, NOT reach the --tool write and abort the process with a
	// nil-map panic. Both shapes: null alone, and null plus the sugar.
	for _, nullish := range [][]string{
		{"--id", "s5", "--spec", "null"},
		{"--id", "s6", "--spec", "null", "--tool", "bash"},
	} {
		args := append([]string{"add", "--type", "tool_access", "--target", "agent:bob", "--action", "deny"}, nullish...)
		if _, nerr := runPolicy(t, args...); nerr == nil || !strings.Contains(nerr.Error(), "not a JSON object") {
			t.Errorf("--spec null escaped as something else (a panic or a policy judgement): %v", nerr)
		}
	}
	if spy.calls.add != 2 {
		t.Errorf("the repo saw %d writes; the null refusals are flag-parse refusals and must not reach the boundary", spy.calls.add)
	}
}

// TestAddRejectsAMalformedExpiryBeforeReachingTheBoundary: --not-after is a
// flag contract (an instant the boundary could never store), so its refusal
// is the CLI's own input error and happens before any boundary call.
func TestAddRejectsAMalformedExpiryBeforeReachingTheBoundary(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	_, err := runPolicy(t, "add", "--type", "tool_access", "--id", "x",
		"--target", "agent:bob", "--action", "deny", "--spec", `{"toolName":"bash","state":"deny"}`,
		"--not-after", "tomorrow-evening")
	if err == nil || !strings.Contains(err.Error(), "--not-after") {
		t.Fatalf("malformed --not-after escaped as something else: %v", err)
	}
	if spy.calls.add != 0 {
		t.Errorf("a malformed expiry still reached the write boundary %d time(s)", spy.calls.add)
	}
}

// TestAddWithoutSpecOrToolLetsTheBoundaryJudgeAlone: the CLI does not decide
// that an empty spec is meaningless — it hands {} to the boundary and prints
// T5's judgement (the single validator, again: the CLI pre-checks nothing).
func TestAddWithoutSpecOrToolLetsTheBoundaryJudgeAlone(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	_, err := runPolicy(t, "add", "--type", "tool_access", "--id", "x",
		"--target", "agent:bob", "--action", "deny")
	if err == nil {
		t.Fatal("the boundary accepted a tool_access policy with no toolName — T5's validator is gone")
	}
	if spy.calls.add != 0 {
		t.Errorf("the refused empty-spec policy still reached storage %d time(s)", spy.calls.add)
	}
}

// TestStoreFailuresSurfaceAsRefusalsNotGuesses: the run* layer never turns a
// failed read into a zero answer — list/why/in-force/events/remove print the
// boundary's reason and fail (D4/P31: an unreadable guard is not an empty
// guard). The spy's read hooks fail; the verbs must follow.
func TestStoreFailuresSurfaceAsRefusalsNotGuesses(t *testing.T) {
	spy := &spyRepo{listErr: errors.New("read failed: disk on fire")}
	withSpy(t, spy)
	for _, args := range [][]string{
		{"list"},
		{"why", "--agent", "bob", "--tool", "bash"},
		{"in-force", "--agent", "bob"},
		{"in-force", "--loop", "my-loop"},
	} {
		out, err := runPolicy(t, args...)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Errorf("policy %v over a failing read: err=%v\n%s", args, err, out)
		}
		if strings.Contains(out, emptyPolicyListLine) || strings.Contains(out, "depth 0") {
			t.Errorf("policy %v printed a confident zero over a failed read:\n%s", args, out)
		}
	}

	rspy := &spyRepo{removeErr: errors.New("remove refused: lock")}
	withSpy(t, rspy)
	if _, err := runPolicy(t, "remove", "--id", "x"); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Errorf("remove swallowed its store failure: %v", err)
	}

	esp := &spyRepo{queryErr: errors.New("events unreadable")}
	withSpy(t, esp)
	out, err := runPolicy(t, "events")
	if err == nil || !strings.Contains(err.Error(), "events unreadable") {
		t.Errorf("events swallowed its store failure: %v\n%s", err, out)
	}
}
