package main

// loop_messages_test.go — the thin client half of `aeon loop messages`
// (S70 T5). The door itself — the said absence, the scope resolution, the
// MB8 byte discipline — is pinned by the protocol suite
// (agent_messages_test.go) and the conformance fixtures; the rows and the
// settlement rules are pinned by the daemon suite (agent_mailbox_test.go).
// What is owned HERE is everything this file added: the refusals that
// spare an operator a round trip, and the usage line that tells them the
// verb READS — the person's door to a loop is `loop message`, and an
// operator who believes otherwise is composing in the wrong window. The
// human path with a live daemon rides the DEMO §S70 walk.

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoopMessagesRefusesWithoutALoopId(t *testing.T) {
	var w bytes.Buffer
	err := cmdLoopMessages(&w, nil)
	if err == nil || !strings.Contains(err.Error(), "needs a loop id") {
		t.Fatalf("the missing id was not refused by name: %v", err)
	}
	// The refusal also names the OTHER door, because confusing them is the
	// one mistake this verb invites.
	if !strings.Contains(err.Error(), "loop message") {
		t.Fatalf("the refusal did not point at the person's door: %v", err)
	}
}

func TestLoopMessagesRefusesANegativeThreadBeforeDialling(t *testing.T) {
	// The daemon refuses this too (its refusal names the ruling); the CLI
	// refuses it BEFORE a socket, so a typo costs nothing. A dial would
	// need a running daemon — reaching one here would mean the refusal
	// moved behind the round trip, which this test exists to catch.
	var w bytes.Buffer
	err := cmdLoopMessages(&w, []string{"l-7", "--thread", "-3"})
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("the negative thread was not refused: %v", err)
	}
}

func TestLoopMessagesNoFlagReadsTheWholeLoop(t *testing.T) {
	// The post-merge smoke measured the shape this pins: when 0 was
	// refused outright, the NO-FLAG default died on the same refusal and
	// the daemon was never dialled. Unset (-1) must sail past the flag
	// gate — any error here is the missing daemon, never the thread.
	t.Setenv("LOCALRPC_ENDPOINT_PATH", t.TempDir()+"/no-daemon.sock")
	var w bytes.Buffer
	err := cmdLoopMessages(&w, []string{"l-7"})
	if err != nil && strings.Contains(err.Error(), "--thread") {
		t.Fatalf("the unset default was refused as a thread value: %v", err)
	}
}

func TestLoopMessagesRefusesZeroThreadToo(t *testing.T) {
	// 0 is the flag's own "unset" spelling — a SPOKEN zero must be
	// refused, not silently widened to the whole loop. Measured at the
	// DEMO walk: the silent drop printed "no thread 0" beside a loop that
	// was merely empty, an absence claim about a row id nobody has.
	var w bytes.Buffer
	err := cmdLoopMessages(&w, []string{"l-7", "--thread", "0"})
	if err == nil || !strings.Contains(err.Error(), "positive") || !strings.Contains(err.Error(), "omit the flag") {
		t.Fatalf("a spoken zero passed as unset: %v", err)
	}
}

func TestLoopMessagesRefusesANonNumericThread(t *testing.T) {
	var w bytes.Buffer
	if err := cmdLoopMessages(&w, []string{"l-7", "--thread", "twelve"}); err == nil {
		t.Fatal("a thread that is not a number passed the flag set")
	}
}

func TestLoopUsageNamesTheReadAndPointsAtTheWrite(t *testing.T) {
	// An operator scanning `aeon loop` must see both doors and which is
	// which: messages READS the agents' exchange; message is the person's
	// own door into a resting conversation.
	var w bytes.Buffer
	if err := cmdLoop(&w, nil); err != nil {
		t.Fatal(err)
	}
	usage := w.String()
	if !strings.Contains(usage, "loop messages ID") {
		t.Fatalf("the usage hides the read: %s", usage)
	}
	if !strings.Contains(usage, "This verb READS") {
		t.Fatalf("the usage does not say which way the door opens: %s", usage)
	}
}
