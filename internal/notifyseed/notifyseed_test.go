package notifyseed_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/notifyseed"
	"github.com/cjlapao/aeon/internal/seed"
)

// notifyseed_test.go — S58 T4: the set's own guarantees, checked without a
// store: the records are canonical, the keys are (kind|variant), the
// version rides the content, and the shipped titles are the switch's
// bytes (the P2 promise, pinned at the SOURCE of the class table).

func TestRecordsAreCanonicalAndKeyed(t *testing.T) {
	recs := notifyseed.Source()
	if len(recs) == 0 {
		t.Fatal("notify.events ships no records")
	}
	seen := map[string]bool{}
	for _, r := range recs {
		if seen[r.Key] {
			t.Fatalf("two records share the key %q", r.Key)
		}
		seen[r.Key] = true
		var c map[string]any
		if err := json.Unmarshal(r.Content, &c); err != nil {
			t.Fatalf("record %q is not JSON: %v", r.Key, err)
		}
		for _, field := range []string{"kind", "variant", "tone", "titleTemplate", "bodyTemplate", "enabled", "version"} {
			if _, has := c[field]; !has {
				t.Fatalf("record %q lacks the canonical field %q", r.Key, field)
			}
		}
		kind, variant, ok := notifyseed.SplitKey(r.Key)
		if !ok {
			t.Fatalf("record key %q is not a kind|variant pair", r.Key)
		}
		if c["kind"] != kind || c["variant"] != variant {
			t.Fatalf("record %q does not agree with its key (%v/%v)", r.Key, c["kind"], c["variant"])
		}
		if c["enabled"] != true {
			t.Fatalf("record %q ships disabled — the shipped set is the live set", r.Key)
		}
	}
}

// TestShippedTitlesAreTheSwitchesBytes is P2 at the table's source: the
// seven arms the old notifyHintFor carried must reappear here with the
// same title and tone, byte for byte. The daemon-level sweep
// (TestResolverSeededRowsEqualTheOldSwitch) proves the resolver renders
// them; this proves the ROWS carry them even if nobody ever boots.
func TestShippedTitlesAreTheSwitchesBytes(t *testing.T) {
	want := map[string]struct{ tone, title string }{
		"loop.finished":       {"success", "Loop finished"},
		"loop.blocked":        {"warning", "Loop blocked"},
		"loop.waiting":        {"info", "Loop waiting on you"},
		"loop.stop.requested": {"warning", "An agent asked you to stop a loop"},
		"watchdog.fired":      {"warning", "The watchdog needs your attention"},
		"budget.warn":         {"warning", "A budget is near its cap"},
		"budget.hit":          {"warning", "A budget cap is exhausted"},
		"question.asked":      {"info", "A question is waiting for you"},
		// the variants repeat the base bytes (table shape, zero hint drift):
		"loop.blocked/exhaustion":  {"warning", "Loop blocked"},
		"loop.blocked/model-error": {"warning", "Loop blocked"},
		"loop.waiting/stopped":     {"info", "Loop waiting on you"},
	}
	got := map[string]struct{ tone, title string }{}
	for _, r := range notifyseed.Source() {
		var c struct {
			Kind          string `json:"kind"`
			Variant       string `json:"variant"`
			Tone          string `json:"tone"`
			TitleTemplate string `json:"titleTemplate"`
		}
		if err := json.Unmarshal(r.Content, &c); err != nil {
			t.Fatal(err)
		}
		key := c.Kind
		if c.Variant != "" {
			key = c.Kind + "/" + c.Variant
		}
		got[key] = struct{ tone, title string }{c.Tone, c.TitleTemplate}
	}
	for key, w := range want {
		g, has := got[key]
		if !has {
			t.Errorf("class %q is not shipped — a curated hint went silent (P2)", key)
			continue
		}
		if g != w {
			t.Errorf("class %q = %+v, want %+v — byte-identity broken", key, g, w)
		}
	}
}

func TestVersionsObeyTheMaxRule(t *testing.T) {
	recs := notifyseed.Source()
	for _, r := range recs {
		if r.Version < 1 || r.Version > notifyseed.SetVersion() {
			t.Fatalf("record %q v%d violates 1 ≤ v ≤ set v%d", r.Key, r.Version, notifyseed.SetVersion())
		}
	}
}

// TestVersionsBumpWithContent is the golden: if a shipped title, tone or
// template changes, either the record's version went with it (and this
// golden is rewritten with NOTIFYSEED_GOLDEN_UPDATE=1 and committed in the
// same change — the engine then UPDATES the row), or the change is refused
// here so a silent content drift can never masquerade as the same version.
func TestVersionsBumpWithContent(t *testing.T) {
	var want []string
	if b, err := os.ReadFile("testdata/notify.events.versions.golden"); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l != "" {
				want = append(want, l)
			}
		}
	}
	var got []string
	for _, r := range notifyseed.Source() {
		sum := sha256.Sum256(r.Content)
		got = append(got, r.Key+"\t"+itoa(r.Version)+"\t"+hex.EncodeToString(sum[:]))
	}
	if strings.Join(want, "\n") == strings.Join(got, "\n") {
		return
	}
	if os.Getenv("NOTIFYSEED_GOLDEN_UPDATE") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/notify.events.versions.golden", []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden rewritten — commit it and say so in the record")
		return
	}
	t.Fatalf("content changed but version did not — bump it, or the golden is stale:\nwant:\n%s\ngot:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"))
}

func TestRegisteredAsInert(t *testing.T) {
	set, ok := seed.DefaultRegistry().Lookup(notifyseed.SetID)
	if !ok {
		t.Fatal("the set is not registered from init (SD2: a domain declares itself)")
	}
	if set.Class != seed.ClassInert {
		t.Fatalf("notify.events is class %s — a hint permits nothing, it must be inert (rule 6)", set.Class)
	}
	if set.Version != notifyseed.SetVersion() {
		t.Fatalf("registered set version %d vs SetVersion() %d — the declaration and the records disagree", set.Version, notifyseed.SetVersion())
	}
	// The refusing Adapter is the global's until the daemon mounts the
	// store-backed applier (the blueprintseed precedent).
	a, ok := seed.DefaultRegistry().Applier(notifyseed.SetID)
	if !ok {
		t.Fatal("no applier registered for notify.events")
	}
	if a.GuardAffecting() {
		t.Fatal("notify.events claims a guard-affecting applier — rule 6 refuses that pairing")
	}
	if _, _, err := a.Fingerprint(nil, "loop.finished|"); err == nil {
		t.Fatal("the global Adapter resolved without a store — it must refuse (errNoStore)")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
