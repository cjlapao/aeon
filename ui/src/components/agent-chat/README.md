# Agent chat

## Choose the surface

| Component | Use it for | Responsibility |
| --- | --- | --- |
| `MessageComposer` | First-message forms or inputs without conversation history | Shared draft, text, attachments, paste/drop, previews, plus menu, triggers, panels, submission, and custom controls. |
| `ChatComposer` | Existing-loop messages and agent steering | Thin MessageComposer adapter supplying target/provider/effort controls, approval, Stop, and legacy callbacks. |
| `AgentChat` | Conversation history plus its composer | Stream rendering and ChatComposer composition. |

Both inputs use one implementation in [MessageComposer.tsx](./MessageComposer.tsx). A first-message host needs neither an empty chat nor a dummy `onSteer`. HomeScreen is migrated: [HomeLaunchComposer.tsx](../../features/home/pages/HomeLaunchComposer.tsx) wires uploads (`attachment.put` receipts on `loop.start`), the route-capability gate, the context pickers, loop creation, and navigation. The loop-workspace hosts keep their own wiring.

Reusable React chat presentation for loop workspaces, agent views, transcript history, and fixtures. The host supplies data and wires callbacks to its services. These components do not call the daemon, fetch picker results, aggregate tasks, persist selections, or create loop links.

Import public components, adapters, and types from this directory's `index.ts`:

```tsx
import {
  AgentChat,
  type ChatItem,
  type ChatPanel,
  type ChatPersistentPanel,
  type ChatSpecialKeyRequest,
  type ChatAddAction,
  type ChatAttachment,
} from "./components/agent-chat";
```

Adjust the import path for the consuming file. Aeon's application already loads its Tailwind stylesheet and then `@cjlapao/ui-kit/styles.css`. Keep that order when embedding these components elsewhere. Component CSS is imported by the components. Apply the application's `dark` ancestor class for dark mode.

## New composer APIs at a glance

Pass these through `AgentChat.composer`, or directly to `ChatComposer`. Standalone `MessageComposer` uses `onSubmit(draft)` instead of the chat callbacks:

| API | Purpose |
| --- | --- |
| `onAddAction(action)` | Handle plus-menu choices: `files`, `folders`, `skills`, `loops`, `agents`, `commands`. |
| `onAttachmentsChange(attachments)` | Observe local file/image drafts, removal, and clearing after send. |
| `onMessage(text, route?, attachments?)` | Receive local files with a loop message; upload them in the host. |
| `onSteer(request)` | Receive local files in optional `request.attachments` when steering an agent. |
| `dropTargetRef` | Override where file/folder drops are accepted. AgentChat defaults to the entire chat. |
| `specialKeys` | Configure triggers, including multi-character keys such as `//`. |
| `onSpecialKeyChange(request \| null)` | Open/filter a host picker or dismiss it. |

For AgentChat, attachment/menu APIs go in its composer object. Top-level `picker`, `onPickerDismiss`, `panels`, and `taskList` take precedence over composer-supplied values. Direct MessageComposer/ChatComposer usage accepts all these props directly. See [local attachments](#plus-menu-and-local-attachments) for upload examples and [special-key callbacks](#special-key-callbacks) for picker integration.

## Quick start

```tsx
import { AgentChat, type ChatItem } from "./components/agent-chat";

const items: ChatItem[] = [
  { id: "hello", kind: "text", agent: "builder", text: "Checking the project…" },
];

export function Conversation({ sendMessage }: {
  sendMessage: (text: string) => Promise<void>;
}) {
  return <AgentChat
    height="70vh"
    items={items}
    agents={{ builder: { id: "builder", name: "Builder", role: "Implementation" } }}
    composer={{
      mode: "message",
      onSteer: () => {}, // Required by the shared props type; unused in message mode.
      onMessage: sendMessage,
    }}
  />;
}
```

Give the chat a bounded height, or place it in a sized flex parent with `min-height: 0`. The stream scrolls; the composer remains below it. `height` sizes the whole chat, including its composer.

## Standalone `MessageComposer`

```tsx
import { useRef, useState } from "react";
import { MessageComposer, type ComposerDraft } from "./components/agent-chat";

export function FirstMessage({ createLoop }: {
  createLoop: (request: { draft: ComposerDraft; blueprint: string }) => Promise<void>;
}) {
  const [draft, setDraft] = useState<ComposerDraft>({ text: "", attachments: [] });
  const [blueprint, setBlueprint] = useState("");
  const inputRef = useRef<HTMLTextAreaElement>(null);
  return <>
    <button onClick={() => {
      setDraft((current) => ({ ...current, text: "Review the current changes." }));
      inputRef.current?.focus();
    }}>Quick starter</button>
    <MessageComposer value={draft} onChange={setDraft} inputRef={inputRef}
      submitLabel="Start loop" submittingLabel="Starting…" submitDisabled={!blueprint}
      selectors={({ disabled }) => (
        <select aria-label="Blueprint" value={blueprint} disabled={disabled}
          onChange={(event) => setBlueprint(event.target.value)}>
          <option value="">Choose a blueprint</option>
          <option value="review">Review</option>
        </select>
      )}
      footer={<p>Blueprint validation belongs to this host.</p>}
      onSubmit={(submitted) => createLoop({ draft: submitted, blueprint })} />
  </>;
}
```

`createLoop` is supplied by your app. Receive original attachment Files through `draft`, upload them, and build the initial request there. The composer does not execute attached scripts or create a loop.

```ts
interface ComposerDraft {
  text: string;
  attachments: readonly ChatAttachment[];
}
```

| Prop | Type / default | Behavior |
| --- | --- | --- |
| `onSubmit` | `(draft: ComposerDraft) => void \| Promise<void>` | Required. Receives trimmed text and original attachments. |
| `value`, `onChange` | `ComposerDraft`, `(draft: ComposerDraft) => void` | Controlled draft. Supply both; text, file edits, and successful clearing notify the host. |
| `defaultValue` | `ComposerDraft` | Initial uncontrolled draft. Omit value for internal ownership; do not switch ownership during a mount. |
| `submitDisabled` | `boolean` | Block sending while leaving input and selectors editable. |
| `disabled`, `disabledReason` | `boolean`, `string` | Disable the surface; optionally explain in its placeholder. |
| `submitLabel`, `submittingLabel` | `string`, defaults `Send` / `Sending…` | Host-specific button labels. |
| `allowEmptySubmit` | `boolean`, default false | Allow a draft without text/files; the chat adapter uses this for settings-only steers. |
| `inputRef` | `Ref<HTMLTextAreaElement>` | Focus the shared input from quick starters or external controls. |
| `inputLabel`, `placeholder`, `rows` | `string`, `string`, `number` | Input name (default `message`), placeholder (`Message…`), and rows (2). |
| `selectors`, `actions`, `footer` | `ComposerSlot` | Custom control/content slots below. |
| `variant` | `"standalone" \| "chat"` | Standalone rounded border by default; chat retains its top-border style and stream-relative panel limits. |
| `className`, `data-testid` | `string` | Input surface classes and test ID (default `message-composer`). |

MessageComposer also accepts `onAddAction`, `onAttachmentsChange`, `dropTargetRef`, `specialKeys`, `onSpecialKeyChange`, `panels`, `picker`, `onPickerDismiss`, and `taskList` directly. Standalone popup height uses the viewport and space above the input; no AgentChat ancestor is required. Keep popup hosts out of clipping containers.

Submission snapshots the draft and blocks duplicate sends. Success clears that draft; failure displays an alert and retains it. A different draft loaded by the host while submission is pending is preserved. Await uploads and submission before resolving `onSubmit`.

### Custom selectors, actions, and footer

```ts
type ComposerSlot = ReactNode | ((controls: ComposerControls) => ReactNode);

interface ComposerControls {
  draft: ComposerDraft;
  busy: boolean;
  disabled: boolean; // Overall disabled OR an asynchronous action is pending.
  pendingAction: string | null; // "submit" during submission.
  runAction: (name: string, action: () => void | Promise<void>) => Promise<void>;
}
```

`selectors` renders beside the plus button: use any Picker, Combobox, or custom component. `actions` renders before the submit button. `footer` renders below the toolbar for validation or advanced controls. The host owns selection state and captures it in `onSubmit`; selections are not automatically added to ComposerDraft.

Use `controls.disabled` on interactive custom controls. `submitDisabled` leaves selectors usable so users can fix validation. Custom async controls can share the busy lock and error display without clearing the draft:

```tsx
actions={({ disabled, runAction }) => (
  <button disabled={disabled} onClick={() => void runAction("save", saveDraft)}>
    Save draft
  </button>
)}
```

`saveDraft` is host-provided. `ChatComposer` inherits the shared draft, styling, and slot APIs, appending custom slots to its built-in controls. Its existing message/steer/stop callbacks stay compatible; the adapter owns `onSubmit` and empty-steer rules.

## `AgentChat` props

| Prop | Type | Behavior |
| --- | --- | --- |
| `items` | `readonly ChatItem[]` | Required presentation items, in display order. Use stable IDs. |
| `agents` | `Record<string, ChatAgent>` | Names, role badges, and colors keyed by agent ID. |
| `actions` | `ChatActions` | Answer, gate, diff, and queued-steer callbacks. |
| `agentFilter` | `string` | Show one agent's conversation; also supplies the composer's default fixed target. |
| `readOnly` | `boolean` | Disable stream actions and omit the composer and its panels. |
| `liveClock` | `boolean` | Enable live tool elapsed-time updates when timestamps are real. |
| `messageIndex` | `LoopMessageIndexProps` | Optional loop-wide human-interaction rail with local or loader-backed jumps. |
| `composer` | `Omit<ChatComposerProps, "agents">` | Omit for history only. Agent metadata comes from `AgentChat.agents`. |
| `panels` | `readonly ChatPersistentPanel[]` | Persistent context rows, such as a goal or queued message. |
| `picker` | `ChatPanel` | One temporary list picker, above the other panels. |
| `onPickerDismiss` | `() => void` | Host clears `picker` on close or Escape within the composer dock. |
| `taskList` | `ChatTaskList` | One task panel, aggregating whichever agents the host chooses. |
| `header`, `empty` | `ReactNode` | Optional chat header and stream empty state. |
| `height`, `className` | `string` | Chat height and outer styling. |
| `onLoadOlder` | `() => void` | Request older history near the stream top or through its fallback button. |
| `loadingOlder`, `hasOlder` | `boolean` | Loading indicator and availability of earlier history. |

Panels appear only when a composer exists and `readOnly` is false. `agentFilter` filters stream items; it does not filter host-supplied panel/task data.

## Three panel surfaces

### Temporary picker: `picker`

Only one picker can be supplied. Opening files, skills, agents, or loops replaces the previous picker; pickers never join the persistent stack. The picker overlays persistent context and tasks, with bounded scrolling. Its UI-kit `ScrollArea` body is capped at 180px/24dvh, further reduced to leave room for its header. Each panel derives its content-height budget from the smallest of 280px, 30% of visible screen height, 35% of chat height, and 45% of the visible space above the composer. The surrounding panel group has no scroll area or clipping height; multiple persistent panels use the carousel once there are more than two. Task bodies have a separate cap of 222px (six standard single-line rows plus padding), 40% of visible screen height, or available space above the composer minus 64px, whichever is smaller. Short screens and wrapped labels can show fewer rows. `vh` fallbacks support browsers without dynamic viewport units.

```ts
interface ChatPanel {
  id: string;
  title: string;
  description?: string;
  icon?: ReactNode;
  items: readonly ChatPanelItem[];
  emptyText?: string;
  onClose?: () => void;
}

interface ChatPanelItem {
  id: string;
  label: string;
  description?: string;
  icon?: ReactNode;
  disabled?: boolean;
  onSelect?: () => void;
}
```

Items with `onSelect` render as buttons. Disabled buttons cannot select. Items without callbacks render as display rows. Empty results show `emptyText`, defaulting to “No items to display.” `onPickerDismiss` takes precedence over `picker.onClose` for the close button and Escape inside the picker. To close on Escape from the composer, supply `onPickerDismiss` or clear picker state in `onSpecialKeyChange(null)`.

The host must clear picker state after dismissal or selection. A callback does not remove controlled props automatically. There is no outside-click dismissal, automatic text insertion, or built-in Arrow/Enter result selection. Native Tab and Enter/Space activate item buttons; Enter in the composer retains its send behavior.

### Persistent context: `panels`

```ts
interface ChatPersistentPanel {
  id: string;
  title: string;
  summary?: string;
  icon?: ReactNode;
  content: ReactNode;
  actions?: ReactNode;
  defaultExpanded?: boolean;
}
```

Panels start as compact header rows unless `defaultExpanded` is true. The summary truncates; expanding shows full `content`. Supply editors, details, or any other React content. Put action buttons in `actions`; they remain separate from the disclosure button.

One or two panels render directly. Three or more use the UI kit's `StackCarousel`, initially stacked. **Expand panels** shows all rows; **Stack panels** compacts the deck again. Each row still has its own disclosure. Carousel controls and Arrow/Home/End keys navigate when the carousel itself has focus. Wheel/swipe navigation is disabled so content scrolling does not change panels.

```tsx
const panels: ChatPersistentPanel[] = [
  {
    id: "goal",
    title: "Ongoing goal",
    summary: "Build the chat integration",
    content: <p>Inspect the API, wire host callbacks, and verify the result.</p>,
    actions: <button type="button" onClick={pauseGoal}>Pause</button>,
  },
  {
    id: "queued",
    title: "Queued message",
    summary: "Then run the checks",
    content: <textarea aria-label="Queued message" defaultValue="Then run the checks" />,
  },
];
```

Expanded panel content and task rows use UI-kit `ScrollArea`, with horizontal scrolling disabled and overscroll contained. Headers remain outside content scrolling. The persistent deck is a plain container, not a `ScrollArea`; only individual panel bodies scroll. Height measurements update for chat/composer resizing, page scrolling, and visual-viewport changes, including an on-screen keyboard.

The host owns editor state and persistence. Disclosure state is local; `defaultExpanded` is an initial value, not a controlled prop. Keep IDs stable. Changing between direct rows and the carousel can remount panels and reset local disclosure/editor state; keep important editor state in the host.

### Tasks: `taskList`

```ts
interface ChatTaskList {
  title?: string; // Default: "To-dos".
  tasks: readonly ChatTask[];
}

interface ChatTask {
  id: string;
  label: string;
  agent?: string; // Display label supplied by the host.
  status: "pending" | "in_progress" | "completed" | "failed";
}
```

Supply one object, aggregating tasks across agents in the loop. The task panel appears above persistent context and starts expanded. Its header counts `in_progress` and `pending`; rows also distinguish completed and failed tasks. An empty supplied list shows “No tasks yet.” Omit `taskList` to hide it. Replacing task data updates counts without resetting its disclosure.

```tsx
<AgentChat
  items={items}
  composer={composer}
  panels={panels}
  taskList={{ tasks: [
    { id: "inspect", label: "Inspect the API", agent: "Explorer", status: "completed" },
    { id: "build", label: "Wire callbacks", agent: "Builder", status: "in_progress" },
    { id: "verify", label: "Run checks", agent: "Reviewer", status: "pending" },
  ] }}
/>
```

## Special-key callbacks

Configure `composer.specialKeys` and `composer.onSpecialKeyChange`. The component recognizes triggers; the host decides what they mean and supplies `picker` results.

| Default trigger | Demo mapping |
| --- | --- |
| `/` | Skills and prompts |
| `//` | Link another loop |
| `@` | Files |
| `$` | Agents |

```ts
interface ChatSpecialKeyRequest {
  key: string;
  query: string;
  start: number;
  end: number;
}

onSpecialKeyChange?: (request: ChatSpecialKeyRequest | null) => void;
specialKeys?: readonly string[]; // Replaces the defaults when supplied.
```

- Triggers start at a whitespace-delimited token boundary. Embedded email/URL symbols do not activate a picker.
- The longest configured trigger wins, independent of declaration order. `/` becomes `//` when the second slash is typed; deleting it returns to `/`.
- `query` excludes the entire trigger. `See //review` reports `{ key: "//", query: "review", start: 4, end: 12 }`.
- `start` and `end` are zero-based JavaScript string offsets, with an exclusive end. They cover the trigger plus query in current composer text.
- Changes, pasted text, click-based caret movement, and horizontal/Home/End navigation re-evaluate the token. IME input waits for composition completion.
- Whitespace ends a token and sends `null`. Space after a bare trigger therefore closes the picker. This also applies after a nonempty query.
- Escape sends `null` and suppresses reopening the same token while it is edited. Moving to another trigger token allows opening again.
- A successful send clears text and sends `null`. Escape does not clear composer text.
- Empty configured triggers are ignored. Pass `[]` to disable trigger recognition.

### Complete host-controlled picker example

```tsx
import { useState } from "react";
import {
  AgentChat,
  type ChatItem,
  type ChatPanel,
  type ChatSpecialKeyRequest,
} from "./components/agent-chat";

interface ContextChoice {
  id: string;
  label: string;
  description?: string;
}

const titles: Record<string, string> = {
  "/": "Skills & prompts",
  "//": "Link another loop",
  "@": "Files",
  "$": "Agents",
  commands: "Commands",
};

export function ContextChat({ items, choices, onChoose, onMessage }: {
  items: readonly ChatItem[];
  choices: Record<string, readonly ContextChoice[]>;
  onChoose: (key: string, id: string) => void;
  onMessage: (text: string) => Promise<void>;
}) {
  const [request, setRequest] = useState<ChatSpecialKeyRequest | null>(null);
  const picker: ChatPanel | undefined = request ? {
    id: request.key,
    title: titles[request.key] ?? "Context",
    emptyText: "No matches.",
    items: (choices[request.key] ?? [])
      .filter((choice) => choice.label.toLowerCase().includes(request.query.toLowerCase()))
      .map((choice) => ({
        ...choice,
        onSelect: () => {
          onChoose(request.key, choice.id);
          setRequest(null);
        },
      })),
  } : undefined;

  return <AgentChat
    height="70vh"
    items={items}
    picker={picker}
    onPickerDismiss={() => setRequest(null)}
    composer={{
      mode: "message",
      onSteer: () => {},
      onMessage,
      specialKeys: ["/", "//", "@", "$"],
      onSpecialKeyChange: setRequest,
      onAddAction: (action) => {
        const keys = {
          files: "", folders: "", skills: "/", loops: "//", agents: "$", commands: "commands",
        };
        const key = keys[action];
        // Native file/folder pickers are opened by the composer itself.
        // Other choices reuse this host's controlled picker without inserting text.
        setRequest(key ? { key, query: "", start: 0, end: 0 } : null);
      },
    }}
  />;
}
```

For `//`, the host's `onChoose` can record a selected loop reference or invoke its link operation. The component does not invent an RPC or navigate to another loop. For remote search, cancel stale requests or check the active trigger/query before applying responses. Picker selection does not modify the internally owned composer text; token offsets are callback metadata, not an insertion API.

## Plus menu and local attachments

The composer includes a plus button with three groups:

| Group | Items | Behavior |
| --- | --- | --- |
| Add | Files and images, Folder | Open native file/folder pickers and stage selected files. |
| Context | Skills and prompts, Link another loop, Agents | Call `onAddAction` so the host can provide a picker. |
| Commands | Browse commands | Call `onAddAction("commands")`. |

`ChatAddAction` is `"files" | "folders" | "skills" | "loops" | "agents" | "commands"`. File/folder choices also notify `onAddAction` after opening their native picker. Without `onAddAction`, context and command rows are disabled; local attachment choices remain available. The menu uses UI-kit `Menu` for keyboard navigation, Escape, outside-click dismissal, and popup placement.

Drop files or folders anywhere over `AgentChat`, or paste images into its composer. Directly rendered `ChatComposer` accepts drops over itself; optionally supply `dropTargetRef` to extend that surface. Folder selection and dropped-directory traversal preserve relative paths when the browser exposes them, including directory-reader batches. Empty folders have no files to stage. Ordinary text paste remains unchanged; image paste stages the images and prevents its default insertion.

```ts
interface ChatAttachment {
  id: string;
  file: File;
  relativePath?: string;
}

onAttachmentsChange?: (attachments: readonly ChatAttachment[]) => void;
onAddAction?: (action: ChatAddAction) => void;
dropTargetRef?: RefObject<HTMLElement | null>;
```

Attachments are local draft data. Image thumbnails and file cards appear above the message input. Each has a remove button. Click an image thumbnail for a UI-kit modal preview; Escape or its close button returns to the composer. The attachment strip uses `ScrollArea` and stays bounded. Temporary object URLs are revoked when images are removed, the draft clears, or the component unmounts. These URLs are not part of `ChatAttachment`.

The host receives files on send:

The callback's `File` values retain the original bytes and browser metadata (`name`, `type`, `size`, `lastModified`). `relativePath` is optional folder context; `id` identifies an attachment within the current composer draft and is not a persisted server ID.

```tsx
<AgentChat
  items={items}
  composer={{
    mode: "message",
    onSteer: () => {},
    onAddAction: (action) => {
      // Open your controlled picker for skills, loops, agents, or commands.
      // Local files/folders are already handled by the composer.
      console.log("Add action", action);
    },
    onAttachmentsChange: (draft) => console.log("Local draft", draft),
    onMessage: async (text, route, attachments) => {
      const body = new FormData();
      body.set("text", text);
      if (route) body.set("route", route);
      for (const attachment of attachments ?? []) {
        body.append("files", attachment.file, attachment.relativePath ?? attachment.file.name);
      }
      // Use the host application's upload/send implementation.
      await sendMessageWithFiles(body);
    },
  }}
/>
```

`onMessage` receives a third optional `readonly ChatAttachment[]` argument only when files are present. Text-only sends retain their original two-argument call. In steer mode, files arrive as optional `SteerRequest.attachments`. `onAttachmentsChange` observes local draft changes, including removal and clearing after a send. Files can be sent without message text. A successful callback clears text and attachments; a rejected callback shows its error and retains both. Adding/removing attachments is disabled during send/stop and while the composer is disabled.

For agent steering, handle the same attachment draft through the request object:

```tsx
<AgentChat
  items={items}
  composer={{
    fixedTarget: "builder",
    onSteer: async ({ target, text, route, reasoningEffort, attachments }) => {
      // Convert local files to your application's persisted attachment references.
      const references = await uploadFiles(attachments ?? []);
      await steerAgent({ target, text, route, reasoningEffort, references });
    },
  }}
/>
```

`uploadFiles`, `steerAgent`, and `sendMessageWithFiles` in these examples are host-provided functions, not exports from this component. Await upload/send completion so a failure preserves the local draft. No new props are required for thumbnails, removal buttons, drag feedback, image paste, or the full-size preview: those are built into the composer.

Upload and persist attachments in the host before passing references to daemon RPCs; `File` objects are not JSON attachment references. The component imposes no file-count, size, or MIME restrictions. Host callbacks must enforce application limits and accepted content. Browser folder APIs and native shell drag/drop behavior vary; Tauri/native integration is separate from browser fixture validation. The demo stages files locally and records filenames in its fixture messages; it does not upload them or persist image messages.

## `ChatComposer` API

Use through `AgentChat.composer` or render `<ChatComposer />` directly.

| Prop | Type / default | Behavior |
| --- | --- | --- |
| `onSteer` | `(request: SteerRequest) => void \| Promise<void>` | Required shared callback; used in steer mode. |
| `mode` | `"steer" \| "message"`, default `"steer"` | Running-agent steering or a loop message. |
| `onMessage` | `(text: string, route?: string, attachments?: readonly ChatAttachment[]) => void \| Promise<void>` | Supply in message mode to perform sends/uploads. |
| `onAddAction` | `(action: ChatAddAction) => void` | Plus-menu action callback. |
| `onAttachmentsChange` | `(attachments: readonly ChatAttachment[]) => void` | Observe the locally staged draft. |
| `dropTargetRef` | `RefObject<HTMLElement \| null>` | Optional drop surface; AgentChat defaults to its whole chat. |
| `targets` | `ComposerOption[]` | Available agents; first is the initial fallback target. |
| `fixedTarget` | `string` | Fix the target and hide its picker. |
| `defaultTarget` | `string` | Initial target when not fixed. |
| `routes`, `efforts` | `ComposerOption[]` | Provider and reasoning-effort choices. |
| `currentRoute`, `currentEffort` | `string` | Current target settings; only changes are included in steers. |
| `onStop` | `(target: string) => void \| Promise<void>` | Optional Stop control in steer mode. |
| `autoApprove` | `{ on: boolean; onChange: (on: boolean) => void; lockedReason?: string }` | Host-controlled approval chip. A lock reason disables it. |
| `disabled`, `disabledReason` | `boolean`, `string` | Disable input/actions and optionally explain through the placeholder. |
| `placeholder` | `string` | Override mode-specific placeholder. |
| `agents` | `Record<string, ChatAgent>` | Direct-render metadata; inherited when used through AgentChat. |
| `specialKeys` | `readonly string[]` | Configurable trigger strings. |
| `onSpecialKeyChange` | `(request: ChatSpecialKeyRequest \| null) => void` | Trigger/query notification and dismissal. |

`ComposerOption` has `value`, `label`, and optional `reasoningEfforts?: string[] | null`. A route's declared efforts constrain the effort picker. `SteerRequest` has required `target` and `text`, plus optional `route`, `reasoningEffort`, and `attachments`.

Enter sends; Shift+Enter inserts a newline. IME composition does not send. Steer mode can send text, attachments, or a settings change; message mode requires nonblank text or an attachment. Text is trimmed before sending. While an async send/stop runs, its actions are busy; failures render an alert. Successful send clears the draft. The host must supply `onMessage` for meaningful message-mode behavior: it is optional in the shared type.

## Stream data and actions

Every `ChatItem` requires an `id`; `at` is an optional ISO timestamp. Agent-scoped items also require `agent`.

| `kind` | Main fields |
| --- | --- |
| `text` | `agent`, `text`, optional `streaming` |
| `thinking` | `agent`, `text`, optional `streaming`, `durationMs` |
| `tool` | `agent`, `tool`, `status`; optional summaries, output/detail, command, file, verdict, approval, timing, and tool/step IDs |
| `question` | `agent`, `questions: ChatQuestion[]` |
| `activity` | `agent`, `label`, `since`, optional `done` |
| `divider` | `tone`, `text`, optional `highlights` |
| `gate` | `title`, `options`, `state`; optional description, preview, answer, and step label |
| `steer` | `target`, `state`; optional text, route, reasoning effort, and steer ID |
| `message` | `author: "human" \| "system"`, `text`, optional source and message ID |
| `run` | `runSeq`, `blueprint`, `state`; optional version, cost, and starter |

`ToolStatus` is `running | done | error | skipped | awaiting | denied`. `PolicyVerdict` is `allowed | denied | asked`. These represent different outcomes and should not be collapsed into a generic error. See [types.ts](./types.ts) for the complete discriminated union and optional fields.

`ChatAgent` requires `id` and optionally supplies `name`, `role`, and CSS `color`. `ChatQuestion` requires `id`, `prompt`, and `kind` (`clarify | gate | permission | free`); optional fields include `label`, `options`, `answer`, and run-start context. `FileChange` requires `path`, with optional `linesAdded`, `linesRemoved`, and `bytes`.

```ts
interface ChatActions {
  onAnswer?: (question: ChatQuestion, answer: string) => void | Promise<void>;
  onGate?: (gateId: string, answer: string, feedback?: string) => void | Promise<void>;
  onViewDiff?: (file: FileChange) => void;
  onCancelSteer?: (steerId: number) => void | Promise<void>;
}
```

`ChatStream` accepts the stream-related AgentChat props: `items`, `agents`, `actions`, `agentFilter`, `readOnly`, `liveClock`, `empty`, `className`, `onLoadOlder`, `loadingOlder`, and `hasOlder`. It follows new content while pinned to the bottom, offers a jump control after scrolling away, and preserves the viewport when older history is prepended. The host fetches and prepends older items.

## Loop interaction index

Pass `messageIndex` to AgentChat or ChatStream to show a narrow navigation rail for human interactions. Include initial goals, user messages, steers, answers, approvals/rejections, commands, and other human actions your host persists. The component does not infer human intent from model `role: user` transcript records, which can include synthetic prompts.

The selected marker is longest, its immediate neighbors are medium-length and higher contrast, and other markers stay short and muted. Lines are right-aligned with compact 12px spacing. Message content receives no selection border; keyboard focus remains visible on marker buttons.

Index metadata is independent of the bounded ChatItem window. A marker can exist even when its target is absent from memory. Hover or focus shows its supplied excerpt and optional cached response context; no lookup occurs on hover. Activating a marker scrolls to a loaded target immediately or calls the host's loader for a surrounding window. The rail is hidden in an `agentFilter` zoom because its human interactions are loop-wide. It remains available in read-only history.

```ts
interface LoopMessageIndexEntry {
  id: string;             // Stable, opaque anchor scoped to the loop.
  targetItemId?: string;   // Matching ChatItem.id; defaults to entry.id for local lookup.
  preview: string;
  at?: string;
  kind?: string;          // e.g. goal, message, steer, answer, approval, command.
  contextPreview?: string;
}

interface MessageIndexRequest {
  before: number;
  after: number;
  signal: AbortSignal;
}

interface MessageIndexTarget { targetItemId: string }

interface LoopMessageIndexProps {
  scopeKey: string;
  entries: readonly LoopMessageIndexEntry[];
  before?: number;
  after?: number;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  onRequestJump?: (entry: LoopMessageIndexEntry, request: MessageIndexRequest) => Promise<MessageIndexTarget>;
  onRequestLatest?: (request: { signal: AbortSignal }) => Promise<MessageIndexTarget>;
  onActiveChange?: (id: string | null) => void;
}
```

`scopeKey` must change when the loop changes; pending requests, hover state, and active selection reset. `entries` are chronological marker metadata, not full messages. The host can page or bound that list separately, using `hasMore`, `loadingMore`, and `onLoadMore`. The plus marker loads more metadata. `onActiveChange` reports selected markers and the nearest loaded marker during manual scrolling; null reports reset.

`before` and `after` default to 20 each; each is clamped to 0–100. They describe durable conversation records, not rendered card counts or pixels. A durable record can yield multiple ChatItems, so the loader must return the actual rendered `targetItemId`. Multiple interactions may intentionally target one card, such as an answered question.

```tsx
<AgentChat
  items={historyWindow.items}
  messageIndex={{
    scopeKey: loopId,
    entries: interactionIndex.entries,
    hasMore: interactionIndex.hasMore,
    loadingMore: loadingIndex,
    onLoadMore: loadMoreIndexEntries,
    onRequestJump: async (entry, { before, after, signal }) => {
      const page = await loadConversationAround({ loopId, anchor: entry.id, before, after, signal });
      signal.throwIfAborted();
      setHistoryWindow(page);
      return { targetItemId: page.targetItemId };
    },
    onRequestLatest: async ({ signal }) => {
      const page = await loadLatestConversation({ loopId, signal });
      signal.throwIfAborted();
      setHistoryWindow(page);
      return { targetItemId: page.targetItemId };
    },
  }}
/>
```

These loader/state functions are host integrations, not new daemon methods. The host must commit its new items/window and paging cursors before resolving the loader. The stream waits for the target's rendered node; a target that does not appear within 10 seconds produces a retryable error. Reject missing/removed anchors explicitly rather than resolving an unrelated item.

New jumps abort older requests. Loop changes and unmounts abort outstanding work. The component ignores stale results; the host must also honor `signal` before committing fetched data, otherwise a stale response can replace its controlled items. Keep the window bounded by replacing or deliberately trimming items, rather than appending the entire loop history.

During indexed navigation the stream stops following live output and suppresses automatic older-history loading, so the selected window stays stable. The existing manual older-history callback remains host-owned. **Return to latest** invokes `onRequestLatest` when supplied, waits for its committed target, then scrolls to the tail and resumes live following. Local-only indexes can omit both loaders and scroll within loaded items. When remote loading is enabled, supply both loaders; returning from a remote window without `onRequestLatest` shows an explicit error rather than claiming the current window is the live tail. Continuous older/newer window paging and merging live/history items remain host responsibilities.

Backend integration needs a lightweight durable human-interaction index and an anchored loop-conversation read. Existing `loop.transcript` windows agent `step_message` rows and is not a unified index of human `chat_message` rows, steers, and answers. Do not compare IDs from separate tables as one cursor or use an array offset as an anchor. The current change adds only component APIs and a fixture loader; no backend protocol or database change is included.

### Host implementation example

Keep the interaction index and rendered history in separate state. The following service contract is an application adapter to implement against your backend; these names are not existing RPC methods:

```ts
import type { ChatItem, LoopMessageIndexEntry } from "./components/agent-chat";

interface InteractionPage {
  entries: readonly LoopMessageIndexEntry[];
  nextCursor?: string;
}

interface ConversationWindow {
  items: readonly ChatItem[];
  targetItemId: string;
  beforeCursor?: string;
  afterCursor?: string;
  hasOlder: boolean;
  hasNewer: boolean;
}

interface LoopHistoryApi {
  listInteractions(request: {
    loopId: string;
    cursor?: string;
    limit: number;
    signal: AbortSignal;
  }): Promise<InteractionPage>;
  around(request: {
    loopId: string;
    anchor: string;
    before: number;
    after: number;
    signal: AbortSignal;
  }): Promise<ConversationWindow>;
  latest(request: {
    loopId: string;
    limit: number;
    signal: AbortSignal;
  }): Promise<ConversationWindow>;
}
```

Bootstrap the first metadata page and latest history window in your screen loader. For an empty loop, render normal empty chat with no markers; successful jump/latest results must identify a real rendered target. Mount the loaded view with a loop key so its local state and in-flight metadata work reset on loop changes:

```tsx
<IndexedLoop
  key={loopId}
  loopId={loopId}
  api={historyApi}
  initialIndex={initialIndex}
  initialWindow={initialWindow}
/>
```

The view below uses the interfaces above. It replaces message windows instead of concatenating full history, handles metadata-page errors, and preserves loader cancellation through the adapter:

```tsx
import { useEffect, useRef, useState } from "react";
import { AgentChat } from "./components/agent-chat";

function IndexedLoop({ loopId, api, initialIndex, initialWindow }: {
  loopId: string;
  api: LoopHistoryApi;
  initialIndex: InteractionPage;
  initialWindow: ConversationWindow;
}) {
  const [page, setPage] = useState(initialIndex);
  const [window, setWindow] = useState(initialWindow);
  const [loadingIndex, setLoadingIndex] = useState(false);
  const [indexError, setIndexError] = useState("");
  const metadataRequest = useRef<AbortController | null>(null);

  useEffect(() => () => metadataRequest.current?.abort(), []);

  const loadMore = async () => {
    if (!page.nextCursor || metadataRequest.current) return;
    const controller = new AbortController();
    metadataRequest.current = controller;
    setLoadingIndex(true);
    setIndexError("");
    try {
      const next = await api.listInteractions({
        loopId, cursor: page.nextCursor, limit: 50, signal: controller.signal,
      });
      controller.signal.throwIfAborted();
      setPage((current) => ({
        // Pages are chronological; stable anchors deduplicate overlapping edges.
        entries: [...new Map(
          [...current.entries, ...next.entries].map((entry) => [entry.id, entry]),
        ).values()],
        nextCursor: next.nextCursor,
      }));
    } catch (cause) {
      if (!controller.signal.aborted) setIndexError(String(cause));
    } finally {
      if (metadataRequest.current === controller) metadataRequest.current = null;
      if (!controller.signal.aborted) setLoadingIndex(false);
    }
  };

  return <>
    {indexError && <p role="alert">{indexError}</p>}
    <AgentChat height="70vh" items={window.items} readOnly
      messageIndex={{
        scopeKey: loopId,
        entries: page.entries,
        hasMore: Boolean(page.nextCursor),
        loadingMore: loadingIndex,
        onLoadMore: () => void loadMore(),
        before: 20,
        after: 20,
        onRequestJump: async (entry, { before, after, signal }) => {
          const next = await api.around({
            loopId, anchor: entry.id, before, after, signal,
          });
          signal.throwIfAborted();
          setWindow(next); // Replace the bounded window and its cursors together.
          return { targetItemId: next.targetItemId };
        },
        onRequestLatest: async ({ signal }) => {
          const next = await api.latest({ loopId, limit: 40, signal });
          signal.throwIfAborted();
          setWindow(next);
          return { targetItemId: next.targetItemId };
        },
      }} />
  </>;
}
```

The example is read-only to isolate navigation. Remove `readOnly` and supply the usual composer/actions for an active loop. `setWindow` schedules the complete new window before the loader resolves; the component then waits for its target DOM node. Do not resolve a target that is absent from `items`, and do not resolve before the host has scheduled its window update.

Metadata in this example accumulates compact entries, while message history stays bounded. For extremely long loops, the host can also bound/replace metadata pages and supply external previous/next page controls; the rail's `onLoadMore` is a single paging callback, not a complete bidirectional index cache. The example does not subscribe to live events or implement adjacent-window pagination. When adding those, buffer live events separately while inspecting remote history, avoid evicting the visible target, and restore/merge the live tail through `onRequestLatest`.

### Implementing the backend adapter

1. **Record human interactions durably.** Cover initial goals, messages, steers, answers, approval/rejection decisions, and commands your application persists. Exclude synthetic model prompts and background agent activity. The frontend only renders the entries you supply.
2. **Give every interaction a stable anchor.** Resolve it within the requested loop. Do not use array indexes, UI row positions, or compare unrelated table IDs. An anchor may internally encode a source and ID, but remains opaque to the component.
3. **Page compact metadata.** Return chronological `id`, `preview`, `kind`, optional `at`, optional `targetItemId`, optional cached `contextPreview`, and a stable next cursor. Cache excerpts rather than loading full responses on hover.
4. **Resolve an anchored range.** Find the interaction's canonical conversation position, return bounded records before/after it in display order, and expose truthful older/newer cursors and flags. Define deterministic ordering across source tables. Do not use timestamps alone when multiple records can share one.
5. **Adapt records into ChatItems.** Run the appropriate host adapter, deduplicate overlapping records, and return the exact generated `ChatItem.id` for the target. If one record expands into several cards, choose the card the user should see. An answer/approval can target its existing question or gate card.
6. **Implement the latest-window read.** Return the current bounded tail and a real item ID from it. Treat missing/deleted interactions, unavailable history, and empty restored windows explicitly; reject the jump when no target can be rendered.
7. **Honor cancellation at the host boundary.** Forward AbortSignal to fetch where available. For an RPC transport without cancellation, check `signal.aborted` after awaiting and before adapting/committing results. The component cannot undo a stale host state update.

These are backend requirements for the adapter, not shipped protocol methods. Existing `loop.transcript` before/after reads cannot by themselves index every human interaction. Scope validation, durable cross-source ordering, transcript adapters, and live/history merging must be provided by the application.

### Integration checks

- Click a loaded marker: no range request; the correct item scrolls into view.
- Click an unloaded marker: only the requested bounded window replaces the current items.
- Rapidly choose markers or switch loops: canceled results never replace newer history.
- Fail a loader or return an absent target: an actionable error appears; no false successful jump.
- Restore latest: the live tail replaces historical items and following resumes.
- Page markers independently: loading metadata does not fetch every message body.
- Exercise goals, steers, answers, approvals, and commands, not only plain messages.
- Check keyboard focus/activation, hover excerpts, both themes, and agent-filtered views.

Use [message-index-demo.html](http://127.0.0.1:5173/message-index-demo.html) and [its fixture loader](../../demos/message-index.tsx) as a runnable component example. The fixture uses 108 records, paged markers, local loaded jumps, bounded remote-style jumps, and Return to latest; it makes no daemon calls.

`MessageIndex` and its `MessageIndexProps` type are also exported as a presentation-only rail with `entries`, `activeId`, `pendingId`, `onJump`, `hasMore`, `loadingMore`, and `onLoadMore`. AgentChat/ChatStream supply scrolling, loading lifecycle, active-marker state, and live-tail coordination.

## Adapters and other exports

| Export | Usage |
| --- | --- |
| `chatItemsFromEvents(events, options?)` | Adapt `ChatEventLike[]` live/replayed events. Each event has `seq`, `kind`, optional `at`, and `payload`. Options: `steerTexts`, `policyDividerTools`, `fallbackAgent`. |
| `chatItemsFromTranscript(messages, options)` | Adapt `TranscriptLikeMessage[]` with required `options.agent`; optional `includePrompt` and `running`. Messages contain a role and typed content blocks. |
| `chatItemsFromLoopTranscriptPage(messages)` | Adapt `LoopTranscriptMessageLike[]` across agents; accepts step/agent/phase/role/time and content or unreadable status. |
| `deriveSession(events)` | Derive `ChatSession`: agent runs, by-step lookup, question-to-step lookup, gates, loop IDs, and loop status. Exports `AgentRun` and `ChatSession` types. |
| `filterForAgent(items, agent)` | Keep the agent's items, steers targeting it, and relevant dividers. |
| `groupItems(items)` | Group consecutive agent items under shared agent headers. |
| `agentColor(id, agents?)` | Resolve configured or deterministic fallback color. |
| `AgentAvatar` | Props: `id`, optional `agents`, and pixel `size` (default 18). |
| `DiffLines` | Render `text` as diff lines, optionally limited by `maxLines`. |
| `useNow(active)` | Current millisecond clock; active mode schedules updates. |
| `NEGATIVE_ANSWER`, `POSITIVE_ANSWER` | Regular expressions used to classify answer wording. |
| `ChatPanels` | Standalone panel surface with `panels`, `picker`, `onPickerDismiss`, `taskList`. Requires a positioned, sized composer host for overlay placement. Prefer AgentChat for automatic available-height measurement and composer integration. |

All public presentation types in [types.ts](./types.ts) are re-exported. The internal `ChatPanelDock` is not exported from the public index.

## Demo and validation

From `aeon/ui`:

```bash
npm run dev -- --host 127.0.0.1
```

Open [chat-panels-demo.html](http://127.0.0.1:5173/chat-panels-demo.html). The demo renders actual components with fixture data. Try `/`, `//review`, `@`, and `$`; Escape and Space dismissal; persistent-panel disclosures; and adding a third panel to expand/stack the deck. Use **Long content** to generate 60 picker results and a long persistent goal body for responsive scrolling checks. No live loop relationship is persisted by the demo.

Open [message-composer-demo.html](http://127.0.0.1:5173/message-composer-demo.html) for a standalone first-message fixture with custom Component/Blueprint selectors, controlled quick starter/focus, attachment APIs, pickers, and Start loop submission. It does not change HomeScreen or create a real loop.

Open [message-index-demo.html](http://127.0.0.1:5173/message-index-demo.html) for a paginated human-interaction index over bounded fixture windows. It starts with 24 of 108 records and 12 of 36 markers. Earlier jumps load up to 20 records on each side; Return to latest restores the 24-record tail.

Run focused checks from `aeon/ui`:

```bash
npm test -- test/agent-chat-index.test.tsx test/message-composer.test.tsx test/agent-chat-panels.test.tsx test/agent-chat-attachments.test.tsx test/agent-chat-component.test.tsx test/agent-chat-adapters.test.ts test/chat-runs.test.tsx
npm run lint
```

Implementation and validation history lives in [T1 Notes](../../../../docs/aeon/UI-chat-panels/tasks/T1.md). Project-wide typecheck failures and unrun native/daemon checks are recorded there separately from component test results.


## Loop interaction index integration

The index implementation must be present before using this API. This checkout does not currently contain `MessageIndex.tsx` or `useMessageIndex.ts`; the contract below describes the index integration developed alongside the message-index demo, not an available backend RPC.

The loop owns the index. Include all durable human interactions: goals, messages, steers, answers, approvals/rejections, and commands. Keep compact index metadata separate from the bounded conversation window.

```ts
interface LoopMessageIndexEntry {
  id: string; // Stable, opaque, loop-scoped interaction anchor.
  targetItemId?: string; // Exact ChatItem.id, when known.
  preview: string;
  at?: string;
  kind?: string;
  contextPreview?: string; // Cached excerpt; hover does not fetch.
}
```

### Component contract

Pass `messageIndex` to `AgentChat` or `ChatStream` once the index implementation is available:

| Property | Purpose |
| --- | --- |
| `scopeKey` | Loop identity; changing it cancels pending navigation. |
| `entries` | Chronological compact interaction metadata. |
| `before`, `after` | Requested surrounding record counts; default 20 each, clamped to 0–100. |
| `hasMore`, `loadingMore`, `onLoadMore` | Independent metadata pagination. |
| `onRequestJump(entry, { before, after, signal })` | Load a missing target window; resolve `{ targetItemId }`. |
| `onRequestLatest({ signal })` | Restore the live tail; resolve `{ targetItemId }`. |
| `onActiveChange(id)` | Observe the active interaction anchor, or null. |

A loaded target scrolls locally without a range request. For an unloaded target, the host replaces its bounded items and then returns the exact rendered target ID. The component waits for the DOM target. Missing targets time out after 10 seconds. New jumps, loop changes, and unmount cancel pending work; the host must also check cancellation before committing state.

### Host implementation

Implement `history.around` and `history.latest` in your application adapter. These are example service names, not shipped protocol methods. Each returns `{ items, targetItemId, beforeCursor, afterCursor, hasOlder, hasNewer }`; keep these together in state so items and pagination cannot disagree.

```tsx
const [conversation, setConversation] = useState(initialWindow);

<AgentChat
  items={conversation.items}
  messageIndex={{
    scopeKey: loopId,
    entries: interactionPage.entries,
    before: 20,
    after: 20,
    hasMore: Boolean(interactionPage.nextCursor),
    loadingMore: loadingIndex,
    onLoadMore: loadNextInteractionPage,
    onRequestJump: async (entry, { before, after, signal }) => {
      const next = await history.around({
        loopId, anchor: entry.id, before, after, signal,
      });
      signal.throwIfAborted();
      setConversation(next);
      return { targetItemId: next.targetItemId };
    },
    onRequestLatest: async ({ signal }) => {
      const next = await history.latest({ loopId, limit: 40, signal });
      signal.throwIfAborted();
      setConversation(next);
      return { targetItemId: next.targetItemId };
    },
  }}
/>
```

Bootstrap the first metadata page and latest window in the screen loader. Key the containing view by `loopId` to reset local state on loop changes. Empty loops render without markers. Metadata pagination should have its own AbortController, loading/error state, and stable cursor; deduplicate overlapping pages by interaction ID. Abort it on unmount. Metadata loading must not fetch every message body.

Schedule the window update before resolving the loader. Do not return an ID absent from the adapted items. If the transport cannot cancel requests, still check the signal after awaiting and before committing. Buffer incoming live events separately while viewing historical windows; restore the tail through `onRequestLatest`. Indexed history suppresses automatic following and automatic older-history loads. Adjacent-window paging and live/history merging remain host responsibilities.

### Backend implementation

1. Persist human interactions with stable loop-scoped anchors. Avoid array indexes or UI positions.
2. Provide a compact paginated metadata read returning entries and a next cursor, in chronological order.
3. Resolve an anchor into a bounded conversation range. Define deterministic ordering across source tables; timestamps alone need a tie breaker.
4. Adapt records into ChatItems and return the exact target ChatItem ID. Answers and approvals may target their existing question/gate card. A record producing multiple cards needs an explicit target choice.
5. Provide a bounded latest-window read with truthful cursors and older/newer flags. Reject missing/deleted targets or unavailable history explicitly.
6. Validate loop scope and propagate cancellation where supported. Existing transcript reads alone do not supply a unified index of every human interaction.

### Verification

Check loaded navigation performs no fetch; unloaded navigation replaces only a bounded window; rapid clicks and loop changes cannot commit stale results; loader failures and absent targets show errors; Return to latest restores following; metadata pagination is independent of message loading. Exercise all human interaction kinds, keyboard activation, hover previews, both themes, and agent-filtered views.

The rail uses compact theme-aware wave markers: the selected marker is widest, adjacent markers are intermediate, and other markers are muted. Navigation does not add a blue border around the message. Agent-filtered views omit the loop-wide index.
