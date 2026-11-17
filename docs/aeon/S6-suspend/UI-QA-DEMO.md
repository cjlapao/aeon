# S6 test bench QA demo

This walk exercises suspension in the desktop test bench. It is a manual release QA procedure, not a claim that a live provider, physical reboot, or one-hour wait ran in the replay-only S6 validation. Record the date, route, step IDs, and observations in the table below when you run it.

## Setup

1. Start an isolated daemon and the Tauri app with `cd aeon && make dev`. A browser opened by Vite alone has no Tauri bridge and cannot run this walk.
2. Use a disposable workspace with a `README.md`. In **Providers**, set a route with a working model and credential, or a provider-compatible local endpoint. Keep the same route ID throughout the provider-swap check.
3. In **Step**, use the workspace's absolute path. Leave **Also grant the shell** off. `read`, `list`, and `write_file` are already available without it.

## A. Ask and suspend

1. In **Step**, set **Declared context files** to `README.md`, set **Iteration bound** to `8`, tick **Allow the step to ask me a question**, and enter: “Read README.md and ask me whether to make a small edit or a rewrite before changing it.” Run the step.
2. Record the numeric step ID. If the model answers without calling `question`, repeat with a prompt or replay fixture that does. A prompt alone cannot guarantee a tool call.
3. Expect a **suspended** result and an **awaiting answer** panel with the exact question and choices. The saved-step panel must show one question use and zero question results. Open **Canonical transcript** to confirm that its final assistant turn has an unanswered `question` tool use.
   Run `pgrep -fa 'aeon.*step'` in a terminal and record an empty result; the daemon and app may remain open, but no step client should be waiting.
4. Close the Step tab or the whole app. The step must remain suspended in the daemon store. Reopen **Step**, enter the ID in **Open saved step**, and select **Open step**. The same question and zero-result transcript must return without restarting a step client.

## B. Answer and inspect the continuation

1. Pick a listed option, or enter a free-form answer. A free-form question accepts an empty answer too. Select **Answer and resume** once.
2. Expect **Answer saved** while the daemon continues, streamed events when available, then a saved **done** or another **suspended** state. If the answer call succeeds but an event is missed, the panel refreshes from `step.show` until the running state ends.
3. The saved panel should now show one result for the original question use. Open **Canonical transcript** and confirm that the result contains the answer and that earlier tool results were not repeated. Select **Verify transcript** and record the result.
4. Reopen the same ID after closing the tab. The saved result should still be present, and no answer form should be offered for the answered question. A second answer through the CLI (`aeon answer ID another-answer`) must be refused with the first answer's timestamp.

## C. Changed and missing declared files

1. Suspend a new step with `README.md` under **Declared context files**. Edit that file on disk while the step waits. Answer in **Step**.
2. Expect the saved panel to show a **Changed while this step waited** note; the opening transcript still contains the original content.
3. Repeat with a new suspended step, deleting `README.md` before answering. Expect a distinct **Missing while this step waited** note.

## D. Bounds, compaction, and route swap

1. To exercise compaction, edit the route in **Providers** and set **Context window** to the model's actual token budget. Use enough complete turns to cross the budget. Expect a `step.compacted` stream line, a **Compactions** section with message range, estimates, summary and cost, and a successful **Verify transcript** result. A very small budget with no safe range should yield `blocked` with a reason.
2. For two suspensions, ask and answer twice under the same step ID. Check the saved manifest in **Store** or `aeon step show ID --manifest`: `iterationsUsed` must accumulate, and `deadlineAt` must stay the original value.
3. For provider replay, suspend under one provider, then edit the **same route ID** in **Providers** to another provider before answering. Record the result and any signature error. This requires two configured providers; a replay-only fixture does not establish live cross-provider compatibility.

## E. Release gate: restart and elapsed time

1. Suspend a step and record the step ID and suspension time. Close the app and daemon. For the required release check, reboot the machine.
2. At least one hour after suspension, start the daemon and app, open the ID in **Step**, and answer it. Record the answer time. Expect the continuation to finish without rerunning a tool whose result was saved before suspension.
3. A quick daemon restart is a useful extra check, but does not satisfy the physical reboot or elapsed-time check.

## Automation available without a provider key

From `aeon/`, run `npm --prefix ui test -- --run test/step.test.tsx test/providers.test.tsx` for the UI contract and `go test ./cmd/aeon -run TestAnswerCLIThroughRealDaemonAndReplayProvider -count=1` for the shipped CLI, temporary daemon, SQLite store, and replay provider. The UI test uses a fake daemon; it verifies the RPC parameters, subscription order, saved-step rendering, and answer flow. It does not prove the desktop window or a live provider.

## Walk record

| Check | Step ID / evidence | Result |
| --- | --- | --- |
| UI contract tests and replay-provider CLI | Automated tests; no provider key | PASS on 2026-10-17 |
| Desktop ask, suspend, and reopen | Record ID and transcript count | Pending manual walk |
| Desktop answer, saved result, and transcript verification | Record ID and result | Pending manual walk |
| Changed and missing declared files | Record both IDs and notes | Pending manual walk |
| Live compaction and two suspension bounds | Record ID, range, cost, manifest | Pending manual walk |
| Live cross-provider continuation | Record route and provider pair | Pending manual walk |
| Physical reboot and answer after at least one hour | Record suspension and answer times | Pending release QA |
