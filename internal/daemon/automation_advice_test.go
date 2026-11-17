package daemon

// automation_advice_test.go — S49 T3: the ADVISES half of the read plane.
// AN8 is the whole argument: advice runs the SAME engine code the gate and
// the clock run, quotes the engine's sentences verbatim, and NEVER gates.
// These tests therefore come in pairs — a preview must equal the clock's
// own NextFires byte for byte, a webhook refusal must BE trigger.PreviewFires'
// sentence, and health must say "can't fire" while the real dispatcher
// walks past the very same trigger and answers for itself. C17 rides with
// them: the webhook secret's NAME crosses the socket, its VALUE never
// appears in a single byte an answer carries.
//
// The fixture is automationEnv from automation_read_test.go — same rows,
// same fixed clock; nothing here re-seeds.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/automation"
	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// TestBlueprintHealthStates walks the ONE validator's three honest states
// over the SAME rows `blueprint.show` reads: ok, invalid (content the
// analyzer's error lines refuse), missing (no row at all).
func TestBlueprintHealthStates(t *testing.T) {
	s, d, _, _ := automationEnv(t)
	const valid = `id: bp-morning
schemaVersion: 1
version: 1
lanes:
  - id: work
    step: {kind: gate, question: go}
questions:
  go:
    prompt: go?
    answers: [yes]
`
	bpForceRow(t, s, "bp-morning", valid)
	h := auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"source":"schedule","id":"sch-morning"}`)
	if len(h.Health) != 1 {
		t.Fatalf("health = %s, want one row", autoJSON(t, h.Health))
	}
	got := h.Health[0]
	if got.Blueprint.State != automationview.BlueprintOK || !got.CanFire || len(got.Reasons) != 0 {
		t.Fatalf("healthy schedule = %s — blueprint valid, enabled, workspace real, so CanFire and no reasons", autoJSON(t, got))
	}
	if h.EvaluatedAt == "" {
		t.Errorf("evaluatedAt empty — health is a snapshot of a moment and must say which")
	}

	// Same name, poisoned bytes: the SAME Analyze `blueprint.show` runs
	// answers invalid, and CanFire flips with its own reason.
	bpForceRow(t, s, "bp-morning", "id: bp-morning\nlanes: [unclosed\n")
	h = auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"source":"schedule","id":"sch-morning"}`)
	got = h.Health[0]
	if got.Blueprint.State != automationview.BlueprintInvalid || got.CanFire {
		t.Fatalf("poisoned blueprint = %s — invalid must block CanFire", autoJSON(t, got))
	}
	if len(got.Reasons) == 0 || !strings.Contains(got.Reasons[len(got.Reasons)-1], "fails validation") {
		t.Errorf("reasons = %v, want the composer's own validation sentence", got.Reasons)
	}

	// bp-deploy was never forced: missing is its own state, said out loud.
	h = auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"source":"webhook","id":"whk-deploy"}`)
	got = h.Health[0]
	if got.Blueprint.State != automationview.BlueprintMissing || got.CanFire {
		t.Fatalf("missing blueprint = %s", autoJSON(t, got))
	}
	if !strings.Contains(autoJSON(t, got), "does not exist") {
		t.Errorf("missing reason = %v, want \"does not exist\"", got.Reasons)
	}
	if got.Webhook == nil || got.Webhook.EnvName != "AEON_T3_SECRET" || got.Webhook.Set {
		t.Errorf("webhook secret row = %s, want the NAME with Set false (env unset here)", autoJSON(t, got.Webhook))
	}
	if !strings.Contains(autoJSON(t, got), "AEON_T3_SECRET is not set in the daemon") {
		t.Errorf("secret reason = %s, want the composer's unset sentence", autoJSON(t, got))
	}
}

// TestWebhookSecretIsPresenceNeverTheValue is C17 over the wire: with the
// env var SET to a canary, health reports the presence, quotes the NAME,
// and the canary's bytes appear nowhere — not in health, not in show, not
// in list.
func TestWebhookSecretIsPresenceNeverTheValue(t *testing.T) {
	const canary = "canary-9f3k-never-an-answer"
	t.Setenv("AEON_T3_SECRET", canary)
	_, d, _, _ := automationEnv(t)

	answers := []string{
		autoJSON(t, auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"source":"webhook","id":"whk-deploy"}`)),
		autoJSON(t, auto[protocol.AutomationShowResult](t, d, protocol.MethodAutomationShow, `{"source":"webhook","id":"whk-deploy"}`)),
		autoJSON(t, auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{"source":"webhook"}`)),
	}
	h := auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"source":"webhook","id":"whk-deploy"}`)
	if h.Health[0].Webhook == nil || !h.Health[0].Webhook.Set {
		t.Fatalf("secret row = %s, want Set true with the env set", autoJSON(t, h.Health[0].Webhook))
	}
	for i, a := range answers {
		if strings.Contains(a, canary) {
			t.Fatalf("answer %d leaks the secret's VALUE: %s", i+1, a)
		}
	}
	// The NAME crosses on purpose (a person must be able to check the
	// right env), so absence-of-everything is NOT the property; presence
	// of the name plus absence of the value is.
	if !strings.Contains(answers[0], "AEON_T3_SECRET") {
		t.Errorf("health lost the secret's NAME: %s", answers[0])
	}
	// bp-deploy is still missing, so CanFire stays false — the secret's
	// presence is one check, not the verdict.
	if h.Health[0].CanFire {
		t.Errorf("CanFire true with a missing blueprint — one ok check is not the whole compose")
	}
}

// autoStarter records every Start the engine attempts. A non-empty list
// after a disabled trigger drains would mean the engine ASKED the loop
// layer at all — it must not reach that far for a disabled row.
type autoStarter struct{ started []string }

func (f *autoStarter) Start(_ context.Context, r automation.EventStartRequest) (automation.StartResult, error) {
	f.started = append(f.started, r.TriggerID)
	return automation.StartResult{LoopID: "loop-never-real", Slug: r.Slug}, nil
}

// TestHealthNeverGatesFiring is AN8's other half, and the dangerous one:
// health says "disabled — enable it when a person is present", and the
// REAL dispatcher then walks the same outbox rows anyway and answers for
// itself. The disabled trigger's outbox rows each earn an engine-written
// skipped fire (the engine's OWN reason, "disabled: <reason>"), the
// already-decided rows stay exactly as they were (DO NOTHING), the starter
// is never consulted, and health — which never writes — says the same
// thing after as before.
func TestHealthNeverGatesFiring(t *testing.T) {
	s, d, _, tr := automationEnv(t)
	db, err := s.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE event_trigger SET enabled=0, disabled_reason='parked for the test' WHERE id=?`, tr.ID); err != nil {
		t.Fatal(err)
	}

	var compID string
	if err := db.QueryRow(`SELECT id FROM component WHERE slug='auto'`).Scan(&compID); err != nil {
		t.Fatal(err)
	}

	before := auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"id":"`+tr.ID+`"}`)
	if before.Health[0].CanFire || !strings.Contains(autoJSON(t, before.Health[0].Reasons), "disabled") {
		t.Fatalf("health of a disabled trigger = %s", autoJSON(t, before.Health[0]))
	}

	starter := &autoStarter{}
	clock := time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)
	disp := &automation.Dispatcher{Store: s, Starter: starter, Clock: func() time.Time { return clock }}
	n, err := disp.Drain(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 3 {
		t.Errorf("drain covered %d events, want all three outbox rows — health's verdict must not have narrowed the scan", n)
	}
	if len(starter.started) != 0 {
		t.Fatalf("starter was called for a disabled trigger: %v — advice leaked into the gate", starter.started)
	}

	// A NEW event, created while the trigger was disabled, is the engine's
	// own test: the trigger still ADMITS it (admission is the match, not
	// the switch), so the engine walks it and answers skipped in its own
	// words — the very verdict health only advised, made by the engine.
	if _, r, err := s.CreateManualTask(store.NewManualTask{
		ComponentID: compID, Title: "the probe", Type: "bug", By: autoActor,
	}); err != nil || r != nil {
		t.Fatalf("probe task: refusal=%v err=%v", r, err)
	}
	n, err = disp.Drain(context.Background())
	if err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if n != 1 {
		t.Errorf("second drain covered %d events, want the new one", n)
	}
	if len(starter.started) != 0 {
		t.Fatalf("starter was called for a disabled trigger: %v — advice leaked into the gate", starter.started)
	}

	firesAfter := auto[protocol.AutomationFiresResult](t, d, protocol.MethodAutomationFires, `{"source":"event","id":"`+tr.ID+`"}`)
	// The ORIGINAL started row never moved (DO NOTHING never overwrites a
	// decided row) and the engine added its own skip with the engine's
	// own reason.
	seen := map[string]int{}
	for _, f := range firesAfter.Fires {
		seen[f.Outcome]++
		if f.Outcome == "started" && f.Reason != "" {
			t.Errorf("the original started fire's reason moved: %+v", f)
		}
		if f.Outcome == "skipped" && !strings.Contains(f.Reason, "parked for the test") {
			t.Errorf("engine skip reason = %q, want it to carry the disabled reason the person set", f.Reason)
		}
	}
	if seen["started"] != 1 || seen["skipped"] != 1 {
		t.Errorf("fires after the drain = %s, want the old started plus one engine skip", autoJSON(t, firesAfter.Fires))
	}
	// Undecided claims stay undecided (the drain's DO NOTHING never
	// resurrects a decided row and never decides a claim without the
	// starter).
	var open int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_trigger_fire WHERE outcome IS NULL`).Scan(&open); err != nil || open != 1 {
		t.Errorf("open claims = %d (err %v), want the fixture's one mid-air claim untouched", open, err)
	}

	after := auto[protocol.AutomationHealthResult](t, d, protocol.MethodAutomationHealth, `{"id":"`+tr.ID+`"}`)
	if after.Health[0].CanFire {
		t.Errorf("health flipped after the engine fired — health must write nothing and learn nothing")
	}
	if len(after.Health[0].Reasons) != len(before.Health[0].Reasons) {
		t.Errorf("reasons before=%v after=%v — the same rows must compose the same advice", before.Health[0].Reasons, after.Health[0].Reasons)
	}
}

// TestPreviewIsTheClockByteForByte is AN8 at its purest: the socket's
// preview and automationview.NextFires run the SAME trigger.PreviewFires;
// the answer must be identical strings, and the DST gap day must appear
// exactly as the engine walks it (no invented instant).
func TestPreviewIsTheClockByteForByte(t *testing.T) {
	_, d, _, _ := automationEnv(t)
	from := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	want, err := automationview.NextFires(automationview.ScheduleSpec{
		ID: "sch-morning", Expr: "0 7 * * *", Zone: "Europe/Lisbon",
	}, from, 3)
	if err != nil {
		t.Fatal(err)
	}
	got := auto[protocol.AutomationPreviewResult](t, d, protocol.MethodAutomationPreview,
		`{"id":"sch-morning","n":3,"from":"2026-11-03T00:00:00Z"}`)
	if len(got.Instants) != len(want) {
		t.Fatalf("preview = %s, want %d instants", autoJSON(t, got), len(want))
	}
	for i, w := range want {
		g := got.Instants[i]
		if g.At != w.At.UTC().Format(time.RFC3339Nano) || g.Kind != w.Kind || g.Wall != w.Wall {
			t.Errorf("instant %d: socket %+v, engine %+v — the advice is not the engine's own answer", i, g, w)
		}
	}

	// The DST gap, still differential: a draft spec across the Lisbon
	// gap (29-Mar-2026, 01:00 does not exist) must equal the engine
	// instant for instant — whatever the engine decides IS the answer.
	got2 := auto[protocol.AutomationPreviewResult](t, d, protocol.MethodAutomationPreview,
		`{"spec":{"expr":"0 1 * * *","zone":"Europe/Lisbon"},"n":4,"from":"2026-03-28T12:00:00Z"}`)
	want2, err := automationview.NextFires(automationview.ScheduleSpec{
		Expr: "0 1 * * *", Zone: "Europe/Lisbon",
	}, time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC), 4)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(got2.Instants)
	b2, _ := json.Marshal(want2)
	if !strings.Contains(string(b1), `"at"`) || strings.Count(string(b1), `"at"`) != 4 {
		t.Fatalf("gap preview = %s", b1)
	}
	// Field-by-field equality (the wire shape renames time→RFC3339).
	for i := range want2 {
		if got2.Instants[i].At != want2[i].At.UTC().Format(time.RFC3339Nano) {
			t.Errorf("gap instant %d: socket %s, engine %s — %s", i, got2.Instants[i].At, want2[i].At, b2)
		}
	}
}

// TestPreviewRefusalsSpeakTheEnginesOwnSentences pins each refusal to the
// exact source of its words: the webhook refusal is trigger.PreviewFires'
// sentence verbatim, the event sentence names the family, a bad zone is
// ErrAutomationPreview quoting the engine, an unknown id is -31890.
func TestPreviewRefusalsSpeakTheEnginesOwnSentences(t *testing.T) {
	_, d, _, tr := automationEnv(t)

	_, err := autoCall(t, d, protocol.MethodAutomationPreview, `{"id":"whk-deploy"}`)
	if err == nil || !strings.Contains(err.Error(), "signed POST") {
		t.Errorf("webhook preview = %v, want the engine's own signed-POST refusal verbatim", err)
	}
	_, err = autoCall(t, d, protocol.MethodAutomationPreview, `{"id":"`+tr.ID+`"}`)
	if err == nil || !strings.Contains(err.Error(), "never on a clock") {
		t.Errorf("event preview = %v, want the never-on-a-clock sentence", err)
	}

	_, err = autoCall(t, d, protocol.MethodAutomationPreview,
		`{"spec":{"expr":"0 7 * * *","zone":"Mars/Olympus_Mons"},"n":1}`)
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != -31891 || e.Data.What == "" {
		t.Fatalf("bad zone preview = %v, want -31891 quoting the engine's zone sentence", err)
	}

	_, err = autoCall(t, d, protocol.MethodAutomationPreview, `{"id":"sch-mornin"}`)
	if !errors.As(err, &e) || e.Code != -31890 || !strings.Contains(e.Data.What, "sch-morning") {
		t.Errorf("typo preview = %v — the nearest name must ride with the refusal", err)
	}
}

// TestDryRunScansAndNeverClaims pins the one read that stands beside a
// WRITE engine: it answers the trigger's own Admits over the outbox
// window the trigger itself would scan, and touches nothing — no fire
// rows, no cursor, no outbox mutation.
func TestDryRunScansAndNeverClaims(t *testing.T) {
	s, d, _, tr := automationEnv(t)
	snap := func() map[string]int {
		db, err := s.GormDB().DB()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		for _, tbl := range []string{"automation_event", "event_trigger_fire", "trigger_fire", "loop", "automation_change"} {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
				t.Fatalf("%s: %v", tbl, err)
			}
			out[tbl] = n
		}
		return out
	}
	before := snap()

	res := auto[protocol.AutomationDryRunResult](t, d, protocol.MethodAutomationDryRun, `{"id":"`+tr.ID+`"}`)
	if res.From != tr.FromOutboxID || res.Scanned != 3 {
		t.Fatalf("dry run window = %+v, want from the trigger's own cursor scanning all three outbox rows", res)
	}
	// The trigger's match is task.created + task_type bug: the feature
	// row must NOT would-match, and the two bug rows must.
	if len(res.WouldMatch) != 2 {
		t.Fatalf("wouldMatch = %s, want exactly the two bug rows", autoJSON(t, res.WouldMatch))
	}
	for _, w := range res.WouldMatch {
		if w.Event != "task.created" || w.SubjectKind != "task" || w.SubjectID == "" {
			t.Errorf("wouldMatch row = %+v", w)
		}
	}

	narrow := auto[protocol.AutomationDryRunResult](t, d, protocol.MethodAutomationDryRun,
		`{"id":"`+tr.ID+`","sinceOutboxId":2}`)
	if narrow.Scanned != 1 || len(narrow.WouldMatch) != 1 || narrow.WouldMatch[0].OutboxID != 3 {
		t.Errorf("sinceOutboxId=2 = %s, want only outbox row 3", autoJSON(t, narrow))
	}

	after := snap()
	for tbl, n := range before {
		if after[tbl] != n {
			t.Errorf("%s went %d → %d — a dry run wrote a row", tbl, n, after[tbl])
		}
	}
	// The trigger's cursor did not move either — that is the dispatcher's
	// frontier, and a read never eats history.
	live, err := s.ListEventTriggers(store.EventTriggerFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range live {
		if row.ID == tr.ID && row.FromOutboxID != tr.FromOutboxID {
			t.Errorf("the trigger's cursor moved %d → %d", tr.FromOutboxID, row.FromOutboxID)
		}
	}

	// A dry run is the event family's question: the schedule refuses,
	// the unknown id refuses.
	_, err = autoCall(t, d, protocol.MethodAutomationDryRun, `{"id":"sch-morning"}`)
	if err == nil || !strings.Contains(err.Error(), "is a schedule") || !strings.Contains(err.Error(), "dry run") {
		t.Errorf("schedule dry run = %v", err)
	}
	var e *protocol.Error
	_, err = autoCall(t, d, protocol.MethodAutomationDryRun, `{"id":"nope"}`)
	if !errors.As(err, &e) || e.Code != -31890 {
		t.Errorf("unknown dry run = %v, want -31890", err)
	}
}

// TestS49ReadsWriteNothing is the whole-plane snapshot: every one of the
// ten methods answers at least once over the fixture, and the database is
// byte-identical afterwards — the same discipline S47 proved for the tool
// catalog.
func TestS49ReadsWriteNothing(t *testing.T) {
	s, d, _, tr := automationEnv(t)
	db, err := s.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	dump := func() []string {
		var out []string
		for _, tbl := range []string{"automation_event", "event_trigger", "event_trigger_fire", "trigger_schedule", "trigger_fire", "loop", "automation_change", "automation_cursor"} {
			rows, err := db.Query(`SELECT * FROM ` + tbl + ` ORDER BY 1`)
			if err != nil {
				t.Fatalf("%s: %v", tbl, err)
			}
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				out = append(out, tbl+" "+autoJSON(t, vals))
			}
			rows.Close()
		}
		return out
	}
	before := dump()

	sweeps := []struct {
		m      protocol.MethodName
		params string
	}{
		{protocol.MethodAutomationList, `{}`},
		{protocol.MethodAutomationList, `{"source":"schedule","q":"morning"}`},
		{protocol.MethodAutomationShow, `{"source":"schedule","id":"sch-morning"}`},
		{protocol.MethodAutomationShow, `{"source":"event","id":"` + tr.ID + `"}`},
		{protocol.MethodAutomationFires, `{}`},
		{protocol.MethodAutomationFires, `{"source":"schedule","id":"sch-morning","outcome":"started"}`},
		{protocol.MethodAutomationRuns, `{"source":"schedule","id":"sch-morning"}`},
		{protocol.MethodAutomationChanges, `{}`},
		{protocol.MethodAutomationChanges, `{"source":"schedule","id":"sch-morning"}`},
		{protocol.MethodAutomationHealth, `{}`},
		{protocol.MethodAutomationHealth, `{"id":"whk-deploy"}`},
		{protocol.MethodAutomationVocabulary, `{}`},
		{protocol.MethodAutomationTest, `{"match":{"event":"task.created","task_type":["bug"]},"sample":{"event":"task.created","taskType":"bug"}}`},
		{protocol.MethodAutomationPreview, `{"spec":{"expr":"@hourly","zone":"UTC"},"n":2}`},
		{protocol.MethodAutomationPreview, `{"id":"sch-morning"}`},
		{protocol.MethodAutomationDryRun, `{"id":"` + tr.ID + `"}`},
	}
	for i, sw := range sweeps {
		if _, err := autoCall(t, d, sw.m, sw.params); err != nil {
			t.Fatalf("sweep %d (%s %s): %v — the fixture must answer, not refuse", i+1, sw.m, sw.params, err)
		}
	}

	after := dump()
	if len(before) != len(after) {
		t.Fatalf("row count moved %d → %d — a read wrote a row", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("row changed:\n before %s\n after  %s", before[i], after[i])
		}
	}
}

// TestS20ReadsUnchangedAfterTheS49Plane pins the compatibility promise:
// the OLD reporting answers exactly what it answered before S49 existed,
// over rows both planes can see.
func TestS20ReadsUnchangedAfterTheS49Plane(t *testing.T) {
	_, d, _, _ := automationEnv(t)
	first := autoJSON(t, func() any {
		r, err := autoCall(t, d, protocol.MethodTriggerList, `{}`)
		if err != nil {
			t.Fatalf("trigger.list: %v", err)
		}
		return r
	}())
	firstHistory := autoJSON(t, func() any {
		r, err := autoCall(t, d, protocol.MethodTriggerHistory, `{"id":"sch-morning"}`)
		if err != nil {
			t.Fatalf("trigger.history: %v", err)
		}
		return r
	}())

	// The new plane reads the same rows hard…
	autoCall(t, d, protocol.MethodAutomationList, `{"archived":"include"}`)
	autoCall(t, d, protocol.MethodAutomationFires, `{}`)
	autoCall(t, d, protocol.MethodAutomationHealth, `{}`)

	again := autoJSON(t, func() any {
		r, err := autoCall(t, d, protocol.MethodTriggerList, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}())
	againHistory := autoJSON(t, func() any {
		r, err := autoCall(t, d, protocol.MethodTriggerHistory, `{"id":"sch-morning"}`)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}())
	if first != again || firstHistory != againHistory {
		t.Errorf("S20's answers drifted under the S49 reads:\n%s\n%s", firstHistory, againHistory)
	}
}
