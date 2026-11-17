# S43 T0 — Investigation Report (agent management)

Grounded in the actual current code at `aeon/` (Go module root). Line numbers verified by read/grep, not
trusted from the design doc. All paths are under `/home/cjlapao/Downloads/new_agents/aeon/` unless noted.
The spec lives at `../docs/aeon/S43-agent-management/` (one level up, repo-root `docs/`).

---

## Q1 — `plugin_agent` table

**It is in the FROZEN legacy block of `internal/store/schema.go`, migration `0023`, NOT a file migration.**
The design's "0024-ish" is a drift; its own caveat said "VERIFY the exact migration number, it moves."

- **Registration** `schema.go:103` — `{23, "0023_plugin_agents", "...", migration0023}`.
- **Body** `schema.go:231-288` (the `const migration0023` block).

Exact column list (verified `schema.go:241-248`):
```
CREATE TABLE plugin_agent (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,   -- 242
  name        TEXT NOT NULL,                       -- 243
  source      TEXT NOT NULL,                       -- 244
  state       TEXT NOT NULL CHECK (state IN ('disabled','enabled','revoked')),  -- 245
  imported_at TEXT NOT NULL,                      -- 246
  CONSTRAINT ck_plugin_agent_state_not_none CHECK (state <> 'not-installed')   -- 247
);
```
**Confirmed: `name, source, state, imported_at` + a state CHECK, exactly as the brief says.**

- Partial unique index (one LIVE row per name; revoked rows are history):
  `schema.go:250` `CREATE UNIQUE INDEX plugin_agent_live ON plugin_agent (name) WHERE state <> 'revoked';`
- Terminal-state **trigger** `schema.go:283-287`:
  ```
  CREATE TRIGGER plugin_agent_no_exit_revoked BEFORE UPDATE OF state ON plugin_agent
    WHEN old.state = 'revoked'
    BEGIN SELECT RAISE(ABORT, 'refused: revoked is terminal (AP4) — a re-import creates a NEW record'); END;
  ```
- **Also in the same migration** (the design's "transitions"): `plugin_transition` table `schema.go:252-274`
  with `ck_plugin_transition_terminal` (262-266: the disabled→enabled→revoked machine, revoked never a FROM)
  and `ck_plugin_transition_reason` (270-273: a revoked transition requires a non-empty reason), plus
  `idx_plugin_transition_agent` (277).
- Read/verify the trigger is asserted by `internal/store/plugin_state_test.go:30`
  (`... WHERE type='trigger' AND name='plugin_agent_no_exit_revoked'` → 1).
- The whole lifecycle (states `disabled/enabled/revoked`) is **untouched by this slice** (AM1); the new
  content table joins `plugin_agent` by **slug**, not name. **Drift to record in T0 Notes:** the design
  repeatedly calls it "0024-ish"; it is `0023`.

---

## Q2 — `Agent` / `CardInfo` / `CardSkill` in `internal/plugin/import.go`

All re-read against the current file (design's line cites were accurate here).

- **`Agent`** `import.go:47-66`. Fields, in order:
  `Name` (48), `Description` (49), `State State` (50), `Requests` (51), `Prompt` (52),
  `Card *CardInfo` (53), `Source` (54), `ImportedAt time.Time` (55), `FirstParty bool` (56),
  `Carried []policy.Policy` (61), `ClaimedFirstParty bool` (65).
- **`CardInfo`** `import.go:406-411`: `Name` (407), `Description` (408), `Version` (409),
  `Skills []CardSkill` (410).
- **`CardSkill`** `import.go:414-418`: `ID` (415), `Name` (416), `Description` (417).
- **The card seam** `import.go:423` `var cardLoader func(path, expectedAgent string) (*CardInfo, error)`;
  `SetCardLoader` `import.go:427`. `nil` cardLoader = unwired = **every card refused** (the safe reading,
  `import.go:273-278`). Production wiring (cmd/aeon) delegates to `agentsvc.LoadCardFile` — one
  implementation, one call (`import.go:402-404` comment). `cardLoader` is called in `readPackage`
  at `import.go:279`.
- **How a card is verified today = S14's SignedCard envelope** — see Q10. The plugin's `CardInfo` is a
  *projection* of agentsvc's `AgentCard`, kept thin by design (the absent-layer sweep forbids plugin
  importing agentsvc; `import.go:399-405`).

---

## Q3 — Policy `Target` / `AgentKind` and the blast radius

`internal/policy/types.go` (re-read; design's `:164` cite is exact).

- **`TargetKind`** `types.go:153-177`: `AllAgents="all-agents"` (162), **`AgentKind="agent"` (165)**
  with the doc "names one agent by name" (164), `LoopKind="loop"` (168), `LaneTypeKind="lane-type"`
  (173), `MCPServerKind="mcp"` (176).
- **`Target` struct** `types.go:220-223`: `Kind TargetKind` + `ID string`. **`ID` is the BARE name**
  (e.g. `review`, not `lane-type:review`); `String()` renders `kind:id` (`types.go:228-236`). For an
  `agent` target, **`Target.ID` = the agent name** — this is exactly the field AM2 migrates name→slug.
- **`ParseTarget`** `types.go:350-370`: `AllAgents` must carry no ID (357); every other kind must carry
  a non-empty ID (362-364); ID must not repeat its kind (365-368). So an agent-kind target's ID is the
  agent name, enforced as a string.
- **`Policy` struct** `types.go:255-263`: `ID`, `Type`, `Target`, `Action`, `Spec json.RawMessage`,
  `Source`, `NotAfter *time.Time`. JSON tags `id/type/target/action/spec/source/not_after`.

**Storage (the `policy` table).** Rebuilt to the closed vocabulary by migration **0016**
(`schema.go:419-430`):
```
CREATE TABLE policy_new (
  id          TEXT PRIMARY KEY,
  type        TEXT ... CHECK (type IN ('tool_access','command_access','route_access','effect_class','cost_cap','skill_access','trigger_access')),
  target_kind TEXT NOT NULL CHECK (target_kind IN ('all-agents','agent','loop','lane-type','mcp')),
  target_id   TEXT,            -- NULL for all-agents, the name otherwise
  action      TEXT ... CHECK (action IN ('allow','deny')),   -- widened by 0019 to admit 'ask'
  spec, source, not_after, created_at, updated_at
);
```
Index `idx_policy_target ON policy (target_kind, target_id)` `schema.go:449`.
`internal/store/policy.go`: `PolicyListAll` (`:192-200`) reads every row, `COALESCE(target_id,'')` and
renders `target_kind || ':' || target_id`; `Add` (`:148`); the per-target read is
`PolicyAcceptanceListFor` `WHERE target_kind=? AND target_id=?` (`:332-334`). `PolicyListAll` returns
**all** rows (no target filter) — a by-target query is the acceptance read, not the list.

**Blast radius (measured, not guessed).**
- **`internal/policy/testdata/corpus.json`** (the command-matcher corpus, generated by
  `tools/corpusgen`, `source: opencode/agents`): **38 distinct agent names** (`grep -c '"agent":'` = 38,
  `sort -u` = 38), **367 rules** (`grep -c '"verdict"'` = 367). Each rule is a command pattern; when
  seeded via `aeon policy seed-corpus --agent NAME`, each rule becomes a `command_access` policy row with
  `target_kind='agent'`, `target_id=<that agent name>`. **So the real agent-name set that agent-kind
  policy rows address is the 38 corpus names** (367 potential rows once seeded). Distinct names include
  `brief-author, code-reviewer-loop, golang-engineer-loop, ...` (38 total, listed in the file).
- **Protocol conformance fixtures** that hold an agent-kind target literally:
  `internal/protocol/conformance/proposal/list.json:18` (`"target":{"kind":"agent","id":"reviewer"}`)
  and `.../proposal/create-agent-import.json:29` (`"id":"reviewer-plus"`). These are conformance
  payloads (test names), not shipped rows.
- **Test fixtures** with `policy.Target{Kind: AgentKind, ID: ...}` use synthetic names
  (`boss`, `main`, `golang-engineer`, `planner`, `golang-engineer-loop`, `reimported-consent`,
  `somebody-else`) — see `internal/policy/evaluate_test.go`, `internal/daemon/policy_gate_test.go`,
  `internal/protocol/policy_test.go:238-239`, `internal/agentsvc/task_test.go`,
  `internal/mcp/public_test.go`. None of these are real agent names.
- **Net for T2:** the real migration touches the **38 corpus agent names** (367 rows once seeded). The
  name→slug backfill must refuse loudly on any name that finds no agent row (DB19), per AM2.

---

## Q4 — Blueprint lane `Agent` field and persistence

- **The field** `internal/blueprint/types.go:85`: `Agent string` inside the **`Step` struct**
  (`types.go:81-109`), tagged `yaml:"agent,omitempty"`. (The design's `:85` cite is exact.)
- Neighbouring fields the design warned about (S32): **`Lane.Parallel`** (`types.go:65`) and
  **`Lane.ParallelGroup`** (`types.go:70`), **`Lane.When`** (54); **`Blueprint.Converse`** (35, names the
  agent that answers after a run — another agent-name reference). `Step.Skill` (93) is
  `<callee>/<skill-id>` — a second agent-name reference (the caller).
- **How blueprints are persisted — FILES, not a table.**
  - Loader: `internal/blueprint/load.go:15` `LoadFile(path)`, `:23` `Load(raw)`, `:191`
    `ResolveWithProject(name, projectDir)`; `internal/blueprint/standard.go:85` `Resolve(name)` returns
    the file path a blueprint name lives at. `internal/daemon/blueprint.go:1051` `bp, err := blueprint.Load(raw)`.
  - **There is no standalone blueprint-definition store table.** `internal/store/blueprint.go` is the
    loop/gate lifecycle (not blueprint storage). Blueprints ride in **two other places as pinned/derived
    copies**: the `loop_blueprint` table (`migration 0014`, `content TEXT` per loop, `schema.go:348-354`)
    and the `event_trigger.blueprint` / `roadmap_item` name columns.
- **Implication for AM4 (deletion-reference check):** a blueprint reference is NOT a formal FK — it is
  **file/structured YAML text** (plus pinned `loop_blueprint.content`). So the blueprint half of the
  delete-guard must **parse each blueprint's YAML and read the lane's `Agent` (now slug) field**, not a
  `LIKE` scan (a substring match would false-positive). The **policy half CAN be a real query**
  (`PolicyListAll` / `PolicyAcceptanceListFor`, filtered by target). The same applies to any
  `agent_chain` edge (`agent_task`/`agent_chain`, Q5) and a task row with an `assignee` (Q5).

---

## Q5 — All by-name agent references (AM2 file list + wider grep)

Classified **real** (a name used as a lookup/FK/identity) vs **incidental** (local var/comment).

**AM2 file list**

| File:line | What it is | Class |
|---|---|---|
| `internal/policy/evaluate.go:34` | `Request.Agent string` — the request's agent identity | **real** (matched against agent-kind targets, `case AgentKind` at `:434`) |
| `internal/policy/evaluate.go:58` | `"agent:" + r.Agent` — subject rendering | **real** (PL8 verdict subject) |
| `internal/policy/inforce.go:374-375` | `case AgentKind: r.Agent = t.ID` — maps target→request | **real** |
| `internal/policy/corpus.go:53` / `:103` | `CorpusAgent.Agent` / wire `agent` — the corpus subject a rule is written for | **real** (this IS the source of agent-kind targets when seeded) |
| `internal/blueprint/validate.go:15,45,38` | `Agents []string` (the allow-list of names a blueprint may put in `agent:`), `AgentTools func(agent string)`, `SkillKnown func(callee, skill string)` | **real** (validator lookups by name) |
| `internal/blueprint/types.go:85` | `Step.Agent` (Q4) | **real** |
| `internal/store/agent_tasks.go:26,45` | `CalleeAgent` on `AgentTask`/`NewAgentTask` (the `agent_task.callee_agent` column, migration 0022) | **real** (delegation edge, name-keyed) |
| `internal/store/transcript.go:500,534,566` | transcript `Agent` reads `s.agent` (the step's agent) | **real** (by-name; reads `step.agent`) |
| `internal/store/events.go:245,251,260,271,288` | `Memory.Agent` / `AppendMemory(..., agent, ...)` — a memory note's agent field | **real** (by-name; a note names its agent) |
| `internal/store/plugin_state.go:30,44,59,129-237` | all by name — the `plugin_agent` governance (name-keyed) | **real** (this is the table AM1 joins by slug; the governance itself stays name-keyed) |
| `internal/store/loops.go:42,250,263` | `Loop.NowAgent` (`now_agent`) | **real** (the agent the loop is running) |
| `internal/store/steps.go:86,91,93,149,173,177,193-207` | `Step.Agent`, `agent` column, `orDefault(in.Agent,"unknown")` | **real** |
| `internal/handler/handler.go:65` | `PolicyAgent string` — the corpus agent whose CommandAccess rules judge the body | **real** |
| `internal/handler/registry.go:110,165` | `PolicyAgent` on the built/registry handler | **real** |
| `internal/plugin/state.go:85,122-160` | plugin state machine keyed by name (Enable/Disable/Revoke) | **real** (governance; joins by slug) |
| `internal/plugin/state_store.go:26,41,48,60,66,90` | `StoreBinding` writes/reads by name over `plugin_agent`/`plugin_refusal` | **real** (governance) |
| `internal/plugin/revoke.go:45,60-61,96-97` | `InvocationGate`/`ArrivalGate` closures keyed by `calleeAgent` | **real** (governance + chain) |
| `internal/plugin/consent.go:35,86,101,141` | `Consent.Agent`; **`:86` `p.Target.Kind==AgentKind && p.Target.ID==a.Name`** — the consent renderer matches an agent-kind target against the agent's NAME | **real** (the exact name→slug spot to migrate) |
| `internal/plugin/import.go:423,427` | `cardLoader(path, expectedAgent)` / `SetCardLoader` | **real** (the card verifier is named by agent name) |

**Wider grep (`Agent\s*string`, `.Agent`, `assignee`, etc.) — additional real ones**

- `internal/daemon/records.go:460,542,802-819,970,1128,1164` — `Assignee`/`AssigneeKind "agent"`/`ByID ident.Agent`;
  a task/loop **assignee that names an agent** (`assignee: agent:NAME`). AM2/AM4 explicitly scope these
  ("any task row with an assignee naming the agent"). **real** (AM3's assignee is `user|agent`, a kind:ID).
- `internal/daemon/blueprint.go:52,128,232,635,645,654-655` — `Agent`/`RetryAgent` on the run's step,
  `handler.IsResolver(step.Agent)` — the resolved agent name. **real** (`:232` "the RESOLVED name").
- `internal/daemon/policy_gate.go:219` `Agent: bindPolicySubject(req.Agent)` — the policy subject. **real**.
- `internal/protocol/step.go:45,365,373-375,974` — `StepParams.Agent`; `HandlerForStep(p.Agent,...)`. **real**
  (dispatch by agent name).
- `internal/protocol/memory.go:258`, `internal/protocol/steer.go:275`, `internal/protocol/policy.go:171,242,254`
  — `Agent` in protocol request/subject shapes. **real** (subject/identity).
- `internal/daemon/learnings_board.go:57-58,74,93,96` — `callerAgent`/`calleeAgent` on the learnings board.
  **real** (delegation identity).

**Incidental (local vars / unrelated types / comments) — not in AM2's scope**

- `internal/plugin/revoke.go:45` (the `Agent` field of a refusal record) is real governance, listed above;
  the `calleeAgent` *params* (60-97) are the same. No purely-incidental `Agent string` outside the
  governance/subject set surfaced in the non-test grep.
- `chat_message.source` author `= 'agent'` (a provenance word, not an identity) — see
  `internal/store/message_source_test.go:33` — **incidental** (a source kind, not a name).

**Literal agent names in test fixtures** (for re-grounding): `boss`, `main`, `golang-engineer`,
`golang-engineer-loop`, `planner`, `reviewer`, `reviewer-plus`, `reimported-consent`,
`somebody-else` — all synthetic; the only real names are the 38 in `corpus.json`.

---

## Q6 — PK / slug house convention

Mixed, but the **new content-table shape is clear**:

- **`INTEGER PRIMARY KEY AUTOINCREMENT`** is the dominant PK for content/ledger tables: `plugin_agent`
  (schema.go:242), `agent_task` (301), `agent_chain` (328), `plugin_transition` (253), `step`,
  `question`, and every file-based migration (`20261030061725_learning.sql`, `..._graph_tables.sql`,
  `..._s32_parallel_agents.sql`, `..._telemetry.sql`).
- **`TEXT PRIMARY KEY`** for identity-ish tables: `policy` (420), `loop_id`/`loop_blueprint` (349),
  `archive_id` (1062), `roadmap_item` (1691), `proposal` (1181), `component`/`project` (1667-1741).
- **Composite PKs**: `step` `PRIMARY KEY (project_root, loop_slug, phase)` (817), task `(project_root,
  loop_slug, taskid)` (843).
- **There is NO GUID convention and NO slug-as-PK convention today.** Slugs exist as *columns*
  (e.g. `loop_slug`, a plain column that later rides a composite FK), never as the PK.

**Recommendation for the new `agent` table (AM1/AM2):** `id INTEGER PRIMARY KEY AUTOINCREMENT`
(internal identity, never shown — matches the dominant house shape) **+ `slug TEXT NOT NULL UNIQUE`**
(the immutable reference key, matching `loop_slug`'s "slug as a unique named column" pattern). A
secondary unique index on `slug` gives the "collision refuses, never suffixes" (AM2) at the DB level.
This matches AM2's own "id (GUID/int, store's usual PK shape)" — **int**, and slug as a unique column.

---

## Q7 — Protocol method-table seam

`internal/protocol/` — the one-seam-many-methods shape.

- **The agent.* surface** `internal/protocol/agent.go`: the header (3-27) says "Seven methods, ONE
  forwarding handler." The **enumeration** `agentServiceMethods` (`:51-111`) is the single list;
  `AgentServiceMethod` (`:39-48`) = `{Name MethodName, ReadOnly bool, Cannot string, Emits string}`.
  `agentServicesCall(name MethodName)` (`:125-162`) is the **one handler** all the agent.* registrations
  share: it binds the method name at registration time and forwards to **`Table.Agents`** (a func seam).
  `agentPluginsCall` (`:169-183`) forwards to `Table.Plugins`.
- **Registered agent.* names** (from the enumeration): `agent.discover`, `agent.card`, `agent.invoke`,
  `agent.task.list`, `agent.task.show`, `agent.task.complete`, `agent.task.fail`, plus read-only
  `agent.plugins` and `agent.list`. **`agent.enable/disable/revoke` (S35 T4) and `tools.grants` are
  deliberately NOT in this enumeration** (`:91-110`) — they are synchronous plain writes / non-agent
  prefixed, documented beside their method consts in `method.go`; `TestMethodTableInOnePlace` excludes
  them by name.
- **The `Table` type** `internal/protocol/method.go:471` holds the **func seams** the daemon wires:
  `Agents` (`:723`), `Plugins` (`:731`), `AgentRoster` (`:739`), `ToolGrants` (`:745`), `MCP` (`:752`),
  `AgentTransition` (`:759`), `AgentImportPreview` (`:783`), etc.
- **Registration pattern**: `t.register(&Method{Name: <MethodName>, RequiresInitialize: true, Handle:
  <handler>})` — e.g. `learnings.go:161-164`, `graph.go:138`, `method.go:916-942`. `Method` has
  `Name`, `RequiresInitialize`, `Handle func(ctx, *Session, json.RawMessage)` (`method.go:463`).
- **Daemon wiring of the seams**: `internal/daemon/daemon.go:364-365` `table.Agents = opts.Agents`,
  `table.Plugins = opts.Plugins`; the `Options` struct is at `daemon.go:114`. Per-seam wiring pattern:
  `internal/daemon/queue_handlers.go:32` `d.Table.QueueAdvance = func(...) {...}`.

**What T7 follows (list/create/duplicate/edit/delete/view-content):**
1. Add `MethodName` consts (e.g. `MethodAgentCreate`, `MethodAgentDuplicate`, `MethodAgentEdit`,
   `MethodAgentDelete`, `MethodAgentList`/roster, `MethodAgentViewContent`) in `method.go` with the
   `Cannot`-shaped doc beside each.
2. Add a **func seam** on `Table` (e.g. `AgentManage func(ctx, method string, params) (any, refusal string, err error)`,
   the `agentServicesCall`/`Agents` shape) — one seam fanning the new methods, mirroring `Agents`.
3. Register each new method via `t.register(&Method{...})` in the table-init block (next to `:916-942`),
   and add an `AgentServiceMethod` entry (or a sibling enumeration) so `TestMethodTableInOnePlace` stays green.
4. Wire the seam from `Options` in `daemon.go` (like `:364-365`), calling the same cmd/aeon service
   functions the CLI uses (P34 — no second implementation).
5. The **P34 grep** the close-out must run: `grep -n 'rpc\(Typed\)\?('` across the new Settings page(s),
   confirming every method-name literal is a registered method (the UI calls through the generic
   `rpc(method, params)` bridge — AGENTS.md §4b; no per-method Rust command).

---

## Q8 — Config surface for `agents.allowUnsignedImport`

- **`internal/config/layers.go:49`** `type Config struct { values map[string]string; layers map[string]File; Files []File }`
  — a flat, 5-layer (defaults/user/project/env/flags) key→string config (`Load` `:62-77`, `Value` `:109`).
- **`internal/daemon/daemon.go:114`** `type Options struct` — holds the daemon's func seams and config.
- **`queue_advance` / `sizing_mode` are NOT in Config/Options — they are per-component columns** on the
  `component` table (migration **0041**; `schema.go:2218` `ALTER TABLE component ADD COLUMN queue_advance
  TEXT NOT NULL DEFAULT 'ask'`; `sizing_mode` in the same 0041 block), **read through a func seam**:
  `internal/daemon/queue_handlers.go:32` `d.Table.QueueAdvance = func(ctx, componentRoot) ...`.
- **Recommendation:** `allowUnsignedImport` should be **daemon-level (app-wide), NOT per-component.**
  Import is a global capability with no per-repo/per-component scoping (unlike the per-repo queue), so
  it should be a single app-wide setting. Concretely: a **Config map key** `agents.allowUnsignedImport`
  (default `"false"`, read by the daemon) **or a daemon `Options` field** — **not** a `component`-table
  column like `queue_advance`. This matches the design's own framing ("a daemon-level config value if
  this is meant to apply app-wide rather than per component"). **This is the mechanical open item
  `design.md` leaves to T0/T5** — recorded as a documented call (not an operator question).

---

## Q9 — UI surface (Settings = the real app, not a bench tab)

- **Settings route** `ui/src/app/routes.ts:46` `{ id: "settings", path: "/settings", title: "Settings",
  access: "open" }`. The route id union includes `"settings"` (`:11`).
- **Settings page** `ui/src/features/settings/SettingsScreen.tsx` (and `ui/src/lib/appSettings.ts`).
  **This is the surface for S43** — the operator's "real app", not a bench tab.
- **Bench registry** `ui/src/features/test-bench/lib/benchRegistry.ts`: `BenchPageDef {id,label,path,
  order, group?, needsConnection, render}`; `registerBenchPage`, `benchPages()` (sorted by `order`),
  `getBenchPage`. The bench **`agents`** page is a **LEGACY bench page** `routes.ts:83`
  `{ id: "agents", label: "Agents", path: "/bench/agents", group: "trust" }` — **NOT** S43's surface.
  A *new* bench page registers via `registerBenchPage` + one line in `pages/registerAll.ts` (AGENTS.md §5).
  **S43 does not use the bench registry at all** (AM5; brief §2).
- **`ui/src/components/minion-avatar/`**: `MinionAvatar.tsx`, `characters.ts`, `catCharacters.ts`,
  `CatFeatures.tsx`, `useMinionAnimation.ts`, `index.ts`.
  - **`MinionAvatar.tsx` props** (`MinionAvatar.tsx:13-45`): `name`, `type` (`AvatarType`), `catCoat`
    (`CatCoat`), `colorVariant`, `color` (`MinionColor`), `mood` (`MinionMood`), `view`
    (`MinionView`), `smile` (`MinionSmile`), `size`, `paused`, `speed`, ... plus colour variants
    (`overallsColor`, `gloveColor`, ...).
  - **`characters.ts`** `MinionMood = 'idle' | 'hi' | 'thinking' | 'working' | 'stuck' | 'done'` (1);
    `MINION_COLORS` = yellow/green/purple/orange/blue/pink (27-29); `MinionCharacter` =
    `{eyes, hair, body}`; `characterForName(name)` hashes unknown names to stable features.
  - **`catCharacters.ts`**: `CAT_COAT_COLORS`, `CatCoat`, `AvatarType` (`'minion' | 'cat'`),
    `catCoatForName`.
  - **Field mapping for T7:** `avatar_type`→`type`, `avatar_color`→`color` (MinionColor) or `catCoat`,
    `avatar_mood`→`mood` (MinionMood). The component is already built; S43 only wires an agent row to
    this subset (brief §2).

---

## Q10 — The signed-card envelope (S14), what AM3 expands

`internal/agentsvc/card.go` (the real implementation; `plugin.CardInfo` is a projection of it).

- **`SignedCard`** `card.go:90-94`: `Card json.RawMessage` (the exact bytes), `PublicKey string`
  (base64 std, 32-byte ed25519 public), `Signature string` (base64 std, over `Card`'s exact bytes).
  The doc (84-89) names it an **addition** to A2A (A2A signs over HTTP with JWS; locally the card is a
  file, the signature is detached ed25519 over the exact stored bytes, so whitespace tampering fails).
- **`Sign`** `card.go:98-121`: canonicalises the card to **compact JSON** (so the signed bytes == the
  stored bytes), signs with ed25519.
- **`LoadCardFile`** `card.go:126-136`: read → unmarshal `SignedCard` → `Verify`.
- **`Verify`** `card.go:142-171`, order is deliberate: **signature FIRST**
  (`ed25519.Verify(pub, sc.Card, sig)` `:157`), then the **claim** (`card.Name == expectedAgent` `:164`),
  then **shape** (`checkCardShape` `:167`). **Unsigned is refused, not warned** (`:146-148`:
  "unsigned is not a valid state").
- **`AgentCard`** `card.go:66-82` (A2A 0.x shape): `Name`, `Description`, `Version`, `Provider`,
  `Capabilities`, `Skills`, `SecuritySchemes`/`Security` (opaque `json.RawMessage`).
- **`CardStore`** `card.go:198-218`: file convention `<agent>.json` in a dir; absence = `(nil,nil)`
  (the normal state for the 25 first-party agents, AS1).
- **What AM3 expands:** today the envelope is ed25519 over a **bare public key**. AM3 changes it to
  **ed25519 signature over an X.509 certificate** (stdlib `crypto/ed25519` + `crypto/x509`), verified for
  **tamper-evidence only** (no trust-root / signer-identity judgement — out of scope), plus the new
  `CardInfo` fields (functionality/metadata/avatar/version). **`allowUnsignedImport` (Q8) flips the
  "unsigned is refused" default at `card.go:146-148`.**

---

## Q11 — The existing `aeon agent` CLI verb (T8's foundation)

- **`cmd/aeon/agent.go`**: `cmdAgent(w, args)` (`:51`) dispatches the verbs
  **`import <path> · list [--all] [--provenance] · enable · disable · revoke --reason`** (the help line
  in the error at `:52`). The doc (`:1-30`) states the two enforced refusals: **nothing fetches**
  (import takes a path), **import widens authority and only import** (R6 — import lives in the CLI/app,
  never a socket method; enable/disable/revoke moved to `agent.enable/disable/revoke` socket methods in
  S35 T4).
- **The CLI opens the store directly** (the justified two-access path, `agent.go` doc): a row written by
  the CLI verb IS the daemon's next decision; no daemon notification needed (N8 forbids caching).
- **The CLI→daemon client pattern (what T8 will use):** `protocol.Client` + `c.Call(method, params,
  &result)` — e.g. `answer.go:41`, `ask.go:85`, `brief.go:87`, `client_verbs.go:327`. Dialing is
  `dial(want int) (*protocol.Client, error)` (`cmd/aeon/dial.go:36`, using `endpoint.Path()` +
  `appenv.Apply()`). Related agent files: `agent_services.go`, `agent_admission.go`,
  `agent_transition_services.go`.
- **For T8 (`aeon agent export <slug>` / `import <file>`):** call the **same daemon methods T7
  registered** through `c.Call(...)` (P34 — the CLI is a secondary client of the daemon surface, never a
  second implementation of import/signature). Default sequencing: ship T7 (daemon) before the CLI
  (plan.md: "the daemon surface is primary").

---

## Q12 — Migration count / count-guard and the frozen block

- **The count-guard is `frozenMigrationCount()`** `internal/store/helpers_test.go:262-291`:
  `frozenLegacyCount() + frozenFileCount()`.
  - **`frozenLegacyCount()`** (`:283-291`): counts the numbered literals in `schema.go` with
    `ID <= 1_000_000_000_000` → **45** (the block runs **1–45**; `{45}` at `schema.go:125`).
  - **`frozenFileCount()`** (`:269-275`): the timestamped files under `migrations/` → **4**.
  - **Total = 49.**
- **The frozen legacy block** `internal/store/schema.go` (the `legacyMigrations()`/`Migrations()`
  numbered list) is **1–45**. **Drift to record:** `migrations/README.md:3` and several test messages
  say "frozen at **1–43**", but the numbered block is **1–45** — `0044` (S29 bug, `schema.go:124`) and
  `0045` (S37 agent_import widening, `schema.go:125`) joined the numbered block on main;
  `helpers_test.go:278-282` documents exactly this ("main grew the block from 43 to 45 ... a hardcoded
  43 here would have made every additive guard wrong").
- **The 4 file-based migrations** (`internal/store/migrations/`), newest last:
  `20261030061725_learning.sql`, `20261030061806_graph_tables.sql`, `20261030071034_s32_parallel_agents.sql`,
  **`20261030084236_telemetry.sql` (newest)**.
- **Where the guards assert it:** `internal/store/policy_test.go:312` (`before != frozenMigrationCount()`),
  `:463`, `:1323`; `internal/store/migrations_dir_test.go:81` (`len(Migrations()) >= 43`).
  **Drift to record:** the `Fatalf` **message** at `policy_test.go:313` still says "want **45**", but the
  **assertion** compares to `frozenMigrationCount()` (49); the comment at `:307-311` explains a total
  beyond 45 is expected. The message text is stale.
- **For T1's new migration:** a file named `<14-digit-YYYYMMDDHHMMSS>_<slug>.sql`, timestamp **> `20261030084236`**
  (the newest file), e.g. `20261030120000_agent.sql`. The leading `-- ` line becomes the `Description`
  (`migrations/README.md`); `Migrations()` picks it up automatically after the frozen block; a name not
  starting with 14 digits fails the build. `frozenMigrationCount()` will then read **50** — the
  "this task adds no migration" guards compare against it, so T1 must **not** rely on a hard-coded number.

---

## Notes for T0's own record (brief.md §5, resolved by documented call)

- **#1 exact table shape** — documented call: `agent` table, `id INTEGER PK AUTOINCREMENT` + `slug TEXT
  UNIQUE` + `name/description/functionality/metadata(json)/version/internal(bool)/avatar_type/
  avatar_color/avatar_mood/signature/signer_certificate/created_at/updated_at` (plan.md T1), joined to
  `plugin_agent` by slug. PK shape matches house convention (Q6).
- **#2 policy rows also block deletion** — the design already locked this (AM4: "both block deletion");
  it is a real query (`PolicyListAll`/`PolicyAcceptanceListFor`), not a text scan (Q3).
- **#3 slug-collision** — documented call: **refuse and name the conflict, never suffix** (AM2); enforce
  at the DB via the unique `slug` index (Q6).
- **#4 signature/certificate format** — operator's call, but the design already locked it: **Ed25519
  over an X.509 certificate, stdlib only** (AM3); Q10 shows the current bare-public-key envelope to
  expand. If the operator wants to re-litigate, that is the one to ask.
- **#5 blueprint YAML raw slug vs name-resolved** — documented call: **raw slug, typed directly** (AM2);
  blueprint persistence is file/structured YAML (Q4), so the delete-guard parses the lane's `Agent`
  field. Affects #2's blast radius (blueprint refs are text, not FK).
- **Mechanical items left by design.md:** table shape (#1 — done), daemon-method seam location (Q7 —
  extend the `agentServicesCall`/`Table.Agents` seam, or a sibling `AgentManage`), and config scoping
  (Q8 — daemon-level, not per-component).
