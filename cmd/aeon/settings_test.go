package main

// settings_test.go — S44 T7. The CLI is a pure client of the socket's five
// settings.* methods (P34); what these tests pin is that this secondary client
// adds NO second truth: the printers speak the wire's layers verbatim (the
// table is the contract), --yes is a seatbelt over data the daemon already
// sent (not a security control, CS8/CS9), a value is sent as typed (no
// coercion), --json is the wire shape with nulls kept, and the CLI opens no
// store, config or SQL of its own.
//
// # A note on the test lane (S44 T7, 2026-10-30)
//
// T5 (the protocol door) is not built in this worktree yet, and a concurrent
// edit left internal/protocol mid-compile (method.go names SettingsService,
// which the door will define). The live-daemon helper (cli_integration_test's
// startDaemon) would need the five settings.* methods registered — which T5
// provides — so per T7's own contingency these tests assert against the
// printer and gate functions with CANNED wire structs, not a running daemon.
// The functions are the ones the CLI itself calls, so a green run here is a
// green run of the CLI's behaviour; only the socket is stood in.

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// --- helpers: canned wire structs, the same shapes the printers read ---

// sp builds a *string, or nil, so a test can pin a null-meaningful field.
func sp(s string) *string { return &s }

// aDefaultView is a declared key at its default: no row, no env, no shadow.
func aDefaultView(key string) settingsView {
	return settingsView{Key: key, Type: "int", Default: "3", Value: "3", Source: "default"}
}

// --- 1 & 2: the dispatch names the verbs, in both failure shapes ---

func TestSettingsNoVerbNamesTheVerbs(t *testing.T) {
	err := cmdSettings(io.Discard, nil)
	if err == nil {
		t.Fatal("no verb returned nil — it must name the verbs and point at help")
	}
	for _, v := range []string{"list", "get", "set", "unset", "history"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("no-verb error %q names %s", err.Error(), "list, get, set, unset, history — but not "+v)
		}
	}
}

func TestSettingsUnknownVerbNamesTheVerbs(t *testing.T) {
	err := cmdSettings(io.Discard, []string{"frob"})
	if err == nil {
		t.Fatal("an unknown verb returned nil — it must name the real verbs")
	}
	for _, v := range []string{"list", "get", "set", "unset", "history"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("unknown-verb error %q does not name the real verbs, missing %q", err.Error(), v)
		}
	}
}

// --- 3 & 4: the list printer ---

func TestSettingsListPrintsSourceAndSensitiveMarker(t *testing.T) {
	var b strings.Builder
	rows := []settingsView{
		{Key: "a.one", Type: "int", Value: "3", Source: "default"},
		{Key: "a.two", Type: "bool", Value: "true", Source: "database", UpdatedBy: "user:1", UpdatedAt: "2026-01-01T00:00:00Z"},
		{Key: "a.three", Type: "string", Value: "x", Source: "default", Sensitive: true, Description: "a 40+ char description of what the default protects here"},
	}
	renderList(&b, rows)
	out := b.String()
	// Each row shows key, value and the source word.
	for _, want := range []string{
		"a.one", "3", "default",
		"a.two", "true", "database",
		"a.three", "x", "default",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output %q missing %q", out, want)
		}
	}
	// The sensitive marker appears on the sensitive row and nowhere else.
	if !strings.Contains(out, "[sensitive]") {
		t.Errorf("list output %q did not mark the sensitive row", out)
	}
	// Only ONE row is sensitive, so the marker appears once.
	if n := strings.Count(out, "[sensitive]"); n != 1 {
		t.Errorf("list output marked %d rows sensitive, want 1: %q", n, out)
	}
}

func TestSettingsListEmptyIsHonest(t *testing.T) {
	var b strings.Builder
	renderList(&b, nil) // a nil slice is the empty state
	out := b.String()
	if !strings.Contains(out, "no settings are declared yet") {
		t.Errorf("an empty list printed %q — the honest empty state must be said, not silent", out)
	}
}

// --- 5: the get printer renders the layers it must never hide ---

func TestSettingsGetPrintsEnvShadowAndWarnings(t *testing.T) {
	// A: the env in force, with a saved value shadowed beneath it.
	var a strings.Builder
	renderGet(&a, settingsView{
		Key: "test.flag", Type: "bool", Value: "true", Source: "environment",
		Stored: sp("false"),
	})
	got := a.String()
	if !strings.Contains(got, "value  true   (environment") {
		t.Errorf("env source line wrong: %q", got)
	}
	if !strings.Contains(got, "AEON_SETTING_TEST_FLAG is set; a saved value, if any, is shadowed") {
		t.Errorf("env shadow line wrong: %q", got)
	}
	if !strings.Contains(got, "saved  false  (shadowed)") {
		t.Errorf("the saved-value line under an env shadow is missing: %q", got)
	}

	// B: a bad env value the daemon ignored.
	var c strings.Builder
	renderGet(&c, settingsView{
		Key: "test.flag", Type: "bool", Value: "true", Source: "default",
		EnvInvalid: sp("AEON_SETTING_TEST_FLAG"),
	})
	if !strings.Contains(c.String(), "warning: AEON_SETTING_TEST_FLAG is set but not valid for this setting and was ignored") {
		t.Errorf("the env-invalid warning is missing: %q", c.String())
	}

	// C: a stored value that is no longer valid; the default is in force.
	var d strings.Builder
	renderGet(&d, settingsView{
		Key: "test.flag", Type: "bool", Value: "true", Source: "default",
		Stored: sp("false"), StoredInvalid: true,
	})
	if !strings.Contains(d.String(), `warning: the saved value "false" is no longer valid; the default is in force`) {
		t.Errorf("the stored-invalid warning is missing: %q", d.String())
	}
}

// --- 6: the value is sent verbatim — no coercion, no lower-casing ---

func TestSettingsSetSendsCanonicalValueVerbatim(t *testing.T) {
	// The param the CLI puts on the wire is the person's value, as typed.
	raw, err := json.Marshal(settingsSetParams{Key: "test.flag", Value: "yes"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"key":"test.flag","value":"yes"}` {
		t.Errorf("the set param %q is not the verbatim value — a coerced spelling would make the CLI a second authority", string(raw))
	}
	// And an uppercase enum member is not lower-cased on the way out.
	raw2, _ := json.Marshal(settingsSetParams{Key: "mode", Value: "Auto"})
	if string(raw2) != `{"key":"mode","value":"Auto"}` {
		t.Errorf("the set param %q lower-cased or altered the value", string(raw2))
	}
}

// --- 7, 8, 9: the --yes gate is a seatbelt over data the daemon sent ---

func TestSettingsSetSensitiveWithoutYesDoesNotCall(t *testing.T) {
	var b strings.Builder
	// A sensitive current value, no --yes: the gate must withhold.
	err := settingsSetGate(&b, settingsView{Key: "db.password", Sensitive: true, Value: "old", Description: "the credential the default protects from exposure"}, false, "new")
	if err == nil {
		t.Fatal("a sensitive key without --yes was allowed to proceed — the gate must withhold and exit non-zero")
	}
	out := b.String()
	for _, want := range []string{"db.password", "what it controls", "the credential the default protects", "old", "new", "re-run with --yes to apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("withheld gate output %q missing %q", out, want)
		}
	}
}

func TestSettingsSetSensitiveWithYesCalls(t *testing.T) {
	var b strings.Builder
	// A sensitive current value, --yes given: the gate proceeds (one write).
	err := settingsSetGate(&b, settingsView{Key: "db.password", Sensitive: true, Value: "old"}, true, "new")
	if err != nil {
		t.Errorf("a confirmed sensitive key was refused: %v", err)
	}
	if b.Len() != 0 {
		t.Errorf("a confirmed gate printed %q — a confirmed write carries no prompt", b.String())
	}
}

func TestSettingsSetNonSensitiveNeverNeedsYes(t *testing.T) {
	var b strings.Builder
	// An ordinary key, no --yes: it never prompts.
	err := settingsSetGate(&b, settingsView{Key: "a.b", Value: "old"}, false, "new")
	if err != nil {
		t.Errorf("an ordinary key was refused without --yes: %v", err)
	}
	if b.Len() != 0 {
		t.Errorf("an ordinary key prompted: %q — only a sensitive key may", b.String())
	}
}

// --- 10 & 11: a no-op is "unchanged", never "updated" ---

func TestSettingsSetUnchangedPrintsUnchanged(t *testing.T) {
	var b strings.Builder
	renderWrite(&b, settingsView{Key: "a.b", Value: "3", Stored: sp("3")}, false)
	out := b.String()
	if !strings.Contains(out, "unchanged") {
		t.Errorf("an unchanged write printed %q — it must say unchanged", out)
	}
	if strings.Contains(out, "updated") {
		t.Errorf("an unchanged write said \"updated\": %q — a no-op is not a success", out)
	}
}

func TestSettingsUnsetAbsentIsUnchanged(t *testing.T) {
	var b strings.Builder
	// An idempotent unset: nothing was stored, so the write is a no-op.
	renderWrite(&b, settingsView{Key: "a.b", Value: "3", Source: "default"}, false)
	out := b.String()
	if !strings.Contains(out, "unchanged") {
		t.Errorf("an absent unset printed %q — an idempotent delete is a no-op, not a failure", out)
	}
}

// --- 12: an unknown-key refusal prints the daemon's three parts, nearest in ---

func TestSettingsErrorPrintsThreePartsAndNearest(t *testing.T) {
	// The daemon's ErrSettingUnknown is a *protocol.Error; the CLI returns it
	// and renderError renders its three parts. A degraded bare string would
	// drop the nearest-key line, so the render must keep it.
	var b strings.Builder
	renderError(&b, &protocol.Error{
		Message: `setting "agents.allowUnsignedImport" is unknown`,
		Code:    -31810,
		Data: protocol.Data{
			What:  `no declared setting "agents.allowUnsignedImport"`,
			Means: "the closed registry does not name it",
			Todo:  "nearest: agents.allowUnsignedImport, agents.maxConcurrent",
		},
	})
	out := b.String()
	for _, want := range []string{
		`setting "agents.allowUnsignedImport" is unknown`,
		"what",
		"means",
		"todo",
		"nearest:",
		"agents.allowUnsignedImport, agents.maxConcurrent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the unknown-key render %q dropped %q — the refusal must keep the nearest-key suggestion", out, want)
		}
	}
}

// --- 13: --json is the wire shape, with nulls kept, not dropped ---

func TestSettingsJSONIsTheWireVerbatim(t *testing.T) {
	// A get result with a null-meaningful stored and a null env-invalid: the
	// --json output must keep them null, never omit them (a client must tell
	// "no row" from "field omitted").
	res := settingsGetResult{Setting: settingsView{
		Key: "a.b", Type: "int", Value: "3", Source: "default",
		Stored:     nil, // no row
		EnvInvalid: nil, // the env is valid
		UpdatedBy:  "",
		UpdatedAt:  "",
	}}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, `"stored":null`) {
		t.Errorf("--json dropped the null stored: %q — a client cannot tell \"no row\" from \"omitted\"", out)
	}
	if !strings.Contains(out, `"envInvalid":null`) {
		t.Errorf("--json dropped the null envInvalid: %q", out)
	}
	// And it round-trips to the same shape (compact-equal, the wire contract).
	var back settingsGetResult
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(res, back) {
		t.Errorf("the --json output did not round-trip: got %v, want %v", back, res)
	}
}

// --- 14: the audit renders newest first, old→new, with the before-state ---

func TestSettingsHistoryPrintsNewestFirstWithOldAndNew(t *testing.T) {
	var b strings.Builder
	events := []settingEventView{
		// newest first (the daemon's order): the second write is on top.
		{Key: "a.b", Op: "set", OldValue: sp("3"), NewValue: sp("5"), Actor: "user:1", OccurredAt: "2026-01-02T00:00:00Z"},
		{Key: "a.b", Op: "set", OldValue: nil, NewValue: sp("3"), Actor: "user:1", OccurredAt: "2026-01-01T00:00:00Z"},
	}
	renderHistory(&b, events)
	out := b.String()
	// Newest first: the 2026-01-02 row is the first line.
	if idx := strings.Index(out, "2026-01-02"); idx < 0 || strings.Index(out, "2026-01-01") < idx {
		t.Errorf("the audit is not newest-first: %q", out)
	}
	// The change is shown: an old→new marker between the two values.
	if !strings.Contains(out, " → ") {
		t.Errorf("the old→new change marker is missing: %q", out)
	}
	// A first write's absent old prints (default), not a blank — the before-state
	// is never dropped.
	if !strings.Contains(out, "(default)") {
		t.Errorf("a first write's absent old must print (default): %q", out)
	}
	// The new value 5 (the top row's after-state) rides the line.
	if !strings.Contains(out, "5") {
		t.Errorf("the new value is missing from the audit line: %q", out)
	}
	// The actor and the time ride the line.
	if !strings.Contains(out, "by user:1") {
		t.Errorf("the actor is missing from the audit line: %q", out)
	}
}

// --- 15: the CLI reaches no store, config or SQL — it dials the socket ---

func TestSettingsNeverOpensTheStore(t *testing.T) {
	// Parse settings.go and refuse any import that would make it a second
	// door (a store, the config registry, or a raw SQL handle) — P34/CS.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "settings.go", nil, 0)
	if err != nil {
		t.Fatalf("parse settings.go: %v", err)
	}
	var imports []string
	for _, imp := range f.Imports {
		if imp.Path != nil {
			imports = append(imports, strings.Trim(imp.Path.Value, `"`))
		}
	}
	for _, banned := range []string{"github.com/cjlapao/aeon/internal/store", "github.com/cjlapao/aeon/internal/config", "database/sql"} {
		for _, got := range imports {
			if got == banned || strings.HasSuffix(got, "/"+strings.TrimPrefix(banned, "github.com/cjlapao/aeon/")) {
				t.Errorf("settings.go imports %q — the CLI reaches the socket only, never the store/config/SQL (P34/CS)", got)
			}
		}
	}
	// The dial-and-call helper is the whole of the connection: the guard above
	// (no store/config/SQL import) is the gate; this is the positive half — the
	// file must reach the daemon through dial, never a store handle.
	src := string(mustReadFile(t, "settings.go"))
	if !strings.Contains(src, "dial(") {
		t.Error("settings.go does not dial the socket — it must be a client of the daemon, not a store reader")
	}
}

// mustReadFile reads a file from the test's own directory, failing the test on
// error. The import guard (TestSettingsNeverOpensTheStore) needs the raw bytes
// for the positive dial check; go/parser needs them for the import list.
func mustReadFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// --- 16: the verb is listed in the binary's usage ---

func TestSettingsUsageListedInMain(t *testing.T) {
	var b strings.Builder
	printUsage(&b)
	if !strings.Contains(b.String(), "settings") {
		t.Error("the usage block does not list `settings` — the verb is not discoverable")
	}
}
