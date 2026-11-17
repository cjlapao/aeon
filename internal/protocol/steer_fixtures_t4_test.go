package protocol

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

// TestStepSteerFixturesAreUntouched is CR10's guard on the conformance set:
// S39 adds a method (loop.close) and an event kind (loop.run.finished) and
// must not disturb a byte of the STEERING fixtures — steers are S38 T12's
// contract, and a changed fixture is either a wire change nobody scheduled
// or a fixture rewritten to pass a broken change. The constants below were
// computed with sha256sum BEFORE this slice touched conformance/ (and T4
// touched only conformance/blueprint/ and conformance/events/); re-pinning
// one is a deliberate act that must name the task that earned it.
//
// Re-pin on record, the way the guard asks for it: S68 T3 (B7) amended
// steers.json to carry the CHOSEN-skills receipt — the optional `skills`
// field on the pending row (the names the queue filed beside the operator's
// pure words, which the app renders as the steer's chips). Purely additive:
// every pre-S68 row shape still parses, steer.json and steer-cancel.json
// are untouched at their original digests, and S68's own fixtures
// (steer-skills.json) carry the new vocabulary's refusals and skips.
func TestStepSteerFixturesAreUntouched(t *testing.T) {
	want := map[string]string{
		"steer.json":        "2e0aab59fabad1b6d83ec6d35f1beca8844a96335490264c965a11c48b1a0c36",
		"steer-cancel.json": "45834eb25568ade102f78cffcde25f05364eac09286171be40eb5bbff90f1313",
		"steers.json":       "7753a8fe9f0bdac6b95d5469cfe569845920292060557fa226f7528e06189665",
	}
	for name, digest := range want {
		raw, err := os.ReadFile(filepath.Join("conformance", "step", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256.Sum256(raw); hexOf(got) != digest {
			t.Errorf("%s changed: sha256 %s, want %s — steer fixtures are S38 T12's contract (CR10); an amendment must name the task that earned it", name, hexOf(got), digest)
		}
	}
}

func hexOf(b [32]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, v := range b {
		out[2*i] = hexdigits[v>>4]
		out[2*i+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
