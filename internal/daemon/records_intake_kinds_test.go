package daemon

// records_intake_kinds_test.go — S61 T5: the per-agent cap carried onto the
// two new kinds (GI9). The cap is ONE counter over the caller's open rows,
// kind-blind by construction (records_policy.go counts created_by_* alone)
// — which is exactly why this test mixes kinds in one agent's ledger: 19
// charter_save asks plus 1 intake_plan ask already hold the cap, and the
// refusal is a COUNTED refusal naming the number and the read that lists
// what is open, never silence. Decide one row and the counter opens.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

func TestProposalLimitApplies(t *testing.T) {
	e := newRecordsEnv(t, "")
	// The cap's counter is the service the daemon wires (recordsSvc.proposals
	// is the SAME store the socket doors answer through — daemon.go); a bare
	// NewRecordsService has no proposal store and refuses at the door, not
	// counts. Build the daemon the records arms live in.
	d := New(Options{Store: e.s})
	initConn(t, d, e.ctx, "c-cap", "test")
	agent := Actor{Kind: "agent", ID: e.agent}
	by := store.Actor{Kind: "agent", ID: e.agent}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	seed := func(kind, payload string) int64 {
		t.Helper()
		row, err := e.s.CreateProposal(kind, payload, by, ts)
		if err != nil {
			t.Fatalf("seed %s: %v", kind, err)
		}
		return row.ID
	}
	// 19 charter_save asks: under the cap on their own.
	for i := 0; i < desk.MaxOpenProposalsPerAgent-1; i++ {
		seed("charter_save", fmt.Sprintf(`{"rootId":"/srv/intake-%d","body":{"goal":"filler %d"},"reason":"cap filler %d"}`, i, i, i))
	}
	if f := d.recordsSvc.openProposalsCap(agent); f != nil {
		t.Fatalf("the %dth open row was already refused: %s", desk.MaxOpenProposalsPerAgent-1, f.Sentence)
	}
	// The 20th ask is an intake_plan: a DIFFERENT kind, the SAME ledger.
	mixed := seed("intake_plan", `{"intakeId":"intake-filler","charter":{"goal":"filler 20"},"components":[{"name":"A","slug":"a","template":"go-service","role":"standalone"}],"reason":"cap filler 20"}`)
	f := d.recordsSvc.openProposalsCap(agent)
	if f == nil {
		t.Fatalf("the cap did not apply across kinds — %d rows held, the counter looked the other way (S45's shared rule, GI9)", desk.MaxOpenProposalsPerAgent)
	}
	if !strings.Contains(f.Sentence, fmt.Sprint(desk.MaxOpenProposalsPerAgent)) || !strings.Contains(f.Sentence, "proposal_list") {
		t.Errorf("the cap refusal does not name the number or the read: %s", f.Sentence)
	}
	if n := countProposals(t, e.s, "charter_save"); n != desk.MaxOpenProposalsPerAgent-1 {
		t.Errorf("%d charter_save rows stored, want %d — a counted refusal stores nothing new", n, desk.MaxOpenProposalsPerAgent-1)
	}
	// A person decides one: the counter opens for the same agent.
	if err := e.s.DecideProposal(mixed, "rejected", store.Actor{Kind: "user", ID: "person"}, "cap test", ts); err != nil {
		t.Fatal(err)
	}
	if f := d.recordsSvc.openProposalsCap(agent); f != nil {
		t.Errorf("the cap stayed shut after a decision: %s", f.Sentence)
	}
	// And the cap is the CALLER's: another agent is untouched by this ledger.
	if f := d.recordsSvc.openProposalsCap(Actor{Kind: "agent", ID: "someone-else"}); f != nil {
		t.Errorf("the cap crossed agents: %s", f.Sentence)
	}
}
