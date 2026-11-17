package loop

import "testing"

func TestABlockedStepWithNoReasonStillSaysSomething(t *testing.T) {
	if got := reasonOrNone("blocked", ""); got == "" {
		t.Fatal("a loop blocked by a step must carry a sentence")
	}
	if got := reasonOrNone("blocked", "why"); got != "why" {
		t.Fatalf("a given reason is kept, got %q", got)
	}
	for _, o := range []string{"ok", "reject"} {
		if got := reasonOrNone(o, ""); got != "" {
			t.Fatalf("%s keeps an absent reason absent, got %q", o, got)
		}
	}
}
