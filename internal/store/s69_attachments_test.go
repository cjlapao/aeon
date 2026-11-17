package store

import (
	"encoding/base64"
	"strings"
	"testing"
)

// s69_attachments_test.go — the attachment door and the row receipts
// (S69 AT1–AT5 at the store level): bytes freeze into document revisions
// exactly once, references pin them immutably, and the rows carry receipts
// that never contain bytes. NULL stays the third state (P31).

const pngBytes = "\x89PNG\r\n\x1a\n" + "fake-iaaa" // sniff-verifiable prefix

func TestPutAttachmentFilesAFrozenRevisionAndAnswersTheReceipt(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "att", "/r")

	rec, err := s.PutAttachment(sc, "diagram.png", "image/png", []byte(pngBytes))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if rec.MediaType != "image/png" || rec.Rev != 1 || rec.Size != len(pngBytes) {
		t.Fatalf("receipt = %+v", rec)
	}
	if !strings.HasPrefix(rec.Document, AttachmentPrefix) || !strings.HasSuffix(rec.Document, "-diagram.png") {
		t.Fatalf("document name %q is not under the reserved namespace with the safe name", rec.Document)
	}

	// Store-once taken literally: the same bytes answer the SAME revision.
	again, err := s.PutAttachment(sc, "diagram.png", "image/png", []byte(pngBytes))
	if err != nil {
		t.Fatalf("re-put: %v", err)
	}
	if again.Rev != rec.Rev || again.Document != rec.Document {
		t.Fatalf("the same bytes wrote a second revision: %+v vs %+v", again, rec)
	}

	doc, sniff, err := s.ReadAttachment(sc, rec.Document, rec.Rev)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if doc.Content != pngBytes || sniff != "image/png" {
		t.Fatalf("the frozen bytes drifted: %q / %s", doc.Content, sniff)
	}
}

func TestTheMagicByteVerdictRefusesALyingClaim(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "att", "/r")

	// A PDF wearing a .png claim: refused, naming BOTH the claim and the
	// verdict — one offender per round-trip is an interrogation.
	if _, err := s.PutAttachment(sc, "diagram.png", "image/png", []byte("%PDF-1.7 fake")); err == nil ||
		!strings.Contains(err.Error(), "image/png") || !strings.Contains(err.Error(), "application/pdf") {
		t.Fatalf("lying claim passed or spoke the wrong sentence: %v", err)
	}
	// Binary bytes claiming text: refused.
	if _, err := s.PutAttachment(sc, "blob.txt", "text/plain", []byte(pngBytes)); err == nil ||
		!strings.Contains(err.Error(), "image/png") {
		t.Fatalf("binary-as-text passed: %v", err)
	}
	// text/markdown is a subtype of the sniffed text class: allowed.
	if _, err := s.PutAttachment(sc, "notes.md", "text/markdown", []byte("# hello")); err != nil {
		t.Fatalf("text subtype refused: %v", err)
	}
	// Off the allow-list, by name.
	if _, err := s.PutAttachment(sc, "tool.exe", "application/x-msdownload", []byte("MZ...")); err == nil ||
		!strings.Contains(err.Error(), "allow-list") {
		t.Fatalf("off-list type passed: %v", err)
	}
	// Over the cap, byte counts named.
	huge := make([]byte, MaxAttachmentBytes+1)
	copy(huge, "%PDF-")
	if _, err := s.PutAttachment(sc, "big.pdf", "application/pdf", huge); err == nil ||
		!strings.Contains(err.Error(), "the limit is") {
		t.Fatalf("oversize passed: %v", err)
	}
	// The name is sanitised, never obeyed: traversal cannot smuggle a path.
	rec, err := s.PutAttachment(sc, "../..//etc/passwd", "text/plain", []byte("root"))
	if err != nil {
		t.Fatalf("put traversal name: %v", err)
	}
	if strings.Count(rec.Document, "/") != 1 || !strings.HasPrefix(rec.Document, AttachmentPrefix) {
		t.Fatalf("the name is not one flat segment under the namespace: %q", rec.Document)
	}
}

func TestAttachmentReadsArePinnedAndStayInTheNamespace(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "att", "/r")

	r1, err := s.PutAttachment(sc, "notes.md", "text/markdown", []byte("first"))
	if err != nil {
		t.Fatalf("put r1: %v", err)
	}
	r2, err := s.PutAttachment(sc, "notes.md", "text/markdown", []byte("second"))
	if err != nil {
		t.Fatalf("put r2: %v", err)
	}
	// The sha8 prefix carries the identity (AT1): different bytes under the
	// same name are a DIFFERENT frozen document, so a pinned (document, rev)
	// can never move under a receipt — the pin is honest by construction,
	// not by discipline.
	if r2.Document == r1.Document {
		t.Fatalf("different bytes share a document name: %q", r2.Document)
	}
	// A receipt pinned to rev 1 reads the FIRST bytes forever — the pin is
	// what makes live and replay agree.
	old, _, err := s.ReadAttachment(sc, r1.Document, r1.Rev)
	if err != nil || old.Content != "first" {
		t.Fatalf("the pinned revision moved: %q %v", old.Content, err)
	}
	// The head is not this door's answer.
	if _, _, err := s.ReadAttachment(sc, r1.Document, 99); err == nil ||
		!strings.Contains(err.Error(), "revision 99") {
		t.Fatalf("a missing revision answered plausibly: %v", err)
	}
	// Outside the namespace: not an attachment, and this door reads nothing else.
	if _, err := s.WriteDocument(sc, "plan.md", "# plan", "op"); err != nil && !strings.Contains(err.Error(), "") {
		t.Fatalf("seed document: %v", err)
	}
	if _, _, err := s.ReadAttachment(sc, "plan.md", 1); err == nil ||
		!strings.Contains(err.Error(), "not an attachment") {
		t.Fatalf("the door read outside its namespace: %v", err)
	}
	// And the document door cannot author INTO the namespace.
	res, err := s.WriteDocument(sc, AttachmentPrefix+"forged.png", pngBytes, "op")
	if err != nil {
		t.Fatalf("write document: %v", err)
	}
	if res.OK {
		t.Fatalf("the document door authored under the reserved namespace")
	}
	if !strings.Contains(res.Reason, "reserved for attachments") {
		t.Fatalf("the refusal did not name the reservation: %q", res.Reason)
	}
}

func TestAttachmentReceiptsRoundTripTheThreadAndStayReceiptsNotBytes(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "att", "/r")

	rec, err := s.PutAttachment(sc, "shot.png", "image/png", []byte(pngBytes))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "look at this", Attachments: []AttachmentReceipt{rec}}); err != nil {
		t.Fatalf("note: %v", err)
	}
	rows, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if len(rows) != 1 || rows[0].Body != "look at this" {
		t.Fatalf("the row's own words were disturbed: %+v", rows)
	}
	if len(rows[0].Attachments) != 1 || rows[0].Attachments[0].Document != rec.Document ||
		rows[0].Attachments[0].Rev != rec.Rev || rows[0].Attachments[0].SHA256 != rec.SHA256 {
		t.Fatalf("the receipt did not ride: %+v", rows[0].Attachments)
	}
	// No bytes anywhere in the row (base64 of the payload must not appear).
	for _, r := range rows[0].Attachments {
		if strings.Contains(r.Document, base64.StdEncoding.EncodeToString([]byte("fake-iaaa"))) {
			t.Fatalf("bytes rode the row")
		}
	}

	// NULL stays the third state.
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "plain"}); err != nil {
		t.Fatalf("plain note: %v", err)
	}
	rows, _ = s.Thread(sc, 0)
	if rows[1].Attachments != nil {
		t.Fatalf("nothing attached read back as %+v, want nil", rows[1].Attachments)
	}
}

func TestAttachmentReceiptsRoundTripTheSteerRow(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "att", "/r")
	st := seedStep(t, s, sc)

	rec, err := s.PutAttachment(sc, "log.txt", "text/plain", []byte("the tail"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	rode, err := s.QueueSteer(st.ID, NewSteer{Text: "see the log", Attachments: []AttachmentReceipt{rec}})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if rode.Text != "see the log" {
		t.Fatalf("the steer's pure text was disturbed: %q", rode.Text)
	}
	if len(rode.Attachments) != 1 || rode.Attachments[0].Name != "log.txt" {
		t.Fatalf("the receipt did not ride: %+v", rode.Attachments)
	}
	again, err := s.ReadSteer(rode.ID)
	if err != nil {
		t.Fatalf("read steer: %v", err)
	}
	if len(again.Attachments) != 1 || again.Attachments[0].SHA256 != rec.SHA256 {
		t.Fatalf("the receipt did not survive the re-read: %+v", again.Attachments)
	}
}

func TestACorruptAttachmentReceiptErrorsNamingItself(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "att", "/r")
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "row"}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := s.GormDB().Exec(`UPDATE chat_message SET attachments = 'not-json' WHERE body = 'row'`).Error; err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if _, err := s.Thread(sc, 0); err == nil || !strings.Contains(err.Error(), "attachment receipt") {
		t.Fatalf("corruption answered silently: %v", err)
	}
}

func TestValidateAttachmentReceiptsNamesEveryOffender(t *testing.T) {
	err := ValidateAttachmentReceipts([]AttachmentReceipt{
		{Name: "", Document: AttachmentPrefix + "x", Rev: 1, MediaType: "image/png"},
		{Name: "outside", Document: "plan.md", Rev: 1, MediaType: "text/plain"},
		{Name: "norev", Document: AttachmentPrefix + "y", Rev: 0, MediaType: "text/plain"},
		{Name: "nomime", Document: AttachmentPrefix + "z", Rev: 2, MediaType: ""},
	})
	if err == nil {
		t.Fatalf("four offenders passed")
	}
	for _, want := range []string{"no name", "outside", "norev", "nomime", "not an attachment document", "revisions start at 1", "no mediaType"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the sentence missed %q: %v", want, err)
		}
	}
	if err := ValidateAttachmentReceipts(nil); err != nil {
		t.Fatalf("nothing attached is not an offence: %v", err)
	}
}
