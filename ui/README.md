# `aeon/ui` — the desktop app and its test bench

The default window opens the S9 product shell: Loops, Blueprint library and System. The earlier slice test bench remains at **Test bench** (`#/bench`) so the replay QA walks still have their controls. S9 is in progress; [PORT.md](PORT.md) records the v1 port decisions.

**Vite 7 · React 19 · `@cjlapao/ui-kit@0.2.3` · Tailwind 4 · TypeScript 5.6 · Tauri 2**

The test bench was added slice by slice from S1 through S8. It exercises each daemon feature through the same Tauri bridge and is retained for QA during S9.

For a disposable replay walk of the new screens and the retained bench, follow [S9 UI QA demo](../docs/aeon/S9-app/UI-QA-DEMO.md).

## Running it

From `aeon/`:

```sh
make dev            # daemon AND app; Ctrl-C stops both
make dev-daemon     # the daemon alone, if the app will not build
```

Or from here, where this folder has its own Makefile:

```sh
make help           # what follows, and the endpoint it will use
make install        # npm ci, but only when the lockfile moved
make dev            # vite alone, in a browser, on :5173
make test           # 192 tests during S9
make test-watch
make typecheck
make check-deps     # are Tauri's system libraries present?
make tauri-dev      # the window, against ../.dev/aeon.sock
make tauri-build    # bundle for this platform
```

**`make install` is stamped**, so it runs `npm ci` when `package.json` or `package-lock.json` has moved and
does nothing when neither has. That is the whole reason this Makefile exists rather than only npm scripts:
`npm run build` cannot know whether your tree matches the lockfile, so it either reinstalls every time or
silently builds against a stale one.

**The endpoint is shared, deliberately.** `tauri-dev` points at `../.dev/aeon.sock` — the same socket
`aeon/Makefile`'s `dev` target and the VS Code launch configs use. Override it with
`make tauri-dev AEON_ENDPOINT=/path/to/rpc.sock`. An app looking at a different socket from the daemon it
is meant to be talking to is the most confusing failure available here.

**`make dev` here is not `make dev` one level up.** This one runs vite in a browser, which has no Tauri
bridge — so `invoke` cannot reach the socket and the pill reads *not connected*. That is correct, and the
target says so before it starts. Use `tauri-dev` for anything involving the daemon.

## Debugging

`aeon/.vscode/launch.json` carries the configurations — **App: debug tests (vitest)** for `.tsx`
breakpoints, **App: web (Chrome)** for layout, **App: desktop shell (Rust)** for the shell, and **Stack:
daemon + desktop app** for both halves at once. See `aeon/AGENTS.md` §6.

For the React side of a *Tauri* window, use the window's own devtools (right-click → Inspect in a debug
build): VS Code attaches to the Rust process, not to the embedded webview.

## Linux needs system packages that Go does not

Tauri renders through the system webview, so `cargo build` fails without them. **The daemon runs
regardless** — `make dev-daemon` is the fallback. `make check-deps` here names what is missing with the
install line for your distribution, and `tauri-dev` and `tauri-build` depend on it, so they refuse early
rather than failing deep inside `glib-sys`'s build script.

```sh
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev \
                 libayatana-appindicator3-dev librsvg2-dev patchelf

# Fedora
sudo dnf install webkit2gtk4.1-devel gtk3-devel \
                 libappindicator-gtk3-devel librsvg2-devel patchelf
```

macOS needs Xcode Command Line Tools; Windows needs the WebView2 runtime, which ships with Windows 11.

## What is verified, and what is not

| | |
| --- | --- |
| The web layer | **verified** — `make test` (192 tests during S9), `make typecheck`, `make build` |
| `src-tauri/endpoint.rs`, `rpc.rs` | **verified** — they compile, and they complete a real JSON-RPC round trip against a running daemon |
| `src-tauri/main.rs` | **verified 2026-10-15** — compiled and run by the operator on a machine with the packages above; the window shows the daemon's version |

**All three rows are now verified**, the last one by the operator rather than by the author of this file:
`make dev` was run on a machine with the system packages and the window showed the daemon's version, which
is the one thing the skeleton exists to demonstrate. S1 is closed.

**The distinction is worth keeping in the record.** The two modules carrying the logic were provable here;
the Tauri binding around them was not, and stayed marked unverified for a day rather than being assumed.
That is the pattern for every later slice: a thing that has not run is not a thing that works.

## The pages

| Tab | Slice | What it exercises |
| --- | :--: | --- |
| **Status** | S1 · S2 | Is the daemon there, at what path, speaking which **negotiated** protocol version. Three connection states, and no protocol number while disconnected |
| **Providers** | S3 | Routes: provider, model, base URL, output cap, prices **in USD per million tokens**, and an API key that goes in and never comes out |
| **Ask** | S3 | A prompt against a route, streaming, with the cost in one of **three** states and errors in three parts |
| **Store** | S4 | The store's facts, which are otherwise only checkable with `sqlite3`: the path (copyable), `foreign_keys` **as read from the connection**, the pool shape, the migration ledger, the derived awaiting query **as SQL**, loops with their project roots, steps with their cost states, `step verify`, and a document-name box for watching a path be **refused** |
| **Step** | S5 · S6 | Run with explicit tool grants and declared context files; inspect a suspended question, reopen it by ID after a restart, answer it, and inspect the saved transcript, changed-file notes, compactions, and verification result. See [S6 test bench QA demo](../docs/aeon/S6-suspend/UI-QA-DEMO.md). |
| **Recovery** | S7 | Inspect a recovered step's saved case, structured failure, and unsafe tool receipts; replay retained recovery events and follow new ones. See [S7 test bench QA demo](../docs/aeon/S7-recovery/UI-QA-DEMO.md). |
| **Blueprints** | S8 | Load, edit, validate and save a blueprint; start a loop; inspect its ladder and pinned YAML; answer a gate and follow loop events. See [S8 test bench QA demo](../docs/aeon/S8-blueprints/UI-QA-DEMO.md). |

### The product pages — where each one lives

The table above is the test bench. The app a person uses is organised by **where the work is**, not by
kind of record: Home, Work, Flows, and a global Configuration.

| Page | Address | What it is | Files (`ui/src/`) |
| --- | --- | --- | --- |
| **Home** | `/` | What needs you across every project (open questions, blocked loops), what is running, your projects, and the launch composer. `/?component=<root>` opens it with that component already picked | `features/home/pages/HomeScreen.tsx`, `HomeOverview.tsx` (stats, needs-you feed, projects), `HomeLaunchComposer.tsx` |
| **Work** | `/work/:component?/:tab?` | One component (optionally with its sub components) or **All projects**. Tabs: Overview, Loops, Plan (Goals · Tasks · Roadmap), Knowledge (Code graph · Search index · Memory), Automation, Guardrails (Gates · Environments · Triage), Files, History | `features/work/WorkScreen.tsx` and one `Work*Tab.tsx` per tab; `workModel.ts` (tree, scope, counts), `planModel.ts` (task columns, Gantt bars), `knowledgeScope.ts` |
| **Jump box** | Ctrl K / Cmd K, anywhere | Components (with an optional tab: `ui know`), loops, pages, "Start a loop in <component>". It only navigates | `features/palette/paletteModel.ts`, `CommandPalette.tsx` |
| **Configuration** | `/config/:page?` | Only what is true for the whole daemon: Agents, Policy, MCP servers, Tools, Skills, Variables & secrets, Continuous work, Daemon settings, Backups, System records, Platform status | `features/config/` (`lib/configRegistry.ts`, `pages/*.ts`) |

Two rules hold the structure together:

- **Scope is the address.** A link to `/work/ui/knowledge` carries the component and the tab. Graph and
  search follow the *repository*, not the component: a sub component inside its parent's repository shares
  the parent's index, and the page says so.
- **A page that moved leaves a door behind.** A Configuration page that moved into Work carries
  `movedTo`; it leaves the menu and its old address redirects (`/config/memory` → `/work/all/knowledge`,
  `/config/gates`, `/config/environments`, `/config/triage` → `/work/all/guardrails`, `/config/automation`
  → `/work/all/automation`). The old list addresses (`/loops`, `/projects`, `/history`, `/roadmap`,
  `/goals`, `/components`) redirect the same way (`features/work/LegacyRedirect.tsx`).

Gates, Environments and Triage need one component picked on Work; on **All projects** they say so. Platform
status is the health of the whole daemon (graph roots, search indexes, the ledger); the explorer and the
search probe are in each component's Knowledge tab.

A tab is disabled-in-effect while disconnected: it says so rather than offering a form that would write to
nothing.

**Why the Store tab shows what it shows.** A memory's failures are invisible from where they happen, so the
tab is built around the four facts that go wrong silently:

- **`foreign_keys` is the answer, not the intention.** The wrong DSN spelling produces a store that looks
  configured and performs no cascades (measured — see S4's T1 notes), so the pill shows what the connection
  reports and names the consequence when it is off.
- **A `failed` ledger row is surfaced loudly.** An upgrade that stopped part-way leaves a store that
  otherwise looks fine.
- **The awaiting query is printed as SQL.** There is no `awaiting_user` column for anyone arriving from v1
  to find, and showing the derivation is how they learn why (DB6).
- **A loop's project root sits beside its slug.** The same slug in two checkouts is two loops, and a list
  keyed on the slug would eventually show two rows that look identical.

And one limitation it states rather than hides: a thinking block saved by `ask --save-step` is **UNSIGNED**,
because S3's streaming surface carries no signature. The tab renders `[thinking · UNSIGNED]` and
`step verify` says *no thinking signature in this transcript* — silence there would read as "a signature was
checked and was fine".

### How a page talks to the daemon

One generic call, and no Tauri command per method:

```ts
await rpc<RoutesListResult>("routes.list");
await rpc("routes.set", { id, provider, model, apiKey });
const unlisten = await onEvent((event) => { /* ask.delta, ask.done, … */ });
```

**This is S9's U11 arriving early**, and for a reason: the method table lives in `internal/protocol` and
nowhere else. A wrapper per method would be a second copy of it that every later slice has to remember to
update — and the one somebody forgets is the method that silently cannot be called from the app.

The cost is that callers name methods as strings. That is the right trade: a typo is a `-32601` from the
daemon, which is a clear failure, where a missing wrapper is a feature that quietly does not exist.

### If every call works and nothing streams

**Check `src-tauri/capabilities/`.** Tauri v2 gates the core `event` plugin behind an access-control list;
commands this app defines itself are not gated. With no capability, `invoke` works perfectly and `listen`
is denied — so the window makes calls, shows results, and receives **not one event**.

That is a real bug this project shipped for an afternoon. The symptom was a page stuck at "Streaming…"
forever. `capabilities/README.md` has the detail, and `test/capability.test.ts` guards it — because
deleting that file breaks no other test in the repository.

The second half of why it was hard: the UI **swallowed** the listener's rejection, so a denial became
silence. It now has the three parts, the Ask page refuses to start a stream it cannot receive, and the
shell shows the cause first. If you see *"the app could not install its event listener"*, that is this.

### Credentials

**The web layer never holds one.** The daemon reports `hasKey` and the last four characters; the key field
is empty on an edit, and empty means *keep*. Nothing is written to `localStorage` — a credential there would
survive the window, outlive the daemon, and be readable by anything that can read that profile. Three tests
assert all of it, and `AGENTS.md` §4c has the full rule.

**Nothing is persisted yet**, and the UI says so where a person will read it: routes and keys live in the
daemon's memory and are gone when it restarts. The store is S4. A settings page that looks like it saved is
worse than one that admits it did not.

## The toolkit

Components come from **`@cjlapao/ui-kit`** — it is already v1's dependency, so S9's port brings components
built on it. Three things to know:

- **`react-router-dom` is a peer dependency**, required even with one window.
- **`@cjlapao/ui-kit/styles.css` is imported once**, at `src/main.tsx`. It is prebuilt Tailwind v4.3.3, so
  this project's Tailwind must coexist with it rather than recompile it.
- **The bundle is large**: ~552 KB CSS, ~502 KB JS. Fine for a desktop app, and measured rather than
  guessed.

### Three things the kit cannot express — the third found by using it

**`Alert` discards its children.** It renders `title` and `description` and nothing else, so an alert
written as

```tsx
<Alert tone="amber" title="Nothing is saved"><p>The explanation…</p></Alert>
```

shows the heading and **silently drops the paragraph**. Found by reading the DOM after a test could not find
content that was plainly in the JSX — five alerts across this app were affected, each of them losing the
sentence that explained the warning.

So: **prose goes in `description`**, and anything with structure — the three-part error panel — uses `Panel`,
which does render children and has a `tone`. This is the same trap as reaching for the nearest existing
type, one level up: a component whose name fits is not necessarily a component that does what you need.

### Two more the kit cannot express yet

**This matters more than it sounds**, because it is where S9's thesis lives.

**`StepStatus`** is `"pending" | "active" | "completed" | "error"`. S9's **D13 needs `not-needed` and
`skipped` rendered distinctly** — and the easy mistake is mapping `not-needed` onto `completed`, which
tells a reader the lane *ran*.

**`ConnectionState`** is `"flowing" | "stopped" | "disabled"` and belongs to the kit's `ConnectionFlow`
diagrams, **not to socket liveness.** So `src/lib/connection.ts` defines its own three states — and a test
asserts it is *not* the kit's, so a later refactor cannot quietly map onto the nearest existing type.

**S9's T4 settles both**, by extending the kit upstream or composing locally. **Never by reusing the
nearest type** — that is precisely how `unknown` becomes `—` and `—` becomes `0`.
