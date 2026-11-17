package daemon

// records_blueprint.go — S48 T10 (BP9): the five blueprint arms of the
// records service. Four are reads that forward to the SAME services the
// person's socket doors answer through — the standing validate seam and
// T7's one read service — so the tool door and the socket door cannot
// drift field for field (rule 4; TestToolReadsEqualSocketReads pins it
// byte-for-byte). The fifth, blueprint_propose, files the INERT
// blueprint_save row through the shared create path: the shared create
// validator (TD3 — the save door's own Analyze, the report verbatim on
// refusal), the per-agent open-proposal cap (S45's named constant, the
// counted refusal naming it and proposal_list), and the author STAMPED
// from the step's identity — never typed (OL3). No arm here saves,
// duplicates, deletes, reverts or commits (rule 3; T8's guard refuses
// such a tool by name), and the two text-checking arms persist NOTHING —
// not a row, not a revision, not a proposal (the spy test pins it).
//
// The size caps are the save door's own numbers, enforced HERE by name:
// a candidate over 1 MiB is refused before the validator runs, because a
// check the desk answers generically ("payload too large") is less
// actionable than the door's own sentence. validate and lint answer from
// the catalog's absence honestly: an unwired read service is a stated
// refusal, never an empty all-clear (the V13 shape, rule 4's tail).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
)

// maxBlueprintContentBytes is the save door's source ceiling, re-declared
// where the tools meet it (blueprint_validate and blueprint_lint take text;
// blueprint_propose carries the whole source). The desk kind file holds the
// proposal-side bound; this is the tool door's.
const maxBlueprintContentBytes = 1 << 20

// maxBlueprintNoteBytes is blueprint_propose's note ceiling — a note a
// person reads at the desk, not a second source.
const maxBlueprintNoteBytes = 256

// blueprintList answers through the standing list door — the same
// blueprintReads.List the socket's blueprint.list handler runs.
func (r *RecordsService) blueprintList(_ context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.listBlueprints == nil {
		return nil, blueprintsViewlessRefusal("blueprint_list"), nil
	}
	var in protocol.BlueprintListParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.listBlueprints(in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// blueprintShow answers through the standing show door (full YAML
// verbatim, the report, what it references).
func (r *RecordsService) blueprintShow(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.showBlueprint == nil {
		return nil, blueprintsViewlessRefusal("blueprint_show"), nil
	}
	var in struct {
		Name string `json:"name"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, &Failure{Sentence: "refused: blueprint_show needs a non-blank name — the schema requires it and an empty one is not a read"}, nil
	}
	out, err := r.showBlueprint(protocol.BlueprintShowParams{Name: in.Name})
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// blueprintValidate runs the standing validate seam over TEXT the caller
// carries, over the size cap, and persists nothing. The refusal is the
// one validator's own report, verbatim (the seam already renders it).
func (r *RecordsService) blueprintValidate(_ context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.validateBlueprint == nil {
		return nil, blueprintsViewlessRefusal("blueprint_validate"), nil
	}
	var in struct {
		Content string `json:"content"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Content == "" {
		return nil, &Failure{Sentence: "refused: blueprint_validate needs non-empty content — the schema requires it; validate a stored blueprint by name through blueprint_show's report instead"}, nil
	}
	if len(in.Content) > maxBlueprintContentBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: blueprint_validate input is %d bytes and the cap is %d (1 MiB) — a source that large is not a candidate, it is an archive", len(in.Content), maxBlueprintContentBytes)}, nil
	}
	out, err := r.validateBlueprint(protocol.BlueprintValidateParams{Content: in.Content})
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// blueprintLint answers the ADVISORY policy lint from the same read
// service the person's door runs — evaluatedAt dated, the unavailable
// state stated when the catalog view is unwired (never a silent
// all-clear). Text or name, size-capped, zero persistence.
func (r *RecordsService) blueprintLint(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.blueprints == nil {
		return nil, blueprintsViewlessRefusal("blueprint_lint"), nil
	}
	var in protocol.BlueprintLintParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Name == "" && in.Content == "" {
		return nil, &Failure{Sentence: "refused: blueprint_lint needs a stored name or content to lint — neither given is not an all-clear"}, nil
	}
	if len(in.Content) > maxBlueprintContentBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: blueprint_lint input is %d bytes and the cap is %d (1 MiB) — lint the stored row by name instead", len(in.Content), maxBlueprintContentBytes)}, nil
	}
	out, err := r.blueprints.Lint(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// blueprintPropose files the INERT row. The shape refuses HERE what the
// schema refuses at parse (blank name, empty source, oversized note), the
// shared create validator refuses what the SAVE door would refuse —
// verbatim, TD3 — and the cap refuses what a runaway loop would file.
// Author: the stamped step identity (by), carried to created_by_*; the
// COMMITTER, not this door, will own the row's actor when a person decides
// (T9). Nothing outside the proposal tables is touched (BP9).
func (r *RecordsService) blueprintPropose(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in struct {
		Name       string `json:"name"`
		Content    string `json:"content"`
		CreateOnly bool   `json:"create_only"`
		Note       string `json:"note"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, &Failure{Sentence: "refused: blueprint_propose needs a non-blank name — the schema requires it"}, nil
	}
	if in.Content == "" {
		return nil, &Failure{Sentence: "refused: blueprint_propose needs the FULL proposed source in content — never a patch, never empty (a blueprint is decided whole)"}, nil
	}
	if len(in.Content) > maxBlueprintContentBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: blueprint_propose content is %d bytes and the cap is %d (1 MiB) — the save door's own ceiling; a source that large is not a candidate", len(in.Content), maxBlueprintContentBytes)}, nil
	}
	if len(in.Note) > maxBlueprintNoteBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: blueprint_propose note is %d bytes and the cap is %d — a note a person reads at the desk, not a second source", len(in.Note), maxBlueprintNoteBytes)}, nil
	}
	// The payload is BUILT here, field by field — never the raw input
	// carried through. The keys are the desk kind's (createOnly is camel,
	// the schema's create_only is the tool's); a smuggled "mode", "origin"
	// or "actor" has already met the strict decode above and meets the
	// desk's parse refusal below if it somehow arrives.
	payload, merr := json.Marshal(map[string]any{
		"name":       in.Name,
		"content":    in.Content,
		"createOnly": in.CreateOnly,
		"note":       in.Note,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: blueprint_propose payload will not encode: %w", merr)
	}
	if len(payload) > desk.MaxBlueprintSourceBytes+8*1024 {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the blueprint_save payload is %d bytes and the cap is %d (1 MiB source + 8 KiB of JSON around it) — a runaway loop must not bury the desk", len(payload), desk.MaxBlueprintSourceBytes+8*1024)}, nil
	}
	if f := r.openProposalsCap(by); f != nil {
		return nil, f, nil
	}
	if err := r.validateProposal(ctx, string(desk.KindBlueprintSave), string(payload)); err != nil {
		// The validator's sentence travels VERBATIM (TD3: what create
		// refuses, commit refuses, in the same words — and an invalid
		// candidate never becomes an inert row).
		return nil, &Failure{Sentence: err.Error()}, nil
	}
	row, err := r.proposals.CreateProposal(ctx, string(desk.KindBlueprintSave), payload, by.Kind, by.ID)
	if err != nil {
		return nil, nil, err
	}
	row.Command = protocol.ProposalCommand(row.ID)
	return protocol.PolicyProposeResult{Proposal: row, Note: protocol.PolicyProposeInertNote}, nil, nil
}

// blueprintsViewlessRefusal is the V13 shape for the family: the read
// service (or the validate seam) is what honours these doors, and a
// mount without it says so, never answering zero or an all-clear.
func blueprintsViewlessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the blueprint read service, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}
