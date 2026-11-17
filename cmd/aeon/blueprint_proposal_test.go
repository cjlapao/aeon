package main

// blueprint_proposal_test.go — S48 T9: the blueprint_save proposal kind,
// inert at rest and a person's act at commit. Every assertion here is
// about rows: the row create REFUSES to write when the candidate does not
// validate (the report verbatim, TD3), the actor the commit stamps (the
// COMMITTER, never the proposer), the one write path the effect walks
// (a single revision), the crash-retry that COMPLETES (TD4's hash answer),
// the inertness (a proposal changes no read), and the preview that reads
// and never writes.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

const bpProposalYAML = "id: bp-proposed\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: model-step, agent: proposal-author, emits: [{type: goal}]}\n"

type bpProposalEnv struct {
	t  *testing.T
	st *store.Store
	d  *daemon.Daemon
}

func newBPProposalEnv(t *testing.T) bpProposalEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	// The store row lint's ToolState asks (the code registry already
	// grants proposal-author's body goal_create — bpGrantGoalCreate's
	// finding, reused shape; the fresh store holds no first-party rows).
	if _, _, err := st.AgentBySlug("proposal-author"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "proposal-author", Name: "Proposal Author", Origin: "system", Version: "1"}, time.Now().UTC().Format(time.RFC3339)); err != nil && !strings.Contains(err.Error(), "exists") {
		t.Fatal(err)
	}
	return bpProposalEnv{t: t, st: st, d: d}
}

func bpPayload(t *testing.T, name, content string, createOnly bool, note string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"name": name, "content": content, "createOnly": createOnly, "note": note})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func bpInitialize(t *testing.T, d *daemon.Daemon, conn string) {
	t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "bp", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		t.Fatal(err)
	}
}

// createViaSocket runs the create door itself — the ValidateProposal seam
// is the door's guard, and the refusal under test must come from the door,
// not a hand call.
func (e bpProposalEnv) createViaSocket(conn, kind, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": kind, "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

func (e bpProposalEnv) commit(id int64, payload, byID string) (desk.CommitResult, error) {
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

// TestBlueprintSaveProposalCreateRefusesInvalidWithReport: an invalid
// candidate never becomes an inert row — the create door refuses with the
// report VERBATIM (the save door's own sentence; no paraphrase, no
// nonsense carried to commit).
func TestBlueprintSaveProposalCreateRefusesInvalidWithReport(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "c-bp-prop")
	before, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	// Two refusals, both must carry the save door's OWN words: an invalid
	// source rides the report JSON verbatim, and an id/name mismatch names
	// the candidate and the source id (the save door's cross-check
	// sentence, not a paraphrase).
	for _, bad := range []struct {
		name    string
		content string
		must    []string
	}{
		{"InvalidSource", "this: is: not: a: blueprint", []string{"diagnostics", "\"state\":\"invalid\""}},
		{"IdMismatch", strings.Replace(bpProposalYAML, "id: bp-proposed", "id: bp-other", 1), []string{"bp-proposed", "bp-other", "does not match"}},
	} {
		_, err := e.createViaSocket("c-bp-prop", "blueprint_save", bpPayload(t, "bp-proposed", bad.content, false, ""))
		if err == nil {
			t.Fatalf("%s: an invalid candidate was accepted as a proposal", bad.name)
		}
		var perr *protocol.Error
		if !errors.As(err, &perr) || perr.Data.What == "" {
			t.Fatalf("%s: the refusal is not the refused shape with a What: %v", bad.name, err)
		}
		for _, needle := range bad.must {
			if !strings.Contains(perr.Data.What, needle) {
				t.Fatalf("%s: the refusal does not carry %q verbatim: %s", bad.name, needle, perr.Data.What)
			}
		}
	}
	after, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("the refused create wrote %d rows — inert nonsense must never be stored", len(after)-len(before))
	}
}

// TestBlueprintSaveCreateAndCommitValidatorsAreOneFunction: TD3, the
// blueprint way (the spec name was taken by S46 T6's twin; the prefix is the
// recorded collision).
// the same refusal text comes from the create door and from the commit
// door's re-run, because there is ONE function behind both (the registry
// entry; the golden of that identity is TestDeskValidateIsTheCreateValidator's
// S35 pin, and this test pins the text equality at both doors for the new
// kind).
func TestBlueprintSaveCreateAndCommitValidatorsAreOneFunction(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "c-bp-one")
	bad := bpPayload(t, "bp-proposed", "nope: nope", false, "")
	_, doorErr := e.createViaSocket("c-bp-one", "blueprint_save", bad)
	if doorErr == nil {
		t.Fatal("the create door accepted an invalid candidate")
	}
	direct := validateBlueprintSaveProposal([]byte(bad))
	if direct == nil {
		t.Fatal("the validator accepted what the door refused")
	}
	// The door wraps (ErrProposalRefused) but carries the validator's
	// sentence in Data.What UNPARAPHRASED — same bytes both ways, because
	// there is one function behind both doors.
	var perr *protocol.Error
	if !errors.As(doorErr, &perr) {
		t.Fatalf("the door did not answer the refused shape: %v", doorErr)
	}
	if perr.Data.What != direct.Error() {
		t.Fatalf("the doors drifted:\n door:  %s\n valid: %s", perr.Data.What, direct)
	}
}

// TestBlueprintSaveCommitWritesRowAndRevisionAsTheCommitter: the effect is
// the save door's own write, and the revision's actor is the person who
// committed — the proposer stays in created_by_*.
func TestBlueprintSaveCommitWritesRowAndRevisionAsTheCommitter(t *testing.T) {
	e := newBPProposalEnv(t)
	payload := bpPayload(t, "bp-proposed", bpProposalYAML, false, "the agent's note")
	row, err := e.st.CreateProposal("blueprint_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q", res.Row.Status)
	}
	bpRow, found, err := e.st.BlueprintGet(context.Background(), "bp-proposed")
	if err != nil || !found {
		t.Fatalf("the committed proposal wrote no row: found=%v err=%v", found, err)
	}
	if bpRow.Content != bpProposalYAML {
		t.Fatal("the row did not carry the proposed bytes verbatim")
	}
	revs, err := e.st.RevisionList(context.Background(), "bp-proposed", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 {
		t.Fatal("no revision")
	}
	last := revs[len(revs)-1]
	if last.ActorKind != "user" || last.ActorID != "u-committ" {
		t.Fatalf("the revision carries actor %s/%s — the COMMITTER writes the row, never the proposer (manny must not appear)", last.ActorKind, last.ActorID)
	}
	if last.Note != "the agent's note" {
		t.Fatalf("the proposal's note did not reach the revision: %q", last.Note)
	}
}

// TestCommitAppliesViaTheOneWritePath: one commit, one revision — the
// effect lands through store.BlueprintPutTx EXACTLY once (the T8 guard
// pins the only legal call sites; the count here pins the single act).
func TestCommitAppliesViaTheOneWritePath(t *testing.T) {
	e := newBPProposalEnv(t)
	payload := bpPayload(t, "bp-proposed", bpProposalYAML, false, "")
	row, err := e.st.CreateProposal("blueprint_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.commit(row.ID, payload, "u-committ"); err != nil {
		t.Fatal(err)
	}
	revs, err := e.st.RevisionList(context.Background(), "bp-proposed", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("the commit wrote %d revisions — the one write path writes one", len(revs))
	}
}

// TestBlueprintSaveAlreadyAppliedCompletesFlip: the effect landed and the
// flip was lost (the crash window) — AlreadyApplied answers the content
// hash and the retry COMPLETES: the row flips to committed and NO second
// revision appears for bytes already on the row.
func TestBlueprintSaveAlreadyAppliedCompletesFlip(t *testing.T) {
	e := newBPProposalEnv(t)
	if _, err := daemon.BlueprintSave(context.Background(), e.st, protocol.Actor{Kind: "user", ID: "u-first"}, protocol.BlueprintSaveParams{
		Name: "bp-proposed", Content: bpProposalYAML,
	}); err != nil {
		t.Fatal(err)
	}
	revsBefore, err := e.st.RevisionList(context.Background(), "bp-proposed", 200)
	if err != nil {
		t.Fatal(err)
	}
	payload := bpPayload(t, "bp-proposed", bpProposalYAML, false, "")
	row, err := e.st.CreateProposal("blueprint_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q — a landed effect must COMPLETE the flip, not replay", res.Row.Status)
	}
	revsAfter, err := e.st.RevisionList(context.Background(), "bp-proposed", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(revsAfter) != len(revsBefore) {
		t.Fatalf("the retry wrote %d revisions for bytes already stored — AlreadyApplied failed its job", len(revsAfter)-len(revsBefore))
	}
}

// TestBlueprintProposalsAreInert: a proposal changes NOTHING. list, show
// and usage answer byte-identically with the proposal (in every state —
// staged here straight through the store) present, and a loop start reads
// the same rows it read before: the S35 arbiter pattern, extended to
// blueprints (BP9).
func TestBlueprintProposalsAreInert(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "c-bp-inert")
	// A blueprint on the row side so list/show/usage have substance.
	if _, err := daemon.BlueprintSave(context.Background(), e.st, protocol.Actor{Kind: "user", ID: "u-first"}, protocol.BlueprintSaveParams{
		Name: "bp-proposed", Content: bpProposalYAML,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		list, err := e.d.Table.ListBlueprints(protocol.BlueprintListParams{})
		if err != nil {
			t.Fatal(err)
		}
		show, err := e.d.Table.ShowBlueprint(protocol.BlueprintShowParams{Name: "bp-proposed"})
		if err != nil {
			t.Fatal(err)
		}
		usage, err := e.d.Table.BlueprintReads.Usage(context.Background(), protocol.BlueprintUsageParams{Name: "bp-proposed"})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(map[string]any{"l": list, "s": show, "u": usage})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	before := snapshot()
	proposed, err := e.st.CreateProposal("blueprint_save", bpPayload(t, "bp-other", bpProposalYAML, false, ""), store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateProposal("blueprint_save", bpPayload(t, "bp-proposed", strings.Replace(bpProposalYAML, "version: 1", "version: 2", 1), false, ""), store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := e.st.DecideProposal(proposed.ID, "rejected", store.Actor{Kind: "user", ID: "u1"}, "not now", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	if before != after {
		t.Fatalf("proposals changed the reads:\n before %s\n after  %s", before, after)
	}
}

// TestPreviewShowsDiffReportAndAdvisoriesReadOnly: proposal.get previews
// the design revision — the diff of the proposed bytes against the row
// (isNew when there is no row), the report verbatim, the dated lint
// advisories — and writes NOTHING (revisions and names byte-stable across
// the read).
func TestPreviewShowsDiffReportAndAdvisoriesReadOnly(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "c-bp-prev")
	namesBefore, err := e.st.BlueprintNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload := bpPayload(t, "bp-proposed", bpProposalYAML, false, "")
	row, err := e.st.CreateProposal("blueprint_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	getRaw, _ := json.Marshal(map[string]any{"id": row.ID})
	res, err := e.d.Table.Dispatch(context.Background(), "c-bp-prev", protocol.MethodProposalGet, getRaw)
	if err != nil {
		t.Fatal(err)
	}
	got := res.(protocol.ProposalGetResult)
	if got.Preview == nil || got.Preview.Blueprint == nil {
		t.Fatalf("the preview is absent: %+v", got.Preview)
	}
	bp := got.Preview.Blueprint
	if !bp.IsNew {
		t.Fatal("no row exists — the preview must say new")
	}
	if !strings.Contains(bp.Diff, "+id: bp-proposed") {
		t.Fatalf("the diff does not show the proposed bytes: %q", bp.Diff)
	}
	if len(bp.Report) == 0 || !json.Valid(bp.Report) {
		t.Fatalf("the report is absent or malformed: %s", bp.Report)
	}
	if bp.Advisories == nil {
		t.Fatal("advisories must be an array, never null")
	}
	if bp.Advisories == nil || (!bp.Equal && bp.EvaluatedAt == "" && bp.Unavailable == "") {
		t.Fatal("the lint half is neither dated nor stated unavailable")
	}
	namesAfter, err := e.st.BlueprintNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(namesBefore, ",") != strings.Join(namesAfter, ",") {
		t.Fatalf("the preview wrote blueprints: %v → %v", namesBefore, namesAfter)
	}
	revs, err := e.st.RevisionList(context.Background(), "bp-proposed", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 0 {
		t.Fatalf("the preview wrote %d revisions", len(revs))
	}
}

// TestCommitRefusedWhenAgentNoLongerExists: the rot that create could not
// see is refused AT COMMIT naming the reason — applied_error set, the row
// STAYS proposed (S35 TD4). The rot is staged as what the commit door can
// observe between the two doors in one process: a createOnly proposal
// whose name was taken after create (the agent-deleted shape needs a
// process-global registry to move, which no test may do — recorded in the
// task Notes; the refusal path exercised is the identical one: validate
// refuses at commit, applied_error speaks, the row stays proposed).
func TestCommitRefusedWhenAgentNoLongerExists(t *testing.T) {
	e := newBPProposalEnv(t)
	payload := bpPayload(t, "bp-proposed", bpProposalYAML, true, "")
	row, err := e.st.CreateProposal("blueprint_save", payload, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	// The world moves under the proposal: the name is taken meanwhile by
	// DIFFERENT bytes — AlreadyApplied must answer false (the hash is not
	// the payload's) and the commit door's re-run of the validator must
	// refuse the collision. Identical bytes here would be TD4's COMPLETION
	// path, not this one (the other test owns that case).
	if _, err := daemon.BlueprintSave(context.Background(), e.st, protocol.Actor{Kind: "user", ID: "u-mid"}, protocol.BlueprintSaveParams{
		Name: "bp-proposed", Content: strings.Replace(bpProposalYAML, "version: 1", "version: 2", 1),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := e.commit(row.ID, payload, "u-committ")
	if err == nil {
		t.Fatalf("the commit applied a rotted proposal: %+v", res)
	}
	stored, found, err := e.st.GetProposal(row.ID)
	if err != nil || !found {
		t.Fatal(err)
	}
	if stored.Status != "proposed" {
		t.Fatalf("status = %q — a refused commit must leave the row proposed", stored.Status)
	}
	if stored.AppliedError == "" {
		t.Fatal("applied_error is empty — the refusal must be readable at the row")
	}
	// And nothing was written by the failed attempt beyond what existed.
	revs, err := e.st.RevisionList(context.Background(), "bp-proposed", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("the refused commit wrote revisions: %d", len(revs))
	}
}
