import { useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Pill, Table, Toggle, type TableColumn, type TableSortState } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError } from "../../../lib/bridge";
import { windowOf, type WindowState } from "../tools/clientWindow";
import type { ServerRow } from "./mcpModel";
import { unclassifiedTools as unclassifiedOf } from "./mcpModel";
import { ProcessPill } from "./ProcessPanel";
import type { McpProcess } from "./useMcpProcesses";

/**
 * The Servers table, drawn the way the Agents page draws its rows: one table that
 * fills the height and reveals more as it is scrolled. Every row is what the daemon
 * answered to `mcp.servers` (filters travel as params); the order and the reveal are
 * this table's. Env appears as NAME chips only; the status is the daemon's word and
 * `unprobed` keeps its own grey. A row opens the server's own panel — nothing here
 * edits.
 */
export interface ServerListProps {
  rows: ServerRow[];
  loading: boolean;
  error: string | undefined;
  onRetry: () => void;
  canWrite: boolean;
  /** The level-0 sentence the daemon's own CLI prints (pinned, not reworded here). */
  emptyStatement: string;
  /** True while a search or filter narrows the rows: an empty result is then "no match", not level 0. */
  filtered: boolean;
  /** Changes when the rows are replaced, so the reveal starts again from the top. */
  resetKey: string;
  onSelect: (name: string) => void;
  onToggleEnabled: (row: ServerRow, enabled: boolean) => void;
  /** The supervisor's view by server name; absent while the first poll is out. */
  processes?: Record<string, McpProcess>;
}

const STEP = 40;

function sortValue(row: ServerRow, column: string): string | number {
  switch (column) {
    case "tier": return `${row.tier}:${row.agent}`;
    case "tools": return (row.tools ?? []).length;
    case "enabled": return row.enabled ? 1 : 0;
    default: return (row.title && row.title !== row.name ? row.title : row.name).toLowerCase();
  }
}

export function ServerList({ rows, loading, error, onRetry, canWrite, emptyStatement, filtered, resetKey, onSelect, onToggleEnabled, processes }: ServerListProps) {
  const { t } = useI18n();
  const [sort, setSort] = useState<{ column: string; dir: "asc" | "desc" }>({ column: "server", dir: "asc" });
  const [shown, setShown] = useState(STEP);
  const reset = `${resetKey}|${sort.column}|${sort.dir}`;
  useEffect(() => setShown(STEP), [reset]);
  const win: WindowState = { page: 1, pageSize: shown, sort: sort.column, dir: sort.dir };
  const answer = useMemo(() => windowOf(rows, win, sortValue), [rows, shown, sort.column, sort.dir]); // eslint-disable-line react-hooks/exhaustive-deps

  if (error) return <ErrorPanel error={new RpcError(0, error)} onRetry={onRetry} />;

  const columns: TableColumn<ServerRow>[] = [
    {
      id: "server",
      groupable: false,
      header: t("settings.mcp.servers.col.server"),
      sortable: true,
      sortValue: (row) => String(sortValue(row, "server")),
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid="server-row">
          <div className="flex items-center gap-2">
            <button type="button" className="cursor-pointer truncate text-left text-sm font-semibold hover:underline" onClick={(event) => { event.stopPropagation(); onSelect(row.name); }} data-testid="server-open">
              {row.title && row.title !== row.name ? row.title : row.name}
            </button>
            <OriginBadge origin={row.origin} />
          </div>
          <code className="block truncate text-xs opacity-60">{row.title && row.title !== row.name ? row.name : ""}</code>
          {row.description && <div className="truncate text-xs opacity-70" title={row.description}>{row.description}</div>}
        </div>
      ),
    },
    {
      id: "tier",
      groupable: false,
      header: t("settings.mcp.servers.col.tier"),
      sortable: true,
      width: 160,
      sortValue: (row) => String(sortValue(row, "tier")),
      render: (row) => (
        <span className="flex flex-wrap items-center gap-1">
          <Pill size="sm" tone={row.tier === "public" ? "blue" : "violet"}>{row.tier}</Pill>
          {row.tier === "private" && row.agent !== "" && <code className="text-xs opacity-70">{row.agent}</code>}
        </span>
      ),
    },
    {
      id: "transport",
      groupable: false,
      header: t("settings.mcp.servers.col.transport"),
      width: 300,
      render: (row) => (
        <div className="w-0 min-w-full text-xs">
          <div className="truncate font-mono" title={row.transport.url ?? undefined}>
            {row.transport.url ? row.transport.url : `${row.transport.command ?? ""} ${(row.transport.args ?? []).join(" ")}`.trim()}
          </div>
          {(row.transport.envNames ?? []).length > 0 && (
            <div className="mt-1 flex flex-wrap gap-1">
              {(row.transport.envNames ?? []).map((name) => <Pill key={name} size="sm" tone="slate" data-testid="env-chip">{name}</Pill>)}
            </div>
          )}
        </div>
      ),
    },
    {
      id: "tools",
      groupable: false,
      header: t("settings.mcp.servers.col.tools"),
      sortable: true,
      width: 130,
      sortValue: (row) => String(sortValue(row, "tools")).padStart(6, "0"),
      render: (row) => {
        const unclassified = unclassifiedOf(row);
        return (
          <span className="text-xs">
            {(row.tools ?? []).length}
            {unclassified.length > 0 && <Pill size="sm" tone="amber" className="ml-1" data-testid="unclassified-chip">{t("settings.mcp.tool.unclassified")}</Pill>}
          </span>
        );
      },
    },
    {
      id: "process",
      groupable: false,
      header: t("settings.mcp.process.col"),
      width: 150,
      render: (row) => <ProcessPill process={row.enabled ? processes?.[row.name] : undefined} />,
    },
    {
      id: "enabled",
      groupable: false,
      header: t("settings.mcp.servers.col.enabled"),
      sortable: true,
      width: 120,
      sortValue: (row) => String(sortValue(row, "enabled")),
      render: (row) => (
        <span onClick={(event) => event.stopPropagation()}>
          <Toggle
            size="sm"
            checked={row.enabled}
            disabled={!canWrite}
            aria-label={`${row.enabled ? t("settings.mcp.servers.enabled") : t("settings.mcp.servers.disabled")} — ${row.name}`}
            onChange={(event) => onToggleEnabled(row, event.target.checked)}
            data-testid="enabled-switch"
          />
        </span>
      ),
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="server-list">
      <div className="min-h-[18rem] flex-1">
        <Table<ServerRow>
          variant="outlined"
          data={answer.rows}
          columns={columns}
          rowKey={(row) => `${row.tier}:${row.agent}:${row.name}`}
          manualSorting
          corner="rounded-sm"
          sortState={{ columnId: sort.column, direction: sort.dir }}
          onSortChange={(next: TableSortState | null) =>
            setSort(next === null ? { column: "server", dir: "asc" } : { column: next.columnId, dir: next.direction === "desc" ? "desc" : "asc" })
          }
          loading={loading}
          loadingMessage={t("settings.mcp.servers.loading")}
          emptyState={
            // The daemon's level-0 text — absence STATED, in the daemon's words; a narrowed list says "no match".
            filtered
              ? <EmptyState title={t("settings.mcp.servers.noMatch")} data-testid="mcp-empty-filter" />
              : <EmptyState title={emptyStatement} data-testid="mcp-empty" />
          }
          onRowClick={(row) => onSelect(row.name)}
          fullHeight
          infiniteScroll={{ hasMore: answer.rows.length < answer.total, onLoadMore: async () => setShown((n) => n + STEP), resetKey: reset }}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="server-count">
        {t("settings.mcp.servers.footer", { shown: answer.rows.length, total: answer.total })}
      </div>
    </div>
  );
}

function OriginBadge({ origin }: { origin: string }) {
  const tone = origin === "system" ? "blue" : origin === "import" ? "amber" : "slate";
  return <Pill size="sm" tone={tone} data-testid="origin-badge">{origin}</Pill>;
}
