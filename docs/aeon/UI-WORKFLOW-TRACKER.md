# Workflow tracker integration

Aeon uses the three kit tracker parts on saved loop pages. `aeon/ui/src/features/loops/components/Workflow.tsx` is the one API-to-`WorkflowData` adapter; the loop screen owns selection and gate submission. Use this guide for subsequent workflow screens and review against the checklist below.

---

## Mission

Use a kit `Panel` for the full-width run header, then `WorkflowTrackerRail`, `WorkflowTrackerStepDetails`, and `WorkflowTrackerDetails` from **`@cjlapao/ui-kit`** for the saved loop workflow. In the header, an icon-only left-caret `IconButton` sits beside **Blueprint workflow** and has a **Project loops** tooltip. The blueprint type aligns with the back button’s left edge beneath the larger **Blueprint workflow** label; the single status pill is level with the button at upper right, with the workspace path directly below it. The path has no extra label. The read-only goal occupies the full row below in a kit `Textarea` fixed to three lines with internal scrolling. Total model cost, wall elapsed time, and recorded step time follow beneath it. The rail occupies the left column; the active step card sits above the details body in the right column. On narrow screens the three cards stack. Aeon does not render the all-in-one `WorkflowTracker` on this page.

The kit peers are `react` `^18.0.0 || ^19.1.0`, `react-dom` likewise, and `react-router-dom` `^6 || ^7` for other kit components; the workflow parts themselves need no router. Remove any bespoke progress, timeline, substep, or action-row markup. A waiting lane with a gate step and `gateAnswers` is an approval gate; the saved `loop.show` snapshot supplies its answers and prompt. Gate-only lanes and nested gate rows use **Approval gate** as their secondary label; omit cost and step time there and in the gate inspector. A lane that also contains model work still reports its model cost and duration.

## Install / verify

```bash
npm install @cjlapao/ui-kit@0.2.4
```

If your app resolves the kit through a local link (e.g. `file:..` / symlink),
the bundle it serves may be the kit's prebuilt `dist` — rebuild the kit
(`npm run build` in the kit repo) whenever you change the installed copy, and
never assume the linked package picked up source edits.

Import surface (all from the package root):

```ts
import {
  WorkflowTrackerRail,        // progress header + vertical timeline + legend
  WorkflowTrackerStepDetails, // active-step card (title, badge, meta, sub-steps, quick actions)
  WorkflowTrackerDetails,     // scrollable caller-content card
} from "@cjlapao/ui-kit";

import type {
  WorkflowData, WorkflowStep, WorkflowSubStep, WorkflowAction,
  WorkflowStatus, WorkflowTrackerRailProps,
  WorkflowTrackerStepDetailsProps, WorkflowTrackerDetailsProps,
  WorkflowTrackerLabels,
} from "@cjlapao/ui-kit";
```

## Core concepts (violating these is the #1 integration mistake)

- **One data object.** Everything — progress %, tallies, legend, substep nesting, and the active step — is **derived**
  from `data: WorkflowData`. You never pass counts, percentages or "is
  active" flags. The parts never mutate it.
- **Stateless.** The parts own no selection. The **parent owns
  `data.activeStepId`** and re-renders with a new one when the user clicks a
  step (`onStepSelect`). For the parts, the *same* parent state must be
  spread into every part's `data`, or they desync.
- **Read-only unless you pass callbacks.** No callback → no interactive
  affordance is rendered anywhere: no row buttons, no action buttons. This is
  a deliberate contract; never fake interactivity with `className` hacks.
  - `onStepSelect(stepId)` → rail rows become buttons.
  - `onSubStepSelect(stepId, subStepId)` → sub-step rows become buttons.
  - `onStepAction(stepId, actionId)` → steps' `actions` render as buttons.
    **Without it, `actions` data renders nothing.**
- **Theming is CSS-only.** The kit styles dark mode with Tailwind `dark:`
  utilities driven by a `.dark` class on `<html>` (or any ancestor). You never
  tell the tracker which theme is active, and you never hardcode colors in
  `details` content — pass nodes that themselves use the kit's components or
  `dark:`-paired utilities.
- **Unknown status values degrade safely**: any unrecognized `status` becomes
  `not_started`; don't pre-sanitize.

## Data model (verbatim from the kit types)

`React` is imported from `react`; `TrueColor` and `ButtonVariant` are imported from the kit theme module in the source. App code should import the public workflow types from `@cjlapao/ui-kit`.

```ts
export type WorkflowStatus =
  | "done"
  | "in_progress"
  | "running"
  | "skipped"
  | "blocked"
  | "attention"
  | "not_started";

export interface WorkflowSubStep {
  id: string;
  label: React.ReactNode;
  status: WorkflowStatus;
  /** Secondary line under the label, e.g. "Vendor uploading — 2 of 3 files received". */
  note?: React.ReactNode;
  /** Pill text. Tint is derived from `status` unless `badgeTone` overrides it. */
  badge?: React.ReactNode;
  badgeTone?: TrueColor;
  /** Right-aligned duration. Renders an em dash when omitted. */
  duration?: React.ReactNode;
}

/**
 * A quick action a step exposes — the kit `Button` row a gate-type step
 * renders in the rail and in the step detail card. It renders only while
 * the tracker is interactive through `onStepAction`.
 */
export interface WorkflowAction {
  id: string;
  label: React.ReactNode;
  /** Button tint. @default the tracker's `accentColor` */
  tone?: TrueColor;
  /**
   * Button treatment. The quiet `soft` default keeps the timeline calm;
   * override per action when it must read stronger.
   * @default "soft"
   */
  variant?: ButtonVariant;
  /** Kit icon name or a custom element, forwarded to `Button` untouched. */
  leadingIcon?: string | React.ReactElement;
  disabled?: boolean;
}

export interface WorkflowStep {
  id: string;
  label: React.ReactNode;
  status: WorkflowStatus;
  /** Pill text. Tint is derived from `status` unless `badgeTone` overrides it. */
  badge?: React.ReactNode;
  badgeTone?: TrueColor;
  /** Secondary line in the rail, e.g. "12m · 3 of 3 sub-steps". */
  meta?: React.ReactNode;
  /** Shown top-right of the detail panel while this step is the active one. */
  elapsed?: React.ReactNode;
  /** Paragraph under the detail-panel title. */
  description?: React.ReactNode;
  owner?: React.ReactNode;
  startedAt?: React.ReactNode;
  sla?: React.ReactNode;
  /** Rendered as a table in the detail panel, and nested in the rail, when active. */
  subSteps?: WorkflowSubStep[];
  /**
   * Free-form content for the details card — a second card stacked below the
   * step card while this step is the active one. The tracker only frames it
   * (shared card surface, `ScrollArea`, accessible name); the node keeps its
   * own headings and layout. The card is omitted when this is `null`,
   * `undefined` or `""`.
   */
  details?: React.ReactNode;
  /**
   * Quick actions (approve/decline/nudge…) for gate-type steps. Rendered as
   * a wrapping row of `xs` soft buttons under the step's meta line in the
   * rail, and in the step detail card's header — only while `onStepAction`
   * is supplied.
   */
  actions?: WorkflowAction[];
}

export interface WorkflowData {
  eyebrow?: React.ReactNode;
  title: React.ReactNode;
  /**
   * Mark, logo or glyph shown beside the title. Any node — an `<img>`, an
   * inline SVG, a letter. It is clipped to `iconCorner`, so an image fills the
   * box without needing its own rounding.
   */
  icon?: React.ReactNode;
  /** Shows the "LIVE · read-only view" chip. */
  live?: boolean;
  /** Which step fills the detail panel. Falls back to the first in-progress step. */
  activeStepId?: string;
  steps: WorkflowStep[];
}
```

## Gate actions and details body

Map a waiting gate to `status: "attention"` and `actions` with the daemon's allowed answers. Pass the same `onStepAction(stepId, actionId)` callback to the rail and step card. The two button placements are intentional: either one calls the same answer handler. Re-derive each action with `disabled: true` while the request is pending. Without `onStepAction`, both parts hide all action buttons even if `actions` is present.

Put run totals and timing in the full-width header `Panel`. Put artifacts and transcript in the active step's `details`. `WorkflowTrackerDetails` frames the node as its own `Panel` with a named scrollable region; the node supplies headings and content. The card mounts only for nonempty `details` on the active step. `null`, `undefined`, and `""` omit it. Use kit primitives and neutral utilities paired with `dark:` variants in this content.

## Three parts in one loop layout

Each part owns its own `Panel` and matches the kit composite card styling. Aeon places the rail in a sidebar and stacks the step card above the details body. The loop page owns one selection and sends the same `WorkflowData` to all three.

```tsx
import { useState } from "react";
import {
  WorkflowTrackerRail,
  WorkflowTrackerStepDetails,
  WorkflowTrackerDetails,
} from "@cjlapao/ui-kit";
import type { WorkflowData } from "@cjlapao/ui-kit";

function SplitRunView({ run, onAction }: {
  run: Omit<WorkflowData, "activeStepId">;
  onAction: (stepId: string, actionId: string) => void;
}) {
  const [activeStepId, setActiveStepId] = useState<string | undefined>();
  const data: WorkflowData = { ...run, activeStepId };

  return (
    <div className="grid min-w-0 gap-4 lg:grid-cols-[20rem_minmax(0,1fr)] lg:items-start">
      <div className="min-w-0">
        <WorkflowTrackerRail
          data={data}
          showLegend={false}
          onStepSelect={setActiveStepId}
          onStepAction={onAction}
        />
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <WorkflowTrackerStepDetails data={data} onStepAction={onAction} />
        <WorkflowTrackerDetails data={data} />
      </div>
    </div>
  );
}
```

Part contract — follow exactly:

- Every part takes the **whole `data` object** (plus styling props relevant to
  it and `labels`) and derives its slice itself: rail = progress/tallies/
  active-with-fallback, step card = active step + sub-steps, details = active
  step's `details` (mounts on the same non-empty rule, `null` while `loading`).
- **All parts are stateless and must be fed the same `data`** — including the
  same `data.activeStepId` from one parent state. Never give a part its own
  local selection state.

Callbacks are per slice:

| Part | `onStepSelect` | `onSubStepSelect` | `onStepAction` |
| --- | --- | --- | --- |
| `WorkflowTrackerRail` | Yes | Yes, for nested rows | Yes |
| `WorkflowTrackerStepDetails` | No | Yes | Yes |
| `WorkflowTrackerDetails` | No | No | No |

- **Styling props** shared by all three: `accentColor`, `attentionColor`,
  `blockedColor`, `mutedColor`, `variant` (Panel surface: `outlined`, `tonal`,
  `subtle`, `glass`, `liquid-glass`…), `cardTone`, `corner`, `glassOpacity`,
  `vibrancy`, `specularMode`, `padding`, `translucentSurfaces`, `className`,
  `style`. The three parts share defaults (`blue`/`rose`/`amber`/`neutral`,
  `outlined`, `cardTone: "neutral"`), so they match without passing anything.
- **Advanced only** (documented escape hatches — don't use for everyday
  styling): `cardClassName` (custom card positioning) and `cardScrollable` on
  the rail (when a constrained height needs internal scrolling).
- Each part renders **its own `Panel` card** — never wrap one in your own
  card/`Panel`; wrap it in a plain `div` for width (as the `min-w-0` above).
- Don't rebuild what the parts already do: no custom progress bars, step
  lists, sub-step tables, badges or action rows. If a part is *almost* right,
  you're holding it wrong — the same `data` drives it.

## Design-language and theme rules

- Tones are the kit's `TrueColor` union — set `accentColor` etc.; never inject
  custom hex colors into tracker chrome. Inside `details` content, prefer kit
  components; if raw markup is unavoidable, use neutral + `dark:`-paired
  utilities so the `.dark` class flip restyles everything with zero JS.
- Copy is overridable through `labels` (including `labels.details` — the
  accessible name of the details region — and `labels.quickActions`, the
  action group's accessible name). Route real i18n through them.
- Mixed content is fine: `React.ReactNode` is accepted for labels, badges,
  meta, owner… — but keep interactive elements out of `details` other than kit
  `Button`s, and never nest a button inside a step row (the kit's actions are
  siblings of the row button, by design).

## Acceptance checklist

1. `rg` finds **zero** hand-rolled step/progress/timeline markup for the
   migrated screens; only kit imports remain.
2. Gate steps show quick-action buttons in **both** the
   rail and the step card header, and clicking reports the right
   `(stepId, actionId)` pair (wire to real handlers with pending→`disabled`).
3. **Read-only rule**: removing `onStepAction` from the tree removes *all*
   action buttons; removing `onStepSelect`/`onSubStepSelect` removes row
   buttons. (Verify in a test or by toggling props.)
4. The details card appears only for the active step and hides for empty
   `details`. Long transcripts remain readable, with no horizontal overflow or nested page scroll.
5. The loop screen uses the **three parts with one parent-owned
   `activeStepId`**; clicking a rail row updates step card and details body;
   no part is wrapped in a custom `Panel`; no local per-part selection state.
6. Light/dark: toggling the `.dark` class restyles all three cards fully with
   no flicker or hardcoded color left behind.
7. `data` arrives normalized from your API layer into `WorkflowData` in **one
   place** (adapter/mapper), not scattered across views; `activeStepId`
   fallback (first `in_progress`, else first step) is respected — don't
   re-implement it.
8. Typecheck and the app's tests pass; add tests mirroring 2–5 for the
   migrated screens (kit suite covers the components; yours covers wiring).

## Anti-patterns (reject in self-review)

- Passing `progress`, counts or `isActive` per step — derive from `steps`.
- Two sources of truth for the active step (parent state + part-local state).
- Rendering your own approve/decline buttons beside the tracker when the
  step's `actions` can carry them — or rendering `actions` data and never
  passing `onStepAction` (dead data: nothing shows).
- Wrapping parts in `Panel`/bordered divs, or restocking their card with
  `className` overrides of padding/rounding — use `cardTone`/`corner`/`padding`
  props instead.
- Hardcoded `bg-white` / `text-gray-…` inside `details` content (breaks dark).
- Supplying example fixture data instead of the saved loop snapshot.

## Reference material (kit repo — read if available)

- `react/demo/src/kit-docs/components/workflow-tracker/examples/ComposableParts.tsx`
  — the split-view story (rail sidebar + stacked cards, one `activeStepId`).
- `.../examples/DetailsCard.tsx` — the approval-gate story (`attention` +
  Approve/Decline actions + details body).
- Specs: `docs/superpowers/specs/2026-10-20-workflow-tracker-details-panel-design.md`,
  `…-quick-actions-design.md`, `…-composable-parts-design.md`.
- Behavior is pinned by the kit suites (`WorkflowTracker.test.tsx` 49 tests,
  `WorkflowTrackerParts.test.tsx` 13 tests) — if integration fights the component, the component is right.
