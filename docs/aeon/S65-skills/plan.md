# S65 plan — Agent Skills

| Task | Delivers |
| --- | --- |
| [T1](tasks/T1.md) | `internal/skills` discovery: roots (`internal/paths` additions), lenient frontmatter parse, spec validation with three-state fold, manifest counts, shadow ordering — real-corpus fixtures |
| [T2](tasks/T2.md) | The install/remove engine: GitHub-locator grammar, staging-then-rename placement, pre-placement validation against the FINAL name, force/trash semantics, managed-only removal with `Revealed`; never executes anything |
| [T3](tasks/T3.md) | The socket family: `skills.list · get · validate · install · remove` behind a `Table.Skills` seam (V13), `ErrSkillsRefused` −31820, seven conformance fixtures, table tests over a fake catalog |
| [T4](tasks/T4.md) | The wiring: `daemon.Options.Skills`, entry-point construction + startup scan logged not fatal, the narrow-surface guard with its can-fail meta-test |
| [T5](tasks/T5.md) | The CLI: `aeon skills list|get|validate|install|remove` — pure socket clients (P34), canned-struct printer tests |
| [T6](tasks/T6.md) | The app: `lib/skills.ts` + `SkillsPage` (bench order 4500) + 9 vitest; contract via `make protogen` |
| [T7](tasks/T7.md) | Gates, the live demo walk, the record (SLICES rows, task Notes, cumulative DEMO §S65) |

Order: T1→T2 are one package and landed together; T3 after them (the
fixtures round-trip pins the wire to the Go shapes); T4–T6 fan out; T7
closes.
