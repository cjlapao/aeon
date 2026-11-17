package protocol

// platform_door.go — S54 T1: the actor and the audit for the four
// derived-store OPERATIONS (retrieval.reindex, graph.build, graph.reparse,
// graph.delete).
//
// # What this file owns
//
// SP3's two protocol halves and nothing else: WHO may start an operation
// (the person at the endpoint — the same human-door shape PL15/S45 drew,
// S46/S50/S57/S59 carried to their planes), and the audit row a STARTED
// operation carries (op, composed subject, fixed summary template, actor).
// The AUDIT WRITE itself is not here: it rides the same transaction as the
// start record, below the seams (store.InsertAsyncWorkerAudited,
// graph's audited deletes) — the door composes, the store commits, one tx.
//
// # The actor is the connection, never the params
//
// `platformActor` mirrors `authoringActor` point for point (the daemon
// wires ActorFor from the peer credential; uid 0 is an unattributable
// peer and is refused, not dressed in a real-looking identity) with ONE
// addition: the audit's actor_kind CHECK admits only 'user' and 'cli', so
// an actor of any other kind — an agent above all — is refused here,
// BEFORE anything starts (SP3/SP4: no agent edge reaches these doors;
// S54's whole claim is that a person pressed the button).
//
// The four doors take no identity keys in params and never did: their
// param structs have no actor field to fill (S59's reject-by-key came to
// planes whose params NAMED such keys; these never grew one).
//
// # Absent, not refusing (V13)
//
// The three graph WRITES and retrieval.reindex register only when BOTH
// their working seam and ActorFor are wired. A table built without a
// stamping source has no persons, so it has no door that could honestly
// say WHO started a reindex — the method is out of the table, exactly as
// the MCP/policy/roadmap authoring doors argue. The graph READS keep
// their old gate (the Graph seam alone): reads are not operations, they
// audit nothing, and SP6 draws the line at the acts.

import (
	"context"
	"strings"

	"github.com/cjlapao/aeon/internal/asyncworkers"
)

// ErrPlatformNoActor is the human-door refusal of S54's four doors (-31956, the
// first code of the S54 block — re-anchored in build, see errors.go): no
// ActorFor wired, an empty id, uid 0, or a non-human kind. Sibling shape
// and sibling reason to ErrPolicyNoActor / ErrMcpNoActor /
// ErrRoadmapNoActor — a write this plane records must name a person, and
// "the audit would name nobody" stops the start before the ledger moves.
var ErrPlatformNoActor = &Error{
	Code:    -31956,
	Message: "no person at the endpoint for this platform operation",
	Data: Data{
		What:  "retrieval.reindex, graph.build, graph.reparse or graph.delete reached its door without a lawful human actor: no stamping source on the table, an empty or uid-0 peer id, or an actor kind the platform audit cannot name (SP3 admits 'user' and 'cli' only)",
		Means: "nothing was started — no worker queued, no rows touched, no audit row",
		Todo:  "reconnect as the person who owns the daemon's socket (the 0600 endpoint carries your uid as the actor); an agent has no door onto these operations at all",
		Fatal: false,
	},
}

// platformActor stamps who started the operation from the CONNECTION. The
// first three refusals are authoringActor's, verbatim in reason; the kind
// check is S54's, because the audit's CHECK says 'user' or 'cli' and an
// audit row that SQL would reject must be refused before the act, not
// after.
func (t *Table) platformActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil || sess == nil {
		return Actor{}, ErrPlatformNoActor
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" || a.ID == "0" {
		return Actor{}, ErrPlatformNoActor
	}
	if a.Kind != "user" && a.Kind != "cli" {
		return Actor{}, ErrPlatformNoActor.WithWhat(
			"the actor at this endpoint has kind %q — the platform audit names people ('user' from the socket peer, 'cli' from the offline direct door) and nothing else; an agent never starts a derived-store operation (SP3, SP4)", a.Kind)
	}
	return a, nil
}

// reindexAudit composes the audit half for a retrieval.reindex START: the
// subject is the flight's own key (root, NUL, corpus) so the ledger row,
// the audit row and the single-flight index speak one identity, and the
// summary is the fixed template over the CLOSED corpus word — never the
// root, never a caller string (SP3 ids-and-templates rule; SEC-033).
func reindexAudit(absRoot, corpus string, actor Actor) asyncworkers.Audit {
	return asyncworkers.Audit{
		Op:      "reindex",
		Subject: absRoot + "\x00" + corpus,
		Summary: "retrieval.reindex started for corpus " + corpus,
		Actor:   asyncworkers.AuditActor{Kind: actor.Kind, ID: actor.ID},
	}
}

// graphBuildAudit composes the audit half for a graph.build START. The
// subject is the absolute root — the same string that is the build's
// dedupe key and the graph rows' component_root.
func graphBuildAudit(absRoot string, actor Actor) asyncworkers.Audit {
	return asyncworkers.Audit{
		Op:      "graph_build",
		Subject: absRoot,
		Summary: "graph.build started",
		Actor:   asyncworkers.AuditActor{Kind: actor.Kind, ID: actor.ID},
	}
}

// starterWords renders the "already running — started by WHO, when" half
// of the accepted answers (SP3's naming of the flying pass). Best-effort
// by design: started:false is a DATA answer that never rides on the audit
// read, so a lookup fault — or a write-hook pass that has no audit row at
// all (SP4: the hook is the daemon's act, nobody's) — leaves the honest
// no-name answer rather than lying, failing, or stalling the door.
func (t *Table) starterWords(ctx context.Context, workerID string) (by, at string) {
	if t.AsyncWorkers == nil {
		return "", ""
	}
	st, err := t.AsyncWorkers.Starter(ctx, workerID)
	if err != nil || st == nil {
		return "", ""
	}
	return st.Kind + ":" + st.ID, st.At
}
