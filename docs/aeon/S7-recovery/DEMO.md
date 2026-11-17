# S7 Crash recovery — the demo

**Stage 2 · Bicycle, second of four.** *S6's step exits because it chose to. This one survives being killed.*

Every step here involves `kill -9`. That is the point: the demo cannot be done gently, because a graceful
shutdown is the case that already worked.

**Replay walk, run 2026-10-17:** `cd aeon && bash scripts/qa-s7-recovery.sh` passed all seven cases below
against a disposable SQLite store and local fixture server. It uses real `SIGKILL` on the daemon it starts,
requires no provider key, and prints the artifact directory. The [test bench QA walk](UI-QA-DEMO.md) shows
the same records in the Recovery tab. A desktop window and live provider were not used for this replay walk.
The manual commands below assume `daemon_pid` is the PID of the disposable daemon process you started;
the script manages that PID for you.

Use a repository you can throw away.

---

## 1. Killed between turns — it just resumes

```
make dev
cd ~/throwaway-repo
aeon step --allow-bash "read every Go file under internal and summarise each one" &
```

Wait until you see a couple of tool calls complete, then:

```
kill -9 "$daemon_pid"
```

**You should see** the terminal report the daemon gone. Now restart it:

```
make dev
```

**You should see** recovery announce itself, before anything else:

```
recovering 1 of 1: step 1 case A
recovery scan: 1 step(s), 1 recovered, 0 failed, 0 skipped
```

**It failed if** recovery is silent. A multi-minute recovery on start with no output looks exactly like a hang,
and the person watching has no way to tell (K35).

**It also failed if** a tool re-ran. Check the transcript:

```
aeon step show <id> | rg -c '\[tool_result '
```

That count should match the number of tool calls that had **completed** before the kill — not one more.

---

## 2. Killed mid-model-call — it re-issues with honest cost

```
aeon step "write a long essay about build systems" &
```

Kill it **while text is streaming**, before any tool call:

```
kill -9 "$daemon_pid"
make dev
```

**You should see:**

```
recovering 1 of 1: step 1 case B no assistant turn was committed
```

**Then check the cost:**

```
aeon step show <id>
```

**You should see** `case B · model call re-issued` on the saved step. If the first call's usage checkpoint
completed before the crash, its cost is added to the second call. If the daemon was killed mid-stream before
usage arrived, the saved cost is **unknown (`stream-aborted-early`)** with the second call's measured cost as a
lower bound. The bill for the first call cannot be reconstructed from a process killed before the provider
reported usage; reporting only the second call as a known total would be false.

**It failed if** a mid-stream crash reports the second call alone as a *known total*.

---

## 3. Killed during a `bash` — it fails honestly

This is the case the slice exists for.

```
aeon step --allow-bash "run: sleep 30 && echo done" &
```

Kill the daemon **while `sleep 30` is running**:

```
sleep 5 && kill -9 "$daemon_pid"
make dev
```

**You should see:**

```
recovering 1 of 1: step 1 case C unknown-tool-outcome: bash receipt ... was started but not completed
recovery scan: 1 step(s), 0 recovered, 1 failed, 0 skipped
```

And:

```
aeon step show <id>
```

**You should see** the step `failed`, with the reason naming **`bash`** and the receipt state.

**It failed if** the command was re-run. `sleep 30 && echo done` is harmless; `git commit` is not, and recovery
cannot tell them apart. Guessing here produces a repository nobody can reason about — so the step fails, and at
S8 a blueprint's redo edge will decide what happens next (K33).

**It also failed if** the step was silently marked `ok` or left `running`. A step left `running` is one the next
start tries to recover again, forever.

---

## 4. Killed *after* the command finished — the receipt earns its place

The satisfying one. Here the command **succeeded** and only the transcript write was lost.

```
aeon step --allow-bash "run: go test ./... > /tmp/out.txt 2>&1; echo captured" &
```

You need to kill the daemon in the window **after** the command exits and **before** its result is written. That
window is short, so start the **debug-tagged daemon** with the hook. The variable belongs on the daemon, which
runs the tool, not on the step client:

```
AEON_DEBUG_CRASH_AFTER_TOOL=1 make dev-daemon
# in another terminal, run aeon step --allow-bash ... against that daemon
# the daemon exits deliberately, after the tool completed and before the write
make dev
```

**You should see:**

```
recovering 1 of 1: step 1 case C bash receipt completed; using captured result
recovery scan: 1 step(s), 1 recovered, 0 failed, 0 skipped
```

**The command did not run again.** Check:

```
aeon step show <id> | rg -c '\[tool_result '
```

**Why this matters:** without the receipt, this case is indistinguishable from §3 — the transcript shows a
`tool_use` with no result either way. The step would fail, and with `go test ./...` that is minutes of work thrown
away for nothing (K31). The receipt is what makes "it finished, we just lost the note" a recoverable state rather
than an unknown one.

Then the check that stops a recovered result landing on the wrong call:

```
aeon debug receipts <id>
```

**You should see** an `arg_digest` recorded per receipt. A captured result whose digest does not match its
`tool_use` is **not** used — the step fails instead. Recovering a result onto a different call would be worse than
failing.

---

## 5. A suspended step is not touched

```
aeon step --allow-question "read README.md and ask me whether to rewrite it" &
# it suspends
kill -9 "$daemon_pid"
make dev
```

**You should see** recovery report **nothing to recover**:

```
recovery scan: 0 step(s), 0 recovered, 0 failed, 0 skipped
```

Then:

```
aeon answer <id> yes
```

**You should see** it resume normally.

**It failed if** recovery touched it. A suspended step also has a `tool_use` with no result — the `question` — so
without the `status` distinction recovery would "recover" every loop that was correctly waiting for a human,
re-issuing a call the model never needed to make (K29).

---

## 6. Crash *during* recovery

```
# create three crashed steps
for i in 1 2 3; do aeon step --allow-bash "run: sleep 20" & done
sleep 3 && kill -9 "$daemon_pid"

# now crash the recovery itself
AEON_DEBUG_CRASH_DURING_RECOVERY=2 make dev-daemon    # dies after recovering 2 of 3
make dev
```

**You should see** the second start finish the job:

```
recovering 1 of 1: step <remaining-id> case A
recovery scan: 1 step(s), 1 recovered, 0 failed, 0 skipped
```

**It failed if** a step was recovered twice, or if the already-recovered ones were touched again. A crash during
recovery is ordinary — recovery is the thing that runs when something has already gone wrong — so each step's
outcome commits atomically (K34).

---

## 7. `--no-recover`, for when recovery is the problem

```
aeon serve --no-recover
```

**You should see** it report what it found and leave it alone:

```
recovering 1 of 2: step <first-id> case A
recovering 2 of 2: step <second-id> case C ...
recovery scan: 2 step(s), 0 recovered, 0 failed, 2 skipped
```

**It still tells you the cases.** Being able to see what recovery *would* do without letting it act is the
difference between debugging a recovery loop and guessing at one.

---

## Findings from the replay walk

| Prediction | What the walk found | Resolution |
| --- | --- | --- |
| A killed model call's exact first-call cost could be added to the retry | Killing before provider usage arrives leaves no reliable token count | Save `unknown(stream-aborted-early)` with measured later calls as a lower bound; never report the retry alone as a known total |
| `ResumeStep` could handle recovery unchanged | Its entry path required a pending human question | Add a recovery mode using the same frozen context and bounds |
| The debug hook could be set on the step client | The daemon executes tools and owns the crash point | Set `AEON_DEBUG_CRASH_AFTER_TOOL` on the debug-tagged daemon |
| A one-off manual kill could reliably hit every window | The completion-to-transcript window is too narrow to time by hand | `scripts/qa-s7-recovery.sh` uses debug hooks and exact disposable daemon PIDs |

The replay walk passed all seven checks on 2026-10-17. The Recovery tab has component tests and a separate
[manual QA procedure](UI-QA-DEMO.md); the desktop window and a live provider were not used in this walk.

---

## What this demo does **not** show

- **No redo.** A step failed by recovery stays failed. A blueprint deciding to retry it is **S8** — this slice
  produces the reason, S8 acts on it.
- **No policy on a recovered `bash`.** It is as unguarded as a fresh one. **S11.**
- **Six of eleven tools still absent.** `write`, `edit`, `glob`, `grep`, `webfetch` and `todo` have their re-run
  class **declared** here and enforced when they arrive at S11.
- **Test bench, not the S9 app.** The Recovery tab inspects receipts and saved outcomes and watches progress.
- **No cross-machine orphan handling.** The daemon owns loops (P29), so a `running` step whose daemon is gone is
  simply a crashed step. There is no `(pid, host)` reconciliation to do.

Next in the bicycle: S8 gives a step a blueprint, and S9 puts the whole thing in a window.
