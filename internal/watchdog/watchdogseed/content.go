package watchdogseed

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/seed"
)

// content is the canonical content of one watchdog rule record — the exact
// columns the rule row holds, in the order the fingerprint hashes them, so
// the round-trip source → row → fingerprint is a content comparison (SD8),
// not a column one. The code is the seed source, not the truth (SD1): a rule
// changed by an operator is 'diverged' or 'removed', never re-seeded.
type content struct {
	Scope     string `json:"scope"`
	SubjectID string `json:"subjectId"`
	Signal    string `json:"signal"`
	Threshold int64  `json:"threshold"`
	Action    string `json:"action"`
	Enabled   bool   `json:"enabled"`
	Origin    string `json:"origin"`
}

// defaultRule builds a record's content from the default-set table (LC18).
// Every seed rule is origin 'system' and enabled: the set ships the default
// the operator can then tighten, loosen or remove.
func defaultRule(scope, subject, signal string, threshold int64, action string) content {
	return content{Scope: scope, SubjectID: subject, Signal: signal, Threshold: threshold, Action: action, Enabled: true, Origin: "system"}
}

// key is the record's stable identity: the rule's unique tuple
// (scope, subject, signal, action, threshold), colon-joined. Subject ids are
// 'workspace' or a component id, and neither carries a colon, so the join is
// reversible.
func key(c content) string {
	return fmt.Sprintf("%s:%s:%s:%s:%d", c.Scope, c.SubjectID, c.Signal, c.Action, c.Threshold)
}

// contentForKey is the inverse of key — the fingerprint's way back from the
// ledger's key to the fields it looks the row up by.
func contentForKey(key string) (content, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 5 {
		return content{}, fmt.Errorf("watchdogseed: a key is scope:subject:signal:action:threshold, got %q", key)
	}
	threshold, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return content{}, fmt.Errorf("watchdogseed: the key %q has no numeric threshold: %v", key, err)
	}
	return content{Scope: parts[0], SubjectID: parts[1], Signal: parts[2], Action: parts[3], Threshold: threshold}, nil
}

// Source is the set's declared records (never nil, rule 3) — the five
// workspace rules LC18 names: a no-activity notify at 15 minutes and a stop
// at the hour, same-position and repeat-block-reason stops at six and three
// dispatches, and a step-over-deadline block with no grace. The content is
// the canonical JSON of each rule; the version is the set's (1), bumped
// together with it whenever a rule's content changes.
func Source() []seed.Record {
	recs := []content{
		defaultRule("workspace", "workspace", "no-activity", 900000, "notify"),
		defaultRule("workspace", "workspace", "no-activity", 3600000, "stop"),
		defaultRule("workspace", "workspace", "same-position", 6, "stop"),
		defaultRule("workspace", "workspace", "repeat-block-reason", 3, "stop"),
		defaultRule("workspace", "workspace", "step-over-deadline", 0, "block"),
	}
	out := make([]seed.Record, 0, len(recs))
	for _, c := range recs {
		b, err := seed.Canonical(c)
		if err != nil {
			panic("watchdogseed: the default set's content is not canonical JSON: " + err.Error())
		}
		out = append(out, seed.Record{Key: key(c), Version: setVersion, Content: b})
	}
	return out
}

// decode parses a record's Content into its content.
func decode(b []byte) (content, error) {
	var c content
	if err := json.Unmarshal(b, &c); err != nil {
		return content{}, err
	}
	return c, nil
}
