import { useState, type ReactNode } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Input } from "../../../components/kit";

/**
 * OperationConfirm — the modal in front of every System operation
 * (S54 T6, SP8; RM9's shape with no authority statement to quote: the
 * platform plane has no vocabulary door, so what the person sees BEFORE
 * the click is the daemon's DATA — the `retrieval.reindexPlan` answer for
 * a reindex, the named root for a graph act — arranged by the caller from
 * response fields only).
 *
 * What this component adds is chrome: no sentence about what the
 * operation means beyond the panel's translated labels, no auto-confirm,
 * no remembering the answer, and the daemon's refusal (the already-running
 * sentence that names its starter included) printed verbatim inside the
 * open modal. This is the click that `--yes` is on the terminal.
 */
export interface OperationConfirmProps {
  open: boolean;
  title: string;
  /** The daemon's data for THIS operation, composed by the caller out of
   *  answer fields (the plan block, the counts, the root). */
  children?: ReactNode;
  confirmLabel: string;
  busy?: boolean;
  /** A daemon sentence (refusal or starter naming), shown verbatim. */
  error?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function OperationConfirm({ open, title, children, confirmLabel, busy = false, error, onConfirm, onCancel }: OperationConfirmProps) {
  const { t } = useI18n();
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog" aria-modal="true" data-testid="operation-confirm">
      <div className="w-full max-w-lg space-y-3 rounded-xl bg-white p-5 shadow-xl dark:bg-slate-900">
        <p className="text-sm font-medium">{title}</p>
        {children}
        {error !== undefined && <Alert intent="danger" title={error} data-testid="operation-error" />}
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel} data-testid="operation-cancel">
            {t("settings.system.actions.cancel")}
          </Button>
          <Button size="sm" disabled={busy} onClick={onConfirm} data-testid="operation-go">
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * ReparseConfirm — the graph.reparse modal: the fixed statement (this
 * REPLACES the file's derived rows; source files are never touched — the
 * panel's own translated words, matching the CLI's statement clause for
 * clause) plus the one field the door needs that the panel cannot know:
 * the path, typed. The confirm refuses to send an empty one.
 */
export interface ReparseConfirmProps {
  open: boolean;
  root: string;
  busy?: boolean;
  error?: string;
  onSubmit: (path: string) => void;
  onCancel: () => void;
}

export function ReparseConfirm({ open, root, busy = false, error, onSubmit, onCancel }: ReparseConfirmProps) {
  const { t } = useI18n();
  const [path, setPath] = useState("");
  if (!open) return null;
  return (
    <OperationConfirm
      open={open}
      title={t("settings.system.graph.reparse.title", { root })}
      confirmLabel={t("settings.system.actions.confirmReparse")}
      busy={busy}
      error={error}
      onConfirm={() => onSubmit(path.trim())}
      onCancel={onCancel}
    >
      <p className="text-sm text-slate-600 dark:text-slate-400" data-testid="reparse-statement">
        {t("settings.system.graph.reparse.statement")}
      </p>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.system.graph.reparse.path")}</span>
        <Input
          aria-label={t("settings.system.graph.reparse.path")}
          data-testid="reparse-path"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="internal/store/store.go"
        />
      </label>
    </OperationConfirm>
  );
}
