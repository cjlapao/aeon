package main

// gate_verbs_test.go — S57 T10 (VG20): the gate verbs' behaviour over
// a fake door: the three dial outcomes, try's offline refusal, JSON as
// the wire bytes, the loosening seatbelt, verbatim refusals, tails
// behind --show-output, and the note printed as the daemon stamped it.
// The parity table and the parser guards are in gate_parity_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// fakeGateDoor is the whole door interface with canned answers and
// call counts — the same stand-in the seed/autonomy parity tests use.
type fakeGateDoor struct {
	calls map[string]int
	err   error // when set, EVERY call returns it (the refusal pass-through case)

	listRes     protocol.VerifyListResult
	showRes     protocol.VerifyShowResult
	runsRes     protocol.VerifyRunsResult
	runShowRes  protocol.VerifyRunShowResult
	vocabRes    protocol.VerifyVocabularyResult
	templatesR  protocol.VerifyTemplatesResult
	writeRes    protocol.VerifyWriteResult
	applyRes    protocol.VerifyApplyResult
	tryRes      protocol.VerifyTryResult
	mrGetRes    protocol.MergeRuleGetResult
	mrPrevRes   protocol.MergeRulePreviewResult
	mrChgRes    protocol.MergeRuleChangesResult
	mrVocRes    protocol.MergeRuleVocabularyResult
	mrSetRes    protocol.MergeRuleSetResult
	eventsRes   protocol.MergeEventsResult
	watchesRes  protocol.MergeWatchesResult
	dodListRes  protocol.DoDListResult
	dodMarkRes  protocol.DoDWriteResult
	lastSet     protocol.MergeRuleSetInput
	lastMark    protocol.DoDMarkInput
	lastTryComp string
}

func newFakeGateDoor() *fakeGateDoor {
	return &fakeGateDoor{calls: map[string]int{}}
}

func (f *fakeGateDoor) Close() error { return nil }

func (f *fakeGateDoor) hit(name string) error {
	f.calls[name]++
	return f.err
}

func (f *fakeGateDoor) VerifyList(context.Context, protocol.VerifyListQuery) (protocol.VerifyListResult, error) {
	return f.listRes, f.hit("VerifyList")
}
func (f *fakeGateDoor) VerifyShow(context.Context, string) (protocol.VerifyShowResult, error) {
	return f.showRes, f.hit("VerifyShow")
}
func (f *fakeGateDoor) VerifyRuns(context.Context, protocol.VerifyRunsQuery) (protocol.VerifyRunsResult, error) {
	return f.runsRes, f.hit("VerifyRuns")
}
func (f *fakeGateDoor) VerifyRunShow(context.Context, string) (protocol.VerifyRunShowResult, error) {
	return f.runShowRes, f.hit("VerifyRunShow")
}
func (f *fakeGateDoor) VerifyVocabulary(context.Context) (protocol.VerifyVocabularyResult, error) {
	return f.vocabRes, f.hit("VerifyVocabulary")
}
func (f *fakeGateDoor) VerifyTemplates(context.Context) (protocol.VerifyTemplatesResult, error) {
	return f.templatesR, f.hit("VerifyTemplates")
}
func (f *fakeGateDoor) VerifyAdd(context.Context, protocol.VerifyAddInput) (protocol.VerifyWriteResult, error) {
	return f.writeRes, f.hit("VerifyAdd")
}
func (f *fakeGateDoor) VerifyUpdate(context.Context, protocol.VerifyUpdateInput) (protocol.VerifyWriteResult, error) {
	return f.writeRes, f.hit("VerifyUpdate")
}
func (f *fakeGateDoor) VerifyArchive(context.Context, string) (protocol.VerifyWriteResult, error) {
	return f.writeRes, f.hit("VerifyArchive")
}
func (f *fakeGateDoor) VerifyRestore(context.Context, string) (protocol.VerifyWriteResult, error) {
	return f.writeRes, f.hit("VerifyRestore")
}
func (f *fakeGateDoor) VerifyApplyTemplate(context.Context, string, []string) (protocol.VerifyApplyResult, error) {
	return f.applyRes, f.hit("VerifyApplyTemplate")
}
func (f *fakeGateDoor) VerifyTry(_ context.Context, componentID, _ string) (protocol.VerifyTryResult, error) {
	f.lastTryComp = componentID
	return f.tryRes, f.hit("VerifyTry")
}
func (f *fakeGateDoor) MergeRuleGet(context.Context, string, bool) (protocol.MergeRuleGetResult, error) {
	return f.mrGetRes, f.hit("MergeRuleGet")
}
func (f *fakeGateDoor) MergeRulePreview(context.Context, string, int) (protocol.MergeRulePreviewResult, error) {
	return f.mrPrevRes, f.hit("MergeRulePreview")
}
func (f *fakeGateDoor) MergeRuleChanges(context.Context, string, int) (protocol.MergeRuleChangesResult, error) {
	return f.mrChgRes, f.hit("MergeRuleChanges")
}
func (f *fakeGateDoor) MergeRuleVocabulary(context.Context) (protocol.MergeRuleVocabularyResult, error) {
	return f.mrVocRes, f.hit("MergeRuleVocabulary")
}
func (f *fakeGateDoor) MergeRuleSet(_ context.Context, in protocol.MergeRuleSetInput) (protocol.MergeRuleSetResult, error) {
	f.lastSet = in
	return f.mrSetRes, f.hit("MergeRuleSet")
}
func (f *fakeGateDoor) MergeRuleClear(context.Context, string) (protocol.MergeRuleSetResult, error) {
	return f.mrSetRes, f.hit("MergeRuleClear")
}
func (f *fakeGateDoor) MergeEvents(context.Context, protocol.MergeEventsQuery) (protocol.MergeEventsResult, error) {
	return f.eventsRes, f.hit("MergeEvents")
}
func (f *fakeGateDoor) MergeWatches(context.Context, protocol.MergeWatchesQuery) (protocol.MergeWatchesResult, error) {
	return f.watchesRes, f.hit("MergeWatches")
}
func (f *fakeGateDoor) DoDList(context.Context, string) (protocol.DoDListResult, error) {
	return f.dodListRes, f.hit("DoDList")
}
func (f *fakeGateDoor) DoDMark(_ context.Context, in protocol.DoDMarkInput) (protocol.DoDWriteResult, error) {
	f.lastMark = in
	return f.dodMarkRes, f.hit("DoDMark")
}

// gateStderr swaps the package's stderr seam for the duration of the
// test and returns the buffer the fixed lines landed in.
func gateStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := stderrW
	stderrW = buf
	t.Cleanup(func() { stderrW = old })
	return buf
}

// gateFake installs fake as the door both selection seams hand out
// (socket path and try path), so the verbs speak the fake and no real
// dial, fallback, or store is ever touched.
func gateFake(t *testing.T, f *fakeGateDoor) *bytes.Buffer {
	t.Helper()
	stderr := gateStderr(t)
	oldSelect, oldTry, oldDirect := gateSelect, gateSelectTry, newGateDirectDoor
	t.Cleanup(func() { gateSelect, gateSelectTry, newGateDirectDoor = oldSelect, oldTry, oldDirect })
	gateSelect = func() (gateDoor, error) { return f, nil }
	gateSelectTry = func() (gateDoor, error) { return f, nil }
	newGateDirectDoor = func() (gateDoor, error) {
		t.Errorf("the direct door was built while the test held the door seam")
		return nil, errors.New("no")
	}
	return stderr
}

func TestGateSelectDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	oldDial, oldDirect := gateDial, newGateDirectDoor
	t.Cleanup(func() { gateDial, newGateDirectDoor = oldDial, oldDirect })
	fake := newFakeGateDoor()
	newGateDirectDoor = func() (gateDoor, error) { return fake, nil }

	// 1. no daemon ⇒ the direct door + the fixed line, on stderr. The
	// fault is the real dial's shape (noDaemonError over ENOENT —
	// nobodyIsHome reads the wrapper and the errno, not prose).
	stderr := gateStderr(t)
	gateDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/x.sock", cause: os.ErrNotExist}
	}
	door, err := selectGateDoor()
	if err != nil {
		t.Fatalf("no daemon should fall back, not refuse: %v", err)
	}
	if door != gateDoor(fake) {
		t.Error("no daemon should yield the direct door")
	}
	if stderr.String() != gateDirectLine+"\n" {
		t.Errorf("the fallback printed %q, want the pinned line %q", stderr.String(), gateDirectLine)
	}

	// 2. a permission fault ⇒ refuse outright, no fallback, no line.
	stderr.Reset()
	gateDial = func(int) (*protocol.Client, error) { return nil, &permissionFault{} }
	if _, err := selectGateDoor(); err == nil {
		t.Error("a permission fault should refuse, not fall back")
	}
	if stderr.Len() != 0 {
		t.Errorf("the fallback line printed for a permission fault — it is a no-daemon sentence only")
	}

	// 3. a version fault (a daemon that exists) ⇒ same refusal.
	stderr.Reset()
	gateDial = func(int) (*protocol.Client, error) {
		return nil, fmt.Errorf("protocol: the daemon's protocol version 2 is not compatible with this client's 1")
	}
	if _, err := selectGateDoor(); err == nil {
		t.Error("a version fault should refuse, not fall back")
	}
	if stderr.Len() != 0 {
		t.Error("the fallback line printed for a version fault")
	}

	// 4. a successful dial ⇒ the socket door, no line.
	stderr.Reset()
	gateDial = func(int) (*protocol.Client, error) { return nil, nil }
	d2, err := selectGateDoor()
	if err != nil {
		t.Fatalf("success should yield the socket door: %v", err)
	}
	if _, ok := d2.(*gateSocketDoor); !ok {
		t.Errorf("success yielded %T, want *gateSocketDoor", d2)
	}
	if stderr.Len() != 0 {
		t.Error("the fallback line printed on the socket path")
	}
}

func TestGateDirectLineIsPinned(t *testing.T) {
	const want = "daemon not running — applied directly to the store"
	if gateDirectLine != want {
		t.Errorf("the direct-door sentence drifted: %q, want %q (the words are the shared contract across the backup/autonomy/gate doors)", gateDirectLine, want)
	}
}

func TestVerifyTryRefusesOffline(t *testing.T) {
	oldDial, oldDirect := gateDial, newGateDirectDoor
	t.Cleanup(func() { gateDial, newGateDirectDoor = oldDial, oldDirect })
	directBuilt := 0
	newGateDirectDoor = func() (gateDoor, error) {
		directBuilt++
		return newFakeGateDoor(), nil
	}
	gateDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/x.sock", cause: os.ErrNotExist}
	}
	stderr := gateStderr(t)
	var out bytes.Buffer
	err := cmdVerify(&out, []string{"try", "gate-1"})
	const wantSentence = "verify try needs the daemon: it runs under the daemon's policy gate — start it with aeon serve"
	if err == nil || err.Error() != wantSentence {
		t.Fatalf("offline try answered %v — want the fixed sentence byte-for-byte", err)
	}
	if directBuilt != 0 {
		t.Errorf("offline try opened the store %d time(s) — try has NO direct door", directBuilt)
	}
	if stderr.Len() != 0 {
		t.Errorf("offline try printed %q — the direct-door line belongs to verbs that DO have a direct door", stderr.String())
	}
}

func TestCLIJSONIsTheWire(t *testing.T) {
	f := newFakeGateDoor()
	f.listRes = protocol.VerifyListResult{ComponentID: "c", Gates: []protocol.VerifyGateView{
		{ID: "g1", ComponentID: "c", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 600},
	}}
	f.vocabRes = protocol.VerifyVocabularyResult{
		Requirements: protocol.VerifyRequirementValues, States: protocol.VerifyStateValues,
		Reasons: protocol.VerifyReasonValues, EnvAllowlist: []string{"PATH", "HOME"},
		TryStatement: "the verifier runs the gate",
	}
	gateFake(t, f)

	var out bytes.Buffer
	if err := runVerifyList(&out, []string{"--component", "c", "--json"}); err != nil {
		t.Fatal(err)
	}
	want := &bytes.Buffer{}
	if err := printJSON(want, f.listRes); err != nil {
		t.Fatal(err)
	}
	if out.String() != want.String() {
		t.Errorf("--json drifted from the door's bytes:\n got: %s\nwant: %s", out.String(), want.String())
	}

	out.Reset()
	if err := runVerifyVocabulary(&out, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	// and the bytes ARE the result's own JSON (a shape the app parses).
	var round protocol.VerifyVocabularyResult
	if err := json.Unmarshal(out.Bytes(), &round); err != nil {
		t.Fatalf("--json is not the wire: %v", err)
	}
	if round.TryStatement != f.vocabRes.TryStatement || len(round.EnvAllowlist) != 2 {
		t.Errorf("--json round-trip lost the answer: %+v", round)
	}
}

func TestMergeRuleSetNeedsYesWhenLoosening(t *testing.T) {
	f := newFakeGateDoor()
	standing := protocol.MergeRuleWire{
		Enabled: true, RequireLocal: true, RequireDoD: "all_met",
		ChecksPolicy: "required", NoChecks: "refuse", ReviewMode: "either", Method: "squash",
	}
	f.mrGetRes = protocol.MergeRuleGetResult{
		ComponentID: "c", Source: "own",
		Rule: &protocol.MergeRuleView{ComponentID: "c", MergeRuleWire: standing, Revision: 3},
	}
	stderr := gateFake(t, f)

	// loosening (all_met → none) WITHOUT --yes: nothing is sent, the
	// classifier's own field name is printed, exit is the usage exit.
	var out bytes.Buffer
	err := runMergeRuleSet(&out, []string{"--component", "c", "--require-dod", "none"})
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("a loosening set without --yes answered %v, want a usage refusal", err)
	}
	if f.calls["MergeRuleSet"] != 0 {
		t.Error("the loosening set was SENT without --yes — the seatbelt is a suggestion then, not a seatbelt")
	}
	if !strings.Contains(stderr.String(), "RequireDoD") || !strings.Contains(stderr.String(), "--yes") {
		t.Errorf("the seatbelt printed %q — want the classifier's field names and the flag that answers it", stderr.String())
	}

	// the same loosening WITH --yes: sent, and the wire carries the
	// whole rule (the effective base plus the one override).
	stderr.Reset()
	out.Reset()
	if err := runMergeRuleSet(&out, []string{"--component", "c", "--require-dod", "none", "--yes"}); err != nil {
		t.Fatalf("--yes should send it: %v", err)
	}
	if f.calls["MergeRuleSet"] != 1 || f.lastSet.RequireDoD != "none" || !f.lastSet.RequireLocal || f.lastSet.NoChecks != "refuse" {
		t.Errorf("the sent rule lost its base: %+v", f.lastSet)
	}

	// a TIGHTENING set (none → all_met on top of none) needs no flag at
	// all: the seatbelt binds loosening only.
	f.mrGetRes.Rule.MergeRuleWire.RequireDoD = "none"
	f.calls = map[string]int{}
	out.Reset()
	if err := runMergeRuleSet(&out, []string{"--component", "c", "--require-dod", "all_met"}); err != nil {
		t.Fatalf("a tightening without --yes should just go: %v", err)
	}
	if f.calls["MergeRuleSet"] != 1 {
		t.Error("a tightening was held up by the loosening seatbelt")
	}

	// the incomparable pair: forge ↔ aeon answers to --yes too.
	f.mrGetRes.Rule.MergeRuleWire.RequireDoD = "all_met"
	f.mrGetRes.Rule.MergeRuleWire.ReviewMode = "forge"
	f.calls = map[string]int{}
	stderr.Reset()
	err = runMergeRuleSet(&out, []string{"--component", "c", "--review-mode", "aeon"})
	if !errors.As(err, &ue) || f.calls["MergeRuleSet"] != 0 {
		t.Fatalf("the review-mode switch answered %v with %d sends — the incomparable pair needs --yes too", err, f.calls["MergeRuleSet"])
	}
}

func TestCLIRefusalsVerbatim(t *testing.T) {
	// the door's refusal — whatever produced it — must reach main's
	// renderError untouched: the CLI adds no wrapper, no re-wording.
	f := newFakeGateDoor()
	refusal := protocol.ErrVerifyRefused.WithWhat("verify archive refused: %s", "gate g1 is the last required gate on c")
	refusal.Data.Kind = "last-required-gate"
	f.err = refusal
	gateFake(t, f)
	var out bytes.Buffer
	err := cmdVerify(&out, []string{"archive", "g1", "--yes"})
	if err != refusal {
		t.Errorf("the CLI re-wrapped the refusal: %v (%T), want the door's own error instance", err, err)
	}
	// renderError (main's renderer) says the sentence, not a wrapper.
	rendered := &bytes.Buffer{}
	renderError(rendered, err)
	if !strings.Contains(rendered.String(), "the last required gate") {
		t.Errorf("renderError lost the daemon's sentence: %q", rendered.String())
	}
}

func TestNoTailWithoutShowOutput(t *testing.T) {
	exit := 1
	f := newFakeGateDoor()
	f.runShowRes = protocol.VerifyRunShowResult{
		Run: protocol.VerifyRunView{ID: "r1", State: "fail", StartedAt: "now"},
		Results: []protocol.VerifyRunResultView{{
			Seq: 1, Name: "test", State: "fail", ExitCode: &exit, WallMS: 12,
			StdoutTail: "secret-tail-line", StderrTail: "secret-err-line",
		}},
	}
	gateFake(t, f)

	var out bytes.Buffer
	if err := runVerifyRunShow(&out, []string{"r1"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "secret-tail-line") || strings.Contains(out.String(), "secret-err-line") {
		t.Errorf("tails printed WITHOUT --show-output — that flag is the whole point:\n%s", out.String())
	}
	if strings.Contains(out.String(), gateTailsSecretLine) {
		t.Error("the may-contain-secrets line printed when no tails printed")
	}

	out.Reset()
	if err := runVerifyRunShow(&out, []string{"r1", "--show-output"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"secret-tail-line", "secret-err-line", gateTailsSecretLine} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("--show-output lost %q:\n%s", want, out.String())
		}
	}
}

func TestSecretWarningLineIsPinned(t *testing.T) {
	if !strings.Contains(gateTailsSecretLine, "may contain secrets") {
		t.Errorf("the tails warning drifted from the spec's pinned phrase: %q", gateTailsSecretLine)
	}
}

func TestScrubbedEnvironmentNoteIsTheDaemonsVerbatim(t *testing.T) {
	f := newFakeGateDoor()
	f.writeRes = protocol.VerifyWriteResult{
		Gate: protocol.VerifyGateView{ID: "g1", ComponentID: "c", Name: "gh", Command: "gh --token ghp_x", Requirement: "required"},
		Note: protocol.VerifyEnvNoteSentence,
	}
	stderr := gateFake(t, f)
	var out bytes.Buffer
	if err := runVerifyAdd(&out, []string{"--component", "c", "--name", "gh", "--command", "gh --token ghp_x", "--requirement", "required"}); err != nil {
		t.Fatal(err)
	}
	if stderr.String() != protocol.VerifyEnvNoteSentence+"\n" {
		t.Errorf("the note printed %q, want the daemon's sentence alone on stderr", stderr.String())
	}
	// the CLI never RE-READS the command to decide — with an empty
	// Note (the daemon said nothing), nothing prints, however shaped
	// the command looked to us.
	f.writeRes.Note = ""
	stderr.Reset()
	out.Reset()
	if err := runVerifyAdd(&out, []string{"--component", "c", "--name", "gh", "--command", "gh --token ghp_x", "--requirement", "required"}); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Errorf("the CLI decided to warn on its own: %q — the daemon owns the note", stderr.String())
	}
}

func TestArchiveNeedsYesClearNeedsYes(t *testing.T) {
	f := newFakeGateDoor()
	gateFake(t, f)
	var out bytes.Buffer
	if err := cmdVerify(&out, []string{"archive", "g1"}); err == nil {
		t.Error("archive without --yes should refuse")
	}
	if f.calls["VerifyArchive"] != 0 {
		t.Error("archive without --yes reached the door")
	}
	if err := cmdMergeRule(&out, []string{"clear", "--component", "c"}); err == nil {
		t.Error("clear without --yes should refuse")
	}
	if f.calls["MergeRuleClear"] != 0 {
		t.Error("clear without --yes reached the door")
	}
}

func TestDodMarkFlagShapes(t *testing.T) {
	f := newFakeGateDoor()
	gateFake(t, f)
	var out bytes.Buffer
	// two evidences named at once is an ambiguous request.
	if err := cmdDoD(&out, []string{"mark", "i1", "--state", "met", "--evidence-run", "r1", "--manual"}); err == nil {
		t.Error("two evidences should refuse")
	}
	if f.calls["DoDMark"] != 0 {
		t.Error("the ambiguous mark reached the door")
	}
	// a run evidence rides as {run, ref}.
	if err := cmdDoD(&out, []string{"mark", "i1", "--state", "met", "--evidence-run", "r9"}); err != nil {
		t.Fatal(err)
	}
	if f.lastMark.Evidence == nil || f.lastMark.Evidence.Kind != "run" || f.lastMark.Evidence.Ref != "r9" {
		t.Errorf("run evidence rode as %+v", f.lastMark.Evidence)
	}
	// a manual evidence rides as {manual, note}.
	if err := cmdDoD(&out, []string{"mark", "i2", "--state", "met", "--manual", "--note", "walked the demo"}); err != nil {
		t.Fatal(err)
	}
	if f.lastMark.Evidence == nil || f.lastMark.Evidence.Kind != "manual" || f.lastMark.Evidence.Note != "walked the demo" {
		t.Errorf("manual evidence rode as %+v", f.lastMark.Evidence)
	}
	// the CLI does NOT police which states need evidence — the daemon
	// does, verbatim: an unmet mark with no evidence is SENT.
	if err := cmdDoD(&out, []string{"mark", "i3", "--state", "unmet"}); err != nil {
		t.Fatalf("unmet without evidence should reach the daemon: %v", err)
	}
	if f.lastMark.Evidence != nil {
		t.Error("the CLI invented evidence")
	}
}

func TestTryNamesTheGatesOwnComponent(t *testing.T) {
	// `try ID` carries only the gate id; the wire names the gate's own
	// component, and the CLI gets it from the daemon's show — a data
	// hand-off, never a guess.
	f := newFakeGateDoor()
	f.showRes = protocol.VerifyShowResult{Gate: protocol.VerifyGateView{ID: "g7", ComponentID: "comp-9"}}
	gateFake(t, f)
	var out bytes.Buffer
	if err := cmdVerify(&out, []string{"try", "g7"}); err != nil {
		t.Fatal(err)
	}
	if f.lastTryComp != "comp-9" || f.calls["VerifyTry"] != 1 {
		t.Errorf("try sent component %q (sends %d) — want the gate's own component from the daemon's show", f.lastTryComp, f.calls["VerifyTry"])
	}
}

// TestActorIsCliOnTheDirectDoorOnly — parsed, not trusted: the fixed
// {cli, cli} actor literal exists exactly once, inside the direct
// door's constructor, and no socket-door method names an actor at all
// (the socket's actor is the daemon's peer, stamped server-side).
func TestActorIsCliOnTheDirectDoorOnly(t *testing.T) {
	src, err := os.ReadFile("gate_client.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if n := strings.Count(text, `Kind: "cli"`); n != 1 {
		t.Errorf("the {cli, cli} actor literal appears %d time(s) in gate_client.go — want exactly one (openGateDirectDoor's stamp)", n)
	}
	if !strings.Contains(text, `actor: protocol.Actor{Kind: "cli", ID: "cli"}`) {
		t.Error("the direct door's actor literal drifted from {Kind: \"cli\", ID: \"cli\"}")
	}
	// the socket door's methods must not speak protocol.Actor at all.
	f := parseGateFile(t, "gate_client.go")
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		rt := fd.Recv.List[0].Type
		if st, ok := rt.(*ast.StarExpr); ok {
			rt = st.X
		}
		if id, ok := rt.(*ast.Ident); !ok || id.Name != "gateSocketDoor" {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if cl, ok := n.(*ast.CompositeLit); ok {
				if sel, ok := cl.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Actor" {
					t.Errorf("%s constructs an Actor — the socket door sends no identity (the daemon stamps the peer)", fd.Name.Name)
				}
			}
			return true
		})
	}
}

var initialStderr = os.Stderr

// TestGateStderrSeamDefaultsToOSErrorr guards the seam itself: the
// package's stderr is os.Stderr, not a silently dead writer.
func TestGateStderrSeamDefaultsToOSStderr(t *testing.T) {
	old := stderrW
	t.Cleanup(func() { stderrW = old })
	// Compared with the process's stderr as it was at package init, which is
	// when stderrW took its default — os.Stderr itself may be swapped for a
	// pipe by a capture helper while this runs.
	if stderrW != io.Writer(initialStderr) {
		t.Errorf("stderrW defaults to %T, want *os.File (os.Stderr)", stderrW)
	}
}

// keep json used even when subtests that need it are edited
var _ = json.Marshal
