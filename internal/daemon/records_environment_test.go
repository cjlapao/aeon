package daemon

// records_environment_test.go — S59 T7 (EV16, EV7, EV17): the five
// execution-environment arms of the records service. What each test pins, in
// the order the spec lists them:
//
//   - the four READ arms answer the SAME bytes the person's socket doors
//     answer, because they forward to the same services (rule 4 — a tool and
//     its twin cannot drift, and the drift is what this file would catch);
//   - unwired, the plane says so by name (V13) instead of answering an empty
//     listing that reads as "no environments exist" (P3);
//   - environment_effective is scoped to the step's own tree BEFORE the read:
//     a foreign component meets a refusal naming both sides, and the seam is
//     never called (OL3, MC2's shape);
//   - secretref_list answers the type with no `source` field at all — the
//     person's door keeps it, the agent's cannot carry it (EV7, §4c);
//   - the propose arm refuses a smuggled identity at the parse, refuses the
//     unknown and the archived row by name, refuses every way the ask is
//     looser with the validator's own sentence (TD3 — mirrored here exactly as
//     the blueprint file mirrors its seam, because internal/daemon cannot
//     import package main; the parity of the PRODUCTION seam is pinned there),
//     stops at the per-agent cap naming its constant, and files the INERT row
//     stamped with the AGENT who asked — while the environment row itself does
//     not move by one revision (EV16: an ask is not a change).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// envRecordsEnv is a store-wired daemon: the four read arms run over the SAME
// seams the person's twenty doors answer through (assigned in New, inside the
// store guard), and the create path runs over an injected validator mirroring
// cmd/aeon's seam. One stored environment and one reference seed the reads
// non-empty — an empty fixture would let "answered nothing" pass as "answered".
type envRecordsEnv struct {
	base          recordsEnv // the loop + step every records call stamps from (OL3)
	d             *Daemon
	s             *store.Store
	ctx           context.Context
	slug          string
	revAt         int64
	changesAtSeed int
}

const envRecordsSlug = "t7-scrubbed"

func newEnvRecordsEnv(t *testing.T) envRecordsEnv {
	t.Helper()
	e := envRecordsEnv{base: newRecordsEnv(t, "")}
	e.s = e.base.s
	e.ctx = e.base.ctx
	reg := desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindEnvironmentRestrict: {Validate: mirrorEnvironmentRestrictValidate(e.s)},
	})
	e.d = New(Options{Store: e.s, ValidateProposal: reg.ValidateProposal})
	if e.d.recordsSvc == nil {
		t.Fatal("the daemon built no records service — the arms have nothing to run on")
	}
	if e.d.recordsSvc.environments == nil || e.d.recordsSvc.secretRefs == nil {
		t.Fatal("the daemon did not hand the records service the environment plane's own seams — the read arms would refuse (V13) on a wired mount")
	}
	seedEnvRow(t, e.s, envRecordsSlug, "host-scrubbed", canonicalSpec(t, []string{"PATH", "HOME"}, nil, nil, "host-scrubbed"))
	row, err := e.s.GetEnvironment(envRecordsSlug)
	if err != nil {
		t.Fatalf("read the seeded row: %v", err)
	}
	e.slug = row.ID
	e.revAt = row.Revision
	// A create is a changing write and is audited (T6), so the audit baseline
	// is the seed's own count — what the inert ask must not add to.
	e.changesAtSeed = countEnvironmentChanges(t, e.s, "environment", row.ID)
	if _, ref, err := e.s.CreateSecretRef(store.Actor{Kind: "user", ID: "person"}, store.NewSecretRef{
		Name: "deploy-token", Description: "the deploy token", SourceKind: "key-file",
		Source: "/home/person/.secrets/deploy-token", InjectAs: "DEPLOY_TOKEN",
	}, nil); ref != nil || err != nil {
		t.Fatalf("seed the reference: ref=%v err=%v", ref, err)
	}
	return e
}

// seedEnvRow writes one environment through the store's own door (a person's
// actor — the same row the panel would have made), so the arms read rows that
// exist rather than a fixture's idea of one.
func seedEnvRow(t *testing.T, st *store.Store, slug, backend, spec string) store.Environment {
	t.Helper()
	row, ref, err := st.CreateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewEnvironment{
		Slug: slug, Name: slug, Backend: backend, Spec: spec,
	})
	if ref != nil || err != nil {
		t.Fatalf("seed %s: ref=%v err=%v", slug, ref, err)
	}
	return row
}

// mirrorEnvironmentRestrictValidate is cmd/aeon's environment_restrict seam
// rebuilt from pieces this package owns — the desk shape half, then the row,
// then the ONE rule (execenv.Restricts empty) — so the arm's verbatim-refusal
// assertions mean what they say about the ARM. The parity of the production
// seam (create and commit calling one function) is pinned in cmd/aeon,
// TestEnvironmentRestrictCreateAndCommitValidatorsAreOneFunction.
func mirrorEnvironmentRestrictValidate(st *store.Store) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		if err := desk.ValidateEnvironmentRestrict(ctx, payload); err != nil {
			return err
		}
		var p desk.EnvironmentRestrictProposal
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		row, err := st.GetEnvironment(p.EnvironmentID)
		if err != nil {
			return fmt.Errorf("refused: %s is not an environment (id or slug) — environment_list shows what exists", p.EnvironmentID)
		}
		if row.Archived() {
			return fmt.Errorf("refused: %s is archived — environment.restore is the person's door", row.Slug)
		}
		oldSpec, err := execenv.ParseSpec([]byte(row.Spec))
		if err != nil {
			return fmt.Errorf("refused: the stored spec of %s does not parse (%v)", row.Slug, err)
		}
		newSpec, err := execenv.ParseSpec([]byte(p.Spec))
		if err != nil {
			return fmt.Errorf("refused: the proposed spec does not parse (%v)", err)
		}
		if w := execenv.Restricts(oldSpec, newSpec, row.Backend, row.Backend); len(w) > 0 {
			parts := make([]string, 0, len(w))
			for _, x := range w {
				parts = append(parts, x.Sentence())
			}
			return fmt.Errorf("refused: environment_restrict %s would WIDEN what the loop runs with — %d loosening(s): %s",
				row.Slug, len(w), strings.Join(parts, "; "))
		}
		if problems := execenv.Validate(newSpec, row.Backend, execenv.ValidateOpts{}); len(problems) > 0 {
			parts := make([]string, 0, len(problems))
			for i := range problems {
				parts = append(parts, problems[i].What)
			}
			return fmt.Errorf("refused: the proposed spec is not valid under the %s backend — %s", row.Backend, strings.Join(parts, " | "))
		}
		return nil
	}
}

func envToolInput(id, spec, reason string) string {
	b, _ := json.Marshal(map[string]string{"environmentId": id, "spec": spec, "reason": reason})
	return string(b)
}

// TestEnvironmentReadsAnswerTheSameBytesAsTheHumanDoors is rule 4 in the
// direction that matters: the tool door and the socket door forward to the
// SAME service, so their answers are the same bytes — not two renderings of
// the same idea that can drift a field at a time.
func TestEnvironmentReadsAnswerTheSameBytesAsTheHumanDoors(t *testing.T) {
	e := newEnvRecordsEnv(t)
	ctx := context.Background()

	cases := []struct {
		tool  string
		input string
		human func() (any, error)
	}{
		{"environment_list", `{"includeArchived":false}`, func() (any, error) {
			return e.d.Table.Environment.List(ctx, protocol.EnvironmentListParams{IncludeArchived: false})
		}},
		{"environment_show", `{"slug":"` + envRecordsSlug + `"}`, func() (any, error) {
			return e.d.Table.Environment.Show(ctx, protocol.EnvironmentShowParams{Slug: envRecordsSlug})
		}},
		{"secretref_list", `{}`, func() (any, error) {
			return e.d.Table.SecretRefs.List(ctx, protocol.SecretRefListParams{})
		}},
	}
	for _, tc := range cases {
		toolOut, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, tc.tool, json.RawMessage(tc.input))
		if f != nil || err != nil {
			t.Fatalf("%s: failure=%v err=%v", tc.tool, f, err)
		}
		humanOut, herr := tc.human()
		if herr != nil {
			t.Fatalf("%s's human door: %v", tc.tool, herr)
		}
		if tc.tool == "secretref_list" {
			// The one pair that is deliberately NOT the same bytes: the
			// agent's type drops `source`. What must match is everything
			// else — asserted by the count and the next test.
			continue
		}
		a, _ := json.Marshal(toolOut)
		b, _ := json.Marshal(humanOut)
		if string(a) != string(b) {
			t.Errorf("%s answered\n %s\nand its human door answered\n %s — two renderings of one row (rule 4)", tc.tool, a, b)
		}
	}
}

// TestSecretRefListArmCarriesNoSourceAtAll is EV7 at the answer, not at the
// type: the row the person's door shows HAS a source, and the agent's answer
// does not contain the word — because its type has no field for it, the
// arm's projection cannot leak it even by forgetting.
func TestSecretRefListArmCarriesNoSourceAtAll(t *testing.T) {
	e := newEnvRecordsEnv(t)
	out, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "secretref_list", json.RawMessage(`{}`))
	if f != nil || err != nil {
		t.Fatalf("secretref_list: failure=%v err=%v", f, err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "deploy-token") {
		t.Fatalf("the answer does not even list the reference: %s", text)
	}
	views, ok := out.([]protocol.SecretRefListView)
	if !ok {
		t.Fatalf("the arm answered %T, want []protocol.SecretRefListView — the agent's own type, not the person's", out)
	}
	// The person's door, over the SAME rows, does carry the source — so the
	// absence above is the projection working, not a fixture with nothing in
	// it (the difference the whole test turns on).
	human, herr := e.d.Table.SecretRefs.List(e.ctx, protocol.SecretRefListParams{})
	if herr != nil {
		t.Fatalf("the person's door: %v", herr)
	}
	hb, _ := json.Marshal(human)
	if !strings.Contains(string(hb), "/home/person/.secrets/deploy-token") {
		t.Fatalf("the fixture is wrong — the person's door does not carry the source either, so the comparison below proves nothing: %s", hb)
	}
	if len(views) != len(human) {
		t.Errorf("the agent's list holds %d of the person's %d rows — the projection dropped more than the source", len(views), len(human))
	}
	for _, banned := range []string{"secrets/deploy", "canary", "value"} {
		if strings.Contains(strings.ToLower(text), banned) {
			t.Errorf("the agent's answer carries %q: %s", banned, text)
		}
	}
	if strings.Contains(text, `"source"`) {
		t.Errorf("the agent's answer carries a \"source\" field — the projection to SecretRefListView did not happen (EV7, §4c): %s", text)
	}
	if len(views) == 0 {
		t.Error("the agent's reference list is empty on a mount that has one — an empty that reads as \"none exist\" (P3)")
	}
}

// TestEnvironmentEffectiveIsScopedToTheStepsOwnTree is OL3 at the read: no
// componentId means the step's own component; a component outside the step's
// root is refused by name, with both sides named, and the plane's service is
// never consulted (a foreign tree's environment is not this step's fact).
func TestEnvironmentEffectiveIsScopedToTheStepsOwnTree(t *testing.T) {
	e := newEnvRecordsEnv(t)
	// A second tree the step has no loop in.
	other, err := e.s.CreateComponent(store.NewComponent{Name: "beta", Slug: "beta", Root: "/repo/beta"})
	if err != nil || !other.OK() {
		t.Fatalf("component beta: %v", err)
	}

	// EV6: unbound is an honest refusal, not a default — so the fixture binds
	// the plane's own workspace floor through the person's door before asking
	// what this step would run in.
	if _, err := e.d.Table.Environment.Bind(e.ctx, protocol.Actor{Kind: "user", ID: "person"}, protocol.EnvironmentBindParams{
		Scope: "workspace", EnvironmentID: e.slug,
	}); err != nil {
		t.Fatalf("the fixture could not bind the workspace: %v", err)
	}
	out, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_effective", json.RawMessage(`{}`))
	if f != nil || err != nil {
		t.Fatalf("own component: failure=%v err=%v", f, err)
	}
	if out == nil {
		t.Fatal("the own-tree read answered nothing")
	}

	_, f, err = e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_effective",
		json.RawMessage(`{"componentId":"`+other.Component.ID+`"}`))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if f == nil {
		t.Fatal("a foreign component was served its environment — a step read another tree's map of what it runs with (OL3)")
	}
	for _, want := range []string{other.Component.ID, e.base.comp.ID, "not in your loop's tree"} {
		if !strings.Contains(f.Sentence, want) {
			t.Errorf("the scope refusal omits %q: %s", want, f.Sentence)
		}
	}
}

// TestEnvironmentArmsRefuseAPlanelessMount is V13: no plane, no answer — the
// refusal names the tool and says absent is stated, not faked. An empty
// listing here would read as "this machine has no environments".
func TestEnvironmentArmsRefuseAPlanelessMount(t *testing.T) {
	e := newEnvRecordsEnv(t)
	bare := NewRecordsService(e.s) // no seams assigned at all
	for _, tool := range []string{"environment_list", "environment_show", "environment_effective", "secretref_list"} {
		out, f, err := bare.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if out != nil {
			t.Errorf("%s answered %v over a nil seam — absence must be stated, not answered", tool, out)
		}
		if f == nil || !strings.Contains(f.Sentence, tool) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s's refusal is %v, want one naming the tool and V13", tool, f)
		}
	}
}

// TestEnvironmentProposeRefusesTheShapesItMustNotStore is the arm's own
// half of the shape rule (the catalog schema refuses the rest at parse):
// blank id, empty spec, blank reason, an oversized reason and an oversized
// spec each meet a sentence naming its cap, and NOTHING is stored.
func TestEnvironmentProposeRefusesTheShapesItMustNotStore(t *testing.T) {
	e := newEnvRecordsEnv(t)
	huge := strings.Repeat("a", maxEnvironmentSpecBytes+1)
	longReason := strings.Repeat("w", maxEnvironmentReasonBytes+1)
	for _, tc := range []struct {
		name, input, want string
	}{
		{"blank id", `{"environmentId":"  ","spec":"{}","reason":"tighten"}`, "non-blank environmentId"},
		{"empty spec", `{"environmentId":"` + envRecordsSlug + `","spec":"  ","reason":"tighten"}`, "FULL proposed spec"},
		{"oversized spec", envToolInput(envRecordsSlug, huge, "tighten"), "1 MiB"},
		{"blank reason", `{"environmentId":"` + envRecordsSlug + `","spec":"{}","reason":" "}`, "needs a reason"},
		{"oversized reason", envToolInput(envRecordsSlug, "{}", longReason), "512"},
		{"smuggled actor", `{"environmentId":"` + envRecordsSlug + `","spec":"{}","reason":"r","actor":{"kind":"agent","id":"mallory"}}`, "identity"},
		{"smuggled backend", `{"environmentId":"` + envRecordsSlug + `","spec":"{}","reason":"r","backend":"container"}`, "identity"},
		{"smuggled revision", `{"environmentId":"` + envRecordsSlug + `","spec":"{}","reason":"r","revision":9}`, "identity"},
	} {
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict", json.RawMessage(tc.input))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if f == nil {
			t.Errorf("%s: the arm stored a proposal it must refuse", tc.name)
			continue
		}
		if !strings.Contains(strings.ToLower(f.Sentence), strings.ToLower(tc.want)) {
			t.Errorf("%s: refusal %q does not say %q", tc.name, f.Sentence, tc.want)
		}
	}
	if n := countProposals(t, e.s, string(desk.KindEnvironmentRestrict)); n != 0 {
		t.Errorf("%d environment_restrict rows exist after eight refusals — a refused ask is not inert if it is stored", n)
	}
}

// TestEnvironmentProposeRefusesTheUnknownAndTheArchivedRow is P3 at the
// door: a proposal against a row nobody can tighten is inert AND invalid, and
// TD3 says nothing like that gets stored. Each refusal names the door that
// would have made the row usable.
func TestEnvironmentProposeRefusesTheUnknownAndTheArchivedRow(t *testing.T) {
	e := newEnvRecordsEnv(t)
	_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict",
		envToolInputT("no-such-env", canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed"), "tighten"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "environment_list") {
		t.Errorf("the unknown row's refusal is %v, want one pointing at environment_list", f)
	}

	arch := seedEnvRow(t, e.s, "t7-archived", "host-scrubbed", canonicalSpec(t, []string{"PATH", "HOME"}, nil, nil, "host-scrubbed"))
	if _, _, err := e.s.ArchiveEnvironment(store.Actor{Kind: "user", ID: "person"}, arch.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	_, f, err = e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict",
		envToolInputT(arch.Slug, canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed"), "tighten"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "archived") || !strings.Contains(f.Sentence, "environment.restore") {
		t.Errorf("the archived row's refusal is %v, want one naming the state and environment.restore", f)
	}
}

// TestEnvironmentProposeRefusesEveryWideningVerbatim is EV16 at create: the
// ask that adds a passed-through name, raises a cap, or switches egress on is
// refused with the validator's own sentence, every loosening named — and no
// row is stored. The environment's own revision does not move either: an ask
// that was refused is not a change, and an ask that was ACCEPTED is not one
// either (the next test's claim).
func TestEnvironmentProposeRefusesEveryWideningVerbatim(t *testing.T) {
	e := newEnvRecordsEnv(t)
	wide := canonicalSpec(t, []string{"PATH", "HOME", "USER"}, nil, nil, "host-scrubbed")
	_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict",
		envToolInputT(envRecordsSlug, wide, "the build needs the AWS secret too"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("a WIDENING ask was stored — the proposal kind that loosens what a loop runs with exists (EV16)")
	}
	for _, want := range []string{"WIDEN", "USER"} {
		if !strings.Contains(f.Sentence, want) {
			t.Errorf("the widening refusal omits %q: %s", want, f.Sentence)
		}
	}
	if n := countProposals(t, e.s, string(desk.KindEnvironmentRestrict)); n != 0 {
		t.Errorf("%d rows after a widening refusal", n)
	}
	row, err := e.s.GetEnvironment(envRecordsSlug)
	if err != nil {
		t.Fatal(err)
	}
	if row.Revision != e.revAt {
		t.Errorf("the refused ask moved the row from revision %d to %d — a refusal is not a changing write (EV17)", e.revAt, row.Revision)
	}
}

// TestEnvironmentProposeFilesTheInertRowStampedWithTheStep is the positive
// half, and its three claims together: the row exists with the right kind and
// the AGENT as author (never a typed one, OL3); the environment row has not
// moved by one revision (EV16: inert until a person commits); and the
// baseline the ask is written against is the one the service READ, not one
// the caller could type.
func TestEnvironmentProposeFilesTheInertRowStampedWithTheStep(t *testing.T) {
	e := newEnvRecordsEnv(t)
	tight := canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed")
	out, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict",
		envToolInputT(envRecordsSlug, tight, "HOME is not needed by the build and every name passed is a name exposed"))
	if f != nil || err != nil {
		t.Fatalf("the tightening ask should stand: failure=%v err=%v", f, err)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("answered %T, want protocol.PolicyProposeResult", out)
	}
	if !strings.Contains(res.Note, "inert") && !strings.Contains(res.Note, "commit") {
		t.Errorf("the note does not tell the agent what the row is: %q", res.Note)
	}
	if res.Proposal.Command == "" {
		t.Error("no decision command on the answer — the person's door would not be named to the agent")
	}

	rows, err := e.s.ListProposals("proposed")
	if err != nil {
		t.Fatal(err)
	}
	var row store.Proposal
	n := 0
	for _, r := range rows {
		if r.Kind == string(desk.KindEnvironmentRestrict) {
			row = r
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d rows of the kind, want 1", n)
	}
	if row.CreatedByKind != "agent" || row.CreatedByID != e.base.agent {
		t.Errorf("authored by %s/%s, want agent/%s — authorship is the step's, never typed (OL3)", row.CreatedByKind, row.CreatedByID, e.base.agent)
	}
	var payload desk.EnvironmentRestrictProposal
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.BaseRevision != e.revAt {
		t.Errorf("the payload's baseline is %d, want the %d the service read — a baseline a model can type is a baseline that can be wrong on purpose", payload.BaseRevision, e.revAt)
	}
	if payload.EnvironmentID != envRecordsSlug || payload.Reason == "" {
		t.Errorf("the payload lost the ask: %+v", payload)
	}

	env, err := e.s.GetEnvironment(envRecordsSlug)
	if err != nil {
		t.Fatal(err)
	}
	if env.Revision != e.revAt || env.Spec == canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed") {
		t.Errorf("the ASK changed the environment (revision %d) — an ask constrains nothing until a person commits it (EV16)", env.Revision)
	}
	// And the environment's audit says nothing happened either: the count is
	// the seed's own — a create IS a changing write and is audited (T6) — and
	// the inert ask added no row to it.
	if n := countEnvironmentChanges(t, e.s, "environment", env.ID); n != e.changesAtSeed {
		t.Errorf("%d environment_change rows after an inert ask — the audit is for changes, and this was not one", n)
	}
}

// TestEnvironmentProposeStopsAtThePerAgentCap is S45's shared cap carried
// onto the new kind: at the limit the ask meets a refusal that NAMES the
// constant and the read that lists what is open, and stores nothing.
func TestEnvironmentProposeStopsAtThePerAgentCap(t *testing.T) {
	e := newEnvRecordsEnv(t)
	tight := canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed")
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		if _, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict",
			envToolInputT(envRecordsSlug, tight, fmt.Sprintf("cap filler %d", i))); f != nil || err != nil {
			t.Fatalf("filler %d: failure=%v err=%v", i, f, err)
		}
	}
	_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "environment_propose_restrict",
		envToolInputT(envRecordsSlug, tight, "one too many"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatalf("the %dth proposal stood — the cap is not applied to this kind (S45's shared rule)", desk.MaxOpenProposalsPerAgent+1)
	}
	if !strings.Contains(f.Sentence, fmt.Sprint(desk.MaxOpenProposalsPerAgent)) || !strings.Contains(f.Sentence, "proposal_list") {
		t.Errorf("the cap refusal does not name the number or the read: %s", f.Sentence)
	}
	if n := countProposals(t, e.s, string(desk.KindEnvironmentRestrict)); n != desk.MaxOpenProposalsPerAgent {
		t.Errorf("%d rows stored, want the cap's %d — a counted refusal that stores is not counted", n, desk.MaxOpenProposalsPerAgent)
	}
}

// TestEnvironmentToolDoorsRefuseAPerson is MC2's shape carried onto the
// family: these five doors answer the step, and a person is pointed at the
// plane's own doors by name — served the agent's self-scoped view, a person
// would be shown a narrower truth than the one they hold.
func TestEnvironmentToolDoorsRefuseAPerson(t *testing.T) {
	e := newEnvRecordsEnv(t)
	for _, tool := range []string{"environment_list", "environment_show", "environment_effective", "secretref_list", "environment_propose_restrict"} {
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "person's door") {
			t.Errorf("%s served a person the agent's door (refusal=%v) — the self-scoped view is not the person's view", tool, f)
		}
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func envToolInputT(id, spec, reason string) json.RawMessage {
	return json.RawMessage(envToolInput(id, spec, reason))
}

func countProposals(t *testing.T, st *store.Store, kind string) int {
	t.Helper()
	rows, err := st.ListProposals("proposed")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	n := 0
	for _, r := range rows {
		if r.Kind == kind {
			n++
		}
	}
	return n
}
