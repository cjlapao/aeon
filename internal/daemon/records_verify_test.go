package daemon

// records_verify_test.go — S57 T9 (VG19): the nine gate catalog arms,
// walked at the door an agent actually stands at. The behaviour rows the
// task names, each pinned here:
//
//   - FORCED IDENTITY: the proposal row is stamped from the step's agent;
//     a params-named author is a parse refusal (the schema has no field
//     for one), and a person on these names is turned back by name before
//     the switch (the socket already speaks the plane for a person);
//   - INERT: the six read surfaces plus the gate tables are byte-identical
//     across a propose; the only new row is the inert proposal with the
//     fixed note and the copyable commit command;
//   - RESTRICTIVE-ONLY AT THE SERVICE (the second defence): a loosening
//     merge rule is refused by the shared validator with each loosened
//     field named, even on a direct service call that bypasses the schema
//     (the schema admits only the tightening direction's values);
//   - EVIDENCE-BOUND dod_mark: a met names a passing run of the caller's
//     own loop on the CURRENT head — a missing, failing, foreign-loop or
//     stale-head run is refused naming what is missing; unmet and
//     unverifiable are free, and a non-met carrying a run id is refused;
//   - SCOPED: a foreign loop or component in params is refused, naming
//     the caller's own; the scope is the stamp, never a param (a smuggled
//     scope field is a parse refusal);
//   - and UNWIRED is STATED, not faked (V13): a mount without a gate
//     seam names the absence rather than answering zero.
//
// The seams follow the autonomy family's idiom (records_autonomy_test.go):
// a store over a throwaway directory, the three REAL gate services over it
// (one implementation, two doors — the tool door and the human door), the
// git root a real repository with one commit (the arm's head-currency
// check reads rev-parse HEAD), and the shared validator as the TEST
// mirror of cmd/aeon's production seams (the daemon package may not import
// main); the mirror runs the desk's own half — THE function the create
// door runs (TD3) — then the store half over the same real store, so a
// loosening is refused with the store's words.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// ── the harness ──────────────────────────────────────────────────────────────

// gateProposeEnv is the T9 arm's world: a component on a real git root, a
// loop running in it, a step standing on the loop, and the records service
// wired with the three real gate services plus the proposal doors the two
// proposes end in (S45's create path, the shared validator).
type gateProposeEnv struct {
	s     *store.Store
	svc   *RecordsService
	ctx   context.Context
	comp  store.Component
	root  string
	agent string
	v     *VerifyService    // the SAME instance the service reads through
	m     *MergeRuleService // — plus the write half for seeding (person door)
}

func newGateProposeEnv(t *testing.T) gateProposeEnv {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := gateProposeEnv{s: st, agent: "implementer"}
	e.root = gateGitRoot(t)
	p, err := st.CreateComponent(store.NewComponent{Name: "org group", Slug: "org-group", Root: "/repo/org-group"})
	if err != nil || !p.OK() {
		t.Fatalf("group: %v", err)
	}
	c, err := st.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: e.root, ParentID: p.Component.ID})
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	e.comp = c.Component
	if _, err := st.CreateLoop(store.NewLoop{Slug: "walk", ComponentRoot: e.root, Title: "W", Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	step, err := st.CreateStep(store.Scope{LoopSlug: "walk", ComponentRoot: e.root}, store.NewStep{TaskID: "T1", Phase: "build", Agent: e.agent})
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	e.ctx = stepctx.WithStepID(context.Background(), step.ID)
	e.svc = NewRecordsService(st)
	log := &wire{}
	e.v = NewVerifyService(st, log)
	e.m = NewMergeRuleService(st, log)
	e.svc.verify = e.v
	e.svc.mergeRule = e.m
	e.svc.dod = NewDoDService(st, log)
	e.svc.proposals = proposalStore(st)
	e.svc.validateProposal = gateTestValidator(st)
	return e
}

func (e gateProposeEnv) do(t *testing.T, name, input string) (any, *Failure, error) {
	t.Helper()
	return e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, name, json.RawMessage(input))
}

func (e gateProposeEnv) doAs(t *testing.T, kind, id, name, input string) (any, *Failure, error) {
	t.Helper()
	return e.svc.Do(e.ctx, Actor{Kind: kind, ID: id}, name, json.RawMessage(input))
}

// head is the component root's current head — the same rev-parse HEAD the
// arm's currency check runs.
func (e gateProposeEnv) head(t *testing.T) string {
	t.Helper()
	h, err := testGitClient().LocalHEAD(e.ctx, e.root)
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	return h
}

// addGate writes one gate through the service's person door — the SAME
// instance the tool reads through, so the gate the tool shows is the gate
// the person added (one implementation, two doors).
func (e gateProposeEnv) addGate(t *testing.T, name, command, requirement string) string {
	t.Helper()
	res, ref, err := e.v.Add(context.Background(), protocol.Actor{Kind: "user", ID: "1000"}, protocol.VerifyAddInput{
		ComponentID: e.comp.ID, Name: name, Command: command, Requirement: requirement, TimeoutS: 30,
	})
	if err != nil {
		t.Fatalf("add gate %q: %v", name, err)
	}
	if ref != nil {
		t.Fatalf("add gate %q refused: %s · %s", name, ref.Kind, ref.Detail)
	}
	return res.Gate.ID
}

// addRun files one FINISHED verify run — the evidence the met mark names —
// and returns its id. state is the run's closed outcome; head the sha the
// run sat on (a stale one is exactly what the currency check must catch);
// position keeps each run off the (loop, root, position, attempt) slot the
// store's duplicate-dispatch index guards.
func (e gateProposeEnv) addRun(t *testing.T, loopSlug, state, head string, position int) string {
	t.Helper()
	actor := store.Actor{Kind: "agent", ID: e.agent}
	draft, err := e.s.CreateVerifyRun(actor, store.NewVerifyRun{
		LoopSlug:      loopSlug,
		ComponentRoot: e.root,
		ComponentID:   e.comp.ID,
		Task:          "T1",
		Position:      position,
		Attempt:       1,
		HeadSHA:       head,
		Trigger:       "loop",
		Actor:         actor,
		StartedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	r := store.VerifyRunResult{Name: "gate", Requirement: "advisory", Command: "true"}
	if state != "pass" {
		r.State, r.Reason = "fail", "the gate failed"
	} else {
		r.State = "pass"
	}
	if err := draft.AppendVerifyResult(r); err != nil {
		t.Fatalf("append result: %v", err)
	}
	run, err := e.s.FinishVerifyRun(draft, state, "", "", "", time.Now())
	if err != nil {
		t.Fatalf("finish run: %v", err)
	}
	return run.ID
}

// materialise files the loop's acceptance list and returns the item ids in
// idx order — the ids dod_mark must name.
func (e gateProposeEnv) materialise(t *testing.T, briefID string, version int, items ...string) []string {
	t.Helper()
	if _, err := e.s.MaterializeDoD(store.Scope{LoopSlug: "walk", ComponentRoot: e.root}, store.DoDBrief{
		BriefID: briefID, BriefVersion: version, Items: items,
	}); err != nil {
		t.Fatalf("materialise: %v", err)
	}
	list, err := e.s.ListDoD(store.Scope{LoopSlug: "walk", ComponentRoot: e.root})
	if err != nil {
		t.Fatalf("list dod: %v", err)
	}
	ids := make([]string, 0, len(list))
	for _, it := range list {
		ids = append(ids, it.ID)
	}
	return ids
}

// setRule writes the component's OWN rule through the service's person
// door — the baseline Tightens runs against.
func (e gateProposeEnv) setRule(t *testing.T, in mergerule.Rule) {
	t.Helper()
	_, ref, err := e.m.Set(context.Background(), protocol.Actor{Kind: "user", ID: "1000"}, protocol.MergeRuleSetInput{
		ComponentID:            in.ComponentID,
		Enabled:                in.Enabled,
		RequireLocal:           in.RequireLocal,
		RequireDoD:             in.RequireDoD,
		RequiredChecks:         in.RequiredChecks,
		ChecksPolicy:           in.ChecksPolicy,
		NoChecks:               in.NoChecks,
		ReviewMode:             in.ReviewMode,
		AeonReviewAgents:       in.AeonReviewAgents,
		RequireThreadsResolved: in.RequireThreadsResolved,
		Method:                 in.Method,
	})
	if err != nil {
		t.Fatalf("set rule: %v", err)
	}
	if ref != nil {
		t.Fatalf("set rule refused: %s · %s", ref.Kind, ref.Detail)
	}
}

// ── the test validator: cmd/aeon's seams, mirrored ──────────────────────────

// gateTestValidator is the test mirror of cmd/aeon's production seams for
// the two gate kinds (the daemon package may not import main): the desk's
// own shape half — THE function the create door runs (TD3) — then the
// store half over the same real store: the component exists and is not
// archived, the name is free, the rule's fields are in the closed sets,
// and the rule TIGHTENS the current one field by field (a loosening is
// refused at create, each loosened field named; the forge↔aeon switch is
// refused too, both directions). cmd/aeon's
// TestProposeValidatorParityWithCreate pins the PRODUCTION seam's bytes
// against proposal.create; this mirror exists so the daemon's create door
// can be walked without that import.
func gateTestValidator(s *store.Store) func(context.Context, string, string) error {
	return func(ctx context.Context, kind, payload string) error {
		switch kind {
		case string(desk.KindVerifyCommand):
			if err := desk.ValidateVerifyCommand(context.Background(), []byte(payload)); err != nil {
				return err
			}
			var p desk.VerifyCommandProposal
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return err
			}
			c, err := s.ReadComponent(p.ComponentID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("refused: %s is not a component — a gate needs a node it can sit on", p.ComponentID)
				}
				return err
			}
			if c.ArchivedAt != "" {
				return fmt.Errorf("refused: %s is archived — its gates cannot be written", p.ComponentID)
			}
			gates, err := s.ListVerifyCommands(store.VerifyCommandFilter{ComponentID: p.ComponentID})
			if err != nil {
				return err
			}
			for _, g := range gates {
				if g.Name == p.Name {
					return fmt.Errorf("refused: %s already has a live gate named %q (archive it first)", p.ComponentID, p.Name)
				}
			}
			return nil
		case string(desk.KindMergeRule):
			if err := desk.ValidateMergeRule(context.Background(), []byte(payload)); err != nil {
				return err
			}
			var p struct {
				Rule json.RawMessage `json:"rule"`
			}
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return err
			}
			var rule mergerule.Rule
			if err := json.Unmarshal(p.Rule, &rule); err != nil {
				return fmt.Errorf("refused: the proposed rule does not parse: %v", err)
			}
			c, err := s.ReadComponent(rule.ComponentID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("refused: %s is not a component", rule.ComponentID)
				}
				return err
			}
			if c.ArchivedAt != "" {
				return fmt.Errorf("refused: %s is archived — its rule is history", rule.ComponentID)
			}
			if err := gateRuleFieldRefusals(rule); err != nil {
				return err
			}
			current, _, err := s.EffectiveMergeRule(rule.ComponentID)
			if err != nil {
				return err
			}
			if tightened, loosened := mergerule.Tightens(current, rule); !tightened {
				if len(loosened) == 0 {
					return fmt.Errorf("refused: the proposed rule switches the review mode between forge and aeon — the one incomparable pair, and a proposal may not authorise a switch either way (S57 T8)")
				}
				return fmt.Errorf("refused: the proposed rule loosens %s — a merge_rule proposal may only tighten, and a loosening is a person's act (S57 T8)", strings.Join(loosened, ", "))
			}
			return nil
		default:
			return fmt.Errorf("refused: kind %q is not a gate kind this mirror knows", kind)
		}
	}
}

// gateRuleFieldRefusals re-declares SetMergeRule's five closed sets, the
// store's words byte for byte (the seam's own half, mirrored).
func gateRuleFieldRefusals(r mergerule.Rule) error {
	switch {
	case r.RequireDoD != mergerule.RequireDoDNone && r.RequireDoD != mergerule.RequireDoDAllMet:
		return fmt.Errorf("refused: require_dod is %q; the closed set is none|all_met", r.RequireDoD)
	case r.ChecksPolicy != mergerule.ChecksReported && r.ChecksPolicy != mergerule.ChecksRequired:
		return fmt.Errorf("refused: checks_policy is %q; the closed set is reported_green|required_only", r.ChecksPolicy)
	case r.NoChecks != mergerule.NoChecksAllow && r.NoChecks != mergerule.NoChecksRefuse:
		return fmt.Errorf("refused: no_checks is %q; the closed set is allow|refuse", r.NoChecks)
	case r.ReviewMode != mergerule.ReviewModeForge && r.ReviewMode != mergerule.ReviewModeAeon &&
		r.ReviewMode != mergerule.ReviewModeEither && r.ReviewMode != mergerule.ReviewModeBoth:
		return fmt.Errorf("refused: review_mode is %q; the closed set is forge|aeon|either|both", r.ReviewMode)
	case r.Method != mergerule.MethodSquash && r.Method != mergerule.MethodMerge && r.Method != mergerule.MethodRebase:
		return fmt.Errorf("refused: method is %q; the closed set is squash|merge|rebase", r.Method)
	}
	return nil
}

// ── the behaviour rows the task names ────────────────────────────────────────

// TestGateProposeCreatedByIsTheAgentActor pins FORCED IDENTITY (VG19's
// identity row): the inert row is stamped from the STEP — the step's agent
// and kind — and the params have no field to smuggle a different author in
// (a smuggled author is a parse refusal, the schema's strict decode), while
// a person on these names is turned back before the switch by name.
func TestGateProposeCreatedByIsTheAgentActor(t *testing.T) {
	e := newGateProposeEnv(t)

	out, f, err := e.do(t, "verify_propose_add", `{"name":"gate-a","command":"make test","requirement":"advisory","timeoutS":30,"reason":"the loop must run the gate before it merges"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f != nil {
		t.Fatalf("refused: %s", f.Sentence)
	}
	pr, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("the arm answered %T, want the inert propose result", out)
	}
	if pr.Proposal.Kind != "verify_command" || pr.Proposal.CreatedByKind != "agent" ||
		pr.Proposal.CreatedByID != e.agent || pr.Proposal.Status != "proposed" {
		t.Errorf("the row is not stamped from the step as an inert proposed row: %+v", pr.Proposal)
	}
	if want := protocol.ProposalCommand(pr.Proposal.ID); pr.Proposal.Command != want {
		t.Errorf("the row carries %q, want the commit command %q", pr.Proposal.Command, want)
	}
	if pr.Note != protocol.PolicyProposeInertNote {
		t.Errorf("the row answers %q, want the fixed inert note", pr.Note)
	}

	// The smuggled author: the schema has no field for one — a parse
	// refusal, with the field named.
	_, f, err = e.do(t, "verify_propose_add", `{"name":"gate-a","command":"make test","requirement":"advisory","timeoutS":30,"reason":"x","createdBy":"the-agent"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f == nil || !strings.Contains(f.Sentence, "createdBy") {
		t.Errorf("a smuggled createdBy is a parse refusal naming the field, got %v", f)
	}

	// The person's half: every one of the nine names turns a person back
	// by name — the six reads point at the socket reads, the three twins
	// at the person's own doors.
	personWant := map[string]string{
		"verify_list":               "the socket already speaks verify.list",
		"verify_show":               "the socket already speaks verify.show",
		"verify_runs":               "the socket already speaks verify.runs",
		"mergerule_show":            "the socket already speaks mergerule.get",
		"mergerule_preview":         "the socket already speaks mergerule.preview",
		"dod_list":                  "the socket already speaks dod.list",
		"verify_propose_add":        "the person's door",
		"mergerule_propose_tighten": "the person's door",
		"dod_mark":                  "the socket already speaks dod.mark",
	}
	for name, want := range personWant {
		_, f, err := e.doAs(t, "user", "1000", name, `{}`)
		if err != nil {
			t.Fatalf("%s err: %v", name, err)
		}
		if f == nil || !strings.Contains(f.Sentence, want) {
			t.Errorf("%s: a person is turned back by name (%q), got %v", name, want, f)
		}
	}
}

// readGateSurface answers the six reads and lists the gate tables — the
// byte stream a propose must not touch.
func (e gateProposeEnv) readGateSurface(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	calls := []struct{ name, input string }{
		{"verify_list", `{}`},
		{"verify_show", `{}`}, // refused: no gateId — a stable refusal, still a read of nothing foreign
		{"verify_runs", `{}`},
		{"mergerule_show", `{}`},
		{"mergerule_preview", `{}`},
		{"dod_list", `{}`},
	}
	for _, c := range calls {
		out, f, err := e.do(t, c.name, c.input)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var payload any
		if f != nil {
			payload = f.Sentence
		} else {
			payload = out
		}
		raw, merr := json.Marshal(payload)
		if merr != nil {
			t.Fatalf("marshal %s: %v", c.name, merr)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	gates, err := e.s.ListVerifyCommands(store.VerifyCommandFilter{ComponentID: e.comp.ID})
	if err != nil {
		t.Fatalf("list gates: %v", err)
	}
	g, _ := json.Marshal(gates)
	buf.Write(g)
	buf.WriteByte('\n')
	rule, rerr := e.s.GetMergeRule(e.comp.ID)
	if rerr == nil {
		if r, _ := json.Marshal(rule); rerr == nil {
			buf.Write(r)
		}
	}
	if eff, _, err := e.s.EffectiveMergeRule(e.comp.ID); err == nil {
		if r, _ := json.Marshal(eff); rerr == nil {
			buf.Write(r)
		}
	}
	items, err := e.s.ListDoD(store.Scope{LoopSlug: "walk", ComponentRoot: e.root})
	if err != nil {
		t.Fatalf("list dod: %v", err)
	}
	if i, _ := json.Marshal(items); err == nil {
		buf.Write(i)
	}
	return buf.Bytes()
}

// TestGateProposeIsInert pins INERT (S35 TD3, the S57 T9 ruling): the
// propose files ONE inert row and nothing else moves — the six read
// surfaces, the gate table, the rule row and the DoD ledger are
// byte-identical across it, and the row itself is the inert one with the
// fixed note. The commit door is the person's; nothing here applies.
func TestGateProposeIsInert(t *testing.T) {
	e := newGateProposeEnv(t)
	e.addGate(t, "existing", "make test", "advisory")
	ids := e.materialise(t, "brief-1", 1, "the tests pass", "the docs build")
	e.setRule(t, mergerule.Rule{
		ComponentID: e.comp.ID, Enabled: true,
		RequireDoD: mergerule.RequireDoDNone, ChecksPolicy: mergerule.ChecksReported,
		NoChecks: mergerule.NoChecksAllow, ReviewMode: mergerule.ReviewModeForge, Method: mergerule.MethodSquash,
	})

	before := e.readGateSurface(t)

	out, f, err := e.do(t, "verify_propose_add", `{"name":"gate-b","command":"make lint","requirement":"required","timeoutS":60,"reason":"the loop lints before it merges"}`)
	if err != nil || f != nil {
		t.Fatalf("verify_propose_add: %v %v", err, f)
	}
	if pr, ok := out.(protocol.PolicyProposeResult); !ok || pr.Note != protocol.PolicyProposeInertNote {
		t.Fatalf("the arm did not answer the inert note: %v", out)
	}
	out, f, err = e.do(t, "mergerule_propose_tighten", `{"fields":{"enabled":true,"requireLocal":false,"requireDod":"all_met","checksPolicy":"reported_green","noChecks":"refuse","reviewMode":"both","method":"squash"},"reason":"the loop must not merge unverified"}`)
	if err != nil || f != nil {
		t.Fatalf("mergerule_propose_tighten: %v %v", err, f)
	}
	if pr, ok := out.(protocol.PolicyProposeResult); !ok || pr.Note != protocol.PolicyProposeInertNote {
		t.Fatalf("the arm did not answer the inert note: %v", out)
	}

	after := e.readGateSurface(t)
	if !bytes.Equal(before, after) {
		t.Errorf("the propose moved the gate surface — inert means byte-identical:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := e.s.GetDoDItem(ids[0]); err != nil {
		t.Fatalf("the DoD items vanished: %v", err)
	}
	rows, err := e.svc.proposals.ListProposals("proposed")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the two proposes left %d inert rows, want exactly 2", len(rows))
	}
	kinds := map[string]bool{}
	for _, r := range rows {
		kinds[r.Kind] = true
	}
	if !kinds["verify_command"] || !kinds["merge_rule"] {
		t.Errorf("the inert rows are %+v, want one per gate kind", rows)
	}
}

// TestGateProposeReasonRequired pins the arm's reason row: missing, blank
// and over-long are refusals with the reason rule in them (1 to 512), and
// a present reason is what lets the create go through.
func TestGateProposeReasonRequired(t *testing.T) {
	e := newGateProposeEnv(t)
	cases := []struct {
		name string
		body string
		want string
	}{
		{"missing", `{"name":"gate-a","command":"make test","requirement":"advisory","timeoutS":30}`, "needs a reason"},
		{"blank", `{"name":"gate-a","command":"make test","requirement":"advisory","timeoutS":30,"reason":"   "}`, "needs a reason"},
		{"overlong", `{"name":"gate-a","command":"make test","requirement":"advisory","timeoutS":30,"reason":"` + strings.Repeat("a", 513) + `"}`, "512"},
	}
	for _, c := range cases {
		_, f, err := e.do(t, "verify_propose_add", c.body)
		if err != nil {
			t.Fatalf("%s: err %v", c.name, err)
		}
		if f == nil || !strings.Contains(f.Sentence, c.want) {
			t.Errorf("%s: want a refusal containing %q, got %v", c.name, c.want, f)
		}
	}
	// the tightening arm carries the same rule on its own name
	_, f, err := e.do(t, "mergerule_propose_tighten", `{"fields":{"enabled":true,"requireDod":"all_met","checksPolicy":"reported_green","noChecks":"allow","reviewMode":"forge","method":"squash"}}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "needs a reason") {
		t.Errorf("mergerule without a reason is a refusal, got %v", f)
	}
	// and the 512-byte boundary is inclusive
	_, f, err = e.do(t, "verify_propose_add", `{"name":"gate-a","command":"make test","requirement":"advisory","timeoutS":30,"reason":"`+strings.Repeat("a", 512)+`"}`)
	if err != nil || f != nil {
		t.Errorf("a 512-byte reason is legal, got %v", f)
	}
}

// TestGateProposeTightenRefusesLooseningEvenIfSchemaBypassed pins the
// second defence (S57 T8's law at T9's door): the schema admits only the
// tightening direction's values, but the SERVICE re-checks — a loosening
// is refused with EACH loosened field named, a forge↔aeon review switch is
// refused both ways, and a real tightening files its inert row. The direct
// arm call is the bypass: it speaks the service, not the schema.
func TestGateProposeTightenRefusesLooseningEvenIfSchemaBypassed(t *testing.T) {
	e := newGateProposeEnv(t)
	e.setRule(t, mergerule.Rule{
		ComponentID: e.comp.ID, Enabled: true, RequireLocal: true,
		RequireDoD: mergerule.RequireDoDNone, ChecksPolicy: mergerule.ChecksReported,
		NoChecks: mergerule.NoChecksRefuse, ReviewMode: mergerule.ReviewModeBoth, Method: mergerule.MethodSquash,
	})

	// Two loosened fields at once: the sentence names both, in the rule's
	// field order.
	_, f, err := e.do(t, "mergerule_propose_tighten", `{"fields":{"enabled":true,"requireLocal":false,"requireDod":"none","checksPolicy":"reported_green","noChecks":"allow","reviewMode":"both","method":"squash"},"reason":"loosen the gate for a hotfix"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f == nil || !strings.Contains(f.Sentence, "RequireLocal") || !strings.Contains(f.Sentence, "NoChecks") ||
		!strings.Contains(f.Sentence, "may only tighten") {
		t.Errorf("a loosening is refused naming each loosened field, got %v", f)
	}

	// The one incomparable pair: a forge↔aeon review switch — neither
	// direction is a loosening (the loosened list stays empty), so the
	// refusal is the switch sentence, and it holds BOTH ways.
	e.setRule(t, mergerule.Rule{
		ComponentID: e.comp.ID, Enabled: true, RequireLocal: true,
		RequireDoD: mergerule.RequireDoDNone, ChecksPolicy: mergerule.ChecksReported,
		NoChecks: mergerule.NoChecksRefuse, ReviewMode: mergerule.ReviewModeForge, Method: mergerule.MethodSquash,
	})
	_, f, err = e.do(t, "mergerule_propose_tighten", `{"fields":{"enabled":true,"requireLocal":true,"requireDod":"none","checksPolicy":"reported_green","noChecks":"refuse","reviewMode":"aeon","method":"squash"},"reason":"aeon review is faster"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "switches the review mode") {
		t.Errorf("a forge→aeon switch is refused as the incomparable pair, got %v", f)
	}
	// and the other direction, from an aeon row
	e.setRule(t, mergerule.Rule{
		ComponentID: e.comp.ID, Enabled: true, RequireLocal: true,
		RequireDoD: mergerule.RequireDoDNone, ChecksPolicy: mergerule.ChecksReported,
		NoChecks: mergerule.NoChecksRefuse, ReviewMode: mergerule.ReviewModeAeon, Method: mergerule.MethodSquash,
	})
	_, f, err = e.do(t, "mergerule_propose_tighten", `{"fields":{"enabled":true,"requireLocal":true,"requireDod":"none","checksPolicy":"reported_green","noChecks":"refuse","reviewMode":"forge","method":"squash"},"reason":"forge-only for speed"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "switches the review mode") {
		t.Errorf("an aeon→forge switch is refused as the incomparable pair, got %v", f)
	}

	// A real tightening (requireDod none→all_met, an additive required
	// check) files the inert row.
	out, f, err := e.do(t, "mergerule_propose_tighten", `{"fields":{"enabled":true,"requireLocal":true,"requireDod":"all_met","requiredChecks":["ci"],"checksPolicy":"reported_green","noChecks":"refuse","reviewMode":"both","method":"squash"},"reason":"the loop must verify the DoD"}`)
	if err != nil || f != nil {
		t.Fatalf("a tightening is refused: %v %v", err, f)
	}
	if pr, ok := out.(protocol.PolicyProposeResult); !ok || pr.Proposal.Kind != "merge_rule" || pr.Proposal.Status != "proposed" {
		t.Errorf("the tightening filed %v, want the inert merge_rule row", out)
	}
}

// TestDoDMarkNeedsEvidenceForMet pins EVIDENCE-BOUND (VG13 at T9's door):
// a met names a PASSING run of the caller's OWN loop on the CURRENT head —
// a missing, failing, foreign-loop or stale-head run is refused, naming
// what is missing; unmet and unverifiable are free; a non-met carrying a
// run id is refused; the accepted mark lands agent-stamped on the row.
func TestDoDMarkNeedsEvidenceForMet(t *testing.T) {
	e := newGateProposeEnv(t)
	ids := e.materialise(t, "brief-1", 1, "the tests pass")
	item := ids[0]
	head := e.head(t)
	passRun := e.addRun(t, "walk", "pass", head, 1)
	failRun := e.addRun(t, "walk", "fail", head, 2)

	// met with no run id: the refusal names the evidence a met carries.
	_, f, err := e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"met"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f == nil || !strings.Contains(f.Sentence, "a met mark names its evidence") {
		t.Errorf("a met without a run is refused naming the evidence, got %v", f)
	}

	// met on a run that exists but did not pass.
	_, f, err = e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"met","evidenceRunId":"`+failRun+`"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "not pass") {
		t.Errorf("a met on a failing run is refused, got %v", f)
	}

	// met on a run of ANOTHER loop of the same component — a foreign-loop
	// act, refused naming the caller's own.
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "other", ComponentRoot: e.root, Title: "O", Goal: "g"}); err != nil {
		t.Fatalf("other loop: %v", err)
	}
	foreignRun := e.addRun(t, "other", "pass", head, 1)
	_, f, err = e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"met","evidenceRunId":"`+foreignRun+`"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "is not your loop walk") {
		t.Errorf("a met on another loop's run is a foreign act, got %v", f)
	}

	// met on a passing run that sat on an OLD head — stale evidence.
	staleRun := e.addRun(t, "walk", "pass", "000000000000000000000000000000000000dead", 3)
	_, f, err = e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"met","evidenceRunId":"`+staleRun+`"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "stale head") {
		t.Errorf("a met on an old head is stale evidence, got %v", f)
	}

	// unmet / unverifiable are free, naming nothing — and a non-met that
	// carries a run id is refused, because a run id is a met's word.
	for _, state := range []string{"unmet", "unverifiable"} {
		out, f, err := e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"`+state+`","note":"the build is red"}`)
		if err != nil || f != nil {
			t.Fatalf("%s is free: %v %v", state, err, f)
		}
		if w, ok := out.(protocol.DoDWriteResult); !ok || w.Item.State != state {
			t.Errorf("%s landed %v", state, out)
		}
	}
	_, f, err = e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"unmet","evidenceRunId":"`+passRun+`"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "only a met carries") {
		t.Errorf("a non-met with a run id is refused, got %v", f)
	}

	// the foreign ITEM: a row of another loop is not the caller's.
	otherItems, err := e.s.MaterializeDoD(store.Scope{LoopSlug: "other", ComponentRoot: e.root}, store.DoDBrief{BriefID: "brief-9", BriefVersion: 1, Items: []string{"their item"}})
	if err != nil || otherItems != 1 {
		t.Fatalf("materialise other loop: %v", err)
	}
	otherList, err := e.s.ListDoD(store.Scope{LoopSlug: "other", ComponentRoot: e.root})
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	_, f, err = e.do(t, "dod_mark", `{"itemId":"`+otherList[0].ID+`","state":"unmet"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "not a definition-of-done item of your loop") {
		t.Errorf("a foreign item is refused naming the caller's loop, got %v", f)
	}

	// the met that is allowed: passing, own loop, current head — and the
	// row lands agent-stamped, the evidence bound to the run.
	out, f, err := e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"met","evidenceRunId":"`+passRun+`","note":"green on the head"}`)
	if err != nil || f != nil {
		t.Fatalf("the allowed met is refused: %v %v", err, f)
	}
	w, ok := out.(protocol.DoDWriteResult)
	if !ok || w.Item.State != "met" || w.Item.EvidenceKind != "verify_run" || w.Item.EvidenceRef != passRun {
		t.Fatalf("the met landed without its evidence: %+v", out)
	}
	if w.Item.ActorKind == nil || *w.Item.ActorKind != "agent" || w.Item.ActorID == nil || *w.Item.ActorID != e.agent {
		t.Errorf("the mark is stamped from the caller, got %v/%v", w.Item.ActorKind, w.Item.ActorID)
	}
	// and the no-op re-mark succeeds quietly (the store's honest state).
	if _, f, err := e.do(t, "dod_mark", `{"itemId":"`+item+`","state":"met","evidenceRunId":"`+passRun+`"}`); err != nil || f != nil {
		t.Errorf("a re-mark of the same evidence is a quiet no-op, got %v", f)
	}
}

// TestGateToolsAreScopedToTheCallersLoop pins SCOPED (VG19): a foreign
// loop or component in the params is refused, naming the caller's own; the
// scope is the stamp, never a param — a smuggled scope field is a parse
// refusal. The scope a step stands on is the one it got from its loop row,
// and the read answers that scope only.
func TestGateToolsAreScopedToTheCallersLoop(t *testing.T) {
	e := newGateProposeEnv(t)

	// a foreign loop, named, on the two loop-scoped reads
	for _, name := range []string{"verify_runs", "dod_list"} {
		_, f, err := e.do(t, name, `{"loopSlug":"other"}`)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "the loop other is not your loop walk") {
			t.Errorf("%s: a foreign loop is refused naming the caller's own, got %v", name, f)
		}
	}

	// a foreign component, named, on the preview
	_, f, err := e.do(t, "mergerule_preview", `{"componentId":"some-other-component"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f == nil || !strings.Contains(f.Sentence, "the component some-other-component is not your component") {
		t.Errorf("a foreign component is refused naming the caller's own, got %v", f)
	}

	// a gate that EXISTS but belongs to another component: the show reads
	// it, finds the owner, and refuses naming both.
	// a separate workspace root — a root path belongs to exactly one
	// component, so the sibling cannot share alpha's
	beta, err := e.s.CreateComponent(store.NewComponent{Name: "beta", Slug: "beta", Root: "/repo/beta", ParentID: e.comp.ParentID})
	if err != nil {
		t.Fatalf("beta: %v", err)
	}
	foreignRes, foreignRef, err := e.v.Add(context.Background(), protocol.Actor{Kind: "user", ID: "1000"}, protocol.VerifyAddInput{
		ComponentID: beta.Component.ID, Name: "beta-gate", Command: "true", Requirement: "advisory", TimeoutS: 30,
	})
	if err != nil || foreignRef != nil {
		t.Fatalf("foreign gate: %v %v", err, foreignRef)
	}
	_, f, err = e.do(t, "verify_show", `{"gateId":"`+foreignRes.Gate.ID+`"}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f == nil || !strings.Contains(f.Sentence, "not your component "+e.comp.ID) {
		t.Errorf("a foreign gate is refused naming the caller's component, got %v", f)
	}

	// the scope is the stamp, never a param: every smuggled scope field is
	// a parse refusal naming the field (the schemas carry none of them).
	smuggled := []struct{ name, body string }{
		{"verify_list", `{"componentId":"x"}`},
		{"mergerule_show", `{"componentId":"x"}`},
		{"dod_mark", `{"itemId":"whatever","state":"unmet","loopSlug":"other"}`},
	}
	for _, c := range smuggled {
		_, f, err := e.do(t, c.name, c.body)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "unknown field") {
			t.Errorf("%s: a smuggled scope field is a parse refusal, got %v", c.name, f)
		}
	}
}

// TestGateArmsUnwiredAreStated pins V13 at T9's door: a mount whose gate
// service was not wired REFUSES the nine names with the named sentence —
// the gate surface is absent here, never answered zero. The plain records
// env wires none of the three gates, so every arm must name its absent
// half.
func TestGateArmsUnwiredAreStated(t *testing.T) {
	e := newRecordsEnv(t, "")
	wants := map[string]string{
		"verify_list":               "needs the verify gate service",
		"verify_show":               "needs the verify gate service",
		"verify_runs":               "needs the verify gate service",
		"mergerule_show":            "needs the merge rule gate service",
		"mergerule_preview":         "needs the merge rule gate service",
		"dod_list":                  "needs the DoD gate service",
		"verify_propose_add":        "the proposal store",
		"mergerule_propose_tighten": "the proposal store",
		"dod_mark":                  "needs the DoD gate service",
	}
	for name, want := range wants {
		_, f, err := e.do(t, name, `{}`)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f == nil || !strings.Contains(f.Sentence, want) {
			t.Errorf("%s: an unwired arm states its absence (%q), got %v", name, want, f)
		}
	}
}
