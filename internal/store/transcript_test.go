package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T6's tests, in order of importance. The first one's failure would otherwise
// surface three slices away, as a provider 400 about signatures with nothing
// pointing at the store.

func TestASignedThinkingBlockSurvivesWriteAndReadByteIdentically(t *testing.T) {
	// D7, and the reason this file exists.
	//
	// Asserted on the EXACT strings, including the trailing newline in both the
	// thinking text and the signature. Anthropic rejects a tool-use turn whose
	// prior thinking was altered, and S7 recovers a crashed step by replaying
	// exactly these rows — so a byte lost here surfaces in S7 as a provider
	// error about something else (P21).
	//
	// A comparison of two parsed structures would pass through the same encoder
	// twice and prove nothing about the column, so the stored bytes are read
	// raw as well.
	s := openTemp(t)
	sc := seedLoop(t, s, "fidelity", "/repo")
	st := seedStep(t, s, sc)

	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatalf("append: %v", err)
	}

	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("read: %s", res.Reason())
	}

	var found bool
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Kind != model.BlockThinking {
				continue
			}
			found = true
			if b.Text != thinkingText {
				t.Errorf("the thinking TEXT changed.\n  got  %q\n  want %q", b.Text, thinkingText)
			}
			if b.Signature != thinkingSignature {
				t.Errorf("the thinking SIGNATURE changed.\n  got  %q\n  want %q", b.Signature, thinkingSignature)
			}
			if len(b.Signature) != len(thinkingSignature) {
				t.Errorf("the signature is %d bytes, want %d — a signature short by one byte is "+
					"rejected by Anthropic on the next tool-use turn", len(b.Signature), len(thinkingSignature))
			}
		}
	}
	if !found {
		t.Fatal("no thinking block came back at all")
	}

	// And the raw column: the exact signature must be IN THE FILE.
	raw, err := s.RawTranscriptContent(st.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	var blocks []model.Block
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatalf("the stored row is not a block array: %v", err)
	}
	if blocks[0].Signature != thinkingSignature {
		t.Errorf("the SIGNATURE ON DISK differs.\n  got  %q\n  want %q", blocks[0].Signature, thinkingSignature)
	}
}

func TestATranscriptWithAllFourBlockKindsRoundTripsAndValidates(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "allkinds", "/repo")
	st := seedStep(t, s, sc)

	want := signedTranscript()
	if err := s.AppendMessages(st.ID, want); err != nil {
		t.Fatal(err)
	}
	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("read: %s", res.Reason())
	}
	if len(res.Messages) != len(want) {
		t.Fatalf("got %d messages, want %d", len(res.Messages), len(want))
	}

	kinds := map[model.BlockKind]bool{}
	for _, m := range res.Messages {
		for _, b := range m.Content {
			kinds[b.Kind] = true
		}
	}
	for _, kind := range []model.BlockKind{model.BlockText, model.BlockThinking,
		model.BlockToolUse, model.BlockToolResult} {
		if !kinds[kind] {
			t.Errorf("the %s block did not survive", kind)
		}
	}

	// S3's validator must accept what came back — a transcript that round-trips
	// into something invalid is unresumable.
	if v := model.ValidateTranscript(res.Messages); !v.OK {
		t.Errorf("the round-tripped transcript is invalid: %s (message %d)", v.Reason, v.Index)
	}

	// The tool_use input survives as JSON, not as a re-encoded string.
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockToolUse && string(b.Input) != `{"expr":"17*23"}` {
				t.Errorf("the tool_use input came back as %s", b.Input)
			}
		}
	}
}

func TestTwoInsertsAtTheSameStepIDAndSeqFailOnTheConstraint(t *testing.T) {
	// D8. UNIQUE (step_id, seq) is what makes a concurrent append DETECTABLY
	// wrong rather than silently lossy — without it, the second write wins and
	// the first message is gone with nothing saying so.
	s := openTemp(t)
	sc := seedLoop(t, s, "unique", "/repo")
	st := seedStep(t, s, sc)

	const insert = `INSERT INTO step_message (step_id, seq, format_version, role, content, created_at)
		VALUES (?,1,1,'user','[{"kind":"text","text":"first"}]','t')`
	if _, err := s.w.Exec(insert, st.ID); err != nil {
		t.Fatalf("the first insert failed: %v", err)
	}
	_, err := s.w.Exec(insert, st.ID)
	if err == nil {
		t.Fatal("a second message at the same (step_id, seq) was accepted")
	}
	if !isUniqueViolation(err) {
		t.Errorf("the second insert failed for the wrong reason: %v", err)
	}
}

func TestAppendMessagesAssignsSeqAndTheCallerCannotSupplyIt(t *testing.T) {
	// O3. There is no parameter for it — asserted here on the behaviour, and
	// by the signature itself, which takes only the step and the messages.
	s := openTemp(t)
	sc := seedLoop(t, s, "seq", "/repo")
	st := seedStep(t, s, sc)

	first := []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("one")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("two")}},
	}
	if err := s.AppendMessages(st.ID, first); err != nil {
		t.Fatal(err)
	}
	// A second append continues from the maximum, rather than restarting.
	second := []model.Message{
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("three")}},
	}
	if err := s.AppendMessages(st.ID, second); err != nil {
		t.Fatal(err)
	}

	rows, err := s.r.Query(`SELECT seq FROM step_message WHERE step_id = ? ORDER BY seq`, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var n int64
		_ = rows.Scan(&n)
		seqs = append(seqs, n)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Errorf("seq is %v, want [1 2 3] — monotonic and contiguous across two appends", seqs)
	}
}

func TestAllRowsForOneAppendLandInOneTransaction(t *testing.T) {
	// A half-written transcript is unresumable. The failure is forced by
	// appending a batch whose LAST message is the one that breaks the
	// whole-transcript rule: the earlier rows are already inserted when the
	// validation runs, so if the transaction were not there they would survive.
	s := openTemp(t)
	sc := seedLoop(t, s, "atomic", "/repo")
	st := seedStep(t, s, sc)

	bad := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("this one is fine")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("so is this")}},
		// Two assistant turns in a row: no provider accepts it, and S3's
		// validator says so. Both carry provenance: without it the fragment
		// gate would refuse this batch on M26 grounds BEFORE the whole-transcript
		// rule ran, and the test would be green for the wrong reason.
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("and this is the problem")}},
	}
	if err := s.AppendMessages(st.ID, bad); err == nil {
		t.Fatal("an append that produces an invalid transcript succeeded")
	}

	n, err := s.TranscriptRows(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d rows survived a failed append; all rows for one call go in one transaction", n)
	}
}

func TestAnInvalidBlockIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	// The cheap check, which names the block's index rather than leaving the
	// caller to find it.
	//
	// The block used is a `tool_use` with no id. An UNSIGNED thinking block
	// would NOT do: S3 permits one deliberately, because not every provider
	// signs — found by writing this test the obvious way and watching it fail.
	// The store does not get to be stricter than the model layer about what a
	// provider may send.
	s := openTemp(t)
	sc := seedLoop(t, s, "badblock", "/repo")
	st := seedStep(t, s, sc)

	err := s.AppendMessages(st.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("fine")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{{Kind: model.BlockToolUse, Name: "calculator"}}},
	})
	if err == nil {
		t.Fatal("a tool_use block with no id was accepted; nothing could ever match a result to it")
	}
	if !strings.Contains(err.Error(), "message 1 block 0") {
		t.Errorf("the refusal does not name the block: %v", err)
	}
	if n, _ := s.TranscriptRows(st.ID); n != 0 {
		t.Errorf("%d rows were written before the refusal", n)
	}
}

func TestReadTranscriptReturnsSeqOrderEvenWhenRowsWereInsertedOutOfOrder(t *testing.T) {
	// The read orders by seq rather than by rowid. Inserting out of order is
	// what compaction and a concurrent append both do.
	s := openTemp(t)
	sc := seedLoop(t, s, "ordering", "/repo")
	st := seedStep(t, s, sc)

	insert := func(seq int, role, text string) {
		content, _ := json.Marshal([]model.Block{model.TextBlock(text)})
		if _, err := s.w.Exec(`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at)
			VALUES (?,?,?,?,?,'t')`, st.ID, seq, model.FormatVersion, role, string(content)); err != nil {
			t.Fatalf("insert seq %d: %v", seq, err)
		}
	}
	insert(3, "assistant", "third")
	insert(1, "system", "first")
	insert(2, "user", "second")

	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("read: %s", res.Reason())
	}
	want := []string{"first", "second", "third"}
	for i, m := range res.Messages {
		if m.Content[0].Text != want[i] {
			t.Errorf("message %d is %q, want %q", i, m.Content[0].Text, want[i])
		}
	}
}

func TestAFormatVersionAboveTheCurrentOneIsRefusedLoudly(t *testing.T) {
	// O4. A newer Aeon wrote the row, and a best-effort parse would silently
	// drop fields this build does not understand — plausibly including a
	// signature. The refusal is S3's wording, from the one place that owns the
	// decision.
	s := openTemp(t)
	sc := seedLoop(t, s, "future", "/repo")
	st := seedStep(t, s, sc)

	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}
	// One row from the future refuses the WHOLE transcript: parsing the rest
	// and dropping that one would hand a recovery a transcript with a hole.
	if _, err := s.w.Exec(`UPDATE step_message SET format_version = ? WHERE step_id = ? AND seq = 2`,
		model.FormatVersion+1, st.ID); err != nil {
		t.Fatal(err)
	}

	res := s.ReadTranscript(st.ID)
	if res.OK {
		t.Fatal("a transcript written by a newer Aeon was parsed anyway")
	}
	if res.Migration.Outcome != model.MigrationFromTheFuture {
		t.Errorf("the outcome is %q, want from-the-future — `unparseable` would send a reader to the "+
			"wrong fix", res.Migration.Outcome)
	}
	if !strings.Contains(res.Reason(), "Upgrade") {
		t.Errorf("the refusal does not say what to do: %s", res.Reason())
	}
	if res.Count != 4 {
		t.Errorf("the result says %d rows were read; `four rows that will not parse` and `no rows` are "+
			"different problems", res.Count)
	}
}

func TestDeletingAStepCascadesToItsStepMessageRows(t *testing.T) {
	// Asserted by ROW COUNT, because a cascade that did not happen leaves rows
	// that read back perfectly well on their own.
	s := openTemp(t)
	sc := seedLoop(t, s, "cascade", "/repo")
	st := seedStep(t, s, sc)
	other := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(other.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.w.Exec(`DELETE FROM step WHERE id = ?`, st.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.TranscriptRows(st.ID); n != 0 {
		t.Errorf("%d rows survived their step", n)
	}
	// And the cascade took ONLY that step's rows.
	if n, _ := s.TranscriptRows(other.ID); n != 4 {
		t.Errorf("the other step's transcript lost rows: %d remain, want 4", n)
	}
}

func TestDeletingALoopCascadesAllTheWayDown(t *testing.T) {
	// The deeper cascade: loop -> step -> step_message, through two levels.
	// This is the one that proves the pragma reached the connection for a
	// multi-level chain rather than only a direct child.
	s := openTemp(t)
	sc := seedLoop(t, s, "deep", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "proceed?"}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.w.Exec(`DELETE FROM loop WHERE component_root = ? AND slug = ?`, sc.ComponentRoot, sc.LoopSlug); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		what  string
		query string
	}{
		{"step", `SELECT count(*) FROM step WHERE loop_slug = ?`},
		{"step_message", `SELECT count(*) FROM step_message`},
		{"question", `SELECT count(*) FROM question WHERE loop_slug = ?`},
	} {
		var n int
		var err error
		if strings.Contains(q.query, "?") {
			err = s.r.QueryRow(q.query, sc.LoopSlug).Scan(&n)
		} else {
			err = s.r.QueryRow(q.query).Scan(&n)
		}
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%d %s rows survived their loop", n, q.what)
		}
	}
}

func TestNoStoredContentContainsAProviderShapedKey(t *testing.T) {
	// `choices`, `tool_calls` and `content_block` are OpenAI's and Anthropic's
	// wire names. One appearing here means something in S3 leaked past the
	// adapter, and finding it HERE turns a mystery three slices later into a
	// visible failure now.
	s := openTemp(t)
	sc := seedLoop(t, s, "canonical", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}

	// # Why the scan lives in the test rather than on the store
	//
	// S3's own boundary guard (D14) forbids a provider's field names anywhere
	// outside `internal/model/adapters/`, and it excludes `_test.go` files
	// deliberately — because a test that LOOKS FOR a provider shape is the
	// opposite of a leak. A `ContainsProviderShape` method on the store failed
	// that guard, and the right answer was to move the needles here rather than
	// to add a file exemption: S3's own comment records that "a check that
	// grows a filename list gets weakened until it matches nothing".
	found, err := scanForProviderShapes(s)
	if err != nil {
		t.Fatal(err)
	}
	if found != "" {
		t.Errorf("a provider shape reached the store: %s", found)
	}

	// The negative control: the scan must be able to find one.
	if _, err := s.w.Exec(`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at)
		VALUES (?,99,1,'assistant','[{"choices":[]}]','t')`, st.ID); err != nil {
		t.Fatal(err)
	}
	found, err = scanForProviderShapes(s)
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Error("the scan did not find a planted provider shape, so it proves nothing")
	}
}

// scanForProviderShapes looks for a provider's wire names in stored content.
//
// `choices` is OpenAI's, `tool_calls` is OpenAI's, `content_block` is
// Anthropic's. One appearing in `step_message` means something in S3 leaked a
// provider shape past the adapter, and finding it HERE turns a mystery three
// slices later into a visible failure now.
func scanForProviderShapes(s *Store) (string, error) {
	rows, err := s.r.Query(`SELECT step_id, seq, content FROM step_message`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	needles := []string{`"choices"`, `"tool_calls"`, `"content_block"`}
	for rows.Next() {
		var stepID, seq int64
		var content string
		if err := rows.Scan(&stepID, &seq, &content); err != nil {
			return "", err
		}
		for _, needle := range needles {
			if strings.Contains(content, needle) {
				return "step " + itoa(stepID) + " message " + itoa(seq) + " contains " + needle, nil
			}
		}
	}
	return "", rows.Err()
}

func TestReplaceRangeKeepsSeqMonotonicAndContiguous(t *testing.T) {
	// Compaction. This is the operation most likely to break the seq
	// invariant, which is why it lives in the store rather than at the caller.
	s := openTemp(t)
	sc := seedLoop(t, s, "compaction", "/repo")
	st := seedStep(t, s, sc)

	original := []model.Message{
		{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("system")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("one")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("two")}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("three")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock("four")}},
	}
	if err := s.AppendMessages(st.ID, original); err != nil {
		t.Fatal(err)
	}

	// Replace the middle two with one summary.
	if err := s.ReplaceRange(st.ID, 2, 3, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("[summary of one and two]")}},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	rows, err := s.r.Query(`SELECT seq FROM step_message WHERE step_id = ? ORDER BY seq`, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var n int64
		_ = rows.Scan(&n)
		seqs = append(seqs, n)
	}
	if len(seqs) != 4 {
		t.Fatalf("after replacing 2 messages with 1, there are %d rows: %v", len(seqs), seqs)
	}
	for i, got := range seqs {
		if got != int64(i+1) {
			t.Errorf("seq is %v, want contiguous from 1", seqs)
			break
		}
	}

	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("the compacted transcript does not read back: %s", res.Reason())
	}
	if res.Messages[1].Content[0].Text != "[summary of one and two]" {
		t.Errorf("the replacement is not where it should be: %q", res.Messages[1].Content[0].Text)
	}
	if res.Messages[3].Content[0].Text != "four" {
		t.Errorf("the tail did not come down with the replacement: %q", res.Messages[3].Content[0].Text)
	}
}

func TestReplaceRangeRefusesAReplacementThatWouldOrphanAToolUse(t *testing.T) {
	// Compaction must never orphan a tool_use from its tool_result. The CALLER
	// enforces that, and this function must not make it impossible — but the
	// whole-transcript validation also catches the case where a replacement
	// removes the tool_use and leaves its result behind.
	s := openTemp(t)
	sc := seedLoop(t, s, "orphan", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}

	// Message 3 holds the tool_use; message 4 holds its result. Replacing 3
	// with plain text leaves the result answering nothing.
	err := s.ReplaceRange(st.ID, 3, 3, []model.Message{
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("I decided not to call anything")}},
	})
	if err == nil {
		t.Fatal("a replacement that orphaned a tool_result was accepted")
	}
	if !strings.Contains(err.Error(), "tool_result") {
		t.Errorf("the refusal does not name the problem: %v", err)
	}

	// And nothing changed.
	res := s.ReadTranscript(st.ID)
	if !res.OK {
		t.Fatalf("the transcript is broken after a refused replacement: %s", res.Reason())
	}
	if len(res.Messages) != 4 {
		t.Errorf("the refused replacement changed the transcript: %d messages", len(res.Messages))
	}
}

func TestAnEmptyTranscriptReadsAsARefusalRatherThanAnEmptySuccess(t *testing.T) {
	// A caller handed an empty success would resume a step with no context and
	// no indication that anything was missing.
	s := openTemp(t)
	sc := seedLoop(t, s, "empty", "/repo")
	st := seedStep(t, s, sc)
	res := s.ReadTranscript(st.ID)
	if res.OK {
		t.Fatal("a step with no transcript read back as OK")
	}
	if res.Count != 0 {
		t.Errorf("count is %d", res.Count)
	}
}

func TestAppendingNothingIsANoOp(t *testing.T) {
	// A runtime that assembled no new messages should not have to check before
	// calling.
	s := openTemp(t)
	sc := seedLoop(t, s, "noop", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, nil); err != nil {
		t.Errorf("appending nothing failed: %v", err)
	}
	if n, _ := s.TranscriptRows(st.ID); n != 0 {
		t.Errorf("appending nothing wrote %d rows", n)
	}
}
