package adapters

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S69 (AT6) — the two widened block kinds on `/v1/messages`.
//
// The fixtures the spec names for this wire are "anthropic image/document
// blocks": the bytes below are the base64 the wire must carry VERBATIM inside
// a base64 source, so the adapter can neither re-encode nor lose them.

const (
	attachPNGBytes = "iVBORw0KGgoFIXTUREpng"
	attachPDFBytes = "JVBERi0xLjQFIXTUREpdf"
)

// The contract, as maps: the person's words FIRST as one untouched `text`
// block (AT7), each attachment beside them as its own typed block. Every case
// is compared with reflect.DeepEqual over the WHOLE block — a presence check
// would pass on a `media_type` that was guessed, a source that was nested
// wrongly, or a title that was invented for a nameless paste.
func TestAnthropicImageAndDocumentBlocksArePinnedToTheWireBytes(t *testing.T) {
	tests := []struct {
		name  string
		block model.Block
		want  map[string]any
	}{
		{
			name:  "an image is a base64 source and nothing else — this wire has no title field for one",
			block: model.ImageDataBlock("diagram.png", "image/png", attachPNGBytes),
			want: map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/png",
					"data":       attachPNGBytes,
				},
			},
		},
		{
			name:  "a document carries the same source plus its title",
			block: model.DocumentDataBlock("spec.pdf", "application/pdf", attachPDFBytes),
			want: map[string]any{
				"type":  "document",
				"title": "spec.pdf",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "application/pdf",
					"data":       attachPDFBytes,
				},
			},
		},
		{
			name:  "a paste's document OMITS the title rather than send one empty",
			block: model.DocumentDataBlock("", "application/pdf", attachPDFBytes),
			want: map[string]any{
				"type": "document",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "application/pdf",
					"data":       attachPDFBytes,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newAnthropic()
			req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
				{Role: model.RoleUser, Content: []model.Block{
					model.TextBlock("look at this"),
					tc.block,
				}},
			}, Route: testRoute(ProviderAnthropic)})
			if err != nil {
				t.Fatal(err)
			}

			msgs := wireBody(t, req)["messages"].([]any)
			content := msgs[0].(map[string]any)["content"].([]any)
			if len(content) != 2 {
				t.Fatalf("the user turn carries %d blocks, want the words and the attachment beside them", len(content))
			}
			// AT7: the words are first, byte-exact, undecorated.
			if words := content[0].(map[string]any); !reflect.DeepEqual(words,
				map[string]any{"type": "text", "text": "look at this"}) {
				t.Fatalf("the person's words were altered or demoted: %#v", words)
			}
			if got := content[1].(map[string]any); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("the attachment block is not the pinned wire shape:\n  got  %#v\n  want %#v", got, tc.want)
			}
		})
	}
}

func TestAnthropicReferenceFormAttachmentsAreCountedAndNeverHalfSent(t *testing.T) {
	// A block that reaches the adapter still pinned to a document revision
	// skipped the daemon's send-time expansion door (AT5) — a bug UPSTREAM,
	// and the adapter's answer is the discipline the switch already keeps for
	// thinking blocks: withhold, and COUNT. Never a half block on the wire
	// (an image whose bytes are a document name is not a degraded image, it
	// is an unreadable one), and never silence either (M4). The count rides
	// its own request-direction number, not `dropped` — the directions
	// answer different questions (O1).
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{
			model.TextBlock("see the chart"),
			model.ImageRefBlock("chart.png", "image/png", "@attachments/ab12cd34-chart.png", 3),
			model.DocumentRefBlock("spec.pdf", "application/pdf", "@attachments/ff00ff00-spec.pdf", 1),
		}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}

	body := wireJSON(t, req)
	for _, forbidden := range []string{`"type":"image"`, `"type":"document"`, "media_type", "base64", "chart.png", "spec.pdf"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("a reference-form block reached the wire (%q) — the expansion door upstream was skipped, and half a block is not a degraded attachment, it is an unreadable one:\n%s", forbidden, body)
		}
	}
	// The person's words still ride (AT8's ruling in miniature: the words
	// outrank their rider).
	if !strings.Contains(body, "see the chart") {
		t.Errorf("the words were dropped along with the unsendable blocks: %s", body)
	}
	if got := a.AttachmentDropped(); got != 2 {
		t.Fatalf("AttachmentDropped = %d, want 2 — a drop nobody counted is M4's silence wearing a new coat", got)
	}
	// And the count stayed out of the response-direction number.
	if _, _, dropped := a.Finalise(); dropped != 0 {
		t.Errorf("the request-direction attachment drop leaked into `dropped` (%d) — the two directions must not blur (O1)", dropped)
	}
}

func TestAnthropicTextOnlyTurnIsByteIdenticalToBeforeTheAttachmentWidening(t *testing.T) {
	// S69 widened the block union; it may NOT widen what a text-only turn
	// sends. The exact serialized bytes of the user message, pinned: the
	// typed-block array this wire has always taken, one text block, sorted
	// keys. Every pre-S69 fixture and replay comparison rests on this.
	a := newAnthropic()
	req, err := a.ToWire(model.ModelRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}},
	}, Route: testRoute(ProviderAnthropic)})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wireBody(t, req)["messages"].([]any)
	raw, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"content":[{"text":"hello","type":"text"}],"role":"user"}`
	if string(raw) != want {
		t.Fatalf("a text-only turn no longer serializes as it did before S69:\n  got  %s\n  want %s", raw, want)
	}
}
