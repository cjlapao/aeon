// cards.go — the answer skill and the cards that carry it. S28 T6.
//
// Every registered body can be ASKED the `answer` skill through its A2A Agent
// Card. A card is what Invoker.Invoke checks before it will run a skill of an
// agent — "agent %q has no signed card, so no skill of it can be asked" — so a
// body with no card is unaskable. This file generates the cards: one per
// registered body, the answer skill as its sole skill, signed by one stable
// key.
//
// Where this lives, and why. T6's files_expected named internal/handler/
// cards.go, but that is unbuildable: handler → agentsvc (this file) → loop →
// handler is an import cycle (loop pin.go reads handler; this file needs
// agentsvc's Sign and card types). agentsvc can import handler — its own test
// files already do — and handler imports neither agentsvc nor loop, so the
// edge that made the cycle is gone. The card generation belongs with the card
// type it produces, and the capability (the board's ask, S28 T7) consumes the
// cards through agentsvc's own Verify, so this is the natural home. The
// deviation from files_expected is recorded in the task Notes.
//
// Why the answer skill is on the card and not physically in the body file.
// T6's original scope said "add one skill declaration to all bodies' front
// matter." Checked against the tree, that collides with two guards older and
// more specific than this slice: the drop records (verify.go's
// CheckDropRecordFiles) count the WHOLE body file — front matter included —
// and A17 (TestZeroLinesAdded) forbids added lines on the prose handlers. The
// bodies' drop records were authored for the v1→aeon rewrite; an answer skill
// added after the rewrite is a later, orthogonal change, and threading it
// through the rewrite's line arithmetic would misrepresent the rewrite's
// delta. T6's own UPDATED note (2026-10-26) already describes the card as
// "derived from its front matter (name, description, the answer skill as an
// AgentSkill{…})" — the name and description are the front matter's, and the
// answer skill is supplied by the generation, not written into the body. This
// file follows that later, more-correct statement: the skill reaches the card,
// the body file and its drop record stay exactly as the v1→aeon rewrite left
// them. The gate tests pin the capability the task actually wanted — every
// body askable, with a real signed card — and the deviation from the original
// "front matter" wording is recorded in the task Notes (the task's
// files_expected never listed drops/ or t7_prose_test.go, so the collision the
// task did not foresee is resolved in the task's own favour).

package agentsvc

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/paths"
)

// AnswerSkillDescription is the ONE verbatim description every card's answer
// skill carries. It is byte-identical across all bodies (TestAnswerSkillTakes
// NoOtherAction pins it), so the board can ask any agent's answer skill and
// know, from the text alone, that the skill takes no other action.
const AnswerSkillDescription = "Respond using what you already know. Take no other action — no file writes, no commands, no tool calls beyond what answering the question requires to recall or reason, nothing that changes project state."

// The answer skill is built inline by CardForBody, not held as package-level
// state: TestExactlyOneAuthorizationMechanism forbids a `var` in this package
// (agentsvc decides nothing it can remember — the only permission path is
// policy.Evaluate over the frozen set). The skill's text is the constant
// above; the id and name are literals, so the declaration carries no state to
// drift and nothing to remember between calls.

// CardVersion is the version the generated card carries. The front matter has
// no version field, so the generation supplies one; a stable constant keeps a
// re-generated card byte-identical, which is what makes GenerateCards
// idempotent and its signature stable across runs.
const CardVersion = "1"

// CardForBody builds the AgentCard one registered body publishes: its front
// matter's name and description, the answer skill as the card's sole skill, and
// the version the generation supplies. It is derived, not hand-authored per
// body (T6 scope 2): one function over the loaded Definition, so a card cannot
// drift from the body it claims to be.
func CardForBody(d handler.Definition) AgentCard {
	return AgentCard{
		Name:        d.Name,
		Description: d.Description,
		Version:     CardVersion,
		Skills:      []AgentSkill{{ID: "answer", Name: "answer", Description: AnswerSkillDescription}},
	}
}

// GenerateCards writes one signed card per registered body to dir. The signing
// key is passed in (EnsureCardSigningKey supplies it in production); one key
// signs every card, so a re-run with the same key reproduces the same bytes —
// GenerateCards is idempotent and the daemon's ask door can verify the cards
// without a second system. The directory is created 0700 and the files 0600,
// the same owner-only posture the cursor key keeps.
func GenerateCards(dir string, key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("refused: the card signing key is %d bytes, not the %d of an ed25519 key", len(key), ed25519.PrivateKeySize)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create card directory %q: %w", dir, err)
	}
	for _, name := range handler.RegisteredBodies() {
		d, rej := handler.LoadDefinition(name)
		if rej != nil {
			return fmt.Errorf("generate card for %q: %w", name, rej)
		}
		raw, err := json.Marshal(CardForBody(d))
		if err != nil {
			return fmt.Errorf("marshal the card for %q: %w", name, err)
		}
		sc, err := Sign(raw, key)
		if err != nil {
			return fmt.Errorf("sign the card for %q: %w", name, err)
		}
		env, err := json.Marshal(sc)
		if err != nil {
			return fmt.Errorf("marshal the signed card for %q: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), env, 0o600); err != nil {
			return fmt.Errorf("write the card for %q: %w", name, err)
		}
	}
	return nil
}

// cardKeyEnv is the variable an operator may set to supply the signing key
// from a vault. An explicit key always wins — Aeon never overwrites a key it
// did not generate — mirrored from internal/secrets.
const cardKeyEnv = "AEON_CARD_SIGNING_KEY"

// cardKeyFile returns the path the card-signing key lives at, beside the store
// (the same secret posture as the cursor key: a dedicated path, never a config
// file, never argv, never a log).
func cardKeyFile() (string, error) {
	dir, err := paths.StoreDir()
	if err != nil {
		return "", fmt.Errorf("resolve the store directory for the card signing key: %w", err)
	}
	return filepath.Join(dir, "agentsvc", "card-signing.key"), nil
}

// EnsureCardSigningKey guarantees a card-signing key, generating and persisting
// one on first run. It mirrors internal/secrets's cursor-key discipline rather
// than inventing a second key-at-rest convention (T6 scope 3): an explicit
// environment key wins; a key on disk is loaded; neither generates a key,
// writes it 0600 in a 0700 directory, and returns it. The key is held in this
// process, not exported — unlike the cursor key, nothing downstream reads it
// from the environment, only the card generation here signs with it.
//
// The key is stable across runs by the same reason the cursor key is: rotating
// it would re-sign every card with a different signature and invalidate the
// ones the daemon already handed out, which surfaces as a client bug rather
// than a key rotation.
func EnsureCardSigningKey() (ed25519.PrivateKey, error) {
	if env := strings.TrimSpace(os.Getenv(cardKeyEnv)); env != "" {
		key, err := base64.StdEncoding.DecodeString(env)
		if err != nil {
			return nil, fmt.Errorf("the %s key is not valid base64: %w", cardKeyEnv, err)
		}
		if len(key) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("the %s key is %d bytes, not the %d of an ed25519 key", cardKeyEnv, len(key), ed25519.PrivateKeySize)
		}
		return ed25519.PrivateKey(append([]byte(nil), key...)), nil
	}

	path, err := cardKeyFile()
	if err != nil {
		return nil, err
	}

	if key, err := readCardKey(path); err == nil {
		return key, nil
	} else if !os.IsNotExist(err) {
		// A key that exists but cannot be read is NOT a reason to mint a new
		// one: overwriting it would re-sign every card and invalidate the
		// ones already handed out, and the cause is usually permissions.
		return nil, fmt.Errorf("read card signing key %s: %w", path, err)
	}

	key, err := generateCardKey()
	if err != nil {
		return nil, err
	}
	if err := writeCardKey(path, key); err != nil {
		return nil, err
	}
	return key, nil
}

func readCardKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is ours, not caller input
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("the card signing key file %s is not valid base64: %w", path, err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("the card signing key file %s is %d bytes, not the %d of an ed25519 key", path, len(key), ed25519.PrivateKeySize)
	}
	if string(key) == "" {
		return nil, fmt.Errorf("card signing key file %s is empty", path)
	}
	return ed25519.PrivateKey(key), nil
}

func generateCardKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate the card signing key: %w", err)
	}
	return priv, nil
}

func writeCardKey(path string, key ed25519.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write card signing key %s: %w", path, err)
	}
	return nil
}
