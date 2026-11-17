import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select, Textarea } from "../../../components/kit";
import { rpc, toRpcError } from "../../../lib/bridge";
import type { RouteView, RoutesListResult, SetRouteParams, TraceRouteParams, TraceRouteResult } from "../../../lib/protocol";
import { availableEfforts } from "../../models/modelConfig";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * Route trace — one request on a route, with the wire on both sides.
 *
 * The page for "the model behaves differently from what we asked": it sends ONE request with the options
 * set here (reasoning effort, an extra request body, an offered tool) and shows the JSON body that was actually
 * sent, the raw bytes the server answered, and what Aeon's adapter read out of them. A reply that visibly carries
 * reasoning while "thinking read" says 0 is a field the adapter does not read; an effort or a body key that is
 * missing from "sent" is one that never left. Nothing is stored unless the person presses Save.
 */

const PRESETS: { label: string; body: Record<string, unknown> }[] = [
  { label: "enable_thinking: false", body: { chat_template_kwargs: { enable_thinking: false } } },
  { label: "enable_thinking: true", body: { chat_template_kwargs: { enable_thinking: true } } },
  { label: "reasoning_effort: low", body: { reasoning_effort: "low" } },
];

function pretty(value: unknown): string {
  try { return JSON.stringify(value, null, 2); } catch { return String(value); }
}

export function RouteTracePage({ connected }: { connected: boolean }) {
  const [routes, setRoutes] = useState<RouteView[]>([]);
  const [routeId, setRouteId] = useState("");
  const [prompt, setPrompt] = useState("Say hello in one short sentence.");
  const [system, setSystem] = useState("");
  const [effort, setEffort] = useState("");
  const [maxTokens, setMaxTokens] = useState("");
  const [extra, setExtra] = useState("");
  const [offerTool, setOfferTool] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [result, setResult] = useState<TraceRouteResult | null>(null);

  useEffect(() => {
    if (!connected) return;
    void (async () => {
      try {
        const list = await rpc<RoutesListResult>("routes.list");
        setRoutes(list.routes);
        setRouteId((cur) => cur || list.routes[0]?.id || "");
      } catch (cause) { setError(toRpcError(cause).message); }
    })();
  }, [connected]);

  const route = useMemo(() => routes.find((r) => r.id === routeId), [routes, routeId]);
  const efforts = availableEfforts(route);
  // The controls follow what the route enables: a thinking switch only on a route that declares thinking, an effort
  // dial only on one that declares efforts (or carries one). They write the request the page will send, below.
  const thinkingEnabled = Boolean(route?.capabilities?.includes("thinking") || route?.wireCapabilities?.includes("thinking"));
  const effortEnabled = Boolean(route?.reasoningEfforts?.length || route?.reasoningEffort);

  // The extra-body text is the person's own JSON: parse it here so a typo is a sentence next to the box, not a daemon refusal.
  const parsed = useMemo((): { ok: true; value?: Record<string, unknown> } | { ok: false; message: string } => {
    if (!extra.trim()) return { ok: true };
    try {
      const value = JSON.parse(extra) as unknown;
      if (!value || typeof value !== "object" || Array.isArray(value)) return { ok: false, message: "Extra body must be one JSON object." };
      return { ok: true, value: value as Record<string, unknown> };
    } catch (cause) { return { ok: false, message: cause instanceof Error ? cause.message : String(cause) }; }
  }, [extra]);

  // Thinking is a tri-state read back from the JSON itself, so the switch and the box can never disagree.
  const thinking = useMemo((): "" | "on" | "off" => {
    if (!parsed.ok) return "";
    const kwargs = parsed.value?.chat_template_kwargs as { enable_thinking?: unknown } | undefined;
    return kwargs?.enable_thinking === true ? "on" : kwargs?.enable_thinking === false ? "off" : "";
  }, [parsed]);
  const setThinking = (next: "" | "on" | "off") => {
    if (!parsed.ok) return;
    const body: Record<string, unknown> = { ...(parsed.value ?? {}) };
    const kwargs: Record<string, unknown> = { ...((body.chat_template_kwargs as Record<string, unknown> | undefined) ?? {}) };
    if (next === "") delete kwargs.enable_thinking; else kwargs.enable_thinking = next === "on";
    if (Object.keys(kwargs).length) body.chat_template_kwargs = kwargs; else delete body.chat_template_kwargs;
    setExtra(Object.keys(body).length ? pretty(body) : "");
  };
  // What the request will carry beyond the prompt: the per-call effort and the extra body, as the daemon merges them.
  const willSend = useMemo(() => {
    if (!parsed.ok) return "";
    const out: Record<string, unknown> = {};
    const eff = effort || route?.reasoningEffort;
    if (eff) out.reasoning_effort = eff;
    let saved: Record<string, unknown> = {};
    try { saved = route?.extraBody ? (JSON.parse(route.extraBody) as Record<string, unknown>) : {}; } catch { /* shown as saved text only */ }
    return pretty({ ...out, ...saved, ...(parsed.value ?? {}) });
  }, [parsed, effort, route]);

  const send = useCallback(async () => {
    if (!route || !parsed.ok) return;
    setBusy(true); setError(""); setNote(""); setResult(null);
    try {
      const params: TraceRouteParams = {
        route: route.id, prompt, ...(system ? { system } : {}), ...(effort ? { reasoningEffort: effort } : {}),
        ...(parsed.value ? { extraBody: parsed.value } : {}), ...(maxTokens ? { maxOutputTokens: Number(maxTokens) } : {}), ...(offerTool ? { offerTool: true } : {}),
      };
      setResult(await rpc<TraceRouteResult>("routes.trace", params));
    } catch (cause) { setError(toRpcError(cause).message); } finally { setBusy(false); }
  }, [route, parsed, prompt, system, effort, maxTokens, offerTool]);

  // Saving writes the box as the route's own extra body — the way to make a tried option permanent. The route's
  // other fields ride unchanged (absent prices and key mean keep), so only the extra body moves.
  const save = useCallback(async () => {
    if (!route || !parsed.ok) return;
    setBusy(true); setError(""); setNote("");
    try {
      const params: SetRouteParams = {
        id: route.id, provider: route.provider, model: route.model, baseUrl: route.baseUrl, maxOutputTokens: route.maxOutputTokens,
        contextWindow: route.contextWindow, noPrice: route.noPrice ? true : undefined, extraBody: parsed.value ? JSON.stringify(parsed.value) : "",
      };
      await rpc("routes.set", params);
      const list = await rpc<RoutesListResult>("routes.list");
      setRoutes(list.routes);
      setNote(parsed.value ? `Saved as ${route.id}'s extra body. Every call on this route now sends it.` : `Cleared ${route.id}'s extra body.`);
    } catch (cause) { setError(toRpcError(cause).message); } finally { setBusy(false); }
  }, [route, parsed]);

  const reasoningSeen = result?.raw ? /"reasoning(_content)?"/.test(result.raw) : false;
  const silent = result && result.status === 200 && reasoningSeen && result.thinkingChars === 0;
  const canSave = Boolean(route) && route?.source !== "environment" && parsed.ok;

  return (
    <div className="space-y-4" data-testid="route-trace-page">
      <Panel variant="simple" className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-3">
          <FormField label="Route">
            <Select value={routeId} onChange={(e) => setRouteId(e.target.value)} aria-label="route">
              {routes.map((r) => <option key={r.id} value={r.id}>{r.displayName || r.id} · {r.model}</option>)}
            </Select>
          </FormField>
          {effortEnabled && (
            <FormField label="Reasoning effort (this call)">
              <Select value={effort} onChange={(e) => setEffort(e.target.value)} aria-label="reasoning effort">
                <option value="">route default{route?.reasoningEffort ? ` (${route.reasoningEffort})` : " (none sent)"}</option>
                {efforts.map((e) => <option key={e} value={e}>{e}</option>)}
              </Select>
            </FormField>
          )}
          {thinkingEnabled && (
            <FormField label="Thinking (this call)">
              <Select value={thinking} onChange={(e) => setThinking(e.target.value as "" | "on" | "off")} aria-label="thinking" disabled={!parsed.ok}>
                <option value="">route default</option>
                <option value="on">on</option>
                <option value="off">off</option>
              </Select>
            </FormField>
          )}
          <FormField label="Max output tokens">
            <Input value={maxTokens} onChange={(e) => setMaxTokens(e.target.value)} placeholder={String(route?.maxOutputTokens ?? "")} inputMode="numeric" aria-label="max output tokens" />
          </FormField>
        </div>
        <FormField label="Prompt"><Textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} aria-label="prompt" /></FormField>
        <FormField label="System (optional)"><Textarea value={system} onChange={(e) => setSystem(e.target.value)} rows={2} aria-label="system" /></FormField>
        <FormField label="Extra body for this call (JSON object, merged over the route's own)">
          <Textarea value={extra} onChange={(e) => setExtra(e.target.value)} rows={4} spellCheck={false} className="font-mono" aria-label="extra body" placeholder='{"chat_template_kwargs": {"enable_thinking": false}}' />
        </FormField>
        <div className="flex flex-wrap items-center gap-2">
          {PRESETS.map((p) => (
            <Button key={p.label} size="sm" variant="ghost" onClick={() => setExtra(pretty({ ...(parsed.ok ? parsed.value : {}), ...p.body }))}>{p.label}</Button>
          ))}
          <Button size="sm" variant="ghost" onClick={() => setExtra("")}>Clear</Button>
          <label className="ml-auto inline-flex items-center gap-2 text-sm">
            <input type="checkbox" checked={offerTool} onChange={(e) => setOfferTool(e.target.checked)} aria-label="offer a tool" />Offer a tool (steps always do)
          </label>
        </div>
        {willSend && willSend !== "{}" && (
          <div data-testid="route-trace-willsend">
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide opacity-70">Options this request will add</div>
            <pre className="whitespace-pre-wrap break-all rounded-lg border border-slate-200 bg-slate-50 p-3 font-mono text-xs dark:border-white/10 dark:bg-white/5">{willSend}</pre>
          </div>
        )}
        {!parsed.ok && <Alert intent="danger" title="Extra body is not valid" description={parsed.message} />}
        {route?.extraBody && <p className="font-mono text-xs opacity-70" data-testid="route-trace-saved">Saved on this route: {route.extraBody}</p>}
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => void send()} disabled={!connected || !route || !parsed.ok || !prompt.trim()} loading={busy} data-testid="route-trace-send">Send one request</Button>
          <Button variant="ghost" onClick={() => void save()} disabled={!connected || !canSave || busy} data-testid="route-trace-save"
            title={route?.source === "environment" ? "This route comes from the environment and cannot be edited here" : "Write the box above as this route's extra body"}>
            Save box as the route's extra body
          </Button>
        </div>
        {error && <Alert intent="danger" title="That did not work" description={error} data-testid="route-trace-error" />}
        {note && <Alert intent="success" title={note} />}
      </Panel>

      {result && (
        <Panel variant="simple" className="space-y-3" data-testid="route-trace-result">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Pill tone={result.status === 200 ? "green" : "red"}>{result.status ? `HTTP ${result.status}` : "no answer"}</Pill>
            <span className="font-mono text-xs">{result.url}</span>
            <span className="opacity-70">{result.durationMs} ms</span>
            <span className="ml-auto flex flex-wrap gap-2">
              <Pill tone={result.thinkingChars > 0 ? "blue" : "slate"}>thinking read: {result.thinkingChars}</Pill>
              <Pill tone="slate">answer read: {result.textChars}</Pill>
              <Pill tone="slate">tool calls: {result.toolCalls}</Pill>
              {result.stopReason && <Pill tone="slate">stop: {result.stopReason}</Pill>}
              <Pill tone="slate">effort sent: {result.effort || "none"}</Pill>
            </span>
          </div>
          {result.error && <Alert intent="danger" title="The call reported" description={result.error} data-testid="route-trace-call-error" />}
          {silent && <Alert intent="warning" title="The reply carries reasoning that was not read" description="The raw reply below has a reasoning field, but the adapter read no thinking from it. The server uses a field name the adapter does not." data-testid="route-trace-silent" />}
          <Block title="Sent (request body)" text={pretty(result.requestBody ?? "(no body)")} testid="route-trace-sent" />
          <Block title={`Raw reply${result.truncated ? " (cut at the cap)" : ""}`} text={result.raw || "(empty)"} testid="route-trace-raw" />
          {(result.thinking || result.text) && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Block title="Thinking the adapter read" text={result.thinking || "(none)"} />
              <Block title="Answer the adapter read" text={result.text || "(none)"} />
            </div>
          )}
        </Panel>
      )}
    </div>
  );
}

function Block({ title, text, testid }: { title: string; text: string; testid?: string }) {
  const copy = () => { try { void navigator.clipboard?.writeText(text); } catch { /* no clipboard */ } };
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-xs font-semibold uppercase tracking-wide opacity-70">
        <span>{title}</span>
        <button type="button" onClick={copy} className="font-normal normal-case underline">Copy</button>
      </div>
      <pre data-testid={testid} className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-lg border border-slate-200 bg-slate-50 p-3 font-mono text-xs dark:border-white/10 dark:bg-white/5">{text}</pre>
    </div>
  );
}

registerBenchPage({
  id: "route-trace",
  label: "Route trace",
  path: "/bench/route-trace",
  order: 4600,
  needsConnection: true,
  render: (ctx) => <RouteTracePage connected={ctx.connected} />,
});
