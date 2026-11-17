package store

// attention_read.go — S58 T2 (SV1): the reads behind the attention sources.
//
// The inbox is a query over live state and nothing else (SV1: an alert table
// would be a second truth that drifts, the DB6 argument that removed
// `awaiting_user`). Everything the `questions`, `blocked` and `stopped`
// sources show a person is DERIVED here, at read time, from the rows that are
// already the truth: the unanswered `question`, the loop row's `status`, and
// the pinned runtime JSON's `lastReason` — the same fields
// `history.blockedReason` and the blueprint views read, never a copy of them
// elsewhere.
//
// # Why these live in the store and not in `internal/attention`
//
// `internal/attention` is forbidden SQL (the no-SQL-outside-the-store grep,
// the `TestNoSQLLivesOutsideTheStore` rule); this file is the SQL half, and
// it returns ids, slugs, statuses, timestamps and the STORED reason text —
// never a severity, a door, or a sentence. Those are shape, and shape is code
// in the attention package (rule 6).
//
// # Scoping
//
// Every reader passes the component ROOT PATHS the scope resolved to (S58's
// `rootId` is a component id; the daemon resolves its tree here in
// `scope.go`-style store reads and hands the paths down). An empty list means
// EVERY component, the `ComponentScope`/`aeon loop list --all` convention —
// it is a separate, deliberate absence, never a default that silently widens
// a loop-scoped read.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// AttentionQuestion is one unanswered question row, the `questions` source's
// raw material. `Prompt` is the question's own body — the attention package
// carries it in a LOCAL-ONLY field and never copies it into `reason` (SV6);
// this layer only hands the stored text over.
type AttentionQuestion struct {
	QuestionID    int64
	ComponentRoot string
	LoopSlug      string
	StepID        *int64 // nullable on purpose — a phase gate has no step (the chat.go comment is the authority)
	Kind          string // clarify|gate|permission|free
	Prompt        string // the stored body, verbatim
	OptionsRaw    string // the stored options TEXT, verbatim (a JSON array, "" when none)
	AskedAt       string
}

// PendingQuestionsForScope is `store.awaiting`'s shape widened to many roots:
// every unanswered question (`answered_at IS NULL`, the exact predicate
// AwaitingSQL uses — one predicate, one definition), oldest first. A new key
// for a recurrence comes free: a re-ask is a new row with a new id and a new
// asked_at (SV3).
func (s *Store) PendingQuestionsForScope(roots []string) ([]AttentionQuestion, error) {
	where, args := RootScopeSQL("q.component_root", roots)
	rows, err := s.r.Query(`
		SELECT q.id, q.component_root, q.loop_slug, q.step_id, q.kind, q.body,
		       COALESCE(q.options, ''), q.asked_at
		  FROM question q
		 WHERE q.answered_at IS NULL`+where+`
		 ORDER BY q.asked_at, q.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the questions waiting for a person: %w", err)
	}
	defer rows.Close()

	var out []AttentionQuestion
	for rows.Next() {
		var (
			q       AttentionQuestion
			stepID  sql.NullInt64
			options string
		)
		if err := rows.Scan(&q.QuestionID, &q.ComponentRoot, &q.LoopSlug, &stepID,
			&q.Kind, &q.Prompt, &options, &q.AskedAt); err != nil {
			return nil, fmt.Errorf("store: read a waiting question: %w", err)
		}
		if stepID.Valid {
			v := stepID.Int64
			q.StepID = &v
		}
		q.OptionsRaw = options
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the waiting questions: %w", err)
	}
	return out, nil
}

// AttentionBlockedLoop is one blocked loop row. `LastReason` is the pinned
// runtime JSON's `lastReason` — the sentence the runtime stored, which the
// person sees VERBATIM (H4: never a rewritten verdict). It is empty when no
// pin exists, which renders as "a blocked loop with no recorded reason", not
// a fabricated one.
type AttentionBlockedLoop struct {
	LoopID        string
	Slug          string
	ComponentRoot string
	LastReason    string
	UpdatedAt     string
}

// BlockedLoopsForScope lists loops whose STATUS is blocked. The status is the
// loop row's — one truth, read; the reason's KIND comes from the durable
// `loop.blocked` event (SV-D12, LatestBlockedReasons below), because T0's F5
// measured that a blockTo kind does NOT survive verbatim into `lastReason`
// (the runtime JSON holds the kind only for cap-hit/stuck/stopped; every
// other block stores the DETAIL there).
func (s *Store) BlockedLoopsForScope(roots []string) ([]AttentionBlockedLoop, error) {
	where, args := RootScopeSQL("l.component_root", roots)
	rows, err := s.r.Query(`
		SELECT l.id, l.slug, l.component_root,
		       COALESCE(json_extract(rb.runtime_json, '$.lastReason'), ''),
		       l.updated_at
		  FROM loop l
		  LEFT JOIN loop_blueprint rb ON rb.loop_id = l.id
		 WHERE l.status = 'blocked'`+where+`
		 ORDER BY l.updated_at, l.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the blocked loops: %w", err)
	}
	defer rows.Close()

	var out []AttentionBlockedLoop
	for rows.Next() {
		var b AttentionBlockedLoop
		if err := rows.Scan(&b.LoopID, &b.Slug, &b.ComponentRoot, &b.LastReason, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: read a blocked loop: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the blocked loops: %w", err)
	}
	return out, nil
}

// AttentionStoppedLoop is a loop a PERSON stopped: status `waiting`, runtime
// `lastReason = 'stopped'`, and no unanswered question (a loop waiting on a
// question is the questions source's, and `answer` is its door — the two
// kinds must never both claim one loop). `StepID` is the step suspended by
// that stop (`pause_reason = 'user'`), the id `step.continue` takes; 0 when
// no such step row matches — the loop's pin says a person stopped it and the
// step row moved first, which is honest to carry as "no step id to name".
type AttentionStoppedLoop struct {
	LoopID        string
	Slug          string
	ComponentRoot string
	StepID        int64
	UpdatedAt     string
}

// StoppedLoopsForScope is the stopped read, one predicate everywhere: the
// same status + lastReason + question-absence test the daemon's own views
// apply (loop_control's park is what WRITES this shape — the read is its
// mirror, not a second definition of it).
func (s *Store) StoppedLoopsForScope(roots []string) ([]AttentionStoppedLoop, error) {
	where, args := RootScopeSQL("l.component_root", roots)
	rows, err := s.r.Query(`
		SELECT l.id, l.slug, l.component_root,
		       COALESCE(st.id, 0), l.updated_at
		  FROM loop l
		  JOIN loop_blueprint rb ON rb.loop_id = l.id
		  LEFT JOIN step st
		         ON st.component_root = l.component_root AND st.loop_slug = l.slug
		        AND st.status = 'suspended' AND st.pause_reason = 'user'
		 WHERE l.status = 'waiting'
		   AND json_extract(rb.runtime_json, '$.lastReason') = 'stopped'
		   AND NOT EXISTS (SELECT 1 FROM question q
		                    WHERE q.component_root = l.component_root
		                      AND q.loop_slug = l.slug
		                      AND q.answered_at IS NULL)`+where+`
		 ORDER BY l.updated_at, l.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the loops a person stopped: %w", err)
	}
	defer rows.Close()

	var out []AttentionStoppedLoop
	for rows.Next() {
		var st AttentionStoppedLoop
		if err := rows.Scan(&st.LoopID, &st.Slug, &st.ComponentRoot, &st.StepID, &st.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: read a stopped loop: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the stopped loops: %w", err)
	}
	return out, nil
}

// BlockedReason is the reason carried by the DURABLE `loop.blocked` event —
// the read `history.blockedReason` pioneered (SV-D12: READ, not invented).
// `Kind` is the emitted reason kind (`action-denied`, `exhaustion`, …); an
// event whose payload predates kinds reads as Kind "" with the text in
// `Detail`, which the source classifies as `blocked.other` — never a guess.
type BlockedReason struct {
	Kind   string
	Detail string
	Action string
	At     string // the event's own timestamp: the blocked item's `since`
}

// LatestBlockedReasons maps loop id → the reason of that loop's LATEST
// `loop.blocked` event, for every blocked loop in scope (the newest block is
// the reason for the current blocked status; an older one is a block it
// recovered from). It reads the event stream — the source design.md names —
// through ReadEvents, the same door history/query.go uses, per component
// root; the window per root is the same 200 events, and a loop with no
// recorded reason is ABSENT from the map, which renders honestly downstream.
func (s *Store) LatestBlockedReasons(roots []string) (map[string]BlockedReason, error) {
	loops, err := s.BlockedLoopsForScope(roots)
	if err != nil {
		return nil, err
	}
	out := map[string]BlockedReason{}
	seenRoot := map[string]bool{}
	byLoop := map[string]bool{}
	for _, l := range loops {
		byLoop[l.LoopID] = true
	}
	for _, l := range loops {
		if seenRoot[l.ComponentRoot] {
			continue
		}
		seenRoot[l.ComponentRoot] = true
		events, err := s.ReadEvents(0, EventFilter{
			Kinds:         []string{"loop.blocked"},
			ComponentRoot: l.ComponentRoot,
		}, 200)
		if err != nil {
			return nil, fmt.Errorf("store: read the blocked reasons for %s: %w", l.ComponentRoot, err)
		}
		for _, ev := range events { // seq order: the last match wins (the history precedent)
			var p struct {
				LoopID string          `json:"loopId"`
				Reason json.RawMessage `json:"reason"`
			}
			if len(ev.Payload) == 0 || json.Unmarshal(ev.Payload, &p) != nil {
				continue
			}
			if !byLoop[p.LoopID] {
				continue
			}
			// TWO real payload shapes, both measured (T0 F5): the object a
			// blockTo/BlockedReason emits, and the bare STRING some older
			// emits carried. Read both; never invent a kind for the string.
			var br struct {
				Kind   string `json:"kind"`
				Detail string `json:"detail"`
				Action string `json:"action"`
			}
			isObject := len(p.Reason) > 0 && p.Reason[0] == '{'
			if isObject {
				if json.Unmarshal(p.Reason, &br) != nil {
					continue
				}
			} else {
				var s2 string
				if json.Unmarshal(p.Reason, &s2) != nil {
					continue
				}
				br.Detail = s2
			}
			out[p.LoopID] = BlockedReason{Kind: br.Kind, Detail: br.Detail, Action: br.Action, At: ev.CreatedAt}
		}
	}
	return out, nil
}

// RootScopeSQL is the scope predicate for a component-root-path column
// (DB4's discipline at component grain: the column is always named with its
// table prefix). The scope is the set of ROOT paths the resolver produced: a
// root's directory carries its own loops (exact match) and every child
// component's (a child is a checkout UNDER its parent's path — S22's
// tree — so `root/%` matches the whole subtree by construction, no
// path-enumeration race against a child created mid-query). LIKE is used
// with ESCAPE and every wildcard escaped, so a directory named with a `%`
// or `_` is a literal, not a pattern. An empty set is the deliberate EVERY.
func RootScopeSQL(column string, roots []string) (string, []any) {
	if len(roots) == 0 {
		return "", nil
	}
	var parts []string
	args := make([]any, 0, 2*len(roots))
	for _, r := range roots {
		er := rootLikeEscape(r)
		parts = append(parts, column+" = ?", column+` LIKE ? ESCAPE '\'`)
		args = append(args, r, er+`/%`)
	}
	return " AND (" + strings.Join(parts, " OR ") + ")", args
}

// escapeLike neutralizes LIKE's wildcards (and the escape char itself).
func rootLikeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ComponentTree is the read the scope resolver needs (*Store satisfies it);
// the named seam keeps `internal/attention` free of anything but reads.
type ComponentTree interface {
	LiveComponentTree() ([]AttentionComponent, error)
}

// AttentionComponent is one LIVE component with the root paths attached to
// it (the `component_root` side table, all paths not just the primary —
// loops key on the path they ran under, and a secondary path is just as
// much a home). The scope resolver (attention/scope.go) turns a component
// id into the paths its tree carries with exactly this read.
type AttentionComponent struct {
	ID       string
	ParentID string
	Paths    []string
}

// LiveComponentTree is the whole live tree in one read — every component
// with archived rows excluded (an archived component is OUT of a scope; a
// scope naming one refuses like any unknown id, rather than silently
// answering for a tree the person can no longer see).
func (s *Store) LiveComponentTree() ([]AttentionComponent, error) {
	rows, err := s.r.Query(`
		SELECT c.id, COALESCE(c.parent_id, ''), COALESCE(cr.component_root, '')
		  FROM component c
		  LEFT JOIN component_root cr ON cr.component_id = c.id
		 WHERE c.archived_at IS NULL
		 ORDER BY c.id, cr.component_root`)
	if err != nil {
		return nil, fmt.Errorf("store: read the live component tree: %w", err)
	}
	defer rows.Close()

	byID := map[string]*AttentionComponent{}
	var order []string
	for rows.Next() {
		var id, parent, path string
		if err := rows.Scan(&id, &parent, &path); err != nil {
			return nil, fmt.Errorf("store: read a component tree row: %w", err)
		}
		c, ok := byID[id]
		if !ok {
			c = &AttentionComponent{ID: id, ParentID: parent}
			byID[id] = c
			order = append(order, id)
		}
		if path != "" {
			c.Paths = append(c.Paths, path)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the component tree: %w", err)
	}
	out := make([]AttentionComponent, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// The named seams the attention sources take (the store satisfies all three;
// naming them keeps `internal/attention` holding reads and nothing else, and
// keeps a source's dependency visible in its field type).
type (
	PendingQuestionsReader interface {
		PendingQuestionsForScope(roots []string) ([]AttentionQuestion, error)
	}
	BlockedLoopsReader interface {
		BlockedLoopsForScope(roots []string) ([]AttentionBlockedLoop, error)
		LatestBlockedReasons(roots []string) (map[string]BlockedReason, error)
	}
	StoppedLoopsReader interface {
		StoppedLoopsForScope(roots []string) ([]AttentionStoppedLoop, error)
	}
)
