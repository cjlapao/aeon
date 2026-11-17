// Package trigger is S20: a clock, and a webhook, which is the same thing
// with a different cause.
//
// The whole safety story of this package is INHERITED, not built here: G14
// pins attendedness at loop start, S11 closed the inference path, S6 suspends
// an unattended loop at its first outward action. What is genuinely new is
// four scheduling questions (overlap, reconciliation, DST, the window) and an
// HMAC check — and the discipline that a schedule FIRES A NAMED BLUEPRINT AND
// NEVER SELECTS WORK (TR1), which is what keeps this slice eight tasks
// instead of twenty. Work selection is S27's, and every boundary in this
// file exists so that fact survives its first convenient field.
package trigger

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Kind is what makes a trigger row tick. The two kinds share the row and the
// fire-record deliberately (the brief's framing: a webhook is the same thing
// with a different cause): a second table would be the same schedule twice,
// and two places to pause one.
type Kind string

const (
	// KindSchedule fires on the clock.
	KindSchedule Kind = "schedule"
	// KindWebhook fires on a verified signature. It has no expression —
	// the schema CHECK makes "it ticks on a signature, not a cron"
	// mechanical, and ValidateSchedule says it in Go too.
	KindWebhook Kind = "webhook"
)

// Schedule is one trigger row: a named blueprint, a wall-clock contract, and
// the operational state that lets it be paused. There is no column here that
// could accept a query, a filter, a tag, a priority or a readiness rule —
// not because the struct lacks ambition, but because TR1 says work selection
// is S27's, and a schema is where that survives (or doesn't) its first
// "just one convenient field". TestScheduleCannotSelectWork enforces the
// whitelist against this very struct; the store's migration 0032 is its
// physical twin.
type Schedule struct {
	// ID is the slug history, pause and fire records all name. Slug-shaped
	// on purpose: it lands in listings, and one day in a URL.
	ID string
	// Kind is schedule or webhook — the closed set, ValidateSchedule's door.
	Kind Kind
	// Blueprint is the named blueprint to fire. TR1 in one field: a
	// schedule FIRES this; it never chooses what to work on.
	Blueprint string
	// Workspace is where the fired loop works. The scheduler does not
	// guess it: a schedule that invents a directory is policy smuggled one
	// layer up, and the loop's own workspace rules (absolute, and a
	// directory — the daemon's typed refusals, S19 T7) belong to the loop.
	// Required at write, checked like the loop would check it.
	Workspace string
	// Goal is the fired loop's goal sentence. Optional: absent, the fire
	// composes one that names the schedule (a print that lies about nobody
	// starting it is the failure §1 exists to prevent).
	Goal string
	// Listens is a webhook's `event` or `event:action,action` (GitHub: `issues:opened`); empty listens to every signed delivery.
	// Schedules do not carry one. TR1 review: this decides whether a delivery is FOR this trigger, the way the window decides
	// whether now is; the trigger still fires its one named blueprint and never selects what to work on.
	Listens string
	// Expr is the cron expression (KindSchedule) and nothing (KindWebhook).
	Expr string
	// Zone is the IANA zone the expression is read in. Required — an
	// offset does not know about DST and the host's zone changes when
	// somebody moves a container (TR6).
	Zone string
	// WindowStart/WindowEnd are the optional working-hours bounds as
	// "HH:MM", wall-clock in Zone, and they exist as a PAIR (schema CHECK
	// and validator agree). Absent, a schedule runs whenever it fires —
	// "the agents worked all weekend" is a per-deployment judgement, not a
	// default (TR8).
	WindowStart string
	WindowEnd   string
	// WindowDays is an ISO weekday list ("1,2,3,4,5", Monday 1 … Sunday 7)
	// or "" for every day.
	WindowDays string
	// Paused is OPERATIONAL control (TR12): "not right now", reversible at
	// the CLI or the app, never the narrow surface. Governance is the
	// other switch — TriggerAccess, in policy, with an author and a
	// record. They are different facts and stay reported apart.
	Paused bool
	// SecretEnv NAMES the environment variable holding this webhook's
	// HMAC secret. It names it; it never holds it. A credential in a row
	// would be a credential in every backup, listing and log line of the
	// row; a credential in argv is the process list. So: the name here,
	// the value in the environment, resolved at delivery (C17's family).
	SecretEnv string
	// CoveredUntil is the reconciliation frontier (T4): the latest
	// scheduled instant already accounted for, fired or recorded. It is
	// why a week down becomes ONE catch-up and a bounded record, not ten
	// thousand rows.
	CoveredUntil string
	// CreatedAt is RFC3339, written by the author of the row.
	CreatedAt string
}

// fieldWhitelist is the schedule's field set as the boundary sees it. It is
// a SEPARATE statement from the struct above — two readings that must agree
// — because a whitelist copied from the struct by reflection would agree
// with itself no matter what fields appear, and then it is not a boundary,
// it is a mirror. The test compares the reflection over Schedule to THIS
// list, and feeds the same enumerator poisoned copies; the day a new field
// is added to the struct, the two disagree until the day's author decides,
// in review, whether the field passes TR1.
var fieldWhitelist = []string{
	"ID", "Kind", "Blueprint", "Workspace", "Goal", "Listens", "Expr", "Zone",
	"WindowStart", "WindowEnd", "WindowDays",
	"Paused", "SecretEnv", "CoveredUntil", "CreatedAt",
}

// forbiddenFieldTokens are the words a field would have to contain to be
// work selection wearing a schedule's clothes. They are matched over lower-
// cased field names; a token list, not a regex, because the risk is not
// "query" — it is the synonym added in a year that nobody thought about.
var forbiddenFieldTokens = []string{
	"query", "filter", "tag", "priorit", "ready", "select", "where",
	"roadmap", "item", "queue", "task", "label", "pick", "candidate",
	"condition", "skill", "route",
}

// ValidateFieldNames is the enumeration, exported so the test can feed it a
// poisoned struct (the narrow surface's poisoned-table pattern): an
// enumerator that has only ever seen the good table proves nothing.
func ValidateFieldNames(names []string) error {
	for _, n := range names {
		lower := strings.ToLower(n)
		for _, tok := range forbiddenFieldTokens {
			if strings.Contains(lower, tok) {
				return fmt.Errorf("schedule field %q is work selection wearing a schedule's clothes — %q names S27's ground: a schedule fires a NAMED blueprint and never selects work (TR1)", n, tok)
			}
		}
	}
	got, want := strings.Join(names, ","), strings.Join(fieldWhitelist, ",")
	if got != want {
		return fmt.Errorf("schedule fields drifted from the boundary: struct has [%s], the whitelist says [%s] — a new field is a TR1 decision made in review, never a struct edit made in passing", got, want)
	}
	return nil
}

// ValidateSchedule is the write-boundary check: an unparseable expression,
// a missing zone or a half-window is refused where it is typed, not
// discovered at the first tick. A schedule that stores cleanly and then
// never fires is the worst failure available here — nothing is wrong,
// nothing happens — so every failure mode below speaks at Add time.
func ValidateSchedule(s Schedule) error {
	if s.ID == "" {
		return errors.New(`a schedule needs an id — it is what "trigger pause", history and every fire record name`)
	}
	if !slugOK(s.ID) {
		return fmt.Errorf("schedule id %q is not a slug — letters, digits, '-' and '_', at most 64 of them", s.ID)
	}
	switch s.Kind {
	case KindSchedule:
		if s.Expr == "" {
			return fmt.Errorf("schedule %q has no cron expression — a schedule ticks; if it fires on a verified signature, that row is kind %q (TR11)", s.ID, KindWebhook)
		}
		if _, err := parseExpr(s.Expr); err != nil {
			return err
		}
		if s.Listens != "" {
			return fmt.Errorf("schedule %q is kind %q but carries an event filter %q — a filter belongs to a webhook, which is delivered events", s.ID, KindSchedule, s.Listens)
		}
		if s.SecretEnv != "" {
			return fmt.Errorf("schedule %q is kind %q but names a webhook secret — the secret belongs to the signature kind, and a field nobody fires is a field nobody retires", s.ID, KindSchedule)
		}
		if _, err := LoadZone(s.Zone); err != nil {
			return fmt.Errorf("schedule %q: %w", s.ID, err)
		}
	case KindWebhook:
		if s.Expr != "" {
			return fmt.Errorf("schedule %q is kind %q but carries expression %q — a webhook fires on a verified signature, not on a tick", s.ID, KindWebhook, s.Expr)
		}
		if s.SecretEnv == "" {
			return fmt.Errorf("webhook %q must name the environment variable holding its HMAC secret (secret-from-env) — an unauthenticated webhook is a public button to start loops, and N15 does not ship buttons", s.ID)
		}
		if !envNameOK(s.SecretEnv) {
			return fmt.Errorf("webhook %q secret-from-env %q is not an environment variable name — upper letters, digits and '_' (a path belongs in the daemon's environment, not in a row)", s.ID, s.SecretEnv)
		}
		if _, _, err := ParseEventFilter(s.Listens); err != nil {
			return fmt.Errorf("webhook %q: %w", s.ID, err)
		}
		if s.Zone != "" {
			return fmt.Errorf("webhook %q carries zone %q — the zone is the wall-clock contract of an expression, and a webhook has no expression to read in it (TR6); a field nobody fires is a field nobody retires", s.ID, s.Zone)
		}
	default:
		return fmt.Errorf("trigger kind %q is not one of %q, %q — the set is closed and a value nobody compiled is a kind nobody fires", string(s.Kind), KindSchedule, KindWebhook)
	}
	if s.Workspace == "" {
		return fmt.Errorf("schedule %q has no workspace — a fire starts a loop SOMEWHERE, and a scheduler that guesses a directory is inventing policy one layer up", s.ID)
	}
	if !strings.HasPrefix(s.Workspace, "/") {
		return fmt.Errorf("schedule %q workspace %q is not absolute — the same rule the loop itself enforces at start (S19 T7's refusal), stated here so the bad row dies at the typing and not at the 03:00 fire", s.ID, s.Workspace)
	}
	if st, err := os.Stat(s.Workspace); err != nil || !st.IsDir() {
		return fmt.Errorf("schedule %q workspace %q is not a directory the daemon can work in — refuse at write; a schedule that stores cleanly and fires into nothing is the worst failure here", s.ID, s.Workspace)
	}
	if s.Blueprint == "" {
		return fmt.Errorf("schedule %q fires nothing — a schedule fires a named blueprint (TR1); it never selects work, and it cannot fire the absence of one", s.ID)
	}
	if (s.WindowStart == "") != (s.WindowEnd == "") {
		return fmt.Errorf("schedule %q has half a window — start %q and end %q are a pair, and half a window is the kind of row that 'works' until a Sunday explains it", s.ID, s.WindowStart, s.WindowEnd)
	}
	if s.WindowStart != "" {
		start, err := parseClock(s.WindowStart)
		if err != nil {
			return fmt.Errorf("schedule %q window_start: %w", s.ID, err)
		}
		end, err := parseClock(s.WindowEnd)
		if err != nil {
			return fmt.Errorf("schedule %q window_end: %w", s.ID, err)
		}
		if start >= end {
			return fmt.Errorf("schedule %q window %s–%s does not run forward in the day — an overnight window is not offered: split it into two schedules rather than invent a midnight rule", s.ID, s.WindowStart, s.WindowEnd)
		}
		if err := validateDays(s.WindowDays); err != nil {
			return fmt.Errorf("schedule %q window_days: %w", s.ID, err)
		}
	} else if s.WindowDays != "" {
		return fmt.Errorf("schedule %q names days with no hours — a window is hours; days without them is a day off, not a window", s.ID)
	}
	return nil
}

// LoadZone reads TR6 at the door. The three refusals each name the thing the
// typer almost certainly typed, because the natural thing to type is "BST"
// or "+01:00" and neither is a zone: an offset cannot know a zone's DST
// rules, "Local" is the host's zone — the second-worst default after none —
// and a bare abbreviation ("BST") is not an IANA name; IANA names are
// Area/Location.
func LoadZone(z string) (*time.Location, error) {
	if z == "" {
		return nil, errors.New(`a schedule must name a timezone — an IANA zone like "Europe/London" or "America/Santiago". An offset ("+01:00") does not know about DST; the host's zone changes when somebody moves a container`)
	}
	if z == "Local" {
		return nil, errors.New(`zone "Local" is the host's zone — it changes when somebody moves a container, silently, in production. Name an IANA zone instead: "Europe/London"`)
	}
	if strings.HasPrefix(z, "+") || strings.HasPrefix(z, "-") {
		return nil, fmt.Errorf("zone %q is an offset, not a zone — offsets do not know about DST. Name an IANA zone instead: \"Europe/London\"", z)
	}
	if strings.HasPrefix(z, "GMT") || strings.HasPrefix(z, "UTC+") || strings.HasPrefix(z, "UTC-") {
		return nil, fmt.Errorf("zone %q is an offset wearing a zone's coat — GMT±n does not know about DST. Name an IANA zone instead: \"Europe/London\"", z)
	}
	loc, err := time.LoadLocation(z)
	if err != nil {
		return nil, fmt.Errorf("zone %q is not an IANA timezone (%v) — names are Area/Location, like \"Europe/London\" or \"America/Santiago\"; abbreviations like \"BST\" are not zones", z, err)
	}
	return loc, nil
}

// parseExpr answers Q1: five-field expressions and the named descriptors
// (@daily, @hourly, @weekly, @monthly, @midnight, @yearly) plus @every, and
// NOTHING finer than a minute. The reasoning, since every syntax feature is
// one more thing to validate, explain and get wrong:
//
//   - the seconds field is unavailable (five fields is the vocabulary), and
//     sub-minute @every is refused: a scheduler finer than a minute is a
//     queue, and queueing is S27's decision with S27's caps (N2's neighbour);
//   - @reboot is refused — a daemon restart is an event, not a schedule, and
//     a row that fires on restart is a boot-time herd with a slug;
//   - 6/7-field (quartz) forms are refused at parse; the timezone lives in
//     Zone, never in an expression extension.
//
// robfig's own parse error is quoted verbatim — the typer deserves the
// parser's words, not a paraphrase.
func parseExpr(expr string) (cron.Schedule, error) {
	if strings.Contains(strings.ToLower(expr), "@reboot") {
		return nil, fmt.Errorf("expression %q names @reboot — a daemon restart is an event, not a schedule. The catch-up already happens at startup (TR3), and a boot-time herd does not need a row to organise it", expr)
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("cron expression %q does not parse — %v — the schedule is refused at write, not discovered at the first tick", expr, err)
	}
	// Sub-minute detection is about the expression's PERIOD, not the gap
	// from this instant to the next: "*/1 * * * *" can legitimately be
	// 30 seconds from now, so the check is two successive fires (measured,
	// not assumed — the first draft of this check fired false on exactly
	// that expression).
	now := time.Now()
	next := sched.Next(now)
	if next.IsZero() {
		return nil, fmt.Errorf("cron expression %q never fires — it parses, which is how it would have stored cleanly and been silent forever", expr)
	}
	if gap := sched.Next(next).Sub(next); gap < time.Minute {
		return nil, fmt.Errorf("expression %q fires every %v — finer than a minute is a queue, not a clock (N2's neighbour); the finest grain a schedule offers is a minute", expr, gap)
	}
	return sched, nil
}

// ScheduleExpr exposes the parsed expression for the tick and reconcile
// (T2, T4) — already validated at write; a fresh parse here is the same
// pure function, never a second dialect.
func (s Schedule) ScheduleExpr() (cron.Schedule, error) {
	return parseExpr(s.Expr)
}

// InWindow is TR8's answer, and its reason is D7's "and says why" — the
// string here is what lands in trigger_fire.reason when a fire is excluded.
// t is an instant; the window speaks wall-clock in the schedule's zone, so
// the caller passes the zone LoadZone returned.
func (s Schedule) InWindow(t time.Time, loc *time.Location) (bool, string) {
	if s.WindowStart == "" {
		return true, ""
	}
	lt := t.In(loc)
	wd := int(lt.Weekday())
	if wd == 0 {
		wd = 7 // ISO: Monday 1 … Sunday 7
	}
	if s.WindowDays != "" && !dayListHas(s.WindowDays, wd) {
		return false, fmt.Sprintf("the day is outside the working-hours window (days %s, %s)", s.WindowDays, s.Zone)
	}
	start, err1 := parseClock(s.WindowStart)
	end, err2 := parseClock(s.WindowEnd)
	if err1 != nil || err2 != nil {
		// ValidateSchedule refused these at write; a row that got here
		// with an unparseable window is a bug, and the honest answer
		// for a broken window is NOT "in the window".
		return false, fmt.Sprintf("the working-hours window on schedule %q does not parse — refusing to guess it open", s.ID)
	}
	mins := lt.Hour()*60 + lt.Minute()
	if mins < start || mins >= end {
		return false, fmt.Sprintf("the hour is outside the working-hours window (%s–%s %s, %s)", s.WindowStart, s.WindowEnd, s.WindowDaysLabel(), s.Zone)
	}
	return true, ""
}

// WindowDaysLabel renders the days for a reason sentence; "" is every day.
func (s Schedule) WindowDaysLabel() string {
	if s.WindowDays == "" {
		return "every day"
	}
	return "days " + s.WindowDays
}

// slugOK is the id shape: printable, URL-safe, bounded.
func slugOK(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// envNameOK: what os/exec sets — upper letters, digits (not first) and '_'.
// Lower-case would parse as a shell variable in every runbook that quotes
// this name, so the shape is enforced, not styled.
func envNameOK(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// parseClock reads "HH:MM" into minutes-from-midnight. The form is fixed
// width so "9:00" is refused rather than guessed — a schedule should not be
// a place where formats are debated.
func parseClock(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("%q is not HH:MM — two digits, a colon, two digits (24-hour, wall-clock in the schedule's zone)", s)
	}
	h, err := strconv.Atoi(s[0:2])
	if err != nil || h > 23 {
		return 0, fmt.Errorf("%q is not a time of day — hours 00–23", s)
	}
	m, err := strconv.Atoi(s[3:5])
	if err != nil || m > 59 {
		return 0, fmt.Errorf("%q is not a time of day — minutes 00–59", s)
	}
	return h*60 + m, nil
}

// validateDays reads the ISO weekday list: "1,2,3,4,5". The list form is
// explicit because the day NAMES (mon..sun) would be a second dialect for
// the same fact.
func validateDays(s string) error {
	if s == "" {
		return nil
	}
	for _, part := range strings.Split(s, ",") {
		if len(part) != 1 || part[0] < '1' || part[0] > '7' {
			return fmt.Errorf("%q is not a weekday list — ISO numbers 1 (Monday) through 7 (Sunday), comma-separated, like \"1,2,3,4,5\"", s)
		}
	}
	return nil
}

func dayListHas(list string, day int) bool {
	want := rune('0' + day)
	for _, part := range strings.Split(list, ",") {
		if len(part) == 1 && rune(part[0]) == want {
			return true
		}
	}
	return false
}

// FieldNames is the reflection reading used by TestScheduleCannotSelectWork
// — kept here so the test asserts the enumerator itself with poisoned
// copies, and the whitelist has a second reader to disagree with.
func FieldNames(v any) []string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Name)
	}
	return out
}
