package notify

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/trigger"
)

// envelope_test.go — S58 T5: what leaves the machine, in the exact bytes
// it leaves in. The envelope is the whole outward surface of Aeon: one
// fixed JSON shape, ≤1 KiB, signed. Everything the SV6 promises lives in
// these tests — closed fields, no user text, a signature the project's
// OWN inbound verifier accepts.

func baseEvent() ResolvedEvent {
	return ResolvedEvent{
		Seq: 42, Kind: "loop.waiting", Variant: "question", Severity: "needs-you",
		RootSlug: "alpha", LoopSlug: "demo", LoopID: "loop-77",
		ComponentRoot: "/home/ops/work/alpha", Title: "Loop waiting for you",
		AttentionKey: "question:42", DeepLink: "aeon://loop/demo",
	}
}

func TestEnvelopeContainsOnlyTheClosedFields(t *testing.T) {
	body, err := Marshal(Build(baseEvent()))
	if err != nil {
		t.Fatal(err)
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(body, &bag); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"v": true, "id": true, "at": true, "event": true, "severity": true,
		"kind": true, "rootSlug": true, "loopSlug": true, "title": true, "deepLink": true,
	}
	for k := range bag {
		if !allowed[k] {
			t.Errorf("envelope carries %q — the field set is closed (SV6)", k)
		}
	}
	// And the values that must be exactly right.
	if string(bag["v"]) != "1" {
		t.Errorf("v = %s, want 1", bag["v"])
	}
	if got := string(bag["kind"]); got != `"loop.waiting.question"` {
		t.Errorf("kind = %s, want the dotted root.variant", got)
	}
	if got := string(bag["event"]); got != `"loop.waiting"` {
		t.Errorf("event = %s, want the class kind", got)
	}
	// loopID and componentRoot exist on the RESOLVED event (the socket
	// plane needs them); neither may reach the wire.
	for _, leak := range []string{"loop-77", "/home/ops/work/alpha", "question:42"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("body leaks %q — resolved-event plumbing never ships", leak)
		}
	}
}

func TestOutboundBodyLeaksNoUserText(t *testing.T) {
	// The sentinels are exactly what SV6 forbids travelling: component
	// names, goals, prompts, message text, paths, branches, commands,
	// costs, token counts. ResolvedEvent has NO field that could carry
	// most of them — this test makes that structural fact loud: it stuffs
	// every field the struct does have with a sentinel-bearing value and
	// shows only the slug/slug/title-shaped ones survive.
	ev := baseEvent()
	goal := "GOAL-SENTINEL-ship-the-report"
	prompt := "PROMPT-SENTINEL-read-all-files"
	msg := "MESSAGE-SENTINEL-please-approve"
	path := "/home/ops/PATH-SENTINEL-secrets.env"
	branch := "BRANCH-SENTINEL-leak-main"
	cmd := "COMMAND-SENTINEL-curl-attacker"
	ev.Title = "Loop waiting for you" // the renderer's honest output (no user text)
	ev.AttentionKey = "question:" + msg
	ev.ComponentRoot = path
	ev.LoopID = branch
	_ = cmd // commands, goals, prompts, message bodies: no field exists to hold them
	_ = goal
	_ = prompt
	body, err := Marshal(Build(ev))
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{goal, prompt, msg, path, branch, cmd} {
		if strings.Contains(string(body), strings.Split(sentinel, "-SENTINEL")[0]+"-SENTINEL") {
			t.Errorf("body carries user-text sentinel %q — the envelope is kind+slug+title only", sentinel)
		}
	}
}

func TestEnvelopeTruncatesTitleNotViolatesCap(t *testing.T) {
	ev := baseEvent()
	ev.Title = strings.Repeat("é", 900) // 2-byte runes: 1800 bytes of title
	body, err := Marshal(Build(ev))
	if err != nil {
		t.Fatalf("a huge title must be truncated to fit, not error the delivery: %v", err)
	}
	if len(body) > MaxEnvelopeBytes {
		t.Fatalf("body is %d bytes, over the %d cap", len(body), MaxEnvelopeBytes)
	}
	var back Envelope
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("truncated body is not valid JSON: %v", err)
	}
	// Truncation is rune-safe: no partial UTF-8 sequence in the body.
	for _, r := range back.Title {
		if r == 0xFFFD {
			t.Fatal("truncation split a rune")
		}
	}
}

func TestEnvelopeCapRefusesAFrameThatCannotShrink(t *testing.T) {
	// With no title left to cut, the fixed fields alone must still fit —
	// if a future field set breaks that, the cap REFUSES the send rather
	// than shipping an oversized body.
	e := Envelope{
		V: 1, ID: strings.Repeat("d", 400), At: strings.Repeat("a", 400),
		Event: strings.Repeat("e", 400), Severity: strings.Repeat("s", 400),
		Kind: strings.Repeat("k", 400), RootSlug: strings.Repeat("r", 400),
		LoopSlug: strings.Repeat("l", 400), Title: strings.Repeat("t", 400),
		DeepLink: strings.Repeat("h", 400),
	}
	if _, err := Marshal(e); err == nil {
		t.Fatal("an unshrinkable over-cap frame was accepted")
	}
}

func TestSignatureMatchesInboundVerifier(t *testing.T) {
	// The round trip: sign with the dispatcher's Sign, verify with the
	// project's OWN inbound verifier (trigger). If these ever drift, a
	// receiver wired for Aeon webhooks would reject Aeon notifications.
	body, err := Marshal(Build(baseEvent()))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "receiver-secret"
	header := SignatureHeader([]byte(secret), body)
	if !strings.HasPrefix(header, "sha256=") {
		t.Fatalf("header %q — the scheme prefix is part of the contract", header)
	}
	if !trigger.VerifyWebhookSignature(secret, body, header) {
		t.Fatal("trigger.VerifyWebhookSignature rejected our own signature")
	}
	flipped := []byte(header)
	flipped[len(flipped)-1] ^= 0x01 // flip the last hex digit
	if trigger.VerifyWebhookSignature(secret, body, string(flipped)) {
		t.Fatal("a flipped byte still verified — the signature is not checked")
	}
	if trigger.VerifyWebhookSignature("other-secret", body, header) {
		t.Fatal("the wrong secret verified")
	}
}

func TestDigestEnvelopeShape(t *testing.T) {
	ev := ResolvedEvent{Seq: 1, Severity: "info", IsDigest: true, DigestCount: 7}
	e := BuildDigest(ev)
	if e.Event != "inbox.digest" {
		t.Errorf("digest event = %q, want inbox.digest", e.Event)
	}
	if e.Kind != "" {
		t.Errorf("digest kind = %q, want none (a digest is about no single thing)", e.Kind)
	}
	if !strings.Contains(e.Title, "7") || !strings.Contains(e.Title, "attention") {
		t.Errorf("digest title %q — a count and the word, nothing else (SV-D6)", e.Title)
	}
}

func TestTestEnvelopeEventIsTest(t *testing.T) {
	ev := baseEvent()
	ev.IsTest = true
	e := Build(ev)
	if e.Event != "test" || e.Kind != "test" {
		t.Errorf("test event/kind = %q/%q, want test — a receiver must be able to tell", e.Event, e.Kind)
	}
}
