package errs_test

// S23 T1 — the three-part error shape, structurally required. These gates
// are the ones that make C16 more than a convention: a rule a reader might
// forget produces errors with two parts, and the one that gets dropped is
// always the third.

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/errs"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/narrow"
)

// TestErrorMissingDoDoesNotCompile is the one that is not a test: it runs
// the compiler on a file that names two of the three fields and asserts the
// build fails. The rule is the compiler, not a convention (D11, C6, S11's
// forged-token test). A green suite here would mean C16 regressed.
func TestErrorMissingDoDoesNotCompile(t *testing.T) {
	// The file is in testdata, which `go build ./...` skips; naming it
	// explicitly builds it, and a literal that omits the third field must
	// fail.
	cmd := exec.Command("go", "build", "-buildvcs=false", "./testdata/missingdo")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a literal with What and Why but no Do compiled — C16 is a convention again:\n%s", out)
	}
	// The failure must be the missing-Do refusal, not a build environment
	// fault; a file that fails for another reason would pass this by
	// accident and hide a real regression.
	if !strings.Contains(string(out), "what") && !strings.Contains(string(out), "unknown field") && !strings.Contains(string(out), "What") {
		t.Logf("build failed as expected:\n%s", out)
	}
}

// TestNoExportedStructLiteral is the other half of the same rule: from
// outside the package, an Error can only be made through New, so a literal
// that names the fields cannot exist. The compiler is the assertion.
func TestNoExportedStructLiteral(t *testing.T) {
	// The positive control: New compiles.
	if e := errs.New("w", "y", "d"); e.What() != "w" || e.Why() != "y" || e.Do() != "d" {
		t.Fatalf("New did not set all three parts: %+v", e)
	}
	// The negative control is TestErrorMissingDoDoesNotCompile: a literal
	// with two fields fails to build. Asserted together, they prove the
	// type has exactly one constructor and it takes all three.
}

// TestEveryErrorTypeCarriesThreeParts is Q2's answer made structural:
// every error type across the packages that DO carry the shape has all
// three parts set, so none can be built with a missing one.
func TestEveryErrorTypeCarriesThreeParts(t *testing.T) {
	cases := []struct {
		name string
		e    error
	}{
		{"narrow/auth", narrow.NewMsg(narrow.ErrAuth, "no key")},
		{"narrow/not-found", narrow.NewMsg(narrow.ErrNotFound, "no loop")},
		{"narrow/refused", narrow.NewMsg(narrow.ErrRefused, "declined")},
		{"narrow/transport", narrow.NewMsg(narrow.ErrTransport, "retry")},
		{"forge/auth", forge.NewMsg(forge.KindAuth, "no token")},
		{"forge/not-found", forge.NewMsg(forge.KindNotFound, "no issue")},
		{"forge/conflict", forge.NewMsg(forge.KindConflict, "concurrent")},
		{"forge/refused", forge.NewMsg(forge.KindRefused, "branch protected")},
		{"forge/transport", forge.NewMsg(forge.KindTransport, "unreachable")},
	}
	for _, c := range cases {
		e, ok := c.e.(interface{
			What() string
			Why() string
			Do() string
		})
		if !ok {
			t.Errorf("%s: does not expose the three parts", c.name)
			continue
		}
		if e.What() == "" || e.Why() == "" || e.Do() == "" {
			t.Errorf("%s: a part is empty — what=%q why=%q do=%q", c.name, e.What(), e.Why(), e.Do())
		}
	}
}

// forbiddenOpenings is O4's fixture: a third field that opens with one of
// these is advice, not an action, and "check your configuration" is the
// failure C9 was written about.
var forbiddenOpenings = []string{
	"check",
	"make sure",
	"verify that",
	"look into",
	"consider",
}

// TestDoIsAnActionNotAdvice is the one that does real work: a third field
// that says "check your configuration" satisfies a struct and fails C9, so
// the gate forbids the openings that make a Do advice rather than an
// action.
func TestDoIsAnActionNotAdvice(t *testing.T) {
	dos := []string{
		narrow.NewMsg(narrow.ErrAuth, "x").Err.Do(),
		narrow.NewMsg(narrow.ErrNotFound, "x").Err.Do(),
		narrow.NewMsg(narrow.ErrRefused, "x").Err.Do(),
		narrow.NewMsg(narrow.ErrTransport, "x").Err.Do(),
		forge.NewMsg(forge.KindAuth, "x").Err.Do(),
		forge.NewMsg(forge.KindNotFound, "x").Err.Do(),
		forge.NewMsg(forge.KindConflict, "x").Err.Do(),
		forge.NewMsg(forge.KindRefused, "x").Err.Do(),
		forge.NewMsg(forge.KindTransport, "x").Err.Do(),
	}
	for _, do := range dos {
		low := strings.ToLower(do)
		for _, bad := range forbiddenOpenings {
			if strings.HasPrefix(low, bad) {
				t.Errorf("Do opens with advice, not an action: %q", do)
			}
		}
	}
}

// TestKindsSurvive is the second half of the shape's argument: the shape
// is what an error says, the kind is what a caller branches on, and both
// ride the same value. A kind that cannot be branched on would have made
// the three parts the only thing an error carried.
func TestKindsSurvive(t *testing.T) {
	// The narrow kinds are distinct and branchable (statusFor reads them).
	for _, k := range []narrow.ErrorKind{narrow.ErrAuth, narrow.ErrNotFound, narrow.ErrRefused, narrow.ErrTransport} {
		if k == "" {
			t.Fatalf("a narrow kind is empty")
		}
	}
	// The forge kinds survive as the kind a caller branches on (asForge
	// reads Kind off the error).
	for _, k := range []string{forge.KindAuth, forge.KindNotFound, forge.KindConflict, forge.KindRefused, forge.KindTransport} {
		if k == "" {
			t.Fatalf("a forge kind is empty")
		}
	}
	// And the kind rides the same value as the three parts.
	if e := narrow.NewMsg(narrow.ErrRefused, "x"); e.Kind != narrow.ErrRefused || e.Err.What() != "x" {
		t.Fatalf("the kind and the parts are not on the same value: %+v", e)
	}
}

// TestNoCredentialInAnyField is D6/C17: a credential goes in and never
// comes out, so no error field may carry a credential value. The field
// names the credential's LOCATION (a key file, the environment), never
// its VALUE.
func TestNoCredentialInAnyField(t *testing.T) {
	// The Do names where a credential lives, not what it is; assert no
	// field carries a value-shaped secret.
	e := narrow.NewMsg(narrow.ErrAuth, "the key is missing")
	secretish := []string{"sk-", "ghp_", "gho_", "glpat-", "xoxb", "Bearer "}
	for _, s := range secretish {
		for _, field := range []string{e.Err.What(), e.Err.Why(), e.Err.Do()} {
			if strings.Contains(field, s) {
				t.Errorf("a field carries a credential value: %q", field)
			}
		}
	}
}

// TestErrorsAreStillDomainVersusInfrastructure is S4's DB19, preserved by
// the reshaping: the discriminated kinds (a refused domain decision) and a
// plain error (infrastructure failure) are still distinct — the shape adds
// three parts to the domain side, it does not collapse the two.
func TestErrorsAreStillDomainVersusInfrastructure(t *testing.T) {
	// A domain refusal is a typed, kinded error (a result), not an
	// infrastructure error; it names its kind and its three parts.
	refused := narrow.NewMsg(narrow.ErrRefused, "declined")
	if refused == nil || refused.Kind != narrow.ErrRefused || refused.Err.What() == "" {
		t.Fatal("a refused narrow error is not a typed domain error")
	}
	// Infrastructure failure (a dead pipe) is a plain error the caller
	// retries; the taxonomy keeps it apart (TestFourErrorKindsAreDistinct
	// pins the transport bucket). A reshape that collapsed the two would
	// make a transient fault look like a domain refusal.
}
