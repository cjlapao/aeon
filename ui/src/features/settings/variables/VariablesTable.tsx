import { useEffect, useMemo, useState } from "react";
import { Button, EmptyState, Pill, Table, type TableColumn, type TableSortState } from "../../../components/kit";
import { windowOf, type WindowState } from "../tools/clientWindow";
import type { VaultVariable } from "./variablesRpc";

// The variables as the Agents page shows its rows: one table that fills the height and reveals
// more as it is scrolled. The daemon answers the whole list, so the order and the reveal are
// this table's; the rows are the daemon's. A secret's value is never a column — the wire has no
// field for it, and the row says so.

const STEP = 40;

export const variableKey = (v: VaultVariable): string => `${v.name}/${v.project ?? ""}`;

function sortValue(v: VaultVariable, column: string): string {
  switch (column) {
    case "kind": return v.secret ? "secret" : "plain";
    case "scope": return v.project ? (v.projectName ?? v.project) : "";
    case "agents": return v.agentVisible ? "1" : "0";
    default: return v.name;
  }
}

export interface VariablesTableProps {
  rows: VaultVariable[];
  total: number;
  loading: boolean;
  filtered: boolean;
  canWrite: boolean;
  resetKey: string;
  onEdit: (variable: VaultVariable) => void;
  onDelete: (variable: VaultVariable) => void;
}

export function VariablesTable({ rows, total, loading, filtered, canWrite, resetKey, onEdit, onDelete }: VariablesTableProps) {
  const [sort, setSort] = useState<{ column: string; dir: "asc" | "desc" }>({ column: "name", dir: "asc" });
  const [shown, setShown] = useState(STEP);
  const reset = `${resetKey}|${sort.column}|${sort.dir}`;
  useEffect(() => setShown(STEP), [reset]);
  const win: WindowState = { page: 1, pageSize: shown, sort: sort.column, dir: sort.dir };
  const answer = useMemo(() => windowOf(rows, win, sortValue), [rows, shown, sort.column, sort.dir]); // eslint-disable-line react-hooks/exhaustive-deps

  const columns: TableColumn<VaultVariable>[] = [
    {
      id: "name",
      groupable: false,
      header: "Name",
      sortable: true,
      sortValue: (v) => v.name,
      render: (v) => (
        <div className="w-0 min-w-full py-1" data-testid={`variable-${v.name}`} data-scope={v.project ?? "global"}>
          <code className="block truncate text-sm font-semibold">{v.name}</code>
          {v.description && <div className="truncate text-xs opacity-70" title={v.description}>{v.description}</div>}
        </div>
      ),
    },
    {
      id: "kind",
      groupable: false,
      header: "Kind",
      sortable: true,
      width: 110,
      render: (v) => <Pill size="sm" tone={v.secret ? "amber" : "slate"} data-testid={`kind-${v.name}`}>{v.secret ? "secret" : "plain"}</Pill>,
    },
    {
      id: "value",
      groupable: false,
      header: "Value",
      width: 220,
      render: (v) => <span className="block truncate font-mono text-xs opacity-80" data-testid={`value-${v.name}`}>{v.secret ? "••••••••" : v.value}</span>,
    },
    {
      id: "scope",
      groupable: false,
      header: "Applies to",
      sortable: true,
      width: 180,
      render: (v) => <Pill size="sm" tone="slate" data-testid={`scope-${v.name}`}>{v.project ? `project: ${v.projectName ?? v.project}` : "global"}</Pill>,
    },
    {
      id: "agents",
      groupable: false,
      header: "Agents",
      sortable: true,
      width: 170,
      render: (v) => <Pill size="sm" tone={v.agentVisible ? "rose" : "slate"} data-testid={`agents-${v.name}`}>{v.agentVisible ? "agents can read" : "hidden from agents"}</Pill>,
    },
    {
      id: "actions",
      header: "Actions",
      width: 150,
      align: "right",
      isActionsColumn: true,
      hideable: false,
      groupable: false,
      render: (v) => (
        <span className="flex justify-end gap-1" onClick={(event) => event.stopPropagation()}>
          <Button size="xs" variant="ghost" disabled={!canWrite} onClick={() => onEdit(v)} data-testid={`edit-${v.name}`}>Edit</Button>
          <Button size="xs" variant="ghost" disabled={!canWrite} onClick={() => onDelete(v)} data-testid={`delete-${v.name}`}>Delete</Button>
        </span>
      ),
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="variable-rows">
      <div className="min-h-[18rem] flex-1">
        <Table<VaultVariable>
          variant="outlined"
          data={answer.rows}
          columns={columns}
          rowKey={variableKey}
          manualSorting
          corner="rounded-sm"
          sortState={{ columnId: sort.column, direction: sort.dir }}
          onSortChange={(next: TableSortState | null) =>
            setSort(next === null ? { column: "name", dir: "asc" } : { column: next.columnId, dir: next.direction === "desc" ? "desc" : "asc" })
          }
          loading={loading}
          loadingMessage="Loading variables…"
          emptyState={
            filtered
              ? <EmptyState title="No variable matches." data-testid="variables-empty-filter" />
              : <EmptyState title="No variables yet. Add one for a token or setting a server or an agent needs." data-testid="variables-empty" />
          }
          onRowClick={onEdit}
          fullHeight
          stickyActions
          infiniteScroll={{ hasMore: answer.rows.length < answer.total, onLoadMore: async () => setShown((n) => n + STEP), resetKey: reset }}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="variables-count">
        Showing {answer.rows.length} of {total} variables
      </div>
    </div>
  );
}
