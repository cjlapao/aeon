import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { RpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { TrueColor } from "@cjlapao/ui-kit";

/**
 * The authoring audit — `policy.changes`, newest first (the door's own
 * order; the UI never re-sorts). Every row is the daemon's summary verbatim.
 *
 * The three states are three different answers: rows, the EMPTY TABLE's own
 * sentence (the daemon sends it only when the audit provably holds none),
 * and a failure. The `unauditedPolicyIds` reconciliation is its own warning
 * — "these policies have no audit row" names the crash gap between a write
 * and its record (DEF-041's third state); it is never collapsed into
 * "nothing happened", and the warning states it as the reconciliation it
 * is while naming the ids exactly as the daemon sent them.
 */
export interface AuthoringAuditProps {
  controller: LiveController;
  live: LiveState;
  /** When set, the audit is filtered to one policy (the drawer's history). */
  policyId?: string;
}

function opTone(op: string): TrueColor {
  switch (op) {
    case "add":
      return "emerald";
    case "remove":
      return "rose";
    case "seed":
      return "blue";
    case "commit":
      return "violet";
    case "reject":
      return "red";
    default:
      return "neutral";
  }
}

export function AuthoringAudit({ controller, live, policyId }: AuthoringAuditProps) {
  const { t } = useI18n();
  const changes = useRpcQuery("policy.changes", policyId === undefined ? {} : { policyId }, controller, live);

  const rows = changes.data?.changes ?? [];
  const unaudited = (changes.data?.unauditedPolicyIds ?? []) as unknown[];
  const statement = changes.data?.statement;

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t("settings.policy.audit.title")}</h2>
        <p className="mt-1 text-sm opacity-70">{t("settings.policy.audit.subtitle")}</p>
      </div>
      <div className="mb-3 flex justify-end">
        <Button size="sm" variant="ghost" onClick={changes.refetch}>
          {t("settings.policy.refresh")}
        </Button>
      </div>
      {changes.loading ? (
        <EmptyState title={t("settings.policy.audit.loading")} />
      ) : changes.error ? (
        <ErrorPanel error={new RpcError(0, changes.error)} onRetry={changes.refetch} />
      ) : (
        <div className="space-y-3">
          {unaudited.length > 0 && (
            <Alert
              intent="warning"
              title={t("settings.policy.audit.unauditedWarning")}
              description={`${t("settings.policy.audit.unauditedBody")} ${unaudited.map((id) => String(id)).join(", ")}`}
              data-testid="unaudited-warning"
            />
          )}
          {rows.length === 0 && unaudited.length === 0 ? (
            // The empty table's OWN sentence, or the honest absence of one —
            // never "nothing happened" dressed as the daemon's answer.
            <EmptyState
              title={statement ?? t("settings.policy.list.noStatement")}
              subtitle={statement === undefined ? t("settings.policy.list.noStatementBody") : undefined}
            />
          ) : (
            <table className="w-full border-collapse text-sm" data-testid="audit-table">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide opacity-70">
                  <th className="px-2 py-1">{t("settings.policy.audit.col.op")}</th>
                  <th className="px-2 py-1">{t("settings.policy.audit.col.actor")}</th>
                  <th className="px-2 py-1">{t("settings.policy.audit.col.policy")}</th>
                  <th className="px-2 py-1">{t("settings.policy.audit.col.summary")}</th>
                  <th className="px-2 py-1">{t("settings.policy.audit.col.when")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((change) => (
                  <tr key={change.id} className="border-t border-slate-200 dark:border-slate-800" data-testid="audit-row">
                    <td className="px-2 py-1">
                      <Pill tone={opTone(change.op)}>{change.op}</Pill>
                    </td>
                    <td className="px-2 py-1 font-mono text-xs">
                      {change.actorKind}:{change.actorId}
                    </td>
                    <td className="px-2 py-1 font-mono text-xs">
                      {change.policyId ?? (change.proposalId !== undefined ? `#${change.proposalId}` : "—")}
                    </td>
                    {/* The daemon's own summary line, verbatim. */}
                    <td className="px-2 py-1">{change.summary}</td>
                    <td className="px-2 py-1 text-xs">
                      <RelativeTime iso={change.occurredAt} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </section>
  );
}
