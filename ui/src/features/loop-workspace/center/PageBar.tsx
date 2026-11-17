import { useState, type ReactNode } from "react";
import { AppIcon } from "../../../components/icons";
import { CustomIcon } from "../../../components/kit";
import type { GitStatusView } from "./useGitStatus";

export interface PageBarProps {
  /** "Loop view", or the zoomed agent's name — see `agent`. */
  title: string;
  /** The loop's own name, shown beside the page title; hover shows the full request it came from. */
  loopTitle?: string;
  loopGoal?: string;
  /** When set, the chat is zoomed to this agent and the title is a way back. */
  agent?: string;
  onBackToLoop?: () => void;
  status: ReactNode;
  git: GitStatusView | null;
  /** Clicking the branch and changed-files pill: show what changed. */
  onOpenChanges?: () => void;
  terminalOpen: boolean;
  onToggleTerminal(): void;
  inspectorOpen: boolean;
  onToggleInspector(): void;
  menu?: { label: string; onSelect(): void }[];
}

const BUTTON = "inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1 font-mono text-sm transition-colors";
const IDLE = "border-slate-200 bg-slate-50 text-slate-700 hover:bg-white dark:border-white/10! dark:bg-white/5! dark:text-slate-200! dark:hover:bg-white/10!";
const ACTIVE = "border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-400/40! dark:bg-blue-400/10! dark:text-blue-300!";

function GitPill({ git, onOpen }: { git: GitStatusView; onOpen?: () => void }) {
  if (git.state === "not-a-repo") return <span data-testid="git-pill" className={`${BUTTON} ${IDLE} text-slate-500`}>not a git repository</span>;
  if (git.state === "unavailable") return <span data-testid="git-pill" title={git.reason} className={`${BUTTON} ${IDLE} text-slate-500`}>git unavailable</span>;
  const changed = git.changed?.length ?? 0;
  const parts = [git.detached ? "detached" : git.branch, `${changed} changed`,
    git.ahead ? `↑${git.ahead}` : undefined, git.behind ? `↓${git.behind}` : undefined].filter(Boolean);
  const title = [git.upstream ? `tracking ${git.upstream}` : undefined, onOpen ? "show the changes" : undefined].filter(Boolean).join(" · ") || undefined;
  const body = <><AppIcon name="branch" />{parts.join(" · ")}
    {changed > 0 && <span className="h-1.5 w-1.5 rounded-full bg-amber-500" aria-hidden />}</>;
  return onOpen
    ? <button type="button" data-testid="git-pill" title={title} onClick={onOpen} className={`${BUTTON} ${IDLE}`}>{body}</button>
    : <span data-testid="git-pill" title={title} className={`${BUTTON} ${IDLE}`}>{body}</span>;
}

function Menu({ items }: { items: NonNullable<PageBarProps["menu"]> }) {
  const [open, setOpen] = useState(false);
  return <div className="relative">
    <button type="button" aria-label="More" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)} className={`${BUTTON} ${IDLE} px-1.5`}>
      <CustomIcon icon="DotsVertical" size="sm" />
    </button>
    {open && <div role="menu" className="absolute right-0 top-full z-20 mt-1 min-w-40 rounded-lg border border-slate-200 bg-white p-1 shadow-lg dark:border-white/10! dark:bg-slate-800!">
      {items.map((item) => <button key={item.label} type="button" role="menuitem" onClick={() => { setOpen(false); item.onSelect(); }}
        className="block w-full rounded-md px-3 py-1.5 text-left text-sm text-slate-700 hover:bg-slate-100 dark:text-slate-200! dark:hover:bg-white/10!">{item.label}</button>)}
    </div>}
  </div>;
}

/** The centre's top bar: where you are, what it is doing, git, and the panel toggles. */
export function PageBar({ title, loopTitle, loopGoal, agent, onBackToLoop, status, git, onOpenChanges, terminalOpen, onToggleTerminal, inspectorOpen, onToggleInspector, menu }: PageBarProps) {
  return <div data-testid="page-bar" className="flex min-w-0 items-center gap-3    px-4 py-2">
    <div className="flex min-w-0 items-center gap-2 text-slate-900 dark:text-slate-50!">
      {agent
        ? <><button type="button" aria-label={title} onClick={onBackToLoop} className="font-semibold text-blue-700 hover:underline dark:text-blue-300!">{title}</button>
          <span className="text-slate-400">›</span><span className="truncate font-semibold">{agent}</span></>
        : <span className="shrink-0 font-semibold">{title}</span>}
      {loopTitle && <><span className="text-slate-400">·</span>
        <span data-testid="loop-title" className="min-w-0 truncate font-medium text-slate-700 dark:text-slate-200!" title={loopGoal && loopGoal !== loopTitle ? loopGoal : loopTitle}>{loopTitle}</span></>}
    </div>
    <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-0.5 font-mono text-xs text-emerald-700 dark:border-emerald-400/30! dark:bg-emerald-400/10! dark:text-emerald-300!">
      <span className="h-1.5 w-1.5 rounded-full bg-current" aria-hidden />{status}
    </span>
    <div className="ml-auto flex items-center gap-2">
      {git && <GitPill git={git} onOpen={onOpenChanges} />}
      <button type="button" aria-label="Terminal" aria-pressed={terminalOpen} onClick={onToggleTerminal} className={`${BUTTON} ${terminalOpen ? ACTIVE : IDLE}`}>
        <AppIcon name="bash" />Terminal
      </button>
      {menu && menu.length > 0 && <Menu items={menu} />}
      <button type="button" aria-label="Toggle inspector" aria-pressed={inspectorOpen} onClick={onToggleInspector} className={`${BUTTON} ${inspectorOpen ? ACTIVE : IDLE} px-1.5`}>
        <AppIcon name="panel" size={16} />
      </button>
    </div>
  </div>;
}
