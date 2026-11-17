package protocol

// attachments.go — the attachment doors (S69 AT3/AT4). Bytes cross the
// socket exactly ONCE, at attachment.put, and every later artefact — the
// thread row, the steer row, the events, the bubble — carries RECEIPT
// REFERENCES into the frozen @attachments/ revisions. A read brings bytes
// back only for a surface that renders them (the bubble's preview); the
// model's wire is built by the daemon's own expansion, not by this door.
//
// The door files receipts from what the STORE says — ReadAttachment's size,
// sha and sniffed mediaType — never from the caller's claims. A client
// asserting "this is 5 KB of png" is asserting nothing; the bytes decide
// (AT3), and the row records the bytes' answer, not the client's.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// AttachmentRef is a reference to one frozen attachment revision as a
// CLIENT states it: only the two facts a client can legitimately know —
// which document, which revision. Every other field on a filed receipt is
// resolved by the door from the store. The message and steer doors refuse an
// EMPTY attachments array: `[]` is a statement that somebody tried to
// attach and stopped, and the third state (absent) says that honestly.
type AttachmentRef struct {
	Document string `json:"document"`
	Rev      int    `json:"rev"`
}

// AttachmentReceiptView is a filed receipt: the row's statement, all fields
// resolved at the door from the store.
type AttachmentReceiptView struct {
	Name      string `json:"name"`
	Document  string `json:"document"`
	Rev       int    `json:"rev"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
}

// AttachmentPutParams writes bytes once. Data is base64 — the only place on
// this surface bytes ever ride — and the caps (per-attachment 10 MiB) and
// the magic-byte verdict are the store door's verdicts, surfaced as
// ErrBadParams naming the file.
type AttachmentPutParams struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	Name          string `json:"name"`
	MediaType     string `json:"mediaType"`
	Data          string `json:"data"`
}

type AttachmentPutResult struct {
	Receipt AttachmentReceiptView `json:"receipt"`
}

// AttachmentReadParams reads one PINNED revision back — for rendering, not
// for re-sending: nothing that read bytes through this door may put them on
// a later message (the transcript is refs all the way down, AT5).
type AttachmentReadParams struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	Document      string `json:"document"`
	Rev           int    `json:"rev"`
}

type AttachmentReadResult struct {
	AttachmentReceiptView
	// Data is base64 of the stored bytes, present only on this door.
	Data string `json:"data,omitempty"`
}

func (t *Table) handleAttachmentPut(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AttachmentPutParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a document lives in a loop: slug and componentRoot are required")}
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.MediaType) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name and mediaType are required — an unnamed, untyped file could not be rendered or honestly checked")}
	}
	if p.Data == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("%s carries no bytes; an empty attachment could render as nothing and say so even less", p.Name)}
	}
	if _, err := base64.StdEncoding.DecodeString(p.Data); err != nil {
		return nil, ErrBadParams{Err: fmt.Errorf("%s is not valid base64: %v", p.Name, err)}
	}
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	return mem.PutAttachment(p)
}

func (t *Table) handleAttachmentRead(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AttachmentReadParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a document lives in a loop: slug and componentRoot are required")}
	}
	if !strings.HasPrefix(p.Document, "@attachments/") {
		return nil, ErrBadParams{Err: fmt.Errorf("%q is not an attachment — attachments live under @attachments/ and this door reads nothing else", p.Document)}
	}
	if p.Rev <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("attachment %q was asked for at revision %d; revisions start at 1", p.Document, p.Rev)}
	}
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	return mem.ReadAttachmentForView(p)
}

// resolveAttachmentReceipts is the MESSAGE door's work (AT3/AT4): every
// reference is checked against the store BEFORE the row is written — a
// message claiming a file the store has never seen is refused, naming every
// offender at once (one per round-trip is an interrogation) — and the filed
// receipts are what the store says, not what the caller claimed. An empty
// (non-nil) slice is refused: absent is honest, `[]` is a lie-shaped hole.
//
// The caps are the message's, not the file's: at most 8 references and at
// most 40 MiB of STORED bytes total, resolved from the store's sizes.
const (
	maxMessageAttachments   = 8
	maxMessageAttachmentSum = 40 << 20
)

type AttachmentResolver interface {
	ResolveAttachment(slug, componentRoot, document string, rev int) (AttachmentReceiptView, error)
}

func ResolveAttachmentRefs(res AttachmentResolver, slug, componentRoot string, refs []AttachmentRef) ([]AttachmentReceiptView, error) {
	if refs == nil {
		return nil, nil
	}
	if len(refs) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("attachments cannot be an empty list: send the field absent for nothing attached, or name the files that ride")}
	}
	if len(refs) > maxMessageAttachments {
		return nil, ErrBadParams{Err: fmt.Errorf("%d attachments is over the bound of %d a single turn may carry", len(refs), maxMessageAttachments)}
	}
	var receipts []AttachmentReceiptView
	var sum int
	var offenders []string
	for _, r := range refs {
		rec, err := res.ResolveAttachment(slug, componentRoot, r.Document, r.Rev)
		if err != nil {
			offenders = append(offenders, fmt.Sprintf("%s rev %d is not a file this loop has put (%v)", r.Document, r.Rev, err))
			continue
		}
		sum += rec.Size
		receipts = append(receipts, rec)
	}
	if len(offenders) > 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("these attachments were never put where this message lands: %s", strings.Join(offenders, "; "))}
	}
	if sum > maxMessageAttachmentSum {
		return nil, ErrBadParams{Err: fmt.Errorf("these attachments total %d bytes; a single turn may carry at most %d", sum, maxMessageAttachmentSum)}
	}
	return receipts, nil
}
