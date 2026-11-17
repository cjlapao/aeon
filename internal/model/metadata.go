package model

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

const MaxIconBytes = 256 * 1024

// Capabilities is the model-purpose vocabulary — what the MODEL is for
// (distinct from WireCapabilities, which says what the SERVER does on the
// wire; capability.go keeps the two axes apart).
//
// "asr" and "tts" are the voice purposes (speech in, speech out), routes like any other.
// "embedding" arrived with S22 T3 (Q5). It is a PURPOSE like the others, and
// it is the answer to whether the route type needed generalising for an
// embedding model: it did not. Kind, provider, model, base-URL, prices and
// the O8 no-price declaration all apply unchanged; the chat-only assumptions
// live in the CALLER (Client.Stream speaks messages), never in the row, and
// M16's required MaxOutputTokens is checked at chat call time — an embedding
// route stores 0 honestly, because an embedding has no output tokens.
func Capabilities() []string {
	return []string{"text", "image", "tool-calling", "pdf", "file", "thinking", "thinking-effort", "embedding", "asr", "tts"}
}

// RequiredCapability names the model purpose an attachment's media type asks
// of a route (S77: a loop can now START with files attached, and a launch
// that names a model which cannot see the bytes must say so at the door).
// The store's allow-list is this mapping's whole domain (S69's sniff admits
// exactly these families): images ask "image", a PDF asks "pdf", text asks
// "file". "" means no declared capability could honestly take the type.
func RequiredCapability(mediaType string) string {
	switch {
	case strings.HasPrefix(mediaType, "image/"):
		return "image"
	case mediaType == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mediaType, "text/"):
		return "file"
	}
	return ""
}

// DeclaresCapability answers whether the route's DECLARED capabilities hold
// one purpose. An undeclared set is not a silent yes: what a route does not
// declare it does not have (P31 — absence spoken, never assumed).
func (r Route) DeclaresCapability(capability string) bool {
	return slices.Contains(r.Capabilities, capability)
}

// NormalizeMetadata validates declarations without assigning capabilities to legacy routes.
func (r *Route) NormalizeMetadata() error {
	r.DisplayName = strings.TrimSpace(r.DisplayName)
	r.DisplayDescription = strings.TrimSpace(r.DisplayDescription)
	if len(r.DisplayName) > 200 || len(r.DisplayDescription) > 4000 {
		return fmt.Errorf("model display name or description is too long")
	}
	if err := ValidateIcon(r.Icon); err != nil {
		return err
	}
	var err error
	if r.Capabilities, err = normalizeSelection(r.Capabilities, Capabilities(), "capability"); err != nil {
		return err
	}
	if r.ReasoningEfforts, err = normalizeSelection(r.ReasoningEfforts, ReasoningEfforts(), "reasoning effort"); err != nil {
		return err
	}
	if r.Capabilities != nil && slices.Contains(r.Capabilities, "pdf") && !slices.Contains(r.Capabilities, "file") {
		return fmt.Errorf("PDF capability requires file capability")
	}
	if r.Capabilities != nil && len(r.ReasoningEfforts) > 0 && !slices.Contains(r.Capabilities, "thinking-effort") {
		return fmt.Errorf("available reasoning efforts require the thinking-effort capability")
	}
	return r.ValidateEffort(r.ReasoningEffort)
}

func normalizeSelection(values, allowed []string, kind string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return nil, fmt.Errorf("unknown %s %q", kind, value)
		}
	}
	out := make([]string, 0, len(values))
	for _, value := range allowed {
		if slices.Contains(values, value) {
			out = append(out, value)
		}
	}
	return out, nil
}

func (r Route) ValidateEffort(effort string) error {
	if !ValidReasoningEffort(effort) {
		return fmt.Errorf("unknown reasoning effort %q", effort)
	}
	if effort != "" && r.ReasoningEfforts != nil && !slices.Contains(r.ReasoningEfforts, effort) {
		return fmt.Errorf("model %q does not support reasoning effort %q", r.ID, effort)
	}
	return nil
}

func ValidateIcon(icon string) error {
	if icon == "" {
		return nil
	}
	header, encoded, ok := strings.Cut(icon, ",")
	if !ok || (header != "data:image/png;base64" && header != "data:image/jpeg;base64" && header != "data:image/webp;base64") {
		return fmt.Errorf("model icon must be a PNG, JPEG or WebP data URI")
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxIconBytes) {
		return fmt.Errorf("model icon exceeds 256 KiB")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > MaxIconBytes {
		return fmt.Errorf("model icon is invalid or exceeds 256 KiB")
	}
	mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if http.DetectContentType(data) != mime {
		return fmt.Errorf("model icon content does not match its image type")
	}
	return nil
}

// OptionalSelection preserves the distinction between an absent declaration and [].
func OptionalSelection(values []string) *[]string {
	if values == nil {
		return nil
	}
	copy := slices.Clone(values)
	return &copy
}
