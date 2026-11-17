package narrow

import "encoding/json"

// trigger.go — the trigger's ONE param transform, shared by both mountings.
//
// # A triggered loop is unattended, pinned (D9, G14)
//
// Attendedness is declared at start and pinned; it is never inferred from
// whether a client is connected — that was G14's ruling, and the narrow
// surface honours it by REFUSING to let anyone claim it: `attended` is
// forced to false here, on both mountings, through this one function. A
// CI job is not present in the sense a gate needs, and a poll every ten
// seconds does not make it so (the polling client's test is
// TestAttendednessCannotBeInferredFromPolling). A caller that sends
// `attended: true` is not trusted, not warned-and-kept, and not
// answered-with-an-error — the field is overwritten, because the claim
// would be about a human the transport cannot attest to, and the answer
// is the same for everyone.
//
// # allowOutward rides through, and it is the only outward lever (D10, O2)
//
// The trigger passes `allowOutward` through mechanically — it is a field
// on loop.start's own params, read from the loop's pinned row at the
// decision site (internal/action.Resolve), never from the caller again.
// There is no key-level, caller-level or configuration version of it
// anywhere on this surface (TestNoKeyLevelGrantExists greps for exactly
// that absence): a standing outward grant is a grant whose theft is
// unbounded, which is why it is one field, in one body, at the one
// moment someone is deciding what THIS run may do.

// triggerForcedAttended is the pinned value; a map key's presence is what
// the daemon reads as "the caller declared" — so forcing means WRITING it,
// not merely deleting the caller's.
const triggerForcedAttended = false

// TriggerParamsFor returns the params a narrow trigger may send: the
// caller's own fields (blueprint, goal, workspaceRoot, allowOutward, …)
// with attendedness NOT in the caller's hands. Input is never mutated —
// the caller's map may be the parsed body another assertion reads.
func TriggerParamsFor(params map[string]any) map[string]any {
	out := make(map[string]any, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	out["attended"] = triggerForcedAttended
	return out
}

// TriggerParamsJSON is the stdio twin (N18: two mountings, one transform —
// both call these functions, neither has its own copy). A body that is not
// an object is passed through untouched: it is not a start at all, and
// the daemon's own validators answer shapes they did not ask narrow to
// pre-judge (the dispatcher says so at the source).
func TriggerParamsJSON(args json.RawMessage) json.RawMessage {
	var params map[string]any
	if err := json.Unmarshal(args, &params); err != nil {
		return args
	}
	raw, err := json.Marshal(TriggerParamsFor(params))
	if err != nil {
		return args
	}
	return raw
}
