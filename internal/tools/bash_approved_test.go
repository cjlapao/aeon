package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/stepctx"
)

// bash only takes an ask-tier verdict as permission for the ONE call a person approved: the exact
// command, once, in the context the runtime marked. A deny is never softened.

func verdictSeam(v policy.Verdict) func(string) (policy.Verdict, bool) {
	return func(string) (policy.Verdict, bool) { return v, true }
}

func runWith(t *testing.T, v policy.Verdict, ctx context.Context, command string) (*spyExecutor, Result) {
	t.Helper()
	spy := &spyExecutor{}
	set, err := BuildToolset([]string{"bash"}, ToolContext{
		WorkspaceRoot: t.TempDir(), CommandVerdict: verdictSeam(v),
		Exec: func(context.Context) (CommandExecutor, error) { return spy, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"command": command})
	return spy, set.Call(ctx, "bash", b)
}

func TestAnAskedCommandRunsOnlyForTheApprovedCall(t *testing.T) {
	asked := policy.Verdict{Asked: true, DefaultAsk: true}
	spy, res := runWith(t, asked, context.Background(), "git push")
	if spy.calls != 0 || !res.IsError {
		t.Fatalf("an unapproved ask ran: calls=%d res=%+v", spy.calls, res)
	}
	spy, _ = runWith(t, asked, stepctx.WithApprovedCall(context.Background(), "bash", "git push"), "git push")
	if spy.calls != 1 {
		t.Fatalf("the approved command did not run: calls=%d", spy.calls)
	}
	spy, res = runWith(t, asked, stepctx.WithApprovedCall(context.Background(), "bash", "git push"), "git push --force")
	if spy.calls != 0 || !res.IsError {
		t.Fatalf("a different command borrowed the approval: calls=%d", spy.calls)
	}
}

func TestAnApprovalNeverSoftensADeny(t *testing.T) {
	denied := policy.Verdict{} // neither allowed nor asked: a refusal
	spy, res := runWith(t, denied, stepctx.WithApprovedCall(context.Background(), "bash", "rm -rf x"), "rm -rf x")
	if spy.calls != 0 || !res.IsError {
		t.Fatalf("an approval ran a denied command: calls=%d", spy.calls)
	}
}
