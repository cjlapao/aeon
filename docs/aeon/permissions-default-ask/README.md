# Permissions: allow, ask, deny — and ask by default (2026-11-15)

An operator ruling that amends S10/S11 (PL5, D4, SD5). It exists so the shipped software is useful with no
setup, while the dangerous things still ask a person. This file is the record; the code carries the details.

## What changed

1. **A call nobody wrote a rule for asks.** For a tool call or a shell command with no matching rule, the
   engine now answers *ask* instead of *refuse* (`policy.EvaluateAskingWhenUnmatched`, the amended PL5). The
   setting `permissions.unmatched` (`ask` default, `deny` restores the strict wall) chooses. Routes, skills and
   triggers keep default-deny. Every fail-closed path (an unreadable or poisoned set, an unknown type) still
   denies, and a matched rule is never softened: a deny denies, an allow allows, an explicit ask asks.
   `Verdict.DefaultAsk` marks an ask that came from the fall-through ("no rule covers this").
2. **Answering "approve" runs the call, once.** Before this, approving a permission question only handed the
   word "approve" to the model. Now the resume path runs the blocked call through the ordinary dispatch
   (`runtime/resume.go`: `answerPermission`). The approval is bound to that tool-call block and a digest of its
   exact arguments and is spent by the first matching dispatch, so a second identical call asks again.
   `bash` has its own policy check before it spawns; it accepts an ask-level verdict only for the approved
   command (`stepctx.ApprovedFor`), and a deny is never softened. A permission question a *model* wrote (on a
   `question` or `ask_agent` block) can never run anything.
3. **Three more answers write a rule.** A permission question offers `approve`, `approve for this loop`,
   `always allow`, `deny`, `always deny`. The three rule answers author the rule through the policy
   authoring door as the operator, with the connection's actor on the audit row, *before* the question is
   answered (`protocol/answer_rules.go`). A shell command becomes an exact-command rule (a command with `*`
   or a line break is refused for a rule answer); any other tool becomes a tool rule. "For this loop" targets
   the loop, "always" targets the agent. The in-process answer door cannot author policy.
4. **The first-party agents ship with their permissions.** The seed set `permissions.firstParty` (class
   **default**, new) is applied at daemon start through the same write boundary and ledger as the command
   corpus. Per first-party agent: allow `*.*.read` and `*.*.write` (every read tool and the write-access tools
   that stay inside the product's records), and *ask* for each dangerous tool plus every MCP tool
   (`tools.DangerousTools`: `bash`, `write`, `edit`, `doc_write`, `doc_edit`, `webfetch`, `ask_agent`;
   `mcp.*.*.*`). Ask outranks allow, which carves the dangerous tools back out of the write allow and names the
   rule that is asking. User-made and imported agents have no rows, so they fall to the default (ask) and never
   inherit these allows. The operator keeps authority: removing a row is recorded and not re-applied, a
   hand-edited row is diverged and left alone, and any rule they write outranks the seed.
5. **A loop can be pre-approved.** `loop.setAccess` (human door; the actor is the connection's peer) turns on
   `full` access or a list of pre-approved `tools` for one loop; `loop.access` reads it. The mode answers an ask
   only: an explicit deny still denies. It is stored in `loop_access` (no row = ask), read at step assembly into
   `StepRequest.AutoApprove`, works in attended and unattended loops, and publishes `loop.access.changed`.
   The loop page has a switch with a warning dialog for full access; the dangerous-tools list comes from the
   daemon (`askingTools`), not a copy.
6. **Class `default` is new** (`seed.ClassDefault`): applied at start like inert, allowed to be
   guard-affecting. It is the one amendment to D4; the corpus set stays class guard and still never applies at
   start (`TestStartupNeverAppliesPolicyCorpus` now pins that only first-party tool rules seed unasked).

## Where

| Piece | File |
| --- | --- |
| Default-ask in the engine | `internal/policy/evaluate.go`, `verdict.go` |
| The setting | `internal/config/settings_decl_policy.go` (`permissions.unmatched`) |
| The gate reads it | `internal/daemon/policy_gate.go` (`GateOption`, `evaluate`) |
| Approve runs the call | `internal/runtime/resume.go`, `loop.go`; `internal/stepctx`; `internal/tools/bash.go` |
| Rule answers | `internal/protocol/answer_rules.go`, `answer.go` |
| The dangerous list | `internal/tools/danger.go` |
| The seed set | `internal/policyseed/firstparty.go`; mounted in `internal/daemon/seed.go` |
| Loop access | `internal/store/loop_access.go` (+ migration `20261115120000`), `internal/protocol/loop_access.go`, `internal/daemon/loop_access.go` |
| The inbox sources it also added | `internal/attention/more_sources.go` (gates, proposals, triage) |
| UI | `ui/src/features/loop-workspace/LoopAccess.tsx`, `ui/src/components/agent-chat/types.ts` |

## Known limits

- The approval is keyed by tool-call block, but bash's own check is keyed by the command: two steps of the
  same agent running the identical command at the same instant could not both be approved by one answer, and
  the approval window is only the one call.
- `policy.why` over the socket agrees with enforcement; the offline CLI and the agent `policy_why` tool read
  the strict default until they are given the setting.
- A skill the person picks with `/` is the person's own choice; skill access (agent-to-agent) stays
  default-deny. Whether the `/` pick bypasses that check was not verified.
- Pre-approval can only be set on a loop that exists (`loop.setAccess`), never on `loop.start`: the narrow
  surface carries `loop.start`, and a field there would let a CI client or trigger pre-approve its own tools.
  A loop started unattended by a schedule therefore asks-and-denies until a person pre-approves it or writes an
  allow rule for its agent on the Policy page. A per-automation standing pre-approval is the natural next step
  and is not built.
- Not yet in the app: a Policy-page banner stating the default, and a "pre-approve" control on loop start (the
  loop page switch covers a running loop).
