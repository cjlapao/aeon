// charter_save.go — S61 T5 (GI4): the eighteenth proposal kind, charter_save,
// as a registry entry in this package.
//
// The desk keeps the pure shape half, exactly the roadmap_kinds.go pattern:
// the payload SHAPE (a strict decode — a smuggled "actor", "author",
// "source", "sourceRef", "revision" or "note" is a PARSE refusal, so no
// payload can name the actor it claims or the source it wants; the revision's
// actor is the COMMITTING PERSON, stamped by the apply at the commit door,
// and the source is 'proposal' because the apply says so and nothing else),
// a non-blank root, a present JSON body object, and the required reason of
// 1–512 bytes shared with every propose door (ProposeReasonSentence).
//
// What the desk may NOT do is validate the charter body or read the
// component. The charter package imports the store (it is the revision
// writer), and this package imports neither — so charter.Validate over the
// decoded body, the root's existence, and the save itself arrive as `Seams`
// from cmd/aeon, replacing this file's shape half with the full
// desk-then-charter function (the merge rule in desk.New), so the create door
// and the commit door run ONE function (S35 TD3). The revision is then
// written by the charter's own Save with the COMMITTING PERSON as actor,
// source 'proposal' and the proposal's id as source_ref — never the
// proposer, and with NO note, because the charter package's law keeps a
// proposal's reason on the proposal row (write.go's note-source rule), which
// is also why this payload carries no note field at all.
//
// The kind adds revisions and nothing else: charter history is append-only,
// so there is deliberately no charter_revert, charter_delete or charter_edit
// kind, and none will be added — the same law S52 set for memories.

package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// KindCharterSave is S61 T5's kind, eighteenth in the store's order
// (migration 20261105110000_proposal_kinds_intake; the store mirror is
// store.ProposalKinds).
const KindCharterSave Kind = "charter_save"

// maxCharterSavePayloadBytes is the desk's own envelope: a charter body is
// capped at 64 KiB once canonical (charter.MaxBodyBytes), and a JSON payload
// under 128 KiB is one a person actually reads at the desk. The cap is the
// desk's (the proposal table carries no length CHECK); the charter's 64 KiB
// belongs to charter.Validate and is the seam's to enforce.
const maxCharterSavePayloadBytes = 128 * 1024

// maxCharterSaveRootBytes bounds the root id — the component registry's
// ids are path-shaped, and 512 is the same bound roadmap_kinds.go put on a
// component root for the same reason.
const maxCharterSaveRootBytes = 512

// CharterSaveProposal is charter_save's payload — WHICH ROOT and WHICH BODY
// and WHY, never WHO saves it or WHERE the revision claims to come from.
// Body arrives raw because the charter package that decodes it is closed to
// this one; the shape half only asks that it be a JSON object.
type CharterSaveProposal struct {
	RootID string          `json:"rootId"`
	Body   json.RawMessage `json:"body"`
	Reason string          `json:"reason"`
}

// validateCharterSavePayload is the desk's pure half of Validate (the
// shape). The charter half (charter.Validate over the decoded body) and the
// store half (the root exists and is a root) are the seam's, and the seam's
// Validate (which runs this function first) is what BOTH doors run.
func validateCharterSavePayload(_ context.Context, payload []byte) error {
	if len(payload) > maxCharterSavePayloadBytes {
		return fmt.Errorf("refused: the charter_save payload is %d bytes and the cap is %d — a charter is a statement, not a document dump",
			len(payload), maxCharterSavePayloadBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p CharterSaveProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: charter_save payload is not {\"rootId\":…, \"body\":…, \"reason\":…} and carries no field besides them: %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: charter_save payload carries trailing JSON: %v", err)
	}
	if strings.TrimSpace(p.RootID) == "" {
		return fmt.Errorf("refused: charter_save needs a rootId — a charter belongs to one component root and nothing else")
	}
	if len(p.RootID) > maxCharterSaveRootBytes {
		return fmt.Errorf("refused: charter_save rootId is %d bytes and the cap is %d", len(p.RootID), maxCharterSaveRootBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(p.Body, &obj); err != nil || obj == nil {
		head := string(p.Body)
		if len(head) > 80 {
			head = head[:80] + "…"
		}
		return fmt.Errorf("refused: charter_save body must be a JSON object — the charter fields are the shape, and %q is not it", head)
	}
	if s := ProposeReasonSentence(p.Reason, "charter_save"); s != "" {
		return errors.New(s)
	}
	return nil
}

// ValidateCharterSave is the exported entry the cmd/aeon seam calls — the
// same function the built-in registry entry runs, so the seam's full half
// starts from these exact sentences (TD3's one-validator rule).
func ValidateCharterSave(ctx context.Context, payload []byte) error {
	return validateCharterSavePayload(ctx, payload)
}
