# S74 — plan

Order: T1 → T2 → (T3, T4 in parallel) → T5 → T6. T1 and T2 are daemon work; T3 and T4 are UI.

| Task | What | Depends on |
| --- | --- | --- |
| [T1](tasks/T1.md) | Address, category and access on the catalog | — |
| [T2](tasks/T2.md) | Wildcard `toolName`: validation, matcher, evaluator, `tools.matchPattern` | T1 |
| [T3](tasks/T3.md) | Policy form pickers and the live preview | T2 |
| [T4](tasks/T4.md) | Catalog page columns, filters and the policy link | T1 |
| [T5](tasks/T5.md) | The optional read-all seed proposal (closed: not implementable, see its Notes) | T2 |
| [T6](tasks/T6.md) | Walk it on a running app, close out | T3, T4, T5 |

Shared-file care: `internal/policy/spec_toolaccess.go` and `SpecFields.tsx` are touched by T2 and T3 only. Every file stays under 1000 lines (SIZE1); measure after each change.
