package envseed

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/seed"
)

// updateGolden rewrites the golden after a LEGITIMATE content change. The
// ritual is the review: the golden's diff says what a fresh install now ships,
// and `SetVersion` has to have moved with it (the version test below fails
// otherwise).
var updateGolden = flag.Bool("update", false, "rewrite the versions golden")

// goldenPath is the golden's file. `key version hash`, one record per line —
// the shape toolseed's golden established, so a reader who knows one knows the
// other.
func goldenPath() string { return filepath.Join("testdata", "environments.standard.versions.golden") }

func TestDeclarationIsInertAndNamed(t *testing.T) {
	set := Declaration()
	if set.ID != SetID {
		t.Errorf("declaration ID = %q, want %q", set.ID, SetID)
	}
	if set.Class != seed.ClassInert {
		t.Errorf("class = %q, want inert: a set that applies at start with no human in the loop may not widen anything (EV19)", set.Class)
	}
	if set.Version != SetVersion {
		t.Errorf("version = %d, want %d", set.Version, SetVersion)
	}
	if set.Records == nil {
		t.Fatalf("Records is nil: the engine's rule 3 forbids it, and a nil source is how a set ships nothing silently")
	}
	recs := set.Records()
	if len(recs) != 1 {
		t.Fatalf("%d records, want the one system environment", len(recs))
	}
	if recs[0].Key != SystemScrubbedSlug {
		t.Errorf("record key = %q, want the system slug %q", recs[0].Key, SystemScrubbedSlug)
	}
	if recs[0].Version != SetVersion {
		t.Errorf("record version = %d, want the set's %d (a record that outgrows its set is a set that was not bumped)", recs[0].Version, SetVersion)
	}
}

// TestShippedSpecValidatesClean is the record's own gate: the daemon ships a
// spec that its validator signs with zero refusals, against the drivers this
// build actually has. A shipped default the validator would refuse is a fresh
// install whose first start records a failed seed — and a person reading
// `seed.status` would see a system record that never landed, with EV2 refusing
// every command meanwhile.
func TestShippedSpecValidatesClean(t *testing.T) {
	recs := Source()
	for _, rec := range recs {
		c, err := decode(rec.Content)
		if err != nil {
			t.Fatalf("decode %s: %v", rec.Key, err)
		}
		if c.Origin != "system" {
			t.Errorf("%s: origin = %q, want system", rec.Key, c.Origin)
		}
		if !c.BindWorkspace {
			t.Errorf("%s: bindWorkspace = false, and this is the record a fresh workspace defaults to", rec.Key)
		}
		if c.Backend != "host-scrubbed" {
			t.Errorf("%s: backend = %q, want host-scrubbed (EV6's downgrade target must be the scrubbed one)", rec.Key, c.Backend)
		}
		spec, err := execenv.ParseSpec([]byte(c.Spec))
		if err != nil {
			t.Fatalf("%s: the shipped spec does not parse: %v", rec.Key, err)
		}
		refusals := execenv.Validate(spec, c.Backend, execenv.ValidateOpts{Drivers: []string{"host-scrubbed", "host-inherit"}})
		for _, r := range refusals {
			t.Errorf("%s: the validator refuses the shipped spec: %s · %s · do: %s", rec.Key, r.What, r.Why, r.Do)
		}
		for _, w := range execenv.Warnings(spec, c.Backend) {
			t.Logf("%s: warning (never a refusal): %s", rec.Key, w)
		}
		// The three shipped choices, pinned as facts about the row rather than
		// as comments: what the allowlist holds, and that no value is in it.
		if len(spec.Env.Secrets) != 0 {
			t.Errorf("%s: the shipped spec names %d secret references; a default that reaches for a credential is a default that fails on a clean machine",
				rec.Key, len(spec.Env.Secrets))
		}
		for name := range spec.Env.Set {
			if isCredentialShaped(name) {
				t.Errorf("%s: the shipped spec SETS the credential-shaped %s (EV7: a literal is for behaviour, never a token)", rec.Key, name)
			}
		}
		// Compared as SETS: `Canonical` sorts the names, and a row's stored
		// order is not a fact about what the child was allowed to see.
		if strings.Join(sortedCopy(spec.Env.Passthrough), " ") != strings.Join(sortedCopy(execenv.DefaultPassthrough()), " ") {
			t.Errorf("%s: passthrough = %v, want the core's allowlist %v — the row must carry the list the core publishes, not a copy that can drift",
				rec.Key, spec.Env.Passthrough, execenv.DefaultPassthrough())
		}
		if spec.Home != execenv.HomeReal {
			t.Errorf("%s: home = %q, want real on a host backend (git's config lives in the person's ~)", rec.Key, spec.Home)
		}
	}
}

// sortedCopy is a copy for an order-insensitive comparison.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// isCredentialShaped is the deny list's own word list applied to a NAME, so
// this test asks the same question the builder will ask at spawn and cannot be
// satisfied by a name the deny list has simply not thought of yet.
func isCredentialShaped(name string) bool {
	upper := strings.ToUpper(name)
	for _, w := range execenv.Deny().Words {
		if strings.Contains(upper, w) {
			return true
		}
	}
	return false
}

// TestVersionsGoldenIsTheContentIs the golden (the toolseed ritual, on this
// set): `key version hash` per record. It fails on an unversioned content
// change, which is the whole point — `execenv.DefaultPassthrough()` is a
// function another slice may edit, and if the shipped default moved without a
// version bump the ledger would keep reporting the old content as applied.
func TestVersionsGoldenIsTheContent(t *testing.T) {
	got := goldenLines()
	path := goldenPath()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s:\n%s", path, strings.Join(got, "\n"))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the versions golden (run with -update to create it): %v", err)
	}
	wantLines := strings.Split(strings.TrimSpace(string(want)), "\n")
	if len(wantLines) != len(got) {
		t.Fatalf("golden has %d records, the set has %d\n golden: %v\n actual:  %v",
			len(wantLines), len(got), wantLines, got)
	}
	for i := range wantLines {
		if wantLines[i] == got[i] {
			continue
		}
		key := strings.Fields(wantLines[i])[0]
		t.Errorf("record %s changed:\n golden: %s\n actual:  %s\n"+
			"if this is intended: bump SetVersion and re-run with -update, and read the diff — this file is what a fresh install ships",
			key, wantLines[i], got[i])
	}
}

// goldenLines renders `key version hash` for every record, sorted so the file
// is stable regardless of any future map iteration inside Source.
func goldenLines() []string {
	recs := Source()
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, fmt.Sprintf("%s %d %s", r.Key, r.Version, seed.Hash(r.Content)))
	}
	sort.Strings(out)
	return out
}

// TestFingerprintMatchesTheDeclaredContent is the divergence check's sunny
// day, run without a store: the applier hashes the live row's columns in the
// form the source wrote, so an UNTOUCHED row must fingerprint to the record's
// own hash. If these two ever disagree while nothing changed, every fresh
// install reports its own system record as diverged — the state in which the
// ledger stops meaning anything.
func TestFingerprintMatchesTheDeclaredContent(t *testing.T) {
	for _, rec := range Source() {
		c, err := decode(rec.Content)
		if err != nil {
			t.Fatalf("decode %s: %v", rec.Key, err)
		}
		live, err := seed.Canonical(content{
			Slug: c.Slug, Name: c.Name, Description: c.Description,
			Backend: c.Backend, Spec: c.Spec, Origin: c.Origin,
			BindWorkspace: c.BindWorkspace,
		})
		if err != nil {
			t.Fatalf("canonical %s: %v", rec.Key, err)
		}
		if seed.Hash(live) != seed.Hash(rec.Content) {
			t.Errorf("%s: an untouched row fingerprints %s, the record says %s", rec.Key, seed.Hash(live), seed.Hash(rec.Content))
		}
	}
}

// TestContentForKeyIsReversibleAndRejectsAnEmptyKey pins the fingerprint's way
// back from the ledger's key. An empty key must not resolve to a content whose
// hash would then be compared against nothing.
func TestContentForKeyIsReversible(t *testing.T) {
	for _, rec := range Source() {
		c, err := contentForKey(rec.Key)
		if err != nil {
			t.Fatalf("contentForKey(%q): %v", rec.Key, err)
		}
		if key(c) != rec.Key {
			t.Errorf("key(contentForKey(%q)) = %q", rec.Key, key(c))
		}
	}
	if _, err := contentForKey(""); err == nil {
		t.Errorf("an empty key resolved; the fingerprint would hash a row it never looked up")
	}
	if _, err := declaredContent("no-such-slug"); err == nil {
		t.Errorf("declaredContent of a key the set does not declare was accepted")
	}
}
