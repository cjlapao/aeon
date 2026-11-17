package main

// intake_plan_proposal_test.go — S61 T5 (GI7/OD4): the intake_plan kind at
// the doors that exist. The create door refuses a plan whose charter the
// charter refuses, and a plan that names a template nobody installed — the
// seam's sentences, byte-identical to the validator the commit door re-runs
// (TD3). And the generic commit door REFUSES: its apply half IS the fixed
// sentence naming intake.commit, the row stays proposed with the reason in
// applied_error, and nothing the plan describes came into being — no
// components, no charter, no churn on the templates. That refusal is the
// kind's design, not a gap: the plan carries no target path and no publish
// choice, and only intake.commit (T6) holds them.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// intakePlanJSON is a VALID plan on its own terms (role law, references,
// caps) — the seam's own reads decide whether it survives.
const intakePlanJSON = `{"intakeId":"intake-t5","charter":{"goal":"ship the demo"},"components":[{"name":"Server","slug":"server","template":"go-service","role":"root","tags":["go"]},{"name":"App","slug":"app","template":"react-app","role":"child"}],"projectName":"Intake Demo","epics":[{"key":"core","title":"Core","objective":"the core"}],"goals":[{"epicKey":"core","title":"Stand it up","type":"goal","componentSlugs":["server","app"],"criteria":[{"text":"it boots","check":"command","ref":"make run"}]}],"items":[{"componentSlug":"server","epicKey":"core","title":"scaffold","priority":"unknown"}],"reason":"the project starts here"}`

type intakePropEnv struct {
	t  *testing.T
	st *store.Store
	d  *daemon.Daemon
}

func newIntakePropEnv(t *testing.T) intakePropEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	for _, slug := range []string{"go-service", "react-app"} {
		_, ref, err := st.SaveTemplate(store.NewTemplate{Slug: slug, Title: slug,
			Files: []store.TemplateFile{{Path: "README.md", Mode: 0o644, Content: "# " + slug + "\n"}}},
			store.Actor{Kind: "user", ID: "person"})
		if err != nil || ref != nil {
			t.Fatalf("install template %s: ref=%v err=%v", slug, ref, err)
		}
	}
	d := daemon.New(daemon.Options{
		Store:            st,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return deskEffects, nil },
	})
	return intakePropEnv{t: t, st: st, d: d}
}

func (e intakePropEnv) createViaSocket(conn, kind, payload string) (protocol.ProposalRow, error) {
	e.t.Helper()
	raw, _ := json.Marshal(protocol.InitializeParams{ClientName: "intake", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, raw); err != nil {
		e.t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"kind": kind, "payload": json.RawMessage(payload)})
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodProposalCreate, raw)
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return res.(protocol.ProposalRow), nil
}

// TestIntakePlanCreateDoorRunsTheCharterAndTemplateHalves: the desk shape
// passes, and the plan still dies at create on a charter sentence or a
// missing template — the seam's words, identical to the validator bytes.
func TestIntakePlanCreateDoorRunsTheCharterAndTemplateHalves(t *testing.T) {
	e := newIntakePropEnv(t)
	unknownTemplate := strings.Replace(intakePlanJSON, `"template":"react-app"`, `"template":"no-such-template"`, 1)
	for _, tc := range []struct {
		name    string
		payload string
		must    []string
	}{
		{"unknown-template", unknownTemplate, []string{"no such scaffold template is installed", "no-such-template", "app"}},
		{"charter-refused", strings.Replace(intakePlanJSON, `"goal":"ship the demo"`, `"goal":""`, 1), []string{"charter: goal is required"}},
	} {
		_, doorErr := e.createViaSocket("c-intake-"+tc.name, "intake_plan", tc.payload)
		if doorErr == nil {
			t.Fatalf("%s: the create door accepted it", tc.name)
		}
		direct := validateIntakePlanProposal([]byte(tc.payload))
		if direct == nil {
			t.Fatalf("%s: the validator accepted what the door refused", tc.name)
		}
		var perr *protocol.Error
		if !errors.As(doorErr, &perr) {
			t.Fatalf("%s: the door did not answer the refused shape: %v", tc.name, doorErr)
		}
		if perr.Data.What != direct.Error() {
			t.Fatalf("%s: the doors drifted:\n door:  %s\n valid: %s", tc.name, perr.Data.What, direct)
		}
		for _, needle := range tc.must {
			if !strings.Contains(perr.Data.What, needle) {
				t.Fatalf("%s: the refusal does not carry %q verbatim: %s", tc.name, needle, perr.Data.What)
			}
		}
	}
	if rows, err := e.st.ListProposals(""); err != nil || len(rows) != 0 {
		t.Fatalf("refused creates stored %d rows", len(rows))
	}
}

// TestGenericCommitRefusesIntakePlan is GI7/OD4 at the real door: a fully
// VALID plan, committed by a person through proposal.commit, meets the
// fixed sentence — the row stays proposed with the refusal in
// applied_error, and the world the plan describes was not built: no
// components, no charter revision anywhere, the templates untouched.
func TestGenericCommitRefusesIntakePlan(t *testing.T) {
	e := newIntakePropEnv(t)
	row, err := e.st.CreateProposal("intake_plan", intakePlanJSON, store.Actor{Kind: "agent", ID: "manny"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, row.ID,
		desk.ProposalPayloadDigest(intakePlanJSON), desk.Actor{Kind: "user", ID: "u-committ"})
	if err == nil {
		t.Fatal("the generic commit door applied an intake_plan — the refusal that IS this kind's apply half went missing (GI7/OD4)")
	}
	if !strings.Contains(err.Error(), desk.IntakeCommitRefusal) {
		t.Fatalf("the refusal is not the fixed sentence verbatim: %v", err)
	}
	if !strings.Contains(err.Error(), "stays proposed") {
		t.Fatalf("the door did not say the row stays proposed: %v", err)
	}
	again, ok, err := e.st.GetProposal(row.ID)
	if err != nil || !ok {
		t.Fatalf("read back: ok=%v err=%v", ok, err)
	}
	if again.Status != "proposed" {
		t.Errorf("status = %q, want proposed — an unappliable ask must not decide itself", again.Status)
	}
	if !strings.Contains(again.AppliedError, "intake.commit") {
		t.Errorf("applied_error = %q, want the refusal recorded", again.AppliedError)
	}
	// Nothing came into being: the registry of components is still the
	// empty one, and no root anywhere carries a charter.
	comps, err := e.st.ListComponents(store.ComponentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 0 {
		t.Errorf("the commit scaffolded %d components — intake_plan applies NOTHING through the generic door", len(comps))
	}
	for _, slug := range []string{"go-service", "react-app"} {
		tmpl, terr := e.st.GetTemplate(slug)
		if terr != nil || tmpl.Version != 1 {
			t.Errorf("template %s was touched: v%d err=%v", slug, tmpl.Version, terr)
		}
	}
}
