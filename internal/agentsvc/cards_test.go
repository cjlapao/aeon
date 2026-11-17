package agentsvc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// The task names these tests "TwentyFive" (S13, when the registry held 25).
// The registry grew to 33 (S38/S39/S24) and any future body re-measures this,
// so the tests cover the whole registered set rather than a remembered 25 —
// the capability must hold for EVERY body the daemon can dispatch to, not a
// subset. The re-measurement is the same discipline the count guards use
// (handler.resolve_test's len(registeredBodies) == 33).

func hasAnswerSkill(c AgentCard) bool {
	for _, sk := range c.Skills {
		if sk.ID == "answer" {
			return true
		}
	}
	return false
}

func answerSkillOf(c AgentCard) AgentSkill {
	for _, sk := range c.Skills {
		if sk.ID == "answer" {
			return sk
		}
	}
	return AgentSkill{}
}

func genCardKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a card signing key: %v", err)
	}
	return priv
}

// TestAllTwentyFiveBodiesDeclareAnswerSkill — every registered body's card
// carries the answer skill. Pinned the way TestRegistryExactNames pins the tool
// registry: the set is read from the registry, not remembered, so a body that
// joins re-measures the pin rather than slipping past it.
func TestAllTwentyFiveBodiesDeclareAnswerSkill(t *testing.T) {
	for _, name := range handler.RegisteredBodies() {
		d, rej := handler.LoadDefinition(name)
		if rej != nil {
			t.Fatalf("%s: %v", name, rej)
		}
		if !hasAnswerSkill(CardForBody(d)) {
			t.Errorf("%s: the card declares no answer skill — every body must be askable (S28 T6)", name)
		}
	}
}

// TestAnswerSkillTakesNoOtherAction — the declared description is byte-identical
// across every body, so the board can read the text and know the skill takes no
// other action. A paraphrase in one body would be a second, unverified meaning
// of "answer", and the board would rely on a text it did not pin.
func TestAnswerSkillTakesNoOtherAction(t *testing.T) {
	for _, name := range handler.RegisteredBodies() {
		d, rej := handler.LoadDefinition(name)
		if rej != nil {
			t.Fatalf("%s: %v", name, rej)
		}
		if got := answerSkillOf(CardForBody(d)).Description; got != AnswerSkillDescription {
			t.Errorf("%s: the answer skill's description is not byte-identical to the pinned text:\n got %q\nwant %q", name, got, AnswerSkillDescription)
		}
	}
}

// TestAllTwentyFiveBodiesHaveVerifiableCards — for each body, the card
// GenerateCards writes verifies through agentsvc's own verifier (signature,
// then the name claim, then shape), and its Skills carries the answer skill.
// A card that does not verify is one the ask door would refuse, so "has a
// card" is not "has a card that can be asked through".
func TestAllTwentyFiveBodiesHaveVerifiableCards(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCards(dir, genCardKey(t)); err != nil {
		t.Fatalf("GenerateCards: %v", err)
	}
	for _, name := range handler.RegisteredBodies() {
		c, err := LoadCardFile(filepath.Join(dir, name+".json"), name)
		if err != nil {
			t.Errorf("%s: the generated card does not verify through Verify: %v", name, err)
			continue
		}
		if !hasAnswerSkill(*c) {
			t.Errorf("%s: the verified card has no answer skill", name)
		}
		// T6 scope 4: no skill beyond answer reaches a card.
		if len(c.Skills) != 1 {
			t.Errorf("%s: the card carries %d skills, want exactly 1 (the answer skill)", name, len(c.Skills))
		}
	}
}

// ── the signing key: the discipline, not the prose ───────────────────────────

// A key that changes per start re-signs every card with a different signature
// and invalidates the ones the daemon already handed out — which surfaces as a
// client bug, not a rotation. Mirrors internal/secrets's cursor key.
func TestCardSigningKeyIsStableAcrossRuns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AEON_CARD_SIGNING_KEY", "")

	first, err := EnsureCardSigningKey()
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := EnsureCardSigningKey()
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the card signing key changed across runs")
	}
}

// An operator injecting from a vault must always win; Aeon never overwrites a
// key it did not generate, and a supplied key writes no file.
func TestAnExplicitCardKeyIsRespected(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	supplied := genCardKey(t)
	t.Setenv("AEON_CARD_SIGNING_KEY", base64.StdEncoding.EncodeToString(supplied))

	got, err := EnsureCardSigningKey()
	if err != nil {
		t.Fatalf("EnsureCardSigningKey: %v", err)
	}
	if !bytes.Equal(got, supplied) {
		t.Fatal("the explicit card key was not used")
	}
	path, err := cardKeyFile()
	if err != nil {
		t.Fatalf("resolve the card key path: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a card key file was written although one was supplied; the supplied key must win and nothing should persist")
	}
}

// An empty key file is corruption, not absence: it is reported, not minted over.
func TestAnEmptyCardKeyFileIsReportedNotReplaced(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("AEON_CARD_SIGNING_KEY", "")

	path := filepath.Join(dir, "aeon", "agentsvc", "card-signing.key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create the key dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write the key file: %v", err)
	}
	if _, err := EnsureCardSigningKey(); err == nil {
		t.Fatal("an empty card key file was silently regenerated instead of reported")
	}
}

// A malformed key value (not base64) is refused, not minted over.
func TestAMalformedCardKeyIsRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AEON_CARD_SIGNING_KEY", "not-valid-base64!!")
	if _, err := EnsureCardSigningKey(); err == nil {
		t.Fatal("a non-base64 card key was accepted")
	}
}
