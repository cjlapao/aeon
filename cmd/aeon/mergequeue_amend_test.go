package main

// mergequeue_amend_test.go — S60 T8 (P4). The guard amendment for the
// merge-queue plane, in the mould of budget_amend_test.go (S55 T8) and
// environment_guard_test.go (S59): the slice's security claim made
// mechanical rather than reviewed.
//
// The claim (MQ13, MQ16): the six merge-queue WRITES (enqueue, dequeue,
// reorder, retry, land, settings) are PERSON doors on the daemon's wide
// socket and are provably absent from every non-human door — the narrow
// (agent / HTTP / MCP-stdio) projection, the agent-services enumeration,
// the tool registry, the records catalog, and by text the four packages
// that own those doors. The plane's queue ACTIONS — the six verbs the
// landing loop performs INSIDE the policy engine (queue.enqueue,
// git.integrate, git.concludeIntegration, git.publishIntegration,
// forge.openLandingPR, forge.land) — are likewise no one's tool and no
// one's narrow action: they live only where S56 put them, as gated loop
// actions. A screen may READ the queue through the person's own socket;
// nothing that is not the person may move a lane.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
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

// guardMergeQueueService is the non-nil seam a guard needs to see the ten
// queue methods REGISTERED (the V13 precedent: RegisterMergeQueue only
// inspects nil — it never calls — so the seam may answer nothing).
type guardMergeQueueService struct{}

var _ protocol.MergeQueueService = guardMergeQueueService{} // compile-time

func (guardMergeQueueService) List(context.Context, protocol.MergeQueueListParams) ([]protocol.ItemView, error) {
	return nil, nil
}
func (guardMergeQueueService) Show(context.Context, protocol.MergeQueueShowParams) (protocol.ShowResult, error) {
	return protocol.ShowResult{}, nil
}
func (guardMergeQueueService) Status(context.Context, protocol.MergeQueueStatusParams) (protocol.StatusResult, error) {
	return protocol.StatusResult{}, nil
}
func (guardMergeQueueService) Vocabulary(context.Context) (protocol.VocabularyResult, error) {
	return protocol.VocabularyResult{}, nil
}
func (guardMergeQueueService) Enqueue(context.Context, protocol.Actor, protocol.MergeQueueEnqueueParams) (protocol.ItemView, error) {
	return protocol.ItemView{}, nil
}
func (guardMergeQueueService) Dequeue(context.Context, protocol.Actor, protocol.MergeQueueIDParams) (protocol.ItemView, error) {
	return protocol.ItemView{}, nil
}
func (guardMergeQueueService) Reorder(context.Context, protocol.Actor, protocol.MergeQueueReorderParams) (protocol.ItemView, error) {
	return protocol.ItemView{}, nil
}
func (guardMergeQueueService) Retry(context.Context, protocol.Actor, protocol.MergeQueueIDParams) (protocol.ItemView, error) {
	return protocol.ItemView{}, nil
}
func (guardMergeQueueService) Land(context.Context, protocol.Actor, protocol.MergeQueueLandParams) (protocol.ItemView, error) {
	return protocol.ItemView{}, nil
}
func (guardMergeQueueService) Settings(context.Context, protocol.Actor, protocol.MergeQueueSettingsParams) (protocol.ConfigView, string, error) {
	return protocol.ConfigView{}, "", nil
}

func newMergeQueueTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.MergeQueue = guardMergeQueueService{}
	if !tbl.RegisterMergeQueue() {
		t.Fatal("RegisterMergeQueue refused to register over a non-nil seam — the app would have no queue door at all")
	}
	return tbl
}

// The six PERSON writes (MQ13) and the ten plane names (six writes + four
// reads).
var (
	mergeQueueWrites = []protocol.MethodName{
		protocol.MethodMergeQueueEnqueue, protocol.MethodMergeQueueDequeue,
		protocol.MethodMergeQueueReorder, protocol.MethodMergeQueueRetry,
		protocol.MethodMergeQueueLand, protocol.MethodMergeQueueSettings,
	}
	mergeQueueReads = []protocol.MethodName{
		protocol.MethodMergeQueueList, protocol.MethodMergeQueueShow,
		protocol.MethodMergeQueueStatus, protocol.MethodMergeQueueVocabulary,
	}
	mergeQueueAll = func() []protocol.MethodName {
		out := append([]protocol.MethodName{}, mergeQueueWrites...)
		return append(out, mergeQueueReads...)
	}()
	// The six LOOP ACTIONS the landing lane performs inside the policy
	// engine (S56's gated verbs). They are action names, not protocol
	// methods — and this file's third guard is exactly the claim that they
	// never LEAK out of the engine into a door an agent can ring.
	mergeQueueLoopActions = []string{
		"queue.enqueue", "git.integrate", "git.concludeIntegration",
		"git.publishIntegration", "forge.openLandingPR", "forge.land",
	}
)

// TestMergeQueueWritesAreAHumanDoor is the positive half (P4): the exact
// write set is registered, each RequiresInitialize, none Debug; the reads
// likewise; and with the seam nil all ten stay ABSENT (V13 — a storeless
// daemon has no queue surface, not a refusing one).
func TestMergeQueueWritesAreAHumanDoor(t *testing.T) {
	tbl := newMergeQueueTable(t)
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}
	for _, m := range append(append([]protocol.MethodName{}, mergeQueueWrites...), mergeQueueReads...) {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the queue plane (MQ13)", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a queue row is a store row", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — the queue is the person's surface, not a diagnostic", m)
		}
	}
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterMergeQueue() // no-op over a nil seam: the ten stay absent
	for _, m := range mergeQueueAll {
		if _, err := tblNil.Dispatch(context.Background(), "c1", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s dispatched on a nil-seam table — the narrow surface can reach a door it must not (want ErrUnknownMethod, got %v)", m, err)
		}
	}
}

// mergeQueueToolAllowlist is the RECORD of what is allowed to look
// queue-shaped in the tool registry. EMPTY at T8: nothing was. S60 T9 (the
// change it was designed for) joins the five queue names HERE — the three
// own-view reads (mergequeue_list · mergequeue_show · mergequeue_status,
// answered over the SAME door service, rule 4), the ONE direct write the
// spec's own shape allows (mergequeue_dequeue: this loop's own
// queued/conflicted item, a removal that can only NARROW what the queue may
// do — MQ17), and the ONE ask (mergequeue_propose_enqueue, inert until a
// person commits, MQ14.3). No enqueue-direct, reorder, retry, land or
// settings name joins any list, ever: the widening names stay the person's
// six doors (MQ13), and the guard below still refuses them by word. An
// exception list is the shape of the record; this is what T9 asked to be
// forgiven, written down and nothing more.
var mergeQueueToolAllowlist = map[string]bool{
	"mergequeue_list":            true,
	"mergequeue_show":            true,
	"mergequeue_status":          true,
	"mergequeue_dequeue":         true,
	"mergequeue_propose_enqueue": true,
}

// mergeQueueSpecAllowlist is the same record for the records catalog. T8's
// first line read "a spec twined to a WRITE never belongs on any list" —
// T9's own spec table (S60 T9.md, the five) settles that question the way
// S51's bug_file settled triage's: the two MUTATES entries here are the
// agent's OWN doors by construction — mergequeue_dequeue twines
// mergequeue.dequeue but its arm admits ONLY the calling loop's own
// queued/conflicted item (another loop's item, or any status in flight, is
// refused by name), and mergequeue_propose_enqueue twines the agent's own
// mergequeue.proposeEnqueue, which files nothing but an inert row. The
// five-writes list (enqueue · reorder · retry · land · settings) stays
// twine-free: those five names appear beside no spec at all, and the
// surface walk proves the plane's whole agent surface is exactly this list
// (TestMergeQueueAgentSurfaceIsExactlyTheFive).
var mergeQueueSpecAllowlist = map[string]bool{
	"mergequeue_list":            true,
	"mergequeue_show":            true,
	"mergequeue_status":          true,
	"mergequeue_dequeue":         true,
	"mergequeue_propose_enqueue": true,
}

// TestMergeQueueAuthorityNeverReachesOtherDoors is the negative half, one
// subtest per door. A queue write on any of them hands a non-person the
// lane: a model could merge its own branch, land its own commit, and
// answer its own conflict policy.
func TestMergeQueueAuthorityNeverReachesOtherDoors(t *testing.T) {
	// ── Negative 1 — the narrow table: no action projects a queue method,
	// read or write. The queue is the person's view of the lane; the agent
	// view of the SAME facts is the loop, with its gates.
	t.Run("narrow", func(t *testing.T) {
		for _, a := range narrow.Actions() {
			sm := string(a.SocketMethod)
			if strings.HasPrefix(sm, "mergequeue.") {
				t.Errorf("action %q projects queue method %q — the narrow surface has no queue door, read or write (MQ13)", a.Name, sm)
			}
			for _, m := range mergeQueueAll {
				if a.SocketMethod == m {
					t.Errorf("action %q projects %q — a queue write through a non-person projection moves a lane nobody queued (MQ13)", a.Name, m)
				}
			}
			for _, act := range mergeQueueLoopActions {
				if a.Name == act || sm == act {
					t.Errorf("action %q carries the loop action %q — the six gated verbs live inside the policy engine, not on a surface (S56/MQ16)", a.Name, act)
				}
			}
		}
	})

	// ── Negative 2 — the agent-services enumeration: the agent-to-agent
	// lifecycle has no queue door, read or write.
	t.Run("agent-services", func(t *testing.T) {
		for _, m := range protocol.AgentServiceMethods() {
			for _, q := range mergeQueueAll {
				if m.Name == q {
					t.Fatalf("%s is an agent-service method — an agent moving a merge lane with no person in the room (MQ13)", m.Name)
				}
			}
		}
	})

	// ── Negative 3 — the tool registry: no tool wears the queue's name,
	// and none wears one of the six loop actions (a tool named forge_land
	// would be a door onto the forge bypassing the gate that guards it).
	t.Run("tools-registry", func(t *testing.T) {
		queueName := regexp.MustCompile(`(?i)merge_?queue|queue_land|queue_retry|queue_enqueue|queue_dequeue`)
		snaked := make(map[string]bool, len(mergeQueueLoopActions))
		for _, a := range mergeQueueLoopActions {
			snaked[strings.ToLower(regexp.MustCompile(`([a-z0-9])([A-Z])`).ReplaceAllString(
				strings.ReplaceAll(a, ".", "_"), "$1_$2"))] = true
		}
		for name := range tools.Registry() {
			if mergeQueueToolAllowlist[name] {
				continue
			}
			if queueName.MatchString(name) {
				t.Errorf("tool %q wears a queue-plane name — a tool named after a queue write is a second door onto the write the socket alone carries (MQ13)", name)
			}
			if snaked[name] {
				t.Errorf("tool %q IS a loop action — the six gated verbs are policy actions answered inside a loop, never tools an agent rings (S56/MQ16)", name)
			}
		}
	})

	// ── Negative 4 — the records catalog: no spec at all twines to a
	// queue WRITE, no Mutates spec twines to any queue method, and no spec
	// at T8 twines to the plane. (T9's read twins join the allowlist in
	// their own change — OD10's discipline: reads may twin, writes never.)
	t.Run("records-catalog", func(t *testing.T) {
		for _, spec := range records.Catalog() {
			if mergeQueueSpecAllowlist[spec.Name] {
				continue
			}
			for _, m := range mergeQueueWrites {
				if spec.Method == string(m) {
					t.Errorf("spec %q twines the person write %q — the records catalog is an agent's surface and moving a lane is a person's act (MQ13)", spec.Name, m)
				}
			}
			if spec.Mutates && strings.HasPrefix(spec.Method, "mergequeue.") {
				t.Errorf("spec %q is a Mutates record twined to queue method %q — only READS may ever twin (OD10)", spec.Name, spec.Method)
			}
			if strings.HasPrefix(spec.Method, "mergequeue.") {
				t.Errorf("spec %q twines queue method %q before the T9 allowlist — the exception list is the record, and this is not on it", spec.Name, spec.Method)
			}
		}
	})

	// ── Negative 5 — by text: the six writes' wire literals, their consts,
	// and the six loop-action literals appear in NO non-test file under the
	// four packages that own the non-human doors. A projection that names
	// mergequeue.land, even in a comment, is the projection the rule
	// forbids.
	t.Run("text-grep", func(t *testing.T) {
		var totalWalked int
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			offenders, walked := walkMergeQueueLiterals(filepath.Join("..", "..", dir))
			totalWalked += walked
			for _, o := range offenders {
				t.Errorf("%s names queue authority — it must not reach a non-human door (MQ13)", o)
			}
		}
		// ── Negative 6 — non-vacuity: the walk saw enough to mean
		// something. Measured 2026-11-04 at T8; a tree that loses a guarded
		// file fails the count.
		if totalWalked < 40 {
			t.Fatalf("the text-grep walk saw %d non-test files across the four guarded directories, want ≥ 40 — the guard would be checking almost nothing", totalWalked)
		}
	})
}

// narrowProjectingMergeQueue returns the narrow actions projecting any
// queue method — a function over action lists, so the canary can feed it a
// poisoned table the real one never carries.
func narrowProjectingMergeQueue(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range mergeQueueAll {
			if a.SocketMethod == m {
				bad = append(bad, a.Name)
			}
		}
	}
	return bad
}

// recordsSpecTwinedToMergeQueueWrite reports whether a spec twines one of
// the six person writes — the predicate over specs, canary-fed.
func recordsSpecTwinedToMergeQueueWrite(spec records.Spec) bool {
	for _, m := range mergeQueueWrites {
		if spec.Method == string(m) {
			return true
		}
	}
	return false
}

// mergeQueueLiteralExemption is the walk's CLOSED exemption table — the
// S59 shape (one file, one literal, counted): S60 T9's own dequeue spec is
// a MUTATES record twined to mergequeue.dequeue BY SPEC (S60 T9.md's five;
// S51's bug_file→triage.fileAs settled what that means — an agent's own
// door, arms admitting only the calling loop's own queued/conflicted item),
// so the family file may hold the literal EXACTLY ONCE — as the
// spec's Method value — and still offend on the second. No other file, no
// other literal, nothing added without its task spec naming it.
// 2026-11-04 (T9 landing merge): the SIZE1 split of records.Catalog() moved
// the entry — the exemption KEY follows the literal, catalog.go →
// catalog_mergequeue.go; still one file, one literal, count one.
var mergeQueueLiteralExemption = map[string]map[string]int{
	"catalog_mergequeue.go": {"mergequeue.dequeue": 1},
}

// walkMergeQueueLiterals walks one guarded directory's non-test .go files
// for the forbidden literals and returns the offenders plus the count.
func walkMergeQueueLiterals(root string) (offenders []string, walked int) {
	forbidden := []string{
		"mergequeue.enqueue", "mergequeue.dequeue", "mergequeue.reorder",
		"mergequeue.retry", "mergequeue.land", "mergequeue.settings",
		"MethodMergeQueueEnqueue", "MethodMergeQueueDequeue", "MethodMergeQueueReorder",
		"MethodMergeQueueRetry", "MethodMergeQueueLand", "MethodMergeQueueSettings",
		"queue.enqueue", "git.integrate", "git.concludeIntegration",
		"git.publishIntegration", "forge.openLandingPR", "forge.land",
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
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
				if allow, has := mergeQueueLiteralExemption[filepath.Base(path)]; has {
					if cap, named := allow[f]; named && strings.Count(text, f) == cap {
						continue // the named exception, counted — see the table
					}
				}
				offenders = append(offenders, path+": "+f)
			}
		}
		return nil
	})
	return offenders, walked
}

// TestMergeQueueGuardsHaveTeeth is the canary meta-test: each predicate fed
// a synthetic violation must report it and must not report a clean input.
func TestMergeQueueGuardsHaveTeeth(t *testing.T) {
	// Canary 1 — a fake narrow action projecting mergequeue.land (the
	// exact "for symmetry" addition the Do-NOT list forbids) is flagged;
	// the real table is not.
	poisoned := []narrow.Action{
		{Name: "aeon_queue_land", SocketMethod: protocol.MethodMergeQueueLand, Kind: narrow.KindRead},
	}
	if got := narrowProjectingMergeQueue(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting mergequeue.land was not flagged — the narrow guard cannot fail, so it is not a guard")
	}
	if got := narrowProjectingMergeQueue(narrow.Actions()); len(got) != 0 {
		t.Errorf("the real narrow table was flagged: %v", got)
	}

	// Canary 2 — a fake records Spec twined to the land write is flagged;
	// a clean READ spec is not (T9's future twins are the clean shape).
	if !recordsSpecTwinedToMergeQueueWrite(records.Spec{Name: "queue_land", Method: "mergequeue.land", Mutates: true}) {
		t.Fatal("a records spec twined to mergequeue.land was not flagged — the catalog guard cannot fail")
	}
	if recordsSpecTwinedToMergeQueueWrite(records.Spec{Name: "queue_list", Method: "mergequeue.list"}) {
		t.Fatal("a clean queue read spec was flagged — the predicate fires on nothing")
	}

	// Canary 3 — a temp non-test .go file naming mergequeue.land is
	// flagged by the text walk; a clean file is not.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poison.go"),
		[]byte("package tools\n\nvar _ = \"mergequeue.land\"\nvar _ = \"forge.land\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	off, _ := walkMergeQueueLiterals(dir)
	if len(off) < 2 {
		t.Fatalf("a file naming mergequeue.land AND forge.land flagged only %v — the walk cannot fail on both families", off)
	}
	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "clean.go"),
		[]byte("package tools\n\nvar _ = \"mergequeue.list\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkMergeQueueLiterals(clean); len(off) != 0 {
		t.Errorf("a clean file was flagged: %v", off)
	}
}

// TestMergeQueueAmendmentLeavesThePriorGuardsByteIdentical re-pins the
// guard files the prior human-door planes own (the budget guard's own
// mechanism, under this slice's name — S45's TestOriginalGuardsAre
// ByteIdentical keeps its name and its spec-owned set; this is S60's
// sibling, and T0 recorded none, so T8 records them). The S60 amendment
// adds this file and changes no existing guard body; hashes are MEASURED
// at 2026-11-04 against the tree this slice merged into.
func TestMergeQueueAmendmentLeavesThePriorGuardsByteIdentical(t *testing.T) {
	pinned := map[string]string{
		// Both entries re-pinned 2026-11-04 (S60 T10's merge walk): main's ea29cfc7
		// moved the two files the T9 pins here freeze — loop_control_amend_test.go
		// by the S60 T7 cascade (TestOnlyTheControlServiceCallsTheCAS names the
		// merge queue's two control-service callers, the one deliberate edit its
		// own re-pin comment records), and budget_amend_test.go BY THAT re-pin's
		// line inside its table. No guard BODY moved; the cascade is the S49 T5
		// precedent this file's header already cites. The amendments THIS test
		// owns remain unedited.
		"budget_amend_test.go":       "0777827e107af856293478473923d97989561c44f6017b3494e840b9b17b4632",
		"loop_control_amend_test.go": "13920fc523c793b75fc2c35ca9523917ea3a830ff11f62874c22ec67edc06b8d",
		"supervision_guard_test.go":  "e5eaab6992960f41feffb389e366a388f5e90f929515e1823219a63a59865f2c",
		"environment_guard_test.go":  "5d0f6a499405703482d81b4f1d9f8279c2b22a701a0f19476ff9d219909b92e2",
		"automation_guard_test.go":   "37a47e7ff8d1886ee7b7ad9c70d932acd69dba56bd21fc2440def08e38e972d2",
		"verify_amend_test.go":       "504976b8e8c44f9e72cac9d9ccdd4b98d4081d1128beb589eada61b29537e7e5",
		"backup_guard_test.go":       "fe0587dcdd0b955da08a262ae3d57d3b657741b83b6e96889f07c5e5bb2b6ab2",
		// 2026-11-08 (inspect door): the closed MCP door set gained mcp.server.inspect and
		// the importer list gained mcp_admin_inspect.go — the two edits the guard's own
		// "closed set" and "named importers" rules require for a new door, no guard
		// body weakened. The pin follows the file.
		// 2026-11-08 (share door): the closed set gained mcp.server.share and the write-path
		// list mcp_share.go — again the two edits a new door requires, no guard weakened.
		"mcp_amend_test.go": "5a8ab013ffc6876bc163df2835822bed40eb1d505d581d8ca01c91698f130632",
		// Re-pinned 2026-11-05 (S61 T6's sweep walk): policy_amend_test.go grew
		// ONE dated allow-list ADMISSON line (internal/intake/resume.go — the
		// intake engine's verbatim DecideProposal call, GI7's dedicated door)
		// and its gofmt alignment; no guard BODY moved — the S49 T5 cascade
		// precedent this file's header cites. roadmap_guard_test.go is a
		// FOREIGN re-pin: main's 50a57b06 (S27 T3, 2026-11-05) moved the file
		// byte-for-byte without paying this pin; the T8 hash already failed on
		// that merge alone, and S61 T6 pays it forward, its contents unedited
		// by this slice.
		"policy_amend_test.go":  "0ecd4982f7815c1a4614c67e74ecae6e382e88554e68690d87128babf65597b5",
		"roadmap_guard_test.go": "9f591f0b0edc185fcfe25130b694190a65c91457d5402bbd5691389d488da62a",
	}
	for path, want := range pinned {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s changed since the T8 pins (want %s, got %s) — the S60 amendment edits no existing guard body", path, want, got)
		}
	}
}
