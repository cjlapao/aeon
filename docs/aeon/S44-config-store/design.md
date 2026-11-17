# Design — S44 Config store

Written 2026-10-30. Rulings **CS1–CS14** below were checked against the real code in the same session
(section "Verified facts" lists what was read, with file and line). They are **documented calls, not operator
rulings**: `brief.md` §4 allowed "ask or decide and record, the way S43 did", and these were decided. Four of
them are real product calls the operator may want to overrule (CS3, CS4, CS6, CS12); each says so and says
what changes if it is overruled. Do not re-open a ruling without recording the reason in that task's Notes.

## Verified facts (2026-10-30, `aeon/` at `57ea4f70`)

These replace the brief's `VERIFY` markers. T0 re-checks them for drift before T1 commits to a schema.

1. **`internal/config` is five files and has no production caller of its merge.** `layers.go`
   (`Layer`, `File`, `Config`, `Load`, `Value`), `validate.go` (`Validate`, `ValidateNumeric`, `ErrInvalid`),
   `creds.go` (`Credential`, `Source`, `isCredentialName`, `ErrCredentialInConfig`), plus two test files.
   `grep -rn 'config\.Load(' aeon` finds **no caller outside the package**. The only importer of
   `internal/config` in the module is `internal/presets/presets.go` (import at line 19, and
   `config.Credential("AEON_KEY", a.KeyFile)` at line 208). So the brief's "read once at `aeon serve`
   startup" describes the **design**, not current wiring: the five-layer merge is a tested library with no
   production consumer yet. This does **not** change CS1 (the five layers stay untouched), but it removes the
   brief's worry about breaking a `Load` caller, and it means nobody can tell a "boot-time" value from a live
   one by looking at today's callers.
2. **`internal/config` imports only `internal/errs`.** It does not import `internal/store`, and nothing in
   `internal/store` imports it. A `config → store` import would be new, and I2 forbids it in spirit anyway (see CS2).
3. **No generic settings table exists.** The full `CREATE TABLE` list across `schema.go` and
   `migrations/*.sql` contains no `setting`, `config`, or key/value table. The only precedent is the two
   `component` columns `queue_advance` / `sizing_mode` (legacy migration 0041).
4. **`queue.settings` is the closest existing precedent for a socket-written setting**
   (`internal/protocol/queue.go:300-345`, `handleQueueSettings`): one method both reads and writes; a write
   carries both knobs or neither (half a pair is a shape error, decided before the store is touched); an unknown
   subject is a named refusal, not an empty success; the read answers the declared defaults with `known=false`
   (P31's third state) rather than empties.
5. **Migrations are files.** `internal/store/migrations/README.md`: `<14-digit>_<slug>.sql`, first `-- ` line
   is the `Description`, applied verbatim in one transaction, forward-only. Latest file today:
   `20261030084236_telemetry.sql`. The legacy literal in `schema.go` is frozen at 1–43. Tests mostly compare
   `len(Migrations())` dynamically (`automation_schema_test.go:103`); fixed prefixes like `Migrations()[:38]`
   are literals on purpose (do not touch them).
6. **SQL lives only in `internal/store`** (`TestNoSQLLivesOutsideTheStore`, `internal/store/store_test.go:18`,
   invariant I2). The domain rule lives one layer up behind a `Repo` seam that holds no SQL —
   `internal/policy/store.go` over `internal/store/policy.go` is the pattern (its header comment states it).
7. **The protocol method table** (`internal/protocol/method.go`): methods are `register`ed in `NewTable`
   (`t.register(&Method{Name: …, RequiresInitialize: true, Handle: …})`, a duplicate panics);
   `PublicMethods()` is everything not `Debug`. The newest block is S33's four `telemetry.*` reads
   (`method.go:1085-1090`). A read/write family with a **local interface seam** is the established shape:
   `internal/protocol/telemetry.go` declares `TelemetryStore`; `Table.TelemetryReads` is the field;
   `internal/daemon/telemetry_reads.go` is the adapter; `internal/daemon/daemon.go:343-346` wires it.
8. **The narrow (agent/HTTP/MCP-stdio) surface is an enumerated allow-list**, not "everything public":
   `internal/narrow/actions.go` holds `actions`, each entry naming one `SocketMethod`, with a mandatory
   non-empty `Cannot`. A socket method is therefore **absent from the narrow surface by default**.
   Two guard tests pin human-only doors by *asserting absence from the method table / narrow table*:
   `cmd/aeon/policy_test.go:483 TestNoPolicyAuthoringReachesTheNarrowSurface` and
   `cmd/aeon/agent_test.go:320 TestNoPluginManagementOnNarrowSurface`.
9. **Wire contract chain.** A new method needs: a `MethodX` const + `register`; a case in
   `internal/protocol/conformance_test.go` (`paramsTypeFor` ~line 570 / `resultTypeFor`); success fixtures under
   `internal/protocol/conformance/routes/*.json` and error fixtures under `…/errors/*.json`
   (`TestEveryMethodHasASuccessFixture`); the handshake want-list (`internal/protocol/handshake_test.go:375`
   region lists methods by block); and `make protogen` (`Makefile:273`) regenerating `ui/src/proto/contract.ts`
   (`make check-protogen` fails CI if stale).
10. **Error codes.** Typed protocol errors live in `internal/protocol/errors.go`. Highest observed codes:
    `-31800 ErrHistoryRefused`, `-31801 ErrNoTelemetry` (S33). T0 re-measures the real maximum.
11. **Session identity.** `protocol.Session` itself carries only `ConnID, Version, ClientName, ClientVersion`
    (`internal/protocol/session.go:23`), **but the kernel peer identity is already available** through the
    `table.ActorFor` seam (`internal/daemon/daemon.go:495`, found while planning S45): handlers receive a
    `protocol.Actor` stamped from the peer credential, never from params — the S35 proposal and S38 records handlers
    use it. T0 confirms its exact shape; the audit `actor` should be built from it (`"<kind>:<id>"`), not from
    `ClientName`.
12. **The UI's Settings screen today is device-local only.** `ui/src/features/settings/SettingsScreen.tsx`
    (64 lines: Appearance, Language, Default tones) reads/writes `localStorage` through
    `ui/src/lib/appSettings.ts`. Nothing on it touches the daemon. Route: `routes.ts:46` (`id: "settings"`,
    `access: "open"`). Daemon data hooks exist: `ui/src/lib/useRpcQuery.ts`, `useRpcAction.ts`; strings are in
    `ui/src/lib/i18n/en.json` + `pt.json`.
13. **Existing credential-name rule** is `isCredentialName` (unexported, `creds.go`) — usable from inside
    package `config` without widening its surface.

## CS1 — A separate accessor beside the five layers, in the same package; the layers are not touched

`internal/config` gains a **new set of files** (`settings*.go`) and **no edit** to `layers.go`, `validate.go`,
`creds.go` or their tests. The five-layer `Layer` enum is not extended: a database-backed, live, writable value
is a different shape from a boot-merged one, and bolting a sixth rung onto an enum whose whole contract is
"merged once, immutable" would be a lie in the type. Fact 1 shows there is no production `Load` caller to break,
but the rule stands on its own: the five layers keep working exactly as their tests say. A guard test
(T3) asserts `layers.go`, `validate.go` and `creds.go` are byte-identical to the pre-slice commit (hash pinned in
T0) — the "adds, does not edit" acceptance criterion, made mechanical rather than reviewed.

## CS2 — SQL stays in `internal/store`; `config` talks to a `Repo` seam

`config.Store` holds **no SQL and imports no store package**. It depends on a small `Repo` interface
(T3) that `internal/store/settings.go` (T1) implements the SQL half of, and a daemon-side adapter bridges the two
(`internal/daemon/settings_repo.go`, T4) — exactly the policy split (fact 6). Reason: I2 is enforced by a test,
and the layering means `config` stays unit-testable with an in-memory fake `Repo` and no SQLite.

## CS3 — Key/value table, **closed key registry**, typing at the boundary (answers brief Q3)

*Product call — overrule means a schema change per setting.* The table is schema-less key/value text (fast to
extend: a new setting is a Go declaration, never a migration). The brief named the cost: no per-key type safety.
This slice recovers it **in the write boundary, not the schema**: every key must be **declared** in a Go
registry before it can be written, with a type, default, bounds and description. The write boundary refuses an
undeclared key (naming the nearest declared keys), a value that fails the key's type/bounds, and — see CS5 — a
credential-shaped name. This is PL3's closed-vocabulary discipline applied to keys: "a value nobody compiled is
a permission nobody granted", one level up. The brief's escape hatch ("a specific setting whose risk argues for a
real column") is **not taken for `agents.allowUnsignedImport`**: the risk it carries is handled by the `Sensitive`
flag (CS8) and the human-only door (CS9), not by a column.

Types (closed): `bool`, `int` (with optional `Min`/`Max`), `string` (with `MaxLen`, default cap 4096), `enum`
(closed member list). **Canonical text form** (what the table stores and what the wire carries): bool is exactly
`"true"` or `"false"`; int is base-10 with no leading `+` or zeros; string is the raw text; enum is one member
verbatim. Anything else is refused, not normalised — one spelling per value, so a comparison is a string compare.

Declarations are **conflict-free across parallel worktrees**: one file per area, `settings_decl_<area>.go`, whose
`init()` calls `mustDeclare(Setting{…})`. Two slices adding settings add two files, not two edits to one list.
`mustDeclare` panics on a duplicate key, a credential-shaped name, an invalid key shape, a default that fails its
own type, or two keys whose env names collide (CS4) — a programmer error caught by `go test` before any daemon
starts, the same stance as `Table.register` panicking on a duplicate method. **T0–T9 ship zero production
declarations** (brief §3: this slice builds the mechanism); the empty registry is a tested, honest state (CS13).
The first production declaration is T10's adoption of S43's setting (CS15).

## CS4 — Precedence: environment beats database beats default; no flags (answers brief Q2)

*Product call — overrule means the UI may show a value that is not in force.* Effective value =
**environment override → database value → declared default**. Flags are **not** supported for registry keys: a
flag is a per-invocation CLI concept and the daemon-global values here are not per-invocation.

The environment variable name is derived, never configured: `AEON_SETTING_` + the key upper-cased with every `.`
replaced by `_` (`agents.allowUnsignedImport` → `AEON_SETTING_AGENTS_ALLOWUNSIGNEDIMPORT`). `mustDeclare` refuses
two keys that map to the same name.

**A write while the environment shadows the key still succeeds and is stored** — but the result says so:
`source: "environment"`, `shadowedBy: "AEON_SETTING_…"`, and the audit row records `shadowed_by='env'`. Silently
writing a value that has no effect would be the dishonest outcome; refusing the write would strand the person who
removes the env var next week and expects their saved choice. The UI shows an env-controlled setting as locked
with the variable's name (T8). Reads always return **all three** facts — `value` (effective), `source`, and
`stored` (what the table holds, or `null`) — so no client has to guess which layer won (D3's "name the layer").
An env value that fails the key's validation is **not used**: the read reports `source` as the next layer down
plus `envInvalid: "<var name>"`, never a silent fallback (the same "name it" rule the five-layer `Validate` states).

## CS5 — A credential never goes in this table (answers brief acceptance 2)

The rule is C4 and the single reader is `config.Credential`. Mechanically: `mustDeclare` refuses any key for
which `isCredentialName` is true (so no credential-shaped key can ever be written — it is simply not a
declarable key), and a `string` key's `MaxLen` plus its description are the only free-text surface. A test
declares a deliberately credential-named key and asserts the panic names the key and C4. The write boundary
re-checks the name defensively (cheap, and the registry is a package global a test could mutate). This slice
adds **no** credential type and **no** redaction machinery: there is nothing to redact because nothing secret is
admitted. If a later slice needs a secret in the UI, it routes through `creds.go`'s existing rule, not here.

## CS6 — Global scope only; no scope column (answers brief Q5)

*Product call — overrule means a table rebuild later (a known, drilled pattern: 0016, 0019).* S43 needs one
global value; the brief's later tiers (policy, MCP, automation defaults) have not been designed and S38's layer
resolution (workspace → project → component) is its own subject. A scope column nothing can fill is the exact
mistake S33 TL5 avoided ("a column nothing can ever fill"). So: PK is `key` alone. When a consumer genuinely
needs a per-workspace value it declares `Scope` on its `Setting` and the slice that adds it owns the migration
(rebuild `setting` with `scope_kind`/`scope_id` and the S38 resolution order) — recorded here so that slice
knows the shape was left open on purpose. `Setting` carries a `Scope` field typed with a single constant
`ScopeGlobal` now, so the declaration site is already scope-shaped.

## CS7 — Live reads: no cache anywhere in the path (answers brief acceptance 1)

`config.Store` holds **no value cache**. Every `Get` is one indexed row read (plus a `os.LookupEnv`). SQLite on
the local store makes this cheap, and the cost is the price of "a Settings-UI write is visible on the very next
read, same process or not". The slice's defining test (T3) opens the same database file through **two
independent `Repo` handles** (and one as a fresh `*sql.DB`), writes through one and reads through the other with no
sleep, no reload and no signal. The other half of the guarantee — that a *consumer* does not cache — is
the consumer's rule, stated in `config.Reader`'s doc and in T4's "Do NOT": a consumer reads at the point of use
(for S43's unsigned-import check: at each import call), never at construction.

## CS8 — One write boundary, one audit table, a `Sensitive` flag

Every mutation goes through `Store.Set` / `Store.Unset`, which in **one transaction** (via the `Repo` seam's
single write call) upserts/deletes the `setting` row and appends a `setting_event` row. A write that changes
nothing (`value` equal to what is stored) returns `changed:false` and writes **no** event (an audit trail of
no-ops is noise). `Unset` of a key with no row is `changed:false`, not an error. The audit table is
**append-only, enforced by triggers** (the `plugin_agent` state-machine trigger is the precedent for enforcing
in the database what a test alone could not hold): `BEFORE UPDATE` and `BEFORE DELETE` on `setting_event` raise.
It records `key, op, old_value, new_value, sensitive, shadowed_by, actor, occurred_at`. Values are stored in
clear in the audit row — safe only because CS5 admits no credential.

`Setting.Sensitive bool` marks a key whose value **widens a guard** (`agents.allowUnsignedImport` is the
motivating case: it lets an unsigned card in). Effects: the audit row carries `sensitive=1`; `settings.list`
marks it; the UI requires an explicit confirmation before a write (T8). It does **not** change the transport and
does **not** stage the write as a proposal (S35's staging exists for writes that widen a *live* guard a running
loop depends on; flipping a setting nothing is mid-flight on is the S43 AM3 argument — "importing a card is not
widening a live guard"). A `Sensitive` declaration's `Default` **must be the restrictive state**; a declaration
test cannot prove that, so `Setting.Description` for a sensitive key must state what the default protects, and
`mustDeclare` refuses a sensitive key with an empty description.

## CS9 — The socket is the human door; settings are absent from the narrow surface and from agents

The write methods are `RequiresInitialize: true`, **not** `Debug` (`Debug` means "exists for diagnosis, not for
the product"; these are product methods, and S31 took the same stance for `step.steer*` and `terminal.*`). The
guard is fact 8: the narrow surface is an enumerated allow-list that simply never names them, and a test pins it. The slice adds a
guard test in `cmd/aeon` in the `TestNoPolicyAuthoringReachesTheNarrowSurface` mould
(`TestNoSettingsWriteReachesTheNarrowSurface`, T6): it asserts no entry in `narrow.Actions()` projects any
`settings.*` method, that `internal/tools`'s registry names no settings tool, and that the string
`settings.set` / `settings.unset` is absent from `internal/narrow` and `internal/agentsvc`. Reason, stated once:
a write path on a surface an agent can reach hands the agent the means to change the rules written to constrain
it (PL15's argument, R6's argument). Reads (`settings.list/get/history`) are also kept off the narrow surface in
this slice — nothing asked for them there, and N8 forbids adding surface without a reason; a later slice can add a
narrow read with its own `Cannot`.

## CS10 — Socket methods (P34), wire value is canonical text

Five methods, all `RequiresInitialize`, none `Debug`:

| Method | Params | Result |
| --- | --- | --- |
| `settings.list` | `{ area?: string }` | `{ settings: SettingView[] }` (declared keys only, sorted by area then key) |
| `settings.get` | `{ key }` | `{ setting: SettingView }` |
| `settings.set` | `{ key, value: string }` | `{ setting: SettingView, changed: boolean }` |
| `settings.unset` | `{ key }` | `{ setting: SettingView, changed: boolean }` |
| `settings.history` | `{ key?: string, limit?: int }` | `{ events: SettingEventView[] }` newest first, `limit` default 50, max 500 |

`SettingView` = `{ key, area, type, description, default, enumValues?, min?, max?, maxLen?, sensitive,
value, source: "default"|"database"|"environment", stored: string|null, shadowedBy: string|null,
envInvalid: string|null, storedInvalid: boolean, updatedAt: string|null, updatedBy: string|null }`.
The descriptor fields travel **on the wire** so the UI renders any declared key without knowing it in advance
(CS13). `value` is a **string** in canonical form on the wire (not `any`): the generated `contract.ts` stays
simple and the type tag tells the client how to render it — T0 confirms `protogen` would have handled `any` or
not, but the string choice is made regardless.
An undeclared key is a typed refusal that **names the declared keys nearest to it** (`ErrSettingUnknown`); an
invalid value is a typed refusal that names the key, the expected form and what was given
(`ErrSettingInvalidValue`) — the three-part shape (what / why / do) every S23 error carries. Reads of a key that
is declared but unset are **answers**, not refusals (`source:"default"`) — absence is normal (DB19).
`storedInvalid:true` covers the one failure a *later* registry tightening can create: the table holds a value
that no longer validates. The read returns the **declared default** as the effective value (the restrictive
state, per CS8) with `storedInvalid:true` and `stored` carrying the raw text, so it is loud in the UI and in
`doctor`-style tooling but never fatal to a consumer.

## CS11 — Consumers hold a `Reader`, not the Store

`config.Reader` is the read-only interface a consumer takes: `Bool(key) (bool, error)`, `Int(key) (int, error)`,
`String(key) (string, error)`, `Enum(key) (string, error)`. Typed helpers refuse a key declared with a
different type (a `Bool("agents.someInt")` is an error, not a coerced guess). One `*config.Store` instance is
built at serve time and handed to **both** doors — the daemon's protocol seam and any in-process consumer
(S43's import code, once T10 has moved it) — "one implementation, two transports", the S22 `search.query` precedent. Nothing outside
`internal/config` and the daemon adapter touches the `setting` table; a grep guard (T6) asserts it.

## CS12 — Change notification: emit if cheap, otherwise name the staleness

*Product call — overrule means multi-window staleness is accepted.* If the event-kind registry
(`runtime.EventKinds()`, named at `internal/protocol/events.go:46`) admits a non-loop kind cheaply, a successful
changing write emits `setting.changed` `{key, source}` so a second open window refetches. T0 measures whether
that is cheap (one kind, additive, fixture) or drags S2's closed event vocabulary into review. If it is not
cheap, T5 ships without it, the UI refetches after its own writes and on reconnect (`live.generation`, as
`useRpcQuery` already does), and the staleness window — another window's write is not seen until it next
refetches — is named in the DEMO findings, not hidden.

## CS13 — The UI is a generic, descriptor-driven section on the real Settings screen

The surface is `SettingsScreen.tsx` (the real app; the bench-page registry does not apply, as in S43). A new
"Daemon settings" panel renders from `settings.list`: grouped by `area`, one control per `type` (switch for
`bool`, select for `enum`, number input with `min`/`max` for `int`, text input with `maxLen` for `string`),
each row showing the effective value, a source badge (default / saved / environment), the default, who last
changed it and when, a "reset to default" action (`settings.unset`), and a lock with the variable's name when
the environment shadows it. A `sensitive` key requires an explicit confirmation naming the key and what it
allows before `settings.set` is called. **The empty registry is an honest state** ("No daemon settings are
declared yet"), not a blank panel. The existing local appearance panels are untouched.

## CS14 — What this slice does not move

No existing `internal/config` layer value, no `component` column (`queue_advance`/`sizing_mode`), no
credential, no `plugin_agent` anything. No setting is declared for S43 or any later tier by T0–T9; S43's one setting is adopted only by T10, after S43 merges (CS15). No scope column. No
flags. No staging. No narrow-surface read. Anything above that a later slice wants is that slice's own design.

## Open items left to T0 — mechanical, not product calls

1. Re-confirm facts 1–13 for drift; pin the SHA-256 of `layers.go`, `validate.go`, `creds.go` for CS1's guard.
2. Whether `.sql` migration parsing (`migrations_dir.go`) supports a `CREATE TRIGGER … BEGIN … END;` body with
   inner semicolons (CS8 needs two). If not: the fallback is Go-side enforcement plus a test, recorded in T1.
3. The real next free error codes (CS10 reserves **-31810 and -31811** for S44).
4. What `actor` can honestly be on a write (`Session.ClientName` only, or peer uid from `Connections()` — fact 11).
5. Whether `setting.changed` is cheap (CS12).
6. Whether `protogen` handles the shapes in CS10 (nullable strings, `string[]` enum values) without a new case.
7. Which `internal/daemon` file the `*config.Store` is constructed in, and where `cmd/aeon serve` holds the one
   instance (CS11).

## CS15 — S43 is not edited by this slice's planning; adopting its setting is S44's own task (T10)

*Operator ruling, 2026-10-30:* S43 is being built and cannot be changed from here, so **any change or refactor of
S43's code is S44 work, done as an S44 task.** Consequences, all recorded so a builder does not re-derive them:

1. T0–T9 touch **no S43 file** (code or docs) and ask nothing of S43. The earlier plan's "S43 declares the key in
   `settings_decl_agents.go`" and "S43 T5 should wait for S44 T5" are withdrawn; the two pointer lines briefly
   added to S43's `tasks/T5.md` and `design.md` were reverted the same day.
2. S43 chooses its own mechanism for `agents.allowUnsignedImport`. S44 does not predict it.
3. **T10** runs after S43 has merged: it investigates what S43 actually built, declares the key in
   `internal/config/settings_decl_agents.go` (an S44-owned file), and refactors S43's read site and any bespoke
   write path onto `config.Reader` / `settings.*`. It preserves S43's observable behaviour (default refuses an
   unsigned card; the override imports it marked `unsigned`) and keeps S43's tests green — adapting an assertion
   only where it pinned the old mechanism, never deleting one without an equivalent.
4. The migration of any value S43 already stores is a forward-only migration file owned by S44 (T10), carrying the
   stored value across with an audit event whose actor says it was an adoption, never a silent reset to the default
   (a reset to `false` is safe for this key but still a lost operator choice).
5. If S43's mechanism cannot be moved without changing its behaviour, T10 stops and reports; it does not change S43's
   behaviour to fit.
