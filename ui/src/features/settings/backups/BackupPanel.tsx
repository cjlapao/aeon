import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, Pill, Toggle } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import { formatBytes, restoreCommand, schemaLine, type BackupView } from "./backupsModel";

/**
 * BackupPanel — one copy opened, as three questions: **where did it come from?**, **is it any good?**
 * and **can I restore it?** (T10). Every fact is the daemon's field or word; nothing is recomputed.
 *
 * Verify is the one act: a quick check, or the full restore drill that opens the copy as a live store
 * and runs its migrations (the only check that a restore would actually work). The daemon's sentence for
 * the last verify rides under the button verbatim, and a refusal is a toast with the drill choice kept.
 * Restore is not in the app — it replaces the database and needs the daemon stopped — so the panel shows
 * the exact terminal command for THIS file and a copy button; no button calls anything.
 */
export interface BackupPanelProps {
  view: BackupView;
  canWrite: boolean;
  /** A verify is in flight — the control stays disabled. */
  busy: boolean;
  onVerify: (file: string, drill: boolean) => void;
  /** The last verify's daemon sentence for THIS file, shown verbatim. */
  verifySentence: string | undefined;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function BackupPanel({ view, canWrite, busy, onVerify, verifySentence }: BackupPanelProps) {
  const { t } = useI18n();
  const [drill, setDrill] = useState(false);
  const [copied, setCopied] = useState(false);
  const command = restoreCommand(view.file);

  const copy = () => {
    void navigator.clipboard
      ?.writeText(command)
      .then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      })
      .catch(() => undefined);
  };

  return (
    <div className="space-y-6 pb-6" data-testid="backup-panel">
      <section className="space-y-2">
        <h3 className={H}>{t("settings.backups.q.origin")}</h3>
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <Pill size="sm" tone="slate">{t(`settings.backups.reason.${view.reason}`)}</Pill>
          <span className="opacity-80"><RelativeTime iso={view.createdAt} /> · {view.createdAt}</span>
        </p>
        <p className="text-sm opacity-80">{formatBytes(view.sizeBytes)} · {schemaLine(view)}</p>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.backups.q.good")}</h3>
        <p className="text-sm">
          <Pill size="sm" tone={view.verified === "unverified" ? "amber" : "green"}>{t(`settings.backups.verified.${view.verified}`)}</Pill>
        </p>
        <Toggle
          label={t("settings.backups.actions.drill")}
          aria-label={t("settings.backups.actions.drill")}
          checked={drill}
          disabled={!canWrite || busy}
          onChange={(event) => setDrill(event.target.checked)}
          data-testid={`backup-row-drill-${view.file}`}
        />
        {drill && <p className="text-xs opacity-70" data-testid={`backup-row-drill-cost-${view.file}`}>{t("settings.backups.actions.drillCost")}</p>}
        <Button size="sm" disabled={!canWrite || busy} onClick={() => onVerify(view.file, drill)} data-testid={`backup-row-verify-${view.file}`}>
          {t("settings.backups.actions.verify")}
        </Button>
        {verifySentence && <p className="rounded-md bg-white/5 px-3 py-2 text-xs opacity-90" data-testid="backup-verify-sentence">{verifySentence}</p>}
      </section>

      <section className="space-y-2">
        <h3 className={H}>{t("settings.backups.q.restore")}</h3>
        <p className={`text-sm ${view.canRestoreWithThisBinary ? "opacity-80" : "font-medium text-amber-600 dark:text-amber-400"}`}>
          {view.canRestoreWithThisBinary ? t("settings.backups.row.restorable") : t("settings.backups.row.notRestorable", { migration: view.lastMigration })}
        </p>
        <p className="text-sm opacity-80" data-testid="backups-restore-body">{t("settings.backups.restore.body")}</p>
        <code className="block break-all rounded-md bg-white/5 px-3 py-2 text-xs" data-testid="backups-restore-command">{command}</code>
        <Button size="sm" variant="ghost" onClick={copy} data-testid="backups-restore-copy">{copied ? t("settings.backups.restore.copied") : t("settings.backups.restore.copy")}</Button>
      </section>
    </div>
  );
}
