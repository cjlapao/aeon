import { useState, type ReactNode } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button } from "../../../components/kit";

/**
 * AuthorityConfirm — the arm/fire/unarchive confirmation whose words are the
 * daemon's (AN12/AN13): the caller passes `authorityStatement` from
 * `automation.vocabulary` verbatim, plus the health answer and the preview
 * sentence for this trigger, and this component arranges them over a
 * cancel-safe modal with one deliberate confirm control.
 *
 * What the component itself adds is chrome and nothing else — no sentence
 * about what arming means (that is `authorityStatement`'s job), no
 * auto-confirm, no remembering the answer. An enabling action that skipped
 * this modal would have to reach past a control the person must press,
 * which is exactly the friction the plane's `--yes` seatbelt is on the
 * terminal: the same authority, here as a click.
 */
export interface AuthorityConfirmProps {
  open: boolean;
  /** The daemon's authority statement (`automation.vocabulary`). */
  authorityStatement: string;
  /** Anything else the daemon said about THIS trigger — description
   *  sentence, preview instants, health reasons — passed in as rendered
   *  nodes the caller built from daemon answers only. */
  children?: ReactNode;
  confirmLabel: string;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export function AuthorityConfirm({ open, authorityStatement, children, confirmLabel, busy = false, onConfirm, onCancel }: AuthorityConfirmProps) {
  const { t } = useI18n();
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog" aria-modal="true" data-testid="authority-confirm">
      <div className="w-full max-w-lg space-y-3 rounded-xl bg-white p-5 shadow-xl dark:bg-slate-900">
        {/* The daemon's statement, rendered as the authority it is — the one
            sentence the plane writes once and every surface quotes. */}
        <Alert intent="warning" title={authorityStatement} data-testid="authority-statement" />
        {children}
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel} data-testid="authority-cancel">
            {t("settings.automation.confirm.cancel")}
          </Button>
          <Button size="sm" disabled={busy} onClick={onConfirm} data-testid="authority-confirm-go">
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * ReasonModal — disable and archive REQUIRE a reason (the doors refuse
 * without one; AN6 says archive is a decision with a stated why, not a bin).
 * The reason the person types travels to the daemon verbatim and returns in
 * the audit row — this component only refuses to send an empty one.
 */
export interface ReasonModalProps {
  open: boolean;
  title: string;
  /** Anything the caller shows for context (the row's description sentence). */
  children?: ReactNode;
  confirmLabel: string;
  busy?: boolean;
  error?: string;
  onSubmit: (reason: string) => void;
  onCancel: () => void;
}

export function ReasonModal({ open, title, children, confirmLabel, busy = false, error, onSubmit, onCancel }: ReasonModalProps) {
  const { t } = useI18n();
  const [reason, setReason] = useState("");
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog" aria-modal="true" data-testid="reason-modal">
      <div className="w-full max-w-lg space-y-3 rounded-xl bg-white p-5 shadow-xl dark:bg-slate-900">
        <p className="text-sm font-medium">{title}</p>
        {children}
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.automation.reason.label")}</span>
          <textarea
            aria-label={t("settings.automation.reason.label")}
            className="aeon-form-input min-h-16 w-full rounded-lg border p-2 text-sm"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>
        {error !== undefined && <Alert intent="danger" title={error} />}
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel} data-testid="reason-cancel">
            {t("settings.automation.confirm.cancel")}
          </Button>
          <Button
            size="sm"
            disabled={busy || reason.trim() === ""}
            onClick={() => onSubmit(reason.trim())}
            data-testid="reason-submit"
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
