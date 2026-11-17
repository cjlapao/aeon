// helpers.go is the card-IO half of agentmgr: read a card file, load its
// envelope, and map it to the agent row the store keeps. The verification
// lives in the manager (Import); these are the pure IO + mapping, separated so
// the manager's refusals read as refusals, not file reads.

package agentmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/store"
)

// readCardFile reads the card file from disk. A missing or unreadable file is
// a refusal that names it, never a nil card that imports an empty agent.
func readCardFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("refused: cannot read the card file %q: %w", path, err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("refused: the card file %q is empty", path)
	}
	return raw, nil
}

// loadSignedAgentCard unmarshals the card file into S43's envelope. The card
// body is the agent content; the signature and certificate ride beside it.
func loadSignedAgentCard(raw []byte) (agentcard.SignedAgentCard, error) {
	var sc agentcard.SignedAgentCard
	if err := json.Unmarshal(raw, &sc); err != nil {
		return agentcard.SignedAgentCard{}, fmt.Errorf("refused: the card file is not a valid card envelope: %w", err)
	}
	if len(sc.Card) == 0 {
		return agentcard.SignedAgentCard{}, fmt.Errorf("refused: the card envelope has no card body")
	}
	return sc, nil
}

// cardContent is the agent content a card carries — the import maps it to the
// agent row. Slug travels with it (never regenerated on import); the rest is
// the expanded CardInfo (AM3).
type cardContent struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Functionality string `json:"functionality"`
	Metadata      string `json:"metadata"`
	Prompt        string `json:"prompt"`
	Version       string `json:"version"`
	AvatarType    string `json:"avatar_type"`
	AvatarColor   string `json:"avatar_color"`
	AvatarMood    string `json:"avatar_mood"`
}

// agentFromCard maps a loaded card's body to the agent row. A blank slug
// derives from the name (a card that predates S43); a blank version defaults
// to 1 (an import with no version is the first version, not a gap).
func agentFromCard(sc agentcard.SignedAgentCard) (store.Agent, error) {
	var c cardContent
	if err := json.Unmarshal(sc.Card, &c); err != nil {
		return store.Agent{}, fmt.Errorf("refused: the card body is not a valid agent card: %w", err)
	}
	if c.Name == "" {
		return store.Agent{}, fmt.Errorf("refused: the card has no name")
	}
	a := store.Agent{
		Slug:          c.Slug,
		Name:          c.Name,
		Description:   c.Description,
		Functionality: c.Functionality,
		Metadata:      c.Metadata,
		Prompt:        c.Prompt,
		Version:       versionOrBlank(c.Version),
		AvatarType:    c.AvatarType,
		AvatarColor:   c.AvatarColor,
		AvatarMood:    c.AvatarMood,
	}
	return a, nil
}

func versionOrBlank(v string) string {
	if v == "" {
		return "1"
	}
	return v
}

// checkTech validates the tech tags in an agent's metadata blob against the
// closed vocabulary and writes them back normalized (sorted, de-duplicated), so
// an agent cannot carry a tag nobody compiled: it would never be dispatched and
// nothing would say why. A blob without tags passes untouched.
func checkTech(metadata string) (string, error) {
	tags := store.Agent{Metadata: metadata}.TechTags()
	if len(tags) == 0 {
		return metadata, nil
	}
	clean, err := handler.NormalizeTech(tags)
	if err != nil {
		return "", fmt.Errorf("refused: %w", err)
	}
	return store.WithTech(metadata, clean)
}

// normalizeToolsJSON checks a tools column value — a JSON list of tool names — against the
// registry and returns it as the clean, sorted JSON list the column stores.
func normalizeToolsJSON(raw string) (string, error) {
	var wanted []string
	if err := json.Unmarshal([]byte(raw), &wanted); err != nil {
		return "", fmt.Errorf("refused: the tools must be a JSON list of tool names: %w", err)
	}
	clean, err := handler.NormalizeTools(wanted)
	if err != nil {
		return "", fmt.Errorf("refused: %w", err)
	}
	out, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// handoffFieldName is a handoff field's name: an identifier, with a trailing "?" marking it optional.
var handoffFieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}\??$`)

// normalizeHandoffJSON checks a handoff column value — a JSON object of field → purpose, a "?" on the
// field's name marking it optional — through the same schema builder a handler is served with, and
// returns it as the clean JSON the column stores. A map the runtime would refuse is refused here,
// by name, instead of being saved and failing the agent's first run.
func normalizeHandoffJSON(raw string) (string, error) {
	var spec map[string]string
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return "", fmt.Errorf("refused: the handoff must be a JSON object of field name to purpose: %w", err)
	}
	return normalizeHandoff(spec)
}

func normalizeHandoff(spec map[string]string) (string, error) {
	clean := make(map[string]string, len(spec))
	for key, purpose := range spec {
		if !handoffFieldName.MatchString(key) {
			return "", fmt.Errorf("refused: handoff field %q is not a name — start with a letter, then letters, digits or _ (a trailing ? marks it optional)", key)
		}
		clean[key] = strings.TrimSpace(purpose)
	}
	if _, rej := handler.BuildSchema(clean, "handoff"); rej != nil {
		return "", fmt.Errorf("refused: %s", rej.Reason)
	}
	out, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
