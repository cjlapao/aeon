package daemon

// S11 T13 — the unforgeable token at the real composition point. The policy
// package's tests prove the minting, the tools package's prove the gate at
// Spawn, and the forge tests prove unforgeability from outside; THIS file is
// the adapter-level half the plan demands, on the DISPATCHED step (T12's
// harness shape): the verdict→receipt→spawn order (G15/QAP-09) lands on
// disk, the spawn key answers from the step's frozen-or-live pair exactly as
// the gate does, and the SEC-054 re-check stands — no file on the token path
// reads a question's Kind.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// permissiveSeam is the harness answer for tests that own their policy
// elsewhere and just need bash to be able to run: ONE operator allow over
// `*` through the REAL evaluator (the corpus's own broadest legal shape —
// spec_commandaccess says so out loud, and the ban on fabricated tokens
// holds in test code too). Tests that assert the policy itself go through
// CommandVerdictFor over the seeded store.
func permissiveSeam(tb testing.TB) func(string, runtime.FrozenManifest, string) (policy.Verdict, bool) {
	tb.Helper()
	ps := []policy.Policy{{
		ID: "test-allow-star", Type: policy.CommandAccess,
		Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"command":"*","state":"allow"}`),
	}}
	return func(agent string, _ runtime.FrozenManifest, command string) (policy.Verdict, bool) {
		return policy.Evaluate(ps, policy.Request{
			Type: policy.CommandAccess, Agent: agent, Tool: "bash", Command: command,
		}, time.Now()), true
	}
}

// t13Store opens the store the way the daemon-facing tests always do.
func t13Store(t *testing.T, dir string) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// dispatchBashStep dispatches ONE `step` with bash and question granted
// whose scripted model runs the given turns. The daemon is built by New over
// a real store, so the gate, the receipt writer AND the T13 spawn-key seam
// are all the production ones.
func dispatchBashStep(t *testing.T, s *store.Store, root string, turns []modelTurn) (protocol.StepResult, *store.Store) {
	t.Helper()
	if _, err := s.CreateLoop(store.NewLoop{
		Slug: "token-adapter", ComponentRoot: root, Title: "token adapter", Goal: "T13",
	}); err != nil && !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatal(err)
	}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "test", MaxOutputTokens: 1024}
	d := New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Models: &scriptedModel{turns: turns}})
	initConn(t, d, context.Background(), "c1", "test")
	raw, err := json.Marshal(protocol.StepParams{
		Prompt: "run it", Tools: []string{"bash", "question"}, WorkspaceRoot: root, LoopSlug: "token-adapter",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodStep, raw)
	if err != nil {
		t.Fatalf("dispatch step: %v", err)
	}
	res, ok := response.(protocol.StepResult)
	if !ok {
		t.Fatalf("dispatch returned %T", response)
	}
	return res, s
}

// bashTurns is the ordinary two-turn shape: the bash call, then the handoff.
func bashTurns(command string) []modelTurn {
	return []modelTurn{
		{toolUses: []model.Block{toolUse("b1", "bash", `{"command":"`+command+`"}`)}},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```"},
	}
}

func TestDispatchedDeniedBashGetsNoReceiptAndNoSpawn(t *testing.T) {
	// G15/QAP-09, end to end on the dispatched step. A denied command gets
	// NEITHER a receipt NOR a spawn: the receipt means "this may have
	// started" (S7's K31), so one for a refused command turns a clean
	// denial into an unknown that recovery must fail open on. The gate
	// short-circuits before BeginReceipt; the assertion is on the ROWS —
	// the receipt table is empty — and on the file the command would have
	// created. The model still gets the verdict sentence as its tool_result
	// (the one formatter, verbatim), so this is a refusal it can act on.
	dir := t.TempDir()
	root := t.TempDir()
	s := t13Store(t, dir)
	const command = "touch marker.txt"
	seedRules(t, s, time.Now().UTC(),
		commandRulePolicy("deny-marker", runtime.DefaultHandlerName, "touch marker.txt", policy.Deny))

	res, _ := dispatchBashStep(t, s, root, bashTurns(command))
	if res.Kind != "complete" {
		t.Fatalf("the denied step did not complete (denials are completions, not failures): %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("the denied command spawned — its file exists")
	}
	receipts, err := s.Receipts(context.Background(), res.StepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 0 {
		t.Fatalf("%d receipts for a denied command (G15): %+v", len(receipts), receipts)
	}
	// The model was told WHY, in the engine's own words, not "permission
	// denied" — the sentence is the tool_result the transcript carries.
	want := policy.Evaluate([]policy.Policy{
		commandRulePolicy("deny-marker", runtime.DefaultHandlerName, "touch marker.txt", policy.Deny),
	}, policy.Request{Type: policy.CommandAccess, Agent: runtime.DefaultHandlerName, Tool: "bash", Command: command},
		time.Now().UTC()).Sentence()
	if !strings.Contains(dumpTranscript(t, s, res.StepID), want) {
		t.Errorf("the transcript did not carry the verdict sentence verbatim:\nwant: %s", want)
	}
}

// dumpTranscript flattens the stored transcript to text for content probes.
func dumpTranscript(t *testing.T, s *store.Store, stepID int64) string {
	t.Helper()
	tr := s.ReadTranscript(stepID)
	if !tr.OK {
		t.Fatalf("the transcript did not read back (%d messages): %s", tr.Count, tr.Reason())
	}
	var b strings.Builder
	for _, m := range tr.Messages {
		for _, blk := range m.Content {
			b.WriteString(blk.Text)
			b.WriteString(blk.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func TestDispatchedAllowedBashSpawnsAndTheReceiptRecordsBothHalves(t *testing.T) {
	// The control that makes the denial test mean something, and the POSITIVE
	// half of verdict→receipt→spawn: with the live store allowing the exact
	// command, the dispatched step creates the file and carries exactly ONE
	// receipt — begun by the runtime before the spawn, completed after it,
	// bound to the block by its tool_use id.
	dir := t.TempDir()
	root := t.TempDir()
	s := t13Store(t, dir)
	// S59 T5: a step may only run a command in a bound environment (EV2).
	bindStepEnvironment(t, s)
	const command = "touch marker.txt"
	seedRules(t, s, time.Now().UTC(),
		commandRulePolicy("allow-marker", runtime.DefaultHandlerName, "touch marker.txt", policy.Allow))

	res, _ := dispatchBashStep(t, s, root, bashTurns(command))
	if res.Kind != "complete" {
		t.Fatalf("the allowed step did not complete: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err != nil {
		t.Fatal("the allowed command did not create its file")
	}
	receipts, err := s.Receipts(context.Background(), res.StepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatalf("%d receipts, want exactly one for the allowed spawn", len(receipts))
	}
	r := receipts[0]
	if r.Tool != "bash" || r.ToolUseID != "b1" || r.StartedAt == "" || r.ArgDigest == "" {
		t.Errorf("the receipt is not the spawn's: %+v", r)
	}
	if r.CompletedAt == nil {
		t.Error("the receipt never completed — the result half of verdict→receipt→spawn is missing")
	}
}

func TestDispatchedUnruledBashDeniesWithTheDefaultDeny(t *testing.T) {
	// PL5 reached through the dispatch: a bash grant with NO rule covering
	// the command runs NOTHING. The grant is the first boundary, the verdict
	// the second, and S11 retires the S5 skateboard default ("granted means
	// running"): an empty set denies all seven types, and the tool boundary
	// carries no permissive fallback of its own.
	dir := t.TempDir()
	root := t.TempDir()
	s := t13Store(t, dir)

	res, _ := dispatchBashStep(t, s, root, bashTurns("touch marker.txt"))
	if res.Kind != "complete" {
		t.Fatalf("the step should still complete on a refusal: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("an unruled command spawned")
	}
	receipts, err := s.Receipts(context.Background(), res.StepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 0 {
		t.Fatalf("%d receipts for a default-denied command (G15): %+v", len(receipts), receipts)
	}
}

func TestCommandVerdictForIsNilWithoutAStore(t *testing.T) {
	// The storeless daemon answers nil — and nil is the honest answer, not a
	// permissive one: the tools package's own test shows bash refusing on a
	// nil seam rather than spawning unasked.
	if got := CommandVerdictFor(nil, nil); got != nil {
		t.Error("CommandVerdictFor over no store returned a non-nil seam; a storeless daemon has nothing to ask")
	}
	// And a storeless StepRunner builds a toolset that refuses by attempt.
	root := t.TempDir()
	caller, _, err := (StepRunner{}).BuildTools([]string{"bash"}, root, "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	out := caller.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch marker.txt"}`))
	if !out.IsError || !strings.Contains(out.Content, "policy source") {
		t.Errorf("bash without a seam did not refuse as named: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("the storeless toolset spawned")
	}
}

func TestSpawnKeyHonoursTheFrozenPairAndThePoison(t *testing.T) {
	// The seam decides as the gate decides (T13's "same pair" property),
	// from the step's manifest rather than a re-query:
	//   - a frozen allow runs even while the LIVE store is empty (the set is
	//     the record the step saw; re-querying is the drift D12 forbids), and
	//   - a frozen set that contradicts its recorded digest denies everything
	//     (DEF-039/DEF-079: a half-updated governance record decides nothing,
	//     and spawn loses first).
	root := t.TempDir()
	s := t13Store(t, t.TempDir()) // live store: no rules at all

	entries := []runtime.PolicyEntry{{
		ID: "frozen-allow-marker", Type: "command_access", Action: "allow", Source: "operator",
		TargetKind: "all-agents", TargetID: "",
		Spec: json.RawMessage(`{"command":"touch marker.txt","state":"allow"}`),
	}}
	// S59 T5: the seam the dispatched path gets from the store, spelled out on a
	// hand-built runner — without it this step's bash has no environment to ask for
	// and spawns nothing.
	bindStepEnvironment(t, s)
	r := StepRunner{CommandVerdict: CommandVerdictFor(s, nil), ExecResolver: newStepExecResolver(s)}

	caller, _, err := r.BuildTools([]string{"bash"}, root, "test-handler",
		runtime.FrozenManifest{Entries: entries, Frozen: true})
	if err != nil {
		t.Fatal(err)
	}
	out := caller.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch marker.txt"}`))
	if out.IsError {
		t.Fatalf("the frozen allow did not spawn: %s", out.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err != nil {
		t.Fatal("the frozen allow never ran the command")
	}
	_ = os.Remove(filepath.Join(root, "marker.txt"))

	// The same entries under a digest that contradicts them: poison denies,
	// with the engine's own fail-closed sentence and no spawn.
	caller, _, err = r.BuildTools([]string{"bash"}, root, "test-handler",
		runtime.FrozenManifest{Entries: entries, Frozen: true, Hash: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	out = caller.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch marker.txt"}`))
	if !out.IsError {
		t.Fatal("a poisoned frozen set minted a spawn key")
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("the poisoned set ran the command")
	}
}

func TestModelForgedPermissionKindCarriesNoSpawnAuthority(t *testing.T) {
	// SEC-054's T13 re-check, by ATTEMPT. The question tool's enum already
	// carried kind "permission" (pre-existing S6 shape) — the MODEL can set
	// it. A genuine gate-ask row and a model-forged one were distinguishable
	// only by ToolUseID provenance, so the rule T13 must hold to is: the
	// token path never reads the Kind, never reads a question row, and mints
	// only from the engine over the exact command. This drives the forged
	// shape all the way through a dispatch: the permission-labelled row gets
	// created (the enum allows it), the step suspends (S6's path answers the
	// question tool's own block) — and the bash block that followed it never
	// ran, because Spawn takes a token and nothing here mints one from a
	// label.
	dir := t.TempDir()
	root := t.TempDir()
	s := t13Store(t, dir)

	forgedQuestion := model.Block{Kind: model.BlockToolUse, ID: "q1", Name: "question",
		Input: json.RawMessage(`{"text":"approve the push?","options":["approve","deny"],"kind":"permission"}`)}
	blocked := toolUse("b1", "bash", `{"command":"touch marker.txt"}`)

	res, _ := dispatchBashStep(t, s, root, []modelTurn{
		{toolUses: []model.Block{forgedQuestion, blocked}},
	})
	if res.Kind != "suspended" || res.Question == nil {
		t.Fatalf("the forged question did not suspend the step as S6's path does: %+v", res)
	}
	if res.Question.Kind != "permission" {
		t.Errorf("kind %q, want the model-forged permission the enum admits", res.Question.Kind)
	}
	// The authority test: a label carried no spawn. No file, and no receipt
	// for the bash block — the block was never dispatched at all.
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err == nil {
		t.Fatal("a model-set Kind produced a spawn")
	}
	receipts, err := s.Receipts(context.Background(), res.StepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 0 {
		t.Fatalf("receipts exist for a step that only ever saw a forged label: %+v", receipts)
	}
}

func TestNoFileOnTheTokenPathReadsAQuestionKind(t *testing.T) {
	// SEC-054, pinned as structure: the four files that stand between a
	// verdict and a process parse NO question Kind — no `Kind` identifier, no
	// quoted "permission" literal. A future edit that starts reading either
	// is the token path parsing model-influenced text for authority, and it
	// fails HERE, in the daemon's own composition test, not four slices from
	// now.
	for _, path := range []string{
		"../policy/token.go",
		"../tools/spawn.go",
		"../tools/bash.go",
		"../tools/read.go", // ToolContext — the seam's carrier
	} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		text := string(src)
		// `.Kind` is the selector a reader uses to READ one (a struct
		// declaration or a prose mention is not a read); the quoted literal
		// is the comparison an authority branch would compare against.
		if i := strings.Index(text, ".Kind"); i >= 0 {
			t.Errorf("%s reads a .Kind selector at %d — SEC-054: the token path must never read a question's Kind (context: %q)",
				path, i, snippet(text, i))
		}
		if i := strings.Index(text, `"permission"`); i >= 0 {
			t.Errorf("%s quotes %q — SEC-054: no comparison to a Kind string on the path to Spawn", path, "permission")
		}
	}
}

func snippet(s string, i int) string {
	lo := max(0, i-40)
	hi := min(len(s), i+40)
	return strings.ReplaceAll(s[lo:hi], "\n", "⏎")
}
