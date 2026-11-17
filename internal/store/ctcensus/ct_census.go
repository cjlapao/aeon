package ctcensus

// # The component-tree census builder (S44-extra T1)
//
// This is the ONE shared, importable builder every `ct_*` golden (store,
// daemon, CLI, records, telemetry) reads the SAME rows from. It lives in an
// importable package — NOT `testdata/`, which Go excludes from the build and
// so cannot hold code the other packages import — because the store/daemon/cmd
// goldens all import it. The task named it `internal/store/testdata/ct_census`;
// that location is a deviation (recorded in T1's Notes): a `testdata/` dir
// cannot be imported, so the builder moves up to `internal/store/ctcensus`
// while the DATA files (the goldens, expected.json) stay under `testdata/`.
//
// # Characterization, not implementation
//
// T1 is Phase A only. This builder creates every component-tree shape THROUGH
// today's store API — never raw SQL where an API exists — so the goldens are
// GREEN on the UNTOUCHED code. It does NOT run the migration (T2/T3). The
// expected.json it writes is the CT5 mapping written BY HAND (the collapse /
// keep / drop rule below), not computed by the migration code it will later
// check: a golden that reuses its own implementation can never fail it.
//
// # Determinism
//
// Every row id the API mints is random (newID, crypto/rand), so a golden that
// serialised raw ids would differ run to run and fail on its own untouched
// code. Two seams make the output reproducible:
//   - I9: a FIXED clock (Options.Clock), so every timestamp is identical.
//   - a SCRUB MAP (raw id -> stable label), assigned by creation order per
//     kind. The goldens scrub the raw ids before comparing, so the golden is
//     byte-stable across runs and changes ONLY when the code changes.
//
// Output is also sorted by a stable key (the scrubbed label) so a read ordered
// by a random id is reproducible.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// fixedClock is the deterministic clock (I9): every write carries the same
// instant, so two censuses built back to back carry identical timestamps.
func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 15, 9, 41, 12, 345_000_000, time.UTC) }
}

// actorUser is the one human author the records tier admits (OL8/OL7): a goal
// or a decision needs a USER, never an agent or trigger.
var actorUser = store.Actor{Kind: "user", ID: "u1"}

// Census is the built fixture: the store it owns and the labels that make its
// rows reproducible. A test keeps S open to read the rows, then closes it (or
// hands the dir to a daemon, as the CLI goldens do).
type Census struct {
	// S is the open store. The goldens read its rows through the store API.
	S *store.Store
	// Dir is where S's database lives; a daemon can reopen it after S.Close().
	Dir string
	// Scrub maps every random id the builder minted to a stable label, by
	// creation order per kind. The goldens replace raw ids with these labels
	// before comparing, which is what makes them reproducible.
	Scrub map[string]string
	// Expected is the CT5 mapping written by hand (below), for expected.json.
	Expected []ExpectedRow
	// Projects records each project's kind, so a test can assert a shape.
	Projects map[string]ProjectInfo

	// label counters: one per kind, in creation order, so the labels
	// (p1, c1, g1, …) are stable across runs.
	projectsSeen int
	compSeen     int
	goalSeen     int
	epicSeen     int
	trigSeen     int
	taskSeen     int

	t *testing.T
}

// ProjectInfo names a project's shape: whether it is explicit, how many
// components it holds, and whether any row references it.
type ProjectInfo struct {
	ID         string
	Implicit   bool
	Components int
	Referenced bool
}

// ExpectedRow is one project's CT5 destination, hand-written (never computed by
// the migration it will check): the table the row must end up in after T2/T3.
type ExpectedRow struct {
	ProjectID  string `json:"projectId"`
	Implicit   bool   `json:"implicit"`
	Components int    `json:"components"`
	Referenced bool   `json:"referenced"`
	// Disposition is the hand-written rule's answer:
	//   collapse — implicit + exactly one component -> that component's id.
	//   keep     — explicit, OR implicit>=2, OR implicit-0-but-referenced
	//             -> the project's OWN id becomes a component id.
	//   drop     — implicit + 0 components + unreferenced -> the row is gone.
	Disposition string `json:"disposition"`
	// TargetID is the id the row resolves to: the component for a collapse,
	// the project itself for a keep, empty for a drop.
	TargetID string `json:"targetId,omitempty"`
}

// Build opens a store at a fresh temp dir, creates every component-tree shape
// through today's store API, and returns the census. It registers no cleanup:
// the caller owns S's lifetime (a store golden keeps it open; a CLI golden
// closes it before handing the dir to a daemon).
func Build(t *testing.T) *Census {
	t.Helper()
	c := &Census{Scrub: map[string]string{}, Projects: map[string]ProjectInfo{}, t: t}
	s, err := store.Open(store.Options{
		Dir:   t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: fixedClock(),
	})
	if err != nil {
		t.Fatalf("ctcensus: open store: %v", err)
	}
	c.S = s
	c.Dir = s.Path()
	c.build()
	return c
}

// build creates every shape and records its labels and expected destination.
func (c *Census) build() {
	s := c.S
	var err error

	// ---- roots (a parentless component) --------------------------------
	// A parentless component is a ROOT. CT1: a root with >=1 child is a
	// "project"; a root with no child is an empty project.
	ep0, err := c.createRoot("ep0", "ep0", "/root/ep0")
	if err != nil {
		c.fail(err)
	}
	ep1, err := c.createRoot("ep1", "ep1", "/root/ep1")
	if err != nil {
		c.fail(err)
	}
	ep3, err := c.createRoot("ep3", "ep3", "/root/ep3")
	if err != nil {
		c.fail(err)
	}
	gy, err := c.createRoot("gy", "gy", "/root/gy")
	if err != nil {
		c.fail(err)
	}
	_ = ep0
	_ = ep3
	_ = gy

	// ---- children ------------------------------------------------------
	// one child under ep1.
	c1, err := c.createChild("c1", "c1", "/repo/c1", ep1)
	if err != nil {
		c.fail(err)
	}
	// three children under ep3.
	c2, err := c.createChild("c2", "c2", "/repo/c2", ep3)
	if err != nil {
		c.fail(err)
	}
	c3, err := c.createChild("c3", "c3", "/repo/c3", ep3)
	if err != nil {
		c.fail(err)
	}
	c4, err := c.createChild("c4", "c4", "/repo/c4", ep3)
	if err != nil {
		c.fail(err)
	}
	_ = c3
	_ = c4

	// ---- roots that become empty by losing their child ------------------
	// c5/c6/c7 are roots with a single child each; moving the child out
	// leaves the root with zero children (an empty project).
	c5, err := c.createRoot("c5", "c5", "/repo/c5")
	if err != nil {
		c.fail(err)
	}
	c6, err := c.createRoot("c6", "c6", "/repo/c6")
	if err != nil {
		c.fail(err)
	}
	c7, err := c.createRoot("c7", "c7", "/repo/c7")
	if err != nil {
		c.fail(err)
	}
	c8, err := c.createRoot("c8", "c8", "/repo/c8")
	if err != nil {
		c.fail(err)
	}
	_ = c8

	// ---- epics and goals on roots -------------------------------------
	e1, err := s.CreateEpic(store.NewEpic{ComponentID: ep1.ID, Title: "epic in ep1", Owner: "u1"})
	if err != nil {
		c.fail(err)
	}
	c.scrubEpic(e1)
	e2, err := s.CreateEpic(store.NewEpic{ComponentID: ep3.ID, Title: "epic in ep3", Owner: "u1"})
	if err != nil {
		c.fail(err)
	}
	c.scrubEpic(e2)

	// g1: a goal in ep1 with goal_component rows (-> c1, c2).
	g1, _, err := s.CreateGoal(store.NewGoal{
		ComponentID: ep1.ID, EpicID: e1.ID, Title: "goal in ep1",
		Type: "feature", SourceKind: "manual", By: actorUser,
		Components: []store.GoalComponent{{ComponentID: c1.ID, Rationale: "owns c1", Basis: []string{"b1"}},
			{ComponentID: c2.ID, Rationale: "also c2", Basis: []string{"b2"}}},
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubGoal(g1)
	// g2: a goal in c5's root.
	g2, _, err := s.CreateGoal(store.NewGoal{
		ComponentID: c5.ID, Title: "goal in c5",
		Type: "bug", SourceKind: "manual", By: actorUser,
		Components: []store.GoalComponent{{ComponentID: c5.ID, Rationale: "owns c5", Basis: []string{"b1"}}},
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubGoal(g2)
	// g3: a goal in c6's root.
	g3, _, err := s.CreateGoal(store.NewGoal{
		ComponentID: c6.ID, Title: "goal in c6",
		Type: "chore", SourceKind: "event", By: actorUser,
		Components: []store.GoalComponent{{ComponentID: c6.ID, Rationale: "owns c6", Basis: []string{"b1"}}},
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubGoal(g3)
	// g4: a goal filed into c7's root, straight into review.
	g4, _, err := s.CreateGoal(store.NewGoal{
		ComponentID: c7.ID, Title: "goal keeps c7",
		Type: "refactor", SourceKind: "manual", By: actorUser, Submit: true,
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubGoal(g4)

	// ---- automation events carrying a root id -------------------------
	mt, _, err := s.CreateManualTask(store.NewManualTask{
		ComponentID: c1.ID, Title: "manual task in c1", Type: "feature",
		By: actorUser, RequireApproval: true,
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubTask(mt)
	// approving g4 emits goal.approved with its root id.
	if _, _, err := s.DecideGoal(g4.ID, g4.Version, "approved", actorUser, "ok"); err != nil {
		c.fail(err)
	}

	// ---- loop task with a component id ----------------------------------
	if _, err := s.CreateLoop(store.NewLoop{Slug: "loop-l", Root: c1.Root, ComponentRoot: c1.Root}); err != nil {
		c.fail(err)
	}
	lt, err := s.CreateTask(store.Scope{LoopSlug: "loop-l", ComponentRoot: c1.Root}, store.NewTask{
		TaskID: "1", Ord: 1, Title: "loop task in c1", Tech: "x", Engineer: "e1",
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubTask(lt)

	// ---- event triggers matching on root and component keys ----------
	trg1, err := s.CreateEventTrigger(actorUser, store.NewEventTrigger{
		Name: "trg-on-approve", RootID: ep1.ID, ComponentID: c1.ID,
		Blueprint: "bp-approve", Kickoff: "kick", MaxRunsPerHour: 10,
		Match: store.Match{Event: "goal.approved", Root: []string{ep1.ID}, Component: []string{c1.ID}},
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubTrigger(trg1)
	trg2, err := s.CreateEventTrigger(actorUser, store.NewEventTrigger{
		Name: "trg-on-create", RootID: ep3.ID, ComponentID: c2.ID,
		Blueprint: "bp-create", Kickoff: "kick", MaxRunsPerHour: 10,
		Match: store.Match{Event: "task.created", Root: []string{ep3.ID}, Component: []string{c2.ID}},
	})
	if err != nil {
		c.fail(err)
	}
	c.scrubTrigger(trg2)

	// ---- telemetry on every tier --------------------------------------
	if _, err := s.CreateLoop(store.NewLoop{Slug: "loop-tel", Root: c2.Root, ComponentRoot: c2.Root}); err != nil {
		c.fail(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: "loop-tel", ComponentRoot: c2.Root}, store.NewStep{Agent: "tel"})
	if err != nil {
		c.fail(err)
	}
	usage := model.Usage{InputTokens: ptr(100), OutputTokens: ptr(40), CacheReadTokens: ptr(7),
		CacheWriteTokens: ptr(3), ReasoningTokens: ptr(5)}
	if err := s.RecordStepTurn(step.ID, 1, usage, model.Cost{State: model.CostKnown, USD: 0.002}, 1200,
		"route-a", store.TelemetryCategoryAssistantOutput); err != nil {
		c.fail(err)
	}
	if err := s.RecordStepCompaction(step.ID, 0, 5, 2000, 1200, 90, usage, model.Cost{State: model.CostKnown, USD: 0.001}); err != nil {
		c.fail(err)
	}
	if err := s.RecordFileActivity(step.ID, "/repo/c2/main.go", "write", ptr(10), ptr(2)); err != nil {
		c.fail(err)
	}
	if err := s.RecordCommandActivity(step.ID, "go test ./...", ptr(0), 250); err != nil {
		c.fail(err)
	}

	// The CT5 mapping is written by hand (below), from the shapes the store
	// now holds: read each root's child count and references, then apply the
	// rule. The rule is the hand-written answer; it never calls the T2/T3
	// migration (a golden that reuses its own implementation can never fail it).
	c.recordExpected()
}

// ---- the CT5 mapping, written by hand from the shapes the store holds ----
//
// recordExpected fills c.Expected — one row per root — with the CT5
// destination the T2/T3 migration will produce. It reads each root's child
// count and its references FROM the store (so a fixture change re-derives the
// shapes and the mapping self-corrects) and then applies the hand-written CT5
// rule. The rule itself is the golden, never the migration: a golden that
// reused the migration code could never fail it.

// recordExpected builds the hand-written CT5 mapping for every root.
func (c *Census) recordExpected() {
	roots, err := c.S.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil {
		c.fail(err)
	}
	// order by the scrub label so the rows are stable run to run (the store's
	// own list is by name, which is fine, but the label is the golden's key).
	sort.Slice(roots, func(i, j int) bool { return c.Scrub[roots[i].ID] < c.Scrub[roots[j].ID] })
	for _, root := range roots {
		components := root.ChildCount // the store's non-archived child count (CT1).
		// A collapse resolves to its one member; read that id from the store so
		// the target is never a guessed label.
		var collapseTarget string
		if components == 1 {
			children, err := c.S.ListChildren(root.ID)
			if err != nil {
				c.fail(err)
			}
			if len(children) == 1 {
				collapseTarget = children[0].ID
			}
		}
		referenced := c.referencedBy(root.ID)
		// "implicit" is the one field the post-tree store cannot read: the
		// project tier (and its implicit flag) is gone (CT1, the 0035 backfill
		// made one implicit project per component). Every root here is created
		// as a plain component — the shape the old tier would have wrapped in an
		// implicit project — so every root is implicit. The build records it.
		implicit := true
		disp, target := ct5Disposition(implicit, components, referenced, root.ID, collapseTarget)
		c.Expected = append(c.Expected, ExpectedRow{
			ProjectID:   root.ID,
			Implicit:    implicit,
			Components:  components,
			Referenced:  referenced,
			Disposition: disp,
			TargetID:    target,
		})
	}
}

// referencedBy answers CT5's "refd" for a root: does any epic, goal,
// automation event, event trigger, or non-zero telemetry name it? It mirrors
// the T2 map's ref query, read through the store's API — the project_id the
// migration read is the root_id the tree store keeps.
func (c *Census) referencedBy(rootID string) bool {
	if eps, err := c.S.ListEpics(rootID); err == nil && len(eps) > 0 {
		return true
	}
	if gs, err := c.S.ListGoals(store.GoalFilter{ComponentID: rootID}); err == nil && len(gs) > 0 {
		return true
	}
	if autos, err := c.S.ReadAutomationEvents(0, 100000); err == nil {
		for _, a := range autos {
			if a.RootID == rootID {
				return true
			}
		}
	}
	if trgs, err := c.S.ListEventTriggers(store.EventTriggerFilter{RootID: rootID}); err == nil && len(trgs) > 0 {
		return true
	}
	if tel, err := c.S.ComponentTelemetry(rootID); err == nil && tel.Exists && telemetryNonZero(tel.TelemetrySums) {
		return true
	}
	return false
}

// telemetryNonZero is the ref query's "non-zero telemetry" gate: a rollup with
// no loops, cost, commands or files touched names nothing.
func telemetryNonZero(s store.TelemetrySums) bool {
	return s.LoopsRun != 0 || s.CostUSDTotal != 0 || s.CommandsRunTotal != 0 || s.FilesTouchedTotal != 0
}

// ct5Disposition is the hand-written CT5 answer (the table the T2/T3 migration
// will check) applied to a root's shapes:
//
//	explicit           -> keep (the project's own id becomes a component).
//	implicit + 1       -> collapse to its one member.
//	implicit + >= 2    -> keep.
//	implicit + 0 + ref -> keep.
//	implicit + 0 + ~ref -> drop (the row is gone; target is empty).
func ct5Disposition(implicit bool, components int, referenced bool, rootID, collapseTarget string) (string, string) {
	if !implicit {
		return "keep", rootID
	}
	switch {
	case components == 1:
		return "collapse", collapseTarget
	case components >= 2:
		return "keep", rootID
	case referenced:
		return "keep", rootID
	default:
		return "drop", ""
	}
}

// createRoot creates a parentless component (a root) and scrubs it.
func (c *Census) createRoot(name, slug, root string) (store.Component, error) {
	w, err := c.S.CreateComponent(store.NewComponent{Name: name, Slug: slug, Root: root})
	if err != nil {
		return store.Component{}, err
	}
	if !w.OK() {
		return store.Component{}, fmt.Errorf("create root %s refused: %s", name, w.Reason)
	}
	c.scrubComponent(w.Component)
	return w.Component, nil
}

// createChild creates a component under a parent (a child) and scrubs it.
func (c *Census) createChild(name, slug, root string, parent store.Component) (store.Component, error) {
	w, err := c.S.CreateComponent(store.NewComponent{Name: name, Slug: slug, Root: root, ParentID: parent.ID})
	if err != nil {
		return store.Component{}, err
	}
	if !w.OK() {
		return store.Component{}, fmt.Errorf("create child %s refused: %s", name, w.Reason)
	}
	c.scrubComponent(w.Component)
	return w.Component, nil
}

// ---- label assignment -----------------------------------------------------

func (c *Census) scrubComponent(comp store.Component) {
	c.compSeen++
	c.Scrub[comp.ID] = "c" + itoa(c.compSeen)
}

func (c *Census) scrubGoal(g store.Goal) {
	c.Scrub[g.ID] = "g" + itoa(c.goalSeen)
	c.goalSeen++
}

func (c *Census) scrubEpic(e store.Epic) {
	c.Scrub[e.ID] = "e" + itoa(c.epicSeen)
	c.epicSeen++
}

func (c *Census) scrubTrigger(t store.Trigger) {
	c.Scrub[t.ID] = "trg" + itoa(c.trigSeen)
	c.trigSeen++
}

func (c *Census) scrubTask(t store.Task) {
	c.Scrub[t.ID] = "tsk" + itoa(c.taskSeen)
	c.taskSeen++
}

func (c *Census) fail(err error) {
	c.t.Fatalf("ctcensus: %v", err)
}

func ptr[T any](v T) *T { return &v }

// ---- scrub: the one thing that makes the goldens reproducible -------------
//
// The goldens serialise the rows, scrub every raw id to its label (Scrub),
// sort by a stable key, and compare. A golden that kept raw ids would differ
// run to run and fail on its own untouched code; scrubbing is what keeps it
// byte-stable so it changes ONLY when the code changes.

// ScrubJSON replaces every raw id in a JSON document with its label and
// re-marshals. It is the door the JSON goldens (store, daemon, records,
// telemetry) use; the CLI golden, which reads text, uses ScrubText.
func ScrubJSON(b []byte, scrub map[string]string) []byte {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return b
	}
	scrubValue(v, scrub)
	out, err := json.Marshal(v)
	if err != nil {
		return b
	}
	return out
}

// ScrubValue walks a decoded JSON value and returns it with every string that
// is a raw id in the scrub map replaced by its label. It returns the (possibly
// new) value so a container can reassign it; a bare string in a map or slice is
// replaced where it sits, not in place.
func scrubValue(v any, scrub map[string]string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = scrubValue(val, scrub)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = scrubValue(val, scrub)
		}
		return t
	case string:
		// A string may be a raw id all by itself OR carry raw ids inside it —
		// a trigger's match_json is a JSON string holding project/component ids.
		// ScrubText replaces every embedded id, longest first, so an implicit
		// project id ("prj-"+<component hex>) is not half-rewritten by its
		// component's hex first.
		return scrubText(t, scrub)
	default:
		return v
	}
}

// scrubText replaces every raw id in a text string with its label. Longer ids
// go first so an implicit project id ("prj-"+<component hex>) is not
// half-rewritten by its component's hex first.
func scrubText(s string, scrub map[string]string) string {
	ids := make([]string, 0, len(scrub))
	for id := range scrub {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return len(ids[i]) > len(ids[j]) })
	for _, id := range ids {
		s = strings.ReplaceAll(s, id, scrub[id])
	}
	return s
}

// itoa is a tiny int-to-string so the file needs no fmt for the labels. The
// loop fills the buffer from the end; after it, i points just before the first
// digit, so the digits are buf[i+1:] — returning buf[i:] would prepend an
// uninitialised null byte and corrupt every label.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf) - 1
	for n > 0 {
		buf[i] = byte('0' + n%10)
		i--
		n /= 10
	}
	return string(buf[i+1:])
}
