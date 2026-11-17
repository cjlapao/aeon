// records_triage.go — S51 T5 (TM6): the three bug arms of the records
// service. Two are reads that forward through the SAME answers the person's
// socket doors run — bug_list calls the daemon's one triageBoard closure
// (the very function value Table.TriageBoard holds, so the board the step
// reads is byte-for-byte the board the person reads), and bug_show runs
// protocol.AnswerTriageShow, the one rendering handleTriageShow now calls
// (rule 4 — one implementation, two doors). The third is the slice's one
// agent filing: bug_file files an UNVALIDATED, priority-unknown claim
// through protocol.AnswerTriageFileAs — the SAME CreateAudited the person's
// triage.file runs — with the filer STAMPED as the agent's slug (resolved
// against the agent table; identity is never guessed, TM3), guarded first
// by TM6's counted cap (triage.MaxOpenClaimsPerAgent, the refusal naming
// the constant and the way to list them).
//
// Three things this file does NOT do, and each is a rule not an omission:
//
//   - it never reaches a state door: priority, duplicate, validation and
//     hand-off are T4's pinned person doors; the filing arm builds its
//     NewBug field by field from the three documented params, so a
//     smuggled priority/validation/duplicate has nowhere to ride even past
//     the strict decode — the shape itself cannot carry one (TM6);
//   - it never takes an identity from the input: filed_by and the audit's
//     agent actor both come from the STAMPED step (OL3), and an id that
//     names no agent row is refused, not filed under;
//   - a person never passes: Do's person half refuses bug_file by name
//     with the pointer sentence (triage.file is the person's door), and the
//     two reads point a person at their own unscoped doors — the
//     roadmap/automation family's shape, carried.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageList answers bug_list through the SAME board answer the person's
// triage.board door runs — the daemon's one triageBoard closure, wired at
// initTriage beside the Table seam, so the tool door and the socket door
// cannot drift. The unknown-filter refusals ride through verbatim (P3).
func (r *RecordsService) triageList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.triageBoard == nil {
		return nil, triagePlanelessRefusal("bug_list"), nil
	}
	var in protocol.TriageBoardParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.triageBoard(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// triageShow answers bug_show through protocol.AnswerTriageShow — the one
// rendering the person's triage.show door runs (unknown-row refusal and
// all, in the plane's own words).
func (r *RecordsService) triageShow(_ context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.triagePlane == nil {
		return nil, triagePlanelessRefusal("bug_show"), nil
	}
	var in protocol.TriageShowParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerTriageShow(r.triagePlane, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// triageFile files the agent's claim — the triage plane's ONE agent door
// (TM6). The order is deliberate: shape (strict decode — three fields,
// nothing else), then identity (the stamped id must name an agent row;
// filed_by is a SLUG, so a step that speaks a name is resolved through the
// agent table and a stranger is refused by name), then the CAP (TM6's rate
// law, counted at the store, the refusal naming triage.MaxOpenClaimsPerAgent
// and bug_list as the way to see them), and only then the SAME
// CreateAudited the person's door runs — unvalidated by the store's
// default, priority-unknown by the honest empty, filed_by the slug, one
// audit row op='file' actor_kind='agent' (the actor law the store itself
// enforces, T1's CHECK). Nothing here chooses a state the person owns.
func (r *RecordsService) triageFile(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.triagePlane == nil {
		return nil, triagePlanelessRefusal("bug_file"), nil
	}
	var in protocol.TriageFileAsParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	// The filer comes from the STAMPED step (OL3) — Do resolved it through
	// the step ctx; params and the caller-passed actor carry no identity.
	slug, f2 := r.agentSlug(ident.Agent)
	if f2 != nil {
		return nil, f2, nil
	}
	held, err := r.st.CountOpenClaimsByFiler(slug)
	if err != nil {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the open claims could not be counted (%v) — the cap is enforced by counting, so a store that cannot answer stops the write", err)}, nil
	}
	if held >= triage.MaxOpenClaimsPerAgent {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: agent %s already holds %d open unvalidated claims — that is the cap (triage.MaxOpenClaimsPerAgent = %d); bug_list with validation=\"unvalidated\" shows them, and a person validating or closing one opens room again", slug, held, triage.MaxOpenClaimsPerAgent)}, nil
	}
	out, err := protocol.AnswerTriageFileAs(r.triagePlane, triage.Actor{Kind: "agent", ID: slug}, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// agentSlug resolves the stamped step id to an agent SLUG — filed_by is a
// slug (T1's conversion made the column's canonical shape the slug), the
// step may speak either, and an id that names no agent at all is refused:
// an unattributable filing is absent, not filed under a guess (OL3's
// sharper half — identity is stamped AND real).
func (r *RecordsService) agentSlug(id string) (string, *Failure) {
	if id == "" {
		return "", &Failure{Sentence: "refused: the step named no agent — a filing without an attributable filer is not filed (OL3)"}
	}
	if _, found, err := r.st.AgentBySlug(id); err == nil && found {
		return id, nil
	}
	if a, found, err := r.st.AgentByName(id); err == nil && found {
		return a.Slug, nil
	}
	return "", &Failure{Sentence: fmt.Sprintf("refused: %q names no agent (slug or name) — filed_by is the stamped agent's slug and identity is never guessed", id)}
}

// triagePlanelessRefusal is the V13 shape for the family: the triage plane
// is what honours these doors (the board's attempts and candidates, the
// show view, the shared Create), and a mount without it says so, never
// answering an empty backlog or filing into nothing.
func triagePlanelessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the triage plane, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}
