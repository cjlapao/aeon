import { DetachedNote } from "../../components/DetachedNote";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { TaskBoard } from "@cjlapao/ui-kit";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Pill, Select } from "../../components/kit";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import type { LiveController, LiveState } from "../../lib/live";
import { useLiveSubscription } from "../../lib/useLiveSubscription";
import { useRpcQuery } from "../../lib/useRpcQuery";
import { TASK_STATUSES, useGoals, type TaskRow } from "../goals/goalsRpc";
import { COLUMN_COLOR, TASK_COLUMNS, columnOf as columnFor, ladderPosition, toCard } from "./planModel";

/**
 * Tasks — the kit's task board over the daemon's real tasks. Six columns group the fourteen statuses;
 * each card keeps its true status as a tag. The board is read-only on purpose: a task moves when an
 * agent finishes a step, and the board follows the daemon's events — nobody drags a task to "merged".
 */
export function WorkTasksView({ controller, live, componentIds }: { controller: LiveController; live: LiveState; componentIds: ReadonlySet<string> | undefined }) {
  const { t } = useI18n();
  const [open, setOpen] = useState<TaskRow | null>(null);
  const [goal, setGoal] = useState("");
  const goals = useGoals("", controller, live);
  const goalRows = (Array.isArray(goals.data) ? goals.data : []).filter((g) => !componentIds || componentIds.has(g.componentId));
  const query = useRpcQuery("task.list", (goal ? { goal } : {}) as never, controller, live);
  useLiveSubscription(live, ["task.*", "loop.*", "goal.*"], () => { void query.refetch(); });

  const tasks = useMemo(() => {
    const all = ((query.data as { tasks?: TaskRow[] } | undefined)?.tasks ?? []);
    return componentIds ? all.filter((task) => componentIds.has(task.componentId)) : all;
  }, [query.data, componentIds]);
  const cards = useMemo(() => tasks.map(toCard), [tasks]);
  const columns = TASK_COLUMNS.map((id) => ({ id, title: t(`work.tasks.col.${id}`), color: COLUMN_COLOR[id] }));

  return (
    <div className="flex min-h-[28rem] flex-col gap-3" data-testid="work-tasks">
      <div className="flex flex-wrap items-center gap-3">
        <div className="w-64">
          <Select variant="ghost" size="md" value={goal} onChange={(e) => setGoal(e.target.value)} aria-label={t("work.tasks.goal")} data-testid="work-tasks-goal">
            <option value="">{t("work.tasks.allGoals")}</option>
            {goalRows.map((g) => <option key={g.id} value={g.id}>{g.title}</option>)}
          </Select>
        </div>
        <p className="flex-1 text-sm opacity-70">{t("work.tasks.hint")}</p>
      </div>
      {query.error && <Alert intent="danger" title={query.error} data-testid="work-tasks-error" />}
      {query.data !== undefined && tasks.length === 0 ? (
        <p className="text-sm opacity-70" data-testid="work-tasks-empty">{t("work.tasks.empty")}</p>
      ) : (
        <TaskBoard
          cards={cards as never}
          columns={columns}
          editable={false}
          cardLayout="detailed"
          loading={query.loading && query.data === undefined}
          onCardOpen={(card) => setOpen(((card.data as { task?: TaskRow } | undefined)?.task) ?? null)}
        />
      )}
      {open && (
        <OverlaySidePanel title={open.title || open.taskid} titleClassName="text-xl font-semibold" variant="simple" width={480} onClose={() => setOpen(null)}>
          <div className="flex flex-col gap-3 text-sm" data-testid="work-task-panel">
            <div className="flex flex-wrap items-center gap-2"><Pill tone="slate">{open.status}</Pill><span className="opacity-70">{t("work.tasks.step", { at: ladderPosition(open.status), of: TASK_STATUSES.length })}</span></div>
            <p className="opacity-80">{t("work.tasks.stage", { column: t(`work.tasks.col.${columnFor(open.status)}`) })}</p>
            <dl className="grid grid-cols-[8rem_1fr] gap-y-1">
              <dt className="opacity-60">{t("work.tasks.assignee")}</dt><dd>{open.assignee ? `${open.assigneeKind} ${open.assignee}` : t("work.tasks.unassigned")}</dd>
              <dt className="opacity-60">{t("work.tasks.updated")}</dt><dd>{open.updatedAt}</dd>
              {open.costUsd > 0 && (<><dt className="opacity-60">{t("work.tasks.cost")}</dt><dd>{`$${open.costUsd.toFixed(2)}${open.costIsFloor ? ` ${t("work.tasks.floor")}` : ""}`}</dd></>)}
            </dl>
            {<DetachedNote kind="task" id={open.id} />}
            {open.loopSlug && <Link className="aeon-accent-text underline" to={`/loops/${encodeURIComponent(open.loopSlug)}`}>{t("work.tasks.openLoop", { slug: open.loopSlug })}</Link>}
          </div>
        </OverlaySidePanel>
      )}
    </div>
  );
}
