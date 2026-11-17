package main

// intake_parity_test.go — S61 T10: the charter, template and intake
// verbs proven against the S27 T9 harness (the same shape, the same
// teeth): fixture ↔ registered-door parity both ways, the parsed-import
// and banned-string walks (the socket is the only road out), the binary's
// dispatch lines, the seatbelts, verbatim refusals, help generated from
// the daemon's own vocabulary, --json as the wire bytes, and two human
// goldens over a stand daemon.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// ---------------------------------------------------------------------------
// the stand services — canned answers, recorded calls, one settable refusal
// ---------------------------------------------------------------------------

type s61FakeCharter struct {
	view     protocol.CharterView
	listRes  protocol.CharterListResult
	revRes   protocol.CharterRevisionsResult
	vocabRes protocol.CharterVocabularyResult
	refusal  error
	calls    []protocol.MethodName
	lastSave protocol.CharterSaveParams
	lastAct  protocol.Actor
}

func (f *s61FakeCharter) Get(_ context.Context, p protocol.CharterGetParams) (protocol.CharterView, error) {
	f.calls = append(f.calls, protocol.MethodCharterGet)
	if f.refusal != nil {
		return protocol.CharterView{}, f.refusal
	}
	v := f.view
	if p.Revision != nil {
		v.Revision = *p.Revision
	}
	return v, nil
}
func (f *s61FakeCharter) List(_ context.Context) (protocol.CharterListResult, error) {
	f.calls = append(f.calls, protocol.MethodCharterList)
	return f.listRes, f.refusal
}
func (f *s61FakeCharter) Revisions(_ context.Context, _ protocol.CharterRevisionsParams) (protocol.CharterRevisionsResult, error) {
	f.calls = append(f.calls, protocol.MethodCharterRevisions)
	return f.revRes, f.refusal
}
func (f *s61FakeCharter) Vocabulary(_ context.Context) (protocol.CharterVocabularyResult, error) {
	f.calls = append(f.calls, protocol.MethodCharterVocabulary)
	return f.vocabRes, f.refusal
}
func (f *s61FakeCharter) Save(_ context.Context, a protocol.Actor, p protocol.CharterSaveParams) (protocol.CharterView, error) {
	f.calls = append(f.calls, protocol.MethodCharterSave)
	f.lastSave, f.lastAct = p, a
	if f.refusal != nil {
		return protocol.CharterView{}, f.refusal
	}
	v := f.view
	v.Revision++
	return v, nil
}

type s61FakeTemplates struct {
	listRes  protocol.TemplateListResult
	view     protocol.TemplateView
	refusal  error
	lastSave protocol.TemplateSaveParams
	lastAct  protocol.Actor
}

func (f *s61FakeTemplates) List(_ context.Context, _ protocol.TemplateListParams) (protocol.TemplateListResult, error) {
	return f.listRes, f.refusal
}
func (f *s61FakeTemplates) Get(_ context.Context, _ protocol.TemplateGetParams) (protocol.TemplateView, error) {
	return f.view, f.refusal
}
func (f *s61FakeTemplates) Save(_ context.Context, a protocol.Actor, p protocol.TemplateSaveParams) (protocol.TemplateView, error) {
	f.lastSave, f.lastAct = p, a
	if f.refusal != nil {
		return protocol.TemplateView{}, f.refusal
	}
	return f.view, nil
}

type s61FakeIntake struct {
	startView protocol.IntakeView
	listRes   protocol.IntakeListResult
	statusRes protocol.IntakeStatusView
	planRes   protocol.IntakePreview
	commitRes protocol.IntakeCommitResult
	abanRes   protocol.IntakeAbandonResult
	vocabRes  protocol.IntakeVocabularyResult
	refusal   error
	calls     []protocol.MethodName
	lastPlan  protocol.IntakePlanParams
	lastCommt protocol.IntakeCommitParams
	lastAband protocol.IntakeAbandonParams
	lastAct   protocol.Actor
}

func (f *s61FakeIntake) Start(_ context.Context, a protocol.Actor, _ protocol.IntakeStartParams) (protocol.IntakeView, error) {
	f.calls = append(f.calls, protocol.MethodIntakeStart)
	f.lastAct = a
	return f.startView, f.refusal
}
func (f *s61FakeIntake) List(_ context.Context) (protocol.IntakeListResult, error) {
	f.calls = append(f.calls, protocol.MethodIntakeList)
	return f.listRes, f.refusal
}
func (f *s61FakeIntake) Status(_ context.Context, _ protocol.IntakeStatusParams) (protocol.IntakeStatusView, error) {
	f.calls = append(f.calls, protocol.MethodIntakeStatus)
	return f.statusRes, f.refusal
}
func (f *s61FakeIntake) Plan(_ context.Context, p protocol.IntakePlanParams) (protocol.IntakePreview, error) {
	f.calls = append(f.calls, protocol.MethodIntakePlan)
	f.lastPlan = p
	return f.planRes, f.refusal
}
func (f *s61FakeIntake) Commit(_ context.Context, a protocol.Actor, p protocol.IntakeCommitParams) (protocol.IntakeCommitResult, error) {
	f.calls = append(f.calls, protocol.MethodIntakeCommit)
	f.lastCommt, f.lastAct = p, a
	return f.commitRes, f.refusal
}
func (f *s61FakeIntake) Abandon(_ context.Context, a protocol.Actor, p protocol.IntakeAbandonParams) (protocol.IntakeAbandonResult, error) {
	f.calls = append(f.calls, protocol.MethodIntakeAbandon)
	f.lastAband, f.lastAct = p, a
	if f.refusal != nil {
		return protocol.IntakeAbandonResult{}, f.refusal
	}
	return f.abanRes, nil
}
func (f *s61FakeIntake) Vocabulary(_ context.Context) (protocol.IntakeVocabularyResult, error) {
	f.calls = append(f.calls, protocol.MethodIntakeVocabulary)
	return f.vocabRes, f.refusal
}

// standS61Socket binds a real unix socket speaking the real table over
// the three seams (the standSchedulerSocket shape, reused wholesale).
func standS61Socket(t *testing.T, ch *s61FakeCharter, tp *s61FakeTemplates, ik *s61FakeIntake) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "rpc.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("bind the stand socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv("LOCALRPC_ENDPOINT_PATH", sock)

	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Charter, tbl.Templates, tbl.Intake = ch, tp, ik
	tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
		return protocol.Actor{Kind: "user", ID: "uid-1000"}
	}
	if !tbl.RegisterCharter() || !tbl.RegisterTemplates() || !tbl.RegisterIntake() {
		t.Fatal("a register refused over a wired seam")
	}
	go func() {
		var i int
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			i++
			go serveParityConn(tbl, conn, idForStand(i))
		}
	}()
}

func idForStand(i int) string { return "c-s61-" + string(rune('a'+i%26)) }

func s61RawClient(t *testing.T) *protocol.Client {
	t.Helper()
	c, err := dial(0)
	if err != nil {
		t.Fatalf("raw dial the stand socket: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func s61FixedFakes() (*s61FakeCharter, *s61FakeTemplates, *s61FakeIntake) {
	ch := &s61FakeCharter{
		view: protocol.CharterView{
			RootID: "root-1", Revision: 2, TotalRevisions: 3,
			Body:    json.RawMessage(`{"goal":"ship the thing","note":"why"}`),
			Summary: "ship the thing", ActorKind: "user", ActorID: "uid-1000",
			Source: "cli", CreatedAt: "2026-11-05T12:00:00Z",
		},
		vocabRes: protocol.CharterVocabularyResult{
			CheckKinds: []string{"test", "lint"},
			Autonomy:   []string{}, AutonomyNote: "S56 wires the grant plane; the door invents no verb",
		},
	}
	tp := &s61FakeTemplates{
		view: protocol.TemplateView{
			TemplateSummary: protocol.TemplateSummary{
				Slug: "go-cli", Title: "A Go command-line tool", Origin: "system",
				Version: 3, FileCount: 2, StackTags: []string{"go"},
			},
			Files: []protocol.TemplateFileWire{{Path: "main.go", Content: "package main\n"}},
		},
	}
	startView := protocol.IntakeView{ID: "int-aa", Name: "ship", ScratchPath: "/state/intake/ship-1a2b3c", LoopSlug: "intake-aa"}
	ik := &s61FakeIntake{
		startView: startView,
		listRes:   protocol.IntakeListResult{Intakes: []protocol.IntakeStatusRow{{IntakeView: startView, State: "interviewing"}}},
		statusRes: protocol.IntakeStatusView{IntakeID: "int-aa", State: "interviewing", Steps: []protocol.IntakeStepView{{Step: "start", Outcome: "done", At: "2026-11-05T12:00:00Z"}}},
		planRes: protocol.IntakePreview{
			IntakeID: "int-aa", ProposalID: "9", Digest: "deadbeef", TargetPath: "/repos", Publish: "none",
			Components: []protocol.IntakePreviewComponent{{Slug: "thing", Role: "primary", Name: "The thing", TargetPath: "/repos/thing", Blueprint: "go-cli", Files: []string{"main.go"}}},
			Goals:      1, Records: []string{"goals: 1"},
		},
		commitRes: protocol.IntakeCommitResult{IntakeID: "int-aa", RootID: "root-9", TargetPath: "/repos/thing", Goals: 1},
		abanRes:   protocol.IntakeAbandonResult{IntakeID: "int-aa", LoopClosed: true, RejectedProposals: []string{"9"}, Intake: startView},
		vocabRes: protocol.IntakeVocabularyResult{
			Publish: []protocol.IntakePublishChoice{{Value: "none", Label: "wizard.publish.none.label"}, {Value: "private", Label: "wizard.publish.private.label"}, {Value: "public", Label: "wizard.publish.public.label"}}, Roles: []string{"primary"},
			WorkspaceRoot: "/repos", WorkspaceRootState: "configured",
		},
	}
	return ch, tp, ik
}

// ---------------------------------------------------------------------------
// parity, socket law, dispatch
// ---------------------------------------------------------------------------

func s61SourceMethods(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, file := range []string{"charter.go", "template.go", "intake.go"} {
		prefixFor := map[string]string{"charter.go": "MethodCharter", "template.go": "MethodTemplate", "intake.go": "MethodIntake"}
		methods := walkMethodSelectors(t, file, prefixFor[file])
		for _, m := range methods {
			out[m] = true
		}
	}
	return out
}

// walkMethodSelectors reports the protocol.Method* constants a file
// actually names (the schedSourceMethods idiom, per file and prefix).
func walkMethodSelectors(t *testing.T, file, prefix string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "protocol" && strings.HasPrefix(sel.Sel.Name, prefix) {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// s61ConstFor names the Go constant a wire method compiles from — the
// join between the registered names and the selectors the AST walk
// reports (the schedConstFor idiom, mechanical for this plane).
func s61ConstFor(m protocol.MethodName) string {
	fam, last, found := strings.Cut(string(m), ".")
	if !found {
		return "?" + string(m)
	}
	if len(last) == 0 {
		return "?" + string(m)
	}
	return "Method" + strings.ToUpper(fam[:1]) + fam[1:] + strings.ToUpper(last[:1]) + last[1:]
}

func TestS61VerbParityBothWays(t *testing.T) {
	fixture := append(append(append([]cliVerb{}, charterVerbs...), templateVerbs...), intakeVerbs...)
	called := s61SourceMethods(t)
	seen := map[protocol.MethodName]int{}
	for _, v := range fixture {
		seen[v.method]++
		if !called[s61ConstFor(v.method)] {
			t.Errorf("verb %q names %s in the fixture but the CLI files never call it — the fixture would vouch for a door nobody opens", v.verb, v.method)
		}
	}
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	ch, tp, ik := s61FixedFakes()
	tbl.Charter, tbl.Templates, tbl.Intake = ch, tp, ik
	if !tbl.RegisterCharter() || !tbl.RegisterTemplates() || !tbl.RegisterIntake() {
		t.Fatal("a register refused over a wired seam")
	}
	registered := 0
	for _, m := range tbl.Methods() {
		if !strings.HasPrefix(string(m), "charter.") && !strings.HasPrefix(string(m), "template.") && !strings.HasPrefix(string(m), "intake.") {
			continue
		}
		registered++
		if seen[m] != 1 {
			t.Errorf("%s is covered by %d CLI verbs — exactly one verb per door, both ways (T0's parity law)", m, seen[m])
		}
	}
	if len(fixture) != registered {
		t.Errorf("the fixtures list %d verbs, the plane registers %d doors", len(fixture), registered)
	}
}

func TestS61VerbsAreSocketClients(t *testing.T) {
	for _, file := range []string{"charter.go", "template.go", "intake.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"net.Dial", "http.", "exec.Command", "store.Open", "sqlite", "/internal/store", "/internal/daemon", "/internal/desk"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s contains %q — charterDial/templateDial/intakeDial are the only roads out (P5, rule 1)", file, banned)
			}
		}
	}
}

func TestS61VerbIsDispatchedByTheBinary(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, pair := range [][2]string{
		{"charter", "err = cmdCharter(os.Stdout, args)"},
		{"template", "err = cmdTemplate(os.Stdout, args)"},
		{"intake", "err = cmdIntake(os.Stdout, args)"},
	} {
		if !strings.Contains(src, "case \""+pair[0]+"\":") {
			t.Errorf(`main.go lost case "%s": — the real binary answers "unknown command" while every verb test stays green (the S53 F-1 lesson)`, pair[0])
		}
		if !strings.Contains(src, pair[1]) {
			t.Errorf("main.go's %s case no longer dispatches to %s — the CLI is ONE implementation", pair[0], pair[1])
		}
	}
	for _, line := range []string{"charter           list, show, history, save, vocabulary", "template          list, show, save", "intake            start, list, status, plan, commit"} {
		if !strings.Contains(src, line) {
			t.Errorf("the usage list no longer advertises %q — a hidden verb is a deleted verb waiting to happen", line)
		}
	}
}

func TestNoSecretFlags(t *testing.T) {
	// C17 in one walk: no flag NAME can hold a secret on the S61 verbs.
	banned := []string{"token", "secret", "password", "passphrase", "credential", "apikey", "api-key"}
	for _, file := range []string{"charter.go", "template.go", "intake.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(src))
		for _, b := range banned {
			if strings.Contains(lower, `"`+b+`",`) || strings.Contains(lower, `fs.String("`+b) {
				t.Errorf("%s mounts a flag named like a secret (%q) — the plane never accepts one (C17)", file, b)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// the seatbelts and the commit flow
// ---------------------------------------------------------------------------

func TestCommitRequiresAPlanDigest(t *testing.T) {
	// No stand daemon: the digest refusal is pre-socket (an input that
	// cannot be a value), and it points at `plan` by name.
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "nothing.sock"))
	var out bytes.Buffer
	err := cmdIntake(&out, []string{"commit", "int-aa", "--proposal", "9"})
	if err == nil {
		t.Fatal("commit ran without --digest — blind landing allowed")
	}
	if !strings.Contains(err.Error(), "--digest") || !strings.Contains(err.Error(), "plan") {
		t.Errorf("the refusal does not point at plan: %v", err)
	}
}

func TestPublishNeedsYes(t *testing.T) {
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	var out bytes.Buffer
	err := cmdIntake(&out, []string{"commit", "int-aa", "--proposal", "9", "--digest", "deadbeef", "--publish", "private"})
	if err == nil {
		t.Fatal("`--publish private` ran without --yes — an accidental publish is exactly what the seatbelt is for")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("the seatbelt refusal does not name --yes: %v", err)
	}
	for _, m := range ik.calls {
		if m == protocol.MethodIntakeCommit {
			t.Error("the refused commit reached the door before the seatbelt did")
		}
	}
	if err := cmdIntake(&out, []string{"commit", "int-aa", "--proposal", "9", "--digest", "deadbeef", "--publish", "public", "--confirm-public", "--yes"}); err != nil {
		t.Fatalf("the seated commit: %v", err)
	}
	if ik.lastCommt.Publish != "public" || !ik.lastCommt.ConfirmPublic || ik.lastCommt.Digest != "deadbeef" {
		t.Errorf("the wire carries %+v — the flags must ride through untouched", ik.lastCommt)
	}
	if ik.lastAct.Kind != "user" || ik.lastAct.ID != "uid-1000" {
		t.Errorf("the seam saw actor %+v — the CONNECTION names the person (P4), no flag ever could", ik.lastAct)
	}
}

func TestCommitPlansFirstAndPrintsTheStatement(t *testing.T) {
	// The spec's flow, witnessed: a human commit calls the plan door
	// BEFORE the commit door and prints the daemon's file list and digest.
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	var out bytes.Buffer
	if err := cmdIntake(&out, []string{"commit", "int-aa", "--proposal", "9", "--digest", "deadbeef"}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(ik.calls) < 2 || ik.calls[0] != protocol.MethodIntakePlan || ik.calls[len(ik.calls)-1] != protocol.MethodIntakeCommit {
		t.Fatalf("the door order was %v — plan first, commit last", ik.calls)
	}
	human := out.String()
	for _, want := range []string{"digest: deadbeef", "main.go", "/repos/thing", "goals 1", "committed: intake int-aa"} {
		if !strings.Contains(human, want) {
			t.Errorf("the human commit output lacks %q:\n%s", want, human)
		}
	}
}

func TestIntakeHelpIsGeneratedFromVocabulary(t *testing.T) {
	// Help generated, never typed twice: with a daemon up, the usage
	// line carries the daemon's OWN closed set; cold, it refuses to
	// invent one.
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	var out bytes.Buffer
	printIntakeUsage(&out)
	got := out.String()
	if !strings.Contains(got, "[none private public]") {
		t.Errorf("the usage line does not quote the daemon's publish set:\n%s", got)
	}
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "cold.sock"))
	var cold bytes.Buffer
	printIntakeUsage(&cold)
	if strings.Contains(cold.String(), "none private public") {
		t.Error("the cold usage typed the closed set itself — help must not know what only the daemon knows")
	}
}

// ---------------------------------------------------------------------------
// verbatim refusals and --json as the wire
// ---------------------------------------------------------------------------

func TestCLIRefusalsAreTheDaemonsSentences(t *testing.T) {
	ch, tp, ik := s61FixedFakes()
	ik.refusal = protocol.ErrIntakeRefused.WithWhat("the loop is running — loop.stop names what stands in the way")
	standS61Socket(t, ch, tp, ik)
	raw := s61RawClient(t)
	rawErr := raw.Call(protocol.MethodIntakeAbandon, map[string]string{"intakeId": "int-aa", "reason": "stalled"}, new(protocol.IntakeAbandonResult))
	if rawErr == nil {
		t.Fatal("the stand refusal did not reach the raw client")
	}
	var out bytes.Buffer
	cliErr := cmdIntake(&out, []string{"abandon", "int-aa", "--reason", "stalled"})
	if cliErr == nil {
		t.Fatal("the refusal did not reach the CLI")
	}
	if got, want := cliErr.Error(), rawErr.Error(); got != want {
		t.Errorf("the CLI re-worded the daemon's refusal:\n CLI  %s\n DAEMON %s", got, want)
	}
	var cliPE, rawPE *protocol.Error
	if !errors.As(cliErr, &cliPE) || !errors.As(rawErr, &rawPE) {
		t.Fatalf("neither door surfaced a protocol error: cli=%v raw=%v", cliErr, rawErr)
	}
	cliBytes, _ := json.Marshal(cliPE)
	if !strings.Contains(string(cliBytes), "loop.stop names what stands in the way") {
		t.Errorf("the refusal's own words must ride through the CLI's error: %s", cliBytes)
	}
	if cliPE.Code != rawPE.Code {
		t.Errorf("codes disagree: cli %d raw %d", cliPE.Code, rawPE.Code)
	}
}

func TestS61JSONIsTheWire(t *testing.T) {
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	raw := s61RawClient(t)
	type probe struct {
		name string
		cmd  []string
		call func() json.RawMessage
	}
	for _, p := range []probe{
		{"charter show", []string{"charter", "show", "root-1", "--json"}, func() json.RawMessage {
			var r json.RawMessage
			_ = raw.Call(protocol.MethodCharterGet, protocol.CharterGetParams{RootID: "root-1"}, &r)
			return r
		}},
		{"intake status", []string{"intake", "status", "int-aa", "--json"}, func() json.RawMessage {
			var r json.RawMessage
			_ = raw.Call(protocol.MethodIntakeStatus, protocol.IntakeStatusParams{IntakeID: "int-aa"}, &r)
			return r
		}},
		{"intake vocabulary", []string{"intake", "vocabulary", "--json"}, func() json.RawMessage {
			var r json.RawMessage
			_ = raw.Call(protocol.MethodIntakeVocabulary, struct{}{}, &r)
			return r
		}},
		{"template list", []string{"template", "list", "--json"}, func() json.RawMessage {
			var r json.RawMessage
			_ = raw.Call(protocol.MethodTemplateList, protocol.TemplateListParams{}, &r)
			return r
		}},
	} {
		var out bytes.Buffer
		if err := dispatchForJSONProbe(&out, p.cmd); err != nil {
			t.Fatalf("%s --json: %v", p.name, err)
		}
		want := string(p.call()) + "\n"
		if out.String() != want {
			t.Errorf("%s --json printed a second formatter:\n CLI %s\n WIRE %s", p.name, out.String(), want)
		}
	}
}

func dispatchForJSONProbe(w io.Writer, args []string) error {
	switch args[0] {
	case "charter":
		return cmdCharter(w, args[1:])
	case "template":
		return cmdTemplate(w, args[1:])
	case "intake":
		return cmdIntake(w, args[1:])
	}
	return fmt.Errorf("no dispatcher for %q", args[0])
}

// ---------------------------------------------------------------------------
// two human goldens (output drift, not behaviour)
// ---------------------------------------------------------------------------

func TestCharterShowGolden(t *testing.T) {
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	var out bytes.Buffer
	if err := cmdCharter(&out, []string{"show", "root-1"}); err != nil {
		t.Fatalf("charter show: %v", err)
	}
	want := `root-1 · revision 2 of 3 · 2026-11-05T12:00:00Z
actor: user/uid-1000 · source: cli 
summary: ship the thing
body:
{"goal":"ship the thing","note":"why"}
`
	if out.String() != want {
		t.Errorf("charter show drifted:\n GOT %q\nWANT %q", out.String(), want)
	}
}

func TestIntakeStatusGolden(t *testing.T) {
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	var out bytes.Buffer
	if err := cmdIntake(&out, []string{"status", "int-aa"}); err != nil {
		t.Fatalf("intake status: %v", err)
	}
	want := `int-aa · state: interviewing
start: done
`
	if out.String() != want {
		t.Errorf("intake status drifted:\n GOT %q\nWANT %q", out.String(), want)
	}
}

// TestStartAbandonRoundTrip carries the row the door files: start prints
// the loop, abandon prints the close and the scratch promise.
func TestStartAbandonRoundTrip(t *testing.T) {
	ch, tp, ik := s61FixedFakes()
	standS61Socket(t, ch, tp, ik)
	var out bytes.Buffer
	if err := cmdIntake(&out, []string{"start", "--name", "ship the thing"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out.String(), "int-aa") || !strings.Contains(out.String(), "intake-aa") {
		t.Errorf("start printed neither the row nor the loop:\n%s", out.String())
	}
	out.Reset()
	if err := cmdIntake(&out, []string{"abandon", "int-aa", "--reason", "stalled"}); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	human := out.String()
	for _, want := range []string{"abandoned: intake int-aa", "closed", "rejected proposals: [9]", "stays for the person"} {
		if !strings.Contains(human, want) {
			t.Errorf("the abandon output lacks %q:\n%s", want, human)
		}
	}
	if ik.lastAband.Reason != "stalled" {
		t.Errorf("the reason must ride the door: %+v", ik.lastAband)
	}
}

// TestEveryIntakeMethodHasAVerb and TestEveryIntakeVerbHasAMethod are the
// spec's named halves of the parity law over the intake plane alone —
// TestS61VerbParityBothWays walks all three families; these two pin the
// seven-door intake face the wizard (T11) will drive, each direction
// under its own name.
func TestEveryIntakeMethodHasAVerb(t *testing.T) {
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	ch, tp, ik := s61FixedFakes()
	tbl.Charter, tbl.Templates, tbl.Intake = ch, tp, ik
	if !tbl.RegisterIntake() {
		t.Fatal("RegisterIntake refused over a wired seam")
	}
	seen := map[protocol.MethodName]int{}
	for _, v := range intakeVerbs {
		seen[v.method]++
	}
	n := 0
	for _, m := range tbl.Methods() {
		if !strings.HasPrefix(string(m), "intake.") {
			continue
		}
		n++
		if seen[m] != 1 {
			t.Errorf("%s covered by %d intake verbs — the wizard needs exactly one door each", m, seen[m])
		}
	}
	if n != len(intakeVerbs) {
		t.Errorf("the intake plane registers %d doors, the fixture lists %d verbs", n, len(intakeVerbs))
	}
}

func TestEveryIntakeVerbHasAMethod(t *testing.T) {
	called := walkMethodSelectors(t, "intake.go", "MethodIntake")
	have := map[string]bool{}
	for _, c := range called {
		have[c] = true
	}
	for _, v := range intakeVerbs {
		if !have[s61ConstFor(v.method)] {
			t.Errorf("`intake %s` dispatches to nothing — intake.go never calls %s", v.verb, s61ConstFor(v.method))
		}
	}
	if len(intakeVerbs) != 7 {
		t.Errorf("the intake fixture lists %d verbs, the face is seven", len(intakeVerbs))
	}
}

// TestBareAeonPrintsUsageWithoutPanic — the S61 T12 demo walk found it:
// `aeon` with NO arguments sliced os.Args[2:] while the no-args case was
// still guarded by the verb branch, and the real binary panicked
// (slice bounds [2:1]) where it owes the person its usage. A finding of
// the walk, fixed with the walk that found it.
func TestBareAeonPrintsUsageWithoutPanic(t *testing.T) {
	out, err := exec.Command(aeonBinary).CombinedOutput()
	if err != nil {
		t.Fatalf("bare `aeon` exited %v — usage is an answer, not a crash: %s", err, out)
	}
	if strings.Contains(string(out), "panic") {
		t.Fatalf("bare `aeon` panicked:\n%s", out)
	}
	if !strings.Contains(string(out), "Usage:") {
		t.Errorf("bare `aeon` printed no usage — a silent CLI is a broken CLI:\n%.200s", out)
	}
}

// TestPreviewPrintsTheCommandsNotJustTheFiles — GI7's preview shows what
// RUNS, not only what lands. The S61 T12 walk found the human renderer
// printing files and the digest but never the git commands the wire
// already carried as `actions`; this pins they stay visible.
func TestPreviewPrintsTheCommandsNotJustTheFiles(t *testing.T) {
	var buf bytes.Buffer
	renderIntakePreview(&buf, protocol.IntakePreview{
		IntakeID: "int-1", ProposalID: "7", Publish: "none", TargetPath: "/p/shop",
		Digest: "sha256:abc",
		Components: []protocol.IntakePreviewComponent{{
			Slug: "shop", Role: "root", Name: "Shop", TargetPath: "/p/shop/shop",
			Blueprint: "base@1", Files: []string{"README.md"},
			Actions: []string{"git init -b main /p/shop/shop", "git -C /p/shop/shop add -A"},
		}},
	})
	got := buf.String()
	for _, want := range []string{"README.md", "$ git init -b main", "$ git -C", "digest: sha256:abc"} {
		if !strings.Contains(got, want) {
			t.Errorf("preview lost %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "$ git") > strings.Index(got, "digest:") {
		t.Errorf("the digest must stay LAST — commit carries it:\n%s", got)
	}
}
