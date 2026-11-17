package runtime

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T1: the loop is where assistant turns are BORN (loop.go's assembly
// point), and the stamp must be applied there, off the very Route the call
// went out on (O4). The store gate refuses an unstamped AppendMessages, but
// the loop checkpoints rather than appends mid-step — a forgotten stamp here
// would therefore be silent, which is why it gets its own test rather than
// riding on the store's.

func TestTheLoopStampsTheAssistantTurnWithItsRoute(t *testing.T) {
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	st := testState(testHandler(), nil)

	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools(), HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q, reason %q", res.Outcome, res.Reason)
	}

	var last *model.Message
	for i := range st.Messages {
		if st.Messages[i].Role == model.RoleAssistant {
			last = &st.Messages[i]
		}
	}
	if last == nil {
		t.Fatal("the loop produced no assistant turn to stamp")
	}
	if last.Provenance == nil {
		t.Fatal("the loop assembled an assistant turn without provenance (M26) — the " +
			"checkpoint would write it silently and M27's replay rule would later run on nils")
	}
	want := model.NewProvenance(st.Request.Route)
	if *last.Provenance != want {
		t.Errorf("the stamp is %+v, want the Route the call went out on %+v (O4 names the "+
			"route too: two routes can share provider+model)", *last.Provenance, want)
	}
}
