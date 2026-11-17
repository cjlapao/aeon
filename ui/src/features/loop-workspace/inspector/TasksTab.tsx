import { useCallback, useEffect, useState } from "react";
import { rpc, toRpcError } from "../../../lib/bridge";
import { MarkdownView } from "../../loops/components/MarkdownView";

interface TaskRow { id: string; taskid: string; title: string; status: string; tech: string; assignee: string; loopSlug?: string; loopSlugs?: string[]; componentRoot?: string }

export interface TasksTabProps {
  slug: string;
  componentRoot: string;
  /** Changes when a task event arrives, so the list follows the loop. */
  refreshKey?: unknown;
}

/** The tasks that belong to this loop: the ones its plan filed, falling back to the component's when none is tied to it. */
export function tasksOfLoop(all: readonly TaskRow[], slug: string, componentRoot: string): TaskRow[] {
  const mine = all.filter((t) => t.loopSlug === slug || (t.loopSlugs ?? []).includes(slug));
  return mine.length > 0 ? mine : all.filter((t) => t.componentRoot === componentRoot);
}

function Detail({ slug, componentRoot, task }: { slug: string; componentRoot: string; task: TaskRow }) {
  const [text, setText] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    void (async () => {
      for (const name of [`tasks/${task.taskid}.md`, `${task.taskid}.md`]) {
        try {
          const doc = await rpc<{ content?: string }>("loop.read", { slug, componentRoot, name });
          if (on && typeof doc?.content === "string") { setText(doc.content); return; }
        } catch { /* try the next name */ }
      }
      if (on) setText("");
    })();
    return () => { on = false; };
  }, [slug, componentRoot, task.taskid]);
  if (text === null) return <p className="px-3 pb-3 text-xs text-slate-500">Opening…</p>;
  return text === "" ? <p className="px-3 pb-3 text-xs text-slate-500">No detail document is attached to this task.</p>
    : <div data-testid="task-detail" className="max-h-96 overflow-auto px-3 pb-3"><MarkdownView content={text} /></div>;
}

/** What the loop's plan would build: its tasks, in order, each opening to the detail the planner wrote for it. */
export function TasksTab({ slug, componentRoot, refreshKey }: TasksTabProps) {
  const [tasks, setTasks] = useState<TaskRow[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const load = useCallback(async () => {
    try {
      const res = await rpc<{ tasks?: TaskRow[] }>("task.list", {});
      const rows = Array.isArray(res?.tasks) ? res.tasks : [];
      setTasks(tasksOfLoop(rows, slug, componentRoot).sort((a, b) => a.taskid.localeCompare(b.taskid, undefined, { numeric: true })));
      setError("");
    } catch (cause) { setError(toRpcError(cause).message); }
  }, [slug, componentRoot]);
  useEffect(() => { void load(); }, [load, refreshKey]);
  if (error) return <p role="alert" className="p-3 text-sm text-red-700">{error}</p>;
  if (tasks === null) return <p className="p-3 text-sm text-slate-500">Reading the tasks…</p>;
  if (tasks.length === 0) return <p data-testid="tasks-empty" className="p-3 text-sm text-slate-500">This loop has no tasks yet.</p>;
  return <div data-testid="tasks-tab" className="divide-y divide-slate-200 dark:divide-white/10!">
    <p className="px-3 py-2 text-xs uppercase tracking-wider text-slate-500">{tasks.length} tasks in the plan</p>
    {tasks.map((task) => <div key={task.id}>
      <button type="button" aria-expanded={open === task.id} onClick={() => setOpen(open === task.id ? null : task.id)}
        className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left hover:bg-slate-50 dark:hover:bg-white/5!">
        <span className="shrink-0 font-mono text-xs text-slate-500">{task.taskid}</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-slate-900 dark:text-slate-100!">{task.title}</span>
        {task.tech && <span className="shrink-0 rounded bg-slate-100 px-1.5 font-mono text-[11px] text-slate-600 dark:bg-white/10! dark:text-slate-300!">{task.tech}</span>}
        <span className="shrink-0 font-mono text-[11px] text-slate-500">{task.status}</span>
      </button>
      {open === task.id && <Detail slug={slug} componentRoot={componentRoot} task={task} />}
    </div>)}
  </div>;
}
