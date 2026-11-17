# First-run log

This is the record of the timed measurement (S23 D1, C14): a stranger who has
not read the design reaches a running loop in under five minutes. It is the
only Definition of Done in the project a machine cannot evaluate, and the
only task whose gate is a person rather than a test.

## Status: not self-certified, and therefore not done

**This slice is not done.** There is no record below of a stranger reaching a
running loop, because **the measurement cannot be self-certified** — and
self-certifying it would be the one thing the rule exists to prevent. An
author who has written the onboarding flow believes it is clear; a tester who
has read the design knows which flag exists, and their five minutes are not a
stranger's.

If no such person is available, the slice is not done. That is the ruling, and
it is honoured here by recording the absence rather than inventing a pass. A
pass that records nothing throws away the problem it found; a pass that
records a number nobody timed is worse — it is the measurement the rule was
written to stop.

## What this log would hold, when a stranger runs it

Per provider (C21), with a different tester each time (the three presets have
different friction — the OpenAI-compatible path asks a base-URL question the
other two do not, which is exactly where a stranger is most likely to stop):

- **Their exact words**, not a paraphrase (O1). *"I didn't know if I needed a
  project first"* names a missing sentence; "confused about setup" does not.
- **Every question they asked.** Each is a failure of the previous screen, and
  the question is more valuable than the answer.
- **Anything they typed that did not work.**
- **Where they hesitated, and for how long.**
- **The clock time when a loop was running** — under five minutes is a pass.

**Recorded even on a pass** (O2). A four-minute run with a forty-second
hesitation has found a problem; a pass that records nothing throws it away.

## If it takes longer than five minutes

A twenty-minute result is not a slow tester; it is a list of places the
product failed to explain itself (QAC2-01). So the finding is **which
question cost the time** — never that the target was optimistic. And if that
question exists because of an earlier slice — a capability that needs
configuring, a route that needs hand-writing — that is a finding about that
slice (N8), recorded as one rather than papered over with documentation.

## What is automatable, and was done

The rest of the demo (§2–§7) and the QA-suite cases in
`test_suites/aeon-cli.md` (QAC2-10 through QAC2-17) are the automatable parts,
and they are done: `init` asking about providers; a key in config refused;
errors that say what to do; `doctor` telling the truth about a
half-configured system; a network trace over a full loop; a project blueprint
overriding the shipped one. **§5 is the one to read carefully** — if any line
reads "ok" over something uncertain, six slices of design work have been
discarded at the last possible moment, and `TestDoctorFlattensNothing`
guards against it.

The timed stranger run is the part that is not done, and it is recorded as
not done, not as a pass.
