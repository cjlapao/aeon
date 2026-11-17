import { Link } from "react-router-dom";
import { originLabel, originPath } from "../../lib/origin";
import { CustomIcon } from "../../components/kit";
import type { LiveState } from "../../lib/live";

const ENGINE: Record<LiveState["state"], { label: string; tone: string }> = {
  current: { label: "ENGINE READY", tone: "border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-400/40! dark:bg-emerald-400/10! dark:text-emerald-300!" },
  connecting: { label: "CONNECTING", tone: "border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-400/40! dark:bg-amber-400/10! dark:text-amber-300!" },
  stale: { label: "RECONNECTING", tone: "border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-400/40! dark:bg-amber-400/10! dark:text-amber-300!" },
  disconnected: { label: "DISCONNECTED", tone: "border-red-300 bg-red-50 text-red-700 dark:border-red-400/40! dark:bg-red-400/10! dark:text-red-300!" },
};

/** `Loop view / two-step / aeon-qa-workspace`, with a way back to the page the loop was opened from (Home, the project, …). */
export function HeaderBreadcrumb({ blueprint, root }: { blueprint?: string; root?: string }) {
  const folder = root?.replace(/\/+$/, "").split("/").pop();
  const origin = originPath();
  const back = origin ?? "/work";
  const label = `Back to ${originLabel(origin)}`;
  return <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-2 text-sm">
    <Link to={back} aria-label={label} title={label} data-testid="loop-back" className="flex items-center gap-1 rounded-md p-1 text-slate-500 hover:bg-slate-200/70 dark:hover:bg-white/10!"><CustomIcon icon="ChevronLeft" size="sm" /><span className="hidden sm:inline">{label}</span></Link>
    <span className="font-mono text-slate-500 dark:text-slate-400!">Loop view</span>
    {blueprint && <><span className="text-slate-300">/</span><span className="truncate rounded-md bg-slate-100 px-2 py-0.5 font-mono dark:bg-white/10!">{blueprint}</span></>}
    {folder && <><span className="text-slate-300">/</span><span className="truncate rounded-md bg-slate-100 px-2 py-0.5 font-mono dark:bg-white/10!">{folder}</span></>}
  </nav>;
}

/**
 * The engine state and the page nav. Telemetry and Agents are named in the
 * design but are not pages yet, so they are drawn and disabled rather than
 * linked to nothing.
 */
export function HeaderNav({ loopId, live }: { loopId: string; live: LiveState }) {
  const engine = ENGINE[live.state];
  const tab = "rounded-lg px-3 py-1 text-sm font-medium transition-colors";
  return <div className="flex min-w-0 items-center gap-3">
    <span className={`rounded-full border px-3 py-0.5 font-mono text-xs font-semibold ${engine.tone}`}>{engine.label}</span>
    <nav aria-label="Loop sections" className="flex items-center gap-1">
      <Link to={`/loops/${loopId}`} aria-current="page" className={`${tab} bg-blue-600 text-white`}>Loop</Link>
      {["Telemetry", "Agents"].map((name) => <button key={name} type="button" disabled title="Coming next"
        className={`${tab} cursor-not-allowed text-slate-400 dark:text-slate-500!`}>{name}</button>)}
    </nav>
  </div>;
}
