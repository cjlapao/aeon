package policy

// ════════════════════════════════════════════════════════════════════════════
// The write boundary. PL1 is true here, in one function, and nowhere else.
//
// # Why this file is the security argument of the slice
//
//	Allow is authored by an operator. Deny may be authored by anyone in the
//	chain. Deny wins.
//
// That is what makes an imported agent or a third-party MCP server safe
// WITHOUT trusting it: whatever policy it carries can only narrow what it can
// do. The dangerous direction — a non-operator grant — is closed where rows
// are written, not where rows are read: a stored Allow from an agent that
// "does not count" is one refactor away from counting, and nothing would
// notice. Refuse the write (D2), in the one place a rule can be trusted
// (D3), and the surfaces above (the CLI, T12) surface this refusal rather
// than reimplementing it.
//
// # What "the single place" costs, and buys
//
// It costs a re-validation duty: Add re-runs every closed-set parser (T2) and
// the per-type validator (T5) on whatever it is handed, because Go's type
// system does not enforce the closed set — `Policy{Action: Action("allow ")}`
// compiles. A check downstream is a check with a bypass (SEC-015/SEC-007), so
// nothing here trusts its caller, and List re-reads every stored row through
// DecodePolicy for the same reason on the read side: a hand-edited row gets
// refused loudly, never silently dropped (dropping a row can drop a Deny).
//
// It buys the grep: TestTheAuthorshipRuleAppearsInExactlyOnePlace walks every
// non-test .go file under the module and fails if the pinned expression below
// appears anywhere except this file. A second enforcement point is the
// beginning of the drift that ends with one of them weakened.
//
// # No SQL (I2), no cache (N8), no expiry filter (D6)
//
// The SQL text lives in internal/store/policy.go; this file imports neither
// database/sql nor internal/store — the Repo seam below is the boundary, and
// the wiring (the daemon, the CLI) bridges the two row shapes with
// RepoFuncs, because Go method identity requires the parameter types to be
// IDENTICAL, not merely shaped alike, across a package line. There is no
// cache: List hits the repo on every call (a cache is where a stale allow
// lives). And List does not filter expired rows — Evaluate owns expiry with
// `at` as a parameter; the store that hides history is the audit that lies.
//
// # The identifier policy (SEC-013, SEC-020) — a gate, not a blade
//
// Policy ids and target ids are third-party text BY PLAN: any source may
// author a Deny, so the strings an untrusted author chooses reach
// Sentence() — an operator's console and policy_event.detail — through every
// verdict. They are gated at the write boundary with a charset allow-list, a
// rune cap, and refusals for control bytes, zero-width characters and
// invisible-only values. Exceeding the cap REFUSES; nothing is truncated,
// because a silently-shortened identifier is a different rule wearing the old
// one's name — the same reason the seed refuses "head-truncation" of the
// state-bearing sentence tail (SEC-020): a security control must never turn
// into a silently-lost explanation.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Row is one policy as it crosses the seam to the store — identical in shape
// to store.PolicyRow, declared here because the policy package must not
// import the store (I2 keeps the SQL text down there; this stays the pure
// adapter holding the rule).
//
// NotAfter "" means no expiry (the column stores NULL; PL7). CreatedAt is the
// RFC3339Nano rendering of Add's `now` — the clock is a parameter everywhere
// in this package (D5), so the row's timestamps descend from the caller's
// instant, never from a wall the boundary could read. Subject is derived
// policy-side (Target.String()) and ignored store-side; see store.PolicyRow
// for why it is not a column.
type Row struct {
	ID         string
	Type       string
	TargetKind string
	TargetID   string
	Action     string
	Source     string
	Subject    string
	SpecJSON   string
	NotAfter   string
	CreatedAt  string
	// Origin is the row's provenance (S45 T12, PM15): system | user |
	// import. It joins the row on the READ side (PolicyListAll carries the
	// column through the bridge, and ListDetailed hands it to the read
	// doors) and on the SEED write side (AddInTx names it). It is not
	// policy content: it names no permission, the evaluator reads none,
	// and Add itself never sets it — a plain Add's row carries no origin
	// and the column's DEFAULT 'user' is the answer.
	Origin string
}

// AcceptanceRow is one `policy_acceptance` row as it crosses the seam —
// identical in shape to store.AcceptanceRow, declared here for the same
// reason Row is: the policy package must not import the store (I2), and two
// identically-shaped structs in two packages are still two types.
//
// All five fields are strings — identifiers and the one timestamp, the exact
// five columns the DDL declares and nothing else (legal constraint 2: this
// record has no free-text column to leak content or a secret through).
// AcceptedAt is the RFC3339Nano rendering of the FIRST record instant; the
// boundary's decode re-reads every value through the parsers before a caller
// sees it (the SEC-015 argument, read side).
type AcceptanceRow struct {
	PolicyID   string
	TargetKind string
	TargetID   string
	AcceptedBy string
	AcceptedAt string
}

// EventRow is one `policy_event` row as it crosses the seam — identical in
// shape to store.EventRow, declared here for the same reason Row and
// AcceptanceRow are: the policy package must not import the store (I2), and
// two identically-shaped structs in two packages are still two types.
//
// The fields are the columns as actually found (verified against S4's DDL
// before T8 wrote a statement): id, policy_id ("" == NULL — the default deny
// and the unimplemented refusal name no rule), subject_kind + subject_id (the
// subject split at the boundary and re-split identically by the store),
// outcome (the event kind: "refusal" as of T8; "disagreement" arrives with
// T10), detail (the bound sentence — ≤ MaxEventDetailRunes runes, enforced on
// write, re-verified on read by decodeEvent) and at (the fixed-width RFC3339
// rendering the trim compares lexicographically).
type EventRow struct {
	ID          int64
	PolicyID    string
	SubjectKind string
	SubjectID   string
	Outcome     string
	Detail      string
	At          string
}

// Repo is the whole of the store the boundary needs. It is a consumer-side
// interface (small, defined where it is used), and the concrete
// *store.Store satisfies it through the RepoFuncs bridge below rather than
// directly: its PolicyAdd takes store.PolicyRow, and two structs in two
// packages are never the same Go type however identically they are shaped —
// pretending otherwise is how a wiring "compiles in your head" and fails at
// the call site.
//
// The two acceptance methods (T7) are shape-wise twins of the store's: the
// record is a write to `policy_acceptance` and a read back per target. They
// do NOT belong to the enforcement path — Evaluate takes []Policy, a Request
// and an instant, and sees neither the Repo nor this table (PL10).
type Repo interface {
	PolicyAdd(ctx context.Context, r Row) error
	PolicyRemove(ctx context.Context, id string) (bool, error)
	PolicyListAll(ctx context.Context) ([]Row, error)
	PolicyAcceptanceRecord(ctx context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error
	PolicyAcceptanceListFor(ctx context.Context, targetKind, targetID string) ([]AcceptanceRow, error)
	PolicyEventInsert(ctx context.Context, kind, subject, policyID, detail string, at time.Time) error
	PolicyEventQuery(ctx context.Context, subject string, limit int) ([]EventRow, error)
	PolicyEventTrim(ctx context.Context, olderThan time.Time) (int, error)
	// PolicyEventStats answers depth and oldest FOR THE LISTING asked for:
	// subject "" is the whole log, a non-empty subject is that subject's rows
	// only (F-13 — the footer of a filtered listing must be about that
	// listing). The shape mirrors PolicyEventQuery's subject argument exactly.
	PolicyEventStats(ctx context.Context, subject string) (depth int, oldest time.Time, err error)
}

// RepoFuncs is that bridge: the wiring layer (daemon bootstrap, `aeon policy`
// — T12) fills the hooks from methods on the concrete *store.Store, converting
// policy.Row ⇄ store.PolicyRow and policy.AcceptanceRow ⇄ store.AcceptanceRow
// field for field. The conversion is boring and total on purpose; the
// alternative — importing store from policy — breaks the package's purity
// story for a struct definition.
//
// A missing hook is a wiring defect, and it surfaces as an error (the
// infrastructure vocabulary) naming the hook, never as a nil dereference in
// the middle of an authoring call.
type RepoFuncs struct {
	Add              func(ctx context.Context, r Row) error
	Remove           func(ctx context.Context, id string) (bool, error)
	ListAll          func(ctx context.Context) ([]Row, error)
	AcceptanceRecord func(ctx context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error
	AcceptanceList   func(ctx context.Context, targetKind, targetID string) ([]AcceptanceRow, error)
	EventInsert      func(ctx context.Context, kind, subject, policyID, detail string, at time.Time) error
	EventQuery       func(ctx context.Context, subject string, limit int) ([]EventRow, error)
	EventTrim        func(ctx context.Context, olderThan time.Time) (int, error)
	// EventStats answers the depth/oldest pair for the LISTING the caller asked
	// about (F-13): "" is the whole log, a `kind:id` is that subject's rows.
	EventStats func(ctx context.Context, subject string) (depth int, oldest time.Time, err error)

	// AddTx and RemoveTx are the SEED door's transaction halves of the two
	// writes (S45 T12): the seed applier writes a policy row inside the
	// engine's open transaction (the Tx seam below), where a row and its
	// ledger row commit together. They are SEPARATE fields, not extra
	// behaviour on Add/Remove: a wiring that does not fill them simply has
	// no seed door — AddInTx/RemoveInTx refuse it by naming the missing
	// wiring — and every plain call keeps travelling the exact path it
	// travelled before. The transaction fields are additive, not a change
	// of direction.
	AddTx    func(ctx context.Context, tx Tx, r Row) error
	RemoveTx func(ctx context.Context, tx Tx, id string) (bool, error)
	// EventInsertTx is the same door for the ONE audit row the seed writes
	// beside the row (S45 T12, PM8): the applier's record of authoring lands
	// on the engine's open transaction, so the row and its authored event
	// commit or vanish together. Nil is the same honest absence: the seed
	// door refuses it by naming the missing wiring.
	EventInsertTx func(ctx context.Context, tx Tx, kind, subject, policyID, detail string, at time.Time) error
}

// PolicyAdd satisfies Repo.
func (f RepoFuncs) PolicyAdd(ctx context.Context, r Row) error {
	if f.Add == nil {
		return fmt.Errorf("policy: the repo wiring has no Add function")
	}
	return f.Add(ctx, r)
}

// PolicyRemove satisfies Repo.
func (f RepoFuncs) PolicyRemove(ctx context.Context, id string) (bool, error) {
	if f.Remove == nil {
		return false, fmt.Errorf("policy: the repo wiring has no Remove function")
	}
	return f.Remove(ctx, id)
}

// PolicyListAll satisfies Repo.
func (f RepoFuncs) PolicyListAll(ctx context.Context) ([]Row, error) {
	if f.ListAll == nil {
		return nil, fmt.Errorf("policy: the repo wiring has no ListAll function")
	}
	return f.ListAll(ctx)
}

// PolicyAddTx satisfies the wiring-side transaction seam below (S45 T12): a
// wiring that filled AddTx writes inside the caller's transaction; one that
// did not REFUSES by naming itself — the seed door is absent, not silent.
func (f RepoFuncs) PolicyAddTx(ctx context.Context, tx Tx, r Row) error {
	if f.AddTx == nil {
		return fmt.Errorf("policy: the repo wiring has no AddTx function — this store has no seed door")
	}
	return f.AddTx(ctx, tx, r)
}

// PolicyRemoveTx is the transaction twin of PolicyRemove (S45 T12), refused
// by name when the wiring does not carry it, for the same reason.
func (f RepoFuncs) PolicyRemoveTx(ctx context.Context, tx Tx, id string) (bool, error) {
	if f.RemoveTx == nil {
		return false, fmt.Errorf("policy: the repo wiring has no RemoveTx function — this store has no seed door")
	}
	return f.RemoveTx(ctx, tx, id)
}

// PolicyEventInsertTx satisfies eventTxRepo.
func (f RepoFuncs) PolicyEventInsertTx(ctx context.Context, tx Tx, kind, subject, policyID, detail string, at time.Time) error {
	if f.EventInsertTx == nil {
		return fmt.Errorf("policy: the repo wiring has no EventInsertTx function — this store has no seed door")
	}
	return f.EventInsertTx(ctx, tx, kind, subject, policyID, detail, at)
}

// Tx is the transaction seam AddInTx and RemoveInTx write through (S45 T12):
// an OPAQUE handle to the caller's open transaction. It is deliberately
// methodless: Go's structural typing cannot bridge a method returning
// database/sql's Result and one returning this package's (wrong-type errors,
// measured), this package must import neither (I2), and the boundary never
// executes anything anyway — it carries the handle to the wiring hooks
// (RepoFuncs.AddTx / RemoveTx), which unwrap the concrete transaction the
// store's methods need. The seed engine's own seam does the same trick one
// layer down, and for the same reason: the SQL text lives in the store and
// the transaction must be the store's.
type Tx interface{}

// Result is the minimal statement outcome the wiring hooks report through —
// declared so the seam above imports no driver. (It exists for a wiring that
// wants to speak of rows touched without naming sql.Result; AddInTx and
// RemoveInTx need only the booleans and errors their hooks return.)
type Result interface {
	// LastInsertId reports the autoincrement id of the inserted row.
	LastInsertId() (int64, error)
	// RowsAffected reports how many rows the statement touched.
	RowsAffected() (int64, error)
}

// PolicyAcceptanceRecord satisfies Repo (T7).
func (f RepoFuncs) PolicyAcceptanceRecord(ctx context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
	if f.AcceptanceRecord == nil {
		return fmt.Errorf("policy: the repo wiring has no AcceptanceRecord function")
	}
	return f.AcceptanceRecord(ctx, policyID, targetKind, targetID, acceptedBy, at)
}

// PolicyAcceptanceListFor satisfies Repo (T7).
func (f RepoFuncs) PolicyAcceptanceListFor(ctx context.Context, targetKind, targetID string) ([]AcceptanceRow, error) {
	if f.AcceptanceList == nil {
		return nil, fmt.Errorf("policy: the repo wiring has no AcceptanceList function")
	}
	return f.AcceptanceList(ctx, targetKind, targetID)
}

// PolicyEventInsert satisfies Repo (T8). The wiring fills this from
// (*store.Store).PolicyEventInsert; its only caller through the boundary is
// recordEvent, the sole writer (legal constraint 1 — a wiring that points the
// hook at anything else is the second writer the grep forbids).
func (f RepoFuncs) PolicyEventInsert(ctx context.Context, kind, subject, policyID, detail string, at time.Time) error {
	if f.EventInsert == nil {
		return fmt.Errorf("policy: the repo wiring has no EventInsert function")
	}
	return f.EventInsert(ctx, kind, subject, policyID, detail, at)
}

// PolicyEventQuery satisfies Repo (T8).
func (f RepoFuncs) PolicyEventQuery(ctx context.Context, subject string, limit int) ([]EventRow, error) {
	if f.EventQuery == nil {
		return nil, fmt.Errorf("policy: the repo wiring has no EventQuery function")
	}
	return f.EventQuery(ctx, subject, limit)
}

// PolicyEventTrim satisfies Repo (T8). The wiring may fill it from
// (*store.Store).PolicyEventTrim; the boundary reaches it ONLY from
// TrimEvents — never from the insert path (PL11: a refusal never pays for
// housekeeping; the zero-spy test pins it).
func (f RepoFuncs) PolicyEventTrim(ctx context.Context, olderThan time.Time) (int, error) {
	if f.EventTrim == nil {
		return 0, fmt.Errorf("policy: the repo wiring has no EventTrim function")
	}
	return f.EventTrim(ctx, olderThan)
}

// PolicyEventStats satisfies Repo (T8, grown by F-13): the subject selects
// whose rows the depth/oldest describe, exactly as it does for PolicyEventQuery,
// so ReportEvents can answer about the listing it is printing. The wiring fills
// this from (*store.Store).PolicyEventStats.
func (f RepoFuncs) PolicyEventStats(ctx context.Context, subject string) (depth int, oldest time.Time, err error) {
	if f.EventStats == nil {
		return 0, time.Time{}, fmt.Errorf("policy: the repo wiring has no EventStats function")
	}
	return f.EventStats(ctx, subject)
}

// Store is the boundary. It holds the repo and nothing else — no map, no
// slice, no memo (N8 forbids a verdict cache and this file holds no policy
// cache either; TestTheStoreAdapterHoldsNoCache pins the shape).
type Store struct {
	repo Repo
}

// NewStore wires the boundary to a repo. Nothing else constructs a Store:
// one boundary, one way in.
func NewStore(repo Repo) *Store {
	return &Store{repo: repo}
}

// MaxIdentifierRunes is the cap on a policy id and on a target id, counted in
// RUNES (an attacker's "id" of 64 emoji is not 64 characters of console).
//
// The sizing, measured (SEC-014's probe): with legal, convention-plausible
// field sizes — a 64-char policy id, a 64-char target id, a 64-char loop slug
// (the loop slug cap), the longest source, an offset expiry — the longest
// Sentence() came out at ~480 runes. T8 renders detail from that sentence and
// caps it at 256 runes with marker-preserving truncation; the 64-rune cap is
// what keeps that truncation cutting padding rather than substance, and it is
// why the sentence's length budget is a property of THIS constant. 200-char
// ids measured 1024 runes, which is the same sentence with its explanation
// amputated — so over-cap is a refusal, never a truncate.
//
// Widening it is a reviewed change to a security bound: the value must move
// with a re-measure of the sentence budget and with T8's cap, not alone.
const MaxIdentifierRunes = 64

// identifierAlphabet is the allow-list for policy ids and target ids, quoted
// verbatim in every refusal. Letters, digits, '-', '_', '.' — the shapes agent
// names, loop slugs, lane-type names and MCP server names actually have
// (golang-engineer-loop, aeon-s10-policy, review, github-reviewer). It is an
// ALLOW-list because the ids are third-party text (any source may author a
// Deny) and a deny-list of bytes is a list that is always one hostile byte
// behind; anything outside these 64 runes is refused and named — newline, CR,
// quotes, ESC/CSI terminal bytes, zero-widths, spaces, non-ASCII all fall out
// of the same check (SEC-013).
const identifierAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_."

// zeroWidth are the invisible characters a "blank" can hide behind. TrimSpace
// alone does not remove them (SEC-020): "\u200b\u200b" is, to TrimSpace, a
// non-empty string — and to a human reading the list, an empty one. A name
// that renders as nothing while being stored as something is the row whose
// absence you cannot tell from presence.
var zeroWidth = map[rune]string{
	'\u200b': "zero-width space (U+200B)",
	'\u200c': "zero-width non-joiner (U+200C)",
	'\u200d': "zero-width joiner (U+200D)",
	'\u2060': "word joiner (U+2060)",
	'\ufeff': "zero-width no-break space / BOM (U+FEFF)",
}

// checkAuthorship is the ONE expression PL1 is enforced by. It lives in this file and nowhere
// else: TestTheAuthorshipRuleAppearsInExactlyOnePlace greps the module and refuses a second
// non-test occurrence. T6 wrote it as Add's own guard; the findings gate (SEC-022/SEC-042) grew
// it to the LOAD side as well, because every route from bytes or columns to a Policy passes
// through DecodePolicy — the socket, the CLI, decodeRow's re-read, and daemon.decodeFrozenSet's
// replay path. A rule enforced at the write only protects writes that went through the boundary.
//
// The condition names Allow because Allow is the only action that widens: a Deny from anyone is
// safe (PL1), and S11's D1 put Ask on the same deny-like footing — an ask takes something away
// (the autonomy to act without asking), so all four sources may author one and the condition
// needs no ask arm. The REFUSAL says so, naming both directions that would have been accepted.
func checkAuthorship(p Policy) *Reject {
	if p.Action == Allow && p.Source != Operator {
		return &Reject{Reason: fmt.Sprintf(
			"refused: Allow authored by source %q requires an operator (PL1) — a Deny or an Ask from %q would be accepted",
			p.Source, p.Source)}
	}
	return nil
}

// Add is THE write boundary: every policy the product stores passes through
// this function or it does not exist. It refuses with a *Reject (DB19 — a bad
// policy is the caller's wrong answer, not the machine's failure) and returns
// nil only once the row is in the table.
//
// The order of the checks is deliberate and each is load-bearing:
//
//  1. the closed-set parsers (T2), re-run on whatever the caller built —
//     string enums cast by hand reach this function as Policy structs with
//     invented values, and there is no path to the table that skips this
//     (SEC-015/SEC-007);
//  2. the identifier policy on the two author-supplied strings — id and target
//     id (SEC-013/SEC-020);
//  3. PL1 — the pinned expression, now held by checkAuthorship just above,
//     which Add and DecodePolicy both CALL (T19's helper/call split, forced
//     by the one-place grep). It runs on the caller's fields so the grep
//     names one expression; after step 1 those fields are proven members of
//     the closed sets;
//  4. the per-type validator (T5) — the ONLY call site of ValidatorFor on a
//     write ("no second validator" is structural: the CLI and every surface
//     above re-present this refusal, they do not re-run it), propagated with
//     the type named.
//
// Add sets nothing the caller did not choose except CreatedAt, which comes
// from `now` (forwarded into the row here and stored at the repo layer), and
// Subject, which is derived from the target and is not persisted.
//
// A store failure (disk, constraint clash) also arrives as a *Reject — the
// caller's answer is the same in both cases (tell the human, the policy is NOT
// in force), and Add keeping its single pinned return shape (D3's discipline)
// outranked giving infra failures a second channel. The Reason says plainly
// which kind it is.
// ValidateForAdd is Add's steps 1-4, pulled out so S35 T2's proposal
// validator can run the EXACT SAME refusal logic Add runs, without writing a
// row — a `policy_add` proposal must be refused at create time with Add's
// own reason, never a second, divergent check (TD3). It touches no store
// state (every check here is pure), which is what makes it safe to call from
// a daemon path that must never reach the write boundary (brief §4's
// guard-blind invariant: validating a payload is not authoring one). Add
// itself calls this now — the single extraction keeps checkAuthorship's
// per-file call count in store.go at the pinned 1 def + 1 call
// (TestTheAuthorshipRuleAppearsInExactlyOnePlace), since the call moved
// within this file rather than leaving it.
func ValidateForAdd(p Policy) (Policy, *Reject) {
	// 1. Re-run the closed set over every enum field. Put the parsed values
	//    into use below; the caller's originals are strings that happen to
	//    have enum-shaped TYPES.
	typ, rej := ParsePolicyType(string(p.Type))
	if rej != nil {
		return Policy{}, rej
	}
	act, rej := ParseAction(string(p.Action))
	if rej != nil {
		return Policy{}, rej
	}
	src, rej := ParseSource(string(p.Source))
	if rej != nil {
		return Policy{}, rej
	}
	target, rej := ParseTarget(p.Target.Kind, p.Target.ID)
	if rej != nil {
		return Policy{}, rej
	}

	// 2. The identifier policy, naming the field it refused.
	if rej := checkIdentifier("policy id", p.ID); rej != nil {
		return Policy{}, rej
	}
	if target.Kind != AllAgents {
		if rej := checkIdentifier(fmt.Sprintf("target id for kind %q", string(target.Kind)), target.ID); rej != nil {
			return Policy{}, rej
		}
	}

	// The one place — the expression itself lives in checkAuthorship above,
	// and this call and DecodePolicy's are its only two uses. A rule enforced
	// in two places is a rule with a gap; a rule CALLED from two places is
	// one rule with two enforcement points.
	if rej := checkAuthorship(p); rej != nil {
		return Policy{}, rej
	}

	// 4. The per-type spec check. An unregistered/unvalidatable type refuses
	//    by name (D10): production registers validators for all seven today,
	//    and if that ever stops being true, the answer is a refusal that says
	//    what to fix, not a stored row nothing has ever read.
	val, ok := ValidatorFor(typ)
	if !ok {
		return Policy{}, rejectf("refused: policy %q has type %q, which has no validator registered; an unvalidatable "+
			"type is not a permission waiting to be granted (D10)", p.ID, string(typ))
	}
	canonical := p
	canonical.Type, canonical.Action, canonical.Source, canonical.Target = typ, act, src, target
	if rej := val.Validate(canonical); rej != nil {
		return Policy{}, rejectf("policy %q · type %q: %s", p.ID, string(typ), rej.Reason)
	}
	return canonical, nil
}

func (s *Store) Add(ctx context.Context, p Policy, now time.Time) *Reject {
	canonical, rej := ValidateForAdd(p)
	if rej != nil {
		return rej
	}

	// The plain door names NO provenance: the row crosses the seam with an
	// empty Origin — PolicyAdd's INSERT does not carry the column at all,
	// and the schema's DEFAULT 'user' is the stored answer. Only the seed
	// door names one, and it must name a real one (AddInTx).
	row := addRow(canonical, "", now)
	if err := s.repo.PolicyAdd(ctx, row); err != nil {
		return &Reject{Reason: fmt.Sprintf("refused: policy %q was NOT written — the store failed: %v", canonical.ID, err)}
	}
	return nil
}

// The three provenance values the policy.origin column admits (S45 T12,
// PM15). They are PROVENANCE, not policy vocabulary: the closed-set rule
// (PL3) governs what a rule SAYS, and origin names only where the row came
// from — the evaluator, the gate and inforce read none of it (PM11), and
// Add never consults this list to widen a permission. A row the seed process
// wrote is OriginSystem; everything an operator authored is OriginUser;
// OriginImport is the reserved third (an imported package's rows, whenever
// that door is built).
const (
	OriginSystem = "system"
	OriginUser   = "user"
	OriginImport = "import"
)

// ParseOrigin is the provenance parser (S45 T12): the same shape every
// closed-set parser in this package has — a refused value names itself. It
// parses PROVENANCE, not policy vocabulary (PM15): the filter's Origin
// criterion and the seed door's origin argument are its only callers, and
// the enforcement path never reaches for it (PM11).
func ParseOrigin(s string) (string, *Reject) {
	switch s {
	case OriginSystem, OriginUser, OriginImport:
		return s, nil
	}
	return "", rejectf("refused: origin %q is not one of %s, %s, %s (S45 T12 provenance)", s, OriginSystem, OriginUser, OriginImport)
}

// addRow is the ONE row construction the write boundary owns (S45 T12):
// Add and AddInTx build through it, so the plain door and the seed door
// cannot drift a column apart — the same builder, the same handover, the
// seed door merely names its provenance. It is pure (no store, no clock):
// `now` arrives as the parameter every instant in this package arrives as
// (D5).
//
// Handover. The row carries no facts the caller did not choose — the
// exception is CreatedAt, which is `now` (D5: the clock arrives as a
// parameter), rendered RFC3339Nano so an expiry written here compares
// exactly where T3 proved the boundary sits.
func addRow(canonical Policy, origin string, now time.Time) Row {
	notAfter := ""
	if canonical.NotAfter != nil {
		notAfter = canonical.NotAfter.UTC().Format(time.RFC3339Nano)
	}
	spec := string(canonical.Spec)
	if strings.TrimSpace(spec) == "" {
		spec = "{}"
	}
	return Row{
		ID:         canonical.ID,
		Type:       string(canonical.Type),
		TargetKind: string(canonical.Target.Kind),
		TargetID:   canonical.Target.ID,
		Action:     string(canonical.Action),
		Source:     string(canonical.Source),
		Subject:    canonical.Target.String(),
		SpecJSON:   spec,
		NotAfter:   notAfter,
		CreatedAt:  now.UTC().Format(time.RFC3339Nano),
		Origin:     origin,
	}
}

// txRepo is the optional half of the repo seam the seed door needs: a wiring
// fills the two transaction hooks (RepoFuncs.AddTx / RemoveTx) or does not,
// and the boundary refuses the door by name when it did not. It is asserted
// per call rather than demanded at construction so every existing wiring —
// the plain CLI door, every test fake — keeps compiling and keeps answering
// exactly as it did (the seed door is additive, not a widening).
type txRepo interface {
	PolicyAddTx(ctx context.Context, tx Tx, r Row) error
	PolicyRemoveTx(ctx context.Context, tx Tx, id string) (bool, error)
}

// eventTxRepo is the transaction half of the ONE audit row the seed writes
// beside its row (S45 T12, PM8): RecordAuthoredInTx asserts this of the
// wiring, so a store without the transaction event hook has no seed door —
// the absence is named, never worked around. Separate from txRepo on
// purpose: the two asserts fail (and say) apart.
type eventTxRepo interface {
	PolicyEventInsertTx(ctx context.Context, tx Tx, kind, subject, policyID, detail string, at time.Time) error
}

// AddInTx is the seed door's Add (S45 T12, PM15): the write a declared seed
// set makes INSIDE the engine's open transaction, where the row and its
// ledger row commit together. It is not a second write path and it holds no
// second rule — it runs the SAME ValidateForAdd (the one boundary, PL1 once
// and only once: TestTheAuthorshipRuleAppearsInExactlyOnePlace counts the
// shared helper's definition and its single call and refuses a third
// enforcement point) and the SAME addRow builder Add builds through; the
// only thing it adds is the origin it names, and the transaction it writes
// on.
//
// The origin is validated HERE, by name, before the row reaches the store:
// the column CHECK is the net under the caller (the same division the
// vocabulary has always had), and a seed door silently writing a blank
// provenance would be the row whose origin nobody can state. A nil tx is
// refused — the transaction variant exists to join the caller's transaction,
// and a nil there is the missing commit, not an absent one.
//
// Everything else is Add's shape: a *Reject naming what was refused, store
// failures folded into the same single return channel (D3's discipline).
func (s *Store) AddInTx(ctx context.Context, tx Tx, p Policy, origin string, now time.Time) *Reject {
	if tx == nil {
		return rejectf("refused: policy %q arrived for a transaction write with no transaction; the seed "+
			"door joins the engine's transaction, and a nil one is the missing commit, not an absent one", p.ID)
	}
	switch origin {
	case OriginSystem, OriginUser, OriginImport:
	default:
		return rejectf("refused: policy %q names origin %q; provenance is one of %s, %s, %s (S45 T12) — "+
			"an origin the column will refuse is named here first", p.ID, origin, OriginSystem, OriginUser, OriginImport)
	}
	canonical, rej := ValidateForAdd(p)
	if rej != nil {
		return rej
	}
	tr, ok := s.repo.(txRepo)
	if !ok {
		return rejectf("refused: policy %q was NOT written — this store's wiring carries no transaction "+
			"add; the seed door is absent here, not bypassed (S45 T12)", canonical.ID)
	}
	row := addRow(canonical, origin, now)
	if err := tr.PolicyAddTx(ctx, tx, row); err != nil {
		return &Reject{Reason: fmt.Sprintf("refused: policy %q was NOT written — the store failed: %v", canonical.ID, err)}
	}
	return nil
}

// RemoveInTx is Remove's twin on the caller's transaction (S45 T12): the
// engine's update path is delete-then-insert inside one transaction (SD3),
// and the judgement stays exactly where Remove keeps it — none at this layer.
// Straight pass-through, same wiring-refusal shape as AddInTx.
func (s *Store) RemoveInTx(ctx context.Context, tx Tx, id string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("policy: remove policy %q on no transaction; the seed door joins the "+
			"engine's transaction, and a nil one is the missing commit, not an absent one", id)
	}
	tr, ok := s.repo.(txRepo)
	if !ok {
		return false, fmt.Errorf("policy: this store's wiring carries no transaction remove — the seed " +
			"door is absent here, not bypassed (S45 T12)")
	}
	return tr.PolicyRemoveTx(ctx, tx, id)
}

// Remove deletes a policy by id, reporting whether one existed. Straight
// pass-through: the boundary refuses to invent judgement here, because removal
// is the operator's surface only (the CLI, T12 — the socket never registers an
// authoring method, T13/PL15) and deleting an unknown-shaped id deletes
// nothing.
func (s *Store) Remove(ctx context.Context, id string) (bool, error) {
	return s.repo.PolicyRemove(ctx, id)
}

// List reads every stored policy — expired rows included (D6/D8: expiry is
// Evaluate's decision, made against `at`, and an audit answer that depends on
// when it is asked is not an audit). `at` is deliberately NOT used to filter
// anything; it exists so callers hand List the same instant they will hand
// Evaluate and the two answers cannot be about different worlds. If List ever
// grows a use for it, that use is presentation, never omission.
//
// Every row is re-read through DecodePolicy (the SEC-015 argument on the read
// side): a row whose vocabulary escaped the closed set — a hand-edited table,
// a row written before this boundary existed — is REFUSED loudly with its id
// named, and the whole List refuses. Fail-closed twice over: a silently
// dropped row can silently drop a Deny, and a permission nobody authored is
// the failure this slice was chartered to make impossible.
//
// There is no cache. Two calls are two repo calls (N8); the counting fake in
// store_test.go is the assertion.
func (s *Store) List(ctx context.Context, at time.Time) ([]Policy, *Reject) {
	_ = at // present for the contract, absent from the decision — see above.
	rows, err := s.repo.PolicyListAll(ctx)
	if err != nil {
		return nil, &Reject{Reason: fmt.Sprintf("refused: the policy list was NOT read — the store failed: %v", err)}
	}
	out := make([]Policy, 0, len(rows))
	for _, r := range rows {
		p, rej := decodeRow(r)
		if rej != nil {
			return nil, rej
		}
		out = append(out, p)
	}
	return out, nil
}

// Listed is one policy as the fail-closed read re-decoded it, beside the
// row's own `created_at` rendering (S45 T2, PM5: the detail reads print when
// a rule was written; the identity view carries no other instant). The
// instant travels byte for byte as decodeEvent returns audit rows
// (record.go's rule): the values are what the row says — a reader that
// "fixes" a timestamp is a reader inventing what the row meant, and a
// surface that needs a different rendering formats what it is given.
type Listed struct {
	Policy
	CreatedAt string
	// Origin is the row's provenance (S45 T12, PM15) — 'system' for a row
	// the seed process wrote, 'user' for an operator's, 'import' reserved.
	// It rides the DETAIL read beside CreatedAt because PM11 keeps the
	// enforcement path blind to it: List (the half Evaluate's inputs come
	// from) stays origin-free and only the read doors ask. A row written
	// before the origin migration backfilled carries 'user' (the column's
	// DEFAULT) — the same answer Add gives today, not a gap.
	Origin string
}

// ListDetailed reads the whole set exactly as List does — every row back
// through DecodePolicy, one escaped row refusing the whole answer loudly
// (SEC-015) — and carries each row's CreatedAt beside it. It exists for the
// detail reads (`policy.list`'s additive fields, `policy.get`) and nothing
// else; List above stays the half that enforces is read, so no later caller
// reaches past the identity view by accident.
//
// `at` is present for the contract List keeps and ABSENT from the decision:
// expiry belongs to `Filter` and to Evaluate, never to the read behind them
// (D6).
func (s *Store) ListDetailed(ctx context.Context, at time.Time) ([]Listed, *Reject) {
	_ = at // present for the contract, absent from the decision — List's rule, carried.
	rows, err := s.repo.PolicyListAll(ctx)
	if err != nil {
		return nil, &Reject{Reason: fmt.Sprintf("refused: the policy list was NOT read — the store failed: %v", err)}
	}
	out := make([]Listed, 0, len(rows))
	for _, r := range rows {
		p, rej := decodeRow(r)
		if rej != nil {
			return nil, rej
		}
		out = append(out, Listed{Policy: p, CreatedAt: r.CreatedAt, Origin: r.Origin})
	}
	return out, nil
}

// decodeRow rebuilds a Policy from the row's columns by going the way every
// other input comes: through DecodePolicy, from JSON, past the closed-set
// parsers. Marshalling the envelope rather than filling the struct by hand is
// the point — filling the struct is the bypass this whole file is arguing
// against.
//
// Say the consequence out loud (SEC-022, the load-side half of T19): a store
// holding a row Add would never have written — an `allow` authored by a
// non-operator, INCLUDING one written before the load-side check existed —
// now refuses the whole List with that row's id named, which means the gate
// denies everything until the operator removes or re-authors that rule. This
// is the fail-closed answer this file already argues for: a silently dropped
// row can silently drop a Deny, so a bad row is refused loudly instead —
// skip-and-continue is not an option here. The refusal is actionable: the CLI
// is the only authoring surface (PL15), so `aeon policy remove <that id>`
// deletes the forged row and `aeon policy add` re-authors the rule through
// the boundary as an operator; the socket never carries a write.
func decodeRow(r Row) (Policy, *Reject) {
	spec := strings.TrimSpace(r.SpecJSON)
	if spec == "" {
		spec = "{}"
	}
	if !json.Valid([]byte(spec)) {
		return Policy{}, rejectf("refused: stored policy %q carries a spec that is not valid JSON; that row did "+
			"not come through the write boundary (or the table was edited by hand) — refusing the list rather than "+
			"evaluating a row nothing ever validated", r.ID)
	}
	env := map[string]any{
		"id":     r.ID,
		"type":   r.Type,
		"action": r.Action,
		"source": r.Source,
		"target": map[string]string{"kind": r.TargetKind, "id": r.TargetID},
		"spec":   json.RawMessage(spec),
	}
	if r.NotAfter != "" {
		env["not_after"] = r.NotAfter
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return Policy{}, rejectf("refused: stored policy %q could not be re-read: %v", r.ID, err)
	}
	p, rej := DecodePolicy(raw)
	if rej != nil {
		return Policy{}, rejectf("refused: stored policy %q is not a policy the engine accepts (%s); the row did "+
			"not come through the write boundary (or the table was edited by hand). Refusing the whole list is the "+
			"fail-closed answer: dropping the row silently can drop a Deny with it", r.ID, rej.Reason)
	}
	return p, nil
}

// acceptedSince is the per-target acceptance read the in-force answer is built
// from: "which of these policies has THIS target been told about, and since
// when", as one lookup from policy id to the first recorded instant.
//
// It is T7's read, not a second one: the same `Repo.PolicyAcceptanceListFor`
// call AcceptancesFor makes, and every row back through the same
// `decodeAcceptance` — there is no second decode here to drift from the one
// the record boundary owns, and a row whose vocabulary escaped the closed set
// still refuses the whole answer with its policy id named (silently dropping a
// row is how an audit loses the one acceptance that proved someone knew).
//
// What it adds over AcceptancesFor is shape, and nothing else: the in-force
// answer needs the instant as an instant and keyed by policy, so the FIRST
// record wins twice over — the table's own key already keeps one row per
// (policy, target), and an out-of-order or hand-edited duplicate keeps its
// EARLIEST instant, because "since when" only ever means the first time.
//
// It lives beside the other read, on the boundary, and it is called from
// InForceFor and from nowhere near a verdict: acceptance never conditions
// enforcement (PL10/D13), and the committed probe that counts it
// (TestTheEnforcementPathReadsNoAcceptanceRows) holds the List → Evaluate →
// RecordRefusal path at zero reads. A read that is allowed to exist must still
// be kept off the path where it would become a gate.
func (s *Store) acceptedSince(ctx context.Context, target Target) (map[string]time.Time, *Reject) {
	rows, err := s.repo.PolicyAcceptanceListFor(ctx, string(target.Kind), target.ID)
	if err != nil {
		return nil, &Reject{Reason: fmt.Sprintf(
			"refused: the in-force answer for %s was NOT computed — the acceptance record could not be read: %v",
			target.String(), err)}
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		a, rej := decodeAcceptance(r)
		if rej != nil {
			return nil, rej
		}
		at, rej := acceptanceInstant(a)
		if rej != nil {
			return nil, rej
		}
		if cur, ok := out[a.PolicyID]; !ok || at.Before(cur) {
			out[a.PolicyID] = at
		}
	}
	return out, nil
}

// acceptanceInstant rebuilds the instant out of the record's own rendering —
// the same layout decodeAcceptance has just proven it parses, re-parsed here
// because the answer needs a value, not a string it has to trust. Trusting a
// neighbour's contract and depending on it are different things: if the decode
// check is ever relaxed, this refuses loudly ("since when", with no when)
// rather than handing the answer a zero instant posing as the year 1.
func acceptanceInstant(a Acceptance) (time.Time, *Reject) {
	at, err := time.Parse(time.RFC3339Nano, a.AcceptedAt)
	if err != nil {
		return time.Time{}, rejectf("refused: the stored acceptance of policy %q by %s carries accepted_at %q, "+
			"which does not read as an instant; the in-force answer cannot say \"since\" without a when, so it "+
			"refuses rather than inventing one (%v)", a.PolicyID, a.Target.String(), a.AcceptedAt, err)
	}
	return at.UTC(), nil
}

// checkIdentifier is the identifier policy both id fields pass through,
// naming the field it refused (so the author fixes the field, not the file).
// Every branch refuses; none of them trims, strips or shortens — a gate that
// silently edits its input is the control that fails closed on nobody.
//
// The order: cap first (bound the work on hostile input), then blankness
// — where blank means no VISIBLE text, because TrimSpace lets a zero-width
// masquerade as content (SEC-020) — then the per-rune allow-list.
func checkIdentifier(field, value string) *Reject {
	if n := utf8.RuneCountInString(value); n > MaxIdentifierRunes {
		return rejectf("refused: %s is %d runes; the cap is %d. It is refused, not truncated: an identifier "+
			"shortened behind its author's back is a different rule wearing the old one's name, and the id is "+
			"what every verdict and every recorded event names — choose a shorter name (SEC-014)",
			field, n, MaxIdentifierRunes)
	}
	if value == "" {
		return rejectf("refused: %s is empty; a name that cannot be seen cannot be audited or explained in a "+
			"verdict", field)
	}
	if visibleRuneCount(value) == 0 {
		return rejectf("refused: %s %s holds no visible character — it is blank to a human and non-empty to "+
			"TrimSpace (zero-width characters like U+200B hide exactly there); a name that renders as nothing "+
			"is the row whose presence you cannot tell from absence (SEC-020)", field, quotePreview(value))
	}
	pos := 0
	for _, r := range value {
		if strings.ContainsRune(identifierAlphabet, r) {
			pos++
			continue
		}
		return rejectf("refused: %s %s carries %s at position %d; %s is limited to the alphabet %q — letters, "+
			"digits, '-', '_', '.' — because an id is third-party text (any source may author a Deny) and it "+
			"reaches an operator's console through every verdict (SEC-013)",
			field, quotePreview(value), describeRune(r), pos, field, identifierAlphabet)
	}
	return nil
}

// CheckIdentifier is the exported face of the ONE identifier policy above,
// for the read-side surfaces that take an identifier as CALLER input —
// T13's socket methods, whose subject and target params arrive from the app
// and reach an operator's screen through the renderers (SEC-046: the closed
// -shape parsers on the read path — ParseTarget included — check vocabulary
// and coherence, not the alphabet, so a surface that forwards a raw string
// forwards whatever bytes the caller chose).
//
// It is DELEGATION, not a second validator (the T12 rule, established at the
// CLI's flag parsers): this function states no rule of its own — every rule,
// every refusal, every cap lives in checkIdentifier and nowhere else. A
// surface that re-implemented the alphabet would be a second place for the
// rule to drift, which is the standing rejection this package exists to hold.
func CheckIdentifier(field, value string) *Reject { return checkIdentifier(field, value) }

// visibleRuneCount counts what a human would read: everything that is neither
// whitespace nor one of the invisibles in zeroWidth.
func visibleRuneCount(value string) int {
	n := 0
	for _, r := range value {
		if unicode.IsSpace(r) {
			continue
		}
		if _, hidden := zeroWidth[r]; hidden {
			continue
		}
		n++
	}
	return n
}

// describeRune names the refused character by category and code point, so the
// refusal is actionable even though the raw byte is not echoed (the refusal
// text itself reaches a terminal — echoing the hostile byte into the error
// that refuses it is SEC-013 walking back in the front door).
func describeRune(r rune) string {
	if name, hidden := zeroWidth[r]; hidden {
		return "the invisible " + name
	}
	switch {
	case r < 0x20:
		return fmt.Sprintf("the control character U+%04X", r)
	case r == 0x7f:
		return "the control character U+007F (DEL)"
	case r > 0x7f && r <= 0x9f:
		return fmt.Sprintf("the control character U+%04X", r)
	}
	return fmt.Sprintf("the character U+%04X", r)
}

// quotePreview renders up to 48 runes of the offending value through
// strconv.Quote — which escapes newlines, quotes, ESC and every other
// terminal-active byte, so the refusal can show what was refused without
// becoming the injection it refuses.
func quotePreview(value string) string {
	runes := []rune(value)
	const keep = 48
	if len(runes) > keep {
		value = string(runes[:keep]) + "…"
	}
	return strconv.Quote(value)
}
