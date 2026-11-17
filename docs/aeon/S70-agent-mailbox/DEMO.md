# DEMO — S70 Agent↔agent mailbox

**Status: planning record — implementation pending.** These are the walks T1–T5 will make real; the gates
line stays honest-until-measured. (S43 precedent: the planning session writes this file; the builder
appends dated evidence to the task files.)

## Lane 1 — the bench: a conversation you can see

Bench → Agent chat → Scenarios → the mailbox scenario: the caller agent asks `author` about a section;
the window grows an **exchange row** — `reviewer → author · is the risk section current?` — in its own
tone, grouped under its thread; the answer lands as the matching `author → reviewer` row; a scripted
dead-target thread renders the honest absence naming the thread and the target. The mapper's dishonesty
checks (forged rows drawing nothing) run in the test lane.

## Lane 2 — the terminal: reading a conversation the daemon remembers

`aeon loop messages <id>` prints the thread rows as the STORE has them (from → to, body, delivered state,
thread id); `--thread N` narrows to one conversation. Live and after restart: the same rows, the same
bytes (S39's rule) — the walk is the demo of "rows are the only state".

## Lane 3 — the ask itself (the policy honesty)

A loop whose grants DO carry `ask_agent`: the agent asks, the ask drains at the peer's boundary (running)
or suspends the asker as a row with the wait visible (D16) and resumes cold with the answer as its own
block. A loop whose grants do NOT: the ask is refused naming the tool and the frozen rule — and the
default grant NEVER carries the tool, so the refusal is the shipped default (S11).

## Gates

Not yet measured — implementation pending. When built, this section carries the dated gate block (full
Go sweep with the preamble + park/restore and sweep reds proven pre-existing at the then-current main;
vitest uncontended; tsc baseline list; protogen/check/guard) and the Unrun line (live provider bytes,
Tauri, Windows pipe, real-desktop walk).
