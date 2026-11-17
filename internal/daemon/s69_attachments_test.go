package daemon

// s69_attachments_test.go — the attachment doors and the send-time
// expansion, end to end (S69 AT3–AT5): bytes cross the socket once, rows
// carry store-resolved receipts, and the wire gets base64 content blocks at
// the one join point — with the missing revision speaking for itself.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var s69PNG = []byte("\x89PNG\r\n\x1a\n" + "iaaa-bytes")

func s69Put(t *testing.T, f *streamFixture, conn, name string, data []byte) map[string]any {
	t.Helper()
	res := dispatchJSON(t, f, conn, protocol.MethodAttachmentPut, map[string]any{
		"slug": "chat", "componentRoot": f.root, "name": name,
		"mediaType": "image/png", "data": base64.StdEncoding.EncodeToString(data),
	})
	rec, ok := res["receipt"].(map[string]any)
	if !ok {
		t.Fatalf("no receipt came back: %v", res)
	}
	return rec
}

func dispatchErr(t *testing.T, f *streamFixture, connID string, method protocol.MethodName, params any) string {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.d.Table.Dispatch(context.Background(), connID, method, raw)
	if err == nil {
		t.Fatalf("%s was expected to refuse and did not", method)
	}
	return err.Error()
}

func TestTheAttachmentDoorsServePutThenRead(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	initConn(t, f.d, context.Background(), "c-init", "cli")

	rec := s69Put(t, f, "c-init", "diagram.png", s69PNG)
	if rec["name"] != "diagram.png" || rec["mediaType"] != "image/png" || rec["rev"].(float64) != 1 {
		t.Fatalf("receipt = %v", rec)
	}
	doc, _ := rec["document"].(string)
	if !strings.HasPrefix(doc, "@attachments/") {
		t.Fatalf("the document is not under the reserved namespace: %q", doc)
	}
	// Store-once, taken literally: the same bytes answer the same revision.
	again := s69Put(t, f, "c-init", "diagram.png", s69PNG)
	if again["document"] != rec["document"] || again["rev"] != rec["rev"] {
		t.Fatalf("the same bytes wrote a second revision: %v vs %v", again, rec)
	}

	got := dispatchJSON(t, f, "c-init", protocol.MethodAttachmentRead, map[string]any{
		"slug": "chat", "componentRoot": f.root, "document": doc, "rev": 1,
	})
	back, err := base64.StdEncoding.DecodeString(got["data"].(string))
	if err != nil || string(back) != string(s69PNG) {
		t.Fatalf("the frozen bytes drifted: %v %v", got["data"], err)
	}
	if got["mediaType"] != "image/png" {
		t.Fatalf("the bytes did not speak their own type: %v", got["mediaType"])
	}
	// A missing revision is named, not blanked.
	errRes := dispatchErr(t, f, "c-init", protocol.MethodAttachmentRead, map[string]any{
		"slug": "chat", "componentRoot": f.root, "document": doc, "rev": 9,
	})
	if !strings.Contains(errRes, "revision 9") {
		t.Fatalf("the absence did not name itself: %q", errRes)
	}
}

func TestThePutDoorRefusesALyingClaimAndTheMessageDoorAnEmptyList(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	initConn(t, f.d, context.Background(), "c-init", "cli")

	errRes := dispatchErr(t, f, "c-init", protocol.MethodAttachmentPut, map[string]any{
		"slug": "chat", "componentRoot": f.root, "name": "diagram.png",
		"mediaType": "image/png", "data": base64.StdEncoding.EncodeToString([]byte("%PDF-1.7 forged")),
	})
	if !strings.Contains(errRes, "image/png") || !strings.Contains(errRes, "application/pdf") {
		t.Fatalf("the magic-byte verdict did not name both sides: %q", errRes)
	}
	// An EMPTY attachments list is refused at the message door (AT3): the
	// third state is ABSENT, and `[]` is a lie-shaped hole.
	l := restedLoop(t, f, "")
	errRes = dispatchErr(t, f, "c-init", protocol.MethodLoopMessage, map[string]any{
		"id": l.ID, "text": "look", "attachments": []any{},
	})
	if !strings.Contains(errRes, "empty list") {
		t.Fatalf("an empty list passed the door: %q", errRes)
	}
}

func TestAMessageWithAttachmentsFilesReceiptsAndPutsBytesOnlyOnTheWire(t *testing.T) {
	f, m := gatedFixture(t, []gatedTurn{{deltas: []string{turnOK}}})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	rec := s69Put(t, f, "c-init", "shot.png", s69PNG)
	doc := rec["document"].(string)

	l := restedLoop(t, f, "")
	res := dispatchJSON(t, f, "c-init", protocol.MethodLoopMessage, map[string]any{
		"id":          l.ID,
		"text":        "look at this",
		"attachments": []any{map[string]any{"document": doc, "rev": 1}},
	})
	if res["turn"] != "started" {
		t.Fatalf("turn refused: %v", res)
	}
	waitFor(t, "the model call", func() bool { return m.callCount() >= 1 })
	waitTurnIdle(t, f.d, l.ID)

	// The ROW carries the receipt — the store's numbers, and no bytes.
	sc := store.Scope{LoopSlug: "chat", ComponentRoot: f.root}
	rows, err := f.s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	var human *store.ChatMessage
	for i := range rows {
		if rows[i].Body == "look at this" {
			human = &rows[i]
		}
	}
	if human == nil || len(human.Attachments) != 1 {
		t.Fatalf("the receipt did not ride the row: %+v", rows)
	}
	r := human.Attachments[0]
	if r.Document != doc || r.Rev != 1 || r.MediaType != "image/png" || r.Size != len(s69PNG) || r.Name != "shot.png" {
		t.Fatalf("the filed receipt is not the store's answer: %+v", r)
	}

	// THE WIRE: the user message is words-first (B3) with the image block
	// beside them — base64 appears HERE and nowhere earlier.
	msgs := m.lastSent(0)
	last := msgs[len(msgs)-1]
	if string(last.Role) != "user" || len(last.Content) != 2 {
		t.Fatalf("the wire user turn is not words+attachment: %+v", last.Content)
	}
	if last.Content[0].Kind != model.BlockText || last.Content[0].Text != "look at this" {
		t.Fatalf("the words are not first and alone-led: %+v", last.Content[0])
	}
	img := last.Content[1]
	if img.Kind != model.BlockImage || img.MediaType != "image/png" {
		t.Fatalf("the attachment did not become an image block: %+v", img)
	}
	back, _ := base64.StdEncoding.DecodeString(img.Data)
	if string(back) != string(s69PNG) {
		t.Fatalf("the expansion lost bytes: %q", img.Data)
	}
	if img.Document != "" || img.Rev != 0 {
		t.Fatalf("the wire block carries a reference beside its data: %+v", img)
	}
}

func TestAMissingRevisionSpeaksInsteadOfRenderingASilentHole(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	initConn(t, f.d, context.Background(), "c-init", "cli")
	rec := s69Put(t, f, "c-init", "gone.png", s69PNG)
	doc := rec["document"].(string)
	if err := f.s.GormDB().Exec(`DELETE FROM loop_document WHERE file = ?`, doc).Error; err != nil {
		t.Fatal(err)
	}
	// The door would refuse this reference now (AT3) — the drain case is
	// the row written while the revision lived, then drained after it
	// vanished. Expand speaks it (ruling 3).
	blocks, missing := f.d.ExpandAttachments(f.root, "chat", []protocol.AttachmentReceiptView{
		{Name: "gone.png", Document: doc, Rev: 1, MediaType: "image/png"},
	})
	if len(blocks) != 0 {
		t.Fatalf("a vanished revision expanded to something: %+v", blocks)
	}
	if len(missing) != 1 || !strings.Contains(missing[0], doc) || !strings.Contains(missing[0], "revision 1") {
		t.Fatalf("the absence did not name itself: %v", missing)
	}
	// And the steer drain: a row filed with a receipt, drained after loss,
	// rides its words WITH the spoken absence — never silent.
	restedLoop(t, f, "")
	st := seedStatusStep(t, f, "running")
	if _, err := f.s.QueueSteer(st, store.NewSteer{Text: "still worth reading",
		Attachments: []store.AttachmentReceipt{{Name: "gone.png", Document: doc, Rev: 1, MediaType: "image/png"}}}); err != nil {
		t.Fatal(err)
	}
	drained := stepSteers{steps: f.s, stepID: st}.Pending(context.Background())
	if len(drained) != 1 {
		t.Fatalf("the steer did not drain: %v", drained)
	}
	s := drained[0]
	if !strings.Contains(s.Text, "still worth reading") {
		t.Fatalf("the steer's words were dropped for the missing file: %q", s.Text)
	}
	if len(s.Attachments) != 1 || s.Attachments[0].Kind != model.BlockText ||
		!strings.Contains(s.Attachments[0].Text, doc) {
		t.Fatalf("the absence did not ride beside the words: %+v", s.Attachments)
	}
}

func TestMoreThanEightReferencesRefuseNamingTheBound(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	initConn(t, f.d, context.Background(), "c-init", "cli")
	l := restedLoop(t, f, "")
	refs := make([]any, 9)
	for i := range refs {
		refs[i] = map[string]any{"document": "@attachments/00000000-x.png", "rev": 1}
	}
	errRes := dispatchErr(t, f, "c-init", protocol.MethodLoopMessage, map[string]any{
		"id": l.ID, "text": "many", "attachments": refs,
	})
	if !strings.Contains(errRes, "bound of 8") {
		t.Fatalf("the nine-attachment call did not name the bound: %q", errRes)
	}
}
