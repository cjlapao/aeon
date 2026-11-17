import { useEffect, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { MINION_DEFAULT_COLOR, MinionAvatar, minionNameFor, type MinionMood } from "../minion-avatar";
import { agentColor, agentInitial } from "./agentStyle";
import type { ChatAgent } from "./types";

const MINION_MOODS: readonly MinionMood[] = ["idle", "hi", "thinking", "working", "stuck", "done"];

export function AgentAvatar({ id, agents, size = 18, view = "full" }: { id: string; agents?: Record<string, ChatAgent>; size?: number; view?: "full" | "face" }) {
  const avatar = agents?.[id]?.avatar;
  if (avatar?.type === "minion") {
    const mood = (MINION_MOODS as readonly string[]).includes(avatar.mood ?? "") ? (avatar.mood as MinionMood) : "idle";
    return <MinionAvatar name={minionNameFor(id)} color={avatar.color || MINION_DEFAULT_COLOR} mood={mood} size={size} view={view} decorative />;
  }
  return <span aria-hidden className="inline-flex shrink-0 items-center justify-center rounded font-semibold text-white"
    style={{ background: avatar?.color || agentColor(id, agents), width: size, height: size, fontSize: Math.round(size * 0.55) }}>
    {agentInitial(id, agents)}
  </span>;
}

export function formatClock(at?: string): string {
  if (!at) return "";
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function formatDuration(ms?: number): string {
  if (ms === undefined) return "";
  if (ms < 1000) return `${(ms / 1000).toFixed(1)}s`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return m < 60 ? `${m}m ${s % 60}s` : `${Math.floor(m / 60)}h ${m % 60}m`;
}

/** Re-renders every second while mounted — for "working for 12m 12s". */
export function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);
  return now;
}

/**
 * Animated expand/collapse. The body mounts on first open (so a collapsed
 * item costs nothing and is not in the accessibility tree), then stays mounted
 * so closing can animate.
 */
export function Expand({ open, children, className = "" }: { open: boolean; children: ReactNode; className?: string }) {
  const [mounted, setMounted] = useState(open);
  // Opened from closed: mount at 0fr, flip on the next frame, or the very
  // first open would jump instead of sliding.
  const [shown, setShown] = useState(open);
  useEffect(() => {
    if (!open) { setShown(false); return; }
    setMounted(true);
    const frame = requestAnimationFrame(() => setShown(true));
    return () => cancelAnimationFrame(frame);
  }, [open]);
  if (!mounted) return null;
  return <div className={`agent-chat-expand ${className}`} data-open={shown} aria-hidden={!open}>
    <div>{children}</div>
  </div>;
}

export function Spinner({ className = "" }: { className?: string }) {
  return <span role="status" aria-label="running"
    className={`inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-r-transparent ${className}`} />;
}

export function Mono({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <span className={`font-mono ${className}`}>{children}</span>;
}

export function Chip({ children, tone = "slate", className = "" }: { children: ReactNode; tone?: "slate" | "green" | "amber" | "red" | "blue" | "violet"; className?: string }) {
  const tones = {
    slate: "border-slate-200 bg-slate-50 text-slate-600 dark:border-white/10! dark:bg-white/5! dark:text-slate-300!",
    green: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/30! dark:bg-emerald-400/10! dark:text-emerald-300!",
    amber: "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-400/30! dark:bg-amber-400/10! dark:text-amber-300!",
    red: "border-red-200 bg-red-50 text-red-700 dark:border-red-400/30! dark:bg-red-400/10! dark:text-red-300!",
    blue: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-400/30! dark:bg-blue-400/10! dark:text-blue-300!",
    violet: "border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-400/30! dark:bg-violet-400/10! dark:text-violet-300!",
  } as const;
  return <span className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-px font-mono text-xs ${tones[tone]} ${className}`}>{children}</span>;
}

/** Assistant prose: markdown, with inline code as the chip the design uses. */
export function Prose({ text }: { text: string }) {
  return <div className="agent-chat-prose min-w-0 break-words text-[15px] leading-relaxed text-slate-900 dark:text-slate-100!">
    <ReactMarkdown remarkPlugins={[remarkGfm]} components={{
      p: ({ children }) => <p className="my-1">{children}</p>,
      // No `pre` override: react-markdown's default wraps a fenced block in
      // an unstyled `<pre>` with native `white-space: pre` and no width of
      // its own — content like an unbroken JSON line stretches it (and the
      // page) wider than the viewport before `overflow-x-auto` on our own
      // `<code>` ever gets a bounded box to scroll within. Dropping it in
      // favour of the one block-level, `max-w-full` element below is what
      // actually keeps a long line inside the chat instead of past it.
      pre: ({ children }) => <>{children}</>,
      code: ({ children, className }) => className
        ? <code className={`block max-w-full overflow-x-auto rounded-lg bg-slate-900 p-3 font-mono text-xs text-slate-100 ${className}`}>{children}</code>
        : <code className="rounded-md bg-blue-50 px-1.5 py-0.5 font-mono text-[0.85em] text-blue-700 dark:bg-blue-400/10! dark:text-blue-300!">{children}</code>,
      a: ({ children, href }) => <a href={href} className="text-blue-700 underline dark:text-blue-300!" target="_blank" rel="noreferrer">{children}</a>,
      ul: ({ children }) => <ul className="my-1 list-disc pl-5">{children}</ul>,
      ol: ({ children }) => <ol className="my-1 list-decimal pl-5">{children}</ol>,
    }}>{text}</ReactMarkdown>
  </div>;
}

/** A diff (or plain lines) with +/− lines coloured. */
export function DiffLines({ text, maxLines }: { text: string; maxLines?: number }) {
  const all = text.replace(/\n$/, "").split("\n");
  const lines = maxLines ? all.slice(0, maxLines) : all;
  return <pre data-testid="agent-chat-diff" className="max-w-full overflow-x-auto rounded-lg border border-slate-200 bg-white p-3 font-mono text-xs leading-5 dark:border-white/10! dark:bg-black/30!">
    {lines.map((line, i) => {
      const tone = line.startsWith("+++") || line.startsWith("---") ? "text-slate-500" :
        line.startsWith("+") ? "text-emerald-700 dark:text-emerald-300!" :
        line.startsWith("-") ? "text-red-700 dark:text-red-300!" :
        line.startsWith("@@") ? "text-blue-700 dark:text-blue-300!" : "text-slate-600 dark:text-slate-300!";
      return <div key={i} className={tone}>{line || " "}</div>;
    })}
    {maxLines && all.length > maxLines && <div className="text-slate-400">… {all.length - maxLines} more lines</div>}
  </pre>;
}
