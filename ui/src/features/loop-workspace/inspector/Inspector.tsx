import type { ReactNode } from "react";
import { AppIcon } from "../../../components/icons";
import { SidePanel, THIN_SCROLLBAR } from "../../../components/kit";
import { useAppSettings } from "../../../lib/appSettings";

export interface OpenFile { path: string; /** Lines added, shown on the tab. */ added?: number }

export interface InspectorProps {
  /** "step", "files", or `file:<path>`. */
  tab: string;
  onTabChange(tab: string): void;
  openFiles: readonly OpenFile[];
  onCloseFile(path: string): void;
  step: ReactNode;
  files: ReactNode;
  /** The loop's tasks; the tab is offered only when this is given. */
  tasks?: ReactNode;
  /** What changed in the project; the tab is offered when this is given, with its file count. */
  changes?: ReactNode;
  changesCount?: number;
  /** The loop's internal files (documents the agents filed); the tab is offered when this is given. */
  documents?: ReactNode;
  file(path: string): ReactNode;
  /** Whether the panel is expanded. @default true */
  open?: boolean;
  /** Gives the panel its collapse strip; the PageBar's button drives the same state. */
  onOpenChange?(open: boolean): void;
  /** Starting width in px — the docked panel resizes itself and reports no width callback. */
  width?: number;
}

const base = (path: string) => path.split("/").pop() || path;

const TAB = "inline-flex items-center gap-1.5 whitespace-nowrap border-b-2 px-2.5 py-2 text-sm font-medium transition-colors";
const TAB_ON = "border-blue-600 text-blue-700 dark:text-blue-300!";
const TAB_OFF = "border-transparent text-slate-500 hover:text-slate-800 dark:text-slate-400! dark:hover:text-slate-100!";

/**
 * The inspector: a docked `SidePanel` — the kit's panel surface, header and
 * fill — holding the tab strip and the active tab's content. Since kit
 * 0.2.14 `PageBodyLayout` is zones only, and this is the component the docs
 * name for exactly this job: "the panel keeps its own size, collapse
 * animation and resize handle; the space around it follows". So the panel
 * itself is resizable (280–560px on its own edge), and with `onOpenChange`
 * it is collapsible too — its rail strip and the PageBar's button drive the
 * one `open` state.
 *
 * The body renders only while expanded: a collapsed Files tab would
 * otherwise go on listing the workspace behind a closed panel.
 */
export function Inspector({ tab, onTabChange, openFiles, onCloseFile, step, files, tasks, changes, changesCount, documents, file, open = true, onOpenChange, width }: InspectorProps) {
  const { surfaceTone } = useAppSettings();
  const filePath = tab.startsWith("file:") ? tab.slice("file:".length) : undefined;
  return <SidePanel docked isOpen title="Inspector" side="right" variant="simple" surfaceTone={surfaceTone} corner="rounded-sm"
    width={width} resizable minWidth={280} maxWidth={560}
    collapsible={onOpenChange !== undefined} collapsed={!open}
    onCollapsedChange={onOpenChange ? (collapsed) => onOpenChange(!collapsed) : undefined}>
    {open ? <div data-testid="inspector-column" className="flex h-full min-h-0 flex-col">
      <div role="tablist" aria-label="Inspector tabs" className="flex shrink-0 overflow-x-auto border-b border-slate-200 px-1 dark:border-white/10!">
        <button type="button" role="tab" aria-selected={tab === "step"} onClick={() => onTabChange("step")} className={`${TAB} ${tab === "step" ? TAB_ON : TAB_OFF}`}>Step</button>
        <button type="button" role="tab" aria-selected={tab === "files"} onClick={() => onTabChange("files")} className={`${TAB} ${tab === "files" ? TAB_ON : TAB_OFF}`}>Files</button>
        {changes !== undefined && <button type="button" role="tab" aria-selected={tab === "changes"} onClick={() => onTabChange("changes")} className={`${TAB} ${tab === "changes" ? TAB_ON : TAB_OFF}`}>Changes{changesCount ? <span className="rounded bg-slate-100 px-1 font-mono text-[11px] text-slate-600 dark:bg-white/10! dark:text-slate-300!">{changesCount}</span> : null}</button>}
        {documents !== undefined && <button type="button" role="tab" aria-selected={tab === "documents"} onClick={() => onTabChange("documents")} className={`${TAB} ${tab === "documents" ? TAB_ON : TAB_OFF}`}>Documents</button>}
        {tasks !== undefined && <button type="button" role="tab" aria-selected={tab === "tasks"} onClick={() => onTabChange("tasks")} className={`${TAB} ${tab === "tasks" ? TAB_ON : TAB_OFF}`}>Tasks</button>}
        {openFiles.map((f) => {
          const id = `file:${f.path}`;
          return <span key={f.path} className="inline-flex items-center">
            <button type="button" role="tab" aria-selected={tab === id} title={f.path} onClick={() => onTabChange(id)} className={`${TAB} ${tab === id ? TAB_ON : TAB_OFF} pr-1`}>
              {base(f.path)}{f.added ? <span className="rounded bg-emerald-50 px-1 font-mono text-[11px] text-emerald-700 dark:bg-emerald-400/10! dark:text-emerald-300!">+{f.added}</span> : null}
            </button>
            <button type="button" aria-label={`Close ${base(f.path)}`} onClick={() => onCloseFile(f.path)}
              className="rounded p-0.5 text-slate-400 hover:bg-slate-200/70 hover:text-slate-700 dark:hover:bg-white/10!"><AppIcon name="cross" size={12} /></button>
          </span>;
        })}
      </div>
      {/* One scrolling column: the panel's width, whatever a tab renders inside it. */}
      <div className={`min-h-0 w-full min-w-0 flex-1 overflow-y-auto overflow-x-hidden ${THIN_SCROLLBAR}`}>
        <div role="tabpanel" className="w-full min-w-0 max-w-full">{tab === "step" ? step : tab === "files" ? files : tab === "tasks" && tasks !== undefined ? tasks : tab === "changes" && changes !== undefined ? changes : tab === "documents" && documents !== undefined ? documents : filePath !== undefined ? file(filePath) : null}</div>
      </div>
    </div> : null}
  </SidePanel>;
}
