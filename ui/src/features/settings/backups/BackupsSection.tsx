import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { PageTitle } from "../../../components/PageTitle";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { BackupStatusHeader } from "./BackupStatusHeader";
import { BackupList } from "./BackupList";
import { BackupPanel } from "./BackupPanel";
import { PruneDialog } from "./PruneDialog";
import { planRemovals, type PrunePlanRow } from "./backupsModel";

/**
 * Configuration → Backups (S62 T10, P7/P34): the database's safety net, as the daemon holds it.
 *
 * The page answers three questions: **is there a recent copy?** (the status cards), **which copies
 * exist and are they any good?** (the table, one click to open a copy) and **how do I get one back?**
 * (each copy's panel carries its own terminal command). Two reads, three writes: `backup.status` and
 * `backup.list` render the cards and the rows; `backup.run`, `backup.verify` and `backup.prune` are the
 * actions — each called only through the generated contract, each refusal a toast in the daemon's own
 * words, each row updated from the next response, never optimistically. The settings the schedule runs
 * on are the `backup.*` keys of the Daemon settings page (S44 T8), which the page links to — no setting
 * is typed twice here.
 *
 * Restore is not in the app: it replaces the database and needs the daemon stopped, so the panel shows a
 * static sentence and a copy button for the terminal command — no button calls anything.
 */
export interface BackupsSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

// The list's shape: pre-migration copies included (they carry their own reason badge), the
// daemon-side limit — the page never asks for more.
const LIST_PARAMS = { includePreMigration: true, limit: 50 } as const;

export function BackupsSection({ controller, live, canWrite = live.state === "current" }: BackupsSectionProps) {
  const { t } = useI18n();
  const list = useRpcQuery("backup.list", LIST_PARAMS, controller, live);
  const status = useRpcQuery("backup.status", {}, controller, live);
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();

  const [prunePlan, setPrunePlan] = useState<PrunePlanRow[] | undefined>(undefined);
  const [pruneResult, setPruneResult] = useState<{ removed?: string[]; error?: string } | undefined>(undefined);
  const [verified, setVerified] = useState<{ file: string; sentence: string } | undefined>(undefined);
  const [openFile, setOpenFile] = useState<string | undefined>(undefined);

  // A successful action changes the rows and the header: refetch both, so the next render's data is
  // the daemon's answer, never a local guess.
  const refresh = () => {
    list.refetch();
    status.refetch();
  };

  const runNow = async () => {
    const result = await action.run(() => rpcTyped("backup.run", { reason: "manual" }), { errorTitle: t("settings.backups.actions.runFailed") });
    if (result) {
      notifications.push({ title: t("settings.backups.actions.runSuccess", { file: result.backup.file }), tone: "success", source: "local" });
      refresh();
    }
    // A refusal is a toast; nothing was written and the list stays as the daemon last answered it.
  };

  const verify = async (file: string, drill: boolean) => {
    const result = await action.run(() => rpcTyped("backup.verify", { file, drill }), { errorTitle: t("settings.backups.actions.verifyFailed") });
    if (result) {
      // The daemon's own sentence, verbatim — and the rows re-read, because the ledger's verify row
      // changed under the list.
      setVerified({ file, sentence: result.report.sentence });
      refresh();
    }
  };

  const openPrune = async () => {
    setPruneResult(undefined);
    const result = await action.run(() => rpcTyped("backup.prune", { dryRun: true }), { errorTitle: t("settings.backups.actions.pruneFailed") });
    // The dry run answered: the plan is shown, nothing deleted. A refusal: no plan, no dialog, a toast.
    if (result) setPrunePlan(result.plan);
  };

  const confirmPrune = async () => {
    const removals = prunePlan ? planRemovals(prunePlan) : [];
    const confirmed = await confirm({
      title: t("settings.backups.actions.pruneConfirmTitle"),
      description: t("settings.backups.actions.pruneConfirmBody", { count: removals.length }),
      confirmValue: "prune",
      confirmValueLabel: t("settings.backups.actions.pruneConfirmValueLabel"),
      confirmLabel: t("settings.backups.actions.pruneConfirm"),
    });
    // Declined: the dialog and its plan stay open, nothing sent.
    if (!confirmed) return;
    const result = await action.run(() => rpcTyped("backup.prune", { dryRun: false }), { errorTitle: t("settings.backups.actions.pruneFailed") });
    // The dry run passed but the real prune was refused: the toast carries the refusal, the plan is kept.
    if (result) {
      setPruneResult({ removed: result.removed ?? [] });
      refresh();
    }
  };

  const rows = list.data?.backups ?? [];
  const open = rows.find((row) => row.file === openFile);

  return (
    <div className="flex min-h-0 min-w-0 w-full flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden" data-testid="backups-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        actions={
          <span className="flex items-center gap-2">
            <Button size="sm" variant="ghost" onClick={() => void openPrune()} disabled={!canWrite || action.busy} data-testid="backup-prune">{t("settings.backups.actions.prune")}</Button>
            <Button size="sm" onClick={() => void runNow()} disabled={!canWrite || action.busy} data-testid="backup-run-now">
              {action.busy ? t("settings.backups.actions.runBusy") : t("settings.backups.actions.runNow")}
            </Button>
          </span>
        }
      >
        <PageTitle title={t("settings.backups.title")} purpose={t("settings.backups.subtitle")} help={{ title: t("settings.backups.help.title"), content: t("settings.backups.help.body") }} />
      </ContentActions>

      {!canWrite && (
        <Alert className="shrink-0" intent="warning" title={t("settings.backups.reconnecting")} description={t("settings.backups.reconnectingBody")} data-testid="backups-offline" />
      )}

      <BackupStatusHeader status={status.data} loading={status.loading} error={status.error} onRetry={status.refetch} />

      <p className="shrink-0 text-sm opacity-70">{t("settings.backups.hint")}</p>

      {list.error ? (
        <Alert className="shrink-0" intent="danger" title={t("settings.backups.list.failed")} description={list.error} data-testid="backup-list-error" />
      ) : (
        <div className="flex min-h-[22rem] min-w-0 flex-1 flex-col gap-2">
          <BackupList rows={rows} loading={list.loading} onOpen={setOpenFile} />
          <p className="shrink-0 text-xs opacity-70" data-testid="backup-list-count">{t("settings.backups.list.footer", { count: rows.length })}</p>
        </div>
      )}

      {open !== undefined && (
        <OverlaySidePanel title={open.file} titleClassName="break-all font-mono text-base font-semibold" variant="simple" width={560} onClose={() => setOpenFile(undefined)}>
          <BackupPanel view={open} canWrite={canWrite} busy={action.busy} onVerify={(file, drill) => void verify(file, drill)} verifySentence={verified?.file === open.file ? verified.sentence : undefined} />
        </OverlaySidePanel>
      )}

      {prunePlan && (
        <PruneDialog
          plan={prunePlan}
          busy={action.busy}
          result={pruneResult}
          onConfirm={() => void confirmPrune()}
          onClose={() => {
            setPrunePlan(undefined);
            setPruneResult(undefined);
          }}
        />
      )}
      {modal}
    </div>
  );
}
