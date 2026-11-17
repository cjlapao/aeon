package main

// desk_move_test.go — S45 T1 Phase B: the two tests that pin the MOVE itself,
// which no behavioural test can. The goldens prove the bytes did not change;
// these prove there is now exactly one definition of each moved rule, and that
// the socket's create door and the CLI's commit door are looking at the same
// function rather than two that happen to agree today.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoLocalCopyOfMovedFunctions is the slice's first acceptance criterion:
// `internal/desk` holds the single definition of every moved function and
// `cmd/aeon` calls it. A second definition anywhere — a leftover in this
// package, or a re-implementation that quietly reappears — is the drift the
// whole extraction exists to delete.
func TestNoLocalCopyOfMovedFunctions(t *testing.T) {
	moved := []string{
		"runPolicySeedCorpus", "seedPolicyID", "seedSpecJSON",
		"resolveSeedTargets", "applyProposalEffect", "effectAlreadyApplied",
	}

	// (a) not defined here any more. Definitions only, never mentions: this
	// file's own header names them, and a header is not an implementation.
	mainFiles, err := filepath.Glob("*.go")
	if err != nil || len(mainFiles) == 0 {
		t.Fatalf("glob cmd/aeon: %v", err)
	}
	var production int
	for _, name := range mainFiles {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		production++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, fn := range moved {
			for _, spelling := range []string{"func " + fn + "(", "func (recv " + fn + ")"} {
				if strings.Contains(string(src), spelling) {
					t.Errorf("%s still defines %s — internal/desk owns it now", name, fn)
				}
			}
		}
	}
	if production < 10 {
		t.Fatalf("only %d production files walked — the grep checked nothing", production)
	}

	// (b) and defined exactly once in the desk, so (a) cannot be satisfied by
	// deleting the rule instead of moving it.
	deskDir := filepath.Join("..", "..", "internal", "desk")
	deskFiles, err := filepath.Glob(filepath.Join(deskDir, "*.go"))
	if err != nil {
		t.Fatalf("glob internal/desk: %v", err)
	}
	counts := map[string]int{}
	deskText := ""
	// What each moved name became on the desk's side of the line — the move
	// exported some of them and turned the two switches into registry
	// methods, so "it moved" is checked against these spellings, never against
	// a lowercase copy that would only prove someone pasted the old code.
	deskSide := map[string]string{
		"runPolicySeedCorpus":  "func RunSeedCorpus(",
		"seedPolicyID":         "func SeedPolicyID(",
		"seedSpecJSON":         "func seedSpecJSON(",
		"resolveSeedTargets":   "func ResolveSeedTargets(",
		"applyProposalEffect":  "func (r *Registry) Apply(",
		"effectAlreadyApplied": "func (r *Registry) AlreadyApplied(",
	}
	deskCounts := map[string]int{} // per-file `.DecideProposal(` counts — see (c)
	for _, path := range deskFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		deskText += string(src)
		deskCounts[filepath.Base(path)] = strings.Count(string(src), ".DecideProposal(")
		for _, fn := range moved {
			if strings.Contains(string(src), deskSide[fn]) {
				counts[fn]++
			}
		}
	}
	for _, fn := range moved {
		if counts[fn] != 1 {
			t.Errorf("internal/desk defines %s (as %q) %d times — exactly one definition is the criterion",
				fn, deskSide[fn], counts[fn])
		}
	}

	// (c) the desk must not have picked up the writes the guards keep outside
	// it. This is the same claim TestOnlyProposalIsSocketWritable makes with a
	// whitelist; here it is a check on the NEW package specifically, so a
	// whitelist addition can never become the quiet way in.
	//
	// Reconciled 2026-11-01 (T11) with T5's legitimate move: `DecideProposal`
	// is NOT banned from the desk — internal/desk/decide.go CLAIMS the decided
	// row through its Deps.Store seam as the single decision orchestration
	// both doors call (PM4: one implementation). The ban becomes a COUNT, which
	// pins the move harder than the ban did: exactly two `.DecideProposal(`
	// call sites, BOTH in decide.go (Commit's flip and Reject's flip). A third
	// — in decide.go or anywhere else in the package — is a second claim point
	// appearing inside the desk, the exact drift the ban existed to catch; the
	// two-way whitelist contract stays with TestOnlyDecideAndEffectCallersAre
	// TheWhitelisted and TestOnlyTheDeskAndTheDaemonSeamsCallTheWriteBoundary.
	for _, no := range []string{"SaveConfig(", ".RecordAuthored(", ".PolicyAdd(", ".PolicyRemove("} {
		if strings.Contains(deskText, no) {
			t.Errorf("internal/desk contains %q — that call belongs to the wiring file the guard whitelists, not to the desk", no)
		}
	}
	{
		total, offenders := 0, []string{}
		for file, n := range deskCounts {
			total += n
			if file != "decide.go" && n > 0 {
				offenders = append(offenders, file)
			}
		}
		if total != 2 || len(offenders) > 0 {
			t.Errorf("internal/desk calls .DecideProposal( %d times (want 2, both in decide.go); per-file counts %v — Commit and Reject are the only claims the desk may make", total, deskCounts)
		}
	}

	// (d) what the CLI must STILL own: the flip, the store bridge, the seam
	// wiring and every print.
	//
	// Reconciled 2026-11-01 (T11) with T5's legitimate move: the CLI decision
	// door's flip SHIM moved from proposal.go to cmd/aeon/proposal_decide.go
	// (the sanctioned door file, whitelisted by name in
	// TestOnlyProposalIsSocketWritable and TestOnlyDecideAndEffectCallersAre
	// TheWhitelisted). The package claim is unchanged and the pin is stricter
	// than the ban it replaces: exactly ONE `.DecideProposal(` call in ALL of
	// cmd/aeon's production files, and it lives in proposal_decide.go — the
	// human CLI door still performs the flip; it simply does so in its own
	// door file beside commit/reject.
	totalCli := 0
	for _, name := range mainFiles {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		n := strings.Count(string(src), ".DecideProposal(")
		totalCli += n
		if name == "proposal_decide.go" && n != 1 {
			t.Errorf("proposal_decide.go calls .DecideProposal( %d times, want 1 — the CLI's flip shim is the one claim the door makes", n)
		}
		if name != "proposal_decide.go" && n > 0 {
			t.Errorf("%s calls .DecideProposal( %d times — the flip left its door file", name, n)
		}
	}
	if totalCli != 1 {
		t.Errorf("cmd/aeon performs the flip %d times in production files, want exactly 1 (in proposal_decide.go) — the human-only door must own one claim", totalCli)
	}
	policySrc, err := os.ReadFile("policy.go")
	if err != nil {
		t.Fatalf("read policy.go: %v", err)
	}
	if !strings.Contains(string(policySrc), "func policyRepo(") {
		t.Error("policy.go no longer holds the RepoFuncs bridge — it moved outside a whitelisted file")
	}
}

// The seam value below is `validateProposalPayload` itself: part (a) has
// already counted the assignments and found the one, so calling it IS calling
// what the daemon holds — there is no second spelling to drift to.

// TestDeskValidateIsTheCreateValidator pins S35 criterion 4 across the move:
// the daemon's `ValidateProposal` seam (what `proposal.create` runs) and the
// desk's validator are the same function reached through one call site, so a
// rule fixed once is fixed at both doors. Two implementations that agree today
// are the failure mode; behavioural equality alone would not catch it, so this
// checks the wiring as well as the answers.
func TestDeskValidateIsTheCreateValidator(t *testing.T) {
	// (a) the wiring: serve.go assigns the seam once, and the seam forwards to
	// the desk's registry exactly once — no branch, no second table.
	serve, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	if n := strings.Count(string(serve), "dopts.ValidateProposal = validateProposalPayload"); n != 1 {
		t.Errorf("serve.go wires the create door %d times, want 1 — one seam, one assignment", n)
	}
	validators, err := os.ReadFile("proposal_validators.go")
	if err != nil {
		t.Fatalf("read proposal_validators.go: %v", err)
	}
	if n := strings.Count(string(validators), "deskEffects.ValidateProposal("); n != 1 {
		t.Errorf("proposal_validators.go calls the desk's validator %d times, want 1 — the one call site is the whole identity claim", n)
	}
	// The effect registry, final shape (reconciled 2026-11-01, T11): T10 folded
	// the direct road's `agent_import` special case into desk.Commit, so no
	// cmd/aeon door calls the effects any more — cmd/aeon WIRES the one
	// registry (`deskEffects`, built over the one seam map) and the single
	// orchestration internal/desk/decide.go CALLS it: Commit checks
	// already-applied once and Commit applies the effect once, both doors
	// through that one path (PM4). The behavioural half of this claim is
	// TestConcurrentCommitsApplyTheEffectOnce / TestCommitAgentImportCommits
	// LikeEveryKind; this pin keeps it structural: wiring defined once, doors
	// never call, the orchestration calls each effect exactly once.
	commit, err := os.ReadFile("proposal.go")
	if err != nil {
		t.Fatalf("read proposal.go: %v", err)
	}
	if n := strings.Count(string(commit), "var deskEffects = desk.New("); n != 1 {
		t.Errorf("proposal.go wires the effect registry %d times, want 1 — one registry, one definition", n)
	}
	cliFiles, err := filepath.Glob("*.go")
	if err != nil || len(cliFiles) == 0 {
		t.Fatalf("glob cmd/aeon: %v", err)
	}
	for _, name := range cliFiles {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, call := range []string{"deskEffects.AlreadyApplied(", "deskEffects.Apply("} {
			if n := strings.Count(string(src), call); n != 0 {
				t.Errorf("%s calls %s %d times — no door calls an effect directly; desk/decide.go is the only caller", name, call, n)
			}
		}
	}
	deskDecide, err := os.ReadFile(filepath.Join("..", "..", "internal", "desk", "decide.go"))
	if err != nil {
		t.Fatalf("read internal/desk/decide.go: %v", err)
	}
	for _, call := range []string{"reg.AlreadyApplied(", "reg.Apply("} {
		if n := strings.Count(string(deskDecide), call); n != 1 {
			t.Errorf("internal/desk/decide.go calls %s %d times, want 1 — the commit door is the only effect caller (through the one orchestration)", call, n)
		}
	}

	// (b) the answers: every kind the socket can be handed, through the seam
	// the daemon is built with (serve.go assigns exactly this identifier to
	// daemon.Options.ValidateProposal, counted above and nowhere else) and
	// through the desk directly, must be word-identical —
	// including the refusals, which are the part that surfaces to a human.
	for _, tc := range []struct {
		name    string
		kind    string
		payload string
	}{
		{"policy_add ok", "policy_add", `{"id":"a","type":"command_access","target":{"Kind":"agent","ID":"alice"},"action":"deny","spec":{"command":"ls*","state":"deny"},"source":"operator","not_after":null}`},
		{"policy_add not a row", "policy_add", `{"id":"a"`},
		{"policy_add allow from agent", "policy_add", `{"id":"a","type":"command_access","target":{"Kind":"agent","ID":"alice"},"action":"allow","spec":{"command":"ls*","state":"allow"},"source":"agent","not_after":null}`},
		{"policy_remove no id", "policy_remove", `{"id":"  "}`},
		{"policy_remove not json", "policy_remove", `[1,2]`},
		{"seed_corpus empty", "seed_corpus", `{"agents":[]}`},
		{"seed_corpus unknown agent", "seed_corpus", `{"agents":["nobody-in-the-corpus"]}`},
		{"agent_import relative", "agent_import", `{"pkgPath":"./relative"}`},
		{"agent_import unknown field", "agent_import", `{"pkgPath":"/abs","extra":1}`},
		{"mcp add bad tier", "mcp_server_add", `{"tier":"sideways","server":{"name":"gh"}}`},
		{"mcp remove empty name", "mcp_server_remove", `{"name":""}`},
		{"unknown kind", "nonsense_kind", `{}`},
		{"empty kind", "", `{}`},
	} {
		seamErr := errText(validateProposalPayload(context.Background(), tc.kind, tc.payload))
		deskErr := errText(deskEffects.ValidateProposal(context.Background(), tc.kind, tc.payload))
		if seamErr != deskErr {
			t.Errorf("%s: the create seam said %q and the desk said %q — two validators have appeared", tc.name, seamErr, deskErr)
		}
	}
	// The registry the socket's create door consults still covers exactly the
	// kinds the store declares — the same invariant S35 T2's own test pins,
	// asserted here because after the move the map's VALUES are wrappers and a
	// lost entry would now also lose the desk's validator for that kind.
	for _, kind := range proposalKindList() {
		if _, ok := proposalValidators[kind]; !ok {
			t.Errorf("proposalValidators has no entry for %q", kind)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// proposalKindList is the store's declared kinds, spelled here rather than
// imported to keep this file's dependencies to the package under test.
func proposalKindList() []string {
	return []string{"policy_add", "policy_remove", "seed_corpus", "mcp_server_add", "mcp_server_remove", "agent_import"}
}
