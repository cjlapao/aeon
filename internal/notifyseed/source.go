package notifyseed

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
)

// recordContent is the canonical JSON body every record of this set
// carries: the row's content columns and nothing else. The engine hashes
// these bytes; the applier writes them into the row, and Fingerprint
// re-builds the SAME bytes from the live row — so "applied == live" holds
// exactly when the row still holds what this file shipped (the diverge
// detector, SD rule: an operator edit re-envelops to different bytes and
// the engine never writes over it again).
//
// Enabled is inside the content ON PURPOSE: turning a class off is the
// operator's Remove right (SV4), and it must read as a divergence so a
// re-seed never re-enables what a person silenced. Version rides along
// for the same reason: an edit bumps it, and a bumped version is an edit.
type recordContent struct {
	Kind          string `json:"kind"`
	Variant       string `json:"variant"`
	Tone          string `json:"tone"`
	TitleTemplate string `json:"titleTemplate"`
	BodyTemplate  string `json:"bodyTemplate"`
	Enabled       bool   `json:"enabled"`
	Version       int    `json:"version"`
}

// shipped is the set, in table order. Every title and tone is BYTE-
// IDENTICAL to the arm of the deleted notifyHintFor switch (T1's
// characterization goldens and TestResolverSeededRowsEqualTheOldSwitch
// are the proof); question.asked is the one NEW hint T4 adds (SV7's
// dedup-aware kind: same subject as loop.waiting, one delivery — the
// consumers dedup by attentionKey, not the resolver). The variant rows
// repeat their kind's base title: the variant exists so an operator can
// DIFFERENTIATE later, not so T4 can change today's bytes.
//
// The rows mirror the emitters measured at T0: loop.blocked's reason
// kinds from the runtime's blockTo paths (action-denied, step-blocked,
// exhaustion, model-error, runner-error), loop.waiting's stop-to-wait
// reason (advance.go/continue.go emit "reason":"stopped"), and the four
// S55 kinds (stop-requested, watchdog, budget warn/hit) whose arms the
// switch had grown — a curated hint must not go silent because its slice
// is not named here (the deviation T4 records).
var shipped = []recordContent{
	{Kind: "loop.finished", Variant: "", Tone: "success", TitleTemplate: "Loop finished", Enabled: true, Version: 1},
	{Kind: "loop.blocked", Variant: "", Tone: "warning", TitleTemplate: "Loop blocked", Enabled: true, Version: 1},
	{Kind: "loop.blocked", Variant: "action-denied", Tone: "warning", TitleTemplate: "Loop blocked", Enabled: true, Version: 1},
	{Kind: "loop.blocked", Variant: "step-blocked", Tone: "warning", TitleTemplate: "Loop blocked", Enabled: true, Version: 1},
	{Kind: "loop.blocked", Variant: "exhaustion", Tone: "warning", TitleTemplate: "Loop blocked", Enabled: true, Version: 1},
	{Kind: "loop.blocked", Variant: "model-error", Tone: "warning", TitleTemplate: "Loop blocked", Enabled: true, Version: 1},
	{Kind: "loop.blocked", Variant: "runner-error", Tone: "warning", TitleTemplate: "Loop blocked", Enabled: true, Version: 1},
	{Kind: "loop.waiting", Variant: "", Tone: "info", TitleTemplate: "Loop waiting on you", Enabled: true, Version: 1},
	{Kind: "loop.waiting", Variant: "stopped", Tone: "info", TitleTemplate: "Loop waiting on you", Enabled: true, Version: 1},
	{Kind: "loop.stop.requested", Variant: "", Tone: "warning", TitleTemplate: "An agent asked you to stop a loop", Enabled: true, Version: 1},
	{Kind: "watchdog.fired", Variant: "", Tone: "warning", TitleTemplate: "The watchdog needs your attention", Enabled: true, Version: 1},
	{Kind: "budget.warn", Variant: "", Tone: "warning", TitleTemplate: "A budget is near its cap", Enabled: true, Version: 1},
	{Kind: "budget.hit", Variant: "", Tone: "warning", TitleTemplate: "A budget cap is exhausted", Enabled: true, Version: 1},
	{Kind: "question.asked", Variant: "", Tone: "info", TitleTemplate: "A question is waiting for you", Enabled: true, Version: 1},
}

// Source returns one record per shipped class, in order. The engine calls
// it at apply time; the golden test pins that content changes ride a
// version bump.
func Source() []seed.Record {
	recs := make([]seed.Record, 0, len(shipped))
	for _, c := range shipped {
		body, err := json.Marshal(c)
		if err != nil {
			// A record that cannot marshal is a build-time fact: the set
			// must never silently ship short (rule 3). Panic at the
			// source — the same init-time stance SD2 takes.
			panic(fmt.Sprintf("notifyseed: record %q cannot encode: %v", c.Key(), err))
		}
		recs = append(recs, seed.Record{Key: c.Key(), Version: c.Version, Content: body})
	}
	return recs
}

// Key is the engine's record key: the (kind, variant) pair in one string.
// The pipe is the same separator the audit's subject_id uses, so a ledger
// row and an audit row name the same class the same way.
func (c recordContent) Key() string { return c.Kind + "|" + c.Variant }

// SplitKey reads a record key back into (kind, variant).
func SplitKey(key string) (kind, variant string, ok bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}

// SetVersion is the set's release number: the maximum of its records'
// versions (rule 3), computed from Source() so the two cannot disagree.
func SetVersion() int {
	max := 1
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}

// DecodeRecord reads a record's canonical body back. The applier's one
// parse; a foreign shape is a refusal naming the key.
func DecodeRecord(r seed.Record) (recordContent, error) {
	var c recordContent
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return c, fmt.Errorf("notifyseed: record %q is not canonical JSON: %w", r.Key, err)
	}
	if c.Key() != r.Key {
		return c, fmt.Errorf("notifyseed: record %q names %s/%s inside — the key and the class must agree", r.Key, c.Kind, c.Variant)
	}
	if c.TitleTemplate == "" {
		return c, fmt.Errorf("notifyseed: record %q carries an empty title template — a class that renders no title is not a class", r.Key)
	}
	return c, nil
}
