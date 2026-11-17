# ui-design-engineer — drop record

original: opencode/agents/ui-design-engineer-loop.md  (139 lines)
rewrite:  aeon/internal/handler/bodies/ui-design-engineer.md  (102 lines)
delta:    38 lines removed, 1 added

## RITUAL (deleted — the engine does this)
- L47-L49  caller guard checking who dispatched (phase build + tech `ui-design`) → the blueprint routes by tech tag; a body that declines its dispatcher contradicts the step that runs it
- L51-L53  "Load the dev-loop-protocol skill first" + path discipline (project-root-relative; `$ROOT` shell-indirection; built-in tools; never the filesystem MCP) + the 1000-line Onion rule → nothing loads skills here; the relative-path rule survives (Boundaries), the 1000-line law survives in step 5
- L75      step 0: `pwd` + `loopSlug` + the loop-file dir + demo-page placement → placement is produces:/the step's request; project-root-relative survives
- L76      loop_get + loop_readFile(plan) + loop_readTask reads → the step's request carries the task spec ("primary contract" role kept)
- L77      **Re-read prior failures** via loop_get counters/lessons → the state read is gone (the same law survives elsewhere this batch; recorded honestly as deleted here)
- L88      scratch note via `loop_writeFile` (files changed, tokens/recipes, variants + API, screenshot paths, a11y results) → exactly the `visual_proof`/`contrast` handoff fields (REWORDED)
- L89      `memory_append` gotcha + memory.md rendering → no memory tool; rendering DEFERRED
- L90      `event_append` agent-return checkpoint → the daemon emits step events
- L91      FINAL REPORT line (half) → key-values deleted; the verification=pass CONDITION (build+lint + every variant×state screenshotted + a11y passed) and the no-uncaptured-pass law survive verbatim in substance
- L135     "You NEVER mutate loop state... state tools... loop_get" (half) → the handoff IS the output

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, webfetch]`: task DROPPED (A1), todowrite→todo (A19), codesearch/lsp/patch retired (W1), question not re-granted (nothing in the body asks)
- L22-L33  permission.bash deny table (git/rm denies)       → S11 corpus content byte-identically (`TestNoPolicyWasAdded` pins corpus==original)
- L80      step 5 `todowrite` → the `todo` tool (A19)

## DOMAIN (kept)
- L37      the role verbatim in substance: visual identity, React+TS+Tailwind kit, Liquid Glass, own the look, NOT component logic → role line
- L39-L44  the domain mandate verbatim: the own/do-not-own lists (styling-only variants; "When you add a variant, you change *styling/markup-for-styling*, not behavior"); blocked naming the framework engineer (the `*-loop` names restated as the registry's `react-ts`/`tauri-react` — REWORDED); the fixed-stack rule with its styled-components/vanilla-extract ban and the Tailwind-mismatch blocked case; toolchain detection v4-CSS-first vs v3-config + Storybook detection → the mandate section
- L55-L64  the Liquid Glass charter VERBATIM: layered translucency not flat blur; backdrop blur + translucent fill with context-tuned blur; specular highlight + inset rim + gradient slab; saturate vibrancy; soft shadows "Glass floats; it doesn't stamp"; restraint + eased motion + reduced-motion; tokenize blur/fill/rim/saturation/shadow so variants compose from tokens not magic numbers, recipe recorded → the charter section
- L66-L71  the A11y guards VERBATIM with their heading (NON-NEGOTIABLE — glass's #1 failure): WCAG AA ≥4.5:1/≥3:1 against the EFFECTIVE background — verify don't eyeball, scrim if needed; prefers-reduced-transparency opaque fallback; prefers-reduced-motion; no translucency-only state + visible focus rings → the guards section
- L78      the design-system doc rule: read `docs/design_system/{capability}.md` (capability from the plan's `design_system` tag, fallback `ui-kit`), create from schema if absent, extend tokens/recipes/catalog rather than duplicate → Procedure 2 (the path is the document's IDENTITY — a long-lived cross-loop contract; the per-loop screenshot placement is what got delegated)
- L79      read the kit: locate UI-kit, v3/v4, Storybook, target components + variants, token source, `src-tauri/tauri.conf.json` for vibrancy → Procedure 3
- L81-L85  the implementation step verbatim: styling-only composition from tokens+recipe matching kit conventions (class API, variant prop names); named reusable tokens/recipes; Tauri v2 windowEffects least-privilege appearance-only; no file over ~1000 lines → Procedure 5
- L93-L100 the visual verification gate verbatim in substance: no logic to line-cover so visual evidence + a11y replace ≥80% coverage; the bounded command set (typecheck/lint/build timeouts, Tailwind clean compile, tauri config validity, `vitest run` never watch, timeout=FAIL never silent re-run); render each variant × state (default/hover/focus/disabled/dark) via Storybook or a minimal mounted dev page; **server DETACHED with bounded readiness poll and ALWAYS kill even on failure — an orphaned server squats the port for the next agent** (the `kill $PID` line restated) → the verification section
- L103     the rare-TypeScript-logic caveat (then it needs unit tests; styling-only work's gate is screenshots + a11y) → verification section
- L105-L129 the design-system doc schema VERBATIM (long-lived append-only header, tokens table with the three glass rows, recipe shape, component catalog with variant/props/a11y/date lines) → the schema section; this body is its single owner
- L133     visual-layer-only boundary with the blocked-naming route; vibrancy appearance vs window behavior → Boundaries
- L134     one task per turn (line shared with the coder originals verbatim) → Boundaries
- L138     never ship glass that fails contrast or lacks the reduced-transparency fallback — automatic fail, not stylistic preference → Boundaries
- L139     impossible criterion / cannot render → blocked naming exactly what's missing → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L86/L101/L137 the Playwright-MCP screenshot surface (capture into `.projects/loops/{loopSlug}/ui-shots/`; "the Playwright MCP stays enabled") → re-read against S12 per ROADMAP §7 — an operator who CONFIGURES the server gets browser automation; the body keeps the GATE ("no visual pass without a captured screenshot — blocked otherwise") and the shot-naming shape, and names no tool for it (T6's Playwright ruling, applied again)
- L45/L79  context7 doc resolution (Tailwind major, Tauri v2 window effects) → the context7 half rides with S12; the when-it-earns-it judgement survives through `webfetch`, the tool the registry has
- L89/L135 the loop-memory rendering + loop-state MCP (memory.md render, orchestrator state recording) → daemon events + the handoff replaced them
- L137     "Absolute `$ROOT/...` paths (root from `pwd`), built-in tools only; the filesystem MCP is disabled for you" → OpenCode-shaped plumbing: `$ROOT` indirection died with it (paths are project-root-relative, the rule that survives), and a body naming disabled servers instructs nothing (TestNoBodiesNameMCP)

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3       description's dev-loops framing compressed; the never-mutates/one-task clauses restated as boundary prose
- L42/L133 `react-typescript-engineer-loop` / `tauri-react-engineer-loop` → the registry's `react-ts` / `tauri-react` engineers (naming a non-existent handler is how a body routes work to nowhere)
- L75      "A demo page goes at e.g. `demo/...` (relative) or wherever the kit keeps them" kept as the path examples in Boundaries; the screenshot dir sentence delegated to the step's request
- L91      FINAL REPORT evidence string ("3 shots in ui-shots/; contrast 5.2:1...") restated as the `visual_proof`/`contrast` handoff fields with the same shapes
- L100     "ALWAYS `kill $PID`" → "ALWAYS kill the server" (the PID variable was the detached-start idiom, kept beside it)
