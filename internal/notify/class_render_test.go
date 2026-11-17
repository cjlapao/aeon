package notify

import (
	"strings"
	"testing"
)

// class_render_test.go — S58 T4: the template door and the renderer, in
// the small. The store-backed half of the resolver lives in the daemon
// package (TestResolverSeededRowsEqualTheOldSwitch); this file owns the
// claims that need no rows: what ValidateTemplate refuses, what Render
// fills, and what it can NEVER fill.

func TestClassTemplateAcceptsTheClosedSet(t *testing.T) {
	for _, tmpl := range []string{
		"",
		"Loop finished",
		"{loopSlug} blocked: {reasonKind}",
		"{rootSlug}/{loopSlug} — {severity} — {count}",
	} {
		if err := ValidateTemplate(tmpl, MaxTitleTemplateBytes); err != nil {
			t.Errorf("ValidateTemplate(%q) refused: %v — the closed set must all pass", tmpl, err)
		}
	}
}

func TestClassTemplateRefusesAnythingElse(t *testing.T) {
	for _, tmpl := range []string{
		"{goalText}", "{prompt}", "{rootName}", "{message}", "{x}",
		"Loop waiting: {question}",           // the question's BODY is user text (SV6)
		"{loopSlug} {reasonKind} {unknown}",  // a good list with one bad token
		"{ loopSlug }", "{LOOP_SLUG}", "{9}", // the grammar is exact
		"{{escaped}}", // there is no escape: the braces around `escaped` still name a token
	} {
		err := ValidateTemplate(tmpl, MaxTitleTemplateBytes)
		if err == nil {
			t.Errorf("ValidateTemplate(%q) was accepted — anything outside {rootSlug, loopSlug, reasonKind, severity, count} is a security refusal", tmpl)
			continue
		}
		if !strings.Contains(err.Error(), "not a placeholder") && !strings.Contains(err.Error(), "bytes") {
			t.Errorf("ValidateTemplate(%q) = %v, want a refusal naming the closed set or the cap", tmpl, err)
		}
	}
}

func TestClassTemplateCapsAreMeasured(t *testing.T) {
	pad := strings.Repeat("a", MaxTitleTemplateBytes)
	if err := ValidateTemplate(pad, MaxTitleTemplateBytes); err != nil {
		t.Errorf("the exact cap must pass: %v", err)
	}
	if err := ValidateTemplate(pad+"a", MaxTitleTemplateBytes); err == nil {
		t.Error("one byte over the title cap was accepted")
	}
	body := strings.Repeat("a", MaxBodyTemplateBytes)
	if err := ValidateTemplate(body, MaxBodyTemplateBytes); err != nil {
		t.Errorf("the exact body cap must pass: %v", err)
	}
	if err := ValidateTemplate(body+"a", MaxBodyTemplateBytes); err == nil {
		t.Error("one byte over the body cap was accepted")
	}
}

func TestClassRenderFillsTheClosedSet(t *testing.T) {
	v := Values{RootSlug: "alpha", LoopSlug: "demo", ReasonKind: "action-denied", Severity: "blocked", Count: "1"}
	got := Render("{rootSlug}/{loopSlug}: {reasonKind} ({severity}) x{count}", v)
	if want := "alpha/demo: action-denied (blocked) x1"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
	// A slug that fails the grammar renders EMPTY: a malformed value is
	// never pasted into user-visible text (the belt beside Validate).
	got = Render("{loopSlug} blocked", Values{LoopSlug: "Not A Slug!"})
	if got != " blocked" {
		t.Errorf("malformed slug rendered %q, want it blanked", got)
	}
	// A hole in Values renders empty, not the token.
	got = Render("{loopSlug}: {reasonKind}", Values{LoopSlug: "demo"})
	if got != "demo: " {
		t.Errorf("missing value rendered %q, want the token gone", got)
	}
}

func TestClassSeverityForFollowsTheInboxClasses(t *testing.T) {
	cases := []struct{ kind, variant, want string }{
		{"loop.blocked", "", "blocked"},
		{"loop.blocked", "exhaustion", "blocked"},
		{"loop.waiting", "", "needs-you"},
		{"loop.waiting", "stopped", "stopped"},
		{"question.asked", "", "needs-you"},
		{"loop.stop.requested", "", "needs-you"},
		{"budget.warn", "", "needs-you"},
		{"budget.hit", "", "needs-you"},
		{"watchdog.fired", "", "stuck"},
		{"loop.finished", "", "info"},
	}
	for _, c := range cases {
		if got := severityFor(c.kind, c.variant); got != c.want {
			t.Errorf("severityFor(%q,%q) = %q, want %q", c.kind, c.variant, got, c.want)
		}
	}
}
