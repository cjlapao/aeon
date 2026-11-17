package model

import (
	"strings"
	"testing"
)

// S69 (AT5) — the either/or the attachment kinds are built on.
//
// The transcript NEVER stores bytes: a stored block carries the pinned
// reference `(Document, Rev)`, and only the daemon's send-time expansion door
// produces `Data`. `Validate` is what makes the illegal middle states exist
// in theory and never in a stored transcript — so every one of them gets a
// row here. The table is the specification: a missing row is a state a
// corrupt row could hold.
func TestAttachmentBlocksEnforceTheEitherOr(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block Block
		// want is a phrase the refusal must contain — a refusal that does
		// not name what it refused is not one (AGENTS §5).
		want string
	}{
		{
			name:  "an image carrying bytes AND a document reference",
			block: Block{Kind: BlockImage, MediaType: "image/png", Data: "aGk=", Document: "@attachments/ab12cd34-chart.png", Rev: 2},
			want:  "both",
		},
		{
			name:  "a document carrying bytes AND a revision",
			block: Block{Kind: BlockDocument, MediaType: "application/pdf", Data: "JVBERi0=", Document: "x", Rev: 1},
			want:  "both",
		},
		{
			name:  "an image carrying neither bytes nor a reference",
			block: Block{Kind: BlockImage, MediaType: "image/png"},
			want:  "neither",
		},
		{
			name:  "a document named but pinned to no revision",
			block: Block{Kind: BlockDocument, MediaType: "application/pdf", Document: "@attachments/ff00ff00-spec.pdf"},
			want:  "without a revision",
		},
		{
			name:  "a revision of no document",
			block: Block{Kind: BlockImage, MediaType: "image/png", Rev: 3},
			want:  "no document",
		},
		{
			name:  "an image with no mediaType — the wire needs it and an empty one is an invention",
			block: Block{Kind: BlockImage, Data: "aGk="},
			want:  "mediaType",
		},
		{
			name:  "a document with no mediaType",
			block: Block{Kind: BlockDocument, Document: "d", Rev: 1},
			want:  "mediaType",
		},
		{
			name:  "a TEXT block carrying attachment bytes",
			block: Block{Kind: BlockText, Text: "hi", Data: "aGk="},
			want:  "data",
		},
		{
			name:  "a TEXT block carrying an attachment reference",
			block: Block{Kind: BlockText, Text: "hi", Document: "d", Rev: 1},
			want:  "document",
		},
		{
			name:  "a TEXT block carrying a mediaType",
			block: Block{Kind: BlockText, Text: "hi", MediaType: "image/png"},
			want:  "mediaType",
		},
		{
			name:  "a TOOL_USE block carrying attachment bytes",
			block: Block{Kind: BlockToolUse, ID: "b1", Name: "bash", Data: "aGk="},
			want:  "data",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.block.Validate()
			if err == nil {
				t.Fatalf("accepted %s — this is a state the transcript must never hold (AT5)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name %q: %v", tc.want, err)
			}
		})
	}
}

// The two legal forms, per kind, from the constructors that are the only
// sanctioned ways to build them: the STORED reference and the ON-WIRE bytes.
// If a constructor here breaks, every send-time expansion downstream is
// building a block the next Validate will refuse.
func TestEveryAttachmentConstructorProducesAValidBlock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block Block
	}{
		{"ImageRefBlock (stored)", ImageRefBlock("chart.png", "image/png", "@attachments/ab12cd34-chart.png", 3)},
		{"DocumentRefBlock (stored)", DocumentRefBlock("spec.pdf", "application/pdf", "@attachments/ff00ff00-spec.pdf", 1)},
		{"ImageRefBlock paste (no name)", ImageRefBlock("", "image/png", "@attachments/ab12cd34.png", 1)},
		{"ImageDataBlock (on wire)", ImageDataBlock("chart.png", "image/png", "aGk=")},
		{"DocumentDataBlock (on wire)", DocumentDataBlock("spec.pdf", "application/pdf", "JVBERi0=")},
		{"DocumentDataBlock paste (no name)", DocumentDataBlock("", "application/pdf", "JVBERi0=")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.block.Validate(); err != nil {
				t.Fatalf("%s produced an invalid block: %v", tc.name, err)
			}
		})
	}
}
