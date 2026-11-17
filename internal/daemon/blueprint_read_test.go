package daemon

// blueprint_read_test.go — S48 T7: the read doors. list keeps `names` and
// grows summaries (filtering is daemon-side); the diverged flag is the
// ledger's truth and is OMITTED, never silently false; show keeps its old
// shape and adds the byte-equal report and the derived refs; usage lists
// the live users and what the row uses (empty, not error, when nobody
// uses it); history is newest-first and survives deletion; one revision
// answers verbatim; diff is computed here (shipped is system-only, with a
// named refusal); lint is advisory, dated, verbatim, and says so when the
// catalog is unwired; and NO read writes anything.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

func bpReads(t *testing.T) (*Daemon, *store.Store, *blueprintWrites, *blueprintReads) {
	t.Helper()
	d, s, w := bpWritesEnv(t)
	r, ok := d.Table.BlueprintReads.(*blueprintReads)
	if !ok {
		t.Fatal("the daemon did not wire its read service behind the doors")
	}
	return d, s, w, r
}

// bpShippedBytes is what the seed would apply — the embedded bytes behind
// the T3 Source seam, the same authority the diff and the diverged flag
// answer from.
func bpShippedBytes(t *testing.T, name string) string {
	t.Helper()
	raw, ok, err := blueprint.EmbeddedSource().Get(name)
	if err != nil || !ok {
		t.Fatalf("no shipped record for %q (ok=%v err=%v)", name, ok, err)
	}
	return string(raw)
}

func TestListKeepsNamesAndAddsSummaries(t *testing.T) {
	_, _, w, r := bpReads(t)
	ctx := context.Background()
	if err := w.SeedSystem(ctx, "two-step", bpShippedBytes(t, "two-step")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "night-owl", Content: bpGate("night-owl", "go")}); err != nil {
		t.Fatal(err)
	}
	got, err := r.List(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	// names stays: every name, sorted, exactly as before.
	if len(got.Names) < 2 {
		t.Fatalf("names thinned: %v", got.Names)
	}
	sorted := true
	for i := 1; i < len(got.Names); i++ {
		if got.Names[i-1] > got.Names[i] {
			sorted = false
		}
	}
	if !sorted || !strings.Contains(strings.Join(got.Names, ","), "night-owl") || !strings.Contains(strings.Join(got.Names, ","), "two-step") {
		t.Fatalf("names drifted (sorted, ours present?): %v", got.Names)
	}
	if len(got.Blueprints) != len(got.Names) {
		t.Fatalf("summaries and names disagree: %d vs %d", len(got.Blueprints), len(got.Names))
	}
	var owl *protocol.BlueprintSummary
	for i := range got.Blueprints {
		if got.Blueprints[i].Name == "night-owl" {
			owl = &got.Blueprints[i]
		}
	}
	if owl == nil || owl.Origin != "user" || owl.Revision < 1 || owl.ContentHash == "" {
		t.Fatalf("the user summary is wrong: %+v", owl)
	}
	// Filters run DAEMON-side: q is a case-insensitive substring on the name.
	filtered, err := r.List(protocol.BlueprintListParams{Q: "NIGHT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Names) != 1 || filtered.Names[0] != "night-owl" {
		t.Fatalf("q filtered client-side or not at all: %v", filtered.Names)
	}
	onlyUser, err := r.List(protocol.BlueprintListParams{Origin: "user"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range onlyUser.Names {
		if n == "two-step" {
			t.Fatal("origin=user leaked a system row")
		}
	}
}

func TestDivergedFlagReflectsTheLedgerAndIsOmittedWhenUnknown(t *testing.T) {
	_, s, w, r := bpReads(t)
	ctx := context.Background()
	shippedBytes := bpShippedBytes(t, "two-step")
	if err := w.SeedSystem(ctx, "two-step", shippedBytes); err != nil {
		t.Fatal(err)
	}
	sumOf := func(name string) *protocol.BlueprintSummary {
		got, err := r.List(protocol.BlueprintListParams{})
		if err != nil {
			t.Fatal(err)
		}
		for i := range got.Blueprints {
			if got.Blueprints[i].Name == name {
				return &got.Blueprints[i]
			}
		}
		t.Fatalf("%q missing from the list", name)
		return nil
	}
	if got := sumOf("two-step").DivergedFromShipped; got == nil || *got {
		t.Fatalf("a pristine system row reads diverged=%v, want false", got)
	}
	if err := w.SeedSystem(ctx, "two-step", shippedBytes+"# operator\n"); err != nil {
		t.Fatal(err)
	}
	if got := sumOf("two-step").DivergedFromShipped; got == nil || !*got {
		t.Fatalf("an edited system row reads diverged=%v, want true", got)
	}
	// User rows answer false — the flag is the seed ledger's question.
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "mine", Content: bpGate("mine", "go")}); err != nil {
		t.Fatal(err)
	}
	if got := sumOf("mine").DivergedFromShipped; got == nil || *got {
		t.Fatalf("a user row reads diverged=%v, want false", got)
	}
	// No shipped authority ⇒ the field is OMITTED — never the silent false.
	blind := &blueprintReads{store: s, shipped: nil, now: time.Now}
	got, err := blind.List(protocol.BlueprintListParams{Q: "two-step"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blueprints) != 1 || got.Blueprints[0].DivergedFromShipped != nil {
		t.Fatalf("unknown diverged read as %v, want omitted", got.Blueprints[0].DivergedFromShipped)
	}
	wire, _ := json.Marshal(got.Blueprints[0])
	if strings.Contains(string(wire), "divergedFromShipped") {
		t.Fatalf("the omitted flag still rode the wire: %s", wire)
	}
}

func TestShowKeepsShapeAndAddsReportAndRefs(t *testing.T) {
	_, _, w, r := bpReads(t)
	ctx := context.Background()
	content := "id: showme\nschemaVersion: 1\nversion: 3\nlanes:\n  - id: work\n    step: {kind: model-step, agent: default}\n"
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "showme", Content: content}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Show(protocol.BlueprintShowParams{Name: "showme"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "" {
		t.Fatalf("path must stay \"\", got %q", got.Path)
	}
	if got.Content != content {
		t.Fatal("show.content is not the row's bytes verbatim")
	}
	if got.Version != 3 || got.Revision != 1 || got.Origin != "user" {
		t.Fatalf("summary fields wrong: %+v", got)
	}
	// The report is Analyze's OWN bytes — no second validator, no paraphrase.
	want, _ := json.Marshal(blueprint.Analyze([]byte(content), blueprintValidationOptions("")))
	if string(got.Report) != string(want) {
		t.Fatalf("the report drifted from Analyze:\n got %s\nwant %s", got.Report, want)
	}
	var sawAgent bool
	for _, ref := range got.References {
		if ref.Kind == "agent" && ref.Target == "default" {
			sawAgent = true
		}
	}
	if !sawAgent {
		t.Fatalf("the derived refs did not ride along: %+v", got.References)
	}
}

func TestUsageListsLiveReferencesAndAgentsOnly(t *testing.T) {
	_, s, w, r := bpReads(t)
	ctx := context.Background()
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "nightly", Content: "id: nightly\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: model-step, agent: default}\n"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{ID: "sched-a", Kind: "schedule", Blueprint: "nightly", Expr: "0 3 * * *", Zone: "UTC", CreatedAt: "2026-11-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEventTrigger(store.Actor{Kind: "user", ID: "1000"}, store.NewEventTrigger{
		Name: "pr-open", Blueprint: "nightly", Match: store.Match{Event: "goal.approved"}, MaxRunsPerHour: 5,
	}); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/repo/g", "/repo/h"} {
		if _, err := s.CreateComponent(store.NewComponent{Name: root, Slug: strings.TrimPrefix(root, "/"), Root: root}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AppendQueueItem(store.NewQueueItemRow{ComponentRoot: "/repo/g", Status: "running", Blueprint: "nightly", Goal: "clean up", WorkspaceRoot: "/repo/g/work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendQueueItem(store.NewQueueItemRow{ComponentRoot: "/repo/h", Status: "done", Blueprint: "nightly", Goal: "finished", WorkspaceRoot: "/repo/h/work"}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Usage(ctx, protocol.BlueprintUsageParams{Name: "nightly"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Schedules) != 1 || got.Schedules[0].ID != "sched-a" {
		t.Fatalf("schedules: %+v", got.Schedules)
	}
	if len(got.Triggers) != 1 || got.Triggers[0].Name != "pr-open" {
		t.Fatalf("triggers: %+v", got.Triggers)
	}
	if len(got.QueueItems) != 1 || got.QueueItems[0].Status != "running" {
		t.Fatalf("queueItems must carry only the OPEN ones: %+v", got.QueueItems)
	}
	if len(got.Agents) != 1 || got.Agents[0] != "default" {
		t.Fatalf("agents from the derived refs: %+v", got.Agents)
	}
	// A blueprint nothing uses answers EMPTY arrays, never an error.
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "lonely", Content: bpGate("lonely", "go")}); err != nil {
		t.Fatal(err)
	}
	empty, err := r.Usage(ctx, protocol.BlueprintUsageParams{Name: "lonely"})
	if err != nil {
		t.Fatalf("an unused blueprint must answer, not refuse: %v", err)
	}
	if len(empty.Schedules)+len(empty.Triggers)+len(empty.QueueItems) != 0 || empty.LoopsStarted != 0 {
		t.Fatalf("the unused answer carried users: %+v", empty)
	}
	if empty.Agents == nil || empty.Actions == nil || empty.Skills == nil {
		t.Fatalf("empty arrays must still be arrays: %+v", empty)
	}
}

func TestHistoryNewestFirstAndSurvivesDeletion(t *testing.T) {
	_, _, w, r := bpReads(t)
	ctx := context.Background()
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "hist", Content: bpGate("hist", "one")}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "hist", Content: bpGate("hist", "two")}); err != nil {
		t.Fatal(err)
	}
	got, err := r.History(ctx, protocol.BlueprintHistoryParams{Name: "hist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Revisions) != 2 || got.Revisions[0].Rev != 2 || got.Revisions[1].Rev != 1 {
		t.Fatalf("history is not newest-first: %+v", got.Revisions)
	}
	if _, err := w.Delete(ctx, bpActor, protocol.BlueprintDeleteParams{Name: "hist"}); err != nil {
		t.Fatal(err)
	}
	after, err := r.History(ctx, protocol.BlueprintHistoryParams{Name: "hist"})
	if err != nil {
		t.Fatalf("deletion destroyed the history: %v", err)
	}
	if len(after.Revisions) != 2 {
		t.Fatalf("the deleted history lost rows: %+v", after.Revisions)
	}
	// A name that never existed is the named unknown refusal.
	_, err = r.History(ctx, protocol.BlueprintHistoryParams{Name: "never-was"})
	if e, ok := err.(*protocol.Error); !ok || e.Code != protocol.ErrBlueprintUnknown.Code {
		t.Fatalf("want ErrBlueprintUnknown, got %T %v", err, err)
	}
}

func TestRevisionReturnsContentVerbatim(t *testing.T) {
	_, _, w, r := bpReads(t)
	ctx := context.Background()
	content := bpGate("verb", "go") + "# exact bytes — trailing comment kept\n"
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "verb", Content: content}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Revision(ctx, protocol.BlueprintRevisionParams{Name: "verb", Rev: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != content {
		t.Fatalf("the revision altered the bytes:\n got %q\nwant %q", got.Content, content)
	}
	if (got.Op != "create" && got.Op != "save") || got.ActorKind != "user" || got.ActorID != "operator" {
		t.Fatalf("the index entry is wrong: %+v", got)
	}
}

func TestDiffAgainstShippedForSystemRefusesForUser(t *testing.T) {
	_, _, w, r := bpReads(t)
	ctx := context.Background()
	shippedBytes := bpShippedBytes(t, "two-step")
	if err := w.SeedSystem(ctx, "two-step", shippedBytes); err != nil {
		t.Fatal(err)
	}
	if err := w.SeedSystem(ctx, "two-step", shippedBytes+"# operator edit\n"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Diff(ctx, protocol.BlueprintDiffParams{Name: "two-step", A: "current", B: "shipped"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Equal || got.Diff == "" || got.ALabel != "current" || got.BLabel != "shipped" {
		t.Fatalf("the shipped diff is not a real diff: %+v", got)
	}
	if !strings.Contains(got.Diff, "-# operator edit") {
		t.Fatalf("the edit does not appear in the diff: %s", got.Diff)
	}
	// A user blueprint has no shipped version: a NAMED refusal, not a diff of nothing.
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "useronly", Content: bpGate("useronly", "go")}); err != nil {
		t.Fatal(err)
	}
	_, err = r.Diff(ctx, protocol.BlueprintDiffParams{Name: "useronly", A: "current", B: "shipped"})
	e := mustRefused(t, err, "system")
	if !strings.Contains(e.Data.What, "user blueprint") {
		t.Fatalf("the refusal did not name the reason: %s", e.Data.What)
	}
}

func TestDiffEqualIsEmptyAndFlagged(t *testing.T) {
	_, _, w, r := bpReads(t)
	ctx := context.Background()
	content := bpGate("same", "go")
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "same", Content: content}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Diff(ctx, protocol.BlueprintDiffParams{Name: "same", A: "current", B: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal || got.Diff != "" {
		t.Fatalf("identical bytes must answer empty and flagged: %+v", got)
	}
}

// bpLintYAML names a tool the default grant does not hold (goal_create,
// from the one emitTool table) — lint's advisory states include
// not_granted, so this content must speak.
const bpLintYAML = "id: linter\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: model-step, agent: proposal-author, emits: [{type: goal}]}\n"

// bpGrantGoalCreate puts goal_create on the proposal-author agent's row —
// the code registry (handler.ToolsFor, the save door's OL9 check) grants
// that body goal_create, and lint's ToolState asks the STORE row: with the
// row present and no policy rows, the state reads granted_no_rule — one of
// the advisory states. (The fresh test store has no first-party rows: the
// CLI's migrate puts them there, not New.)
func bpGrantGoalCreate(t *testing.T, s *store.Store) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	a, ok, err := s.AgentBySlug("proposal-author")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		a = store.Agent{Slug: "proposal-author", Name: "Proposal Author", Origin: "system"}
		if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, a, now); err != nil {
			t.Fatal(err)
		}
	}
	a.Tools = `["read", "write", "edit", "glob", "grep", "bash", "webfetch", "todo", "question", "goal_create", "goal_list", "epic_list", "attach"]`
	if _, err := s.UpdateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, a, now); err != nil {
		t.Fatal(err)
	}
}

func TestLintAdvisoriesVerbatimAndDated(t *testing.T) {
	_, s, w, r := bpReads(t)
	ctx := context.Background()
	bpGrantGoalCreate(t, s)
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "linter", Content: bpLintYAML}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Lint(ctx, protocol.BlueprintLintParams{Name: "linter"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Unavailable != "" {
		t.Fatalf("the mounted daemon answered unavailable: %+v", got)
	}
	if _, err := time.Parse(time.RFC3339, got.EvaluatedAt); err != nil {
		t.Fatalf("evaluatedAt is not a date: %q", got.EvaluatedAt)
	}
	var hit *protocol.BlueprintAdvisory
	for i := range got.Advisories {
		if got.Advisories[i].Tool == "goal_create" {
			hit = &got.Advisories[i]
		}
	}
	if hit == nil {
		t.Fatalf("the ungranted emit produced no advisory: %+v", got.Advisories)
	}
	if hit.Agent != "proposal-author" || hit.Kind != "grant" || hit.EvaluatedAt != got.EvaluatedAt {
		t.Fatalf("advisory shape: %+v", hit)
	}
	// Verbatim: the state and reason are the catalog's own answer for the
	// SAME (agent, tool) pair through the SAME resolver — no paraphrase.
	d, _, _, _ := bpReads(t)
	svc, ok := d.Table.ToolsCatalog.(*toolsCatalogService)
	if !ok {
		t.Fatal("no mounted catalog to compare the sentence against")
	}
	world, err := svc.world(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := toolcatalog.Effective("goal_create", "proposal-author", world.inputs)
	if hit.State != string(want.State) || hit.Reason != want.Reason {
		t.Fatalf("lint reworded the catalog: %+v vs %s/%q", hit, want.State, want.Reason)
	}
}

func TestLintUnwiredIsExplicitNotAllClear(t *testing.T) {
	_, s, w, _ := bpReads(t)
	ctx := context.Background()
	bpGrantGoalCreate(t, s)
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "linter", Content: bpLintYAML}); err != nil {
		t.Fatal(err)
	}
	blind := &blueprintReads{store: s, now: time.Now} // no catalog, no shipped authority
	got, err := blind.Lint(ctx, protocol.BlueprintLintParams{Name: "linter"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Advisories) != 0 {
		t.Fatalf("an unwired catalog invented advisories: %+v", got.Advisories)
	}
	if got.Unavailable != "the tools catalog is not wired — policy advisories are unavailable" {
		t.Fatalf("the unwired state is not the explicit sentence: %q", got.Unavailable)
	}
	if got.EvaluatedAt == "" {
		t.Fatal("even the unavailable answer must be dated")
	}
}

func TestLintNeverChangesValidateOrSave(t *testing.T) {
	_, s, w, r := bpReads(t)
	ctx := context.Background()
	bpGrantGoalCreate(t, s)
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "linter", Content: bpLintYAML}); err != nil {
		t.Fatal(err)
	}
	before, err := r.Show(protocol.BlueprintShowParams{Name: "linter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lint(ctx, protocol.BlueprintLintParams{Name: "linter"}); err != nil {
		t.Fatal(err)
	}
	after, err := r.Show(protocol.BlueprintShowParams{Name: "linter"})
	if err != nil {
		t.Fatal(err)
	}
	if string(before.Report) != string(after.Report) {
		t.Fatal("lint changed the stored report — it is ADVISORY")
	}
	// And the save door's verdict is untouched by anything lint said.
	res, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "linter", Content: bpLintYAML + "# again\n"})
	if err != nil {
		t.Fatalf("lint gated a valid save: %v", err)
	}
	want, _ := json.Marshal(blueprint.Analyze([]byte(bpLintYAML+"# again\n"), blueprintValidationOptions("")))
	if string(res.Report) != string(want) {
		t.Fatal("the save report drifted from Analyze")
	}
}

func TestReadsWriteNothingAndAreLive(t *testing.T) {
	_, s, w, r := bpReads(t)
	ctx := context.Background()
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "livey", Content: bpGate("livey", "one")}); err != nil {
		t.Fatal(err)
	}
	revsBefore, _ := s.RevisionList(ctx, "livey", 100)
	namesBefore, _ := s.BlueprintNames(ctx)
	ledgerBefore, _ := s.SeedLedgerList(ctx, "blueprints.standard")

	_, _ = r.List(protocol.BlueprintListParams{})
	_, _ = r.Show(protocol.BlueprintShowParams{Name: "livey"})
	_, _ = r.Usage(ctx, protocol.BlueprintUsageParams{Name: "livey"})
	_, _ = r.History(ctx, protocol.BlueprintHistoryParams{Name: "livey"})
	_, _ = r.Revision(ctx, protocol.BlueprintRevisionParams{Name: "livey", Rev: 1})
	_, _ = r.Diff(ctx, protocol.BlueprintDiffParams{Name: "livey", A: "current", B: "1"})
	_, _ = r.Lint(ctx, protocol.BlueprintLintParams{Name: "livey"})

	revsAfter, _ := s.RevisionList(ctx, "livey", 100)
	namesAfter, _ := s.BlueprintNames(ctx)
	ledgerAfter, _ := s.SeedLedgerList(ctx, "blueprints.standard")
	if len(revsBefore) != len(revsAfter) || len(namesBefore) != len(namesAfter) || len(ledgerBefore) != len(ledgerAfter) {
		t.Fatalf("a read wrote: revisions %d→%d, names %d→%d, ledger %d→%d",
			len(revsBefore), len(revsAfter), len(namesBefore), len(namesAfter), len(ledgerBefore), len(ledgerAfter))
	}

	// And live: a save the very next read must see (no cache anywhere here).
	if _, err := w.Save(ctx, bpActor, protocol.BlueprintSaveParams{Name: "livey", Content: bpGate("livey", "two")}); err != nil {
		t.Fatal(err)
	}
	show, err := r.Show(protocol.BlueprintShowParams{Name: "livey"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show.Content, "two") {
		t.Fatal("the next read did not see the save — something cached")
	}
}
