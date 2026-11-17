import { memo, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { AppIcon } from "../icons";
import { agentName } from "./agentStyle";
import { handoffProse } from "./handoffText";
import { WorkingIndicator } from "./WorkingIndicator";
import { TurnMeta } from "./TurnMeta";
import { GateCard, MessageBubble, QuestionCard, RunCard, SteerBubble } from "./cards";
import { ActivityRow, Divider, ExchangeRow, InjectionRow, ThinkingRow, ToolRow } from "./items";
import { AgentAvatar, Chip, formatClock, Prose } from "./parts";
import type { ChatActions, ChatAgent, ChatItem } from "./types";
import { MessageIndex, type LoopMessageIndexProps } from "./MessageIndex";
import { useMessageIndex } from "./useMessageIndex";
import { useStableItems } from "./useStableItems";

const NO_ACTIONS: ChatActions = {};

export interface ChatStreamProps {
  items: readonly ChatItem[];
  agents?: Record<string, ChatAgent>;
  actions?: ChatActions;
  /** Zoom: show only this agent's own conversation (and steers sent to it). */
  agentFilter?: string;
  /** No answering, no withdrawing — a finished loop's history. */
  readOnly?: boolean;
  /** Running tools show a live elapsed timer. Only meaningful when `at` is real time. */
  liveClock?: boolean;
  /** The loop is alive: show the working line at the foot while nothing else on screen shows the work. */
  working?: boolean;
  /** Shown when there is nothing to render. */
  empty?: ReactNode;
  className?: string;
  /**
   * Scrolling near the top, or the manual fallback button, calls this to
   * fetch the window immediately before what's loaded (S42's FIFO history).
   * Omit it and the stream has nothing more to reach for — exactly today's
   * behaviour.
   */
  onLoadOlder?: () => void;
  loadingOlder?: boolean;
  /** False once there is provably nothing earlier to load — hides the trigger instead of offering a dead end. */
  hasOlder?: boolean;
  /** Loop-wide human interaction index; omit for a normal stream. */
  messageIndex?: LoopMessageIndexProps;
}

const hasAgent = (item: ChatItem): item is Extract<ChatItem, { agent: string }> => "agent" in item;

/** What the zoomed agent view keeps. Loop-level gates and other agents' moves are the loop view's business. */
export function filterForAgent(items: readonly ChatItem[], agent: string): ChatItem[] {
  return items.filter((item) => {
    if (hasAgent(item)) return item.agent === agent;
    if (item.kind === "steer") return item.target === agent;
    if (item.kind === "divider") return item.highlights?.includes(agent) || item.text.startsWith(`${agent} `);
    // The catalog offer belongs to the daemon, not to one agent — every
    // zoomed view should be able to see what every step was given (S66).
    // S70 MB6: the exchange belongs to BOTH its agents — a zoomed view of
    // either one shows the rows it authored and the rows it was addressed.
    if (item.kind === "exchange") return item.from === agent || item.to === agent;
    if (item.kind === "injection") return true;
    return false;
  });
}

interface Group { agent?: string; items: ChatItem[] }

/** Consecutive items of one agent share a header; anything loop-level breaks the run. */
export function groupItems(items: readonly ChatItem[]): Group[] {
  const groups: Group[] = [];
  for (const item of items) {
    const agent = hasAgent(item) ? item.agent : undefined;
    const last = groups[groups.length - 1];
    if (agent !== undefined && last?.agent === agent) last.items.push(item);
    else groups.push({ agent, items: [item] });
  }
  return groups;
}

const Item = memo(function Item({ item, actions, agents, readOnly, liveClock }: { item: ChatItem; actions: ChatActions; agents?: Record<string, ChatAgent>; readOnly?: boolean; liveClock?: boolean }) {
  switch (item.kind) {
    case "divider": return <Divider item={item} />;
    case "text": return <div data-testid="agent-chat-text" className="py-1"><Prose text={handoffProse(item.text, item.streaming) ?? item.text} />{item.streaming && <span className="ml-0.5 inline-block h-4 w-1.5 animate-pulse bg-blue-500 align-middle" />}</div>;
    case "thinking": return <ThinkingRow item={item} />;
    case "tool": return <ToolRow item={item} actions={actions} liveClock={liveClock} readOnly={readOnly} />;
    case "question": return <QuestionCard item={item} actions={actions} readOnly={readOnly} />;
    case "gate": return <GateCard item={item} actions={actions} readOnly={readOnly} />;
    case "steer": return <SteerBubble item={item} actions={actions} agents={agents} readOnly={readOnly} />;
    case "activity": return <ActivityRow item={item} />;
    case "injection": return <InjectionRow item={item} />;
    case "message": return <MessageBubble item={item} actions={actions} />;
    case "exchange": return <ExchangeRow item={item} />;
    case "run": return <RunCard item={item} />;
    case "turn": return <TurnMeta item={item} />;
  }
});

function AgentHeader({ id, at, agents }: { id: string; at?: string; agents?: Record<string, ChatAgent> }) {
  const role = agents?.[id]?.role;
  return <div className="mb-1 mt-3 flex items-center gap-2" data-testid="agent-chat-agent-header">
    <AgentAvatar id={id} agents={agents} size={22} view="face" />
    <span className="font-semibold text-slate-900 dark:text-slate-50!">{agentName(id, agents)}</span>
    {at && <><span className="text-slate-400">·</span><span className="font-mono text-xs text-slate-500">{formatClock(at)}</span></>}
    {role && <Chip tone="blue">{role}</Chip>}
  </div>;
}

export function ChatStream({ items: rawItems, agents, actions = NO_ACTIONS, agentFilter, readOnly, liveClock, working, empty, className = "", onLoadOlder, loadingOlder, hasOlder, messageIndex }: ChatStreamProps) {
  const items = useStableItems(rawItems);
  const shown = agentFilter ? filterForAgent(items, agentFilter) : items;
  const groups = groupItems(shown);
  const scroller = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const [showJump, setShowJump] = useState(false);
  // The scroll height captured the instant a load-older starts, so the
  // prepend that lands can be compensated for rather than read as "new
  // content arrived at the bottom" (which would yank the view down to it).
  const olderAnchor = useRef<number | null>(null);
  // User interactions are loop-wide, and are deliberately absent from an agent-only zoom.
  const index = agentFilter ? undefined : messageIndex;
  const navigation = useMessageIndex(index, scroller,
    () => { pinned.current = false; olderAnchor.current = null; setShowJump(true); },
    () => { pinned.current = true; setShowJump(false); });

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
    pinned.current = atBottom && !navigation.historical.current;
    setShowJump(!atBottom || navigation.historical.current);
    navigation.syncVisible();
    if (!navigation.navigating.current && !navigation.historical.current && onLoadOlder && hasOlder && !loadingOlder && el.scrollTop < 120) onLoadOlder();
  };
  useEffect(() => {
    if (!loadingOlder) return;
    const el = scroller.current;
    if (el) olderAnchor.current = el.scrollHeight;
  }, [loadingOlder]);
  // Follow new content only while the reader is already at the bottom —
  // scrolling up to read must not be yanked back by the next delta. A
  // prepend from load-older is the one exception: hold the viewport's
  // visual position steady instead, by the exact height the prepend added.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if (navigation.historical.current) return;
    if (olderAnchor.current !== null) {
      el.scrollTop += el.scrollHeight - olderAnchor.current;
      olderAnchor.current = null;
      return;
    }
    if (pinned.current) el.scrollTop = el.scrollHeight;
  }, [shown]);
  useEffect(() => { pinned.current = true; }, [agentFilter, index?.scopeKey]);
  // The floating panels grew or shrank: keep the bottom in view if it was.
  useEffect(() => {
    const chat = scroller.current?.closest("[data-testid='agent-chat']");
    if (!chat) return;
    const follow = () => { const el = scroller.current; if (el && pinned.current && !navigation.historical.current) el.scrollTop = el.scrollHeight; };
    chat.addEventListener("chat-panels-occupied", follow);
    return () => chat.removeEventListener("chat-panels-occupied", follow);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <div className={`relative min-h-0 ${className}`}>
    <div ref={scroller} onScroll={onScroll} data-testid="agent-chat-stream" className={`h-full overflow-y-auto px-4 ${index ? "pr-12" : ""}`}
      style={{ paddingBottom: "calc(1rem + var(--chat-panels-occupied, 0px))" }}>
      {(loadingOlder || hasOlder) && <div data-testid="agent-chat-load-older" className="flex justify-center py-2">
        {loadingOlder
          ? <span className="text-xs text-slate-500">Loading earlier turns…</span>
          : <button type="button" onClick={onLoadOlder} className="text-xs text-blue-600 underline hover:text-blue-700 dark:text-blue-400!">Load earlier turns</button>}
      </div>}
      {shown.length === 0 ? (empty ?? <p className="py-8 text-center text-sm text-slate-500">Nothing has happened yet.</p>) :
        groups.map((group, gi) => <section key={`${gi}:${group.items[0]?.id}`} className={group.agent ? "border-b border-slate-100 pb-2 last:border-b-0 dark:border-white/5!" : ""}>
          {group.agent && !agentFilter && <AgentHeader id={group.agent} at={group.items[0]?.at} agents={agents} />}
          {/* The wrapper is keyed by item, so the rise-in runs once, on arrival,
              and never again when the item updates in place. */}
          {group.items.map((item) => <div key={item.id} data-chat-item-id={item.id}
            ref={(node) => { if (node) navigation.nodes.current.set(item.id, node); else navigation.nodes.current.delete(item.id); }}
            className={`agent-chat-enter ${navigation.activeItemId === item.id ? "chat-index-target" : ""}`}>
            <Item item={item} actions={actions} agents={agents} readOnly={readOnly} liveClock={liveClock} />
          </div>)}
        </section>)}
      {working && <WorkingIndicator items={shown} />}
    </div>
    {index && <MessageIndex key={index.scopeKey} {...index} activeId={navigation.activeId} pendingId={navigation.pendingId} onJump={(entry) => void navigation.jump(entry)} />}
    {(navigation.pendingId || navigation.restoring) && <div role="status" className="chat-message-index-status">{navigation.restoring ? "Restoring latest history…" : "Loading interaction…"}</div>}
    {navigation.error && <div role="alert" className="chat-message-index-status">{navigation.error}</div>}
    {(showJump || navigation.away) && <button type="button" onClick={() => {
      if (index) { void navigation.goLatest(); return; }
      const el = scroller.current; if (el) { el.scrollTop = el.scrollHeight; pinned.current = true; setShowJump(false); }
    }}
      className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full border border-slate-200 bg-white px-3 py-1 text-xs text-slate-700 shadow-md hover:bg-slate-50 dark:border-white/10! dark:bg-slate-800! dark:text-slate-200!">
      <AppIcon name="arrow-down" size={12} className="mr-1 inline-block align-[-2px]" />{index ? "Return to latest" : "Jump to latest"}
    </button>}
  </div>;
}
