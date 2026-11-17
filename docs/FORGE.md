# FORGE.md — the token, the identity, and what Aeon will never do with either

This is the document someone reads **before** handing a credential to a
coding agent, and **when** a merge is refused for a reason two commits
upstream. It is deliberately blunt.

`internal/forge/github.go` is the only code in the repository that knows
the forge has a network side; the automated suite answers every request
from committed fixtures behind a loopback server, and a repository-wide
tripwire test (`TestNoOtherTestGrewANetworkDependency`) fails the build
if any test or fixture anywhere names the live host.

## The token

### Where it comes from

The environment, in this order, first non-empty wins:

1. `AEON_FORGE_TOKEN`
2. `GITHUB_TOKEN`
3. `GH_TOKEN`

That is the whole supply story, and the whole of it is `DefaultTokenSource`.
The `Config` struct has **no token field** — a token a caller can set
directly is a token that came from wherever the caller is standing, and
one of the places a caller can stand is model output. No signature in
the package accepts a token as a parameter (asserted by
`TestTokenIsNotReadFromModelOutput`).

**Never a flag value.** A credential in `argv` is in the process list and
in the shell history, and that failure is silent. Aeon has no verb that
takes one.

### Where it never goes

- **Never into a log line.** The boundary contains no logging call at
  all — asserted by grep (`TestTokenIsNeverLogged`).
- **Never into an error message.** A failure caused by the credential
  names the header and the cause, never the value.
- **Never into a fixture.** Every recorded response is token-free; the
  test sentinel is a fake secret, and the assertion is that it appears
  exactly once on the wire — the `Authorization` header — and nowhere
  observable otherwise.
- **Never anywhere but the forge host.** The only `http.Client` is the
  one you inject or one this package builds, and it dials only
  `Config.BaseURL`. There is no defaulted live host: `New` with an empty
  `BaseURL` fails loudly rather than dialling out by omission.
- **Never into the web layer.** §4c of AGENTS.md applies: a credential
  goes in and never comes out — and here it does not even come to rest
  in a struct field; it is read per request through `TokenSource`.

### What Aeon will never do with it

Beyond the transport rules above, scoped to what the token *can be used
for* — the promises, not the hopes:

- **Never force-push. Never delete a branch or a tag.** No method of the
  `Forge` interface does either, and the implementation carries no
  method beyond the interface (`TestImplementationIsNotRicherThanThe
  Interface`). Pushing happens through local `git` against the origin
  the operator configured, and the same promises apply there (N8/N9).
- **Never read private data it does not need.** The ten methods touch
  issues, pull requests, reviews, and merge, for the repository named in
  the `Config`; the checks read (S57) adds one commit's check runs and
  legacy statuses, still by that repository, still read-only. There is
  no `GET /user`, no org listing, no "while we're here".
- **Never sent to a model.** The token is not in a prompt, a transcript
  row, or an event payload. Evidence rows carry numbers and SHAs (Q7).

### What it needs to be able to do

A fine-grained token scoped to the one repository, with:

| Scope | Why |
| --- | --- |
| Contents: Read and write | push the task branch; read branches for the exists-checks |
| Issues: Read and write | `forge.openIssue`, `forge.closeIssue`, the completeness search |
| Pull requests: Read and write | `forge.openPR`, `forge.merge`, `ReadPR` (the phantom-merge guard reads `base.ref` — L26) |
| Checks: Read | S57's merge gate reads the head commit's check runs; legacy commit statuses are readable under the existing Contents: Read. **Read-only** — the token never writes a check, never re-runs one, never fakes a green |

That is all. A token that can do more than this can be harmed harder; if
yours cannot be scoped this far, the loop will still run and will report
`auth` failures naming the missing scope, verbatim.

### When it is missing

Every call fails fast with a `ForgeError` of kind `auth` whose detail
names the three environment variables and points at this file. Nothing
else stops: the loop blocks **at its position** with that sentence as
its reason; already-recorded evidence stays recorded.

## The five kinds, and the 403 that says so

GitHub's failures arrive classified (T1/O5), because the human response
differs per kind — retry, stop and tell someone, treat as absent,
reconcile, report:

| GitHub says | Kind | Retried? |
| --- | --- | --- |
| connection refused, DNS failure, timeout, 5xx | `transport` | yes — bounded (below) |
| rate limit (403 with `x-ratelimit-remaining: 0`) | `transport`, with the reset window in the detail | yes — the detail says how long |
| 401, or 403 naming a missing scope | `auth` | **never** |
| 404 (read by identity) | `not-found` | never |
| 409, or 422 naming a duplicate | `conflict` | never |
| 403 from branch protection ("not mergeable", required checks/approvals) | `refused` | **never** |

A 403 is the interesting case: the status alone does not distinguish
*the token lacks a scope* from *branch protection declined* from *the
rate limit bit*. The body does, mostly. **When it cannot be
distinguished, the error says so** — the detail reads `ambiguous 403 —
could not be told apart…`, and the kind is the never-retried `refused`,
because the safe guess when the machine cannot tell is to stop and let a
human look.

### Retries are bounded, and `refused` never retries

Transport failures retry up to `Config.MaxRetries` times with backoff
(`Config.Backoff`, injectable; tests run it at zero). An unbounded retry
inside an action would make S5's step deadline a lie, so the bound is a
config field the loop sets, asserted by `TestRetriesAreBounded` —
attempt counts are measured at the server, not assumed.

## The identity limit — read this before pointing a loop at a protected branch

**One token, one identity.** Every issue opened, every PR opened, every
review posted, and every merge lands as the **same user**.

That has a consequence on a protected branch that requires "approving
reviews from the owner are ignored" (the usual configuration):

> A PR opened and approved under one identity is self-approved, and
> **the approval counts for nothing** against branch protection — while
> the merge gate, reading `APPROVED` from the review, believes otherwise.

This is recorded, not solved. **Two identities are deliberately not
built here**: two credentials under C17's rule, a per-action identity
mapping, and the behaviour when only one is configured are design
questions, and inventing answers mid-task is worse than stating the
limit. A per-forge-identity `postReview` under a reviewer account is the
first candidate; until something like it exists, on a protected branch
with owner-approvals-ignored:

- the merge will be refused by GitHub with a protection-flavoured 403
  (kind `refused`, not retried, reported verbatim), and
- the loop will block at the merge step rather than proceed on the
  self-approval.

That is the failure behaving correctly. The sentence you get names the
protection cause so you do not go auditing scopes for a governance
problem — and this document is where "why" is answered.

## The fixtures

Everything the test suite answers is a committed file under
`internal/forge/testdata/`, in envelope form
(`{"__status":…, "__headers":…, "body":…}`) served by a loopback server.
`TestFixturesAreRecordedNotLive` fails if the fixture set empties — the
only alternative to a recorded answer is a live call, and that is
forbidden (I7).
