// filer_test.go — the VG16 templates: a bug body that is a FIXED template
// (ids, enums, the aggregate's own one-line word) and never forge output
// — the hostile check name is filtered before it enters a title, a name
// that filters to nothing becomes the placeholder, and the refusal's
// detail is never quoted into the body.

package gatewatch

import (
	"strings"
	"testing"
)

// TestBugBodyIsAFixedTemplate — every one of the three kinds renders the
// fixed template: the ids and enums the facts carry, nothing else. A
// hostile check name (newlines, markdown, 500 chars) cannot put output
// text into a title, and the merge-refusal's detail — the only free text
// on the path — is never quoted verbatim.
func TestBugBodyIsAFixedTemplate(t *testing.T) {
	t.Run("verify failed", func(t *testing.T) {
		facts := Facts{
			Kind:          KindVerify,
			ComponentRoot: "/repo",
			ComponentSlug: "core",
			LoopSlug:      "dev-loop",
			RunID:         "run-12",
			HeadSHA:       "abc123",
			Summary:       "1 failed, 2 passed (3 gates)",
			GateName:      "go test",
			GateState:     "failed",
		}
		code := 1
		facts.ExitCode = &code
		title, body := BugFor(KindVerify, facts)
		if title != "Gate red: verify failed on core" {
			t.Errorf("title = %q", title)
		}
		for _, want := range []string{
			"Gate red: the verify gate failed.\n",
			"loop: dev-loop\n",
			"run: run-12 — `aeon verify runs show run-12`\n",
			"head: abc123\n",
			"gates: 1 failed, 2 passed (3 gates)\n",
			"gate: go test failed (exit 1)\n",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("body misses %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "reason:") {
			t.Errorf("the failed (non-exhausted) filing has no reason line:\n%s", body)
		}
	})

	t.Run("verify exhausted", func(t *testing.T) {
		facts := Facts{Kind: KindVerify, Exhausted: true, ComponentSlug: "core", Detail: "the rework round failed verify a third time"}
		title, body := BugFor(KindVerify, facts)
		if title != "Gate red: verify exhausted on core" {
			t.Errorf("title = %q", title)
		}
		if !strings.Contains(body, "the verify gate was exhausted") || !strings.Contains(body, "reason: the rework round failed verify a third time") {
			t.Errorf("body = %s — the exhaustion line and the reason must stand", body)
		}
	})

	t.Run("merge-watch red", func(t *testing.T) {
		hostile := "ci/build\n\n# INJECT `evil` <!-- rm -rf -->"
		facts := Facts{
			Kind:          KindMergeWatch,
			ComponentRoot: "/repo",
			ComponentSlug: "core",
			PR:            41,
			LoopSlug:      "dev-loop",
			MergeSHA:      "deadbeef",
			BaseRef:       "main",
			RuleRevision:  3,
			RunID:         "run-9",
			CheckName:     hostile,
			CheckReason:   "check ci/build is failure",
		}
		title, body := BugFor(KindMergeWatch, facts)
		// The hostile name filters to its legal characters only (letters,
		// digits, space, dot, underscore, hyphen, slash): the markdown,
		// the shell fragment's brackets and the newlines are gone.
		filtered := "ci/build INJECT evil -- rm -rf --"
		if !strings.Contains(title, "("+filtered+")") {
			t.Errorf("title = %q — the hostile check name must filter to %q before it enters the title", title, filtered)
		}
		for _, forbidden := range []string{"<!--", "`", "\n"} {
			if strings.Contains(title, forbidden) {
				t.Errorf("title carries %q — forge output in a title", forbidden)
			}
		}
		for _, line := range []string{
			"Gate red: a check failed on the merged sha.\n",
			"loop: dev-loop\n",
			"pr: #41\n",
			"merge: deadbeef\n",
			"base: main\n",
			"rule revision: 3\n",
			"check: " + filtered + " is failure\n",
			"reason: check ci/build is failure\n",
			"measured by run: run-9 — `aeon verify runs show run-9`\n",
		} {
			if !strings.Contains(body, line) {
				t.Errorf("body misses %q:\n%s", line, body)
			}
		}
		if strings.Contains(title, "# INJECT") {
			t.Error("untrusted markdown heading reached the title")
		}
		if strings.Contains(body, hostile) {
			t.Errorf("body quotes the hostile check name verbatim:\n%s", body)
		}
	})

	t.Run("merge-refused", func(t *testing.T) {
		// The refusal's detail is the ONLY free text on the path — and it
		// is never quoted: the body says the state ("not green"), which is
		// the fixed template's own word.
		facts := Facts{
			Kind:          KindMergeRefused,
			ComponentRoot: "/repo",
			ComponentSlug: "core",
			PR:            7,
			MergeSHA:      "cafebabe",
			CheckName:     "ci/build",
			Detail:        "PR #7 check ci/build is failure, not green — hostile free text: `malicious command`",
		}
		title, body := BugFor(KindMergeRefused, facts)
		if title != "Gate red: merge refused on core PR #7 (ci/build)" {
			t.Errorf("title = %q", title)
		}
		if !strings.Contains(body, "the merge was refused because a check is not green") {
			t.Errorf("body misses the refusal's lead:\n%s", body)
		}
		if !strings.Contains(body, "check: ci/build is not green\n") {
			t.Errorf("body misses the check's state (the refusal's own word):\n%s", body)
		}
		if strings.Contains(body, facts.Detail) || strings.Contains(body, "malicious command") {
			t.Errorf("body quotes the refusal's detail verbatim — the free text never crosses:\n%s", body)
		}
		if strings.Contains(body, "reason:") {
			t.Errorf("the refusal filing has no reason line (its detail is not a reason):\n%s", body)
		}
	})

	t.Run("an empty slug is the generic, an empty name is the placeholder", func(t *testing.T) {
		title, _ := BugFor(KindVerify, Facts{Kind: KindVerify})
		if title != "Gate red: verify failed on the component" {
			t.Errorf("title = %q, want the generic component", title)
		}
		title, body := BugFor(KindMergeWatch, Facts{Kind: KindMergeWatch, PR: 3, CheckName: "###"})
		if !strings.Contains(title, "(the-failing-check)") {
			t.Errorf("title = %q — a name that filters to nothing becomes the placeholder", title)
		}
		if !strings.Contains(body, "check: the-failing-check is failure") {
			t.Errorf("body = %s", body)
		}
	})
}

// TestSafeCheckNameIsBounded — the filter's contract: legal characters
// only, capped at 80, a deterministic placeholder on an empty result.
func TestSafeCheckNameIsBounded(t *testing.T) {
	if got := safeCheckName("ci/build"); got != "ci/build" {
		t.Errorf("plain name = %q, want it kept", got)
	}
	if got := safeCheckName(strings.Repeat("a", 500)); len(got) != 80 {
		t.Errorf("500-char name = %d chars, want the 80 cap", len(got))
	}
	if got := safeCheckName(""); got != checkNamePlaceholder {
		t.Errorf("empty name = %q, want the placeholder", got)
	}
	if got := safeCheckName("\n\t`$`"); got != checkNamePlaceholder {
		t.Errorf("all-illegal name = %q, want the placeholder", got)
	}
	// Deterministic: the same name always yields the same title, which is
	// what the dedupe's stability relies on.
	if safeCheckName("a/b\nc") != safeCheckName("a/b\nc") {
		t.Error("the filter is not deterministic")
	}
}
