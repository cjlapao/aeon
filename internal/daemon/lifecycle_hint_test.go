package daemon

// S58 T4 — the sweep survives the switch it replaced. T1's
// TestNotifyHintForIsExactlyTheSevenCuratedKinds walked every kind
// protocol.Kinds() declares against the live notifyHintFor; T4 deleted
// that function (SV5: one resolution point, the resolver, inside
// publish), so the sweep walks the RESOLVER over a real store holding
// the seeded notify.events set. The promise is unchanged and stronger:
// every curated hint survives BYTE-IDENTICAL on title and tone (the
// design's P2 line: no hint changes its bytes on the way through the
// table), question.asked is the one kind that GAINS a hint (SV7), and
// the switch's absence is itself asserted — a revived fallback would be
// the second truth this slice pins shut (P2).

import (
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestResolverSeededRowsEqualTheOldSwitch(t *testing.T) {
	d, _, _ := newNotifyClassDaemon(t)
	want := map[string]protocol.NotifyHint{
		protocol.KindLoopFinished:      {Title: "Loop finished", Tone: protocol.NotifySuccess},
		protocol.KindLoopBlocked:       {Title: "Loop blocked", Tone: protocol.NotifyWarning},
		protocol.KindLoopWaiting:       {Title: "Loop waiting on you", Tone: protocol.NotifyInfo},
		protocol.KindLoopStopRequested: {Title: "An agent asked you to stop a loop", Tone: protocol.NotifyWarning},
		protocol.KindWatchdogFired:     {Title: "The watchdog needs your attention", Tone: protocol.NotifyWarning},
		protocol.KindBudgetWarn:        {Title: "A budget is near its cap", Tone: protocol.NotifyWarning},
		protocol.KindBudgetHit:         {Title: "A budget cap is exhausted", Tone: protocol.NotifyWarning},
		// The one NEW hint T4 adds (SV7: the question's own event, the
		// same subject as loop.waiting — consumers dedup by attentionKey).
		protocol.KindQuestionAsked: {Title: "A question is waiting for you", Tone: protocol.NotifyInfo},
	}
	// The whole closed kind plane, resolved with no loop behind it: the
	// byte comparison is what T1's sweep was, on the new machinery.
	for _, kind := range protocol.Kinds() {
		got := d.classes.HintFor(kind, map[string]any{})
		w, curated := want[kind]
		switch {
		case !curated && got != nil:
			t.Errorf("kind %s resolves hint %q but is not a shipped class — the table grew a row nobody reviewed", kind, got.Title)
		case curated && got == nil:
			t.Errorf("class %s resolves nil — a curated hint went silent through the resolver (P2)", kind)
		case curated:
			if got.Title != w.Title || got.Body != w.Body || got.Tone != w.Tone {
				t.Errorf("hint for %s drifted from the switch's bytes: got %+v want %+v", kind, *got, w)
			}
		}
	}
	if h := d.classes.HintFor("loop.definitely.not.a.kind", map[string]any{}); h != nil {
		t.Errorf("an unknown kind must resolve nil, got %+v", *h)
	}

	// The variant plane: every reason kind the emitters use resolves
	// (the seeded variant row OR the base row) to the base bytes — the
	// variants exist so an operator CAN differentiate, and T4 changed
	// no bytes either way (T0's measured variants, both payload shapes).
	for _, vk := range []string{"action-denied", "step-blocked", "exhaustion", "model-error", "runner-error", "cap-hit", "something-nobody-ships"} {
		got := d.classes.HintFor(protocol.KindLoopBlocked, map[string]any{"reason": map[string]any{"kind": vk}})
		if got == nil || got.Title != "Loop blocked" || got.Tone != protocol.NotifyWarning {
			t.Errorf("blocked/%s resolved %+v, want the base bytes", vk, got)
		}
	}
	if got := d.classes.HintFor(protocol.KindLoopWaiting, map[string]any{"reason": "stopped"}); got == nil || got.Title != "Loop waiting on you" {
		t.Errorf("waiting/stopped resolved %+v, want the base bytes", got)
	}
}

// TestNotifyHintForIsGone is the one-truth assertion (P2/SV5): the
// function is not hiding in the package. A grep is honest here because
// a call needs the identifier — the deleted symbol, in call shape, must
// appear in no non-test daemon source. (Comments may still name it to
// say it is gone; the paren is what a call looks like.)
func TestNotifyHintForIsGone(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "notifyHintFor(") {
			t.Errorf("%s still calls notifyHintFor — the resolver is the only source of hints (SV5)", name)
		}
	}
}
