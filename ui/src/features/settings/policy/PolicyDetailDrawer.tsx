import { useCallback, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, InfoRow, Pill } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { RpcError, rpcTyped } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import type { LiveController, LiveState } from "../../../lib/live";
import { actionTone } from "./PolicyList";
import { describeDiff, type InForceAnswerView } from "./policyModel";

/**
 * The detail drawer for one policy (`policy.get`): every field, the raw
 * `spec` read-only in a code block, the `describe` sentence verbatim, the
 * acceptance and authored flags, and the authoring audit for that id
 * (`policy.changes {policyId}`).
 *
 * Remove is the only write a row affords — policies have **no edit**
 * (PM16): the drawer says so with the documented rule and offers no edit
 * control. The confirmation names the policy, carries its `describe`
 * sentence and the target's current in-force answer (a `policy.inForce`
 * read, never a client-side guess); a system row's confirmation adds the
 * daemon's own PM16 statement — the shipped rule, removal allowed, a
 * re-seed will not restore it. The result is the door's `before`/`after`
 * rendered verbatim.
 */
export interface PolicyDetailDrawerProps {
  id: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  /** After an accepted removal, the section re-reads the list. */
  onChanged: () => void;
}

/** The in-force door answers questions for the two identities its params
 *  carry (`agent`, `loop` — the wire shape, not the vocabulary); the drawer
 *  can only ask the target's in-force answer when the target names one. */
function inForceParamsFor(target: string): { agent: string; loop?: string } | undefined {
  const split = target.indexOf(":");
  const kind = split === -1 ? target : target.slice(0, split);
  const id = split === -1 ? "" : target.slice(split + 1);
  if (kind === "agent" && id !== "") return { agent: id };
  if (kind === "loop" && id !== "") return { agent: "", loop: id };
  return undefined;
}

function InForceBlock({ view, label }: { view?: InForceAnswerView; label: string }) {
  const { t } = useI18n();
  const answers = view?.answers ?? [];
  return (
    <div className="space-y-1" data-testid="inforce-block">
      <p className="text-xs uppercase tracking-wide opacity-70">{label}</p>
      {view?.target ? <p className="font-mono text-xs">{view.target}</p> : null}
      {answers.length === 0 ? (
        <p className="text-sm opacity-70">{t("settings.policy.drawer.inForceNone")}</p>
      ) : (
        <ul className="space-y-1 text-sm" data-testid="inforce-answers">
          {answers.map((answer, index) => (
            <li key={`${answer.policyID ?? "row"}-${index}`} className="space-x-2">
              {answer.state !== undefined && <Pill tone={actionTone(answer.state)}>{answer.state}</Pill>}
              {answer.policyID !== undefined && <span className="font-mono text-xs">{answer.policyID}</span>}
              {/* Render() output — the one formatter's line, byte for byte. */}
              {answer.render !== undefined && <span>{answer.render}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function PolicyDetailDrawer({ id, controller, live, canWrite, onClose, onChanged }: PolicyDetailDrawerProps) {
  const { t } = useI18n();
  const detail = useRpcQuery("policy.get", { id }, controller, live);
  const audit = useRpcQuery("policy.changes", { policyId: id }, controller, live);
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [result, setResult] = useState<{ before?: InForceAnswerView; after?: InForceAnswerView; removed?: boolean } | null>(null);
  const [inForceFailed, setInForceFailed] = useState<string | undefined>(undefined);

  const policy = detail.data?.policy;
  const isSystem = policy?.origin === "system";

  const remove = useCallback(async () => {
    if (!policy) return;
    setResult(null);
    setInForceFailed(undefined);
    // The target's CURRENT in-force answer, fetched with a real
    // `policy.inForce` read — the confirmation quotes what the daemon
    // answers now, never a client-side reconstruction.
    let currentInForce: InForceAnswerView | undefined;
    const params = inForceParamsFor(policy.target);
    if (params) {
      const answer = await action.run(() => rpcTyped("policy.inForce", params), {
        errorTitle: t("settings.policy.drawer.inForceFailed"),
      });
      if (answer) currentInForce = answer;
      else setInForceFailed(action.error ?? t("settings.policy.drawer.inForceFailed"));
    }
    const lines = (currentInForce?.answers ?? []).map((a) => a.render ?? a.state ?? "").filter((line) => line !== "");
    const description = [
      policy.describe,
      lines.length > 0 ? lines.join(" | ") : t("settings.policy.drawer.inForceNone"),
      params === undefined ? t("settings.policy.drawer.inForceNA") : "",
      inForceFailed ? `${t("settings.policy.drawer.inForceUnreadable")} ${inForceFailed}` : "",
      // The daemon's own PM16 statement, quoted for the system row — the
      // shipped rule, the operator may remove it, and re-seeding will not
      // put it back (restore is explicit). The words are the daemon's.
      isSystem ? t("settings.policy.drawer.systemLine") : "",
    ]
      .filter((line) => line !== "")
      .join("\n");
    const confirmed = await confirm({
      title: t("settings.policy.drawer.removeConfirmTitle", { id: policy.id }),
      description,
      confirmValue: policy.id,
      confirmValueLabel: t("settings.policy.drawer.confirmValueLabel"),
      confirmLabel: t("settings.policy.drawer.remove"),
    });
    if (!confirmed) return; // decline sends nothing
    const removed = await action.run(() => rpcTyped("policy.remove", { id: policy.id }), {
      errorTitle: t("settings.policy.drawer.removeFailed"),
    });
    if (removed) {
      // The door's before/after, rendered verbatim — the effect, shown.
      setResult({ before: removed.before as InForceAnswerView, after: removed.after as InForceAnswerView, removed: removed.removed });
      onChanged();
    }
  }, [action, confirm, isSystem, onChanged, policy, t]);

  // Every state opens in the same right-edge panel: a loading or failed read
  // must not render inline under the table while the loaded one slides in.
  const shell = (title: string, node: React.ReactNode, testId?: string) => (
    <OverlaySidePanel title={title} titleClassName="text-xl font-semibold" variant="simple" onClose={onClose} testId={testId}>{node}</OverlaySidePanel>
  );
  if (detail.loading) return shell(id, <EmptyState title={t("settings.policy.drawer.loading")} />);
  if (detail.error) return shell(id, <ErrorPanel error={new RpcError(0, detail.error)} onRetry={detail.refetch} />);
  if (!policy) return shell(id, <EmptyState title={t("settings.policy.drawer.notFound")} />);

  return (
    <OverlaySidePanel titleClassName="text-xl font-semibold" variant="simple" title={t("settings.policy.drawer.title", { id: policy.id })} onClose={onClose} testId="policy-drawer">
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Pill tone={actionTone(policy.action)}>{policy.action}</Pill>
          {policy.origin ? <Pill tone={isSystem ? "blue" : "slate"}>{policy.origin}</Pill> : null}
          <span className="font-mono text-sm">{policy.type}</span>
          <span className="font-mono text-sm">{policy.target}</span>
          <span className="text-xs opacity-70">{policy.source}</span>
          {policy.createdAt ? <RelativeTime iso={policy.createdAt} prefix={t("settings.policy.drawer.created")} /> : null}
        </div>

        {/* The one formatter's sentence, verbatim. */}
        <p className="text-sm" data-testid="drawer-describe">{policy.describe}</p>

        <InfoRow label={t("settings.policy.drawer.accepted")} value={policy.acceptedBy ? `${policy.acceptedBy} · ${policy.acceptedAt ?? ""}` : t("settings.policy.drawer.acceptedNone")} />
        <InfoRow label={t("settings.policy.drawer.authored")} value={policy.authored ? t("settings.policy.drawer.authoredYes") : t("settings.policy.drawer.authoredNo")} />
        <InfoRow label={t("settings.policy.drawer.expiry")} value={policy.notAfter ?? t("settings.policy.list.noExpiry")} />

        {/* The raw spec, read-only — the bytes the boundary validated. */}
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.drawer.spec")}</p>
          <pre className="overflow-x-auto rounded-md bg-slate-100 p-2 text-xs dark:bg-slate-900" data-testid="drawer-spec" aria-readonly="true">
            {typeof policy.spec === "string" ? policy.spec : JSON.stringify(policy.spec, null, 2)}
          </pre>
        </div>

        {isSystem && (
          <Alert intent="info" title={t("settings.policy.drawer.pl4Hint")} data-testid="drawer-pl4-hint" />
        )}

        {result && (
          <div className="space-y-2" data-testid="remove-result">
            <Alert
              intent={result.removed ? "success" : "warning"}
              title={result.removed ? t("settings.policy.drawer.removed") : t("settings.policy.drawer.notRemoved")}
            />
            <div className="grid gap-3 md:grid-cols-2">
              <InForceBlock view={result.before} label={t("settings.policy.before")} />
              <InForceBlock view={result.after} label={t("settings.policy.after")} />
            </div>
            {describeDiff(result.before, result.after).changed && (
              <p className="text-xs opacity-70">{t("settings.policy.drawer.diffChanged")}</p>
            )}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 pt-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            {t("settings.policy.drawer.close")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            color="red"
            disabled={!canWrite || action.busy}
            onClick={() => void remove()}
            title={!canWrite ? t("settings.policy.reconnecting") : undefined}
          >
            {t("settings.policy.drawer.remove")}
          </Button>
        </div>

        {/* The authoring audit for this id — `policy.changes {policyId}`. */}
        <div className="space-y-1" data-testid="drawer-audit">
          <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.drawer.audit")}</p>
          {audit.loading ? (
            <p className="text-sm opacity-70">{t("settings.policy.audit.loading")}</p>
          ) : audit.error ? (
            <p className="text-sm text-red-600 dark:text-red-400">{audit.error}</p>
          ) : (audit.data?.changes ?? []).length === 0 ? (
            <p className="text-sm opacity-70">{audit.data?.statement ?? t("settings.policy.audit.noneForId")}</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {(audit.data?.changes ?? []).map((change) => (
                <li key={change.id} className="flex flex-wrap gap-2">
                  <Pill tone="slate">{change.op}</Pill>
                  <span className="font-mono text-xs">
                    {change.actorKind}:{change.actorId}
                  </span>
                  <span>{change.summary}</span>
                  <RelativeTime iso={change.occurredAt} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      {modal}
    </OverlaySidePanel>
  );
}
