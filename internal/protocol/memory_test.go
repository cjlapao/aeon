package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The wire contract for collections, and the bug it exists because of.
//
// # What happened
//
// The app crashed on a FRESH store with
// `TypeError: null is not an object (evaluating 'info.pending.length')`, and the
// Store tab went blank. Go marshals a nil slice as `null`, and `pending` is
// empty on every healthy store — so it was `null` every single time.
//
// The conformance fixture said `"pending": []` and the UI test used the same
// shape, so neither could catch it. **A hand-written fixture asserts what
// somebody believed the shape was.** This test asserts what the code actually
// emits, by marshalling the handlers' real output.

// nilMemory is a Memory that returns nothing but nils and zero values.
//
// It is the adversary this test needs: an implementation that does everything
// Go permits and nothing more. The real adapter now initialises its slices, so
// a test against it would pass with the handlers' normalisation removed.
type nilMemory struct{}

// The S69 attachment doors on the nil double REFUSE by name: a runtime with
// no memory has no bytes to put and none to read; pretending otherwise would
// file a receipt for nothing.
func (nilMemory) PutAttachment(p AttachmentPutParams) (AttachmentPutResult, error) {
	return AttachmentPutResult{}, ErrStoreUnavailable.WithWhat("no store: attachment %s cannot be put", p.Name)
}

func (nilMemory) ReadAttachmentForView(p AttachmentReadParams) (AttachmentReadResult, error) {
	return AttachmentReadResult{}, ErrStoreUnavailable.WithWhat("no store: attachment %s rev %d cannot be read", p.Document, p.Rev)
}

func (nilMemory) ResolveAttachment(slug, componentRoot, document string, rev int) (AttachmentReceiptView, error) {
	return AttachmentReceiptView{}, ErrStoreUnavailable.WithWhat("no store: attachment %s rev %d cannot be resolved", document, rev)
}

func (nilMemory) Info() (StoreInfoResult, error)                     { return StoreInfoResult{}, nil }
func (nilMemory) CreateLoop(LoopCreateParams) (LoopView, error)      { return LoopView{}, nil }
func (nilMemory) ListLoops(string, bool, string, string) ([]LoopView, error) { return nil, nil }
func (nilMemory) WriteDocument(DocumentWriteParams) (DocumentWriteResult, error) {
	return DocumentWriteResult{}, nil
}
func (nilMemory) ReadDocument(string, string, string) (DocumentView, error) {
	return DocumentView{}, nil
}
func (nilMemory) ListDocuments(string, string) ([]DocumentView, error) { return nil, nil }
func (nilMemory) ShowStep(int64) (StepView, error)                     { return StepView{ID: 1}, nil }
func (nilMemory) VerifyStep(int64) (StepVerifyResult, error)           { return StepVerifyResult{}, nil }
func (nilMemory) ListSteps(string, string) ([]StepView, error)         { return nil, nil }
func (nilMemory) ReadLoopSteps(LoopStepsParams) (LoopStepsResult, error) {
	return LoopStepsResult{}, nil
}
func (nilMemory) ReadLoopEvents(LoopEventsParams) (LoopEventsResult, error) {
	return LoopEventsResult{}, ErrStoreUnavailable
}

func (nilMemory) ReadLoopTranscript(LoopTranscriptParams) (LoopTranscriptResult, error) {
	return LoopTranscriptResult{}, nil
}
func (nilMemory) Awaiting(string, bool) (AwaitingResult, error)        { return AwaitingResult{}, nil }
func (nilMemory) PendingQuestion(string, string) (QuestionView, bool, error) {
	return QuestionView{}, false, nil
}
func (nilMemory) SaveAsk(AskSaveParams) (int64, error) { return 0, nil }

func TestNoStoreMethodEverSendsNullForACollection(t *testing.T) {
	// Driven through Dispatch, so what is marshalled is exactly what a client
	// receives — not a struct a test built by hand.
	tbl := newTestTable(t)
	tbl.Memory = nilMemory{}
	sess := initialized(t, tbl, "c1", PROTOCOL_VERSION)

	cases := []struct {
		method MethodName
		params string
		fields []string
	}{
		{MethodStoreInfo, `{}`, []string{"applied", "pending", "tables"}},
		{MethodStoreAwaiting, `{"all":true}`, []string{"loops"}},
		{MethodLoopList, `{"all":true}`, []string{"loops"}},
		{MethodLoopRead, `{"slug":"l","componentRoot":"/r"}`, []string{"documents"}},
		{MethodStepList, `{"slug":"l","componentRoot":"/r"}`, []string{"steps"}},
	}

	for _, tc := range cases {
		t.Run(string(tc.method), func(t *testing.T) {
			result, err := tbl.Dispatch(context.Background(), sess.ConnID, tc.method, json.RawMessage(tc.params))
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}

			var into map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &into); err != nil {
				t.Fatalf("%s did not marshal to an object: %v", tc.method, err)
			}
			for _, field := range tc.fields {
				raw, present := into[field]
				if !present {
					t.Fatalf("%s has no %q field at all: %s", tc.method, field, encoded)
				}
				if string(raw) == "null" {
					t.Errorf("%s sends %q as null.\n  %s\n\n"+
						"A field that is sometimes null and sometimes [] makes every client write a "+
						"null-guard forever, and the one that forgets crashes on the happy path — which "+
						"is what happened: the app's Store tab went blank on a fresh store, because "+
						"`pending` is empty every time.", tc.method, field, encoded)
				}
			}
		})
	}
}

func TestTheNullGuardCanFail(t *testing.T) {
	// The negative control. Without it, a normalisation that silently stopped
	// working would leave the test above passing on whatever the adapter
	// happened to send.
	raw, err := json.Marshal(StoreInfoResult{Path: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"pending":null`) {
		t.Fatalf("a zero-valued StoreInfoResult no longer marshals `pending` as null, so the guard "+
			"above is not testing anything: %s", raw)
	}
	// And the normalised form does not.
	raw, err = json.Marshal(StoreInfoResult{Path: "/x"}.normalised())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "null") {
		t.Errorf("a normalised StoreInfoResult still contains a null: %s", raw)
	}
}

func TestACostWithNoMissingListSendsAnEmptyArray(t *testing.T) {
	// `missing` is read by the app to explain a `partial` cost. A null there
	// would crash the same way `pending` did, in the one state a person most
	// wants explained.
	tbl := newTestTable(t)
	tbl.Memory = nilMemory{}
	sess := initialized(t, tbl, "c1", PROTOCOL_VERSION)

	result, err := tbl.Dispatch(context.Background(), sess.ConnID, MethodStepShow, json.RawMessage(`{"id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"missing":null`) {
		t.Errorf("step.show sends cost.missing as null: %s", encoded)
	}
}
