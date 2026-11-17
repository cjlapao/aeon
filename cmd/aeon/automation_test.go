package main

// automation_test.go — S49 T8's CLI rules as tests: the event-trigger CLI
// that never existed, the authority seatbelt that writes nothing without
// --yes, refusals arriving in the validator's own sentences through BOTH
// doors, no path for a secret value anywhere, the store-free answers truly
// store-free, the door falling back ONLY on "no daemon", the direct door
// audited as the cli actor, and trigger.go's output pinned byte for byte
// (the S20 goldens). The two-door byte parity itself is
// automation_parity_test.go's TestAutomationSocketAndDirectDoors… claim.

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/protocol"
)

// --- the spy door: canned answers, a call log ---------------------------------------

type automationCall struct {
	method string
	param  any
}

type automationSpyDoor struct {
	calls []automationCall
	// addReason overrides the trigger.add answer (the refusal-parity test
	// feeds it the OFFLINE door's real sentence to compare bytes).
	addFound  bool
	addReason string
}

func (s *automationSpyDoor) record(m string, p any) { s.calls = append(s.calls, automationCall{m, p}) }
func (s *automationSpyDoor) called(m string) int {
	n := 0
	for _, c := range s.calls {
		if c.method == m {
			n++
		}
	}
	return n
}

func spyView() protocol.AutomationView {
	return protocol.AutomationView{
		Source: "schedule", ID: "spy-nightly", Name: "spy-nightly", Blueprint: "dev-loop",
		Expr: "0 7 * * *", Zone: "UTC", Enabled: true, CreatedByKind: "user", CreatedByID: "cli",
		CreatedAt: "2026-11-04T00:00:00Z",
	}
}

func spyHealth() protocol.AutomationHealthView {
	return protocol.AutomationHealthView{
		Source: "schedule", ID: "spy-nightly",
		Blueprint:  protocol.AutomationBlueprintHealth{State: "ok"},
		Workspace:  protocol.AutomationExists{Exists: true},
		Scope:      protocol.AutomationExists{Exists: true},
		CanFire:    true,
	}
}

func (s *automationSpyDoor) list(_ context.Context, p protocol.AutomationListParams) (protocol.AutomationListResult, error) {
	s.record("automation.list", p)
	return protocol.AutomationListResult{Automations: []protocol.AutomationView{spyView()}}, nil
}
func (s *automationSpyDoor) show(_ context.Context, p protocol.AutomationShowParams) (protocol.AutomationShowResult, error) {
	s.record("automation.show", p)
	return protocol.AutomationShowResult{
		Automation: spyView(), Description: automationview.Describe(automationview.View{Source: "schedule", ID: "spy-nightly", Expr: "0 7 * * *", Zone: "UTC", Blueprint: "dev-loop"}),
		Health:     spyHealth(),
	}, nil
}
func (s *automationSpyDoor) fires(_ context.Context, p protocol.AutomationFiresParams) (protocol.AutomationFiresResult, error) {
	s.record("automation.fires", p)
	return protocol.AutomationFiresResult{Fires: []protocol.AutomationFireView{
		{Source: "event", TriggerID: "trg-spy", FireID: 1, At: "2026-11-04T00:00:00Z", Outcome: "could-not-say", Reason: "the loop died before its first record"},
	}}, nil
}
func (s *automationSpyDoor) runs(_ context.Context, p protocol.AutomationRunsParams) (protocol.AutomationRunsResult, error) {
	s.record("automation.runs", p)
	return protocol.AutomationRunsResult{}, nil
}
func (s *automationSpyDoor) changes(_ context.Context, p protocol.AutomationChangesParams) (protocol.AutomationChangesResult, error) {
	s.record("automation.changes", p)
	return protocol.AutomationChangesResult{Changes: []protocol.AutomationChangeView{
		{ID: 1, Source: "schedule", TriggerID: "spy-nightly", Op: "pause", Summary: "paused", ActorKind: "user", ActorID: "cli", OccurredAt: "2026-11-04T00:00:00Z"},
	}}, nil
}
func (s *automationSpyDoor) health(_ context.Context, p protocol.AutomationHealthParams) (protocol.AutomationHealthResult, error) {
	s.record("automation.health", p)
	return protocol.AutomationHealthResult{Health: []protocol.AutomationHealthView{spyHealth()}, EvaluatedAt: "2026-11-04T00:00:00Z"}, nil
}
func (s *automationSpyDoor) vocabulary(context.Context) (automationview.VocabularyView, error) {
	s.record("automation.vocabulary", nil)
	return automationview.Vocabulary(), nil
}
func (s *automationSpyDoor) preview(_ context.Context, p protocol.AutomationPreviewParams) (protocol.AutomationPreviewResult, error) {
	s.record("automation.preview", p)
	return protocol.AutomationPreviewResult{Instants: []protocol.AutomationInstant{{At: "2026-11-05T07:00:00Z", Kind: "fire", Wall: "Thu 07:00"}}}, nil
}
func (s *automationSpyDoor) test(_ context.Context, p protocol.AutomationTestParams) (protocol.AutomationTestResult, error) {
	s.record("automation.test", p)
	return protocol.AutomationTestResult{Matches: true}, nil
}
func (s *automationSpyDoor) dryRun(_ context.Context, p protocol.AutomationDryRunParams) (protocol.AutomationDryRunResult, error) {
	s.record("automation.dryrun", p)
	return protocol.AutomationDryRunResult{Scanned: 7}, nil
}
func (s *automationSpyDoor) triggerList(context.Context) (protocol.TriggerListResult, error) {
	s.record("trigger.list", nil)
	return protocol.TriggerListResult{}, nil
}
func (s *automationSpyDoor) triggerHistory(_ context.Context, id string, limit int) (protocol.TriggerHistoryResult, error) {
	s.record("trigger.history", id)
	return protocol.TriggerHistoryResult{}, nil
}
func (s *automationSpyDoor) triggerAdd(_ context.Context, p protocol.TriggerAddParams) (protocol.TriggerControlResult, error) {
	s.record("trigger.add", p)
	found := s.addFound
	reason := s.addReason
	if reason == "" {
		found, reason = true, "registered — the clock owns its first answer now"
	}
	return protocol.TriggerControlResult{TriggerID: p.ID, Found: found, Reason: reason}, nil
}
func (s *automationSpyDoor) triggerUpdate(_ context.Context, p protocol.TriggerUpdateParams) (protocol.TriggerUpdateResult, error) {
	s.record("trigger.update", p)
	after := spyView()
	return protocol.TriggerUpdateResult{Before: spyView(), After: after, Changed: true}, nil
}
func (s *automationSpyDoor) triggerPause(_ context.Context, id string) (protocol.TriggerControlResult, error) {
	s.record("trigger.pause", id)
	return protocol.TriggerControlResult{TriggerID: id, Found: true, Paused: true, Reason: "paused — the clock will skip this trigger until it is resumed"}, nil
}
func (s *automationSpyDoor) triggerResume(_ context.Context, id string) (protocol.TriggerControlResult, error) {
	s.record("trigger.resume", id)
	return protocol.TriggerControlResult{TriggerID: id, Found: true, Reason: "resumed — the clock owns the next answer now"}, nil
}
func (s *automationSpyDoor) triggerFireNow(_ context.Context, id string) (protocol.TriggerControlResult, error) {
	s.record("trigger.fire-now", id)
	return protocol.TriggerControlResult{TriggerID: id, Found: true, Outcome: "fired", LoopID: "lp-spy"}, nil
}
func (s *automationSpyDoor) eventCreate(_ context.Context, p protocol.EventTriggerCreateParams) (protocol.EventTriggerView, error) {
	s.record("automation.trigger.create", p)
	return spyEventView(true, ""), nil
}
func (s *automationSpyDoor) eventUpdate(_ context.Context, p protocol.EventTriggerUpdateParams) (protocol.EventTriggerView, error) {
	s.record("automation.trigger.update", p)
	v := spyEventView(true, "")
	v.Name = "spy-renamed"
	return v, nil
}
func (s *automationSpyDoor) eventEnable(_ context.Context, id string) (protocol.EventTriggerView, error) {
	s.record("automation.trigger.enable", id)
	return spyEventView(true, ""), nil
}
func (s *automationSpyDoor) eventDisable(_ context.Context, id, reason string) (protocol.EventTriggerView, error) {
	s.record("automation.trigger.disable", id)
	return spyEventView(false, reason), nil
}
func (s *automationSpyDoor) archive(_ context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	s.record("automation.archive", p)
	after := spyView()
	after.Archived, after.Paused, after.ArchivedReason = true, true, p.Reason
	h := spyHealth()
	h.CanFire, h.Reasons = false, []string{"archived — the engine will not reach it"}
	return protocol.AutomationArchiveResult{Before: spyView(), After: after, Changed: true, Health: h}, nil
}
func (s *automationSpyDoor) unarchive(_ context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	s.record("automation.unarchive", p)
	after := spyView()
	after.Paused = true
	return protocol.AutomationArchiveResult{Before: spyView(), After: after, Changed: true, Health: spyHealth()}, nil
}

func spyEventView(enabled bool, disabledReason string) protocol.EventTriggerView {
	return protocol.EventTriggerView{
		ID: "trg-spy", Name: "spy-event", Source: "event", Blueprint: "dev-loop",
		Match: json.RawMessage(`{"event":"task.completed"}`), MaxRunsPerHour: 2,
		Enabled: enabled, DisabledReason: disabledReason,
		CreatedByKind: "user", CreatedByID: "cli", CreatedAt: "2026-11-04T00:00:00Z",
	}
}

// runAutomationOnSpy answers a verb through runAutomationVerb with the spy
// bolted in — the gate arms (seatbelts, reason requirements) all live
// inside the verbs, so the spy sees exactly what the real door would.
func runAutomationOnSpy(t *testing.T, s *automationSpyDoor, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := runAutomationVerb(&out, s, args[0], args[1:])
	return out.String(), err
}

// --- the parity of METHOD names (lives in the parity file's fixture) ------------------

// (TestEveryAutomationVerbHasASocketMethod / TestEverySocketAutomationMethodHasAVerb
// are in automation_parity_test.go against the live table.)

// --- the goldens ----------------------------------------------------------------------

func TestTriggerCLIGoldensUnchanged(t *testing.T) {
	b, err := os.ReadFile("trigger.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, golden := range []string{
		`fmt.Fprintf(w, "%-6s %-20s %-9s %s\n", state, t.ID, t.Kind, t.Blueprint)`,
		`fmt.Fprintf(w, "        cron %s in %s — next: %s\n", t.Expr, t.Zone, t.NextFire)`,
		`fmt.Fprintln(w, "no triggers — the clock has nothing to answer (aeon trigger --help shows how to think about this)")`,
		`fmt.Fprint(w, "\nPAUSED = operational (undo it with resume) · governance is policy: aeon policy …\n")`,
		`fmt.Fprintf(w, "refused: %s\n", res.Reason)`,
		`fmt.Fprintf(w, "%s: %s\n", res.TriggerID, res.Reason)`,
		`fmt.Fprintf(w, "%s: %s — %s\n", verb, res.TriggerID, res.Reason)`,
		`fmt.Fprintf(w, "fire-now %s: %s%s%s\n", res.TriggerID, res.Outcome, dash, res.Reason)`,
		`fmt.Fprintf(w, "fire-now %s: %s%s%s (loop %s, started as a trigger: %s)\n", res.TriggerID, res.Outcome, dash, res.Reason, res.LoopID, hand)`,
		`fmt.Fprintf(w, "%s: no fire records — every instant so far is still owed an answer (reconcile speaks at startup)\n", id)`,
		`fmt.Fprint(w, "\nfired / did not fire / could not say / skipped / refused — the five answers, recorded (TR4)\n")`,
		`aeon trigger pause  <id>          operational off — "not right now" (TR12).`,
		`fmt.Fprintln(w, "usage: aeon trigger fire-now <id>   (it fires AS A TRIGGER — unattended, by construction)")`,
	} {
		if !strings.Contains(src, golden) {
			t.Errorf("trigger.go's output changed — golden missing:\n%s", golden)
		}
	}
}

// --- the CLI that never existed --------------------------------------------------------

func TestEventTriggersNowHaveACLI(t *testing.T) {
	spy := &automationSpyDoor{}
	out, err := runAutomationOnSpy(t, spy,
		"add-event", "--name", "evt-p", "--match", `{"event":"task.completed"}`, "--blueprint", "dev-loop", "--max-runs-per-hour", "2")
	if err != nil {
		t.Fatalf("add-event: %v (%s)", err, out)
	}
	if spy.called("automation.trigger.create") != 1 || spy.called("automation.trigger.disable") != 1 {
		t.Fatalf("add-event WITHOUT --enabled answered %+#v — it must create, then immediately disable through the same door (no --enabled arms nothing)", spy.calls)
	}
	if !strings.Contains(out, "event:trg-spy spy-event — disabled") || !strings.Contains(out, "created disarmed by automation add-event without --enabled") {
		t.Errorf("the disarmed creation printed %q — it must read as disabled, with the reason", out)
	}

	spy = &automationSpyDoor{}
	out, err = runAutomationOnSpy(t, spy,
		"add-event", "--name", "evt-p", "--match", `{"event":"task.completed"}`, "--blueprint", "dev-loop", "--max-runs-per-hour", "2", "--enabled", "--yes")
	if err != nil {
		t.Fatalf("add-event --enabled --yes: %v (%s)", err, out)
	}
	if spy.called("automation.trigger.create") != 1 || spy.called("automation.trigger.disable") != 0 {
		t.Fatalf("add-event --enabled --yes answered %+#v — armed at creation, ONE write", spy.calls)
	}
	// The disarmed creation carried the reason; the armed one does not.
	if !strings.Contains(out, "armed") {
		t.Errorf("a freshly armed trigger printed %q — it must read armed-and-never-fired, not proven", out)
	}

	spy = &automationSpyDoor{}
	if _, err := runAutomationOnSpy(t, spy, "enable", "event:trg-spy", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAutomationOnSpy(t, spy, "disable", "event:trg-spy", "--reason", "quiet season"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAutomationOnSpy(t, spy, "update", "event:trg-spy", "--name", "new-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAutomationOnSpy(t, spy, "dryrun", "event:trg-spy"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"automation.trigger.enable", "automation.trigger.disable", "automation.trigger.update", "automation.dryrun"} {
		if spy.called(m) != 1 {
			t.Errorf("%s was called %d times — event triggers NOW HAVE A CLI (T8's first line)", m, spy.called(m))
		}
	}
}

// --- the seatbelt ------------------------------------------------------------------------

func TestAuthorityVerbsNeedYes(t *testing.T) {
	line := automationview.Vocabulary().AuthorityStatement
	if !strings.Contains(line, "starts agents with no one present") {
		t.Fatal("the vocabulary's authority sentence itself changed — every seatbelt below prints it")
	}

	t.Run("enable", func(t *testing.T) {
		spy := &automationSpyDoor{}
		out, err := runAutomationOnSpy(t, spy, "enable", "event:trg-spy")
		if err == nil {
			t.Fatal("enable WITHOUT --yes wrote")
		}
		if spy.called("automation.trigger.enable") != 0 {
			t.Error("enable without --yes called the write")
		}
		if !strings.Contains(out, "starts agents with no one present") || !strings.Contains(out, "re-run with --yes") {
			t.Errorf("enable without --yes printed %q — the daemon's statement + description + health + the word yes", out)
		}
	})
	t.Run("fire-now", func(t *testing.T) {
		spy := &automationSpyDoor{}
		out, err := runAutomationOnSpy(t, spy, "fire-now", "spy-nightly")
		if err == nil || spy.called("trigger.fire-now") != 0 {
			t.Fatalf("fire-now without --yes wrote (err %v, calls %+#v)", err, spy.calls)
		}
		if !strings.Contains(out, "starts agents with no one present") {
			t.Errorf("fire-now's seatbelt printed %q", out)
		}
	})
	t.Run("unarchive", func(t *testing.T) {
		spy := &automationSpyDoor{}
		out, err := runAutomationOnSpy(t, spy, "unarchive", "schedule:spy-nightly")
		if err == nil || spy.called("automation.unarchive") != 0 {
			t.Fatalf("unarchive without --yes wrote (err %v, calls %+#v)", err, spy.calls)
		}
		if !strings.Contains(out, "re-run with --yes") || !strings.Contains(out, "health now") {
			t.Errorf("unarchive's seatbelt printed %q — statement, health, and the yes word", out)
		}
	})
	t.Run("add --enabled", func(t *testing.T) {
		spy := &automationSpyDoor{}
		out, err := runAutomationOnSpy(t, spy, "add-schedule", "--id", "armed-direct", "--blueprint", "dev-loop", "--cron", "0 7 * * *", "--timezone", "UTC", "--enabled")
		if err == nil || spy.called("trigger.add") != 0 {
			t.Fatalf("add-schedule --enabled WITHOUT --yes wrote (err %v, calls %+#v)", err, spy.calls)
		}
		if !strings.Contains(out, "every day at 07:00") || !strings.Contains(out, "no health yet") {
			t.Errorf("the add seatbelt printed %q — the DRAFT's description and the honest no-row health", out)
		}
		spy = &automationSpyDoor{}
		if _, err := runAutomationOnSpy(t, spy, "add-event", "--name", "e", "--match", `{"event":"x.y"}`, "--blueprint", "b", "--enabled"); err == nil || spy.called("automation.trigger.create") != 0 {
			t.Fatalf("add-event --enabled without --yes wrote (err %v, calls %+#v)", err, spy.calls)
		}
		spy = &automationSpyDoor{}
		if _, err := runAutomationOnSpy(t, spy, "add-webhook", "--id", "wh", "--blueprint", "dev-loop", "--secret-env", "AEON_S", "--enabled"); err == nil || spy.called("trigger.add") != 0 {
			t.Fatalf("add-webhook --enabled without --yes wrote (err %v, calls %+#v)", err, spy.calls)
		}
	})
}

func TestDisablePauseArchiveNeedReasonWhereRequired(t *testing.T) {
	spy := &automationSpyDoor{}
	if _, err := runAutomationOnSpy(t, spy, "disable", "event:trg-spy"); err == nil || spy.called("automation.trigger.disable") != 0 {
		t.Errorf("disable without --reason passed (err %v) — AN3's narrowing needs its sentence", err)
	}
	if _, err := runAutomationOnSpy(t, spy, "archive", "schedule:spy-nightly"); err == nil || spy.called("automation.archive") != 0 {
		t.Errorf("archive without --reason passed (err %v)", err)
	}
	if _, err := runAutomationOnSpy(t, spy, "disable", "event:trg-spy", "--reason", "   "); err == nil {
		t.Error("a whitespace reason is no reason")
	}
	if _, err := runAutomationOnSpy(t, spy, "pause", "event:trg-spy"); err == nil || spy.called("trigger.pause") != 0 {
		t.Errorf("pause on an event passed (err %v) — events are disabled, not paused (AN6)", err)
	}
	if _, err := runAutomationOnSpy(t, spy, "enable", "schedule:spy-nightly", "--yes"); err == nil || spy.called("automation.trigger.enable") != 0 {
		t.Errorf("enable on a schedule passed (err %v) — clock rows are resumed, not enabled", err)
	}
	if _, err := runAutomationOnSpy(t, spy, "pause", "schedule:spy-nightly"); err != nil || spy.called("trigger.pause") != 1 {
		t.Errorf("pause (no reason needed) failed: err %v calls %+#v", err, spy.calls)
	}
}

// --- the honest refusals ------------------------------------------------------------------

func TestNoSecretValueFlagOrPathExists(t *testing.T) {
	spy := &automationSpyDoor{}
	// C17: there is no flag for a secret VALUE. Every shape a person might
	// reach for is refused at parsing, before any door.
	for _, bad := range []string{"--secret", "--secret-value", "--secretValue", "--token", "--password", "--hmac"} {
		if _, err := runAutomationOnSpy(t, spy, "add-webhook", "--id", "wh", "--blueprint", "dev-loop", bad, "hunter2"); err == nil {
			t.Errorf("add-webhook accepted %q — a value flag must not exist", bad)
		}
	}
	if len(spy.calls) != 0 {
		t.Errorf("a refused flag still reached the door: %+#v", spy.calls)
	}
	if _, err := runAutomationOnSpy(t, spy, "add-webhook", "--id", "wh", "--blueprint", "dev-loop", "--secret-env", "AEON_CI_SECRET"); err != nil {
		t.Errorf("the NAME flag must work: %v", err)
	}
	// And no path READS a value (stdin or file): the source scan.
	for _, file := range []string{"automation_cmd.go", "automation_add.go", "automation_flags.go", "automation_client.go", "automation_print.go"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, banned := range []string{`"secret"`, `"secret-value"`, `"secretValue"`, `os.Stdin`} {
			if strings.Contains(src, banned) {
				t.Errorf("%s carries %q — no path, flag, prompt or stdin may hold a secret value (C17)", file, banned)
			}
		}
		if !strings.Contains(src, "secret-env") && (file == "automation_cmd.go" || file == "automation_add.go") {
			t.Error("automation_cmd.go lost --secret-env itself")
		}
	}
}

func TestPrinterNeverShowsAnEnvValueAndKeepsCouldNotSay(t *testing.T) {
	// AN1/C17: the health line prints the env NAME and whether it is set —
	// a value could not appear even if one were planted, because no field
	// carries one; this test pins what the printers DO print.
	h := spyHealth()
	h.Source, h.ID = "webhook", "wh-ci"
	h.Webhook = &protocol.AutomationSecretHealth{EnvName: "AEON_CI_SECRET", Set: true}
	h.CanFire, h.Reasons = false, []string{"secret env AEON_CI_SECRET is not set in the daemon's environment"}
	var out strings.Builder
	printAutomationHealth(&out, protocol.AutomationHealthResult{Health: []protocol.AutomationHealthView{h}})
	got := out.String()
	if !strings.Contains(got, "AEON_CI_SECRET: set") {
		t.Errorf("the webhook health line lost the NAME/present shape: %q", got)
	}
	if strings.Contains(got, "hunter2") || regexp.MustCompile(`(?i)secret value`).MatchString(got) {
		t.Errorf("a secret VALUE-shaped rendering appeared: %q", got)
	}

	// AN16's fifth outcome survives verbatim into the fire printing.
	var f strings.Builder
	printAutomationFires(&f, protocol.AutomationFiresResult{
		Fires:  []protocol.AutomationFireView{{Source: "schedule", TriggerID: "x", At: "T", Outcome: "could-not-say", Reason: "the daemon fell between start and record"}},
		Counts: protocol.AutomationFireCounts{CouldNotSay: 1},
	})
	if !strings.Contains(f.String(), "could-not-say") || !strings.Contains(f.String(), "could-not-say 1") {
		t.Errorf("could-not-say was smoothed away: %q", f.String())
	}
}

// --- the store-free answers ------------------------------------------------------------------

func TestPreviewAndTestWorkOfflineWithoutAStore(t *testing.T) {
	// A state dir that CANNOT open a store, and a dial that panics: the
	// store-free answers must answer anyway — they reach neither door.
	blocked := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEON_STATE_DIR", blocked)
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(blocked, "nothing.sock"))
	restoreDial := automationDial
	automationDial = func(int) (*protocol.Client, error) { panic("the store-free answers must never dial") }
	t.Cleanup(func() { automationDial = restoreDial })
	var errs strings.Builder
	restoreErr := automationErrOut
	automationErrOut = &errs
	t.Cleanup(func() { automationErrOut = restoreErr })

	var out strings.Builder
	if err := cmdAutomation(&out, []string{"preview", "--cron", "*/5 * * * *", "--timezone", "UTC", "-n", "3"}); err != nil {
		t.Fatalf("preview --cron over an unopenable store: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(out.String()), "\n"); lines < 2 {
		t.Errorf("preview --cron printed %q — three instants were asked", out.String())
	}
	out.Reset()
	if err := cmdAutomation(&out, []string{"test", "--match", `{"event":"task.completed"}`, "--sample", `{"event":"task.completed"}`}); err != nil {
		t.Fatalf("test over an unopenable store: %v", err)
	}
	if !strings.Contains(out.String(), "MATCHES") {
		t.Errorf("test printed %q", out.String())
	}
	if strings.Contains(errs.String(), "applied directly to the store") {
		t.Errorf("the store-free answers announced a door they never used: %q", errs.String())
	}
}

// --- the door selection ----------------------------------------------------------------------

func automationNoDaemonErr() error {
	return fmt.Errorf("no daemon at /x/nothing.sock: dial unix: connect: no such file or directory")
}

func TestSelectAutomationDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	restore := automationDial
	t.Cleanup(func() { automationDial = restore })

	t.Run("no endpoint is the direct door, and it says the fixed line first", func(t *testing.T) {
		automationDial = func(int) (*protocol.Client, error) { return nil, automationNoDaemonErr() }
		var errOut strings.Builder
		d, err := selectAutomationDoor(&errOut)
		if err != nil {
			t.Fatalf("a missing endpoint must fall back, not refuse: %v", err)
		}
		if _, ok := d.(*automationDirectDoor); !ok {
			t.Fatalf("the fallback door is %T, not the direct door", d)
		}
		if got := errOut.String(); got != automationDirectNotice+"\n" {
			t.Errorf("the fallback said %q, want the fixed line", got)
		}
	})
	t.Run("a permission refusal REFUSES", func(t *testing.T) {
		automationDial = func(int) (*protocol.Client, error) {
			return nil, &net.OpError{Op: "dial", Net: "unix", Err: os.ErrPermission, Addr: &net.UnixAddr{Name: "/x/aeon.sock", Net: "unix"}}
		}
		var errOut strings.Builder
		if _, err := selectAutomationDoor(&errOut); err == nil {
			t.Fatal("a locked-out socket fell back — the CLI routed around a daemon that exists")
		} else if errOut.Len() != 0 {
			t.Errorf("a refusal printed the fallback notice: %q", errOut.String())
		}
	})
	t.Run("a live daemon is the socket door, silent", func(t *testing.T) {
		f := startDaemon(t)
		t.Setenv("LOCALRPC_ENDPOINT_PATH", f.endpoint)
		t.Setenv("DATABASE_SQLITE_STORAGE_PATH", "")
		t.Setenv("DATABASE_SQLITE_FILE_NAME", "")
		t.Setenv("DATABASE_SQLITE_DSN", "")
		automationDial = dial
		var errOut strings.Builder
		d, err := selectAutomationDoor(&errOut)
		if err != nil {
			t.Fatalf("the live daemon refused the dial: %v", err)
		}
		if _, ok := d.(*automationSocketDoor); !ok {
			t.Fatalf("the live road answered with %T, not the socket door", d)
		}
		if errOut.Len() != 0 {
			t.Errorf("the socket road said %q — it says nothing", errOut.String())
		}
	})
}

// --- the one-implementation guards -------------------------------------------------------------

func TestDirectDoorUsesTheSameService(t *testing.T) {
	b, err := os.ReadFile("automation_client.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	// The door must REACH the daemon's services — these constructors are
	// the only roads to the store's automation writes.
	for _, need := range []string{
		"daemon.NewAutomationReadDoors(", "daemon.NewAutomationWriteDoors(",
		"daemon.NewAutomationControlDoors(", "daemon.NewTriggerWrites(",
		"daemon.SeedStandardBlueprints(", "daemon.PreviewDraftSpec",
	} {
		if !strings.Contains(src, need) {
			t.Errorf("automation_client.go no longer calls %s — the offline door is losing the shared service", need)
		}
	}
	// And must hold NO local copy of the plane's logic.
	for _, banned := range []string{"trigger.NewScheduler", "automationRows{", "SaveTriggerSchedule", "SetCovered"} {
		if strings.Contains(src, banned) {
			t.Errorf("automation_client.go carries %q — a second scheduler is being born beside the daemon's", banned)
		}
	}
}

func TestAutomationCLINeverImportsStoreOutsideDirectDoor(t *testing.T) {
	files, err := filepath.Glob("automation*.go")
	if err != nil || len(files) < 4 {
		t.Fatalf("the glob saw %d automation files (%v) — the pattern is wrong, not the code", len(files), err)
	}
	// automation_client.go is the one DOOR; automation_proposal_seams.go is
	// T6's proposal-desk registration — daemon wiring that reaches the
	// daemon's own write services, not a verb printing anything.
	allowed := map[string]bool{"automation_client.go": true, "automation_proposal_seams.go": true}
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue // the tests may seed rows however they like
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value != `"github.com/cjlapao/aeon/internal/store"` {
				continue
			}
			if !allowed[filepath.Base(path)] {
				t.Errorf("%s imports internal/store — only the direct door may open a store; the verbs speak the door", filepath.Base(path))
			}
		}
	}
	if checked < 4 {
		t.Fatalf("only %d non-test automation CLI files were checked", checked)
	}
}

// TestAutomationDirectDoorActorIsCliAudited reads the audited actor back from real
// rows: the offline writes land as actor_kind='user' (the frozen CHECK
// admits nothing else — AU1's lock) with the CLI's identity in
// actor_id='cli'. The T8.md shorthand {cli,cli} is refused BY that lock;
// the task Notes carry the resolution.
func TestAutomationDirectDoorActorIsCliAudited(t *testing.T) {
	if automationCLIActor.Kind != "user" || automationCLIActor.ID != "cli" {
		t.Fatalf("the offline actor is %+#v — design §28 says {user, cli}; the store's CHECK locks kind='user'", automationCLIActor)
	}
	state := t.TempDir()
	t.Setenv("AEON_STATE_DIR", state)
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(state, "nothing.sock"))
	restore := automationDial
	automationDial = func(int) (*protocol.Client, error) { return nil, automationNoDaemonErr() }
	t.Cleanup(func() { automationDial = restore })
	var quiet strings.Builder
	door, err := selectAutomationDoor(&quiet)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view, err := door.eventCreate(ctx, protocol.EventTriggerCreateParams{
		Name: "actor-check", Match: json.RawMessage(`{"event":"task.completed"}`), Blueprint: "dev-loop", MaxRunsPerHour: 2,
	})
	if err != nil {
		t.Fatalf("offline event create: %v", err)
	}
	if _, err := door.triggerAdd(ctx, protocol.TriggerAddParams{
		ID: "actor-sched", Blueprint: "dev-loop", Expr: "0 7 * * *", Zone: "UTC", Workspace: t.TempDir(),
	}); err != nil {
		t.Fatalf("offline trigger add: %v", err)
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	check := func(source, id string) {
		cs, err := st.AutomationChangeList(context.Background(), source, id, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) == 0 {
			t.Fatalf("%s:%s audited NOTHING — an offline write without an audit line is AU1's failure", source, id)
		}
		for _, c := range cs {
			if c.ActorID != "cli" {
				t.Errorf("%s %s audited actor_id=%q, want cli", source, c.Op, c.ActorID)
			}
			if c.ActorKind != "user" {
				t.Errorf("%s %s audited actor_kind=%q — the frozen CHECK admits user only", source, c.Op, c.ActorKind)
			}
		}
	}
	check("event", view.ID)
	check("schedule", "actor-sched")
}

// --- cross-door refusal parity (the validators speak once) -------------------------------

func TestAddRefusalsAreTheValidatorsVerbatimBothDoors(t *testing.T) {
	// The offline road FIRST, to capture the validator's real sentence…
	state := t.TempDir()
	t.Setenv("AEON_STATE_DIR", state)
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(state, "nothing.sock"))
	restore := automationDial
	automationDial = func(int) (*protocol.Client, error) { return nil, automationNoDaemonErr() }
	t.Cleanup(func() { automationDial = restore })
	var quiet strings.Builder
	door, err := selectAutomationDoor(&quiet)
	if err != nil {
		t.Fatal(err)
	}
	var offlineOut strings.Builder
	if err := runAutomationVerb(&offlineOut, door, "add-schedule", []string{
		"--id", "bad-cron", "--blueprint", "dev-loop", "--cron", "this is not a cron", "--timezone", "UTC"}); err != nil {
		t.Fatalf("a refused add must still exit 0 printing the refusal: %v", err)
	}
	sentence := strings.TrimPrefix(offlineOut.String(), "refused: ")
	if sentence == offlineOut.String() || !strings.Contains(sentence, "cron") {
		t.Fatalf("the offline refusal was %q — the validator's own sentence, verbatim", offlineOut.String())
	}
	// …then the socket door's presentation with THAT sentence canned in:
	// the printed bytes must be identical, because one printer serves both.
	spy := &automationSpyDoor{addReason: strings.TrimSuffix(sentence, "\n")}
	var socketOut strings.Builder
	if err := runAutomationVerb(&socketOut, spy, "add-schedule", []string{
		"--id", "bad-cron", "--blueprint", "dev-loop", "--cron", "this is not a cron", "--timezone", "UTC"}); err != nil {
		t.Fatal(err)
	}
	if socketOut.String() != offlineOut.String() {
		t.Errorf("the two doors printed a refusal differently:\n socket: %q\n offline: %q", socketOut.String(), offlineOut.String())
	}
	// The daemon's read/write services are the source — the CLI re-validates
	// NOTHING about cron: the sentence above came through untouched.
	var out strings.Builder
	err = runAutomationVerb(&out, spy, "add-schedule",
		[]string{"--id", "x", "--blueprint", "dev-loop", "--cron", "nonsense", "--timezone", "Mars/Olympus"})
	if err != nil || !strings.HasPrefix(out.String(), "refused: ") {
		t.Errorf("the CLI started re-validating (err %v, out %q) — the validators own cron and zone, verbatim", err, out.String())
	}
}

// keep the imports this file needs alive through the file's own guards
