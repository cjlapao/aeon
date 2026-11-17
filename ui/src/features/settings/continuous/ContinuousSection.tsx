import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, Tabs } from "../../../components/kit";
import { PageTitle } from "../../../components/PageTitle";
import type { LiveController, LiveState } from "../../../lib/live";
import { useLiveSubscription } from "../../../lib/useLiveSubscription";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { AuthorityConfirm, ReasonModal } from "../automation/AuthorityConfirm";
import {
  disableMaster,
  enableMaster,
  pauseMaster,
  resumeMaster,
  stopAllScheduler,
  useSchedulerAttention,
  useSchedulerDecisions,
  useSchedulerPreview,
  useSchedulerStatus,
} from "./continuousRpc";
import { readableStatus, withLists, DECISIONS_LIMIT } from "./continuousModel";
import { ContinuousStatus } from "./ContinuousStatus";
import { RepositoriesTab } from "./RepositoriesTab";
import { PreviewTab } from "./PreviewTab";
import { DecisionsTab } from "./DecisionsTab";
import { NeedsYouTab } from "./NeedsYouTab";

/**
 * ContinuousSection — Configuration → Continuous work (S27 T10, CN13/CN15/CN22).
 *
 * The page answers four questions a person has about an agent system that starts work by itself:
 * **is it on, which repositories may it work in, what would it start next (and why not the rest),
 * and what did it decide.** Everything shown is the daemon's own answer, in its own words — the
 * section holds no scheduling vocabulary and no logic, and no row is ever patched locally: each
 * write refetches.
 *
 * The seatbelts live in the header, at click speed. Turning it on confirms with the daemon's OWN
 * statement (the string `scheduler.status` carries, the same one the CLI prints before `--yes`) and
 * declining sends nothing. Pause and Stop everything REQUIRE the person's reason (CN18). A refusal is
 * a toast — the daemon's sentence, then what it means and what to do — and no panel stays behind.
 * `scheduler.enableTree` has no control here on purpose: enabling a whole tree is a typed act on
 * the CLI (CN6). Reads refresh on the event stream (`loop.finished`, `loop.blocked`, `scheduler.*`),
 * never on a timer.
 */
export interface ContinuousSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

type Tab = "repositories" | "preview" | "decisions" | "attention";

/** Which seatbelt modal is open. Decline closes it and sends nothing. */
type Pending = "enable" | "pause" | "stopAll" | null;

export function ContinuousSection({ controller, live, canWrite = live.state === "current" }: ContinuousSectionProps) {
  const { t } = useI18n();
  const [tab, setTab] = useState<Tab>("repositories");
  const [pending, setPending] = useState<Pending>(null);
  const [busy, setBusy] = useState(false);

  const status = useSchedulerStatus(controller, live);
  const preview = useSchedulerPreview(controller, live);
  const attention = useSchedulerAttention(controller, live);
  const decisions = useSchedulerDecisions("", controller, live);

  // The tick's decisions surface AS loop events, and the `scheduler.*` prefix picks up any the
  // daemon starts publishing — no poll, no timer anywhere in this folder.
  useLiveSubscription(live, ["loop.finished", "loop.blocked", "scheduler.*"], () => {
    status.refetch();
    preview.refetch();
    decisions.refetch();
    attention.refetch();
  });

  const title = (
    <PageTitle
      title={t("settings.continuous.title")}
      purpose={t("settings.continuous.subtitle")}
      help={{ title: t("settings.continuous.help.title"), content: t("settings.continuous.help.body") }}
    />
  );

  if (live.state !== "current") {
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-4" data-testid="continuous-section">
        <ContentActions className="shrink-0" gap="md" align="end">{title}</ContentActions>
        <Alert intent="warning" title={t("settings.continuous.reconnecting")} data-testid="continuous-reconnecting" />
      </div>
    );
  }

  // An answer shaped outside the contract renders as unreadable, never as a guess or a crash.
  const view = readableStatus(status.data) ? withLists(status.data) : undefined;
  const unreadable = status.data !== undefined && view === undefined;
  const reloadAll = () => {
    status.refetch();
    preview.refetch();
    decisions.refetch();
    attention.refetch();
  };

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      const answered = (await fn()) as { runningLoops?: string } | undefined;
      setPending(null);
      // CN18's pause sentence — the daemon's own `runningLoops` words, verbatim.
      if (answered?.runningLoops !== undefined && answered.runningLoops !== "") {
        notifications.push({ title: t("settings.continuous.pausedToast"), body: answered.runningLoops, tone: "success", source: "local" });
      }
      reloadAll();
    } catch (cause) {
      notifications.push({ title: t("settings.continuous.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  };

  const open = attention.data?.open ?? 0;
  const off = canWrite ? undefined : t("settings.continuous.disabled");
  const controls =
    view === undefined ? undefined : !view.enabled ? (
      <Button size="sm" disabled={!canWrite} title={off} onClick={() => setPending("enable")} data-testid="continuous-enable">
        {t("settings.continuous.actions.enable")}
      </Button>
    ) : (
      <span className="flex flex-wrap items-center gap-2" data-testid="continuous-controls">
        {view.paused ? (
          <Button size="sm" variant="ghost" disabled={!canWrite} title={off} onClick={() => void act(resumeMaster)} data-testid="continuous-resume">
            {t("settings.continuous.actions.resume")}
          </Button>
        ) : (
          <Button size="sm" variant="ghost" disabled={!canWrite} title={off} onClick={() => setPending("pause")} data-testid="continuous-pause">
            {t("settings.continuous.actions.pause")}
          </Button>
        )}
        <Button size="sm" variant="ghost" disabled={!canWrite} title={off} onClick={() => setPending("stopAll")} data-testid="continuous-stop-all">
          {t("settings.continuous.actions.stopAll")}
        </Button>
        <Button size="sm" variant="ghost" disabled={!canWrite} title={off} onClick={() => void act(disableMaster)} data-testid="continuous-disable">
          {t("settings.continuous.actions.disable")}
        </Button>
      </span>
    );

  return (
    <div className="flex min-h-0 min-w-0 w-full flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden" data-testid="continuous-section">
      <ContentActions className="shrink-0" gap="md" align="end" data-testid="continuous-actions" actions={controls}>
        {title}
      </ContentActions>

      {status.error !== undefined && (
        <Alert className="shrink-0" intent="danger" title={t("settings.continuous.status.failed")} description={status.error} data-testid="continuous-status-failed">
          <button type="button" onClick={status.refetch} data-testid="continuous-status-retry">{t("settings.continuous.actions.retry")}</button>
        </Alert>
      )}
      {unreadable && <Alert className="shrink-0" intent="danger" title={t("settings.continuous.status.unreadable")} data-testid="continuous-status-unreadable" />}
      {status.loading && view === undefined && !unreadable && (
        <p className="shrink-0 text-xs opacity-70" data-testid="continuous-loading">{t("settings.continuous.loading")}</p>
      )}
      {view !== undefined && <ContinuousStatus view={view} />}

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "repositories", label: t("settings.continuous.tabs.repositories") },
            { id: "preview", label: t("settings.continuous.tabs.preview") },
            { id: "decisions", label: t("settings.continuous.tabs.decisions") },
            { id: "attention", label: t("settings.continuous.tabs.attention"), badge: open > 0 ? open : undefined },
          ]}
          value={tab}
          onChange={(id) => setTab(id as Tab)}
        />
      </div>
      <p className="shrink-0 text-sm opacity-70">{t(`settings.continuous.hint.${tab}`)}</p>

      {tab === "repositories" && (
        <RepositoriesTab view={view} error={status.error} canWrite={canWrite} controller={controller} live={live} onChanged={reloadAll} />
      )}
      {tab === "preview" && <PreviewTab preview={preview.data} error={preview.error} onRetry={preview.refetch} />}
      {tab === "decisions" && (
        <DecisionsTab descriptor={view?.descriptor} decisions={decisions.data} error={decisions.error} onRetry={decisions.refetch} limit={DECISIONS_LIMIT} />
      )}
      {tab === "attention" && <NeedsYouTab attention={attention.data} error={attention.error} onRetry={attention.refetch} />}

      <AuthorityConfirm
        open={pending === "enable"}
        authorityStatement={view?.statement ?? ""}
        confirmLabel={t("settings.continuous.actions.enable")}
        busy={busy}
        onConfirm={() => void act(enableMaster)}
        onCancel={() => setPending(null)}
      />
      <ReasonModal
        open={pending === "pause"}
        title={t("settings.continuous.pause.title")}
        confirmLabel={t("settings.continuous.actions.pause")}
        busy={busy}
        onSubmit={(reason) => void act(() => pauseMaster(reason))}
        onCancel={() => setPending(null)}
      />
      <ReasonModal
        open={pending === "stopAll"}
        title={t("settings.continuous.stopAll.title")}
        confirmLabel={t("settings.continuous.actions.stopAll")}
        busy={busy}
        onSubmit={(reason) => void act(() => stopAllScheduler(reason))}
        onCancel={() => setPending(null)}
      >
        {/* The daemon's own live numbers BEFORE the act — no sentence of ours about what stopping means. */}
        {view !== undefined && (
          <p className="text-xs opacity-80" data-testid="continuous-stop-all-note">{t("settings.continuous.stopAll.note", { used: view.slots.used })}</p>
        )}
      </ReasonModal>
    </div>
  );
}
