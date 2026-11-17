package model

import (
	"encoding/json"
	"fmt"
)

// # The guard on a STORED schema
//
// A transcript is written when a step suspends or crashes and read back later —
// possibly after a release, possibly against a different provider (S7, S18). So
// these rules stand between a persisted transcript and a provider 400 that says
// nothing about which message was wrong.

// ValidationResult is a result, NEVER an error.
//
// This is called on data read from a database. The caller must be able to tell
// "this stored transcript is unusable" from "the database failed" — and an
// `error` conflates them, so a recovery path would retry a corrupt transcript
// forever or abandon a healthy one.
type ValidationResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	// Index is the offending message, or -1 when the problem is the transcript
	// as a whole. Naming the index is the difference between a fixable report
	// and "invalid transcript".
	Index int `json:"index"`
}

func valid() ValidationResult { return ValidationResult{OK: true, Index: -1} }

func invalid(index int, format string, args ...any) ValidationResult {
	return ValidationResult{OK: false, Reason: fmt.Sprintf(format, args...), Index: index}
}

// ValidateTranscript checks a transcript against the rules a provider enforces,
// before the wire rather than after a 400.
//
// # The rule that reads like a bug and is not
//
// **A suspended transcript is VALID.** It ends on an assistant turn containing
// a `tool_use` with no matching `tool_result` — which is exactly what a pending
// question looks like (S6), and the normal resting state of a waiting loop. A
// validator requiring every `tool_use` to be answered would reject every
// suspended step.
//
// So the rule is: every `tool_use` must be answered **unless it is in the final
// assistant turn**. A future contributor will read that as an oversight; the
// test for it says why it is not.
func ValidateTranscript(msgs []Message) ValidationResult {
	if len(msgs) == 0 {
		return invalid(-1, "the transcript is empty")
	}

	// Anthropic never accepts a conversation opening with an assistant turn.
	if msgs[0].Role == RoleAssistant {
		return invalid(0, "the transcript begins with an assistant message; a conversation "+
			"starts with system or user")
	}

	toolUseAt := map[string]int{} // tool_use id -> message index
	answered := map[string]bool{}
	lastAssistant := -1
	for i, m := range msgs {
		if m.Role == RoleAssistant {
			lastAssistant = i
		}
	}

	for i, m := range msgs {
		switch m.Role {
		case RoleSystem, RoleUser, RoleAssistant:
		default:
			return invalid(i, "message %d has unknown role %q", i, m.Role)
		}

		if len(m.Content) == 0 {
			// An empty message is accepted by nobody and means nothing. It is
			// also what a buggy accumulator produces, so catching it here names
			// the index instead of surfacing a provider error.
			return invalid(i, "message %d (%s) has no content", i, m.Role)
		}

		// Two consecutive assistant messages: Index is the SECOND one, which is
		// the one to remove or merge.
		if i > 0 && m.Role == RoleAssistant && msgs[i-1].Role == RoleAssistant {
			return invalid(i, "messages %d and %d are both assistant turns; no provider accepts "+
				"two in a row", i-1, i)
		}

		for j, b := range m.Content {
			if err := b.Validate(); err != nil {
				return invalid(i, "message %d block %d: %v", i, j, err)
			}
			switch b.Kind {
			case BlockToolUse:
				if prev, dup := toolUseAt[b.ID]; dup {
					return invalid(i, "tool_use id %q appears twice, at messages %d and %d; a "+
						"tool result could match either", b.ID, prev, i)
				}
				toolUseAt[b.ID] = i
			case BlockToolResult:
				at, exists := toolUseAt[b.ToolUseID]
				if !exists {
					return invalid(i, "message %d has a tool_result for %q, which no earlier "+
						"tool_use declared", i, b.ToolUseID)
				}
				if at > i {
					return invalid(i, "message %d answers a tool_use that appears later, at %d",
						i, at)
				}
				answered[b.ToolUseID] = true
			}
		}
	}

	for id, at := range toolUseAt {
		if answered[id] {
			continue
		}
		// THE exception. See the doc comment: this is a suspended transcript,
		// which is valid and is what a waiting loop looks like.
		if at == lastAssistant {
			continue
		}
		return invalid(at, "tool_use %q at message %d is never answered, and it is not in the "+
			"final assistant turn — so this is not a suspended transcript, it is a lost "+
			"tool result", id, at)
	}

	return valid()
}

// --- the stored envelope -----------------------------------------------------

// StoredTranscript is the envelope on disk: a version and the messages.
type StoredTranscript struct {
	FormatVersion int       `json:"formatVersion"`
	Messages      []Message `json:"messages"`
}

// MigrationOutcome distinguishes the three things that can happen, because
// "refused" and "failed to parse" need different handling by a recovery.
type MigrationOutcome string

const (
	// MigrationOK — parsed, at the current version.
	MigrationOK MigrationOutcome = "ok"
	// MigrationFromTheFuture — written by a newer Aeon. Refused loudly.
	MigrationFromTheFuture MigrationOutcome = "from-the-future"
	// MigrationTooOld — a version below the current one with no migration.
	MigrationTooOld MigrationOutcome = "too-old"
	// MigrationUnparseable — not a stored transcript at all.
	MigrationUnparseable MigrationOutcome = "unparseable"
)

type MigrationResult struct {
	Outcome  MigrationOutcome `json:"outcome"`
	Reason   string           `json:"reason,omitempty"`
	Messages []Message        `json:"messages,omitempty"`
	// Version is what the stored envelope claimed, 0 when unparseable.
	Version int `json:"version"`
}

func (r MigrationResult) OK() bool { return r.Outcome == MigrationOK }

// MigrateTranscript parses a stored transcript, refusing what it cannot handle.
//
// # Why a future version is refused rather than parsed best-effort
//
// A newer Aeon wrote it. A best-effort parse would silently drop fields this
// release does not understand — **plausibly including a `thinking`
// signature**, which Anthropic then rejects on the next tool-use turn, in a
// different process, with an error about something else.
//
// Refusing names both numbers. A lower version is refused too, with a
// DISTINGUISHABLE reason, because today only version 1 exists and conflating
// "too old" with "from the future" would send somebody to the wrong fix.
func MigrateTranscript(stored []byte) MigrationResult {
	if len(stored) == 0 {
		return MigrationResult{
			Outcome: MigrationUnparseable,
			Reason:  "the stored transcript is empty",
		}
	}

	var env StoredTranscript
	if err := json.Unmarshal(stored, &env); err != nil {
		return MigrationResult{
			Outcome: MigrationUnparseable,
			Reason:  fmt.Sprintf("the stored transcript is not valid JSON: %v", err),
		}
	}

	switch {
	case env.FormatVersion > FormatVersion:
		return MigrationResult{
			Outcome: MigrationFromTheFuture,
			Version: env.FormatVersion,
			Reason: fmt.Sprintf("the transcript is format version %d; this build understands %d. "+
				"A newer Aeon wrote it, and parsing it anyway would silently drop fields this "+
				"build does not know — including, plausibly, a thinking signature. Upgrade "+
				"rather than continue", env.FormatVersion, FormatVersion),
		}
	case env.FormatVersion < FormatVersion:
		return MigrationResult{
			Outcome: MigrationTooOld,
			Version: env.FormatVersion,
			Reason: fmt.Sprintf("the transcript is format version %d; this build understands %d "+
				"and no migration from %d exists yet", env.FormatVersion, FormatVersion,
				env.FormatVersion),
		}
	}

	// A valid envelope can still hold an invalid transcript — a corrupt row, or
	// one written by a build with a bug. Reported as unparseable rather than OK,
	// since handing a recovery an invalid transcript is worse than refusing.
	if res := ValidateTranscript(env.Messages); !res.OK {
		return MigrationResult{
			Outcome: MigrationUnparseable,
			Version: env.FormatVersion,
			Reason:  fmt.Sprintf("the transcript parsed but is not valid: %s", res.Reason),
		}
	}

	return MigrationResult{Outcome: MigrationOK, Version: env.FormatVersion, Messages: env.Messages}
}

// StoreTranscript renders the envelope S4 will persist.
func StoreTranscript(msgs []Message) ([]byte, error) {
	if res := ValidateTranscript(msgs); !res.OK {
		return nil, fmt.Errorf("model: refusing to store an invalid transcript: %s (message %d)",
			res.Reason, res.Index)
	}
	return json.Marshal(StoredTranscript{FormatVersion: FormatVersion, Messages: msgs})
}
