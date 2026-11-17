import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, EmptyState, Pill, SearchBar, Select, StatCard, Table, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { PageTitle } from "../../../components/PageTitle";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError, rpcTyped } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { SeedSetPanel } from "./SeedSetPanel";
import { SeedPlanDialog } from "./SeedPlanDialog";
import { badgeFor, canApplyWithoutConfirm, describePlan, summarize, type SeedPlan, type SeedSetStatus } from "./seedModel";
import { statusText, stateText } from "./seedText";

export interface SystemRecordsPanelProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

/**
 * The System records panel — descriptor-driven, the S43-extra counterpart of
 * DaemonSettingsPanel on the same Settings screen.
 *
 * It lists every seed set from `seed.status`, plans and applies through
 * `seed.plan` / `seed.apply` and restores through `seed.restore`, and names
 * **no** set id, record key or state of its own: it renders whatever the daemon
 * returns, so a newly declared set appears with zero UI change. It never
 * optimistically updates — a write only changes a card once a fresh `seed.status`
 * replaces it — and never compares versions: a record's state is the daemon's
 * word, shown, not re-derived. A guard set is never one-clicked: every apply
 * funnels through `canApplyWithoutConfirm`, and a declined confirmation sends
 * nothing.
 */
/**
 * The System records page — descriptor-driven, the S43-extra counterpart of the Daemon settings page.
 *
 * What the app ships as rows the daemon keeps in its database — agents, policy corpus, blueprints and
 * so on — and whether the copy on this machine is current. A strip of stat cards says how many sets
 * there are and how many need a look; the table lists every seed set from `seed.status`; a click opens a
 * set to see its records, plan it and apply it (`seed.plan` / `seed.apply`) or restore one record
 * (`seed.restore`). The page names **no** set id, record key or state of its own: it renders whatever
 * the daemon returns, so a newly declared set appears with zero UI change. It never optimistically
 * updates — a write only changes a row once a fresh `seed.status` replaces it — and never compares
 * versions: a record's state is the daemon's word, shown, not re-derived. A guard set is never
 * one-clicked: every apply funnels through `canApplyWithoutConfirm`, a declined confirmation sends
 * nothing, and a refusal is a toast.
 */
export function SystemRecordsPanel({ controller, live, canWrite = live.state === "current" }: SystemRecordsPanelProps) {
  const { t } = useI18n();
  // `set` is a required filter in the contract; an empty set lists every set
  // (as `settings.list`'s empty area lists every area) — no set id is hardcoded.
  const { data, loading, error, refetch } = useRpcQuery("seed.status", { set: "" }, controller, live);
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();

  // The sets the page holds — seeded from the last `seed.status` and replaced, never optimistically,
  // by a fresh `seed.status` after a write.
  const [sets, setSets] = useState<SeedSetStatus[]>([]);
  const [openId, setOpenId] = useState<string | undefined>(undefined);
  const [planDialog, setPlanDialog] = useState<{ setId: string; plan: SeedPlan } | null>(null);
  const [q, setQ] = useState("");
  const [cls, setCls] = useState("");
  const [health, setHealth] = useState("");

  useEffect(() => {
    // A malformed or empty status must not crash the page: the daemon always answers `{ sets: [...] }`,
    // but a generic fake (or an older daemon) may not, and the rows simply stay empty.
    if (!data?.sets) return;
    setSets(data.sets);
  }, [data]);

  const openPlan = useCallback(
    async (setId: string) => {
      // A plan is a read: it never writes, so the dialog it opens is safe to show on its own. The side
      // panel steps aside first so the dialog is not drawn under it. `keys: []` plans the whole set.
      setOpenId(undefined);
      const result = await action.run(() => rpcTyped("seed.plan", { set: setId, keys: [] }), { errorTitle: t("settings.seed.planFailed", { set: setId }) });
      if (result) setPlanDialog({ setId, plan: result.plan });
    },
    [action, t],
  );

  const apply = useCallback(async () => {
    const dialog = planDialog;
    if (!dialog) return;
    const set = sets.find((candidate) => candidate.id === dialog.setId);
    if (!set) return;
    const summary = describePlan(dialog.plan);
    // The confirmation text is composed from the plan and the set the daemon
    // sent — its record keys and ops, its count and its `guard` class. It adds
    // no prose about what the set permits beyond what the daemon returned. It is
    // one string (the confirm modal renders it inside a <p>), so no block nesting.
    const recordsText = summary.decisions.map((decision) => `${decision.key} (${decision.op})`).join(", ");

    // The one safety gate: an inert set applies on the single click that opened
    // this path; a guard set never — it must pass the confirmation first.
    if (!canApplyWithoutConfirm(set)) {
      const confirmed = await confirm({
        title: t("settings.seed.confirmGuardTitle", { set: set.id }),
        description: t("settings.seed.confirmGuardBody", { count: summary.count, records: recordsText }),
        confirmValue: set.id,
        confirmValueLabel: t("settings.seed.confirmValueLabel"),
        confirmLabel: t("settings.seed.apply"),
      });
      if (!confirmed) return; // decline sends nothing
    }

    const result = await action.run(
      () => rpcTyped("seed.apply", { set: set.id, keys: [] }),
      { errorTitle: t("settings.seed.applyFailed", { set: set.id }) },
    );
    if (result) {
      // A success closes the plan and refreshes from `seed.status`, never from a
      // local guess — the card is replaced by the daemon's new state.
      setPlanDialog(null);
      refetch();
      notifications.push({ title: t("settings.seed.applyToast", { set: set.id }), tone: "success", source: "local" });
    }
    // A refusal is a toast; the control re-enables.
  }, [action, confirm, planDialog, refetch, sets, t]);

  const restore = useCallback(
    async (setId: string, key: string) => {
      const set = sets.find((candidate) => candidate.id === setId);
      const record = set?.records.find((candidate) => candidate.key === key);
      if (!set || !record) return;
      // The panel steps aside first: the confirm is a modal and must not sit under it.
      setOpenId(undefined);
      // Confirm the daemon's reason, naming the version the restore would land on.
      const confirmed = await confirm({
        title: t("settings.seed.restoreConfirmTitle", { set: set.id, key }),
        description: t("settings.seed.restoreConfirmBody", { reason: record.reason, version: record.availableVersion }),
        confirmValue: key,
        confirmValueLabel: t("settings.seed.confirmValueLabel"),
        confirmLabel: t("settings.seed.restore"),
      });
      if (!confirmed) return; // decline sends nothing
      const result = await action.run(
        () => rpcTyped("seed.restore", { set: setId, key }),
        { errorTitle: t("settings.seed.restoreFailed", { set: set.id, key }) },
      );
      if (result) {
        // A success refreshes from `seed.status`; the record takes the new state.
        refetch();
        notifications.push({ title: t("settings.seed.restoreToast", { set: set.id, key }), tone: "success", source: "local" });
      }
    },
    [action, confirm, refetch, sets, t],
  );

  const summaries = useMemo(() => new Map(sets.map((set) => [set.id, summarize(set)])), [sets]);
  const needle = q.trim().toLowerCase();
  const shown = sets.filter((set) => {
    if (cls !== "" && set.class !== cls) return false;
    const current = summaries.get(set.id)?.headline === "upToDate";
    if (health === "current" && !current) return false;
    if (health === "attention" && current) return false;
    return needle === "" || set.id.toLowerCase().includes(needle) || set.records.some((r) => r.key.toLowerCase().includes(needle));
  });
  const classes = [...new Set(sets.map((set) => set.class))];
  const upToDate = sets.filter((set) => summaries.get(set.id)?.headline === "upToDate").length;
  const open = sets.find((set) => set.id === openId);

  const columns: TableColumn<SeedSetStatus>[] = [
    {
      id: "set",
      groupable: false,
      header: t("settings.seed.col.set"),
      render: (set) => (
        <div className="w-0 min-w-full py-1" data-testid="seed-set-row">
          <span className="block truncate text-sm font-semibold">{set.id}</span>
          <span className="block text-xs opacity-60">{t("settings.seed.recordCount", { count: set.records.length })}</span>
        </div>
      ),
    },
    {
      id: "class",
      groupable: false,
      header: t("settings.seed.col.class"),
      width: 110,
      render: (set) => {
        const label = set.class === "inert" || set.class === "guard" ? t(`settings.seed.class.${set.class}`) : set.class;
        return <Pill size="sm" tone={set.class === "guard" ? "red" : set.class === "inert" ? "emerald" : "neutral"}>{label}</Pill>;
      },
    },
    {
      id: "status",
      groupable: false,
      header: t("settings.seed.col.status"),
      width: 330,
      render: (set) => {
        const summary = summaries.get(set.id) ?? summarize(set);
        return (
          <div className="py-1">
            <span className="block text-sm">{statusText(t, summary)}</span>
            <span className="mt-1 flex flex-wrap gap-1">
              {Object.entries(summary.counts).map(([state, count]) => (
                <Pill key={state} size="sm" tone={badgeFor(state).tone}>{count} {stateText(t, state)}</Pill>
              ))}
            </span>
          </div>
        );
      },
    },
    {
      id: "versions",
      groupable: false,
      header: t("settings.seed.col.versions"),
      width: 190,
      render: (set) => <span className="text-xs opacity-80">{t("settings.seed.available")} {set.availableVersion} · {t("settings.seed.applied")} {set.appliedVersion}</span>,
    },
    {
      id: "act",
      groupable: false,
      header: "",
      width: 100,
      render: (set) => (
        <Button
          variant="ghost"
          size="sm"
          disabled={!canWrite || action.busy}
          onClick={(event) => {
            event.stopPropagation();
            void openPlan(set.id);
          }}
          data-testid={`seed-plan-${set.id}`}
        >
          {t("settings.seed.plan")}
        </Button>
      ),
    },
  ];

  return (
    <div className="flex min-h-0 min-w-0 w-full flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden" data-testid="system-records-section">
      <ContentActions className="shrink-0" gap="md" align="end">
        <PageTitle title={t("settings.seed.title")} purpose={t("settings.seed.subtitle")} help={{ title: t("settings.seed.help.title"), content: t("settings.seed.help.body") }} />
      </ContentActions>

      {error ? (
        <ErrorPanel error={new RpcError(0, error)} onRetry={refetch} />
      ) : loading ? (
        <EmptyState title={t("settings.seed.loading")} />
      ) : sets.length === 0 ? (
        <EmptyState title={t("settings.seed.emptyTitle")} subtitle={t("settings.seed.emptyBody")} />
      ) : (
        <>
          {!canWrite && <Alert className="shrink-0" intent="warning" title={t("settings.seed.reconnecting")} />}
          <div className="grid shrink-0 gap-3" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 220px), 1fr))" }} data-testid="seed-strip">
            <StatCard variant="simple" size="sm" padding="sm" icon="Script" label={t("settings.seed.strip.sets")} value={sets.length} subtitle={t("settings.seed.strip.setsSub", { guard: sets.filter((s) => s.class === "guard").length })} className="min-w-0" />
            <StatCard variant="simple" size="sm" padding="sm" icon="Check" label={t("settings.seed.strip.current")} value={`${upToDate} / ${sets.length}`} subtitle={t("settings.seed.strip.currentSub")} className="min-w-0" />
            <StatCard variant="simple" size="sm" padding="sm" tone={sets.length - upToDate > 0 ? "amber" : undefined} icon="Warning" label={t("settings.seed.strip.attention")} value={sets.length - upToDate} subtitle={t("settings.seed.strip.attentionSub")} className="min-w-0" />
          </div>
          <p className="shrink-0 text-sm opacity-70">{t("settings.seed.hint")}</p>
          <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="seed-filters">
            <label className="block w-full max-w-sm">
              <span className="sr-only">{t("settings.seed.filter.q")}</span>
              <SearchBar placeholder={t("settings.seed.filter.q")} debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ("")} />
            </label>
            <div className="w-44 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.seed.col.class")} value={cls} onChange={(e) => setCls(e.target.value)} data-testid="seed-filter-class">
                <option value="">{t("settings.seed.col.class")}: {t("settings.seed.filter.any")}</option>
                {classes.map((c) => <option key={c} value={c}>{c}</option>)}
              </Select>
            </div>
            <div className="w-52 shrink-0">
              <Select variant="ghost" size="md" aria-label={t("settings.seed.col.status")} value={health} onChange={(e) => setHealth(e.target.value)} data-testid="seed-filter-health">
                <option value="">{t("settings.seed.col.status")}: {t("settings.seed.filter.any")}</option>
                <option value="current">{t("settings.seed.filter.current")}</option>
                <option value="attention">{t("settings.seed.filter.attention")}</option>
              </Select>
            </div>
          </div>
          <div className="flex min-h-[20rem] min-w-0 flex-1 flex-col gap-2">
            <div className="min-h-[16rem] min-w-0 flex-1">
              <Table<SeedSetStatus>
                variant="outlined"
                data={shown}
                columns={columns}
                rowKey={(set) => set.id}
                corner="rounded-sm"
                onRowClick={(set) => setOpenId(set.id)}
                emptyState={<div data-testid="seed-nomatch"><EmptyState title={t("settings.seed.noMatch")} /></div>}
                fullHeight
              />
            </div>
            <p className="shrink-0 text-xs opacity-70">{t("settings.seed.footer", { shown: shown.length, total: sets.length })}</p>
          </div>
        </>
      )}

      {open !== undefined && (
        <OverlaySidePanel title={open.id} titleClassName="text-xl font-semibold" variant="simple" width={600} onClose={() => setOpenId(undefined)}>
          <SeedSetPanel set={open} canWrite={canWrite} busy={action.busy} onPlan={() => void openPlan(open.id)} onRestore={(key) => void restore(open.id, key)} />
        </OverlaySidePanel>
      )}

      {planDialog && (() => {
        const set = sets.find((candidate) => candidate.id === planDialog.setId);
        return set ? (
          <SeedPlanDialog set={set} plan={planDialog.plan} canWrite={canWrite} busy={action.busy} onApply={() => void apply()} onClose={() => setPlanDialog(null)} />
        ) : null;
      })()}
      {modal}
    </div>
  );
}
