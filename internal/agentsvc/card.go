// Package agentsvc is S14's agent services: A2A cards, typed skills, Tasks
// that are rows, the chain that delegation records, and the cost that rolls
// up along it. AS1 says all of it is OPTIONAL — a handler with no card, no
// skills and no tools is markdown and a prompt, and the ladder runs on it
// unchanged — so this package is decorated around S13's registry, never
// required by it, and T11 proves the whole layer is deletable.
//
// card.go is T1: the A2A Agent Card, signed. AS2 — the format is A2A's
// because S15 imports agents this repository did not write, and inventing a
// shape means a translation layer nobody maintains. The slice's design names
// the 0.x convention the design was written against (/.well-known/agent.json,
// card-level url/preferredTransport); A2A v1.0's renames (agent-card.json,
// supportedInterfaces) are S21's remote-facing problem, recorded in the task
// notes — locally the card is a FILE (Q2, answered there) and the byte-level
// shape is what verification covers.
package agentsvc

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentProvider is A2A's AgentProvider.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentExtension is A2A's AgentExtension declaration.
type AgentExtension struct {
	URI         string         `json:"uri"`
	Description string         `json:"description,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
	Required    bool           `json:"required,omitempty"`
}

// AgentCapabilities is A2A's AgentCapabilities.
type AgentCapabilities struct {
	Streaming              bool             `json:"streaming,omitempty"`
	PushNotifications      bool             `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool             `json:"stateTransitionHistory,omitempty"`
	Extensions             []AgentExtension `json:"extensions,omitempty"`
}

// AgentSkill is A2A's AgentSkill — the set the design names: id, name,
// description, tags, examples, inputModes, outputModes.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// AgentCard is A2A's AgentCard (0.x card-level shape, as the design specifies).
// securitySchemes/security are carried OPAQUE: AS8 says authorization is
// SkillAccess policy, expressed through this card, never beside it — so the
// declaration survives a round trip while nothing here evaluates it.
type AgentCard struct {
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Version            string             `json:"version"`
	ProtocolVersion    string             `json:"protocolVersion,omitempty"`
	URL                string             `json:"url,omitempty"`
	PreferredTransport string             `json:"preferredTransport,omitempty"`
	IconURL            string             `json:"iconUrl,omitempty"`
	DocumentationURL   string             `json:"documentationUrl,omitempty"`
	Provider           *AgentProvider     `json:"provider,omitempty"`
	Capabilities       *AgentCapabilities `json:"capabilities,omitempty"`
	DefaultInputModes  []string           `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string           `json:"defaultOutputModes,omitempty"`
	Skills             []AgentSkill       `json:"skills,omitempty"`
	SecuritySchemes    json.RawMessage    `json:"securitySchemes,omitempty"`
	Security           json.RawMessage    `json:"security,omitempty"`
}

// SignedCard is the local transport envelope — an ADDITION, named as such:
// A2A signs cards over HTTP with JWS (AgentCardSignature, v1.0 §8.4); a local
// agent has no origin to fetch from, so the card travels as a file and the
// signature is detached ed25519 over the EXACT bytes of the card the file
// carries. Verification therefore covers whitespace: a re-marshalled card is
// a different card. JWS remains S21's remote-facing format.
type SignedCard struct {
	Card      json.RawMessage `json:"card"`
	PublicKey string          `json:"publicKey"` // base64 std, 32-byte ed25519 public
	Signature string          `json:"signature"` // base64 std, over Card's exact bytes
}

// Sign produces the envelope for card bytes. It is the authoring helper (the
// operator path and the fixtures); nothing at verification time calls it.
func Sign(card []byte, private ed25519.PrivateKey) (SignedCard, error) {
	if len(card) == 0 {
		return SignedCard{}, fmt.Errorf("refused: cannot sign an empty card")
	}
	var probe map[string]any
	if err := json.Unmarshal(card, &probe); err != nil {
		return SignedCard{}, fmt.Errorf("refused: cannot sign a card that is not valid JSON: %w", err)
	}
	// The signed bytes are the bytes the file will carry. json.Marshal COMPACTS
	// an embedded json.RawMessage, so signing anything else means the stored
	// card is not the signed card — the envelope is canonicalised to compact
	// JSON at signing time and verification checks the exact stored bytes
	// after that (tampering with whitespace afterwards still fails).
	var compact bytes.Buffer
	if err := json.Compact(&compact, card); err != nil {
		return SignedCard{}, fmt.Errorf("refused: cannot canonicalise the card: %w", err)
	}
	signed := compact.Bytes()
	return SignedCard{
		Card:      json.RawMessage(append([]byte(nil), signed...)),
		PublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, signed)),
	}, nil
}

// LoadCardFile reads one card file and verifies it against expectedAgent —
// the name the agent is registered under, which the card must CLAIM. Every
// failure is a refusal naming the agent; there is no partial card.
func LoadCardFile(path, expectedAgent string) (*AgentCard, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("card for %q is refused: %w", expectedAgent, err)
	}
	var sc SignedCard
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, fmt.Errorf("card for %q is refused: the envelope is not valid JSON (%v)", expectedAgent, err)
	}
	return Verify(sc, expectedAgent)
}

// Verify checks the envelope and returns the card, or a refusal that names
// the agent. The order is deliberate: signature FIRST (a card cannot earn
// shape-validation attention by being well-formed while forged), then the
// claim (the card must name the agent it is loaded for), then shape.
func Verify(sc SignedCard, expectedAgent string) (*AgentCard, error) {
	if len(sc.Card) == 0 {
		return nil, fmt.Errorf("card for %q is refused: it carries no card", expectedAgent)
	}
	if sc.Signature == "" || sc.PublicKey == "" {
		return nil, fmt.Errorf("card for %q is refused: it has no signature — unsigned is not a valid state (missing signature, not a warning)", expectedAgent)
	}
	pub, err := base64.StdEncoding.DecodeString(sc.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("card for %q is refused: its public key is not an ed25519 key", expectedAgent)
	}
	sig, err := base64.StdEncoding.DecodeString(sc.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("card for %q is refused: its signature is not an ed25519 signature", expectedAgent)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), sc.Card, sig) {
		return nil, fmt.Errorf("card for %q is refused: signature does not verify — the card was tampered with or is not from the key it claims", expectedAgent)
	}
	var card AgentCard
	if err := json.Unmarshal(sc.Card, &card); err != nil {
		return nil, fmt.Errorf("card for %q is refused: it is not valid AgentCard JSON (%v)", expectedAgent, err)
	}
	if card.Name != expectedAgent {
		return nil, fmt.Errorf("card for %q is refused: it claims to be %q — a card cannot be filed under one agent and name another", expectedAgent, card.Name)
	}
	if err := checkCardShape(&card, expectedAgent); err != nil {
		return nil, err
	}
	return &card, nil
}

// checkCardShape refuses the shapes that cannot mean anything: the required
// identity fields and skills that are not addressable (A2A addresses a skill
// by id within its card).
func checkCardShape(card *AgentCard, expectedAgent string) error {
	if card.Name == "" || card.Description == "" || card.Version == "" {
		return fmt.Errorf("card for %q is refused: name, description and version are required", expectedAgent)
	}
	seen := map[string]bool{}
	for _, sk := range card.Skills {
		if sk.ID == "" || sk.Name == "" || sk.Description == "" {
			return fmt.Errorf("card for %q is refused: skill %q is missing id, name or description", expectedAgent, sk.ID)
		}
		if seen[sk.ID] {
			return fmt.Errorf("card for %q is refused: duplicate skill id %q", expectedAgent, sk.ID)
		}
		seen[sk.ID] = true
	}
	return nil
}

// CardStore is the file convention that answers Q2 (recorded in the task
// notes): locally a card is a file in the agent-services directory, one per
// agent as <agent>.json; /.well-known/agent.json is an HTTP convention and
// S19 owns HTTP, S21 owns remote. The socket READ of cards is T10's — this
// task opens no listener.
type CardStore struct {
	dir string
}

// OpenCardStore points a store at a directory of card files. The directory
// need not exist: absence is the normal state for 25 of 25 agents (AS1).
func OpenCardStore(dir string) *CardStore { return &CardStore{dir: dir} }

// Card returns the verified card for one agent. ABSENCE is not an error —
// (nil, nil) is the ordinary state of every agent S13 shipped. Anything
// present but wrong is a refusal, and a refusal contributes nothing.
func (cs *CardStore) Card(agent string) (*AgentCard, error) {
	path := filepath.Join(cs.dir, agent+".json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("card for %q is refused: %w", agent, err)
	}
	return LoadCardFile(path, agent)
}
