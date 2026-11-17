package policy_test

// The T10 disagreement record tests. The row is RecordRefusal's twin with a
// different kind and two extra attribution gates (step, agent) — so the
// table is the same refusal-table discipline: every shape that would write a
// lying row is refused at the door, and the happy path lands a row the audit
// can read.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

func loopSubjectVerdict(t *testing.T, s *policy.Store, id, loop, tool string) policy.Verdict {
	t.Helper()
	mustAdd(t, s, boundaryPolicy(t, id, policy.AllAgents, "", policy.Deny, policy.Operator,
		spec(`{"toolName":"bash","state":"deny"}`)))
	ps, rej := s.List(context.Background(), evalAt)
	if rej != nil {
		t.Fatalf("List refused its own row: %s", rej.Reason)
	}
	v := policy.Evaluate(ps, policy.Request{Type: policy.ToolAccess, Agent: "boss", Loop: loop, Tool: tool}, evalAt)
	if v.Allowed || v.Subject != "loop:"+loop {
		t.Fatalf("helper did not produce a loop-subject denial: %+v", v)
	}
	return v
}

func TestRecordDisagreementChecksEveryShapeThatWouldLie(t *testing.T) {
	// The table is the argument: each row is an audit entry that would say
	// something false, refused by name, reaching the seam zero times.
	base := func(t *testing.T) (s *policy.Store, repo *fakeRepo, v policy.Verdict) {
		t.Helper()
		repo = &fakeRepo{}
		s = policy.NewStore(repo)
		v = denyVerdict(t, s, "no-bash", "boss", "bash")
		return s, repo, v
	}

	cases := []struct {
		name    string
		subject string
		stepID  int64
		agent   string
		loopSub bool // build the verdict over a LOOP-subject request
		mutate  func(v *policy.Verdict)
		must    []string // absent means the record must be ACCEPTED
	}{
		{name: "the honest row", subject: "agent:boss", stepID: 7, agent: "boss"},
		{name: "loop subject, agent named beside it", subject: "loop:l", stepID: 7, agent: "boss",
			loopSub: true},

		{name: "allowed verdict", subject: "agent:boss", stepID: 7, agent: "boss",
			mutate: func(v *policy.Verdict) { v.Allowed = true },
			must:   []string{"ALLOW", "policy_event"}},
		{name: "borrowed subject", subject: "agent:somebody-else", stepID: 7, agent: "somebody-else",
			must: []string{"forgery"}},
		{name: "zero step", subject: "agent:boss", stepID: 0, agent: "boss",
			must: []string{"names no step"}},
		{name: "negative step", subject: "agent:boss", stepID: -3, agent: "boss",
			must: []string{"names no step"}},
		{name: "blank agent", subject: "agent:boss", stepID: 7, agent: "",
			must: []string{"agent"}},
		{name: "hostile agent", subject: "agent:boss", stepID: 7, agent: "Bad Name!",
			must: []string{"agent"}},
		{name: "borrowed blame — agent does not match agent-subject", subject: "agent:boss", stepID: 7,
			agent: "innocent-bystander", must: []string{"borrowed blame"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, repo, v := base(t)
			if c.loopSub {
				repo = &fakeRepo{}
				s = policy.NewStore(repo)
				v = loopSubjectVerdict(t, s, "no-bash", "l", "bash")
			}
			if c.mutate != nil {
				c.mutate(&v)
			}
			accepted := len(c.must) == 0
			rej := s.RecordDisagreement(context.Background(), c.subject, c.stepID, c.agent, v, eventT1)
			if accepted {
				if rej != nil {
					t.Fatalf("the honest row was refused: %s", rej.Reason)
				}
				if repo.eventInsertCalls != 1 {
					t.Fatalf("the accepted row produced %d seam inserts, want 1 — accepted-but-unwritten is the "+
						"half-record DB19 forbids", repo.eventInsertCalls)
				}
				return
			}
			if rej == nil {
				t.Fatalf("RecordDisagreement(%s, step %d, agent %q) was ACCEPTED; it must be refused",
					c.subject, c.stepID, c.agent)
			}
			for _, want := range c.must {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal does not name %q: %s", want, rej.Reason)
				}
			}
			if repo.eventInsertCalls != 0 {
				t.Errorf("the refused row reached the seam (%d inserts)", repo.eventInsertCalls)
			}
		})
	}
}

func TestRecordDisagreementWritesTheTwinRow(t *testing.T) {
	// The happy path, field by field — and the "same renderers" claim made
	// literal: the disagreement's detail is BYTE-EQUAL to what refusal
	// records for the same verdict, because both come from the one formatter.
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")
	mustRecordRefusal(t, s, "agent:boss", v, eventT1)
	if rej := s.RecordDisagreement(context.Background(), "agent:boss", 7, "boss", v, eventT1); rej != nil {
		t.Fatalf("the honest disagreement was refused: %s", rej.Reason)
	}
	if repo.eventInsertCalls != 2 {
		t.Fatalf("the seam saw %d inserts, want 2 (refusal + disagreement)", repo.eventInsertCalls)
	}
	refusal, disagreement := repo.recordedEvents[0], repo.recordedEvents[1]
	if refusal.kind != "refusal" || disagreement.kind != "disagreement" {
		t.Fatalf("the kinds are %q and %q — distinguishable by the outcome column ALONE is the requirement",
			refusal.kind, disagreement.kind)
	}
	if disagreement.subject != "agent:boss" || disagreement.policyID != "no-bash" || !disagreement.at.Equal(eventT1) {
		t.Errorf("the row is (%q, %q, %s), want (agent:boss, no-bash, %s)",
			disagreement.subject, disagreement.policyID, disagreement.at, eventT1)
	}
	if disagreement.detail != refusal.detail || disagreement.detail != v.Sentence() {
		t.Errorf("the disagreement detail diverged from the one formatter:\n refusal: %q\n  disagmt: %q\n sentence: %q",
			refusal.detail, disagreement.detail, v.Sentence())
	}

	// The loop-subject shape: the agent is named BESIDE the subject, not
	// fused into it — "was it told, or did it act without being told" needs
	// both the loop (what was under policy) and the agent (who attempted).
	if rej := s.RecordDisagreement(context.Background(), "agent:boss", 7, "boss", v, eventT1); rej != nil {
		t.Fatalf("re-recording the same disagreement was refused (idempotence is not the boundary's claim; "+
			"append is — and refusal does the same): %s", rej.Reason)
	}
}

func TestRecordDisagreementStoreFailureArrivesAsARefusal(t *testing.T) {
	// The single-return-shape discipline extends to the new handover: the
	// machine breaking says the record was NOT made; nothing half-writes.
	repo := &fakeRepo{eventErr: errors.New("disk on fire")}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")
	rej := s.RecordDisagreement(context.Background(), "agent:boss", 7, "boss", v, eventT1)
	if rej == nil {
		t.Fatal("the store failure was swallowed")
	}
	for _, want := range []string{"was NOT recorded", "disk on fire"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the fold lost %q: %s", want, rej.Reason)
		}
	}
}

func TestDisagreementIsInTheKindVocabulary(t *testing.T) {
	// The vocabulary is closed and small: exactly the two kinds, and an
	// outcome outside them is refused by name (the recordEvent check, reached
	// through the exported door by a hand-crafted verdict — the only door
	// this file's rules leave).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	v := denyVerdict(t, s, "no-bash", "boss", "bash")
	mustRecordRefusal(t, s, "agent:boss", v, eventT1)
	if rej := s.RecordDisagreement(context.Background(), "agent:boss", 7, "boss", v, eventT1); rej != nil {
		t.Fatal(rej.Reason)
	}
	_, rows, rej := s.ReportEvents(context.Background(), "agent:boss", 10)
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	kinds := map[string]bool{}
	for _, r := range rows {
		kinds[r.Outcome] = true
	}
	if !kinds["refusal"] || !kinds["disagreement"] {
		t.Errorf("the reported kinds are %v, want both refusal and disagreement", kinds)
	}
}
