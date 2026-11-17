import { useCallback, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import type { LiveController, LiveState } from "../../../lib/live";
import { PolicyForm } from "../policy/PolicyForm";
import type { Vocabulary } from "../policy/policyModel";
import { declareNeedsAllow, describeBeforeAfter, type AgentGrants, type CoreSetParamsWire, type GrantsAnswer } from "./mcpModel";
import type { RpcMethods } from "../../../proto/contract";

/**
 * The grant half's writes (MG7): **two steps, never one call**. Declare
 * goes through `mcp.grant.declare` with a before/after confirm; when the
 * response shows the fresh declaration `unavailable (no allow)` the UI
 * OFFERS "Allow in policy…" — which opens S45's own authoring form
 * prefilled and lets S45's own confirmation run. There is no code path
 * from declare to policy.add that skips the person.
 */
export interface GrantFlowProps {
  agent: string;
  answer: GrantsAnswer;
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  onChanged: () => void;
}

export function GrantFlow({ agent, answer, canWrite, controller, live, onChanged }: GrantFlowProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [qualified, setQualified] = useState("");
  const [allowFor, setAllowFor] = useState<string | undefined>(undefined);
  const [allowOpen, setAllowOpen] = useState(false);
  const [lastResult, setLastResult] = useState<string | undefined>(undefined);

  const policyVocabulary = useRpcQuery("policy.vocabulary", {}, controller, live);
  const vocabulary = policyVocabulary.data?.vocabulary as Vocabulary | undefined;

  const doDeclare = useCallback(async () => {
    if (qualified === "") return;
    const before = answer.grants;
    const confirmed = await confirm({
      title: t("settings.mcp.grants.declare.title", { agent, qualified }),
      description: describeBeforeAfter(before, [{ qualified, state: "?", tier: "declared" }]).changed
        ? t("settings.mcp.grants.declare.willAdd", { qualified })
        : t("settings.mcp.grants.declare.alreadyListed", { qualified }),
      confirmValue: agent,
      confirmValueLabel: t("settings.mcp.grants.confirm.agentLabel"),
      confirmLabel: t("settings.mcp.grants.declare.confirm"),
    });
    if (!confirmed) return;
    setAllowFor(undefined);
    setAllowOpen(false);
    const result = await action.run(() => rpcTyped("mcp.grant.declare", { agent, qualified }), { errorTitle: t("settings.mcp.grants.declare.failed") });
    if (!result) return;
    const entry = (result.grants as unknown as AgentGrants[]).find((candidate) => candidate.agent === agent) ?? result.grants[0];
    const after = ((entry?.after ?? []) as unknown as GrantsAnswer["grants"]) ?? [];
    const diff = describeBeforeAfter(before, after);
    setLastResult(
      diff.changed
        ? `${t("settings.mcp.grants.after", { added: diff.added.join(", "), removed: diff.removed.join(", "), changed: diff.changedState.map((row) => `${row.qualified}: ${row.from} → ${row.to}`).join(", ") })}`
        : t("settings.mcp.grants.unchanged"),
    );
    if (declareNeedsAllow(after, qualified)) {
      // The MG7 moment: the declaration exists, policy has not spoken.
      setAllowFor(qualified);
    }
    onChanged();
  }, [action, agent, answer.grants, confirm, onChanged, qualified, t]);

  const doRevoke = useCallback(async () => {
    if (qualified === "") return;
    const confirmed = await confirm({
      title: t("settings.mcp.grants.revoke.title", { agent, qualified }),
      description: t("settings.mcp.grants.revoke.narrowing"),
      confirmValue: agent,
      confirmValueLabel: t("settings.mcp.grants.confirm.agentLabel"),
      confirmLabel: t("settings.mcp.grants.revoke.confirm"),
    });
    if (!confirmed) return;
    setAllowFor(undefined);
    setAllowOpen(false);
    const result = await action.run(() => rpcTyped("mcp.grant.revoke", { agent, qualified }), { errorTitle: t("settings.mcp.grants.revoke.failed") });
    if (result) {
      setLastResult(t("settings.mcp.grants.revoked", { qualified }));
      onChanged();
    }
  }, [action, agent, confirm, onChanged, qualified, t]);

  const setCore = useCallback(
    async (core: string[] | null) => {
      const whole = core === null;
      const confirmed = await confirm({
        title: t(whole ? "settings.mcp.grants.core.whole" : "settings.mcp.grants.core.set", { agent }),
        description: whole ? t("settings.mcp.grants.core.wholeBody") : t("settings.mcp.grants.core.setBody", { core: (core ?? []).join(", ") || "—" }),
        confirmValue: agent,
        confirmValueLabel: t("settings.mcp.grants.confirm.agentLabel"),
        confirmLabel: t("settings.mcp.grants.core.confirm"),
      });
      if (!confirmed) return;
      // null (the WHOLE set) and [] (none) are different facts on the wire
      // — the wire shim types it honestly; the contract flattened the pointer.
      const params = { agent, core } as CoreSetParamsWire as RpcMethods["mcp.core.set"]["params"];
      const result = await action.run(() => rpcTyped("mcp.core.set", params), { errorTitle: t("settings.mcp.grants.core.failed") });
      if (result) {
        setLastResult(t("settings.mcp.grants.core.done", { agent }));
        onChanged();
      }
    },
    [action, agent, confirm, onChanged, t],
  );

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

  return (
    <section className="space-y-3" data-testid="grant-flow">
      <h3 className={H}>{t("settings.mcp.grants.qChange")}</h3>
      {action.error && <Alert intent="danger" title={action.error} />}
      {lastResult && <div data-testid="grant-result"><Alert intent="success" title={lastResult} /></div>}
      <div className="flex flex-wrap items-end gap-2">
        <div className="min-w-48 flex-1">
          <FormField label={t("settings.mcp.grants.qualified")}>
            <Input
              variant="ghost"
              aria-label={t("settings.mcp.grants.qualified")}
              value={qualified}
              placeholder={t("settings.mcp.grants.qualifiedHint")}
              disabled={!canWrite || action.busy}
              onChange={(event) => setQualified(event.target.value)}
            />
          </FormField>
        </div>
        <Button size="sm" disabled={!canWrite || action.busy || qualified === ""} onClick={() => void doDeclare()} title={!canWrite ? t("settings.mcp.reconnecting") : undefined} data-testid="declare-button">
          {t("settings.mcp.grants.declare.button")}
        </Button>
        <Button size="sm" variant="ghost" disabled={!canWrite || action.busy || qualified === ""} onClick={() => void doRevoke()} data-testid="revoke-button">
          {t("settings.mcp.grants.revoke.button")}
        </Button>
      </div>
      {allowFor && (
        <div className="space-y-2" data-testid="allow-in-policy">
          <p className="text-sm" data-testid="allow-offer">{t("settings.mcp.grants.allowOffer", { qualified: allowFor })}</p>
          <Button size="sm" disabled={!canWrite} onClick={() => setAllowOpen(true)} data-testid="allow-open">{t("settings.mcp.grants.allowInPolicy")}</Button>
        </div>
      )}
      {allowFor !== undefined && allowOpen && (
        // S45's ONE authoring form, prefilled (tool_access · allow · operator, target = the
        // agent, tool = the qualified name), in its own side panel — and S45's own
        // confirmation stands between the offer and policy.add.
        <OverlaySidePanel title={t("settings.mcp.grants.allowInPolicy")} titleClassName="text-xl font-semibold" variant="simple" width={600} onClose={() => setAllowOpen(false)}>
          <PolicyForm
            vocabulary={vocabulary}
            canWrite={canWrite}
            controller={controller}
            live={live}
            busy={false}
            onCancel={() => setAllowOpen(false)}
            onAdded={() => {
              setAllowOpen(false);
              onChanged();
            }}
            initial={{
              type: "tool_access",
              targetKind: "agent",
              targetId: agent,
              action: "allow",
              source: "operator",
              spec: { toolName: allowFor },
            }}
          />
        </OverlaySidePanel>
      )}
      <div className="flex flex-wrap items-center gap-2 pt-2">
        <span className="text-xs opacity-60">{t("settings.mcp.grants.core.label")}</span>
        <Button size="xs" variant="ghost" disabled={!canWrite || action.busy} onClick={() => void setCore(null)} data-testid="core-whole-button">{t("settings.mcp.grants.core.wholeButton")}</Button>
        <Button size="xs" variant="ghost" disabled={!canWrite || action.busy} onClick={() => void setCore([])} data-testid="core-none-button">{t("settings.mcp.grants.core.noneButton")}</Button>
      </div>
      {modal}
    </section>
  );
}
