# Exit codes

A loop in CI must distinguish **failed** from **blocked** from **waiting for a
human** (C10). Those are three different things and only one of them is a
problem with the run. A CI job that treats all non-zero exits as failures will
fail a build because a loop politely asked a question — so there is one
table, one mapping, and a code means the same thing everywhere. This is the
only way a CI job can branch on it.

| Code | Name | Meaning | A CI job should |
| --- | --- | --- | --- |
| `0` | ok | did what was asked — **including an aborted loop** (S17: a plan read and declined is a working system) | pass |
| `1` | failed | the work failed — the one that is a problem with the run | fail the build |
| `2` | blocked | the loop cannot proceed: a code action is unavailable, a bad plan | inspect, do not fail |
| `3` | waiting | a human is needed: a gate, a permission ask, an escalation | notify, do not fail |
| `4` | usage | the caller's mistake: a bad flag, a bad argument | fail the build (the caller is wrong) |
| `5` | config | configuration or credentials (S23 T2) | fail the build (configuration is wrong) |

## Why `waiting` earns the table

A CI job seeing `3` knows the loop is fine and a person is needed — and can
notify rather than fail. Seeing `1` it would page whoever broke the build.

## Why it composes with S19

A CI-triggered loop is **unattended**, so a gate does not produce `waiting` —
it produces `blocked` with the ask denied (G7, L22). Both codes exist because
both situations do, and which one a CI job sees tells it whether running
attended would have helped.

| Situation | Attended | Unattended (CI) |
| --- | --- | --- |
| a gate, permission ask, escalation | `3` waiting | `2` blocked |
| a step failed | `1` failed | `1` failed |
| a code action unavailable, a bad plan | `2` blocked | `2` blocked |
| a plan read and declined (aborted) | `0` ok | `0` ok |
| a bad flag / argument | `4` usage | `4` usage |
| a bad config / credential | `5` config | `5` config |

## The code and the message

The code is what a process returns; the message is what a message contains
(S23 T1). Both, for the same event: a code with no message is a CI job with
nothing to read, so every non-zero exit accompanies a three-part error
(what / why / do). An `ok` result may stand alone — did what was asked needs
no explanation.

## Where a code comes from

One table, one mapping. A code is produced only by the table
(`internal/exitcode`), never a bare `os.Exit` literal — a literal is a second
truth that drifts from the table. `cmd/aeon`'s exits are routed through the
table (a recorded follow-up while `main.go` is being edited by another agent).
