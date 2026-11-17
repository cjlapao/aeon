import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, FormField, Pill, Select, Table, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import type { LiveController, LiveState } from "../../../lib/live";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { AuthorityConfirm } from "../automation/AuthorityConfirm";
import { useComponentRoots } from "../../projects/useComponentRoots";
import { disableSchedulerRoot, setSchedulerRoot } from "./continuousRpc";
import { hasDetail, repoStateTone, type SchedulerRepo, type SchedulerStatus } from "./continuousModel";
import { RootForm } from "./RootForm";

/**
 * RepositoriesTab — the repositories the scheduler may start work in (S27 T10 behaviour 1/2).
 *
 * One row per scheduler row the daemon holds, with the state word, its reason and its detail
 * untouched — a closed window carries its instant inside the daemon's own `detail`, and this tab
 * adds no sentence about when anything opens. Clicking a row opens its panel: the same facts, the
 * enable / disable act, and the template form. A repository nobody has enrolled yet is added from
 * the toolbar (it picks among the component roots the daemon already knows and saves through the
 * same `scheduler.root.set` door). Enabling a repository is an authority WIDENING (CN6): it always
 * confirms with the daemon's own statement, and declining sends nothing.
 *
 * The names come from the projects folder's own `component.list` read (`useComponentRoots`); a
 * scheduler row whose path matches no component still renders, under its own path.
 */
export interface RepositoriesTabProps {
  view: SchedulerStatus | undefined;
  error: string | undefined;
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  onChanged: () => void;
}

type Open = { kind: "row"; path: string } | { kind: "new"; path: string } | undefined;

const BLANK = (componentRoot: string): SchedulerRepo => ({
  componentRoot,
  enabled: false,
  lastStartAt: "",
  minPriority: "",
  rootId: "",
  state: { kind: "off", reason: "", detail: "" },
  template: { blueprint: "", tech: "", with: { brief_path: "" } },
  window: { days: "", end: "", start: "", zone: "" },
});

export function RepositoriesTab({ view, error, canWrite, controller, live, onChanged }: RepositoriesTabProps) {
  const { t } = useI18n();
  const roots = useComponentRoots(controller, live);
  const [open, setOpen] = useState<Open>(undefined);
  const [pendingEnable, setPendingEnable] = useState<SchedulerRepo | null>(null);
  const [busy, setBusy] = useState(false);

  if (error !== undefined) {
    return <Alert intent="danger" title={t("settings.continuous.repositories.failed")} description={error} data-testid="repositories-failed" />;
  }
  if (view === undefined) {
    return <p className="text-xs opacity-70" data-testid="repositories-loading">{t("settings.continuous.loading")}</p>;
  }

  const rows = Array.isArray(view.repos) ? view.repos : [];
  const nameOf = (path: string): string | undefined => roots.roots.find((r) => path === r.root || path.startsWith(`${r.root}/`))?.name;
  const enrolled = new Set(rows.map((r) => r.componentRoot));
  const free = roots.roots.filter((r) => !enrolled.has(r.root));
  const current = open?.kind === "row" ? rows.find((r) => r.componentRoot === open.path) : open?.kind === "new" ? BLANK(open.path) : undefined;

  const act = async (fn: () => Promise<unknown>, after?: () => void) => {
    setBusy(true);
    try {
      await fn();
      after?.();
      onChanged();
    } catch (cause) {
      notifications.push({ title: t("settings.continuous.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  };

  const enableConfirmed = (repo: SchedulerRepo) =>
    act(
      // The row's own template, resent whole with `enabled: true` — the daemon revalidates and
      // refuses in its own words if it no longer loads.
      () =>
        setSchedulerRoot({
          componentRoot: repo.componentRoot,
          enabled: true,
          blueprint: repo.template.blueprint,
          tech: repo.template.tech || undefined,
          minPriority: repo.minPriority || undefined,
          window: repo.window,
          with: repo.template.with,
        }),
      () => setPendingEnable(null),
    );

  const columns: TableColumn<SchedulerRepo>[] = [
    {
      id: "repo",
      groupable: false,
      header: t("settings.continuous.col.repository"),
      render: (repo) => (
        <div className="w-0 min-w-full py-1" data-testid={`repo-row-${repo.componentRoot}`}>
          <span className="block truncate text-sm font-semibold">{nameOf(repo.componentRoot) ?? repo.componentRoot}</span>
          {nameOf(repo.componentRoot) !== undefined && <code className="block truncate text-xs opacity-60">{repo.componentRoot}</code>}
        </div>
      ),
    },
    {
      id: "status",
      groupable: false,
      header: t("settings.continuous.col.status"),
      width: 240,
      render: (repo) => (
        <div className="py-1">
          <span className="flex flex-wrap items-center gap-1">
            <Pill size="sm" tone={repo.enabled ? "green" : "slate"} data-testid={`repo-enabled-${repo.componentRoot}`}>
              {repo.enabled ? t("settings.continuous.repo.on") : t("settings.continuous.repo.off")}
            </Pill>
            {/* The daemon's state word, toned by its map. */}
            <Pill size="sm" tone={repoStateTone(repo.state.kind)} data-testid={`repo-state-${repo.componentRoot}`}>{repo.state?.kind ?? ""}</Pill>
          </span>
          {repo.state?.reason !== undefined && repo.state.reason !== "" && (
            <span className="mt-1 block text-xs opacity-80" data-testid={`repo-reason-${repo.componentRoot}`}>
              {repo.state.reason}
              {hasDetail(repo.state) && <span> — {repo.state.detail}</span>}
            </span>
          )}
        </div>
      ),
    },
    {
      id: "template",
      groupable: false,
      header: t("settings.continuous.col.template"),
      width: 170,
      render: (repo) => (
        <span className="text-xs opacity-80" data-testid={`repo-template-${repo.componentRoot}`}>
          {repo.template.blueprint}{repo.template.tech !== "" && ` · ${repo.template.tech}`} · {t("settings.continuous.repo.minPriority", { state: repo.minPriority })}
        </span>
      ),
    },
    {
      id: "window",
      groupable: false,
      header: t("settings.continuous.col.window"),
      width: 160,
      render: (repo) => (
        <span className="text-xs opacity-80">
          {repo.window.start === "" && repo.window.end === ""
            ? t("settings.continuous.repo.anytime")
            : t("settings.continuous.repo.window", { start: repo.window.start, end: repo.window.end, days: repo.window.days, zone: repo.window.zone })}
        </span>
      ),
    },
    {
      id: "last",
      groupable: false,
      header: t("settings.continuous.col.lastStart"),
      width: 150,
      render: (repo) => <span className="text-xs opacity-80">{repo.lastStartAt === "" ? "—" : repo.lastStartAt}</span>,
    },
  ];

  return (
    <div className="flex min-h-[22rem] min-w-0 flex-1 flex-col gap-3" data-testid="repositories-tab">
      <div className="flex shrink-0 items-center justify-between gap-3">
        <p className="text-xs opacity-70">{t("settings.continuous.repositories.count", { count: rows.length })}</p>
        <Button size="sm" variant="ghost" disabled={!canWrite || free.length === 0} title={free.length === 0 ? t("settings.continuous.repositories.noneFree") : undefined} onClick={() => setOpen({ kind: "new", path: free[0]?.root ?? "" })} data-testid="repo-add">
          {t("settings.continuous.actions.addRepo")}
        </Button>
      </div>
      <div className="min-h-[18rem] min-w-0 flex-1">
        <Table<SchedulerRepo>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(repo) => repo.componentRoot}
          corner="rounded-sm"
          onRowClick={(repo) => setOpen({ kind: "row", path: repo.componentRoot })}
          emptyState={
            <div data-testid="repositories-empty">
              <EmptyState title={t("settings.continuous.repositories.empty.title")} subtitle={t("settings.continuous.repositories.empty.body")} />
            </div>
          }
          fullHeight
        />
      </div>

      {current !== undefined && open !== undefined && (
        <OverlaySidePanel
          title={nameOf(current.componentRoot) ?? (open.kind === "new" ? t("settings.continuous.form.newTitle") : current.componentRoot)}
          titleClassName="text-xl font-semibold"
          variant="simple"
          width={560}
          onClose={() => setOpen(undefined)}
        >
          <div className="space-y-6" data-testid="repo-panel">
            {open.kind === "new" ? (
              <FormField label={t("settings.continuous.form.pickRepo")}>
                <Select variant="ghost" aria-label={t("settings.continuous.form.pickRepo")} value={open.path} onChange={(e) => setOpen({ kind: "new", path: e.target.value })} data-testid="repo-pick">
                  {free.map((r) => <option key={r.root} value={r.root}>{r.name} — {r.root}</option>)}
                </Select>
              </FormField>
            ) : (
              <section className="space-y-2">
                <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.continuous.q.working")}</h3>
                <p className="flex flex-wrap items-center gap-2 text-sm">
                  <Pill size="sm" tone={repoStateTone(current.state.kind)}>{current.state?.kind ?? ""}</Pill>
                  {current.state?.reason !== undefined && current.state.reason !== "" && (
                    <span className="opacity-80">{current.state.reason}{hasDetail(current.state) && <span> — {current.state.detail}</span>}</span>
                  )}
                </p>
                <code className="block truncate text-xs opacity-60">{current.componentRoot}</code>
                <div className="flex gap-2">
                  {current.enabled ? (
                    <Button size="sm" variant="ghost" disabled={!canWrite || busy} title={canWrite ? undefined : t("settings.continuous.disabled")} onClick={() => void act(() => disableSchedulerRoot(current.componentRoot))} data-testid={`repo-disable-${current.componentRoot}`}>
                      {t("settings.continuous.actions.disableRepo")}
                    </Button>
                  ) : (
                    <Button size="sm" disabled={!canWrite || busy} title={canWrite ? undefined : t("settings.continuous.disabled")} onClick={() => {
                        // The panel closes first: the confirm is a modal and must not sit under the panel it came from.
                        setOpen(undefined);
                        setPendingEnable(current);
                      }} data-testid={`repo-enable-${current.componentRoot}`}>
                      {t("settings.continuous.actions.enableRepo")}
                    </Button>
                  )}
                </div>
              </section>
            )}
            <RootForm
              key={`${current.componentRoot}|${current.enabled}`}
              repo={current}
              descriptor={view.descriptor}
              statement={view.statement}
              canWrite={canWrite}
              controller={controller}
              live={live}
              onClose={() => setOpen(undefined)}
              onDone={() => {
                setOpen(undefined);
                onChanged();
              }}
            />
          </div>
        </OverlaySidePanel>
      )}

      <AuthorityConfirm
        open={pendingEnable !== null}
        authorityStatement={view.statement}
        confirmLabel={t("settings.continuous.actions.enableRepo")}
        busy={busy}
        onConfirm={() => pendingEnable !== null && void enableConfirmed(pendingEnable)}
        onCancel={() => setPendingEnable(null)}
      />
    </div>
  );
}
