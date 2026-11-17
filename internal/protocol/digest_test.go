package protocol

// digest_test.go — S63 T5 (RT12): the digest's fixed order is its
// contract, and the order is a property of the WIRE, so this file reads
// the marshalled bytes — never the struct declaration — and walks the
// emitted keys. An empty window answers with the sections at their
// zero/cannot-say shape and the sentence (the stub's zero result IS that
// shape: dispatch succeeds, no error), which is RT12's own clause: an
// empty digest is a sentence, not a failure.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestDigestSectionsAreFixedOrder walks the marshalled result and
// compares each nesting level's key order against the design's list.
// Go's encoding/json writes struct fields in DECLARED order, so this
// test fails the moment a field is reordered or a derived field (a
// "delta", a "changePct", a "score") is added to any section.
func TestDigestSectionsAreFixedOrder(t *testing.T) {
	raw, err := json.Marshal(DigestResult{})
	if err != nil {
		t.Fatal(err)
	}
	top := topLevelKeys(t, raw)
	wantTop := []string{
		"window", "statement", "coverage", "outcomes", "rework",
		"blocked", "cost", "learned", "attention",
	}
	assertKeyOrder(t, "sections", top, wantTop)

	// RT12 §2's own order inside outcomes (the previous-window echo is
	// omitempty, so the nesting is walked on a result that HAS it), §6's
	// inside learned, §7's inside attention.
	withPrev, err := json.Marshal(DigestResult{Outcomes: DigestOutcomesSection{
		Previous: &DigestOutcomesSection{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertKeyOrder(t, "outcomes", nestedKeys(t, withPrev, "outcomes"),
		[]string{"cardWindow", "byKind", "previous"})
	assertKeyOrder(t, "learned", nestedKeys(t, raw, "learned"), []string{"groups"})
	assertKeyOrder(t, "attention", nestedKeys(t, raw, "attention"),
		[]string{"blockedNow", "pendingQuestions", "proposalsPending", "statement"})
	assertKeyOrder(t, "coverage", nestedKeys(t, raw, "coverage"),
		[]string{"recordedSince", "loopsInHistory", "loopsRecorded", "loopsNotRecorded"})

	// No derived metric sits anywhere in the digest's vocabulary (RT8):
	// the bytes themselves must not carry the words.
	for _, banned := range []string{"delta", "trend", "changePct", "change_pct", "score", "rank", "healthy"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Errorf("the digest's wire vocabulary carries %q — RT8 forbids the derived word", banned)
		}
	}
}

func TestDigestRequiresAComponent(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Digest = digestTableStub{}
	if !tbl.RegisterDigest() {
		t.Fatal("RegisterDigest refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	for _, params := range []string{`{}`, `{"id":"  "}`} {
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodDigestLearned, json.RawMessage(params)); err == nil {
			t.Errorf("the digest answered without a component (%s)", params)
		}
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodDigestLearned,
		json.RawMessage(`{"id":"aeon","actor":{"kind":"user","id":"9"}}`)); err == nil {
		t.Error("the digest accepted a typed actor")
	} else {
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("the identity refusal came as %T, want ErrBadParams", err)
		}
	}
	// The empty window answers (the stub's zero result is the empty
	// digest, sentence and all): a component and no window SUCCEEDS.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodDigestLearned,
		json.RawMessage(`{"id":"aeon"}`)); err != nil {
		t.Errorf("an empty-window digest errored — RT12 says an empty digest is a sentence: %v", err)
	}
}

func TestDigestAbsentWhenSeamNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterDigest() {
		t.Fatal("RegisterDigest claimed a nil seam is wired")
	}
	if _, ok := tbl.Lookup(MethodDigestLearned); ok {
		t.Error("digest.learned registered over no seam")
	}
}

// --- byte-level key-order readers ---------------------------------------------

// topLevelKeys returns the object's keys in EMISSION order (a Decoder
// walks tokens as written — never a map). The wantKey bookkeeping is
// the JSON object's own alternating key/value rhythm.
func topLevelKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("the digest did not marshal to an object: %v", err)
	}
	type frame struct{ inArray, wantKey bool }
	stack := []*frame{{wantKey: true}}
	var keys []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, &frame{})
			case '[':
				stack = append(stack, &frame{inArray: true})
			default: // } or ]
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return keys // the document closed
				}
				if top := stack[len(stack)-1]; !top.inArray {
					top.wantKey = true // the value this key held has ended
				}
			}
		case string:
			f := stack[len(stack)-1]
			if !f.inArray {
				if f.wantKey && len(stack) == 1 {
					keys = append(keys, v)
				}
				f.wantKey = !f.wantKey
			}
		default: // number, bool, null — a scalar value in an object closes its key's turn
			f := stack[len(stack)-1]
			if !f.inArray {
				f.wantKey = true
			}
		}
	}
	return keys
}

func nestedKeys(t *testing.T, raw []byte, field string) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	sub, ok := m[field]
	if !ok {
		t.Fatalf("the digest carries no %q section", field)
	}
	return topLevelKeys(t, sub)
}

func assertKeyOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s order = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s order = %v, want %v — the order is RT12's, not the encoder's", what, got, want)
		}
	}
}
