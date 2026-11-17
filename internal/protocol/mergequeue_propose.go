// ════════════════════════════════════════════════════════════════════════════
// mergequeue_propose.go — S60 T9 (MQ14.3, MQ15): the queue plane's ONE agent
// door, `mergequeue.proposeEnqueue`, answered by the records service
// (Table.Records — RecordsAPI.Do), NOT by the queue's own service. It creates
// an INERT proposal row of kind mergequeue_enqueue — never a queue row, never
// a position, never a base: the plane's ten write doors (enqueue · dequeue ·
// reorder · retry · land · settings, and the four reads beside them) stay the
// person's human door (T8), and no tool, no method and no proposal shape
// reaches the six writes beyond the one ask this file opens.
//
// An agent may SEE the queue (the three read twins of T9's catalog family,
// answered over the SAME service the person's reads answer through), REMOVE
// its own loop's still-queued or conflicted item (the one narrowing write,
// records_mergequeue.go), and PROPOSE that a branch join; the proposal
// constrains nothing until a person commits it (S35 TD2), and the commit
// stamps the row {proposal, <id>} and lands it UNATTENDED (MQ14.3) — asking
// never buys an agent a watched landing or a queue row of its own authorship.
//
// The catalog row (internal/records' `mergequeue_propose_enqueue` spec) is
// the one definition; this file is the socket's half of the propose door,
// exactly as environment_propose.go does it for the execution environment:
// registered CONDITIONALLY (V13) because this is a MUTATING catalog action
// and a table without the records service leaves it ABSENT, not
// registered-and-refusing. registerRecords skips the whole mergequeue family
// (the three reads belong to RegisterMergeQueue and this name to
// RegisterMergeQueueProposeEnqueue — a projection here would double-register
// at the first wiring).
//
// What the door refuses, and where: the catalog schema (additionalProperties:
// false) carries NO actor, status, attended, position, mode or baseBranch
// field — identity is the step's, stamped on the service side from the peer
// (OL3); the base is resolved by the DAEMON at enqueue, never typed here
// (MQ15); attended is the kind's, fixed false by MQ14.3. The plane's own key
// gate (rejectMergeQueueIdentityKeys, T8's) runs on the raw params BEFORE the
// decode, so a smuggled "actor" or "base" meets a named refusal rather than a
// silently-ignored field.
// ════════════════════════════════════════════════════════════════════════════

package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// MethodMergeQueueProposeEnqueue is the queue plane's ONE agent method — the
// ask half of MQ14.3, the twin of the catalog tool below. It is a write by
// nature (it files a row) but never a human door: it stands on the agent's
// surface and classifies SurfaceWrite WITHOUT HumanDoorOnly, the one
// mergequeue name whose surface_table entry carries no door flag.
const MethodMergeQueueProposeEnqueue MethodName = "mergequeue.proposeEnqueue"

// mergeQueueProposeEnqueueTool is the catalog tool name the twin dispatches
// under (RecordsAPI.Do takes the tool name — the catalog's vocabulary, not a
// second method table).
const mergeQueueProposeEnqueueTool = "mergequeue_propose_enqueue"

// MergeQueueProposeEnqueueParams is the propose door's documented params —
// the catalog schema's shape as a Go type so the conformance fixtures
// round-trip through it. The repository, the branch, and the reason a person
// reads at the desk; nothing else has anywhere to go (a smuggled "actor",
// "attended", "status", "position" or "base" meets the plane's key gate
// below and the desk's parse refusal after it).
type MergeQueueProposeEnqueueParams struct {
	ComponentRoot string `json:"componentRoot"`
	Branch        string `json:"branch"`
	Reason        string `json:"reason"`
}

// RegisterMergeQueueProposeEnqueue registers the twin when (and only when)
// the records service is wired — the RegisterSeed/V13 rule the policy,
// autonomy, blueprint and environment twins carry: a table without
// Table.Records leaves it ABSENT (ErrUnknownMethod), never
// registered-and-refusing, because the records service is what honours what
// makes this door safe (the stamped agent actor, the shared create
// validator — ValidateEnqueue over gathered facts, MQ14's one function —
// and the per-agent open-proposal cap). Returns true when it stands (already
// or just now); false when the seam is missing.
func (t *Table) RegisterMergeQueueProposeEnqueue() bool {
	if t.Records == nil {
		return false
	}
	if _, ok := t.methods[MethodMergeQueueProposeEnqueue]; ok {
		return true // already standing (a re-registration is a no-op, not a second door)
	}
	t.register(&Method{
		Name:               MethodMergeQueueProposeEnqueue,
		RequiresInitialize: true,
		Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
			// The plane's own key gate first — before the service, before
			// the decode, in the plane's own words (T8's gate) — plus the
			// twin's OWN two keys: attended and the base belong to the
			// person's enqueue door and to MQ15's resolver, never to an
			// ask, so they cannot join the shared list without weakening
			// mergequeue.enqueue (which legitimately takes them).
			if err := rejectMergeQueueAskKeys(string(MethodMergeQueueProposeEnqueue), params); err != nil {
				return nil, err
			}
			// One definition, two doors: the SAME RecordsAPI.Do the tool
			// path calls, the actor stamped on the service side from the
			// step's identity, never decoded from params.
			out, sentence, err := t.Records.Do(ctx, sess, mergeQueueProposeEnqueueTool, params)
			if err != nil {
				return nil, err
			}
			if sentence != "" {
				return nil, ErrBadParams{Err: errors.New(sentence)}
			}
			return out, nil
		},
	})
	return true
}

// mergeQueueAskKeys are the three keys this ONE ask door adds to the plane's
// identity law: `attended` (MQ14.3 fixes it FALSE for every committed ask —
// an agent's ask cannot buy a watched landing) and the base (MQ15 — the
// daemon resolves it at enqueue from the settings and the remote; a
// proposal that could name a base could aim a branch at one its author was
// never shown). mergequeue.enqueue legitimately takes all three, so they
// live here, on the twin, not on the shared list.
var mergeQueueAskKeys = []string{"attended", "base", "baseBranch"}

// rejectMergeQueueAskKeys is the twin's OWN raw-key gate — T8's shared
// rejectMergeQueueIdentityKeys cannot serve it verbatim, because the shared
// list bans `reason` (on the person's doors the reason an event carries is
// the driver's) while the ask MUST carry a reason: it is the desk's
// sentence about the ask, bounded 1..512 by the validator, never a column
// smuggled into an event. So the twin walks the shared list MINUS reason
// with T8's shared voice, then its own three keys with the ask's words.
func rejectMergeQueueAskKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range mergeQueueIdentityKeys {
		if key == "reason" {
			continue // the ask's own legitimate key — see above
		}
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the caller names what to act on, never who acted, what state the row is in, or what evidence it carries (S60 MQ13)",
				method, key)}
		}
	}
	for _, key := range mergeQueueAskKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — an ask names a repository, a branch and a reason; the attendedness is the kind's (always false, MQ14.3) and the base is the daemon's to resolve (MQ15)",
				method, key)}
		}
	}
	return nil
}
