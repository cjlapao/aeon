import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill, SearchBar, Select, Table, type TableColumn } from "../../../components/kit";
import { DetailSidePanel } from "./DetailSidePanel";
import { doorChips, emptySurfaceFilter, surfaceCountChips, surfaceFilterIsEmpty, type SurfaceCounts, type SurfaceDetail, type SurfaceFilterState, type SurfaceRow } from "./toolsModel";
import { useReveal } from "./useReveal";

// The Surface tab: what the daemon will answer, on which door, and what it says about each method.
// The summary on every row is the daemon's — the same sentence `surface.list` sends to any client
// and the CLI prints. The kind, the door chips and the two badges are computed from the entry's own
// fields, and `doorChips` keeps the one case a screen is most tempted to lie about: a narrow flag the
// daemon could not decide ("unknown") renders as the daemon's own word, never as "not on narrow".
// The counts line stays beside the filtered answer: `counts.unfiltered` is how a narrow listing
// avoids reading as the whole surface.

export interface SurfaceListProps {
  rows: SurfaceRow[];
  counts: SurfaceCounts | undefined;
  statement?: string;
  filter: SurfaceFilterState;
  onFilter: (next: SurfaceFilterState) => void;
  selected?: string;
  onSelect: (method: string) => void;
  loading: boolean;
  error?: string;
}

const SORT_VALUE = (row: SurfaceRow, column: string): string => (column === "kind" ? row.kind : row.method);

export function SurfaceList({ rows, counts, statement, filter, onFilter, onSelect, loading, error }: SurfaceListProps) {
  const { t } = useI18n();
  const chips = surfaceCountChips(counts);
  const set = (patch: Partial<SurfaceFilterState>) => onFilter({ ...filter, ...patch });
  const view = useReveal(rows, `${filter.kind}|${filter.door}|${filter.humanDoorOnly}|${filter.q}|${rows.length}`, "method", SORT_VALUE);

  const columns: TableColumn<SurfaceRow>[] = [
    {
      id: "method",
      groupable: false,
      header: t("settings.tools.col.method"),
      sortable: true,
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`surface-row-${row.method}`}>
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-semibold" data-testid={`surface-method-${row.method}`}>{row.method}</span>
            {row.humanDoorOnly ? <Pill tone="indigo" size="sm" data-testid={`surface-human-only-${row.method}`}>{t("settings.tools.surface.badges.humanDoorOnly")}</Pill> : null}
            {row.debug ? <Pill tone="orange" size="sm" data-testid={`surface-debug-${row.method}`}>{t("settings.tools.surface.badges.debug")}</Pill> : null}
          </div>
          <div className="truncate text-xs opacity-70" title={row.summary} data-testid={`surface-summary-${row.method}`}>{row.summary}</div>
        </div>
      ),
    },
    {
      id: "kind",
      groupable: false,
      header: t("settings.tools.col.kind"),
      sortable: true,
      width: 120,
      render: (row) => <Pill tone={kindTone(row.kind)} size="sm" data-testid={`surface-kind-${row.method}`}>{row.kind}</Pill>,
    },
    {
      id: "doors",
      groupable: false,
      header: t("settings.tools.col.doors"),
      width: 330,
      render: (row) => (
        <span className="flex flex-wrap gap-1" data-testid={`surface-doors-${row.method}`}>
          {doorChips(row).map((chip) => (
            <Pill key={chip.id} tone={chip.tone} size="sm" data-testid={`surface-door-${row.method}-${chip.id}`}>
              {t(chip.labelKey)}{chip.value ? `: ${chip.value}` : ""}
            </Pill>
          ))}
        </span>
      ),
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-3" data-testid="tools-surface">
      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="surface-filters">
        <label className="block w-full max-w-sm">
          <span className="sr-only">{t("settings.tools.filters.search")}</span>
          <SearchBar placeholder={t("settings.tools.surface.filters.searchPlaceholder")} debounceMs={200} initialValue={filter.q} onSearch={(q) => set({ q })} onClear={() => set({ q: "" })} />
        </label>
        <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label={t("settings.tools.surface.filters.kind")} value={filter.kind} disabled={loading} onChange={(e) => set({ kind: e.target.value })}>
          <option value="">{t("settings.tools.filters.anyOf", { what: t("settings.tools.surface.filters.kind") })}</option>
          {Object.keys(counts?.byKind ?? {}).map((kind) => <option key={kind} value={kind}>{kind}</option>)}
        </Select></div>
        <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label={t("settings.tools.surface.filters.door")} value={filter.door} disabled={loading} onChange={(e) => set({ door: e.target.value })}>
          <option value="">{t("settings.tools.filters.anyOf", { what: t("settings.tools.surface.filters.door") })}</option>
          {Object.keys(counts?.byDoor ?? {}).map((door) => <option key={door} value={door}>{door}</option>)}
        </Select></div>
        <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label={t("settings.tools.surface.filters.humanOnly")} value={filter.humanDoorOnly} disabled={loading} onChange={(e) => set({ humanDoorOnly: e.target.value })}>
          <option value="">{t("settings.tools.filters.anyOf", { what: t("settings.tools.surface.filters.humanOnly") })}</option>
          <option value="true">{t("settings.tools.surface.filters.humanOnly")}: {t("settings.tools.filters.availableYes")}</option>
          <option value="false">{t("settings.tools.surface.filters.humanOnly")}: {t("settings.tools.filters.availableNo")}</option>
        </Select></div>
        {!surfaceFilterIsEmpty(filter) && <Button size="sm" variant="ghost" onClick={() => onFilter(emptySurfaceFilter())} data-testid="surface-filters-clear">{t("settings.tools.filters.clear")}</Button>}
      </div>

      {counts ? (
        <div className="flex shrink-0 flex-wrap items-center gap-2 text-xs opacity-70" data-testid="surface-counts">
          <span data-testid="surface-count-total">{t("settings.tools.surface.countTotal", { total: counts.total, unfiltered: counts.unfiltered })}</span>
          {chips.map((chip) => <span key={chip.label} data-testid={`surface-count-${chip.label}`}>{chip.label}: {chip.value}</span>)}
          {!surfaceFilterIsEmpty(filter) ? <span data-testid="surface-count-filtered">{t("settings.tools.surface.countFiltered")}</span> : null}
        </div>
      ) : null}

      {error ? <Alert className="shrink-0" intent="danger" title={t("settings.tools.surface.failed")} description={error} data-testid="surface-error" /> : null}

      <div className="min-h-[18rem] flex-1">
        <Table<SurfaceRow>
          variant="outlined"
          data={view.rows}
          columns={columns}
          rowKey={(row) => row.method}
          manualSorting
          corner="rounded-sm"
          sortState={view.sortState}
          onSortChange={view.onSortChange}
          onRowClick={(row) => onSelect(row.method)}
          loading={loading}
          loadingMessage={t("settings.tools.surface.loading")}
          emptyState={
            <div data-testid="surface-empty">
              {statement ? <p className="text-sm opacity-80" data-testid="surface-empty-statement">{statement}</p> : <EmptyState title={t("settings.tools.surface.empty")} />}
            </div>
          }
          fullHeight
          infiniteScroll={view.infinite}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="surface-footer">{t("settings.tools.footer", { shown: view.rows.length, total: view.total })}</div>
    </div>
  );
}

/** The panel for one method (`surface.get`). Every word is the entry's, and the door chips are the
 *  same function the list uses, so the list and the panel cannot disagree about a door. */
export interface SurfaceDrawerProps {
  method: string;
  entry: SurfaceDetail | undefined;
  loading: boolean;
  error?: string;
  onClose: () => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function SurfaceDrawer({ method, entry, loading, error, onClose }: SurfaceDrawerProps) {
  const { t } = useI18n();
  if (loading) return <DetailSidePanel title={method} testId="surface-drawer" onClose={onClose}><EmptyState title={t("settings.tools.surface.drawer.loading")} /></DetailSidePanel>;
  if (error) return <DetailSidePanel title={method} testId="surface-drawer" onClose={onClose}><Alert intent="danger" title={t("settings.tools.surface.drawer.failed")} description={error} /></DetailSidePanel>;
  if (!entry) return <DetailSidePanel title={method} testId="surface-drawer" onClose={onClose}><EmptyState title={t("settings.tools.surface.drawer.none")} /></DetailSidePanel>;

  const yes = (v: boolean | undefined) => (v ? t("settings.tools.filters.availableYes") : t("settings.tools.filters.availableNo"));
  return (
    <DetailSidePanel title={entry.method} testId="surface-drawer" onClose={onClose}>
      <div className="space-y-8">
        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.what")}</h3>
          <p className="text-sm" data-testid="surface-drawer-summary">{entry.summary}</p>
          <dl className="space-y-1 text-sm">
            <div><dt className="inline opacity-70">{t("settings.tools.surface.drawer.kind")}: </dt><dd className="inline"><Pill tone={kindTone(entry.kind)} size="sm">{entry.kind}</Pill></dd></div>
            <div><dt className="inline opacity-70">{t("settings.tools.surface.drawer.humanOnly")}: </dt><dd className="inline">{yes(entry.humanDoorOnly)}</dd></div>
            <div><dt className="inline opacity-70">{t("settings.tools.surface.drawer.debug")}: </dt><dd className="inline">{yes(entry.debug)}</dd></div>
            <div><dt className="inline opacity-70">{t("settings.tools.surface.drawer.requiresInitialize")}: </dt><dd className="inline">{yes(entry.requiresInitialize)}</dd></div>
          </dl>
        </section>
        <section className="space-y-3">
          <h3 className={H}>{t("settings.tools.q.where")}</h3>
          <div className="flex flex-wrap gap-1" data-testid="surface-drawer-doors">
            {doorChips(entry).map((chip) => (
              <Pill key={chip.id} tone={chip.tone} size="sm" data-testid={`surface-drawer-door-${chip.id}`}>{t(chip.labelKey)}{chip.value ? `: ${chip.value}` : ""}</Pill>
            ))}
          </div>
        </section>
        <p className="text-xs opacity-60" data-testid="surface-drawer-note">{t("settings.tools.surface.drawer.note")}</p>
      </div>
    </DetailSidePanel>
  );
}

/** The kinds are a closed set the DAEMON owns; the tones are this screen's. An unknown kind is slate. */
function kindTone(kind: string): "green" | "rose" | "amber" | "slate" {
  if (kind.includes("read")) return "green";
  if (kind.includes("write")) return "rose";
  if (kind.includes("control")) return "amber";
  return "slate";
}
