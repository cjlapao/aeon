# S8 Blueprints test bench QA demo

This walk uses a local replay server and a disposable workspace. It exercises the real daemon, store, `write_file` tool, loop runner, gate, and UI protocol without a provider key. The replay fixture writes deterministic artifact content; it does not evaluate a live model's ability to follow a prompt.

## Run the replay check

From `aeon/`:

```bash
bash scripts/qa-s8-blueprints.sh
```

The script prints `S8 replay walk passed; QA artifacts: /tmp/aeon-s8-qa-...`. It checks that `two-step` passes strict validation, `dev-loop` has zero invalid diagnostics, the first step creates `SURVEY.md`, the gate stops before `SUMMARY.md`, answering the gate creates `SUMMARY.md`, the original loop retains its pinned YAML after a source edit, and a new loop pins the edit. The directory contains `waiting.json`, `done.json`, both pinned views, both artifacts, and daemon/server logs.

## Open the same state in the UI

Replace `EXAMPLE` with the path printed by the script. It leaves the state and workspace in place but stops its processes. In terminal one, from `aeon/`:

```bash
export AEON_STATE_DIR=/tmp/aeon-s8-qa-EXAMPLE/state
export LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s8-qa-EXAMPLE/rpc.sock
export ANTHROPIC_API_KEY=unused-by-replay
go run ./cmd/aeon serve
```

In terminal two:

```bash
cd aeon/ui
LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s8-qa-EXAMPLE/rpc.sock npm run tauri dev
```

Open **Blueprints**. Select `two-step`, then check that the YAML and editable source path appear. Press **Validate**; expect zero invalid and zero unverifiable diagnostics. In **Loop ID**, paste the original ID from `waiting.json` or `done.json` and press **Refresh ladder**. The ladder should show `survey`, `approve`, `summarise` in that order, with `done` as the final status and the gate and both model steps recorded. Expand **Pinned YAML**: it must have no `editorial-check` lane, while the source editor does. Open the new loop ID from `new-pin.txt` and confirm its pinned YAML includes `editorial-check`.

The **Loop events** panel subscribes to retained `loop.*` events. It should show `loop.started`, `loop.advanced`, `loop.waiting`, and `loop.finished` for the original loop. The page deduplicates repeated sequence numbers. Refreshing the ladder reads durable state, so it remains useful if old events were pruned.

## Prompts to copy into the test bench

Use a new disposable workspace and a running fixture server when starting a new loop. The replay fixture has a fixed file-write sequence, regardless of prompt wording. These prompts describe the intended live behavior and make the expected artifacts clear:

**Basic two-step goal**

> Survey the Go packages in this workspace. Write SURVEY.md with the packages and one sentence about each. After I approve it, write SUMMARY.md with the three most useful findings. Do not use bash.

**Missing-artifact check**

> Survey README.md, but finish without writing SURVEY.md.

With a live model that obeys this request, the engine must turn an `ok` handoff into a `reject` naming `SURVEY.md`; the loop must not reach the gate. The replay fixture always writes the file, so run `TestMissingProducedFileRejectsStep` for the deterministic version of this check.

**Step question before the gate**

> Before writing SURVEY.md, ask me which package to focus on. After my answer, write SURVEY.md and then stop at the blueprint's approval gate.

This requires a model or a question fixture that actually invokes `question`; wording alone cannot force a tool call. Answer the step question in **Step** or with `aeon answer STEP_ID CHOICE`. The loop should then reach the separate `approve` gate. Answer that gate in **Blueprints** or with `aeon answer LOOP_ID approve`. A gate answer advances a loop position; a step answer resumes the saved model step.

**Code action refusal**

> Complete the dev loop and open a pull request.

`dev-loop` may be inspected and validated now, but its future S13 handlers and S16 code actions are reported as `unverifiable`. If execution reaches a code action, the loop must show `blocked` with `unimplemented-action` and remain at that position. It must not report a completed pull request.

## Edit and validation checks in the UI

1. Change `schemaVersion: 1` to `schemaVersion: 99` in the editor and press **Validate**. Expect an invalid diagnostic. **Save source** must refuse it, and the file on disk must remain valid.
2. Restore version `1`. Add a lane before `questions:`:

   ```yaml
     - id: editorial-check
       step: {kind: gate, question: approve-survey}
   ```

   Validate and save. The source changes on disk. An existing loop keeps its pinned YAML; a new loop uses the added lane.
3. Select `dev-loop` and validate. Expect **zero invalid** and named `unverifiable` diagnostics for S13/S16. The strict CLI command `aeon blueprint validate --strict dev-loop` must exit nonzero. No future action or handler is silently treated as working.
4. For a gate waiting in the UI, choose an answer from the options returned by `loop.show` and press **Answer gate**. The ladder should refresh and the event stream should advance. Answers outside that set are refused by the store.
5. Choose `revise` on a fresh `two-step` gate. The loop should return to `survey`; its `survey_revisions` counter caps that edge at two. Choose `abort` at a later gate. With no `on_abort` handler in `two-step`, the loop records `blocked` and says it was aborted. `dev-loop` declares `on_abort: git-abort`; that lane is reached, then its S16 forge action is refused at the terminal position.

## Checks recorded

- `bash scripts/qa-s8-blueprints.sh`: replay walk passed on 2026-10-18.
- `internal/loop` replay tests: gate stop and resume, closed answers, artifact enforcement, and `loop.finished` event passed.
- `ui/test/blueprint.test.tsx`: editor validation/save, start, pinned ladder, gate answer, and event display passed.
- A desktop visual walkthrough and a live provider step-question walk remain manual QA. No live provider was used for this result.
