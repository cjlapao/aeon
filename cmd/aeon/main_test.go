package main

import (
	"bytes"
	"strings"
	"testing"
)

// `cmd/aeon` had no test file at all before this. These cover the CLI's
// contract — the verbs, and the two outputs a demo reads.

func TestPrintVersionReportsBuildAndToolchain(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)
	out := buf.String()

	for _, want := range []string{"aeon", "built", "commit", "go "} {
		if !strings.Contains(out, want) {
			t.Errorf("version output lacks %q:\n%s", want, out)
		}
	}
	// cgo is on (B3), so the toolchain is part of what the binary IS. A bug
	// report without it costs a round trip on exactly the class of problem
	// cgo introduces.
	if !strings.Contains(out, "go1.") {
		t.Errorf("version output does not name the Go toolchain:\n%s", out)
	}
}

func TestPrintUsageListsEveryVerb(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()

	// Every verb the dispatch accepts. A verb that works and is undocumented is
	// a verb nobody uses, so this list grows with the switch in main().
	for _, verb := range []string{
		"serve", "ping", "version", "watch", "debug",
		"--print-services", "--vectors", "help",
	} {
		if !strings.Contains(out, verb) {
			t.Errorf("usage does not list %q:\n%s", verb, out)
		}
	}
	// And the flags, since `watch` is useless without knowing them.
	for _, flag := range []string{"--kinds", "--component", "--after", "--protocol-version"} {
		if !strings.Contains(out, flag) {
			t.Errorf("usage does not mention %q:\n%s", flag, out)
		}
	}
}

// S1 refused extra arguments for every verb; S2 added verbs that take flags.
// The check became per-verb, and the deny-list is what keeps the strict
// behaviour the default for a verb added later.
func TestVerbsThatTakeNoArgumentsAreStillStrict(t *testing.T) {
	for _, verb := range []string{"--print-services", "--vectors", "help"} {
		if !takesNoArguments(verb) {
			t.Errorf("%q should take no arguments; S1 fixed a bug where `serve "+
				"--print-services` silently started the daemon", verb)
		}
	}
	for _, verb := range []string{"serve", "ping", "version", "watch", "debug"} {
		if takesNoArguments(verb) {
			t.Errorf("%q takes flags, so it must not be in the no-arguments list", verb)
		}
	}
}

// D5's demo step reads this, and it must answer WITHOUT starting the daemon —
// the reason to ask "what would this run" is usually that you do not want it
// running yet.
func TestPrintServicesAnswersOffline(t *testing.T) {
	var buf bytes.Buffer
	printServices(&buf) // no system.Initialize, no store, no endpoint
	out := buf.String()

	for _, want := range []string{"localrpc", "api", "registered services"} {
		if !strings.Contains(out, want) {
			t.Errorf("print-services lacks %q:\n%s", want, out)
		}
	}
}

// Registered is not running. If this output ever stops distinguishing them,
// somebody will read `api` in the list and conclude the daemon is listening.
func TestPrintServicesDistinguishesPinnedState(t *testing.T) {
	var buf bytes.Buffer
	printServices(&buf)
	out := buf.String()

	if !strings.Contains(out, "pinned ON") {
		t.Error("print-services does not show localrpc as pinned on")
	}
	if !strings.Contains(out, "pinned OFF") {
		t.Error("print-services does not show api as pinned off — a reader would think it listens")
	}
}

// The foundational three are wired by system.Initialize and deliberately absent
// from bootstrap.Register. Listing them would imply Aeon chose them.
func TestPrintServicesExplainsTheFoundationalAbsence(t *testing.T) {
	var buf bytes.Buffer
	printServices(&buf)
	out := buf.String()

	if !strings.Contains(out, "system.Initialize") {
		t.Errorf("print-services does not explain why app/config/logging are absent:\n%s", out)
	}
	for _, name := range []string{"\n  app ", "\n  config ", "\n  logging "} {
		if strings.Contains(out, name) {
			t.Errorf("print-services lists %q as registered; Register does not register it", strings.TrimSpace(name))
		}
	}
}
