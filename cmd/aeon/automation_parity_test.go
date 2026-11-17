package main

// automation_parity_test.go — S49 T8's parity guard, BOTH directions, and
// the claim that the offline road is the SAME code: one real daemon on its
// endpoint and one fresh offline environment, the same verbs, byte-for-byte
// stdout (the actor pair and instants normalize — the differences the two
// doors are REQUIRED to have, already named by the policy door's harness).

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// automationVerbMethods maps every `aeon automation` verb and every
// `aeon trigger` verb to the socket methods it speaks. Left column pinned
// against the table by direction 1; the table's automation/trigger surface
// pinned against this map by direction 2. (The two legacy trigger reads,
// trigger.list and trigger.history, are S20's operational reads — they are
// person methods of this plane too, and T2 registered them as such.)
func automationVerbMethods() map[string][]protocol.MethodName {
	return map[string][]protocol.MethodName{
		"automation list":       {protocol.MethodAutomationList, protocol.MethodAutomationHealth}, // dots ride the health read
		"automation show":       {protocol.MethodAutomationShow},
		"automation fires":      {protocol.MethodAutomationFires},
		"automation runs":       {protocol.MethodAutomationRuns},
		"automation health":     {protocol.MethodAutomationHealth},
		"automation changes":    {protocol.MethodAutomationChanges},
		"automation vocabulary": {protocol.MethodAutomationVocabulary},
		"automation preview":    {protocol.MethodAutomationPreview},
		"automation test":       {protocol.MethodAutomationTest},
		"automation dryrun":     {protocol.MethodAutomationDryRun},
		"automation add-schedule": {protocol.MethodTriggerAdd, protocol.MethodTriggerPause}, // disarmed road = add + pause (audited pair)
		"automation add-webhook":  {protocol.MethodTriggerAdd, protocol.MethodTriggerPause},
		"automation add-event":    {protocol.MethodAutomationTriggerCreate, protocol.MethodAutomationTriggerDisable},
		"automation update":     {protocol.MethodTriggerUpdate, protocol.MethodAutomationTriggerUpdate}, // by family
		"automation enable":     {protocol.MethodAutomationTriggerEnable},
		"automation disable":    {protocol.MethodAutomationTriggerDisable},
		"automation pause":      {protocol.MethodTriggerPause},
		"automation resume":     {protocol.MethodTriggerResume},
		"automation fire-now":   {protocol.MethodTriggerFireNow},
		"automation archive":    {protocol.MethodAutomationArchive},
		"automation unarchive":  {protocol.MethodAutomationUnarchive},
		"trigger list":          {protocol.MethodTriggerList},
		"trigger history":       {protocol.MethodTriggerHistory},
		"trigger add":           {protocol.MethodTriggerAdd},
		"trigger pause":         {protocol.MethodTriggerPause},
		"trigger resume":        {protocol.MethodTriggerResume},
		"trigger fire-now":      {protocol.MethodTriggerFireNow},
	}
}

// automationPlaneMethods is the plane's whole person-method surface: the
// eleven writes and ten reads of the guard file plus S20's two operational
// reads — twenty-three, the number T2 registered and T8 consumes.
func automationPlaneMethods() []protocol.MethodName {
	out := []protocol.MethodName{protocol.MethodTriggerList, protocol.MethodTriggerHistory}
	out = append(out, automationWrites...)
	out = append(out, automationReads...)
	return out
}

// TestEveryAutomationVerbHasASocketMethod is direction 1: every verb's
// method exists on a fully wired daemon, RequiresInitialize, and is no
// Debug diagnostic (the app has the same door the CLI speaks).
func TestEveryAutomationVerbHasASocketMethod(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for verb, methods := range automationVerbMethods() {
		for _, m := range methods {
			got, ok := d.Table.Lookup(m)
			if !ok {
				t.Errorf("%s speaks %s, which is not registered — the verb would have no door", verb, m)
				continue
			}
			if !got.RequiresInitialize {
				t.Errorf("%s: %s does not RequireInitialize", verb, m)
			}
			if got.Debug {
				t.Errorf("%s: %s is Debug — a person method is not a diagnostic", verb, m)
			}
		}
	}
}

// TestEverySocketAutomationMethodHasAVerb is direction 2: nobody registers
// an automation method the terminal cannot reach (AN10's floor: no socket
// method without a CLI client), and the surface is exactly the twenty-three.
func TestEverySocketAutomationMethodHasAVerb(t *testing.T) {
	spoken := map[protocol.MethodName]bool{}
	for _, methods := range automationVerbMethods() {
		for _, m := range methods {
			spoken[m] = true
		}
	}
	d := blueprintGuardDaemon(t)
	seen := 0
	for _, m := range automationPlaneMethods() {
		if !spoken[m] {
			t.Errorf("%s has no CLI verb — AN10: the plane's methods are spoken at the terminal too", m)
		}
		if _, ok := d.Table.Lookup(m); !ok {
			t.Errorf("%s vanished from the live table", m)
		}
		seen++
	}
	if seen != 23 {
		t.Errorf("the automation plane has %d person methods, want 23 (the guard file's 11+10 plus trigger.list/history) — the parity fixture and the table drifted", seen)
	}
	// The proposal/agent-side methods are deliberately NOT in this map:
	// automation.proposeDisable is T6's tool surface, and
	// automation.trigger.list/show/fires are S40 reads whose CLI is
	// `automation fires/…` over the unified plane; pinning their ABSENCE
	// here stops this fixture silently absorbing them.
	for _, notVerb := range []protocol.MethodName{protocol.MethodAutomationProposeDisable} {
		if spoken[notVerb] {
			t.Errorf("%s is spoken by a verb — the agent-facing proposal is not a terminal authority (AU1)", notVerb)
		}
	}
}

// TestAutomationCLITableLookupsAreRegistered is the guard shape the
// blueprint plane's verb-guard keeps: the fixture's method words must be
// the LIVE table's words, checked through the table itself, not through a
// second list this file could invent.
func TestAutomationCLITableLookupsAreRegistered(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for verb, methods := range automationVerbMethods() {
		for _, m := range methods {
			if _, ok := d.Table.Lookup(m); !ok {
				t.Errorf("%s: %s is not in the live table — fixture drift", verb, m)
			}
		}
	}
}

// --- the byte parity -------------------------------------------------------------------

// TestAutomationCLISocketAndDirectDoorsProduceTheSameOutput is T8's claim
// that the offline road is the SAME code, told the blueprint way: the real
// binary, one verb, two roads, identical bytes — the fixed notice aside,
// the actor pair aside, the instants aside.
//
// fire-now and the OFFLINE event enable are the named exclusions: offline
// fire-now REFUSES by design (a loop belongs to a running daemon — T8.md
// records the sentence), and the exclusion is written down here rather
// than the comparison loosened. `preview` on an id walks instants from
// NOW; the harness already normalizes instants for that reason.
func TestAutomationCLISocketAndDirectDoorsProduceTheSameOutput(t *testing.T) {
	// The minted event ids differ per road; the harness forgives them by
	// name (a REQUIRED difference — the daemon and the offline door each
	// mint their own row), restored after this test so no other claim
	// inherits the forgiveness.
	// The socket road's actor is the OS user running the test, not a fixed 1000.
	uid := strconv.Itoa(os.Getuid())
	guard := doorNormalizations
	t.Cleanup(func() {
		doorNormalizations = guard
	})
	doorNormalizations = append(doorNormalizations,
		struct {
			re   *regexp.Regexp
			repl string
		}{regexp.MustCompile(`trg-[0-9a-z]+`), "<TRGID>"},
		// The JSON road prints the door's own typed answer, and the actor
		// pair rides inside it too — the same REQUIRED difference, in its
		// JSON spelling (the harness's text rule catches the "user:1000"
		// form; --json names it as a field).
		struct {
			re   *regexp.Regexp
			repl string
		}{regexp.MustCompile(`"createdById": "(` + uid + `|cli)"`), `"createdById": "<ACTOR>"`},
		struct {
			re   *regexp.Regexp
			repl string
		}{regexp.MustCompile(`"actorId": "(` + uid + `|cli)"`), `"actorId": "<ACTOR>"`},
		// The TEXT forms of the same pair: `created by 1000 (user)` and the
		// changes line's `1000@user` versus `cli@…`.
		struct {
			re   *regexp.Regexp
			repl string
		}{regexp.MustCompile(`created by (` + uid + `|cli) \(user\)`), `created by <ACTOR> (user)`},
		struct {
			re   *regexp.Regexp
			repl string
		}{regexp.MustCompile(`\b(` + uid + `|cli)@user\b`), `<ACTOR>@user`})

	b := newBothDoors(t)
	// The add-validator owns the workspace: a real directory both doors can
	// see (one machine, two roads — the same path is the same workspace).
	ws := t.TempDir()
	add := []string{"automation", "add-schedule", "--id", "parity-nightly", "--blueprint", "dev-loop", "--cron", "0 7 * * *", "--timezone", "UTC", "--workspace", ws}
	b.must("socket", add...)
	b.must("offline", add...)
	evAdd := []string{"automation", "add-event", "--name", "parity-event", "--match", `{"event":"task.completed"}`, "--blueprint", "dev-loop", "--max-runs-per-hour", "3", "--max-cost-per-day-usd", "2.50", "--kickoff", "check it", "--enabled", "--yes"}
	b.must("socket", evAdd...)
	b.must("offline", evAdd...)

	// Reads over the unified plane.
	b.parity(false, "automation", "list")
	b.parity(false, "automation", "list", "--source", "event")
	b.parity(false, "automation", "list", "--json")
	b.parity(false, "automation", "show", "schedule:parity-nightly")
	b.parity(false, "automation", "show", "schedule:parity-nightly", "--json")
	b.parity(false, "automation", "fires", "schedule:parity-nightly")
	b.parity(false, "automation", "health")
	b.parity(false, "automation", "health", "schedule:parity-nightly")
	b.parity(false, "automation", "changes", "schedule:parity-nightly")
	b.parity(false, "automation", "runs", "schedule:parity-nightly")
	b.parity(false, "automation", "preview", "schedule:parity-nightly")
	b.parity(false, "automation", "preview", "--cron", "*/5 * * * *", "--timezone", "UTC", "-n", "2")
	b.parity(false, "automation", "vocabulary")
	b.parity(false, "automation", "vocabulary", "--json")
	b.parity(false, "automation", "test", "--match", `{"event":"task.completed"}`, "--sample", `{"event":"task.completed","rootId":"root-a"}`)
	// S20's operational plane through the SAME door (its goldens ride).
	b.parity(false, "trigger", "list")
	b.parity(false, "trigger", "history", "parity-nightly")

	// The disarmed control road and back.
	b.parity(false, "automation", "resume", "schedule:parity-nightly")
	b.parity(false, "automation", "pause", "schedule:parity-nightly")
	// update: the change happens on both roads in the same order.
	b.parity(false, "automation", "update", "schedule:parity-nightly", "--goal", "parity goal")
	// archive carries the health it just changed.
	b.parity(false, "automation", "archive", "schedule:parity-nightly", "--reason", "parity drill")
	b.parity(false, "automation", "list", "--archived", "include")

	// Refusals, both roads, verbatim.
	b.parity(true, "automation", "show", "bogus:nope")
	b.parity(true, "automation", "show", "schedule:")
	b.parity(false, "automation", "add-schedule", "--id", "bad", "--blueprint", "dev-loop", "--cron", "not a cron", "--timezone", "UTC")
	b.parity(false, "automation", "add-schedule", "--id", "parity-nightly", "--blueprint", "dev-loop", "--cron", "0 8 * * *", "--timezone", "UTC")
	// A client-side refusal (broken JSON, the wrong door for an event) is a
	// usage error: both roads exit 1 with the same sentence on stderr.
	b.parity(true, "automation", "add-event", "--name", "bad", "--match", `{not json`, "--blueprint", "dev-loop", "--max-runs-per-hour", "1")
	b.parity(true, "automation", "pause", "event:whatever")
	b.parity(true, "automation", "enable", "schedule:parity-nightly", "--yes")
	b.parity(true, "automation", "disable", "schedule:parity-nightly", "--reason", "wrong door")
	b.parity(true, "automation", "fire-now", "parity-nightly") // seatbelt: the statement, then exit 1
	b.parity(true, "automation", "enable", "event:parity-event") // seatbelt reads the row first — id-normalized line follows
	b.parity(true, "automation", "unarchive", "schedule:parity-nightly")
	b.parity(true, "automation", "disable", "event:parity-event") // missing --reason
	// The seatbelt with --yes answers, for real (the event row's own id is
	// MINTED per road, so the armed event writes ride the spy tests; the
	// schedule row has a stable id and carries the --yes road here).
	b.parity(false, "automation", "unarchive", "schedule:parity-nightly", "--yes")
	// The fires of the event row (a disabled trigger answered for nothing):
	b.parity(false, "automation", "fires")
}

// TestAutomationCLIJSONIsTheDoorsOwnBytes pins the --json claim: the door's
// typed answer, indent-two, identical across a canned spy and the offline
// door's structs (the result types ARE the wire types, so this is a one-run
// claim the socket road shares structurally).
func TestAutomationCLIJSONIsTheDoorsOwnBytes(t *testing.T) {
	spy := &automationSpyDoor{}
	out, err := runAutomationOnSpy(t, spy, "show", "schedule:spy-nightly", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "{\n  \"automation\"") {
		t.Errorf("--json did not print the door's own typed answer with the house indent:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("%q", spyView().ID)) {
		t.Error("--json lost the id")
	}
}
