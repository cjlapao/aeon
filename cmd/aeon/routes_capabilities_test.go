package main

import (
	"bytes"
	"strings"
	"testing"
)

// S18 T2 at the CLI: the two new flags cross the socket, and the listing
// prints what the daemon actually says — wire flags with their state (all
// `declared` until the probe, T3) and the third price answer, `unmetered`.

func TestRoutesSetCarriesWireFlagsAndNoPrice(t *testing.T) {
	d := startDaemon(t)

	d.mustRun("routes", "set", "--id", "flagged", "--provider", "openai", "--model", "m",
		"--capabilities", "thinking,tools", "--no-price")

	list := d.mustRun("routes", "list")
	// Canonical order, not typing order — and each flag carries its STATE
	// with it. `declared` is the honest word before any probe exists; the
	// CLI must not promote a claim because printing `verified` is shorter.
	if !strings.Contains(list, "wire flags: tools(declared) thinking(declared)") {
		t.Errorf("the listing does not print the wire flags with their states:\n%s", list)
	}
	if !strings.Contains(list, "unmetered") {
		t.Errorf("a declared-unmetered route is not listed as such (a blank would be read as free):\n%s", list)
	}

	// A flag outside the closed set is refused by name, and the refusal lists
	// what IS accepted — nobody has to remember the set from the man page.
	out, err := d.run("routes", "set", "--id", "flagged", "--provider", "openai", "--model", "m",
		"--capabilities", "vision")
	if err == nil {
		t.Fatalf("the daemon accepted a flag outside the closed set:\n%s", out)
	}
	if !strings.Contains(out+err.Error(), "vision") || !strings.Contains(out+err.Error(), "parallel-tools") {
		t.Errorf("the refusal names neither the flag nor the accepted set:\n%s\n%v", out, err)
	}
}

// TestNoPriceAndAPriceCannotBothBeGiven — two decisions in one breath are
// refused at the flag, not precedence-resolved. The reasoning-flag conflict
// above it established the idiom; this is the same mistake in pricing dress.
func TestNoPriceAndAPriceCannotBothBeGiven(t *testing.T) {
	var out bytes.Buffer
	err := cmdRoutesSet(&out, []string{
		"--id", "r", "--provider", "openai", "--model", "m",
		"--no-price", "--input-price", "3",
	})
	if err == nil {
		t.Fatal("--no-price beside --input-price was accepted")
	}
	if !strings.Contains(err.Error(), "opposite things") {
		t.Errorf("the error does not explain the conflict: %v", err)
	}
}
