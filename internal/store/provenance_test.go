package store

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T1: the provenance column, the write gate that requires it on new
// assistant turns, and the grandfathering that lets pre-M26 rows keep living.
// The rule lives in model.Message.ValidateProvenance (one home); these tests
// pin that the STORE is where it bites.

// TestAppendRefusesAnAssistantTurnWithoutProvenance — M26 at the gate. The
// refusal must name what it refused, not just fail.
func TestAppendRefusesAnAssistantTurnWithoutProvenance(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "prov-gate", t.TempDir())
	st := seedStep(t, s, sc)

	err := s.AppendMessages(st.ID, []model.Message{
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("produced by someone")}},
	})
	if err == nil {
		t.Fatal("an assistant turn without provenance was appended (M26)")
	}
	if !strings.Contains(err.Error(), "provenance") {
		t.Errorf("the refusal does not name provenance: %v", err)
	}

	n, err := s.TranscriptRows(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d rows survived the refused append; the gate precedes the write", n)
	}
}

// TestAppendRefusesHollowTriples — each empty leg of the triple is its own
// refusal, so a stamped-but-vacuous row cannot pass the gate on the strength
// of the field being present.
func TestAppendRefusesHollowTriples(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "prov-hollow", t.TempDir())
	st := seedStep(t, s, sc)

	hollow := []*model.Provenance{
		{Route: "", Provider: "anthropic", Model: "claude"},
		{Route: "r1", Provider: "", Model: "claude"},
		{Route: "r1", Provider: "anthropic", Model: ""},
	}
	for i, p := range hollow {
		err := s.AppendMessages(st.ID, []model.Message{
			{Role: model.RoleAssistant, Provenance: p, Content: []model.Block{model.TextBlock("x")}},
		})
		if err == nil {
			t.Errorf("hollow triple %d (%+v) was appended", i, p)
		}
	}
	n, err := s.TranscriptRows(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d rows survived refused appends", n)
	}
}

// TestAppendRefusesProvenanceOnAUserTurn — symmetric half of the rule: a
// user turn has no producing model, and stamping one trains readers that
// provenance is optional noise.
func TestAppendRefusesProvenanceOnAUserTurn(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "prov-user", t.TempDir())
	st := seedStep(t, s, sc)

	err := s.AppendMessages(st.ID, []model.Message{
		{Role: model.RoleUser, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("asked it myself")}},
	})
	if err == nil {
		t.Fatal("a user turn with provenance was appended")
	}
	if !strings.Contains(err.Error(), "user") {
		t.Errorf("the refusal does not name the role: %v", err)
	}
}

// TestProvenanceSurvivesRoundTrip — the column stores the triple and the
// read path rehydrates it. An append→read that loses it silently would make
// the M27 replay rule (thinking goes only to its producer) operate on nils.
func TestProvenanceSurvivesRoundTrip(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "prov-roundtrip", t.TempDir())
	st := seedStep(t, s, sc)

	p := &model.Provenance{Route: "prod-route", Provider: "anthropic", Model: "claude-sonnet-4-5"}
	if err := s.AppendMessages(st.ID, []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("terse")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("2+2?")}},
		{Role: model.RoleAssistant, Provenance: p, Content: []model.Block{model.TextBlock("4")}},
	}); err != nil {
		t.Fatal(err)
	}

	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("the stamped transcript does not read back: %s", res.Reason())
	}
	if len(res.Messages) != 3 {
		t.Fatalf("%d messages, want 3", len(res.Messages))
	}
	if res.Messages[0].Provenance != nil || res.Messages[1].Provenance != nil {
		t.Error("system/user rows came back with provenance they never had")
	}
	got := res.Messages[2].Provenance
	if got == nil || *got != *p {
		t.Errorf("the assistant's triple did not survive the round trip: %+v", got)
	}

	// And the same transcript still passes the append gate when a new stamped
	// turn joins it — validateWhole now reads provenance rows too.
	p2 := &model.Provenance{Route: "other-route", Provider: "openai", Model: "gpt-4o"}
	if err := s.AppendMessages(st.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("again")}},
		{Role: model.RoleAssistant, Provenance: p2, Content: []model.Block{model.TextBlock("4")}},
	}); err != nil {
		t.Fatalf("a stamped turn refused to join a stamped transcript: %v", err)
	}
	res = s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("the extended transcript does not read back: %s", res.Reason())
	}
}

// TestTranscriptSpanningProvidersReportsBoth — M26's promise, DoD D1's
// store half: A→B→A, and every turn still names the server that produced
// IT, in order. A loop-level stamp could not report the middle turn at all.
func TestTranscriptSpanningProvidersReportsBoth(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "prov-span", t.TempDir())
	st := seedStep(t, s, sc)

	a := &model.Provenance{Route: "a", Provider: "anthropic", Model: "claude"}
	b := &model.Provenance{Route: "b", Provider: "openai", Model: "gpt-4o"}
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
		{Role: model.RoleAssistant, Provenance: a, Content: []model.Block{model.TextBlock("first")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("continue")}},
		{Role: model.RoleAssistant, Provenance: b, Content: []model.Block{model.TextBlock("second")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("and again")}},
		{Role: model.RoleAssistant, Provenance: a, Content: []model.Block{model.TextBlock("third")}},
	}
	if err := s.AppendMessages(st.ID, msgs); err != nil {
		t.Fatal(err)
	}

	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("span transcript: %s", res.Reason())
	}
	want := []string{"a", "b", "a"}
	var got []string
	for _, m := range res.Messages {
		if m.Role != model.RoleAssistant {
			continue
		}
		if m.Provenance == nil {
			t.Fatalf("assistant turn lost its provenance on the read path")
		}
		got = append(got, m.Provenance.Route)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("producers read back %v, want %v — per turn and in order (I4)", got, want)
	}
}

// TestPreM26RowsStillReadAndStillAcceptAppends — the grandfathering half,
// and the reason the rule is a WRITE rule: a row written before migration
// 0027 has no producer on record, inventing one would be fabrication, and
// REFUSING to read it would break S7's recovery of its own history. So the
// old row reads (Provenance nil, transcript still OK) and new STAMPED turns
// can still join the old step.
func TestPreM26RowsStillReadAndStillAcceptAppends(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "prov-legacy", t.TempDir())
	st := seedStep(t, s, sc)

	// The INSERT as it stood before 0027: no provenance column at all, which
	// the ALTER's DEFAULT '' absorbs. Two rows, because a real transcript
	// starts with a user turn and the validator says so.
	pre := []struct{ role, content string }{
		{string(model.RoleUser), `[{"kind":"text","text":"before M26"}]`},
		{string(model.RoleAssistant), `[{"kind":"text","text":"written before M26, producer unknown"}]`},
	}
	for i, r := range pre {
		if _, err := s.w.ExecContext(context.Background(),
			`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at)
			VALUES (?,?,?,?,?,?)`,
			st.ID, i+1, model.FormatVersion, r.role, r.content, s.Now()); err != nil {
			t.Fatal(err)
		}
	}

	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("a pre-M26 transcript must still read back for recovery (P2): %s", res.Reason())
	}
	if res.Messages[1].Provenance != nil {
		t.Error("a pre-M26 row was given provenance it never earned — fabrication (M26)")
	}

	if err := s.AppendMessages(st.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("new turn")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("stamped")}},
	}); err != nil {
		t.Fatalf("an old step refused a new stamped turn: %v", err)
	}
	res = s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("the mixed transcript does not read: %s", res.Reason())
	}
	if res.Messages[0].Provenance != nil || res.Messages[1].Provenance != nil || res.Messages[3].Provenance == nil {
		t.Error("the mixed transcript does not distinguish earned provenance from its absence")
	}
}

// TestProvenanceColumnIsWhatTheAlterSaysItIs — NOT NULL DEFAULT ” in TEXT.
// Asserted against the live schema, never against the DSN or the const.
func TestProvenanceColumnIsWhatTheAlterSaysItIs(t *testing.T) {
	s := openTemp(t)
	rows, err := s.r.Query(
		`SELECT name, type, "notnull", dflt_value FROM pragma_table_info('step_message') WHERE name = 'provenance'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("step_message has no provenance column — migration 0027 did not land")
	}
	var name, typ string
	var notnull int
	var dflt interface{}
	if err := rows.Scan(&name, &typ, &notnull, &dflt); err != nil {
		t.Fatal(err)
	}
	if typ != "TEXT" || notnull != 1 {
		t.Errorf("provenance is %s notnull=%d, want TEXT notnull=1", typ, notnull)
	}
	if d, ok := dflt.(string); !ok || d != "''" {
		t.Errorf("the default is %v, want '' — pre-M26 rows read, they are not refused", dflt)
	}
}

// TestProvenanceIsNotLoopLevel — I4. The triple belongs to the TURN. A
// loop- or step-level provider/model column would report the LAST server as
// though it produced everything, and the day somebody adds one "as a
// convenience" this test says why not. The loop and step tables are also
// exactly where a tempting denormalisation would land.
func TestProvenanceIsNotLoopLevel(t *testing.T) {
	s := openTemp(t)
	banned := map[string]bool{"provider": true, "model": true, "model_id": true,
		"route": true, "route_id": true, "provenance": true}
	for _, table := range []string{"loop", "step", "loop_phase"} {
		rows, err := s.r.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if banned[strings.ToLower(col)] {
				t.Errorf("%s.%s exists — provenance is per-turn, never per-loop or per-step (I4)", table, col)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
}
