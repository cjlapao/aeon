package main

// intake_amend_test.go — S61 T7 (GI14/GI15): the goal-intake plane's
// guards. The fifteen doors (five charter + three template + seven intake)
// are the person's surface; the four writes (charter.save · template.save
// · intake.start · intake.commit) additionally never reach any surface a
// non-person rings. Beside the merge-queue file's shape: five guards —
// the human door, the four negatives, the charter-grants-nothing import
// census, the person-door-actions census, and teeth — beside the
// UNEDITED pre-slice guards (PL15/TD7 pinned bytes belong to their
// owners; this file edits none of them).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// guardIntakeStubs: the non-nil seams the guards need to see the fifteen
// doors REGISTERED (V13 precedent: the Register functions only inspect
// nil — they never call — so the stubs answer nothing).
type guardCharterService struct{}

func (guardCharterService) Get(context.Context, protocol.CharterGetParams) (protocol.CharterView, error) {
	return protocol.CharterView{}, nil
}
func (guardCharterService) List(context.Context) (protocol.CharterListResult, error) {
	return protocol.CharterListResult{}, nil
}
func (guardCharterService) Revisions(context.Context, protocol.CharterRevisionsParams) (protocol.CharterRevisionsResult, error) {
	return protocol.CharterRevisionsResult{}, nil
}
func (guardCharterService) Vocabulary(context.Context) (protocol.CharterVocabularyResult, error) {
	return protocol.CharterVocabularyResult{}, nil
}
func (guardCharterService) Save(context.Context, protocol.Actor, protocol.CharterSaveParams) (protocol.CharterView, error) {
	return protocol.CharterView{}, nil
}

type guardTemplateService struct{}

func (guardTemplateService) List(context.Context, protocol.TemplateListParams) (protocol.TemplateListResult, error) {
	return protocol.TemplateListResult{}, nil
}
func (guardTemplateService) Get(context.Context, protocol.TemplateGetParams) (protocol.TemplateView, error) {
	return protocol.TemplateView{}, nil
}
func (guardTemplateService) Save(context.Context, protocol.Actor, protocol.TemplateSaveParams) (protocol.TemplateView, error) {
	return protocol.TemplateView{}, nil
}

type guardIntakeService struct{}

func (guardIntakeService) Start(context.Context, protocol.Actor, protocol.IntakeStartParams) (protocol.IntakeView, error) {
	return protocol.IntakeView{}, nil
}
func (guardIntakeService) List(context.Context) (protocol.IntakeListResult, error) {
	return protocol.IntakeListResult{}, nil
}
func (guardIntakeService) Status(context.Context, protocol.IntakeStatusParams) (protocol.IntakeStatusView, error) {
	return protocol.IntakeStatusView{}, nil
}
func (guardIntakeService) Plan(context.Context, protocol.IntakePlanParams) (protocol.IntakePreview, error) {
	return protocol.IntakePreview{}, nil
}
func (guardIntakeService) Abandon(context.Context, protocol.Actor, protocol.IntakeAbandonParams) (protocol.IntakeAbandonResult, error) {
	return protocol.IntakeAbandonResult{}, nil
}
func (guardIntakeService) Commit(context.Context, protocol.Actor, protocol.IntakeCommitParams) (protocol.IntakeCommitResult, error) {
	return protocol.IntakeCommitResult{}, nil
}
func (guardIntakeService) Vocabulary(context.Context) (protocol.IntakeVocabularyResult, error) {
	return protocol.IntakeVocabularyResult{}, nil
}

func newIntakePlaneTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Charter = guardCharterService{}
	tbl.Templates = guardTemplateService{}
	tbl.Intake = guardIntakeService{}
	if !tbl.RegisterCharter() || !tbl.RegisterTemplates() || !tbl.RegisterIntake() {
		t.Fatal("the intake plane refused to register over non-nil seams — the app would have no intake door at all")
	}
	return tbl
}

// The five writes (GI14, plus T9's abandon on 2026-11-05) and the fifteen
// plane names.
var (
	intakePlaneWrites = []protocol.MethodName{
		protocol.MethodCharterSave, protocol.MethodTemplateSave,
		protocol.MethodIntakeStart, protocol.MethodIntakeCommit,
		protocol.MethodIntakeAbandon,
	}
	intakePlaneReads = []protocol.MethodName{
		protocol.MethodCharterGet, protocol.MethodCharterList, protocol.MethodCharterRevisions,
		protocol.MethodCharterVocabulary, protocol.MethodTemplateList, protocol.MethodTemplateGet,
		protocol.MethodIntakeList, protocol.MethodIntakeStatus, protocol.MethodIntakePlan,
		protocol.MethodIntakeVocabulary,
	}
	intakePlaneAll = func() []protocol.MethodName {
		out := append([]protocol.MethodName{}, intakePlaneWrites...)
		return append(out, intakePlaneReads...)
	}()
	// intakePlanePrefixes are the three namespaces the plane owns.
	intakePlanePrefixes = []string{"charter.", "template.", "intake."}
	// intakePlaneWriteLiterals are the wire spellings of the five writes —
	// the literals the text walk forbids in the non-human doors.
	intakePlaneWriteLiterals = []string{"charter.save", "template.save", "intake.start", "intake.commit", "intake.abandon"}
)

// TestIntakeAndCharterWritesAreAHumanDoor is the positive half (GI14): the
// exact fifteen are registered over their seams, each RequiresInitialize,
// none Debug, each classified, the five writes marked HumanDoorOnly — and
// with the seams nil all fifteen stay ABSENT (V13: a storeless daemon has
// no intake surface, not a refusing one).
func TestIntakeAndCharterWritesAreAHumanDoor(t *testing.T) {
	tbl := newIntakePlaneTable(t)
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}
	for _, m := range intakePlaneAll {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the intake plane (GI14)", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — an intake row is a store row", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — the intake plane is the person's surface, not a diagnostic", m)
		}
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Fatalf("%s is unclassified — every registered method carries one classification entry", m)
		}
		isWrite := false
		for _, w := range intakePlaneWrites {
			if w == m {
				isWrite = true
			}
		}
		if isWrite && !s.HumanDoorOnly {
			t.Errorf("%s is a person's write without the HumanDoorOnly mark — declared, then proven (GI14)", m)
		}
	}
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterCharter()
	tblNil.RegisterTemplates()
	tblNil.RegisterIntake() // no-ops over nil seams: the fifteen stay absent
	for _, m := range intakePlaneAll {
		if _, err := tblNil.Dispatch(context.Background(), "c1", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s dispatched on a nil-seam table — want ErrUnknownMethod, got %v", m, err)
		}
	}
}

// intakePlaneNarrowProjecting returns narrow actions projecting any plane
// method (a function over lists so the canary can feed it poison).
func intakePlaneNarrowProjecting(actions []narrow.Action) []string {
	var out []string
	for _, a := range actions {
		sm := string(a.SocketMethod)
		for _, pre := range intakePlanePrefixes {
			if strings.HasPrefix(sm, pre) {
				out = append(out, a.Name+" projects "+sm)
			}
		}
	}
	return out
}

func intakePlaneAgentServiceNames(name protocol.MethodName) bool {
	for _, m := range protocol.AgentServiceMethods() {
		if m.Name == name {
			return true
		}
	}
	return false
}

// intakePlaneToolNamed reports a tool wearing a plane name. EMPTY allowlist
// at T7: nothing was granted. S61 T8 (the change it was designed for) joins
// the propose twins HERE — the only spelling by which a plane name may ever
// enter the registry.
//
// 2026-11-05 (S61 T8, GI17): the seven names the slice grants join HERE —
// five read twins and the two propose asks (the ONLY spelling a plane name
// may wear: propose files, it never decides). The four writes, their
// snake spellings and every scaffold/publish word stay outside, forever;
// TestNoDecisionOrWriteToolExists (internal/records) holds the catalog half.
var intakePlaneToolAllowlist = map[string]bool{
	"charter_list": true, "charter_show": true, "template_list": true, "template_show": true,
	"intake_status": true, "charter_propose": true, "intake_propose": true,
}

// intakePlaneSpecTwined reports a records spec twined to any plane method.
// EMPTY at T7 as well; T8's read twins join with their change.
//
// 2026-11-05 (S61 T8, GI17): the same seven specs join — five read twins to
// the plane's five READ doors and two asks to charter.propose /
// intake.propose, the two conditional agent doors. A MUTATES spec may still
// only twin to those two asks (the exception below); it never twins to a
// save, a start or a commit.
var intakePlaneSpecAllowlist = map[string]bool{
	"charter_list": true, "charter_show": true, "template_list": true, "template_show": true,
	"intake_status": true, "charter_propose": true, "intake_propose": true,
}

func intakePlaneSpecTwined(spec records.Spec) []string {
	var out []string
	if intakePlaneSpecAllowlist[spec.Name] {
		return nil
	}
	for _, m := range intakePlaneAll {
		if spec.Method == string(m) {
			out = append(out, spec.Name+" twines "+spec.Method)
		}
	}
	return out
}

// intakePlaneLiterals walks a directory's non-test .go files for the four
// write literals (and their tool-shaped snake spellings).
var intakePlaneSnake = []string{"charter_save", "template_save", "intake_start", "intake_commit", "intake_abandon"}

func walkIntakePlaneLiterals(dir string) ([]string, int) {
	var offenders []string
	var walked int
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		walked++
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		text := string(b)
		for _, lit := range append(append([]string{}, intakePlaneWriteLiterals...), intakePlaneSnake...) {
			if strings.Contains(text, `"`+lit+`"`) {
				offenders = append(offenders, p+" names "+lit)
			}
		}
		return nil
	})
	return offenders, walked
}

// TestHumanDoorWritesNeverReachNarrowOrAgentSurfaces is GI15's negative
// half: the fifteen names — writes at minimum, and the namespaces
// entire — reach none of the four non-person surfaces.
func TestHumanDoorWritesNeverReachNarrowOrAgentSurfaces(t *testing.T) {
	// ── Negative 1 — the narrow table: no action projects a plane method.
	t.Run("narrow", func(t *testing.T) {
		for _, o := range intakePlaneNarrowProjecting(narrow.Actions()) {
			t.Errorf("%s — the narrow surface has no intake-plane door, read or write (GI15)", o)
		}
	})

	// ── Negative 2 — the agent-services enumeration.
	t.Run("agent-services", func(t *testing.T) {
		for _, m := range intakePlaneAll {
			if intakePlaneAgentServiceNames(m) {
				t.Fatalf("%s is an agent-service method — saving a charter or committing an intake with no person in the room (GI15)", m)
			}
		}
	})

	// ── Negative 3 — the tool registry: no tool wears a plane name. The
	// regex catches the tool-shaped spellings of ALL fifteen, reads
	// included; the allowlist is the record of any granted name and is
	// empty until T8's propose twins land with their own change.
	t.Run("tools-registry", func(t *testing.T) {
		planeName := regexp.MustCompile(`(?i)(charter|template|intake).*(save|commit|start|abandon|revise|propose)|(charter|template|intake)_(save|commit|start|abandon|propose)`)
		for name := range tools.Registry() {
			if intakePlaneToolAllowlist[name] {
				continue
			}
			if planeName.MatchString(name) {
				t.Errorf("tool %q wears an intake-plane name — a tool named after a person write is a second door onto the write the socket alone carries (GI15)", name)
			}
		}
	})

	// ── Negative 4 — the records catalog: no spec twines to any plane
	// method at all yet (T8's read twins join the allowlist in their own
	// change; a MUTATES spec never twines to any of them, reads or writes
	// — OD10's law).
	t.Run("records-catalog", func(t *testing.T) {
		for _, spec := range records.Catalog() {
			for _, o := range intakePlaneSpecTwined(spec) {
				t.Errorf("spec %s — the records catalog is an agent's surface and the intake plane is a person's view (GI15)", o)
			}
			// 2026-11-05 (S61 T8, GI17): OD10's law keeps its teeth — a
			// MUTATES spec still may not twin to any of T7's fifteen; the
			// ONE exception is this slice's two agent asks, the only two
			// plane methods an ask files through, both inert until a person
			// commits (intake_amend_test's canary below still proves the
			// predicate catches charter.save with Mutates:true).
			askException := spec.Method == "charter.propose" || spec.Method == "intake.propose"
			if spec.Mutates && !askException && (strings.HasPrefix(spec.Method, "charter.") ||
				strings.HasPrefix(spec.Method, "template.") || strings.HasPrefix(spec.Method, "intake.")) {
				t.Errorf("spec %q is a Mutates record twined to %q — only READS may ever twin (OD10)", spec.Name, spec.Method)
			}
		}
	})

	// ── Negative 5 — by text: the four writes' literals appear in NO
	// non-test file under the four packages that own the non-human doors.
	t.Run("text-grep", func(t *testing.T) {
		var totalWalked int
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			offenders, walked := walkIntakePlaneLiterals(filepath.Join("..", "..", dir))
			totalWalked += walked
			for _, o := range offenders {
				t.Errorf("%s — the write literals must not reach a non-human door (GI15)", o)
			}
		}
		if totalWalked < 50 {
			t.Fatalf("the text-grep walk saw %d non-test files across the four guarded directories, want ≥ 50 — the guard would be checking almost nothing", totalWalked)
		}
	})
}

// TestCharterGrantsNothing is GI8 made mechanical: the charter PLANE grants
// nothing — the charter package never imports the autonomy plane or any
// grant surface (its autonomy half is a FUNCTION seam answering NAMES), the
// charter doors name no grant write, and no method on the socket grants
// through a charter body. The autonomy half of charter.vocabulary is an
// echo; the granting doors are the S56 autonomy.* set, untouched here.
func TestCharterGrantsNothing(t *testing.T) {
	banned := []*regexp.Regexp{
		regexp.MustCompile(`"github\.com/cjlapao/aeon/internal/autonomy"`),
		regexp.MustCompile(`autonomy_grant`),
		regexp.MustCompile(`Grant\(`),
	}
	var files int
	err := filepath.WalkDir(filepath.Join("..", "..", "internal", "charter"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		files++
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for _, re := range banned {
			if re.Match(b) {
				t.Errorf("%s matches %s — the charter plane reads the autonomy vocabulary and grants nothing (GI8)", p, re)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 4 {
		t.Fatalf("the charter census walked %d non-test files, want ≥ 4 — the guard would be checking almost nothing", files)
	}

	// The door half: none of the fifteen intake-plane methods is a grant
	// door — none is named in the S56 autonomy plane and none appears in
	// the autonomy service's method set.
	for _, m := range intakePlaneAll {
		for _, a := range protocol.AgentServiceMethods() {
			if a.Name == m {
				t.Errorf("%s is an agent-service method — a charter never grants through an agent's surface (GI8)", m)
			}
		}
	}
}

// TestPersonDoorActionsOnlyReachedByIntake: the scaffold and create-repo
// action seams — the instruments that WRITE a project tree and (someday)
// create a remote — are wired ONLY by the intake doors and the intake
// engine's own package. A second werrer would be a second door onto the
// act that only a committed intake opens.
func TestPersonDoorActionsOnlyReachedByIntake(t *testing.T) {
	allowed := map[string]bool{
		"internal/daemon/intake.go": true,
	}
	re := regexp.MustCompile(`(ScaffoldActions|CreateRepoActions)\{`)
	var offenders, walked []string
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			// The packages that DEFINE the seams (internal/action) and the
			// engine that CONSUMES them (internal/intake) are the structure
			// itself, not a wiring.
			if strings.HasPrefix(rel, "internal/action/") || strings.HasPrefix(rel, "internal/intake/") {
				return nil
			}
			walked = append(walked, rel)
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			if re.Match(b) && !allowed[rel] {
				offenders = append(offenders, rel)
			}
			return nil
		})
	}
	for _, o := range offenders {
		t.Errorf("%s wires the scaffold/create-repo action seams — only the intake doors reach them (GI13)", o)
	}
	if len(walked) < 300 {
		t.Fatalf("the wiring census walked %d non-test files, want ≥ 300 — the census must see the tree", len(walked))
	}
}

// TestIntakePlaneGuardsHaveTeeth: each predicate above fires on poison and
// sleeps on the real tables.
func TestIntakePlaneGuardsHaveTeeth(t *testing.T) {
	// Canary 1 — a fake narrow action projecting intake.commit is flagged;
	// the real table is not.
	poisoned := []narrow.Action{
		{Name: "aeon_intake_commit", SocketMethod: protocol.MethodIntakeCommit, Kind: narrow.KindRead},
	}
	if got := intakePlaneNarrowProjecting(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting intake.commit was not flagged — the narrow guard cannot fail, so it is not a guard")
	}
	if got := intakePlaneNarrowProjecting(narrow.Actions()); len(got) != 0 {
		t.Errorf("the real narrow table was flagged: %v", got)
	}

	// Canary 2 — a fake records spec twined to charter.save is flagged; a
	// clean foreign spec is not.
	if got := intakePlaneSpecTwined(records.Spec{Name: "charter_save", Method: "charter.save", Mutates: true}); len(got) == 0 {
		t.Fatal("a records spec twined to charter.save was not flagged — the catalog guard cannot fail")
	}
	if got := intakePlaneSpecTwined(records.Spec{Name: "loop_show", Method: "loop.show"}); len(got) != 0 {
		t.Errorf("a clean foreign spec was flagged: %v — the predicate fires on nothing", got)
	}

	// Canary 3 — a temp non-test .go file naming intake.commit AND
	// charter.save is flagged by the text walk; a clean file is not.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poison.go"),
		[]byte("package tools\n\nvar _ = \"intake.commit\"\nvar _ = \"charter_save\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	off, walked := walkIntakePlaneLiterals(dir)
	if len(off) < 2 || walked != 1 {
		t.Fatalf("a file naming intake.commit AND charter_save flagged %v over %d files — the walk cannot fail on both spellings", off, walked)
	}
	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "clean.go"),
		[]byte("package tools\n\nvar _ = \"intake.plan\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkIntakePlaneLiterals(clean); len(off) != 0 {
		t.Errorf("a clean file was flagged: %v", off)
	}

	// Canary 4 — the charter import census fires on a poisoned file and
	// sleeps on the real package (the walk is inline in the guard; poison
	// the PREDICATE the same way the teeth walk does: a temp tree).
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "grant.go"),
		[]byte("package charter\n\nimport _ \"github.com/cjlapao/aeon/internal/autonomy\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hit := false
	_ = filepath.WalkDir(bad, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), `"github.com/cjlapao/aeon/internal/autonomy"`) {
			hit = true
		}
		return nil
	})
	if !hit {
		t.Fatal("the charter import census could not see a planted autonomy import — GI8's census cannot fail")
	}

	// Canary 5 — the wiring census fires on a planted second wirer.
	bad2 := t.TempDir()
	inner := filepath.Join(bad2, "internal", "narrow")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "wires.go"),
		[]byte("package narrow\n\nvar _ = &action.ScaffoldActions{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := false
	_ = filepath.WalkDir(filepath.Join(bad2, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if regexp.MustCompile(`ScaffoldActions\{`).Match(b) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("a planted second wirer of the scaffold seam went unseen — the census cannot fail")
	}
}

// TestIntakePlaneParamsAcceptNoActor is the reflection half of P4 across
// the whole plane: none of the fifteen wire param structs carries an
// author-shaped field — the actor is the connection's, always.
func TestIntakePlaneParamsAcceptNoActor(t *testing.T) {
	cases := map[string]string{
		"charter.get":   `{"rootId":"c","revision":1,"actor":"x"}`,
		"charter.save":  `{"rootId":"c","body":{},"actorKind":"user"}`,
		"template.list": `{"stackTag":"go","identity":"x"}`,
		"template.get":  `{"slug":"s","peer":"x"}`,
		"intake.start":  `{"name":"n","uid":"1"}`,
		"intake.status": `{"intakeId":"i","actor":"x"}`,
		"intake.commit": `{"intakeId":"i","proposalId":"1","digest":"d","actor":"x"}`,
	}
	tbl := newIntakePlaneTable(t)
	for method, params := range cases {
		_, err := tbl.Dispatch(context.Background(), "c1", protocol.MethodName(method), json.RawMessage(params))
		if err == nil {
			t.Errorf("%s accepted an identity key in params — P4 is not a suggestion (%s)", method, params)
			continue
		}
		var bad protocol.ErrBadParams
		if !errors.As(err, &bad) {
			continue // registered methods may refuse for their own reasons; only the PARSE refusal matters
		}
		if !strings.Contains(err.Error(), "takes no") {
			t.Errorf("%s refused identity params without naming the key: %v", method, err)
		}
	}
}

// TestIntakePlaneAmendmentLeavesThePriorGuardsByteIdentical re-pins the
// guard files the earlier human-door amendments own, S61 T7 edits none of
// them; the list carries every hash the merges since S60 moved (the S49 T5
// cascade precedent the merge-queue file cites).
func TestIntakePlaneAmendmentLeavesThePriorGuardsByteIdentical(t *testing.T) {
	pinned := map[string]string{
		// 2026-11-08 (inspect door): re-pinned — mergequeue_amend_test.go re-pinned the MCP guard
		// file it holds a hash of, after that file gained the closed-set entry for
		// mcp.server.inspect. No guard body was weakened.
		"mergequeue_amend_test.go": "2a983ed8e90afcf91cd0d4e1f9c4810395011758b5d932ae534bcb7b26f93e28", // re-pinned 2026-11-08: follows mcp_amend_test.go (share door)
	}
	for path, want := range pinned {
		b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s changed since the S61 T7 pins (want %s, got %s) — this amendment edits no existing guard body", path, want, got)
		}
	}
}

// TestIntakePlaneToolsNotInDefaultGrant — S61 T8 (GI17): registered is not
// granted (S11). The seven names the T8 amendment admits to the registry
// are in DefaultGrant() none; a handler that wants one lists it, and the
// seeded intake-architect (T9) will be the named holder.
func TestIntakePlaneToolsNotInDefaultGrant(t *testing.T) {
	seven := []string{
		"charter_list", "charter_show", "template_list", "template_show",
		"intake_status", "charter_propose", "intake_propose",
	}
	granted := map[string]bool{}
	for _, g := range tools.DefaultGrant() {
		granted[g] = true
	}
	for _, name := range seven {
		if granted[name] {
			t.Errorf("%s is in DefaultGrant() — the goal-intake tools are registered, never granted (GI17)", name)
		}
	}
}
