package main

// automation_proposal_test.go — S49 T6: the automation_disable kind, an ask
// at rest and a person's act at commit. Every assertion is about rows: the
// create door REFUSES an ask that changes nothing (already-off, unknown,
// archived, cross-family) before a byte is stored (TD3's nothing-inert
// carried), the two doors run ONE function, the commit stamps the
// COMMITTER and writes through the family's OWN write path (the engine's
// and the store's frozen sentences are the fingerprint — a paraphrase
// cannot forge them), the crash-retry COMPLETES the flip without a second
// change (TD4/AN4), no payload can widen, a proposal at rest is inert, the
// preview reads and writes nothing, and the desk half keeps its imports.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type autoPropEnv struct {
	t  *testing.T
	st *store.Store
	d  *daemon.Daemon
	// The event family's ids are generated (trg-…), so the seed hands them
	// back and every payload names the REAL row.
	evtLive, evtOff, evtArch string
}

func newAutoPropEnv(t *testing.T) autoPropEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	return autoPropEnv{t: t, st: st, d: d}
}

// seedAutomations plants one live row of every family plus the states the
// refusals are about: a paused schedule, a disabled event, an archived
// event and an archived schedule. Ids are the payload's words verbatim so
// the sentences under test name what the person typed.
func (e *autoPropEnv) seedAutomations() {
	e.t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	must := func(err error) {
		e.t.Helper()
		if err != nil {
			e.t.Fatal(err)
		}
	}
	must(e.st.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "sch-nightly", Kind: "schedule", Blueprint: "bp-desk", Expr: "@daily", Zone: "UTC", CreatedAt: now,
	}))
	must(e.st.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "sch-stilled", Kind: "schedule", Blueprint: "bp-desk", Expr: "@hourly", Zone: "UTC", Paused: true, CreatedAt: now,
	}))
	must(e.st.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "wh-build", Kind: "webhook", Blueprint: "bp-desk", CreatedAt: now,
	}))
	user := store.Actor{Kind: "user", ID: "u1"}
	live, err := e.st.CreateEventTrigger(user, store.NewEventTrigger{
		Name: "nightly sync", Blueprint: "bp-desk", Kickoff: "sync",
		Match: store.Match{Event: "task.created"}, MaxRunsPerHour: 4,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	off, err := e.st.CreateEventTrigger(user, store.NewEventTrigger{
		Name: "stilled sync", Blueprint: "bp-desk", Kickoff: "sync",
		Match: store.Match{Event: "goal.approved"}, MaxRunsPerHour: 4,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	// The disabled event: the person's own switch-off, BEFORE any snapshot
	// a test takes, so its audit row is never counted against a commit.
	if _, err := e.st.SetEventTriggerEnabled(user, off.ID, false, "drill"); err != nil {
		e.t.Fatal(err)
	}
	arch, err := e.st.CreateEventTrigger(user, store.NewEventTrigger{
		Name: "retired sync", Blueprint: "bp-desk", Kickoff: "sync",
		Match: store.Match{Event: "task.completed"}, MaxRunsPerHour: 4,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.evtLive, e.evtOff, e.evtArch = live.ID, off.ID, arch.ID
	// AN6's archives, planted as the store's columns (the archive doors
	// themselves are T4's and tested there).
	if err := e.st.GormDB().Exec(`UPDATE event_trigger SET archived_at=?, archived_reason=? WHERE id=?`,
		now, "retired", arch.ID).Error; err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.GormDB().Exec(`UPDATE trigger_schedule SET archived_at=?, archived_reason=? WHERE id=?`,
		now, "retired", "wh-build").Error; err != nil {
		e.t.Fatal(err)
	}
}

func autoPropPayload(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func autoPropInitialize(t *testing.T, d *daemon.Daemon, conn string) {
	t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "auto", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		t.Fatal(err)
	}
}

func (e autoPropEnv) createViaSocket(conn, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": "automation_disable", "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

func (e autoPropEnv) getViaSocket(id int64) (protocol.ProposalGetResult, error) {
	e.t.Helper()
	raw, _ := json.Marshal(protocol.ProposalGetParams{ID: id})
	res, err := e.d.Table.Dispatch(context.Background(), "c-prev", protocol.MethodProposalGet, raw)
	if err != nil {
		return protocol.ProposalGetResult{}, err
	}
	return res.(protocol.ProposalGetResult), nil
}

func (e autoPropEnv) commit(id int64, payload, byID string) (desk.CommitResult, error) {
	return desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: byID})
}

func (e autoPropEnv) changes(source, id string) []store.AutomationChange {
	e.t.Helper()
	rows, err := e.st.AutomationChangeList(context.Background(), source, id, 100)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e autoPropEnv) proposalCount() int {
	e.t.Helper()
	var n int
	if err := e.st.GormDB().Exec(`SELECT 1`).Error; err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.GormDB().Raw(`SELECT COUNT(*) FROM proposal`).Scan(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

// TestDisableProposalCreateRefusesAlreadyOffOrUnknown: the create door
// refuses every ask that changes NOTHING, and it refuses it naming what is
// true — an already-off automation by its off state, a missing one through
// automation.list, an archived one as history (AN6), an id under the wrong
// family as the OTHER thing it is (AN1), and every shape the desk cannot
// parse as the shape it is. Nothing inert reaches a commit.
func TestDisableProposalCreateRefusesAlreadyOffOrUnknown(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-refuse")
	before := e.proposalCount()
	cases := []struct {
		name   string
		fields map[string]any
		wants  []string
	}{
		{"unknown event", map[string]any{"source": "event", "id": "evt-nope", "reason": "quiet"}, []string{"no event automation", "automation.list"}},
		{"unknown schedule", map[string]any{"source": "schedule", "id": "sch-nope", "reason": "quiet"}, []string{"no schedule automation", "automation.list"}},
		{"already paused", map[string]any{"source": "schedule", "id": "sch-stilled", "reason": "quiet"}, []string{"already off"}},
		{"already disabled", map[string]any{"source": "event", "id": e.evtOff, "reason": "quiet"}, []string{"already off"}},
		{"archived event", map[string]any{"source": "event", "id": e.evtArch, "reason": "quiet"}, []string{"archived"}},
		{"archived webhook", map[string]any{"source": "webhook", "id": "wh-build", "reason": "quiet"}, []string{"archived"}},
		{"cross-family", map[string]any{"source": "schedule", "id": e.evtLive, "reason": "quiet"}, []string{"is an event automation, not the schedule named"}},
		{"source outside the three", map[string]any{"source": "clock", "id": "sch-nightly", "reason": "quiet"}, []string{"schedule, webhook, event"}},
		{"smuggled enabled", map[string]any{"source": "schedule", "id": "sch-nightly", "reason": "quiet", "enabled": true}, nil},
		{"smuggled actor", map[string]any{"source": "schedule", "id": "sch-nightly", "reason": "quiet", "actor": "u2"}, nil},
		{"no reason", map[string]any{"source": "schedule", "id": "sch-nightly"}, nil},
		{"blank id", map[string]any{"source": "schedule", "id": "   ", "reason": "quiet"}, []string{"non-blank id"}},
	}
	for _, c := range cases {
		if _, err := e.createViaSocket("c-refuse", autoPropPayload(t, c.fields)); err == nil {
			t.Errorf("%s: the create door stored a nothing-ask", c.name)
			continue
		} else if c.wants != nil {
			// The socket answers a refused proposal as the envelope (-31710)
			// with the validator's sentence in Data.What — the thing under
			// test is the sentence.
			msg := err.Error()
			var perr *protocol.Error
			if errors.As(err, &perr) {
				msg = perr.Data.What
			}
			for _, w := range c.wants {
				if !strings.Contains(msg, w) {
					t.Errorf("%s: refusal %q does not name %q", c.name, msg, w)
				}
			}
		}
	}
	// The long reason: over the 256 cap is an essay, not an ask.
	long := strings.Repeat("x", 300)
	if _, err := e.createViaSocket("c-refuse", autoPropPayload(t, map[string]any{"source": "schedule", "id": "sch-nightly", "reason": long})); err == nil {
		t.Error("a 300-byte reason walked through the door")
	}
	if n := e.proposalCount(); n != before {
		t.Fatalf("the refused asks stored %d proposal rows — refusals must not write", n-before)
	}
	// And one honest ask DOES stand, inert at rest.
	row, err := e.createViaSocket("c-refuse", autoPropPayload(t, map[string]any{"source": "schedule", "id": "sch-nightly", "reason": "budget month"}))
	if err != nil {
		t.Fatalf("a live automation's disable ask should stand at the door: %v", err)
	}
	if row.Status != "proposed" {
		t.Fatalf("staged status = %q, want proposed", row.Status)
	}
}

// TestAutomationDisableCreateAndCommitValidatorsAreOneFunction is TD3 for
// the new kind (the mcp file holds the bare name; the suffix is the
// blueprint/environment per-kind pattern): the create door's refusal and
// the validator carry the SAME bytes, because one function stands behind
// both doors.
func TestAutomationDisableCreateAndCommitValidatorsAreOneFunction(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-one")
	bad := autoPropPayload(t, map[string]any{"source": "event", "id": "evt-nope", "reason": "quiet"})
	_, doorErr := e.createViaSocket("c-one", bad)
	if doorErr == nil {
		t.Fatal("the create door accepted an ask about a row that does not exist")
	}
	direct := validateAutomationDisableProposal([]byte(bad))
	if direct == nil {
		t.Fatal("the validator accepted what the door refused")
	}
	var perr *protocol.Error
	if !errors.As(doorErr, &perr) {
		t.Fatalf("the door did not answer the refused shape: %v", doorErr)
	}
	if perr.Data.What != direct.Error() {
		t.Fatalf("the doors drifted:\n door:  %s\n valid: %s", perr.Data.What, direct)
	}
	// Already-off is the door's other shared half: one sentence both ways.
	off := autoPropPayload(t, map[string]any{"source": "schedule", "id": "sch-stilled", "reason": "quiet"})
	offDirect := validateAutomationDisableProposal([]byte(off))
	if offDirect == nil || !strings.Contains(offDirect.Error(), "already off") {
		t.Fatalf("the validator's already-off refusal is %v", offDirect)
	}
	if _, err := e.createViaSocket("c-one", off); err == nil {
		t.Fatal("the create door stored an ask the validator refuses")
	}
}

// TestDisableCommitDisablesAsTheCommitterAndAudits: the committed ask
// flips the row with the COMMITTER as actor — the proposer stays in the
// proposal's created_by_* — and AN4 counts the ONE audit row the path
// itself wrote: op disable, actor user/u-committ, the person's reason with
// the proposal's id beside it in the row's disabled_reason.
func TestDisableCommitDisablesAsTheCommitterAndAudits(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-commit")
	payload := autoPropPayload(t, map[string]any{"source": "event", "id": e.evtLive, "reason": "spend review says stop"})
	row, err := e.createViaSocket("c-commit", payload)
	if err != nil {
		t.Fatal(err)
	}
	auditBefore := len(e.changes("event", e.evtLive))
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatalf("commit of a live row's disable: %v", err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q, want committed", res.Row.Status)
	}
	tr, err := e.st.GetEventTrigger(e.evtLive)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Enabled {
		t.Fatal("the committed disable did not switch the event row off")
	}
	if !strings.Contains(tr.DisabledReason, "spend review says stop") || !strings.Contains(tr.DisabledReason, "proposal #"+strconv.FormatInt(row.ID, 10)) {
		t.Errorf("disabled_reason = %q — the person's sentence and the proposal id must ride the row", tr.DisabledReason)
	}
	// AN4 counts the ONE change this commit made: exactly one new audit
	// row (the seed's own `create` row is already counted in before), and
	// it carries the COMMITTER, through the family's own op.
	changes := e.changes("event", e.evtLive)
	if len(changes) != auditBefore+1 {
		t.Fatalf("the disable left %d new audit rows, want exactly the ONE the path wrote (AN4)", len(changes)-auditBefore)
	}
	var sawDisable bool
	for _, c := range changes {
		if c.Op == "disable" {
			if sawDisable {
				t.Fatalf("two disable rows for one committed ask (AN4 counts changes, not touches)")
			}
			sawDisable = true
			if c.ActorKind != "user" || c.ActorID != "u-committ" {
				t.Fatalf("audit row actor=%s/%s — the COMMITTER acts", c.ActorKind, c.ActorID)
			}
		}
	}
	if !sawDisable {
		t.Fatal("no disable audit row for the committed disable")
	}
}

// TestDisableCommitUsesTheOneWriteService: each family's disable rides
// ITS OWN write path, and the paths' FROZEN sentences are the proof no
// paraphrase can forge — the store's "disabled event" for the event family,
// the ENGINE's "pause schedule" (audited through the HumanWriter fork) for
// the schedule family. A second write service could not produce those
// words; one change, one audit row per path (AN4).
func TestDisableCommitUsesTheOneWriteService(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-path")
	payload := autoPropPayload(t, map[string]any{"source": "schedule", "id": "sch-nightly", "reason": "quiet month"})
	row, err := e.createViaSocket("c-path", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.commit(row.ID, payload, "u-committ"); err != nil {
		t.Fatal(err)
	}
	sr, err := e.st.TriggerSchedule(context.Background(), "sch-nightly")
	if err != nil {
		t.Fatal(err)
	}
	if !sr.Paused {
		t.Fatal("the committed ask did not pause the schedule row")
	}
	changes := e.changes("schedule", "sch-nightly")
	if len(changes) != 1 {
		t.Fatalf("the pause left %d audit rows, want exactly the ONE the engine wrote (AN4)", len(changes))
	}
	if changes[0].Op != "pause" || !strings.Contains(changes[0].Summary, "pause schedule") {
		t.Fatalf("audit op=%s summary=%q — the engine's own frozen sentence must appear verbatim", changes[0].Op, changes[0].Summary)
	}
	if changes[0].ActorKind != "user" || changes[0].ActorID != "u-committ" {
		t.Fatalf("the pause audit names %s/%s — the HumanWriter stamp is the COMMITTER", changes[0].ActorKind, changes[0].ActorID)
	}
}

// TestDisableAlreadyAppliedCompletesFlip: the effect landed (the person
// switched it off by hand, or a crash lost the flip between write and
// decision) and the retry COMPLETES: the row flips to committed, AlreadyApplied
// answers the off state, and NO second change is written — AN4 counts what
// changed, not what was touched.
func TestDisableAlreadyAppliedCompletesFlip(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-flip")
	payload := autoPropPayload(t, map[string]any{"source": "event", "id": e.evtLive, "reason": "cost"})
	row, err := e.createViaSocket("c-flip", payload)
	if err != nil {
		t.Fatal(err)
	}
	// The crash window: the disable lands through the same path, the flip
	// is lost.
	if _, err := daemon.NewTriggerWrites(e.st).DisableEventTrigger(protocol.Actor{Kind: "user", ID: "u-first"}, e.evtLive, "lost the flip"); err != nil {
		t.Fatal(err)
	}
	auditBefore := len(e.changes("event", e.evtLive))
	res, err := e.commit(row.ID, payload, "u-committ")
	if err != nil {
		t.Fatalf("the retry must COMPLETE, not replay: %v", err)
	}
	if res.Row.Status != "committed" {
		t.Fatalf("status = %q — a landed effect completes the flip", res.Row.Status)
	}
	if auditAfter := len(e.changes("event", e.evtLive)); auditAfter != auditBefore {
		t.Fatalf("the retry wrote %d audit rows for a change already made — AlreadyApplied failed its job", auditAfter-auditBefore)
	}
}

// TestNoPayloadCanWiden: the payload's grammar is WHO and WHY and nothing
// else. Every widening shape — enable, unarchive, actor, target state, the
// missing reason, junk after the JSON — is a PARSE refusal at the desk,
// before any store opens, and none of them stores a row.
func TestNoPayloadCanWiden(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-widen")
	before := e.proposalCount()
	widening := []string{
		`{"source":"schedule","id":"sch-nightly","reason":"go","enabled":true}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go","paused":false}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go","archived":false}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go","status":"live"}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go","actor":{"kind":"user","id":"u9"}}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go","kind":"automation_enable"}`,
		`{"source":"event","id":"nightly-sync"}`,
		`{"source":"schedule","id":"sch-nightly","reason":""}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go","reason2":"go"}`,
		`{"source":"schedule","id":"sch-nightly","reason":7}`,
		`{"source":"schedule","id":["sch-nightly"],"reason":"go"}`,
		`{"source":"schedule","id":"sch-nightly","reason":"go"} {"source":"schedule","id":"sch-stilled","reason":"go"}`,
		`{"source":"automation","id":"sch-nightly","reason":"go"}`,
	}
	for _, p := range widening {
		if err := validateAutomationDisableProposal([]byte(p)); err == nil {
			t.Errorf("the validator accepted a widening: %s", p)
		}
		if _, err := e.createViaSocket("c-widen", p); err == nil {
			t.Errorf("the create door stored a widening: %s", p)
		}
	}
	if n := e.proposalCount(); n != before {
		t.Fatalf("the widening attempts stored %d rows — none may exist", n-before)
	}
}

// TestAutomationProposalsAreInert: an ask at rest changes NOTHING. Every
// automation read answers byte-identically with the proposals staged (and
// one rejected) in every state — the S35 blueprint/environment pattern
// carried to the automation plane (AN9's asks move no row until a person
// commits).
func TestAutomationProposalsAreInert(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-inert")
	reads := e.d.Table.AutomationReads
	snapshot := func() string {
		list, _, err := reads.ListAutomations(context.Background(), protocol.AutomationListParams{Archived: "include"})
		if err != nil {
			t.Fatal(err)
		}
		show, err := reads.ShowAutomation(context.Background(), protocol.AutomationShowParams{Source: "event", ID: e.evtLive})
		if err != nil {
			t.Fatal(err)
		}
		changes, err := reads.ListAutomationChanges(context.Background(), protocol.AutomationChangesParams{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(map[string]any{"l": list, "s": show, "c": changes})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	before := snapshot()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	agent := store.Actor{Kind: "agent", ID: "manny"}
	staged, err := e.st.CreateProposal("automation_disable", autoPropPayload(t, map[string]any{"source": "schedule", "id": "sch-nightly", "reason": "quiet"}), agent, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateProposal("automation_disable", autoPropPayload(t, map[string]any{"source": "event", "id": e.evtLive, "reason": "cost"}), agent, now); err != nil {
		t.Fatal(err)
	}
	if err := e.st.DecideProposal(staged.ID, "rejected", store.Actor{Kind: "user", ID: "u1"}, "not now", now); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	if before != after {
		t.Fatalf("proposals changed the reads:\n before %s\n after  %s", before, after)
	}
	// And the rows themselves never moved: still enabled, still unpaused.
	if tr, err := e.st.GetEventTrigger(e.evtLive); err != nil || !tr.Enabled {
		t.Fatalf("a staged ask switched the event row — inertness is broken (enabled=%v err=%v)", tr.Enabled, err)
	}
	if sr, err := e.st.TriggerSchedule(context.Background(), "sch-nightly"); err != nil || sr.Paused {
		t.Fatalf("a staged ask paused the schedule row — inertness is broken (paused=%v err=%v)", sr.Paused, err)
	}
}

// TestPreviewIsReadOnly: proposal.get on the disable ask carries the
// automation preview — the AN5 sentence, the live state, the last five
// fires, the health verdict — and WRITES NOTHING: the fire log, the audit
// and the proposal's own state are byte-stable across the read, and the
// preview is advisory (the row still reads with it absent or present).
func TestPreviewIsReadOnly(t *testing.T) {
	e := newAutoPropEnv(t)
	e.seedAutomations()
	autoPropInitialize(t, e.d, "c-prev")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := e.st.RecordTriggerFire(context.Background(), store.TriggerFireRow{ScheduleID: "sch-nightly", Instant: "2026-11-02T07:00:00Z", Outcome: "fired", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.RecordTriggerFire(context.Background(), store.TriggerFireRow{ScheduleID: "sch-nightly", Instant: "2026-11-01T07:00:00Z", Outcome: "skipped", Reason: "outside window", At: now}); err != nil {
		t.Fatal(err)
	}
	firesBefore := func() string {
		f, c, next, err := e.d.Table.AutomationReads.ListAutomationFires(context.Background(), protocol.AutomationFiresParams{Source: "schedule", ID: "sch-nightly", Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		b, merr := json.Marshal(map[string]any{"f": f, "c": c, "n": next})
		if merr != nil {
			t.Fatal(merr)
		}
		return string(b)
	}
	auditBefore := len(e.changes("schedule", "sch-nightly"))
	fb := firesBefore()
	payload := autoPropPayload(t, map[string]any{"source": "schedule", "id": "sch-nightly", "reason": "quiet month"})
	row, err := e.createViaSocket("c-prev", payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.getViaSocket(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preview == nil || got.Preview.Automation == nil {
		t.Fatal("the disable ask carried no automation preview")
	}
	ap := got.Preview.Automation
	if ap.Source != "schedule" || ap.ID != "sch-nightly" {
		t.Errorf("preview names %s/%s, want the payload's schedule/sch-nightly", ap.Source, ap.ID)
	}
	if strings.TrimSpace(ap.Describe) == "" {
		t.Error("the preview lost the AN5 sentence")
	}
	if !ap.Enabled {
		t.Error("the live row's preview says disabled — the CURRENT state is what the preview owes the person")
	}
	if len(ap.LastFires) != 2 {
		t.Errorf("preview shows %d fires, want the two the log holds", len(ap.LastFires))
	}
	if ap.Health == nil {
		t.Error("no health verdict beside a schedulable row")
	}
	if got.Preview.Kind != "automation_disable" {
		t.Errorf("preview kind = %q", got.Preview.Kind)
	}
	// Read-only in every direction: fires, audit, proposal state unmoved.
	if firesBefore() != fb {
		t.Fatal("the preview wrote fire-log rows")
	}
	if len(e.changes("schedule", "sch-nightly")) != auditBefore {
		t.Fatal("the preview audited")
	}
	if got.Proposal.Status != "proposed" {
		t.Fatalf("proposal status after the read = %q — a preview that flips the row is a second authority", got.Proposal.Status)
	}
}

// TestDeskImportsNoStoreOrDaemon: the desk half of the kind — its payload
// grammar — lives in internal/desk and imports NOTHING of the readers or
// the engines: no internal/store, no internal/daemon, no internal/protocol,
// no aeon package at all. stdlib only, beside the package's own kinds. The
// package-wide import guard (imports_test.go) covers the tree; this pins
// the S49 file by name so the seam direction is stated where the kind is.
func TestDeskImportsNoStoreOrDaemon(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "desk", "automation_kinds.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "import (")
	if start < 0 {
		t.Fatal("no import block found")
	}
	block := body[start:]
	if end := strings.Index(block, "\n)"); end > 0 {
		block = block[:end]
	}
	for _, banned := range []string{"internal/store", "internal/daemon", "internal/protocol", "github.com/cjlapao/aeon/"} {
		if strings.Contains(block, banned) {
			t.Errorf("the desk half of automation_disable imports %s — the store and the engines reach the kind only through the seams", banned)
		}
	}
}
