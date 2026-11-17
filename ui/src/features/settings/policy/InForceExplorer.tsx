import { useCallback, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill } from "../../../components/kit";
import { Input } from "../../../components/kit";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { AgentTagPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { SpecFields } from "./SpecFields";
import { actionTone } from "./PolicyList";
import {
  specFieldsFor,
  type InForceAnswerView,
  type Vocabulary,
  type WhyAnswer,
  type WhyParamsWire,
} from "./policyModel";

/**
 * The in-force / why explorer — the D14 window on the daemon's answers.
 *
 * The form collects the identities the doors ask about (agent via the
 * picker, loop, lane type, MCP server, type, the question's own fields from
 * the type's spec descriptor, `at`); the two buttons run `policy.inForce`
 * and `policy.why` and render the daemon's answers **verbatim**. The three
 * states are three visibly different results: an allow, a deny, and — its
 * own state, never a denial (SEC-033) — `unimplemented`, "the engine has no
 * answer yet", shown as such. Nothing here computes a verdict: a UI that
 * re-derived one is a second truth.
 */
export interface InForceExplorerProps {
  vocabulary: Vocabulary | undefined;
  controller: LiveController;
  live: LiveState;
}

/** The why door's question fields carry the same names the descriptors use,
 *  except the two the protocol names differently on the why side. */
function whyParamName(fieldName: string): string {
  if (fieldName === "toolName") return "tool";
  if (fieldName === "pattern") return "command";
  return fieldName;
}

export function InForceExplorer({ vocabulary, controller, live }: InForceExplorerProps) {
  const { t } = useI18n();
  const [agent, setAgent] = useState("");
  const [loop, setLoop] = useState("");
  const [laneType, setLaneType] = useState("");
  const [mcp, setMcp] = useState("");
  const [type, setType] = useState("");
  const [at, setAt] = useState("");
  const [question, setQuestion] = useState<Record<string, string>>({});
  const [inForce, setInForce] = useState<InForceAnswerView | null>(null);
  const [why, setWhy] = useState<WhyAnswer | null>(null);
  const action = useRpcAction();

  const fields = useMemo(() => (vocabulary && type !== "" ? specFieldsFor(vocabulary, type) : []), [vocabulary, type]);

  const askInForce = useCallback(async () => {
    setWhy(null);
    const result = await action.run(
      () =>
        rpcTyped("policy.inForce", {
          agent,
          ...(loop !== "" ? { loop } : {}),
        }),
      { errorTitle: t("settings.policy.inForce.inForceFailed") },
    );
    if (result) setInForce(result as InForceAnswerView);
  }, [action, agent, loop, t]);

  const askWhy = useCallback(async () => {
    setInForce(null);
    if (type === "") return;
    const params: WhyParamsWire = { type, agent };
    if (laneType !== "") params.laneType = laneType;
    if (mcp !== "") params.mcp = mcp;
    if (at !== "") params.at = at;
    for (const [name, value] of Object.entries(question)) {
      if (value.trim() === "") continue;
      (params as Record<string, unknown>)[whyParamName(name)] = value.trim();
    }
    const result = await action.run(() => rpcTyped("policy.why", params), {
      errorTitle: t("settings.policy.inForce.whyFailed"),
    });
    if (result) setWhy(result);
  }, [action, agent, at, laneType, mcp, question, t, type]);

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t("settings.policy.inForce.title")}</h2>
        <p className="mt-1 text-sm opacity-70">{t("settings.policy.inForce.subtitle")}</p>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.agent")}</p>
          <AgentTagPicker value={agent} onChange={setAgent} controller={controller} live={live} />
        </div>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.loop")}</span>
          <Input aria-label={t("settings.policy.inForce.loop")} value={loop} onChange={(e) => setLoop(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.laneType")}</span>
          <Input aria-label={t("settings.policy.inForce.laneType")} value={laneType} onChange={(e) => setLaneType(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.mcp")}</span>
          <Input aria-label={t("settings.policy.inForce.mcp")} value={mcp} onChange={(e) => setMcp(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.type")}</span>
          <select
            aria-label={t("settings.policy.inForce.type")}
            className="rounded-md border border-slate-300 bg-transparent px-2 py-1 dark:border-slate-700"
            value={type}
            onChange={(e) => setType(e.target.value)}
          >
            <option value="">—</option>
            {(vocabulary?.types ?? []).map((option) => (
              <option key={option} value={option}>{option}</option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.at")}</span>
          <Input aria-label={t("settings.policy.inForce.at")} value={at} placeholder={t("settings.policy.inForce.atPlaceholder")} onChange={(e) => setAt(e.target.value)} />
        </label>
        {type !== "" && (
          <div className="md:col-span-2 space-y-2">
            <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.question")}</p>
            <SpecFields fields={fields} values={question} onChange={(name, value) => setQuestion((q) => ({ ...q, [name]: value }))} />
          </div>
        )}
      </div>

      {action.error && <Alert intent="danger" title={action.error} className="mt-3" data-testid="explorer-error" />}

      <div className="mt-4 flex gap-2">
        <Button size="sm" variant="outline" disabled={action.busy} onClick={() => void askInForce()}>
          {t("settings.policy.inForce.askInForce")}
        </Button>
        <Button size="sm" disabled={action.busy || type === ""} onClick={() => void askWhy()}>
          {t("settings.policy.inForce.askWhy")}
        </Button>
      </div>

      {inForce && (
        <div className="mt-4 space-y-2" data-testid="inforce-result">
          <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.inForce.answer")}</p>
          {(inForce.answers ?? []).length === 0 ? (
            <p className="text-sm opacity-70">{t("settings.policy.inForce.noAnswers")}</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {(inForce.answers ?? []).map((answer, index) => (
                <li key={index} className="flex flex-wrap items-center gap-2" data-testid="inforce-answer">
                  {answer.state !== undefined && <Pill tone={actionTone(answer.state)}>{answer.state}</Pill>}
                  {answer.policyID !== undefined && <span className="font-mono text-xs">{answer.policyID}</span>}
                  {answer.render !== undefined && <span>{answer.render}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {why && (
        <div className="mt-4" data-testid="why-result">
          {why.unimplemented === true ? (
            // D14/SEC-033: the third state is its own answer — the engine
            // has no verdict yet — rendered so no one reads it as a refusal.
            <Alert intent="info" title={t("settings.policy.inForce.unimplemented")} description={why.sentence} data-testid="why-unimplemented" />
          ) : why.allowed ? (
            <Alert intent="success" title={t("settings.policy.inForce.allowed")} description={why.sentence} data-testid="why-allowed" />
          ) : (
            <Alert intent="danger" title={t("settings.policy.inForce.denied")} description={why.sentence} data-testid="why-denied" />
          )}
          <div className="mt-1 flex flex-wrap gap-2">
            {why.defaultDeny === true && <Pill tone="slate">{t("settings.policy.inForce.defaultDeny")}</Pill>}
            {why.asked === true && <Pill tone="amber">{t("settings.policy.inForce.asked")}</Pill>}
            {why.unmatchable === true && <Pill tone="orange">{t("settings.policy.inForce.unmatchable")}</Pill>}
            {why.by !== undefined && why.by !== "" && (
              <span className="font-mono text-xs opacity-70">
                {why.by}
                {why.byTarget !== undefined && why.byTarget !== "" ? `:${why.byTarget}` : ""}
              </span>
            )}
          </div>
        </div>
      )}

      {/* The why door's sentence and the in-force door's `Render()` lines are
          the daemon's one formatters — both shown above verbatim. */}
    </section>
  );
}
