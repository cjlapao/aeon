package daemon

// attachments.go — the daemon half of the attachment doors (S69 AT3/AT4).
// The protocol half refuses cheap lies; this half owns the store and turns
// its verdicts into named errors: a cap blown, a magic byte that contradicts
// a claim, a reference to a revision that was never put — each refusal names
// the file and the fact. Receipts are answered from the STORE's numbers, so
// a row records what the bytes are, not what a client said they were.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func (m *Memory) PutAttachment(p protocol.AttachmentPutParams) (protocol.AttachmentPutResult, error) {
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return protocol.AttachmentPutResult{}, protocol.ErrBadParams{Err: err}
	}
	rec, err := m.store.PutAttachment(store.Scope{LoopSlug: p.Slug, ComponentRoot: p.ComponentRoot}, p.Name, p.MediaType, data)
	if err != nil {
		// The store's sentences name the file, the byte count and the limit
		// (or the claim versus the sniff); wrapping into ErrProviderFailed
		// would hide the refusal inside a retryable code — a lying .png is
		// not a disk that will clear. ErrBadParams carries the sentence on.
		return protocol.AttachmentPutResult{}, protocol.ErrBadParams{Err: err}
	}
	return protocol.AttachmentPutResult{Receipt: attachmentView(rec)}, nil
}

// ReadAttachmentForView is the rendering door: bytes come back base64 for a
// surface that draws them (the bubble's image preview), never to ride a
// later message. A missing revision answers ErrNotFound naming document and
// revision — the caller speaks the absence, it does not invent a blank
// (ruling 3; P31's missing-is-not-zero).
func (m *Memory) ReadAttachmentForView(p protocol.AttachmentReadParams) (protocol.AttachmentReadResult, error) {
	doc, sniff, err := m.store.ReadAttachment(store.Scope{LoopSlug: p.Slug, ComponentRoot: p.ComponentRoot}, p.Document, p.Rev)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The absence is NAMED — document and revision — so the caller
			// speaks it instead of inventing a blank (ruling 3, P31).
			return protocol.AttachmentReadResult{}, protocol.ErrBadParams{Err: err}
		}
		return protocol.AttachmentReadResult{}, protocol.ErrProviderFailed.WithWhat("%v", err)
	}
	return protocol.AttachmentReadResult{
		AttachmentReceiptView: protocol.AttachmentReceiptView{
			Name: doc.File, Document: doc.File, Rev: doc.Rev,
			MediaType: sniff, Size: doc.Bytes, SHA256: doc.SHA256,
		},
		Data: base64.StdEncoding.EncodeToString([]byte(doc.Content)),
	}, nil
}

// storeAttachmentResolver is the ONE store-backed implementation of the
// door resolver: every reference check — the message door's, the steer
// door's, Memory's — reads through the same pinned door, so there is one
// answer to "does this revision exist" everywhere.
type storeAttachmentResolver struct{ st *store.Store }

func (r storeAttachmentResolver) ResolveAttachment(slug, componentRoot, document string, rev int) (protocol.AttachmentReceiptView, error) {
	doc, sniff, err := r.st.ReadAttachment(store.Scope{LoopSlug: slug, ComponentRoot: componentRoot}, document, rev)
	if err != nil {
		return protocol.AttachmentReceiptView{}, err
	}
	name := doc.File
	if i := lastSlash(name); i >= 0 {
		name = name[i+1:]
	}
	if j := indexDashAfterSha(name); j > 0 {
		name = name[j+1:]
	}
	return protocol.AttachmentReceiptView{
		Name: name, Document: doc.File, Rev: doc.Rev,
		MediaType: sniff, Size: doc.Bytes, SHA256: doc.SHA256,
	}, nil
}

// ExpandAttachments is the daemon half of the send-time expansion (S69
// AT5): pinned references become base64 content blocks at the one join
// point beside request compilation. A revision the store no longer has is
// returned as its own spoken sentence — the caller puts the absence where
// it belongs (the step's notes) — never a silent skip.
func (d *Daemon) ExpandAttachments(componentRoot, loopSlug string, refs []protocol.AttachmentReceiptView) ([]model.Block, []string) {
	sc := store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot}
	var blocks []model.Block
	var missing []string
	for _, r := range refs {
		doc, sniff, err := d.store.ReadAttachment(sc, r.Document, r.Rev)
		if err != nil {
			missing = append(missing, fmt.Sprintf(
				"the attachment %s at revision %d is no longer in this loop's store: %v — the turn runs with its words and the attachments that remain",
				r.Document, r.Rev, err))
			continue
		}
		data := base64.StdEncoding.EncodeToString([]byte(doc.Content))
		if strings.HasPrefix(sniff, "image/") {
			blocks = append(blocks, model.ImageDataBlock(r.Name, sniff, data))
		} else {
			blocks = append(blocks, model.DocumentDataBlock(r.Name, sniff, data))
		}
	}
	return blocks, missing
}

// ResolveAttachment is what the message and steer doors consult BEFORE
// filing a row (AT3): the reference must exist HERE, pinned, now — and the
// receipt that gets filed is this answer, not the caller's claim. The
// display name is the store's sanitised tail of the document name, so a
// client never files its own spelling of a file it does not own.
func (m *Memory) ResolveAttachment(slug, componentRoot, document string, rev int) (protocol.AttachmentReceiptView, error) {
	doc, sniff, err := m.store.ReadAttachment(store.Scope{LoopSlug: slug, ComponentRoot: componentRoot}, document, rev)
	if err != nil {
		return protocol.AttachmentReceiptView{}, err
	}
	name := doc.File
	if i := lastSlash(name); i >= 0 {
		name = name[i+1:]
	}
	// The document name is <sha8>-<safe> under the namespace: the tail after
	// the first dash is what the person called the file.
	if j := indexDashAfterSha(name); j > 0 {
		name = name[j+1:]
	}
	return protocol.AttachmentReceiptView{
		Name: name, Document: doc.File, Rev: doc.Rev,
		MediaType: sniff, Size: doc.Bytes, SHA256: doc.SHA256,
	}, nil
}

func attachmentView(r store.AttachmentReceipt) protocol.AttachmentReceiptView {
	return protocol.AttachmentReceiptView{
		Name: r.Name, Document: r.Document, Rev: r.Rev,
		MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
	}
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// indexDashAfterSha finds the dash after the 8-hex digest — the boundary
// between the identity and the person's name for the file.
func indexDashAfterSha(name string) int {
	const hexDigits = "0123456789abcdef"
	if len(name) <= 9 || name[8] != '-' {
		return -1
	}
	for i := 0; i < 8; i++ {
		if !stringsContains(hexDigits, name[i]) {
			return -1
		}
	}
	return 8
}

func stringsContains(haystack string, b byte) bool {
	for i := 0; i < len(haystack); i++ {
		if haystack[i] == b {
			return true
		}
	}
	return false
}
