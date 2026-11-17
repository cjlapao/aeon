package model

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestModelDeclarationsAndRequestEfforts(t *testing.T) {
	r := Route{ID: "r", Capabilities: []string{"thinking-effort", "text", "text"}, ReasoningEfforts: []string{"max", "low", "xhigh", "low"}, ReasoningEffort: "max"}
	if err := r.NormalizeMetadata(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.ReasoningEfforts, ",") != "low,xhigh,max" || strings.Join(r.Capabilities, ",") != "text,thinking-effort" {
		t.Fatalf("not canonical: %+v", r)
	}
	req := ModelRequest{Route: r, Messages: []Message{{Role: RoleUser, Content: []Block{{Kind: BlockText, Text: "hello"}}}}}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	req.ReasoningEffort = "high"
	if err := req.Validate(); err == nil {
		t.Fatal("unsupported per-call effort accepted")
	}
	req.Route.ReasoningEfforts = nil
	if err := req.Validate(); err != nil {
		t.Fatalf("legacy route refused: %v", err)
	}
	req.Route.ReasoningEfforts = []string{}
	if err := req.Validate(); err == nil {
		t.Fatal("explicit empty effort declaration ignored")
	}
	for _, r := range []Route{
		{Capabilities: []string{"invented"}}, {Capabilities: []string{"pdf"}},
		{Capabilities: []string{"thinking"}, ReasoningEfforts: []string{"low"}},
		{ReasoningEfforts: []string{"non"}}, {ReasoningEfforts: []string{"low"}, ReasoningEffort: "high"},
	} {
		if err := r.NormalizeMetadata(); err == nil {
			t.Fatalf("invalid declaration accepted: %+v", r)
		}
	}
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if !ValidReasoningEffort(effort) {
			t.Fatalf("missing effort %q", effort)
		}
	}
}

func TestModelIconsAreBoundedImages(t *testing.T) {
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/nWQAAAAASUVORK5CYII="
	if err := ValidateIcon(png); err != nil {
		t.Fatal(err)
	}
	for _, icon := range []string{"https://example.com/icon.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,aGVsbG8=", "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, MaxIconBytes+1))} {
		if err := ValidateIcon(icon); err == nil {
			t.Fatal("invalid icon accepted")
		}
	}
}

func TestEmptyCapabilitiesRemainDeclaredInViews(t *testing.T) {
	if OptionalSelection(nil) != nil {
		t.Fatal("nil is not unspecified")
	}
	data, err := json.Marshal(OptionalSelection([]string{}))
	if err != nil || string(data) != "[]" {
		t.Fatalf("empty array lost: %s %v", data, err)
	}
}

// S77: the media-type → required-capability mapping the launch-attachment
// gate leans on, and the rule that an UNDECLARED capability is never read
// as a silent yes.
func TestRequiredCapabilityMapsTheStoreAllowList(t *testing.T) {
	for _, c := range []struct{ media, want string }{
		{"image/png", "image"}, {"image/jpeg", "image"}, {"image/gif", "image"}, {"image/webp", "image"},
		{"application/pdf", "pdf"}, {"text/plain", "file"}, {"text/markdown", "file"},
		{"application/octet-stream", ""}, {"", ""},
	} {
		if got := RequiredCapability(c.media); got != c.want {
			t.Fatalf("RequiredCapability(%q) = %q, want %q", c.media, got, c.want)
		}
	}
	r := Route{ID: "r", Capabilities: []string{"text", "image"}}
	if !r.DeclaresCapability("image") || r.DeclaresCapability("pdf") {
		t.Fatal("DeclaresCapability misread the declared set")
	}
	if (&Route{ID: "legacy"}).DeclaresCapability("text") {
		t.Fatal("an undeclared set read as a yes — absence is not consent")
	}
}
