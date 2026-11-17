// The CLI surface of S15, tested in-process over a REAL temp store (the
// policy verbs' idiom: XDG_STATE_HOME at a throwaway, store.Open({}) for
// the second handle, WAL serialising behind the daemon's absent writer).
// What is under test here is not argument parsing — it is that the three
// refusals this slice is made of survive contact with a command line:
// nothing fetches (AP10), import does not enable (AP2), and no state verb
// reaches the narrow surface (R6).
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/agentsvc"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func agentHome(t *testing.T) {
	t.Helper()
	isolateStoreEnv(t)
	plugin.Reset()
	plugin.BindStateStore(nil)
	t.Cleanup(func() { plugin.Reset(); plugin.BindStateStore(nil) })
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func openAgentStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{})
	if err != nil {
		t.Fatalf("open the temp store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// writeAgentPackage builds the allow-listed three: manifest, signed card,
// and (optionally) a carried denial file. The card signs through the ONE
// implementation (agentsvc.Sign); the CLI's verification runs through the
// ONE verifier (agentsvc.LoadCardFile, injected into the plugin seam by
// wirePluginSeams) — the fixture cannot be lenient in a way production
// would not be.
func writeAgentPackage(t *testing.T, name string, extra map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: the CLI fixture\nrequests:\n  tools: [read]\n---\nYou are " + name + ". Answer in one line.\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	card, err := json.Marshal(map[string]any{
		"name": name, "description": "fixture", "version": "1.0.0",
		"protocolVersion": "0.3", "url": "local",
		"defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"},
		"skills": []map[string]any{{"id": "do-thing", "name": "Do the thing", "description": "does it", "inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte(name))
	env, err := json.Marshal(agentsvc.SignedCard{
		Card:      card,
		PublicKey: base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed[:]), card)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card.json"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range extra {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func carriedFile(name string) map[string][]byte {
	return map[string][]byte{"policies/no-outbound.json": []byte(`{"id":"no-outbound-` + name + `","type":"tool_access","target":{"kind":"agent","id":"` + name + `"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"operator"}`)}
}

func runAgent(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := cmdAgent(&buf, args)
	return buf.String(), err
}

func TestImportPrintsConsentAndDoesNotEnable(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "groot", carriedFile("groot"))
	out, err := runAgent(t, "import", dir)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	// The consent, T4's own words — REQUESTS, never grants.
	if !strings.Contains(out, "REQUESTS") || !strings.Contains(out, "read") {
		t.Fatalf("the consent must print what the package REQUESTS: %q", out)
	}
	if strings.Contains(out, "enabled") && !strings.Contains(out, "as DISABLED") {
		t.Fatalf("the output must not read like a grant: %q", out)
	}
	if !strings.Contains(out, "aeon agent enable groot") {
		t.Fatalf("the next step is NAMED, and it is a decision the operator still has to take: %q", out)
	}
	// The behavioural half: it is still disabled.
	out, err = runAgent(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "groot") || !strings.Contains(out, "disabled") || strings.Contains(out, "enabled") {
		t.Fatalf("AP2 — import ran the check and STOPPED: %q", out)
	}
	// And the carried denial became a real row (the CLI is the authoring
	// door — S10's rule carried to plugin packages).
	s := openAgentStore(t)
	rows, err := s.PluginAgentTransitions("groot")
	if err != nil || len(rows) == 0 {
		t.Fatalf("the import landed a record and its (none)→disabled row: %v", err)
	}
	var n int
	for _, p := range mustPolicyList(t, s) {
		if p == "no-outbound-groot" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the carried denial is stored through the ONE authoring boundary: %v", mustPolicyList(t, s))
	}
}

func mustPolicyList(t *testing.T, s *store.Store) []string {
	t.Helper()
	rows, err := s.PolicyListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestListShowsStateProvenanceAndGrantCount(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "quiet", carriedFile("quiet"))
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	out, err := runAgent(t, "list", "--provenance")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "quiet") || !strings.Contains(out, "disabled") {
		t.Fatalf("name and state: %q", out)
	}
	if !strings.Contains(out, "imported from "+dir) {
		t.Fatalf("provenance names the source: %q", out)
	}
	if !strings.Contains(out, "first-party: no (registry-set)") {
		t.Fatalf("the provenance says who set the flag: %q", out)
	}
	// The carried count column (%-9d): the stored policy row renders as 1.
	if !strings.Contains(out, fmt.Sprintf("%-9d", 1)) {
		t.Fatalf("the carried count renders from the stored rows: %q", out)
	}
	// The empty state says so without alarm (AP11).
	agentHome(t)
	out, err = runAgent(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "normal state") || !strings.Contains(out, "nothing was fetched") {
		t.Fatalf("an empty registry reads as NORMAL, never as pressure to import: %q", out)
	}
}

func TestEnableDisableRevokeAndListAll(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "walker", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	if out, err := runAgent(t, "enable", "walker"); err != nil || !strings.Contains(out, "enabled") {
		t.Fatalf("enable: %v\n%s", err, out)
	}
	if out, err := runAgent(t, "disable", "walker"); err != nil || !strings.Contains(out, "disabled") {
		t.Fatalf("disable: %v\n%s", err, out)
	}
	if _, err := runAgent(t, "enable", "walker"); err != nil {
		t.Fatal(err)
	}
	if out, err := runAgent(t, "revoke", "walker", "--reason", "the vendor shipped a data-exfil update"); err != nil || !strings.Contains(out, "revoked") {
		t.Fatalf("revoke: %v\n%s", err, out)
	}
	// Hidden by default; LOUD with --all (the life stays readable, AP4).
	out, err := runAgent(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "walker") {
		t.Fatalf("revoked is out of the default listing (what is here now): %q", out)
	}
	out, err = runAgent(t, "list", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "walker") || !strings.Contains(out, "revoked") {
		t.Fatalf("--all includes revoked: %q", out)
	}
	// Terminal: enable after revoke is refused by name, not by silence.
	if _, err := runAgent(t, "enable", "walker"); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revoked is terminal — the refusal names it: %v", err)
	}
}

func TestRevokeRequiresReason(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "silent", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	_, err := runAgent(t, "revoke", "silent")
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("revoke without a reason fails fast and names the flag: %v", err)
	}
	// And the store agrees even if a caller skips the CLI's check.
	if _, err := runAgent(t, "revoke", "silent", "--reason", "   "); err == nil {
		t.Fatal("whitespace is not a reason")
	}
}

func TestEveryTransitionIsRecordedWithReason(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "lifecycle", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"enable", "lifecycle"}, {"disable", "lifecycle"}, {"enable", "lifecycle"}} {
		if _, err := runAgent(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if _, err := runAgent(t, "revoke", "lifecycle", "--reason", "incident 4471"); err != nil {
		t.Fatal(err)
	}
	s := openAgentStore(t)
	rows, err := s.PluginAgentTransitions("lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	want := [][3]string{{"", "disabled", ""}, {"disabled", "enabled", ""}, {"enabled", "disabled", ""}, {"disabled", "enabled", ""}, {"enabled", "revoked", "incident 4471"}}
	if len(rows) != len(want) {
		t.Fatalf("five moves, five rows: %+v", rows)
	}
	for i, w := range want {
		if rows[i].FromState != w[0] || rows[i].ToState != w[1] || rows[i].Reason != w[2] {
			t.Fatalf("transition %d: got %q→%q %q want %q→%q %q", i, rows[i].FromState, rows[i].ToState, rows[i].Reason, w[0], w[1], w[2])
		}
	}
}

func TestNothingFetches(t *testing.T) {
	// AP10 by grep AND by behaviour: no HTTP client in the CLI's plugin
	// surface nor in the plugin package; a URL is refused BEFORE any disk
	// is touched; and there is no update verb to fetch with.
	for _, f := range []string{"agent.go", "agent_services.go", "../../internal/plugin/import.go", "../../internal/plugin/state.go", "../../internal/plugin/revoke.go", "../../internal/plugin/provenance.go", "../../internal/plugin/consent.go", "../../internal/plugin/check.go"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(raw), "net/http") {
			t.Fatalf("%s imports net/http — nothing here fetches, ever (AP10)", f)
		}
	}
	agentHome(t)
	_, err := runAgent(t, "import", "https://evil.example/groot.tgz")
	if err == nil || !strings.Contains(err.Error(), "refused") || !strings.Contains(strings.ToLower(err.Error()), "fetch") {
		t.Fatalf("a URL is refused, naming the refusal: %v", err)
	}
	if _, err := runAgent(t, "import", "git+ssh://evil/repo"); err == nil {
		t.Fatal("no scheme reaches the disk")
	}
	_, err = runAgent(t, "update", "groot")
	if err == nil || !strings.Contains(err.Error(), "no update") {
		t.Fatalf("there is no update verb, and the refusal says so: %v", err)
	}
}

// TestNoPluginManagementOnNarrowSurface pins R6, NARROWED by S35 T4 — not
// dropped. import/install/uninstall/update still name nothing a socket
// caller can reach (each would widen authority: installing the thing that
// bypasses the guards). enable/disable/revoke moved OFF this list (brief
// §3's ruling: the imported agent already passed blocklist + consent at
// import, so moving it starts something already permitted, like
// loop.create starting a loop from an approved blueprint) — asserted
// POSITIVELY below, the same way the PL15 amendment states its whitelist
// positively rather than merely shrinking a negative one.
func TestNoPluginManagementOnNarrowSurface(t *testing.T) {
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}
	if !have[protocol.MethodAgentPlugins] {
		t.Fatal("agent.plugins must be registered — the app has one window onto the registry")
	}
	// R6 forbids a socket caller from INSTALLING a plugin: install/uninstall/
	// update are the install lifecycle, refused three slices running (S10
	// authoring, S12 MCP, S15 plugins) — a socket caller handed the means to
	// install the thing that bypasses the guards is refused. S43 T7 brought
	// agent.import onto the table as a CARD import (AM3, a (slug, version)
	// idempotent import), not a plugin install: import a card is the
	// management surface's whole job, so R6's install list is install/
	// uninstall/update, and agent.import is deliberately NOT in it.
	for _, banned := range []protocol.MethodName{"agent.install", "agent.uninstall", "agent.update"} {
		if have[banned] {
			t.Fatalf("%s is registered — R6: a socket caller handed the means to install a plugin is refused (S10 authoring, S12 MCP, S15 plugins — the rule has not aged for install)", banned)
		}
	}
	for _, m := range []protocol.MethodName{protocol.MethodAgentEnable, protocol.MethodAgentDisable, protocol.MethodAgentRevoke} {
		if !have[m] {
			t.Fatalf("%s must be registered — S35 T4's ruling moved it off R6's list: moving an already-consented agent starts nothing new, unlike import", m)
		}
	}
	// The read is read-only in the enumeration that documents it.
	var sawPlugins bool
	for _, m := range protocol.AgentServiceMethods() {
		if m.Name == protocol.MethodAgentPlugins {
			sawPlugins = true
			if !m.ReadOnly {
				t.Fatal("agent.plugins must be ReadOnly — it is the window, not the door")
			}
		}
	}
	if !sawPlugins {
		t.Fatal("the enumeration must carry the plugins read (N12: every entry states what it cannot do)")
	}
	// Dispatch proves the absence from the caller's side: method-not-found.
	// agent.install stays the example — it is the plugin install R6 refuses,
	// and it is NOT registered (agent.import is, now a card import; agent.enable
	// is a real method too).
	_, err := tbl.Dispatch(context.Background(), "c9", protocol.MethodName("agent.install"), nil)
	var ue protocol.ErrUnknownMethod
	if !errors.As(err, &ue) {
		t.Fatalf("agent.install is not a method — the narrow surface cannot even name it: %v", err)
	}
	// S19's specced projection agrees, by text — import still banned;
	// enable/disable/revoke were never named there to begin with (checked
	// this session), so there is nothing stale to narrow in that document.
	raw, err := os.ReadFile("../../docs/aeon/S19-narrow-surface/brief.md")
	if err != nil {
		t.Fatalf("S19's brief is the contract this grep reads: %v", err)
	}
	for _, banned := range []string{"agent.import"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("S19's brief names %s as a surface action — R6 says the narrow surface never manages plugin installation", banned)
		}
	}
}

// TestReimportConsentReportsThePutBack walks §9's second promise: a revoked
// life that comes back must be VISIBLE in the consent, not inferred from a
// timestamp ("somebody put this back" is the fact an operator deserves).
// T9's walk found this line missing and the DEMO still printed it — the
// finding was fixed here rather than edited away.
func TestReimportConsentReportsThePutBack(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "putback", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := runAgent(t, "enable", "putback"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAgent(t, "revoke", "putback", "--reason", "the vendor shipped a data-exfil update"); err != nil {
		t.Fatal(err)
	}
	out, err := runAgent(t, "import", dir)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "revoked") || !strings.Contains(low, "data-exfil") || !strings.Contains(low, "new record") {
		t.Fatalf("the consent must say this is a re-import of a REVOKED life with its reason — AP4's visibility: %s", out)
	}
	// And the re-import is a new life: disabled on arrival (AP2), policies
	// untouched (Q2) — the consent still lists what already applies.
	if !strings.Contains(out, "disabled") {
		t.Fatalf("re-import arrives disabled, always: %s", out)
	}
}

// TestListAllShowsRevokedReason is §9's first promise: `--all` answers
// "what did we once run, and why did we stop" from the rows alone.
func TestListAllShowsRevokedReason(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "whydidwestop", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := runAgent(t, "enable", "whydidwestop"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAgent(t, "revoke", "whydidwestop", "--reason", "exfiltration via the docs tool"); err != nil {
		t.Fatal(err)
	}
	out, err := runAgent(t, "list", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "whydidwestop") || !strings.Contains(out, "revoked") || !strings.Contains(out, "exfiltration via the docs tool") {
		t.Fatalf("`list --all` carries the state AND the recorded reason (AP4 — the record survives, and so does its why): %s", out)
	}
}

// --- S34 T1: agent.list, the socket's roster read ---------------------------------

// TestAgentListMatchesCLIRoster proves the socket door and the CLI door read
// the same facts for the same store (TB10's comparison mandate): every
// imported row `aeon agent list --all` prints also appears in agent.list's
// result, by name/provenance/state/source, and vice versa.
func TestAgentListMatchesCLIRoster(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "roster-check", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	s := openAgentStore(t)
	result, refusal, err := agentRosterHandleFor(s)(context.Background(), nil)
	if err != nil || refusal != "" {
		t.Fatalf("agentRosterHandleFor: err=%v refusal=%q", err, refusal)
	}
	out := result.(agentListResult)

	rows, err := s.PluginAgentsAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the fixture imported an agent; the CLI's own source has nothing to compare against")
	}
	byName := map[string]agentRosterEntry{}
	for _, e := range out.Agents {
		byName[e.Name] = e
	}
	for _, r := range rows {
		e, ok := byName[r.Name]
		if !ok {
			t.Fatalf("agent.list is missing %q, which the CLI's own PluginAgentsAll read returns", r.Name)
		}
		if e.Provenance != "imported" || e.State != r.State || e.Source != r.Source || e.ImportedAt != r.ImportedAt {
			t.Fatalf("agent.list disagrees with the CLI's source for %q: got %+v, want state=%s source=%s importedAt=%s",
				r.Name, e, r.State, r.Source, r.ImportedAt)
		}
	}
	// And the first-party side: every compiled body the CLI counts into
	// "first-party: N compiled bodies" also appears in the roster as running.
	fpCount := 0
	for _, a := range plugin.Agents() {
		if !a.FirstParty {
			continue
		}
		fpCount++
		e, ok := byName[a.Name]
		if !ok || e.Provenance != "first_party" || e.State != "running" {
			t.Fatalf("agent.list disagrees with plugin.Agents() for first-party %q: got %+v", a.Name, e)
		}
	}
	if fpCount == 0 {
		t.Fatal("no first-party bodies registered; the comparison proves nothing")
	}
}

// TestAgentListStateVocabularyIsReal pins the vocabulary this task measured
// — never design.md's pre-code guess of "blocked"/"never_arrived".
func TestAgentListStateVocabularyIsReal(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "vocab-check", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatal(err)
	}
	s := openAgentStore(t)
	result, _, err := agentRosterHandleFor(s)(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	real := map[string]bool{"running": true, "disabled": true, "enabled": true, "revoked": true}
	for _, e := range result.(agentListResult).Agents {
		if !real[e.State] {
			t.Fatalf("%q carries state %q, outside the real vocabulary {running, disabled, enabled, revoked} — "+
				"\"blocked\"/\"never_arrived\" do not exist in plugin.Agents()/PluginAgentsAll", e.Name, e.State)
		}
	}
}

// TestAgentListRejectsParams mirrors agent.plugins' own SEC-T6-005 check —
// the roster is the whole surface or nothing (R6).
func TestAgentListRejectsParams(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	_, refusal, err := agentRosterHandleFor(s)(context.Background(), json.RawMessage(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(refusal, "takes no params") {
		t.Fatalf("a non-empty params object must refuse, naming why: refusal=%q", refusal)
	}
}

// --- S34 T2: tools.grants, the authority view per agent ----------------------------

// mcpHomeNoConfig points AEON_MCP_CONFIG at a path that does not exist —
// deterministic MC11 absence, independent of whatever the test machine's
// real XDG config happens to hold.
func mcpHomeNoConfig(t *testing.T) {
	t.Helper()
	t.Setenv(paths.MCPConfigEnv, filepath.Join(t.TempDir(), "does-not-exist.json"))
}

// aFirstPartyAgent names one real compiled body — the roster is this
// checkout's own agent set (handler.RegisteredBodies' embedded set, not a
// fixed literal like "default"), so a test picks one rather than assumes a
// name.
func aFirstPartyAgent(t *testing.T) string {
	t.Helper()
	for _, a := range plugin.Agents() {
		if a.FirstParty {
			return a.Name
		}
	}
	t.Fatal("no first-party body is registered; the comparison proves nothing")
	return ""
}

// TestToolsGrantsCoreOnlyWithNoMCPConfig proves MC11: an agent with no MCP
// config file present still gets its core-tool grants, never a refusal.
func TestToolsGrantsCoreOnlyWithNoMCPConfig(t *testing.T) {
	agentHome(t)
	mcpHomeNoConfig(t)
	s := openAgentStore(t)
	result, refusal, err := toolGrantsHandleFor(s)(context.Background(), mustAgentParams(t, aFirstPartyAgent(t)))
	if err != nil || refusal != "" {
		t.Fatalf("an absent MCP config must not refuse the method: err=%v refusal=%q", err, refusal)
	}
	out := result.(toolsGrantsResult)
	if len(out.Grants) == 0 {
		t.Fatal("no MCP config still leaves the core tier; an empty grants list is MC11's shape told wrong")
	}
	for _, g := range out.Grants {
		if g.Tier != "core" {
			t.Fatalf("with no MCP config nothing but core can appear, got %+v", g)
		}
		if g.State != "available" {
			t.Fatalf("core tools are always available (S12 does not re-decide core): %+v", g)
		}
	}
}

// TestToolsGrantsUnknownAgentNamesTheRoster proves the error names the
// valid roster rather than answering an empty-everything 200 (TB2).
func TestToolsGrantsUnknownAgentNamesTheRoster(t *testing.T) {
	agentHome(t)
	mcpHomeNoConfig(t)
	s := openAgentStore(t)
	known := aFirstPartyAgent(t)
	_, refusal, err := toolGrantsHandleFor(s)(context.Background(), mustAgentParams(t, "nobody-by-this-name"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(refusal, "unknown agent") || !strings.Contains(refusal, "nobody-by-this-name") {
		t.Fatalf("the refusal names the unknown agent: %q", refusal)
	}
	if !strings.Contains(refusal, known) {
		t.Fatalf("the refusal lists the known roster (at least %q): %q", known, refusal)
	}
}

// TestToolsGrantsStateVocabularyIsGrantState pins the reality finding in
// tasks/T2.md: four GrantState values, never a brief-style bucket name.
func TestToolsGrantsStateVocabularyIsGrantState(t *testing.T) {
	agentHome(t)
	mcpHomeNoConfig(t)
	s := openAgentStore(t)
	result, refusal, err := toolGrantsHandleFor(s)(context.Background(), mustAgentParams(t, aFirstPartyAgent(t)))
	if err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	real := map[string]bool{"available": true, "unavailable": true, "refused": true, "ask": true}
	for _, g := range result.(toolsGrantsResult).Grants {
		if !real[string(g.State)] {
			t.Fatalf("state %q is outside mcp.GrantState's real vocabulary — the brief's imagined "+
				"declared/granted/registered_ungranted/unregistered buckets were replaced by this task's own reality finding", g.State)
		}
	}
}

// TestToolsGrantsReturnsGrantViewVerbatim proves the handler ships
// Registry.GrantView's own answer unchanged — no second join.
func TestToolsGrantsReturnsGrantViewVerbatim(t *testing.T) {
	agentHome(t)
	mcpHomeNoConfig(t)
	s := openAgentStore(t)
	agent := aFirstPartyAgent(t)
	result, refusal, err := toolGrantsHandleFor(s)(context.Background(), mustAgentParams(t, agent))
	if err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	out := result.(toolsGrantsResult)

	reg, err := mcp.New(mcp.Config{})
	if err != nil {
		t.Fatal(err)
	}
	existing, rj := policy.NewStore(policyRepo(s)).List(context.Background(), time.Now().UTC())
	if rj != nil {
		t.Fatal(rj.Reason)
	}
	// The clock moves between the two calls, and GrantView's core branch
	// carries no timestamp in its Reason — equal length and equal
	// Qualified/Tier/State is the comparison that is actually stable.
	direct := reg.GrantView(agent, existing, time.Now().UTC())
	if len(out.Grants) != len(direct) {
		t.Fatalf("got %d grants, a direct GrantView call returns %d", len(out.Grants), len(direct))
	}
	for i := range direct {
		if out.Grants[i].Qualified != direct[i].Qualified || out.Grants[i].Tier != direct[i].Tier || out.Grants[i].State != direct[i].State {
			t.Fatalf("grant %d disagrees with a direct GrantView call: got %+v, want %+v", i, out.Grants[i], direct[i])
		}
	}
}

func mustAgentParams(t *testing.T, agent string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(struct {
		Agent string `json:"agent"`
	}{Agent: agent})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
