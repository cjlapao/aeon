package daemon

// records_input.go — the small input-shaping helpers every arm of the
// management catalog shares (S38 T9's one implementation, split for SIZE1 in
// S47 T7 by cohesion: this file is the door's plumbing). `decode` is where
// OL3 is enforced at the byte level — unknown fields are refused rather than
// ignored, because a field a model typed and the code silently dropped is a
// field the model believes it set.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// ok1 adapts a (value, error) store call to the service's three-way answer.
func ok1[T any](v T, err error) (any, *Failure, error) {
	if err != nil {
		return nil, nil, err
	}
	return v, nil, nil
}

// decode is the strict door every arm shares: unknown fields are refused at
// parse time, so an identity field a model typed never reaches the store
// even as an ignored extra.
func decode(input json.RawMessage, v any) (*Failure, error) {
	if len(bytes.TrimSpace(input)) == 0 {
		input = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return &Failure{Sentence: fmt.Sprintf("refused: %v — identity fields (project, loop, component, author) are stamped by the step, and every other field belongs in the schema", err)}, nil
		}
		return nil, fmt.Errorf("daemon: catalog input: %w", err)
	}
	return nil, nil
}

func notFoundFailure(id string, err error) *Failure {
	if errors.Is(err, store.ErrNotFound) || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "does not exist") {
		return &Failure{Sentence: fmt.Sprintf("refused: %s is not a record the store has", id)}
	}
	return &Failure{Sentence: fmt.Sprintf("refused: %s cannot be read (%v)", id, err)}
}

// isPlainChat decides whether a step's loop may file MANUAL tasks (OL14:
// only a plain chat — no blueprint run — hand-makes tasks from a conversation).
// MEASURED 2026-10-27 (S39 T0 drafting): every loop pins a blueprint today,
// so there is nothing to test yet — the helper returns false and the
// `manual` argument is always refused, which is the safe direction (an agent
// cannot smuggle a hand-made task into a blueprint loop). S39 T6 lands the
// `chat` agent and blueprint and re-points THIS helper at
// pinnedBlueprint(ident) == "chat"; tests stub the variable, never a fake chat.
var isPlainChat = func(ident Identity) bool { return false }

// orDefaultStr keeps Go from reading like a lisp in the create paths.
func orDefaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// parseConfidence maps the schema's words to the column's integers (1 low,
// 2 medium, 3 high; the CHECK of 0036 admits exactly those). Absent stays
// nil — unknown, which the rollup must SEE (P31: three states, never two).
func parseConfidence(v string) (*int, *Failure) {
	if v == "" {
		return nil, nil
	}
	n := map[string]int{"low": 1, "medium": 2, "high": 3}[v]
	if n == 0 {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: confidence %q is not low, medium or high", v)}
	}
	return &n, nil
}

// slugify derives a component slug from a name the way every other slug in
// this codebase is derived: lower, non-alphanumerics to single dashes,
// trimmed. Two components with names differing only in punctuation collide
// on purpose — the UNIQUE index says so before anything half-registered does.
func slugify(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// taskCreate files a task through the step's identity. Inside a loop it is a
// LOOP task — the plan's id, tech, engineer (default: the step's own agent,
// OL14's "assignee that changes as it climbs" starting at the planner's
// hand); `manual: true` exists only in a plain chat (isPlainChat above).
// Dependencies are validated BEFORE the insert against the loop's existing
// tasks, and the refusal speaks plan's own sentence — the one renderer, so
// `aeon loop`, the planner's transcript and this door all say the same words.
