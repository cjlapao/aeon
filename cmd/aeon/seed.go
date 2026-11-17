package main

// seed.go — S43-extra T7 (SD5, SD10, P34). `aeon seed` is a CLIENT of the
// daemon's four seed.* methods; offline it is the same engine through a direct
// door (seed_door.go) and says so; any other dial fault refuses.
//
// # The CLI re-validates nothing and prints what the wire said (the drift rule)
//
// Every rule — the inert/guard gate, the divergence, the refusal — is the
// engine's, enforced in internal/seed and nowhere else. This file presents the
// engine's refusals and results VERBATIM and never re-checks: a second
// validator is a second place for the rule to drift. The one gate the CLI
// keeps is the --yes seatbelt on a guard set, which mirrors the engine's own
// class gate (it reads the class the daemon sends), not a second rule.
//
// # What the printer must never hide
//
// status shows set id, class, available vs applied version and counts by
// state, and lists EVERY non-applied record with its state and the daemon's
// reason verbatim — a status that hid a diverged or a failed record would be a
// confident wrong answer. It prints no record CONTENT (ids, versions, states
// only), because content is the operator's and the seed is the engine's
// business. The --json flag is the wire result verbatim.
//
// No credential-bearing flag exists; a seed is records, not a secret.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

const seedUsage = `aeon seed — the seed sets, asked of the daemon (S43-extra, P34)

  aeon seed status [--set ID] [--json]              every set: class, versions, counts, and every non-applied record with its reason
  aeon seed plan   --set ID [--key K ...] [--json]  the decisions apply would make (read-only)
  aeon seed apply  --set ID [--key K ...] [--yes] [--json]  apply; a guard set needs --yes (a seatbelt, not a lock)
  aeon seed restore --set ID --key K [--yes] [--json]  re-apply one removed/diverged/retired record (its reason names what it replaces)

A client of the daemon's four seed.* methods — the same door the app uses
(P34). Offline, with no daemon, it opens the SAME seed engine directly over
the store and says so; any other dial fault it refuses, never falling back
(the direct door is the engine, not a copy). --yes mirrors S44: a seatbelt
against a typo, not a security control. For a guard set, apply without --yes
prints the plan and "re-run with --yes" and writes nothing; inert sets never
need it. --json prints the wire result verbatim. No flag accepts a secret: a
seed is records, not a credential.`

func cmdSeed(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("seed needs a verb: status, plan, apply, restore — try \"aeon seed help\"")
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, seedUsage)
		return nil
	case "status":
		return seedStatus(w, args[1:])
	case "plan":
		return seedPlan(w, args[1:])
	case "apply":
		return seedApply(w, args[1:])
	case "restore":
		return seedRestore(w, args[1:])
	default:
		return fmt.Errorf("seed verb %q unknown — the verbs are status, plan, apply, restore", args[0])
	}
}

// seedDoorFactory is the selection the verbs speak through (S45 PM4): dial, and
// on "no daemon" open the same engine directly, the fixed line going to stderr
// (a diagnostic, not output). A package var so a test can stand in a fake door
// without standing up a store (the verb then speaks the fake).
var seedDoorFactory = func() (seedDoor, error) {
	return selectSeedDoor(context.Background(), os.Stderr)
}

// --- status ---

func seedStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("seed status", flag.ContinueOnError)
	set := fs.String("set", "", "only this set")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := seedDoorFactory()
	if err != nil {
		return err
	}
	res, err := door.status(context.Background(), *set)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderSeedStatus(w, res.Sets)
	return nil
}

// --- plan ---

func seedPlan(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("seed plan", flag.ContinueOnError)
	set := fs.String("set", "", "the set to plan")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	var keys repeatableString
	fs.Var(&keys, "key", "only these record keys (repeat)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*set) == "" {
		return fmt.Errorf("seed plan needs --set — name the seed set to plan")
	}
	door, err := seedDoorFactory()
	if err != nil {
		return err
	}
	res, err := door.plan(context.Background(), *set, []string(keys))
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderSeedPlan(w, res.Plan)
	return nil
}

// --- apply ---

func seedApply(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("seed apply", flag.ContinueOnError)
	set := fs.String("set", "", "the set to apply")
	yes := fs.Bool("yes", false, "confirm a guard-set apply (a seatbelt against a typo, not a lock)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	var keys repeatableString
	fs.Var(&keys, "key", "only these record keys (repeat)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*set) == "" {
		return fmt.Errorf("seed apply needs --set — name the seed set to apply")
	}
	door, err := seedDoorFactory()
	if err != nil {
		return err
	}
	// The --yes gate reads the set's CLASS the daemon sends (from status), not
	// a second rule: a guard set without --yes prints the plan and refuses,
	// with no seed.apply call; an inert set never needs it (SD5, mirrored).
	class, err := seedSetClass(door, *set)
	if err != nil {
		return err
	}
	if class == "guard" && !*yes {
		plan, perr := door.plan(context.Background(), *set, []string(keys))
		if perr != nil {
			return perr
		}
		renderSeedPlan(w, plan.Plan)
		// A refusal is non-zero; the plan above is the operator's to read, and
		// no seed.apply was called (the gate's teeth, TestSeedApplyGuardSetNeedsYes).
		return fmt.Errorf("guard set %s: re-run with --yes to apply", *set)
	}
	res, err := door.apply(context.Background(), *set, []string(keys))
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderSeedApply(w, res.Apply)
	return nil
}

// --- restore ---

func seedRestore(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("seed restore", flag.ContinueOnError)
	set := fs.String("set", "", "the set to restore from")
	key := fs.String("key", "", "the record key to restore")
	yes := fs.Bool("yes", false, "confirm the restore (a seatbelt against a typo, not a lock)")
	jsonFlag := fs.Bool("json", false, "print the raw result object as one JSON document")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*set) == "" {
		return fmt.Errorf("seed restore needs --set — name the seed set")
	}
	if strings.TrimSpace(*key) == "" {
		return fmt.Errorf("seed restore needs --key — name the record to restore")
	}
	door, err := seedDoorFactory()
	if err != nil {
		return err
	}
	// Restore is the explicit consent for a removable record (SD6); it is the
	// human door, so the --yes gate reads the class the same way apply does.
	class, err := seedSetClass(door, *set)
	if err != nil {
		return err
	}
	if class == "guard" && !*yes {
		return fmt.Errorf("guard set %s: re-run with --yes to restore", *set)
	}
	res, err := door.restore(context.Background(), *set, *key)
	if err != nil {
		return err
	}
	if *jsonFlag {
		return printJSON(w, res)
	}
	renderSeedApply(w, res.Apply)
	return nil
}

// seedSetClass asks the daemon the set's class (from status) so the --yes gate
// reads the class the daemon sends, never a second rule.
func seedSetClass(door seedDoor, set string) (string, error) {
	res, err := door.status(context.Background(), set)
	if err != nil {
		return "", err
	}
	for _, s := range res.Sets {
		if s.ID == set {
			return s.Class, nil
		}
	}
	return "", fmt.Errorf("seed status returned no set named %q", set)
}

// --- the printer (what it must never hide) ---

func renderSeedStatus(w io.Writer, sets []protocol.SeedSetStatus) {
	if len(sets) == 0 {
		fmt.Fprintln(w, "no seed sets declared")
		return
	}
	for _, s := range sets {
		fmt.Fprintf(w, "seed %s [%s] available v%d applied v%d\n", s.ID, s.Class, s.AvailableVersion, s.AppliedVersion)
		// counts by state, and the non-applied records listed with their reason.
		counts := map[string]int{}
		for _, r := range s.Records {
			counts[r.State]++
		}
		fmt.Fprintf(w, "  %s\n", countsLine(counts))
		// every non-applied record, with its state and the daemon's reason.
		for _, r := range s.Records {
			if r.State == "applied" {
				continue
			}
			line := fmt.Sprintf("  - %s %s", r.Key, r.State)
			if r.Reason != "" {
				line += " — " + r.Reason
			}
			fmt.Fprintln(w, line)
		}
	}
}

// countsLine is the "applied 30, diverged 3" summary the printer shows; it
// orders by a fixed sequence so the output is stable across doors.
func countsLine(counts map[string]int) string {
	order := []string{"applied", "removed", "diverged", "retired", "failed", "absent"}
	var parts []string
	seen := map[string]bool{}
	for _, st := range order {
		if n, ok := counts[st]; ok {
			parts = append(parts, fmt.Sprintf("%s %d", st, n))
			seen[st] = true
		}
	}
	for st, n := range counts { // any state the fixed order did not name
		if !seen[st] {
			parts = append(parts, fmt.Sprintf("%s %d", st, n))
		}
	}
	return strings.Join(parts, ", ")
}

func renderSeedPlan(w io.Writer, plan protocol.SeedSetPlan) {
	if plan.Refusal.Reason != "" {
		fmt.Fprintf(w, "refused: %s\n", plan.Refusal.Reason)
		return
	}
	fmt.Fprintf(w, "plan for %s:\n", plan.SetID)
	for _, d := range plan.Decisions {
		line := fmt.Sprintf("  %s %s", d.Key, d.Op)
		if d.Reason != "" {
			line += " — " + d.Reason
		}
		fmt.Fprintln(w, line)
	}
}

func renderSeedApply(w io.Writer, res protocol.SeedApplyOutcome) {
	var applied, skipped, diverged, removed int
	for _, r := range res.Records {
		if r.Reason != "" {
			skipped++
			switch r.Op {
			case "skip":
				if r.Reason == "diverged" {
					diverged++
				} else if r.Reason == "removed" {
					removed++
				}
			}
			fmt.Fprintf(w, "  %s %s — %s\n", r.Key, r.Op, r.Reason)
			continue
		}
		applied++
		fmt.Fprintf(w, "  %s %s\n", r.Key, r.Op)
	}
	fmt.Fprintf(w, "applied %d, skipped %d (%d diverged, %d removed)\n", applied, skipped, diverged, removed)
}
