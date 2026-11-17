import { useI18n } from "@cjlapao/ui-kit";
import { Button, Modal, Pill } from "../../../components/kit";
import { planRemovals, type PrunePlanRow } from "./backupsModel";

/**
 * The prune plan dialog (T10): the daemon's own `backup.prune {dryRun:true}`
 * answer, shown before anything is deleted.
 *
 * The rows — file, keep or remove, the reason — are the daemon's statement,
 * rendered verbatim; the dialog composes nothing about what a prune would
 * do. Confirming routes through the section's type-to-confirm, which calls
 * the door again with `dryRun:false`; refusing or closing deletes nothing
 * and keeps the dialog's data for another look.
 */
export interface PruneDialogProps {
  plan: PrunePlanRow[];
  /** The real prune is in flight. */
  busy: boolean;
  /** The confirm's outcome once the real prune answered: the removed files, or the refusal. */
  result: { removed?: string[]; error?: string } | undefined;
  onConfirm: () => void;
  onClose: () => void;
}

export function PruneDialog({ plan, busy, result, onConfirm, onClose }: PruneDialogProps) {
  const { t } = useI18n();
  const title = t("settings.backups.actions.pruneTitle");
  const removals = planRemovals(plan);

  return (
    <Modal
      isOpen
      onClose={onClose}
      title={title}
      ariaLabel={title}
      role="dialog"
      footer={
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            {t("settings.backups.actions.pruneClose")}
          </Button>
          {!result && (
            <Button size="sm" onClick={onConfirm} disabled={busy || removals.length === 0} data-testid="prune-confirm-button">
              {t("settings.backups.actions.pruneConfirm")}
            </Button>
          )}
        </div>
      }
    >
      <p className="text-sm text-slate-600 dark:text-slate-400" data-testid="prune-preview-body">
        {t("settings.backups.actions.prunePreviewBody")}
      </p>

      {removals.length === 0 && !result?.error ? (
        <p className="mt-3 text-sm text-slate-500 dark:text-slate-400" data-testid="prune-nothing-to-remove">
          {t("settings.backups.actions.pruneNothing")}
        </p>
      ) : (
        <ul className="mt-3 space-y-2" data-testid="prune-plan-rows">
          {plan.map((row) => (
            <li key={row.file} className="flex flex-wrap items-center gap-2" data-testid={`prune-row-${row.file}`}>
              <Pill tone={row.keep ? "green" : "red"}>{t(row.keep ? "settings.backups.actions.pruneKeep" : "settings.backups.actions.pruneRemove")}</Pill>
              <span className="font-mono text-sm">{row.file}</span>
              <span className="text-sm text-slate-500 dark:text-slate-400">{row.reason}</span>
            </li>
          ))}
        </ul>
      )}

      {result?.removed && result.removed.length > 0 && (
        <p className="mt-3 text-sm text-slate-600 dark:text-slate-400" data-testid="prune-removed">
          {t("settings.backups.actions.pruneRemoved", { files: result.removed.join(", ") })}
        </p>
      )}
    </Modal>
  );
}
