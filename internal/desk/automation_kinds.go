// automation_kinds.go — S49 T6: the fourteenth proposal kind,
// automation_disable, as a registry entry in this package.
//
// The desk keeps only the pure half, exactly the blueprint_kinds.go and
// environment_kinds.go shape: the payload SHAPE (a strict decode — a
// smuggled "enabled", "paused", "archived", "actor", "status" or "kind" is
// a PARSE refusal, so no payload can name the state it wants; the payload
// names WHO to switch off and WHY, never WHAT the switch should become),
// the closed source family (the automationview's three — schedule, webhook,
// event: the source is part of the automation's name, AN1), the non-blank
// id, and the required reason of 1–256 bytes (AN9's ask-a-person rule: an
// unreasoned disable ask is not an ask a person can decide).
//
// What the desk may NOT do is read the row. The store halves — the
// automation exists under that source, is not archived, and is not already
// off (enabled for an event row, not paused for a schedule or webhook row)
// — arrive as `Seams` from `cmd/aeon`: the desk's dependency rule is stdlib
// + policy + paths, and importing internal/store (the reader) or
// internal/daemon (the write path) would drag both engines into the
// decision code. The seam's Validate REPLACES this file's shape half with
// the full desk-then-store function (the merge rule in desk.New), so the
// create door and the commit door run ONE function (S35 TD3) and the effect
// is performed with the COMMITTING PERSON as actor — the proposer stays
// where it belongs, in the row's created_by_*.
//
// The kind is narrowing-only in AN9's sense, beside AL11's and EV16's: a
// disable can only take authority away — pause the clock, switch the
// dispatcher off. There is deliberately no automation_create,
// automation_update, automation_enable or automation_archive kind: starting
// a loop's rhythm, rewiring one, switching one on, or hiding one (AN6) all
// move by a person alone, through the human-door writes T4 built.

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

// KindAutomationDisable is S49 T6's kind, fourteenth in the store's order
// (migration 20261103193000; the store mirror is store.ProposalKinds).
const KindAutomationDisable Kind = "automation_disable"

// The payload bounds, re-declared (the importer pin — see the file header):
// the source families are automationview's closed three, spelled once here;
// ids are capped at 64 (both families' grammars are shorter — an id is a
// name, not an essay); the reason cap is AN9's ask-shape (1–256: long
// enough for the sentence a person decides on, short enough that the desk
// reads it).
const (
	maxAutomationDisableIdBytes     = 64
	maxAutomationDisableReasonBytes = 256
)

// automationDisableSources is AN1's closed three — the family a row belongs
// to. It is the SAME closed set automationview publishes (and
// internal/protocol/automation_read.go's automationSourceNames); re-declared
// here because the desk imports neither (the importer rule above).
var automationDisableSources = []string{"schedule", "webhook", "event"}

// AutomationDisableProposal is automation_disable's payload — WHO and WHY,
// never WHAT. Source+ID is the automation's name (the source is part of the
// name — an id that lives under the other source is a different automation
// and the store half refuses the mix); reason is the sentence a person reads
// at the desk. There is no target-state field because there is no
// target-state choice: the effect can only disable.
type AutomationDisableProposal struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// validateAutomationDisablePayload is the desk's pure half of Validate (the
// shape). The store half — the automation exists, is not archived, and is
// not already off — is the seam's, and the seam's Validate (which runs this
// function first) is what BOTH doors run.
func validateAutomationDisablePayload(_ context.Context, payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p AutomationDisableProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: automation_disable payload is not {\"source\":\"schedule|webhook|event\", \"id\":…, \"reason\":…}: %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: automation_disable payload carries trailing JSON: %v", err)
	}
	known := false
	for _, s := range automationDisableSources {
		if p.Source == s {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("refused: automation_disable source %q is not one of schedule, webhook, event — the source is part of the automation's name (AN1)", p.Source)
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("refused: automation_disable needs a non-blank id — automation.list shows what exists")
	}
	if len(p.ID) > maxAutomationDisableIdBytes {
		return fmt.Errorf("refused: automation_disable id is longer than %d — an id is a name, not an essay", maxAutomationDisableIdBytes)
	}
	if r := strings.TrimSpace(p.Reason); r == "" {
		return fmt.Errorf("refused: automation_disable needs a reason — an unreasoned disable ask is not an ask a person can decide (AN9)")
	} else if len(p.Reason) > maxAutomationDisableReasonBytes {
		return fmt.Errorf("refused: automation_disable reason is %d bytes and the cap is %d — a sentence for the desk, not a justification essay", len(p.Reason), maxAutomationDisableReasonBytes)
	}
	return nil
}

// ValidateAutomationDisable is the desk half exported for the seam wrapper
// (the seam runs it, then the store halves — one function per door, TD3).
func ValidateAutomationDisable(ctx context.Context, payload []byte) error {
	return validateAutomationDisablePayload(ctx, payload)
}
