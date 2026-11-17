package protocol

// ════════════════════════════════════════════════════════════════════════════
// learnings_mine.go — S52 T4 (LM5, LM7): the four `learnings.mine.*` socket
// twins for the agent's OWN memory view, its own filing, and its one ask.
//
// The catalog rows (internal/records' learning_query · learning_show ·
// learning_add · learning_propose_retract, family "memory") are the ONE
// definition; these are the socket's half of the two doors — the mcp.mine.*
// shape (mcp_mine.go), registered conditionally by RegisterLearningsMine
// and skipped by the catalog loop, so no records service means no door at
// all (V13), never a registered-and-refusing name.
//
// The `learnings.mine.` namespace is the whole point (MG8's lesson, carried
// to memory): a grep and T3's guards can tell the AGENT-SCOPED doors apart
// from the person's unscoped learnings.query / .show / .add / .retract,
// which stay a separate plane with separate callers. They are agent-scoped
// in BOTH directions: a tool call carries the step and the service stamps
// identity from it (OL3); a PERSON session reaching these names through the
// socket is refused with a pointer sentence naming the door that answers it
// (the daemon's personDoorRefusal half). An agent's memory is not an oracle
// for whoever holds the endpoint — the person's board and query already
// answer everything a person legitimately sees.
//
// The schemas (the catalog's, additionalProperties:false) carry NO actor,
// agent, scope-of-another, provenance-beyond-observed-or-assumed, reason-
// length or state field. learning_add's scopeKey is STAMPED: an agent scope
// is the caller's own slug, a project scope its own root, and a param
// naming a different key is a refusal, not an overwrite. The propose twin
// files the INERT learning_retract row through proposal.create's own path
// (shared validator, shared caps, TD3) — never a stamp; retract and restore
// are the person's doors (LM7/LM8), and no name on this list can reach
// them.
// ════════════════════════════════════════════════════════════════════════════

import (
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/records"
)

// MethodLearningsMineQuery is the agent's browse of its readable set — the
// same merged set and ordering a PROMPT would draw from (LM5), tags as the
// only extra filter, query text ranking within it.
const MethodLearningsMineQuery MethodName = "learnings.mine.query"

// MethodLearningsMineShow is one learning by id, readable-checked: a row
// outside the agent's scopes answers with the SAME sentence a nonexistent
// id gets, so the door never proves what other agents remember.
const MethodLearningsMineShow MethodName = "learnings.mine.show"

// MethodLearningsMineAdd is the agent's OWN filing: observed or assumed
// only, into its own scope (or its running root), rate-capped per agent at
// learnings.MaxSelfLogAddsPerAgent counted off the audit ledger.
const MethodLearningsMineAdd MethodName = "learnings.mine.add"

// MethodLearningsMineProposeRetract is the one ask an agent may make about
// memory: that a LEARNING BE TAKEN BACK, inert until a person commits it —
// and when they do, they are the actor, not the proposer (LM8).
const MethodLearningsMineProposeRetract MethodName = "learnings.mine.proposeRetract"

// mineCatalogNames is the four names the catalog loop skips and
// RegisterLearningsMine owns (the mcpMineCatalogNames shape).
var mineCatalogNames = map[string]bool{
	string(MethodLearningsMineQuery):          true,
	string(MethodLearningsMineShow):           true,
	string(MethodLearningsMineAdd):            true,
	string(MethodLearningsMineProposeRetract): true,
}

// RegisterLearningsMine registers the four agent-scoped twins when — and
// only when — Table.Records stands (these are catalog actions; their Do IS
// the answer, one definition two doors). Absent otherwise, never
// registered-and-refusing (V13). Returns true when all four stand (already
// or just now); false when the records seam is missing.
func (t *Table) RegisterLearningsMine() bool {
	if t.Records == nil {
		return false
	}
	for _, sp := range records.Catalog() {
		if !mineCatalogNames[sp.Method] {
			continue
		}
		name := MethodName(sp.Method)
		if _, ok := t.methods[name]; ok {
			continue // a re-registration is a no-op, not a second door
		}
		t.register(&Method{Name: name, RequiresInitialize: true, Handle: t.handleRecord(sp)})
	}
	return true
}

// ── wire shapes (the catalog schemas' Go mirrors; handleRecord forwards
// raw JSON and the service decodes these same shapes strictly, so the
// schemas and the service refuse the same bytes) ────────────────────────

// LearningsMineQueryParams is the readable-set browse: query text (ranks,
// never widens — D6), tag patterns (the same AND-of-patterns the person's
// query uses), and a limit. There is NO scope or scopeKey: the readable
// set is resolved from the stamped identity, never chosen (or widened) by
// the caller.
type LearningsMineQueryParams struct {
	QueryText string         `json:"queryText,omitempty"`
	Tags      []LearningsTag `json:"tags,omitempty"`
	Limit     int            `json:"limit,omitempty"`
}

// LearningsMineShowParams names one learning by its positive id.
type LearningsMineShowParams struct {
	ID int64 `json:"id"`
}

// LearningsMineAddParams is the agent's own filing. Provenance is bounded
// by the schema's enum to observed|assumed — told says "a person said it"
// and relayed is the board hook's word; neither is nameable here by a
// field OR by the service (LM4 checks again below the schema). scope may
// be "agent" (the caller's own slug, STAMPED — a scopeKey naming another
// agent is refused, not honoured) or "project" (the identity's own root,
// same law).
type LearningsMineAddParams struct {
	Scope      string   `json:"scope"`
	ScopeKey   string   `json:"scopeKey,omitempty"`
	Body       string   `json:"body"`
	Tags       []string `json:"tags,omitempty"`
	Provenance string   `json:"provenance"`
}

// LearningsMineProposeRetractParams is the ask: WHICH learning and WHY.
// The payload's id+reason become the learning_retract proposal verbatim;
// the committer's stamp, not these bytes, decides who retracts.
type LearningsMineProposeRetractParams struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// ── the shared renderings (rule 4): the tool arm and the standing
// learnings.forAgent door answer in the SAME bytes, produced by ONE
// function each ──────────────────────────────────────────────────────────

// AnswerLearningsForAgent renders hits into LM5's answer — the ONE
// forAgent rendering. The learnings.forAgent door and the agent's own
// learning_query twin both call it, so "what the prompt would show" and
// "what the agent asks and sees" are one renderer's output, not two that
// agree by luck.
func AnswerLearningsForAgent(hits []learnings.ForAgentHit) LearningsForAgentResult {
	out := make([]LearningsForAgentHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, LearningsForAgentHit{
			Learning: learningsView(h.Learning),
			Why:      LearningsForAgentWhy{Rank: h.Why.Rank, M1Note: h.Why.M1Note, Basis: h.Why.Basis},
		})
	}
	return LearningsForAgentResult{Hits: out}
}

// AnswerLearningsItem renders one learning — the view the person's add
// door already answers with, exported so the agent's own add door and show
// twin file and read in the same words.
func AnswerLearningsItem(lr learnings.Learning) LearningsItemResult {
	return LearningsItemResult{Learning: learningsView(lr)}
}
