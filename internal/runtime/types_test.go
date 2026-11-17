package runtime

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// T1's tests: the union's shape, and the limits file.

func TestStepResultNarrowsToCompleteOrSuspended(t *testing.T) {
	// Two kinds and no third. A result with no Kind switches to neither
	// branch, which is why `Complete` is a constructor.
	if got := Complete(OutcomeOK, nil); got.Kind != ResultComplete {
		t.Errorf("Complete produced kind %q", got.Kind)
	}
	for _, kind := range []ResultKind{ResultComplete, ResultSuspended} {
		if kind == "" {
			t.Error("a result kind is empty")
		}
	}
}

func TestThereIsNoFifthOutcome(t *testing.T) {
	// The four describe WORK. Suspension describes a PAUSE, and adding it here
	// would make `blocked` ambiguous: a caller could no longer tell "ran out of
	// iterations" from "waiting for a human", and those need opposite
	// responses.
	outcomes := Outcomes()
	if len(outcomes) != 4 {
		t.Fatalf("there are %d outcomes: %v", len(outcomes), outcomes)
	}
	want := map[Outcome]bool{OutcomeOK: true, OutcomeReject: true, OutcomeBlocked: true, OutcomeFailed: true}
	for _, o := range outcomes {
		if !want[o] {
			t.Errorf("%q is not one of the four", o)
		}
		delete(want, o)
	}
	for missing := range want {
		t.Errorf("%q is missing from Outcomes()", missing)
	}

	// And `suspended` is NOT among them, which is the specific confusion this
	// test exists to prevent.
	for _, o := range outcomes {
		if string(o) == string(ResultSuspended) {
			t.Error("`suspended` is an outcome; it is a RESULT KIND, because it describes a pause " +
				"rather than work")
		}
	}
}

func TestOnlyTheSharedLoopProducesSuspended(t *testing.T) {
	// S6 adds one suspension path to the shared loop. Another constructor
	// could bypass sibling tool results or iteration accounting.
	assign := regexp.MustCompile(`Kind:\s*ResultSuspended|\.Kind\s*=\s*ResultSuspended`)
	count := 0
	for name, src := range packageSources(t) {
		if name == "types.go" {
			continue // where the constant is declared
		}
		matches := assign.FindAllString(src, -1)
		count += len(matches)
		if len(matches) > 0 && name != "loop.go" {
			t.Errorf("%s sets a result to suspended outside the shared loop", name)
		}
	}
	if count != 1 {
		t.Errorf("found %d suspended result paths, want one", count)
	}
}

func TestDeadlineAtIsAnAbsoluteTimeNotADuration(t *testing.T) {
	// A relative deadline cannot survive a suspension that lasts overnight: it
	// would grant the resumed step its whole budget again, and a step could
	// escape its bounds by suspending repeatedly.
	//
	// Asserted on the TYPE, because a `time.Duration` field would round-trip
	// through every test above without complaint.
	fixed := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	res := AssembleContext(context.Background(),
		[]SectionSpec{{Title: "Task", Text: "x", Category: CategorySystemPrompt, Destination: SectionTranscript}},
		ContextDeps{Clock: func() time.Time { return fixed }},
	)
	if !res.OK {
		t.Fatal(res.Reason)
	}
	want := fixed.Add(DefaultDeadline)
	if !res.Manifest.DeadlineAt.Equal(want) {
		t.Errorf("DeadlineAt is %s, want %s — it must be an instant computed once, not a remaining "+
			"duration", res.Manifest.DeadlineAt, want)
	}
	// It must be in the FUTURE relative to the clock, not a zero value.
	if !res.Manifest.DeadlineAt.After(fixed) {
		t.Error("DeadlineAt is not after the clock")
	}
}

func TestTheEventVocabularyKeepsTheInheritedNames(t *testing.T) {
	// S9 was specced against these names. Extending is additive; RENAMING
	// would mean amending reviewed work for no gain.
	kinds := EventKinds()
	for _, inherited := range []string{"step.started", "model.delta", "step.done"} {
		found := false
		for _, k := range kinds {
			if k == inherited {
				found = true
			}
		}
		if !found {
			t.Errorf("the inherited event name %q is gone; S9 was specced against it", inherited)
		}
	}
	// And S5's two additions, without which a tool call is invisible in the
	// stream and a step reading files looks like a step that hung.
	for _, added := range []string{"step.tool.start", "step.tool.end"} {
		found := false
		for _, k := range kinds {
			if k == added {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is missing from the vocabulary", added)
		}
	}
}

func TestEveryLimitIsInTheLimitsFileAndLabelled(t *testing.T) {
	// O4 / I6. Scattered constants become folklore: a `12` in a loop condition
	// is a number nobody can revisit, because nobody can find where it came
	// from.
	limits := Limits()
	if len(limits) < 4 {
		t.Fatalf("only %d limits are declared", len(limits))
	}
	for _, l := range limits {
		if l.Provenance == "" {
			t.Errorf("%s has no provenance; the QA suite is written to CHANGE these numbers, and it "+
				"cannot argue with a number whose origin is unknown", l.Name)
		}
		if l.Value <= 0 {
			t.Errorf("%s is %d", l.Name, l.Value)
		}
	}

	// Most are guesses, and the file says so. A file where nothing is a guess
	// is a file somebody has stopped being honest in.
	guesses := 0
	for _, l := range limits {
		if l.Guess {
			guesses++
		}
	}
	if guesses == 0 {
		t.Error("no limit is labelled a guess; all but one of these numbers were derived from prose")
	}
}

func TestTheLimitsAreNotDuplicatedAsLiteralsElsewhere(t *testing.T) {
	// The other half of I6: a number collected here and ALSO written inline
	// somewhere is two numbers, and only one gets changed.
	//
	// It checks the two that would actually be retyped — the iteration bound
	// and the retry count — rather than every integer in the package, because
	// a test that flags `0` and `1` everywhere is a test somebody switches off.
	for name, src := range packageSources(t) {
		if name == "limits.go" {
			continue
		}
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// `MaxIterations` compared against a literal is the shape being
			// looked for: `IterationsUsed >= 12`.
			if strings.Contains(line, "IterationsUsed") && regexp.MustCompile(`>=?\s*\d+`).MatchString(line) {
				t.Errorf("%s compares iterations against a literal:\n    %s\n  Use MaxIterations from "+
					"limits.go", name, trimmed)
			}
		}
	}
}

func TestADiscardSinkAcceptsEverything(t *testing.T) {
	// The default, so a caller that does not care passes nothing rather than
	// nil — a nil sink would need a check at every emit, and the one that got
	// missed would panic mid-step.
	var sink EventSink = DiscardSink{}
	for _, kind := range EventKinds() {
		sink.Emit(StepEvent{Kind: kind})
	}
}
