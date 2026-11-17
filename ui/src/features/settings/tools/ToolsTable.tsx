import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import type { ToolRow } from "./toolsModel";
import { useReveal } from "./useReveal";

// The Tools tab's table. One quiet row per tool: the name with its address under it, the family,
// the access and the source. The status column speaks only when something is wrong — UNWIRED
// (`available:false`), UNCLASSIFIED (an MCP tool nobody classified), stale (`rowCurrent:false`) —
// so a healthy catalog reads as plain rows. Capabilities, re-run class, version and the rest
// are the tool panel's business. Every word is the daemon's; `tools.list` answers every tool,
// so the sort and the reveal are this table's (`useReveal`).

export interface ToolsTableProps {
  tools: ToolRow[];
  statement?: string;
  selected?: string;
  onSelect: (name: string) => void;
  loading?: boolean;
}

const SORT_VALUE = (row: ToolRow, column: string): string | number => {
  switch (column) {
    case "access": return row.access ?? "";
    case "family": return row.family;
    case "source": return row.source;
    default: return row.name;
  }
};

export function ToolsTable({ tools, statement, onSelect, loading = false }: ToolsTableProps) {
  const { t } = useI18n();
  const view = useReveal(tools, String(tools.length), "name", SORT_VALUE);

  const columns: TableColumn<ToolRow>[] = [
    {
      id: "name",
      groupable: false,
      header: t("settings.tools.col.name"),
      sortable: true,
      render: (tool) => (
        <div className="w-0 min-w-full py-1" data-testid={`tool-row-${tool.name}`}>
          <div className="flex min-w-0 items-baseline gap-2">
            <span className="truncate text-sm font-semibold" data-testid={`tool-name-${tool.name}`}>{tool.name}</span>
            {(tool.aliases ?? []).slice(0, 1).map((alias) => (
              <span key={alias} className="shrink-0 text-xs opacity-60" data-testid={`tool-alias-${tool.name}-${alias}`}>{t("settings.tools.row.alias", { alias })}</span>
            ))}
          </div>
          <code className="block truncate text-xs opacity-60" data-testid={`tool-address-${tool.name}`}>{tool.address}</code>
        </div>
      ),
    },
    { id: "family", groupable: false, header: t("settings.tools.col.family"), sortable: true, width: 130, accessor: "family" },
    {
      id: "access",
      groupable: false,
      header: t("settings.tools.col.access"),
      sortable: true,
      width: 90,
      render: (tool) => <Pill tone={tool.access === "write" ? "amber" : "slate"} size="sm" data-testid={`tool-access-${tool.name}`}>{tool.access}</Pill>,
    },
    {
      id: "source",
      groupable: false,
      header: t("settings.tools.col.source"),
      sortable: true,
      width: 110,
      render: (tool) => <Pill tone={typeof tool.server === "string" && tool.server !== "" ? "violet" : "blue"} size="sm" data-testid={`tool-source-${tool.name}`}>{tool.source}</Pill>,
    },
    {
      id: "status",
      groupable: false,
      header: t("settings.tools.col.status"),
      width: 210,
      render: (tool) => {
        const isMCP = typeof tool.server === "string" && tool.server !== "";
        return (
          <span className="flex flex-wrap gap-1">
            {tool.available ? null : <Pill tone="amber" size="sm" data-testid={`tool-unwired-${tool.name}`}>{t("settings.tools.row.unwired")}</Pill>}
            {isMCP && !tool.classified ? <Pill tone="rose" size="sm" data-testid={`tool-unclassified-${tool.name}`}>{t("settings.tools.row.unclassified")}</Pill> : null}
            {!tool.rowCurrent ? <Pill tone="orange" size="sm" data-testid={`tool-stale-${tool.name}`}>{t("settings.tools.row.stale")}</Pill> : null}
          </span>
        );
      },
    },
  ];

  const empty = statement ? (
    <p className="text-sm opacity-80" data-testid="tools-empty-statement">{statement}</p>
  ) : (
    <EmptyState title={t("settings.tools.empty")} subtitle={t("settings.tools.emptyNone")} />
  );

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="tools-list">
      <div className="min-h-[18rem] flex-1">
        <Table<ToolRow>
          variant="outlined"
          data={view.rows}
          columns={columns}
          rowKey={(tool) => tool.name}
          manualSorting
          corner="rounded-sm"
          sortState={view.sortState}
          onSortChange={view.onSortChange}
          onRowClick={(tool) => onSelect(tool.name)}
          loading={loading}
          emptyState={<div data-testid="tools-empty">{empty}</div>}
          fullHeight
          infiniteScroll={view.infinite}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="tools-count">{t("settings.tools.footer", { shown: view.rows.length, total: view.total })}</div>
    </div>
  );
}
