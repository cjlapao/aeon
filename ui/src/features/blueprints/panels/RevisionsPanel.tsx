import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Panel, Pill, Select } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { rpcTyped, toRpcError, RpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import type { LiveController, LiveState } from "../../../lib/live";
import { DiffPanel } from "./DiffView";
import { BLUEPRINTS_STANDARD_SET, revisionLabel, type BlueprintRevision, type DiffSide } from "../blueprintsModel";

// RevisionsPanel.tsx — the row history T6 keeps and T7 exposes, drawn as
// something a person can read, compare, and turn back.
//
// Two properties the rest of the app takes for granted are the point here:
// a revert ADDES a revision (the history is never rewritten, so the panel shows
// the new revision the daemon answered with, not the old one), and the diff is
// the DAEMON's text — this file picks two sides and asks, it never computes a
// comparison. `shipped` is offered only for a system row, because only a system
// row has shipped bytes to differ from; the daemon refuses the question for a
// user row and this panel does not invite a refusal it can already predict.

export interface RevisionsPanelProps {
  name: string;
  /** From `blueprint.show` — decides whether a shipped comparison exists. */
  origin?: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  /** Called after a revert or a restore so the screen re-reads what changed. */
  onChanged?: () => void;
}

interface DiffState {
  loading: boolean;
  data?: { aLabel: string; bLabel: string; diff: string; equal: boolean };
  error?: RpcError;
}

export function RevisionsPanel({ name, origin, controller, live, canWrite, onChanged }: RevisionsPanelProps) {
  const { t } = useI18n();
  const history = useRpcQuery("blueprint.history", { name, limit: 50 }, controller, live);
  const [sides, setSides] = useState<{ a: DiffSide; b: DiffSide }>({ a: "current", b: "current" });
  const [diff, setDiff] = useState<DiffState>({ loading: false });
  const [note, setNote] = useState("");
  const [outcome, setOutcome] = useState<{ key: string; revision: number } | undefined>(undefined);
  const [error, setError] = useState<RpcError | undefined>(undefined);
  const { confirm, modal } = useTypeToConfirm();

  const revisions: BlueprintRevision[] = Array.isArray(history.data?.revisions) ? history.data.revisions : [];
  const isSystem = origin === "system";

  const compare = async (a: DiffSide, b: DiffSide) => {
    setSides({ a, b });
    setDiff({ loading: true, data: diff.data });
    try {
      setDiff({ loading: false, data: await rpcTyped("blueprint.diff", { name, a, b }) });
    } catch (cause) {
      setDiff({ loading: false, error: toRpcError(cause) });
    }
  };

  const revert = async (rev: number) => {
    setError(undefined);
    // The note is required on the wire: a revision without a reason is a
    // revision nobody can explain later. Pre-filled with the plain fact, and
    // the person may say more.
    const reason = note.trim() !== "" ? note.trim() : t("blueprints.revisions.defaultNote", { revision: rev });
    const agreed = await confirm({
      title: t("blueprints.revisions.revertTitle", { name, revision: rev }),
      description: t("blueprints.revisions.revertBody", { name, revision: rev }),
      confirmValue: name,
      confirmValueLabel: t("blueprints.list.nameLabel"),
      confirmLabel: t("blueprints.revisions.revertConfirm"),
    });
    if (!agreed) return;
    try {
      const result = await rpcTyped("blueprint.revert", { name, rev, note: reason });
      setOutcome({ key: result.changed ? "saved" : "unchanged", revision: result.revision });
      history.refetch();
      onChanged?.();
    } catch (cause) {
      setError(toRpcError(cause));
    }
  };

  const restoreShipped = async () => {
    setError(undefined);
    const agreed = await confirm({
      title: t("blueprints.revisions.restoreTitle", { name }),
      description: t("blueprints.revisions.restoreBody", { name }),
      confirmValue: name,
      confirmValueLabel: t("blueprints.list.nameLabel"),
      confirmLabel: t("blueprints.revisions.restoreConfirm"),
    });
    if (!agreed) return;
    try {
      await rpcTyped("seed.restore", { set: BLUEPRINTS_STANDARD_SET, key: name });
      setOutcome({ key: "saved", revision: 0 });
      history.refetch();
      onChanged?.();
    } catch (cause) {
      setError(toRpcError(cause));
    }
  };

  const sideOptions = (extraShipped: boolean) => [
    { value: "current", label: t("blueprints.diff.current") },
    ...(extraShipped ? [{ value: "shipped", label: t("blueprints.diff.shipped") }] : []),
    ...revisions.map((rev) => ({ value: String(rev.rev), label: `${revisionLabel(rev.rev)} · ${rev.op}` })),
  ];

  return (
    <Panel title={t("blueprints.revisions.title")} subtitle={t("blueprints.revisions.subtitle")} data-testid="revisions-panel">
      {history.loading && !history.data ? (
        <EmptyState title={t("blueprints.revisions.loading")} />
      ) : history.error ? (
        <ErrorPanel error={new RpcError(0, history.error)} onRetry={history.refetch} />
      ) : revisions.length === 0 ? (
        <EmptyState title={t("blueprints.revisions.empty")} />
      ) : (
        <div className="space-y-4">
          <div className="flex flex-wrap items-end gap-2">
            <label className="text-xs">
              <span className="block text-slate-400">{t("blueprints.diff.from")}</span>
              <Select size="sm" aria-label={t("blueprints.diff.from")} value={sides.a} onChange={(e) => setSides((s) => ({ ...s, a: e.target.value }))}>
                {sideOptions(isSystem).map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </Select>
            </label>
            <label className="text-xs">
              <span className="block text-slate-400">{t("blueprints.diff.to")}</span>
              <Select size="sm" aria-label={t("blueprints.diff.to")} value={sides.b} onChange={(e) => setSides((s) => ({ ...s, b: e.target.value }))}>
                {sideOptions(isSystem).map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </Select>
            </label>
            <Button size="sm" variant="outline" disabled={diff.loading} onClick={() => void compare(sides.a, sides.b)} data-testid="compare-revisions">
              {diff.loading ? t("blueprints.diff.comparing") : t("blueprints.diff.compare")}
            </Button>
            {isSystem && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => void compare("shipped", "current")}
                data-testid="compare-shipped"
              >
                {t("blueprints.revisions.compareShipped")}
              </Button>
            )}
          </div>

          {diff.error && <ErrorPanel error={diff.error} onRetry={() => void compare(sides.a, sides.b)} />}
          {diff.data && (
            <DiffPanel
              aLabel={diff.data.aLabel}
              bLabel={diff.data.bLabel}
              diff={diff.data.diff}
              equal={diff.data.equal}
              onClose={() => setDiff({ loading: false })}
            />
          )}

          {canWrite && (
            <label className="block text-xs">
              <span className="block text-slate-400">{t("blueprints.revisions.note")}</span>
              <input
                className="aeon-form-input mt-1 h-8 w-full rounded-lg px-2 text-sm"
                aria-label={t("blueprints.revisions.note")}
                value={note}
                placeholder={t("blueprints.revisions.noteHint")}
                onChange={(event) => setNote(event.target.value)}
              />
            </label>
          )}

          {error && <ErrorPanel error={error} />}
          {outcome && (
            <Alert
              intent={outcome.key === "saved" ? "success" : "info"}
              title={
                outcome.key === "saved"
                  ? outcome.revision > 0
                    ? t("blueprints.revisions.reverted", { revision: outcome.revision })
                    : t("blueprints.revisions.restored")
                  : t("blueprints.editor.unchanged")
              }
              data-testid="revisions-outcome"
            />
          )}

          <ul className="space-y-1">
            {revisions.map((rev) => (
              <li
                key={rev.rev}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm"
                data-testid={`revision-row-${rev.rev}`}
              >
                <Pill tone="blue" size="sm">
                  {revisionLabel(rev.rev)}
                </Pill>
                <span className="text-slate-300">{rev.op}</span>
                <span className="text-xs text-slate-400">{`${rev.actorKind}:${rev.actorId}`}</span>
                {rev.note && <span className="text-slate-200">{rev.note}</span>}
                <RelativeTime iso={rev.occurredAt} />
                {canWrite && (
                  <Button size="xs" variant="outline" onClick={() => void revert(rev.rev)} data-testid={`revert-${rev.rev}`}>
                    {t("blueprints.revisions.revertTo")}
                  </Button>
                )}
              </li>
            ))}
          </ul>

          {isSystem && canWrite && (
            <Button size="sm" variant="outline" onClick={() => void restoreShipped()} data-testid="restore-shipped">
              {t("blueprints.revisions.restoreShipped")}
            </Button>
          )}
        </div>
      )}
      {modal}
    </Panel>
  );
}
