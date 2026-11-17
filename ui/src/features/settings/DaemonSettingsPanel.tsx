import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, ContentActions, EmptyState, Pill, SearchBar, Select, Table, type TableColumn } from "../../components/kit";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import { PageTitle } from "../../components/PageTitle";
import { RelativeTime } from "../../components/RelativeTime";
import { ErrorPanel } from "../../components/ErrorPanel";
import { RpcError, rpcTyped } from "../../lib/bridge";
import { useRpcQuery } from "../../lib/useRpcQuery";
import { useRpcAction } from "../../lib/useRpcAction";
import { useTypeToConfirm } from "../../lib/useConfirm";
import { notifications } from "../../lib/notifications";
import type { LiveController, LiveState } from "../../lib/live";
import { controlKindFor, hasEnvInvalid, hasStoredInvalid, parseDraft, resetDisabled, sourceBadge, type SettingView } from "./daemonSettings";
import { DaemonSettingPanel } from "./DaemonSettingPanel";
import { useReveal } from "./tools/useReveal";

export interface DaemonSettingsPanelProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

/**
 * The Daemon settings page — descriptor-driven, and the first thing in the app that writes a
 * daemon-side setting from the Settings route (P34).
 *
 * A table of every declared setting from `settings.list` — what it is, its area, the value in force and
 * which layer that value comes from (default, saved, or environment) — that opens a setting in a side
 * panel to change it (`settings.set`) or start over (`settings.unset`). The page holds **no**
 * client-side cache beyond the row state seeded from the last response: a row is replaced by a
 * response's `setting`, never optimistically, and nothing is written to `localStorage`/
 * `sessionStorage`. A newly declared key appears here with zero UI change (CS10). The filters narrow
 * the rows already fetched; a refusal is a toast, and a sensitive key confirms by typing its name first.
 */
export function DaemonSettingsPanel({ controller, live, canWrite = live.state === "current" }: DaemonSettingsPanelProps) {
  const { t } = useI18n();
  // `area` is a required filter in the contract; an empty area lists every area.
  const { data, loading, error, refetch } = useRpcQuery("settings.list", { area: "" }, controller, live);
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();

  const [rows, setRows] = useState<SettingView[]>([]);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [parseErrors, setParseErrors] = useState<Record<string, string>>({});
  const [openKey, setOpenKey] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");
  const [area, setArea] = useState("");
  const [layer, setLayer] = useState("");

  useEffect(() => {
    // A malformed or empty list response must not crash the page: the daemon always answers
    // `{ settings: [...] }`, but a generic fake (or an older daemon) may not.
    if (!data?.settings) return;
    setRows(data.settings);
    const nextDrafts: Record<string, string> = {};
    for (const view of data.settings) {
      const kind = controlKindFor(view);
      if (kind === "number" || kind === "text") nextDrafts[view.key] = view.value;
    }
    setDrafts(nextDrafts);
    setParseErrors({});
  }, [data]);

  const setDraft = (key: string, value: string) => setDrafts((current) => ({ ...current, [key]: value }));

  const commit = useCallback(
    async (view: SettingView, text: string) => {
      const parsed = parseDraft(view, text);
      if (!parsed.ok || parsed.value === undefined) {
        // Malformed: shown inline, never sent. The draft stays so it can be fixed.
        setParseErrors((current) => ({ ...current, [view.key]: parsed.reason ?? "" }));
        return;
      }
      const value = parsed.value;
      setParseErrors((current) => {
        const next = { ...current };
        delete next[view.key];
        return next;
      });

      if (view.sensitive) {
        // The panel closes first: the confirm is a modal and must not sit under the panel it came from.
        setOpenKey(undefined);
        const confirmed = await confirm({
          title: view.description || view.key,
          description: t("settings.daemon.confirmBody", { key: view.key, from: view.value, to: text }),
          confirmValue: view.key,
          confirmValueLabel: t("settings.daemon.confirmValueLabel"),
          confirmLabel: t("settings.daemon.confirmAction"),
        });
        if (!confirmed) {
          // Declined: send nothing, restore the control to the current value.
          setDraft(view.key, view.value);
          return;
        }
      }

      const result = await action.run(() => rpcTyped("settings.set", { key: view.key, value }), { errorTitle: t("settings.daemon.setFailed", { key: view.key }) });
      if (result) {
        // The row takes the response's view — not the draft.
        setRows((current) => current.map((row) => (row.key === view.key ? result.setting : row)));
        setDraft(view.key, result.setting.value);
        notifications.push({
          title: t(result.changed ? "settings.daemon.savedToast" : "settings.daemon.unchangedToast", { key: view.key }),
          tone: "success",
          source: "local",
        });
      } else {
        // A refusal: restore the control; the toast carries the daemon's words.
        setDraft(view.key, view.value);
      }
    },
    [action, confirm, t],
  );

  const reset = useCallback(
    async (view: SettingView) => {
      if (resetDisabled(view)) return;
      const result = await action.run(() => rpcTyped("settings.unset", { key: view.key }), { errorTitle: t("settings.daemon.resetFailed", { key: view.key }) });
      if (result) {
        setRows((current) => current.map((row) => (row.key === view.key ? result.setting : row)));
        setDraft(view.key, result.setting.value);
        notifications.push({ title: t("settings.daemon.resetToast", { key: view.key }), tone: "success", source: "local" });
      }
    },
    [action, t],
  );

  const areas = useMemo(() => [...new Set(rows.map((row) => row.area))], [rows]);
  const needle = q.trim().toLowerCase();
  const shown = rows.filter((row) => {
    if (area !== "" && row.area !== area) return false;
    if (layer === "attention" ? !(hasEnvInvalid(row) || hasStoredInvalid(row)) : layer !== "" && sourceBadge(row) !== layer) return false;
    return needle === "" || `${row.key} ${row.description}`.toLowerCase().includes(needle);
  });
  const view = useReveal(shown, `${q}|${area}|${layer}|${rows.length}`, "area", (row, column) => (column === "key" ? row.key : column === "layer" ? sourceBadge(row) : column === "changed" ? row.updatedAt : row.area));
  const open = rows.find((row) => row.key === openKey);

  const columns: TableColumn<SettingView>[] = [
    {
      id: "key",
      groupable: false,
      header: t("settings.daemon.col.setting"),
      sortable: true,
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`setting-row-${row.key}`}>
          <span className="flex items-center gap-2">
            <span className="truncate text-sm font-semibold">{row.key}</span>
            {row.sensitive && <Pill size="sm" tone="rose">{t("settings.daemon.sensitive")}</Pill>}
          </span>
          {row.description !== "" && <span className="block truncate text-xs opacity-60">{row.description}</span>}
        </div>
      ),
    },
    { id: "area", groupable: false, header: t("settings.daemon.col.area"), sortable: true, width: 120, render: (row) => <Pill size="sm" tone="slate">{row.area}</Pill> },
    {
      id: "value",
      groupable: false,
      header: t("settings.daemon.col.value"),
      width: 210,
      render: (row) => <code className="block truncate text-xs" data-testid={`setting-value-${row.key}`}>{row.value === "" ? "—" : row.value}</code>,
    },
    {
      id: "layer",
      groupable: false,
      header: t("settings.daemon.col.layer"),
      sortable: true,
      width: 190,
      render: (row) => (
        <span className="flex flex-wrap gap-1">
          <Pill size="sm" tone={sourceBadge(row) === "environment" ? "amber" : sourceBadge(row) === "saved" ? "blue" : "slate"} data-testid={`setting-layer-${row.key}`}>{t(`settings.daemon.source.${sourceBadge(row)}`)}</Pill>
          {(hasEnvInvalid(row) || hasStoredInvalid(row)) && <Pill size="sm" tone="orange" data-testid={`setting-invalid-${row.key}`}>{t("settings.daemon.invalid")}</Pill>}
        </span>
      ),
    },
    {
      id: "changed",
      groupable: false,
      header: t("settings.daemon.col.changed"),
      sortable: true,
      width: 150,
      render: (row) => (row.updatedAt === "" ? <span className="text-xs opacity-60">—</span> : <span className="text-xs opacity-80"><RelativeTime iso={row.updatedAt} /></span>),
    },
  ];

  return (
    <div className="flex min-h-0 min-w-0 w-full flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden" id="daemon-settings" data-testid="daemon-settings-panel">
      <ContentActions className="shrink-0" gap="md" align="end">
        <PageTitle title={t("settings.daemon.title")} purpose={t("settings.daemon.subtitle")} help={{ title: t("settings.daemon.help.title"), content: t("settings.daemon.help.body") }} />
      </ContentActions>

      {error ? (
        <ErrorPanel error={new RpcError(0, error)} onRetry={refetch} />
      ) : loading ? (
        <EmptyState title={t("settings.daemon.loading")} />
      ) : rows.length === 0 ? (
        <EmptyState title={t("settings.daemon.emptyTitle")} subtitle={t("settings.daemon.emptyBody")} />
      ) : (
        <>
          {!canWrite && <Alert className="shrink-0" intent="warning" title={t("settings.daemon.reconnecting")} />}
          <p className="shrink-0 text-sm opacity-70">{t("settings.daemon.hint")}</p>
          <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="daemon-settings-filters">
            <label className="block w-full max-w-sm" data-testid="daemon-filter-q">
              <span className="sr-only">{t("settings.daemon.filter.q")}</span>
              <SearchBar placeholder={t("settings.daemon.filter.q")} debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ("")} />
            </label>
            <div className="w-48 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.daemon.col.area")} value={area} onChange={(e) => setArea(e.target.value)} data-testid="daemon-filter-area">
                <option value="">{t("settings.daemon.col.area")}: {t("settings.daemon.filter.any")}</option>
                {areas.map((a) => <option key={a} value={a}>{a}</option>)}
              </Select>
            </div>
            <div className="w-52 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.daemon.col.layer")} value={layer} onChange={(e) => setLayer(e.target.value)} data-testid="daemon-filter-layer">
                <option value="">{t("settings.daemon.col.layer")}: {t("settings.daemon.filter.any")}</option>
                <option value="default">{t("settings.daemon.source.default")}</option>
                <option value="saved">{t("settings.daemon.source.saved")}</option>
                <option value="environment">{t("settings.daemon.source.environment")}</option>
                <option value="attention">{t("settings.daemon.filter.attention")}</option>
              </Select>
            </div>
          </div>
          <div className="flex min-h-[22rem] min-w-0 flex-1 flex-col gap-2">
            <div className="min-h-[18rem] min-w-0 flex-1">
              <Table<SettingView>
                variant="outlined"
                data={view.rows}
                columns={columns}
                rowKey={(row) => row.key}
                manualSorting
                corner="rounded-sm"
                sortState={view.sortState}
                onSortChange={view.onSortChange}
                onRowClick={(row) => setOpenKey(row.key)}
                emptyState={<div data-testid="daemon-settings-nomatch"><EmptyState title={t("settings.daemon.noMatch")} /></div>}
                fullHeight
                infiniteScroll={view.infinite}
              />
            </div>
            <p className="shrink-0 text-xs opacity-70" data-testid="daemon-settings-count">{t("settings.daemon.footer", { shown: view.rows.length, total: view.total })}</p>
          </div>
        </>
      )}

      {open !== undefined && (
        <OverlaySidePanel title={open.key} titleClassName="text-xl font-semibold" variant="simple" width={560} onClose={() => setOpenKey(undefined)}>
          <DaemonSettingPanel
            view={open}
            canWrite={canWrite}
            busy={action.busy}
            draft={drafts[open.key] ?? open.value}
            parseError={parseErrors[open.key]}
            onDraftChange={(text) => setDraft(open.key, text)}
            onCommit={(text) => void commit(open, text)}
            onReset={() => void reset(open)}
          />
        </OverlaySidePanel>
      )}
      {modal}
    </div>
  );
}
