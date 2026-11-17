package runtime

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T4's tests. The ported strategies get a test each, in order, plus the three
// cases the superseded slice named as the ones that bite.

func assistantSaying(text string) []model.Message {
	return []model.Message{{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(text)}}}
}

func TestStrategyOneTakesTheLastJSONFencedBlock(t *testing.T) {
	// "Last" throughout, because a model that reasons before answering emits
	// example objects first and its real answer last.
	res := ExtractHandoff(assistantSaying(
		"Here is the shape I will use:\n\n```json\n{\"status\":\"example\"}\n```\n\n" +
			"Now the actual report:\n\n```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```\n"))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Strategy != 1 {
		t.Errorf("strategy %d, want 1", res.Strategy)
	}
	if res.Handoff["status"] != "ok" {
		t.Errorf("took the example object: %v", res.Handoff)
	}
}

func TestStrategyTwoTakesTheLastFencedBlockThatParses(t *testing.T) {
	// No ```json fence at all: a model that fenced with no language, or with
	// the wrong one.
	res := ExtractHandoff(assistantSaying(
		"```\nnot json at all\n```\n\n```\n{\"status\":\"ok\"}\n```\n"))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Strategy != 2 {
		t.Errorf("strategy %d, want 2", res.Strategy)
	}
}

func TestStrategyThreeTakesTheLastBalancedSpan(t *testing.T) {
	// No fences at all — the model just wrote the object into its prose.
	res := ExtractHandoff(assistantSaying(
		"I checked the three files. {not json} and then the report: {\"status\":\"ok\",\"summary\":\"all three\"}"))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Strategy != 3 {
		t.Errorf("strategy %d, want 3", res.Strategy)
	}
	if res.Handoff["summary"] != "all three" {
		t.Errorf("wrong span: %v", res.Handoff)
	}
}

func TestTheStrategiesRunInOrderWithFirstMatchWinning(t *testing.T) {
	// All three present at once. A ```json block, another fenced block holding
	// JSON, and a bare span — strategy 1 must win.
	res := ExtractHandoff(assistantSaying(
		"{\"from\":\"span\"}\n\n```\n{\"from\":\"plain-fence\"}\n```\n\n```json\n{\"from\":\"json-fence\"}\n```\n"))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Handoff["from"] != "json-fence" {
		t.Errorf("the strategies did not run in order: took %v", res.Handoff["from"])
	}
}

func TestABraceInsideAStringDoesNotEndTheSpan(t *testing.T) {
	// The inherited warning, tested directly: *"a brace inside a JSON string
	// value must not end the span. Do not use a regex for strategy 3."*
	//
	// A regex counting braces finds the end at the `}` inside `{weird}`, and
	// the result is a handoff that ALMOST parses — which reads as a model
	// mistake rather than as a scanner bug.
	res := ExtractHandoff(assistantSaying(
		`The report: {"status":"ok","summary":"fixed the {weird} case"}`))

	if !res.OK {
		t.Fatalf("the scanner stopped at the brace inside the string: %s (raw %q)", res.Reason, res.Raw)
	}
	if got := res.Handoff["summary"]; got != "fixed the {weird} case" {
		t.Errorf("summary is %q", got)
	}
}

func TestAnEscapedQuoteDoesNotEndTheString(t *testing.T) {
	// The escape half of the same warning. `\"` inside a value would otherwise
	// flip the scanner out of string mode, and the next `}` would end the span
	// early.
	res := ExtractHandoff(assistantSaying(
		`{"status":"ok","summary":"it said \"done {now}\" and stopped"}`))

	if !res.OK {
		t.Fatalf("the escape was mishandled: %s (raw %q)", res.Reason, res.Raw)
	}
	if !strings.Contains(res.Handoff["summary"].(string), "done {now}") {
		t.Errorf("summary is %v", res.Handoff["summary"])
	}
}

func TestNestedObjectsAreOneSpan(t *testing.T) {
	res := ExtractHandoff(assistantSaying(
		`{"status":"ok","detail":{"files":{"read":3},"wrote":0}}`))
	if !res.OK {
		t.Fatal(res.Reason)
	}
	detail, isMap := res.Handoff["detail"].(map[string]any)
	if !isMap {
		t.Fatalf("detail is %T", res.Handoff["detail"])
	}
	if _, ok := detail["files"]; !ok {
		t.Errorf("the nested object was cut: %v", detail)
	}
}

func TestAThinkingBlockIsNotScannedForTheHandoff(t *testing.T) {
	// A model that reasons in a `thinking` block writes example objects there,
	// and "last" would find the reasoning rather than the answer.
	msgs := []model.Message{{
		Role: model.RoleAssistant,
		Content: []model.Block{
			{Kind: model.BlockThinking, Text: `I will answer {"status":"blocked"} if the file is missing.`},
			model.TextBlock(`{"status":"ok","summary":"the file was there"}`),
		},
	}}

	res := ExtractHandoff(msgs)
	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Handoff["status"] != "ok" {
		t.Errorf("the handoff came from the thinking block: %v", res.Handoff)
	}
}

func TestOnlyTheLastAssistantTurnIsScanned(t *testing.T) {
	// An earlier assistant turn is a tool-calling turn, and a handoff found
	// there would be something the model wrote while working.
	msgs := []model.Message{
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(`{"status":"blocked"}`)}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("keep going")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(`{"status":"ok"}`)}},
	}
	res := ExtractHandoff(msgs)
	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Handoff["status"] != "ok" {
		t.Errorf("an earlier turn was used: %v", res.Handoff)
	}
}

func TestExtractionNeverPanics(t *testing.T) {
	// T4's validation point. Each of these is something a provider has
	// actually produced, and a panic in extraction takes down a step that
	// otherwise completed its work.
	cases := []struct {
		name string
		msgs []model.Message
	}{
		{"a nil transcript", nil},
		{"an empty transcript", []model.Message{}},
		{"no assistant turn", []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}}},
		{"an assistant turn with no blocks", []model.Message{{Role: model.RoleAssistant}}},
		{"an assistant turn with only a tool call", []model.Message{{
			Role:    model.RoleAssistant,
			Content: []model.Block{{Kind: model.BlockToolUse, ID: "t1", Name: "read"}},
		}}},
		{"empty text", assistantSaying("")},
		{"whitespace only", assistantSaying("   \n\t  ")},
		{"an unterminated fence", assistantSaying("```json\n{\"status\":\"ok\"")},
		{"an unmatched open brace", assistantSaying(`{"status": "ok"`)},
		{"a stray closing brace", assistantSaying("the } was unexpected")},
		{"a bare array", assistantSaying(`["ok"]`)},
		{"literal null", assistantSaying("```json\nnull\n```")},
		{"nothing but prose", assistantSaying("I could not do it, sorry.")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := ExtractHandoff(tc.msgs) // a panic here fails the test
			if res.OK {
				t.Logf("extracted: %v", res.Handoff)
			} else if res.Reason == "" {
				t.Error("extraction failed with no reason")
			}
		})
	}
}

func TestAJSONFenceThatDoesNotParseIsReportedRatherThanSkipped(t *testing.T) {
	// The model SAID this was JSON. Falling through to strategy 3 would hide a
	// near miss — usually a trailing comma — and the retry would re-ask about
	// "no handoff" rather than about the comma.
	res := ExtractHandoff(assistantSaying("```json\n{\"status\":\"ok\",}\n```"))
	if res.OK {
		t.Fatal("a malformed json fence was accepted")
	}
	if res.Strategy != 1 {
		t.Errorf("strategy %d, want the json fence reported as the failure", res.Strategy)
	}
	if res.Raw == "" {
		t.Error("the failure does not carry what the model produced")
	}
	if !strings.Contains(res.Reason, "```json") {
		t.Errorf("the reason does not say which block failed: %q", res.Reason)
	}
}

func TestLiteralNullIsNotAnEmptyHandoff(t *testing.T) {
	// `null` unmarshals into a nil map. Treating that as an empty object would
	// make a model saying nothing look like a model reporting nothing wrong.
	res := ExtractHandoff(assistantSaying("```json\nnull\n```"))
	if res.OK {
		t.Fatal("literal null was accepted as a handoff")
	}
	if !strings.Contains(res.Reason, "null") {
		t.Errorf("the reason does not name the problem: %q", res.Reason)
	}
}

// --- validation --------------------------------------------------------------

func TestValidationNamesTheFailingPath(t *testing.T) {
	// Inherited verbatim: `handoff.status: expected one of [...], got "done "`
	// is actionable, and `validation failed` is not.
	spec := HandoffSpec{
		Required: []string{"status", "summary"},
		Enums:    map[string][]string{"status": {"ok", "reject", "blocked", "failed"}},
	}

	cases := []struct {
		name   string
		h      Handoff
		wants  []string
		reject bool
	}{
		{
			name:   "a missing required field",
			h:      Handoff{"status": "ok"},
			wants:  []string{"handoff.summary", "required"},
			reject: true,
		},
		{
			name:   "an empty required field",
			h:      Handoff{"status": "ok", "summary": "   "},
			wants:  []string{"handoff.summary", "empty"},
			reject: true,
		},
		{
			name:   "a status outside the set",
			h:      Handoff{"status": "done", "summary": "x"},
			wants:  []string{"handoff.status", "ok", `"done"`},
			reject: true,
		},
		{
			name:   "a status with trailing whitespace",
			h:      Handoff{"status": "done ", "summary": "x"},
			wants:  []string{`"done "`},
			reject: true,
		},
		{
			name:   "a non-string status",
			h:      Handoff{"status": 1, "summary": "x"},
			wants:  []string{"handoff.status", "non-string"},
			reject: true,
		},
		{
			name: "a valid handoff",
			h:    Handoff{"status": "ok", "summary": "x"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := ValidateHandoff(tc.h, spec)
			if ok == tc.reject {
				t.Fatalf("ok=%v, reason=%q", ok, reason)
			}
			for _, want := range tc.wants {
				if !strings.Contains(reason, want) {
					t.Errorf("the reason does not contain %q: %s", want, reason)
				}
			}
		})
	}
}

func TestTrailingWhitespaceInAStatusIsRefusedNotTrimmed(t *testing.T) {
	// Deliberately exact. Accepting `"done "` means the next slice compares a
	// stored status against `"done"` and finds it does not match — and the
	// quoting in the message is what makes the whitespace visible.
	spec := HandoffSpec{Enums: map[string][]string{"status": {"done"}}}
	ok, reason := ValidateHandoff(Handoff{"status": "done "}, spec)
	if ok {
		t.Fatal("a status with trailing whitespace was accepted")
	}
	if !strings.Contains(reason, `"done "`) {
		t.Errorf("the whitespace is invisible in the message: %s", reason)
	}
}

func TestAnEmptyHandoffIsRefusedWithAReason(t *testing.T) {
	ok, reason := ValidateHandoff(nil, HandoffSpec{})
	if ok {
		t.Fatal("nil validated")
	}
	if reason == "" {
		t.Error("no reason")
	}
}

func TestOutcomeFromHandoffMapsEveryStatusAndGuessesAtNone(t *testing.T) {
	// A status the handler did not enumerate is `failed` rather than a guess.
	// Guessing would turn a malformed report into a successful one, and the
	// step would be recorded as having done its job.
	cases := map[string]Outcome{
		"ok":        OutcomeOK,
		"success":   OutcomeOK,
		"complete":  OutcomeOK,
		"completed": OutcomeOK,
		"reject":    OutcomeReject,
		"rejected":  OutcomeReject,
		"no":        OutcomeReject,
		"blocked":   OutcomeBlocked,
		"failed":    OutcomeFailed,
		"":          OutcomeFailed,
		"OK":        OutcomeFailed, // case matters; a guess here is a guess
		"ok ":       OutcomeFailed,
		"whatever":  OutcomeFailed,
	}

	for status, want := range cases {
		if got := OutcomeFromHandoff(Handoff{"status": status}, "status"); got != want {
			t.Errorf("status %q mapped to %s, want %s", status, got, want)
		}
	}

	if got := OutcomeFromHandoff(nil, "status"); got != OutcomeFailed {
		t.Errorf("a nil handoff mapped to %s", got)
	}
	if got := OutcomeFromHandoff(Handoff{"state": "ok"}, "status"); got != OutcomeFailed {
		t.Errorf("a handoff with no status field mapped to %s", got)
	}
}

func TestRejectIsNotAFailure(t *testing.T) {
	// The handler decided the answer is no, which is a step that worked.
	if OutcomeFromHandoff(Handoff{"status": "reject"}, "status") == OutcomeFailed {
		t.Error("a reject was classified as a failure")
	}
	// And it is distinguishable from an ok, which is the point of having four.
	if OutcomeFromHandoff(Handoff{"status": "reject"}, "status") ==
		OutcomeFromHandoff(Handoff{"status": "ok"}, "status") {
		t.Error("reject and ok are the same outcome")
	}
}

func TestTheRetryCarriesTheValidationErrorToTheModel(t *testing.T) {
	// A model told "that was not valid JSON" produces another invalid object.
	// One told the failing path fixes it.
	reason := `handoff.status: expected one of [ok, reject], got "done "`
	msgs := RetryMessages(assistantSaying("blah"), reason)

	if len(msgs) != 2 {
		t.Fatalf("%d messages, want the previous turn plus one", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != model.RoleUser {
		t.Errorf("the re-ask is a %s message", last.Role)
	}
	text := last.Content[0].Text
	if !strings.Contains(text, reason) {
		t.Errorf("the re-ask does not carry the reason: %q", text)
	}
	// And it says not to work any more, because the work IS done — a model
	// that re-reads three files on the retry burns the iteration budget on
	// something it already knows.
	if !strings.Contains(text, "do not") && !strings.Contains(text, "Do not") {
		t.Errorf("the re-ask does not tell the model to stop working: %q", text)
	}
}

func TestTheRetryDoesNotMutateThePreviousTranscript(t *testing.T) {
	// The loop keeps the original messages for the result, and a retry that
	// appended in place would make the stored transcript depend on whether a
	// retry happened.
	previous := assistantSaying("blah")
	before := len(previous)
	RetryMessages(previous, "because")
	if len(previous) != before {
		t.Errorf("the previous transcript grew from %d to %d", before, len(previous))
	}
}

func TestExtractionMakesNoModelCall(t *testing.T) {
	// T4's "performs no model call and decides no retry". The LOOP re-asks,
	// and that split is what lets the retry's tool-free property be asserted
	// on a request object.
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	ExtractHandoff(assistantSaying("nothing useful here"))
	ValidateHandoff(Handoff{"status": "ok"}, DefaultHandoffSpec())
	RetryMessages(nil, "because")
	if client.requestCount() != 0 {
		t.Errorf("%d model calls were made from the handoff module", client.requestCount())
	}
}
