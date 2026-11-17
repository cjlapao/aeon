# Aeon UI design language — the management page

Written 2026-11-11, from the Agents page (`aeon/ui/src/features/agents/`), the first page built this way. This is
the reference for applying the same design to the other management pages (Policy, MCP servers, Skills, Tools,
Variables, and the rest). It describes what the Agents page does today and why, so a new page can say "the
management page, with these differences" and be understood. Where a rule has a reason, the reason is here: a page
that breaks a rule should say why.

The pages are built from `@cjlapao/ui-kit` (re-exported through `ui/src/components/kit.tsx`, which sets the app's
defaults: glass variant, the user's control tone). Reach for a kit component before writing markup; the kit already
owns copy buttons, ellipsis, focus and a11y, and a page that rebuilds them drifts. **Never change a kit component's
default behaviour to suit one page** — pass props, or compose.

## 1. The three page shapes

| Shape | Used for | Example |
| --- | --- | --- |
| **List** | Finding a row among many | `AgentsScreen.tsx` — a table with search, sort and infinite scroll, plus an Activity tab |
| **Detail** | Reading one row, and editing it in place | `AgentDetail.tsx` — header, tabs, scrolling body |
| **Side panel** | One short task: create, import, duplicate, export, migrate, delete | `AgentCreateBody.tsx`, `AgentImportBody.tsx`, `AgentVerbs.tsx` |

The list opens the detail, and the detail's header offers the verbs that open a side panel. The address bar holds
the state that should survive a reload and a shared link: `?agent=<slug>` (which row), `?view=activity` (which list
tab), `?edit=1` (editing). Use `useSearchParams`; keep everything else in component state.

A list page keeps the same frame whatever it lists: a header built with `ContentActions` (title and one line of
purpose on the left, the primary verb on the right — "New", "Grant access"), underline `Tabs`, one action bar of
ghost controls (search, `TagPicker`, ghost `Select`), and a kit `Table` with `fullHeight` and `infiniteScroll` that
scrolls under a sticky header. The page registers with `frame: "bare", fill: true` so the height is CSS all the way
down. Where the daemon answers a whole list at once (the Policy rules), the table reveals it in steps as it is
scrolled; the order and the rows are still the daemon's. `features/settings/policy/PolicySection.tsx` and
`PolicyList.tsx` are the second reference after the Agents page.

## 2. The detail page

The page is three stacked parts, and **only the last one scrolls**:

1. **Header** (fixed). 2. **Tabs** (fixed). 3. **Body** (`min-h-0 flex-1 overflow-y-auto overflow-x-hidden pr-5`).

The body takes all the free height and scrolls inside itself, so the header and tabs never move. The `pr-5` is a
gutter: it keeps the scrollbar from sitting on top of the content. Every flex ancestor between the page and the body
needs `min-h-0`, or the body grows instead of scrolling. A tab bar from the kit is `h-full`; wrap it in `shrink-0`.

### The header

Built with the kit's `ContentActions`: content on the left, actions on the right.

- A ghost `IconButton` with an arrow, to go back (it asks before leaving unsaved edits).
- The avatar, then the **title** with its truth pills beside it (`yours`, `unsigned`, `Editing`), the slug under it
  in mono, then the description (clamped to two lines), then one line of state ("Nothing changed yet." / "Draft —
  changed: metadata").
- **Actions are hidden, never disabled**, when they cannot be done: a shipped row has no Edit or Delete; with no
  daemon there is no write at all and one line says why. A row of dead buttons teaches nothing.
- Actions are `ExpandableButton`s: an icon that opens into its label on hover. They are quiet, and the header stays
  calm. The one primary action in a state is a full `Button`.

### Edit mode: what the buttons mean

Editing happens in place, on the same page (`?edit=1`), not in a separate form page. The header's actions follow the
state of the draft:

| Draft | Actions |
| --- | --- |
| Nothing changed | **Done** (expandable, check icon). Leaves edit mode. |
| Something changed | **Cancel** (expandable, close icon) and **Save** (full button). Cancel asks before throwing the edits away. |

Done is never shown beside Save: a person with changes has to decide to keep or drop them, and a person without has
nothing to decide. Save only exists when there is something to save.

### Tabs

Overview, Prompt, Metadata, History. Overview is the form a person uses; **Metadata is where the details most rows
never need live**, kept apart so they do not crowd the common path. Anything the system sets and a person rarely
should (the avatar's kind and mood) is not offered at all.

## 3. The Overview: one form, in sections

A form is a vertical stack of **sections**, `space-y-8` apart. Every section has the same three parts:

```tsx
<section className="space-y-2" aria-label={title}>
  <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{title}</h3>   {/* a question */}
  <p className="text-sm opacity-80">{help}</p>                                              {/* one plain sentence */}
  {/* the control */}
</section>
```

- **The title is a question** a person would ask: "Who is it?", "What does it work with?", "What can it use?", "What
  does it report back?". It orders the page in the order a person thinks, not the order of the database columns.
- **The help line is one sentence in plain words**, written for someone who has never seen the daemon. No column
  names, no "saving posts the whole row". If a technical fact matters, it goes in a tooltip or the Metadata tab.
- **The control is the best control for the data**, never a raw text box when a structured one exists (section 5).

The identity section (`AgentIdentityFields`) is the model of a "who" block: the avatar is clickable and cycles the
character, colour swatches sit under it, and the name and description are ghost inputs beside it. One component is
shared by the create panel and the edit page, so the two cannot drift.

## 4. Inputs

- **Ghost variant** for inputs and textareas in these pages (`variant="ghost"`): they read as text until focused.
- **One size.** Set `size="md"` explicitly on every control in a form. Mixed sizes are the most visible way a page
  looks unfinished.
- **A label, then the field, then a hint** (`FormField`). A hint says what the field is for, not how it is stored.
- **A field that cannot change says so beside the field** ("The slug stays the same even if you rename the agent")
  instead of being silently read-only.
- **Prompts and other long text** use the kit's markdown editor with a rendered preview, never a bare textarea.
  Markdown renders through the kit's preview with `.aeon-markdown` styles (`ui/src/index.css`); raw HTML in it must
  not be trusted.
- **Code and JSON** use the kit's `CodeBlock`: read-only for display (coloured, copyable) and `readOnly={false}`
  for editing the same text in place (the metadata field). The colours come from `--code-*` variables in
  `index.css`, so it follows the light and dark palette.

## 5. Choosing the control for the data

| The data is | Use | Notes |
| --- | --- | --- |
| One or several agents (a rule's target, a filter, a seed selection) | `TagPicker` through `AgentTagPicker` / `AgentTagsPicker` (`ui/src/components/AgentTagPicker.tsx`) | The roster is the daemon's `agent.list`, so only an agent that exists can be chosen. One agent per field where the daemon takes one target; several only where it takes a list. |
| One of a closed list, many allowed (technologies) | `TagPicker` (`TechPicker.tsx`) | The list comes from the daemon (`handlers.list`), never hard-coded. |
| Many of a large grouped list (about seventy tools) | `Picker` with `multi` and `groupSelectable` (`ToolsPicker.tsx`) | The daemon's kits come first as groups; a group header is a checkbox that takes the whole group; search narrows what the header acts on. Rows carry a name and a three-line description. |
| A short list of structured rows (the handoff) | **A builder** (`HandoffBuilder.tsx`) | See below. |
| A file | `FileUpload`, simple variant, then show what is in it before acting | `AgentImportBody.tsx`: file, summary, import. |
| A closed pick with colour or picture | Swatches or a clickable avatar | The minion chooser. |
| Anything else with a shape | Think before reaching for JSON in a textarea | The metadata JSON is the exception, and it is on its own tab. |

### The builder pattern

When a column holds a small structured list, give each item a line and let the person edit the pieces: a **grid**
(`grid-cols-[9rem_minmax(0,1fr)_4.5rem_2rem]`) so no control is ever pushed out of view, a **header row** naming the
columns, an **Add** ghost button under the list, and **one-click suggestions** for the entries people most often
need (taken from what the shipped data already uses), which disappear once used. The row stores the daemon's own
format (a `?` on a field name marks it optional); the person never sees that detail, a switch says "Optional".

A builder holds its lines in its own state, so a line with no name yet stays on screen while it is typed, and the
parent hears only the finished lines. When the value changes from outside (a reload, a revert) the builder resets.

Free text is acceptable where the text IS the instruction (a field's purpose, an agent's prompt). The daemon still
checks what is saved and refuses by name.

## 6. Side panels

Side panels are for one short task. They use `OverlaySidePanel` with **`variant="simple"`**, a bigger title
(`titleClassName="text-xl font-semibold"`), and ghost inputs. They are **guided**: the text tells the person what
will happen, in order, in plain words.

- **Create** has the same sections as the Overview, in the same order, so the page a person lands on after creating
  is familiar. On success the new row's own page opens and a toast confirms it.
- **Import** is three steps: pick a file, see what is in it, import. Whatever is risky about the file is said before
  it is imported (an unsigned card asks for a confirmation dialog, `ConfirmModal`, and the import is marked).
- **Duplicate** says the copy starts identical and the original is untouched, then opens the copy.
- **Export** asks where the card should go; with no path it shows the card in a `CodeBlock` with a Copy button.
- **Migrate and Delete** are confirmations that say what will happen and what cannot be undone; delete asks for the
  slug to be typed.

## 7. Telling the person what happened

- **Toasts** (`useAppToast`, the kit's `ToastProvider`/`ToastViewport`) are how outcomes are reported. A success
  toast goes away by itself. **An error toast is sticky**: it stays until the person dismisses it, and it carries the
  daemon's own sentence, verbatim, so it can be read, copied and acted on. Panels do not keep a status box.
- **A refusal is the daemon's sentence**, shown as it came. The UI does not paraphrase it. A button stays live on an
  empty required field on purpose: the daemon's refusal teaches the rule better than a dead button.
- **A confirmation is a dialog** (`ConfirmModal`) only for something risky or irreversible, and the dialog says what
  will happen in plain words.
- **History** is a vertical `Stepper` timeline, newest first, for the whole page's activity and per row.

## 8. Behaviour rules

1. **Hide, don't disable**, an action that cannot be done at all. Disable only what is temporarily unavailable
   (saving in progress).
2. **The daemon is the source of truth for every list** (tags, tool kits, tools, vocabularies). The UI holds no name
   of its own, so it cannot drift from the registry. A page asks one registered method for each list.
3. **Writes go through `usePanelWrites`**: busy state, the daemon's refusal as a sticky toast, and the refresh after
   a landed write. A write that was already in flight when the panel closed still refreshes the table.
4. **The page owns one `agent.view`-style read** and draws the header and the body from it: asked once, shown twice.
5. **Nothing the page cannot do is offered.** No daemon, no write; a shipped (internal) row, no Edit or Delete.
6. **Whole-row writes** (the agent edit posts the whole row, AR5) mean the page must hold the row as loaded and put
   the typing on top. Never post only the fields that differ.

## 9. Words

- Plain English, short sentences, a person's vocabulary: "What can it use?" not "tool_access".
- Both `en-*.json` and `pt-*.json` are updated together (`ui/src/lib/i18n/`), key for key. Messages are ICU:
  **avoid raw apostrophes** (use the typographic ’), and a line break in a message collapses, so multi-line text is
  built from one key per line.
- Do not repeat a technical hint the page already makes obvious.

## 10. Tests and guards

- A page's RPC use is pinned by a **guard test** (`agentsRpc.guard.test.ts`): the page opens exactly its allowed
  doors and no other, the writable fields match what the daemon's `Edit` assigns, and en/pt keys match.
- Product code in a guarded feature avoids a few patterns the guard bans (for example `.filter`/`.slice` on the
  daemon's lists): use loops. Read the guard before adding a file to a feature.
- Test with the fake daemon (`installFakeDaemon`) and shared fixtures (`agentPanelKit.tsx`). Wait for the form to
  load (`agent-edit-description`) before asserting on the header.
- **Every file stays under 1000 lines** ([AGENTS.md §4e](../../AGENTS.md#4e-files-stay-under-1000-lines)): a test
  file that grows past it is split by concern, with its shared fixtures moved to a kit file.
- Verify visually before calling a page done: run the app (or a throwaway harness) and look. Tests do not catch a
  clipped column.

## 11. Applying it to another page — a checklist

1. Which shape is it: list, detail, side panel? Most management pages are list + detail + a few panels.
2. Detail header: back, identity, truth pills, one line of state; actions hidden when impossible; Done, or Cancel +
   Save.
3. Overview sections as questions, each with one plain help line.
4. For every field, pick the control from section 5. Move rarely-needed fields to a Metadata-style tab.
5. Side panels: simple variant, bigger title, ghost inputs, guided text, toasts for outcomes.
6. Lists from the daemon, not from the UI. Refusals verbatim, errors sticky.
7. i18n in both languages, a guard test for the page's doors, files under 1000 lines.
8. Look at it running.

## 12. Where the Agents page keeps each piece

| Piece | File |
| --- | --- |
| List, tabs, URL state | `ui/src/features/agents/AgentsScreen.tsx` |
| Detail: header, actions, edit state | `AgentDetail.tsx`, `AgentPanel.tsx` |
| Edit tabs (Overview, Metadata) | `AgentEditTabs.tsx` |
| Identity block (minion, colour, name, description) | `AgentIdentityFields.tsx`, `MinionChooser.tsx` |
| Technology tag picker | `TechPicker.tsx` |
| Tools picker | `ToolsPicker.tsx` |
| Handoff builder | `HandoffBuilder.tsx` |
| Create, import, other verbs | `AgentCreateBody.tsx`, `AgentImportBody.tsx`, `AgentVerbs.tsx` |
| History timeline | `AgentChanges.tsx`, `AgentsActivity.tsx` |
| Model: fields, params, helpers | `agentsModel.ts` |
| The same frame on another list (Policy): header, tabs, action bar, infinite table | `ui/src/features/settings/policy/PolicySection.tsx`, `PolicyList.tsx`, `PolicyFilters.tsx` |
| The same frame without tabs (Variables, MCP servers, Skills): header with one or two verbs, action bar, infinite table, side panels | `ui/src/features/settings/variables/`, `mcp/`, `skills/` (`SkillsSection.tsx`, `SkillsTable.tsx`, `SkillPanel.tsx`, `SkillInstallForm.tsx`) |
| Shared panel shell | `ui/src/components/OverlaySidePanel.tsx` |
| Toasts | `ui/src/lib/useAppToast.tsx` |
| Code colours | `ui/src/index.css` (`--code-*`) |

## 13. The Work page and the way around it

The Agents page is the reference for one kind of record. The **Work page** (`ui/src/features/work/`) is the
reference for a page that answers the same question for a *scope*. It was added after the rest of this
document and reuses its frame.

**Scope.** The component tree sits in the side menu. The page is for the node picked there, or **All
projects**. Every tab answers for the scope: one component, plus its sub components while "Include sub
components" is on (the default for a component that has some). The scope is the address
(`/work/:component?/:tab?`); `workModel.ts` holds the tree, `scopeRoots` and the counts. A tab that is about
one component only (Gates, Environments, Triage, Roadmap) says "pick one component" on All projects; it never
shows an empty table.

**One frame, per tab.** The page header is `ContentActions` with `PageTitle` and the verb ("Start a loop"),
then underline `Tabs`. A tab with several views uses **pill** `Tabs` under the tab row (Plan: Goals · Tasks ·
Roadmap; Knowledge: Code graph · Search index · Memory; Guardrails: Gates · Environments · Triage). Rows are
kit `Table`s with `fullHeight`; a row opens a side panel (`OverlaySidePanel`), never a hand-rolled modal.

**Boards follow the daemon.** The task board and the Gantt are read-only views of real states: a card moves
when an agent finishes a step, there is no dragging. Columns are a grouping of the daemon's own statuses
(`planModel.ts`), a card keeps its true status as a tag, and a bar's dates are the daemon's first-started and
last-finished times when it has them (an item that has not started sits on its creation day, labelled "not
started"). Writes stay behind the doors the old pages had: the item drawer confirms, goal decisions are a
person's.

**Inheritance, said out loud.** A value belonging to a component is *set here*, *inherited* from a parent, or
the *global default*, and the page says which. A change that others inherit names how many before it is
saved.

**Finding your way.** Four aids, in this order: the breadcrumb (every part a link), the component tree, the
**Ctrl K jump box** (`features/palette/`, navigates only, remembers the last place), and Home's "needs you"
feed. A page that moves keeps its old address working: redirect, never a dead link.

**Configuration stays global.** A page belongs in Configuration only if it is true for the whole daemon. A
page that is about one component moves into Work, and its registry entry carries `movedTo`.

| Piece | File |
| --- | --- |
| Page, tabs, scope toggle | `ui/src/features/work/WorkScreen.tsx` |
| Tree, scope, counts, addresses | `workModel.ts` |
| Task columns, Gantt bars, links | `planModel.ts`, `WorkTasksView.tsx`, `WorkRoadmapView.tsx` |
| Graph/search scope rules | `knowledgeScope.ts`, `WorkKnowledgeTab.tsx` |
| Old addresses | `LegacyRedirect.tsx` |
| Home feed, stats, projects | `ui/src/features/home/pages/HomeOverview.tsx` |
| Jump box | `ui/src/features/palette/` |
