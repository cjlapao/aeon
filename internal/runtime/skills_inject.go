package runtime

// skills_inject.go is the S66 seam: the ONE place the operator's Agent
// Skills catalog reaches a model. The content is not assembled here —
// this package must not know what a skill IS, only that a section may
// arrive. The daemon, which owns the catalog, builds the text once
// (daemon/skills_wire.go) and wires it through SetSkillsProvider; the
// compile appends it as injected_context and reports what landed so the
// dispatcher can mark the chat. The shape is the policy canonicalizer's
// (S35's SEC-059 precedent): an init-time seam, exactly one production
// call (pinned by daemon/skills_wire_test.go), nil means inert.

import "strings"

// SkillsSection is what the wired catalog offers one step.
type SkillsSection struct {
	// Text is the finished section — the framing sentence and the
	// per-skill lines, built by the daemon's ONE renderer so the
	// transcript and any later read-back cannot drift from each other.
	Text string
	// Names/Count/Truncated describe the offer without re-parsing the
	// text: they become the chat's marker row and the step.started note.
	Names     []string
	Count     int
	Truncated bool
	// Marker is the one sentence every surface prints about an injection
	// — the step.started note and the chat's system message both render
	// THIS string, so live and reload cannot tell two versions of one
	// fact. The daemon sets it at build time; empty falls back to the
	// default shape below.
	Marker string

	title string
}

// Title names the section in the manifest. One definition, defaulted
// here so no second spelling of "Agent skills" can exist.
func (s *SkillsSection) Title() string {
	if s.title == "" {
		return "Agent skills"
	}
	return s.title
}

// SetTitle lets the daemon's renderer fix the label once at build time.
func (s *SkillsSection) SetTitle(t string) { s.title = t }

// MarkerSentence answers the default sentence when the daemon left
// Marker empty; the two shapes are kept identical so they can never
// disagree about the same injection.
func (s *SkillsSection) MarkerSentence() string {
	if s.Marker != "" {
		return s.Marker
	}
	shown := s.Names
	if len(shown) > 12 {
		shown = shown[:12]
	}
	out := "skills injected: " + itoa(s.Count) + " — " + strings.Join(shown, ", ")
	if s.Truncated || len(s.Names) > 12 {
		out += " (+more)"
	}
	return out
}

// SkillsProvider answers the catalog right now — nil or an empty
// section means "nothing to inject", which is the same answer to the
// model and a DIFFERENT one to the operator (the daemon logs which). It
// is called per compile; caching is the provider's business, not this
// package's.
type SkillsProvider func() *SkillsSection

var skillsProvider SkillsProvider

// SetSkillsProvider is the init-time seam. ONE production call
// (daemon.New, when a catalog is wired), pinned by a guard test; nil is
// identity-inert, so a daemon built without the catalog compiles
// byte-identical transcripts to its pre-S66 self.
func SetSkillsProvider(fn SkillsProvider) { skillsProvider = fn }

// TheSkillsProvider answers the wired provider — exported shape for the
// daemon's own tests (it must see the SAME seam the compile sees).
func TheSkillsProvider() SkillsProvider { return skillsProvider }

// skillsSectionForStep is the compile's read: nil or an empty section
// injects nothing.
func skillsSectionForStep() *SkillsSection {
	fn := skillsProvider
	if fn == nil {
		return nil
	}
	// NO recover here, on purpose: this is construction, and this
	// package's own guard (TestConstructionIsNotInsideThePanicBoundary)
	// says construction lives outside the panic boundaries. A provider
	// that panics is a daemon bug and dies loudly, like any other bug.
	sec := fn()
	if sec == nil || sec.Count == 0 || len(sec.Names) == 0 || strings.TrimSpace(sec.Text) == "" {
		return nil
	}
	return sec
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [24]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
