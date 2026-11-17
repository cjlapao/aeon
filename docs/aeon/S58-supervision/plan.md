# Plan — S58 Supervision

## What is settled before planning

`brief.md` (the measured gap and acceptance) and `design.md` SV1–SV18. Short version: the inbox is **derived** from live state through registered sources (questions, blocked,
stopped here; budget/stuck, gates, scheduler attach from S55/S57/S27), with only acknowledge/snooze stored; the three hard-coded notification hints become a seeded inert
`notify.events` record set with a closed, user-text-free placeholder vocabulary; outbound **signed-webhook channels** are human-door authority records (never seeded, secret by
reference, SSRF-safe bounded dispatcher, closed envelope, append-only delivery log, `nettest` invariant extended); the Tauri shell keeps the webview alive in a tray so OS
notifications arrive with the window closed, with `aeon://` deep links that navigate and never answer; `mission.status` gives the per-root overview; everything daemon-first, CLI second, agent reads own-tree.

## How we will do it

Investigate and characterize first (T0, then T1's migration + goldens of today's hints). Then the **attention core** (T2) and its **write half** (T3), which together need nothing from the
sibling slices. The **event classes** (T4) replace the hint switch behind byte-identical goldens. **Channels and the dispatcher** (T5) are the risky authority piece and carry the P4 guards
from the first commit. Then the doors in parallel: mission status + CLI + agent tools (T6) and the Tauri shell (T7), then the app adapters (T8), the consolidated guard/classification task (T9) and the close-out (T10).
S55/S57/S27 sources are plugged in by **their** slices through the T2 interface; this slice ships the interface, the fake sources for tests, and the seed-file convention.

## Task sequence

### T0 — Re-verify, inventories, `secrets` API, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Migration, store half, and characterization goldens ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — The attention core: sources, items, severity, doors, `inbox.list/summary` ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Needs S44-extra's `rootId` (T0 records the merge state).

### T3 — Acknowledge and snooze: the write half, audited, human-door ([`tasks/T3.md`](tasks/T3.md))
**Depends: T2.**

### T4 — The event-class table: `notify.events` seed set and the resolver ([`tasks/T4.md`](tasks/T4.md))
**Depends: T1; S43-extra T3 (engine).** Parallel-safe with T2/T3.

### T5 — Channels, the dispatcher, the delivery log, the `nettest` invariant ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3, T4.**

### T6 — Mission status, CLI (`aeon inbox|status|notify`) and agent reads ([`tasks/T6.md`](tasks/T6.md))
**Depends: T2, T4, T5.** Parallel-safe with T7, T8.

### T7 — The shell: tray, close-to-tray, deep link ([`tasks/T7.md`](tasks/T7.md))
**Depends: T4** (the hint carries `deepLink`). Parallel-safe with T6, T8.

### T8 — The app adapters: enriched hint consumer and `aeon://navigate` ([`tasks/T8.md`](tasks/T8.md))
**Depends: T4, T7; S45 T9** (shared UI components, only if a component is reused). Parallel-safe with T6.

### T9 — Guards, surface classification, docs ([`tasks/T9.md`](tasks/T9.md))
**Depends: T3, T5, T6.**

### T10 — Close-out ([`tasks/T10.md`](tasks/T10.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 → T3 ─┐
         └─ T4 ──────┼─ T5 ─┬─ T6 ─┐
   (S43-extra T3)    │      │      ├─ T9 → T10
                     │      └─ T7 → T8 ─┘
```

## Risks

| Risk | Mitigation |
| --- | --- |
| A channel becomes an exfiltration path | human-door-only (three locks), closed envelope with a sentinel test, `nettest` allowlist, SSRF-safe IP-level dialer, no agent write tool, P4 guards with canaries (T5/T9) |
| A secret leaks into a row, log, audit or result | reference-only storage; raw-DB-bytes and log sentinel tests; no secret in params (file path only) |
| The three existing hints change | P2: goldens in T1 first, byte-identical after T4 |
| The inbox lies by omission | sources named with state; `TestEveryDoorNamesARegisteredMethod`; a consistency test between `summary` and `mission.status.needsYou` |
| A notification sink slows or crashes a loop | dispatcher off the event path, bounded queue, timeouts, drop-oldest; a "dead receiver" test measures loop latency unchanged |
| The tray change widens the shell's authority | Rust-side only; capability file unchanged (asserted by a test reading it); deep-link scheme reviewed in T7's Notes |
| Tray unsupported on a Linux desktop | `close_policy` falls back to a normal close and the doctor panel says why |
| Sibling slices disagree on kind names | brief §6 names the contract; T0 re-checks their state; sources register by name, so a missing one is `not-wired`, not a crash |
| S44-extra not merged when T2 starts | T0 records it; T2 codes against `component.list` roots via the store read, behind one seam |
