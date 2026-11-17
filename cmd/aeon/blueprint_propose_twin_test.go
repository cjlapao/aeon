package main

// blueprint_propose_twin_test.go — S48 T10 (BP9): the socket twin of the
// blueprint_propose tool, end to end, over the PRODUCTION validator. The
// daemon-side suite (internal/daemon/records_blueprint_test.go) measures
// the arms' contract — byte-equal reads, zero writes, the caps, the agent
// stamp — over a mirrored validator, because package daemon cannot import
// package main. What lives HERE is the other half of that honesty: the
// same door with cmd/aeon's real blueprint_save seam wired, so the
// refusal an agent meets is the save door's own report (TD3), and the row
// a valid proposal files is inert until a person commits it. The author
// over the socket is the PEER (a person holding the endpoint may propose
// too — the stamp is the point, never the value); the agent-stamped case
// is the daemon suite's.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func (e bpProposalEnv) proposeViaTwin(conn, params string) (protocol.PolicyProposeResult, error) {
	e.t.Helper()
	res, err := e.d.Table.Dispatch(context.Background(), conn, protocol.MethodBlueprintPropose, json.RawMessage(params))
	if err != nil {
		return protocol.PolicyProposeResult{}, err
	}
	return res.(protocol.PolicyProposeResult), nil
}

// TestBlueprintProposeTwinRefusesInvalidWithTheReportAndFilesNothing:
// rule 2 over the real seam — the save door's report travels VERBATIM in
// Data.What, and the desk holds no new row.
func TestBlueprintProposeTwinRefusesInvalidWithTheReportAndFilesNothing(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "bp-twin")
	before, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	broken := "id: broken-twin\nschemaVersion: 1\nversion: 1\nlanes: []\n"
	params, err := json.Marshal(map[string]any{"name": "broken-twin", "content": broken, "create_only": true, "note": "worth a look"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.proposeViaTwin("bp-twin", string(params))
	if err == nil {
		t.Fatal("an invalid candidate passed the twin")
	}
	// The report travels VERBATIM — the save door's own JSON, the state it
	// reached, the diagnostics inside it — rendered through the params
	// refusal the records seam carries (the same sentence the tool door
	// answers with; a paraphrase here would be a second validator with a
	// second opinion, TD3).
	if !strings.Contains(err.Error(), `"state":"invalid"`) || !strings.Contains(err.Error(), "diagnostics") {
		t.Errorf("the refusal is not the save door's report: %v", err)
	}
	after, err := e.st.ListProposals("")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("the refused proposal still created a row (%d → %d)", len(before), len(after))
	}
}

// TestBlueprintProposeTwinFilesAnInertRow: the twin answers the SAME
// PolicyProposeResult the tool answers (one machinery, one inertness
// sentence), the payload is the desk's camel shape carrying the WHOLE
// source, and the standing reads do not move — a proposal sitting in the
// desk changes no blueprint read (BP9/TD2; the commit-side proof is T9's
// TestBlueprintProposalsAreInert over this same env).
func TestBlueprintProposeTwinFilesAnInertRow(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "bp-twin-inert")
	content := strings.Replace(bpProposalYAML, "id: bp-proposed", "id: bp-twin-new", 1)
	readBefore, err := e.d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, _ := json.Marshal(readBefore)
	params, err := json.Marshal(map[string]any{"name": "bp-twin-new", "content": content, "create_only": true, "note": "whole source, decided by a person"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.proposeViaTwin("bp-twin-inert", string(params))
	if err != nil {
		t.Fatalf("the twin refused a valid proposal: %v", err)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("inertness note drifted: %q", res.Note)
	}
	if res.Proposal.Kind != "blueprint_save" || res.Proposal.Status != "proposed" {
		t.Errorf("row is %s/%s, want blueprint_save/proposed", res.Proposal.Kind, res.Proposal.Status)
	}
	if res.Proposal.CreatedByKind != "user" {
		t.Errorf("the socket author is %s, want the peer (user) — identity is stamped, never typed", res.Proposal.CreatedByKind)
	}
	var payload struct {
		Name       string `json:"name"`
		Content    string `json:"content"`
		CreateOnly bool   `json:"createOnly"`
		Note       string `json:"note"`
	}
	if err := json.Unmarshal(res.Proposal.Payload, &payload); err != nil {
		t.Fatalf("stored payload will not parse: %v", err)
	}
	if payload.Name != "bp-twin-new" || payload.Content != content || !payload.CreateOnly || payload.Note == "" {
		t.Errorf("stored payload drifted: %+v", payload)
	}
	readAfter, err := e.d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, _ := json.Marshal(readAfter)
	if string(beforeBytes) != string(afterBytes) {
		t.Fatal("the read service moved while a proposal sat in the desk")
	}
	if _, found, _ := e.st.BlueprintGet(context.Background(), "bp-twin-new"); found {
		t.Fatal("the twin WROTE a blueprint row — the door files a proposal, not a blueprint")
	}
	// The command is copyable, which is the whole point of proposing from
	// a step: a person can decide it without leaving the row's own answer.
	if res.Proposal.Command != protocol.ProposalCommand(res.Proposal.ID) {
		t.Errorf("command %q is not the copyable commit form", res.Proposal.Command)
	}
}

// TestBlueprintProposeTwinCarriesNoIdentityField: rule 1 at the socket —
// the twin's params have nowhere to put an actor, a status or a rev, and
// one that arrives meets the service's strict decode as a refusal, not a
// silently dropped field.
func TestBlueprintProposeTwinCarriesNoIdentityField(t *testing.T) {
	e := newBPProposalEnv(t)
	bpInitialize(t, e.d, "bp-twin-id")
	for _, smuggle := range []string{`"actor":{"kind":"user","id":"root"}`, `"rev":9`, `"status":"applied"`, `"kind":"blueprint_save"`, `"origin":"system"`} {
		content, merr := json.Marshal(strings.Replace(bpProposalYAML, "id: bp-proposed", "id: bp-smuggle", 1))
		if merr != nil {
			t.Fatal(merr)
		}
		params := `{"name":"bp-smuggle","content":` + string(content) + `,` + smuggle + `}`
		if _, err := e.proposeViaTwin("bp-twin-id", params); err == nil {
			t.Errorf("smuggled %s was accepted by the twin", smuggle)
		}
	}
}
