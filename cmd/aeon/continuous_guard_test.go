package main

// continuous_guard_test.go — S27 T7: the scheduler's human-door guards.
// New siblings; NO existing guard body edited (S45 T6 / S35 T6 / S62 T6's
// method — the pre-existing family in policy_amend_test.go and the S40
// AU1 lock tests stays byte-identical, and this file leans on their
// helpers, forbiddenAgentSurfaces first).
//
// The boundary in one sentence: the scheduler's rows are AUTHORITY records
// — the master, the pause, and one template row per repository — so every
// control is a socket method on the human half (0600 + peer credentials,
// P26), the actor is the CONNECTION's person and never a param (CN2), and
// none of the twelve names reaches the narrow projection, the agent
// services, the tool registry or the records catalog; the reads the agent
// surface may one day twin (CN20) still never twin a CONTROL. The two
// writer tests close the other half: the rows have no non-person writer,
// and only the worker's own start path stamps scheduler provenance.

import (
	"context"
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

// schedulerControlMethods are the eight human doors; schedulerReadMethods
// the four reads. Together the plane's exact twelve (T7's table).
var schedulerControlMethods = []protocol.MethodName{
	protocol.MethodSchedulerEnable, protocol.MethodSchedulerDisable,
	protocol.MethodSchedulerPause, protocol.MethodSchedulerResume,
	protocol.MethodSchedulerRootSet, protocol.MethodSchedulerRootDisable,
	protocol.MethodSchedulerEnableTree, protocol.MethodSchedulerStopAll,
}

var schedulerReadMethods = []protocol.MethodName{
	protocol.MethodSchedulerStatus, protocol.MethodSchedulerPreview,
	protocol.MethodSchedulerDecisions, protocol.MethodSchedulerAttention,
}

func schedulerAllMethods() []protocol.MethodName {
	return append(append([]protocol.MethodName{}, schedulerReadMethods...), schedulerControlMethods...)
}

// schedulerControlWireNames / schedulerControlConstants: the wire names and
// the Go constants of the CONTROLS — the literals the four caller-class
// directories may never carry (the reads twin elsewhere when their tasks
// say so; the controls twin NOWHERE).
var schedulerControlWireNames = []string{
	`"scheduler.enable"`, `"scheduler.disable"`, `"scheduler.pause"`, `"scheduler.resume"`,
	`"scheduler.root.set"`, `"scheduler.root.disable"`, `"scheduler.enableTree"`, `"scheduler.stopAll"`,
}

var schedulerControlConstants = []string{
	"MethodSchedulerEnable", "MethodSchedulerDisable", "MethodSchedulerPause", "MethodSchedulerResume",
	"MethodSchedulerRootSet", "MethodSchedulerRootDisable", "MethodSchedulerEnableTree", "MethodSchedulerStopAll",
}

func schedulerControlLiteralInFile(src string) []string {
	var bad []string
	for _, p := range append(append([]string{}, schedulerControlWireNames...), schedulerControlConstants...) {
		if strings.Contains(src, p) {
			bad = append(bad, p)
		}
	}
	return bad
}

// schedulerToolNameViolates is the registry rule (T7 §2): no tool named for
// a control. Reads are not in this regex — CN20's read tools, whenever they
// register, are a different family name.
var schedulerToolControlRE = regexp.MustCompile(`scheduler_(enable|disable|pause|resume|root|stop)`)

func schedulerToolNameViolates(name string) bool {
	return schedulerToolControlRE.MatchString(name)
}

// ─── the predicates: the walks and the canaries run the same functions ──────

func narrowProjectingScheduler(actions []narrow.Action, forbidden []protocol.MethodName) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range forbidden {
			if a.SocketMethod == m {
				bad = append(bad, a.Name)
			}
		}
	}
	return bad
}

func agentServicesNamingScheduler(ms []protocol.AgentServiceMethod, forbidden []protocol.MethodName) []string {
	var bad []string
	for _, m := range ms {
		for _, f := range forbidden {
			if m.Name == f {
				bad = append(bad, string(m.Name))
			}
		}
	}
	return bad
}

func boundaryStatementNamingScheduler(stmt string) []string {
	var bad []string
	for _, m := range schedulerControlWireNames {
		w := strings.Trim(m, `"`)
		if strings.Contains(stmt, w) {
			bad = append(bad, w)
		}
	}
	return bad
}

func catalogSpecsNamingSchedulerControl(specs []records.Spec) []string {
	var bad []string
	for _, sp := range specs {
		for _, m := range schedulerControlWireNames {
			if sp.Method == strings.Trim(m, `"`) {
				bad = append(bad, sp.Name+" → "+sp.Method)
			}
		}
		if sp.Family == "scheduler" && sp.Mutates {
			bad = append(bad, sp.Name+" (mutates)")
		}
	}
	return bad
}

// ─── the seam marker ─────────────────────────────────────────────────────────

// guardContinuousService is a registration marker, the guardAuthoringService
// shape: enough of ContinuousAPI to register, nothing to dispatch.
type guardContinuousService struct{}

func (guardContinuousService) Status(context.Context) (protocol.ContinuousStatusView, error) {
	return protocol.ContinuousStatusView{}, nil
}
func (guardContinuousService) Preview(context.Context, string) (protocol.ContinuousPreview, error) {
	return protocol.ContinuousPreview{}, nil
}
func (guardContinuousService) Decisions(context.Context, protocol.ContinuousDecisionQuery) ([]protocol.ContinuousDecisionRow, error) {
	return nil, nil
}
func (guardContinuousService) Attention(context.Context, string) (protocol.ContinuousAttention, error) {
	return protocol.ContinuousAttention{}, nil
}
func (guardContinuousService) SetMaster(context.Context, protocol.ContinuousActor, bool) (*protocol.ContinuousRefusal, error) {
	return nil, nil
}
func (guardContinuousService) SetPaused(context.Context, protocol.ContinuousActor, bool, string) (*protocol.ContinuousRefusal, error) {
	return nil, nil
}
func (guardContinuousService) SetRoot(context.Context, protocol.ContinuousActor, protocol.ContinuousRootInput) (protocol.ContinuousRepoView, *protocol.ContinuousRefusal, error) {
	return protocol.ContinuousRepoView{}, nil, nil
}
func (guardContinuousService) DisableRoot(context.Context, protocol.ContinuousActor, string) (*protocol.ContinuousRefusal, error) {
	return nil, nil
}
func (guardContinuousService) EnableTree(context.Context, protocol.ContinuousActor, string, protocol.ContinuousRootInput) ([]string, *protocol.ContinuousRefusal, error) {
	return nil, nil, nil
}
func (guardContinuousService) StopAll(context.Context, protocol.ContinuousActor, string) (protocol.ContinuousStopAllResult, *protocol.ContinuousRefusal, error) {
	return protocol.ContinuousStopAllResult{}, nil, nil
}

func guardContinuousTable(t *testing.T, wired bool) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if wired {
		tbl.Continuous = guardContinuousService{}
		if !tbl.RegisterContinuous() {
			t.Fatal("RegisterContinuous refused a non-nil seam — the guard would test a table without its doors")
		}
	}
	return tbl
}

// ─── the positive half ───────────────────────────────────────────────────────

// TestSchedulerControlsAreAHumanDoor states the rule positively (T7 §1):
// the exact twelve are registered with the service wired, each
// RequiresInitialize, none Debug, the eight controls classified
// HumanDoorOnly — and with the service nil every one of the twelve,
// scheduler.stopAll included, is ABSENT: Lookup fails and Dispatch answers
// ErrUnknownMethod (V13 — never registered-and-refusing).
func TestSchedulerControlsAreAHumanDoor(t *testing.T) {
	tbl := guardContinuousTable(t, true)
	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	for _, m := range schedulerAllMethods() {
		if !registered[m] {
			t.Fatalf("%s is not registered with its service wired — the app has its door (P34), and the guard's table says it does not", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s vanished between Methods() and Lookup()", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize — a scheduler control on an unnegotiated connection is unthinkable (CN18)", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — these are product methods; the exclusion must never rest on a Debug flag", m)
		}
	}
	for _, m := range schedulerControlMethods {
		if s, ok := protocol.SurfaceClassified(m); !ok || !s.HumanDoorOnly {
			t.Errorf("%s is not classified HumanDoorOnly — an authority write without the person's door is an authority record nobody signed (CN2)", m)
		}
	}
	for _, m := range schedulerReadMethods {
		if s, ok := protocol.SurfaceClassified(m); !ok || s.HumanDoorOnly {
			t.Errorf("%s is not a plain classified read — it is the board's face, not a door", m)
		}
	}

	// V13 at the app level: the NIL seam leaves all twelve absent.
	bare := guardContinuousTable(t, false)
	if bare.RegisterContinuous() {
		t.Fatal("RegisterContinuous registered over a NIL seam — presence without its service is the disagreement V13 exists to name; scheduler.stopAll most of all (it has no S55 under it here)")
	}
	for _, m := range schedulerAllMethods() {
		if _, ok := bare.Lookup(m); ok {
			t.Errorf("%s is present on a table whose seam is nil (V13)", m)
		}
		_, err := bare.Dispatch(context.Background(), "guard-conn", m, nil)
		var unknown protocol.ErrUnknownMethod
		if !asErrUnknownMethod(err, &unknown) {
			t.Errorf("%s without its seam answered %v — absent is ErrUnknownMethod, not a refusal (V13)", m, err)
		}
	}
}

// asErrUnknownMethod is errors.As spelled out narrowly so this file owns
// its one import set; the sentinel is a value type.
func asErrUnknownMethod(err error, out *protocol.ErrUnknownMethod) bool {
	e, ok := err.(protocol.ErrUnknownMethod)
	if ok {
		*out = e
	}
	return ok
}

// ─── the negative half ───────────────────────────────────────────────────────

// TestSchedulerWritesNeverReachOtherSurfaces, one subtest per door (T7 §2)
// so a failure names WHICH surface moved: narrow's table AND its rendered
// boundary statement hold none of the twelve; the agent-services
// enumeration names no control; the tool registry matches no control name;
// the records catalog twins no control and holds no mutating scheduler
// spec; and no non-test file under the four caller-class directories even
// QUOTES a control's wire name or Go constant. The walks report their
// measured file counts and fail under the slice's floor of 50.
func TestSchedulerWritesNeverReachOtherSurfaces(t *testing.T) {
	all := schedulerAllMethods()

	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		if bad := narrowProjectingScheduler(actions, all); len(bad) != 0 {
			t.Errorf("the narrow surface projects the scheduler through %s — a key-holder's projection must not hand out the person's authority (CN2, P4)", strings.Join(bad, ", "))
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		if bad := boundaryStatementNamingScheduler(stmt); len(bad) != 0 {
			t.Errorf("the projection's boundary statement lists %s — a surface that DISPLAYS a door its table must not hold is a second truth", strings.Join(bad, ", "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := agentServicesNamingScheduler(ms, schedulerControlMethods); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying a scheduler control is the authority record writing itself (CN20 reads-only)", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		var bad []string
		for name := range reg {
			if schedulerToolNameViolates(name) {
				bad = append(bad, name)
			}
		}
		if len(bad) != 0 {
			t.Errorf("the tool registry carries %s — a tool that enables, pauses or stops the scheduler is a second door onto the human half (CN2, CN20)", strings.Join(bad, ", "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := catalogSpecsNamingSchedulerControl(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — the scheduler's family is READS only if it ever registers, and never twinned to a control (CN20)", strings.Join(bad, ", "))
		}
	})

	t.Run("TextGrep", func(t *testing.T) {
		var total int
		for _, dir := range forbiddenAgentSurfaces {
			root := filepath.Join("..", "..", dir)
			var seen int
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				if strings.HasSuffix(path, "_test.go") {
					return nil // the guards' own text is not the guarded text
				}
				seen++
				src, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				if bad := schedulerControlLiteralInFile(string(src)); len(bad) != 0 {
					t.Errorf("%s names %s — a projection or registry that NAMES a scheduler control, even in a comment, is the projection the rule forbids", filepath.ToSlash(path), strings.Join(bad, ", "))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if seen == 0 {
				t.Fatalf("%s: the walk saw no non-test files — the grep checked nothing", dir)
			}
			total += seen
			t.Logf("%s: %d non-test files", dir, seen)
		}
		if total < 50 {
			t.Fatalf("the four walks saw %d non-test files, under the slice's floor of 50 — the grep checked too little", total)
		}
		t.Logf("walked %d non-test files total (floor 50)", total)
	})
}

// ─── the writers ─────────────────────────────────────────────────────────────

// TestSchedulerRowsHaveNoNonPersonWriter (T7 §4, the AU1 sibling): every
// write to scheduler_state / scheduler_root goes through the store's three
// stamped entry points, and their ONLY non-test callers are the daemon's
// control handlers — the socket service and the S55-backed stopAll seam.
// No seed engine, trigger, event dispatcher, reactor or agent path may
// reach them: a scheduler that moves its own authority is the loop eating
// its tail (the actor-law CHECK refuses an agent kind in the row; this
// walk refuses it in the call graph).
func TestSchedulerRowsHaveNoNonPersonWriter(t *testing.T) {
	callers := map[string]string{
		// the store file OWNS the definitions.
		"internal/store/scheduler.go": "definition",
		// the socket's control handlers (T7), and the stopAll seam (T6)
		// — both take the actor from the connection's peer, neither can
		// be reached without it.
		"internal/daemon/continuous_socket.go": "control handler",
		"internal/daemon/continuous_seams.go":  "stopAll seam (S55-backed, T6)",
	}
	writers := []string{".SetSchedulerState(", ".UpsertSchedulerRoot(", ".DisableSchedulerRoot("}
	for _, dir := range []string{"internal", "cmd"} {
		root := filepath.Join("..", "..", dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel := relSlash(t, path)
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			s := string(src)
			for _, w := range writers {
				if strings.Contains(s, w) {
					if _, ok := callers[rel]; !ok {
						t.Errorf("%s calls %s — the scheduler's authority rows have exactly the person's doors as writers (CN2); this file is not one", rel, w)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestOnlyTheWorkerStartsWork (T7 §5): the start provenance the recovery
// and the ledger read back is authored at exactly one site — the worker's
// own start path. Anything else stamping `scheduler` provenance is a
// second starter wearing the scheduler's name.
func TestOnlyTheWorkerStartsWork(t *testing.T) {
	want := "internal/daemon/continuous_start.go"
	found := 0
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(src), `StartedBy: "scheduler"`) {
			found++
			if rel := relSlash(t, path); rel != want {
				t.Errorf("%s stamps StartedBy \"scheduler\" — only the worker's start path (%s) authors that provenance", rel, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatalf("found %d scheduler-provenance authors, want exactly 1 — a second starter is a second authority", found)
	}
}

// relSlash makes a walked path comparable to the allow-list's
// repository-relative form, regardless of the walker's prefix.
func relSlash(t *testing.T, path string) string {
	t.Helper()
	p := filepath.ToSlash(path)
	if i := strings.Index(p, "/internal/"); i >= 0 {
		return p[i+1:]
	}
	if i := strings.Index(p, "/cmd/"); i >= 0 {
		return p[i+1:]
	}
	t.Fatalf("walked path %s is outside internal/ and cmd/ — the walk root moved", path)
	return p
}

// ─── the canaries: a guard that cannot fail is not a guard ───────────────────

// TestSchedulerGuardsHaveTeeth feeds every negative predicate a synthetic
// violation and a clean input (T7 §3), as the walks use them — the same
// functions, never copies (S45's canary idiom).
func TestSchedulerGuardsHaveTeeth(t *testing.T) {
	// 1. The narrow projection.
	if got := narrowProjectingScheduler([]narrow.Action{
		{Name: "aeon_scheduler_enable", SocketMethod: protocol.MethodSchedulerEnable, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}, schedulerAllMethods()); len(got) == 0 {
		t.Fatal("an action projecting scheduler.enable was not flagged — the narrow predicate cannot fail")
	}
	if got := narrowProjectingScheduler([]narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}, schedulerAllMethods()); len(got) != 0 {
		t.Fatalf("a clean action was flagged: %v — the predicate flags everything, which guards nothing", got)
	}

	// 2. The agent-services enumeration.
	if got := agentServicesNamingScheduler([]protocol.AgentServiceMethod{
		{Name: protocol.MethodSchedulerPause},
	}, schedulerControlMethods); len(got) == 0 {
		t.Fatal("an agent service naming scheduler.pause was not flagged")
	}

	// 3. The boundary statement.
	if got := boundaryStatementNamingScheduler("this surface may call scheduler.enable and scheduler.stopAll"); len(got) != 2 {
		t.Fatalf("boundary statement flagged %v, want both control names", got)
	}
	if got := boundaryStatementNamingScheduler("this surface may call loop.list only"); len(got) != 0 {
		t.Fatalf("a clean statement flagged %v", got)
	}

	// 4. The tool registry name rule.
	for _, name := range []string{"scheduler_enable", "scheduler_disable", "scheduler_pause", "scheduler_resume", "scheduler_root_set", "scheduler_stop_all", "scheduler_stop"} {
		if !schedulerToolNameViolates(name) {
			t.Errorf("tool name %q not flagged — the regex must catch every control-shaped name", name)
		}
	}
	for _, name := range []string{"roadmap_list", "loop_stop_requests", "settings_get"} {
		if schedulerToolNameViolates(name) {
			t.Errorf("tool name %q flagged — the predicate flags unrelated names", name)
		}
	}

	// 5. The catalog rule.
	if got := catalogSpecsNamingSchedulerControl([]records.Spec{
		{Name: "scheduler_enable", Family: "scheduler", Method: "scheduler.enable", Mutates: true},
	}); len(got) == 0 {
		t.Fatal("a scheduler spec twinned to a control was not flagged")
	}
	if got := catalogSpecsNamingSchedulerControl([]records.Spec{
		{Name: "scheduler_status", Family: "scheduler", Method: "scheduler.status", Mutates: false},
	}); len(got) != 0 {
		t.Fatalf("a clean read spec was flagged: %v", got)
	}

	// 6. The literal walk: a planted file under a TEMPORARY tree shaped
	// like internal/tools names scheduler.pause — the predicate sees it;
	// the clean text does not trip it.
	dir := t.TempDir()
	planted := filepath.Join(dir, "internal", "tools")
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(planted, "scheduler.go")
	if err := os.WriteFile(bad, []byte("package tools\n\n// someday: call \"scheduler.pause\" from here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(bad)
	if err != nil {
		t.Fatal(err)
	}
	if got := schedulerControlLiteralInFile(string(src)); len(got) == 0 {
		t.Fatal("the planted file's text was not flagged — the literal predicate cannot fail")
	}
	if got := schedulerControlLiteralInFile("package tools\n\n// loop.list lives here\n"); len(got) != 0 {
		t.Fatal("clean text was flagged by the literal predicate")
	}
}

// schedulerAgentReads is the whole declared agent surface of the scheduler
// family — the four reads, and nothing else, forever.
var schedulerAgentReads = map[string]bool{
	"scheduler_status": true, "scheduler_preview": true,
	"scheduler_decisions": true, "scheduler_attention": true,
}

// TestNoSchedulerWriteToolExists is S27 T8's positive-side pin beside T7's
// negatives (the spec's name): no registry name matches a control verb, the
// catalog's scheduler family is exactly the four reads, every one class
// safe with Mutates false, and no spec's METHOD smuggles a control either.
// A fifth name, a control verb in a new disguise (enable_x, pause_repo,
// root_set, stop_all) or a Mutates:true on a read is an authority
// widening, and each lands here first.
func TestNoSchedulerWriteToolExists(t *testing.T) {
	// The registry: every scheduler-named tool is one of the four, and the
	// four are all there (recordTools' projection of the catalog, S27 T8).
	var regNames []string
	for name := range tools.Registry() {
		if strings.HasPrefix(name, "scheduler_") {
			regNames = append(regNames, name)
		}
	}
	if len(regNames) != 4 {
		t.Errorf("the registry holds scheduler tools %v — the family is exactly the four reads", regNames)
	}
	for _, name := range regNames {
		if !schedulerAgentReads[name] {
			t.Errorf("%s is an agent scheduler tool the slice did not declare — no control reaches the agent (S27 CN2)", name)
		}
		if schedulerToolNameViolates(name) {
			t.Errorf("%s names a control — T7's registry rule re-armed and caught it", name)
		}
	}

	// The catalog: the scheduler family is exactly the four, all safe, all
	// pure reads, and no spec's METHOD smuggles a control verb either.
	var fam []records.Spec
	for _, sp := range records.Catalog() {
		if sp.Family == "scheduler" {
			fam = append(fam, sp)
		}
	}
	if len(fam) != 4 {
		t.Fatalf("the catalog carries %d scheduler specs — the family is exactly the four reads", len(fam))
	}
	for _, sp := range fam {
		if !schedulerAgentReads[sp.Name] {
			t.Errorf("catalog spec %s is outside the declared four", sp.Name)
		}
		if sp.Mutates || sp.Class != "safe" {
			t.Errorf("catalog spec %s is class %q mutates=%v — the scheduler's agent family is reads only", sp.Name, sp.Class, sp.Mutates)
		}
		if schedulerToolNameViolates(strings.ReplaceAll(sp.Method, ".", "_")) {
			t.Errorf("spec %s answers %s — a control method behind a read-shaped name is the disguise this guard exists for", sp.Name, sp.Method)
		}
	}
}

// TestSchedulerToolsNotInDefaultGrant is registered-is-not-granted (S11)
// for the family: the default grant is unchanged (read, list, write) and
// names no scheduler tool; a step sees these four only through a
// per-agent tool_access decision a person makes.
func TestSchedulerToolsNotInDefaultGrant(t *testing.T) {
	for _, name := range tools.DefaultGrant() {
		if schedulerAgentReads[name] || strings.HasPrefix(name, "scheduler_") {
			t.Errorf("DefaultGrant() hands out %s — scheduler eyes are granted per agent, never by default (S11, S27 T8)", name)
		}
	}
}
