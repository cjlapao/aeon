package main

// ════════════════════════════════════════════════════════════════════════════
// `aeon policy` — the operator's door, and `why`.
//
// # The door, and its offline twin (PL15 as amended by S45, PM4)
//
// Authoring is a HUMAN DOOR: the three authoring methods S45 T4 put on the
// table ride the daemon's socket (the 0600 endpoint with peer credentials
// — the surface a person holds), and this verb DIALS them. The historical R4/R6
// ruling — "authoring may never reach a surface a non-person reaches" —
// still stands, and is now proven positively: the authoring half is absent
// from the narrow HTTP/MCP-stdio projection, from the agent-services
// enumeration, from the tool registry and from the records catalog
// (`cmd/aeon/policy_amend_test.go`). Reading a verdict was always fine over
// the socket; what changed is that authoring reaches the PERSON's socket
// too. Inert proposal rows are not the corpus (S35 T6): `proposal.create`
// writes the `proposal` table over the socket, and that row constrains
// nothing until `proposal commit` applies it through the one desk
// implementation — the distinction TestOnlyProposalIsSocketWritable
// (policy_test.go) keeps honest.
//
// # Why this verb also opens the store directly — and only when nobody is home
//
// S45 T8: every verb here is a CLIENT of the socket methods
// (`policy_client.go` selects the door; the socket answers when the daemon
// is up). The direct store open below is the OFFLINE FALLBACK only — the
// three real cases with no daemon (first-run seeding, a broken daemon, make
// tooling) — and it runs the SAME internal/desk and internal/policy
// functions the daemon's services call, after saying so on stderr. This is
// the second use of the justified two-access-path exception `db info` and
// `debug hammer` already use (see the note in store_verbs.go, where this
// verb's justification is recorded beside it). When no daemon answers,
// "go through the socket" is not a shortcut to avoid — there is no socket
// to speak — and the next daemon read sees the write immediately because
// N8 forbids any cache: the store is re-read per decision. A write through
// a second handle is safe the same way hammer's is: WAL, with the writer
// taking its lock at BEGIN (_txlock=immediate), so a policy add waits
// behind the daemon's write rather than failing with SQLITE_BUSY on a lock
// upgrade.
//
// # The CLI re-validates NOTHING (the drift rule)
//
// Every rule — the closed sets, the identifier policy, PL1 authorship, the
// per-type spec validators — is T6's write boundary's, enforced in
// internal/policy/store.go and nowhere else. This file presents those
// refusals verbatim and never re-checks: a second validator is a second place
// for the rule to drift, and the loop already killed one slice for growing
// one. Where a flag value cannot even be a policy (a non-RFC3339 --at, a
// --type outside the closed set), the flag parser refuses its OWN input —
// input parsing, not policy validation; where the closed set already has a
// parser (ParsePolicyType), the flag calls THAT one call, never a copy.
// The one-call property ("add does not re-validate") is pinned by a spy test
// counting exactly one repo round-trip per accepted add.
//
// # One formatter, printed whole (D9)
//
// `why` prints policy.Verdict.Sentence(); `in-force` prints
// InForceAnswer.Render(). Both are printed VERBATIM — no re-casing, no status
// column that replaces the sentence, no truncation (byte-equality pinned by
// TestWhyRendersTheSameSentenceAsTheRuntimeRefusal and the suffix assertion
// beside TestInForceAnswersPerTarget). Any extra operator guidance this verb
// adds is ADDITIVE and on its own line, and every added line is a fixed
// constant, never assembled from stored or requested text.
//
// # No credential flag, ever (AGENTS §4/§5)
//
// Every flag is identity or scalar. There is nothing here that accepts a
// secret — and a note in `--help` because `--spec` is free-form JSON and
// somebody will eventually put something in it that should not sit in shell
// history: this is a local single-user store, the spec is not a secret store.
//
// # The audit's third state (DEF-041/SEC-043, three-state honesty for the log)
//
// `events` may print ZERO rows while refusals happened: a refusal whose audit
// row the record boundary refused still STANDS (enforcement never waits on
// the audit). So an empty listing is never allowed to read as "nothing was
// refused" — the verb says so explicitly, and probes whether the runtime's
// ad-hoc actor identity can be an audit subject AT ALL (through the boundary's
// own parseSubject, by way of ReportEvents — no second validator). When it
// cannot, every ad-hoc refusal is unaudited by construction and the verb says
// THAT too. The third state is `unaudited`, never `nothing`.
//
// # Display boundary (SEC-033, SEC-046)
//
// The sentences printed by why/in-force come from the one formatters and are
// printed raw (D9 — sanitising them HERE would be the second formatter the
// slice exists to forbid). The real fix for a forged row smuggling ANSI or
// BiDi Cf controls into a terminal is the decode chokepoint in
// internal/policy (open finding SEC-046). What THIS file owns: every line it
// assembles itself quotes any caller-supplied value with %q, so a crafted
// flag value cannot inject escape bytes through this verb's own prose.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// cmdPolicy dispatches the seven subcommands, shaped exactly like cmdRoutes.
func cmdPolicy(w io.Writer, args []string) error {
	if len(args) == 0 {
		printPolicyUsage(w)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdPolicyList(w, args[1:])
	case "get":
		return cmdPolicyGet(w, args[1:])
	case "vocabulary":
		return cmdPolicyVocabulary(w, args[1:])
	case "changes":
		return cmdPolicyChanges(w, args[1:])
	case "add":
		return cmdPolicyAdd(w, args[1:])
	case "remove":
		return cmdPolicyRemove(w, args[1:])
	case "seed-corpus":
		return cmdPolicySeedCorpus(w, args[1:])
	case "why":
		return cmdPolicyWhy(w, args[1:])
	case "in-force":
		return cmdPolicyInForce(w, args[1:])
	case "events":
		return cmdPolicyEvents(w, args[1:])
	default:
		printPolicyUsage(w)
		return fmt.Errorf("unknown `policy` subcommand %q", args[0])
	}
}

// printPolicyUsage is the verb's own account of what it can author. It is a
// fixed string for the same reason every operator line here is one — a line
// assembled at run time is a line that can say something the engine does not
// do — and the test beside it (TestAddHelpAndUsageNameEveryAction) pins it
// against policy.AllActions() rather than against a second literal list, so
// when the closed set widens the help fails LOUDLY instead of quietly lying
// about there being two actions, which is exactly the drift S11 left behind
// when migration 0019 admitted `ask`.
func printPolicyUsage(w io.Writer) {
	fmt.Fprint(w, policyUsage())
}

// policyUsage is that same fixed string, reachable as a value so `add`'s
// --help prints it (fs.Usage) and the test pins it — one text, two surfaces,
// no second copy to drift.
func policyUsage() string {
	return `aeon policy — the policy engine (authoring is a human door: the socket's, with this verb as its client — PM4)

  policy list [--type TYPE]
  policy get --id ID
  policy vocabulary
  policy changes [--policy ID] [--limit N]
  policy add --type TYPE --id ID --target KIND[:ID] --action ACTION
             --spec JSON [--tool NAME | --command PATTERN] [--source SRC] [--not-after RFC3339]
  policy remove --id ID
  policy seed-corpus --agent NAME [--agent NAME]... [--dry-run]
  policy why --agent NAME [--tool NAME] [--type TYPE] [--loop SLUG]
             [--lane-type NAME] [--mcp NAME] [--command CMD] [--route ID]
             [--effect CLASS] [--cost TOKENS] [--skill NAME] [--trigger NAME]
             [--at RFC3339]
  policy in-force (--agent NAME | --loop SLUG)
  policy events [--subject KIND:ID] [--depth N]

Actions are allow, deny or ask. Only an operator may author an allow (PL1) —
the write boundary refuses it from any other source; a deny and an ask come
from any source, because both take autonomy away rather than grant it. An ask
is not a permission: it is a command routed to a human (S11), and the engine
folds the three as deny over ask over allow.

Sources are operator|agent|mcp_server|import; default --source is operator,
overridable ON PURPOSE so the PL1 refusal is demonstrable (the demo authors
--source agent to show it). Targets are agent:NAME, loop:SLUG, lane-type:NAME,
mcp:NAME or all-agents.

--tool is the tool_access spec's toolName sugar; --command is the
command_access spec's command sugar, and it writes the spec's state as the
row's action restated — the only spelling the boundary accepts. Say it once:
--spec beside either sugar is refused, never precedence-resolved.

seed-corpus turns this repository's own agent rules — the committed corpus —
into stored policy for the agents you name: command_access rows authored as
operator through the write boundary, one authored audit event per rule
written. A rule already stored is LEFT ALONE (reported unchanged), so
re-running seeds nothing twice and reverts nothing an operator edited. It
runs when you type it and never otherwise — nothing seeds automatically (D4).
--dry-run prints the per-agent counts it WOULD write and writes nothing.

--spec is free-form JSON. It is NOT a secret store: this is a local
single-user file and a spec value sits in your shell history. There is
deliberately no credential flag anywhere in this verb.
`
}

// policyBoundary is how the verbs reach the boundary. It is a variable for
// ONE reason: tests point it at a temp store (the isolation the subprocess
// tests get from XDG_STATE_HOME) or at a spy repo. Production wires it to
// the direct open below; nothing else reassigns it.
var policyBoundary = openPolicyBoundaryDirectly

// openPolicyBoundaryDirectly opens the store DIRECTLY (the justified
// two-access-path exception, documented beside store_verbs.go's note) and
// wraps it in the policy boundary over the RepoFuncs bridge.
func openPolicyBoundaryDirectly() (*policy.Store, func(), error) {
	s, err := openStoreDirectly(false)
	if err != nil {
		return nil, nil, err
	}
	return policy.NewStore(policyRepo(s)), func() { _ = s.Close() }, nil
}

// deskBoundary adapts the policy write boundary to the desk's authoring seam
// (internal/desk/boundary.go). It lives in THIS file for exactly one reason and
// it adds nothing to what it forwards: the PL15 guard
// (TestNoPolicyAuthoringReachesTheNarrowSurface) admits this file — and only
// this file — as the place the boundary's authoring and its authored audit
// record are called. So the forwarding calls stay here while the DECISION to
// make them lives in the desk; T6 rewrites that whitelist positively (PM10) and
// this adapter moves with it, and until then the guard's claim is exactly as
// true as it was: one door, reached from wiring the guard can see.
//
// Nothing here caches, re-validates or opens a store handle. Every call still
// clears internal/policy's boundary — the closed sets, PL1's authorship check,
// the per-type validators — because this type does not contain a rule.
type deskBoundary struct{ s *policy.Store }

func (b deskBoundary) AddPolicy(ctx context.Context, p policy.Policy, now time.Time) *policy.Reject {
	return b.s.Add(ctx, p, now)
}

func (b deskBoundary) RemovePolicy(ctx context.Context, id string) (bool, error) {
	return b.s.Remove(ctx, id)
}

func (b deskBoundary) ListPolicies(ctx context.Context, at time.Time) ([]policy.Policy, *policy.Reject) {
	return b.s.List(ctx, at)
}

func (b deskBoundary) RecordAuthoredPolicy(ctx context.Context, subject, policyID string, at time.Time) *policy.Reject {
	return b.s.RecordAuthored(ctx, subject, policyID, at)
}

// openDeskBoundary is the desk's opener over this file's own `policyBoundary`
// variable, read at CALL time rather than captured at init — which is what lets
// the CLI's fixtures keep pointing every verb at a temp store or a spy repo
// while one registry serves both doors.
func openDeskBoundary() (desk.Boundary, func(), error) {
	s, closeFn, err := policyBoundary()
	if err != nil {
		return nil, nil, err
	}
	return deskBoundary{s}, closeFn, nil
}

// policyRepo is the RepoFuncs bridge for this wiring layer — the same boring,
// total conversion daemon/policy_gate.go's newPolicyRepo makes (I2 keeps the
// SQL in internal/store and the rule in internal/policy; Go method identity
// means the two identically-shaped Row structs meet only in wiring code).
// It is duplicated rather than shared because newPolicyRepo is unexported and
// the CLI must not reach into the daemon's internals to get a store handle
// across a package line it has no reason to import.
func policyRepo(s *store.Store) policy.RepoFuncs {
	return policy.RepoFuncs{
		Add: func(ctx context.Context, r policy.Row) error {
			return s.PolicyAdd(ctx, store.PolicyRow{
				ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
				Action: r.Action, Source: r.Source, Subject: r.Subject,
				SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
			})
		},
		Remove: func(ctx context.Context, id string) (bool, error) {
			return s.PolicyRemove(ctx, id)
		},
		ListAll: func(ctx context.Context) ([]policy.Row, error) {
			rows, err := s.PolicyListAll(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]policy.Row, 0, len(rows))
			for _, r := range rows {
				out = append(out, policy.Row{
					ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
					Action: r.Action, Source: r.Source, Subject: r.Subject,
					SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
					Origin: r.Origin,
				})
			}
			return out, nil
		},
		// AddTx and RemoveTx carry the seed door through this wiring too
		// (S45 T12): the same transaction the direct door's seed engine
		// opened passes through the policy-side Tx seam untouched, and the
		// assertion below is the only unwrap. A wiring without them simply
		// has no seed door — AddInTx refuses it by name; the plain CLI
		// verbs never travel this path.
		AddTx: func(ctx context.Context, tx policy.Tx, r policy.Row) error {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				return fmt.Errorf("the policy seed door was handed a transaction this wiring did not open")
			}
			return s.PolicyAddTx(ctx, sqlTx, store.PolicyRow{
				ID: r.ID, Type: r.Type, TargetKind: r.TargetKind, TargetID: r.TargetID,
				Action: r.Action, Source: r.Source, Subject: r.Subject,
				SpecJSON: r.SpecJSON, NotAfter: r.NotAfter, CreatedAt: r.CreatedAt,
				Origin: r.Origin,
			})
		},
		RemoveTx: func(ctx context.Context, tx policy.Tx, id string) (bool, error) {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				return false, fmt.Errorf("the policy seed door was handed a transaction this wiring did not open")
			}
			return s.PolicyDeleteTx(ctx, sqlTx, id)
		},
		EventInsertTx: func(ctx context.Context, tx policy.Tx, kind, subject, policyID, detail string, at time.Time) error {
			sqlTx, ok := tx.(*sql.Tx)
			if !ok {
				return fmt.Errorf("the policy seed door was handed a transaction this wiring did not open")
			}
			return s.PolicyEventInsertTx(ctx, sqlTx, kind, subject, policyID, detail, at)
		},
		AcceptanceRecord: func(ctx context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
			return s.PolicyAcceptanceRecord(ctx, policyID, targetKind, targetID, acceptedBy, at)
		},
		AcceptanceList: func(ctx context.Context, targetKind, targetID string) ([]policy.AcceptanceRow, error) {
			rows, err := s.PolicyAcceptanceListFor(ctx, targetKind, targetID)
			if err != nil {
				return nil, err
			}
			out := make([]policy.AcceptanceRow, 0, len(rows))
			for _, r := range rows {
				out = append(out, policy.AcceptanceRow{
					PolicyID: r.PolicyID, TargetKind: r.TargetKind, TargetID: r.TargetID,
					AcceptedBy: r.AcceptedBy, AcceptedAt: r.AcceptedAt,
				})
			}
			return out, nil
		},
		// EventInsert reaches ONLY the boundary's sole writer (record.go);
		// the CLI itself never has a reason to call it — why records nothing.
		EventInsert: s.PolicyEventInsert,
		EventQuery: func(ctx context.Context, subject string, limit int) ([]policy.EventRow, error) {
			rows, err := s.PolicyEventQuery(ctx, subject, limit)
			if err != nil {
				return nil, err
			}
			out := make([]policy.EventRow, 0, len(rows))
			for _, r := range rows {
				out = append(out, policy.EventRow{
					ID: r.ID, PolicyID: r.PolicyID, SubjectKind: r.SubjectKind, SubjectID: r.SubjectID,
					Outcome: r.Outcome, Detail: r.Detail, At: r.At,
				})
			}
			return out, nil
		},
		EventTrim: func(ctx context.Context, olderThan time.Time) (int, error) {
			return s.PolicyEventTrim(ctx, olderThan)
		},
		EventStats: func(ctx context.Context, subject string) (int, time.Time, error) {
			// The subject travels with the read (F-13): the depth the CLI
			// footer prints is the listing's, not the table's.
			return s.PolicyEventStats(ctx, subject)
		},
	}
}

// --- the pinned operator lines -------------------------------------------------
//
// Fixed constants so no surface "tidies" one into a synonym and so the
// honesty lines cannot be assembled from caller- or store-supplied text.

// emptyPolicyListLine is D4: a guard whose empty state reads as "fine" is the
// one failure this slice cannot have. The sentence, not `0 policies`.
const emptyPolicyListLine = "no policies. default is DENY (PL5) — nothing is granted until something grants it."

// The authoring-time operator warnings moved to internal/desk (S45 T4,
// PM2/PM4): the socket door prints the SAME bytes through the SAME functions
// (a second copy of a warning sentence is the second formatter D9 forbids).
// The names stay here as ONE-LINE FORWARDERS — the CLI flow and the pinned
// tests (policy_test.go's SEC-014/DEF-038 cases) call these spellings;
// TestNoLocalCopyOfMovedFunctions pins that the DEFINITION lives in the desk.

// addUnmatchableTargetWarn is DEF-038's operator-side signal at authoring:
// target kinds no runtime decision carries until S18/S20 are stored inert.
func addUnmatchableTargetWarn(kind string) string {
	return desk.UnmatchableTargetWarn(kind)
}

// broadAllowWarn renders SEC-014's line over a measured coverage (the corpus
// is sized by broadAllowWarning; this is the sentence, defined once in the
// desk).
func broadAllowWarn(pattern string, matched, total int) string {
	return desk.BroadAllowWarn(pattern, matched, total)
}

// broadAllowWarning decides whether the row being authored earns SEC-014's
// stderr line (warn, never refuse; the desk owns the measurement).
func broadAllowWarning(action policy.Action, specJSON []byte) (string, bool) {
	return desk.BroadAllowWarning(action, specJSON)
}

// commandPatternFromSpec digs the "command" string out of the merged spec
// bytes — the warning probe, not a validator (the desk owns it).
func commandPatternFromSpec(specJSON []byte) string {
	return desk.CommandPatternFromSpec(specJSON)
}

// whyUnmatchableIdentitiesWarn is DEF-038's `why` half: an explanation asked
// with identity fields the runtime never fills must not read as if matching
// worked on those dimensions.
const whyUnmatchableIdentitiesWarn = "warning  no runtime decision carries a loop, lane-type or mcp identity " +
	"until S18/S20 — policies targeting those kinds cannot match a running step today; the answer above is " +
	"about the question you asked, not about what a step would see"

// whyUnimplementedNote rides ONLY the Verdict.Unimplemented field (never a
// prefix match on the shared "refused:" text, SEC-033 class): "nobody can
// answer yet" sends an operator somewhere different from "a rule refused you".
const whyUnimplementedNote = "note  the binary cannot decide this type yet (D10) — that is not a rule " +
	"refusing you; the answer changes when the slice registering this type's evaluator lands"

// whyToolDenyOutranked is SEC-050's visibility fix: the engine's type filter
// means a bash call carrying a command asks the COMMAND question, and a
// tool_access deny for bash is inert for it — a coexisting command allow
// therefore runs despite the tool deny. That is the documented composition
// (S10: "types are other questions' answers"), not a defect; what was missing
// was that the operator reading truth in `why` could not SEE it. This line
// rides only the field-tested pair (command allowed AND a tool deny coexists);
// it names the deny rule by ID and renders its target through Target.String,
// the product's one target renderer. It never re-renders a verdict sentence.
func whyToolDenyOutranked(denyID, denyTarget string) string {
	return fmt.Sprintf("note  the tool_access rule %q (target %s) also refuses bash — with a command the "+
		"command question governs and that tool deny does not bind (SEC-050); to stop bash entirely, deny "+
		"the command shapes as well", denyID, denyTarget)
}

// eventsEmptyHonesty and the unaudited line are the DEF-041 third state.
const eventsEmptyHonesty = "honesty  an empty events log is NOT \"nothing was refused\": a refusal whose " +
	"audit row the record boundary refused still stands, and only the daemon log names those refusals"

// eventsFilteredEmptyHonesty is F-14's half of the audit's third state: zero rows UNDER A FILTER is a
// filter answer, not a claim about the log — and after T22 the footer's depth agrees with it, which is
// precisely why the sentence is still needed. An operator who reads "depth 0" and stops there has been
// told the truth about the wrong question. The subject is caller-supplied text, so it rides %q for
// the same reason eventsUnauditedAdHoc does (SEC-033's class).
func eventsFilteredEmptyHonesty(subject string) string {
	return "honesty  no policy_event rows matched " + fmt.Sprintf("%q", subject) +
		" — that is the filter's answer, not the log's: run aeon policy events without --subject " +
		"for every subject"
}

func eventsUnauditedAdHoc(actor string) string {
	// %q, not %s: the actor is caller-supplied text on the test path and the
	// runtime mint in production; escape bytes must not ride this line (SEC-033).
	return "unaudited  the ad-hoc step actor " + fmt.Sprintf("%q", actor) +
		" cannot be an audit subject (the identifier policy refuses it) — every refusal of an " +
		"ad-hoc step stands with NO policy_event row"
}

// --- list ------------------------------------------------------------------------

func cmdPolicyList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy list", flag.ContinueOnError)
	fs.SetOutput(w)
	typ := fs.String("type", "", "list only this policy type (one of the seven); omitted lists every type")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	// --type is flag input: a value outside the closed set cannot even name a
	// filter, so the flag parser refuses its OWN input — one call to the
	// boundary's single parser (delegation, not a second validator; the
	// header's input-parsing rule, the same class as why's --type).
	var only policy.PolicyType
	if strings.TrimSpace(*typ) != "" {
		parsed, rej := policy.ParsePolicyType(*typ)
		if rej != nil {
			return fmt.Errorf("%s", rej.Reason)
		}
		only = parsed
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, err := door.list(context.Background(), time.Now())
	if err != nil {
		return err
	}
	return printPolicyList(w, res, only)
}

// printPolicyList prints ONE door's answer — the socket's or the offline
// door's, both carrying the protocol list answer, so the printed bytes do
// not depend on which door spoke (PM4's parity claim; T6's store reads
// arrive through PolicyListAnswer on the direct side). Every stored policy
// rides through, expired rows included (D6: expiry is Evaluate's decision,
// and an audit that hides history is an audit that lies). It re-decodes
// nothing itself: the answer's read already re-decoded every row through
// the closed set and refused the whole list loudly if a row escaped it; that
// refusal arrives as the door's error and prints verbatim.
//
// `only` is a PRESENTATION selection, not a second read: the rows still come
// from the one uncached answer read, and the filter runs on what it returned
// (an operator asking for the command_access rows the seeder wrote asks for
// a type, not a query). Under a filter, an empty listing is the FILTER's
// answer — never the whole-table default-deny sentence, which is a claim
// about every row and belongs to the unfiltered empty alone (the F-14 class,
// applied to the listing).
func printPolicyList(w io.Writer, res protocol.PolicyListResult, only policy.PolicyType) error {
	shown := res.Policies
	if only != "" {
		shown = make([]protocol.PolicyView, 0, len(shown))
		for _, p := range res.Policies {
			if policy.PolicyType(p.Type) == only {
				shown = append(shown, p)
			}
		}
	}
	if len(shown) == 0 {
		if only != "" {
			fmt.Fprintln(w, listFilteredEmptyLine(string(only)))
			return nil
		}
		if res.Statement != nil {
			fmt.Fprintln(w, *res.Statement) // the answer's own D4 sentence (pinned equal to the CLI's line)
			return nil
		}
		fmt.Fprintln(w, emptyPolicyListLine)
		return nil
	}
	for _, p := range shown {
		expiry := "no expiry"
		if p.NotAfter != nil {
			expiry = p.NotAfter.UTC().Format(time.RFC3339)
		}
		// The target renders through Target.String() on the answer side —
		// the one target rendering in the product (verdict.go delegates to
		// it for the same reason; a second one is the drift this rule
		// exists to kill).
		fmt.Fprintf(w, "%-24s %-14s %-28s %-6s %-12s %s\n",
			p.ID, p.Type, p.Target, p.Action, p.Source, expiry)
	}
	return nil
}

// listFilteredEmptyLine is D4's discipline for the FILTERED empty listing
// (the F-14 class beside eventsFilteredEmptyHonesty's): zero rows under
// --type is the filter's answer, not the table's — and it must never be the
// whole-table default-deny sentence, which would claim an empty store the
// filter never looked at. The type is caller-supplied text, so it rides %q
// (SEC-033's class) even though the flag parser has already confined it to
// the closed set.
func listFilteredEmptyLine(typ string) string {
	return "no policies of type " + fmt.Sprintf("%q", typ) + " — that is the filter's answer, not the " +
		"table's: run `aeon policy list` without --type for every policy"
}

// --- add -------------------------------------------------------------------------

// cmdPolicyAdd is the only door a policy has into the product (PL15). The
// flags are identity and scalars; the three spec sugars — --spec, --tool,
// --command — are the only places a value is assembled rather than passed, and
// each of them refuses its own construction error at the flag (the header's
// input-parsing rule) so the boundary below sees one spec, said once.
func cmdPolicyAdd(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy add", flag.ContinueOnError)
	fs.SetOutput(w)
	// `add --help` prints the verb's own usage — which names all three
	// actions — beside the flag list, and THEN the closed sets as the
	// compiled engine answers for them (S45 T8, PM6): the value lines are
	// generated from policy.Vocabulary() — the socket's policy.vocabulary
	// when a daemon answers, the same function's offline answer when none
	// does — never restated here. The flag docs already say "allow, deny
	// or ask"; this makes the help carry the full account (PL1, ask-as-question)
	// from the SAME string the bare verb prints, not a second copy.
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), policyUsage())
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), vocabularyHelp(policyVocabularyForHelp()))
	}
	typ := fs.String("type", "", "the policy type: tool_access|command_access|route_access|effect_class|cost_cap|skill_access|trigger_access")
	id := fs.String("id", "", "the policy id — what every verdict, event and removal names")
	target := fs.String("target", "", "the target: agent:NAME, loop:SLUG, lane-type:NAME, mcp:NAME, or all-agents")
	action := fs.String("action", "", "allow, deny or ask. An allow may only be authored by an operator (PL1) — the write boundary refuses it otherwise; deny and ask come from any source. An ask is a question for a human, never a permission")
	source := fs.String("source", string(policy.Operator),
		"who authored this: operator|agent|mcp_server|import. Overridable ON PURPOSE: the demo authors --source agent to show the PL1 refusal")
	spec := fs.String("spec", "", "the spec as JSON; the shape depends on --type. NOT a secret store — a spec value sits in shell history")
	tool := fs.String("tool", "", "sugar for the ToolAccess spec's toolName (the alternative is the spec carrying toolName itself)")
	command := fs.String("command", "", "sugar for the CommandAccess spec's command pattern AND its state (the state is the --action restated, which is the only value the boundary accepts); the alternative is the spec carrying both")
	notAfter := fs.String("not-after", "", "expiry as an RFC3339 instant. Omitted means NO EXPIRY (PL7) — never a far-future sentinel")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	// Flag hygiene, not policy validation: each sugar writes the spec field of
	// ONE type, so the pairings that cannot mean a row are refused by the flag
	// parser that owns them — the same class as `in-force`'s one-target rule and
	// --not-after's parse. What a spec MEANS stays the boundary's question, made
	// there once; nothing here pre-checks a pattern, an identifier or a source.
	if *command != "" {
		if *tool != "" {
			return fmt.Errorf("`policy add` takes --tool or --command, not both — --tool writes toolName " +
				"(a tool_access spec) and --command writes command (a command_access spec); one row has one type")
		}
		if policy.PolicyType(*typ) != policy.CommandAccess {
			return fmt.Errorf(`--command is the sugar for a command_access spec's "command" field; --type %q `+
				"has no such field — the sugar that belongs to that type is --tool, or say the spec once with --spec", *typ)
		}
	}

	specJSON, err := mergeSpecToolName(*spec, *tool)
	if err != nil {
		return err
	}
	commandJSON, err := mergeSpecCommandName(*spec, *command, *action)
	if err != nil {
		return err
	}
	if commandJSON != nil {
		specJSON = commandJSON
	}

	var notAfterAt *time.Time
	if *notAfter != "" {
		t, err := time.Parse(time.RFC3339Nano, *notAfter)
		if err != nil {
			return fmt.Errorf("--not-after %q is not an RFC3339 instant (like 2026-10-15T00:00:00Z): %v", *notAfter, err)
		}
		notAfterAt = &t
	}

	// The presentation split of KIND[:ID]; whether the half-names mean
	// anything is the write boundary's own target-parse judgement, made
	// there once (this file does not pre-check it).
	kindStr, idStr, _ := strings.Cut(*target, ":")
	notAfterStr := ""
	if notAfterAt != nil {
		notAfterStr = notAfterAt.UTC().Format(time.RFC3339Nano)
	}
	p := protocol.PolicyAddParams{
		ID:     *id,
		Type:   *typ,
		Target: protocol.PolicyTargetP{Kind: kindStr, ID: idStr},
		Action: *action,
		Spec:   specJSON,
		Source: *source,
	}
	if notAfterStr != "" {
		p.NotAfter = &notAfterStr
	}
	// ONE door, both roads (PM4): the socket's human authoring door when a
	// daemon answers, the desk over the boundary when none is home (the
	// door says so on stderr first). Neither leg pre-judges the row: the
	// authorship rule, the closed sets and the per-type validator run in
	// internal/policy exactly once (TestAddDoesNotRevalidate pins the
	// single round-trip), and the SEC-014/DEF-038 warnings come back as
	// the answer's data — printed by the one presentation below, on the
	// stream each line has always owned.
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, err := door.add(context.Background(), p)
	if err != nil {
		return err
	}
	return printPolicyAddResult(w, p, res)
}

// printPolicyAddResult prints the add door's answer: the stored line, then
// the authoring warnings the door carried back — the unmatchable-target line
// on w (T1's golden position, beside the row it comments on) and the
// SEC-014 broad-allow line on os.Stderr, where it has always spoken (a
// mandatory warning does not travel on the caller's chosen output stream —
// step_run's bash rule, the same house pattern). The list the socket
// carries and the list the offline door computes are built by the SAME
// desk functions in the SAME order, so both doors print the same bytes.
func printPolicyAddResult(w io.Writer, p protocol.PolicyAddParams, res protocol.PolicyAddResult) error {
	fmt.Fprintf(w, "policy %s stored — the daemon's next read sees it (N8 forbids caching)\n", p.ID)
	for _, warn := range res.Warnings {
		if warn == desk.UnmatchableTargetWarn(p.Target.Kind) {
			fmt.Fprintln(w, warn)
			continue
		}
		fmt.Fprintln(os.Stderr, warn)
	}
	return nil
}

// policyVocabularyForHelp answers the closed sets for `add --help` without
// printing anything: it asks the socket's policy.vocabulary when a daemon
// answers, and falls back to the compiled engine's own Vocabulary() —
// the same function the offline door serves. --help applies nothing, so
// the direct-door notice stays where it belongs: on doors that write.
func policyVocabularyForHelp() policy.VocabularyView {
	if c, err := policyDial(0); err == nil {
		if v, verr := (&policySocketDoor{c: c}).vocabulary(context.Background()); verr == nil {
			return v
		}
	}
	return policy.Vocabulary()
}

// vocabularyHelp renders the compiled engine's closed sets for `add --help`
// (PM6): the sets, and the authorship matrix COMPUTED from the boundary's
// own rule — refused cells carry the boundary's sentence verbatim, because
// a help text that paraphrases PL1 is a second PL1. Every value on these
// lines is read from the VocabularyView the engine assembled; this function
// recalls nothing.
func vocabularyHelp(v policy.VocabularyView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nclosed sets, as the compiled engine answers for itself (policy vocabulary):\n")
	fmt.Fprintf(&b, "  types    %s\n", strings.Join(v.Types, ", "))
	fmt.Fprintf(&b, "  targets  %s  (spelled KIND:ID, or all-agents)\n", strings.Join(v.TargetKinds, ", "))
	fmt.Fprintf(&b, "  sources  %s\n", strings.Join(v.Sources, ", "))
	fmt.Fprintf(&b, "  actions  %s\n", strings.Join(v.Actions, ", "))
	b.WriteString("  authorship (computed from the write boundary's own rule — PL1):\n")
	for _, e := range v.Authorship {
		if e.Allowed {
			fmt.Fprintf(&b, "    %-11s x %-5s  allowed\n", e.Source, e.Action)
			continue
		}
		fmt.Fprintf(&b, "    %-11s x %-5s  refused — %s\n", e.Source, e.Action, e.Reason)
	}
	for _, s := range v.Specs {
		if len(s.Fields) == 0 {
			fmt.Fprintf(&b, "  spec %s — the validator declares no fields\n", s.Type)
			continue
		}
		fmt.Fprintf(&b, "  spec %s\n", s.Type)
		for _, f := range s.Fields {
			req := ""
			if f.Required {
				req = ", required"
			}
			enum := ""
			if len(f.EnumValues) > 0 {
				enum = ", one of " + strings.Join(f.EnumValues, "|")
			}
			fmt.Fprintf(&b, "    %-12s %s%s%s — %s\n", f.Name, f.Kind, req, enum, f.Description)
		}
	}
	return b.String()
}

// mergeSpecToolName is the --tool sugar: the flag sets the spec's toolName
// and nothing else — never "state", whose defaulting the write boundary
// deliberately refuses (a spec whose state silently follows the action is a
// policy whose author meant one thing and stored another).
func mergeSpecToolName(spec, tool string) ([]byte, error) {
	base := strings.TrimSpace(spec)
	if base == "" && tool == "" {
		return nil, nil // the boundary defaults the spec to {} and the per-type validator decides
	}
	fields := map[string]json.RawMessage{}
	if base != "" {
		if err := json.Unmarshal([]byte(base), &fields); err != nil {
			return nil, fmt.Errorf("--spec %q is not a JSON object for this type: %v", spec, err)
		}
		// JSON `null` decodes into a map WITHOUT an error — it leaves the map
		// nil — and the --tool write below would then abort the process with
		// a nil-map panic. null is as little an object as 5 or []; the parser
		// refuses its own input here (the header's promise), naming it the
		// same way every other JSON non-object is named.
		if fields == nil {
			return nil, fmt.Errorf("--spec %q is not a JSON object for this type: JSON null is not an object", spec)
		}
	}
	if tool != "" {
		raw, err := json.Marshal(tool)
		if err != nil {
			return nil, fmt.Errorf("tool name %q is not encodable: %v", tool, err)
		}
		fields["toolName"] = raw
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("the spec could not be built: %v", err)
	}
	return out, nil
}

// mergeSpecCommandName is the --command sugar: the flag sets the spec's
// "command" field, and — the one place it differs from --tool, deliberately —
// the spec's "state" to the row's action restated. It exists for the same
// reason --tool does: typing spec JSON by hand in a shell is how a guard's
// rules end up with a typo that only surfaces as a verdict nobody can explain.
//
// Why state, when --tool refuses to write it: a CommandAccess spec has exactly
// two keys and `state` carries no information the row does not already carry.
// The write boundary refuses any state that is not the row's action byte for
// byte (stateAgreesWithAction), so there is precisely ONE value the sugar can
// write, and it is the one the operator just typed into --action. Defaulting
// state there cannot store an intention nobody wrote; leaving it out would make
// the sugar unable to author a row at all, which is the whole reason the flag
// exists. --tool keeps state hand-written and pinned that way by
// TestSpecSugarSetsOnlyToolName, and stays untouched. If --action is blank the
// sugar writes no state at all: the boundary refuses the row for its own
// reasons, and this function invents no vocabulary.
//
// Refusals (each naming both inputs, the same voice as --tool's):
//
//	--spec and --command together → "say it once"
//	--command with --type other than command_access → refused by cmdPolicyAdd,
//	    which is the only place that sees --type (the flag parser refuses its own
//	    pairings; this function is the pure JSON half)
//	--command "" with no --spec → nil spec: the boundary's own missing-command
//	    refusal surfaces, unechoed and unrephrased
func mergeSpecCommandName(spec, command, action string) ([]byte, error) {
	base := strings.TrimSpace(spec)
	if command == "" {
		// The sugar is unused — and an unused sugar has NO opinion: a non-nil
		// return here would overwrite what the --tool merge already built
		// (the shipped TestSpecSugarSetsOnlyToolName pins --tool winning
		// toolName). An unparseable --spec is refused by the tool-side merge
		// on every path, so nothing escapes validation by being absent here.
		return nil, nil
	}
	if base != "" {
		return nil, fmt.Errorf(`--spec and --command say "command" twice — say it once: --spec %q carries `+
			`the whole spec, --command %q writes only the "command" field (and the "state" the row's action `+
			`restates); drop one of them`, spec, command)
	}
	// Past the two guards the sugar has a clean sheet: no spec to parse (the
	// tool-side merge already refused any unparseable one, and a parseable
	// one hit say-it-once above), just the two fields to write.
	fields := map[string]json.RawMessage{}
	raw, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("command pattern %q is not encodable: %v", command, err)
	}
	fields["command"] = raw
	if strings.TrimSpace(action) != "" {
		state, err := json.Marshal(action)
		if err != nil {
			return nil, fmt.Errorf("action %q is not encodable: %v", action, err)
		}
		fields["state"] = state
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("the spec could not be built: %v", err)
	}
	return out, nil
}

// runPolicyAdd hands the policy to THE write boundary (T6) and prints what
// comes back. The authorship rule, the closed sets and the per-type validator
// run inside boundary.Add exactly once — this function adds none of its own
// (TestAddDoesNotRevalidate pins the single round-trip). On refusal the
// reason prints verbatim and the verb fails; the message already names the
// source and the Deny-would-be-accepted alternative.
// StoreCarriedPluginDenial is S15 T7's door for the denials a plugin
// package carries: when `aeon agent import` makes them real rows, the
// authoring call still lives IN THIS FILE — the PL15 sweep pins authoring
// to policy.go by file, and an import that bypassed the pin (or a second
// Add call site that drifted from PL1) is exactly what the pin exists for.
// The boundary is still the ONE that enforces PL1 (an Allow from source
// import is refused here exactly as it was at decode; a package cannot
// author its own permissions twice). Print-and-continue lives here too:
// the carried set's every row is stated — stored or refused — because a
// silently unstored denial is the hole a consent never mentioned.
func StoreCarriedPluginDenial(w io.Writer, boundary *policy.Store, p policy.Policy, now time.Time) {
	if rej := boundary.Add(context.Background(), p, now); rej != nil {
		fmt.Fprintf(w, "carried policy %s NOT stored: %s\n", p.ID, rej.Reason)
		return
	}
	fmt.Fprintf(w, "carried policy %s stored — the daemon's next read sees it (N8 forbids caching)\n", p.ID)
}

// --- remove ------------------------------------------------------------------------

func cmdPolicyRemove(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy remove", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("id", "", "the policy id to remove")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, err := door.remove(context.Background(), *id)
	if err != nil {
		return fmt.Errorf("remove policy %q: %w", *id, err)
	}
	// Removal passes the id through — the id is the whole request; deleting
	// an unknown-shaped id deletes nothing, which is said rather than
	// guessed. The audit row beside the deletion (PM8) is the door's half:
	// the daemon stamps the peer, the offline door names itself as cli.
	if !res.Removed {
		fmt.Fprintf(w, "no policy called %q — nothing was removed\n", *id)
		return nil
	}
	fmt.Fprintf(w, "policy %q removed — the daemon's next read sees its absence\n", *id)
	return nil
}

// --- seed-corpus --------------------------------------------------------------------

// cmdPolicySeedCorpus is decision D4 made executable: an explicit verb for
// turning this repository's own agent rules (the committed corpus) into
// stored policy. Four properties, each with a test in policy_seed_corpus_test.go:
//
//  1. EXPLICIT. It runs when an operator types it and never otherwise: no
//     daemon-start loader, no auto-seed-when-empty, no test helper that calls
//     it to get a row into a store. Nothing under internal/daemon or
//     internal/store may name it (the grep test pins it), and PL15 keeps it
//     off the socket like every other authoring.
//  2. THROUGH THE BOUNDARY. Every row goes through the boundary's Add — so
//     the write boundary checks authorship and the per-type validator runs —
//     and every authored row carries one authored audit event through the
//     boundary's record half. Inserting straight through the repo seam is
//     the second writer the sole-writer grep forbids.
//  3. OPERATOR-AUTHORED. Source is Operator and there is no --source flag:
//     the corpus is this repository's own rules, entered by a person running
//     a command. An allow from any other source would be refused by the
//     boundary, and it should be — offering the flag would only advertise
//     the refusal.
//  4. IDEMPOTENT. Re-running changes nothing: no duplicate rows, no
//     duplicate events. The witness is the row and event COUNT before and
//     after the second run, not a "did it feel like it worked" reading. An
//     existing rule is LEFT ALONE (reported unchanged) — re-seeding must
//     never overwrite an operator's later edit to a rule it seeded.
//
// The one input is policy.EmbeddedCorpus() — the committed corpus compiled
// into the binary. This verb opens no file and carries no path: not to
// opencode/ (that directory is T1's read-only ground), not to testdata/, not
// even to itself. A built binary seeds correctly on a machine with no source
// tree, which is what makes "the corpus is the specification" true of the
// shipped product rather than only of the checkout.
func cmdPolicySeedCorpus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy seed-corpus", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), policyUsage())
		fs.PrintDefaults()
	}
	var agents []string
	fs.Func("agent", "an agent whose corpus rules to seed; repeat the flag for more than one agent",
		func(s string) error {
			agents = append(agents, s)
			return nil
		})
	dryRun := fs.Bool("dry-run", false, "print the per-agent counts it WOULD write and write nothing")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	// Flag hygiene, the D4 spelling: a seed with no --agent is a request to
	// seed EVERYTHING by omission, and "explicit" is the property this verb
	// exists to have. The agents are named or the verb refuses its own input.
	if len(agents) == 0 {
		return fmt.Errorf("`policy seed-corpus` needs --agent NAME — it seeds the agents you name, never " +
			"the whole corpus by surprise (D4: explicit is the property, not the default)")
	}
	// ONE door, both roads (PM4, T12's re-base): the socket's seedCorpus
	// door when a daemon answers — which runs THE seed engine
	// (policy.Store.ApplyCorpus, the one implementation of idempotence) and
	// answers per-agent counts as data — or the desk runner over the store
	// when none is home, which prints the run itself. The bytes are one
	// formatter's either way: desk.SeedAgentLine and desk.SeedRunTotalsLine
	// are the only spellers of these lines in the product, the runner
	// prints through them and this presentation prints the wire counts
	// through them.
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, printed, err := door.seedCorpus(context.Background(), w, agents, *dryRun)
	if err != nil || printed {
		return err
	}
	for _, a := range res.PerAgent {
		fmt.Fprintln(w, desk.SeedAgentLine(a.Agent, a.Seeded, a.Unchanged, res.DryRun))
	}
	fmt.Fprintln(w, desk.SeedRunTotalsLine(res.Seeded, len(res.PerAgent), res.Unchanged, res.DryRun))
	return nil
}

// --- why -----------------------------------------------------------------------------

func cmdPolicyWhy(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy why", flag.ContinueOnError)
	fs.SetOutput(w)
	agent := fs.String("agent", "", "who the question is about (the subject of the answer)")
	loop := fs.String("loop", "", "the loop the question is inside (names the subject over the agent)")
	laneType := fs.String("lane-type", "", "the lane role asked about")
	mcp := fs.String("mcp", "", "the MCP server asked about")
	typ := fs.String("type", string(policy.ToolAccess), "the question's type; tool_access is the default because it is the only type with a real evaluator today")
	tool := fs.String("tool", "", "the tool the question wants to call (tool_access)")
	command := fs.String("command", "", "the command the question wants to run (command_access)")
	route := fs.String("route", "", "the model route the question wants (route_access)")
	effect := fs.String("effect", "", "the effect class the question wants (effect_class)")
	cost := fs.String("cost", "", "the token spend asked about (cost_cap)")
	skill := fs.String("skill", "", "the skill the question wants to load (skill_access)")
	trigger := fs.String("trigger", "", "the trigger the question wants to start (trigger_access)")
	at := fs.String("at", "", "answer as of this RFC3339 instant; default is now. This flag is why the expiry boundary is explorable (demo §6) — the clock is a parameter (PL6), no test sleeps")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	instant := time.Now()
	if *at != "" {
		t, err := time.Parse(time.RFC3339Nano, *at)
		if err != nil {
			return fmt.Errorf("--at %q is not an RFC3339 instant (like 2026-10-14T23:59:59Z): %v", *at, err)
		}
		instant = t
	}

	// The type is flag input: a value outside the closed set cannot even BE
	// the question, so the flag parser refuses its OWN input — one call to
	// the boundary's single parser (delegation, not a second validator; the
	// header's input-parsing rule, same class as --at). Handing such a value
	// to Evaluate instead would print the D10 answer for the seven ("has no
	// evaluator yet") plus its promise that a registration is coming — for a
	// typo no slice will ever register. Unimplemented is for a member whose
	// evaluator has not landed; this refuses a non-member by name.
	if _, rej := policy.ParsePolicyType(*typ); rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}

	req := protocol.PolicyWhyParams{
		Type:      *typ,
		Agent:     *agent,
		Loop:      *loop,
		LaneType:  *laneType,
		MCPServer: *mcp,
		Tool:      *tool,
		Command:   *command,
		Route:     *route,
		Effect:    *effect,
		Cost:      *cost,
		Skill:     *skill,
		Trigger:   *trigger,
	}
	if *at != "" {
		req.At = *at
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, err := door.why(context.Background(), req, instant)
	if err != nil {
		return err
	}
	return printPolicyWhy(w, res)
}

// printPolicyWhy answers "what would happen if" with ZERO writes, on either
// door — and structurally so: the answer behind this print is
// PolicyWhyAnswer's (the socket's own implementation, shared by both
// doors), which reaches the boundary only as far as List (the one read,
// uncached) and the pure engine policy.Evaluate; the record half of the
// boundary (RecordRefusal/RecordDisagreement) is not on this path at all —
// a spy pinned at zero writes is the proof
// (TestWhyExplainsAVerdictAndChangesNothing). A refusal by `why` is not a
// refusal that happened, and an audit row for a hypothetical is the record
// that lies.
//
// The set-read refusal arrives as the door's error and prints verbatim:
// unlike the enforcement gate (which denies on an unreadable set — and can
// render the engine's own fail-closed line), an explanation has no verdict
// to print, so the boundary's reason prints as it arrived and the verb
// fails. Inventing an answer would be the confident wrong one.
func printPolicyWhy(w io.Writer, res protocol.PolicyWhyResult) error {
	fmt.Fprintln(w, res.Sentence) // D9: the one formatter, whole.

	// Branch on the FIELD (SEC-033 class): matching the shared "refused: "
	// prefix would conflate "nobody can answer yet" with "a rule refused
	// you" — the two answers send an operator to different places.
	if res.Unimplemented {
		fmt.Fprintln(w, whyUnimplementedNote)
	}
	// SEC-050's composition visibility, on the FIELDS (never a prefix match,
	// the SEC-033 class again): the COMMAND question allowed, and the TOOL
	// question — asked over the very same set at the very same instant — was
	// refused by a rule. The answer function ran that second question and
	// names the refusing rule only when that pair holds; this is the
	// product's ONE site composing that note (D9 — a second composition
	// site is a second truth). It says what the coexistence means, and
	// prints neither sentence again.
	if res.By != "" {
		fmt.Fprintln(w, whyToolDenyOutranked(res.By, res.ByTarget))
	}
	// DEF-038: an explanation asked on identity dimensions no runtime
	// decision carries must say so, or it reads as if those dimensions
	// matched somewhere real.
	if res.Unmatchable {
		fmt.Fprintln(w, whyUnmatchableIdentitiesWarn)
	}
	return nil
}

// --- in-force --------------------------------------------------------------------------

func cmdPolicyInForce(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy in-force", flag.ContinueOnError)
	fs.SetOutput(w)
	agent := fs.String("agent", "", "answer for this agent")
	loop := fs.String("loop", "", "answer for this loop")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	// One target per question — and this is flag hygiene (an ambiguous input
	// the boundary could not even express), not policy validation: whether
	// the NAME is a legal identifier is the boundary's own judgement, made
	// inside InForceFor.
	if *agent != "" && *loop != "" {
		return fmt.Errorf("`policy in-force` takes --agent or --loop, not both — a target is one party per answer")
	}
	if *agent == "" && *loop == "" {
		return fmt.Errorf("`policy in-force` needs --agent NAME or --loop SLUG — \"in force for whom\" is the question")
	}
	target := policy.Target{Kind: policy.AgentKind, ID: *agent}
	if *loop != "" {
		target = policy.Target{Kind: policy.LoopKind, ID: *loop}
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, err := door.inForce(context.Background(), protocol.PolicyInForceParams{Agent: *agent, Loop: *loop}, time.Now())
	if err != nil {
		return err
	}
	return printPolicyInForce(w, res, target.String())
}

// printPolicyInForce prints the three-state answer (T11) per governing
// policy on either door: each line is the policy's identity and then
// InForceAnswer.Render() verbatim — additive columns beside a whole sentence
// (D9's allowance), never a replacement of it. The socket's rows and the
// offline door's rows are PolicyInForceAnswer's rows (the same
// implementation, both doors), so the state name beside the sentence is
// the same string either way. A store failure arrives as the boundary's
// *Reject and exits non-zero: a half-read acceptance table would report
// "unacknowledged" for rules whose records the read lost, so there is no
// partial answer here.
func printPolicyInForce(w io.Writer, res protocol.PolicyInForceResult, target string) error {
	if len(res.Answers) == 0 {
		// The empty answer is a statement (D4's discipline, this verb's
		// shape): PL5 binds everyone whether or not any rule names them.
		fmt.Fprintf(w, "nothing governs %q — the default deny (PL5) binds everyone until an operator grant names an exception\n", target)
		return nil
	}
	for _, a := range res.Answers {
		fmt.Fprintf(w, "%-14s %-24s %s\n", a.Type, a.PolicyID, a.Render)
	}
	return nil
}

// --- events -----------------------------------------------------------------------------

func cmdPolicyEvents(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy events", flag.ContinueOnError)
	fs.SetOutput(w)
	subject := fs.String("subject", "", "list only this subject's rows: agent:NAME or loop:SLUG; omitted lists every subject")
	depth := fs.Int("depth", 20, "how many rows to list, newest first; the summary line reports the depth of "+
		"the rows this listing covers — every subject's when --subject is omitted, this subject's when it is given")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	ans, err := door.events(context.Background(), *subject, *depth, time.Now())
	if err != nil {
		return err
	}
	return printPolicyEvents(w, ans, *subject)
}

// trimLine is the trim footer both event doors print through — "" for the
// silent success (a trim that removed nothing has nothing to report), the
// FAILED line when housekeeping failed (a silently-skipped cap is how
// PL11's table grew back), and the removed-count line otherwise. One
// speller for the line the direct door's runner and the on-demand verb
// both need.
func trimLine(n int, err error) string {
	if err != nil {
		return fmt.Sprintf("trim    FAILED: %v — the age cap was not enforced this run", err)
	}
	if n > 0 {
		return fmt.Sprintf("trim    %d row(s) past the age cap removed", n)
	}
	return ""
}

// eventsRowLine is the one speller of an audit row line, shared by
// printPolicyEvents (both doors) and runPolicyEvents (the offline report
// the tests drive): id, instant, outcome, "kind:id" subject, the policy id
// quoted (— when absent) and the detail. Values only, assembled once.
func eventsRowLine(id int64, at, outcome, subject, policyID, detail string) string {
	pol := "-"
	if policyID != "" {
		pol = `"` + policyID + `"`
	}
	return fmt.Sprintf("%-6d %s  %-12s  %s  %-24s  %s\n", id, at, outcome, subject, pol, detail)
}

// policyAdHocAuditActor is the runtime's ad-hoc step actor identity — the
// probe subject the offline events door asks the boundary about (the same
// name the pre-T8 wiring passed as runPolicyEvents' adHocActor: "agent:"+
// runtime.DefaultHandlerName). One definition, both the pinned offline
// report and the door's probe.
func policyAdHocAuditActor() string { return runtime.DefaultHandlerName }

// printPolicyEvents is the one presentation of the events door's answer,
// both doors. Its three jobs are runPolicyEvents' three jobs (below),
// stated once: rows and the D15 summary line, and the audit's three empty
// states — never collapsing "unaudited" into "nothing" (P31 applied to the
// audit; DEF-041, F-14). The trim line and the ad-hoc probe line are the
// offline door's (the reader holds the store there); over the socket the
// daemon is the one running and both lines are silence, which is the
// honest answer, not a missing one.
func printPolicyEvents(w io.Writer, ans policyEventsAnswer, subject string) error {
	if ans.TrimLine != "" {
		fmt.Fprintln(w, ans.TrimLine)
	}
	for _, r := range ans.Res.Rows {
		fmt.Fprint(w, eventsRowLine(r.ID, r.At, r.Kind, r.Subject, r.PolicyID, r.Detail))
	}
	fmt.Fprintf(w, "depth %d · cap %s · oldest %s\n",
		ans.Res.Depth, capLabel(time.Duration(ans.Res.CapDays)*24*time.Hour), oldestLabelWire(ans.Res.Oldest))
	if ans.Res.Depth == 0 {
		if subject != "" {
			fmt.Fprintln(w, eventsFilteredEmptyHonesty(subject))
		} else {
			fmt.Fprintln(w, eventsEmptyHonesty)
		}
	}
	if ans.UnauditedLine != "" {
		fmt.Fprintln(w, ans.UnauditedLine)
	}
	return nil
}

// oldestLabelWire is oldestLabel's wire-shaped twin — the same answer for
// the same fact, spelled from the answer's *string (null is the EMPTY
// table's "(none)", distinguishable from failure, which is a refusal and
// exits non-zero, P31).
func oldestLabelWire(oldest *string) string {
	if oldest == nil || *oldest == "" {
		return "(none)"
	}
	return *oldest
}

// runPolicyEvents does three jobs, and each is a wiring the boundary always
// had but nothing called:
//
//  1. DEF-030/SEC-034: the ON-DEMAND trim. TrimEvents belongs to daemon
//     start (T13) and to demand — this verb is the demand. The 90-day cap
//     gains an operator-reachable trigger here or it is a promise nobody
//     keeps. A trim failure NEVER hides the report (housekeeping failure is
//     not a read failure) but is said out loud, because a silently-skipped
//     cap is how PL11's table grew back.
//  2. The rows and the D15 summary line (`depth · cap · oldest`), with the
//     cap rendered from policy.PolicyEventAgeCap — the same const TrimEvents
//     enforces — so the printed promise can never disagree with the enforced
//     one. The depth and oldest are THE LISTING'S (F-13): ReportEvents reads
//     them for the same subject it reads the rows for, so `--subject agent:x`
//     prints agent:x's depth and `oldest`, and the unfiltered run prints the
//     whole log's — the D15 number is unchanged when no filter was asked for.
//  3. DEF-041/SEC-043/F-14: the audit's third state — three of them, each with its own sentence.
//     A genuinely empty log says why it may not be read as "nothing was refused"; a listing that is
//     empty UNDER a --subject filter names the filter and says the zero is the filter's answer, not
//     the log's (F-14 — after T22 the footer's depth agrees with the filter, which is exactly why the
//     sentence is still needed: "depth 0" alone is the truth about the wrong question); and the
//     boundary itself (through ReportEvents' own subject parse — no second validator here) answers
//     whether the runtime's ad-hoc actor can be an audit subject at all. When it cannot, "unaudited"
//     prints. The word "nothing" never stands alone.
//
// The adHocActor parameter is the production wiring's "agent:"+
// runtime.DefaultHandlerName; tests inject variants to drive both branches.
func runPolicyEvents(w io.Writer, boundary *policy.Store, subject string, limit int, now time.Time, adHocActor string) error {
	trimN, trimErr := boundary.TrimEvents(context.Background(), now)
	if line := trimLine(trimN, trimErr); line != "" {
		fmt.Fprintln(w, line)
	}

	report, rows, rej := boundary.ReportEvents(context.Background(), subject, limit)
	if rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}
	for _, r := range rows {
		fmt.Fprint(w, eventsRowLine(r.ID, r.At, r.Outcome, r.SubjectKind+":"+r.SubjectID, r.PolicyID, r.Detail))
	}
	fmt.Fprintf(w, "depth %d · cap %s · oldest %s\n",
		report.Depth, capLabel(report.Cap), oldestLabel(report.Oldest))

	// The third state, never collapsed into "nothing" (P31 applied to the audit — legal/observability
	// carry DEF-041; the filtered half is F-14). Two claims, two sentences: "the log holds nothing"
	// and "this filter matched nothing" are different facts, and the one that goes unprinted is the
	// one an operator will later misremember.
	if report.Depth == 0 {
		if subject != "" {
			fmt.Fprintln(w, eventsFilteredEmptyHonesty(subject))
		} else {
			fmt.Fprintln(w, eventsEmptyHonesty)
		}
	}
	// In production this probe prints NOTHING: the wired actor is "agent:"+runtime.DefaultHandlerName
	// = "agent:step-ad-hoc", which the identifier policy accepts, so the boundary never refuses it and
	// the honest outcome is silence — not a broken branch. The slice DEMO's earlier caveat claimed the
	// opposite (the actor named `step:ad-hoc`, with a colon, refused as a subject); T17 measured the
	// mint and found `step-ad-hoc` — identifier-valid — so the blind-zero story belongs to a name the
	// runtime does not mint, not to this wiring. The line stays conditional, not a banner: the two
	// subtests in policy_test.go drive both arms.
	if _, _, probeRej := boundary.ReportEvents(context.Background(), adHocActor, 1); probeRej != nil {
		// The main read above succeeded, so this refusal is the boundary's
		// own subject gate — the ad-hoc actor cannot file an audit row, and
		// every refusal of an ad-hoc step is unaudited BY CONSTRUCTION.
		fmt.Fprintln(w, eventsUnauditedAdHoc(adHocActor))
	}
	return nil
}

// capLabel renders the retention cap from policy.PolicyEventAgeCap — never a
// literal (D15: the printed cap must be the enforced one; a literal is a
// second source of truth that drifts the day the const moves).
func capLabel(cap time.Duration) string {
	if cap > 0 && cap%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", int64(cap/(24*time.Hour)))
	}
	return cap.String()
}

// oldestLabel states the oldest retained instant, and states the EMPTY table
// as the answer it is — `(none)`, distinguishable from failure, which is a
// refusal and exits non-zero (P31's honesty applied to the audit).
func oldestLabel(oldest time.Time) string {
	if oldest.IsZero() {
		return "(none)"
	}
	return oldest.UTC().Format(time.RFC3339)
}

// --- get ------------------------------------------------------------------------------

// cmdPolicyGet reads ONE policy whole — the row the list prints plus the
// spec, the describe sentence, the acceptance record and the authored flag
// (S45 T8's new verb, the CLI twin of policy.get). It re-reads nothing
// locally: the door's answer (PolicyGetAnswer, both doors) carries the
// fail-closed spec re-decode, and a row that escapes the closed set is the
// refusal that prints verbatim.
func cmdPolicyGet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy get", flag.ContinueOnError)
	fs.SetOutput(w)
	idFlag := fs.String("id", "", "the policy id to read whole")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	id := strings.TrimSpace(*idFlag)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" {
		return fmt.Errorf("`policy get` takes one policy id — `aeon policy get <id>` (or --id ID)")
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	d, err := door.get(context.Background(), id, time.Now())
	if err != nil {
		return err
	}
	expiry := "no expiry"
	if d.NotAfter != nil {
		expiry = d.NotAfter.UTC().Format(time.RFC3339)
	}
	accepted := "nobody on record"
	if d.AcceptedBy != "" {
		accepted = fmt.Sprintf("%s at %s", d.AcceptedBy, d.AcceptedAt)
	}
	fmt.Fprintf(w, "policy %s\n", d.ID)
	fmt.Fprintf(w, "  type      %s\n", d.Type)
	fmt.Fprintf(w, "  target    %s\n", d.Target)
	fmt.Fprintf(w, "  action    %s\n", d.Action)
	fmt.Fprintf(w, "  source    %s\n", d.Source)
	fmt.Fprintf(w, "  origin    %s\n", d.Origin)
	fmt.Fprintf(w, "  created   %s\n", d.CreatedAt)
	fmt.Fprintf(w, "  expiry    %s\n", expiry)
	fmt.Fprintf(w, "  spec      %s\n", string(d.Spec))
	fmt.Fprintf(w, "  describe  %s\n", d.Describe)
	fmt.Fprintf(w, "  accepted  %s\n", accepted)
	authored := "no — the audit carries no authored row for this rule at this subject"
	if d.Authored {
		authored = "yes"
	}
	fmt.Fprintf(w, "  authored  %s\n", authored)
	return nil
}

// --- vocabulary -----------------------------------------------------------------------

// cmdPolicyVocabulary asks the compiled engine what it can speak (S45 T8's
// new verb, the CLI twin of policy.vocabulary, PM6): the closed sets, the
// authorship matrix COMPUTED from the boundary's rule, and each type's
// spec shape as its validator reads it. The socket asks the daemon's
// engine; the offline door asks the same function the daemon would — one
// source, and this file recalls none of it.
func cmdPolicyVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	v, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprint(w, vocabularyBody(v))
	return nil
}

// vocabularyBody spells the whole picture; vocabularyHelp (add --help)
// prints the same lines, so the sets live in one speller.
func vocabularyBody(v policy.VocabularyView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "types     %s\n", strings.Join(v.Types, ", "))
	fmt.Fprintf(&b, "targets   %s  (spelled KIND:ID, or all-agents)\n", strings.Join(v.TargetKinds, ", "))
	fmt.Fprintf(&b, "sources   %s\n", strings.Join(v.Sources, ", "))
	fmt.Fprintf(&b, "actions   %s\n", strings.Join(v.Actions, ", "))
	b.WriteString("authorship (computed from the write boundary's own rule — PL1):\n")
	for _, e := range v.Authorship {
		if e.Allowed {
			fmt.Fprintf(&b, "  %-11s x %-5s  allowed\n", e.Source, e.Action)
			continue
		}
		fmt.Fprintf(&b, "  %-11s x %-5s  refused — %s\n", e.Source, e.Action, e.Reason)
	}
	for _, s := range v.Specs {
		if len(s.Fields) == 0 {
			fmt.Fprintf(&b, "spec %s — the validator declares no fields\n", s.Type)
			continue
		}
		fmt.Fprintf(&b, "spec %s\n", s.Type)
		for _, f := range s.Fields {
			req := ""
			if f.Required {
				req = ", required"
			}
			enum := ""
			if len(f.EnumValues) > 0 {
				enum = ", one of " + strings.Join(f.EnumValues, "|")
			}
			fmt.Fprintf(&b, "  %-12s %s%s%s — %s\n", f.Name, f.Kind, req, enum, f.Description)
		}
	}
	return b.String()
}

// --- changes --------------------------------------------------------------------------

// cmdPolicyChanges reads the authoring audit (S45 T8's new verb, the CLI
// twin of policy.changes, PM8): every policy_change row newest first, and
// the DEF-041 third state — the stored rules whose audit row never landed
// are NAMED, so an audit that lost a row is an audit that says so. A
// --policy filter's id passes the boundary's own identifier gate through
// the door (no second validator here); --limit passes through to the
// accessor's clamp, which owns it.
func cmdPolicyChanges(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("policy changes", flag.ContinueOnError)
	fs.SetOutput(w)
	policyID := fs.String("policy", "", "show only this rule's trail")
	limit := fs.Int("limit", 0, "how many rows to list, newest first (the accessor owns the default and the clamp)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, err := door.changes(context.Background(), *policyID, *limit, time.Now())
	if err != nil {
		return err
	}
	for _, c := range res.Changes {
		who := c.ActorKind + ":" + c.ActorID
		target := "-"
		if c.PolicyID != "" {
			target = `"` + c.PolicyID + `"`
		}
		proposal := ""
		if c.ProposalID != nil {
			proposal = fmt.Sprintf("  proposal #%d", *c.ProposalID)
		}
		fmt.Fprintf(w, "%s  %-16s  %-7s  %-24s  %s%s\n",
			c.OccurredAt, who, c.Op, target, c.Summary, proposal)
	}
	if res.Statement != "" {
		// The EMPTY TABLE's own sentence (the wire's, pinned byte for byte
		// beside the store's — T4's CLI twin rule): it rides only the
		// Count-proved zero. A filtered-empty read prints nothing, because
		// zero rows under a filter is the filter's answer, not the audit's.
		fmt.Fprintln(w, res.Statement)
	}
	for _, id := range res.UnauditedPolicyIDs {
		// The third state of the audit (DEF-041): the rule stands, its
		// record is missing, and the listing names the gap rather than
		// letting a clean-looking audit lie.
		fmt.Fprintln(w, changesUnauditedLine(id))
	}
	return nil
}

// changesUnauditedLine spells the reconciliation's line. The id is stored
// text, so it rides %q — SEC-033's class, the same discipline
// eventsUnauditedAdHoc holds.
func changesUnauditedLine(policyID string) string {
	return "unaudited  policy " + fmt.Sprintf("%q", policyID) +
		" was written with NO authoring audit row — the gap between the write and its record; " +
		"the rule stands, and this line is the audit saying so"
}
