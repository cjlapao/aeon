# S9 UI QA — one continuing workspace and daemon

> **S38 T4 (2026-10-27)** — the tier this document called *project* is now 
> **component**, everywhere: the screen is **Components** (`/components`,
> `/components/:id`), buttons and tooltips say component, and the wire field is
> `componentRoot`. A NEW **Projects** page (`/projects`) is S38's GROUP tier —
> the groups components belong to — walked in `docs/aeon/S38-org-layers/DEMO.md`
> at slice close; this document's QA walks stay component-tier, with every
> "project" below read as "component".

This is the **manual product walkthrough** for S9 and the starting point for later slices. Use the daemon, database, provider route, and QA workspace you already use. Keep their state across stages. Each new slice adds a loop or action to the same history; it does not create another database. The isolated fixture scripts and automated tests remain in CI, not in this walkthrough.

A real provider can choose different wording, tool calls, and costs. The checks below name durable outcomes and UI states, not exact model text or dollar amounts. This walkthrough makes real model calls through your configured route.

## Keep one QA context

Record these once in your local QA notes. Keep credentials out of the notes.

| Value | Where to find it | Your value |
| --- | --- | --- |
| Daemon socket | Startup output or **System → Socket** | __________ |
| State database | **Test bench → Store** | __________ |
| Provider route ID | **Test bench → Providers**; choose an existing route showing a key | __________ |
| QA workspace | One persistent absolute directory containing `go.mod` | __________ |
| Saved component | Name and slug for that same QA workspace, if created | __________ |
| S9 baseline loop | Copy its `#/loops/...` URL after starting | __________ |
| Earlier S13-blocked loop, if present | Copy its `#/loops/...` URL | __________ |

If the daemon is already running, keep it. If you normally use the default state and need to start it, run `make run` from `aeon/`. **Do not set a new `AEON_STATE_DIR`, delete `state.db`, or run `scripts/qa-s8-blueprints.sh` for this walkthrough.** Open the app against that daemon's printed socket, for example from `aeon/ui/`:

```bash
make tauri-dev AEON_ENDPOINT=/absolute/socket/path/printed/by/daemon
```

On Linux, `make tauri-dev` also registers a hidden **Aeon (dev)** desktop entry in your user applications directory. On GNOME/Wayland its filename matches the dev window's app ID, so the dock should show the Aeon mark rather than a gear. Close any window started before this icon change and run the command again. The registration does not limit the two-window QA checks below.

Keep the same endpoint and state on later visits. The app should show **current** in its header and **System**. In **Test bench → Providers**, confirm the selected route is configured and has a key. If it is absent, configure a route there before starting a loop; do not put the key in the goal or this document.

Use a dedicated QA workspace so later stages can build on the files without changing Aeon's source tree. It needs a small Go program and a `go.mod`. If you do not already have one, create it **once** and keep it at the same absolute path:

```bash
qa_workspace="$HOME/aeon-qa-workspace"
mkdir -p "$qa_workspace/cmd/hello"
if [ ! -e "$qa_workspace/go.mod" ]; then
  cat > "$qa_workspace/go.mod" <<'GO_MOD'
module example.com/aeon-qa

go 1.22
GO_MOD
fi
if [ ! -e "$qa_workspace/cmd/hello/main.go" ]; then
  cat > "$qa_workspace/cmd/hello/main.go" <<'GO_MAIN'
package main

import "fmt"

func main() { fmt.Println("hello") }
GO_MAIN
fi
```

Do not repeat this setup after a later stage changes the program. Enter the **absolute** workspace path in the app; a literal `~` in the form is not expanded. The app and daemon trim surrounding whitespace before storing the path. The workspace is persistent, as is the daemon database.

The daemon must see this path in **its own filesystem** before you start a loop. A terminal or app on another host,
container, or mount can show a directory that the daemon cannot open. Current builds check the workspace at
**Start loop** and refuse it before saving a loop or calling the provider if the daemon cannot resolve or list it.
If you see “not visible to the daemon,” first compare the displayed workspace path with the real directory,
including hidden trailing spaces. Then compare **System → Socket** and **Test bench → Store** with the daemon you
meant to keep running, and use a path mounted at the same absolute location for that daemon. A loop already
blocked by a missing workspace keeps its recorded outcome; after correcting the path, start a new S9 baseline loop
and record that URL.

## Save the continuing QA component

In **Components**, click **New component**. A right-hand ui-kit side panel should slide over the dashboard with a dimmed backdrop, a scrollable form, and **Create component** and **Cancel** in a footer that stays visible while scrolling. Drag its left resize handle to widen it, then focus that handle and use the left/right arrow keys to resize it. Close with **Cancel**, the close button, or Escape, and reopen; unsaved values should be discarded. Enter a name such as `Aeon QA`, accept the suggested slug `aeon-qa`, and enter the same absolute QA workspace path above. Add the optional metadata you use for this repository: remote URL, owner, comma-separated tags, and, under **Advanced identity fields**, remote key and repository key. Click **Create component**. The panel should close and the component should appear immediately even if this workspace has no loops yet. Open it and check the name, slug, root, optional values, and creation/update times. **Start a loop here** must open the New loop form with this workspace prefilled. Return to Components and reopen the record to confirm it persists in the same database.

If this workspace already has recorded loops, the Components page first shows it as a **Recorded workspace**. Saving it with the same root should turn it into a **Saved component** and keep those loops in its history. If you saved this QA component on an earlier visit, open that record instead of creating a duplicate. A second component cannot claim the same root. Keep using the provider route recorded above when starting each loop.

### Components overview dashboard

Return to **Components** without selecting a workspace. Five ui-kit stat tiles show the workspace count, active loop count, approval requests, blocked loops, and daemon state above a scrollable grid of component cards. In a wide content area they share one row; narrow the window and confirm they wrap into more rows without clipping. In **Aeon → Settings**, change the Controls tone: the four ordinary stat tiles should follow it while **Blocked loops** remains red. The saved QA component has its chosen icon or initials, its absolute root, loop counts, and a link to its component history. A workspace with loops but no saved component appears as **Recorded workspace**. Search by component name, root, or loop title; try **Saved**, **Recorded**, and **Attention**, then return to **All**. Sort by name and by loop count, then return to **Most recent**. With more than nine workspaces, scroll inside the component grid to load the next cards while the page heading and metrics stay in place. There is no **Bind your path** tile and no **What the component model stores today** disclosure.

During Walk 1, leave the `approve` gate waiting and return here. The approval count should rise, and **Current loop activity** should show that loop with its workflow rail and selected step details. Use **Open full workflow** to inspect the transcript and artifacts. You can answer **Approve** or **Revise** from the dashboard's gate actions; for the prescribed Walk 1 revision, return to the loop page and answer there. If another loop is running or waiting in the same database, both active loops should appear as separate activity cards. When all loops are done, blocked, failed, or aborted, the activity section disappears; their component cards and history remain. A blocked loop still contributes to the blocked count and **Attention** filter.

On the saved QA component, click **Edit component**. The same resizable side panel should open with the existing values and **Save changes** in its fixed footer. Change the display name or owner and add an emoji in **Icon emoji**, then save. The component card and detail header should show the emoji, the edited metadata should persist after leaving and returning, and the baseline loop should remain under the same root. Edit again, click **Use initials**, and save: the icon should become the first letters of the name (or slug if there is no name). You can also use **Upload image** with a PNG, JPEG, WebP, or GIF of at most 256 KiB; its preview and saved component card should show the image. An unsupported or oversized file should produce an error without changing the saved icon. The workspace root is shown but cannot be edited because it is the identity used by existing loops.

### Component file browser preview

Stay on this same saved component and workspace. The **Component files** panel is a read-only trial of ui-kit's `Tree` file browser. Click **Browse workspace**: its root should show `go.mod` and `cmd`; select `go.mod` to see monospace source, then select `cmd` and `hello` to reveal `main.go`. The folder rows expand inside the tree, and the arrow keys can move through them. After Walk 1 writes `SURVEY.md`, return here, click **Refresh root**, and select `SURVEY.md`; its heading and lists should render as Markdown. Later generated documents should appear in this same tree without changing the daemon database.

To exercise image and binary previews once in the persistent QA workspace, create two small fixtures there (use the absolute path you recorded above):

```bash
qa_workspace="/absolute/path/to/your/aeon-qa-workspace" # replace with the path recorded above
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6dJkAAAAASUVORK5CYII=' | base64 -d > "$qa_workspace/PREVIEW.png"
printf '\000\001\002' > "$qa_workspace/PREVIEW.bin"
```

Click **Refresh root**. Selecting `PREVIEW.png` should show the image; selecting `PREVIEW.bin` should say it cannot be opened in the preview. The panel should retain a visible error if the desktop app cannot access the component path. The browser uses local Rust commands, so rebuilding the desktop shell is required after this change; restarting the daemon alone does not add these commands. Files over 1 MiB of text or 8 MiB of image data are reported as too large to preview.

## Walk 1 — one real `two-step` loop

Before starting, open **Blueprints → two-step** and inspect its source. It should say `version: 2` and the
`approve` lane must contain `on_reject: {goto: survey, counter: survey_revisions, max: 2, then: block}`. Older
materialized copies lacked this edge even though their gate offered `revise`; after updating Aeon, opening the
blueprint upgrades that exact unmodified copy (and the later pristine version 1 copy). An edited blueprint is kept
as edited, so add the edge yourself if you deliberately customized it.

On the same **Blueprints** page, inspect the **Blueprint structure** connection flow. For `two-step`, its single track should read `survey → approve → summarise`. The `approve` card should say **Approval gate**, show the question **“Approve the survey and continue?”**, and show a rose return branch to `survey` labelled `survey · max 2 · then block`. Select `dev-loop` without starting another loop: its `build` lane should say **For each tasks** and contain one vertical column of ordered `issue`, `implement`, `verify`, `security`, and later ladder steps within the same flow. Each ladder step occupies its own row. The conditional `security` card should show `flags.security_scan`, and `review` should have a return branch to `implement` labelled with its retry limit and fallback. Use pan, zoom, and fit controls to inspect the taller graph. The source and validation report must still match the selected blueprint. Invalid or incomplete YAML should show **Flow unavailable** while keeping the source and diagnostics readable. Conditions and fallback outcomes remain text on cards; only explicit `goto` targets become graph branches.

In **Loops → New loop** choose:

| Field | Value |
| --- | --- |
| Blueprint | `two-step` |
| Workspace path | Your persistent QA workspace above |
| Route | The existing configured provider route you recorded |
| Mode | `Standard` |
| Goal | Paste the prompt below |

> [QA-S9-BASELINE] Inspect go.mod and cmd/hello/main.go in this workspace. In the survey lane, use list and read, then write SURVEY.md with the module name, the Go packages you found, and what the program currently prints. Do not change the source files. In the summarise lane, after I approve the survey, write SUMMARY.md with three findings based on that survey. Use write_file for the declared artifact in each lane. Report ok only after the file for the current lane exists.

Click **Start loop** once and save its loop URL in your QA notes. You should see the ladder move from `survey running` to `survey ok`, with a link to the step transcript and `SURVEY.md` as its declared artifact. The next lane must be `approve waiting` with the inline question **“Approve the survey and continue?”** and choices `approve`, `revise`, `abort`. The exact survey prose and model cost will vary. If the model does not produce `SURVEY.md`, the loop must report a rejection or block rather than silently mark the step `ok`; inspect the step transcript for the cause before trying another loop.

Leave the gate untouched for five minutes. **System** and the header should stay `current` while the socket is healthy. This waiting loop is now a durable QA checkpoint. Close and reopen the app, or restart the **same** daemon with its existing state, and return to the saved loop URL: the gate and its choices should still be there. A restart must reread the stored state; it must not invent an answer.

Click **revise** once. The thread should add a daemon-confirmed `You · revise` row, and the position should return
to `survey` on the **same loop ID**. The model runs that lane again and writes a revised `SURVEY.md`; the gate then
waits again. The `survey_revisions` edge allows two reworks before its `block` cap. The exact revised prose varies.
Now click **approve**. The thread should add a daemon-confirmed `You · approve` row; it must not show your answer
before confirmation. The `summarise` lane should run and finish. Expect `SUMMARY.md` in the same QA workspace and
the loop to read `done`. If you click twice or answer from two windows, only one answer may persist; a losing
request that reaches the daemon shows a refusal, without a second `You` row.

## Walk 2 — inspect the finished run in the app

### Component and document view (added after S9)

Use the same daemon, database, provider route, and QA workspace from Walk 1. Open **Components** in the product menu. The saved QA component appears with the S9 baseline loop and any later loops recorded under that same root. Open the component and confirm its metadata and baseline loop are listed; **Start a loop here** opens the New loop form with that exact workspace path filled in. Return to the saved baseline loop.

Check the app shell while moving between **Components**, the saved loop, **System**, and **Test bench**. The main side menu contains Loops, Components, and Blueprints. Its top **Aeon** workspace item shows the app icon and title; open it to find System, Settings, and Test bench in the dropdown. Those three destinations should not appear as ordinary navigation rows. The side menu and top header should stay in place; scroll a long loop transcript or file view and confirm only the page body moves. The header must keep the connection state visible. On a narrow window, use **Open menu**, then **Aeon**, to reach the same dropdown pages; the menu should close after selection and the page body should still scroll. The test bench uses this same page area, without a second outer page scrollbar.

Check route restoration in that same window. Copy the saved loop's `#/loops/...` URL, open **Aeon → Settings**, then paste the loop URL back into the address bar and confirm the same loop and selected daemon state reopen. Open **Aeon → Test bench → Files** and copy its `#/bench/files` URL; visit another page, paste that URL, and confirm the Files tab is selected. Switching to **Store** should change the hash to `#/bench/store` while the header connection stays current. A made-up hash such as `#/does-not-exist` should show **Page not found** inside the app shell with a link back to Loops. These navigation checks use the existing daemon and database.

Open **Aeon → Settings** from the top workspace item. Under **Appearance**, choose **Light**, **Dark**, then **System**. Each choice should recolor the menu, glass cards, controls, and page background. Close and reopen the app: the chosen mode should persist. With **System** selected, changing the operating system appearance should update the app without another click. Under **Default tones**, use the ui-kit swatches to set Controls to blue, Text to orange, and Surfaces to violet. The live preview, side menu, inherited text, page labels, panel tint, and controls should change immediately; the choices should remain after opening Components, a loop, the test bench, and after restarting the app. Status colors such as blocked red and approval amber retain their meaning. Restore your preferred tones when done. The **Other configuration** panel links to the current provider, system, and component screens; these settings remain in their existing places.

With the floating side menu visible, check its card body, Aeon top item and icon, border, labels, and dropdown in both Light and Dark modes. In Dark mode the body and header should both be dark; a white menu body with only changed icons or accents means the kit's dark utility rules are being overridden by Aeon's stylesheet.

The Aeon top item should show the app cube icon beside the title rather than a generic icon or letter `A`. Collapse and reopen the menu, then open the mobile menu at a narrow width; the same sharp icon and its dropdown should remain usable against both Light and Dark glass surfaces.

Look at the app background behind the Settings cards and in the gaps between the side menu, header, and content panels. In Light mode, those exposed areas should be solid white throughout, including the upper right; changing **Controls** must not tint them cream. Panels may still show the selected **Surfaces** tone. In Dark mode, the Controls-colored radial glow remains over the dark base. Check that page text and controls remain legible. Scroll to the bottom and switch pages to confirm the background remains stable across routes.

The workflow starts with a full-width glass header panel. Its first row shows an icon-only left-caret back button beside **Blueprint workflow** and the loop slug, with the single status pill at upper right. The button tooltip says **Component loops** and returns to that component's loop list. The second row aligns the blueprint type with the left edge of the back button and shows the workspace path on the right, directly below the status pill, without an extra folder label. The **Blueprint workflow** text is slightly larger than a caption, and the status pill starts level with the back button. The goal fills the entire row beneath them in a read-only ui-kit Textarea with exactly three visible lines; scroll inside it to read the rest of the long `two-step` prompt, and confirm the full text is selectable. On a narrow window, the workspace path moves below the blueprint type and the goal still fills the available width. The panel also shows **Total model cost**, **Wall elapsed**, and **Recorded step time**. There should be no oversized goal title or second status pill above this panel. Beneath it are three ui-kit parts: **WorkflowTrackerRail** on the left, **WorkflowTrackerStepDetails** at the top right, and **WorkflowTrackerDetails** below it. The cards stack on a narrow window. Click `survey`, `approve`, and `summarise` in the rail: both right-hand cards must follow the selected lane without changing the loop URL, while the run totals remain in the header. These `two-step` lanes each declare one direct `step`, so **none should show a nested substep row or a Sub-steps section**. `survey` and `summarise` are model steps; `approve` is a gate with its prompt or recorded decision. A blueprint lane declared with `ladder` still shows actual nested steps, even if it contains only one step; `iterate` shows its item steps. The lower kit body card contains the selected step's artifacts and transcript, with no separate inspector panel. Each model lane shows its cost and duration. A gate-only lane shows **Approval gate** in place of cost and step time. Nested gate rows use the same label, and the gate details show status and decision without Cost or Step time fields. An unknown cost stays unknown, and a partial one reads **at least**. Wall elapsed time includes pauses for a person; recorded step time sums stored model step spans and may also include time spent waiting for a step answer.

At desktop width, check that the rail remains in the left column and the step details and body stay stacked on the right while selecting lanes. A long transcript must remain readable in the body card; the page scrolls through the rest of the loop below. Narrow the window and confirm that all three cards stack without horizontal overflow. They all read the same daemon snapshot and do not create another database or provider session.

Click `survey` in the timeline. The details card's **Step details** section should show its status, cost, step time, a link to the full step, the saved transcript, and `SURVEY.md` under **Artifacts and changes**, without a second click on `survey`. Open that file there and verify the rendered Markdown against the workspace copy. Select `summarise` to inspect `SUMMARY.md`. Files identified from that step's `write_file` calls are marked **step write**; declared lane outputs without such a call are marked **lane output**. A changed or missing file should show the current read error or change note, rather than old text. Select `approve`: its **Approval gate** details should show the recorded decision and link to the loop thread, with no Cost or Step time fields and no model artifact browser. In a `ladder` lane, clicking a nested step opens the **Selected substep** section.

While the gate is waiting, `approve` must show **Approval requested** in the workflow and a visible prompt above it. The allowed answers appear as kit quick actions in both the rail and step card. Clicking either placement submits one answer through the same RPC as the thread and disables both placements while pending. The thread points back to the workflow instead of offering duplicate gate buttons. A disconnected or read-only session shows no gate actions. If you have the older loop that blocked after answering `revise` with `reject has no rework target`, open its saved URL: the `approve` lane must show **Blocked** and the reason beside the workflow, even though its gate substep records the completed `revise` answer. `summarise` stays pending. That older loop remains blocked; this UI correction does not retry it.

In **Files and documents**, open `SURVEY.md` and `SUMMARY.md` from **Blueprint outputs** or the workspace browser. Each opens as rendered Markdown with headings, lists and tables. Enter `tasks/` if a later loop wrote task documents there. The reader requests the selected file only; **Refresh** rereads the directory and open file after an external edit. A file deleted since the loop ran shows a read error instead of stale content. The **Stored loop documents** group appears when a loop has documents in Aeon's database, and opens those through `loop.read`.

If the panel says the connected daemon does not support workspace file reading, the process on the saved socket is an older build: `loop.files` and `loop.file.read` are registered in the current daemon. Stop that daemon process, rebuild/restart it from this checkout using the **same endpoint and state database**, then reopen the saved loop. Keep the database and QA workspace; do not start a new isolated state. The panel must show the method error rather than claim **No visible files here**.

In **Test bench → Files**, select the same loop and repeat the workspace and stored-document reads. This exercises `loop.files`, `loop.file.read`, and `loop.read` against the same daemon and state used by the product screen. The file reader only displays UTF-8 Markdown, text, JSON, and YAML under the loop's pinned workspace root; it refuses traversal and symlink escapes.

Open the saved loop from **Loops** and check these surfaces:

1. **Workflow:** `survey done → approve done → summarise done`; click each direct lane in the timeline and inspect the kit detail card and its step or gate details below. No duplicate nested row should appear. Expand **Pinned blueprint source** and confirm the version 2 `two-step` definition includes the rework edge.
2. **Thread:** both gate prompts and the confirmed `revise` then `approve` answers remain in order after completion. A model step question, if your provider asked one, appears in the same inline card; the `two-step` blueprint itself guarantees only the gate.
3. **Step:** open each transcript. Inspect the `list`, `read`, and `write_file` calls the model actually made. Expand tool results for full stored output. The context manifest, iteration count, and cost must be visible. A cost may be known, partial, or unknown; an unknown must explain why, and a partial value must read “at least.”
4. **Blueprints:** select `two-step`; it should validate with no missing-agent finding. Select `conversational-map`; its future `task-coordinator` agent should be **unverifiable**, naming S13. Strict verdict changes the verdict, not the diagnostics.
5. **System:** verify the same socket and negotiated daemon/protocol versions. If the daemon disconnects, the app must show stale with age and cause; after reconnection it rereads the saved loop. An ordinary completed run need not show a recovery incident.
6. **Test bench:** the existing Providers, Store, Step, and Blueprints tabs remain available to inspect the same daemon. They must not connect to a separate database.

The model may write more than the two required documents. Do not use an exact token count, output sentence, or cost as the acceptance check. The declared artifacts, gate, durable answer, ordered ladder, and honest uncertainty are the checks.

## Walk 3 — preserve the S13 handoff

In **Loops → New loop**, select `conversational-map`. Today it should read **needs S13 agents**, name a missing handler lane, and disable **Start loop**. This is expected: the current executable registry has `default` and `read-only`; S13 supplies `task-coordinator` and the other specialists. A direct `loop.start` also refuses before saving a new loop. Validation remains `unverifiable`, since the blueprint syntax itself is valid.

If your existing database already contains the earlier `conversational-map` run that blocked at `triage` with `unknown handler "task-coordinator"`, **keep it** and record its loop URL above. It is the historical S13 retry case. Its pinned blueprint and position should remain visible; opening it does not retry it. If there is no such old run, do not manufacture one by changing the database. The disabled `conversational-map` selection is the reproducible S9 checkpoint for a fresh QA history.

[S13 T10](../S13-agents/tasks/T10.md) owns making the handler registry real **and** an explicit, safe retry for the historical blocked loop. Its acceptance must use the saved loop and pinned blueprint, avoid duplicating a completed step, and refuse unrelated blocked loops. [S13 T11](../S13-agents/tasks/T11.md) walks that retry from the app. Once S13 ships, the same `conversational-map` source should become startable; the earlier blocked loop should be retriable only through that deliberate action. Until then, do not describe the old loop as resumed or fixed.

## Handoff to later UI demos

Carry the same socket, state database, provider route, QA workspace, S9 baseline loop URL, and optional historical blocked-loop URL forward. Keep `SURVEY.md` and `SUMMARY.md` as evidence. In your next stage's UI guide, start by reopening the S9 baseline and checking its state before adding that stage's scenario.

| Stage | Add to this continuing QA history |
| --- | --- |
| S10 policy | Run the new policy scenario in this workspace; keep the S9 baseline as the before-policy record. |
| S11 permissions | Use the same provider and workspace to inspect a permission decision; do not replace the S9 loop. |
| S12 MCP | Exercise the new tool boundary on this daemon, leaving prior loops visible. |
| S13 agents | Confirm the `conversational-map` start preflight changes as agents arrive, then retry the saved historical block if one exists; add a specialist build loop in the same QA workspace. |

Automated tests may still create disposable state and use the fixture server; they protect deterministic protocol and crash behavior. They are separate from this continuing manual UI history. Earlier S9 replay evidence is recorded in [DEMO.md](DEMO.md#replay-only-acceptance--2026-10-19). Do not paste provider keys or local database contents into QA notes.
