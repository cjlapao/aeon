package agentsvc

import (
	"context"
	"fmt"
	"strings"
)

// skill.go is T2: typed requests. A typed request is reviewable — "this takes
// a topic and returns markdown" can be read and reasoned about — and a
// malformed one is refused BEFORE any model call. The second property is the
// one with teeth: a validation that happens after the model call is a
// validation that costs exactly as much as no validation.
//
// AS3 lives here by construction: a skill is a request for judgement, not a
// capability. Nothing in this file touches S12's tool registry, and the test
// suite asserts the registry stays exactly the ten S11 pinned — a skill that
// shows up there means AS3 is being violated.

// SkillRequest is the typed ask one agent makes of another's skill. It is the
// validated face of what T0 carried from a blueprint step (Params arrive
// verbatim from there; the blueprint layer never interpreted them — this layer
// is where contents finally answer to a type).
type SkillRequest struct {
	// Agent is the callee — whose card must offer the skill.
	Agent string `json:"agent"`
	// Skill is the skill id within that agent's card.
	Skill string `json:"skill"`
	// Text is the judgement being asked for. A skill is asked a question; an
	// empty ask is refused as loudly as an unknown field.
	Text string `json:"text,omitempty"`
	// InputMode is the media type of the ask. Absent falls back to the
	// skill's own first input mode, then the card's defaults — it is a fact
	// about the skill, never a second refusal surface.
	InputMode string `json:"inputMode,omitempty"`
	// Params is the free-form mapping carried from the pinned step (T0).
	// Validated for presence-shape, contents belong to the skill's own
	// description — the same discipline the blueprint layer deferred to here.
	Params map[string]any `json:"params,omitempty"`
}

// Reject is the refusal of a request — a value, not an error (AGENTS §5:
// refusals are data). Detail names the field or the supported set; a refusal
// that says only "unsupported" sends somebody to read source.
type Reject struct {
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

func (r *Reject) Error() string { return r.Detail }

// Client is the model-facing seam Ask runs accepted requests through — small
// on purpose, so the tests can hold it at zero and PROVE the refusal happened
// before any money was spent.
type Client interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// FindSkill looks a skill up by id within the card, and reports absence
// honestly. Discovery (T9) reads the result — examples and tags included —
// to decide whether a skill fits.
func (c *AgentCard) FindSkill(id string) (AgentSkill, bool) {
	for _, sk := range c.Skills {
		if sk.ID == id {
			return sk, true
		}
	}
	return AgentSkill{}, false
}

// supportedInputModes is the skill's declared modes, falling back to the
// card's defaults when the skill names none (A2A: skill modes override card
// defaults; absence inherits).
func supportedInputModes(card *AgentCard, sk AgentSkill) []string {
	if len(sk.InputModes) > 0 {
		return sk.InputModes
	}
	return card.DefaultInputModes
}

// validate is the whole pre-money contract: required fields by name, the
// skill existing in the callee's card (with what exists enumerated), and the
// input mode against the supported set (with the set named).
func (req SkillRequest) validate(card *AgentCard) *Reject {
	if strings.TrimSpace(req.Agent) == "" {
		return &Reject{Field: "agent", Detail: `skill request refused: field "agent" is required — a skill is asked of someone`}
	}
	if req.Agent != card.Name {
		return &Reject{Field: "agent", Detail: fmt.Sprintf("skill request refused: field %q is %q but the card is %q", "agent", req.Agent, card.Name)}
	}
	if strings.TrimSpace(req.Skill) == "" {
		return &Reject{Field: "skill", Detail: `skill request refused: field "skill" is required — name the skill id the card offers`}
	}
	if strings.TrimSpace(req.Text) == "" {
		return &Reject{Field: "text", Detail: `skill request refused: field "text" is required — a skill is asked a question, and an empty ask judges nothing`}
	}
	sk, ok := card.FindSkill(req.Skill)
	if !ok {
		if len(card.Skills) == 0 {
			return &Reject{Field: "skill", Detail: fmt.Sprintf("skill request refused: agent %q offers no skills", card.Name)}
		}
		ids := make([]string, 0, len(card.Skills))
		for _, s := range card.Skills {
			ids = append(ids, s.ID)
		}
		return &Reject{Field: "skill", Detail: fmt.Sprintf("skill request refused: skill %q is not offered by %q; offered: %s", req.Skill, card.Name, strings.Join(ids, ", "))}
	}
	mode := strings.TrimSpace(req.InputMode)
	if mode == "" {
		// The fallback IS a supported mode by construction (it came from the
		// skill or the card); an empty card defaults text/plain.
		if modes := supportedInputModes(card, sk); len(modes) > 0 {
			mode = modes[0]
		} else {
			mode = "text/plain"
		}
	} else {
		supported := supportedInputModes(card, sk)
		found := false
		for _, m := range supported {
			if m == mode {
				found = true
				break
			}
		}
		if !found {
			return &Reject{Field: "inputMode", Detail: fmt.Sprintf("skill request refused: inputMode %q not supported. Supported: %s", mode, strings.Join(supported, ", "))}
		}
	}
	return nil
}

// Ask validates, and only then runs the accepted ask through the client — the
// order is the contract, and the tests hold a Client spy at zero to prove it.
// T4's invoke is the production caller; anything else that wants a skill's
// judgement goes through this door so the pre-money rule holds in ONE place.
func Ask(ctx context.Context, client Client, card *AgentCard, req SkillRequest) (string, *Reject) {
	if reject := req.validate(card); reject != nil {
		return "", reject
	}
	sk, _ := card.FindSkill(req.Skill) // validate already proved it exists
	prompt := fmt.Sprintf("skill %s (agent %s): %s", sk.ID, card.Name, req.Text)
	out, err := client.Complete(ctx, prompt)
	if err != nil {
		return "", &Reject{Field: "client", Detail: fmt.Sprintf("skill %s/%s failed: %v", card.Name, sk.ID, err)}
	}
	return out, nil
}
