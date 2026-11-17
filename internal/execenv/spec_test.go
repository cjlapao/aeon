package execenv

import (
	"encoding/json"
	"strings"
	"testing"
)

// The closed shape (EV4). Two failures are worth the tokens here: a field
// nobody thought of that parses to its zero value, and a spec with two
// documents in it where "the first one won" is not a decision anybody made.

func TestSpecClosedShape(t *testing.T) {
	legal := `{"env":{"passthrough":["PATH"],"set":{"CI":"1"},"secrets":["db-url"]},` +
		`"home":"real","limits":{"wallSeconds":60},"egress":{"mode":"unenforced"},` +
		`"image":null,"services":[],"preview":null}`

	t.Run("legal spec parses", func(t *testing.T) {
		s, err := ParseSpec([]byte(legal))
		if err != nil {
			t.Fatalf("ParseSpec: %v", err)
		}
		if s.Home != "real" || len(s.Env.Passthrough) != 1 || s.Env.Passthrough[0] != "PATH" {
			t.Errorf("parsed into the wrong shape: %+v", s)
		}
		if s.Env.Set["CI"] != "1" || s.Env.Secrets[0] != "db-url" {
			t.Errorf("set/secrets did not survive the parse: %+v", s.Env)
		}
		if s.Limits.WallSeconds == nil || *s.Limits.WallSeconds != 60 {
			t.Errorf("limits.WallSeconds = %v, want 60", s.Limits.WallSeconds)
		}
		if s.Preview != nil {
			t.Errorf("preview = %+v, want nil", s.Preview)
		}
	})

	// Each of these was a real risk, not a hypothetical: a spec that parses
	// and then does nothing is more dangerous than one that fails.
	for _, tc := range []struct {
		name, raw, wantFragment string
	}{
		{"unknown top-level key", `{"env":{},"home":"real","egress":{"mode":"unenforced"},"allowAll":true}`, "allowAll"},
		{"unknown env key", `{"env":{"passthrough":[],"secretz":[]},"home":"real","egress":{"mode":"unenforced"}}`, "secretz"},
		{"unknown limits key", `{"env":{},"home":"real","limits":{"wallsecond":30},"egress":{"mode":"unenforced"}}`, "wallsecond"},
		{"wrong type", `{"env":[],"home":"real","egress":{"mode":"unenforced"}}`, ""},
		{"trailing second document", `{"env":{},"home":"real","egress":{"mode":"unenforced"}} {"env":{}}`, "after the spec object"},
		{"not json at all", `PATH=1`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseSpec([]byte(tc.raw)); err == nil {
				t.Fatalf("ParseSpec(%s) = nil error; a closed shape must refuse it", tc.raw)
			} else if tc.wantFragment != "" && !strings.Contains(err.Error(), tc.wantFragment) {
				t.Errorf("error %q does not name %q", err, tc.wantFragment)
			}
		})
	}

	t.Run("canonical is stable and order-insensitive", func(t *testing.T) {
		a := Spec{Env: EnvSection{Passthrough: []string{"TZ", "PATH", "HOME"},
			Secrets: []string{"b", "a"}, Set: map[string]string{"Z": "1", "A": "2"}}}
		b := Spec{Env: EnvSection{Passthrough: []string{"HOME", "PATH", "TZ"},
			Secrets: []string{"a", "b"}, Set: map[string]string{"A": "2", "Z": "1"}}}
		if string(Canonical(a)) != string(Canonical(b)) {
			t.Errorf("two specs that mean the same canonicalise differently:\n %s\n %s",
				Canonical(a), Canonical(b))
		}
		// And re-parsing the canonical bytes gives the same canonical bytes —
		// the audit stores these, so a round trip that drifts makes a diff lie.
		got, err := ParseSpec(Canonical(a))
		if err != nil {
			t.Fatalf("re-parse of canonical: %v", err)
		}
		if string(Canonical(got)) != string(Canonical(a)) {
			t.Errorf("canonical is not a fixed point:\n %s\n %s", Canonical(a), Canonical(got))
		}
		if !json.Valid(Canonical(a)) {
			t.Errorf("Canonical did not produce valid JSON")
		}
	})

	t.Run("a nil set parses to an empty set, not a nil map", func(t *testing.T) {
		s, err := ParseSpec([]byte(`{"env":{"passthrough":["PATH"]},"home":"real","egress":{"mode":"unenforced"}}`))
		if err != nil {
			t.Fatalf("ParseSpec: %v", err)
		}
		if s.Env.Set == nil {
			t.Error("Env.Set is nil; every caller would have to nil-guard a field that means empty")
		}
	})
}
