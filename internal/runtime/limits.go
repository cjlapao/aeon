package runtime

import "time"

// Every limit, in one file, each labelled a guess (O4, I6).
//
// # Why this file exists
//
// Scattered constants become folklore. A `12` in a loop condition is a number
// nobody can revisit, because nobody can find out where it came from or whether
// anything depends on it. Collected here, each one carries its PROVENANCE — and
// where there is none, it says that too.
//
// **The QA suite is written to CHANGE these numbers rather than confirm them.**
// A case that passes because the limit happened to be right teaches nothing; one
// that measures what the limit costs tells you whether it is the right one.
//
// The tools' own limits live beside the tools (`internal/tools/limits.go`) for
// the same reason in the other direction: a cap on `read`'s output belongs where
// `read` is, and S11 will change them together.

// MaxIterations bounds the tool loop.
//
// # Provenance: NONE. This number is chosen, not inherited.
//
// The superseded slices define `maxIterations` as a field and never give it a
// default — so unlike the others below, there is no earlier judgement to carry.
// The reasoning for 12, stated so it can be argued with:
//
//	a read-and-report step    2–3 iterations
//	several files plus a test 5–7
//	12                        headroom for both, and a tail that costs at most
//	                          twelve model calls rather than an unbounded number
//
// It bounds SPEND as much as time: every iteration is a request, and a model
// that has stopped making progress will keep not making it until something
// stops it. Exceeding this yields `blocked`, naming the bound and its value
// (O2), because the right response is usually to raise it rather than to
// investigate.
const MaxIterations = 12

// DefaultDeadline bounds a step in wall-clock time.
//
// # Provenance: the superseded 05-tools plan, `deadlineMs` of 1 800 000
//
// Its reasoning, quoted: *"leaves room for two such commands plus overhead in
// one step"* — "such commands" being the 600-second bash timeout below. So this
// number is derived from that one, and changing that one should change this.
//
// A guess, like the rest. Thirty minutes is long enough that nothing legitimate
// hits it and short enough that a wedged step does not hold a slot overnight.
const DefaultDeadline = 30 * time.Minute

// DefaultMaxOutputTokens is the per-call output cap when a handler does not set
// one.
//
// # Provenance: S3's route default (4096), which came from M16
//
// It is NOT used to fill in a missing handler value — a handler with no
// `MaxOutputTokens` is a construction error (T3), because the right cap depends
// on what the handler asks for and a silent default truncates a long report
// with no explanation. This constant is the value a CALLER passes when building
// a handler and has no better number.
const DefaultMaxOutputTokens = 4096

// HandoffRetries is how many times an invalid handoff is re-asked.
//
// # Provenance: K4, inherited and not a guess
//
// **One.** Both halves are load-bearing. One retry bounds the tail: a model
// that cannot produce valid JSON twice will not produce it on the fifth
// attempt, and each attempt costs money. And the retry is TOOL-FREE — a retry
// that can call tools can start new work, so a step that already did its job
// could do it again, differently, while ostensibly just restating its report.
//
// The constant is here for the limits test; the tool-free property is asserted
// on the REQUEST OBJECT in the loop's tests, because counting attempts cannot
// see it.
const HandoffRetries = 1

// Limits reports every limit with its provenance, for the test that asserts
// each is defined here and labelled.
//
// A function rather than a comment the test parses: a test reading comments is
// a test that breaks on a reflow. This way the labels are data.
func Limits() []Limit {
	return []Limit{
		{
			Name:       "MaxIterations",
			Value:      MaxIterations,
			Guess:      true,
			Provenance: "none — chosen, because the superseded slices define the field without a default",
		},
		{
			Name:  "DefaultDeadline",
			Value: int(DefaultDeadline / time.Millisecond),
			Guess: true,
			Provenance: "the superseded 05-tools plan's deadlineMs of 1 800 000, which it justified as " +
				"room for two 600-second commands plus overhead",
		},
		{
			Name:       "DefaultMaxOutputTokens",
			Value:      DefaultMaxOutputTokens,
			Guess:      true,
			Provenance: "S3's route default, from M16",
		},
		{
			Name:  "HandoffRetries",
			Value: HandoffRetries,
			Guess: false,
			Provenance: "K4, inherited: one retry bounds the tail and a second attempt that still fails " +
				"will not succeed on a fifth",
		},
	}
}

// Limit is one bound, with where its number came from.
type Limit struct {
	Name  string
	Value int
	// Guess is false only for a number that was MEASURED or is a rule rather
	// than a budget. Most are guesses, and saying so is the point of this file.
	Guess      bool
	Provenance string
}
