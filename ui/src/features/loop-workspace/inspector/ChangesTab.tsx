import { useEffect, useRef, useState } from "react";
import { DiffView } from "../../../components/DiffView";
import { AppIcon } from "../../../components/icons";
import type { GitChangesView } from "../center/useGitChanges";
import { useFilePreview } from "./useFilePreview";

const COUNT = (n?: number) => (n === undefined ? "" : String(n));

function FileDiff({ componentRoot, path, split }: { componentRoot: string; path: string; split: boolean }) {
  const { loading, diff, preview, error } = useFilePreview(componentRoot, path);
  if (loading) return <p className="px-3 py-2 text-xs text-slate-500">Opening…</p>;
  if (error) return <p role="alert" className="px-3 py-2 text-xs text-red-700 dark:text-red-300!">{error}</p>;
  if (diff) return <div className="px-2 pb-2"><DiffView diff={diff} path={path} split={split} /></div>;
  return <p className="px-3 py-2 text-xs text-slate-500">{preview?.reason || "There is no text diff for this file."}</p>;
}

export interface ChangesTabProps {
  componentRoot: string;
  changes: GitChangesView | null;
  /** A file to open when the tab is shown (the chat panel or the header sent the person here). */
  focusPath?: string;
}

/**
 * Everything that changed in the project, file by file: the counts, and each file's diff opening in place. One
 * column by default (the panel is narrow); "Side by side" splits old and new.
 */
export function ChangesTab({ componentRoot, changes, focusPath }: ChangesTabProps) {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [split, setSplit] = useState(false);
  const rowRefs = useRef(new Map<string, HTMLElement>());
  useEffect(() => {
    if (!focusPath) return;
    setOpen((cur) => new Set(cur).add(focusPath));
    rowRefs.current.get(focusPath)?.scrollIntoView?.({ block: "nearest" });
  }, [focusPath]);
  if (!changes) return <p className="p-4 text-sm text-slate-500">Reading the changes…</p>;
  if (changes.state === "not-a-repo") return <p className="p-4 text-sm text-slate-500" data-testid="changes-none">This project is not a git repository, so there is no change list.</p>;
  if (changes.state === "unavailable") return <p className="p-4 text-sm text-slate-500" data-testid="changes-none">Git could not tell what changed: {changes.reason}</p>;
  const files = changes.files ?? [];
  if (files.length === 0) return <p className="p-4 text-sm text-slate-500" data-testid="changes-none">No uncommitted changes.</p>;
  const toggle = (path: string) => setOpen((cur) => { const next = new Set(cur); if (next.has(path)) next.delete(path); else next.add(path); return next; });
  return <div data-testid="changes-tab">
    <div className="flex items-center gap-3 border-b border-slate-200 px-4 py-2 dark:border-white/10!">
      <span className="text-sm font-medium text-slate-900 dark:text-slate-100!">{files.length} {files.length === 1 ? "file" : "files"} changed</span>
      <span className="font-mono text-xs"><span className="text-emerald-600 dark:text-emerald-400!">+{changes.totalAdded}</span>{" "}<span className="text-red-600 dark:text-red-400!">−{changes.totalRemoved}</span></span>
      <button type="button" aria-pressed={split} onClick={() => setSplit((v) => !v)} data-testid="changes-split"
        className="ml-auto rounded-md border border-slate-200 px-2 py-0.5 text-xs text-slate-600 hover:bg-slate-50 dark:border-white/10! dark:text-slate-300! dark:hover:bg-white/5!">{split ? "Unified" : "Side by side"}</button>
    </div>
    <ul>
      {files.map((f) => <li key={f.path} ref={(node) => { if (node) rowRefs.current.set(f.path, node); else rowRefs.current.delete(f.path); }}
        className="border-b border-slate-100 dark:border-white/5!">
        <button type="button" aria-expanded={open.has(f.path)} onClick={() => toggle(f.path)}
          className="flex w-full min-w-0 items-center gap-2 px-4 py-2 text-left hover:bg-slate-50 dark:hover:bg-white/5!">
          <AppIcon name={open.has(f.path) ? "chevron-expanded" : "chevron-hover"} size={12} className="shrink-0 text-slate-400" />
          <span className="min-w-0 flex-1 truncate text-sm text-slate-800 dark:text-slate-100!" title={f.path}>{f.path}</span>
          <span className="shrink-0 rounded bg-slate-100 px-1.5 font-mono text-[11px] text-slate-500 dark:bg-white/10! dark:text-slate-300!">{f.status}</span>
          {f.binary ? <span className="shrink-0 font-mono text-xs text-slate-400">binary</span>
            : <span className="shrink-0 font-mono text-xs"><span className="text-emerald-600 dark:text-emerald-400!">+{COUNT(f.added)}</span>{" "}<span className="text-red-600 dark:text-red-400!">−{COUNT(f.removed)}</span></span>}
        </button>
        {open.has(f.path) && <FileDiff componentRoot={componentRoot} path={f.path} split={split} />}
      </li>)}
    </ul>
  </div>;
}
