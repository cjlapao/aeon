# S6 — suspend, answer, and compact

The desktop test bench walk is in [UI-QA-DEMO.md](UI-QA-DEMO.md). It exercises the same flow through the **Providers** and **Step** tabs, including reopening a suspended step by ID.

This walk uses decimal step IDs. Replace `ID` below with the number printed by `aeon step`. Run it in a throwaway workspace and state directory. The commands assume `aeon` is the binary built by `cd aeon && make build` (or is on `PATH`).

The repeatable local path is `go test ./cmd/aeon -run TestAnswerCLIThroughRealDaemonAndReplayProvider -count=1`. It runs the shipped CLI against a temporary daemon, SQLite store, and replay provider. S6 was completed with replay-only validation at the user's direction. The live walk below remains the release QA procedure; no provider key was available in this workspace as of 2026-10-17.

## 1. Ask, suspend, and exit

Set up an isolated workspace, daemon, and route. Use a short socket path because Unix sockets have a path limit.

```sh
mkdir -p /tmp/aeon-s6-work /tmp/aeon-s6-state
printf 'original install notes\n' > /tmp/aeon-s6-work/README.md
export AEON_STATE_DIR=/tmp/aeon-s6-state
export LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s6-state/rpc.sock
aeon serve
```

In another terminal with the same two environment variables, configure a route with `aeon routes set --id live --provider anthropic --model MODEL --api-key-file KEYFILE --context-window TOKENS`. Use the real model's context window. Then run:

```sh
aeon step --route live --workspace /tmp/aeon-s6-work --context-files README.md --allow-question \
  'Read the declared README and ask me to choose between a small edit and a rewrite before proceeding.'
```

The model must actually call `question`; a prompt cannot guarantee that. If it answers directly, use the replay test above to verify the mechanism, and repeat the live prompt. On suspension, the CLI prints the question, any options, and `aeon answer ID "<answer>"`. The step row is `suspended`; the transcript ends on an unanswered `question` tool use. The CLI command has returned.

Check that no **step client process** remains:

```sh
pgrep -fa 'aeon.*step'
```

Expected: no matching step process. `aeon serve` remains because it serves future requests; no step waits on a channel or timer. Record the command's output, including an empty result.

## 2. Answer after a restart and after an hour

Record the suspended ID and time. Stop `aeon serve`, start it again with the same state directory, do other work, and wait at least 60 minutes. Then:

```sh
aeon step show ID
aeon answer ID 'small edit'
aeon step show ID --json
```

Expected: the answer command streams the continuation, prints a final cost line with `1 resume`, and the stored transcript has one answer tool result attached to the original `question` use. Any tool result already in the transcript must not be produced again. Run `aeon answer ID 'rewrite'` and confirm the refusal names the first answer's timestamp.

Record the suspension and answer timestamps and the daemon restart in the walk notes. A quick restart test does not substitute for the one-hour check.

## 3. Changed and deleted declared files

`--context-files README.md` freezes a workspace-relative file at assembly. Suspend a new step with that flag. While it waits, edit `README.md`, then answer. The CLI should print a changed-file warning. In `aeon step show ID --json`, the note is in a **user** message and the opening context still contains the original content.

Repeat with a new suspended step and delete `README.md` before answering. The note should say **Missing**, distinctly from **Changed**. A file read later by the `read` tool is not a declared context file; use `--context-files` for this check.

## 4. Siblings finish before a question

Use the scripted runtime test `go test ./internal/runtime -run TestQuestion -count=1` for a deterministic single turn with `read` and `question`. A live model may choose two turns, which cannot prove the sibling rule. Inspect the suspended transcript:

```sh
aeon step show ID --json | jq '[.[] | .content[]? | select(.kind=="tool_result")] | length'
```

For a turn with exactly one sibling call, this is `1`. Canonical tool results are blocks in a **user** message; there is no `role: "tool"` message. The final assistant turn has exactly one unanswered use, named `question`.

## 5. Bounds survive two suspensions

Run a step with `--max-iterations 8 --allow-question`, answer two questions, then inspect `aeon step show ID --manifest`. `iterationsUsed` accumulates and `deadlineAt` remains the original absolute timestamp. The scripted identity and bound test is `go test ./internal/runtime -run TestCold -count=1`; the live walk records the same fields before and after each answer.

## 6. Compaction is visible and accounted for

Set a realistic but small **route** context budget with `aeon routes set --id live --provider anthropic --model MODEL --context-window TOKENS` (include the existing key only if changing it). Then run a long step with enough complete tool turns to leave a safe middle range. Watch `step.compacted`, and inspect:

```sh
aeon step show ID --compactions
aeon step show ID --json
aeon step verify ID
```

The compaction view prints the replaced message range, estimates before and after, the summary text, and its own cost. Check that the opening system and user context remain, the step cost includes the summary call, and a signed thinking block in the last assistant turn is unchanged when one is present. `step verify` checks stored transcript fidelity; compare the signature bytes directly when the provider emitted one.

If no safe range fits, the step ends `blocked` with a reason. The CLI sets the budget on **routes**, not on `aeon step`; `--context-window` is a `routes set` flag.

## 7. The question tool never executes

Before answering a suspended step, inspect its transcript:

```sh
aeon step show ID --json | jq '[.[] | .content[]? | select(.kind=="tool_use" and .name=="question")] | length'
aeon step show ID --json | jq '[.[] | .content[]? | select(.kind=="tool_result" and .toolUseId=="QUESTION_TOOL_USE_ID")] | length'
```

Expected before answering: one question use and zero matching results. After `aeon answer`, there is one matching result containing the human answer. `question`'s execute function panics if reached; the runtime intercepts it and returns instead.

## Walk record — replay-only S6 validation

| Check | Result on 2026-10-17 |
| --- | --- |
| Replay provider CLI, temporary daemon and store, `pgrep` with no step client, `jq` sibling count = 1, question use/result counts 1/0 before answer and 1/1 after, double-answer | PASS — `TestAnswerCLIThroughRealDaemonAndReplayProvider` |
| Fresh daemon instance after closing and reopening SQLite; changed declared file note | PASS — `TestQuestionSurvivesDaemonRestartAndAnswerResumes` |
| Live provider and human choice | Pending release QA — excluded from this replay-only run |
| Answer after a physical machine reboot and at least one hour | Pending release QA — excluded from this replay-only run |
| Live sibling, compaction, signed thinking, and bound checks | Pending release QA — requires a provider that emits those turns |

The automated restart test starts a new daemon object with a reopened store in one test process. It proves durable reconstruction; it does not claim to be a real machine reboot. S7 will recover a daemon killed during a running step. S6 handles a step that intentionally suspended and has no running step process.
