package automationview

import "github.com/cjlapao/aeon/internal/match"

// vocabulary.go — everything the forms render from, so no form hardcodes a
// word list. The applicability table is DERIVED, not restated: each
// (event, key) pair is probed through the grammar's own Validate, and the
// answer this file prints is the validator's answer. A rule that changes
// inside internal/match changes this read in the same commit, because there
// is no second table here to forget.

// VocabularyView is one read of the whole vocabulary. The json tags are its
// OWN, added by T3: `automation.vocabulary` answers with this struct
// directly (no protocol mirror, no restatement — the spec's read IS this
// function), so the wire names live here, on the data, not in a copy.
type VocabularyView struct {
	Events        []EventWords  `json:"events"`
	MatchKeys     []string      `json:"matchKeys"`
	TaskTypes     []string      `json:"taskTypes"`
	GoalTypes     []string      `json:"goalTypes"` // OL14's five — goal.type shares them; one list, one truth
	Outcomes      []OutcomeWord `json:"outcomes"`
	Limits        Limits        `json:"limits"`
	WindowGrammar string        `json:"windowGrammar"`
	Zones         []string      `json:"zones"`
	// AuthorityStatement is the fixed sentence the UI shows on EVERY
	// authority-granting confirm (enabling, resuming, firing now). The
	// caller fills <description> from Describe; the rest is not editable.
	AuthorityStatement string `json:"authorityStatement"`
}

// EventWords is one event, the keys that may carry values on it, and the
// one-line meaning the forms print.
type EventWords struct {
	Event          string   `json:"event"`
	Meaning        string   `json:"meaning"`
	ApplicableKeys []string `json:"applicableKeys"`
}

// OutcomeWord is one fire outcome as a person reads it. The five are the
// union the AN5 view speaks; the Family names which engine produces it
// ("both" when either can).
type OutcomeWord struct {
	Word    string `json:"word"`
	Meaning string `json:"meaning"`
	Family  string `json:"family"`
}

// Limits are the numbers the validators enforce (mirrored from
// internal/match's rules and the store's clamps — the vocabulary read
// prints what the code checks so a form cannot offer a value the write will
// refuse).
type Limits struct {
	MaxValuesPerKey int `json:"maxValuesPerKey"`
	MaxValueLen     int `json:"maxValueLen"`
	PreviewMax      int `json:"previewMax"`
	AuditPageMax    int `json:"auditPageMax"`
	TriggerIDMaxLen int `json:"triggerIdMaxLen"`
}

// probeApplicable asks the validator one question per (event, key) pair and
// keeps its answer. The probe value is chosen legal-per-key where the key
// also validates values (task_type), and non-empty everywhere, so the only
// thing the probe can be refused for is the KEY's applicability.
func probeApplicable(event, key string) bool {
	val := "probe-value"
	if key == "task_type" {
		val = "bug" // task_type validates values too; probe with a legal one
	}
	_, err := match.ParseMatch([]byte(`{"event":"` + event + `","` + key + `":"` + val + `"}`))
	return err == nil
}

// Vocabulary answers the whole form-side question set.
func Vocabulary() VocabularyView {
	ev := VocabularyView{
		MatchKeys: match.MatchKeyNames()[1:], // past "event": the five value keys
		TaskTypes: match.GoalTypeWords(),
		GoalTypes: match.GoalTypeWords(),
		Outcomes: []OutcomeWord{
			{"started", "a loop was started", "both"},
			{"skipped", "the engine declined before deciding (overlap, a DST note, or a paused parent)", "both"},
			{"did-not-fire", "the schedule's own contract excluded the instant (outside the working-hours window)", "schedule"},
			{"refused", "a gate or guard refused the fire (depth, budget, policy, disabled)", "both"},
			{"limited", "an hourly or daily cap was already spent", "event"},
			{"could-not-say", "the engine cannot tell (a claim left by a crash, an edit mid-flight)", "both"},
		},
		Limits: Limits{
			MaxValuesPerKey: 16, MaxValueLen: 128,
			PreviewMax: previewMax, AuditPageMax: 500, TriggerIDMaxLen: 64,
		},
		WindowGrammar: "window_start and window_end are HH:MM in the trigger's zone; window_days is an ISO list (1=Monday … 7=Sunday, e.g. \"1,2,3,4,5\"); start and end arrive together or not at all",
		Zones:         curatedZones,
		AuthorityStatement: "This starts agents with no one present. Outward actions are denied. " +
			"It will run: <description>",
	}
	meanings := map[string]string{
		match.EventGoalApproved:  "a goal left the approval question approved",
		match.EventTaskCreated:   "a manual task was created",
		match.EventTaskApproved:  "a manual task was approved (its goal approved, or the task is independent)",
		match.EventTaskCompleted: "a manual task reached the done column",
	}
	for _, e := range match.Events() {
		ews := EventWords{Event: e, Meaning: meanings[e]}
		for _, k := range ev.MatchKeys {
			if probeApplicable(e, k) {
				ews.ApplicableKeys = append(ews.ApplicableKeys, k)
			}
		}
		ev.Events = append(ev.Events, ews)
	}
	return ev
}

// curatedZones — LoadZone accepts ANY IANA name (T0 item 10); this is the
// picker's list, longest-used first, not a whitelist. A zone absent here
// still saves if it is real IANA; the form says so.
var curatedZones = []string{
	"UTC", "Europe/Lisbon", "Europe/Madrid", "Europe/London", "Europe/Paris",
	"Europe/Berlin", "Europe/Warsaw", "Europe/Istanbul", "Europe/Moscow",
	"America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles",
	"America/Toronto", "America/Mexico_City", "America/Sao_Paulo", "America/Buenos_Aires",
	"America/Santiago", "America/Bogota", "America/Lima",
	"Asia/Dubai", "Asia/Karachi", "Asia/Kolkata", "Asia/Bangkok", "Asia/Singapore",
	"Asia/Hong_Kong", "Asia/Shanghai", "Asia/Tokyo", "Asia/Seoul",
	"Australia/Perth", "Australia/Sydney", "Australia/Melbourne", "Australia/Brisbane",
	"Australia/Adelaide", "Australia/Darwin", "Australia/Broken_Hill", "Australia/Lord_Howe",
	"Pacific/Auckland", "Pacific/Chatham", "Africa/Lagos", "Africa/Cairo",
	"Africa/Johannesburg", "Africa/Nairobi", "Asia/Riyadh", "Asia/Jerusalem",
}
