import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Pill, Table, type TableColumn } from "../../components/kit";
import { LoopActions, useDeleteLoop } from "../components/LoopCard";
import type { WorkComponent, WorkLoop } from "./workModel";

/**
 * Loops — every loop in the scope, newest first. The Component column appears only when the scope
 * spans more than one component, so inside a leaf it is not noise.
 */
export function WorkLoopsTab({ loops, components, showComponent, canWrite = false, onChanged, showArchived = false, onShowArchived }: {
  loops: readonly WorkLoop[]; components: readonly WorkComponent[]; showComponent: boolean;
  /** Archive and Delete are offered to a connection that may write. */
  canWrite?: boolean; onChanged?: () => void;
  showArchived?: boolean; onShowArchived?: (on: boolean) => void;
}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const del = useDeleteLoop(() => onChanged?.());
  const nameOf = useMemo(() => new Map(components.map((component) => [component.root, component.name])), [components]);
  const rows = useMemo(() => loops.slice().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)), [loops]);
  const columns: TableColumn<WorkLoop>[] = [
    { id: "loop", groupable: false, header: t("work.loops.col.loop"), minWidth: 240, maxWidth: 520, render: (row) => <span className="block max-w-full truncate font-medium" title={row.title || row.slug} data-testid={`work-loop-${row.id}`}>{row.title || row.slug}{(row as WorkLoop & { archivedAt?: string }).archivedAt ? " · archived" : ""}</span> },
    ...(showComponent ? [{ id: "component", groupable: false, header: t("work.loops.col.component"), width: 180, render: (row: WorkLoop) => <span className="block truncate text-sm opacity-80">{nameOf.get(row.componentRoot) ?? row.componentRoot}</span> } as TableColumn<WorkLoop>] : []),
    { id: "status", groupable: false, header: t("work.loops.col.status"), width: 130, render: (row) => <Pill size="sm" tone={row.status === "blocked" ? "red" : row.status === "waiting" ? "amber" : "emerald"}>{row.status}</Pill> },
    { id: "phase", groupable: false, header: t("work.loops.col.phase"), width: 140, render: (row) => <span className="text-sm opacity-80">{row.phase || "—"}</span> },
    { id: "updated", groupable: false, header: t("work.loops.col.updated"), width: 200, render: (row) => <span className="whitespace-nowrap text-xs opacity-70">{row.updatedAt}</span> },
    // Always in view at the right edge, so a long row never hides them.
    ...(onChanged ? [{ id: "actions", groupable: false, hideable: false, isActionsColumn: true, sticky: "right", header: "", width: 210,
      render: (row: WorkLoop) => <LoopActions loop={row as never} canWrite={canWrite} onChanged={onChanged} onDelete={del.open} /> } as TableColumn<WorkLoop>] : []),
  ];
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2" data-testid="work-loops">
      {del.dialog}
      <div className="min-h-[18rem] min-w-0 flex-1">
        <Table<WorkLoop>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(row) => row.id}
          corner="rounded-sm"
          onRowClick={(row) => navigate(`/loops/${encodeURIComponent(row.id)}`)}
          emptyState={<div data-testid="work-loops-empty"><EmptyState title={t("work.loops.empty")} /></div>}
          fullHeight
        />
      </div>
      <div className="flex shrink-0 items-center gap-4 text-xs opacity-80">
        <p>{t("work.loops.footer", { shown: rows.length })}</p>
        {onShowArchived && <label className="ml-auto flex items-center gap-2">
          <input type="checkbox" checked={showArchived} onChange={(e) => onShowArchived(e.target.checked)} data-testid="show-archived-loops" />
          Show archived
        </label>}
      </div>
    </div>
  );
}
