package protocol

// blueprint_manage.go — S48 T6 (BP4, BP6, BP7, BP10, BP11, BP12): the four
// WRITE doors for blueprints as records. blueprint.save is REWRITTEN onto
// rows here (the file writer is gone; the seam keeps its field name so the
// registration and the bench keep working — its implementation is the
// daemon's write service); duplicate, delete and revert are new and register
// CONDITIONALLY over the write service (V13 — a nil seam leaves them ABSENT,
// the RegisterBackup/RegisterSeed shape), RequiresInitialize true, Debug
// false.
//
// The wire shape carries no actor, origin, create-time rev or timestamps:
// the actor is the CONNECTION's person (the daemon's ActorFor seam, the
// kernel's peer credential) and is refused when unresolvable
// (ErrBlueprintNoActor) — nothing half-stamped is ever written (BP12). The
// params decode leniently (T3's additive-evolution rule) and the identity
// KEY BAG is checked — the backup/policy-authoring shape, not
// DisallowUnknownFields.
//
// Every validation refusal carries the Analyze report VERBATIM in
// data.what (the report JSON exactly as Analyze spoke it — the same bytes
// blueprint.validate answers), so no second validator and no paraphrase
// exists between the caller and the truth (BP4).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the params -------------------------------------------------------------------

// BlueprintSaveParams is blueprint.save's request. Content is the YAML text
// the person wrote — stored VERBATIM, never re-serialized (BP-verbatim); the
// 1 MiB bound is the door's, the same size today's file writer refused.
// CreateOnly says "create, never overwrite": a collision is a refusal
// naming the existing name, not a merge. Mode is the validation mode
// (absent means "standard" — the same options as today). Note is a ≤ 256
// byte free-text audit note stored in the revision; it is NEVER interpolated
// into a log or advisory line unescaped (SEC-033 — the door refuses an
// over-long one, and nothing here prints one).
type BlueprintSaveParams struct {
	Name       string `json:"name"`
	Content    string `json:"content"`
	CreateOnly bool   `json:"createOnly,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Note       string `json:"note,omitempty"`
}

// BlueprintDuplicateParams is blueprint.duplicate's request: a TEXT copy
// (blueprint.Duplicate — comments, layout and every other byte preserved;
// only the top-level id and version are rewritten) from an existing name to
// a new one. `from` unknown is a refusal naming the nearest names; `name`
// taken is a refusal (a duplicate never merges onto a row).
type BlueprintDuplicateParams struct {
	From string `json:"from"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

// BlueprintDeleteParams is blueprint.delete's request. One name: the delete
// is scoped to THE blueprint the person is looking at.
type BlueprintDeleteParams struct {
	Name string `json:"name"`
}

// BlueprintRevertParams is blueprint.revert's request: make revision `rev`'s
// content the current row again — as a NEW revision (op=revert). History is
// never rewritten; a revision that no longer validates (an agent it names
// was deleted, say) is refused with the report, never silently written.
type BlueprintRevertParams struct {
	Name string `json:"name"`
	Rev  int    `json:"rev"`
	Note string `json:"note,omitempty"`
}

// --- the results -------------------------------------------------------------------

// BlueprintRefView is one derived reference the save reports back — the same
// rows the write stored (BP11), so the caller sees what the database now
// believes without a second read.
type BlueprintRefView struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

// BlueprintSaveResult is the answer of save, duplicate and revert: the name,
// whether the write CHANGED anything (identical content answers
// changed:false and writes NO revision — audit is a record of acts, not of
// calls), the revision number the write appended (0 when nothing was
// written), the row's provenance origin (system rows stay system through a
// person's edit — BP6; the seed ledger will call it diverged and the next
// apply skips it), the row's version, the Analyze report VERBATIM, and the
// derived refs the same transaction stored.
type BlueprintSaveResult struct {
	Name       string             `json:"name"`
	Changed    bool               `json:"changed"`
	Revision   int                `json:"revision"`
	Origin     string             `json:"origin"`
	Version    int                `json:"version"`
	Report     json.RawMessage    `json:"report"`
	References []BlueprintRefView `json:"references"`
}

// BlueprintDeleteResult is blueprint.delete's answer. LoopsStartedFromIt is
// informational and NEVER a refusal reason: loops started from the blueprint
// carry their pinned content (loop_blueprint is untouched by any of these
// writes), so the note says the truth out loud on every success.
type BlueprintDeleteResult struct {
	Deleted            bool   `json:"deleted"`
	LoopsStartedFromIt int    `json:"loopsStartedFromIt"`
	Note               string `json:"note"`
}

// --- the seam -------------------------------------------------------------------

// BlueprintWriteService is the daemon's ONE blueprint write service (S48 T6,
// internal/daemon/blueprint_write.go — the only daemon file that calls
// store.BlueprintPutTx/BlueprintDeleteTx). Refusals come back as the
// protocol's typed errors (ErrBlueprintRefused / ErrBlueprintNoActor /
// ErrBlueprintInUse) carrying their sentences; a non-nil plain error is an
// infrastructure failure the door cannot name any better.
type BlueprintWriteService interface {
	Save(ctx context.Context, actor Actor, p BlueprintSaveParams) (BlueprintSaveResult, error)
	Duplicate(ctx context.Context, actor Actor, p BlueprintDuplicateParams) (BlueprintSaveResult, error)
	Delete(ctx context.Context, actor Actor, p BlueprintDeleteParams) (BlueprintDeleteResult, error)
	Revert(ctx context.Context, actor Actor, p BlueprintRevertParams) (BlueprintSaveResult, error)
}

// --- registration -------------------------------------------------------------------

// RegisterBlueprintManage registers the three NEW write doors over a non-nil
// seam (V13: a nil seam is the configuration speaking — absent, never
// registered-and-refusing). blueprint.save keeps its existing unconditional
// registration in NewTable; its seam (Table.SaveBlueprint) keeps its field
// name with an updated signature, and this call does not touch it.
// Returns whether the doors are in the table, so the daemon's wiring is
// asserted, not trusted.
func (t *Table) RegisterBlueprintManage() bool {
	if t.BlueprintWrites == nil {
		return false
	}
	t.register(&Method{Name: MethodBlueprintDuplicate, RequiresInitialize: true, Handle: t.handleBlueprintDuplicate})
	t.register(&Method{Name: MethodBlueprintDelete, RequiresInitialize: true, Handle: t.handleBlueprintDelete})
	t.register(&Method{Name: MethodBlueprintRevert, RequiresInitialize: true, Handle: t.handleBlueprintRevert})
	if t.BlueprintOverrides != nil {
		t.register(&Method{Name: MethodBlueprintOverride, RequiresInitialize: true, Handle: t.handleBlueprintOverride})
	}
	return true
}

// --- the door-shared rules ---------------------------------------------------------

// blueprintSeam is the one nil-guard shape the four handlers share:
// registration is conditional on the seam (save excepted — its door answers
// ErrStoreUnavailable when unwired), so a nil here is a registration/seam
// disagreement the guard tests catch — spoken, not silent.
func (t *Table) blueprintWritesSeam() error {
	if t.BlueprintWrites == nil {
		return ErrStoreUnavailable.WithWhat("the blueprint writes need the daemon's write service, and this build did not link it — the surface is absent for the same reason (V13)")
	}
	return nil
}

// blueprintActor stamps the write's author from the CONNECTION (the daemon's
// ActorFor seam), never from params. The honest absence — no identity seam,
// a blank id, or the uid 0 a connection the transport never recorded reads
// as (the mcp shape: uid 0 is refused as unresolvable, never stamped as the
// person "0") — refuses with ErrBlueprintNoActor before anything is read,
// let alone written (BP12).
func (t *Table) blueprintActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBlueprintNoActor.WithWhat("the blueprint write is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	id := strings.TrimSpace(a.ID)
	if id == "" || id == "0" {
		return Actor{}, ErrBlueprintNoActor.WithWhat("the blueprint write is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	return Actor{Kind: "user", ID: id}, nil
}

// maxBlueprintNoteBytes bounds the revision note — a bounded string the
// audit stores, never a blob smuggled in an audit field.
const maxBlueprintNoteBytes = 256

func refuseBlueprintNote(note string) error {
	if len(note) > maxBlueprintNoteBytes {
		return ErrBadParams{Err: fmt.Errorf("note is %d bytes; the bound is %d — it is an audit line, not a document (it is stored verbatim in the revision and never printed unescaped)", len(note), maxBlueprintNoteBytes)}
	}
	return nil
}

// blueprintIdentityKeys names the keys the WRITE params may never carry: the
// actor is stamped by the door that holds the connection, origin is the
// seed's and the adoption's, rev/version are the content's and the store's,
// and the timestamps are the store's — a param that named one would let a
// caller forge provenance (the params-carry-no-actor rule, BP12).
var blueprintIdentityKeys = []string{"actor", "actorKind", "origin", "rev", "version", "createdAt", "updatedAt", "createdBy", "updatedBy"}

// rejectBlueprintIdentityKeys checks the raw key BAG before the lenient
// struct decode (the policy-authoring shape: additive keys are tolerated,
// identity keys never are). Revert excepts rev from the ban — for revert,
// rev is the request (which revision to restore), not a provenance claim.
func rejectBlueprintIdentityKeys(method string, raw json.RawMessage, exceptRev bool) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range blueprintIdentityKeys {
		if key == "rev" && exceptRev {
			continue
		}
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor, origin, version and timestamps are stamped by the daemon and the store, never named by the caller (S48 BP12)",
				method, key)}
		}
	}
	return nil
}

// --- the handlers ----------------------------------------------------------------------

// handleBlueprintSave is the rewritten save door: bounds (name, content,
// size, note) at the door, actor from the peer, and every decision — the
// unchanged Analyze, the id/name agreement, the createOnly collision, the
// identical-content no-op, the row+revision+refs transaction — at the
// daemon's ONE write service. Registration stays exactly where it was.
func (t *Table) handleBlueprintSave(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.SaveBlueprint == nil {
		return nil, ErrStoreUnavailable
	}
	if err := rejectBlueprintIdentityKeys(string(MethodBlueprintSave), raw, false); err != nil {
		return nil, err
	}
	var p BlueprintSaveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" || p.Content == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name and content are required")}
	}
	if len(p.Content) > 1<<20 {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint source exceeds 1 MiB")}
	}
	if err := refuseBlueprintNote(p.Note); err != nil {
		return nil, err
	}
	actor, err := t.blueprintActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.SaveBlueprint(ctx, actor, p)
}

func (t *Table) handleBlueprintDuplicate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.blueprintWritesSeam(); err != nil {
		return nil, err
	}
	if err := rejectBlueprintIdentityKeys(string(MethodBlueprintDuplicate), raw, false); err != nil {
		return nil, err
	}
	var p BlueprintDuplicateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.From == "" || p.Name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("from and name are required — the copy is one existing blueprint made into one new name")}
	}
	if p.From == p.Name {
		return nil, ErrBadParams{Err: fmt.Errorf("duplicate of %q onto itself changes nothing — name a NEW name", p.From)}
	}
	if err := refuseBlueprintNote(p.Note); err != nil {
		return nil, err
	}
	actor, err := t.blueprintActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.BlueprintWrites.Duplicate(ctx, actor, p)
}

func (t *Table) handleBlueprintDelete(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.blueprintWritesSeam(); err != nil {
		return nil, err
	}
	if err := rejectBlueprintIdentityKeys(string(MethodBlueprintDelete), raw, false); err != nil {
		return nil, err
	}
	var p BlueprintDeleteParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name is required — the delete is scoped to one blueprint, never a sweep")}
	}
	actor, err := t.blueprintActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.BlueprintWrites.Delete(ctx, actor, p)
}

func (t *Table) handleBlueprintRevert(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.blueprintWritesSeam(); err != nil {
		return nil, err
	}
	if err := rejectBlueprintIdentityKeys(string(MethodBlueprintRevert), raw, true); err != nil {
		return nil, err
	}
	var p BlueprintRevertParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" || p.Rev < 1 {
		return nil, ErrBadParams{Err: fmt.Errorf("name and a rev of at least 1 are required — revert restores a NAMED revision as a NEW revision")}
	}
	if err := refuseBlueprintNote(p.Note); err != nil {
		return nil, err
	}
	actor, err := t.blueprintActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.BlueprintWrites.Revert(ctx, actor, p)
}

// --- S48 T7: the READ doors (BP7, BP8, BP11) ------------------------------------
//
// Reads write nothing, take no actor and cache nothing. The five below
// register CONDITIONALLY over the daemon's read service (V13 nil⇒absent);
// list and show keep their unconditional registrations and only their
// ANSWERS grew — additively: `names` and `path:""` stay exactly as they
// were (compat pinned by the fixtures), the summaries, report and refs
// join them. The params still decode leniently (T3's rule).

// BlueprintListParams is blueprint.list's request. Origin filters "system"
// or "user" (empty means both); Q is a case-insensitive substring on the
// NAME — filtered DAEMON-side, never by the client.
type BlueprintListParams struct {
	Origin string `json:"origin,omitempty"`
	Q      string `json:"q,omitempty"`
}

// BlueprintSummary is one row of blueprint.list's blueprints array — what
// a person needs to pick a row without fetching every byte. Revision is
// the highest revision the name has (0 before the first append lands);
// UsedByCount counts the derived blueprint_ref rows (what the blueprint
// USES — "who uses it" is blueprint.usage's question).
// DivergedFromShipped is the seed ledger's answer for SYSTEM rows: the
// row's bytes no longer match what the shipped record would apply (false
// for every user row; the field is OMITTED — never a silent false — when
// the daemon has no shipped-record authority wired).
type BlueprintSummary struct {
	Name                string `json:"name"`
	Origin              string `json:"origin"`
	Version             int    `json:"version"`
	Revision            int    `json:"revision"`
	ContentHash         string `json:"contentHash"`
	DivergedFromShipped *bool  `json:"divergedFromShipped,omitempty"`
	UpdatedAt           string `json:"updatedAt"`
	UpdatedBy           string `json:"updatedBy"`
	UsedByCount         int    `json:"usedByCount"`
	// Title is the flow's friendly name ("Developer Loop"); Name stays the slug every
	// reference uses. Empty when the flow has not been given one.
	Title string `json:"title,omitempty"`
	// Description is the flow's own sentence about what it is for; empty when it has none.
	Description string `json:"description,omitempty"`
}

// BlueprintListResult keeps `names` byte-compatibly (the UI's list.names
// is load-bearing, T0) and ADDS the summaries beside it.
type BlueprintListResult struct {
	Names      []string           `json:"names"`
	Blueprints []BlueprintSummary `json:"blueprints"`
}

// BlueprintShowParams is blueprint.show's request (shape unchanged).
type BlueprintShowParams struct {
	Name string `json:"name"`
}

// BlueprintShowResult keeps the old shape (name, content verbatim,
// path:"" — cosmetic since T3) and adds the summary, the Analyze report
// over the STORED content in standard mode (the same bytes
// blueprint.validate answers, BP4), and the derived references.
type BlueprintShowResult struct {
	Name                string             `json:"name"`
	Title               string             `json:"title,omitempty"`
	Description         string             `json:"description,omitempty"`
	Path                string             `json:"path"`
	Content             string             `json:"content"`
	Origin              string             `json:"origin"`
	Version             int                `json:"version"`
	Revision            int                `json:"revision"`
	ContentHash         string             `json:"contentHash"`
	DivergedFromShipped *bool              `json:"divergedFromShipped,omitempty"`
	UpdatedAt           string             `json:"updatedAt"`
	UpdatedBy           string             `json:"updatedBy"`
	Report              json.RawMessage    `json:"report"`
	References          []BlueprintRefView `json:"references"`
	// Locked is true for a shipped blueprint: its bytes never change, and
	// only the Overridable fields can be set (blueprint.override). Overrides
	// are what a person has set so far. Both are absent on a user blueprint.
	Locked      bool                       `json:"locked,omitempty"`
	Overridable []BlueprintLaneOverridable `json:"overridable,omitempty"`
	Overrides   []BlueprintOverrideView    `json:"overrides,omitempty"`
}

// BlueprintLaneOverridable names the fields a person may override on a lane.
type BlueprintLaneOverridable struct {
	Lane   string   `json:"lane"`
	Fields []string `json:"fields"`
}

// BlueprintOverrideView is one override a person has set.
type BlueprintOverrideView struct {
	Lane  string `json:"lane"`
	Field string `json:"field"`
	Value string `json:"value"`
}

// BlueprintUsageParams is blueprint.usage's request.
type BlueprintUsageParams struct {
	Name string `json:"name"`
}

// BlueprintUsageRef is one LIVE thing that names the blueprint — kind is
// "schedule", "webhook", "trigger" or "queue" (queue items are OPEN ones
// only: pending|sizing|ready|running|blocked). LoopsStarted is a count,
// never a list: started loops carry pinned content and are not live users
// (BP10's informational half).
type BlueprintUsageRef struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

// BlueprintUsageResult answers both directions of BP11 at once: what uses
// this blueprint (schedules/triggers/queueItems/loopsStarted) and what it
// uses (agents/actions/skills from the derived blueprint_ref). A blueprint
// nothing uses answers with EMPTY arrays, never an error.
type BlueprintUsageResult struct {
	Schedules    []BlueprintUsageRef `json:"schedules"`
	Triggers     []BlueprintUsageRef `json:"triggers"`
	QueueItems   []BlueprintUsageRef `json:"queueItems"`
	LoopsStarted int                 `json:"loopsStarted"`
	Agents       []string            `json:"agents"`
	Actions      []string            `json:"actions"`
	Skills       []string            `json:"skills"`
}

// BlueprintHistoryParams is blueprint.history's request. Limit caps the
// newest-first window (the daemon's bound is 200; absent means the same).
type BlueprintHistoryParams struct {
	Name  string `json:"name"`
	Limit int    `json:"limit,omitempty"`
}

// BlueprintRevisionView is one revision WITHOUT content — the history
// index. actorId is who the daemon stamped (never a param, BP12).
type BlueprintRevisionView struct {
	Rev         int    `json:"rev"`
	Version     int    `json:"version"`
	Op          string `json:"op"`
	Note        string `json:"note,omitempty"`
	ActorKind   string `json:"actorKind"`
	ActorID     string `json:"actorId"`
	OccurredAt  string `json:"occurredAt"`
	ContentHash string `json:"contentHash"`
}

// BlueprintHistoryResult is newest-first, and survives deletion: a deleted
// blueprint's history stays readable (the revision table is append-only).
type BlueprintHistoryResult struct {
	Revisions []BlueprintRevisionView `json:"revisions"`
}

// BlueprintRevisionParams is blueprint.revision's request.
type BlueprintRevisionParams struct {
	Name string `json:"name"`
	Rev  int    `json:"rev"`
}

// BlueprintRevisionResult is the index entry PLUS the stored bytes,
// verbatim as written — the diff and restore of T7/T6 read from here.
type BlueprintRevisionResult struct {
	BlueprintRevisionView
	Content string `json:"content"`
}

// BlueprintDiffParams is blueprint.diff's request. A and B each name a
// revision number (as text, e.g. "3"), "current" (the row's stored bytes)
// or "shipped" (what the seed would apply — SYSTEM blueprints only; a user
// blueprint has no shipped version and gets a named refusal, not a diff of
// nothing). The diff is computed DAEMON-side by blueprint.Diff (T2) on the
// stored bytes — never client-side.
type BlueprintDiffParams struct {
	Name string `json:"name"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// BlueprintDiffResult: equal is true for identical bytes (the diff is then
// the empty string — noise suppressed, the flag says so).
type BlueprintDiffResult struct {
	Diff   string `json:"diff"`
	ALabel string `json:"aLabel"`
	BLabel string `json:"bLabel"`
	Equal  bool   `json:"equal"`
}

// BlueprintLintParams is blueprint.lint's request: an existing name, or
// draft content to ask about before saving (name is then the label the
// lanes are checked against, and may be absent only with content).
type BlueprintLintParams struct {
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// BlueprintAdvisory mirrors blueprint.Advisory — the reason is the
// verdict sentence VERBATIM, never reworded, and every advisory carries
// its evaluation instant (T2's lint contract).
type BlueprintAdvisory struct {
	Lane        string `json:"lane"`
	Step        string `json:"step"`
	Agent       string `json:"agent"`
	Tool        string `json:"tool"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	EvaluatedAt string `json:"evaluatedAt"`
	Kind        string `json:"kind"`
}

// BlueprintLintResult is advisory-only, always: lint NEVER changes
// Report.OK, save or validate. When the daemon has no tools catalog wired
// the answer is the explicit unavailable state with empty advisories —
// never an all-clear invented from absence.
type BlueprintLintResult struct {
	Advisories  []BlueprintAdvisory `json:"advisories"`
	EvaluatedAt string              `json:"evaluatedAt"`
	Unavailable string              `json:"unavailable,omitempty"`
}

// BlueprintReadService is the daemon's read half (internal/daemon/
// blueprint_read.go). Every method answers from rows, writes nothing and
// takes no actor.
type BlueprintReadService interface {
	Usage(context.Context, BlueprintUsageParams) (BlueprintUsageResult, error)
	History(context.Context, BlueprintHistoryParams) (BlueprintHistoryResult, error)
	Revision(context.Context, BlueprintRevisionParams) (BlueprintRevisionResult, error)
	Diff(context.Context, BlueprintDiffParams) (BlueprintDiffResult, error)
	Lint(context.Context, BlueprintLintParams) (BlueprintLintResult, error)
}

// RegisterBlueprintReads registers the five T7 doors — call it where the
// read service is wired, and never with a nil one (a door without its
// service is ABSENT, not a 500 waiting to happen).
func (t *Table) RegisterBlueprintReads() bool {
	if t.BlueprintReads == nil {
		return false
	}
	t.register(&Method{Name: MethodBlueprintUsage, RequiresInitialize: true, Handle: t.handleBlueprintUsage})
	t.register(&Method{Name: MethodBlueprintHistory, RequiresInitialize: true, Handle: t.handleBlueprintHistory})
	t.register(&Method{Name: MethodBlueprintRevision, RequiresInitialize: true, Handle: t.handleBlueprintRevision})
	t.register(&Method{Name: MethodBlueprintDiff, RequiresInitialize: true, Handle: t.handleBlueprintDiff})
	t.register(&Method{Name: MethodBlueprintLint, RequiresInitialize: true, Handle: t.handleBlueprintLint})
	return true
}

func (t *Table) handleBlueprintList(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p BlueprintListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Origin != "" && p.Origin != "system" && p.Origin != "user" {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint.list origin %q is outside the vocabulary (system|user, or absent for both)", p.Origin)}
	}
	if t.ListBlueprints == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ListBlueprints(p)
}

func (t *Table) handleBlueprintShow(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.ShowBlueprint == nil {
		return nil, ErrStoreUnavailable
	}
	var p BlueprintShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name is required")}
	}
	return t.ShowBlueprint(p)
}

func (t *Table) handleBlueprintUsage(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.BlueprintReads == nil {
		return nil, ErrStoreUnavailable
	}
	var p BlueprintUsageParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name is required — usage is asked ABOUT one blueprint")}
	}
	return t.BlueprintReads.Usage(context.Background(), p)
}

func (t *Table) handleBlueprintHistory(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.BlueprintReads == nil {
		return nil, ErrStoreUnavailable
	}
	var p BlueprintHistoryParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name is required — history is asked ABOUT one blueprint")}
	}
	if p.Limit < 0 || p.Limit > 200 {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint.history limit must be between 1 and 200 (absent means the full newest-first window)")}
	}
	return t.BlueprintReads.History(context.Background(), p)
}

func (t *Table) handleBlueprintRevision(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.BlueprintReads == nil {
		return nil, ErrStoreUnavailable
	}
	var p BlueprintRevisionParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" || p.Rev < 1 {
		return nil, ErrBadParams{Err: fmt.Errorf("name and a rev of at least 1 are required — one revision is NAMED, never guessed")}
	}
	return t.BlueprintReads.Revision(context.Background(), p)
}

func (t *Table) handleBlueprintDiff(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.BlueprintReads == nil {
		return nil, ErrStoreUnavailable
	}
	var p BlueprintDiffParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" || p.A == "" || p.B == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name, a and b are required — each side is a revision number, \"current\" or \"shipped\"")}
	}
	return t.BlueprintReads.Diff(context.Background(), p)
}

func (t *Table) handleBlueprintLint(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.BlueprintReads == nil {
		return nil, ErrStoreUnavailable
	}
	var p BlueprintLintParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" && p.Content == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name or content is required — lint is asked about ONE blueprint, stored or drafted")}
	}
	if len(p.Content) > 1<<20 {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint source exceeds 1 MiB")}
	}
	return t.BlueprintReads.Lint(context.Background(), p)
}
