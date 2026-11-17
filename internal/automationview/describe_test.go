package automationview

import (
	"strings"
	"testing"
)

func TestDescribeTable(t *testing.T) {
	cases := []struct {
		name string
		in   View
		want string
	}{
		{"daily", View{Source: "schedule", ID: "nightly", Expr: "0 3 * * *", Zone: "Europe/Lisbon",
			Blueprint: "dev-loop", Workspace: "/ws", Goal: "sweep", Enabled: true},
			`every day at 03:00 in "Europe/Lisbon"; starts blueprint "dev-loop" in "/ws" with goal "sweep"`},
		{"weekly-ordinal", View{Source: "schedule", ID: "mon", Expr: "30 8 * * 1", Zone: "America/New_York",
			Blueprint: "b", Enabled: true},
			`every Monday at 08:30 in "America/New_York"; starts blueprint "b"`},
		{"weekdays", View{Source: "schedule", ID: "w", Expr: "0 9 * * 1-5", Zone: "UTC",
			Blueprint: "b", Enabled: true},
			`weekdays at 09:00 in "UTC"; starts blueprint "b"`},
		{"window", View{Source: "schedule", ID: "w2", Expr: "0 10 * * *", Zone: "Europe/Madrid",
			WindowStart: "09:00", WindowEnd: "18:00", WindowDays: "1,2,3,4,5",
			Blueprint: "b", Enabled: true},
			`every day at 10:00 in "Europe/Madrid"; working hours Mon–Fri 09:00–18:00; starts blueprint "b"`},
		{"window-gap-days", View{Source: "schedule", ID: "w3", Expr: "0 10 * * *", Zone: "UTC",
			WindowStart: "09:00", WindowEnd: "12:00", WindowDays: "1,3,5",
			Blueprint: "b", Enabled: true},
			`every day at 10:00 in "UTC"; working hours Mon,Wed,Fri 09:00–12:00; starts blueprint "b"`},
		{"every-minutes", View{Source: "schedule", ID: "q", Expr: "*/15 * * * *", Zone: "UTC",
			Blueprint: "b", Enabled: true},
			`every 15 minutes in "UTC"; starts blueprint "b"`},
		{"raw-elsewhere", View{Source: "schedule", ID: "r", Expr: "0 0 25 12 *", Zone: "UTC",
			Blueprint: "b", Enabled: true},
			`cron "0 0 25 12 *" in "UTC"; starts blueprint "b"`},
		{"paused-appends", View{Source: "schedule", ID: "p", Expr: "0 3 * * *", Zone: "UTC",
			Blueprint: "b", Paused: true},
			`every day at 03:00 in "UTC"; starts blueprint "b" — paused`},
		{"webhook", View{Source: "webhook", ID: "hook", SecretEnv: "HOOK_SECRET",
			Blueprint: "triage", Workspace: "/w", Enabled: true},
			`on a signed POST; secret from env "HOOK_SECRET"; starts blueprint "triage" in "/w"`},
		{"event", View{Source: "event", ID: "pr", Enabled: true,
			Match:     MatchView{Event: "goal.approved", GoalType: []string{"feature", "bug"}, Component: []string{"comp-1"}},
			Blueprint: "B", MaxRunsPerHour: 5, MaxCostPerDayUSD: 2.5},
			`when "goal.approved" (component in "comp-1") (goal_type in "feature", "bug") starts blueprint "B"; at most 5 runs/hour, ≤ $2.50/day`},
		{"archived-appends", View{Source: "event", ID: "e", Archived: true, DisabledReason: "seasonal"},
			`when "" starts blueprint "" — archived: "seasonal"`},
		{"disabled-reason", View{Source: "event", ID: "h", Enabled: false, DisabledReason: "rotate first"},
			`when "" starts blueprint "" — disabled: "rotate first"`},
		{"unknown-source-refuses", View{Source: "quantum", ID: "x"},
			`unknown automation "x" (source "quantum" is not one of schedule, webhook, event)`},
	}
	for _, tc := range cases {
		got := Describe(tc.in)
		if got != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", tc.name, got, tc.want)
		}
	}
}

func TestDescribeCanonicalKeyOrder(t *testing.T) {
	// AU6 canonical order (root, component, goal_type, task_type, source_kind)
	// whatever order the caller's slices arrived in.
	got := Describe(View{Source: "event", Enabled: true, Match: MatchView{
		Event: "task.created", SourceKind: []string{"manual"}, TaskType: []string{"bug"},
	}})
	if i, j := strings.Index(got, "task_type"), strings.Index(got, "source_kind"); i > j {
		t.Errorf("keys printed out of canonical order: %s", got)
	}
}

func TestDescribeIsSingleLineAndEscapesHostileValues(t *testing.T) {
	hostile := "evil\n\u001b[31mred\u001b[0m \u202eAAA\u202c goal"
	out := Describe(View{Source: "schedule", ID: "h", Expr: "0 3 * * *",
		Zone: hostile, Blueprint: hostile, Workspace: hostile, Goal: hostile, Enabled: true})
	if strings.ContainsAny(out, "\n") {
		t.Errorf("a newline reached the sentence: %q", out)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\n"} {
		if strings.Contains(out, bad) {
			t.Errorf("raw %q reached the sentence: %q", bad, out)
		}
	}
	if !strings.Contains(out, `\n`) || !strings.Contains(out, `\u202e`) {
		t.Errorf("escapes are not visible in %q", out)
	}
	if len(out) > 400 {
		t.Errorf("the render is %d chars, the cap is 400", len(out))
	}
}

func TestDescribeLongValuesAreCapped(t *testing.T) {
	long := strings.Repeat("word ", 200)
	out := Describe(View{Source: "schedule", ID: "l", Expr: "0 3 * * *", Zone: "UTC",
		Blueprint: "b", Goal: long, Enabled: true})
	if len(out) > 400 {
		t.Fatalf("len %d over the 400 cap", len(out))
	}
	if !strings.HasSuffix(out, "…") {
		t.Errorf("a capped render must say so: …%s", out[len(out)-20:])
	}
}
