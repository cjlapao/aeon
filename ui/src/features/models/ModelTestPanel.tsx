import { useState } from "react";
import { Alert, Button, Pill, Tabs, Textarea } from "../../components/kit";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import { rpcTyped, describeError } from "../../lib/bridge";
import { costLine } from "../../lib/cost";
import type { Cost, RouteView } from "../../lib/protocol";
import { CAPABILITIES, modelName } from "./modelConfig";
import { ModelVoiceTests } from "./ModelVoiceTests";

/**
 * Test a model without starting a loop. Two checks, each one explicit action that sends real requests
 * to the provider and says so before it does:
 *
 * - **Try it** sends one short prompt through `ask` and shows what came back with the time it took, the
 *   tokens and the cost in the daemon's own three states (never a bare number).
 * - **Capabilities** asks `routes.probe` to check what the model was DECLARED to support. A verdict is
 *   `verified`, `contradicted` (the provider disagreed) or `declared` (not yet checked); the probe never
 *   edits the declaration, it only reports.
 *
 * Nothing here runs on its own: opening the panel sends nothing.
 */

const DEFAULT_PROMPT = "Reply with one short sentence that says which model you are.";

type Ask = { text: string; ms: number; inputTokens?: number; outputTokens?: number; stop?: string; truncated: boolean; cost: ReturnType<typeof costLine> };
type Probe = { capability: string; state: string; detail: string; probedAt?: string; probedModel?: string };

const stateTone = (state: string): "green" | "red" | "slate" => (state === "verified" ? "green" : state === "contradicted" ? "red" : "slate");
const capLabel = (id: string) => CAPABILITIES.find((c) => c.id === id)?.label ?? id;

export function ModelTestPanel({ route, canRun, onClose }: { route: RouteView; canRun: boolean; onClose: () => void }) {
  const [tab, setTab] = useState<"try" | "caps" | "voice">("try");
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [asking, setAsking] = useState(false);
  const [answer, setAnswer] = useState<Ask | undefined>();
  const [probing, setProbing] = useState(false);
  const [probes, setProbes] = useState<Probe[] | undefined>();
  const [error, setError] = useState("");

  const ask = async () => {
    if (!canRun || asking || prompt.trim() === "") return;
    setAsking(true);
    setError("");
    setAnswer(undefined);
    const started = performance.now();
    try {
      const res = await rpcTyped("ask", { route: route.id, prompt });
      setAnswer({
        text: res.text ?? "", ms: Math.round(performance.now() - started), inputTokens: res.usage?.inputTokens, outputTokens: res.usage?.outputTokens,
        stop: res.stopReason, truncated: res.truncated === true, cost: costLine(res.cost as Cost | undefined),
      });
    } catch (cause) {
      setError(describeError(cause));
    } finally {
      setAsking(false);
    }
  };

  const probe = async () => {
    if (!canRun || probing) return;
    setProbing(true);
    setError("");
    try {
      const res = await rpcTyped("routes.probe", { route: route.id });
      setProbes(res.probes);
    } catch (cause) {
      setError(describeError(cause));
    } finally {
      setProbing(false);
    }
  };

  const declared = route.capabilities ?? [];
  const verdictOf = (cap: string) => probes?.find((p) => p.capability === cap);

  return (
    <OverlaySidePanel title={`Test ${modelName(route)}`} titleClassName="text-xl font-semibold" subtitle={`${route.provider} / ${route.model}`} variant="simple" width={560} onClose={onClose}>
      <div className="flex flex-col gap-4" data-testid="model-test-panel">
        <Tabs variant="pill" items={[{ id: "try", label: "Try it" }, { id: "voice", label: "Speech & embedding" }, { id: "caps", label: "Capabilities" }]} value={tab} onChange={(id) => setTab(id as "try" | "caps" | "voice")} />
        {!canRun && <Alert intent="warning" title="Reconnecting — tests are off until the daemon is current." />}
        {error && <Alert intent="danger" title="The test did not run" description={error} data-testid="model-test-error" />}

        {tab === "try" && (
          <div className="flex flex-col gap-3" data-testid="model-test-try">
            <p className="text-sm opacity-70">Sends one short request to the provider and shows what comes back. It uses a few tokens and may cost a fraction of a cent.</p>
            <Textarea rows={3} value={prompt} maxLength={2000} aria-label="Test prompt" onChange={(e) => setPrompt(e.target.value)} />
            <div><Button size="sm" onClick={() => void ask()} disabled={!canRun || prompt.trim() === "" || (route.capabilities != null && route.capabilities.length > 0 && !route.capabilities.includes("text"))} loading={asking} data-testid="model-test-run">Send test request</Button></div>
            {route.capabilities != null && route.capabilities.length > 0 && !route.capabilities.includes("text") && <p className="text-xs opacity-60" data-testid="model-test-needs-text">Off: this model does not declare Text. Use the Speech &amp; embedding tab for what it does declare.</p>}
            {answer && (
              <div className="flex flex-col gap-2 rounded-xl border border-current/10 p-3" data-testid="model-test-answer">
                <p className="whitespace-pre-wrap break-words text-sm">{answer.text || "The model answered with no text."}</p>
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <Pill size="sm" tone="green">answered in {answer.ms} ms</Pill>
                  {answer.inputTokens != null && <Pill size="sm" tone="slate">{answer.inputTokens} in · {answer.outputTokens ?? 0} out tokens</Pill>}
                  <Pill size="sm" tone={answer.cost.tone === "green" ? "green" : answer.cost.tone === "amber" ? "amber" : "red"} data-testid="model-test-cost">{answer.cost.text}</Pill>
                  {answer.truncated && <Pill size="sm" tone="amber">cut off at the output limit</Pill>}
                </div>
                {answer.cost.detail && <p className="text-xs opacity-70">{answer.cost.detail}</p>}
              </div>
            )}
          </div>
        )}

        {tab === "voice" && <ModelVoiceTests target={{ route: route.id }} capabilities={route.capabilities ?? []} canRun={canRun} />}

        {tab === "caps" && (
          <div className="flex flex-col gap-3" data-testid="model-test-caps">
            <p className="text-sm opacity-70">Checks what this model was declared to support by sending a minimal real request for each. The check only reports: it never changes the declaration.</p>
            <div><Button size="sm" onClick={() => void probe()} disabled={!canRun || declared.length === 0} loading={probing} data-testid="model-probe-run">Check capabilities</Button></div>
            {declared.length === 0 && <p className="text-sm opacity-70">No capabilities are declared for this model, so there is nothing to check. Declare some in Edit.</p>}
            <ul className="flex flex-col gap-2">
              {declared.map((cap) => {
                const v = verdictOf(cap);
                const state = v?.state ?? "declared";
                return (
                  <li key={cap} className="flex flex-col gap-1 rounded-xl border border-current/10 p-3" data-testid={`probe-${cap}`}>
                    <div className="flex items-center justify-between gap-2"><span className="text-sm font-medium">{capLabel(cap)}</span><Pill size="sm" tone={stateTone(state)}>{state}</Pill></div>
                    {v?.detail && <p className="break-words text-xs opacity-80">{v.detail}</p>}
                    {v?.probedAt && <p className="text-xs opacity-60">checked {v.probedAt}{v.probedModel ? ` with ${v.probedModel}` : ""}</p>}
                    {!v && <p className="text-xs opacity-60">Not checked yet.</p>}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </div>
    </OverlaySidePanel>
  );
}
