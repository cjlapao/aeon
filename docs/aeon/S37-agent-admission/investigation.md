# T0 · Investigation — S35's real names, and the S15 signatures

**Step 0 (precondition): PASSED.** All four command families printed something.
S35 T1–T5 is merged: the `proposal` table exists (migration `0042`), `proposal.create`
is registered, and `cmdProposal*` exist in `aeon/cmd/aeon/proposal.go`. T1–T7 may proceed.

## Replacements (design name → real name), for T1–T7

| Design name (TD) | Real name | Where |
| --- | --- | --- |
| the `proposal` table | `proposal` | store migration `0042_proposal` |
| the `kind` CHECK / Go mirror | `ProposalKinds` | `aeon/internal/store/proposal.go:18` |
| `readPackage` (export for preview) | `ReadPackagePreview` (rename `readPackage`) | `internal/plugin/import.go:194` |
| `ImportChecked` write half | `recordImport` + `StoreCarriedPluginDenial` | `import.go:112` / `cmd/aeon/policy.go:758` |
| the validator registry | `proposalValidators` | `aeon/cmd/aeon/proposal_validators.go:43` |
| the commit dispatch | `applyProposalEffect` / `effectAlreadyApplied` | `aeon/cmd/aeon/proposal.go:343` / `:230` |
| the daemon seam (like `Plugins`) | `ValidateProposal` + `Proposals` | `aeon/internal/protocol/method.go:721` / `:711` |

---

## Q1 — The `proposal` table

Command: `grep -n 'proposal' aeon/internal/store/schema.go`

```
94:  {42, "0042_proposal", "...", migration0042}
2244: CREATE TABLE proposal ( ... kind TEXT NOT NULL CHECK (kind IN
      ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove')) ... )
```

**Verdict:** `proposal`: migration **0042** `0042_proposal`; DDL at `schema.go:2244`; indexes:
**none** (no `CREATE INDEX` on `proposal`); referenced by any FK: **no** (inert row, no FK).

## Q2 — The `kind` CHECK literal list and Go mirror

Command: `grep -rn "policy_add\|mcp_server_remove" ... | grep -v _test`

```
aeon/internal/store/proposal.go:18:
  var ProposalKinds = []string{"policy_add", "policy_remove", "seed_corpus", "mcp_server_add", "mcp_server_remove"}
schema.go:2244 (CREATE TABLE proposal):
  kind TEXT NOT NULL CHECK (kind IN ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove'))
```

**Verdict:** CHECK list = the five kinds above (verbatim in DDL); Go mirror =
`ProposalKinds` at `proposal.go:18`. **S37 adds `'agent_import'` to both** (a new numbered
migration for the DDL, AD1; the `ProposalKinds` slice gets the one new element).

## Q3 — Store functions + row struct

Command: `grep -n '^func (s \*Store) .*Proposal\|type Proposal' aeon/internal/store/proposal.go`

```
type Proposal struct { ID; Kind; Payload; Status; CreatedAt; CreatedByKind; CreatedByID;
  DecidedAt; DecidedByKind; DecidedByID; DecideReason; AppliedError }   // proposal.go:26
CreateProposal(kind, payloadJSON string, by Actor, at string) (Proposal, error)   // :47
ListProposals(status string) ([]Proposal, error)                                 // :70
GetProposal(id int64) (Proposal, bool, error)                                    // :104
SetProposalAppliedError(id int64, msg string) error                               // :128
DiscardProposal(id int64, by Actor, at string) error                             // :169
DecideProposal(id int64, status string, by Actor, reason, at string) error      // :181
```

**Verdict:** matches design. Row struct `Proposal` (field `Payload`, `AppliedError`);
`SetProposalAppliedError` is the AD5 path. No new store function needed.

## Q4 — The validator registry

Command: `grep -rn 'proposalValidators\|map\[string\]func' aeon/cmd/aeon/proposal_validators.go`

```
aeon/cmd/aeon/proposal_validators.go:43:
  var proposalValidators = map[string]func(payload []byte) error { ... }
```

**Verdict:** registry lives in **`cmd/aeon`** (not `internal/protocol`), type
`map[string]func(payload []byte) error`. An entry returns **only `error`** (TD3 design) — it
cannot return a preview. So the create-time **preview composes in `cmd/aeon`** (T3, AD6), and
`proposal.create`'s handler (in `internal/protocol`) reaches the validator through the
`ValidateProposal` function seam (Q12), not by importing `internal/plugin` (Q6).

## Q5 — `proposal.create/list/discard`

Command: `grep -n 'Proposal\|ValidateProposal' aeon/internal/protocol/method.go`

```
373: MethodProposalCreate  MethodName = "proposal.create"
374: MethodProposalList    MethodName = "proposal.list"
375: MethodProposalDiscard MethodName = "proposal.discard"
711: Proposals ProposalStore                                  // the store seam
721: ValidateProposal func(ctx context.Context, kind, payload string) error   // the validator seam
948-950: registered handlers handleProposalCreate/List/Discard
```

**Verdict:** matches design. The result struct (design says S37 adds a `preview` field) — the
response is composed in `cmd/aeon` behind the seam; **S37 adds the `preview` by composing the
three read-only steps in `cmd/aeon` (T3), not by a new store field.** The registry entry
cannot return a preview (Q4), so the preview is the `cmd/aeon` function, passed in through
`ValidateProposal`-like seam.

## Q6 — Which package may import `internal/plugin`

Command: `sed -n 70,112p aeon/internal/plugin/absent_test.go`

```
TestNoUnconditionalImportOfPlugin: only cmd/aeon (seam) and internal/plugin (itself) may
import it; any runtime importer fails the test (AP11). The layer knows itself.
```

**Verdict:** matches. **Only `cmd/aeon` and `internal/plugin` may import `internal/plugin`.**
Therefore the validator registry (in `cmd/aeon`, Q4) **can** call `plugin.*`; the preview
composes in `cmd/aeon` (T3, AD6). Confirmed: the registry is in `cmd/aeon`, so it may call
`plugin.*` directly.

## Q7 — `aeon proposal commit` dispatch + `applied_error`

Command: `grep -rn 'applied_error\|AppliedError\|case "policy_add"' aeon/cmd/aeon/proposal.go`

```
commit (proposal.go:176-188):
  already, err := effectAlreadyApplied(row.Kind, row.Payload)
  if !already {
    if verr := validateProposalPayload(ctx, row.Kind, row.Payload); verr != nil {
      _ = st.SetProposalAppliedError(id, verr.Error());  // row stays proposed
      return ... "nothing was applied; the row stays proposed"
    }
    if aerr := applyProposalEffect(row.Kind, row.Payload); aerr != nil {
      _ = st.SetProposalAppliedError(id, aerr.Error());  // row stays proposed
      return ... "the row stays proposed for retry"
    }
  }
  ... st.DecideProposal(id, "committed", by, "", at)
applyProposalEffect (proposal.go:343): switch kind { case "policy_add": boundary.Add(...); ... }
  default: return "refused: proposal kind %q has no apply path"
```

**Verdict:** matches design. The dispatch is a `switch kind` in `applyProposalEffect`
(one case per kind, calling the existing function — **AD4**); a failure sets
`applied_error` and leaves the row `proposed` (**AD5**). **S37 adds `case "agent_import"` to
`applyProposalEffect` calling the existing `ImportChecked` write half** (T5, AD4), and a
matching `effectAlreadyApplied` case (Q14).

## Q8 — The TD10 grep test (PL15 whitelist)

Command: `grep -n 'proposal\|TestNoPolicyAuthoringReachesTheNarrowSurface' aeon/cmd/aeon/policy_test.go`

```
483: func TestNoPolicyAuthoringReachesTheNarrowSurface(t *testing.T) {
523:   "cmd/aeon/proposal.go": true,   // the whitelist entry (a caller file, not a method)
```

**Verdict:** `TestNoPolicyAuthoringReachesTheNarrowSurface` at `policy_test.go:483`; the
whitelist is a set of **caller files** including `"cmd/aeon/proposal.go"` (`:523`). **S37
needs no new amendment** — `agent_import`'s create writes only the `proposal` table (like S35's
four kinds), and its commit applies through `cmd/aeon/proposal.go` (already whitelisted).
Criterion 7: verified, not re-argued (the test is run unchanged, zero-diff).

## Q9 — The bench Trust group

Command: `grep -rn 'AgentsPage\|ProposalsPage' aeon/ui/src`

```
aeon/ui/src/features/test-bench/TestBench.tsx:24: import { AgentsPage }
:26: import { ProposalsPage }
:254: <AgentsPage />   :264: <ProposalsPage />
pages/AgentsPage.tsx:25: export function AgentsPage()
pages/ProposalsPage.tsx:17: export function ProposalsPage()
```

**Verdict:** `AgentsPage` at `.../test-bench/pages/AgentsPage.tsx`; `ProposalsPage` at
`.../pages/ProposalsPage.tsx`. The toast is raised after `proposal.create` (S35's pattern).
**S37's T6 adds `ProposeImportForm` to `AgentsPage`** (path field → `proposal.create` →
S35's toast + the labelled preview); `ProposalsPage.tsx` needs **zero changes** (criterion 6).

## Q10 — Migration count assertions

Command: `grep -rn 'len(all) != \|len(Migrations()) != \|!= 42' ...`

```
aeon/internal/store/schema_test.go:909:  if len(all) != 42
aeon/internal/store/policy_test.go:307:  if before != 42
aeon/internal/store/policy_test.go:458:  if n := len(Migrations()); n != 42
aeon/internal/store/policy_test.go:1318: if n := len(Migrations()); n != 42
```

**Verdict:** current count **K = 42**. Every assertion to bump after T1 (the new `0043`
migration): `schema_test.go:909`, `policy_test.go:307`, `policy_test.go:458`,
`policy_test.go:1318` → **43**.

## Q11 — The S15 signatures (verbatim, built code)

Command: `grep -n '^func ' ... | grep -i 'readPackage\|ImportChecked\|...'`

```
import.go:112: func ImportChecked(pkgPath string, ps []policy.Policy, at time.Time) (*Agent, *Reject)
import.go:194: func readPackage(pkgPath string) (*Agent, *Reject)
check.go:27:   func CheckRequests(a *Agent, ps []policy.Policy, at time.Time) *Reject
check.go:71:   func ReadCarriedPolicies(pkgPath string) ([]policy.Policy, *Reject)
consent.go:50: func RenderConsent(a *Agent, existing []policy.Policy) Consent
policy.go:758: func StoreCarriedPluginDenial(w io.Writer, boundary *policy.Store, p policy.Policy, now time.Time)
agent.go:111:  func cmdAgentImport(w io.Writer, args []string) error
```

**Verdict:** matches design **except two line numbers** — `StoreCarriedPluginDenial` is
`policy.go:758` (design said `:753`) and `cmdAgentImport` is `agent.go:111` (design said
`:98`). Signatures are otherwise exact. T2 renames `readPackage`→`ReadPackagePreview`.

## Q12 — The daemon seam pattern (S37 copies `Plugins`)

Command: `grep -n 'Plugins\|ValidateProposal\|Proposals' aeon/internal/daemon/daemon.go aeon/internal/protocol/method.go aeon/cmd/aeon/agent_services.go`

```
daemon.go:171:   Plugins func(ctx context.Context, params json.RawMessage) (any, string, error)
daemon.go:294:   table.Plugins = opts.Plugins
method.go:676:   Plugins func(ctx context.Context, params json.RawMessage) (result any, refusal string, err error)
method.go:711:   Proposals ProposalStore
method.go:721:   ValidateProposal func(ctx context.Context, kind, payload string) error
```

**Verdict:** matches. The seam S37 copies is `ValidateProposal` (a `func(ctx, kind, payload) error`
in `method.go:721`, set by `cmd/aeon` like `Plugins` at `daemon.go:294`). **S37 composes the
preview in `cmd/aeon` (T3) and threads it through a `ValidateProposal`-like seam** — the
protocol layer never imports `internal/plugin`.

## Q13 — `TestNoPluginManagementOnNarrowSurface` (cmd/aeon/agent_test.go)

Command: `grep -n 'TestNoPluginManagementOnNarrowSurface' aeon/cmd/aeon/agent_test.go`

```
311: // TestNoPluginManagementOnNarrowSurface pins R6, NARROWED by S35 T4
320: func TestNoPluginManagementOnNarrowSurface(t *testing.T) {
```

**Verdict:** S35's `agent.enable` registration **did not** make it fail (S35 T4 narrowed it to
name what is allowed on the narrow surface). **S37 adds no `agent.*` method** (it adds a
`proposal` kind, not an `agent.*` method), so it passes unedited. Criterion 7's PL15 is
separate (Q8).

## Q14 — Does a failed commit leave a re-commit idempotent for `agent_import`?

Reading Q7's `effectAlreadyApplied` (proposal.go:230): each kind checks "has THIS exact effect
already landed" — a mismatch answers false so validate+apply run and the real validator refuses
the genuine conflict loudly.

**Verdict:** S35's mechanism is per-kind, in `effectAlreadyApplied`. For `agent_import` there
is **no reliable way to tell "my earlier commit imported it" from "the CLI imported it"** —
both leave a live `plugin_agent_live` row. So the idempotency gate **must not return `true`
silently** (that would skip criterion 4's refusal). S37's `effectAlreadyApplied` case for
`agent_import` **answers `false`** (let the real validator run), and the `plugin_agent_live`
unique index refuses the second commit with its own text (criterion 4 — the same refusal a
second CLI import already produces). The idempotency gate can be skipped per kind (the
`default` arm refuses, and `agent_import` joins it, not the `true`-path).
