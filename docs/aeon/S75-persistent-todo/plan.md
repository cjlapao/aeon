# S75 — plan

Order: T1 → T2 → (T3, T4, T5 in parallel) → T6 → T7.

| Task | What | Depends on |
| --- | --- | --- |
| [T1](tasks/T1.md) | Store: `loop_todo`, `loop_todo_list`, migration, accessors | — |
| [T2](tasks/T2.md) | The tool seam, W20 guard amended, daemon wiring, `todo.updated` | T1 |
| [T3](tasks/T3.md) | `loop.todos` read, `loop.todos.close`, CLI, contract | T1, T2 |
| [T4](tasks/T4.md) | Rehydrate: the `Open todo list` section at compile | T2 |
| [T5](tasks/T5.md) | The nudge and the completion guard in the runtime | T2 |
| [T6](tasks/T6.md) | UI: checklist card, pinned strip, load on open, divider | T3 |
| [T7](tasks/T7.md) | Walk on a throwaway daemon, close out | T4, T5, T6 |

Shared files: `internal/runtime/loop.go` is touched by T5 only; `internal/tools/todo.go` by T2 only; `components/agent-chat/fromEvents.ts` by T6 only. Every file stays under 1000 lines (SIZE1); measure after each change.
