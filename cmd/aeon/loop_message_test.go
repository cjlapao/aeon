package main

// loop_message_test.go — the thin client half of `aeon loop message`
// (S69 T5). The door itself — put's caps and magic-byte verdict, the
// message door's reference resolution, the expansion — is pinned by the
// daemon suite (s69_attachments_test.go); what is owned HERE is everything
// this file added: the repeatable flag's honesty and the usage refusal. The
// human path with real bytes rides the DEMO §S69 walk.

import (
	"bytes"
	"strings"
	"testing"
)

func TestTheAttachFlagIsRepeatableAndRefusesBlank(t *testing.T) {
	var r repeatFlag
	if err := r.Set("first.png"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("second.pdf"); err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0] != "first.png" || r[1] != "second.pdf" {
		t.Fatalf("order or content drifted: %v", r)
	}
	if err := r.Set("   "); err == nil || !strings.Contains(err.Error(), "needs a path") {
		t.Fatalf("a blank path passed: %v", err)
	}
}

func TestLoopMessageUsageNamesTheShape(t *testing.T) {
	var w bytes.Buffer
	if err := cmdLoopMessage(&w, []string{"only-one-argument"}); err == nil ||
		!strings.Contains(err.Error(), `loop message ID "TEXT"`) {
		t.Fatalf("the usage did not speak: %v", err)
	}
}
