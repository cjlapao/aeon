import { useMemo } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import type { LiveController, LiveState } from "../../../lib/live";
import { useReveal } from "../tools/useReveal";
import { useAutomationHealth, useAutomationShow } from "./automationRpc";
import {
  healthKey, needsAttention, outcomePill, rowKey, stateWord, stateWordTone,
  type AutomationList, type AutomationRow, type HealthView,
} from "./automationModel";

/**
 * AutomationList — the unified list (AN10: `automation.list` with the filters as PARAMS; the rows
 * shown are exactly what the daemon answered), as the Agents page shows its rows: one table that
 * fills the height and reveals more as it is scrolled.
 *
 * The plain-words sentence per row is the DAEMON's: `automation.list` rows carry no description
 * field, so each row asks `automation.show` for its own answer — the one sentence
 * `automationview.Describe` wrote, quoted and never re-derived here (AN11). Health arrives once for
 * the whole plane (`automation.health`, joined by source:id): the blueprint dot, the canFire verdict
 * and the "needs attention" count are all the daemon's verdicts.
 *
 * Empty is honest twice over (AN5): when the daemon answered with a `statement` — the filter's
 * sentence or the store's — that is what renders, byte for byte; the UI writes its own line only for
 * a response with no statement, which is not an empty the daemon chose.
 */
export interface AutomationListProps {
  list: AutomationList | undefined;
  error: string | undefined;
  loading: boolean;
  onOpen: (source: string, id: string) => void;
  controller: LiveController;
  live: LiveState;
}

const SORT_VALUE = (row: AutomationRow, column: string): string => {
  switch (column) {
    case "source": return row.source;
    case "state": return stateWord(row);
    default: return row.name;
  }
};

export function AutomationList({ list, error, loading, onOpen, controller, live }: AutomationListProps) {
  const { t } = useI18n();
  const health = useAutomationHealth(controller, live);
  const healthByName = useMemo(() => {
    const map = new Map<string, HealthView>();
    for (const h of health.data?.health ?? []) map.set(healthKey(h), h);
    return map;
  }, [health.data]);

  const rows = useMemo(() => list?.automations ?? [], [list]);
  const attention = rows.reduce((n, r) => n + (needsAttention(healthByName.get(rowKey(r.source, r.id)), r) ? 1 : 0), 0);
  const view = useReveal(rows, String(rows.length), "name", SORT_VALUE);

  const columns: TableColumn<AutomationRow>[] = [
    {
      id: "name",
      groupable: false,
      header: t("settings.automation.col.automation"),
      sortable: true,
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`automation-row-${row.id}`}>
          <div className="flex min-w-0 items-baseline gap-2">
            <span className="truncate text-sm font-semibold">{row.name}</span>
            <code className="shrink-0 text-xs opacity-60">{row.id}</code>
          </div>
          <RowDescription source={row.source} id={row.id} controller={controller} live={live} />
        </div>
      ),
    },
    {
      id: "source",
      groupable: false,
      header: t("settings.automation.col.kind"),
      sortable: true,
      width: 110,
      render: (row) => <Pill size="sm" tone="slate" data-testid={`row-source-${row.id}`}>{row.source}</Pill>,
    },
    {
      id: "state",
      groupable: false,
      header: t("settings.automation.col.state"),
      sortable: true,
      width: 120,
      render: (row) => {
        const state = stateWord(row);
        return <Pill size="sm" tone={stateWordTone[state]} data-testid={`row-state-${row.id}`}>{t(`settings.automation.state.${state}`)}</Pill>;
      },
    },
    {
      id: "blueprint",
      groupable: false,
      header: t("settings.automation.col.blueprint"),
      width: 260,
      render: (row) => <BlueprintDot row={row} health={healthByName.get(rowKey(row.source, row.id))} />,
    },
    {
      id: "last",
      groupable: false,
      header: t("settings.automation.col.last"),
      width: 230,
      render: (row) => {
        const last = row.lastFire;
        return last === undefined || last === null ? (
          <span className="text-xs opacity-60" data-testid={`row-last-${row.id}`}>{t("settings.automation.list.neverFired")}</span>
        ) : (
          <span className="flex items-center gap-2 text-xs" data-testid={`row-last-${row.id}`}>
            <Pill size="sm" tone={outcomePill(last.outcome).tone}>{last.outcome}</Pill>
            <span className="opacity-70"><RelativeTime iso={last.at} /></span>
          </span>
        );
      },
    },
  ];

  const emptyTitle = list?.statement ?? (loading ? t("settings.automation.list.loading") : t("settings.automation.list.noStatement"));

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="automation-list">
      {attention > 0 && (
        <div className="shrink-0">
          <Pill size="sm" tone="amber" data-testid="attention-count">{t("settings.automation.list.needsAttention")} {attention}</Pill>
        </div>
      )}
      {error !== undefined && <Alert className="shrink-0" intent="danger" title={error} data-testid="automation-list-error" />}
      <div className="min-h-[18rem] flex-1">
        <Table<AutomationRow>
          variant="outlined"
          data={view.rows}
          columns={columns}
          rowKey={(row) => rowKey(row.source, row.id)}
          manualSorting
          corner="rounded-sm"
          sortState={view.sortState}
          onSortChange={view.onSortChange}
          onRowClick={(row) => onOpen(row.source, row.id)}
          loading={loading && list === undefined}
          emptyState={error !== undefined ? <span /> : <div data-testid="automation-list-empty"><EmptyState showIcon={false} title={emptyTitle} /></div>}
          fullHeight
          infiniteScroll={view.infinite}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="automation-count">{t("settings.automation.footer", { shown: view.rows.length, total: view.total })}</div>
    </div>
  );
}

/** The blueprint with its health dot: `ok` / `missing` / `invalid` / `unverifiable` — text AND
 *  colour, the daemon's state word, never the UI's guess. No health answer yet draws the
 *  unverifiable colour: absence of an answer is honestly not-ok without pretending to be invalid. */
function BlueprintDot({ row, health }: { row: AutomationRow; health: HealthView | undefined }) {
  const word = health === undefined ? "unverifiable" : (health.blueprint?.state ?? "unverifiable");
  const tone = word === "ok" ? "green" : word === "unverifiable" ? "amber" : "red";
  return <Pill size="sm" tone={tone} data-testid={`row-blueprint-${row.id}`} title={row.blueprint}>{row.blueprint} · {word}</Pill>;
}

/** One row's plain-words sentence, asked of the door that owns it (`automation.show`). Renders
 *  nothing until the daemon answers — the absence is honest, a client-composed sentence would not be. */
function RowDescription({ source, id, controller, live }: { source: string; id: string; controller: LiveController; live: LiveState }) {
  const show = useAutomationShow(source, id, controller, live);
  const description = show.data?.description;
  if (description === undefined || description === "") return null;
  return <p className="truncate text-xs opacity-70" title={description} data-testid={`row-description-${id}`}>{description}</p>;
}
