# S66 DEMO — the walked live record (2026-10-31)

Lane: real `aeon serve` binary (built with `sqlite_fts5` tags) in a
throwaway env (`LOCALRPC_ENDPOINT_PATH`, `XDG_STATE_HOME`,
`XDG_DATA_HOME`, `AEON_SKILLS_DIR`, `AEON_USER_SKILLS_DIR` all under
`.tmp/s66/`), raw unix-socket JSON-RPC client (python), real fsnotify.

1. **Boot**: `skills: watching both roots — dropped-in folders are
   noticed without a restart` in the startup log. `aeon skills list`:
   `watch   on — folders dropped into either root are noticed without a
   restart`. (The first binary of this task got this line WRONG —
   `watch off —` with an empty reason — because the `skills.list`
   handler's field-by-field projection dropped the new pair; fixed and
   re-walked. The empty reason beside "off" is exactly the lie P31
   forbids.)
2. **Drop**: `mkdir .tmp/s66/user/docx && write SKILL.md` while
   subscribed to `skills.changed` → `{"added":["docx"],"watching":true}`
   after **0.50 s**; a same-connection `skills.list` immediately lists
   `docx` — no restart, no validate click. `rm -rf` the folder →
   `{"removed":["docx"],…}` after **0.50 s**. (500 ms debounce + scan
   time; the walk measured, not the budget.)
3. **Injection**: with `caveman` (valid) present and a dummy route
   (`--no-price`, key file — no provider call ever succeeds), a real
   `aeon step "say hi briefly" --route probe` produced, on the wire:
   `step.started` payload `skills = {"count":1,"names":["caveman"],
   "marker":"skills injected: 1 — caveman"}` and summary
   `… · skills injected: 1 — caveman`. The chat page renders THAT
   field as the injection row (14 vitest tests cover the mapping and
   the row; the wire now proven live too).
4. **Honest absence**: the same step with an empty catalog emits
   `skills:null` and renders no row (vitest-pinned; the daemon-side
   empty offer is provider-returns-nil, T2-pinned).

What this walk does NOT show: the Tauri window (jsdom lanes stand in),
the Windows named pipe, and the chat row inside the actual desktop app
— the row's two inputs (the wire field, the mapper) are each verified
at their own level, the composed render is verified in jsdom, not in
the native window.
