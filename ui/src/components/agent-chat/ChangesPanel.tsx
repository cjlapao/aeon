import { useId, useState } from "react";
import { Panel } from "../kit";
import { AppIcon } from "../icons";

/** One changed file, with its line counts when git could count them. */
export interface ChatChangedFile { path: string; added?: number; removed?: number; binary?: boolean }

export interface ChatChanges {
  files: readonly ChatChangedFile[];
  /** Clicking a file opens it (the host shows its diff beside the chat). */
  onOpenFile?: (path: string) => void;
  /** Shown at the foot as "Review all changes", for hosts that have a changes view. */
  onOpenAll?: () => void;
}

/** How many files show at first, and how many each "Show more" adds. */
export const CHANGES_STEP = 5;

const Counts = ({ added, removed }: { added?: number; removed?: number }) => (added === undefined && removed === undefined ? null :
  <span className="shrink-0 font-mono text-xs"><span className="text-emerald-600 dark:text-emerald-400!">+{added ?? 0}</span>{" "}<span className="text-red-600 dark:text-red-400!">−{removed ?? 0}</span></span>);

/**
 * What the agents changed, as a panel above the to-dos: "Edited N files +A −R", folded until asked. Open, it lists five
 * files and offers five more at a time; when every file shows, the same control folds it back to five.
 */
export function ChangesPanel({ files, onOpenFile, onOpenAll }: ChatChanges) {
  const [expanded, setExpanded] = useState(false);
  const [shown, setShown] = useState(CHANGES_STEP);
  const bodyId = useId();
  const total = files.reduce((n, f) => ({ a: n.a + (f.added ?? 0), r: n.r + (f.removed ?? 0) }), { a: 0, r: 0 });
  const visible = files.slice(0, shown);
  const left = files.length - visible.length;
  return <Panel variant="simple" padding="none" className="chat-panel" aria-label="Changes" data-testid="agent-chat-changes">
    <div className="chat-persistent-heading">
      <button type="button" className="chat-panel-heading chat-panel-disclosure" aria-expanded={expanded} aria-controls={bodyId}
        onClick={() => { setExpanded((v) => !v); setShown(CHANGES_STEP); }}>
        <AppIcon name="edit" /><strong>Edited {files.length} {files.length === 1 ? "file" : "files"}</strong>
        <Counts added={total.a} removed={total.r} />
        <AppIcon className="chat-panel-chevron" name={expanded ? "chevron-expanded" : "chevron-hover"} />
      </button>
    </div>
    <div id={bodyId} hidden={!expanded}>
      <ul className="border-t border-slate-200 dark:border-white/10!" data-testid="agent-chat-changes-list">
        {visible.map((f) => <li key={f.path}>
          <button type="button" onClick={() => onOpenFile?.(f.path)} title={f.path}
            className="flex w-full min-w-0 items-center gap-3 px-4 py-2 text-left hover:bg-slate-50 dark:hover:bg-white/5!">
            <span className="min-w-0 flex-1 truncate text-sm text-slate-700 dark:text-slate-200!">{f.path}</span>
            {f.binary ? <span className="shrink-0 font-mono text-xs text-slate-400">binary</span> : <Counts added={f.added} removed={f.removed} />}
          </button>
        </li>)}
      </ul>
      <div className="flex items-center gap-4 border-t border-slate-200 px-4 py-2 text-sm dark:border-white/10!">
        {left > 0 && <button type="button" data-testid="agent-chat-changes-more" onClick={() => setShown((n) => n + CHANGES_STEP)}
          className="text-blue-700 hover:underline dark:text-blue-300!">Show {Math.min(CHANGES_STEP, left)} more ({left} left)</button>}
        {left === 0 && files.length > CHANGES_STEP && <button type="button" data-testid="agent-chat-changes-less" onClick={() => setShown(CHANGES_STEP)}
          className="text-blue-700 hover:underline dark:text-blue-300!">Collapse</button>}
        {onOpenAll && <button type="button" onClick={onOpenAll} className="ml-auto text-slate-500 hover:underline">Review all changes</button>}
      </div>
    </div>
  </Panel>;
}
