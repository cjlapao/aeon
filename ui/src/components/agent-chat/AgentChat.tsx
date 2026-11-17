import { useRef, type ReactNode } from "react";
import { ChatComposer, type ChatComposerProps } from "./ChatComposer";
import { ChatStream } from "./ChatStream";
import type { ChatPanelsProps } from "./ChatPanels";
import type { ChatActions, ChatAgent, ChatItem } from "./types";
import "./agentChat.css";
import type { LoopMessageIndexProps } from "./MessageIndex";

/**
 * The agent chat: the stream, and — while something can still be steered — the
 * composer under it. A finished loop passes no `composer` (or `readOnly`) and
 * gets history only, which is the design's rule: once the loop is done the
 * message box goes away and everything stays browsable.
 *
 * It owns no data and calls no daemon. The host adapts its source into
 * `items` (`chatItemsFromEvents`, `chatItemsFromTranscript`) and wires the
 * callbacks to RPCs — which is what makes it reusable in the loop workspace,
 * the step screen and the bench alike.
 *
 * # Why every `dark:` class in this folder ends in `!`
 *
 * The kit's prebuilt stylesheet loads AFTER Aeon's Tailwind output (main.tsx)
 * and shares its utilities layer, so wherever the kit also ships a light class
 * (`bg-red-50`, `bg-white`…) it beats our `dark:` twin at equal specificity and
 * the dark theme silently keeps light surfaces. The important modifier is the
 * smallest fix that survives the kit adding more utilities later.
 */
export interface AgentChatProps extends ChatPanelsProps {
  items: readonly ChatItem[];
  agents?: Record<string, ChatAgent>;
  actions?: ChatActions;
  agentFilter?: string;
  readOnly?: boolean;
  liveClock?: boolean;
  working?: boolean;
  composer?: Omit<ChatComposerProps, "agents">;
  header?: ReactNode;
  empty?: ReactNode;
  /** Fixed height for the scroll area, e.g. "70vh". Default fills the parent. */
  height?: string;
  className?: string;
  /** See `ChatStreamProps` — passed straight through. */
  onLoadOlder?: () => void;
  loadingOlder?: boolean;
  hasOlder?: boolean;
  messageIndex?: LoopMessageIndexProps;
}

export function AgentChat({ items, agents, actions, agentFilter, readOnly, liveClock, working, composer, panels, taskList, changes, picker, onPickerDismiss, header, empty, height, className = "", onLoadOlder, loadingOlder, hasOlder, messageIndex }: AgentChatProps) {
  const chatRef = useRef<HTMLDivElement>(null);
  return <div ref={chatRef} data-testid="agent-chat" className={`flex min-h-0 flex-col overflow-hidden bg-transparent  ${className}`}
    style={height ? { height } : undefined}>
    {header && <div className="aeon-accent-outline border-b px-4 py-2">{header}</div>}
    <ChatStream className="flex-1" items={items} agents={agents} actions={actions} agentFilter={agentFilter} readOnly={readOnly} liveClock={liveClock} working={working} empty={empty}
      onLoadOlder={onLoadOlder} loadingOlder={loadingOlder} hasOlder={hasOlder} messageIndex={messageIndex} />
    {composer && !readOnly && <ChatComposer {...composer} panels={panels ?? composer.panels} taskList={taskList ?? composer.taskList} changes={changes ?? composer.changes}
      picker={picker ?? composer.picker} onPickerDismiss={onPickerDismiss ?? composer.onPickerDismiss}
      dropTargetRef={composer.dropTargetRef ?? chatRef} agents={agents} fixedTarget={composer.fixedTarget ?? agentFilter} />}
  </div>;
}
