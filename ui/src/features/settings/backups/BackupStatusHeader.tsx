import { useI18n } from "@cjlapao/ui-kit";
import { Alert, CollapsiblePanel, EmptyState, Pill, StatCard } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError } from "../../../lib/bridge";
import { RelativeTime } from "../../../components/RelativeTime";
import { formatBytes, type BackupStatus } from "./backupsModel";

/**
 * The status strip (T10): the `backup.status` answer as three kit stat cards — the last backup, the
 * next one, and the database — with the schedule's settings folded underneath.
 *
 * Every word the strip shows — the state and its sentence, the last success age, the next due, the
 * database and WAL sizes, the schedule — is the daemon's: the state is captioned from the daemon's own
 * enum string, and nothing here computes freshness (the daemon owns it, T6). On a machine with no backup
 * yet the state is the daemon's first enum word and the card says so, not a blank. The query lives in the
 * section (which also runs the actions and must refetch it), so this component is presentational: it
 * renders the state it is given and keeps the last answer on an error — reads keep the last data, writes
 * are disabled by the section.
 */
export interface BackupStatusHeaderProps {
  status: BackupStatus | undefined;
  loading: boolean;
  error: string | undefined;
  onRetry: () => void;
}

export function BackupStatusHeader({ status, loading, error, onRetry }: BackupStatusHeaderProps) {
  const { t } = useI18n();

  if (loading && !status) {
    return (
      <div data-testid="backup-status-header">
        <EmptyState title={t("settings.backups.status.loading")} />
      </div>
    );
  }

  if (!status) {
    return (
      <div data-testid="backup-status-header">
        <ErrorPanel error={new RpcError(0, error ?? "backup.status answered nothing")} onRetry={onRetry} />
      </div>
    );
  }

  // The state is the daemon's enum word; the caption is keyed by it, so a state the daemon can newly
  // report arrives as a missing caption, not a word the UI invented. The pill stays a neutral tone on
  // purpose: colouring by state would mean spelling the daemon's words in the source.
  const stateWord = t(`settings.backups.status.state.${status.state}`);
  const stateBody = t(`settings.backups.status.stateBody.${status.state}`);
  const db = status.db;

  return (
    <div className="shrink-0 space-y-3" data-testid="backup-status-header">
      {error && <Alert intent="warning" title={error} />}
      <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 260px), 1fr))" }}>
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          icon="Save"
          label={t("settings.backups.status.lastSuccess")}
          value={<span data-testid="backup-last-success">{status.lastSuccess ? <RelativeTime iso={status.lastSuccess} /> : t("settings.backups.status.lastSuccessNever")}</span>}
          subtitle={<span data-testid="backup-state-sentence">{stateBody}</span>}
          actions={<Pill size="sm" tone="slate" data-testid="backup-state-pill">{stateWord}</Pill>}
          className="min-w-0"
        />
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          icon="Live"
          label={t("settings.backups.status.nextDue")}
          value={
            <span data-testid="backup-next-due">
              {status.enabled && status.nextDueAt ? <RelativeTime iso={status.nextDueAt} prefix={t("settings.backups.status.nextDuePrefix")} /> : t("settings.backups.status.scheduleOff")}
            </span>
          }
          subtitle={<span data-testid="backup-schedule">{status.enabled ? t("settings.backups.status.scheduleOn") : t("settings.backups.status.scheduleOff")}</span>}
          className="min-w-0"
        />
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          icon="Share"
          label={t("settings.backups.status.database")}
          value={formatBytes(db.bytes)}
          subtitle={
            <span data-testid="backup-db-size">
              {formatBytes(db.bytes)} + {formatBytes(db.walBytes)} WAL · {db.applied}/{db.applied + db.pending} {t("settings.backups.status.migrations")}
            </span>
          }
          className="min-w-0"
        />
      </div>

      <CollapsiblePanel
        tone="slate"
        data-testid="backup-settings"
        title={t("settings.backups.settings.title")}
        subtitle={t("settings.backups.settings.subtitle")}
        defaultExpanded={false}
      >
        {status.settings.length > 0 && (
          <p className="text-xs opacity-80" data-testid="backup-settings-line">
            {status.settings.map((entry) => `${entry.key}=${entry.value} (${entry.source})`).join(" · ")}
          </p>
        )}
        <p className="mt-2 text-xs opacity-70" data-testid="backups-settings-pointer">
          {t("settings.backups.settingsPointerBefore")}
          <a href="#daemon-settings" className="underline" data-testid="backups-settings-link">{t("settings.backups.settingsPointerLink")}</a>
        </p>
      </CollapsiblePanel>
    </div>
  );
}
