package agentsvc

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// T1 — the A2A card: A2A's shape, signed, and optional.
//
// The signature envelope is OURS (named as an addition): a local agent has no
// HTTP origin to fetch from, so the card travels as a file —
// {"card": <the standard AgentCard JSON, byte-for-byte>, "publicKey": …,
// "signature": …} — and ed25519 verifies the EXACT bytes of the card the file
// carries. A tampered card is a REFUSAL that names the agent, never a warning,
// and a refused card contributes NO skills. No new dependency: crypto/ed25519.

func testKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

const goodCard = `{
  "name": "reddit-agent",
  "description": "reads reddit and summarises sentiment",
  "version": "1.0.0",
  "protocolVersion": "0.3",
  "url": "local",
  "capabilities": { "streaming": false, "pushNotifications": false },
  "defaultInputModes": ["text/plain"],
  "defaultOutputModes": ["text/plain"],
  "skills": [
    { "id": "summarise-sentiment", "name": "Summarise sentiment",
      "description": "given a topic, return what people are saying",
      "tags": ["reddit", "sentiment"],
      "examples": ["what do people think of aeon?"],
      "inputModes": ["text/plain"], "outputModes": ["text/plain"] }
  ]
}`

func writeCardFile(t *testing.T, dir, name string, sc SignedCard) string {
	t.Helper()
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func signedGood(t *testing.T) SignedCard {
	t.Helper()
	sc, err := Sign([]byte(goodCard), testKey())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return sc
}

func TestValidSignatureVerifies(t *testing.T) {
	dir := t.TempDir()
	path := writeCardFile(t, dir, "reddit-agent", signedGood(t))
	card, err := LoadCardFile(path, "reddit-agent")
	if err != nil {
		t.Fatalf("a valid signature must verify: %v", err)
	}
	if card.Name != "reddit-agent" || card.Version != "1.0.0" || len(card.Skills) != 1 {
		t.Fatalf("card parsed wrong: %#v", card)
	}
	sk := card.Skills[0]
	if sk.ID != "summarise-sentiment" || sk.Tags[0] != "reddit" || len(sk.Examples) != 1 {
		t.Fatalf("A2A skill shape lost: %#v", sk)
	}
	if card.DefaultInputModes[0] != "text/plain" || !strings.Contains(card.Description, "reddit") {
		t.Fatalf("card-level standard fields lost: %#v", card)
	}
}

func TestTamperedCardIsRefused(t *testing.T) {
	// Refused, NOT warned about: an error and no card, for every way the bytes
	// can move — content, whitespace, key, or a signature from another key.
	for _, test := range []struct {
		name   string
		mutate func(sc *SignedCard)
	}{
		{"content", func(sc *SignedCard) {
			sc.Card = json.RawMessage(strings.Replace(string(sc.Card), "reads reddit", "reads everything", 1))
		}},
		// (a whitespace case lives below as a FILE-level edit: json.Marshal
		// compacts a struct-level RawMessage, so the honest attack is bytes on
		// disk, and that is what TestWhitespaceInFileIsRefused walks.)
		{"signature", func(sc *SignedCard) {
			raw, _ := base64.StdEncoding.DecodeString(sc.Signature)
			raw[0] ^= 0xff
			sc.Signature = base64.StdEncoding.EncodeToString(raw)
		}},
		{"key-replacement", func(sc *SignedCard) {
			// A different (right-length) public key under the same signature:
			// verification must not find the signature "close enough".
			sc.PublicKey = base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
		}},
	} {
		dir := t.TempDir()
		sc := signedGood(t)
		test.mutate(&sc)
		path := writeCardFile(t, dir, "reddit-agent", sc)
		card, err := LoadCardFile(path, "reddit-agent")
		if err == nil {
			t.Fatalf("%s: tampered card accepted", test.name)
		}
		if card != nil {
			t.Fatalf("%s: refused card still returned", test.name)
		}
		if !strings.Contains(err.Error(), "reddit-agent") {
			t.Fatalf("%s: refusal must name the agent, got %v", test.name, err)
		}
	}
}

func TestWhitespaceInFileIsRefused(t *testing.T) {
	// The file on disk is the attack surface: json.Unmarshal keeps the RawMessage
	// byte-for-byte, so a hand-inserted space inside the card object changes the
	// signed bytes and the signature must die for it.
	dir := t.TempDir()
	sc := signedGood(t)
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"description":"reads reddit`, `"description": "reads reddit`, 1)
	if tampered == string(raw) {
		t.Fatal("fixture drift: nothing was tampered")
	}
	path := filepath.Join(dir, "reddit-agent.json")
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if card, err := LoadCardFile(path, "reddit-agent"); err == nil || card != nil {
		t.Fatalf("whitespace-tampered card accepted: (%v, %v)", card, err)
	}
}

func TestMissingSignatureIsRefused(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*SignedCard)
	}{
		{"no-signature", func(sc *SignedCard) { sc.Signature = "" }},
		{"no-key", func(sc *SignedCard) { sc.PublicKey = "" }},
		{"junk-key", func(sc *SignedCard) { sc.PublicKey = "not-base64" }},
		{"short-key", func(sc *SignedCard) { sc.PublicKey = base64.StdEncoding.EncodeToString([]byte("short")) }},
	} {
		dir := t.TempDir()
		sc := signedGood(t)
		test.mutate(&sc)
		path := writeCardFile(t, dir, "reddit-agent", sc)
		card, err := LoadCardFile(path, "reddit-agent")
		if err == nil || card != nil {
			t.Fatalf("%s: must refuse with no card, got (%v, %v)", test.name, card, err)
		}
		if !strings.Contains(err.Error(), "reddit-agent") {
			t.Fatalf("%s: refusal names the agent: %v", test.name, err)
		}
	}
	// The card-without-any-envelope (bare AgentCard JSON, no signature field)
	// is ALSO a refusal: unsigned is not a valid state here.
	dir := t.TempDir()
	path := filepath.Join(dir, "bare.json")
	if err := os.WriteFile(path, []byte(goodCard), 0o600); err != nil {
		t.Fatal(err)
	}
	if card, err := LoadCardFile(path, "reddit-agent"); err == nil || card != nil {
		t.Fatalf("a bare unsigned card must be refused: (%v, %v)", card, err)
	}
}

// TestAgentWithNoCardStillDispatches — AS1 in its plainest form: S13's 25
// bodies have no card and dispatch exactly as they did before this package
// existed. The card store is empty; the handler registry is untouched.
func TestAgentWithNoCardStillDispatches(t *testing.T) {
	empty := t.TempDir()
	cs := OpenCardStore(empty)
	card, err := cs.Card("golang-engineer")
	if err != nil || card != nil {
		t.Fatalf("absence must be (nil, nil): (%v, %v)", card, err)
	}
	if _, ok := handler.Lookup("golang-engineer"); !ok {
		t.Fatal("S13's registry broke — golang-engineer is gone")
	}
	if _, ok := handler.Build("golang-engineer", "write go"); !ok {
		t.Fatal("the 25 must build unchanged (AS1)")
	}
}

// TestRefusedCardContributesNoSkills — not partially used. There is no path
// where "signature failed but the skills look fine" hands anything back.
func TestRefusedCardContributesNoSkills(t *testing.T) {
	dir := t.TempDir()
	sc := signedGood(t)
	sc.Card = json.RawMessage(strings.Replace(string(sc.Card), "summarise-sentiment", "evil-sentiment", 1))
	path := writeCardFile(t, dir, "reddit-agent", sc)
	cs := OpenCardStore(dir)
	card, err := cs.Card("reddit-agent")
	if err == nil || card != nil {
		t.Fatalf("tampered card returned from the store: (%v, %v)", card, err)
	}
	if direct, derr := LoadCardFile(path, "reddit-agent"); derr == nil || direct != nil {
		t.Fatalf("the direct entry point must refuse too: (%v, %v)", direct, derr)
	}
	if !strings.Contains(err.Error(), "reddit-agent") {
		t.Fatalf("store refusal must name the agent: %v", err)
	}
}

func TestCardClaimingAnotherAgentIsRefused(t *testing.T) {
	// A card signed by the right key but naming a different agent is a
	// substitution attempt: refused, both agents named.
	dir := t.TempDir()
	sc := signedGood(t)
	// sign the SAME envelope bytes but expect it under a name the card does not
	// carry — the claim check happens after the signature holds.
	path := writeCardFile(t, dir, "evil-agent", sc)
	if card, err := LoadCardFile(path, "evil-agent"); err == nil || card != nil {
		t.Fatalf("a card claiming another agent must be refused: (%v, %v)", card, err)
	} else if !strings.Contains(err.Error(), "evil-agent") || !strings.Contains(err.Error(), "reddit-agent") {
		t.Fatalf("refusal must name BOTH the expected and the claimed agent: %v", err)
	}
}

// TestCommittedFixturesMatchTheirNames — the fixtures under testdata/cards are
// the states this task promises; a test must load them or they are dead weight
// that drifts.
func TestCommittedFixturesMatchTheirNames(t *testing.T) {
	card, err := LoadCardFile("testdata/cards/reddit-agent.json", "reddit-agent")
	if err != nil || card == nil {
		t.Fatalf("the valid fixture must verify: (%v, %v)", card, err)
	}
	if card, err := LoadCardFile("testdata/cards/reddit-agent-tampered.json", "reddit-agent"); err == nil || card != nil {
		t.Fatalf("the tampered fixture must refuse: (%v, %v)", card, err)
	}
	if card, err := LoadCardFile("testdata/cards/reddit-agent-unsigned.json", "reddit-agent"); err == nil || card != nil {
		t.Fatalf("the unsigned fixture must refuse: (%v, %v)", card, err)
	}
}

func TestCardShapeRefusals(t *testing.T) {
	for _, card := range []string{
		`{"description":"x","version":"1"}`, // no name
		`{"name":"a","version":"1"}`,        // no description
		`{"name":"a","description":"x"}`,    // no version
		`{"name":"a","description":"x","version":"1","skills":[{"name":"n","description":"d"}]}`,                                                  // skill without id
		`{"name":"a","description":"x","version":"1","skills":[{"id":"i","name":"n","description":"d"},{"id":"i","name":"m","description":"d"}]}`, // duplicate id
	} {
		key := testKey()
		sc, err := Sign([]byte(card), key)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		path := writeCardFile(t, dir, "a", sc)
		got, err := LoadCardFile(path, "a")
		if err == nil || got != nil {
			t.Fatalf("bad card shape must be refused: %s", card[:min(len(card), 60)])
		}
	}
}
