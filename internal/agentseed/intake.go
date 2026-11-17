package agentseed

import (
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/seed"
)

// The S61 T9 (GI15) second agent set: the goal-intake interview's voice,
// seeded INERTLY as its own set — "all three sets run against the
// S43-extra engine and each area's existing applier" (GI15, R4: an applier
// may serve a second set). It is deliberately NOT inside agents.firstParty
// (content.go's exclusion filter names why): the firstParty bytes are a
// settled census, and this body is S61's, not a widened 34th first-party
// handler. Like intakeseed's blueprint set, the declaration is mounted by
// daemon's NewSeedEngine rebuild block rather than from an init() — the
// package-global registry is what test engines build against, and a set
// declared from a distance forces every manual appliers map to grow
// (policyseed/decl.go's recorded reason; toolseed's pattern).

const (
	// IntakeSetID is the second agents set's id (SD2 grammar).
	IntakeSetID = "agents.intake"
	// intakeBody is the one registered handler this set seeds.
	intakeBody = "intake-architect"
	// intakeSetVersion is this set's version — its own number, not
	// firstParty's (SD1: sets do not share ledgers).
	intakeSetVersion = 1
)

// IntakeSource is the agents.intake record list: the one body's content,
// canonical, built by the same contentFromDefinition the firstParty set
// uses — the round-trip shape is shared, the set is not.
func IntakeSource() []seed.Record {
	var out []seed.Record
	d, rej := handler.EmbeddedDefinition(intakeBody)
	if rej != nil {
		panic("agentseed: the intake body will not load — " + intakeBody + ": " + rej.Reason)
	}
	c := contentFromDefinition(intakeBody, d)
	out = append(out, seed.Record{
		Key:     c.Slug,
		Version: intakeSetVersion,
		Content: mustCanonical(c),
	})
	return out
}

// IntakeDeclaration is the set the daemon mounts through NewSeedEngine's
// rebuild block: inert, the one record, the class a start reaches.
func IntakeDeclaration() seed.Set {
	return seed.Set{
		ID:      IntakeSetID,
		Class:   seed.ClassInert,
		Version: intakeSetVersion,
		Records: IntakeSource,
	}
}
