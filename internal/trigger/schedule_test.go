package trigger

import (
	"os"
	"strings"
	"testing"
	"time"
)

// valid is the reference schedule every negative test starts from, so each
// failure names exactly one edited field.
var testWS string

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "aeon-trigger-ws")
	if err != nil {
		panic(err)
	}
	testWS = d
	os.Exit(m.Run())
}

func valid() Schedule {
	return Schedule{
		ID: "nightly-survey", Kind: KindSchedule, Blueprint: "dev-loop",
		Workspace: testWS,
		Expr:      "30 3 * * *", Zone: "Europe/London", CreatedAt: "2026-10-27T09:00:00Z",
	}
}

func wantRefusal(t *testing.T, s Schedule, must ...string) {
	t.Helper()
	err := ValidateSchedule(s)
	if err == nil {
		t.Fatalf("the row stored cleanly; expected a refusal naming %v", must)
	}
	for _, m := range must {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("refusal %q does not name %q", err, m)
		}
	}
}

// ── the TR1 boundary ────────────────────────────────────────────────────────

// TestScheduleCannotSelectWork is TR1 as a test. Two readings of the field
// set must agree (the struct by reflection, the whitelist by hand — a
// whitelist derived from the struct would agree with itself no matter what
// appeared, which is a mirror and not a boundary), and the same enumerator
// must catch the poisoned tables the risk actually looks like: not "query"
// (everyone forbids query), but "candidate", "ready", the synonym added in a
// year that nobody thought about.
func TestScheduleCannotSelectWork(t *testing.T) {
	if err := ValidateFieldNames(FieldNames(Schedule{})); err != nil {
		t.Fatalf("the honest table fails its own enumerator: %v", err)
	}
	poisons := [][]string{
		append(FieldNames(Schedule{}), "Query"),
		append(FieldNames(Schedule{}), "ReadyPredicate"),
		append(FieldNames(Schedule{}), "CandidateFilter"),
		append(FieldNames(Schedule{}), "Priority"),
		append(FieldNames(Schedule{}), "RoadmapItem"),
		append(FieldNames(Schedule{}), "TaskSelect"),
	}
	for _, p := range poisons {
		if err := ValidateFieldNames(p); err == nil {
			t.Errorf("a schedule grew work selection (%v) and nobody noticed", p[len(p)-1])
		}
	}
	// And drift without a forbidden word is still drift: a harmless-looking
	// field is still a TR1 decision that has to be made in review.
	drift := append(FieldNames(Schedule{}), "Owner")
	if err := ValidateFieldNames(drift); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Errorf("a silent new field passed the whitelist: %v", drift)
	}
}

func TestScheduleNamesABlueprint(t *testing.T) {
	s := valid()
	s.Blueprint = ""
	wantRefusal(t, s, "fires nothing", "named blueprint", "TR1")
}

// ── TR6: the zone ───────────────────────────────────────────────────────────

func TestScheduleWithoutZoneRefused(t *testing.T) {
	s := valid()
	s.Zone = ""
	wantRefusal(t, s, "must name a timezone", "Europe/London")
}

func TestOffsetRefusedAsTimezone(t *testing.T) {
	for _, z := range []string{"+01:00", "-0800", "GMT+1"} {
		s := valid()
		s.Zone = z
		wantRefusal(t, s, "offset", "DST")
	}
	s := valid()
	s.Zone = "Local"
	wantRefusal(t, s, "host's zone", "IANA")
	s = valid()
	s.Zone = "BST"
	wantRefusal(t, s, "not an IANA timezone")
	// And the good ones load — UTC included; it is a zone, not an excuse.
	for _, z := range []string{"Europe/London", "America/Santiago", "UTC"} {
		s := valid()
		s.Zone = z
		if err := ValidateSchedule(s); err != nil {
			t.Errorf("zone %q refused: %v", z, err)
		}
	}
}

// ── Q1 + write-time refusal ────────────────────────────────────────────────

func TestUnparseableExpressionRefusedAtWrite(t *testing.T) {
	s := valid()
	s.Expr = "definitely not cron"
	wantRefusal(t, s, "does not parse", "refused at write")

	s = valid()
	s.Expr = "@reboot"
	wantRefusal(t, s, "@reboot", "event, not a schedule")

	s = valid()
	s.Expr = "@every 30s"
	wantRefusal(t, s, "finer than a minute", "queue")

	s = valid()
	s.Expr = "*/1 * * * * *" // the quartz-shaped temptation
	wantRefusal(t, s, "does not parse")

	s = valid()
	s.Expr = ""
	wantRefusal(t, s, "no cron expression", "webhook")
}

func TestExpressionsThatPass(t *testing.T) {
	for _, e := range []string{"*/1 * * * *", "30 3 * * *", "@daily", "@hourly", "@weekly", "@every 1h", "@every 90m"} {
		s := valid()
		s.Expr = e
		if err := ValidateSchedule(s); err != nil {
			t.Errorf("expression %q refused: %v", e, err)
		}
	}
}

// ── TR8: the window ─────────────────────────────────────────────────────────

func TestScheduleWithNoWindowRunsWhenever(t *testing.T) {
	s := valid()
	s.WindowStart, s.WindowEnd, s.WindowDays = "", "", ""
	loc, err := LoadZone(s.Zone)
	if err != nil {
		t.Fatal(err)
	}
	// 03:00 Sunday — the hour and the day a default-on window would bite.
	sunday := time.Date(2026, 11, 1, 3, 0, 0, 0, loc)
	ok, reason := s.InWindow(sunday, loc)
	if !ok {
		t.Errorf("an absent window excluded a fire (%q) — TR8: absent means whenever", reason)
	}
}

func TestWindowExcludesAndSaysWhy(t *testing.T) {
	s := valid()
	s.WindowStart, s.WindowEnd, s.WindowDays = "09:00", "17:00", "1,2,3,4,5"
	if err := ValidateSchedule(s); err != nil {
		t.Fatal(err)
	}
	loc, _ := LoadZone(s.Zone)
	saturday := time.Date(2026, 10, 31, 10, 0, 0, 0, loc)
	ok, reason := s.InWindow(saturday, loc)
	if ok || !strings.Contains(reason, "day is outside") || !strings.Contains(reason, "window") {
		t.Errorf("Saturday: ok=%v reason=%q", ok, reason)
	}
	tuesdayNight := time.Date(2026, 11, 3, 22, 0, 0, 0, loc)
	ok, reason = s.InWindow(tuesdayNight, loc)
	if ok || !strings.Contains(reason, "hour is outside") {
		t.Errorf("Tuesday 22:00: ok=%v reason=%q", ok, reason)
	}
	tuesdayMorning := time.Date(2026, 11, 3, 9, 0, 0, 0, loc)
	if ok, _ := s.InWindow(tuesdayMorning, loc); !ok {
		t.Errorf("Tuesday 09:00 is IN the window (the bounds are inclusive at the start)")
	}
	// 17:00 sharp is out — the end bound is exclusive, so a 09–17 window
	// is eight hours of work, not nine.
	tuesdayFive := time.Date(2026, 11, 3, 17, 0, 0, 0, loc)
	if ok, _ := s.InWindow(tuesdayFive, loc); ok {
		t.Errorf("17:00 admitted by a 09:00–17:00 window — the end is exclusive")
	}
}

func TestHalfWindowRefused(t *testing.T) {
	s := valid()
	s.WindowEnd = "17:00"
	wantRefusal(t, s, "half a window", "pair")

	s = valid()
	s.WindowStart, s.WindowEnd = "17:00", "09:00"
	wantRefusal(t, s, "does not run forward", "two schedules")

	s = valid()
	s.WindowStart, s.WindowEnd, s.WindowDays = "9:00", "17:00", ""
	wantRefusal(t, s, "HH:MM")

	s = valid()
	s.WindowStart, s.WindowEnd, s.WindowDays = "09:00", "17:00", "mon,tue"
	wantRefusal(t, s, "weekday list", "Monday")
}

// ── the kinds ──────────────────────────────────────────────────────────────

func TestWebhookKindShape(t *testing.T) {
	web := func() Schedule {
		s := valid()
		s.Kind = KindWebhook
		s.Expr = ""
		s.Zone = "" // the honest webhook shape (S20 T6): the zone belongs to an expression
		s.SecretEnv = "GH_WEBHOOK_SECRET"
		return s
	}
	if err := ValidateSchedule(web()); err != nil {
		t.Fatalf("the honest webhook row refused: %v", err)
	}
	s := web()
	s.Expr = "*/1 * * * *"
	wantRefusal(t, s, "verified signature", "not on a tick")

	s = web()
	s.SecretEnv = ""
	wantRefusal(t, s, "environment variable", "public button")

	// S20 T6, the row no test had until a webhook existed to need it: the
	// honest webhook carries NO zone (the zone is the wall-clock contract
	// of an expression — TR6 — and this kind has no expression).
	w := web()
	w.Zone = ""
	if err := ValidateSchedule(w); err != nil {
		t.Fatalf("the zoneless webhook row — the honest shape — refused: %v", err)
	}
	w = web()
	w.Zone = "Europe/London"
	wantRefusal(t, w, "carries zone", "no expression")

	s = web()
	s.SecretEnv = "gh secret /etc/passwd"
	wantRefusal(t, s, "not an environment variable name")

	s = valid() // schedule kind carrying a secret name
	s.SecretEnv = "GH_WEBHOOK_SECRET"
	wantRefusal(t, s, "kind \"schedule\" but names a webhook secret")

	s = valid()
	s.Kind = "telegram"
	wantRefusal(t, s, "not one of", "closed")
}

func TestIDShape(t *testing.T) {
	s := valid()
	s.ID = ""
	wantRefusal(t, s, "needs an id")
	s = valid()
	s.ID = "two words"
	wantRefusal(t, s, "not a slug")
	s = valid()
	s.ID = strings.Repeat("x", 65)
	wantRefusal(t, s, "not a slug")
}

func TestWorkspaceRequiredAtWrite(t *testing.T) {
	s := valid()
	s.Workspace = ""
	wantRefusal(t, s, "no workspace", "SOMEWHERE")
	s = valid()
	s.Workspace = "relative/here"
	wantRefusal(t, s, "not absolute")
	s = valid()
	s.Workspace = "/definitely/not/a/directory/aeon-walk"
	wantRefusal(t, s, "not a directory")
	// webhook kind carries it too — the fire lands a loop either way.
	w := valid()
	w.Kind = KindWebhook
	w.Expr = ""
	w.Zone = "" // the honest webhook shape (S20 T6)
	w.SecretEnv = "GH_WEBHOOK_SECRET"
	w.Workspace = ""
	wantRefusal(t, w, "no workspace")
}
