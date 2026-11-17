package protocol

// agent_management.go — S43 T7: the eight agent-management methods forward
// to Table.AgentManage, the one-seam-many-methods shape agentTransitionCall
// already uses (agent_transition.go). internal/agentmgr is not importable
// from this package (deletability, like agentsvc), so the real logic lives
// in cmd/aeon, which builds the closure over internal/agentmgr and wires
// Table.AgentManage. The method name is bound at REGISTRATION time (the
// constant beside the registration is its only source) — there is no name in
// params to trust, so a caller cannot claim to be another method.
//
// S73 adds the second half of that sentence (T4's steps 1–4, landed inside
// T3: once the store's writers became audited doors, `cmd/aeon` had no
// placeholder actor left to compile against — T3's Definition of done names
// exactly this choice, and its Notes record that it was taken). The six
// WRITES of this family change an `agent` row, and AR1 says no such change is
// nameless — so the handler that used to receive the `*Session` and throw it
// away now keeps it, and for a write stamps the connection's peer into the
// context before the seam runs. The seam reads the stamp back (`AgentActor`)
// and refuses its absence; a road that bypassed the door therefore cannot
// write, which is the whole point of putting the actor in the context rather
// than in a parameter list someone could fill in. The three READS (`view`,
// `rows`, `changes`) take no actor at all: SP6 draws the line at the acts,
// not at the reading (platform_door.go:53-57 says the same thing for its own
// plane).

import (
	"context"
	"encoding/json"
	"fmt"
)

// ErrAgentNoActor is an agent-management WRITE that could not name the person
// pressing it (S73 T4's step 3, spent by T3, AR4). S73 owns -31970…-31972:
// measured 2026-11-06, the deepest code REGISTERED in the table anywhere in
// the tree is -31965 (`grep -rhoE 'Code:[[:space:]]*-[0-9]+' internal/protocol
// | sort -n` → -31970 is this one, -31965 next, then -31964; -31998 is a test's
// invented code and -31999 is AeonCodeMin, a bound, not an error). S61's own
// comment reserves -31966…-31969, so this slice starts its block one past that
// reservation rather than spending inside another slice's — the S47/S59
// convention, that a slice owns its block and a task spends only what it says.
var ErrAgentNoActor = &Error{
	Code: -31970,
	Data: Data{
		What:  "an agent-management write reached the daemon over a connection that resolves to no peer identity — uid 0, a runtime that wired no actor stamp, or a dispatch path that skipped the door",
		Means: "nothing was written: no agent row changed and no agent_change row was minted, because AR1 has no word for a change nobody performed and the daemon will not invent one to satisfy a request",
		Todo:  "ask over a connection that carries a person (the endpoint's peer credential is the only source; there is no actor parameter to send), or — for the direct door — run the verb on this machine, where the CLI stamps itself honestly",
	},
	Message: "the agent-management write has no nameable author",
}

// agentWriteMethods is the family's WRITE set: the six methods that change an
// `agent` row and therefore owe the ledger a name. Reads are absent by design,
// and the absence is the claim (SP6). `export` is absent too — it writes a
// file, not a row (AR4).
var agentWriteMethods = map[MethodName]bool{
	MethodAgentCreate:    true,
	MethodAgentDuplicate: true,
	MethodAgentEdit:      true,
	MethodAgentDelete:    true,
	MethodAgentImport:    true,
	MethodAgentMigrate:   true,
}

// --- the actor channel (the seam reads what the handler stamped) -------------

type agentActorCtxKey struct{}

// WithAgentActor stamps the person behind an agent-management write into the
// context. Exported for the dispatch layer and for the two doors that do not
// run through a Session at all — the CLI's direct door, which stamps `cli:cli`,
// and the desk's commit effect, which stamps the COMMITTER (AR4 paths 7 and 8).
// Handlers, never params, are the only source of identity.
func WithAgentActor(ctx context.Context, kind, id string) context.Context {
	return context.WithValue(ctx, agentActorCtxKey{}, Actor{Kind: kind, ID: id})
}

// AgentActor reads the stamp back. ok=false means NOBODY pressed this door from
// the protocol's point of view, and a write seam must refuse rather than guess.
func AgentActor(ctx context.Context) (kind, id string, ok bool) {
	a, ok := ctx.Value(agentActorCtxKey{}).(Actor)
	if !ok || a.Kind == "" || a.ID == "" {
		return "", "", false
	}
	return a.Kind, a.ID, true
}

// agentWriteEdge is this family's stamping edge, the sibling of mcpWriteEdge
// (mcp_admin.go:698-713) and the reason `Options.AgentManage`'s signature never
// changed. It does the three things that must happen while the Session still
// exists: refuse an identity smuggled in as a param, resolve the peer, and
// refuse a peer that is not a nameable someone (no ActorFor wired, uid 0).
//
// It does NOT decode params: this family's shape decoding belongs to the seam,
// where each verb's own shape lives, and a decode failure there already answers
// with the voice the family has always used.
func (t *Table) agentWriteEdge(ctx context.Context, sess *Session, name MethodName, params json.RawMessage) (context.Context, error) {
	if err := rejectAgentIdentityKeys(string(name), params); err != nil {
		return nil, err
	}
	if t.ActorFor == nil {
		return nil, ErrAgentNoActor.WithWhat("the agent write needs a person, and this runtime wired no actor stamp at all — the door is registered but unattributable, which is refused, not half-open")
	}
	actor := t.ActorFor(ctx, sess)
	if actor.Kind == "" || actor.ID == "" || actor.ID == "0" {
		return nil, ErrAgentNoActor.WithWhat("the agent write needs a person, and this connection resolved to no peer identity (uid 0) — the actor is the kernel's peer credential, never a param, and an unresolvable peer is refused rather than stamped \"user 0\"")
	}
	return WithAgentActor(ctx, actor.Kind, actor.ID), nil
}

// agentIdentityKeys is the CLOSED set of param keys the six write doors refuse
// by name before anything else runs — S59's identity-vocabulary move, kept
// narrow on purpose (four keys, not a grammar). They are not "unknown keys":
// T3's forward-compat rule says an unknown key must be ignored, and it is. These
// four share one property, which is that they ask the daemon to record an
// identity the caller chose, and the answer to that is always the same sentence.
// The nested `fields` blob of agent.edit is NOT walked, and that is recorded, not
// overlooked: design deviation 8 says so in words, and S73 does not silently
// narrow a shipped verb while it is adding a ledger.
var agentIdentityKeys = []string{"actor", "actorKind", "createdBy", "updatedBy"}

func rejectAgentIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // the seam's own decode reports a malformed envelope, in its own words
	}
	for _, key := range agentIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor is the connection's peer credential and the row's timestamps are the daemon's, never named by the caller (S73 AR3)",
				method, key)}
		}
	}
	return nil
}

// agentManageCall builds the ONE handler shape the family's registrations share.
// nil means half-wired (the methods answer ErrNotWired by name, never silently
// permissive — P34's "the daemon is the primary surface"), the same discipline
// AgentImportPreview states.
func (t *Table) agentManageCall(name MethodName) func(context.Context, *Session, json.RawMessage) (any, error) {
	return func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
		if t.AgentManage == nil {
			return nil, ErrNotWired.WithWhat("agent management is not wired — the daemon started without the entry point's seam (cmd/aeon did not wire Table.AgentManage); `aeon serve` wires it, a hand-built Table does not")
		}
		if agentWriteMethods[name] {
			stamped, err := t.agentWriteEdge(ctx, sess, name, params)
			if err != nil {
				return nil, err
			}
			ctx = stamped
		}
		// An edit or a delete changes the agent's tools (or removes them), so the rules that go with
		// them (agent_grants.go) are worked out from what the agent held BEFORE.
		var before *agentGrantState
		target := agentSlugParam(params)
		if name == MethodAgentEdit || name == MethodAgentDelete {
			if st, ok := t.readAgentGrantState(ctx, target); ok {
				before = &st
			}
		}
		result, refusal, err := t.AgentManage(ctx, string(name), params)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			return nil, ErrAgentRefused.WithWhat("%s", refusal)
		}
		switch name {
		case MethodAgentCreate, MethodAgentDuplicate:
			if after, ok := agentGrantStateOf(result); ok {
				if gerr := t.syncAgentGrants(ctx, sess, nil, &after); gerr != nil {
					return nil, gerr
				}
			}
		case MethodAgentEdit:
			after, ok := t.readAgentGrantState(ctx, target)
			if ok {
				if gerr := t.syncAgentGrants(ctx, sess, before, &after); gerr != nil {
					return nil, gerr
				}
			}
		case MethodAgentDelete:
			if before != nil {
				if gerr := t.syncAgentGrants(ctx, sess, before, nil); gerr != nil {
					return nil, gerr
				}
			}
		}
		return result, nil
	}
}

// agentSlugParam reads the `slug` an agent door was asked about ("" when there is none).
func agentSlugParam(params json.RawMessage) string {
	var p struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	return p.Slug
}
