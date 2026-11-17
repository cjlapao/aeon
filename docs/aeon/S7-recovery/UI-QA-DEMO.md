# S7 Recovery test bench QA demo

This walk uses the local fixture server and disposable state. No provider key is needed. It exercises the real daemon and real `SIGKILL` crashes; the UI checks below are a repeatable manual inspection procedure.

## Produce the recovery records

From `aeon/`, run:

```bash
bash scripts/qa-s7-recovery.sh
```

The script prints `QA artifacts: /tmp/aeon-s7-qa-...` and ends with `Replay crash walk passed`. Keep that path. It contains separate state directories and logs for seven checks: completed and incomplete `bash` receipts, a suspended question, a complete tool tail, a lost model turn, a dry run, and a crash during recovery. The script uses the debug build tag for its controlled crash hooks and kills only daemons it starts.

## Open a saved case in the test bench

The following commands use the directory printed above. Replace the example path with your script's path:

```bash
cd aeon
export AEON_STATE_DIR=/tmp/aeon-s7-qa-EXAMPLE/incomplete/state
export LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s7-qa-EXAMPLE/incomplete/ui-rpc.sock
go run -tags aeondebug ./cmd/aeon serve
```

Open the Tauri test bench from another terminal using the same socket path:

```bash
cd aeon/ui
LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s7-qa-EXAMPLE/incomplete/ui-rpc.sock npm run tauri dev
```

In **Recovery**, inspect step ID `1`. It should show `done`, outcome `failed`, Case C, a structured `unknown-tool-outcome` naming `bash`, and a receipt with `outcome unknown`, no completion time, and no captured result. Compare the panel with `incomplete/result.txt` and `incomplete/receipt-before.txt` in the artifacts.

Stop that daemon, point both terminals at `completed/state` with a new socket path, and inspect step ID `1` again. It should show Case C, outcome `ok`, and a completed receipt with a captured result. The replay script compared the command's marker timestamp before and after recovery to prove it was not run twice. For `model/state`, inspect step ID `1`: Case B records that the model call was reissued and cost is unknown with reason `stream-aborted-early` because the first stream was killed before usage arrived. For `complete-tail/state`, Case A keeps one saved read result.

The **Recovery stream** replays retained `recovery.*` events from the selected state and then follows new ones. It lists case, progress (`index` of `total`), outcome, and structured failure details. A missing event from an older, pruned log does not erase the saved recovery record or receipts; inspect the step for durable evidence.

## Checks recorded for this implementation

- `bash scripts/qa-s7-recovery.sh`: passed all seven replay cases on 2026-10-17.
- `ui/test/recovery.test.tsx`: verifies saved failure and receipt rendering, replay subscription from `after: 0`, event deduplication, progress, structured failure text, and refreshing the inspected step on resolution.
- The Tauri window walkthrough above remains a manual QA step; no desktop window or live provider was used to claim the replay result.
