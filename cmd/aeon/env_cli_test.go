package main

// env_cli_test.go — S59 T8: the environment and secret-reference CLI is a pure
// client, and these are the eight proofs the task names. The shape of every one
// of them is the same: the CLI is allowed to NAME a method and PRINT an answer,
// and nothing else. A verb that lags or leads the daemon's registration, a
// re-worded refusal, a reshaped --json, a closed set typed into a help string,
// a store import, or a flag that could carry a secret value into argv, is each a
// separate failure with its own name.
//
// The door is stood in (envDialDoor, the gate door's precedent), except in the
// last test, which drives the REAL daemon's dispatch — the golden that proves
// `env effective --json` answers names and sources and cannot answer a value.

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// envMethods is the plane's thirteen environment doors as the daemon registers
// them. The constants are the registry's own names (Table.RegisterEnvironment
// registers these and nothing else), so walking them is walking the daemon.
var envMethods = []protocol.MethodName{
	protocol.MethodEnvironmentList, protocol.MethodEnvironmentShow,
	protocol.MethodEnvironmentEffective, protocol.MethodEnvironmentVocabulary,
	protocol.MethodEnvironmentChanges, protocol.MethodEnvironmentProvisions,
	protocol.MethodEnvironmentCreate, protocol.MethodEnvironmentUpdate,
	protocol.MethodEnvironmentArchive, protocol.MethodEnvironmentRestore,
	protocol.MethodEnvironmentBind, protocol.MethodEnvironmentUnbind,
	protocol.MethodEnvironmentTeardown,
}

// envTwin is the plane's twenty-first method, the agent's own door (S59 T7),
// which has NO verb and must be accounted for as such.
var envTwin = protocol.MethodEnvironmentProposeRestrict

var secretrefMethods = []protocol.MethodName{
	protocol.MethodSecretRefList, protocol.MethodSecretRefShow, protocol.MethodSecretRefCheck,
	protocol.MethodSecretRefCreate, protocol.MethodSecretRefUpdate,
	protocol.MethodSecretRefArchive, protocol.MethodSecretRefRestore,
}

// envFakeDoor answers methods from a table and records what was asked. A method
// with no answer and no error is a FAILURE of the test, not an empty answer: a
// guard that passes because nothing was walked proves nothing.
type envFakeDoor struct {
	answers map[protocol.MethodName]any
	errs    map[protocol.MethodName]error
	calls   []protocol.MethodName
	params  map[protocol.MethodName]json.RawMessage
}

func newEnvFakeDoor() *envFakeDoor {
	return &envFakeDoor{answers: map[protocol.MethodName]any{}, errs: map[protocol.MethodName]error{},
		params: map[protocol.MethodName]json.RawMessage{}}
}

func (f *envFakeDoor) relay(method protocol.MethodName, params, out any) error {
	f.calls = append(f.calls, method)
	raw, _ := json.Marshal(params)
	f.params[method] = raw
	if err, ok := f.errs[method]; ok {
		return err
	}
	answer, ok := f.answers[method]
	if !ok {
		return errors.New("env_cli_test: the verb asked " + string(method) + " and the fixture has no answer — the walk would prove nothing")
	}
	b, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (f *envFakeDoor) close() error { return nil }

// withEnvDoor stands a fake door behind the seam for one test.
func withEnvDoor(t *testing.T, d envDoor) {
	t.Helper()
	prev := envDialDoor
	envDialDoor = func() (envDoor, error) { return d, nil }
	t.Cleanup(func() { envDialDoor = prev })
}

// --- 1 · the tables and the daemon agree in both directions ----------------------

func TestEveryEnvVerbHasASocketMethod(t *testing.T) {
	seen := map[protocol.MethodName]int{}
	agentOnly := 0
	for _, v := range envVerbs {
		if v.method == "" {
			t.Errorf("verb %q names no socket method", v.name)
		}
		seen[v.method]++
		if v.agentOnly {
			agentOnly++
			if strings.TrimSpace(v.why) == "" {
				t.Errorf("the agent-only verb %q carries no reason", v.name)
			}
		}
		if v.method != protocol.MethodEnvironmentProposeRestrict && !isEnvMethod(v.method) {
			t.Errorf("verb %q names %q, which is not one of the thirteen environment doors", v.name, v.method)
		}
	}
	if len(envVerbs) != len(envMethods)+1 {
		t.Errorf("the env verb table has %d rows, want %d doors + the 1 agent-only row", len(envVerbs), len(envMethods))
	}
	if agentOnly != 1 {
		t.Errorf("%d agent-only env verbs, want exactly 1 (the propose twin)", agentOnly)
	}
	for m, n := range seen {
		if n != 1 {
			t.Errorf("method %q is named by %d verbs; one method, one verb", m, n)
		}
	}
	for _, v := range secretrefVerbs {
		if !isSecretRefMethod(v.method) {
			t.Errorf("secretref verb %q names %q, which is not one of the seven doors", v.name, v.method)
		}
	}
	if len(secretrefVerbs) != len(secretrefMethods) {
		t.Errorf("the secretref verb table has %d rows, want %d", len(secretrefVerbs), len(secretrefMethods))
	}
	// The wire names, typed once and compared: a constant that drifted from the
	// daemon's registration would make every verb below call a method that does
	// not exist, and only this line would say so.
	want := map[protocol.MethodName]string{
		protocol.MethodEnvironmentList: "environment.list", protocol.MethodEnvironmentShow: "environment.show",
		protocol.MethodEnvironmentEffective: "environment.effective", protocol.MethodEnvironmentVocabulary: "environment.vocabulary",
		protocol.MethodEnvironmentChanges: "environment.changes", protocol.MethodEnvironmentProvisions: "environment.provisions",
		protocol.MethodEnvironmentCreate: "environment.create", protocol.MethodEnvironmentUpdate: "environment.update",
		protocol.MethodEnvironmentArchive: "environment.archive", protocol.MethodEnvironmentRestore: "environment.restore",
		protocol.MethodEnvironmentBind: "environment.bind", protocol.MethodEnvironmentUnbind: "environment.unbind",
		protocol.MethodEnvironmentTeardown: "environment.teardown", protocol.MethodEnvironmentProposeRestrict: "environment.proposeRestrict",
		protocol.MethodSecretRefList: "secretref.list", protocol.MethodSecretRefShow: "secretref.show",
		protocol.MethodSecretRefCheck: "secretref.check", protocol.MethodSecretRefCreate: "secretref.create",
		protocol.MethodSecretRefUpdate: "secretref.update", protocol.MethodSecretRefArchive: "secretref.archive",
		protocol.MethodSecretRefRestore: "secretref.restore",
	}
	if len(want) != 21 {
		t.Fatalf("the wire-name table has %d entries, want the plane's 21", len(want))
	}
	for m, name := range want {
		if string(m) != name {
			t.Errorf("method %q is spelled %q on the wire", m, name)
		}
	}
}

func TestEverySocketEnvMethodHasAVerb(t *testing.T) {
	named := map[protocol.MethodName]bool{}
	for _, v := range envVerbs {
		named[v.method] = true
	}
	for _, m := range envMethods {
		if !named[m] {
			t.Errorf("the daemon registers %q and the CLI has no verb for it", m)
		}
	}
	if !named[envTwin] {
		t.Errorf("%q is unaccounted for: the parity walk must see the agent's door and its reason", envTwin)
	}
	namedRef := map[protocol.MethodName]bool{}
	for _, v := range secretrefVerbs {
		namedRef[v.method] = true
	}
	for _, m := range secretrefMethods {
		if !namedRef[m] {
			t.Errorf("the daemon registers %q and the CLI has no verb for it", m)
		}
	}
	// The agent's door has no VERB: a person asking for a tightening through a
	// terminal would be a second author of something only the desk's commit
	// applies, so the dispatcher refuses it — with the reason, not silence.
	var out strings.Builder
	if err := cmdEnv(&out, []string{"propose-restrict"}); err == nil {
		t.Fatal("aeon env propose-restrict was accepted; it is the agent's door, not a person's verb")
	} else if !strings.Contains(err.Error(), "agent") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

func isEnvMethod(m protocol.MethodName) bool {
	for _, x := range envMethods {
		if x == m {
			return true
		}
	}
	return false
}

func isSecretRefMethod(m protocol.MethodName) bool {
	for _, x := range secretrefMethods {
		if x == m {
			return true
		}
	}
	return false
}

// --- 2 · no path carries a secret value ------------------------------------------

// TestSecretRefCreateTakesALocationNeverAValue is C17 and §4c at the terminal.
// Both halves run: the flag sets refuse every value-shaped flag, and the source
// of the two CLI files contains no definition of one, no stdin read and no
// prompt. The walk is asserted non-empty on both sides, so a renamed file or a
// deleted flag cannot turn the guard green by proving nothing.
func TestSecretRefCreateTakesALocationNeverAValue(t *testing.T) {
	banned := []string{"--value", "--secret-value", "--secret", "--from-stdin", "--stdin", "--key", "--token", "--contents", "--raw", "--env-value"}
	// The door is stood in so the ONLY thing that can refuse is the flag set:
	// a dial failure would look like a refusal and prove nothing. And the door
	// must never be asked — a value that parsed would travel.
	for _, flag := range banned {
		door := newEnvFakeDoor()
		door.answers[protocol.MethodSecretRefCreate] = protocol.SecretRefView{ID: "1", Name: "n", InjectAs: "VAR"}
		withEnvDoor(t, door)
		args := []string{"create", "--name", "n", "--inject-as", "VAR", flag, "a-value"}
		if err := cmdSecretRef(&strings.Builder{}, args); err == nil {
			t.Errorf("secretref create accepted %s — a value-shaped flag exists", flag)
		} else if !strings.Contains(err.Error(), "not defined") {
			t.Errorf("secretref create refused %s for the wrong reason: %v", flag, err)
		}
		if len(door.calls) != 0 {
			t.Errorf("%s parsed and the daemon was asked anyway: %v", flag, door.calls)
		}
	}
	// The two locations ARE accepted, so the loop above is not green because
	// nothing parses at all.
	accepted := 0
	for _, loc := range [][]string{{"--from-env", "GH_TOKEN_NAME"}, {"--from-file", "/tmp/none"}} {
		door := newEnvFakeDoor()
		door.answers[protocol.MethodSecretRefCreate] = protocol.SecretRefView{ID: "1", Name: "n", InjectAs: "VAR", SourceKind: "daemon-env", Source: "x"}
		withEnvDoor(t, door)
		args := []string{"create", "--name", "n", "--inject-as", "VAR", loc[0], loc[1]}
		if err := cmdSecretRef(&strings.Builder{}, args); err != nil {
			t.Errorf("secretref create refused the location %s: %v", loc[0], err)
			continue
		}
		accepted++
	}
	if accepted != 2 {
		t.Fatalf("only %d of the 2 location flags worked; the refusal loop above would then prove nothing", accepted)
	}

	// The source half: no flag DEFINITION of a value-shaped name, no stdin read,
	// no prompt. Definition shapes only — these files' own prose is allowed (and
	// does) name the words it forbids.
	bannedDefs := []string{
		// A flag NAME as any definition would spell it (fs.String, fs.Bool,
		// fs.Var all take the name as the same-shaped argument), plus the two
		// ways a value could be read from the terminal instead of argv. The
		// legal ones are absent on purpose: --secret carries a reference NAME
		// and --set carries a spec LITERAL (EV4); neither is a credential.
		`, "value",`, `, "secret-value",`, `, "secretValue",`, `, "from-stdin",`, `, "stdin",`,
		`, "token",`, `, "apiKey",`, `, "key",`, `, "password",`, `, "credential",`,
		`, "contents",`, `, "raw",`, `, "envValue",`,
		"os.Stdin", "bufio.NewReader", "term.ReadPassword", "promptFor",
	}
	files := []string{"env_verbs.go", "secretref_verbs.go"}
	walked := 0
	for _, f := range files {
		src := readCmdFile(t, f)
		walked++
		for _, n := range bannedDefs {
			if strings.Contains(src, n) {
				t.Errorf("%s carries the forbidden definition or reader %s", f, n)
			}
		}
	}
	if walked != len(files) {
		t.Fatalf("walked %d files, want %d", walked, len(files))
	}
	// And the two locations are defined, or the grep half is vacuous.
	if !strings.Contains(readCmdFile(t, "secretref_verbs.go"), `"from-env"`) ||
		!strings.Contains(readCmdFile(t, "secretref_verbs.go"), `"from-file"`) {
		t.Error("secretref_verbs.go no longer defines --from-env / --from-file; the location pair is the design")
	}
}
func readCmdFile(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(src)
}

// --- 3 · a refusal is the daemon's sentence, byte for byte -----------------------

func TestEnvCLIRefusalsVerbatim(t *testing.T) {
	refusal := &protocol.Error{
		Code:    -31002,
		Message: "the environment refuses",
		Data: protocol.Data{
			What:  "environment create refused: spec.set names AWS_SECRET_ACCESS_KEY",
			Means: "a credential never enters a spec; the value would be in the row, the log and the audit (EV7)",
			Todo:  "file a secret reference instead: aeon secretref create --name … --inject-as AWS_SECRET_ACCESS_KEY --from-env NAME",
		},
	}
	door := newEnvFakeDoor()
	door.errs[protocol.MethodEnvironmentCreate] = refusal
	withEnvDoor(t, door)

	var out strings.Builder
	err := cmdEnv(&out, []string{"create", "--name", "x", "--backend", "container", "--set", "AWS_SECRET_ACCESS_KEY=hunter2"})
	if err == nil {
		t.Fatal("the CLI swallowed the daemon's refusal")
	}
	var e *protocol.Error
	if !errors.As(err, &e) || e != refusal {
		t.Fatalf("the CLI re-wrapped the refusal (%v); the renderer must see the daemon's own error", err)
	}
	// renderError is the ONE renderer; these are the bytes a person reads.
	var rendered strings.Builder
	renderError(&rendered, err)
	for _, want := range []string{refusal.Message, refusal.Data.What, refusal.Data.Means, refusal.Data.Todo} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("the rendered refusal is missing, verbatim: %q\ngot:\n%s", want, rendered.String())
		}
	}
	if strings.Contains(rendered.String(), "hunter2") {
		t.Error("a value the person typed into --set reached the rendered refusal")
	}
}

// --- 4 · the seatbelts -------------------------------------------------------------

func TestEnvBindRequiresYes(t *testing.T) {
	door := newEnvFakeDoor()
	withEnvDoor(t, door)

	// No --yes, no --dry-run: refused, and the daemon never heard the ask.
	if err := cmdEnv(&strings.Builder{}, []string{"bind", "--component", "COMP-1", "my-env"}); err == nil {
		t.Fatal("an applied bind was accepted without --yes")
	}
	if len(door.calls) != 0 {
		t.Errorf("the refused bind still asked the daemon: %v", door.calls)
	}
	// Flags after the positional parse too (envParse): the usage the task
	// spells is `aeon env bind --component ID <id|slug> --dry-run` and a
	// person must get to type it that way.
	// A dry run needs NOTHING: a terminal command is already a deliberate act,
	// and the statement is the daemon's own (EV18).
	door.answers[protocol.MethodEnvironmentBind] = protocol.BindResult{Statement: "would bind COMP-1 to my-env", DryRun: true}
	var out strings.Builder
	if err := cmdEnv(&out, []string{"bind", "--component", "COMP-1", "my-env", "--dry-run"}); err != nil {
		t.Fatalf("a dry run needs no seatbelt: %v", err)
	}
	if !strings.Contains(out.String(), "would bind COMP-1 to my-env") {
		t.Errorf("the daemon's statement was not printed verbatim:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Error("a dry run did not say so")
	}
	// With --yes the applied bind goes through, and both calls named the one method.
	door.answers[protocol.MethodEnvironmentBind] = protocol.BindResult{Statement: "bound COMP-1 to my-env", Changed: true}
	if err := cmdEnv(&strings.Builder{}, []string{"bind", "my-env", "--component", "COMP-1", "--yes"}); err != nil {
		t.Fatalf("the applied bind: %v", err)
	}
	for _, c := range door.calls {
		if c != protocol.MethodEnvironmentBind {
			t.Errorf("a bind verb asked %q", c)
		}
	}
	// unbind and teardown carry the same seatbelt, and refuse without a target.
	for _, args := range [][]string{{"unbind", "--component", "COMP-1"}, {"teardown", "PROV-1"}} {
		if err := cmdEnv(&strings.Builder{}, args); err == nil {
			t.Errorf("%v was accepted without --yes", args)
		}
	}
}

func TestHostInheritCreateRequiresYes(t *testing.T) {
	door := newEnvFakeDoor()
	withEnvDoor(t, door)
	if err := cmdEnv(&strings.Builder{}, []string{"create", "--name", "inherit me", "--backend", "host-inherit", "--passthrough", "PATH"}); err == nil {
		t.Fatal("a host-inherit environment was created without --yes — the widest environment there is (EV6)")
	}
	if len(door.calls) != 0 {
		t.Errorf("the refused create still asked the daemon: %v", door.calls)
	}
	door.answers[protocol.MethodEnvironmentCreate] = protocol.EnvironmentView{ID: "1", Slug: "inherit-me", Backend: "host-inherit", Origin: "user"}
	if err := cmdEnv(&strings.Builder{}, []string{"create", "--name", "inherit me", "--backend", "host-inherit", "--passthrough", "PATH", "--yes"}); err != nil {
		t.Fatalf("with --yes the create goes through: %v", err)
	}
	if len(door.calls) != 1 || door.calls[0] != protocol.MethodEnvironmentCreate {
		t.Errorf("calls = %v", door.calls)
	}
}

// --- 5 · --json is the wire -------------------------------------------------------

func TestEnvJSONIsTheWire(t *testing.T) {
	rows := []protocol.EnvironmentView{
		{ID: "1", Slug: "a", Name: "A", Backend: "container", Origin: "user", Revision: 3,
			Spec: json.RawMessage(`{"home":"real"}`), CreatedAt: "t", UpdatedAt: "t", Capabilities: []string{"limits"}},
	}
	door := newEnvFakeDoor()
	door.answers[protocol.MethodEnvironmentList] = rows
	withEnvDoor(t, door)

	var out strings.Builder
	if err := cmdEnv(&out, []string{"list", "--json"}); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("--json did not print one JSON document: %v\n%s", err, out.String())
	}
	want := mustDecode[[]map[string]any](t, mustEncode(t, rows))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--json is not the wire result:\n got %v\nwant %v", got, want)
	}
	// A field the human printer drops must still be there: --json reshapes nothing.
	for _, k := range []string{"revision", "capabilities", "createdAt", "origin"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("--json dropped the wire field %q", k)
		}
	}
	// And the human path prints neither JSON nor a value: one formatter, and the
	// daemon's words inside it.
	var human strings.Builder
	if err := cmdEnv(&human, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(human.String(), "{") {
		t.Errorf("the human path printed JSON:\n%s", human.String())
	}
	if !strings.Contains(human.String(), "a") || !strings.Contains(human.String(), "container") {
		t.Errorf("the human path printed nothing useful:\n%s", human.String())
	}
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustDecode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// --- 6 · the closed sets are the daemon's, not the CLI's --------------------------

func TestCreateHelpIsGeneratedFromVocabulary(t *testing.T) {
	door := newEnvFakeDoor()
	withEnvDoor(t, door)
	door.answers[protocol.MethodEnvironmentVocabulary] = protocol.EnvironmentVocabularyView{
		Backends:         []protocol.BackendView{{Name: "zeta-backend", Strength: 7}, {Name: "alpha-backend", Strength: 1}},
		DefaultAllowlist: []string{"ZETA_ALLOWLIST_WORD"},
		DenyWords:        []string{"ZETA_DENY_WORD"}, DenyPrefixes: []string{"ZETA_"}, DenyExact: []string{"ZETA_EXACT"},
		DenyLoader: []string{"ZETA_LOADER"}, EgressModes: []string{"zeta-mode"}, PullPolicies: []string{"zeta-pull"},
		HomeModes: []string{"zeta-home"}, SourceKinds: []string{"zeta-kind"},
		ProvisionIdleTTL: "24h", ProvisionIdleTTLFrom: "declared-default",
	}
	for _, verb := range []string{"create", "update"} {
		var out strings.Builder
		if err := cmdEnv(&out, []string{verb, "--help"}); err != nil {
			t.Fatalf("%s --help: %v", verb, err)
		}
		s := out.String()
		for _, want := range []string{"zeta-backend(7)", "alpha-backend(1)", "ZETA_ALLOWLIST_WORD", "ZETA_DENY_WORD", "ZETA_", "zeta-mode", "zeta-pull", "zeta-home"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s --help does not carry the vocabulary's %q:\n%s", verb, want, s)
			}
		}
	}
	// The same help, from a DIFFERENT daemon's words, with no CLI edit: a closed
	// set typed into a string here would still print the old one.
	door.answers[protocol.MethodEnvironmentVocabulary] = protocol.EnvironmentVocabularyView{
		Backends: []protocol.BackendView{{Name: "only-this-one", Strength: 2}},
	}
	var out strings.Builder
	if err := cmdEnv(&out, []string{"create", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "only-this-one(2)") {
		t.Errorf("the help did not follow the daemon:\n%s", out.String())
	}
	if strings.Contains(out.String(), "zeta-backend") {
		t.Error("the help still names a backend this daemon never answered — a closed set is typed into the CLI")
	}
	// And the CLI carries no copy of the daemon's closed WORDS. The list comes
	// from `internal/execenv` itself, so a word added there is checked here the
	// day it is added — and the help above can only be the answer. The two
	// source kinds the location flags map to are the exception the task spells
	// (--from-env → daemon-env, --from-file → key-file): the CLI's own contract,
	// not a closed set a person types.
	src := readCmdFile(t, "env_verbs.go") + readCmdFile(t, "secretref_verbs.go")
	typed := []string{execenv.BackendHostInherit, execenv.BackendHostScrubbed,
		execenv.BackendContainer, execenv.BackendVM}
	typed = append(typed, execenv.EgressModes()...)
	typed = append(typed, execenv.PullPolicies()...)
	typed = append(typed, execenv.HomeModes()...)
	if len(typed) < 10 {
		t.Fatalf("only %d closed words to check — execenv's vocabulary did not answer, so this walk would prove nothing", len(typed))
	}
	for _, w := range typed {
		if strings.Contains(src, "\""+w+"\"") {
			t.Errorf("the CLI hardcodes the closed word %q — it lives in execenv, and the help prints it from the daemon's answer", w)
		}
	}
}

// --- 7 · no direct door ------------------------------------------------------------

func TestEnvVerbsNeverImportTheStore(t *testing.T) {
	banned := []string{"github.com/cjlapao/aeon/internal/store", "github.com/cjlapao/aeon/internal/daemon",
		"github.com/cjlapao/aeon/internal/paths", "gorm.io"}
	walked := 0
	for _, name := range []string{"env_verbs.go", "secretref_verbs.go"} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(f.Imports) == 0 {
			t.Fatalf("%s has no imports — the walk proved nothing", name)
		}
		walked++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if path == b {
					t.Errorf("%s imports %s — this plane has NO direct door (EV18)", name, path)
				}
			}
		}
	}
	if walked != 2 {
		t.Fatalf("walked %d files, want 2", walked)
	}
	// The door interface has exactly ONE implementation besides the tests: the
	// socket. A second door would be a second implementation of the plane.
	if _, err := selectEnvDoor(); err == nil {
		t.Error("selectEnvDoor answered without a daemon — there is no fallback allowed here")
	}
}

// --- 8 · the golden over a real daemon --------------------------------------------

// envDaemonDoor is the second door in this file and the only one that is not a
// fake: it rides the REAL daemon's dispatch, so the golden below is the answer
// the socket would carry, not an imitation of it.
type envDaemonDoor struct {
	d    *daemon.Daemon
	conn string
}

func (d *envDaemonDoor) relay(method protocol.MethodName, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	res, err := d.d.Table.Dispatch(context.Background(), d.conn, method, raw)
	if err != nil {
		return err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (d *envDaemonDoor) close() error { return nil }

func TestEnvEffectiveJSONOverATestDaemonHasNamesAndSourcesNeverValues(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{Store: st})
	const conn = "t8-effective-conn"
	envPropInitialize(t, d, conn)

	row, ref, err := st.CreateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewEnvironment{
		Slug: "t8-effective-env", Name: "T8 effective env", Backend: "host-scrubbed",
		Spec: envPropSpec(t, []string{"PATH", "HOME", "LANG"}, nil, nil),
	})
	if ref != nil || err != nil {
		t.Fatalf("seed the environment: ref=%v err=%v", ref, err)
	}
	if _, _, err := st.BindEnvironment(store.Actor{Kind: "user", ID: "person"},
		store.NewBinding{ScopeKind: "workspace", EnvironmentID: row.ID}); err != nil {
		t.Fatalf("bind the workspace floor: %v", err)
	}

	withEnvDoor(t, &envDaemonDoor{d: d, conn: conn})
	var out strings.Builder
	if err := cmdEnv(&out, []string{"effective", "--json"}); err != nil {
		t.Fatalf("aeon env effective --json: %v", err)
	}
	view := mustDecode[map[string]any](t, []byte(out.String()))
	vars, _ := view["variables"].([]any)
	if len(vars) == 0 {
		t.Fatalf("the daemon answered no variables at all:\n%s", out.String())
	}
	// The closed verbs the daemon speaks for a name's source (EV5): the
	// allowlist, a spec literal, a passthrough name, or an injected reference.
	// A source outside that set is a word the CLI invented.
	knownSource := map[string]bool{"allowlist": true, "set": true, "passthrough": true}
	sawKnown := false
	for _, v := range vars {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("a variable is not an object: %v", v)
		}
		if _, ok := m["name"].(string); !ok {
			t.Errorf("a variable carries no name: %v", m)
		}
		if src, ok := m["source"].(string); !ok || src == "" {
			t.Errorf("a variable carries no source: %v", m)
		} else if knownSource[src] || strings.HasPrefix(src, "injected:") {
			sawKnown = true
		} else {
			t.Errorf("a variable names a source the daemon does not speak: %q", src)
		}
		if len(m) != 2 {
			t.Errorf("a variable carries more than name and source: %v", m)
		}
	}
	if !sawKnown {
		t.Errorf("no variable carried a source from the daemon's closed set:\n%s", out.String())
	}
	// The whole answer, walked: no value-shaped key anywhere.
	for _, k := range scanKeys(t, []byte(out.String())) {
		switch k {
		case "value", "secret", "secretValue", "token", "apiKey", "key", "password", "credential", "contents", "data", "raw", "bytes", "envValue":
			t.Errorf("environment.effective answered the value-shaped key %q:\n%s", k, out.String())
		}
	}
}

// scanKeys walks every JSON object key in a document, however deep.
func scanKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var keys []string
	var walk func(any)
	walk = func(x any) {
		switch n := x.(type) {
		case map[string]any:
			for k, val := range n {
				keys = append(keys, k)
				walk(val)
			}
		case []any:
			for _, e := range n {
				walk(e)
			}
		}
	}
	walk(v)
	return keys
}

// TestCLINeverAcceptsASecretValue is the slice-wide property, walked as syntax
// rather than as text: every flag this plane's two files DEFINE is named, and no
// name may be credential-shaped — not "value", not "token", not the source's
// contents, in any spelling. A substring test can be satisfied by a flag spelled
// just outside its needles; this one asks the parser what flags exist, so the
// only way to pass is not to define one.
func TestCLINeverAcceptsASecretValue(t *testing.T) {
	credentialShaped := map[string]bool{
		"value": true, "secret": true, "secretvalue": true, "token": true, "apikey": true,
		"key": true, "password": true, "pass": true, "credential": true, "contents": true,
		"data": true, "raw": true, "bytes": true, "envvalue": true, "secretvaluehere": true,
		"fromstdin": true, "stdin": true, "passphrase": true, "authtoken": true, "accesstoken": true,
	}
	// `--secret` is legal ON THE ENV SIDE and illegal on the secretref side: it
	// carries a reference NAME there, so the shape alone cannot decide. The
	// environment file may define it; the secret-reference file may not (its own
	// location flags are the pair), and both are checked for it below.
	defined := 0
	for _, name := range []string{"env_verbs.go", "secretref_verbs.go"} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || recv.Name != "fs" {
				return true
			}
			// The flag NAME is the string literal these three shapes all take:
			// String/Bool/Int/Int64/Float64 take it first, Var takes it second.
			idx := 0
			if sel.Sel.Name == "Var" {
				idx = 1
			}
			if len(call.Args) <= idx {
				return true
			}
			lit, ok := call.Args[idx].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			flag := strings.Trim(lit.Value, `"`)
			defined++
			clean := strings.NewReplacer("-", "", "_", "").Replace(flag)
			if credentialShaped[strings.ToLower(clean)] {
				if name == "env_verbs.go" && flag == "secret" {
					return true // a reference NAME, not a value (EV7)
				}
				t.Errorf("%s defines the credential-shaped flag --%s", name, flag)
			}
			return true
		})
	}
	// Non-vacuous: the two files together define the whole verb surface. A
	// renamed flag helper that hid every definition would fail here, not pass.
	// 2026-11-04 (S49 T10 merge reconciliation): the floor moves 40 -> 39 —
	// S59 T12's 2712ead2 stopped the preview door accepting attendedness,
	// removing one flag definition, and the floor did not move with it. The
	// drift is the finding; 39 is the measured count on this merged tree.
	if defined < 39 {
		t.Fatalf("the walk found %d flag definitions across the two files; the verb surface is far larger, so this guard is not walking it", defined)
	}
	t.Logf("walked %d flag definitions; none is credential-shaped", defined)
}
