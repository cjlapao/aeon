// Package desk is the one implementation of what it means to COMMIT a policy or
// proposal effect: the per-kind registry (validate → already-applied → apply),
// the seed-corpus runner, and the payload validators the create door and the
// commit door share.
//
// # Why the package exists
//
// That code used to live in `cmd/aeon` (S35 T2/T5, S11 loop T9), and the daemon
// cannot import `cmd/aeon` — so the socket could never commit a proposal, which
// is the whole of S45's T5. A package the daemon may import had to hold it, and
// "one implementation" (PM4) means the CLI stops being the implementation and
// becomes a caller of it, exactly as it already is for everything the daemon
// owns. Two callers, one body:
//
//   - the CLI (`cmd/aeon`'s policy/proposal verbs) — printing and flag parsing
//     stay there; the deciding and the writing come from here;
//   - the daemon (`internal/daemon`, from T5's socket commit) — over the same
//     registry, with the same seams wired.
//
// # The dependency rules (asserted by TestDeskImportsNothingForbidden)
//
// Allowed: `internal/policy` (the boundary's own types and the pure validators),
// `internal/paths` (where the MCP config file is), the standard library.
//
// Forbidden, and the reason each is a rule rather than a preference:
//
//   - `internal/mcp` — S34's MC11 deletability rule pins the layer's importers
//     to `cmd/aeon/` (`internal/mcp/absent_test.go`). A convenience import here
//     would decay that guarantee, so the two MCP proposal kinds arrive as
//     **seams**: their whole `Effect` is handed in by `cmd/aeon serve`, and this
//     package never names the MCP config type at all;
//   - `internal/plugin`, `internal/agentsvc` — same shape for `agent_import`:
//     the import body prints a consent and reaches the plugin layer, so its
//     AlreadyApplied and Apply halves arrive as seams and this package keeps
//     only the payload validation, which is pure JSON;
//   - `internal/daemon`, `internal/protocol`, `internal/narrow`, `internal/tools`,
//     `cmd/aeon` — the daemon imports the desk, never the reverse; a surface
//     package reaching back in would put a socket handler's authority inside the
//     code that decides what an operator may author.
//
// `internal/store` is not imported either: the store handle reaches the boundary
// through the caller's `BoundaryOpener`, so the one bridge from store rows to
// policy rows stays where the PL15 guard already pins it.
//
// # The guard shape, and what T6 changes
//
// PL15's guard (`cmd/aeon/policy_guards_cases_test.go`'s `TestNoPolicyAuthoringReachesTheNarrowSurface`)
// admits only the CLI's own files as callers of the policy boundary's authoring
// and its authored audit record, and `TestOnlyProposalIsSocketWritable` admits
// only `cmd/aeon/proposal.go` as a caller of the proposal flip and the MCP
// config save. Until T6 rewrites those whitelists positively (PM10), the
// authoring primitives therefore arrive here as the `Boundary` seam and the MCP
// and import effects as `Seams`, both implemented in the files the guards
// already name. What is NOT weakened: this package holds the single definition
// of every moved function, and every write still clears the same boundary — the
// adapter that calls it is a forwarding type, not a second implementation.
package desk

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"
)

// Kind is a proposal kind whose effect the desk can commit. The five S35 kinds
// are implemented here; `agent_import` is declared here and gets its effect from
// a seam, because the plugin layer it must reach is closed to this package.
type Kind string

const (
	KindPolicyAdd    Kind = "policy_add"
	KindPolicyRemove Kind = "policy_remove"
	KindSeedCorpus   Kind = "seed_corpus"
	KindMCPServerAdd Kind = "mcp_server_add"
	KindMCPServerRem Kind = "mcp_server_remove"
	KindAgentImport  Kind = "agent_import"
	// KindMCPGrantRevoke is S46 T6's seventh kind (S35 TD1's design
	// revision, made here): the narrowing direction only — an agent
	// proposes taking a declaration back. No widening kind exists or may
	// be added (MG9). (With S56's two restrictive autonomy kinds merged,
	// it ships ninth in the store's order; the "seventh" is its slice
	// position, not its index.)
	KindMCPGrantRevoke Kind = "mcp_grant_revoke"
)

// kinds is the closed list the registry is checked against, in the order the
// store's `ProposalKinds` declares them.
var kinds = []Kind{
	KindPolicyAdd, KindPolicyRemove, KindSeedCorpus,
	KindMCPServerAdd, KindMCPServerRem, KindAgentImport,
	KindAutonomyRevoke, KindAutonomyDeny,
	KindMCPGrantRevoke,
}

// Kinds returns the kinds the desk knows about — a copy, so a caller cannot
// reorder the list the registry is measured against.
func Kinds() []Kind { return append([]Kind(nil), kinds...) }

// Effect is one kind's whole commit story: the create-time validation the
// socket's create door runs (TD3: the SAME function the commit door re-runs),
// the idempotency gate that answers "has this exact effect already landed"
// (TD4), and the apply that performs it.
//
// A half may be nil only on a kind whose seam was never wired; the registry
// then refuses the call by name rather than treating the kind as absent or
// silently skipping the step (`Registry.Missing` says which half).
type Effect struct {
	// Validate is the shared validator — the create door's and the commit
	// door's, one function, so a rule fixed once is fixed at both doors.
	Validate func(ctx context.Context, payload []byte) error

	// AlreadyApplied answers TD4's question. It answers only "has THIS exact
	// effect already landed", never "would it be fine to apply again": a
	// mismatch answers false so validate+apply still run and the real
	// validator refuses the genuine conflict loudly, rather than this gate
	// silently papering over it.
	AlreadyApplied func(ctx context.Context, payload []byte) (bool, error)

	// Apply performs the effect. The writer reaches the effects that print for
	// the human at the door — today, the import seam's fresh consent. The
	// effects that print nothing today keep printing nothing (the seed corpus
	// under commit runs on `io.Discard`, exactly as it did when this lived in
	// `cmd/aeon/proposal.go`).
	Apply func(ctx context.Context, w io.Writer, payload []byte) error
}

// Deps carries what the registry needs from its caller. Both are function
// values, so the daemon and the CLI differ only in what they hand in — never in
// what the desk then does.
type Deps struct {
	// Boundary opens the policy write boundary an effect runs over. It is an
	// OPENER rather than a live handle because every effect opens its own
	// boundary today and closes it before returning (validator, gate and apply
	// are three separate fresh reads, exactly as S35 T2 wrote them), and
	// because the two doors reach the store by different paths: the CLI through
	// its justified direct-open exception, the daemon through the handle it
	// already holds.
	Boundary BoundaryOpener

	// Clock is the instant the effects stamp. nil means time.Now; the desk
	// never decides to read a clock on its own authority (D5), it reads the one
	// it was given.
	Clock func() time.Time
}

// Seams carries the effects this package may not implement because the package
// they need is closed to it — the two MCP kinds in full, and the `agent_import`
// kind's AlreadyApplied and Apply halves (T0 item 3.8's list). `cmd/aeon`
// registers them once, as the package-level `deskEffects` in `proposal.go`, so
// the CLI's commit door and the daemon `cmd/aeon serve` builds run the same map
// and cannot drift — the registration sits in that file because it is the one
// the PL15 sibling guard admits as a caller of the MCP config writer.
//
// A seam replaces only the halves it fills: a nil half falls back to what the
// desk implements, which is what lets `agent_import` keep the desk's own
// validator while its effect arrives from outside.
type Seams map[Kind]Effect

// Registry is the desk's per-kind effect table, read-only after New.
type Registry struct {
	deps    Deps
	effects map[Kind]Effect
}

// New builds the registry from the caller's deps and seams. A seam kind that
// was never wired is not an error at construction: the refusal happens at the
// call, naming the kind and the half that is missing (a daemon that started
// without the MCP layer wired must still answer every other kind, and must say
// why it will not answer this one).
func New(deps Deps, seams Seams) *Registry {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	effects := map[Kind]Effect{
		KindPolicyAdd: {
			Validate:       deps.validatePolicyAdd,
			AlreadyApplied: deps.alreadyAppliedPolicyAdd,
			Apply:          deps.applyPolicyAdd,
		},
		KindPolicyRemove: {
			Validate:       deps.validatePolicyRemove,
			AlreadyApplied: deps.alreadyAppliedPolicyRemove,
			Apply:          deps.applyPolicyRemove,
		},
		KindSeedCorpus: {
			Validate:       deps.validateSeedCorpus,
			AlreadyApplied: noDetection,
			Apply:          deps.applySeedCorpus,
		},
		KindAgentImport: {
			Validate: validateAgentImport,
		},
		// S56 T8: the built-in halves are the pure shape validators; the
		// store halves (grant exists, node exists, subject valid) arrive as
		// seams from cmd/aeon, which REPLACE the Validate with the full
		// desk-then-store half (the merge rule below). Without the seam the
		// kind answers "missing AlreadyApplied, Apply" — a named refusal,
		// not a silent pass.
		KindAutonomyRevoke: {
			Validate: validateAutonomyRevokePayload,
		},
		KindAutonomyDeny: {
			Validate: validateAutonomyDenyPayload,
		},
		// S57 T8 (2026-11-02): the two RESTRICTIVE gate kinds, the
		// autonomy shape — the built-in halves are the pure shape
		// validators; the store halves (component exists and not
		// archived, the name is free, the rule's fields are in the
		// closed sets, the proposed rule tightens the current one)
		// arrive as seams from cmd/aeon, which REPLACE the Validate
		// with the full desk-then-store half. Without the seam the
		// kind answers "missing AlreadyApplied, Apply" — a named
		// refusal, not a silent pass.
		KindVerifyCommand: {
			Validate: validateVerifyCommandPayload,
		},
		KindMergeRule: {
			Validate: validateMergeRulePayload,
		},
		// S48 T9 (2026-11-03): the design revision S35 TD1 names. The
		// built-in half is the pure shape validator (blueprint_kinds.go);
		// the seam from cmd/aeon replaces Validate with the full
		// desk-then-store half (the write path's own Analyze, the id/name
		// cross-check, the createOnly collision) and supplies
		// AlreadyApplied (the content hash) and Apply (T6's one write path
		// with the COMMITTER as actor). Without the seam the kind answers
		// "missing AlreadyApplied, Apply" — a named refusal, not a silent
		// pass. Not in `kinds` above: the S57 precedent — that nine-name
		// list is its sibling test's pin and this kind is registry-only.
		KindBlueprintSave: {
			Validate: validateBlueprintSavePayload,
		},
		// S59 T7 (2026-11-03): the one ask an agent may make of its
		// execution environment — that it be TIGHTENED. The built-in half
		// is the pure shape validator (environment_kinds.go); the seam
		// from cmd/aeon replaces Validate with the full desk-then-store
		// half (execenv.ParseSpec + Validate over the row's own backend,
		// execenv.Restricts empty, the baseline revision still current)
		// and supplies AlreadyApplied (the row's spec already IS the
		// proposed one) and Apply (the plane's own CAS update with the
		// COMMITTER as actor). Without the seam the kind answers "missing
		// AlreadyApplied, Apply" — a named refusal, not a silent pass.
		// Registry-only, as the blueprint kind: the `kinds` list above is
		// a sibling test's pin.
		KindEnvironmentRestrict: {
			Validate: validateEnvironmentRestrictPayload,
		},
		// S49 T6 (2026-11-03): the one ask an agent may make of an
		// automation — that it be DISABLED. The built-in half is the pure
		// shape validator (automation_kinds.go — WHO and WHY, never a
		// target state); the seam from cmd/aeon replaces Validate with the
		// full desk-then-store half (the row exists under that source, is
		// not archived, is not already off) and supplies AlreadyApplied
		// (the row already IS off — the crash-retry completes the flip)
		// and Apply (T4's own disable/pause path with the COMMITTER as
		// actor). Without the seam the kind answers "missing
		// AlreadyApplied, Apply" — a named refusal, not a silent pass.
		// Registry-only, as the blueprint and environment kinds: the
		// `kinds` list above is a sibling test's pin.
		KindAutomationDisable: {
			Validate: validateAutomationDisablePayload,
		},
		// S50 T5 (2026-11-04): the one ask an agent may make of the
		// roadmap — that one IDEA be ADDED. The built-in half is the pure
		// shape validator (roadmap_kinds.go — WHICH IDEA and NOTE, never a
		// rank, a size, a status or an actor); the seam from cmd/aeon
		// replaces Validate with the full desk-then-store half (the
		// component root exists in the registry, and the identical open
		// item does not already exist) and supplies AlreadyApplied (an
		// identical open item does exist — the crash-retry completes the
		// add) and Apply (roadmap's own create with provenance authored
		// and the COMMITTER as actor, one 'add' audit line). Without the
		// seam the kind answers "missing AlreadyApplied, Apply" — a named
		// refusal, not a silent pass. Registry-only, as the blueprint,
		// environment and automation kinds: the `kinds` list above is a
		// sibling test's pin.
		KindRoadmapIdea: {
			Validate: validateRoadmapIdeaPayload,
		},
		// S52 T4 (2026-11-04): the memory plane's one agent ask — a
		// learning to be TAKEN BACK. The built-in half is the pure shape
		// validator (learning_kinds.go — WHICH memory and WHY, never an
		// actor, a scope, or a reason the stamp column cannot hold); the
		// seam from cmd/aeon replaces Validate with the desk-then-store
		// half (the row exists and is not already retracted) and supplies
		// AlreadyApplied (the row IS retracted — the crash-retry completes
		// the stamp) and Apply (learnings' own retract with the COMMITTING
		// PERSON as actor, one 'retract' audit line — LM7/LM8). Without
		// the seam the kind answers "missing AlreadyApplied, Apply" — a
		// named refusal, not a silent pass. Registry-only, as the
		// blueprint, environment, automation and roadmap kinds: the
		// `kinds` list above is a sibling test's pin.
		KindLearningRetract: {
			Validate: validateLearningRetractPayload,
		},
		// S60 T9 (2026-11-04, MQ14.3): mergequeue_enqueue, the SEVENTEENTH
		// kind — an agent asks that ONE BRANCH join the merge queue. The
		// built-in half is the pure shape validator (mergequeue.go —
		// WHICH repository and branch and WHY, never an actor, an
		// attended flag, or a base the daemon resolves at enqueue, MQ15);
		// the seam from cmd/aeon replaces Validate with the desk-then-
		// facts half (the shape, then mergequeue.ValidateEnqueue over
		// gathered facts — the ONE function the socket door, the arm and
		// the commit all run, TD3/MQ14) and supplies AlreadyApplied (the
		// branch is already an OPEN queue item for that root — the
		// crash-retry completes the ask it filed) and Apply (the queue's
		// own Service.Enqueue, attended = false, the row stamped
		// {proposal, <id>} and the enqueue event carried by the
		// COMMITTING PERSON — MQ14.3). Without the seam the kind answers
		// "missing AlreadyApplied, Apply" — a named refusal, not a silent
		// pass. Registry-only, as the blueprint, environment, automation,
		// roadmap and memory kinds: the `kinds` list above is a sibling
		// test's pin.
		KindMergeQueueEnqueue: {
			Validate: validateMergeQueueEnqueuePayload,
		},
		// S61 T5 (2026-11-05): the eighteenth kind — a charter body to be
		// SAVED. The built-in half is the pure shape validator
		// (charter_save.go — WHICH ROOT and WHICH BODY and WHY, never an
		// actor, a source, or a note the revision cannot lawfully carry);
		// the seam from cmd/aeon replaces Validate with the full
		// desk-then-charter half (charter.Validate over the decoded body,
		// the root exists) and supplies AlreadyApplied (the current
		// revision IS the body — the crash-retry completes without
		// churning history) and Apply (the charter's own Save with the
		// COMMITTING PERSON as actor, source 'proposal', the proposal id
		// as source_ref — GI4, never the proposer). Without the seam the
		// kind answers "missing AlreadyApplied, Apply" — a named refusal,
		// not a silent pass. Registry-only, as the kinds above: the
		// `kinds` list is a sibling test's pin.
		KindCharterSave: {
			Validate: validateCharterSavePayload,
		},
		// S61 T5 (2026-11-05): the nineteenth kind — a goal-intake plan.
		// The built-in half is the FULL commit story on the apply side and
		// the pure shape on the validate side (intake_plan.go — the
		// closed GI7 schema, the caps, the role law; no path, no secret,
		// no actor). The seam from cmd/aeon replaces ONLY Validate (the
		// charter.Validate half and the template-exists store read); the
		// built-in AlreadyApplied (always false, by law) and Apply (the
		// fixed sentence naming intake.commit) STAY — GI7/OD4 make the
		// generic door's refusal the kind's design, not a missing wire,
		// so no seam may replace those halves.
		KindIntakePlan: {
			Validate:       validateIntakePlanPayload,
			AlreadyApplied: intakePlanNeverAlreadyApplied,
			Apply:          applyIntakePlanIsRefused,
		},
	}
	for k, seam := range seams {
		merged := effects[k]
		if seam.Validate != nil {
			merged.Validate = seam.Validate
		}
		if seam.AlreadyApplied != nil {
			merged.AlreadyApplied = seam.AlreadyApplied
		}
		if seam.Apply != nil {
			merged.Apply = seam.Apply
		}
		effects[k] = merged
	}
	return &Registry{deps: deps, effects: effects}
}

// Effect returns the effect registered for a kind, and whether it is complete.
// ok is false for a kind the desk does not know and for a seam kind that was
// never wired; `Missing` then says which of the two it is, in the same words the
// refusal uses.
func (r *Registry) Effect(kind string) (Effect, bool) {
	e, ok := r.effects[Kind(kind)]
	if !ok {
		return Effect{}, false
	}
	if e.Validate == nil || e.AlreadyApplied == nil || e.Apply == nil {
		return Effect{}, false
	}
	return e, true
}

// Missing names why `Effect` refused: the kind the caller asked for and the
// half nobody supplied. "" means the effect is complete.
func (r *Registry) Missing(kind string) string {
	e, ok := r.effects[Kind(kind)]
	if !ok {
		return fmt.Sprintf("no effect is registered for kind %q — the desk knows %s",
			kind, knownKindList())
	}
	var half []string
	if e.Validate == nil {
		half = append(half, "Validate")
	}
	if e.AlreadyApplied == nil {
		half = append(half, "AlreadyApplied")
	}
	if e.Apply == nil {
		half = append(half, "Apply")
	}
	if len(half) == 0 {
		return ""
	}
	return fmt.Sprintf("kind %q is missing %s — the seam that supplies it was never registered with desk.New",
		kind, joinQuoted(half))
}

// knownKindList renders the kinds the registry answers, sorted so the refusal
// reads the same every time.
func knownKindList() string {
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, string(k))
	}
	sort.Strings(names)
	return joinQuoted(names)
}

func joinQuoted(items []string) string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		out = append(out, fmt.Sprintf("%q", s))
	}
	joined := ""
	for i, s := range out {
		if i > 0 {
			joined += ", "
		}
		joined += s
	}
	return joined
}
