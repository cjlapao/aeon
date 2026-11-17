import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Modal, Pill } from "../../../components/kit";
import { badgeFor, describePlan, type SeedPlan, type SeedSetStatus } from "./seedModel";

/**
 * The read-only plan a set would apply — the read half of the apply flow.
 *
 * `seed.plan` is a read: it never writes. This dialog lists the decisions the
 * daemon returned (op, key, from→to, reason) and a refusal when one is present,
 * so a person can see exactly what an apply would do before they click Apply.
 * It composes nothing of its own about what a set "permits": the ops and reasons
 * are the daemon's, shown verbatim. The one write this dialog can start is the
 * Apply button, which the panel routes through its inert/guard gate.
 */
export interface SeedPlanDialogProps {
  set: SeedSetStatus;
  plan: SeedPlan;
  /** Writes are refused while the connection is not current. */
  canWrite: boolean;
  /** An apply is in flight — the control stays disabled. */
  busy: boolean;
  onApply: () => void;
  onClose: () => void;
}

export function SeedPlanDialog({ set, plan, canWrite, busy, onApply, onClose }: SeedPlanDialogProps) {
  const { t } = useI18n();
  const summary = describePlan(plan);
  const title = t("settings.seed.dialogTitle", { set: set.id });

  return (
    <Modal isOpen onClose={onClose} title={title} ariaLabel={title} role="dialog"
      footer={
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>{t("settings.seed.dialogClose")}</Button>
          <Button size="sm" onClick={onApply} disabled={!canWrite || busy}>{t("settings.seed.dialogApply")}</Button>
        </div>
      }>
      {summary.count === 0 ? (
        <p className="text-sm text-slate-600 dark:text-slate-400">{t("settings.seed.dialogNoDecisions")}</p>
      ) : (
        <ul className="space-y-2" data-testid="seed-plan-decisions">
          {summary.decisions.map((decision) => (
            <li key={decision.key} className="space-y-0.5" data-testid="seed-plan-decision">
              <div className="flex flex-wrap items-center gap-2">
                <Pill tone={badgeFor(decision.state).tone}>{decision.op}</Pill>
                <span className="font-medium">{decision.key}</span>
                <span className="text-sm text-slate-500 dark:text-slate-400">
                  {decision.fromVersion} → {decision.toVersion}
                </span>
              </div>
              {decision.reason !== "" && (
                <p className="text-sm text-slate-600 dark:text-slate-400">{decision.reason}</p>
              )}
            </li>
          ))}
        </ul>
      )}

      {summary.hasRefusal && summary.refusal && (
        <Alert intent="warning" className="mt-3"
          title={`${summary.refusal.key}: ${summary.refusal.reason}`} />
      )}
    </Modal>
  );
}
