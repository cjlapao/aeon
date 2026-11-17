import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill, Select, Table, type TableColumn } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { GoalDrawer } from "./GoalDrawer";
import { REVIEW_STATES, useGoals, type GoalRow } from "./goalsRpc";

/**
 * The goals a brainstorm (or a person) filed: one row each with its review state, and the decision a
 * person makes in the side panel a row opens. What a goal owns (documents, loops, tasks) is read there;
 * the tasks themselves live on the Tasks board.
 */
export interface GoalsScope {
  /** The project (root component) the daemon narrows goals to. */
  rootId: string;
  /** The components inside the scope; goals outside it are left out. */
  componentIds: ReadonlySet<string>;
}

const STATE_TONE: Record<string, "emerald" | "amber" | "red" | "slate"> = { approved: "emerald", in_review: "amber", rejected: "red", draft: "slate", parked: "slate" };

export function GoalsPanel({ controller, live, canWrite, scope }: { controller: LiveController; live: LiveState; canWrite: boolean; scope?: GoalsScope }) {
  const { t } = useI18n();
  const [state, setState] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const goalsQuery = useGoals(state, controller, live, scope?.rootId);
  // The daemon narrows goals to the project; the sub-component cut is made here, over rows it answered.
  const rows = scope ? goalsQuery.data?.filter((g) => scope.componentIds.has(g.componentId)) : goalsQuery.data;

  const columns: TableColumn<GoalRow>[] = [
    { id: "goal", groupable: false, header: t("work.goals.col.goal"), render: (row) => <span className="block truncate font-medium" data-testid={`goal-row-${row.id}`}>{row.title}</span> },
    { id: "state", groupable: false, header: t("work.goals.col.state"), width: 130, render: (row) => <Pill size="sm" tone={STATE_TONE[row.reviewState] ?? "slate"}>{row.reviewState}</Pill> },
    { id: "type", groupable: false, header: t("work.goals.col.type"), width: 120, render: (row) => <span className="text-sm opacity-80">{row.type}</span> },
    { id: "version", groupable: false, header: t("work.goals.col.version"), width: 90, render: (row) => <span className="text-sm opacity-80">v{row.version}</span> },
    { id: "source", groupable: false, header: t("work.goals.col.source"), width: 150, render: (row) => <span className="text-sm opacity-80">{row.sourceKind}</span> },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3" data-testid="goals-panel">
      <div className="flex shrink-0 flex-wrap items-center gap-3">
        <div className="w-56">
          <Select variant="ghost" size="md" value={state} onChange={(e) => setState(e.target.value)} aria-label={t("work.goals.state")} data-testid="goals-state-filter">
            <option value="">{t("work.goals.allStates")}</option>
            {REVIEW_STATES.map((s) => <option key={s} value={s}>{s}</option>)}
          </Select>
        </div>
        <p className="text-sm opacity-70">{t("work.goals.hint")}</p>
      </div>
      {goalsQuery.error !== undefined && <Alert intent="danger" title={goalsQuery.error} data-testid="goals-error" />}
      <div className="min-h-[18rem] min-w-0 flex-1">
        <Table<GoalRow>
          variant="outlined"
          data={rows ?? []}
          columns={columns}
          rowKey={(row) => row.id}
          corner="rounded-sm"
          onRowClick={(row) => setOpen(row.id)}
          emptyState={<div data-testid="goals-empty"><EmptyState title={t("work.goals.empty")} /></div>}
          fullHeight
        />
      </div>
      {open !== null && (
        <GoalDrawer goalId={open} controller={controller} live={live} canWrite={canWrite} onClose={() => setOpen(null)} onChanged={goalsQuery.refetch} />
      )}
    </div>
  );
}
