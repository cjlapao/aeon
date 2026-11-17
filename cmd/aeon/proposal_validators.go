package main

// proposal_validators.go — S35 T2: one validator per proposal kind, each
// calling EXISTING code (TD3). Both doors call the same registry entry on
// the same payload — `proposal.create` (T3, through the ValidateProposal
// seam below) and `aeon proposal commit` (T5) — so a bug fixed in a
// validator fixes both at once, never two divergent checks.
//
// Q1's investigation (this task): `cmd/aeon/policy.go`'s validation was
// NOT already a call-able function — it was inline inside
// `(*policy.Store).Add` (internal/policy/store.go, not cmd/aeon as this
// task's own text guessed), interleaved with the write. Extracted as
// `policy.ValidateForAdd` (store.go, same file checkAuthorship's call-count
// pin scans — see that function's own comment) in a separate, first commit;
// `Add` now calls it and does the write. `seed_corpus`'s per-agent
// resolve-before-write check was similarly inline in
// `runPolicySeedCorpus` and is now `resolveSeedTargets` (policy.go),
// called by both. `policy_remove` and the two mcp kinds had no reusable
// logic to extract — a membership check is written fresh below, same as
// S34 T2's own precedent for `mcp_server_remove`-shaped checks.

import (
	"context"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mcp"
)

// proposalValidators is the four-entry registry TD3 asks for — one per
// `proposal.kind` (store.ProposalKinds). Each validator opens its own
// boundary/config fresh, exactly as every other `aeon policy`/`aeon mcp`
// verb already does (policyBoundary, mcp.LoadConfigFile) — there is no
// shared daemon handle to reuse here, by design: CLI commit (T5) and the
// daemon's create-time call (T3) are different processes/call sites, and
// "the same function" (TD3) means the same validation, not a shared
// connection. After T1 the policy/seed/import entries are one-line calls into
// the desk's registry (the same function the daemon's ValidateProposal seam and
// `aeon proposal commit` reach); the two MCP entries stay the local functions
// below, which is also exactly what the desk runs for those kinds — they arrive
// back as registered seams, one definition either way.
var proposalValidators = map[string]func(payload []byte) error{
	"policy_add":        validatePolicyAddProposal,
	"policy_remove":     validatePolicyRemoveProposal,
	"seed_corpus":       validateSeedCorpusProposal,
	"mcp_server_add":    validateMcpServerAddProposal,
	"mcp_server_remove": validateMcpServerRemoveProposal,
	"agent_import":      validateAgentImportProposal,
	// S56 T8 (2026-11-01): the two RESTRICTIVE autonomy kinds join the closed
	// set — one-line calls into the desk's registry, which runs the full
	// desk-then-store Validate seam (TD3: the same function the commit door
	// re-runs; the store half opens the CLI's justified second handle).
	"autonomy_revoke": validateAutonomyRevokeProposal,
	"autonomy_deny":   validateAutonomyDenyProposal,
	// S46 T6: the ninth kind's one line, same TD3 shape.
	"mcp_grant_revoke": validateMCPGrantRevokeProposal,
	// S57 T8 (2026-11-02): the two RESTRICTIVE gate kinds — one-line
	// calls into the desk's registry, which runs the full desk-then-store
	// Validate seam (gate_proposal_seams.go); the commit door re-runs
	// exactly what the create door checked (TD3).
	"verify_command": validateVerifyCommandProposal,
	"merge_rule":     validateMergeRuleProposal,
	// S48 T9 (2026-11-03): the twelfth kind — one line into the desk's
	// registry, which runs the full desk-then-store Validate seam
	// (blueprint_proposal_seams.go): the write path's own Analyze with the
	// write path's own options, the id/name cross-check verbatim, the
	// createOnly collision. The commit door re-runs exactly what the
	// create door checked (TD3).
	"blueprint_save": validateBlueprintSaveProposal,
	// S59 T7 (2026-11-03): the thirteenth kind — one line into the desk's
	// registry, which runs the full desk-then-plane Validate seam
	// (environment_proposal_seams.go): the strict payload shape, the row read
	// by id or slug, `execenv.Restricts` EMPTY (every widening named verbatim)
	// and the proposed spec valid under the row's own backend. The commit door
	// re-runs exactly what the create door checked (TD3), and there is no
	// create-side half that the commit could skip.
	"environment_restrict": validateEnvironmentRestrictProposal,
	// S49 T6 (2026-11-03): the fourteenth kind — one line into the desk's
	// registry, which runs the full desk-then-row Validate seam
	// (automation_proposal_seams.go): the strict payload shape (WHO and WHY,
	// no target state), the row read under ITS OWN source (AN1), not
	// archived (AN6) and not already off — the narrowing-only kind, refused
	// inert at create. The commit door re-runs exactly what the create door
	// checked (TD3), and there is no create-side half the commit could skip.
	"automation_disable": validateAutomationDisableProposal,
	// S50 T5 (2026-11-04): the fifteenth kind — one line into the desk's
	// registry, which runs the full desk-then-row Validate seam
	// (roadmap_proposal_seams.go): the strict payload shape (WHICH IDEA and
	// NOTE, no rank, size, status or actor), the component root read in the
	// registry, and no identical OPEN twin already on the board — the
	// add-only kind, refused inert at create. The commit door re-runs
	// exactly what the create door checked (TD3), and there is no
	// create-side half the commit could skip.
	"roadmap_idea":     validateRoadmapIdeaProposal,
	"learning_retract": validateLearningRetractProposal,
	// S60 T9 (2026-11-04): the seventeenth kind — one line into the desk's
	// registry, which runs the full desk-then-facts Validate seam
	// (mergequeue_proposal_seams.go): the strict payload shape (WHICH
	// repository and branch and WHY, no actor, no attended flag, no base —
	// MQ15), then mergequeue.ValidateEnqueue over MergeQueueRepoFacts: the
	// root, the branch's existence, the base, the open duplicate — the ONE
	// function the socket door answers on too. The commit door re-runs
	// exactly what the create door checked (TD3).
	"mergequeue_enqueue": validateMergeQueueEnqueueProposal,
	// S61 T5 (2026-11-05): the eighteenth and nineteenth kinds — one line
	// each into the desk's registry. charter_save runs the full
	// desk-then-charter Validate seam (charter_proposal_seams.go): shape,
	// charter.Validate verbatim, the root facts with the store's own
	// sentences. intake_plan runs the full desk-then-charter-then-template
	// seam (intake_plan_proposal_seams.go) — Validate ONLY: its built-in
	// apply is the fixed sentence naming intake.commit (GI7/OD4), and the
	// census test below is why these lines exist at all despite the spec's
	// hoped-for "no edit" (the deviation is recorded in T5's Notes). The
	// commit door re-runs exactly what the create door checked (TD3).
	"charter_save": validateCharterSaveProposal,
	"intake_plan":  validateIntakePlanProposal,
}

// validateProposalPayload is the one entry point both T3 and T5 call — its
// signature IS the `ValidateProposal` seam named in this task's own Seam
// note (`func(ctx context.Context, kind, payload string) error`,
// daemon.Options field, wired in serve.go beside
// wireAgentServices/wireMCPServices — S34 T3/T4's own precedent for a
// cmd/aeon-implemented seam `internal/protocol` reaches through rather than
// importing `internal/mcp`/`cmd/aeon` directly). `payload` is `string` here
// to match that seam exactly; every registry entry below works in `[]byte`
// (json.Unmarshal's natural input), converted once at this one boundary.
//
// Since T1 it forwards to the desk's registry, which is where the per-kind
// validators live — so the create door (this very function, wired as
// daemon.Options.ValidateProposal in serve.go) and the commit door
// (`aeon proposal commit`) run ONE function per kind, and
// TestDeskValidateIsTheCreateValidator pins that identity. An unknown kind still
// refuses with the same words it always used.
func validateProposalPayload(ctx context.Context, kind, payload string) error {
	return deskEffects.ValidateProposal(ctx, kind, payload)
}

// The four entries the desk now owns, kept at these names because S35 T2's
// tests call them directly (and the registry above references them). Each is one
// line: no rule lives here any more. `agent_import`'s validator is the desk's
// too — the payload decode needs nothing from the plugin layer, only its effect
// does, so only that half arrives as a seam.
func validatePolicyAddProposal(payload []byte) error {
	return validateKindPayload("policy_add", payload)
}

func validatePolicyRemoveProposal(payload []byte) error {
	return validateKindPayload("policy_remove", payload)
}

func validateSeedCorpusProposal(payload []byte) error {
	return validateKindPayload("seed_corpus", payload)
}

func validateAgentImportProposal(payload []byte) error {
	return validateKindPayload("agent_import", payload)
}

// S56 T8 (2026-11-01): the two RESTRICTIVE autonomy kinds' validators. Same
// one-line shape as the four above — the rule lives in the desk's registry,
// which for these kinds runs the full desk-then-store Validate seam, so the
// commit door re-runs exactly what the create door checked (TD3). The store
// half opens the CLI's justified second handle, the same as policy/proposal.
func validateAutonomyRevokeProposal(payload []byte) error {
	return validateKindPayload("autonomy_revoke", payload)
}

func validateAutonomyDenyProposal(payload []byte) error {
	return validateKindPayload("autonomy_deny", payload)
}

// validateKindPayload is the one call site: the desk's registry, the process
// clock's context, the kind named. Every refusal's wording is the desk's, byte
// for byte what S35 T2's tests pinned before the move.
func validateKindPayload(kind string, payload []byte) error {
	// The public seam, not the registry directly: the CLI's per-kind entry and
	// the daemon's create door are then literally the same function, with the
	// desk reached from exactly one call site in this file.
	return validateProposalPayload(context.Background(), kind, string(payload))
}

// policyRemovePayload and seedCorpusPayload are the desk's types now — one
// definition, shared by the validator, the gate and the effect. These aliases
// keep the spelling this file's callers and the CLI's staging helpers use.
type (
	policyRemovePayload = desk.PolicyRemovePayload
	seedCorpusPayload   = desk.SeedCorpusPayload
)

// mcpServerAddPayload names the tier explicitly — unlike remove, there is
// no existing row to read it from. "private" also names the one agent the
// server belongs to (MC2); "public" names none.
type mcpServerAddPayload struct {
	Tier   string           `json:"tier"`
	Agent  string           `json:"agent,omitempty"`
	Server mcp.ServerConfig `json:"server"`
	// Enabled (S46 T6): MC3's default-true lives at the door; the payload
	// may carry an explicit false (added but not offered), never a
	// surprise — the same *bool discipline as mcp.server.add's params.
	Enabled *bool `json:"enabled,omitempty"`
}

// validateMcpServerAddProposal is S45 T1's pattern applied to the MCP kind
// (S46 T6): one line to the registry, so the create door and the commit
// door run the SAME function (TD3) and the validator is THE one
// (Rows.With + Assemble inside seamValidateMCPServerAdd). The old body —
// WithServer over the file-era Config — retired with the file mirror.
func validateMcpServerAddProposal(payload []byte) error {
	return validateKindPayload("mcp_server_add", payload)
}

type mcpServerRemovePayload struct {
	Name string `json:"name"`
	// Reason (S46 T8): the agent's own-view propose tool requires a reason
	// and it must ride to the person deciding (the payload is what they
	// read). Additive; the commit effect never read it and never will — it
	// is for the deciding human, the same field the seventh kind carries.
	Reason string `json:"reason,omitempty"`
	// CascadeDeclares (S46 T6): the proposal MEANS the revokes — each one
	// is listed in the commit result and audited as its own grant_revoke
	// row. Without it the validator refuses while declarations name the
	// server (never a silent revoke).
	CascadeDeclares bool `json:"cascadeDeclares,omitempty"`
}

// mcpGrantRevokePayload is S46 T6's seventh kind: the NARROWING direction
// (MG9 — there is no widening kind to fuzz into one). reason is the
// proposer's note to the human; it travels in the proposal row, never in
// the effect (the audit's summary is composed by the door, not dictated
// by the payload).
type mcpGrantRevokePayload struct {
	Agent     string `json:"agent"`
	Qualified string `json:"qualified"`
	Reason    string `json:"reason,omitempty"`
}

// validateMcpServerRemoveProposal is a membership check — no Config.Validate
// call needed (design.md TD3): MC12 keeps a server name unique across the
// whole config (one tier, one agent), so the name alone is enough to find
// it without the payload repeating the tier it already lives in.
func validateMcpServerRemoveProposal(payload []byte) error {
	return validateKindPayload("mcp_server_remove", payload)
}

// validateMCPGrantRevokeProposal is the seventh kind's one line (S46 T6).
func validateMCPGrantRevokeProposal(payload []byte) error {
	return validateKindPayload("mcp_grant_revoke", payload)
}
