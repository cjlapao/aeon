import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  EmptyState,
  FormField,
  InfoRow,
  Panel,
  Pill,
  Select,
  Textarea,
} from "../../../components/kit";
import { listenerUnavailable, onEvent, rpc, RpcError, toRpcError } from "../../../lib/bridge";
import type {
  AeonEvent,
  AskDeltaPayload,
  AskDonePayload,
  AskParams,
  AskResult,
  RouteView,
  RoutesListResult,
  SubscribeResult,
} from "../../../lib/protocol";
import { availableEfforts } from "../../models/modelConfig";
import { EVENT_ASK_DELTA, EVENT_ASK_DONE } from "../../../lib/protocol";
import { costLine, usageLine } from "../../../lib/cost";
import { ErrorPanel } from "../../../components/ErrorPanel";

/**
 * Ask — the page that exercises the model layer.
 *
 * # It subscribes BEFORE it asks
 *
 * The daemon streams a reply through the subscription machinery (M23), and the
 * subscription must exist before the call that will stream into it. Doing it
 * the other way round works almost always and fails on a fast model — which is
 * the worst kind of ordering bug, because it passes every test on a slow one.
 *
 * # It renders the cost in three states
 *
 * Via `costLine`, which mirrors the CLI's renderer deliberately: the app and
 * `aeon ask` must not disagree about what a cost means. **An unknown cost is
 * never shown as `$0.00`** — that is the dishonesty the whole three-state
 * design exists to prevent.
 */
export function AskPage() {
  const [routes, setRoutes] = useState<RouteView[] | null>(null);
  const [routeId, setRouteId] = useState("");
  const [prompt, setPrompt] = useState("");
  const [system, setSystem] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState("");
  const [error, setError] = useState<RpcError | null>(null);

  const [running, setRunning] = useState(false);
  const [text, setText] = useState("");
  const [thinking, setThinking] = useState("");
  const [toolCalls, setToolCalls] = useState<string[]>([]);
  const [done, setDone] = useState<AskDonePayload | null>(null);

  // Whether the event listener is actually installed. **A page that cannot
  // receive events must not start a stream** — it would sit there forever.
  const [listening, setListening] = useState(false);
  const [listenerError, setListenerError] = useState<RpcError | null>(null);

  // The ask currently streaming. A ref because the event handler is installed
  // once and must see the latest value without being re-subscribed.
  const askIdRef = useRef<string | null>(null);

  /**
   * ONE subscription for the page's lifetime.
   *
   * It used to subscribe on every ask and never unsubscribe. The daemon fans
   * each event out to EVERY matching subscription on the connection, so the
   * fourth ask streamed every delta four times — which renders as the model
   * repeating itself, and sends somebody looking at the provider.
   *
   * Filtering by `askId` hid it here: a delta for the current ask arrived N
   * times and all N passed the filter.
   */
  const subscriptionRef = useRef<string | null>(null);

  useEffect(() => {
    void (async () => {
      try {
        const result = await rpc<RoutesListResult>("routes.list");
        // Normalised before it is indexed — see the note in StepPage: the
        // throw happens during render, not here, and takes the tab with it.
        const list = result.routes ?? [];
        setRoutes(list);
        setRouteId((current) => current || list[0]?.id || "");
      } catch (e) {
        setError(e as RpcError);
      }
    })();
  }, []);

  // One listener for the whole page's lifetime, filtering by askId.
  //
  // # Its failure is surfaced, not swallowed
  //
  // The first version wrapped this in `void (async () => …)()`, which
  // **discarded the rejection** — and when `listen` was denied by Tauri's ACL,
  // the page looked exactly like a slow model: a stream that started and never
  // produced anything. A listener that cannot be installed is a page that
  // cannot work, so it says so and refuses to start a stream.
  useEffect(() => {
    // **Guarded against StrictMode's dev-mode double invoke.** React runs
    // mount → cleanup → mount synchronously, before `installListener`'s
    // promise settles — so the first mount's cleanup finds `unlisten` still
    // `undefined` and tears down nothing. Both calls then resolve, leaving
    // TWO live listeners, and every event fires twice: the symptom is every
    // streamed word doubled. `disposed` lets the late resolution tear itself
    // down instead of becoming the second listener.
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void (async () => {
      try {
        const stop = await installListener();
        if (disposed) { stop(); return; }
        unlisten = stop;
        setListening(true);
      } catch (e) {
        if (disposed) return;
        setListening(false);
        // The three parts, because Tauri's own message says what the runtime
        // refused and nothing about what it means for the person reading it.
        setListenerError(listenerUnavailable(e));
      }
    })();

    async function installListener() {
      return onEvent((payload) => {
        const event = payload as AeonEvent;
        const askId = askIdRef.current;
        if (!askId) return;

        if (event.kind === EVENT_ASK_DELTA) {
          const delta = event.payload as AskDeltaPayload;
          if (delta.askId !== askId) return;
          switch (delta.kind) {
            case "thinking":
              setThinking((t) => t + (delta.text ?? ""));
              break;
            case "tool_use":
              if (delta.block) {
                setToolCalls((c) => [
                  ...c,
                  `${delta.block?.name ?? "tool"}(${JSON.stringify(delta.block?.input ?? {})})`,
                ]);
              }
              break;
            default:
              setText((t) => t + (delta.text ?? ""));
          }
          return;
        }

        if (event.kind === EVENT_ASK_DONE) {
          const payload = event.payload as AskDonePayload;
          if (payload.askId !== askId) return;
          setDone(payload);
          setRunning(false);
          askIdRef.current = null;
          // **The structured error, where the daemon sent one.** In streaming
          // mode this is the only place a failure arrives, and rendering it as
          // a sentence would lose the `todo`.
          if (payload.error) {
            setError(
              new RpcError(payload.error.code, payload.error.message, payload.error.data),
            );
          }
        }
      });
    }

    return () => { disposed = true; unlisten?.(); };
  }, []);

  const ask = useCallback(async () => {
    if (!listening) {
      // Refused rather than attempted. The daemon would do the work, bill for
      // it, and send the reply into a void.
      // Refused rather than attempted. The daemon would otherwise do the work,
      // bill for it, and send the reply into a void.
      setError(listenerError ?? listenerUnavailable("the listener is not installed yet"));
      return;
    }
    setError(null);
    setText("");
    setThinking("");
    setToolCalls([]);
    setDone(null);
    setRunning(true);

    try {
      // **Subscribe before the first ask.** See the note on this component.
      if (!subscriptionRef.current) {
        const sub = await rpc<SubscribeResult>("subscribe", {
          filter: { kinds: ["ask.*"] },
        });
        subscriptionRef.current = sub.subscriptionId;
      }

      const params: AskParams = {
        route: routeId,
        prompt,
        subscriptionId: subscriptionRef.current,
        system: system.trim() || undefined,
      };
      if (reasoningEffort) params.reasoningEffort = reasoningEffort;
      const result = await rpc<AskResult>("ask", params);
      askIdRef.current = result.askId;
    } catch (e) {
      setError(toRpcError(e));
      setRunning(false);
      askIdRef.current = null;
    }
  }, [listenerError, listening, prompt, reasoningEffort, routeId, system]);

  const selected = routes?.find((r) => r.id === routeId);

  if (routes && routes.length === 0) {
    return (
      <EmptyState
        title="No routes to call"
        subtitle="Add a provider in Providers first. A route names a provider, a model and its prices."
        data-testid="no-routes-to-call"
      />
    );
  }

  return (
    <div className="space-y-4">
      <Panel>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            void ask();
          }}
        >
          <FormField label="Route" required>
            <Select
              value={routeId}
              onChange={(e) => { setRouteId(e.target.value); setReasoningEffort(""); }}
              data-testid="ask-route"
            >
              {(routes ?? []).map((r) => (
                <option key={r.id} value={r.id}>
                  {r.displayName || r.id} — {r.provider}/{r.model}
                </option>
              ))}
            </Select>
          </FormField>

          {selected && !selected.priced && (
            <Alert
              tone="amber"
              title="This route has no prices"
              description="The cost will read “not reported”. That is honest, not broken — add prices in Providers to see a figure."
              data-testid="unpriced-warning"
            />
          )}
          {selected && !selected.hasKey && (
            <Alert
              tone="amber"
              title="This route has no API key"
              description="The call will fail unless its base URL points at something that does not need one."
              data-testid="no-key-warning"
            />
          )}

          <FormField label="System prompt" hint="Optional. Each provider puts this somewhere different; you will not see the difference.">
            <Textarea
              value={system}
              rows={2}
              onChange={(e) => setSystem(e.target.value)}
              placeholder="be brief"
              data-testid="ask-system"
            />
          </FormField>

          <FormField
            label="Reasoning effort"
            hint="This call only. A chat offers no tools, so it can reason freely — unlike a step."
          >
            <Select
              value={reasoningEffort}
              onChange={(e) => setReasoningEffort(e.target.value)}
              data-testid="ask-reasoning-effort"
            >
              <option value="">the route's default</option>
              {availableEfforts(selected).map((e) => (
                <option key={e} value={e}>
                  {e}
                </option>
              ))}
            </Select>
          </FormField>

          <FormField label="Prompt" required>
            <Textarea
              value={prompt}
              rows={4}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="in one sentence, what is a monorepo?"
              data-testid="ask-prompt"
            />
          </FormField>

          <Button
            type="submit"
            loading={running}
            disabled={running || !prompt.trim() || !routeId || !listening}
            data-testid="ask-submit"
          >
            {running ? "Streaming…" : "Ask"}
          </Button>
          {!listening && !listenerError && (
            <p className="text-xs opacity-60" data-testid="listener-pending">
              installing the event listener…
            </p>
          )}
        </form>
      </Panel>

      {/* The listener's failure comes first: everything else on this page
          depends on it, and showing a stream error above its cause would send
          somebody looking in the wrong place. */}
      {listenerError && <ErrorPanel error={listenerError} />}
      {error && <ErrorPanel error={error} />}

      {(thinking || text || toolCalls.length > 0 || done) && (
        <Panel data-testid="ask-reply">
          {thinking && (
            // Reasoning is not the answer, and inlining it makes the answer
            // hard to find.
            <details className="mb-3 text-sm opacity-70" data-testid="ask-thinking">
              <summary className="cursor-pointer">thinking</summary>
              <p className="mt-1 whitespace-pre-wrap">{thinking}</p>
            </details>
          )}

          <p className="whitespace-pre-wrap" data-testid="ask-text">
            {text}
            {running && <span className="opacity-50">▌</span>}
          </p>

          {toolCalls.map((call, i) => (
            <div key={i} className="mt-2 text-sm opacity-80" data-testid="ask-tool-call">
              → <code>{call}</code>
              <span className="ml-2 text-xs opacity-60">
                nothing runs it yet — tools arrive at S5
              </span>
            </div>
          ))}

          {done && <Accounting done={done} />}
        </Panel>
      )}
    </div>
  );
}

/** Usage, cost and the stop reason, with the cost in one of three forms. */
function Accounting({ done }: { done: AskDonePayload }) {
  const cost = costLine(done.cost);

  return (
    <div className="mt-4 border-t border-white/10 pt-3" data-testid="ask-accounting">
      <InfoRow
        label="cost"
        value={
          <span data-testid="cost-line">
            <Pill tone={cost.tone} size="sm">
              {cost.text}
            </Pill>
            {cost.detail && (
              <span className="ml-2 text-xs opacity-70" data-testid="cost-detail">
                {cost.detail}
              </span>
            )}
          </span>
        }
      />
      <InfoRow label="tokens" value={usageLine(done.usage)} mono />
      {done.stopReason && <InfoRow label="stop" value={done.stopReason} mono />}
      {done.dropped ? (
        <InfoRow
          label="dropped"
          value={`${done.dropped} provider block(s) mapped to no canonical kind`}
        />
      ) : null}
      {done.owedToolErrors?.map((id) => (
        <InfoRow
          key={id}
          label="note"
          value={`tool call ${id} had unparseable arguments; a tool_result with isError is owed`}
        />
      ))}
    </div>
  );
}
