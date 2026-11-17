package daemon

// blueprint_write.go — S48 T6 (BP4, BP6, BP7, BP10, BP11, BP12): the ONE
// blueprint write service behind the four socket doors (save, duplicate,
// delete, revert). It is the ONLY daemon file that calls
// store.BlueprintPutTx / store.BlueprintDeleteTx (a test pins the call
// sites); every changing write lands row + next revision + derived refs in
// ONE transaction (BP7, BP11), and nothing outside this file writes them.
//
// The validator is the UNCHANGED blueprint.Analyze with the same options the
// validate seam has always used (BP4): the candidate is judged exactly as
// `blueprint.validate` judges it, and a refusal carries the report VERBATIM
// in the typed error's data.what — no second validator, no paraphrase.
// Identical content is not an act: changed:false answers with NO new
// revision (audit is a record of acts, not of calls). Origin is the store's
// to keep (BlueprintPutTx keeps it on conflict): a person's edit of a system
// row stays system (BP6) — the seed ledger reads it diverged and the next
// apply skips it. The delete refuses shipped rows, and refuses while ANY
// live automation names the blueprint, naming every refuser (BP10).
//
// Pinned loops (loop_blueprint) are not touched by any door here (BP-
// pinned): loops started from a blueprint carry their own bytes; the delete
// says so in its answer.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprintWrites is the daemon's one write service. It holds only the
// store: Analyze is the validation and the registry readers (handler,
// action) are the same package-level doors the validate seam always used.
type blueprintWrites struct {
	store *store.Store
}

// newBlueprintWrites builds the service; initBlueprints wires it onto
// Table.SaveBlueprint and Table.BlueprintWrites and calls
// RegisterBlueprintManage.
func newBlueprintWrites(s *store.Store) *blueprintWrites { return &blueprintWrites{store: s} }

// blueprintValidationOptions is the ONE options builder the Analyze calls
// share: mode defaults to "standard", the registries are read live (the
// next loop.start sees a new agent name with no restart, BP-live).
func blueprintValidationOptions(mode string) blueprint.ValidationOptions {
	if mode == "" {
		mode = "standard"
	}
	return blueprint.ValidationOptions{
		Mode:       mode,
		Agents:     handler.Names(),
		Resolvers:  handler.Resolvers(),
		Actions:    action.Names(),
		AgentTools: handler.ToolsFor,
	}
}

// blueprintRefused is the one refusal constructor: the family's code with
// the closed-set kind and the sentence (or the verbatim report) in What.
func blueprintRefused(kind, format string, args ...any) *protocol.Error {
	e := protocol.ErrBlueprintRefused.WithWhat(format, args...)
	e.Data.Kind = kind
	return e
}

// SeedSystem puts SHIPPED standard bytes on the row side at boot (the
// arrival net, T5's D-T5-4 deviation): origin system, actor
// system:blueprints.standard, op seed. It is deliberately NOT a human door
// — no Analyze (shipped content is a build-time guarantee, and judging it
// against a live registry would make boot a race with the registries) and
// no peer actor (the row says what it is). It lives in this file because
// this file is the daemon's only writer (BP7's guard).
func (w *blueprintWrites) SeedSystem(ctx context.Context, name, content string) error {
	bp, err := blueprint.Load([]byte(content))
	if err != nil {
		return fmt.Errorf("blueprint seed: shipped %q does not load (build bug): %w", name, err)
	}
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	// The row's version is the content's own (two-step ships version 2) —
	// hardcoding 1 here would put the live row out of reach of the seed
	// fingerprint's applied==live test (T6 measured it).
	row := store.BlueprintRow{Name: name, Version: bp.Version, Content: content, ContentHash: hash, Origin: "system", UpdatedBy: "system:blueprints.standard"}
	rev := store.RevisionRow{Version: bp.Version, Content: content, ContentHash: hash, Op: "seed", ActorKind: "system", ActorID: "blueprints.standard"}
	var refs []store.RefRow
	for _, r := range blueprint.References(&bp) {
		refs = append(refs, store.RefRow{Name: name, Kind: r.Kind, Target: r.Target})
	}
	return w.store.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, refs)
	})
}

// --- save -------------------------------------------------------------------

// Save is the save door's service half: the name grammar, the UNCHANGED
// Analyze, the id/name agreement (the cross-check T4 deferred: an
// adoption-time file's row must answer to the name its bytes declare, and a
// save is held to the same truth), the createOnly collision, the
// identical-content no-op, and the one-transaction write — in that order,
// with nothing written before every refusal has passed.
func (w *blueprintWrites) Save(ctx context.Context, actor protocol.Actor, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error) {
	return w.candidate(ctx, actor, p.Name, p.Content, p.Mode, "", p.CreateOnly, p.Note)
}

// BlueprintSave is T6's ONE save path handed to the blueprint_save
// proposal effect (S48 T9): the committed effect runs THE SAME function the
// socket door runs — the same Analyze with the same options, the same
// id/name cross-check, the same createOnly collision, the same
// identical-content no-op, the same single transaction — and it stamps the
// COMMITTING PERSON (desk.Commit's decider), never the proposer. The
// guard's allow-list keeps this call inside this file: the write path has
// one door, and a proposal effect walks through it like any other caller.
func BlueprintSave(ctx context.Context, s *store.Store, actor protocol.Actor, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error) {
	return newBlueprintWrites(s).Save(ctx, actor, p)
}

// --- duplicate --------------------------------------------------------------

// Duplicate makes a TEXT copy (blueprint.Duplicate — comments, layout and
// every other byte preserved; only the top-level id and version rewrite)
// and sends it through the SAME Analyze and write path with op='duplicate'.
// `from` unknown is a refusal naming the nearest names; `name` taken is a
// refusal (a duplicate never merges onto a row).
func (w *blueprintWrites) Duplicate(ctx context.Context, actor protocol.Actor, p protocol.BlueprintDuplicateParams) (protocol.BlueprintSaveResult, error) {
	var zero protocol.BlueprintSaveResult
	src, found, err := w.store.BlueprintGet(ctx, p.From)
	if err != nil {
		return zero, err
	}
	if !found {
		names, err := w.store.BlueprintNames(ctx)
		if err != nil {
			return zero, err
		}
		return zero, blueprintRefused("from-unknown", "duplicate source %q has no row — the nearest names are: %s", p.From, nearestToolNames(p.From, names))
	}
	cp, err := blueprint.Duplicate([]byte(src.Content), p.Name)
	if err != nil {
		return zero, blueprintRefused("diagnostics", "%v", err)
	}
	if _, taken, err := w.store.BlueprintGet(ctx, p.Name); err != nil {
		return zero, err
	} else if taken {
		return zero, blueprintRefused("collision", "duplicate name %q is taken — a duplicate never merges onto an existing row; name a new name", p.Name)
	}
	return w.candidate(ctx, actor, p.Name, string(cp), "standard", "duplicate", false, p.Note)
}

// --- delete -----------------------------------------------------------------

// Delete removes the row and its derived refs — never the revisions (they
// outlive the name; re-creating the name continues the numbering, BP7). The
// shipped rows (origin system) are undeletable (BP6): restore or duplicate
// instead. While any LIVE automation names the blueprint — every schedule,
// every trigger, every queue item in a live status — the delete is refused
// naming EVERY refuser (BP10); loops already started never refuse, they are
// reported as a count.
func (w *blueprintWrites) Delete(ctx context.Context, actor protocol.Actor, p protocol.BlueprintDeleteParams) (protocol.BlueprintDeleteResult, error) {
	var zero protocol.BlueprintDeleteResult
	row, found, err := w.store.BlueprintGet(ctx, p.Name)
	if err != nil {
		return zero, err
	}
	if !found {
		names, err := w.store.BlueprintNames(ctx)
		if err != nil {
			return zero, err
		}
		return zero, blueprintRefused("from-unknown", "delete of %q: no such blueprint — the nearest names are: %s", p.Name, nearestToolNames(p.Name, names))
	}
	if row.Origin == "system" {
		return zero, blueprintRefused("system", "a shipped blueprint cannot be deleted; restore or duplicate it")
	}
	refs, err := w.store.BlueprintReferences(ctx, p.Name)
	if err != nil {
		return zero, err
	}
	if refs.Any() {
		return zero, protocol.ErrBlueprintInUse.WithWhat("%s", namedReferences(refs))
	}
	if err := w.store.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		return store.BlueprintDeleteTx(tx, p.Name)
	}); err != nil {
		return zero, err
	}
	return protocol.BlueprintDeleteResult{
		Deleted:            true,
		LoopsStartedFromIt: refs.LoopsStarted,
		Note:               "started loops keep their pinned content",
	}, nil
}

// namedReferences names EVERY live refuser, so the operator knows exactly
// which doors to close before the delete answers (BP10): each schedule by
// id, each trigger by name and id, each live queue item by id and status.
func namedReferences(r store.References) string {
	out := ""
	add := func(part string) {
		if out != "" {
			out += "; "
		}
		out += part
	}
	if len(r.Schedules) > 0 {
		s := "schedules:"
		for _, x := range r.Schedules {
			s += " " + x.ID + " (" + x.Kind + ")"
		}
		add(s)
	}
	if len(r.Triggers) > 0 {
		s := "event triggers:"
		for _, x := range r.Triggers {
			s += " " + x.Name + " (id " + x.ID + ")"
		}
		add(s)
	}
	if len(r.QueueItems) > 0 {
		s := "live queue items:"
		for _, x := range r.QueueItems {
			s += " #" + x.ID + " (status " + x.Status + ")"
		}
		add(s)
	}
	return out
}

// --- revert -----------------------------------------------------------------

// Revert restores a NAMED revision's content as the current row — as a NEW
// revision (op='revert'); history is never rewritten (BP7). The restored
// bytes run through the SAME Analyze: a revision that no longer validates
// (an agent it names was deleted, say) is refused with the report, never
// silently written (the delete-protection gap closing in the other
// direction).
func (w *blueprintWrites) Revert(ctx context.Context, actor protocol.Actor, p protocol.BlueprintRevertParams) (protocol.BlueprintSaveResult, error) {
	var zero protocol.BlueprintSaveResult
	rev, found, err := w.store.RevisionGet(ctx, p.Name, p.Rev)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, blueprintRefused("rev-unknown", "revert of %q: revision r%d does not exist — `aeon blueprint history %s` names the revisions that do", p.Name, p.Rev, p.Name)
	}
	return w.candidate(ctx, actor, p.Name, rev.Content, "standard", "revert", false, p.Note)
}

// --- the one candidate path -----------------------------------------------------

// candidate is the shared Analyze + write path every changing write takes
// (save, duplicate and revert converge here). opOverride names the
// revision op for duplicate/revert; the empty string means infer
// create|save from whether a row exists. It writes NOTHING before every
// refusal has passed, and writes row+revision+refs in exactly one
// transaction, through store.BlueprintPutTx — the file's reason to exist.
func (w *blueprintWrites) candidate(ctx context.Context, actor protocol.Actor, name, content, mode, opOverride string, createOnly bool, note string) (protocol.BlueprintSaveResult, error) {
	var zero protocol.BlueprintSaveResult
	if !blueprint.ValidBlueprintName(name) {
		return zero, protocol.ErrBadParams{Err: fmt.Errorf("blueprint name %q is outside the grammar (lowercase words joined by single hyphens, ≤ 64)", name)}
	}
	if len(content) > 1<<20 {
		return zero, protocol.ErrBadParams{Err: fmt.Errorf("blueprint source exceeds 1 MiB")}
	}
	report := blueprint.Analyze([]byte(content), blueprintValidationOptions(mode))
	raw, err := json.Marshal(report)
	if err != nil {
		return zero, fmt.Errorf("blueprint write: rendering the report: %w", err)
	}
	if !report.OK(false) {
		return zero, blueprintRefused("diagnostics", "%s", string(raw))
	}
	if report.Blueprint != name {
		return zero, blueprintRefused("id-mismatch", "source id %q does not match selected blueprint %q", report.Blueprint, name)
	}
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])

	var (
		result protocol.BlueprintSaveResult
		refs   []store.RefRow
	)
	err = w.store.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		existing, present, err := w.store.BlueprintRowTx(ctx, tx, name)
		if err != nil {
			return err
		}
		maxRev, err := store.BlueprintMaxRevTx(ctx, tx, name)
		if err != nil {
			return err
		}
		// Spec order: the createOnly collision is refused BEFORE the
		// identical-content no-op — createOnly says "this is a CREATE" and
		// a create against an existing name is a collision even when the
		// bytes happen to agree (the caller's intent was creation).
		// Shipped blueprints are LOCKED: their bytes never change. What a person
		// may still choose rides blueprint.override; a full edit is a copy.
		if present && existing.Origin == "system" {
			return blueprintRefused("system", "%q is a shipped blueprint and is locked — override its allowed fields (the route a step uses), or duplicate it to change anything else", name)
		}
		if present && createOnly {
			return blueprintRefused("collision", "create refused: blueprint %q already exists — createOnly never overwrites; save without createOnly edits it", name)
		}
		if present && existing.Content == content {
			// Not an act: identical content writes NO revision and touches
			// no row (audit noise is a lie by inflation).
			result = protocol.BlueprintSaveResult{
				Name: name, Changed: false, Revision: maxRev,
				Origin: existing.Origin, Version: existing.Version,
				Report: json.RawMessage(raw),
			}
			return nil
		}
		op := opOverride
		if op == "" {
			op = "save"
			if !present {
				op = "create"
			}
		}
		bp, err := blueprint.Load([]byte(content))
		if err != nil {
			return blueprintRefused("diagnostics", "an analyzed blueprint refused to load: %v", err)
		}
		refs = nil
		for _, r := range blueprint.References(&bp) {
			refs = append(refs, store.RefRow{Name: name, Kind: r.Kind, Target: r.Target})
		}
		origin := "user"
		if present {
			origin = existing.Origin // kept by the store on conflict; read back for the answer (BP6)
		}
		row := store.BlueprintRow{
			Name: name, Version: report.Version, Content: content, ContentHash: hash,
			Origin: origin, UpdatedBy: actor.Kind + ":" + actor.ID,
		}
		rev := store.RevisionRow{
			Version: report.Version, Content: content, ContentHash: hash,
			Op: op, Note: note, ActorKind: actor.Kind, ActorID: actor.ID,
		}
		if err := store.BlueprintPutTx(tx, row, rev, refs); err != nil {
			return err
		}
		views := make([]protocol.BlueprintRefView, 0, len(refs))
		for _, r := range refs {
			views = append(views, protocol.BlueprintRefView{Kind: r.Kind, Target: r.Target})
		}
		result = protocol.BlueprintSaveResult{
			Name: name, Changed: true, Revision: maxRev + 1,
			Origin: origin, Version: report.Version,
			Report: json.RawMessage(raw), References: views,
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}
