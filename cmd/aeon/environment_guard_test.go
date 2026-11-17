package main

// environment_guard_test.go — S59 T6: the twenty doors of the execution-
// environment plane are a HUMAN DOOR, proved rather than reviewed.
//
// The family this belongs to (`policy_amend_test.go`, `mcp_amend_test.go`,
// `autonomy_guard_test.go`, `verify_amend_test.go`, `backup_guard_test.go`) all
// proves the same shape: a write that changes what a loop will run with is the
// person's act, taken on the `0600` socket with the peer as its author, and
// absent from every surface a non-person reaches. Here the stakes are the
// highest in that family — an environment is who a command runs as, with which
// credentials and against which network — so an agent that could widen one has
// widened everything downstream of it.
//
// Each guard is a function over DATA (a table, a list, a source string) so its
// meta-test can feed it the violation the real data does not contain. A guard
// that cannot fail is a green light, not a guard.

import (
	"context"
	"errors"
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

// The plane's twenty doors, spelled once. The guards below read these lists,
// so "the plane is these twenty names" is one assertion rather than six.
var environmentWriteMethods = []protocol.MethodName{
	protocol.MethodEnvironmentCreate, protocol.MethodEnvironmentUpdate,
	protocol.MethodEnvironmentArchive, protocol.MethodEnvironmentRestore,
	protocol.MethodEnvironmentBind, protocol.MethodEnvironmentUnbind,
	protocol.MethodEnvironmentTeardown,
	protocol.MethodSecretRefCreate, protocol.MethodSecretRefUpdate,
	protocol.MethodSecretRefArchive, protocol.MethodSecretRefRestore,
}

var environmentReadMethods = []protocol.MethodName{
	protocol.MethodEnvironmentList, protocol.MethodEnvironmentShow,
	protocol.MethodEnvironmentEffective, protocol.MethodEnvironmentVocabulary,
	protocol.MethodEnvironmentChanges, protocol.MethodEnvironmentProvisions,
	protocol.MethodSecretRefList, protocol.MethodSecretRefShow, protocol.MethodSecretRefCheck,
}

func environmentAllMethods() []protocol.MethodName {
	return append(append([]protocol.MethodName{}, environmentWriteMethods...), environmentReadMethods...)
}

// guardEnvSeam / guardRefSeam are the non-nil seams the registration needs.
// The door inspects only whether the seam is nil, so a guard may stand in one
// that answers nothing rather than standing up a store.
type guardEnvSeam struct{}

var _ protocol.EnvironmentService = guardEnvSeam{}

func (guardEnvSeam) List(context.Context, protocol.EnvironmentListParams) ([]protocol.EnvironmentView, error) {
	return nil, nil
}
func (guardEnvSeam) Show(context.Context, protocol.EnvironmentShowParams) (protocol.EnvironmentView, error) {
	return protocol.EnvironmentView{}, nil
}
func (guardEnvSeam) Effective(context.Context, protocol.EnvironmentEffectiveParams) (protocol.EnvironmentEffectiveView, error) {
	return protocol.EnvironmentEffectiveView{}, nil
}
func (guardEnvSeam) Vocabulary(context.Context) (protocol.EnvironmentVocabularyView, error) {
	return protocol.EnvironmentVocabularyView{}, nil
}
func (guardEnvSeam) Changes(context.Context, protocol.EnvironmentChangesParams) ([]protocol.EnvironmentChangeView, error) {
	return nil, nil
}
func (guardEnvSeam) Provisions(context.Context, protocol.EnvironmentProvisionsParams) ([]protocol.ProvisionView, error) {
	return nil, nil
}
func (guardEnvSeam) Create(context.Context, protocol.Actor, protocol.EnvironmentCreateParams) (protocol.EnvironmentView, error) {
	return protocol.EnvironmentView{}, nil
}
func (guardEnvSeam) Update(context.Context, protocol.Actor, protocol.EnvironmentUpdateParams) (protocol.EnvironmentView, error) {
	return protocol.EnvironmentView{}, nil
}
func (guardEnvSeam) Archive(context.Context, protocol.Actor, protocol.EnvironmentIDParams) (protocol.EnvironmentView, error) {
	return protocol.EnvironmentView{}, nil
}
func (guardEnvSeam) Restore(context.Context, protocol.Actor, protocol.EnvironmentIDParams) (protocol.EnvironmentView, error) {
	return protocol.EnvironmentView{}, nil
}
func (guardEnvSeam) Bind(context.Context, protocol.Actor, protocol.EnvironmentBindParams) (protocol.BindResult, error) {
	return protocol.BindResult{}, nil
}
func (guardEnvSeam) Unbind(context.Context, protocol.Actor, protocol.EnvironmentUnbindParams) (protocol.UnbindResult, error) {
	return protocol.UnbindResult{}, nil
}
func (guardEnvSeam) Teardown(context.Context, protocol.Actor, protocol.ProvisionIDParams) (protocol.ProvisionView, error) {
	return protocol.ProvisionView{}, nil
}

type guardRefSeam struct{}

var _ protocol.SecretRefService = guardRefSeam{}

func (guardRefSeam) List(context.Context, protocol.SecretRefListParams) ([]protocol.SecretRefView, error) {
	return nil, nil
}
func (guardRefSeam) Show(context.Context, protocol.SecretRefShowParams) (protocol.SecretRefView, error) {
	return protocol.SecretRefView{}, nil
}
func (guardRefSeam) Check(context.Context, protocol.SecretRefCheckParams) (protocol.SecretRefCheckResult, error) {
	return protocol.SecretRefCheckResult{}, nil
}
func (guardRefSeam) Create(context.Context, protocol.Actor, protocol.SecretRefCreateParams) (protocol.SecretRefView, error) {
	return protocol.SecretRefView{}, nil
}
func (guardRefSeam) Update(context.Context, protocol.Actor, protocol.SecretRefUpdateParams) (protocol.SecretRefView, error) {
	return protocol.SecretRefView{}, nil
}
func (guardRefSeam) Archive(context.Context, protocol.Actor, protocol.SecretRefNameParams) (protocol.SecretRefView, error) {
	return protocol.SecretRefView{}, nil
}
func (guardRefSeam) Restore(context.Context, protocol.Actor, protocol.SecretRefNameParams) (protocol.SecretRefView, error) {
	return protocol.SecretRefView{}, nil
}

// newEnvironmentGuardTable builds the table the way the daemon does: NewTable,
// assign the seams, register. The positive half of every guard starts here —
// the app HAS its door.
func newEnvironmentGuardTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Environment = guardEnvSeam{}
	tbl.SecretRefs = guardRefSeam{}
	if !tbl.RegisterEnvironment() {
		t.Fatal("RegisterEnvironment refused over a wired seam — the app would have no environment door at all (P34)")
	}
	if !tbl.RegisterSecretRef() {
		t.Fatal("RegisterSecretRef refused over a wired seam — the references would have no door")
	}
	return tbl
}

// TestEnvironmentWritesAreAHumanDoor is the plane's EV17/EV16 claim at the one
// place it can be proved: registered iff wired, behind initialize, never a
// diagnostic, and answered by the connection's person.
func TestEnvironmentWritesAreAHumanDoor(t *testing.T) {
	tbl := newEnvironmentGuardTable(t)

	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	for _, m := range environmentAllMethods() {
		if !registered[m] {
			t.Errorf("%s is not registered over a wired seam — the panel and the CLI have one door each and this one is shut (P34)", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			continue
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — an environment door is the person's window onto what their loops run with, never an operator probe", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s is reachable before initialize — nothing about the environment plane is safe to read or change on a connection that has not negotiated", m)
		}
	}
	if len(registered) < 20 {
		t.Fatalf("the table holds %d methods; the plane's twenty cannot be a subset of nothing", len(registered))
	}

	// V13: no seam, no doors — absent, not refusing.
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if tblNil.RegisterEnvironment() || tblNil.RegisterSecretRef() {
		t.Fatal("the plane registered itself over a nil seam (V13)")
	}
	for _, m := range environmentAllMethods() {
		if _, err := tblNil.Dispatch(context.Background(), "c1", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Errorf("%s answered %v without its seam, want method-not-found — an app enumerating the surface would see a half-built plane", m, err)
		}
	}
}

// environmentNameIsPlane reports whether a name belongs to this plane. One
// predicate, used by every negative half below, so the halves cannot disagree
// about what the plane is.
func environmentNameIsPlane(name string) bool {
	return strings.HasPrefix(name, "environment.") || strings.HasPrefix(name, "secretref.")
}

func narrowProjectingEnvironment(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if environmentNameIsPlane(string(a.SocketMethod)) {
			bad = append(bad, a.Name+" → "+string(a.SocketMethod))
		}
	}
	return bad
}

func boundaryNamingEnvironment(stmt string) []string {
	var bad []string
	for _, line := range strings.Split(stmt, "\n") {
		for _, name := range environmentAllMethods() {
			if strings.Contains(line, string(name)) {
				bad = append(bad, string(name))
			}
		}
	}
	return bad
}

func agentServicesNamingEnvironment(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range ms {
		if environmentNameIsPlane(string(m.Name)) {
			bad = append(bad, string(m.Name))
		}
	}
	return bad
}

var environmentToolName = regexp.MustCompile(`(?i)environment|secretref|secret_ref|env_bind`)

func registryNamingEnvironment(reg map[string]tools.Tool) []string {
	var bad []string
	for name := range reg {
		// S59 T7 (2026-11-03, EV16): ONE exemption, named in
		// environment_propose_guard_test.go — the five catalog tools this
		// slice put on the agent's surface by spec (four reads plus the
		// tightening-only propose). It is a closed list keyed on the exact
		// tool name AND its twin, and every write-named tool still fires;
		// canaried in TestEnvironmentProposeGuardHasTeeth. Nothing else about
		// this predicate changed.
		if environmentToolName.MatchString(name) && !environmentAgentSurfaceTool(name) {
			bad = append(bad, name)
		}
	}
	return bad
}

func catalogNamingEnvironment(specs []records.Spec) []string {
	var bad []string
	for _, sp := range specs {
		// S59 T7 (2026-11-03, EV16): the same closed list, and it must match
		// BOTH halves — the tool name and the method it twins to — so a spec
		// named environment_list that points at environment.create still
		// fires here. Canaried, as the registry half.
		if (environmentNameIsPlane(sp.Method) || environmentToolName.MatchString(sp.Name) || sp.Family == "environment") &&
			!environmentAgentSurfaceSpec(sp) {
			bad = append(bad, sp.Name)
		}
	}
	return bad
}

// TestEnvironmentPlaneNeverReachesNarrowOrAgentSurfaces walks the four
// caller-classes that are not the person — the narrow HTTP / MCP-stdio
// projection and its own boundary statement, the agent-services enumeration,
// the tool registry, the records catalog — and then the sources themselves.
//
// There is no read twin here either, unlike S62's `backup_status`: an agent
// that can read which environment a loop runs with, and which secret references
// it injects, is an agent that has been handed the map of what to ask for. The
// reads belong to the person's panel; an agent's own view of its step's
// environment is T7's, through the tool surface, and is not this plane's door.
func TestEnvironmentPlaneNeverReachesNarrowOrAgentSurfaces(t *testing.T) {
	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — this guard would pass on nothing")
		}
		if bad := narrowProjectingEnvironment(actions); len(bad) != 0 {
			t.Errorf("the narrow projection carries %s — a key-holder's projection must not hand out the door that decides what a loop runs with (EV17)", strings.Join(bad, ", "))
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		if bad := boundaryNamingEnvironment(stmt); len(bad) != 0 {
			t.Errorf("the projection's own boundary statement lists %s — a surface that DISPLAYS a door its table must not hold is a second truth", strings.Join(bad, ", "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := agentServicesNamingEnvironment(ms); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying an environment door is the refusal made mechanical", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		if bad := registryNamingEnvironment(reg); len(bad) != 0 {
			t.Errorf("the tool registry carries %s — a tool that binds an environment or creates a secret reference is a second door onto the person's act, and a tool is not a person", strings.Join(bad, ", "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := catalogNamingEnvironment(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — this plane has no catalog entry at all until a task argues for one, and no agent read exists to twin it to (EV17)", strings.Join(bad, ", "))
		}
	})

	t.Run("TheirSources", func(t *testing.T) {
		// By TEXT, not only by table: a projection that names `environment.bind`
		// in a comment, a route, or a test helper is the projection the rule
		// forbids, and a reader finds it in the source before the table.
		//
		// The floors are MEASURED, not guessed (2026-11-03: narrow 8,
		// agentsvc 10, tools 26, records 2 non-test files — 46 in all). A dir
		// that walks to zero has lost its files or its path, and a walk that
		// thin is a pass that checked nothing.
		total := 0
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			n := walkNoEnvironmentLiteral(t, filepath.Join("..", "..", dir))
			if n == 0 {
				t.Errorf("the %s walk saw no non-test file — it checked nothing, which is what a broken guard looks like", dir)
			}
			total += n
		}
		if total < 40 {
			t.Errorf("the four walks saw %d files in total, want the ~46 measured on 2026-11-03 — a surface that shrank out of the walk is a surface no longer guarded", total)
		}
	})
}

// walkNoEnvironmentLiteral fails on any non-test Go file under root that names
// one of the plane's doors by its wire literal or its const. It returns how
// many files it read so the caller can assert the walk was not vacuous.
func walkNoEnvironmentLiteral(t *testing.T, root string) int {
	t.Helper()
	var walked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable directory holds no Go code we own
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		walked++
		for _, f := range environmentLiteralsIn(string(b)) {
			// S59 T7 (2026-11-03, EV16): the ONE named exemption in the
			// whole walk — internal/records/catalog.go's two wire-literal
			// prefixes, because the catalog IS data and the five agent specs
			// this slice was given carry their method twins there by design.
			// The list (file × literal, with each one's why) lives in
			// environment_propose_guard_test.go, it is closed, the METHOD
			// constants are exempt NOWHERE, and the walk still reads the
			// file (the per-directory floors would fail if it did not).
			if environmentLiteralExempt(path, f) {
				continue
			}
			t.Errorf("%s names %q — an environment or secret-reference door must not reach the narrow, agent, tool or catalog surfaces (EV17)", path, f)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return walked
}

// environmentLiteralsIn is the walk's predicate, split from the walker so the
// canary test can feed it a poisoned source and see the offender back. A guard
// whose predicate is welded to its `t.Errorf` cannot be shown to work.
func environmentLiteralsIn(text string) []string {
	forbidden := []string{
		`"environment.`, `"secretref.`,
		"MethodEnvironment", "MethodSecretRef",
	}
	var bad []string
	for _, f := range forbidden {
		if strings.Contains(text, f) {
			bad = append(bad, f)
		}
	}
	return bad
}

// TestEnvironmentGuardsHaveTeeth is the canary family: every predicate above is
// fed the violation the real data does not contain, and must name it. Without
// this file the guards above are assertions that something never arrived —
// which is what a broken guard always looked like.
func TestEnvironmentGuardsHaveTeeth(t *testing.T) {
	const canary = protocol.MethodName("environment.bind")

	t.Run("NarrowSurface", func(t *testing.T) {
		poisoned := []narrow.Action{{Name: "aeon_environment_bind", SocketMethod: canary, Kind: narrow.KindRead}}
		bad := narrowProjectingEnvironment(poisoned)
		if len(bad) != 1 || !strings.Contains(bad[0], "environment.bind") {
			t.Fatalf("a narrow action projecting environment.bind was not named: %v", bad)
		}
		clean := []narrow.Action{{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead}}
		if len(narrowProjectingEnvironment(clean)) != 0 {
			t.Fatal("a clean action was flagged — the predicate fires on nothing")
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		if bad := boundaryNamingEnvironment("methods:\n  - environment.teardown\n"); len(bad) == 0 || bad[0] != "environment.teardown" {
			t.Fatalf("a boundary statement naming environment.teardown was not flagged: %v", bad)
		}
		if len(boundaryNamingEnvironment("methods:\n  - loop.list\n")) != 0 {
			t.Fatal("a clean boundary statement was flagged")
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		if bad := agentServicesNamingEnvironment([]protocol.AgentServiceMethod{{Name: canary}}); len(bad) == 0 {
			t.Fatal("an agent-service method named environment.bind was not flagged")
		}
		if len(agentServicesNamingEnvironment([]protocol.AgentServiceMethod{{Name: protocol.MethodLoopList}})) != 0 {
			t.Fatal("a clean agent-service method was flagged")
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		poisoned := map[string]tools.Tool{"environment_bind": {}}
		if bad := registryNamingEnvironment(poisoned); len(bad) == 0 {
			t.Fatal("a tool named environment_bind was not flagged — an agent that can bind can widen what a loop runs with")
		}
		if len(registryNamingEnvironment(map[string]tools.Tool{"bash": {}, "read": {}})) != 0 {
			t.Fatal("an ordinary tool was flagged")
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		if bad := catalogNamingEnvironment([]records.Spec{{Name: "environment_bind", Method: string(canary), Family: "environment"}}); len(bad) == 0 {
			t.Fatal("a catalog spec twinned to environment.bind was not flagged")
		}
		if len(catalogNamingEnvironment([]records.Spec{{Name: "loop_show", Method: string(protocol.MethodLoopShow), Family: "loop"}})) != 0 {
			t.Fatal("an ordinary catalog entry was flagged")
		}
	})

	t.Run("SourceWalk", func(t *testing.T) {
		poisoned := "package x\n\nconst m = \"environment.bind\"\nvar n = protocol.MethodSecretRefCreate\n"
		bad := environmentLiteralsIn(poisoned)
		if len(bad) < 2 {
			t.Fatalf("a source naming environment.bind AND MethodSecretRefCreate produced %v — the walk cannot fail, so it is not a guard", bad)
		}
		if len(environmentLiteralsIn("package x\n\nconst m = \"loop.list\"\n")) != 0 {
			t.Fatal("a clean source was flagged — the walk fires on nothing")
		}
	})

	t.Run("Registration", func(t *testing.T) {
		// The two planes are separately wired (V13), and the teeth here prove
		// they are separately WIRED-OUT too: wire one seam, and the OTHER
		// plane's doors must still be unreachable. A registration that
		// registered both whenever either seam was present would hand a
		// half-built daemon a door onto references it cannot resolve.
		tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
		tbl.Environment = guardEnvSeam{}
		if !tbl.RegisterEnvironment() {
			t.Fatal("the environment doors did not appear over their own seam")
		}
		if tbl.RegisterSecretRef() {
			t.Fatal("the reference doors appeared over a nil reference seam")
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", protocol.MethodSecretRefCreate, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Errorf("secretref.create reached a table whose reference seam is nil: %v", err)
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", protocol.MethodEnvironmentBind, nil); err == nil {
			t.Error("environment.bind answered before initialize — the door opened without the handshake")
		} else if errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Error("environment.bind is absent although its seam is wired")
		}
	})
}
