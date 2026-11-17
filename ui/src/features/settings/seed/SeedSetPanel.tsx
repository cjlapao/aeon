import { useI18n } from "@cjlapao/ui-kit";
import { Button, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { useReveal } from "../tools/useReveal";
import { badgeFor, isRestorable, summarize, type SeedRecordStatus, type SeedSetStatus } from "./seedModel";
import { stateText, statusText } from "./seedText";

/**
 * SeedSetPanel — one seed set opened, as three questions: **is it current?**, **what is in it?** and
 * **what would applying do?** Drawn entirely from the daemon's `seed.status` element.
 *
 * The panel names no id, key or state of its own: the id, the class, the available/applied versions and
 * every per-record state come from `set`, so a newly declared set (or a state a later slice adds) renders
 * with zero change. It never decides safety — that is `canApplyWithoutConfirm` in the page — and never
 * compares versions: a record's state is the daemon's word, shown, not re-derived from `availableVersion`
 * against `appliedVersion`. Every non-applied record shows the daemon's reason verbatim; a failed one
 * shows it as an alert.
 */
export interface SeedSetPanelProps {
  set: SeedSetStatus;
  /** Writes (plan→apply, restore) are refused while the connection is not current. */
  canWrite: boolean;
  /** A write is in flight — the controls stay disabled. */
  busy: boolean;
  onPlan: () => void;
  onRestore: (key: string) => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function SeedSetPanel({ set, canWrite, busy, onPlan, onRestore }: SeedSetPanelProps) {
  const { t } = useI18n();
  const summary = summarize(set);
  const classLabel = set.class === "inert" || set.class === "guard" ? t(`settings.seed.class.${set.class}`) : set.class;
  const classTone = set.class === "guard" ? "red" : set.class === "inert" ? "emerald" : "neutral";

  const view = useReveal(set.records, set.id, "key", (record, column) => (column === "state" ? record.state : record.key));

  const columns: TableColumn<SeedRecordStatus>[] = [
    {
      id: "key",
      groupable: false,
      header: t("settings.seed.col.record"),
      sortable: true,
      render: (record) => <span className="block truncate text-sm font-medium" data-testid="seed-record">{record.key}</span>,
    },
    {
      id: "state",
      groupable: false,
      header: t("settings.seed.col.status"),
      sortable: true,
      width: 130,
      render: (record) => <Pill size="sm" tone={badgeFor(record.state).tone}>{stateText(t, record.state)}</Pill>,
    },
    {
      id: "versions",
      groupable: false,
      header: t("settings.seed.col.versions"),
      width: 130,
      render: (record) => <span className="text-xs opacity-70">{record.availableVersion} / {record.appliedVersion}</span>,
    },
    {
      id: "reason",
      groupable: false,
      header: t("settings.seed.col.reason"),
      // Every non-applied record shows the daemon's reason verbatim.
      render: (record) =>
        record.state !== "applied" && record.reason !== "" ? (
          <span className={`block py-1 text-xs ${record.state === "failed" ? "text-rose-500" : "opacity-80"}`}>{record.reason}</span>
        ) : null,
    },
    {
      id: "act",
      groupable: false,
      header: "",
      width: 100,
      render: (record) =>
        isRestorable(record.state) ? (
          <Button variant="ghost" size="sm" onClick={() => onRestore(record.key)} disabled={!canWrite || busy}>{t("settings.seed.restore")}</Button>
        ) : null,
    },
  ];

  return (
    <div className="flex h-[calc(100vh-8rem)] min-h-[30rem] flex-col gap-5" data-testid="seed-set-panel">
      <section className="shrink-0 space-y-2">
        <h3 className={H}>{t("settings.seed.q.current")}</h3>
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <Pill size="sm" tone={classTone}>{classLabel}</Pill>
          <span data-testid="seed-set-status">{statusText(t, summary)}</span>
        </p>
        <p className="text-sm opacity-80">{t("settings.seed.available")}: {set.availableVersion} · {t("settings.seed.applied")}: {set.appliedVersion}</p>
        {set.class === "guard" && <p className="text-xs opacity-70">{t("settings.seed.guardNote")}</p>}
      </section>

      <section className="flex min-h-0 flex-1 flex-col gap-2">
        <h3 className={`${H} shrink-0`}>{t("settings.seed.q.inside")}</h3>
        <div className="min-h-[10rem] min-w-0 flex-1" data-testid="seed-records">
          <Table<SeedRecordStatus>
            variant="outlined"
            data={view.rows}
            columns={columns}
            rowKey={(record) => record.key}
            manualSorting
            corner="rounded-sm"
            sortState={view.sortState}
            onSortChange={view.onSortChange}
            emptyState={<EmptyState title={t("settings.seed.noRecords")} />}
            fullHeight
            infiniteScroll={view.infinite}
          />
        </div>
        <p className="shrink-0 text-xs opacity-70">{t("settings.seed.recordsFooter", { shown: view.rows.length, total: view.total })}</p>
      </section>

      <section className="shrink-0 space-y-2 border-t border-white/10 pt-4">
        <h3 className={H}>{t("settings.seed.q.apply")}</h3>
        <p className="text-sm opacity-80">{t("settings.seed.applyBody")}</p>
        <Button size="sm" onClick={onPlan} disabled={!canWrite || busy} data-testid="seed-panel-plan">{t("settings.seed.planThis")}</Button>
      </section>
    </div>
  );
}
