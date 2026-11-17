package records

// catalog_mergequeue_test.go — S60 T9 (MQ17): the merge-queue family as
// catalog rows. The plane ships the person TEN doors (T8); the catalog
// carries exactly FIVE of its names — three own-view reads twinned to the
// standing read methods, the ONE direct write that can only NARROW what the
// queue may do (this loop's own queued/conflicted item — MQ17), and the ONE
// ask, INERT until a person commits it (MQ14.3). The five forbidden names
// (enqueue · reorder · retry · land · settings) appear beside no row, in no
// schema, and in no name — the lane's forward motion is a person's act
// (MQ13), and these tests are the catalog's half of the proof.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogHasTheFiveMergeQueueSpecs(t *testing.T) {
	want := map[string]struct {
		method   string
		mutates  bool
		class    string
		required []string
	}{
		"mergequeue_list":            {"mergequeue.list", false, "safe", nil},
		"mergequeue_show":            {"mergequeue.show", false, "safe", []string{"id"}},
		"mergequeue_status":          {"mergequeue.status", false, "safe", []string{"componentRoot"}},
		"mergequeue_dequeue":         {"mergequeue.dequeue", true, "detectable", []string{"id"}},
		"mergequeue_propose_enqueue": {"mergequeue.proposeEnqueue", true, "detectable", []string{"componentRoot", "branch", "reason"}},
	}
	seen := map[string]bool{}
	for _, sp := range Catalog() {
		if sp.Family != "mergequeue" {
			continue
		}
		w, ok := want[sp.Name]
		if !ok {
			t.Errorf("%s is family mergequeue but is not one of the five — the plane ships three reads, ONE narrowing write and ONE ask (MQ17)", sp.Name)
			continue
		}
		seen[sp.Name] = true
		if sp.Method != w.method {
			t.Errorf("%s twinned to %q, want %q — the dot-name IS the standing door, and a pair that moved is a different door", sp.Name, sp.Method, w.method)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates=%v, want %v", sp.Name, sp.Mutates, w.mutates)
		}
		if sp.Class != w.class {
			t.Errorf("%s class %q, want %q — reads safe, the two agent-doored writes detectable", sp.Name, sp.Class, w.class)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s description too short to say what it answers", sp.Name)
		}
		var sch struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(sp.Schema, &sch); err != nil {
			t.Fatalf("%s schema: %v", sp.Name, err)
		}
		if len(sch.Properties) == 0 {
			t.Errorf("%s has an empty schema", sp.Name)
		}
		if strings.Join(sch.Required, ",") != strings.Join(w.required, ",") {
			t.Errorf("%s required %v, want %v", sp.Name, sch.Required, w.required)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s is missing from the catalog — the plane's agent surface is exactly the five (MQ17)", name)
		}
	}
}

// TestMergeQueueSchemasCarryNoIdentityOrStatus: the fields that decide the
// queue are not fields. `status` and `position` are the queue's own
// assignments; `attended` is the kind's (always false for a committed ask,
// MQ14.3); `base`/`baseBranch` is the daemon's to resolve (MQ15); `actor`,
// `enqueuedBy` and every stamped column is the store's and the peer's
// (OL3, MQ13's gate). A schema that named one would invite the argument the
// stamp exists to end — so none names any, and additionalProperties:false
// makes the attempt a parse error anyway.
func TestMergeQueueSchemasCarryNoIdentityOrStatus(t *testing.T) {
	banned := []string{
		"actor", "actorKind", "actorId", "createdBy", "createdByKind", "enqueuedBy",
		"status", "state", "decision", "decidedBy", "reasonKind",
		"attended", "position", "mode", "conflictPolicy", "landMethod", "maxCatchUps",
		"base", "baseBranch", "baseSha", "headSha", "landedSha", "verifiedSha",
		"kind", "loop", "loopSlug", "digest", "acknowledged",
	}
	names := []string{"mergequeue_list", "mergequeue_show", "mergequeue_status", "mergequeue_dequeue", "mergequeue_propose_enqueue"}
	all := Catalog()
	for _, name := range names {
		var sp *Spec
		for i := range all {
			if all[i].Name == name {
				sp = &all[i]
				break
			}
		}
		if sp == nil {
			t.Fatalf("%s is missing from the catalog", name)
		}
		var s map[string]any
		if err := json.Unmarshal(sp.Schema, &s); err != nil {
			t.Fatalf("%s: schema does not parse: %v", name, err)
		}
		if s["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false — a smuggled field must be a parse error", name)
		}
		props, _ := s["properties"].(map[string]any)
		for _, b := range banned {
			if _, ok := props[b]; ok {
				t.Errorf("%s carries %q — the queue's state, place, attendedness, base and authorship are nobody's to type (MQ13/MQ14.3/MQ15)", name, b)
			}
		}
	}
	// The ask's own shape, pinned: three fields, all required, nothing else
	// has anywhere to go.
	var ask map[string]any
	for _, sp := range all {
		if sp.Name == "mergequeue_propose_enqueue" {
			if err := json.Unmarshal(sp.Schema, &ask); err != nil {
				t.Fatalf("mergequeue_propose_enqueue schema: %v", err)
			}
		}
	}
	props, _ := ask["properties"].(map[string]any)
	if len(props) != 3 {
		t.Errorf("the ask carries %d fields (%v), want exactly the three (componentRoot, branch, reason)", len(props), props)
	}
}

// TestNoMergeQueueDecisionSpecExists is MQ17's negative half as POSITIVE
// data beside T8's guard: no spec is twinned to a queue method that MOVES
// the lane, and no spec's NAME speaks one. mergequeue.dequeue may twin (the
// own-item narrowing, its arm the whole restriction); the five that move
// forward may not, in method or in word.
func TestNoMergeQueueDecisionSpecExists(t *testing.T) {
	forbiddenTwins := []string{
		"mergequeue.enqueue", "mergequeue.reorder", "mergequeue.retry",
		"mergequeue.land", "mergequeue.settings",
	}
	forbiddenWords := []string{"mergequeue_enqueue", "mergequeue_reorder", "mergequeue_retry", "mergequeue_land", "mergequeue_settings"}
	for _, sp := range Catalog() {
		for _, f := range forbiddenTwins {
			if sp.Method == f {
				t.Errorf("%s is twinned to %q — nothing that moves the lane reaches an agent through the catalog (MQ13/MQ17)", sp.Name, f)
			}
		}
		for _, w := range forbiddenWords {
			if sp.Name == w {
				t.Errorf("%s speaks a forbidden queue verb — the name itself is the violation (MQ17)", sp.Name)
			}
		}
	}
}
