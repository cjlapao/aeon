package main

// policy_amend_test.go — S45 T6 (PM10): PL15 and TD7, stated POSITIVELY,
// with canaries. The slice's security-invariant file.
//
// PL15 ("authoring lives on no socket") and TD7 ("commit/reject are
// CLI-only") were enforced by tests whose WORDING is now false for the
// socket and still true for everything else. The operator's ruling of
// 2026-10-30 (PM1) moved policy.add/remove/seedCorpus and
// proposal.commit/reject onto the socket as the HUMAN door — the 0600
// endpoint plus peer credentials, no claim or role in the path — and what
// keeps that from being a quiet hole is this file:
//
//   - POSITIVE: the five methods are registered when their services are
//     wired (the app has its door, P34), each RequiresInitialize, none
//     Debug, none present without its service (V13); the write boundary
//     they cross has exactly the callers it may have, by walk.
//   - NEGATIVE: the four caller-classes that are not the person — the
//     narrow HTTP/MCP-stdio projection, the agent-services enumeration,
//     the tool registry, the records catalog — name none of them, and the
//     rendered boundary statement does not list them (PM1's argument, made
//     mechanical).
//   - CANARIES: every negative predicate is fed a synthetic violation and
//     must report it. A guard that cannot fail is not a guard (PM10).
//   - THE PIN: the pre-amendment assertion LOGIC of the guards this slice
//     superseded or amended is pinned by hash (the method of S35 T6 and
//     T4's/T5's Notes; the hash mechanism is recorded in T6's Notes).
//
// Sibling functions, never edits: not one pre-amendment body in
// policy_test.go, policy_seed_corpus_test.go, internal/protocol/
// policy_test.go or internal/protocol/proposals_test.go is touched here.
// T4 and T5 already skipped the four bodies that became false by
// construction and restated each beside them; this file consolidates the
// PM10 statement and does NOT duplicate those siblings
// (TestNoPolicyAuthoringReachesTheNarrowSurfaceAmended,
// TestAuthoringServiceIsTheOnlyDaemonCallerOfBoundaryAddRemove,
// TestNothingSeedsAutomaticallyAmended,
// TestPolicyAuthoringRegistrationIsConditional,
// TestPolicyAuthoringNamesStayConfined,
// TestProposalCommitExistsOnlyAsAHumanDoor,
// TestOnlyDecideAndEffectCallersAreTheWhitelisted).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

// ─── the five doors, as data ────────────────────────────────────────────────

// authoringDoorMethods is PM1's exact set: the three authoring verbs (T4)
// and the two decision verbs (T5) that ride the socket as the human door.
// The set is CLOSED — TestPolicyAuthoringIsAHumanDoor asserts the table
// names nothing else under policy./proposal. beyond the reads and the
// propose trio, so a sixth door cannot enter uninvited.
var authoringDoorMethods = []protocol.MethodName{
	protocol.MethodPolicyAdd, protocol.MethodPolicyRemove, protocol.MethodPolicySeedCorpus,
	protocol.MethodProposalCommit, protocol.MethodProposalReject,
}

// policyReadMethods / proposalReadMethods are the non-door names the table
// legitimately carries under those two prefixes: the seven policy reads
// (four S13-era, three S45's PM5/PM6/PM8) and proposal's inert trio
// (create/list/discard) plus the decision door's own read (get). Anything
// else with these prefixes is the uninvited sixth.
var sanctionedSocketNames = map[protocol.MethodName]bool{
	protocol.MethodPolicyList: true, protocol.MethodPolicyWhy: true,
	protocol.MethodPolicyInForce: true, protocol.MethodPolicyEvents: true,
	protocol.MethodPolicyGet: true, protocol.MethodPolicyVocabulary: true,
	protocol.MethodPolicyChanges: true,
	protocol.MethodPolicyAdd:     true, protocol.MethodPolicyRemove: true,
	protocol.MethodPolicySeedCorpus: true,
	protocol.MethodProposalCreate:   true, protocol.MethodProposalList: true,
	protocol.MethodProposalDiscard: true,
	protocol.MethodProposalGet:     true, protocol.MethodProposalCommit: true,
	protocol.MethodProposalReject: true,
}

// authoringLiterals are the five wire names as they may be SPOKEN in Go
// source (the quoted literal) and as the constants that name them — the
// ten tokens the text grep forbids under the four agent-reachable packages.
var authoringLiterals = []string{
	`"policy.add"`, `"policy.remove"`, `"policy.seedCorpus"`,
	`"proposal.commit"`, `"proposal.reject"`,
	"MethodPolicyAdd", "MethodPolicyRemove", "MethodPolicySeedCorpus",
	"MethodProposalCommit", "MethodProposalReject",
}

// forbiddenAgentSurfaces are the four caller-classes that are not the
// person (PM1): the enumerated projection, the agent-services surface, the
// tool registry, the records catalog.
var forbiddenAgentSurfaces = []string{
	"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records",
}

// ─── the stubs: enough seam to register, nothing to dispatch ────────────────

// guardAuthoringService and guardDecideService are registration markers.
// This file asserts the SHAPE of the doors (registered iff wired, gated,
// non-debug) and the ABSENCE of them from the agent-reachable surfaces; the
// behaviour behind each door belongs to internal/daemon's authoring and
// decide suites (T4's and T5's), which are the only tests that dispatch.
type guardAuthoringService struct{}

func (guardAuthoringService) Add(context.Context, protocol.Actor, protocol.PolicyAddParams) (protocol.PolicyAddResult, error) {
	return protocol.PolicyAddResult{}, nil
}
func (guardAuthoringService) Remove(context.Context, protocol.Actor, string) (protocol.PolicyRemoveResult, error) {
	return protocol.PolicyRemoveResult{}, nil
}
func (guardAuthoringService) SeedCorpus(context.Context, protocol.Actor, protocol.SeedCorpusParams) (protocol.SeedCorpusResult, error) {
	return protocol.SeedCorpusResult{}, nil
}

type guardDecideService struct{}

func (guardDecideService) GetProposal(context.Context, int64) (protocol.ProposalRow, bool, error) {
	return protocol.ProposalRow{}, false, nil
}
func (guardDecideService) ProposalDigest(payload string) string { return "digest" }
func (guardDecideService) CommitProposal(context.Context, protocol.Actor, int64, string) (protocol.ProposalCommitResult, error) {
	return protocol.ProposalCommitResult{}, nil
}
func (guardDecideService) RejectProposal(context.Context, protocol.Actor, int64, string) (protocol.ProposalRow, error) {
	return protocol.ProposalRow{}, nil
}

// newDoorTable builds the table the way a daemon WITH both doors builds it
// (daemon.go's wiring): NewTable, assign the seams, register both. The
// returns are asserted, not trusted — a false here means the human door is
// wired out of existence and the app has no door at all.
func newDoorTable(t *testing.T, withAuthoring, withDecider bool) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if withAuthoring {
		tbl.PolicyAuthoring = guardAuthoringService{}
		if !tbl.RegisterPolicyAuthoring() {
			t.Fatal("RegisterPolicyAuthoring refused to register over a non-nil seam — PM1's authoring door is gone")
		}
	}
	if withDecider {
		tbl.ProposalDecider = guardDecideService{}
		if !tbl.RegisterProposalDecider() {
			t.Fatal("RegisterProposalDecider refused to register over a non-nil seam — PM1's decision door is gone")
		}
	}
	return tbl
}

// ─── the positive half ─────────────────────────────────────────────────────

// TestPolicyAuthoringIsAHumanDoor states the amended rule POSITIVELY,
// first: the five doors exist, on the socket, gated, product surface — and
// only with the services behind them. The negative halves (they reach
// nothing else) are TestAuthoringNeverReachesTheNarrowOrAgentSurfaces; the
// absence claim is not load-bearing on this test's name.
func TestPolicyAuthoringIsAHumanDoor(t *testing.T) {
	tbl := newDoorTable(t, true, true)

	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}

	// (1) the exact set is present.
	for _, m := range authoringDoorMethods {
		if !registered[m] {
			t.Fatalf("%s is not registered with its service wired — PM1's human door is wired out of existence (P34: the app must have its door)", m)
		}
	}

	// (2) and NOTHING ELSE under policy./proposal. slipped in beside them:
	// every registered name with those two prefixes is a sanctioned read or
	// one of the five. A sixth door (policy.delete, proposal.accept,
	// proposal.approve) is the hole this closes.
	for _, m := range tbl.Methods() {
		s := string(m)
		if !strings.HasPrefix(s, "policy.") && !strings.HasPrefix(s, "proposal.") {
			continue
		}
		if !sanctionedSocketNames[m] {
			t.Errorf("%s is registered under the policy./proposal. prefix and is neither a read nor one of PM1's five doors — the door set is closed", m)
		}
	}

	// (3) each is gated and non-debug: a write before the handshake is
	// unthinkable, and Debug is never what keeps a method off the narrow
	// projection (that absence is this file's negative half, owned there —
	// never an accident of a flag).
	for _, m := range authoringDoorMethods {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s vanished between Methods() and Lookup()", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize — an authoring/decision write on an unnegotiated connection is unthinkable (PM7)", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — these are product methods on the wide socket; the narrow exclusion must never rest on a Debug flag (PM10)", m)
		}
	}

	// (4) each service nil ⇒ its doors ABSENT, and Dispatch answers the
	// sentinel (V13: absent, never registered-and-refusing). Checked per
	// service, so a daemon wired for authoring but not deciding cannot
	// quietly keep the decision doors (or vice versa).
	for _, tc := range []struct {
		name          string
		withAuthoring bool
		withDecider   bool
		want          []protocol.MethodName
	}{
		{"both nil", false, false, authoringDoorMethods},
		{"authoring nil", false, true, []protocol.MethodName{
			protocol.MethodPolicyAdd, protocol.MethodPolicyRemove, protocol.MethodPolicySeedCorpus}},
		{"decider nil", true, false, []protocol.MethodName{
			protocol.MethodProposalCommit, protocol.MethodProposalReject}},
	} {
		none := newDoorTable(t, tc.withAuthoring, tc.withDecider)
		for _, m := range tc.want {
			if _, ok := none.Lookup(m); ok {
				t.Errorf("%s: %s is registered without its service — registered-and-refusing is V13's forbidden shape", tc.name, m)
			}
			if _, err := none.Dispatch(context.Background(), "c-door", m, nil); err == nil {
				t.Errorf("%s: %s dispatched without its service", tc.name, m)
			} else if !errors.As(err, new(protocol.ErrUnknownMethod)) {
				t.Errorf("%s: %s answered %v, want ErrUnknownMethod — absent means the table never heard of it", tc.name, m, err)
			}
		}
		// The doors that ARE wired in each case stay present: one service's
		// absence never takes the other's door down with it.
		for _, m := range authoringDoorMethods {
			wired := (tc.withAuthoring && strings.HasPrefix(string(m), "policy.")) ||
				(tc.withDecider && strings.HasPrefix(string(m), "proposal."))
			if !wired {
				continue
			}
			if _, ok := none.Lookup(m); !ok {
				t.Errorf("%s: %s is missing although its service is wired", tc.name, m)
			}
		}
		// And the read window survives every combination (a closed door is
		// not a closed read).
		for _, m := range []protocol.MethodName{protocol.MethodPolicyList, protocol.MethodPolicyGet,
			protocol.MethodPolicyVocabulary, protocol.MethodPolicyChanges, protocol.MethodProposalList} {
			if _, ok := none.Lookup(m); !ok {
				t.Errorf("%s: read %s vanished under %s", tc.name, m, tc.name)
			}
		}
	}
}

// writeBoundaryCallers is the real call graph of the authoring/decision
// write boundary, by walk — the positive statement of who may cross it
// (PM1, PM11). Every entry names the file, the seam it is, and why.
//
// The measured set is on record in T4's, T5's and T6's Notes; the walk
// below re-measures it every run and fails on a stray caller, so this map
// is the CONTRACT, not the discovery.
var writeBoundaryCallers = map[string]string{
	"internal/policy/store.go":            "the boundary's own implementation (its RepoFuncs hooks are the storage crossing; excluding the definition file does not weaken the claim)",
	"internal/desk/decide.go":             "the ONE orchestration (PM4: one implementation, two doors) — the claimed-from flip and the audit row",
	"internal/daemon/policy_authoring.go": "the socket's authoring door — the ONLY daemon-side caller of the boundary's Add/Remove (T4)",
	"internal/daemon/proposal_decide.go":  "the socket's decision door — the store's DecideProposal shim (T5)",
	"internal/daemon/policy_gate.go":      "the enforcement gate's RepoFuncs bridge (the store's own authoring hooks, never the boundary)",
	"cmd/aeon/policy.go":                  "the CLI door (PM4's offline fallback) and the boundary constructor", "cmd/aeon/proposal_decide.go": "the CLI decision door's DecideProposal shim (T5, PM4: the CLI is a caller of desk, not a copy)",
	"internal/protocol/agent_grants.go":     "the agent doors (create · duplicate · edit · delete) author the rules that go with an agent's tools THROUGH the PolicyAuthoring interface — the same socket door, as the person who pressed the button — never around it (PL5: a tool an agent was given needs the rule that lets it be called; reviewed 2026-11-11)",
	"internal/protocol/policy_authoring.go": "the socket DISPATCH of the authoring door: its `.Remove(ctx…)` matches the Remove regex although it forwards to the PolicyAuthoring INTERFACE (T4's ruling, re-measured by T6's grep)",
	"internal/intake/resume.go":             "S61 T6: the intake engine marks its own plan applied, under the COMMITTER, when its ledger completes (GI7's dedicated door, GI13's link step) — the generic proposal.commit door refuses an intake_plan (T5), so this is the one seam that ever decides an intake_plan, and it holds no decide logic of its own: it calls the store's decide verbatim",
}

// writeBoundaryPackages is the one package-level admission: internal/desk
// owns the single implementation of every authoring/decision effect (PM4),
// so a file of its own may reach the boundary it orchestrates. A package
// admission is the widest form of exemption, so it is the ONLY one, it is
// named here rather than buried in a prefix test, and nothing outside desk
// gets it — above all not internal/daemon (which reaches the boundary only
// through the two door files above) or internal/store (whose PolicyAdd/
// PolicyRemove are definitions, not calls).
var writeBoundaryPackages = []string{"internal/desk/"}

// writeBoundaryExempt is the whitelist as a predicate.
func writeBoundaryExempt(rel string) bool {
	if _, ok := writeBoundaryCallers[rel]; ok {
		return true
	}
	for _, dir := range writeBoundaryPackages {
		if strings.HasPrefix(rel, dir) {
			return true
		}
	}
	return false
}

// writeBoundaryPatterns are the calls that ARE the write boundary and its
// commit effects. The Add arm requires the policy type in the argument
// list (S20 T9's honest tightening, carried from the original guard); the
// Remove arm stays wide — id-only authoring verbs keep the wide net.
var writeBoundaryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\.Add\([^)]*\),\s*policy\.Policy\{`),
	regexp.MustCompile(`\.Remove\((ctx\b|context\.)`),
	regexp.MustCompile(`\.PolicyAdd\((ctx\b|context\.)`),
	regexp.MustCompile(`\.PolicyRemove\((ctx\b|context\.)`),
	regexp.MustCompile(`\.RecordAuthored\((ctx\b|context\.)`),
	regexp.MustCompile(`\.DecideProposal\(`),
	regexp.MustCompile(`\bSaveConfig\(`),
}

// writeBoundaryCallSites returns the pattern each CALL (not definition) in
// the text matches. A match sitting on a `func ` line is a DEFINITION — the
// boundary's Add, the store's PolicyAdd/PolicyRemove, mcp.SaveConfig — and
// definitions belong to the files that own them: this guard's subject is
// CALLERS. Shared by the walk and by the contract-check below so both read
// the same regexes the same way.
func writeBoundaryCallSites(text string) map[string]string {
	hits := map[string]string{}
	for _, re := range writeBoundaryPatterns {
		for _, m := range re.FindAllStringIndex(text, -1) {
			lineStart := strings.LastIndex(text[:m[0]], "\n") + 1
			lineEnd := strings.Index(text[m[0]:], "\n")
			if lineEnd < 0 {
				lineEnd = len(text)
			} else {
				lineEnd += m[0]
			}
			line := strings.TrimSpace(text[lineStart:lineEnd])
			if strings.HasPrefix(line, "func ") || strings.HasPrefix(line, "func(") {
				continue
			}
			hits[re.String()] = line
			break
		}
	}
	return hits
}

// writeBoundaryOffenses reports every way one file's text crosses the write
// boundary from outside the contract. Factored out of the walk so the
// canary (TestAuthoringGuardsHaveTeeth) proves the IDENTICAL regexes reject
// a forbidden call — never a copy that could silently diverge.
func writeBoundaryOffenses(rel, text string) []string {
	if writeBoundaryExempt(rel) {
		return nil
	}
	var out []string
	for re, line := range writeBoundaryCallSites(text) {
		out = append(out, "calls the authoring/decision write boundary ("+re+" at `"+line+"`) from outside the contract — the boundary is crossed only by internal/policy, the desk, the two daemon door files, the gate's bridge and the CLI (S45 PM1/PM11)")
	}
	return out
}

// TestOnlyTheDeskAndTheDaemonSeamsCallTheWriteBoundary walks the repository
// and asserts the call graph above and nothing else. It fails on a stray
// // caller of Add/Remove/PolicyAdd/PolicyRemove/RecordAuthored/DecideProposal/
// SaveConfig — i.e. a side door onto authoring or onto a commit effect that
// the human door does not own. The walk asserts it saw ≥ 50 non-test files,
// so it can never pass on an empty walk.
func TestOnlyTheDeskAndTheDaemonSeamsCallTheWriteBoundary(t *testing.T) {
	var walked, offenders int
	seen := map[string]bool{}
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs (dist/, node_modules) hold no Go code we own
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev":
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
		for _, reason := range writeBoundaryOffenses(rel, string(src)) {
			offenders++
			t.Errorf("%s %s", rel, reason)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — the grep checked nothing (never pass on an empty walk)", walked)
	}
	if offenders == 0 && walked == 0 {
		t.Fatal("the grep checked nothing")
	}
	// The contract is a CONTRACT both ways: a file listed and no longer
	// present (or no longer crossing the boundary) is the drift the map
	// would otherwise hide — a whitelist entry outliving its reason is how
	// a whitelist grows.
	for rel, why := range writeBoundaryCallers {
		if !seen[rel] {
			t.Errorf("the contract lists %s (%s) but the walk never saw it — the map is keeping a file that no longer exists, or moved", rel, why)
		}
	}
	// And the walk must have seen the doors themselves crossing it: the map
	// is not carrying files that no longer call anything. A listed file that
	// matches no pattern today is an exemption without its reason — the
	// shape a hole waits for.
	for rel := range writeBoundaryCallers {
		src, readErr := os.ReadFile(filepath.Join("../../", rel))
		if readErr != nil {
			t.Errorf("the contract lists %s and the walk saw it, but it cannot be read: %v", rel, readErr)
			continue
		}
		if len(writeBoundaryCallSites(string(src))) == 0 {
			t.Errorf("the contract lists %s as a caller of the write boundary, but it crosses none of the patterns any more — drop the entry (an exemption without its reason is a hole waiting for a caller)", rel)
		}
	}
}

// ─── the negative half — each door a subtest, so a failure names which ──────

// narrowProjectingAuthoring returns the names of the actions whose projected
// SocketMethod is one of the five doors, or ANY policy.* method at all: the
// narrow surface names no policy method today (nine actions, all reads and
// the one trigger), and N8 — no surface without a reason — keeps it that
// way until a slice argues for a read projection. A function over tables so
// the canary can feed it a poisoned one.
func narrowProjectingAuthoring(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		s := string(a.SocketMethod)
		for _, m := range authoringDoorMethods {
			if s == string(m) {
				bad = append(bad, a.Name+"→"+s)
				break // one report per action: the count is the canary's assertion
			}
		}
		if strings.HasPrefix(s, "policy.") {
			bad = append(bad, a.Name+"→"+s)
		}
	}
	return bad
}

// agentServicesNamingAuthoring: the agent-to-agent surface, same predicate
// shape. Over an enumeration argument so the canary can poison it.
func agentServicesNamingAuthoring(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range ms {
		s := string(m.Name)
		reported := false
		for _, d := range authoringDoorMethods {
			if s == string(d) {
				bad = append(bad, s)
				reported = true
				break
			}
		}
		if !reported && (strings.HasPrefix(s, "policy.") || strings.HasPrefix(s, "proposal.")) {
			bad = append(bad, s)
		}
	}
	return bad
}

// isAuthoringToolName reports whether a tool NAME is an authoring/decision
// tool. T7 adds the positive list of the seven allowed names beside this
// predicate (the propose twins and the reads); until then the registry
// carries none, and after it the seven are the whole policy/proposal
// vocabulary a granted agent may hold.
func isAuthoringToolName(name string) bool {
	return regexp.MustCompile(`(?i)commit|reject|policy_(add|remove|seed)|propose_commit|proposal_commit|proposal_reject`).MatchString(name)
}

// catalogSpecsNamingAuthoring reports every catalog Spec that names a door:
// a Method among the five, or a MUTATING spec of the policy/proposal family
// that is not one of the two propose twins (S35's ruling: an agent may
// PROPOSE, never decide; T7 lands those two and nothing else).
func catalogSpecsNamingAuthoring(specs []records.Spec) []string {
	var bad []string
	for _, sp := range specs {
		for _, m := range authoringDoorMethods {
			if sp.Method == string(m) {
				bad = append(bad, sp.Name+"→"+sp.Method)
			}
		}
		if (sp.Family == "policy" || sp.Family == "proposal") && sp.Mutates {
			switch sp.Method {
			case "policy.proposeAdd", "policy.proposeRemove": // T7's propose twins — inert rows, decided by a human
			default:
				bad = append(bad, sp.Name+" (family "+sp.Family+", Mutates, Method "+sp.Method+")")
			}
		}
		if isAuthoringToolName(sp.Name) {
			bad = append(bad, sp.Name+" (name)")
		}
	}
	return bad
}

// authoringLiteralInFile is the text arm: does this file's text name any of
// the five doors, by literal or by constant?
func authoringLiteralInFile(text string) []string {
	var bad []string
	for _, lit := range authoringLiterals {
		if strings.Contains(text, lit) {
			bad = append(bad, lit)
		}
	}
	return bad
}

// boundaryStatementNamingAuthoring checks the projection's own account of
// itself (narrow.BoundaryStatement) for the door names: what the surface
// DISPLAYS must agree with what the table holds, and a statement that lists
// policy.add is a projection advertising a door it must not have.
func boundaryStatementNamingAuthoring(statement string) []string {
	var bad []string
	for _, m := range authoringDoorMethods {
		if strings.Contains(statement, string(m)) {
			bad = append(bad, string(m))
		}
	}
	return bad
}

// TestAuthoringNeverReachesTheNarrowOrAgentSurfaces is PM10's negative half:
// the four caller-classes that are not the person name none of the five
// doors. Each is its own subtest so a failure names WHICH door opened.
func TestAuthoringNeverReachesTheNarrowOrAgentSurfaces(t *testing.T) {
	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		if bad := narrowProjectingAuthoring(actions); len(bad) != 0 {
			t.Errorf("the narrow surface projects authoring through %s — an action projecting policy.add/remove/seedCorpus or proposal.commit/reject hands a key-holder the means to widen the guards written to constrain them (PL15, amended by S45 PM1: the human door stays off this projection)", strings.Join(bad, ", "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := agentServicesNamingAuthoring(ms); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying an authoring or decision door is PM1's refusal made mechanical", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		var bad []string
		for name := range reg {
			if isAuthoringToolName(name) {
				bad = append(bad, name)
			}
		}
		if len(bad) != 0 {
			t.Errorf("the tool registry carries %s — a tool that commits, rejects, adds, removes or seeds is a second door onto the write the socket's human half alone carries (T7's positive list of the allowed propose/read names is the only vocabulary policy may enter here)", strings.Join(bad, ", "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := catalogSpecsNamingAuthoring(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — an agent-reachable authoring or decision action is the side door PL15 named and PM1 kept shut; only the two propose twins (policy.proposeAdd, policy.proposeRemove) may mutate in these families, and only from T7 (T0-Q9's boundary, restated for S45)", strings.Join(bad, ", "))
		}
	})

	t.Run("TextGrep", func(t *testing.T) {
		for _, dir := range forbiddenAgentSurfaces {
			root := filepath.Join("..", "..", dir)
			var walked int
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				src, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				walked++
				if bad := authoringLiteralInFile(string(src)); len(bad) != 0 {
					t.Errorf("%s names %s — a projection or registry that NAMES an authoring/decision method, even in a comment, is the projection the rule forbids (PM10)", filepath.ToSlash(path), strings.Join(bad, ", "))
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

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		if bad := boundaryStatementNamingAuthoring(stmt); len(bad) != 0 {
			t.Errorf("the projection's rendered boundary statement lists %s — the surface DISPLAYS a door its table must not hold (D16: a boundary that contradicts its own data is a second truth)", strings.Join(bad, ", "))
		}
	})
}

// ─── the canary: a guard that cannot fail is not a guard ────────────────────

// TestAuthoringGuardsHaveTeeth feeds every negative predicate a SYNTHETIC
// violation and a clean input: each must REPORT the violation and stay
// silent on the clean one (S35's TestTheCommitEffectWhitelistHasTeeth
// pattern; T4's and T5's inline canaries do the same for their own walks).
// The predicates are exercised as the walks use them — the same functions,
// never copies — so a predicate weakened to vacuity fails HERE, not in the
// next slice that trusts it.
func TestAuthoringGuardsHaveTeeth(t *testing.T) {
	// 1. The narrow projection: a poisoned action projecting
	// proposal.commit must be named; a clean read must not.
	poisoned := []narrow.Action{
		{Name: "aeon_proposal_commit", SocketMethod: protocol.MethodProposalCommit, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
		{Name: "aeon_policy_add", SocketMethod: protocol.MethodPolicyAdd, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}
	if got := narrowProjectingAuthoring(poisoned); len(got) == 0 {
		t.Fatalf("an action projecting proposal.commit/policy.add was not flagged (%v) — the narrow predicate cannot fail, so it is not a guard", got)
	}
	clean := []narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}
	if got := narrowProjectingAuthoring(clean); len(got) != 0 {
		t.Fatalf("a clean action was flagged as projecting authoring (%v) — the narrow predicate fires on nothing", got)
	}

	// 2. The agent-services enumeration.
	poisonedAgents := []protocol.AgentServiceMethod{
		{Name: protocol.MethodPolicySeedCorpus, Cannot: "must not exist"},
		{Name: protocol.MethodProposalReject, Cannot: "must not exist"},
	}
	if got := agentServicesNamingAuthoring(poisonedAgents); len(got) == 0 {
		t.Fatalf("an agent-service method named policy.seedCorpus/proposal.reject was not flagged (%v) — the enumeration predicate cannot fail", got)
	}
	if got := agentServicesNamingAuthoring([]protocol.AgentServiceMethod{
		{Name: protocol.MethodAgentPlugins, ReadOnly: true, Cannot: "must not exist"},
	}); len(got) != 0 {
		t.Fatalf("a clean agent-service method was flagged (%v) — the enumeration predicate fires on the reads", got)
	}

	// 3. The tool registry: the violating names, and the names that must
	// NOT be flagged (a predicate that fires on the reads is as useless as
	// one that fires on nothing — it would delete T7's allowed vocabulary).
	for _, bad := range []string{"policy_add", "policy_remove", "policy_seed_corpus", "proposal_commit", "proposal_reject", "commit_proposal", "reject_proposal"} {
		if !isAuthoringToolName(bad) {
			t.Fatalf("tool name %q is an authoring/decision tool and was not flagged — the registry predicate cannot fail", bad)
		}
	}
	for _, ok := range []string{"policy_list", "policy_show", "proposal_list", "proposal_show", "goal_list", "seed_status"} {
		if isAuthoringToolName(ok) {
			t.Fatalf("tool name %q is a read and was flagged — the registry predicate fires on the reads, so T7's vocabulary would be deleted rather than bounded", ok)
		}
	}

	// 4. The records catalog: a spec whose Method is a door, and a MUTATING
	// policy-family spec that is not a propose twin, must both be named;
	// the reads and the two propose twins must not.
	poisonedSpecs := []records.Spec{
		{Name: "policy_add", Method: "policy.add", Family: "policy", Mutates: true, Class: "detectable"},
		{Name: "policy_remove", Method: "policy.remove", Family: "policy", Mutates: true, Class: "detectable"},
		{Name: "proposal_commit", Method: "proposal.commit", Family: "proposal", Mutates: true, Class: "detectable"},
		{Name: "policy_grant", Method: "policy.grant", Family: "policy", Mutates: true, Class: "detectable"},
	}
	if got := catalogSpecsNamingAuthoring(poisonedSpecs); len(got) == 0 {
		t.Fatalf("four violating catalog specs were fed and only %v was reported — the catalog predicate cannot fail", got)
	}
	cleanSpecs := []records.Spec{
		{Name: "policy_list", Method: "policy.list", Family: "policy", Class: "safe"},
		{Name: "policy_propose_add", Method: "policy.proposeAdd", Family: "policy", Mutates: true, Class: "detectable"},
		{Name: "policy_propose_remove", Method: "policy.proposeRemove", Family: "policy", Mutates: true, Class: "detectable"},
	}
	if got := catalogSpecsNamingAuthoring(cleanSpecs); len(got) != 0 {
		t.Fatalf("the reads and the two propose twins were flagged (%v) — the catalog predicate fires on what S45 permits", got)
	}

	// 5. The text grep: a file under a fake internal/tools tree naming
	// "policy.add" must be caught; one naming only the reads must not.
	dirty := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirty, "dirty.go"),
		[]byte("package tools\n\nvar x = map[string]string{\"commit\": \"policy.add\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty, "clean.go"),
		[]byte("package tools\n\nvar y = map[string]string{\"read\": \"policy.list\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !treeNamesAuthoring(dirty) {
		t.Fatal("a file naming \"policy.add\" was not caught by the text walk — the walk cannot fail")
	}
	onlyClean := t.TempDir()
	if err := os.WriteFile(filepath.Join(onlyClean, "clean.go"),
		[]byte("package tools\n\nvar y = map[string]string{\"read\": \"policy.list\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if treeNamesAuthoring(onlyClean) {
		t.Fatal("a file naming only policy.list was flagged — the text walk fires on the reads")
	}

	// 6. The write-boundary walk: a synthetic caller of DecideProposal in a
	// file the map does not name must be reported, and the same text under
	// a whitelisted path must not (the exemption is the file, not the text).
	const fake = "package daemon\n\nfunc sneaky() {\n\tstore.DecideProposal(id, \"committed\", actor, \"\", at)\n}\n"
	if got := writeBoundaryOffenses("internal/daemon/sneaky.go", fake); len(got) == 0 {
		t.Fatal("a DecideProposal call in a non-contracted daemon file was not caught — the write-boundary walk has no teeth")
	}
	if got := writeBoundaryOffenses("internal/daemon/proposal_decide.go", fake); len(got) != 0 {
		t.Fatalf("the contracted decision door was reported (%v) — the whitelist is not being honoured", got)
	}
	// The Add arm is S20 T9's tightened shape (the arg list must CLOSE
	// before the policy literal), so the synthetic caller spells a real
	// one: `b.Add(withCtx(ctx), policy.Policy{…}, at)`. A literal that
	// cannot produce a hit is the vacuous canary in the other direction.
	const fakeAdd = "package runtime\n\nfunc alsoSneaky() {\n\tb.Add(withCtx(ctx), policy.Policy{Type: policy.TypeToolAccess}, at)\n}\n"
	if got := writeBoundaryOffenses("internal/runtime/sneaky.go", fakeAdd); len(got) == 0 {
		t.Fatal("a boundary Add call outside the contract was not caught — the Add arm of the walk has no teeth")
	}
	const fakeSeed = "package daemon\n\nfunc seedSneaky() {\n\tb.RecordAuthored(ctx, subject, id, at)\n}\n"
	if got := writeBoundaryOffenses("internal/daemon/seed_sneaky.go", fakeSeed); len(got) == 0 {
		t.Fatal("a RecordAuthored call outside the contract was not caught — the authored-audit arm has no teeth")
	}
	// And a definition is NOT a call: the store's own PolicyAdd must not be
	// reported as a caller (this is the arm that could otherwise fire on
	// every definition in internal/store and be "fixed" by whitelisting it).
	const definition = "package store\n\nfunc (s *Store) PolicyAdd(ctx context.Context, r PolicyRow) error {\n\treturn nil\n}\n"
	if got := writeBoundaryOffenses("internal/store/policy.go", definition); len(got) != 0 {
		t.Fatalf("a definition was reported as a caller (%v) — the guard would demand a whitelist entry for every owner", got)
	}

	// 7. The boundary statement: the rendered statement carrying a door name
	// must be caught.
	if got := boundaryStatementNamingAuthoring("Available: policy.add, proposal.commit"); len(got) == 0 {
		t.Fatalf("a boundary statement listing the doors was not flagged (%v) — the statement check cannot fail", got)
	}
	if got := boundaryStatementNamingAuthoring(narrow.BoundaryStatement()); len(got) != 0 {
		t.Fatalf("the REAL boundary statement names the doors (%v) — the projection advertises an authoring method", got)
	}
}

// treeNamesAuthoring is the text-walk predicate as a question, so the
// canary can aim it at a temp tree instead of a package directory.
func treeNamesAuthoring(root string) bool {
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
		if len(authoringLiteralInFile(string(src))) > 0 {
			found = true
		}
		return nil
	})
	return found
}

// ─── the amended R-lines that must still hold ──────────────────────────────

// TestAmendmentDoesNotLoosenOtherHumanDoors re-asserts the two rulings this
// slice's amendment could plausibly have "tidied" away, so that nobody
// weakens them while editing beside them:
//
//   - R6 (S10, narrowed by S35 T4 and S43 T7): the plugin install lifecycle
//     — agent.install / agent.uninstall / agent.update — stays absent from
//     the socket table. agent_test.go:320's TestNoPluginManagementOnNarrowSurface
//     owns the claim; this sibling fails if the two are ever edited
//     together and the ban shrinks. agent.import is NOT in the ban: S43 T7
//     brought it onto the table as a CARD import (AM3), and it asserts
//     POSITIVE here so the two neighbours cannot drift.
//   - S35 TD4: MCP config authoring is proposal + commit only. There is no
//     direct-config verb on the table — the mcp.* surface is exactly
//     {mcp.status, mcp.check}, and mcp.add / mcp.remove answer
//     ErrUnknownMethod. The commit door this slice added is the ONLY way a
//     config write reaches the daemon (T5's effects registry), so adding a
//     direct verb here would delete the human door this slice just built.
func TestAmendmentDoesNotLoosenOtherHumanDoors(t *testing.T) {
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}

	// R6, exactly as agent_test.go states it today.
	for _, banned := range []protocol.MethodName{"agent.install", "agent.uninstall", "agent.update"} {
		if have[banned] {
			t.Fatalf("%s is registered — R6: a socket caller handed the means to install the thing that bypasses the guards is refused; S45's authoring door is not a precedent for this (the S43 files are read-only to this slice)", banned)
		}
		if _, err := tbl.Dispatch(context.Background(), "c-r6", banned, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s is dispatchable (%v) — an absent method must answer the sentinel", banned, err)
		}
	}
	for _, allowed := range []protocol.MethodName{protocol.MethodAgentPlugins, protocol.MethodAgentImport,
		protocol.MethodAgentEnable, protocol.MethodAgentDisable, protocol.MethodAgentRevoke} {
		if !have[allowed] {
			t.Fatalf("%s must stay registered — S35 T4 and S43 T7 moved it off R6's list; shrinking the ban while amending PL15 is the tidy-up this test exists to catch", allowed)
		}
	}

	// S35 TD4: the mcp.* surface is the two reads and nothing else.
	var mcpNames []string
	for m := range have {
		if strings.HasPrefix(string(m), "mcp.") {
			mcpNames = append(mcpNames, string(m))
		}
	}
	got := strings.Join(mcpNames, ",")
	if got != "mcp.check,mcp.status" && got != "mcp.status,mcp.check" {
		t.Fatalf("the mcp.* surface is {%s}, want exactly {mcp.status, mcp.check} — config authoring is proposal + commit (S35 TD4), and this slice's commit door is the only way a config write reaches the daemon", got)
	}
	for _, banned := range []protocol.MethodName{"mcp.add", "mcp.remove", "mcp.saveConfig", "mcp.apply"} {
		if _, err := tbl.Dispatch(context.Background(), "c-mcp", banned, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s answered %v, want ErrUnknownMethod — a direct config verb would be the side door TD4 welded shut, and PM1's human door beside it", banned, err)
		}
	}
	// And the human door beside it really is there: a config write still has
	// its ONE way in (the committed proposal), so the refusal above is not
	// an accident of an unwired table.
	// The precondition that makes the refusal above a REFUSAL and not an
	// accident: with the decision service wired, the commit door is really
	// there — so the absence of a direct mcp verb is the ruling, not a
	// missing wire. (The registration itself is TestPolicyAuthoringIsAHumanDoor's
	// claim; this re-checks it on its own table so the test stands alone.)
	doorTable := newDoorTable(t, true, true)
	if _, ok := doorTable.Lookup(protocol.MethodProposalCommit); !ok {
		t.Fatal("proposal.commit is absent even with the decider wired — the positive half (TestPolicyAuthoringIsAHumanDoor) owns that claim; here it is the precondition that TD4's refusal is deliberate, not vacuous")
	}
}

// ─── the "originals preserved" pin ─────────────────────────────────────────
//
// The mechanism, chosen (T6.md allows the choice) and recorded in T6's
// Notes: REVERSE THE RECORDED LISTED AMENDMENT, NORMALISE, HASH.
//
//  1. EXTRACT the guard's own text with go/ast — the FuncDecl for the named
//     test, `func` keyword through its matching `}`. No awk: the recipe
//     T0/T4/T5 recorded (`awk '/func NAME/,/^}/'`) stops at the first line
//     that is exactly `}` AT COLUMN 0, which for TestOnlyProposalIsSocket-
//     Writable is a line INSIDE its inline canary's raw string — the
//     recorded digest therefore covers 45 of that body's 112 lines. The
//     parser cannot be fooled that way, and it excludes the doc comment,
//     which is prose and not assertion.
//  2. UNDO the listed amendment: every line this slice added to the body is
//     recorded below and must appear EXACTLY ONCE (a missing one means the
//     body moved under the record); the one recorded non-additive change
//     (T5's predicate line in the §7 walk) is recorded as a replacement and
//     is reversed too.
//  3. NORMALISE to the assertion logic: drop blank lines, drop comment-only
//     lines, drop the `t.Skip(...)` first-line, and drop the whitelist DATA
//     map literals (`X := map[string]bool{…}` through its `}`) — the maps
//     are the half option 1 lets grow; the assertions are not.
//  4. SHA-256 the remaining lines, joined by "\n" with a trailing newline.
//
// The pinned values were produced by that same code from `git show
// <parent-of-the-amending-commit>:<file>` (T4's parent 3cf22736 for the
// three T4-era guards, T5's DATA-amendment parent a0388ecd for
// TestOnlyProposalIsSocketWritable, T5's supersession parent 48b36239 for
// TestNoProposalCommitMethodExists), and they cross-check against the
// pre-amendment BODY digests T0, T4 and T5 recorded by the awk recipe — the
// agreement (and the one place the awk recipe is weak) is in T6's Notes.
//
// A change here is not a failure to route around: the pre-amendment logic
// IS the contract, and changing it must be a deliberate, listed amendment
// with a line added to the table below and a sentence in T6's Notes.
type preAmendmentPin struct {
	guard    string
	file     string      // relative to aeon/ (the walk roots at ../../ from cmd/aeon)
	recorded string      // the body digest the superseding task recorded, by its recipe
	why      string      // what the guard enforced, and which task superseded it
	added    []string    // lines the amendment ADDED to this body (recorded, each exactly once)
	changed  [][2]string // recorded non-additive edits: {current text, pre-amendment text}
	logic    string      // the pinned normalised assertion-logic digest
}

var preAmendmentPins = []preAmendmentPin{
	{
		guard:    "TestNoPolicyAuthoringReachesTheNarrowSurface",
		file:     "cmd/aeon/policy_guards_cases_test.go",
		recorded: "ee916b35bccad0c446c5e026d5e154fed4f17649d2a4153617c1f28325a6e19d (T4 Notes; awk '/func NAME/,/^}/')",
		why:      "clauses (a)–(e): the authoring regexes, the four whitelists, the ≥50-file floor, the api pin — superseded by S45 T4/T6 (PM1)",
		added: []string{
			`\t\t"internal/daemon/policy_authoring.go": true,`,
			`\t\t"internal/protocol/policy_authoring.go": true,`,
			"\t\t`\"policy.add`:        true,",
			"\t\t`\"policy.remove`:     true,",
			"\t\t`\"policy.seedCorpus`: true,",
		},
		logic: "3a747d75fdd665293d58374f0bad29dfe4cee06e9a90786f1835270b61a95bf8",
	},
	{
		guard:    "TestNoPolicyAuthoringMethodIsRegistered",
		file:     "internal/protocol/policy_test.go",
		recorded: "59e5135a4eefd1e3ca76d7f73500bfa7d68c347099823cd9dbb930d23ceb0d13 (T4 Notes; awk '/func NAME/,/^}/')",
		why:      "the forbidden-scan and the package source-scan of the authoring names — superseded by S45 T4/T6 (PM1)",
		added:    []string{},
		logic:    "1469c8af11db1e969cff30d07e631158e4bbce709f03fb8754d2a007588b599c",
	},
	{
		guard:    "TestNothingSeedsAutomatically",
		file:     "cmd/aeon/policy_seed_corpus_test.go",
		recorded: "0687b52806aea2cccb0ded2fae9a4ec3b631ce2e848359626b174c321bd3cb71 (T4 Notes; awk '/func NAME/,/^}/')",
		why:      "the three seed spellings across the daemon/CLI dirs — superseded by S45 T4/T6 (PM1), which exempted only the door and only for the capital spelling",
		added:    []string{},
		logic:    "0687b52806aea2cccb0ded2fae9a4ec3b631ce2e848359626b174c321bd3cb71",
	},
	{
		guard:    "TestNoProposalCommitMethodExists",
		file:     "internal/protocol/proposals_test.go",
		recorded: "b1f552d6e2637e7708c143e1ae3da93c782c740d5dbc32d9a4b47238a9acc451 (T5 Notes; pre-skip body, recorded in the skip itself)",
		why:      "proposal.commit/proposal.reject may never be registered names — superseded by S45 T5/T6 (PM1), sibling TestProposalCommitExistsOnlyAsAHumanDoor",
		added:    []string{},
		logic:    "36cd5a95f66d9d1e7a7eb0402197e888131f26ef065da3ec5a72ec534641beb8",
	},
	{
		guard:    "TestOnlyProposalIsSocketWritable",
		file:     "cmd/aeon/policy_guards_cases_test.go",
		recorded: "013de5bf198d0372b77449fb176d8b16ffdb4f21b0881831b5c97956752e805b (T0 Notes; awk '/func NAME/,/^}/' — stops inside the canary's raw string, which is why this pin re-extracts by parser)",
		why:      "S35 TD10's commit-effect whitelist and the §7 literal walk — AMENDED (not superseded) by S45 T5 (PM1/PM4): the test still runs",
		// The three allowedCommitEffect entries T5 listed
		// (cmd/aeon/proposal_decide.go, internal/daemon/proposal_decide.go,
		// internal/desk/decide.go) live in the package-level `var` block
		// OUTSIDE this function, so they are not part of the extraction and
		// need no reverse-op — the assertion half that DID move is the
		// predicate below, and it is recorded.
		changed: [][2]string{
			{
				"if lit := commitRejectLiteral.FindString(string(src)); lit != \"\" && !decisionMethodLiteralSites[filepath.ToSlash(path)] {",
				"if lit := commitRejectLiteral.FindString(string(src)); lit != \"\" {",
			},
		},
		logic: "882731f65436c7e4935b05e9a49167901317de6f162027f8e30c51b09df9db5b",
	},
}

// guardLogicDigest extracts, un-amends, normalises and hashes one guard body.
func guardLogicDigest(t *testing.T, pin preAmendmentPin) string {
	t.Helper()
	path := filepath.Join("../../", pin.file)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", pin.file, err)
	}
	text := extractFuncSource(t, string(src), pin.guard)

	// (2) undo the recorded amendment. Each recorded line must appear
	// exactly once: absent means the body changed under the record (the
	// amendment was edited, moved or deleted); twice means someone grew a
	// second copy of a whitelist entry the record does not list.
	for _, line := range pin.added {
		want := strings.ReplaceAll(line, `\t`, "\t")
		if n := strings.Count(text, want+"\n"); n != 1 {
			t.Fatalf("%s: the recorded amendment line %q appears %d times, want exactly 1 — the body has changed under the record, and a hash pin whose reverse-ops do not apply is not a pin", pin.guard, line, n)
		}
		text = strings.Replace(text, want+"\n", "", 1)
	}
	for _, pair := range pin.changed {
		if n := strings.Count(text, pair[0]); n != 1 {
			t.Fatalf("%s: the recorded amended text %q appears %d times, want exactly 1 — the body has changed under the record", pin.guard, pair[0], n)
		}
		text = strings.Replace(text, pair[0], pair[1], 1)
	}

	// (3) normalise to the assertion logic, then (4) hash.
	var kept []string
	skipMap := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if skipMap {
			if trimmed == "}" {
				skipMap = false
			}
			continue
		}
		if strings.HasSuffix(trimmed, "map[string]bool{") {
			skipMap = true
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "t.Skip(") {
			continue
		}
		kept = append(kept, line)
	}
	sum := sha256.Sum256([]byte(strings.Join(kept, "\n") + "\n"))
	return hex.EncodeToString(sum[:])
}

// extractFuncSource returns the named function's own text — `func` keyword
// through its matching `}` — by parsing the file. go/ast is the point: a
// column-0 `}` inside a raw string (the canary in
// TestOnlyProposalIsSocketWritable) is the exact thing a line-based recipe
// cannot see, and this pin's whole value is that it sees the real body.
func extractFuncSource(t *testing.T, src, name string) string {
	t.Helper()
	if !strings.HasPrefix(strings.TrimSpace(src), "package ") {
		src = "package main\n\n" + src // a re-extraction of a body (the teeth-check), not a file
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
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

// TestOriginalGuardsAreByteIdentical is the pin: the pre-amendment assertion
// LOGIC of the guards this slice superseded or amended is the contract, and
// it has not drifted. Each entry fails with the guard's name, the task that
// superseded it, and the digest it recorded, so the fix is a listed
// amendment in T6's Notes — never a re-pinned constant.
func TestOriginalGuardsAreByteIdentical(t *testing.T) {
	for _, pin := range preAmendmentPins {
		got := guardLogicDigest(t, pin)
		if got != pin.logic {
			t.Errorf("%s (%s): the pre-amendment assertion logic changed — pinned %s, computed %s. %s. The bodies are never edited: an amendment here is a deliberate, listed change (a line in the table above + a sentence in T6's Notes), not a re-pinned constant.",
				pin.guard, pin.file, pin.logic, got, pin.why)
		}
	}

	// The pin has teeth of its own: the SAME code, run against a body whose
	// assertion was quietly loosened, must produce a different digest. The
	// synthetic case is the narrow-surface guard's (b) clause deleted — the
	// exact edit that would re-open the door this slice is accused of
	// opening, and the one a whitelist-shaped pin (which sees only the maps)
	// would let through.
	src, err := os.ReadFile("../../cmd/aeon/policy_guards_cases_test.go")
	if err != nil {
		t.Fatal(err)
	}
	body := extractFuncSource(t, string(src), "TestNoPolicyAuthoringReachesTheNarrowSurface")
	loosened := strings.Replace(body, "if boundaryAuthoringCall.MatchString(text) && !allowedBoundary[rel] {",
		"if false && !allowedBoundary[rel] {", 1)
	if loosened == body {
		t.Fatal("the teeth-check could not find the (b) clause to loosen — the guard's shape changed under this test; re-read it, do not re-pin it")
	}
	pin := preAmendmentPins[0]
	pin.added = nil // the loosened text is a synthetic body, not the file's
	untouched := normaliseAndHash(extractFuncSource(t, loosened, "TestNoPolicyAuthoringReachesTheNarrowSurface"))
	amended := normaliseAndHash(body)
	if untouched == amended {
		t.Fatal("a guard whose (b) clause was replaced by `if false` hashed identically — the pin cannot fail, so it is not a pin")
	}
	_ = pin
}

// normaliseAndHash is step (3)+(4) of the pin mechanism without the reverse
// ops — the teeth-check's half.
func normaliseAndHash(text string) string {
	var kept []string
	skipMap := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if skipMap {
			if trimmed == "}" {
				skipMap = false
			}
			continue
		}
		if strings.HasSuffix(trimmed, "map[string]bool{") {
			skipMap = true
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "t.Skip(") {
			continue
		}
		kept = append(kept, line)
	}
	sum := sha256.Sum256([]byte(strings.Join(kept, "\n") + "\n"))
	return hex.EncodeToString(sum[:])
}
