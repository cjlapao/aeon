// Consent is the text an operator reads BEFORE anything is decided (AP5):
// it renders REQUESTS, never capabilities. Under MC4 a package holds no
// capability — a request is an ask, and the only way a request becomes a
// permission is a policy the operator authors through `aeon policy`. So
// consent says REQUESTS, shows each request as ungranted, borrows the
// words for what granting WOULD mean from the tools registry itself (the
// same objects the model is shown — one description, never a copy that can
// drift), and writes nothing whatsoever.
package plugin

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// RequestLine is one ask, structured (the CLI renders it; a test can assert
// it without parsing prose). Granted is what CONSENT has given — always
// false; anything actually in force is a separate list (Existing), because
// the two answers are different questions and a merged blur is how a bench
// starts lying.
type RequestLine struct {
	Kind    string // "tool" | "skill" | "route"
	Name    string
	Granted bool // what CONSENT gave: ALWAYS false — this field exists so a
	// caller cannot confuse the two answers (rendered as "not granted")
	Says  string // what granting WOULD mean — the registry's own sentence
	Extra string // third-state notes ("no such tool is registered", card attestation)
}

// Consent is a rendered consent — a value, not a side effect.
type Consent struct {
	Agent        string
	Description  string
	Source       string
	State        string // what the operator is letting in: disabled until THEY enable
	Prompt       string // the body — the biggest request of all
	Requests     []RequestLine
	Existing     []policy.Policy // what ALREADY applies to this name (Q2: re-import inherits it, so SHOW it)
	CarriedShown []string        // the package's own denials — narrowing, listed for completeness
}

// RenderConsent builds the consent from the imported agent and the caller's
// current policy rows. It reads — from the agent it holds, from the tools
// registry, from the rows handed to it — and WRITES NOTHING: a consent that
// could mint the permission it is displaying is a grant button wearing a
// preview's clothes.
func RenderConsent(a *Agent, existing []policy.Policy) Consent {
	c := Consent{
		Agent:       a.Name,
		Description: a.Description,
		Source:      a.Source,
		State:       string(a.State),
		Prompt:      a.Prompt,
	}
	for _, t := range a.Requests.Tools {
		line := RequestLine{Kind: "tool", Name: t}
		if tool, ok := tools.Registry()[t]; ok {
			line.Says = tool.Spec.Description
		} else {
			// The third state (P31): not silence, not invention — the name
			// asks for something that does not exist in this binary.
			line.Extra = "no such tool is registered — asking for it asks for nothing that exists here"
		}
		c.Requests = append(c.Requests, line)
	}
	for _, s := range a.Requests.Skills {
		c.Requests = append(c.Requests, RequestLine{
			Kind: "skill", Name: s,
			Says:  "calling this skill goes through S14's skill_access gate at USE time — nothing here opens it",
			Extra: fmt.Sprintf("attested by the signed card (%d skills on it)", len(a.Card.Skills)),
		})
	}
	for _, r := range a.Requests.Routes {
		c.Requests = append(c.Requests, RequestLine{
			Kind: "route", Name: r,
			Says: "route access is judged by RouteAccess policy at use time — the request opens nothing",
		})
	}
	for _, p := range existing {
		// Q2, made visible: policies survive revocation and re-import, so
		// consent shows what ALREADY applies to this name — and only what
		// applies: another agent's rules are not this agent's consent.
		if p.Target.Kind == policy.AllAgents || (p.Target.Kind == policy.AgentKind && p.Target.ID == a.Name) {
			c.Existing = append(c.Existing, p)
		}
	}
	for _, p := range a.Carried {
		c.CarriedShown = append(c.CarriedShown, fmt.Sprintf("%s · %s · %s (carried by the package; source import)", p.ID, p.Action, p.Type))
	}
	return c
}

// String renders the consent the CLI prints and the panel shows. The words
// are chosen against the failure they prevent: consent read as an
// inventory of capabilities is consent that has already decided.
func (c Consent) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Consent — %s\n", c.Agent)
	fmt.Fprintf(&b, "  %s\n  source: %s · arriving as: %s\n", c.Description, c.Source, c.State)
	b.WriteString("  REQUESTS — this agent ASKED for the following. Nothing below is granted; the only way a request becomes a permission is a policy YOU author with `aeon policy`.\n")
	if len(c.Requests) == 0 {
		b.WriteString("    (the package requests nothing — it still arrives disabled)\n")
	}
	for _, r := range c.Requests {
		fmt.Fprintf(&b, "    [%s] %s — not granted by this consent", r.Kind, r.Name)
		if r.Says != "" {
			fmt.Fprintf(&b, " · what granting would mean: %s", r.Says)
		}
		if r.Extra != "" {
			fmt.Fprintf(&b, " · %s", r.Extra)
		}
		b.WriteString("\n")
	}
	if len(c.Existing) > 0 {
		b.WriteString("  ALREADY APPLIES (operator policies this name inherits — consent lists them, it does not change them):\n")
		for _, p := range c.Existing {
			target := string(p.Target.Kind)
			if p.Target.ID != "" {
				target += ":" + p.Target.ID
			}
			fmt.Fprintf(&b, "    %s · %s · %s · target %s · source %s\n", p.ID, p.Type, p.Action, target, p.Source)
		}
	}
	if len(c.CarriedShown) > 0 {
		b.WriteString("  THE PACKAGE NARROWS ITSELF (its own denials, accepted at the authorship boundary):\n")
		for _, s := range c.CarriedShown {
			fmt.Fprintf(&b, "    %s\n", s)
		}
	}
	// The body is the largest request an agent makes — what it will tell
	// the model to BE. Shown, not summarised: a preview nobody wrote is a
	// preview nobody trusts.
	preview := c.Prompt
	if len(preview) > 400 {
		preview = preview[:400] + " …(truncated — the whole body is the prompt at the source path)"
	}
	fmt.Fprintf(&b, "  ITS PROMPT (what it will tell the model to be):\n    %s\n", strings.ReplaceAll(preview, "\n", "\n    "))
	fmt.Fprintf(&b, "  Nothing above enabled anything: %s stays %s until you run `aeon agent enable %s`.\n", c.Agent, c.State, c.Agent)
	return b.String()
}
