package main

// autonomy_guard_test.go — S56 T7. The slice widens authority for the first
// time since S19, so its security invariants are made provable: each absence
// has a guard, each guard is shown to fail by a canary, the pre-slice guard
// bodies stay byte-identical (digests), and the amendment this slice makes
// (the T5 decision-site walk admitting daemon/autonomy.go's read-only pinned
// view) is codified here, not hidden in a body.
//
//   - POSITIVE: the exact write set {grant, deny, revoke} and read set
//     (list, show, vocabulary, effective, pinned, changes, uses) register
//     iff the seam is wired (V13), every one RequiresInitialize, none Debug;
//     the grant-table write boundary has exactly the callers it may have, by
//     walk (≥ 50 files seen).
//   - NEGATIVE: the four caller-classes that are not the person — the narrow
//     projection, the agent-services enumeration, the tool registry, the
//     records catalog — name none of the writes; the three AL11 locks each
//     have a subtest; no agent can author an allow (no autonomy_grant kind).
//   - CANARIES: every negative predicate is fed a synthetic violation and
//     must report it. A guard that cannot fail is not a guard.
//
// Sibling functions, never edits: no pre-slice body in narrow/trigger_test.
// go, loop/allowoutward_test.go, action/class_test.go or
// daemon/trigger_fire_test.go is touched here — TestOriginalGuardsAre
// ByteIdentical pins them by digest instead. The ONE amendment this slice
// makes is the loop guard's (internal/loop/autonomy_site_test.go, recorded
// with its reason in that body's comment and in T7's Notes), re-asserted
// here as TestResolveWithGrantIsCalledOnlyFromTheDecisionSite so the
// amended allow set lives in one reviewed place.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// ─── the ten doors, as data ─────────────────────────────────────────────────

var autonomyWriteMethods = []protocol.MethodName{
	protocol.MethodAutonomyGrant, protocol.MethodAutonomyDeny, protocol.MethodAutonomyRevoke,
}

var autonomyReadMethods = []protocol.MethodName{
	protocol.MethodAutonomyList, protocol.MethodAutonomyShow, protocol.MethodAutonomyVocabulary,
	protocol.MethodAutonomyEffective, protocol.MethodAutonomyPinned, protocol.MethodAutonomyChanges,
	protocol.MethodAutonomyUses,
}

// autonomyAllMethods is the closed set — eleven would be the uninvited door.
func autonomyAllMethods() []protocol.MethodName {
	return append(append([]protocol.MethodName{}, autonomyWriteMethods...), autonomyReadMethods...)
}

// T8's positive list, reserved now: the ONLY tool names that may ever start
// autonomy_ are the five (T8 lands them; until then the registry carries
// none, and this test asserts the subset).
var t8AllowedAutonomyTools = map[string]bool{
	"autonomy_list": true, "autonomy_show": true, "autonomy_effective": true,
	"autonomy_propose_revoke": true, "autonomy_propose_deny": true,
}

// ─── the stub seam: enough to register, nothing to dispatch ─────────────────

// guardAutonomyService is a registration marker: this file asserts the SHAPE
// of the doors (registered iff wired, gated, non-debug) and the ABSENCE of
// them from the agent-reachable surfaces; the behaviour behind each door
// belongs to internal/protocol's autonomy suite (T6), which is the only test
// that dispatches.
type guardAutonomyService struct{}

var _ protocol.AutonomyService = (*guardAutonomyService)(nil) // compile-time: the fake satisfies the seam

func (*guardAutonomyService) List(context.Context, string, string, bool) ([]protocol.GrantView, error) {
	return nil, nil
}
func (*guardAutonomyService) Show(context.Context, string) (protocol.GrantView, *protocol.ChangeView, int, []string, *protocol.AutonomyRefusal, error) {
	return protocol.GrantView{}, nil, 0, nil, nil, nil
}
func (*guardAutonomyService) Vocabulary() (protocol.VocabularyView, error) {
	return protocol.VocabularyView{}, nil
}
func (*guardAutonomyService) Effective(context.Context, string, string, []string) (protocol.EffectiveView, *protocol.AutonomyRefusal, error) {
	return protocol.EffectiveView{}, nil, nil
}
func (*guardAutonomyService) Pinned(context.Context, string) (protocol.PinnedView, *protocol.AutonomyRefusal, error) {
	return protocol.PinnedView{}, nil, nil
}
func (*guardAutonomyService) Changes(context.Context, string, string, int) ([]protocol.ChangeView, error) {
	return nil, nil
}
func (*guardAutonomyService) Uses(context.Context, string, string, string, int) ([]protocol.UseView, error) {
	return nil, nil
}
func (*guardAutonomyService) Grant(context.Context, protocol.Actor, protocol.AutonomyGrantRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	return protocol.AutonomyWriteOutcome{}, nil, nil
}
func (*guardAutonomyService) Deny(context.Context, protocol.Actor, protocol.AutonomyDenyRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	return protocol.AutonomyWriteOutcome{}, nil, nil
}
func (*guardAutonomyService) Revoke(context.Context, protocol.Actor, protocol.AutonomyRevokeRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	return protocol.AutonomyWriteOutcome{}, nil, nil
}

// newAutonomyTable builds the table the way a daemon with a store builds it
// (daemon.go): NewTable, assign the seam, RegisterAutonomy. A non-nil seam
// registers the ten (the return is asserted, not trusted); a nil seam is the
// V13 configuration — the ten stay ABSENT, not registered-and-refusing.
func newAutonomyTable(t *testing.T, wired bool) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if wired {
		tbl.Autonomy = &guardAutonomyService{}
		if !tbl.RegisterAutonomy() {
			t.Fatal("RegisterAutonomy refused to register over a non-nil seam — the app would have no autonomy door at all")
		}
	} else {
		tbl.RegisterAutonomy() // no-op over a nil seam: the ten stay absent (V13)
	}
	return tbl
}

// ─── the positive half ──────────────────────────────────────────────────────

// TestAutonomyWritesAreAHumanDoor states the rule positively, first: the
// exact ten exist on the socket, gated, product surface, and only with the
// service behind them (P34). The absence from everything else is the
// negative half's claim.
func TestAutonomyWritesAreAHumanDoor(t *testing.T) {
	tbl := newAutonomyTable(t, true)
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}

	// (1) the exact set is present: three writes + seven reads.
	for _, m := range autonomyAllMethods() {
		if !have[m] {
			t.Fatalf("%s is not registered with its service wired — the human door is wired out of existence (P34)", m)
		}
	}

	// (2) and NOTHING ELSE under the autonomy. prefix slipped in: an
	// eleventh door cannot enter uninvited.
	known := map[protocol.MethodName]bool{}
	for _, k := range autonomyAllMethods() {
		known[k] = true
	}
	for m := range have {
		if strings.HasPrefix(string(m), "autonomy.") && !known[m] {
			t.Errorf("%s is registered under the autonomy. prefix and is not one of the ten — the door set is closed", m)
		}
	}

	// (3) every one is gated and non-debug: a write before the handshake is
	// unthinkable, and Debug is never what keeps a method off a surface.
	for _, m := range autonomyAllMethods() {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s vanished between Methods() and Lookup()", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize — an autonomy write on an unnegotiated connection is unthinkable (AL11)", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — these are product methods on the wide socket, not diagnostics (AL11)", m)
		}
	}

	// (4) the seam nil ⇒ each of the ten ABSENT, and Dispatch answers the
	// sentinel (V13: absent, never registered-and-refusing).
	tblNil := newAutonomyTable(t, false)
	for _, m := range autonomyAllMethods() {
		if _, ok := tblNil.Lookup(m); ok {
			t.Errorf("%s is registered without its service — registered-and-refusing is V13's forbidden shape", m)
		}
		if _, err := tblNil.Dispatch(context.Background(), "c-door", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Errorf("%s answered %v, want ErrUnknownMethod — absent means the table never heard of it (V13)", m, err)
		}
	}
}

// ─── the grant-table write boundary, by walk ────────────────────────────────

// grantTableNeedles are the identifiers that ARE the grant-table boundary:
// the store's two write doors, the use log, the one live read the decision
// site makes, and the pin's one INSERT. A fifth door naming them is the
// hole this walk closes.
var grantTableNeedles = []string{
	"CreateAutonomyGrant(",
	"RevokeAutonomyGrant(",
	"RecordAutonomyUse(",
	"AutonomyLive(",
	"INSERT INTO loop_autonomy_pin",
}

// grantTableCallers is the CONTRACT: who may name them, and why. Every
// OBSERVED entry must still cross a needle (an exemption without its reason
// is a hole waiting for a caller); the one prospective entry (autonomy_start.
// go, spec-named) carries no crossing duty — it is admitted because the
// origin site is where a write caller WOULD land, not because it crosses
// one today.
var grantTableCallers = map[string]string{
	"internal/store/autonomy.go":        "the door's own implementation (definitions + the store's reads)",
	"internal/store/blueprint.go":       "CreatePinnedLoop's transaction — the pin's one INSERT",
	"internal/daemon/autonomy.go":       "the socket human door (grant/deny/revoke) + the pinned view's read-only renderer (AL14/AL17)",
	"internal/daemon/autonomy_start.go": "the origin site (spec-named, prospective: it consults and pins)",
	"internal/loop/autonomy_site.go":    "the decision site (AL9): the live check and the use log",
	"internal/loop/execute.go":          "S56 T7 deviation, recorded: the begun use row (execute.go:147)",
	// S56 T8 (2026-11-01): the two restrictive proposal kinds' commit
	// seams — the committed revoke and veto ride the store's own doors with
	// the committer as actor and the author as proposed_by. The file is the
	// one the deskSeams map lives in (proposal_decide.go), so the seam
	// definition and its crossings are one reviewed diff.
	"cmd/aeon/proposal_decide.go": "the commit door's autonomy seams (S56 T8): RevokeAutonomyGrant / CreateAutonomyGrant with the committer as actor",
}

// grantTableProspective is the spec-named entry with no crossing duty.
var grantTableProspective = map[string]bool{"internal/daemon/autonomy_start.go": true}

// grantTableExempt is the whitelist as a predicate: the contracted files,
// plus the CLI's direct door (cmd/aeon/autonomy*.go, T9 — prospective,
// spec-named; the CLI opens the store directly, as aeon policy does).
func grantTableExempt(rel string) bool {
	if _, ok := grantTableCallers[rel]; ok {
		return true
	}
	return strings.HasPrefix(rel, "cmd/aeon/autonomy")
}

// grantTableCrossings counts the needles a file's text names, comments
// stripped (store.go's doc block names the tables in prose — a mention is
// not a door). Shared by the walk's reverse check and the offense predicate
// so both read the same stripping.
func grantTableCrossings(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		for _, nd := range grantTableNeedles {
			if strings.Contains(line, nd) {
				n++
			}
		}
	}
	return n
}

// grantTableOffenses reports every way one file's text crosses the
// grant-table boundary from outside the contract. Factored out of the walk
// so the canary (TestAutonomyGuardsHaveTeeth) feeds the IDENTICAL predicate
// a synthetic violation — never a copy that could silently diverge.
func grantTableOffenses(rel, text string) []string {
	if grantTableExempt(rel) {
		return nil
	}
	var out []string
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		for _, nd := range grantTableNeedles {
			if strings.Contains(line, nd) {
				out = append(out, fmt.Sprintf("%s:%d names %q", rel, i+1, nd))
			}
		}
	}
	return out
}

// TestOnlyTheServiceAndTheStartSitesTouchTheGrantTables walks the repository
// and asserts the call graph above and nothing else: a stray caller of the
// write boundary is a side door onto the grants. The walk asserts it saw ≥ 50
// non-test files, so it can never pass on an empty walk.
func TestOnlyTheServiceAndTheStartSitesTouchTheGrantTables(t *testing.T) {
	var walked int
	seen := map[string]bool{}
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs (dist/, node_modules) hold no Go code we own
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		walked++
		seen[rel] = true
		for _, reason := range grantTableOffenses(rel, string(src)) {
			t.Errorf("%s — a second door onto the grant tables (S56 AL10/AL11)", reason)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — it checked nothing (never pass on an empty walk)", walked)
	}
	// The contract is a contract both ways, for the files that exist: a
	// listed file no longer present (or no longer crossing) is the drift the
	// map would otherwise hide.
	for rel, why := range grantTableCallers {
		if !seen[rel] {
			t.Errorf("the contract lists %s (%s) but the walk never saw it — the file moved or is gone", rel, why)
			continue
		}
		if grantTableProspective[rel] {
			continue
		}
		src, readErr := os.ReadFile(filepath.Join("../../", rel))
		if readErr != nil {
			t.Errorf("the contract lists %s and the walk saw it, but it cannot be read: %v", rel, readErr)
			continue
		}
		if grantTableCrossings(string(src)) == 0 {
			t.Errorf("the contract lists %s as a caller, but it crosses no needle any more — drop the entry (an exemption without its reason is a hole waiting for a caller)", rel)
		}
	}
}

// ─── the T4/T5 guards, re-asserted from the sibling ─────────────────────────

// TestResolveAutonomyIsCalledOnlyFromOriginSites re-asserts T4's guard from
// here, so the origin-site invariant lives beside the amendment rather than
// only in the daemon package.
func TestResolveAutonomyIsCalledOnlyFromOriginSites(t *testing.T) {
	daemonDir := filepath.Join("..", "..", "internal", "daemon")
	for _, f := range []string{"autonomy_start.go", "trigger_fire.go", "event_fire.go", "autonomy_start_test.go"} {
		src, err := os.ReadFile(filepath.Join(daemonDir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(src), "resolveAutonomy") {
			t.Errorf("%s is expected to reference resolveAutonomy and does not", f)
		}
	}
	// The person door and the dispatch reactors must NOT consult the policy.
	for _, f := range []string{"blueprint.go", "queue_reactor.go", "roadmap_dispatch.go", "triage_dispatch.go"} {
		src, err := os.ReadFile(filepath.Join(daemonDir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(src), "resolveAutonomy") {
			t.Errorf("%s consults resolveAutonomy — a person/narrow path must not (AL8, S56 T4)", f)
		}
	}
}

// TestResolveWithGrantIsCalledOnlyFromTheDecisionSite re-asserts T5's guard
// as AMENDED by this slice: the allow set is the decision site
// (loop/execute.go) plus the pinned view's read-only renderer
// (daemon/autonomy.go, AL14/AL17 — zero seams, display only, never the
// loop's decision). The loop package's original carries the same amendment
// with its reason recorded in the body's comment; this sibling is the
// single reviewed place for the amended set.
func TestResolveWithGrantIsCalledOnlyFromTheDecisionSite(t *testing.T) {
	intRoot := filepath.Join("..", "..", "internal")
	var calls []string
	err := filepath.WalkDir(intRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(intRoot, p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || !strings.Contains(line, "ResolveWithGrant(") {
				continue
			}
			if strings.Contains(line, "func ResolveWithGrant(") {
				continue // the definition, not a call
			}
			calls = append(calls, filepath.ToSlash(rel)+": line "+strconv.Itoa(i+1)+": "+trimmed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 {
		t.Error("ResolveWithGrant is never called — the decision site lost its door")
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "loop/execute.go:") && !strings.HasPrefix(c, "daemon/autonomy.go:") {
			t.Errorf("a second decision door: %s — the only callers are the decision site and the pinned view's renderer (S56 T7 amendment)", c)
		}
	}
}

// startParamsAuthorityOffenses is T4's reflect check factored over a type,
// so the canary can feed it a synthetic struct. A field whose name carries
// grant/merge/allowmerge/autonomy is a wire route to merge authority.
func startParamsAuthorityOffenses(tt reflect.Type) []string {
	if tt.Kind() != reflect.Struct {
		return []string{"(not a struct: " + tt.Kind().String() + ")"}
	}
	var bad []string
	for i := 0; i < tt.NumField(); i++ {
		name := strings.ToLower(tt.Field(i).Name)
		for _, f := range []string{"grant", "merge", "allowmerge", "autonomy"} {
			if strings.Contains(name, f) {
				bad = append(bad, tt.Field(i).Name)
			}
		}
	}
	return bad
}

// TestStartParamsCarryNoAuthority re-asserts T4's guard from the sibling: no
// wire type may carry merge authority — the only authority fields the start
// params carry are the legacy pair (Attended, AllowOutward).
func TestStartParamsCarryNoAuthority(t *testing.T) {
	tt := reflect.TypeOf(protocol.LoopStartParams{})
	if got := startParamsAuthorityOffenses(tt); len(got) != 0 {
		t.Errorf("protocol.LoopStartParams fields %s are wire routes to merge authority — refuse them (T4 Do-NT, S56 T7)", strings.Join(got, ", "))
	}
	var legacy []string
	for i := 0; i < tt.NumField(); i++ {
		n := tt.Field(i).Name
		if n == "Attended" || n == "AllowOutward" {
			legacy = append(legacy, n)
		}
	}
	if len(legacy) != 2 || (legacy[0] != "Attended" && legacy[0] != "AllowOutward") {
		t.Errorf("the wire's authority fields = %v, want exactly Attended and AllowOutward", legacy)
	}
}

// ─── the negative half — each door a subtest, so a failure names which ──────

// narrowProjectingAutonomy returns the names of the narrow actions whose
// projected SocketMethod is under the autonomy. prefix. Over a table
// argument so the canary can feed it a poisoned one.
func narrowProjectingAutonomy(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if strings.HasPrefix(string(a.SocketMethod), "autonomy.") {
			bad = append(bad, a.Name+"→"+string(a.SocketMethod))
		}
	}
	return bad
}

// agentServicesNamingAutonomy: the agent-to-agent surface, same shape.
func agentServicesNamingAutonomy(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range ms {
		if strings.HasPrefix(string(m.Name), "autonomy.") {
			bad = append(bad, string(m.Name))
		}
	}
	return bad
}

// autonomyGrantToolName: a tool name twinned to a grant write.
var autonomyGrantToolRe = regexp.MustCompile(`^autonomy_(grant|deny|revoke|set)$`)

// catalogAutonomyOffenses reports every catalog Spec that names an autonomy
// write: a Method among the three writes, an autonomy-family spec that
// MUTATES without being one of the two propose twins (T0-Q9's boundary: an
// agent may PROPOSE a restriction, never grant an allow), or a tool name
// twinned to a grant.
func catalogAutonomyOffenses(specs []records.Spec) []string {
	var bad []string
	for _, sp := range specs {
		switch sp.Method {
		case "autonomy.grant", "autonomy.deny", "autonomy.revoke":
			bad = append(bad, sp.Name+"→"+sp.Method+" (a grant write on the records catalog)")
		}
		if strings.HasPrefix(sp.Method, "autonomy.") && sp.Mutates {
			switch sp.Method {
			case "autonomy.proposeRevoke", "autonomy.proposeDeny": // T8's restrictive twins — inert until a human decides
			default:
				bad = append(bad, sp.Name+" (family autonomy, Mutates, Method "+sp.Method+")")
			}
		}
		if autonomyGrantToolRe.MatchString(sp.Name) {
			bad = append(bad, sp.Name+" (a tool name twinned to a grant write)")
		}
	}
	return bad
}

// autonomyLiterals are the ten tokens the text grep forbids under the four
// agent-reachable packages: the three writes' quoted wire names and the
// three constants that name them.
var autonomyLiterals = []string{
	`"autonomy.grant"`, `"autonomy.deny"`, `"autonomy.revoke"`,
	"MethodAutonomyGrant", "MethodAutonomyDeny", "MethodAutonomyRevoke",
}

func autonomyLiteralInFile(text string) []string {
	var bad []string
	for _, lit := range autonomyLiterals {
		if strings.Contains(text, lit) {
			bad = append(bad, lit)
		}
	}
	return bad
}

// treeNamesAutonomy is the text-walk predicate as a question, so the canary
// can aim it at a temp tree instead of a package directory.
func treeNamesAutonomy(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if len(autonomyLiteralInFile(string(src))) > 0 {
			found = true
		}
		return nil
	})
	return found
}

// autonomyKindOffenses reports every proposal kind under the autonomy_
// prefix that is not one of T8's two (autonomy_revoke, autonomy_deny) —
// autonomy_grant above all: an allow an agent can file is the door this
// slice exists to keep shut.
func autonomyKindOffenses(kinds []string) []string {
	var bad []string
	for _, k := range kinds {
		if strings.HasPrefix(k, "autonomy_") && k != "autonomy_revoke" && k != "autonomy_deny" {
			bad = append(bad, k)
		}
	}
	return bad
}

// proposalKindLineOffenses reports migration lines that put autonomy_grant
// into a proposal.kind context. The TABLE name autonomy_grant is legal (it
// is the grant record, T3) — the KIND is not: the offense is a line carrying
// both the token and the kind column.
func proposalKindLineOffenses(text string) []string {
	var bad []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "autonomy_grant") && strings.Contains(line, "kind") {
			bad = append(bad, strings.TrimSpace(line))
		}
	}
	return bad
}

func initializeForGuard(t *testing.T, tbl *protocol.Table, connID string) {
	t.Helper()
	raw, err := json.Marshal(protocol.InitializeParams{
		ClientName: "guard", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tbl.Dispatch(context.Background(), connID, protocol.MethodInitialize, raw)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, ok := res.(protocol.InitializeResult); !ok {
		t.Fatalf("initialize returned %T", res)
	}
}

// TestAutonomyNeverReachesTheNarrowOrAgentSurfaces is AL17's negative half:
// the four caller-classes that are not the person name none of the writes,
// and the three AL11 locks each hold. Each is its own subtest so a failure
// names WHICH door opened.
func TestAutonomyNeverReachesTheNarrowOrAgentSurfaces(t *testing.T) {
	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		if bad := narrowProjectingAutonomy(actions); len(bad) != 0 {
			t.Errorf("the narrow surface projects autonomy.* through %s — an action projecting an autonomy method hands a key-holder a door onto authority it must not hold (AL17)", strings.Join(bad, ", "))
		}
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		if strings.Contains(strings.ToLower(stmt), "autonomy") {
			t.Errorf("the projection's rendered boundary statement mentions autonomy — the surface DISPLAYS a door its table must not hold (D16)")
		}
		// No non-test narrow file names the vocabulary at all (literal or
		// constant): a projection that names the authority can smuggle it in
		// without naming a method.
		root := filepath.Join("..", "..", "internal", "narrow")
		walked := 0
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			walked++
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if strings.Contains(strings.ToLower(string(src)), "autonomy") {
				t.Errorf("%s names autonomy — the narrow surface must not name the authority at all (AL17)", filepath.ToSlash(path))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if walked == 0 {
			t.Fatal("the narrow walk saw no non-test files — the grep checked nothing")
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := agentServicesNamingAutonomy(ms); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying an autonomy method is AL11's refusal made mechanical", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		var bad []string
		for name := range reg {
			if autonomyGrantToolRe.MatchString(name) {
				bad = append(bad, name+" (twinned to a grant write)")
			} else if strings.HasPrefix(name, "autonomy_") && !t8AllowedAutonomyTools[name] {
				bad = append(bad, name+" (not in T8's five)")
			}
		}
		if len(bad) != 0 {
			t.Errorf("the tool registry carries %s — the only autonomy_ vocabulary an agent may hold is T8's five (the two propose twins and the reads)", strings.Join(bad, ", "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := catalogAutonomyOffenses(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — an agent-reachable autonomy write is the side door T0-Q9 and AL17 kept shut; only the two propose twins may mutate in the family (T8)", strings.Join(bad, ", "))
		}
	})

	t.Run("TextGrep", func(t *testing.T) {
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			root := filepath.Join("..", "..", dir)
			walked := 0
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				walked++
				src, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				if bad := autonomyLiteralInFile(string(src)); len(bad) != 0 {
					t.Errorf("%s names %s — a surface that NAMES a grant write, even in a comment, is the projection the rule forbids (AL17)", filepath.ToSlash(path), strings.Join(bad, ", "))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if walked == 0 {
				t.Fatalf("%s: the walk saw no non-test files — the grep checked nothing", dir)
			}
		}
	})

	t.Run("NoGrantProposalKind", func(t *testing.T) {
		// The Go mirror: no autonomy_grant kind; any autonomy_* kind is one
		// of T8's two (the existence halves land with T8).
		if bad := autonomyKindOffenses(store.ProposalKinds); len(bad) != 0 {
			t.Errorf("store.ProposalKinds carries %s — an allow an agent can file as a proposal is the door AL11 lock 1 keeps shut", strings.Join(bad, ", "))
		}
		// The schema's frozen block: the token may not appear at all (the
		// autonomy tables live in the migration FILES, not schema.go).
		schemaSrc, err := os.ReadFile("../../internal/store/schema.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(schemaSrc), "autonomy_grant") {
			t.Error("schema.go names autonomy_grant — a standing grant as a proposal kind in the frozen schema is the side door (AL11 lock 1)")
		}
		// The migration files: a kind-context line may not carry the token.
		migs, err := filepath.Glob(filepath.Join("..", "..", "internal", "store", "migrations", "*.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if len(migs) == 0 {
			t.Fatal("no migration files found — the glob checked nothing")
		}
		for _, m := range migs {
			b, readErr := os.ReadFile(m)
			if readErr != nil {
				t.Fatalf("read %s: %v", m, readErr)
			}
			if bad := proposalKindLineOffenses(string(b)); len(bad) != 0 {
				t.Errorf("%s puts autonomy_grant into a proposal.kind context: %v (AL11 lock 1)", filepath.Base(m), bad)
			}
		}
	})

	t.Run("NoToolTwinnedToAGrant", func(t *testing.T) {
		// AL11 lock 1: no agent tool, no tool-shaped string twinned to a
		// grant — neither the registry's keys nor the names it hands a model.
		var bad []string
		for name := range tools.Registry() {
			if autonomyGrantToolRe.MatchString(name) {
				bad = append(bad, "registry:"+name)
			}
		}
		for _, name := range tools.ToolNames() {
			if autonomyGrantToolRe.MatchString(name) {
				bad = append(bad, "model-vocab:"+name)
			}
		}
		if len(bad) != 0 {
			t.Errorf("agent tool vocabulary twinned to a grant write: %v — AL11 lock 1", bad)
		}
	})

	t.Run("SocketRefusesNonHumanActors", func(t *testing.T) {
		// AL11 lock 2: the socket refuses an actor whose kind is not user,
		// before the seam is ever asked (the identity door is the kernel's,
		// not a claim).
		for _, kind := range []string{"agent", "trigger"} {
			tbl := newAutonomyTable(t, true)
			tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
				return protocol.Actor{Kind: kind, ID: "x-1"}
			}
			initializeForGuard(t, tbl, "c-lock")
			params, err := json.Marshal(protocol.AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR"})
			if err != nil {
				t.Fatal(err)
			}
			_, derr := tbl.Dispatch(context.Background(), "c-lock", protocol.MethodAutonomyGrant, params)
			if derr == nil {
				t.Fatalf("kind %s: a non-person grant dispatch succeeded — the socket's identity door is gone (AL11 lock 2)", kind)
			}
			var e *protocol.Error
			if !errors.As(derr, &e) {
				t.Fatalf("kind %s: the refusal is not a typed Aeon error: %T: %v", kind, derr, derr)
			}
			if e.Code != protocol.ErrAutonomyRefused.Code {
				t.Errorf("kind %s: code = %d, want %d (the autonomy refusal family)", kind, e.Code, protocol.ErrAutonomyRefused.Code)
			}
			if !strings.Contains(e.Data.What, "not a person") {
				t.Errorf("kind %s: the refusal does not name the rule: %q", kind, e.Data.What)
			}
		}
	})

	t.Run("StoreRefusesBeforeSQL", func(t *testing.T) {
		// AL11 lock 3: the store refuses before any SQL, and the migration's
		// CHECK is the backstop a store bypassed. The refusal precedes the
		// node probe, so no root component is needed to prove it.
		s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		for _, tc := range []struct {
			kind string
			id   string
			want string
		}{
			{"agent", "agent-1", "actor-not-human"},
			{"trigger", "trg-1", "actor-not-human"},
			{"user", "", "actor-anonymous"},
			{"cli", "cli-1", "actor-not-human"}, // an ALLOW admits only a user; cli is the second door for the restrictive writes (deny, revoke) — AL13, corrected at T9
		} {
			_, ref, err := s.CreateAutonomyGrant(store.Actor{Kind: tc.kind, ID: tc.id}, store.NewAutonomyGrant{
				NodeID: "root-x", SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: "{}",
			})
			if err != nil {
				t.Fatalf("kind %s: a refusal arrived as an error: %v", tc.kind, err)
			}
			if ref == nil {
				t.Fatalf("kind %s: a non-person wrote a grant — the store's actor door is gone (AL11 lock 3)", tc.kind)
			}
			if ref.Kind != tc.want {
				t.Errorf("kind %s: refusal kind = %q, want %q", tc.kind, ref.Kind, tc.want)
			}
		}
		// Zero rows: a refused write writes nothing.
		grants, err := s.ListAutonomyGrants(store.AutonomyGrantFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(grants) != 0 {
			t.Errorf("refused writes left %d grant row(s) — the refusal is not before the SQL", len(grants))
		}
		changes, err := s.AutonomyChanges(store.AutonomyChangeFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 0 {
			t.Errorf("refused writes left %d change row(s) — an audit row for a write that never happened is a second truth", len(changes))
		}
		// The CHECK is the backstop: the migration's literals.
		mig, err := os.ReadFile("../../internal/store/migrations/20261101150000_autonomy.sql")
		if err != nil {
			t.Fatal(err)
		}
		// Amended 2026-11-01 at T9's pre-flight: the creator CHECK is
		// state-aware — an allow row is still user-only, a deny row admits the
		// cli direct door (AL13). The user-only literal for the allow half is
		// what this assertion keeps.
		for _, want := range []string{`CHECK (created_by_kind = 'user' OR (state = 'deny' AND created_by_kind = 'cli'))`, `CHECK (actor_kind IN ('user','cli'))`} {
			if !strings.Contains(string(mig), want) {
				t.Errorf("the autonomy migration lost its CHECK %q — the backstop for a store bypassed is gone (AL11 lock 3)", want)
			}
		}
	})
}

// TestNoStandingGrantInTheNarrowSurface is O2's shape re-run for the
// authority this slice adds: the narrow types may not be grant-shaped (the
// AUTONOMY package's types are allowed to be grant-shaped — they ARE the
// grant), and no narrow file may IMPORT the autonomy packages (a projection
// that imports the authority can smuggle a verdict in without naming a
// method — the import is the door a name-grep misses).
func TestNoStandingGrantInTheNarrowSurface(t *testing.T) {
	granty := regexp.MustCompile(`(?i)^(grants?$|permissions?$|roles?$|privileges?$|allowed)|outward`)
	for _, name := range []any{narrow.Key{}, narrow.Validator{}, narrow.Error{}, narrow.Action{}, narrow.MuxEntry{}} {
		tv := reflect.TypeOf(name)
		for i := 0; i < tv.NumField(); i++ {
			if f := tv.Field(i); granty.MatchString(f.Name) {
				t.Errorf("narrow.%s.%s is grant-shaped — a standing grant on the key/validator/action is the O2 refusal (S19, S56 AL17)", tv.Name(), f.Name)
			}
		}
	}
	root := filepath.Join("..", "..", "internal", "narrow")
	walked := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if ip == "github.com/cjlapao/aeon/internal/autonomy" || strings.HasPrefix(ip, "github.com/cjlapao/aeon/internal/autonomy/") {
				t.Errorf("%s imports %s — the narrow surface reaches the autonomy core (S19 O2, S56 AL17)", path, ip)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked == 0 {
		t.Fatal("the import walk saw no non-test narrow files — it proved nothing")
	}
}

// ─── the canary: a guard that cannot fail is not a guard ────────────────────

// TestAutonomyGuardsHaveTeeth feeds every negative predicate a SYNTHETIC
// violation and a clean input: each must REPORT the violation and stay
// silent on the clean one. The predicates are exercised as the guards use
// them — the same functions, never copies — so a predicate weakened to
// vacuity fails HERE, not in the next slice that trusts it.
func TestAutonomyGuardsHaveTeeth(t *testing.T) {
	// 1. The narrow projection: a poisoned action projecting autonomy.grant
	// must be named; a clean read must not.
	poisoned := []narrow.Action{
		{Name: "aeon_autonomy_grant", SocketMethod: protocol.MethodAutonomyGrant, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}
	if got := narrowProjectingAutonomy(poisoned); len(got) == 0 {
		t.Fatalf("an action projecting autonomy.grant was not flagged — the narrow predicate cannot fail, so it is not a guard")
	}
	clean := []narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}
	if got := narrowProjectingAutonomy(clean); len(got) != 0 {
		t.Fatalf("a clean action was flagged as projecting autonomy.* (%v) — the narrow predicate fires on nothing", got)
	}

	// 2. The records catalog: a spec twinned to a grant must be named; the
	// reads and the propose twins must not (a predicate that fires on the
	// twins would delete T8's allowed vocabulary).
	poisonedSpecs := []records.Spec{
		{Name: "autonomy_grant", Method: "autonomy.grant", Family: "autonomy", Mutates: true, Class: "detectable"},
	}
	if got := catalogAutonomyOffenses(poisonedSpecs); len(got) == 0 {
		t.Fatalf("a catalog spec twinned to autonomy.grant was not flagged — the catalog predicate cannot fail")
	}
	cleanSpecs := []records.Spec{
		{Name: "autonomy_list", Method: "autonomy.list", Family: "autonomy", Class: "safe"},
		{Name: "autonomy_propose_revoke", Method: "autonomy.proposeRevoke", Family: "autonomy", Mutates: true, Class: "detectable"},
		{Name: "autonomy_propose_deny", Method: "autonomy.proposeDeny", Family: "autonomy", Mutates: true, Class: "detectable"},
	}
	if got := catalogAutonomyOffenses(cleanSpecs); len(got) != 0 {
		t.Fatalf("the reads and the two propose twins were flagged (%v) — the catalog predicate fires on what S56 permits", got)
	}

	// 3. The text walk: a file under a fake tree naming "autonomy.grant"
	// must be caught; one naming only a read must not.
	dirty := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirty, "dirty.go"),
		[]byte("package tools\n\nvar x = map[string]string{\"grant\": \"autonomy.grant\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !treeNamesAutonomy(dirty) {
		t.Fatal("a file naming \"autonomy.grant\" was not caught by the text walk — the walk cannot fail")
	}
	onlyClean := t.TempDir()
	if err := os.WriteFile(filepath.Join(onlyClean, "clean.go"),
		[]byte("package tools\n\nvar y = map[string]string{\"read\": \"autonomy.list\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if treeNamesAutonomy(onlyClean) {
		t.Fatal("a file naming only autonomy.list was flagged — the text walk fires on the reads")
	}

	// 4. The grant-table walk: a synthetic caller outside the contract must
	// be reported, and the same text under a contracted path must not (the
	// exemption is the file, not the text).
	const fakeCaller = "package daemon\n\nfunc sneaky() {\n\ts.CreateAutonomyGrant(actor, in)\n}\n"
	if got := grantTableOffenses("internal/daemon/sneaky.go", fakeCaller); len(got) == 0 {
		t.Fatal("a CreateAutonomyGrant call in a non-contracted daemon file was not caught — the grant-table walk has no teeth")
	}
	if got := grantTableOffenses("internal/store/autonomy.go", fakeCaller); len(got) != 0 {
		t.Fatalf("the contracted store door was reported (%v) — the whitelist is not being honoured", got)
	}
	const fakePin = "package store\n\nfunc alsoSneaky() {\n\tx.Exec(`INSERT INTO loop_autonomy_pin (loop_id) VALUES (?)`, id)\n}\n"
	if got := grantTableOffenses("internal/store/sneaky_pin.go", fakePin); len(got) == 0 {
		t.Fatal("an INSERT INTO loop_autonomy_pin outside the contract was not caught — the pin arm has no teeth")
	}

	// 5. The start-params reflect: a struct carrying AllowMerge must be
	// flagged; the real wire type must not.
	if got := startParamsAuthorityOffenses(reflect.TypeOf(struct{ AllowMerge bool }{})); len(got) == 0 {
		t.Fatal("a start params struct with an AllowMerge field was not flagged — the reflect predicate cannot fail")
	}
	if got := startParamsAuthorityOffenses(reflect.TypeOf(protocol.LoopStartParams{})); len(got) != 0 {
		t.Fatalf("the real LoopStartParams was flagged (%v) — the predicate fires on the legacy pair's neighbours", got)
	}

	// 6. The proposal-kind predicate: a list carrying autonomy_grant must be
	// flagged; the real closed set must not.
	if got := autonomyKindOffenses([]string{"policy_add", "autonomy_grant"}); len(got) == 0 {
		t.Fatal("a kind list carrying autonomy_grant was not flagged — the kind predicate cannot fail")
	}
	if got := autonomyKindOffenses(store.ProposalKinds); len(got) != 0 {
		t.Fatalf("the real ProposalKinds were flagged (%v) — the predicate fires on the closed set", got)
	}
}

// ─── the amendment does not loosen the rest ─────────────────────────────────

// TestAmendmentDoesNotLoosenOtherGuards re-asserts the neighbouring guards
// this slice's amendment sits beside, so nobody weakens one while editing
// the other: the R6 body is called UNEDITED from this same package; the
// PL15 ruling runs S45's merged guard (the amendment made the authoring
// verbs the human door — see the subtest's note); the mcp CLI's verb set
// is parsed (no config-authoring verb); and S40 AU1's trigger-authoring
// lock is re-run (no trigger tool, no trigger-write verb in the
// agent-facing packages, the scan floor held).
func TestAmendmentDoesNotLoosenOtherGuards(t *testing.T) {
	t.Run("R6PluginInstallLifecycleStaysAbsent", func(t *testing.T) {
		TestNoPluginManagementOnNarrowSurface(t)
	})
	// Amended 2026-11-01 at the S45-merge rebase: the pre-S45 version of
	// this subtest re-asserted the OLD PL15 (authoring never reaches the
	// method table) with its own walk, exempting one pre-existing offender
	// (internal/seed/seed.go:53, S43-extra \u2014 the red in
	// TestNoPolicyAuthoringReachesTheNarrowSurface that the sibling existed
	// to keep out of this slice's P10). S45 merged with PL15 AMENDED
	// POSITIVELY (PM1): the authoring verbs ARE on the method table, as the
	// human door \u2014 and S45's own guards pin what that means (absent from
	// the narrow surface, the agent services, the tool registry and the
	// catalog save the two propose twins). S45's merge also resolved the
	// seed.go red (TestNoPolicyAuthoringReachesTheNarrowSurface is green
	// on the merged main), so the exemption is gone. The subtest now runs
	// S45's guard: the amended ruling is green here by construction, and a
	// regression in either slice's half of the ruling fails this test.
	// (The old walk's api-pin clause is asserted in
	// internal/bootstrap/register_test.go's pin tests.)
	t.Run("PL15PolicyAuthoringStaysOffTheSurface", func(t *testing.T) {
		TestAuthoringNeverReachesTheNarrowOrAgentSurfaces(t)
	})

	t.Run("MCPHasNoConfigAuthoringVerb", func(t *testing.T) {
		src, err := os.ReadFile("mcp.go")
		if err != nil {
			t.Fatalf("mcp.go is the CLI's MCP dispatcher: %v", err)
		}
		verbs := map[string]bool{}
		for _, m := range regexp.MustCompile(`case\s+"([^"]+)"`).FindAllStringSubmatch(string(src), -1) {
			verbs[m[1]] = true
		}
		for _, v := range []string{"list", "call", "serve"} {
			if !verbs[v] {
				t.Errorf("the mcp CLI lost its %q verb — the refusal below would be an accident, not a ruling", v)
			}
		}
		// Amended 2026-11-02 at S46 T9, the same way the PL15 subtest above
		// was amended at the S45 merge: the old loop welded the CLI shut in
		// the PRE-S46 world (file config; writes ride proposal + commit
		// only). S46 MG5 rules the human management verbs into existence as
		// CLIENTS of the human socket methods — add/remove/update are door
		// calls, not file writes — and S46's own guards, not this stale
		// grep, pin that ruling: TestMcpWritesAreAHumanDoor (the write
		// methods registered iff the seam is wired, RequiresInitialize,
		// never on the narrow or agent surfaces) and
		// TestMcpRowsAreWrittenOnlyThroughTheAdminSeam (rows reachable only
		// through the named admin files — the CLI files carry no store
		// selectors, walked by both guards). A regression in EITHER half of
		// the ruling fails this test. What the CLI-side still owes and
		// stays welded here: no file-writing word ever enters the mcp CLI.
		for _, v := range []string{"save", "apply", "commit"} {
			if verbs[v] {
				t.Errorf("the mcp CLI carries a %q verb — nothing writes an MCP file from the CLI any more (rows through the human door only; S46 MG5)", v)
			}
		}
		TestMcpWritesAreAHumanDoor(t)
		TestMcpRowsAreWrittenOnlyThroughTheAdminSeam(t)
	})

	t.Run("S40AU1TriggerAuthoringStaysHumanOnly", func(t *testing.T) {
		for name := range tools.Registry() {
			if strings.Contains(strings.ToLower(name), "trigger") {
				t.Errorf("tool registry carries %q — AU1: no trigger tool, ever", name)
			}
		}
		for _, name := range tools.ToolNames() {
			if strings.Contains(strings.ToLower(name), "trigger") {
				t.Errorf("ToolNames() carries %q — the model's vocabulary has no trigger verb", name)
			}
		}
		reach := regexp.MustCompile(`(?i)(CreateEventTrigger|UpdateEventTrigger|SetEventTriggerEnabled|EventTriggerWriter|trigger\.(create|update|enable|disable))`)
		seen := 0
		var offenders []string
		for _, dir := range []string{"tools", "records", "narrow", "agentsvc", "mcp"} {
			root := filepath.Join("..", "..", "internal", dir)
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return err
				}
				seen++
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if reach.Find(b) != nil {
					offenders = append(offenders, path)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scanning %s: %v", dir, err)
			}
		}
		if seen < 10 {
			t.Fatalf("the scan read %d files — a grep that reads nothing passes forever", seen)
		}
		if len(offenders) != 0 {
			t.Fatalf("trigger-write reach from agent-facing packages: %v — AU1 says these doors are human-only", offenders)
		}
	})
}

// ─── the "originals preserved" pin ──────────────────────────────────────────
//
// The mechanism (recorded in T7's Notes): the PRE-SLICE guard bodies are
// pinned by the SHA-256 of their parser-extracted source — the `func`
// keyword through the matching `}` — byte-for-byte, exactly the way the
// digest helper computed the pinned values on 2026-11-01 (go/parser, not
// awk: a column-0 `}` inside a raw string would fool a line recipe, and the
// parser cannot be fooled). Green means this slice edited none of them; the
// ONE amendment this slice makes is the loop's decision-site walk (not one
// of the seven — it is re-asserted above, and its change is recorded in the
// body's comment and in T7's Notes).
type originalGuardPin struct {
	guard  string
	file   string // relative to aeon/ (the walk roots at ../../ from cmd/aeon)
	digest string // sha256 of the parser-extracted body, byte-for-byte
}

var originalGuardPins = []originalGuardPin{
	{"TestNoKeyLevelGrantExists", "internal/narrow/trigger_test.go", "343fce53efe4c144f129c234535283894e2b5d1ca69204fc9f8ad70c34df5fd9"},
	{"TestTriggeredLoopIsUnattended", "internal/loop/allowoutward_test.go", "5fb7a2a606438ea30eb51e09f7ac53948e892a92c1d93845ef0121b0c3a340d9"},
	{"TestIrreversibleDeniesToo", "internal/loop/allowoutward_test.go", "9e75822cab9a202ef653616f676b861a3f58d9798b1ce35e919b510ad262345d"},
	{"TestAllowOutwardIsPerLoop", "internal/loop/allowoutward_test.go", "cf7ebd0673b9c62a27579a57b7b769b153ccc968a9b561a4ab3e69f4c815087f"},
	{"TestAttendednessCannotBeInferredFromPolling", "internal/loop/allowoutward_test.go", "30a14ce7d9baae6c4849ae0a4fb42ddba26d8e20003f519c2efab81ddc31a59b"},
	{"TestClassesAreNotConfigurable", "internal/action/class_test.go", "f62e2a5bfe9504d02fe07e7c72fa328fab42cea91128ab50cd4bc9a655b7384e"},
	{"TestClockStartedLoopIsUnattendedAndCarriesProvenance", "internal/daemon/trigger_fire_test.go", "5c2f581f54ae98c73328c9847964561efd2542a115d22c961f606204ca535c5c"},
}

// extractGuardFuncSource returns the named function's own text — the `func`
// keyword through its matching `}` — by parsing the file. The synthetic-body
// case (the teeth-check) prepends a package clause so the parser has a file
// to chew; the offsets are relative either way.
func extractGuardFuncSource(t *testing.T, src, name string) string {
	t.Helper()
	if !strings.HasPrefix(strings.TrimSpace(src), "package ") {
		src = "package main\n\n" + src // a re-extraction of a body (the teeth-check), not a file
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name || fd.Body == nil {
			continue
		}
		start := fset.Position(fd.Pos()).Offset
		end := fset.Position(fd.Body.Rbrace).Offset + 1 // the `}` itself
		return src[start:end]
	}
	t.Fatalf("no func %s in the file — the guard this pin covers is GONE (a deleted guard is the loudest kind of amendment)", name)
	return ""
}

// guardBodyDigest is the pin's digest: sha256 of the parser-extracted body,
// byte-for-byte — no normalisation, because these seven bodies are pinned
// WHOLE (this slice amends none of them; the one amendment is re-asserted,
// not re-pinned).
func guardBodyDigest(t *testing.T, src, name string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(extractGuardFuncSource(t, src, name)))
	return hex.EncodeToString(sum[:])
}

// TestOriginalGuardBodiesAreByteIdentical is the pin: the seven pre-slice
// guard bodies have not drifted. (S45's sibling
// TestOriginalGuardsAreByteIdentical in policy_amend_test.go pins the
// assertion LOGIC of the guards S45 itself amended; this one pins the BODIES
// S56 T7's amendments left untouched — the two landed on the same name in
// the same package when S45 merged first, so this one takes the
// disambiguating one.) Each entry fails with the guard's name, its file,
// and the digest it recorded, so the fix is a deliberate, listed amendment
// in T7's Notes — never a re-pinned constant.
func TestOriginalGuardBodiesAreByteIdentical(t *testing.T) {
	for _, pin := range originalGuardPins {
		path := filepath.Join("..", "..", pin.file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", pin.file, err)
		}
		got := guardBodyDigest(t, string(src), pin.guard)
		if got != pin.digest {
			t.Errorf("%s (%s): the pre-slice guard body changed — pinned %s, computed %s. The bodies are never edited: an amendment here is a deliberate, listed change in T7's Notes, not a re-pinned constant.",
				pin.guard, pin.file, pin.digest, got)
		}
	}

	// The pin has teeth of its own: the SAME extraction, run against a body
	// whose granty clause was quietly loosened to `false`, must hash
	// differently — the exact edit that would re-open O2's door.
	src, err := os.ReadFile("../../internal/narrow/trigger_test.go")
	if err != nil {
		t.Fatal(err)
	}
	body := extractGuardFuncSource(t, string(src), "TestNoKeyLevelGrantExists")
	const clause = "if f := tv.Field(i); granty.MatchString(f.Name) {"
	loosened := strings.Replace(body, clause, "if f := tv.Field(i); false {", 1)
	if loosened == body {
		t.Fatal("the teeth-check could not find the granty clause to loosen — the guard's shape changed under this test; re-read it, do not re-pin it")
	}
	if guardBodyDigest(t, loosened, "TestNoKeyLevelGrantExists") == guardBodyDigest(t, body, "TestNoKeyLevelGrantExists") {
		t.Fatal("a guard whose granty clause was replaced by `false` hashed identically — the pin cannot fail, so it is not a pin")
	}
}
