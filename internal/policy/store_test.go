package policy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T6 — the write boundary. PL1 (an Allow only from an operator; a Deny from
// anyone) is the slice's whole security argument, and these tests are where it
// is proven true: at the write, from every non-operator source, naming the
// source; the single-place grep (D3); the re-validation duty (SEC-015/007);
// the identifier policy (SEC-013/020); expired-returned (D6) and no-cache (N8).

// ── the fake repo ────────────────────────────────────────────────────────────

// fakeRepo is the counting fake: every call is tallied (the no-cache claim is
// a COUNT), every handed row is kept (the "sets nothing but CreatedAt" claim
// is a field-for-field comparison), and every error can be made to fire on
// command (the store-failure fold).
type fakeRepo struct {
	rows        []policy.Row
	addErr      error
	listErr     error
	removeErr   error
	addCalls    int
	listCalls   int
	removeCalls int
	removed     []string

	// The T7 acceptance state. The methods on these fields live in
	// record_test.go, where the acceptance story is told. The fake mirrors
	// the table's PRIMARY KEY faithfully — a re-record keeps the FIRST row
	// (instant included) and is never an error — because a boundary test
	// against a lying double proves the double; the real ON CONFLICT SQL is
	// tested against SQLite in internal/store/policy_test.go.
	acceptances  []policy.AcceptanceRow
	recorded     []recordedAcceptance
	recordErr    error
	listForErr   error
	recordCalls  int
	listForCalls int

	// The T8 event state. The methods on these fields live in
	// record_test.go, where the record story is told (the same discipline as
	// the acceptance half above). The fake mirrors policy_event faithfully:
	// AUTOINCREMENT ids from 1, the subject split at the FIRST colon exactly
	// as the store splits it, the fixed-width `at` rendering the store uses
	// (so a decode that rejects the layout would fail here too), newest-first
	// (at DESC, id DESC) query order, and a trim that is strict `<`. The real
	// SQL — the trim's covering-index form, the NULL rules, the fixed-width
	// bytes — is tested against SQLite in internal/store/policy_test.go.
	events           []fakeEvent
	recordedEvents   []recordedEvent
	lastTrim         time.Time
	lastStatsSubject string // F-13: the subject the depth/oldest read was asked FOR
	eventErr         error
	queryErr         error
	trimErr          error
	statsErr         error
	eventInsertCalls int
	queryCalls       int
	trimCalls        int
	statsCalls       int
}

func (f *fakeRepo) PolicyAdd(_ context.Context, r policy.Row) error {
	f.addCalls++
	if f.addErr != nil {
		return f.addErr
	}
	f.rows = append(f.rows, r)
	return nil
}

func (f *fakeRepo) PolicyRemove(_ context.Context, id string) (bool, error) {
	f.removeCalls++
	f.removed = append(f.removed, id)
	if f.removeErr != nil {
		return false, f.removeErr
	}
	for i, r := range f.rows {
		if r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepo) PolicyListAll(_ context.Context) ([]policy.Row, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]policy.Row, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// addAt is the instant every Add in this file is made at. Fixed (D5): the
// clock is an input, and CreatedAt is asserted against it character for
// character.
var addAt = time.Date(2026, 10, 19, 12, 0, 0, 123456789, time.UTC)

func allowSpec() json.RawMessage    { return json.RawMessage(`{"toolName":"bash","state":"allow"}`) }
func denySpec() json.RawMessage     { return json.RawMessage(`{"toolName":"bash","state":"deny"}`) }
func askSpec() json.RawMessage      { return json.RawMessage(`{"toolName":"bash","state":"ask"}`) }
func spec(s string) json.RawMessage { return json.RawMessage(s) }

// boundaryPolicy builds a ToolAccess policy through the parsers where the shape
// has parsers — everything deliberately hand-built (a casted enum, a raw
// target) is constructed at the call site, which is exactly the point of the
// SEC-015 cases.
func boundaryPolicy(t *testing.T, id string, kind policy.TargetKind, targetID string,
	action policy.Action, source policy.Source, s json.RawMessage) policy.Policy {
	t.Helper()
	target, rej := policy.ParseTarget(kind, targetID)
	if rej != nil {
		t.Fatalf("helper built an illegal target %q/%q: %s", kind, targetID, rej.Reason)
	}
	return policy.Policy{ID: id, Type: policy.ToolAccess, Target: target, Action: action, Source: source, Spec: s}
}

func mustAdd(t *testing.T, s *policy.Store, p policy.Policy) {
	t.Helper()
	if rej := s.Add(context.Background(), p, addAt); rej != nil {
		t.Fatalf("Add(%q) refused: %s", p.ID, rej.Reason)
	}
}

func refuseReason(t *testing.T, s *policy.Store, p policy.Policy) string {
	t.Helper()
	rej := s.Add(context.Background(), p, addAt)
	if rej == nil {
		t.Fatalf("Add(%q) was ACCEPTED; it must be refused", p.ID)
	}
	return rej.Reason
}

// ── D2 — the authorship refusals, named per source ───────────────────────────

func assertNonOperatorAllowRefused(t *testing.T, src policy.Source) {
	t.Helper()
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	p := boundaryPolicy(t, "self-grant", policy.AgentKind, "boss", policy.Allow, src, allowSpec())
	got := refuseReason(t, s, p)

	for _, want := range []string{
		fmt.Sprintf("%q", string(src)),                                         // names the source, quoted
		"requires an operator (PL1)",                                           // the rule, by its lock
		fmt.Sprintf("a Deny or an Ask from %q would be accepted", string(src)), // the two ways through
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%s: the refusal does not contain %q: %s", string(src), want, got)
		}
	}
	if repo.addCalls != 0 || len(repo.rows) != 0 {
		t.Errorf("%s: the repo saw %d writes — a refusal must write nothing", string(src), repo.addCalls)
	}
}

func TestAnAllowFromAgentIsRefusedAtWriteTime(t *testing.T) {
	assertNonOperatorAllowRefused(t, policy.Agent)
}

func TestAnAllowFromMCPServerIsRefusedAtWriteTime(t *testing.T) {
	assertNonOperatorAllowRefused(t, policy.MCPServer)
}

func TestAnAllowFromImportIsRefusedAtWriteTime(t *testing.T) {
	assertNonOperatorAllowRefused(t, policy.Import)
}

func TestTheRefusalNamesTheSourceAndSaysADenyOrAskWouldBeAccepted(t *testing.T) {
	// The full shape, pinned character for character — the person hitting this
	// is usually trying to do something legitimate in the wrong direction, and
	// the sentence that says so is the pinned one (the seed's "the refusal
	// says that a Deny from that source WOULD be accepted", widened by S11's
	// D1 to name both deny-like directions the boundary admits).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	p := boundaryPolicy(t, "mcp-wish", policy.MCPServerKind, "files", policy.Allow, policy.MCPServer, allowSpec())
	got := refuseReason(t, s, p)

	want := `refused: Allow authored by source "mcp_server" requires an operator (PL1) — a Deny or an Ask from "mcp_server" would be accepted`
	if got != want {
		t.Errorf("the refusal drifted:\n got %q\nwant %q", got, want)
	}
	if repo.addCalls != 0 {
		t.Errorf("the refusal wrote anyway (%d calls)", repo.addCalls)
	}
}

// ── what IS accepted ─────────────────────────────────────────────────────────

func TestADenyFromEverySourceIsAccepted(t *testing.T) {
	// All four, by enumeration over the closed list itself — a fifth source
	// could not be constructed anyway, and a missing one fails HERE rather
	// than in whichever surface forgot it.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	for _, src := range policy.AllSources() {
		t.Run(string(src), func(t *testing.T) {
			p := boundaryPolicy(t, "narrow-"+string(src), policy.AgentKind, "boss", policy.Deny, src, denySpec())
			mustAdd(t, s, p)
			if repo.addCalls != len(repo.rows) {
				t.Fatalf("%d accepts, %d rows — the fake lost one", repo.addCalls, len(repo.rows))
			}
			got := repo.rows[len(repo.rows)-1]
			if got.Source != string(src) {
				t.Errorf("the source was rewritten: stored %q, authored %q", got.Source, string(src))
			}
			if got.Action != "deny" {
				t.Errorf("the action was rewritten: %q", got.Action)
			}
		})
	}
}

func TestAnAskFromEverySourceIsAccepted(t *testing.T) {
	// S11's D1, the write-boundary half: ASK joins DENY on the deny-like side
	// of the boundary. Any of the four sources may author one, on Add's write
	// path AND on DecodePolicy's load path — the same routes the SEC-022
	// findings gate pinned for deny, because an ask takes something away (the
	// autonomy to act without asking) and can therefore never widen anyone's
	// permissions. The pin that keeps this honest is that NO second
	// enforcement expression appeared: checkAuthorship still names only Allow
	// (TestTheAuthorshipRuleAppearsInExactlyOnePlace counts them).
	t.Run("Add", func(t *testing.T) {
		repo := &fakeRepo{}
		s := policy.NewStore(repo)
		for _, src := range policy.AllSources() {
			t.Run(string(src), func(t *testing.T) {
				p := boundaryPolicy(t, "ask-"+string(src), policy.AgentKind, "boss", policy.Ask, src, askSpec())
				mustAdd(t, s, p)
				if repo.addCalls != len(repo.rows) {
					t.Fatalf("%d accepts, %d rows — the fake lost one", repo.addCalls, len(repo.rows))
				}
				got := repo.rows[len(repo.rows)-1]
				if got.Source != string(src) {
					t.Errorf("the source was rewritten: stored %q, authored %q", got.Source, string(src))
				}
				if got.Action != "ask" {
					t.Errorf("the action was rewritten: %q", got.Action)
				}
			})
		}
	})
	t.Run("DecodePolicy", func(t *testing.T) {
		// The load side — the frozen manifest, the socket payload, the CLI
		// bytes: every route that reaches a Policy without passing Add.
		for _, src := range policy.AllSources() {
			t.Run(string(src), func(t *testing.T) {
				raw := `{"id":"ask-load","type":"tool_access","target":{"kind":"agent","id":"boss"},` +
					`"action":"ask","spec":{"toolName":"bash","state":"ask"},"source":"` + string(src) + `"}`
				p, rej := policy.DecodePolicy([]byte(raw))
				if rej != nil {
					t.Fatalf("an ask from %q was refused on the load side: %s", src, rej.Reason)
				}
				if p.Action != policy.Ask {
					t.Errorf("decoded action %q, want ask", p.Action)
				}
			})
		}
	})
	// And the side that stays closed: Allow is still operator-only. The
	// per-source refusals above pin the three refusals; the operator accept
	// lives in TestAnAllowFromOperatorIsAccepted. Between them the full
	// matrix — allow: operator only; deny and ask: every source — is covered.
}

func TestAnAllowFromOperatorIsAccepted(t *testing.T) {
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	p := boundaryPolicy(t, "grant-bash", policy.AgentKind, "boss", policy.Allow, policy.Operator, allowSpec())
	mustAdd(t, s, p)
	if repo.addCalls != 1 {
		t.Fatalf("the repo saw %d writes, want 1", repo.addCalls)
	}
}

func TestAddWritesTheRowTheCallerChosePlusCreatedAtOnly(t *testing.T) {
	// "Add sets nothing the caller didn't choose except CreatedAt" — tested as
	// a field-for-field comparison, because the friendly version of this bug
	// is a boundary that "helpfully" rewrites one field and everyone forgets.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	expiry := time.Date(2027, 1, 1, 0, 0, 0, 500000000, time.UTC)
	p := boundaryPolicy(t, "grant-read", policy.LaneTypeKind, "review", policy.Allow, policy.Operator,
		json.RawMessage(`{"toolName":"read","state":"allow"}`))
	p.NotAfter = &expiry
	mustAdd(t, s, p)

	want := policy.Row{
		ID: "grant-read", Type: "tool_access", TargetKind: "lane-type", TargetID: "review",
		Action: "allow", Source: "operator", Subject: "lane-type:review",
		SpecJSON: `{"toolName":"read","state":"allow"}`,
		NotAfter: "2027-01-01T00:00:00.5Z", CreatedAt: "2026-10-19T12:00:00.123456789Z",
	}
	got := repo.rows[0]
	if got != want {
		t.Errorf("the stored row drifted:\n got %+v\nwant %+v", got, want)
	}
	// The expiry keeps nanosecond-precision through the seam (T3 proved the
	// boundary at ±1ns; a millisecond render here would move that proof into
	// the store and out of the tests).
	nano := time.Date(2027, 1, 1, 0, 0, 0, 1, time.UTC)
	p2 := boundaryPolicy(t, "nanosecond", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
	p2.NotAfter = &nano
	mustAdd(t, s, p2)
	if repo.rows[1].NotAfter != "2027-01-01T00:00:00.000000001Z" {
		t.Errorf("the nanosecond expiry came out %q", repo.rows[1].NotAfter)
	}
}

func TestAddWithoutExpiryLeavesNotAfterEmpty(t *testing.T) {
	// The seam's "" is the store's NULL is "no expiry" (PL7) — the adapter
	// must not invent an instant when the caller passed none.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	mustAdd(t, s, boundaryPolicy(t, "forever", policy.AllAgents, "", policy.Deny, policy.Operator, denySpec()))
	if repo.rows[0].NotAfter != "" {
		t.Errorf("NotAfter is %q for a policy with none", repo.rows[0].NotAfter)
	}
	if repo.rows[0].Subject != "all-agents" {
		t.Errorf("Subject is %q, want the bare kind", repo.rows[0].Subject)
	}
}

func TestRemovePassesThroughToTheRepo(t *testing.T) {
	repo := &fakeRepo{}
	repo.rows = []policy.Row{{ID: "here"}}
	s := policy.NewStore(repo)

	existed, err := s.Remove(context.Background(), "here")
	if err != nil || !existed {
		t.Fatalf("removing an existing row: %v %v", existed, err)
	}
	existed, err = s.Remove(context.Background(), "gone")
	if err != nil || existed {
		t.Fatalf("removing a missing row: %v %v", existed, err)
	}
	if repo.removeCalls != 2 || strings.Join(repo.removed, ",") != "here,gone" {
		t.Errorf("the repo saw %d calls %v", repo.removeCalls, repo.removed)
	}

	// And the error channel is the repo's own (DB19: Remove has no domain
	// refusal to make).
	failing := &fakeRepo{removeErr: errors.New("disk on fire")}
	if _, err := policy.NewStore(failing).Remove(context.Background(), "x"); err == nil ||
		!strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("the store failure did not travel: %v", err)
	}
}

func TestAddFoldsAStoreFailureIntoARefusalNamingTheStore(t *testing.T) {
	// The documented exception to "error is for infrastructure": Add keeps ONE
	// return shape (D3's discipline), and the fold says plainly which kind of
	// refusal it is — the caller's answer, "tell the human, the policy is not
	// in force", is the same either way.
	repo := &fakeRepo{addErr: errors.New("disk on fire")}
	s := policy.NewStore(repo)
	got := refuseReason(t, s, boundaryPolicy(t, "doomed", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec()))
	for _, want := range []string{"was NOT written", "disk on fire"} {
		if !strings.Contains(got, want) {
			t.Errorf("the fold lost %q: %s", want, got)
		}
	}
}

// ── D3 — one rule, one place, asserted by grep ───────────────────────────────

// authorshipRuleExpr is the pinned expression. The constant lives in the test
// so the rule cannot move its own guard by being renamed in one place only.
const authorshipRuleExpr = "p.Action == Allow && p.Source != Operator"

// authorshipViolation is the guard as a pure function over text — testable
// without editing the tree, which is the house standard for every guard here.
// Test files are exempt: the pinned refusal text lives in this very file's
// assertions (and the seed doc), which is prose about the rule, not a second
// enforcement of it.
func authorshipViolation(text string, isTest bool) bool {
	if isTest {
		return false
	}
	return strings.Contains(text, authorshipRuleExpr)
}

func TestTheAuthorshipRuleAppearsInExactlyOnePlace(t *testing.T) {
	// A rule enforced in two places is a rule with a gap: whichever one a
	// later "cleanup" weakens stops mattering the moment the other is the one
	// nobody removed. So the expression is grepped over every NON-TEST .go
	// file in the module, and exactly one file may contain it —
	// internal/policy/store.go, the boundary.
	var hits []string
	for _, file := range moduleSources(t) {
		text := readSource(t, file)
		if authorshipViolation(text, strings.HasSuffix(file, "_test.go")) {
			hits = append(hits, file)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("the authorship expression lives in %d files %v; it lives in exactly one", len(hits), hits)
	}
	if !strings.HasSuffix(filepathToSlash(hits[0]), "internal/policy/store.go") {
		t.Errorf("the authorship expression lives in %s, not in the boundary (internal/policy/store.go)", hits[0])
	}

	// The call-site half of the pin (T19). One expression held in one file is
	// half the claim; the other half is that the rule's two named enforcement
	// points are exactly Add (store.go) and DecodePolicy (types.go) — a third
	// caller is a second enforcement point wearing the helper's name, and a
	// missing one is the load-side check deleted while the expression grep
	// still passes.
	var defs int
	for _, file := range moduleSources(t) {
		d, c := authorshipHelperCalls(readSource(t, file), strings.HasSuffix(file, "_test.go"))
		defs += d
		slash := filepathToSlash(file)
		switch {
		case strings.HasSuffix(slash, "internal/policy/store.go"):
			if d != 1 || c != 1 {
				t.Errorf("the boundary holds %d definitions and %d calls of checkAuthorship; want 1 and 1 (the helper and Add's call)", d, c)
			}
		case strings.HasSuffix(slash, "internal/policy/types.go"):
			if d != 0 || c != 1 {
				t.Errorf("types.go holds %d definitions and %d calls of checkAuthorship; want 0 and 1 (DecodePolicy's load-side call, never a second definition)", d, c)
			}
		case d != 0 || c != 0:
			t.Errorf("%s mentions checkAuthorship (%d defs, %d calls); the rule is called by Add and DecodePolicy only — anything else is a second enforcement point", slash, d, c)
		}
	}
	if defs != 1 {
		t.Errorf("checkAuthorship is defined %d times; the rule has exactly one home", defs)
	}
}

func filepathToSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// authorshipHelperCalls reports the rule's home and its enforcement points in
// one file's text: the DEFINITION of checkAuthorship and every CALL site.
// T19 split the rule from its use — the expression lives in the helper and
// both Add and DecodePolicy call it — so counting the expression text alone
// would let a call site silently vanish while the expression grep stayed
// green. Counting calls honestly means the pin also says WHICH files may
// call: exactly the two named ones. Test files are exempt (the guards' own
// prose and the planted duplicates legitimately name the helper here).
func authorshipHelperCalls(text string, isTest bool) (defs, calls int) {
	if isTest {
		return 0, 0
	}
	defs = strings.Count(text, "func checkAuthorship(")
	calls = strings.Count(text, "checkAuthorship(") - defs
	return defs, calls
}

func TestTheAuthorshipGrepGuardCanFail(t *testing.T) {
	// A guard that cannot fire guards nothing (T2's TestTheLockCanFail).
	dupe := "package cli\n\nfunc add(p Policy) *Reject {\n\tif " + authorshipRuleExpr + " {\n\t\treturn nil\n\t}\n\treturn nil\n}\n"
	if !authorshipViolation(dupe, false) {
		t.Error("the guard missed a second enforcement point")
	}
	if authorshipViolation(dupe, true) {
		t.Error("the guard caught a test file; the pinned text legitimately lives in tests")
	}
	if authorshipViolation("package policy\n\nfunc add(p Policy) *Reject {\n\treturn nil\n}\n", false) {
		t.Error("the guard cried wolf on a file with no rule")
	}

	// T19's call-site counter, held to the same standard: a planted third
	// enforcement point is caught, test files are exempt, and the honest
	// shapes — one definition plus one call in the boundary, one bare call in
	// types.go, and a file that only PROSE about the helper — count exactly
	// what they are.
	thirdPoint := "package daemon\n\nfunc replay(p Policy) *Reject {\n\tif rej := checkAuthorship(p); rej != nil {\n\t\treturn rej\n\t}\n\treturn nil\n}\n"
	if d, c := authorshipHelperCalls(thirdPoint, false); d != 0 || c != 1 {
		t.Errorf("the call-site counter missed a planted third enforcement point (got defs=%d calls=%d, want 0/1)", d, c)
	}
	if d, c := authorshipHelperCalls(thirdPoint, true); d != 0 || c != 0 {
		t.Errorf("the call-site counter caught a test file (got defs=%d calls=%d, want 0/0)", d, c)
	}
	home := "package policy\n\nfunc checkAuthorship(p Policy) *Reject {\n\tif " + authorshipRuleExpr + " {\n\t\treturn nil\n\t}\n\treturn nil\n}\n\nfunc add(p Policy) *Reject {\n\tif rej := checkAuthorship(p); rej != nil {\n\t\treturn rej\n\t}\n\treturn nil\n}\n"
	if d, c := authorshipHelperCalls(home, false); d != 1 || c != 1 {
		t.Errorf("the helper's home counted defs=%d calls=%d; want 1 and 1 — the definition must not be counted as a call", d, c)
	}
	prose := "package policy\n\n// checkAuthorship is discussed here, in prose.\nfunc add(p Policy) *Reject {\n\treturn nil\n}\n"
	if d, c := authorshipHelperCalls(prose, false); d != 0 || c != 0 {
		t.Errorf("the counter cried wolf on prose (got defs=%d calls=%d, want 0/0)", d, c)
	}
}

// ── SEC-022(b) — the repo seam has only named wiring ─────────────────────────

// repoSeamCalls finds, in one file's source, every CALL of the three policy
// table methods (PolicyAdd · PolicyRemove · PolicyListAll) and every
// CONSTRUCTION of the RepoFuncs bridge. It reads the AST rather than grepping
// (the house idiom, T2/T3/T4): internal/store's comments NAME these methods
// daily — `PolicyAdd` in prose is documentation about the seam, not a hand
// on it, and a text grep would either cry wolf there or get weakened with
// exclusions until it catches nothing.
func repoSeamCalls(src string) []string {
	f, _, err := parseSource(src)
	if err != nil {
		return nil
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "PolicyAdd", "PolicyRemove", "PolicyListAll":
					hits = append(hits, "calls "+sel.Sel.Name)
				}
			}
		case *ast.CompositeLit:
			switch t := x.Type.(type) {
			case *ast.Ident:
				if t.Name == "RepoFuncs" {
					hits = append(hits, "constructs RepoFuncs")
				}
			case *ast.SelectorExpr:
				if t.Sel.Name == "RepoFuncs" {
					hits = append(hits, "constructs RepoFuncs")
				}
			}
		}
		return true
	})
	return hits
}

// repoSeamAllowed is the allow-list, every entry named with its reason. An
// allow-list, never a deny-list (B8's shape): a new wiring is refused until
// it arrives BY NAME, with a reason, in front of a reviewer. SEC-022's fix
// text anticipated exactly this: "only the T12 wiring may, and it should be
// named in the allow-list."
var repoSeamAllowed = map[string]string{
	"internal/policy/store.go":       "the boundary: it owns the Repo seam, the RepoFuncs bridge, and the only in-package calls",
	"internal/store/policy.go":       "where the three store methods are DEFINED — the SQL lives here and nowhere else (I2)",
	"internal/daemon/policy_gate.go": "the daemon wiring: newPolicyRepo is the bridge the gate, the fetch and the replay ride",
	"cmd/aeon/policy.go":             "the CLI wiring (T12): the only authoring surface (PL15)",
}

// moduleRelPath renders a walked path as the module-relative slash path the
// allow-list names (moduleSources hands back paths rooted at the module).
func moduleRelPath(path string) string {
	slash := filepathToSlash(path)
	for _, marker := range []string{"internal/", "cmd/"} {
		if i := strings.Index(slash, marker); i >= 0 {
			return slash[i:]
		}
	}
	return slash
}

func TestOnlyTheNamedFilesTouchThePolicyRepoSeam(t *testing.T) {
	// SEC-022(b). Every claim this slice's guards make — PL1 at the write,
	// the pair at the load, one event writer — descends from one fact: reads
	// and writes of the policy table happen in named code. A call of
	// PolicyAdd/PolicyRemove/PolicyListAll, or a fresh RepoFuncs, anywhere
	// else is a second door: a wiring that can write a row Add refused,
	// delete an audit-critical rule behind the boundary's back, rewire the
	// bridge to a lying source in production, or read rows nobody re-parsed.
	// The tree as it stands must pass; TestTheRepoSeamGuardCanFail proves the
	// finder fires (a guard that cannot fire guards nothing).
	scanned := 0
	for _, file := range moduleSources(t) {
		if strings.HasSuffix(file, "_test.go") {
			continue // tests seed fakes; the counting fake IS the test idiom
		}
		scanned++
		rel := moduleRelPath(file)
		if _, named := repoSeamAllowed[rel]; named {
			continue
		}
		for _, hit := range repoSeamCalls(readSource(t, file)) {
			t.Errorf("%s %s; the seam is touched only by its named wiring — a new hand arrives by name, with its reason, in repoSeamAllowed (SEC-022(b))", rel, hit)
		}
	}
	if scanned < 40 {
		t.Fatalf("only %d non-test files scanned; this guard would pass almost vacuously", scanned)
	}
}

func TestTheRepoSeamGuardCanFail(t *testing.T) {
	// The guard on the guard (TestTheAuthorshipGrepGuardCanFail's discipline,
	// TestNoSQLLivesOutsideTheStore's shape): every forbidden hand is fed to
	// the SAME finder as synthetic source, next to the shapes it must leave
	// alone — including the comment mentions the real tree already carries.
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"a write outside the wiring",
			"package whatever\n\nfunc w(ctx context.Context, s store.Store) error {\n\treturn s.PolicyAdd(ctx, store.PolicyRow{ID: \"x\"})\n}\n", 1},
		{"a delete outside the wiring",
			"package whatever\n\nfunc w(s store.Store) (bool, error) {\n\treturn s.PolicyRemove(context.Background(), \"x\")\n}\n", 1},
		{"a whole-table read outside the wiring",
			"package whatever\n\nfunc w(s store.Store) { _, _ = s.PolicyListAll(context.Background()) }\n", 1},
		{"a bridge constructed across the package line",
			"package whatever\n\nfunc w() policy.RepoFuncs {\n\treturn policy.RepoFuncs{Add: func(context.Context, policy.Row) error { return nil }}\n}\n", 1},
		{"a bridge constructed inside the package",
			"package policy\n\nfunc w() RepoFuncs { return RepoFuncs{} }\n", 1},
		{"comments naming the seam (the real tree does this)",
			"package whatever\n\n// PolicyAdd, PolicyRemove, PolicyListAll and RepoFuncs are documented here.\nfunc w() {}\n", 0},
		{"a clean file",
			"package whatever\n\nfunc w() int { return 1 }\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := repoSeamCalls(c.src); len(got) != c.want {
				t.Errorf("the finder reported %v, want %d hit(s):\n%s", got, c.want, c.src)
			}
		})
	}
}

// ── SEC-015 / SEC-007 — the caller is not trusted ────────────────────────────

func TestAHandCastedPolicyIsRefusedRatherThanTrusted(t *testing.T) {
	// Go's type system does not enforce the closed set: Policy{Action:
	// Action("ALLOW")} compiles and reaches Add untouched. Every field here
	// is a value that could only exist as a cast — and none of them reaches
	// the table. A check downstream is a check with a bypass (SEC-015), so
	// the boundary re-runs the parsers on whatever it is handed.
	cases := []struct {
		name string
		p    policy.Policy
		want string
	}{
		{"a type cast from a hostile string",
			policy.Policy{ID: "forged", Type: policy.PolicyType(`tool_access" · target all-agents`),
				Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny, Source: policy.Operator, Spec: denySpec()},
			`unknown policy type`},
		{"an action cast to the wrong case",
			policy.Policy{ID: "shouty", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Action("ALLOW"), Source: policy.Operator, Spec: denySpec()},
			`unknown action "ALLOW"`},
		{"an action cast with a trailing space",
			policy.Policy{ID: "trailing", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Action("allow "), Source: policy.Operator, Spec: allowSpec()},
			`unknown action "allow "`},
		{"a source cast with padding",
			policy.Policy{ID: "pad", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Allow, Source: policy.Source("operator "), Spec: allowSpec()},
			`unknown source "operator "`},
		{"a source cast from outside the four",
			policy.Policy{ID: "guest", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Allow, Source: policy.Source("guest"), Spec: allowSpec()},
			`unknown source "guest"`},
		{"a target kind cast to 0005's vocabulary",
			policy.Policy{ID: "ancient", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.TargetKind("global")}, Action: policy.Deny, Source: policy.Operator, Spec: denySpec()},
			`unknown target kind "global"`},
		{"a target id cast that repeats its kind",
			policy.Policy{ID: "twice", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AgentKind, ID: "agent:boss"}, Action: policy.Deny, Source: policy.Operator, Spec: denySpec()},
			`repeats its kind`},
		{"a named kind with no id",
			policy.Policy{ID: "nameless", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AgentKind}, Action: policy.Deny, Source: policy.Operator, Spec: denySpec()},
			`must name the thing it governs`},
		{"an all-agents target that does name an id",
			policy.Policy{ID: "contradiction", Type: policy.ToolAccess,
				Target: policy.Target{Kind: policy.AllAgents, ID: "boss"}, Action: policy.Deny, Source: policy.Operator, Spec: denySpec()},
			`must not name an id`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			got := refuseReason(t, policy.NewStore(repo), c.p)
			if !strings.Contains(got, c.want) {
				t.Errorf("the refusal does not contain %q: %s", c.want, got)
			}
			if repo.addCalls != 0 {
				t.Errorf("the casted policy reached the repo (%d calls)", repo.addCalls)
			}
		})
	}
}

// ── SEC-013 — ids are third-party text and are gated ─────────────────────────

// hostileIDCases are the SEC-013 probe set: the strings that used to reach
// Sentence() raw (newlines, CR, quotes, ANSI escapes) plus the shapes a
// deny-list forgets (NUL, DEL, non-ASCII, space). Each refusal must name the
// FIELD it refused and the character it saw.
var hostileIDCases = []struct {
	name  string
	id    string
	token string // what the refusal must name
}{
	{"newline", "line1\nline2", "control character U+000A"},
	{"carriage return", "back\rforth", "control character U+000D"},
	{"double quote (the Sentence() forger)", `x" · expires 2999-01-01T00:00:00Z`, "character U+0022"},
	{"single quote", "shell'; --", "character U+0027"},
	{"ANSI escape (CSI)", "esc\x1b[2Jdone", "control character U+001B"},
	{"tab", "lead\ttrail", "control character U+0009"},
	{"NUL", "mid\x00dle", "control character U+0000"},
	{"DEL", "soft\x7fball", "control character U+007F"},
	{"space (the two-words trap)", "two words", "character U+0020"},
	{"non-ASCII", "café", "character U+00E9"},
	{"C1 control (the range a plain <0x20 check forgets)", "a\u0085b", "control character U+0085"},
	{"preview past 48 runes", strings.Repeat("a", 55) + "\x01", "control character U+0001"},
}

func TestAHostilePolicyIDIsRefusedAtWriteTime(t *testing.T) {
	for _, c := range hostileIDCases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			p := boundaryPolicy(t, "x", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
			p.ID = c.id
			got := refuseReason(t, policy.NewStore(repo), p)
			for _, want := range []string{"policy id", c.token} {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not name %q: %s", want, got)
				}
			}
			if repo.addCalls != 0 {
				t.Errorf("the hostile id reached the repo")
			}
		})
	}
}

func TestAHostileTargetIDIsRefusedAtWriteTime(t *testing.T) {
	for _, c := range hostileIDCases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			p := boundaryPolicy(t, "rule", policy.AgentKind, "x", policy.Deny, policy.Operator, denySpec())
			p.Target = policy.Target{Kind: policy.AgentKind, ID: c.id}
			got := refuseReason(t, policy.NewStore(repo), p)
			for _, want := range []string{"target id", `kind "agent"`, c.token} {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not name %q: %s", want, got)
				}
			}
			if repo.addCalls != 0 {
				t.Errorf("the hostile target id reached the repo")
			}
		})
	}
}

func TestTheRefusalQuotesTheHostileValueRatherThanEmittingIt(t *testing.T) {
	// The refusal text itself reaches a terminal (the CLI prints it). Echoing
	// the raw ESC into the error that refuses the ESC is SEC-013 re-entering
	// through the front door — so the echoed preview must be QUOTED: control
	// bytes arrive escaped (\n, \x1b), never as bytes.
	repo := &fakeRepo{}
	p := boundaryPolicy(t, "probe", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
	p.ID = "a\x1b[31mR\n"
	got := refuseReason(t, policy.NewStore(repo), p)
	if strings.ContainsAny(got, "\x1b\n\r") {
		t.Errorf("the refusal carries a raw control byte: %q", got)
	}
	if !strings.Contains(got, `\x1b`) {
		t.Errorf("the refusal does not quote the escape for reading: %q", got)
	}

	// A long over-visible value is PREVIEWED, not dumped: 48 runes and an
	// ellipsis, so the refusal a person reads stays bounded even when the
	// refused value was not.
	p.ID = strings.Repeat("a", 55) + "\x01"
	got = refuseReason(t, policy.NewStore(repo), p)
	if !strings.Contains(got, `…"`) {
		t.Errorf("the preview was not truncated with an ellipsis: %q", got)
	}
}

// ── SEC-020 — blank means INVISIBLE, not TrimSpace-empty ─────────────────────

func TestAZeroWidthOnlyIDIsRefusedAsBlankNotAcceptedAsVisible(t *testing.T) {
	// TrimSpace alone calls "\u200b\u200b" content. A name that renders as
	// nothing while being stored as something is the row whose presence you
	// cannot tell from absence — the blank-gate counts VISIBLE runes.
	cases := []struct{ name, value string }{
		{"ZWSP pair", "\u200b\u200b"},
		{"ZWJ pair", "\u200d\u200d"},
		{"word joiner + ZWNJ", "\u2060\u200c"},
		{"BOM", "\ufeff"},
		{"spaces and zero-widths", " \u200b\t\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			s := policy.NewStore(repo)

			p := boundaryPolicy(t, "x", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
			p.ID = c.value
			got := refuseReason(t, s, p)
			for _, want := range []string{"policy id", "no visible character"} {
				if !strings.Contains(got, want) {
					t.Errorf("the blank refusal does not name %q: %s", want, got)
				}
			}

			p2 := boundaryPolicy(t, "rule", policy.AgentKind, "x", policy.Deny, policy.Operator, denySpec())
			p2.Target = policy.Target{Kind: policy.AgentKind, ID: c.value}
			got2 := refuseReason(t, s, p2)
			for _, want := range []string{"target id", "no visible character"} {
				if !strings.Contains(got2, want) {
					t.Errorf("the blank target refusal does not name %q: %s", want, got2)
				}
			}
			if repo.addCalls != 0 {
				t.Errorf("an invisible-only name reached the repo")
			}
		})
	}
}

func TestAHiddenZeroWidthInsideAVisibleIDIsRefusedByName(t *testing.T) {
	// "bo\u200oss" is a visible string whose second character is invisible —
	// the same character the blank-gate names, refused here per-rune with
	// its code point, so a homoglyph attack is refused while SAYING what it is.
	repo := &fakeRepo{}
	p := boundaryPolicy(t, "bo\u200boss", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
	got := refuseReason(t, policy.NewStore(repo), p)
	for _, want := range []string{"policy id", "invisible zero-width", "U+200B", "position 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal does not name %q: %s", want, got)
		}
	}
}

// ── SEC-013/SEC-014 — the cap REFUSES; it never truncates ────────────────────

func TestTheIdentifierLengthCapRefusesRatherThanTruncates(t *testing.T) {
	// The cap is 64 runes — sized from SEC-014's measured ~480-rune
	// worst-case sentence at legal 64-char fields, against T8's 256-rune
	// detail budget. And the failure mode matters more than the number:
	// head-truncating a hostile over-cap id would amputate exactly what
	// identifies the rule (the id is what every verdict names) — the same
	// error SEC-020 names for the sentence tail, so the control is a gate.
	if policy.MaxIdentifierRunes != 64 {
		t.Fatalf("MaxIdentifierRunes is %d; the budget was measured against 64", policy.MaxIdentifierRunes)
	}

	repo := &fakeRepo{}
	s := policy.NewStore(repo)

	atCap := boundaryPolicy(t, strings.Repeat("a", 64), policy.AgentKind, strings.Repeat("b", 64),
		policy.Deny, policy.Operator, denySpec())
	mustAdd(t, s, atCap)
	if repo.rows[0].ID != strings.Repeat("a", 64) {
		t.Errorf("an at-cap id was edited on the way in (len %d)", len(repo.rows[0].ID))
	}

	over := boundaryPolicy(t, strings.Repeat("a", 64)+"x", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
	got := refuseReason(t, s, over)
	for _, want := range []string{"policy id", "65 runes", "64", "refused, not truncated"} {
		if !strings.Contains(got, want) {
			t.Errorf("the over-cap refusal does not name %q: %s", want, got)
		}
	}

	overTarget := boundaryPolicy(t, "rule", policy.AgentKind, strings.Repeat("b", 65), policy.Deny, policy.Operator, denySpec())
	gotT := refuseReason(t, s, overTarget)
	for _, want := range []string{"target id", "65 runes", "refused, not truncated"} {
		if !strings.Contains(gotT, want) {
			t.Errorf("the over-cap target refusal does not name %q: %s", want, gotT)
		}
	}

	// The truncation proof: the repo received EXACTLY ONE row (the at-cap
	// one) — the two over-cap Adds were refused before the seam. A
	// truncating "cap" would have written 64-rune copies of the over-cap
	// values and reported success.
	if len(repo.rows) != 1 || repo.addCalls != 1 {
		t.Fatalf("repo rows %+v calls %d — over-cap must write nothing", repo.rows, repo.addCalls)
	}
	if repo.rows[0].ID != strings.Repeat("a", 64) || repo.rows[0].TargetID != strings.Repeat("b", 64) {
		t.Errorf("the accepted row mutated: %+v", repo.rows[0])
	}
}

func TestEmojiCountAsRunesNotBytes(t *testing.T) {
	// "64 runes" must mean 64 CHARACTERS: a hostile id of many wide runes is
	// refused by count even though it is huge in bytes — and a legal ASCII id
	// of the same byte length passes. The check counts what a person reads.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	wide := strings.Repeat("🙂", 65)
	p := boundaryPolicy(t, wide, policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec())
	got := refuseReason(t, s, p)
	if !strings.Contains(got, "65 runes") {
		t.Errorf("the emoji id was not refused by rune count: %s", got)
	}
}

// ── spec validation (T5's validators, the only write-path caller) ────────────

func TestAPolicyWithAnInvalidSpecIsRefusedNamingTheType(t *testing.T) {
	// The propagation shape is pinned by the task: `policy %q · type %q:
	// <reason>` — the id and the type are both in the message, because "the
	// spec is wrong" without the type is the answer that sends someone to the
	// source.
	cases := []struct {
		name    string
		action  policy.Action
		spec    string
		mustHas []string
	}{
		{"state contradicts the action", policy.Allow, `{"toolName":"bash","state":"deny"}`,
			[]string{`policy "bad-spec" · type "tool_access"`, "deny", "allow"}},
		{"an unknown field (the tool/toolName typo)", policy.Allow, `{"tool":"bash","state":"allow"}`,
			[]string{`policy "bad-spec" · type "tool_access"`, "unknown", "toolName"}},
		{"an empty toolName", policy.Allow, `{"toolName":"","state":"allow"}`,
			[]string{`policy "bad-spec" · type "tool_access"`, "toolName"}},
		{"not JSON at all", policy.Deny, `{"toolName":`,
			[]string{`policy "bad-spec" · type "tool_access"`}},
		{"a spec that is null", policy.Deny, `null`,
			[]string{`policy "bad-spec" · type "tool_access"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			p := boundaryPolicy(t, "bad-spec", policy.AgentKind, "boss", c.action, policy.Operator, spec(c.spec))
			got := refuseReason(t, policy.NewStore(repo), p)
			for _, want := range c.mustHas {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not contain %q: %s", want, got)
				}
			}
			if repo.addCalls != 0 {
				t.Errorf("the invalid spec reached the repo")
			}
		})
	}
}

func TestAValidCostCapStillStoresEvenAtZero(t *testing.T) {
	// T5's instruction carried forward: "CostCap accepts a zero cap — do not
	// tighten that in the store." The boundary validates through the
	// registered validator and nothing more; a zero cap means nothing may be
	// spent, and that is a policy, not a hole.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	p := boundaryPolicy(t, "cap-zero", policy.AgentKind, "boss", policy.Allow, policy.Operator,
		spec(`{"token":"input","max":0}`))
	p.Type = policy.CostCap
	mustAdd(t, s, p)
	if repo.rows[0].Type != "cost_cap" {
		t.Errorf("type stored as %q", repo.rows[0].Type)
	}
}

// ── D6 / N8 — the read side of the boundary ─────────────────────────────────

// row builds a stored-shaped row (valid vocabulary) for the List tests.
func row(id, action, source, kind, targetID, notAfter, specJSON string) policy.Row {
	return policy.Row{
		ID: id, Type: "tool_access", TargetKind: kind, TargetID: targetID,
		Action: action, Source: source, SpecJSON: specJSON, NotAfter: notAfter,
		CreatedAt: "2026-10-19T12:00:00Z",
	}
}

func TestExpiredPoliciesAreStillReturnedByTheStore(t *testing.T) {
	// The adapter-level half of the claim (the SQL half is in
	// internal/store/policy_test.go): List does NOT filter. Expiry is
	// Evaluate's decision against `at`, and an audit answer that depends on
	// when it is asked is not an audit. `at` in the future changes nothing.
	repo := &fakeRepo{}
	repo.rows = []policy.Row{
		row("old", "deny", "operator", "all-agents", "", "2000-01-01T00:00:00Z", `{"toolName":"bash","state":"deny"}`),
		row("live", "deny", "operator", "all-agents", "", "", `{"toolName":"bash","state":"deny"}`),
	}
	s := policy.NewStore(repo)

	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	got, rej := s.List(context.Background(), future)
	if rej != nil {
		t.Fatalf("List refused its own valid rows: %s", rej.Reason)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d of 2 rows (the expired one was filtered): %+v", len(got), got)
	}
	if got[0].ID != "old" {
		t.Errorf("the expired row is missing: %+v", got)
	}
	if got[0].NotAfter == nil || !got[0].NotAfter.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the expired row lost its expiry: %+v", got[0].NotAfter)
	}
	if got[1].NotAfter != nil {
		t.Errorf("the live row invented an expiry: %v", *got[1].NotAfter)
	}
	if repo.listCalls != 1 {
		t.Errorf("List made %d repo calls for one call", repo.listCalls)
	}
}

func TestListIsNotCachedTwoCallsTwoRepoCalls(t *testing.T) {
	// N8, asserted as a COUNT: a cache is where a stale allow lives. The
	// second List must also SEE the change the fake made in between — the
	// counting alone catches "remember once", the content check catches
	// "remember forever".
	repo := &fakeRepo{}
	repo.rows = []policy.Row{row("one", "deny", "operator", "all-agents", "", "", deny())}
	s := policy.NewStore(repo)

	first, rej := s.List(context.Background(), addAt)
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	repo.rows = append(repo.rows, row("two", "deny", "operator", "all-agents", "", "", deny()))
	second, rej := s.List(context.Background(), addAt)
	if rej != nil {
		t.Fatal(rej.Reason)
	}

	if repo.listCalls != 2 {
		t.Errorf("%d List calls made %d repo calls; two calls are two repo calls (N8)", 2, repo.listCalls)
	}
	if len(first) != 1 || len(second) != 2 {
		t.Errorf("the second List did not see the new row: %d then %d — that is a cache", len(first), len(second))
	}
}

func deny() string { return `{"toolName":"bash","state":"deny"}` }

func TestAStoredRowThatEscapedTheBoundaryIsRefusedOnList(t *testing.T) {
	// The read-side half of SEC-015: List re-reads every row through
	// DecodePolicy, and a hand-edited row REFUSES the list rather than being
	// silently dropped — dropping a row silently can drop a Deny silently,
	// and the failure this slice exists to prevent is a permission (or a
	// missing denial) nobody authored. Each case names the row id.
	cases := []struct {
		name    string
		row     policy.Row
		mustHas []string
	}{
		{"an invented action",
			row("tampered", "maybe", "operator", "all-agents", "", "", deny()),
			[]string{"tampered", "not a policy the engine accepts"}},
		{"an invented source",
			row("tampered", "deny", "guest", "all-agents", "", "", deny()),
			[]string{"tampered", "unknown source \"guest\""}},
		{"a spec that is not JSON",
			row("tampered", "deny", "operator", "all-agents", "", "", "not json"),
			[]string{"tampered", "not valid JSON"}},
		{"a named kind whose id was emptied out",
			row("tampered", "deny", "operator", "agent", "", "", deny()),
			[]string{"tampered", "must name the thing it governs"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			repo.rows = []policy.Row{
				row("healthy", "deny", "operator", "all-agents", "", "", deny()),
				c.row,
			}
			_, rej := policy.NewStore(repo).List(context.Background(), addAt)
			if rej == nil {
				t.Fatalf("List ACCEPTED the tampered row and its neighbours")
			}
			for _, want := range c.mustHas {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal does not name %q: %s", want, rej.Reason)
				}
			}
		})
	}
}

// TestListRefusesAStoredAllowFromANonOperatorSource is SEC-022's load-side
// half (T19): the write boundary refused an allow/agent row, but a store can
// still HOLD one — written before the load-side check existed, or by a hand
// edit that escaped Add. Add would never write it, so List must not read it:
// the whole list refuses with that row's id named, exactly like every other
// row that escaped the boundary (the case above), because a silently dropped
// row can drop a Deny and a silently honoured allow IS the forgery. The
// gate's read is this List, so until the operator clears the row through the
// CLI (the only authoring surface, PL15) the gate denies everything — the
// fail-closed answer, recorded on decodeRow.
func TestListRefusesAStoredAllowFromANonOperatorSource(t *testing.T) {
	repo := &fakeRepo{}
	repo.rows = []policy.Row{
		row("agent-no-bash", "deny", "agent", "all-agents", "", "", deny()),
		row("forged-self-grant", "allow", "agent", "agent", "boss", "", `{"toolName":"bash","state":"allow"}`),
	}
	_, rej := policy.NewStore(repo).List(context.Background(), addAt)
	if rej == nil {
		t.Fatal("List honoured a stored allow authored by an agent — PL1 is not a write-only rule")
	}
	// The id (which row to remove) and the source (why it is refused) must
	// both be spoken — an un-named row is an operator hunting through the
	// table, and this slice's refusals name the thing they refuse.
	for _, want := range []string{"forged-self-grant", `source "agent"`, "PL1"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the refusal does not name %q: %s", want, rej.Reason)
		}
	}

	// The positive control: the same non-operator source authored the SAFE
	// direction, and it lists as it always did. A pair-check that refused
	// denies too would "fix" SEC-022 by deleting half of PL1.
	repo2 := &fakeRepo{}
	repo2.rows = []policy.Row{row("agent-no-bash", "deny", "agent", "all-agents", "", "", deny())}
	got, rej := policy.NewStore(repo2).List(context.Background(), addAt)
	if rej != nil {
		t.Fatalf("List refused a deny authored by an agent: %s", rej.Reason)
	}
	if len(got) != 1 || got[0].ID != "agent-no-bash" || got[0].Action != policy.Deny {
		t.Errorf("the deny row listed as %+v", got)
	}
}

func TestListFoldsAStoreFailureIntoARefusalNamingTheStore(t *testing.T) {
	repo := &fakeRepo{listErr: errors.New("reader pool gone")}
	_, rej := policy.NewStore(repo).List(context.Background(), addAt)
	if rej == nil {
		t.Fatal("the store failure vanished")
	}
	for _, want := range []string{"was NOT read", "reader pool gone"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the fold lost %q: %s", want, rej.Reason)
		}
	}
}

func TestAddThenListRoundTripsWhatWasWritten(t *testing.T) {
	// The seam to the seam: what Add wrote is what List returns, field for
	// field, expiry and all — the two directions of the boundary may not
	// disagree about what a policy is.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	expiry := time.Date(2027, 6, 1, 8, 30, 0, 0, time.UTC)
	p := boundaryPolicy(t, "grant-read", policy.LoopKind, "l1", policy.Allow, policy.Operator,
		json.RawMessage(`{"toolName":"read","state":"allow"}`))
	p.NotAfter = &expiry
	mustAdd(t, s, p)

	got, rej := s.List(context.Background(), addAt)
	if rej != nil {
		t.Fatalf("List refused a row Add itself wrote: %s", rej.Reason)
	}
	if len(got) != 1 {
		t.Fatalf("%d policies", len(got))
	}
	back := got[0]
	if back.ID != "grant-read" || back.Type != policy.ToolAccess || back.Action != policy.Allow ||
		back.Source != policy.Operator || back.Target.Kind != policy.LoopKind || back.Target.ID != "l1" {
		t.Errorf("the round trip changed the policy: %+v", back)
	}
	if back.NotAfter == nil || !back.NotAfter.Equal(expiry) {
		t.Errorf("the expiry did not survive: %v", back.NotAfter)
	}
	if string(back.Spec) != `{"toolName":"read","state":"allow"}` {
		t.Errorf("the spec came back as %s", back.Spec)
	}
}

func TestListTreatsAnEmptySpecAsTheColumnDefault(t *testing.T) {
	// The store column defaults to '{}' and the seam says "no spec" by
	// leaving SpecJSON empty; List must read that as the empty object, not
	// as a broken row — the two layers must agree on what an empty spec is.
	repo := &fakeRepo{}
	repo.rows = []policy.Row{row("bare", "deny", "operator", "all-agents", "", "", "")}
	got, rej := policy.NewStore(repo).List(context.Background(), addAt)
	if rej != nil {
		t.Fatalf("the empty spec was refused: %s", rej.Reason)
	}
	if string(got[0].Spec) != "{}" {
		t.Errorf("the spec came back as %q, want {}", got[0].Spec)
	}
}

// ── N8 — the no-cache claim, asserted as STRUCTURE too ───────────────────────

// storeStructFields reads the field names of the policy.Store struct from the
// package's own source. Pure over text, like every guard here.
func storeStructFields(src string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "store.go", src, 0)
	if err != nil {
		return nil, err
	}
	var fields []string
	var found bool
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Store" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return nil, fmt.Errorf("policy.Store is not a struct")
			}
			found = true
			for _, fld := range st.Fields.List {
				for _, n := range fld.Names {
					fields = append(fields, n.Name)
				}
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("no policy.Store struct found; the guard went vacuous")
	}
	return fields, nil
}

func TestTheStoreAdapterHoldsNoCache(t *testing.T) {
	// N8 as shape: the boundary holds a repo and nothing else. No map, no
	// slice, no memo — a field added later is a decision the compiler here
	// forces into the open, not a quiet upgrade to cached reads.
	fields, err := storeStructFields(readSource(t, "store.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0] != "repo" {
		t.Errorf("policy.Store holds %v; the boundary holds exactly the repo (N8)", fields)
	}
}

func TestTheNoCacheGuardCanFail(t *testing.T) {
	// Same synthetic text, one field added: the guard must see it.
	drifted := "package policy\n\ntype Store struct {\n\trepo  Repo\n\tcache map[string][]Policy\n}\n"
	fields, err := storeStructFields(drifted)
	if err != nil {
		t.Fatalf("the guard went vacuous on the drift case: %v", err)
	}
	if len(fields) != 2 {
		t.Errorf("the guard missed the cache field: %v", fields)
	}
	if _, err := storeStructFields("package policy\n\nfunc f() {}\n"); err == nil {
		t.Error("the guard went vacuous on a source with no Store")
	}
}

// ── the RepoFuncs bridge (what the wiring — T12/daemon — fills in) ───────────

func TestRepoFuncsBridgeForwardsEveryCall(t *testing.T) {
	var added []policy.Row
	var removed []string
	var lists int
	bridge := policy.RepoFuncs{
		Add:    func(_ context.Context, r policy.Row) error { added = append(added, r); return nil },
		Remove: func(_ context.Context, id string) (bool, error) { removed = append(removed, id); return id == "x", nil },
		ListAll: func(_ context.Context) ([]policy.Row, error) {
			lists++
			return []policy.Row{row("x", "deny", "operator", "all-agents", "", "", deny())}, nil
		},
	}
	s := policy.NewStore(bridge)
	mustAdd(t, s, boundaryPolicy(t, "via-bridge", policy.AgentKind, "boss", policy.Deny, policy.Agent, denySpec()))
	if len(added) != 1 || added[0].ID != "via-bridge" {
		t.Errorf("Add did not forward: %+v", added)
	}
	ok, err := s.Remove(context.Background(), "x")
	if err != nil || !ok || len(removed) != 1 {
		t.Errorf("Remove did not forward: %v %v %v", ok, err, removed)
	}
	if _, rej := s.List(context.Background(), addAt); rej != nil {
		t.Errorf("List did not forward: %s", rej.Reason)
	}
	if lists != 1 {
		t.Errorf("ListAll fired %d times", lists)
	}
}

func TestRepoFuncsBridgeRefusesMissingHooks(t *testing.T) {
	// A half-wired bridge is a defect, and it surfaces as an error naming the
	// missing hook — not a nil dereference inside an authoring call.
	empty := policy.NewStore(policy.RepoFuncs{})
	rej := empty.Add(context.Background(), boundaryPolicy(t, "n", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec()), addAt)
	if rej == nil || !strings.Contains(rej.Reason, "no Add function") {
		t.Errorf("the missing Add hook did not name itself: %+v", rej)
	}
	if _, err := empty.Remove(context.Background(), "n"); err == nil || !strings.Contains(err.Error(), "no Remove function") {
		t.Errorf("the missing Remove hook did not name itself: %v", err)
	}
	if _, rej := empty.List(context.Background(), addAt); rej == nil || !strings.Contains(rej.Reason, "no ListAll function") {
		t.Errorf("the missing ListAll hook did not name itself: %+v", rej)
	}
}
