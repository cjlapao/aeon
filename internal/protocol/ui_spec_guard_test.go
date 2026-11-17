package protocol

// ui_spec_guard_test.go — S61 T11: the wizard spec is proved against the
// wire, not against memory. Every method literal the spec quotes must be
// a method the generated contract actually carries — the spec that
// drifted from the door (writing `intake.preview` for what the plane
// calls `intake.plan`) is T11's named failure, and a document nobody
// checks against the contract drifts silently.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestUISpecNamesOnlyRegisteredMethods(t *testing.T) {
	spec, err := os.ReadFile("../../docs/aeon/S61-goal-intake/ui-spec.md")
	if err != nil {
		t.Fatalf("read the ui spec: %v", err)
	}
	contract, err := os.ReadFile("../../ui/src/proto/contract.ts")
	if err != nil {
		t.Fatalf("read the generated contract: %v", err)
	}
	fileName := regexp.MustCompile(`^[A-Za-z0-9_\-]+\.(ts|tsx|md|go|json|ya?ml)$`)
	span := regexp.MustCompile("`([^`]+)`")
	head := regexp.MustCompile(`^[a-z]+\.[a-z]+`)
	seen := map[string]bool{}
	for _, m := range span.FindAllStringSubmatch(string(spec), -1) {
		lit := m[1]
		if fileName.MatchString(lit) {
			continue // file names are not methods
		}
		cand := head.FindString(lit)
		if cand == "" {
			continue
		}
		// A method candidate ends at the literal's method head: what
		// follows must NOT continue a dotted path (`wizard.publish.*`
		// is an i18n key shape, not a plane method) — but a space or a
		// brace (`intake.plan {intakeId}`) still names the method.
		rest := strings.TrimPrefix(lit, cand)
		if rest != "" && strings.HasPrefix(rest, ".") {
			continue
		}
		seen[cand] = true
	}
	if len(seen) == 0 {
		t.Fatal("the walk extracted no method literals — the spec or the walk changed shape; a guard that inspects nothing is not a guard")
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(contract), "\n") {
		for _, m := range regexp.MustCompile(`"([a-z]+\.[a-z]+)"`).FindAllStringSubmatch(line, -1) {
			have[m[1]] = true
		}
	}
	for cand := range seen {
		if !have[cand] {
			t.Errorf("ui-spec.md quotes `%s` — the generated contract.ts carries no such method (the spec drifted from the door, or a method was renamed under the spec)", cand)
		}
	}
}
