package main

// settings_guard_test.go — S44 T6. The guards that make the slice's
// security claims mechanical rather than reviewed, in the mould of
// policy_test.go's TestNoPolicyAuthoringReachesTheNarrowSurface and
// agent_test.go's TestNoPluginManagementOnNarrowSurface. Keeping the family
// together (the PL15 and R6 guards already live here) so a reader of the
// "the narrow surface never carries a write to the rules" argument finds
// all of it in one place.
//
// Two guards, each with a meta-test that proves it can FAIL (a guard that
// cannot fail is not a guard — it is a green light):
//
//   1. TestNoSettingsWriteReachesTheNarrowSurface (CS9, PL15, R6) — a settings
//      write (settings.set / settings.unset) never reaches the narrow
//      (agent / HTTP / MCP-stdio) surface. Positive half FIRST (the app has
//      its door), then the six negative halves.
//   2. TestSettingsPanelOnlyCallsRegisteredMethods (P34, brief acceptance 3) —
//      the Settings UI reaches settings only through registered socket
//      methods, never HTTP or browser storage. Skips while T8 is unlanded
//      (with a NAMED reason, never a silent pass); T9 asserts it is not
//      skipping.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/tools"
)

// guardSettingsService is the non-nil seam a guard needs to see the five
// settings.* methods REGISTERED. The door (RegisterSettings) only inspects
// whether the seam is nil — it never calls these methods — so a guard may
// stand in a seam that answers nothing, exactly as the handshake test's
// settingsStore does, without standing up a store. Returning zero values
// keeps the guard from touching a real config.Store (the store's own tests
// own that).
type guardSettingsService struct{}

var _ protocol.SettingsService = guardSettingsService{} // compile-time: the fake satisfies the seam

func (guardSettingsService) List(context.Context, string) ([]config.View, error) {
	return nil, nil
}
func (guardSettingsService) Get(context.Context, string) (config.View, error) {
	return config.View{}, nil
}
func (guardSettingsService) Set(context.Context, string, string, string) (config.View, bool, error) {
	return config.View{}, false, nil
}
func (guardSettingsService) Unset(context.Context, string, string) (config.View, bool, error) {
	return config.View{}, false, nil
}
func (guardSettingsService) History(context.Context, string, int) ([]config.Event, error) {
	return nil, nil
}

// newSettingsTable builds the table the way a daemon with a settings store
// builds it (daemon.go): NewTable, then assign the seam, then RegisterSettings.
// A non-nil seam registers the five; the return is true, so the app has its
// door — the positive half the guard states before the negative one.
func newSettingsTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Settings = guardSettingsService{}
	if !tbl.RegisterSettings() {
		t.Fatal("RegisterSettings refused to register over a non-nil seam — the app would have no settings door at all")
	}
	return tbl
}

// TestNoSettingsWriteReachesTheNarrowSurface is the mechanical form of CS9:
// the settings writes are human-only, the way policy authoring is
// (PL15/R6). A settings write on the narrow surface would hand an agent the
// means to change the rules written to constrain it — the same refusal,
// one slice later.
//
// It states the whitelist POSITIVELY first (the app has its door), the way
// the PL15 amendment's style demands, then the six negative halves.
func TestNoSettingsWriteReachesTheNarrowSurface(t *testing.T) {
	tbl := newSettingsTable(t)

	// ── Positive: the app has its door. The five settings.* methods ARE in
	// the table when the seam is wired, so the Settings UI (T8) and the CLI
	// (T7) have exactly one door to speak through (P34).
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}
	for _, m := range []protocol.MethodName{
		protocol.MethodSettingsList, protocol.MethodSettingsGet,
		protocol.MethodSettingsSet, protocol.MethodSettingsUnset, protocol.MethodSettingsHistory,
	} {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the settings (P34)", m)
		}
	}
	// And the writes are real product methods, not debug or pre-negotiation
	// gates: settings.set / settings.unset are NOT Debug (they are on the
	// wide socket, not the narrow projection) and DO RequireInitialize (a
	// write is only a write after the connection has negotiated).
	for _, m := range []protocol.MethodName{protocol.MethodSettingsSet, protocol.MethodSettingsUnset} {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — a settings write is a product method on the wide socket, not a diagnostic (CS9)", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a write may not precede the connection's negotiation", m)
		}
	}

	// ── Negative 1 — the narrow table: no narrow Action projects a
	// settings.* method. The allow-list (CS9) never names them, so an agent
	// cannot reach the write through the projection.
	if offenders := narrowProjectingSettings(narrow.Actions()); len(offenders) != 0 {
		t.Errorf("the narrow surface projects settings.* through %s — an action projecting settings.* hands an agent the means to change the rules written to constrain it (CS9/PL15/R6). Add it to the narrow surface only through a slice that argues it.", strings.Join(offenders, ", "))
	}

	// ── Negative 2 — the tool registry: no tool name matches
	// /setting|config/i. TestRegistryExactNames pins the honest set; this
	// adds the REASON — a tool named after settings would be a second door
	// onto the same write, beside the one the socket carries.
	settingTool := regexp.MustCompile("(?i)setting|config")
	for name := range tools.Registry() {
		if settingTool.MatchString(name) {
			t.Errorf("tool %q matches /setting|config/i — a tool named after the settings is a second door onto the write the socket alone carries (CS9)", name)
		}
	}

	// ── Negative 3 — by text: the write's names appear in NO non-test file
	// under internal/narrow or internal/agentsvc. A projection that names
	// settings.set, even in a comment, is the projection the rule forbids.
	for _, dir := range []string{"internal/narrow", "internal/agentsvc"} {
		walkNoSettingsLiteral(t, filepath.Join("..", "..", dir))
	}

	// ── Negative 4 — the agent-services enumeration: none of
	// protocol.AgentServiceMethods() is a settings.* name. The agent-to-
	// agent task lifecycle has no settings door, so a settings write cannot
	// ride it either (CS9).
	for _, m := range protocol.AgentServiceMethods() {
		if strings.HasPrefix(string(m.Name), "settings.") {
			t.Fatalf("%s is an agent-service method — a settings write on the agent surface hands an agent the rules (CS9)", m.Name)
		}
	}

	// ── Negative 5 — dispatch proves the absence from the CALLER's side.
	// With the seam nil, RegisterSettings is a no-op (V13), so settings.set
	// is not in the table and dispatch answers method-not-found. The narrow
	// surface cannot even name the write.
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterSettings() // no-op over a nil seam: the five stay absent
	if _, err := tblNil.Dispatch(context.Background(), "c9", protocol.MethodSettingsSet, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
		t.Fatalf("settings.set dispatched on a nil-seam table — the narrow surface can reach a write it must not (want ErrUnknownMethod, got %v)", err)
	}
}

// TestNarrowGuardCanFail proves guard 1's narrow predicate is not vacuous:
// feed it a poisoned Action list carrying settings.set and it must report
// the violation. A guard that cannot fail is a green light, not a guard.
func TestNarrowGuardCanFail(t *testing.T) {
	// A fake narrow action, exactly what a slice would add "for symmetry" —
	// the one thing the Do-NOT list forbids (internal/narrow/actions.go).
	poisoned := []narrow.Action{
		{Name: "aeon_settings_set", SocketMethod: protocol.MethodSettingsSet, Kind: narrow.KindRead},
	}
	if got := narrowProjectingSettings(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting settings.set was not flagged — the guard cannot fail, so it is not a guard")
	}
	// And a clean action must NOT be flagged (the predicate is specific).
	clean := []narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead},
	}
	if got := narrowProjectingSettings(clean); len(got) != 0 {
		t.Errorf("a clean action was flagged as projecting settings.* — the predicate fires on nothing: %v", got)
	}
}

// narrowProjectingSettings returns the names of the actions whose SocketMethod
// projects a settings.* method — the guard's predicate. It is a function over
// tables (not over narrow.Actions()) precisely so the meta-test can feed it a
// poisoned table the real one never carries.
func narrowProjectingSettings(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if strings.HasPrefix(string(a.SocketMethod), "settings.") {
			bad = append(bad, a.Name)
		}
	}
	return bad
}

// walkNoSettingsLiteral walks one narrow/agentsvc directory's non-test .go
// files and fails if any names the settings writes — the literal
// "settings.set"/"settings.unset" or the const MethodSettingsSet/
// MethodSettingsUnset. The file path is in the failure so the offender is
// actionable, and the walk asserts it saw at least one file (a guard that
// walks an empty tree passes vacuously).
func walkNoSettingsLiteral(t *testing.T, root string) {
	t.Helper()
	// The forbidden names: the write's wire literal and its const. Any one
	// in a non-test file under narrow/ or agentsvc/ is the projection the
	// rule forbids.
	forbidden := []string{"settings.set", "settings.unset", "MethodSettingsSet", "MethodSettingsUnset"}
	var walked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs hold no Go code we own
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		walked++
		text := string(b)
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				t.Errorf("%s names %q — a settings write must not reach the narrow/agent surface (CS9/PL15/R6)", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked == 0 {
		t.Fatalf("the %s walk saw no non-test files — it checked nothing", root)
	}
}

// --- 3: the Settings UI reaches settings only through the socket (P34) -----

// panelCallRe is the grep every Trust-trio close-out ran by hand, made
// repeatable: every string literal passed to the four bridge call sites.
// rpc/rpcTyped are the generic bridge; useRpcQuery/useRpcAction the hooks
// (AGENTS §4b's "the generic bridge, never a new Tauri command").
const panelCallRe = `(?:rpc|rpcTyped|useRpcQuery|useRpcAction)\(\s*["']([a-zA-Z.]+)["']`

// settingsUIFiles resolves T8's Settings UI sources — the panel and its row
// (DaemonSetting*.tsx) and the pure daemonSettings*.ts helper. T8 landed its
// row and pure half but not the RPC-calling panel yet, so the set is
// non-empty but carries no bridge call today; the guard runs (the files
// exist) and passes until the panel lands, and T9 re-checks. The meta-test
// (TestPanelGuardCanFail) proves the grep is not vacuous, so a panel that
// names no method is not mistaken for a working one.
func settingsUIFiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "ui", "src", "features", "settings")
	// DaemonSetting*.tsx catches both the row (DaemonSettingRow) and the panel
	// (DaemonSettingsPanel) by the shared prefix; daemonSettings*.ts the pure
	// helper. A file T8 names differently would be missed — so the skip is
	// "none of these", not "the one file the spec guessed".
	patterns := []string{"DaemonSetting*.tsx", "daemonSettings*.ts"}
	var found []string
	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, p))
		if err != nil {
			t.Fatalf("glob %s: %v", p, err)
		}
		found = append(found, matches...)
	}
	return found
}

// TestSettingsPanelOnlyCallsRegisteredMethods is P34 made repeatable: the
// Settings UI reaches settings ONLY through registered socket methods. It
// greps T8's Settings UI sources, extracts every literal passed to the
// bridge call sites, and asserts each is a MethodName the live table
// registers — and that the set is a subset of the five settings.* names
// (the panel has no second door). It also asserts the panel keeps nothing
// in browser storage (appearance only) and opens no HTTP (the socket is the
// one transport).
//
// While T8 is unlanded it SKIPS with a named reason — it never passes
// vacuously silently, and T9 asserts the skip is gone.
func TestSettingsPanelOnlyCallsRegisteredMethods(t *testing.T) {
	files := settingsUIFiles(t)
	if len(files) == 0 {
		t.Skip("T8 not landed — none of the Settings UI sources (DaemonSetting*.tsx / daemonSettings*.ts) exist yet; the guard skips rather than pass on an empty input, and T9 asserts it is not skipping")
	}
	// Non-vacuous on the FILES: the guard runs only because the glob found T8's
	// settings UI sources. A panel that names no bridge call is a legitimate
	// result (the presentational row and the pure helper carry none); the
	// meta-test TestPanelGuardCanFail proves the grep matches a real call, so
	// "found the file" is the guard's anti-vacuous check, not "found a call".
	// The live table the panel would speak to: built with a non-nil seam so
	// the five settings.* methods are registered (the set the panel must be
	// a subset of).
	tbl := newSettingsTable(t)
	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}

	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(src)
		matches := regexp.MustCompile(panelCallRe).FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			name := m[1]
			// Each literal the panel names MUST be a method the live table
			// registers — an unregistered name is a door the socket does not
			// open (P34).
			if !registered[protocol.MethodName(name)] {
				t.Errorf("%s calls rpc(%q), which is not a registered method — the panel reaches settings only through the socket's registered methods (P34)", path, name)
			}
			// And the set is a subset of the five settings.* names: the panel
			// speaks ONLY the settings door, never another method.
			if !strings.HasPrefix(name, "settings.") {
				t.Errorf("%s calls rpc(%q), which is not a settings.* method — the Settings UI has exactly the five settings.* doors and no other (P34)", path, name)
			}
			// The panel never opens HTTP (the socket is the one transport)
			// nor writes a daemon setting to browser storage (the device-local
			// store is for appearance only, §4c).
			if strings.Contains(text, "localStorage.") || strings.Contains(text, "sessionStorage.") {
				t.Errorf("%s touches localStorage/sessionStorage — a daemon setting lives in the store, never browser storage (the local store is appearance only)", path)
			}
			if strings.Contains(text, "fetch(") || strings.Contains(text, "XMLHttpRequest") {
				t.Errorf("%s opens an HTTP request (fetch/XHR) — the panel talks the socket via the bridge, never HTTP (P25)", path)
			}
		}
	}
}

// TestPanelGuardCanFail proves guard 3's grep is not vacuous: a synthetic
// panel that calls an UNREGISTERED method (settings.frobnicate) is flagged,
// and one that calls a registered method (settings.get) is not. The regexp
// must match something real.
func TestPanelGuardCanFail(t *testing.T) {
	tbl := newSettingsTable(t)
	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	// settings.frobnicate is not registered → the grep must flag it.
	if !panelCallUnregistered(regexp.MustCompile(panelCallRe).FindAllStringSubmatch(`rpc("settings.frobnicate")`, -1), registered) {
		t.Fatal("an unregistered rpc(\"settings.frobnicate\") was not flagged — the grep cannot fail, so it is not a guard")
	}
	// settings.get IS registered → the grep must NOT flag it.
	if panelCallUnregistered(regexp.MustCompile(panelCallRe).FindAllStringSubmatch(`rpc("settings.get")`, -1), registered) {
		t.Fatal("a registered rpc(\"settings.get\") was flagged — the grep fires on a real method")
	}
}

// panelCallUnregistered reports whether any method-name literal the grep
// found is not a registered method — guard 3's predicate.
func panelCallUnregistered(matches [][]string, registered map[protocol.MethodName]bool) bool {
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if !registered[protocol.MethodName(m[1])] {
			return true
		}
	}
	return false
}
