package protocol

// ════════════════════════════════════════════════════════════════════════════
// policy_reads.go — S45 T7 (PM9): the policy read ANSWERS as four exported
// functions, so the records-catalog tools (internal/daemon's
// records_policy.go) and the socket handlers above answer through the ONE
// implementation each — the seed_status precedent (protocol.SeedStatusView,
// S43-extra T6) generalised from a mapping to the whole read. The socket
// handlers in policy.go and policy_detail.go are now thin wrappers over
// these; the bodies moved verbatim (same boundary delegations, same
// refusal classes, same sentences), so the wire behaviour is byte-identical
// and the protocol suite is the pin that says so. A tool result that
// drifted from its socket twin's field for field is TestPolicyToolReadsMatch
// SocketReads' failure, not a convention (internal/records' catalog pins
// the pairing; this file makes the pairing structurally unbreakable).
//
// Every function returns (result, error): the error is the SAME concrete
// *Error (or ErrBadParams) value the handler returned before the move, so
// the socket's wire shape is unchanged. The records service renders one as
// a refusal sentence — the tool door's presentation of the same fact.
//
// These four are READS (zero writes, D9 — the header above policy.go's own
// D9 section says why); the two PROPOSE twins are catalog actions end to
// end and live in policy_propose.go with the records service answering
// them. Nothing here reaches an authoring method: the reads read the store
// the gate reads and add nothing to it.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// PolicyListAnswer is policy.list's whole answer — filter built through the
// boundary's own parsers, ListDetailed as the single fail-closed read, the
// one Describe sentence per row, and WHICH empty it is stated by whether a
// filter was asked for. Body moved verbatim from handlePolicyList.
func PolicyListAnswer(ctx context.Context, store *policy.Store, p PolicyListParams, instant time.Time) (PolicyListResult, error) {
	// The filter, built through the boundary's own parsers — the same
	// delegation policy.why practises since S10 (SEC-046): the closed sets
	// refuse values by name and the refusals travel verbatim; the target
	// rebuilds through the one target parser, and an unparsable spelling is
	// the caller's -32602 naming it.
	var f policy.ListFilter
	if p.Type != "" {
		typ, rej := policy.ParsePolicyType(p.Type)
		if rej != nil {
			return PolicyListResult{}, policyRefusal(rej)
		}
		f.Type = typ
	}
	if p.Source != "" {
		src, rej := policy.ParseSource(p.Source)
		if rej != nil {
			return PolicyListResult{}, policyRefusal(rej)
		}
		f.Source = src
	}
	if p.Action != "" {
		act, rej := policy.ParseAction(p.Action)
		if rej != nil {
			return PolicyListResult{}, policyRefusal(rej)
		}
		f.Action = act
	}
	if p.Target != "" {
		// The presentation split is the CLI's own (`kind:id`, cut on the
		// FIRST colon — the same one cmd/aeon performs); whether the
		// halves mean anything is the boundary's parse, made there once.
		kindStr, idStr, _ := strings.Cut(p.Target, ":")
		// The alphabet gate is the write side's own (SEC-046's delegation)
		// and applies to the NAMED half only — `all-agents` carries no id
		// to gate, and ParseTarget below is what decides which kinds may
		// carry which shape of id.
		if idStr != "" {
			if rej := policy.CheckIdentifier("target id", idStr); rej != nil {
				return PolicyListResult{}, ErrBadParams{Err: fmt.Errorf("target %q is refused: %s", p.Target, rej.Reason)}
			}
		}
		target, rej := policy.ParseTarget(policy.TargetKind(kindStr), idStr)
		if rej != nil {
			return PolicyListResult{}, ErrBadParams{Err: fmt.Errorf("target %q is refused: %s", p.Target, rej.Reason)}
		}
		f.Target = &target
	}
	if p.Origin != "" {
		// Provenance is its own small closed set (S45 T12) — parsed by the
		// boundary, refused by name, the refusal travelling verbatim like
		// every other filter value's.
		origin, rej := policy.ParseOrigin(p.Origin)
		if rej != nil {
			return PolicyListResult{}, ErrBadParams{Err: fmt.Errorf("origin %q is refused: %s", p.Origin, rej.Reason)}
		}
		f.Origin = origin
	}
	f.IncludeExpired = p.IncludeExpired == nil || *p.IncludeExpired

	ps, rej := store.ListDetailed(ctx, instant)
	if rej != nil {
		return PolicyListResult{}, policyRefusal(rej)
	}

	views := make([]PolicyView, 0, len(ps))
	for _, l := range ps {
		if !f.Match(l.Policy, instant) || !f.MatchOrigin(l.Origin) {
			continue
		}
		views = append(views, PolicyView{
			ID:     l.ID,
			Type:   string(l.Type),
			Target: l.Target.String(),
			Action: string(l.Action),
			Source: string(l.Source),
			// Carried as the instant the row carries; nil is no expiry
			// (PL7) and renders as JSON null, which is a stated fact
			// rather than a gap.
			NotAfter: l.NotAfter,
			// The one sentence (D9), rendered by the engine beside the
			// spec it reads — this file adds nothing to it — and the
			// row's own written-at instant (S45 T2 additive).
			Describe:  policy.Describe(l.Policy),
			CreatedAt: l.CreatedAt,
			// The row's provenance (S45 T12 additive) — carried as the
			// column says, rendered by no one on the way out.
			Origin: l.Origin,
		})
	}
	res := PolicyListResult{Policies: views}
	if len(views) == 0 {
		// Which empty this is, stated by whether a filter was asked for
		// at all: the corpus's answer or the filter's answer, never both,
		// never neither.
		sentence := PolicyListEmptyStatement
		if !f.Empty() {
			sentence = PolicyListFilterEmptyStatement
		}
		res.Statement = &sentence
	}
	return res, nil
}

// PolicyWhyAnswer is policy.why's whole answer — the question validated by
// delegation (SEC-046), the clock a parameter (D5), List → Evaluate →
// Sentence with the recorder nowhere near (D9). Body moved verbatim from
// handlePolicyWhy; `fallback` is the handler's now() — absent `at` means
// now, present `at` must read as an RFC3339 instant or be refused, not
// ignored.
func PolicyWhyAnswer(ctx context.Context, store *policy.Store, p PolicyWhyParams, fallback time.Time, askUnmatched ...bool) (PolicyWhyResult, error) {
	instant := fallback
	if p.At != "" {
		at, perr := time.Parse(time.RFC3339Nano, p.At)
		if perr != nil {
			return PolicyWhyResult{}, ErrPolicyReadRefused.WithWhat(
				"policy.why cannot answer as of %q: it does not read as an RFC3339 instant "+
					"like 2026-10-14T23:59:59Z (%v)", p.At, perr)
		}
		instant = at
	}

	// A type outside the closed seven cannot even BE the question, so the
	// params refuse it by name (the CLI's flag-parser rule, carried to the
	// socket verbatim). Handing it to Evaluate instead would print D10's
	// answer — "has no evaluator yet" plus its promise that a registration
	// is coming — for a typo no slice will ever register. Unimplemented is
	// for a member whose evaluator has not landed.
	if _, rej := policy.ParsePolicyType(p.Type); rej != nil {
		return PolicyWhyResult{}, policyRefusal(rej)
	}

	// The identity dimensions are identifiers that reach the operator's
	// console through the sentence's subject (SEC-046): the boundary's own
	// gate, one call, not a second looser validator. The per-type VALUES
	// (tool, command, …) are deliberately NOT gated here — what a value
	// means is the registered evaluator's question, and a command name with
	// a space in it is a real question.
	for field, id := range map[string]string{
		"agent": p.Agent, "loop": p.Loop, "lane-type": p.LaneType, "mcp server": p.MCPServer,
	} {
		if id == "" {
			continue
		}
		if rej := policy.CheckIdentifier(field, id); rej != nil {
			return PolicyWhyResult{}, policyRefusal(rej)
		}
	}

	req := policy.Request{
		Type:      policy.PolicyType(p.Type),
		Agent:     p.Agent,
		Loop:      p.Loop,
		LaneType:  p.LaneType,
		MCPServer: p.MCPServer,
		Tool:      p.Tool,
		Command:   p.Command,
		Route:     p.Route,
		Effect:    p.Effect,
		Cost:      p.Cost,
		Skill:     p.Skill,
		Trigger:   p.Trigger,
	}

	// List → Evaluate → Sentence. The recorder is not on this path (D9):
	// what `why` refuses has not happened, and the audit records refusals
	// that happened.
	ps, rej := store.List(ctx, instant)
	if rej != nil {
		// The set cannot be read; there is NOTHING to explain — the
		// boundary's own reason travels, and no verdict is invented.
		return PolicyWhyResult{}, policyRefusal(rej)
	}
	// The answer must agree with enforcement: under the default setting a tool or command no rule covers
	// is ASKED, so `why` says asked, not refused.
	var v policy.Verdict
	if len(askUnmatched) > 0 && askUnmatched[0] {
		v = policy.EvaluateAskingWhenUnmatched(ps, req, instant)
	} else {
		v = policy.Evaluate(ps, req, instant)
	}
	res := PolicyWhyResult{Allowed: v.Allowed, Sentence: v.Sentence(), Unimplemented: v.Unimplemented,
		Asked: v.Asked, DefaultDeny: v.DefaultDeny}
	// S45 T8: the four verdict shapes ride as FIELDS (see PolicyWhyResult),
	// and the two operator notes this answer exists to let a door print are
	// answered HERE as data, so the CLI composes each note in exactly one
	// place from fields, never by re-running the engine or parsing prose
	// (SEC-033's rule, carried onto the second door).
	//
	// DEF-038's half: the question was asked on identity dimensions no
	// runtime decision carries.
	res.Unmatchable = p.Loop != "" || p.LaneType != "" || p.MCPServer != ""
	// SEC-050's half, the pair the CLI's composition note rides: the
	// COMMAND question allowed, and the TOOL question — asked over the very
	// same set at the very same instant — refused by a rule. Both
	// questions' verdicts are engine outputs; By/ByTarget name the refusing
	// rule only when the whole documented pair holds, so a door prints the
	// note when By speaks and never re-derives the condition.
	if v.Allowed && !v.Asked && req.Type == policy.CommandAccess &&
		strings.TrimSpace(req.Command) != "" && strings.TrimSpace(req.Tool) == "bash" {
		toolReq := req
		toolReq.Type = policy.ToolAccess
		toolReq.Command = ""
		tv := policy.Evaluate(ps, toolReq, instant)
		if tv.By != nil && !tv.Allowed && !tv.Asked && !tv.DefaultDeny && !tv.Unimplemented {
			res.By = tv.By.ID
			res.ByTarget = tv.By.Target.String()
		}
	}
	return res, nil
}

// PolicyInForceAnswer is policy.inForce's whole answer — one party, the
// boundary's InForceFor, and the three states un-collapsed with Render()
// verbatim (D14). Body moved verbatim from handlePolicyInForce.
func PolicyInForceAnswer(ctx context.Context, store *policy.Store, p PolicyInForceParams, instant time.Time) (PolicyInForceResult, error) {
	switch {
	case p.Agent != "" && p.Loop != "":
		return PolicyInForceResult{}, ErrPolicyReadRefused.WithWhat(
			"policy.inForce takes `agent` or `loop`, not both — a target is one party per answer")
	case p.Agent == "" && p.Loop == "":
		return PolicyInForceResult{}, ErrPolicyReadRefused.WithWhat(
			"policy.inForce needs `agent` or `loop` — \"in force for whom\" is the question")
	}

	kind := policy.AgentKind
	id := p.Agent
	field := "agent"
	if p.Loop != "" {
		kind, id, field = policy.LoopKind, p.Loop, "loop"
	}
	// SEC-046: the name passes the boundary's own identifier gate before it
	// is echoed by Render() into the "acknowledged by" position. ParseTarget
	// (InForceFor re-runs it, fail-closed) owns the kind/id shape; the
	// alphabet is this gate's, and there is only one of each.
	if rej := policy.CheckIdentifier(field, id); rej != nil {
		return PolicyInForceResult{}, policyRefusal(rej)
	}
	target, rej := policy.ParseTarget(kind, id)
	if rej != nil {
		return PolicyInForceResult{}, policyRefusal(rej)
	}

	answers, rej := store.InForceFor(ctx, target, instant)
	if rej != nil {
		// No partial answer here: a half-read acceptance table would report
		// "unacknowledged" for rules whose records the read lost.
		return PolicyInForceResult{}, policyRefusal(rej)
	}
	rows := make([]PolicyInForceRow, 0, len(answers))
	for _, a := range answers {
		rows = append(rows, PolicyInForceRow{
			// S45 T8: the type rides beside the id so the CLI's in-force
			// column prints the SAME line through either door (PM4).
			Type:     string(a.Policy.Type),
			PolicyID: a.Policy.ID,
			State:    string(a.State),
			Render:   a.Render(),
		})
	}
	return PolicyInForceResult{Answers: rows}, nil
}

// PolicyGetAnswer is policy.get's whole answer — ONE read path for every
// policy answer (the same fail-closed re-decode policy.list runs, so a row
// that escaped the write boundary refuses get as loudly as it refuses the
// list), the typed unknown-id refusal naming the nearest ids, and the
// detail assembly shared with the authoring results via
// PolicyDetailFromListed (T4's D9 one-definition rule). Body moved verbatim
// from handlePolicyGet.
func PolicyGetAnswer(ctx context.Context, store *policy.Store, p PolicyGetParams, instant time.Time) (PolicyGetResult, error) {
	rows, rej := store.ListDetailed(ctx, instant)
	if rej != nil {
		return PolicyGetResult{}, policyRefusal(rej)
	}

	var found *policy.Listed
	for i := range rows {
		if rows[i].ID == p.ID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		// The typed refusal: it names the id it could not find AND the
		// nearest ids it did (up to three), so a mistyped guess turns into
		// a pick instead of a re-guess.
		return PolicyGetResult{}, ErrPolicyUnknown.WithWhat(
			"the policy.get read named %q, which is not a policy this store carries; the nearest ids are: %s",
			p.ID, nearestPolicyIDs(p.ID, rows))
	}

	// S45 T4: the DETAIL assembly is ONE definition — PolicyDetailFromListed
	// (policy_authoring.go), shared with the authoring results so what the
	// add/remove doors return as the row is exactly what policy.get would
	// return for it (D9's one-definition rule, carried onto the write
	// doors). Behaviour unchanged, byte for byte, from T2's body.
	detail, drej := PolicyDetailFromListed(ctx, store, *found)
	if drej != nil {
		return PolicyGetResult{}, policyRefusal(drej)
	}

	return PolicyGetResult{Policy: detail}, nil
}
