import { DetachedNote } from "../../components/DetachedNote";
import { Pill } from "../../components/kit";
import type { TaskRow } from "./goalsRpc";

/** The task rows as the daemon answered them: status, who it is assigned to,
 *  the loop that owns it, the cost as a floor when it is one. */
export function TaskList({ tasks, loading, emptyText }: { tasks: TaskRow[] | undefined; loading: boolean; emptyText: string }) {
  if (tasks === undefined) return <p className="text-xs opacity-60">{loading ? "Loading tasks…" : ""}</p>;
  if (tasks.length === 0) return <p className="text-xs opacity-70" data-testid="tasks-empty">{emptyText}</p>;
  return (
    <ul className="space-y-1" data-testid="task-rows">
      {tasks.map((task) => (
        <li key={task.id} className="rounded-lg border border-slate-200 p-2 text-xs dark:border-slate-700" data-testid={`task-${task.id}`}>
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono opacity-70">{task.taskid || task.id}</span>
            <strong>{task.title}</strong>
            <Pill tone="slate">{task.status}</Pill>
            {task.manual && <Pill tone="slate">manual</Pill>}
            {task.approvalState !== "none" && <Pill tone="slate">approval {task.approvalState}</Pill>}
          </div>
          <div className="pt-0.5 opacity-70">
            {task.assignee ? `${task.assigneeKind} ${task.assignee}` : "unassigned"}
            {task.loopSlug ? ` · loop ${task.loopSlug}` : ""}
            {task.costUsd > 0 ? ` · $${task.costUsd.toFixed(2)}${task.costIsFloor ? " (floor)" : ""}` : ""}
            {<DetachedNote kind="task" id={task.id} />}
          </div>
        </li>
      ))}
    </ul>
  );
}
