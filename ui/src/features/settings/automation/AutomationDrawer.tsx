import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { RelativeTime } from "../../../components/RelativeTime";
import type { LiveController, LiveState } from "../../../lib/live";
import { AuthorityConfirm, ReasonModal } from "./AuthorityConfirm";
import { FireLog } from "./FireLog";
import { HealthChips } from "./HealthChips";
import {
  archiveAutomation,
  enableEvent,
  disableEvent,
  fireNowSchedule,
  pauseSchedule,
  resumeSchedule,
  unarchiveAutomation,
  useAutomationHealth,
  useAutomationRuns,
  useAutomationShow,
} from "./automationRpc";
import {
  type FireRow,
  outcomePill,
  stateWord,
  stateWordTone,
  type HealthView,
  type ShowAutomation,
  type Vocabulary,
} from "./automationModel";

/**
 * AutomationDrawer — one automation as the daemon describes it
 * (`automation.show`): the plain-words sentence, the canonical shape, the
 * health with its reasons, last fires, loops started, and the audit for this
 * row. The link to S48's screen points at the blueprint by name; the link to
 * a loop routes to the loop page — the drawer shows other features' records,
 * it never edits them from here.
 *
 * Actions are exactly the plane's doors, each with the confirmation the
 * plane demands: enable/fire-now/unarchive pass the daemon's authority
 * statement (AN12); disable and archive REQUIRE a reason (the door refuses
 * without one); archive is never delete (AN6) and unarchive comes back
 * DISARMED — the drawer says so with the daemon's own post-unarchive state,
 * not a promise. There is no delete control because no delete door exists.
 */
export interface AutomationDrawerProps {
  source: string;
  id: string;
  vocabulary: Vocabulary | undefined;
  authorityStatement: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
  onEdit: () => void;
}

export function AutomationDrawer({ source, id, vocabulary, authorityStatement, controller, live, canWrite, onChanged, onEdit }: AutomationDrawerProps) {
  const { t } = useI18n();
  const show = useAutomationShow(source, id, controller, live);
  const health = useAutomationHealth(controller, live);
  const runs = useAutomationRuns(source, id, controller, live);

  const [modal, setModal] = useState<"enable" | "unarchive" | "fire" | "disable" | "archive" | null>(null);
  const [busy, setBusy] = useState(false);
  const [fireResult, setFireResult] = useState<FireRow | { outcome: string; reason: string } | undefined>(undefined);

  // Widened exactly like the model widens the list row (fixture-narrow
  // generated types — see automationModel): the wire carries all of these.
  const view = show.data?.automation as ShowAutomation | undefined;
  const rowHealth = useMemo(() => {
    for (const h of health.data?.health ?? []) {
      if (h.source === source && h.id === id) return h;
    }
    return undefined;
  }, [health.data, source, id]);

  useEffect(() => {
    setModal(null);
    setFireResult(undefined);
  }, [source, id]);

  if (show.error !== undefined) {
    return <Alert intent="danger" title={show.error} data-testid="drawer-error-panel" />;
  }
  if (view === undefined) {
    return <EmptyState showIcon={false} title={t("settings.automation.drawer.loading")} data-testid="drawer-loading" />;
  }

  const state = stateWord(view);
  const isSchedule = source === "schedule";
  const isEvent = source === "event";

  // A refusal is a toast and nothing else: the daemon's sentence — a stolen actor, a missing reason,
  // a widening it will not apply — with what it means and what to do, never ours.
  const refuse = (err: unknown) => {
    const body = err instanceof Error && !(err as { data?: unknown }).data ? err.message : refusalBody(toRpcError(err));
    notifications.push({ title: t("settings.automation.drawer.refused"), body, tone: "danger", source: "local" });
  };

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
      await show.refetch();
      onChanged();
    } catch (err) {
      // The door's refusal, verbatim — a stolen actor, a missing reason, a
      // widening the daemon will not apply: whatever it is, the person reads
      // the daemon's sentence, not ours.
      refuse(err);
    } finally {
      setBusy(false);
    }
  };

  const fireNow = async () => {
    setBusy(true);
    try {
      const res = await fireNowSchedule(id) as { outcome: string; reason?: string } & Record<string, unknown>;
      // AN16's five outcomes survive the click: the answer pill carries the
      // door's own outcome word and reason, never folded or softened.
      setFireResult({ outcome: res.outcome, reason: res.reason ?? "" });
      await show.refetch();
    } catch (err) {
      refuse(err);
    } finally {
      setBusy(false);
    }
  };

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
  const fires = show.data?.recentFires ?? [];

  return (
    <div className="space-y-8" data-testid="automation-drawer">
      <div className="flex flex-wrap gap-2" data-testid="drawer-actions">
        <Button size="sm" disabled={!canWrite} onClick={onEdit} data-testid="drawer-edit">{t("settings.automation.drawer.edit")}</Button>

        {isSchedule && !view.archived && (
          <>
            {view.paused ? (
              <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => void act(() => resumeSchedule(id))} data-testid="drawer-resume">{t("settings.automation.drawer.resume")}</Button>
            ) : (
              <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => void act(() => pauseSchedule(id))} data-testid="drawer-pause">{t("settings.automation.drawer.pause")}</Button>
            )}
            {/* Fire now is an ARMED action by definition — it starts a loop with no schedule having
                promised it. The daemon's statement goes up first; the click below is the person's answer. */}
            <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => setModal("fire")} data-testid="drawer-fire-now">{t("settings.automation.drawer.fireNow")}</Button>
          </>
        )}

        {isEvent && !view.archived && (
          view.enabled ? (
            <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => setModal("disable")} data-testid="drawer-disable">{t("settings.automation.drawer.disable")}</Button>
          ) : (
            <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => setModal("enable")} data-testid="drawer-enable">{t("settings.automation.drawer.enable")}</Button>
          )
        )}

        {!view.archived ? (
          <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => setModal("archive")} data-testid="drawer-archive">{t("settings.automation.drawer.archive")}</Button>
        ) : (
          <Button size="sm" disabled={!canWrite || busy} onClick={() => setModal("unarchive")} data-testid="drawer-unarchive">{t("settings.automation.drawer.unarchive")}</Button>
        )}
      </div>

      {fireResult !== undefined && (
        <Alert
          intent={fireResult.outcome === "started" ? "success" : "warning"}
          title={`${t("settings.automation.drawer.fireNow")}: ${fireResult.outcome}`}
          description={fireResult.reason}
          data-testid="drawer-fire-result"
        />
      )}
      {view.archived && view.archivedReason !== undefined && view.archivedReason !== "" && <Alert intent="info" title={`${t("settings.automation.drawer.archivedReason")}: ${view.archivedReason}`} />}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.what")}</h3>
        {view.description !== undefined && view.description !== "" && <p className="text-sm" data-testid="drawer-description">{view.description}</p>}
        <div className="flex flex-wrap items-center gap-2">
          <Pill size="sm" tone="slate">{source}</Pill>
          <Pill size="sm" tone={stateWordTone[state]} data-testid="drawer-state">{t(`settings.automation.state.${state}`)}</Pill>
          {view.disabledReason !== undefined && view.disabledReason !== "" && <span className="text-xs opacity-70" data-testid="drawer-disabled-reason">{view.disabledReason}</span>}
        </div>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.health")}</h3>
        <HealthChips health={rowHealth ?? (show.data?.health as HealthView | undefined)} />
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.shape")}</h3>
        <dl className="space-y-1 text-sm" data-testid="drawer-facts">
          <Fact label={t("settings.automation.drawer.blueprint")}>
            <Link className="underline" to={`/blueprints?name=${encodeURIComponent(view.blueprint)}`} data-testid="drawer-blueprint-link">{view.blueprint}</Link>
          </Fact>
          {isSchedule && (
            <>
              <Fact label={t("settings.automation.drawer.schedule")}><code>{show.data?.schedule?.expr ?? view.expr}</code></Fact>
              <Fact label={t("settings.automation.drawer.zone")}>{view.zone ?? show.data?.schedule?.zone ?? "UTC"}</Fact>
            </>
          )}
          {source === "webhook" && (
            // The NAME of the daemon's variable plus its presence — there is no value on the wire to
            // show (§4c/C17; the type has nowhere to put one and this line has nothing to leak).
            <Fact label={t("settings.automation.drawer.secretEnv")}>
              <code data-testid="drawer-secret-env">{view.secretEnv ?? ""} · {rowHealth?.webhook?.set === true ? "set" : rowHealth?.webhook?.set === false ? "unset" : "?"}</code>
            </Fact>
          )}
          {isEvent && (
            <>
              <Fact label={t("settings.automation.drawer.match")}><code>{JSON.stringify(view.match ?? {})}</code></Fact>
              <Fact label={t("settings.automation.drawer.kickoff")}><code>{view.kickoff ?? ""}</code></Fact>
              <Fact label={t("settings.automation.drawer.component")}><code>{view.componentId ?? "—"}</code></Fact>
            </>
          )}
          <Fact label={t("settings.automation.drawer.workspace")}><code>{view.workspace ?? "—"}</code></Fact>
          {view.goal !== undefined && view.goal !== "" && <Fact label={t("settings.automation.drawer.goal")}>{view.goal}</Fact>}
        </dl>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.limits")}</h3>
        <dl className="space-y-1 text-sm">
          <Fact label={t("settings.automation.drawer.limits")}>{`maxRuns/hour ${view.maxRunsPerHour ?? "—"} · maxCost/day ${view.maxCostPerDayUsd ?? "—"}`}</Fact>
          <Fact label={t("settings.automation.drawer.createdBy")}>{`${view.createdByKind ?? "?"}:${view.createdById ?? "?"}`}</Fact>
          <Fact label={t("settings.automation.drawer.updated")}><RelativeTime iso={view.updatedAt ?? view.createdAt} /></Fact>
        </dl>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.fires")}</h3>
        <ul className="space-y-1">
          {fires.map((fire) => (
            <li key={fire.fireId} className="flex flex-wrap items-center gap-2 text-xs" data-testid={`drawer-fire-${fire.fireId}`}>
              <Pill size="sm" tone={outcomePill(fire.outcome).tone}>{fire.outcome}</Pill>
              <RelativeTime iso={fire.at} />
              {fire.loopSlug !== undefined && fire.loopSlug !== "" && <Link className="underline" to={`/loops/${encodeURIComponent(fire.loopId)}`}>{fire.loopSlug}</Link>}
              <span className="opacity-70">{fire.reason}</span>
            </li>
          ))}
          {fires.length === 0 && <li className="text-xs opacity-60">{t("settings.automation.list.neverFired")}</li>}
        </ul>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.loops")}</h3>
        <div data-testid="drawer-runs">
          <p className="text-xs opacity-70">{runs.data?.total ?? 0} · {t("settings.automation.drawer.total")}</p>
          <ul className="space-y-1">
            {(runs.data?.loops ?? []).map((loop) => (
              <li key={loop.id} className="flex items-center gap-2 text-xs">
                <Link className="underline" to={`/loops/${encodeURIComponent(loop.id)}`} data-testid={`drawer-run-${loop.id}`}>{loop.slug}</Link>
                <Pill size="sm" tone="slate">{loop.status}</Pill>
                <RelativeTime iso={loop.startedAt} />
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.changes")}</h3>
        <ul className="space-y-1">
          {(show.data?.changes ?? []).map((change) => (
            <li key={change.id} className="flex flex-wrap items-center gap-2 text-xs" data-testid={`drawer-change-${change.id}`}>
              <Pill size="sm" tone="slate">{change.op}</Pill>
              <span className="opacity-80">{change.summary}</span>
              <span className="font-mono opacity-60">{change.actorKind}:{change.actorId}</span>
              <RelativeTime iso={change.occurredAt} />
            </li>
          ))}
        </ul>
      </section>

      <FireLog scope={{ source, id }} embedded controller={controller} live={live} vocabulary={vocabulary} />

      {/* ── authority confirmations: the daemon's words, one click to pass ── */}
      <AuthorityConfirm
        open={modal === "enable" || modal === "unarchive" || modal === "fire"}
        authorityStatement={authorityStatement}
        confirmLabel={t(modal === "unarchive"
          ? "settings.automation.drawer.unarchive"
          : modal === "fire"
            ? "settings.automation.drawer.fireNow"
            : "settings.automation.drawer.enable")}
        busy={busy}
        onConfirm={() => {
          const which = modal;
          setModal(null);
          if (which === "fire") {
            void fireNow();
            return;
          }
          void act(() => (which === "unarchive"
            ? unarchiveAutomation(source, id)
            : enableEvent(id)));
        }}
        onCancel={() => setModal(null)}
      >
        <p className="font-mono text-xs opacity-80">{view.id} · {view.blueprint}</p>
        {modal === "unarchive" && <p className="text-xs opacity-80" data-testid="unarchive-disarmed-note">{t("settings.automation.drawer.unarchiveDisarmed")}</p>}
      </AuthorityConfirm>

      <ReasonModal
        open={modal === "disable"}
        title={t("settings.automation.drawer.disable")}
        confirmLabel={t("settings.automation.drawer.disable")}
        busy={busy}
        onSubmit={(reason) => {
          setModal(null);
          void act(() => disableEvent(id, reason));
        }}
        onCancel={() => setModal(null)}
      >
        {show.data?.description !== undefined && <p className="text-xs opacity-80">{show.data.description}</p>}
      </ReasonModal>

      <ReasonModal
        open={modal === "archive"}
        title={t("settings.automation.drawer.archive")}
        confirmLabel={t("settings.automation.drawer.archive")}
        busy={busy}
        onSubmit={(reason) => {
          setModal(null);
          void act(() => archiveAutomation(source, id, reason));
        }}
        onCancel={() => setModal(null)}
      >
        {show.data?.description !== undefined && <p className="text-xs opacity-80">{show.data.description}</p>}
        {/* AN6: archive hides from the engine, the row stays, the audit
            remembers the reason. Nothing is deleted — there is nothing to
            delete with: the plane has no delete door. */}
        <p className="text-xs opacity-80" data-testid="archive-safer-than-delete">{t("settings.automation.drawer.archiveNotDelete")}</p>
      </ReasonModal>
    </div>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return <div><dt className="inline opacity-70">{label}: </dt><dd className="inline">{children}</dd></div>;
}
