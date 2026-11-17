import { useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Panel, ScrollArea, StackCarousel } from "../kit";
import { AppIcon } from "../icons";
import { ChangesPanel, type ChatChanges } from "./ChangesPanel";
import "./chatPanels.css";

export interface ChatPanelItem {
  id: string;
  label: string;
  description?: string;
  icon?: ReactNode;
  disabled?: boolean;
  onSelect?: () => void;
}

export interface ChatPanel {
  id: string;
  title: string;
  description?: string;
  icon?: ReactNode;
  items: readonly ChatPanelItem[];
  emptyText?: string;
  onClose?: () => void;
}

export interface ChatTask {
  id: string;
  label: string;
  agent?: string;
  status: "pending" | "in_progress" | "completed" | "failed";
}

export interface ChatTaskList {
  title?: string;
  tasks: readonly ChatTask[];
  /** A line beside the counts, e.g. "closed · all-done" (S75: a closed list rests, it does not vanish). */
  note?: string;
  /** Host controls in the heading (S75: the person's Close list). */
  actions?: ReactNode;
}

export interface ChatPersistentPanel {
  id: string;
  title: string;
  summary?: string;
  icon?: ReactNode;
  content: ReactNode;
  actions?: ReactNode;
  defaultExpanded?: boolean;
}

export interface ChatPanelsProps {
  /** Persistent context panels; three or more use an expandable stack. */
  panels?: readonly ChatPersistentPanel[];
  /** One temporary picker, overlaid above the persistent panels. */
  picker?: ChatPanel;
  onPickerDismiss?: () => void;
  taskList?: ChatTaskList;
  /** What the agents changed; shown above the to-dos when given. The host decides when to give it. */
  changes?: ChatChanges;
}

const statusLabels = { pending: "Pending", in_progress: "In progress", completed: "Completed", failed: "Failed" };
const statusMarks = { pending: "•", in_progress: "◌", completed: "✓", failed: "!" };

/** Bound overlays to the space between the chat top and the composer. */
export function ChatPanelDock({ children, layout = "chat", ...props }: ChatPanelsProps & { children: ReactNode; layout?: "chat" | "standalone" }) {
  const ref = useRef<HTMLDivElement>(null);
  const [available, setAvailable] = useState<number>();
  const [taskHeight, setTaskHeight] = useState<number>();
  useLayoutEffect(() => {
    const dock = ref.current;
    const chat = dock?.parentElement;
    if (!dock || !chat) return;
    const measure = () => {
      const viewport = window.visualViewport;
      const top = Math.max(layout === "chat" ? chat.getBoundingClientRect().top : 0, viewport?.offsetTop ?? 0);
      const bottom = Math.min(dock.getBoundingClientRect().top,
        (viewport?.offsetTop ?? 0) + (viewport?.height ?? window.innerHeight));
      // Six standard task rows plus list padding, bounded on short screens.
      setTaskHeight(Math.max(0, Math.min(222,
        (viewport?.height ?? window.innerHeight) * 0.4, bottom - top - 64)));
      // Reserve most of the visible stream for messages, even in a short host.
      setAvailable(Math.max(0, Math.min(
        280,
        layout === "chat" ? chat.clientHeight * 0.35 : Infinity,
        (viewport?.height ?? window.innerHeight) * 0.30,
        (bottom - top - 16) * (layout === "chat" ? 0.45 : 1),
      )));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(chat);
    observer.observe(dock);
    window.addEventListener("resize", measure);
    window.addEventListener("scroll", measure, true);
    window.visualViewport?.addEventListener("resize", measure);
    window.visualViewport?.addEventListener("scroll", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
      window.removeEventListener("scroll", measure, true);
      window.visualViewport?.removeEventListener("resize", measure);
      window.visualViewport?.removeEventListener("scroll", measure);
    };
  }, [layout]);
  // The panels float above the composer, over the stream. Tell the stream how much
  // of its bottom they cover (`--chat-panels-occupied` on the chat root), so it
  // pads by that much and the last messages stay readable — open or collapsed.
  useLayoutEffect(() => {
    const dock = ref.current;
    const chat = dock?.parentElement;
    if (!dock || !chat) return;
    let watched: Element | null = null;
    const publish = () => {
      const panels = dock.querySelector(".chat-panels");
      const covered = panels ? Math.ceil(panels.getBoundingClientRect().height) + 8 : 0;
      const value = panels && covered > 8 ? `${covered}px` : "0px";
      if (chat.style.getPropertyValue("--chat-panels-occupied") === value) return;
      chat.style.setProperty("--chat-panels-occupied", value);
      chat.dispatchEvent(new CustomEvent("chat-panels-occupied"));
    };
    const resize = new ResizeObserver(publish);
    const watch = () => {
      const panels = dock.querySelector(".chat-panels");
      if (panels !== watched) {
        if (watched) resize.unobserve(watched);
        watched = panels;
        if (panels) resize.observe(panels);
      }
      publish();
    };
    const mutations = new MutationObserver(watch);
    mutations.observe(dock, { childList: true });
    watch();
    return () => { mutations.disconnect(); resize.disconnect(); chat.style.removeProperty("--chat-panels-occupied"); };
  }, []);
  return <div ref={ref} className="chat-composer-anchor" style={available === undefined ? undefined : {
    "--chat-panels-height": `${available}px`,
    "--chat-task-height": `${taskHeight ?? 222}px`,
  } as CSSProperties} onKeyDown={(event) => {
    if (event.key === "Escape" && !event.nativeEvent.isComposing && props.picker) {
      props.onPickerDismiss?.();
    }
  }}><ChatPanels {...props} />{children}</div>;
}

/** The list matters only while something is still to do. */
export const hasOpenTasks = (tasks: readonly { status: string }[]): boolean => tasks.some((task) => task.status === "pending" || task.status === "in_progress");

/**
 * The to-do bar is there while something is still to do, and gone once everything is done (or there is nothing): a wall of
 * ticked items is not worth the room. New to-dos bring it back, and it always comes back folded; opening it is the
 * person's choice. It is mounted only while it shows, so each return starts folded again.
 */
function TaskPanel(props: ChatTaskList) {
  return hasOpenTasks(props.tasks) ? <TaskPanelBody {...props} /> : null;
}

function TaskPanelBody({ title = "To-dos", tasks, note, actions }: ChatTaskList) {
  const [expanded, setExpanded] = useState(false);
  const bodyId = useId();
  const running = tasks.filter((task) => task.status === "in_progress").length;
  const pending = tasks.filter((task) => task.status === "pending").length;
  return <Panel variant="simple" padding="none" className="chat-panel" aria-label={title}>
    <div className="chat-persistent-heading">
      <button type="button" className="chat-panel-heading chat-panel-disclosure" aria-expanded={expanded} aria-controls={bodyId}
        onClick={() => setExpanded((value) => !value)}>
        <AppIcon name="panel" /><strong>{title}</strong>
        <span className="chat-panel-muted">{running} in progress · {pending} pending</span>
        {note && <span className="chat-panel-muted" data-testid="chat-task-note">{note}</span>}
        <AppIcon className="chat-panel-chevron" name={expanded ? "chevron-expanded" : "chevron-hover"} />
      </button>
      {actions && <div className="chat-persistent-actions">{actions}</div>}
    </div>
    <div id={bodyId} hidden={!expanded}><ScrollArea className="chat-panel-body chat-task-scroll" horizontal={false} viewportClassName="chat-panel-viewport" aria-label={`${title} content`}>
      {tasks.length === 0 ? <p className="chat-panel-empty">No tasks yet.</p> : <ul className="chat-panel-tasks">
        {tasks.map((task) => <li key={task.id} data-status={task.status}>
          <span className="chat-task-mark" aria-label={statusLabels[task.status]}>
            {task.status === "in_progress" ? <span className="chat-task-spinner" data-testid="chat-task-spinner" aria-hidden="true" /> : statusMarks[task.status]}
          </span>
          <span className="chat-task-label">{task.label}</span>
          {task.agent && <span className="chat-panel-muted chat-task-agent">{task.agent}</span>}
        </li>)}
      </ul>}
    </ScrollArea></div>
  </Panel>;
}

function ListPanel({ panel }: { panel: ChatPanel }) {
  return <Panel variant="simple" padding="none" className="chat-panel chat-panel-list" aria-label={panel.title}>
    <header className="chat-panel-heading">
      {panel.icon && <span aria-hidden="true">{panel.icon}</span>}<strong>{panel.title}</strong>
      {panel.onClose && <button type="button" className="chat-panel-close" aria-label={`Close ${panel.title}`} onClick={panel.onClose}><AppIcon name="cross" /></button>}
    </header>
    {panel.description && <p className="chat-panel-description chat-panel-muted">{panel.description}</p>}
    <ScrollArea className="chat-panel-body" horizontal={false} viewportClassName="chat-panel-viewport" aria-label={`${panel.title} content`}>
      {panel.items.length === 0 ? <p className="chat-panel-empty">{panel.emptyText ?? "No items to display."}</p> :
        <ul className="chat-panel-items">{panel.items.map((item) => <li key={item.id}>
          {item.onSelect ? <button type="button" className="chat-panel-row" disabled={item.disabled} onClick={item.onSelect}>
            <PanelItemContent item={item} />
          </button> : <div className="chat-panel-row" aria-disabled={item.disabled || undefined}><PanelItemContent item={item} /></div>}
        </li>)}</ul>}
    </ScrollArea>
  </Panel>;
}

function PanelItemContent({ item }: { item: ChatPanelItem }) {
  return <>{item.icon && <span aria-hidden="true">{item.icon}</span>}<span className="chat-panel-item-text">
    <span>{item.label}</span>{item.description && <span className="chat-panel-muted">{item.description}</span>}
  </span></>;
}

function PersistentPanel({ panel }: { panel: ChatPersistentPanel }) {
  const [expanded, setExpanded] = useState(panel.defaultExpanded ?? false);
  const bodyId = useId();
  return <Panel variant="simple" padding="none" className="chat-panel chat-persistent-panel" aria-label={panel.title}>
    <div className="chat-persistent-heading">
      <button type="button" className="chat-panel-heading chat-panel-disclosure" aria-expanded={expanded}
        aria-controls={bodyId} onClick={() => setExpanded((value) => !value)}>
        {panel.icon && <span aria-hidden="true">{panel.icon}</span>}
        <strong>{panel.title}</strong>
        {panel.summary && <span className="chat-persistent-summary chat-panel-muted">{panel.summary}</span>}
        <AppIcon className="chat-panel-chevron" name={expanded ? "chevron-expanded" : "chevron-hover"} />
      </button>
      {panel.actions && <div className="chat-persistent-actions">{panel.actions}</div>}
    </div>
    <div id={bodyId} hidden={!expanded}><ScrollArea className="chat-panel-body" horizontal={false}
      viewportClassName="chat-panel-viewport" contentClassName="chat-persistent-content" aria-label={`${panel.title} content`}>{panel.content}</ScrollArea></div>
  </Panel>;
}

/** One temporary picker overlays a persistent panel deck and the single task slot. */
export function ChatPanels({ panels = [], taskList, changes, picker, onPickerDismiss }: ChatPanelsProps) {
  const showTasks = taskList !== undefined && hasOpenTasks(taskList.tasks);
  if (!showTasks && !changes && panels.length === 0 && !picker) return null;
  return <>
    {(taskList || changes || panels.length > 0) && <div className="chat-panels chat-panel-stack" data-testid="agent-chat-panels">
      {changes && <ChangesPanel {...changes} />}
      {showTasks && taskList && <TaskPanel {...taskList} />}
      {panels.length > 2 ? <div className="chat-panel-deck"><StackCarousel items={panels} getItemKey={(panel) => panel.id}
        getItemLabel={(panel) => panel.title} renderItem={(panel) => <PersistentPanel key={panel.id} panel={panel} />}
        ariaLabel="Chat panels" padding="none" wheel={false} swipe={false} expandable showExpandButton
        expandLabel="Expand panels" collapseLabel="Stack panels" /> </div> :
        panels.map((panel) => <PersistentPanel key={panel.id} panel={panel} />)}
    </div>}
    {picker && <div className="chat-panels chat-picker" data-testid="agent-chat-picker" onKeyDown={(event) => {
      if (event.key === "Escape" && !event.nativeEvent.isComposing) {
        event.stopPropagation();
        (onPickerDismiss ?? picker.onClose)?.();
      }
    }}><ListPanel key={picker.id} panel={{ ...picker, onClose: onPickerDismiss ?? picker.onClose }} /></div>}
  </>;
}
