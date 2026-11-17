import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, Input, Pill } from "../../../components/kit";
import { useRpcAction } from "../../../lib/useRpcAction";
import { askMergeRulePreview } from "./gatesRpc";
import type { RpcMethods } from "../../../proto/contract";

/**
 * Preview a merge (S57 T11): `mergerule.preview` asks the forge about one
 * PR against the component's effective rule and WRITES nothing. The answer
 * is rendered as sent — the verdict's refusals in the daemon's order and
 * wording, the evidence grid under the head SHA it was evaluated at (the
 * `verify_head_sha` entry of the evidence the daemon itself gathered), and
 * the `cannotSay` line, which is the honest boundary: what a preview is
 * allowed not to know.
 */
type PreviewResult = RpcMethods["mergerule.preview"]["result"];

export interface MergePreviewProps {
  componentId: string;
}

export function MergePreview({ componentId }: MergePreviewProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const [prText, setPrText] = useState("");
  const [answer, setAnswer] = useState<PreviewResult | undefined>(undefined);

  const ask = () => {
    const pr = Number(prText);
    if (!Number.isInteger(pr) || pr <= 0) return;
    void (async () => {
      const result = await action.run(() => askMergeRulePreview(componentId, pr), {
        errorTitle: t("settings.gates.merge.preview.failed"),
      });
      if (result) setAnswer(result);
    })();
  };

  const verdict = answer?.verdict;
  const head = verdict?.evidence?.verify_head_sha;

  return (
    <section className="space-y-3" data-testid="gates-preview">
      <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.gates.q.try")}</h3>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Input
          variant="ghost"
          aria-label={t("settings.gates.merge.preview.pr")}
          className="w-32"
          inputMode="numeric"
          value={prText}
          onChange={(event) => setPrText(event.target.value)}
          placeholder={t("settings.gates.merge.preview.pr")}
          data-testid="gates-preview-pr"
        />
        <Button size="sm" onClick={ask} disabled={action.busy} data-testid="gates-preview-ask">
          {t("settings.gates.merge.preview.ask")}
        </Button>
      </div>

      {answer && (
        <div className="space-y-2 rounded-sm border border-[var(--border)] p-3 text-sm" data-testid="gates-preview-answer">
          <div className="flex flex-wrap items-center gap-2">
            <Pill size="sm" tone={verdict?.allowed ? "emerald" : "rose"} data-testid="gates-preview-verdict">
              {t(verdict?.allowed ? "settings.gates.merge.preview.allowed" : "settings.gates.merge.preview.refused")}
            </Pill>
            {typeof head === "string" && head !== "" && (
              <span className="font-mono text-xs opacity-80" data-testid="gates-preview-head">
                {t("settings.gates.merge.preview.head", { sha: head.slice(0, 12) })}
              </span>
            )}
          </div>
          {(verdict?.refusals ?? []).length > 0 && (
            <ul className="list-disc space-y-1 pl-5" data-testid="gates-preview-refusals">
              {(verdict?.refusals ?? []).map((refusal, index) => (
                <li key={`${refusal.code}-${index}`} data-testid={`gates-preview-refusal-${index}`}>
                  {refusal.sentence}
                </li>
              ))}
            </ul>
          )}
          {verdict?.evidence && (
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs" data-testid="gates-preview-evidence">
              {Object.entries(verdict.evidence).map(([key, value]) => (
                <div key={key} className="contents">
                  <dt className="font-mono opacity-70">{key}</dt>
                  <dd>{String(value)}</dd>
                </div>
              ))}
            </dl>
          )}
          {answer.cannotSay && (
            <p className="text-xs italic opacity-80" data-testid="gates-preview-cannotsay">
              {answer.cannotSay}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
