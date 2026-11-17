import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { EmptyState, MultiToggle, Pill, SearchBar, Table, type TableColumn } from "../../../components/kit";
import { useReveal } from "./useReveal";
import type { RpcMethods } from "../../../proto/contract";

// The By agent tab's list (S75): every agent in the roster as a table, with a
// search box and quick filters, instead of a picker that only ever shows names.
// Choosing a row opens that agent's tool view in a side panel (the page owns it).
//
// `agent.list` answers the whole roster and carries identity only, so the
// search, the quick filters, the sort and the page are this table's to keep
// (`clientWindow.ts`). The filter choices are read off the rows that arrived —
// the provenance and state words are the daemon's, never typed here — and each
// choice says how many rows it holds, so a filter that would empty the table is
// visible before it is clicked.

type Entry = RpcMethods["agent.list"]["result"]["agents"][number];

/** The identity a roster entry governs (a newer daemon may add `slug`). */
export function identityOf(entry: Entry): string {
  const slug = (entry as { slug?: unknown }).slug;
  return typeof slug === "string" && slug !== "" ? slug : entry.name;
}

const ALL = "";

export interface AgentRosterTableProps {
  agents: Entry[];
  loading: boolean;
  error?: string;
  selected: string;
  onSelect: (identity: string) => void;
}

const SORT_VALUE = (row: Entry, column: string): string => {
  switch (column) {
    case "provenance": return row.provenance;
    case "state": return row.state ?? "";
    default: return identityOf(row);
  }
};

function QuickFilter({ label, field, rows, value, onChange }: {
  label: string; field: "provenance" | "state"; rows: Entry[]; value: string; onChange: (v: string) => void;
}) {
  const { t } = useI18n();
  const counts = new Map<string, number>();
  for (const r of rows) {
    const v = (field === "state" ? r.state : r.provenance) ?? "";
    if (v !== "") counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  if (counts.size === 0) return null;
  const options = [{ value: ALL, label: `${t("settings.tools.filters.any")} (${rows.length})` }, ...[...counts.entries()].sort().map(([v, n]) => ({ value: v, label: `${v} (${n})` }))];
  return (
    <div className="flex items-center gap-2 text-xs" data-testid={`agents-filter-${field}`}>
      <span className="font-medium text-slate-600 dark:text-slate-400">{label}</span>
      <MultiToggle aria-label={label} options={options} value={value} onChange={onChange} size="sm" />
    </div>
  );
}

export function AgentRosterTable({ agents, loading, error, onSelect }: AgentRosterTableProps) {
  const { t } = useI18n();
  const [term, setTerm] = useState("");
  const [provenance, setProvenance] = useState(ALL);
  const [state, setState] = useState(ALL);

  const needle = term.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      agents.filter(
        (a) =>
          (provenance === ALL || a.provenance === provenance) &&
          (state === ALL || a.state === state) &&
          (needle === "" || identityOf(a).toLowerCase().includes(needle) || a.name.toLowerCase().includes(needle)),
      ),
    [agents, provenance, state, needle],
  );
  const view = useReveal(filtered, `${needle}|${provenance}|${state}|${agents.length}`, "name", SORT_VALUE);

  const columns: TableColumn<Entry>[] = [
    {
      id: "name",
      groupable: false,
      header: t("settings.tools.col.agent"),
      sortable: true,
      render: (a) => <span className="block truncate text-sm font-semibold" data-testid={`agent-row-name-${identityOf(a)}`}>{identityOf(a)}</span>,
    },
    {
      id: "provenance",
      groupable: false,
      header: t("settings.tools.col.provenance"),
      sortable: true,
      width: 150,
      render: (a) => <Pill tone="slate" size="sm">{a.provenance}</Pill>,
    },
    {
      id: "state",
      groupable: false,
      header: t("settings.tools.col.state"),
      sortable: true,
      width: 120,
      render: (a) => (a.state ? <Pill tone="blue" size="sm">{a.state}</Pill> : null),
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-3" data-testid="tools-agent-roster">
      <div className="flex shrink-0 flex-wrap items-center gap-3">
        <label className="block w-full max-w-sm" data-testid="tools-agent-search">
          <span className="sr-only">{t("settings.tools.byAgent.search")}</span>
          <SearchBar placeholder={t("settings.tools.byAgent.searchPlaceholder")} debounceMs={200} initialValue={term} onSearch={setTerm} onClear={() => setTerm("")} />
        </label>
        <QuickFilter label={t("settings.tools.col.provenance")} field="provenance" rows={agents} value={provenance} onChange={setProvenance} />
        <QuickFilter label={t("settings.tools.col.state")} field="state" rows={agents} value={state} onChange={setState} />
      </div>
      <div className="min-h-[18rem] flex-1">
        <Table<Entry>
          variant="outlined"
          data={view.rows}
          columns={columns}
          rowKey={identityOf}
          manualSorting
          corner="rounded-sm"
          sortState={view.sortState}
          onSortChange={view.onSortChange}
          onRowClick={(a) => onSelect(identityOf(a))}
          loading={loading}
          emptyState={
            error ? (
              <EmptyState title={t("settings.tools.byAgent.rosterFailed")} subtitle={error} />
            ) : (
              <EmptyState title={t(agents.length === 0 ? "components.agentPicker.noAgents" : "settings.tools.byAgent.noMatch")} />
            )
          }
          fullHeight
          infiniteScroll={view.infinite}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="agents-count">{t("settings.tools.footer", { shown: view.rows.length, total: view.total })}</div>
    </div>
  );
}
