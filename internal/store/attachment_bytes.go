package store

// attachment_bytes.go — S69: bytes once, refs forever (the byte door; the goals-attachment pins live in attachments.go).
//
// An attachment is a loop_document revision under the reserved
// `@attachments/` namespace. The documents mechanism already stores bytes
// (gzip-compressed BLOB), verifies sha256 on read, and pins revisions that
// never move — which is exactly the "store once, render from storage" the
// provider investigation decided on (spec S69 AT1). So there is no second
// byte table: an attachment PUT writes a document revision, and everything
// downstream carries REFERENCES ({document, rev}) plus a receipt the door
// files on the row. The transcript never sees bytes (AT5); the daemon's
// send-time expansion is what turns a pinned revision back into base64, and
// because revisions are immutable, live and replay expand identically (D17
// of the record — byte-identity comes from the store, not from discipline).
//
// The mediaType has one honest source: the bytes. Put declares it, the door
// sniffs and refuses a lie ("the .png whose first bytes are MZ"), and a read
// returns the sniff — the declared subtype survives on the row's receipt for
// surfaces to render, because a row is a statement made at a moment, while a
// read is a statement about the bytes as they are.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Reserved namespace + the caps (S69 AT3/AT4). MaxDocumentBytes (2 MiB) is
// the DOCUMENT cap; an attachment has its own — the Anthropic image cap is
// the binding one across the providers investigated, and the 50 MiB OpenAI
// file number is deliberately not adopted.
const (
	AttachmentPrefix   = "@attachments/"
	MaxAttachmentBytes = 10 << 20
)

// AttachmentReceipt is the row's statement about one attachment. It rides
// chat_message.attachments and step_steer.attachments as a JSON array —
// never bytes, never base64 (AT2).
type AttachmentReceipt struct {
	Name      string `json:"name"`
	Document  string `json:"document"`
	Rev       int    `json:"rev"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
}

// SniffMediaType reports what the bytes ARE: one of the four provider-safe
// image types, application/pdf, text/plain for decodable non-binary content,
// or "" when nothing on the allow-list could honestly be said.
func SniffMediaType(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 5 && string(data[:5]) == "%PDF-":
		return "application/pdf"
	}
	// Text is the class, not a claim: no NUL bytes and valid UTF-8 is what
	// can honestly be said; the declared subtype (text/markdown, text/x-go)
	// belongs to the row's receipt, not to the bytes' verdict.
	if !utf8.Valid(data) {
		return ""
	}
	for _, b := range data {
		if b == 0 {
			return ""
		}
	}
	return "text/plain"
}

// attachmentNameSafe reduces a client-supplied name to one flat segment:
// no directory (Windows separators included), no control characters, at most
// 80 visible characters. It never invents — a name with nothing legal left
// becomes "attachment", and the sha8 prefix keeps the document distinct
// regardless.
func attachmentNameSafe(raw string) string {
	unified := strings.ReplaceAll(raw, `\`, "/")
	if i := strings.LastIndex(unified, "/"); i >= 0 {
		unified = unified[i+1:]
	}
	var b strings.Builder
	prevDash := false
	for _, r := range unified {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == '_' || r == ' '
		if !ok {
			r = '-'
		}
		if r == '-' && prevDash {
			continue
		}
		prevDash = r == '-'
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), "-. ")
	if out == "" {
		out = "attachment"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

// PutAttachment is the ONLY door bytes cross into the store (AT3). It
// validates at this door — cap, allow-list, and the magic bytes against the
// declared mediaType — writes (or dedupes to) a frozen document revision,
// and answers the receipt. A refusal leaves NOTHING behind: no revision, no
// partial row (the whole write is one tx that never opens for a refused
// file).
//
// The per-message totals (≤8, ≤40 MiB) are NOT checked here: a client may
// put more files than one message uses. The count belongs to the message
// door, where a turn is actually composed (AT4).
func (s *Store) PutAttachment(sc Scope, name, declaredMediaType string, data []byte) (AttachmentReceipt, error) {
	if !sc.Valid() {
		return AttachmentReceipt{}, fmt.Errorf("store: PutAttachment needs both a slug and a component root, got %s", sc)
	}
	if len(data) == 0 {
		return AttachmentReceipt{}, fmt.Errorf("store: attachment %q is empty; an empty file could render as nothing and say so even less", attachmentNameSafe(name))
	}
	if len(data) > MaxAttachmentBytes {
		return AttachmentReceipt{}, fmt.Errorf("store: attachment %q is %d bytes; the limit is %d", attachmentNameSafe(name), len(data), MaxAttachmentBytes)
	}
	declared := strings.ToLower(strings.TrimSpace(declaredMediaType))
	if !attachmentMediaAllowed(declared) {
		return AttachmentReceipt{}, fmt.Errorf("store: attachment %q declares mediaType %q, which is not on the allow-list (image/png, image/jpeg, image/gif, image/webp, application/pdf, text/*)", attachmentNameSafe(name), declaredMediaType)
	}
	sniffed := SniffMediaType(data)
	if sniffed == "" {
		return AttachmentReceipt{}, fmt.Errorf("store: attachment %q (%s) is not bytes any allowed type could honestly describe", attachmentNameSafe(name), declared)
	}
	if !mediaTypesAgree(declared, sniffed) {
		return AttachmentReceipt{}, fmt.Errorf("store: attachment %q declares %s but its magic bytes say %s; the declared type must agree with the content", attachmentNameSafe(name), declared, sniffed)
	}

	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	doc := AttachmentPrefix + digest[:8] + "-" + attachmentNameSafe(name)

	tx, err := s.w.Begin()
	if err != nil {
		return AttachmentReceipt{}, fmt.Errorf("store: put attachment %q in %s: %w", doc, sc, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Store-once, taken literally: the same bytes at the same document name
	// answer the existing revision instead of writing a duplicate.
	var headRev int
	var headSHA string
	err = tx.QueryRow(`SELECT rev, sha256 FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ?
		ORDER BY rev DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug, doc).Scan(&headRev, &headSHA)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AttachmentReceipt{}, fmt.Errorf("store: read the head of %s in %s: %w", doc, sc, err)
	}
	if headSHA == digest {
		if err := tx.Commit(); err != nil {
			return AttachmentReceipt{}, fmt.Errorf("store: put attachment %s: %w", doc, err)
		}
		return AttachmentReceipt{Name: attachmentNameSafe(name), Document: doc, Rev: headRev, MediaType: declared, Size: len(data), SHA256: digest}, nil
	}

	written, err := s.insertDocumentRevision(tx, sc, doc, string(data), "attachment")
	if err != nil {
		return AttachmentReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return AttachmentReceipt{}, fmt.Errorf("store: put attachment %s: %w", doc, err)
	}
	return AttachmentReceipt{Name: attachmentNameSafe(name), Document: written.File, Rev: written.Rev, MediaType: declared, Size: written.Bytes, SHA256: written.SHA256}, nil
}

func attachmentMediaAllowed(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf":
		return true
	}
	return strings.HasPrefix(m, "text/")
}

// mediaTypesAgree is the magic-byte verdict: an exact type must match the
// sniff word for word; any declared text subtype agrees with the text class.
func mediaTypesAgree(declared, sniffed string) bool {
	if strings.HasPrefix(declared, "text/") {
		return sniffed == "text/plain"
	}
	return declared == sniffed
}

// ReadAttachment reads one PINNED revision — never the head (a receipt that
// drifted under its own row would be the second truth). The bytes are
// returned with the sniffed mediaType: the bytes' own verdict (see the file
// comment). The document must live under the reserved namespace; anything
// else is not an attachment and this door does not read it.
func (s *Store) ReadAttachment(sc Scope, document string, rev int) (Document, string, error) {
	if !sc.Valid() {
		return Document{}, "", fmt.Errorf("store: ReadAttachment needs both a slug and a component root, got %s", sc)
	}
	if !strings.HasPrefix(document, AttachmentPrefix) {
		return Document{}, "", fmt.Errorf("store: %q is not an attachment — attachments live under %s and this door reads nothing else (documents have their own door)", document, AttachmentPrefix)
	}
	if rev <= 0 {
		return Document{}, "", fmt.Errorf("store: attachment %q was asked for at revision %d; revisions start at 1", document, rev)
	}
	var doc Document
	var compressed []byte
	err := s.r.QueryRow(`
		SELECT id, component_root, loop_slug, file, rev, content, bytes, sha256, COALESCE(written_by,''), written_at
		FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ? AND rev = ?`,
		sc.ComponentRoot, sc.LoopSlug, document, rev).
		Scan(&doc.ID, &doc.ComponentRoot, &doc.LoopSlug, &doc.File, &doc.Rev, &compressed,
			&doc.Bytes, &doc.SHA256, &doc.WrittenBy, &doc.WrittenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, "", fmt.Errorf("store: %s has no %s at revision %d: %w", sc, document, rev, ErrNotFound)
	}
	if err != nil {
		return Document{}, "", fmt.Errorf("store: read attachment %s rev %d in %s: %w", document, rev, sc, err)
	}
	plain, err := ungzipBytes(compressed)
	if err != nil {
		return Document{}, "", fmt.Errorf("store: decompress attachment %s in %s: %w", document, sc, err)
	}
	// The same corruption honesty ReadDocument practices: a row that does not
	// match its own digest is corruption, and reporting it here is the only
	// place anybody would notice.
	if got := sha256.Sum256(plain); hex.EncodeToString(got[:]) != doc.SHA256 {
		return Document{}, "", fmt.Errorf("store: attachment %s rev %d in %s does not match its stored sha256; the row is corrupt", document, rev, sc)
	}
	doc.Content = string(plain)
	return doc, SniffMediaType(plain), nil
}

// ValidateAttachmentReceipts is the message door's check (AT3): the shape a
// row may carry. The door resolves each reference against the store; this
// only refuses the shapes that could never render — empty name, missing
// document, rev below one, a document outside the reserved namespace, a
// blank mediaType. A refusal names every offender: one per round-trip is an
// interrogation, not a door.
func ValidateAttachmentReceipts(rs []AttachmentReceipt) error {
	var offenders []string
	for _, r := range rs {
		switch {
		case r.Name == "":
			offenders = append(offenders, "an attachment with no name")
		case r.Document == "" || !strings.HasPrefix(r.Document, AttachmentPrefix):
			offenders = append(offenders, fmt.Sprintf("%s references %q, which is not an attachment document (attachments live under %s)", r.Name, r.Document, AttachmentPrefix))
		case r.Rev <= 0:
			offenders = append(offenders, fmt.Sprintf("%s references revision %d of %s; revisions start at 1", r.Name, r.Rev, r.Document))
		case r.MediaType == "":
			offenders = append(offenders, fmt.Sprintf("%s has no mediaType", r.Name))
		}
	}
	if len(offenders) > 0 {
		return fmt.Errorf("store: these attachments would never render: %s", strings.Join(offenders, "; "))
	}
	return nil
}
