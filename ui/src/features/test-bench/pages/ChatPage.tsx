import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Badge, Button, EmptyState, FormField, Panel, Pill, Select, Textarea, UserAvatar } from "../../../components/kit";
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
import { EVENT_ASK_DELTA, EVENT_ASK_DONE } from "../../../lib/protocol";
import { costLine, usageLine } from "../../../lib/cost";
import { ErrorPanel } from "../../../components/ErrorPanel";

/**
 * Chat — a multi-turn harness over `ask` (S3's single-shot method), so a
 * stream can be watched turn after turn instead of once.
 *
 * # What this is, and the one thing it is not
 *
 * `ask`'s own params say it plainly: *"S5 replaces this with a full
 * transcript; at S3 one prompt is the whole conversation."* That has not
 * changed — there is no transcript on the wire for this call, so **no
 * history is sent**. Each turn is its own independent `ask`, same as the Ask
 * tab, with no memory of the turns before it. What this page adds is purely
 * presentational: the replies stack as a conversation, and the route can be
 * switched between turns, so streaming is easy to watch across more than one
 * exchange and more than one provider in the same sitting. A follow-up that
 * assumes the model remembers the last turn will read as the model having
 * forgotten — because it was never told.
 *
 * # One turn in flight at a time
 *
 * Simplest honest model: Send is disabled while a reply is streaming, so
 * there is never more than one `askId` to route events to, and the ordering
 * rule the Ask tab already proved — subscribe before the first ask — is the
 * only ordering rule this page needs too.
 */
export function ChatPage() {
  const [routes, setRoutes] = useState<RouteView[] | null>(null);
  const [routeId, setRouteId] = useState("");
  const [input, setInput] = useState("");
  const [turns, setTurns] = useState<ChatTurn[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<RpcError | null>(null);

  const [listening, setListening] = useState(false);
  const [listenerError, setListenerError] = useState<RpcError | null>(null);
  const subscriptionRef = useRef<string | null>(null);
  // The one turn currently streaming, so a delta/done can be routed back to
  // it without re-subscribing per turn (the same reason the Ask tab's askId
  // is a ref: the listener is installed once and must see the latest value).
  const activeRef = useRef<{ askId: string; turnId: number } | null>(null);
  const nextTurnId = useRef(1);

  useEffect(() => {
    void (async () => {
      try {
        const result = await rpc<RoutesListResult>("routes.list");
        const list = result.routes ?? [];
        setRoutes(list);
        setRouteId((current) => current || list[0]?.id || "");
      } catch (e) {
        setError(e as RpcError);
      }
    })();
  }, []);

  useEffect(() => {
    // **Guarded against StrictMode's dev-mode double invoke.** Without
    // `disposed`, mount → cleanup → mount (which React runs synchronously,
    // before either `onEvent` call has resolved) tears down nothing — the
    // first mount's cleanup runs while its `unlisten` is still `undefined` —
    // and leaves TWO live listeners once both calls resolve. Every event then
    // fires twice, which is exactly what doubled every streamed word here.
    // StepScreen's own subscription effect already carries this guard (as
    // `disposed`); this page needs the same one.
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void (async () => {
      try {
        const stop = await onEvent((payload) => {
          const event = payload as AeonEvent;
          const active = activeRef.current;
          if (!active) return;

          if (event.kind === EVENT_ASK_DELTA) {
            const delta = event.payload as AskDeltaPayload;
            if (delta.askId !== active.askId) return;
            setTurns((ts) => ts.map((t) => (t.id !== active.turnId ? t : applyDelta(t, delta))));
            return;
          }

          if (event.kind === EVENT_ASK_DONE) {
            const payload = event.payload as AskDonePayload;
            if (payload.askId !== active.askId) return;
            setTurns((ts) => ts.map((t) => (t.id !== active.turnId ? t : {
              ...t,
              status: payload.error ? "error" : "done",
              done: payload,
              error: payload.error ? new RpcError(payload.error.code, payload.error.message, payload.error.data) : undefined,
            })));
            setRunning(false);
            activeRef.current = null;
          }
        });
        // The cleanup below may already have run by the time this resolves
        // (StrictMode's simulated unmount) — in which case tear this one down
        // immediately rather than leaving it as the second live listener.
        if (disposed) { stop(); return; }
        unlisten = stop;
        setListening(true);
      } catch (e) {
        if (disposed) return;
        setListening(false);
        setListenerError(listenerUnavailable(e));
      }
    })();
    return () => { disposed = true; unlisten?.(); };
  }, []);

  const send = useCallback(async () => {
    const prompt = input.trim();
    if (!prompt || !routeId || running) return;
    if (!listening) {
      setError(listenerError ?? listenerUnavailable("the listener is not installed yet"));
      return;
    }
    setError(null);
    setInput("");
    setRunning(true);
    const turnId = nextTurnId.current++;
    // The route is snapshotted at send time — this IS "select the provider
    // per turn": switching the control after this point affects only the
    // NEXT turn, never one already sent.
    setTurns((ts) => [...ts, { id: turnId, prompt, routeId, status: "streaming", text: "", thinking: "", toolCalls: [] }]);

    try {
      if (!subscriptionRef.current) {
        const sub = await rpc<SubscribeResult>("subscribe", { filter: { kinds: ["ask.*"] } });
        subscriptionRef.current = sub.subscriptionId;
      }
      const params: AskParams = { route: routeId, prompt, subscriptionId: subscriptionRef.current };
      const result = await rpc<AskResult>("ask", params);
      activeRef.current = { askId: result.askId, turnId };
    } catch (e) {
      const rpcError = toRpcError(e);
      setTurns((ts) => ts.map((t) => (t.id !== turnId ? t : { ...t, status: "error", error: rpcError })));
      setRunning(false);
    }
  }, [input, routeId, running, listening, listenerError]);

  if (routes && routes.length === 0) {
    return (
      <EmptyState
        title="No routes to call"
        subtitle="Add a provider in Providers first. A route names a provider, a model and its prices."
        data-testid="no-routes-to-chat"
      />
    );
  }

  return (
    <div className="space-y-4" data-testid="chat-page">
      {listenerError && <ErrorPanel error={listenerError} />}
      {error && <ErrorPanel error={error} />}

      {turns.length === 0 ? (
        <Alert tone="slate" title="Nothing sent yet"
          description="Pick a route below and send a message. Each turn is its own ask (S3) — no history travels between turns, so switching the route mid-conversation is testing the provider, not a shared memory."
          data-testid="chat-empty" />
      ) : (
        <ol className="space-y-3" data-testid="chat-turns">
          {turns.map((turn) => <TurnBubble key={turn.id} turn={turn} />)}
        </ol>
      )}

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-48 flex-1">
            <FormField label="Route for the next turn">
              <Select value={routeId} onChange={(e) => setRouteId(e.target.value)} data-testid="chat-route">
                {(routes ?? []).map((r) => <option key={r.id} value={r.id}>{r.id} — {r.provider}/{r.model}</option>)}
              </Select>
            </FormField>
          </div>
        </div>
        <div className="mt-3">
          <Textarea
            value={input}
            rows={2}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(); }
            }}
            placeholder="say something — Enter to send, Shift+Enter for a new line"
            data-testid="chat-input"
          />
        </div>
        <div className="mt-2 flex items-center gap-3">
          <Button size="sm" loading={running} disabled={running || !input.trim() || !routeId || !listening} onClick={() => void send()} data-testid="chat-send">
            {running ? "streaming…" : "send"}
          </Button>
          {!listening && !listenerError && <p className="text-xs opacity-60" data-testid="chat-listener-pending">installing the event listener…</p>}
        </div>
      </Panel>
    </div>
  );
}

interface ChatTurn {
  id: number;
  prompt: string;
  routeId: string;
  status: "streaming" | "done" | "error";
  text: string;
  thinking: string;
  toolCalls: string[];
  done?: AskDonePayload;
  error?: RpcError;
}

function applyDelta(turn: ChatTurn, delta: AskDeltaPayload): ChatTurn {
  switch (delta.kind) {
    case "thinking":
      return { ...turn, thinking: turn.thinking + (delta.text ?? "") };
    case "tool_use":
      return delta.block ? { ...turn, toolCalls: [...turn.toolCalls, `${delta.block.name ?? "tool"}(${JSON.stringify(delta.block.input ?? {})})`] } : turn;
    default:
      return { ...turn, text: turn.text + (delta.text ?? "") };
  }
}

function TurnBubble({ turn }: { turn: ChatTurn }) {
  return (
    <li className="space-y-2" data-testid={`chat-turn-${turn.id}`}>
      <div className="flex min-w-0 gap-3 rounded-lg border border-white/10 p-3">
        <UserAvatar user={{ name: "You" }} size="sm" />
        <p className="min-w-0 flex-1 whitespace-pre-wrap break-words text-sm" data-testid={`chat-turn-${turn.id}-prompt`}>{turn.prompt}</p>
      </div>
      <div className="flex min-w-0 gap-3 rounded-lg border border-white/10 p-3">
        <UserAvatar user={{ name: "Aeon" }} size="sm" />
        <div className="min-w-0 flex-1 text-sm">
          <Badge variant="soft" size="sm" tone="neutral" data-testid={`chat-turn-${turn.id}-route`}>{turn.routeId}</Badge>
          {turn.thinking && (
            <details className="mt-2 text-xs italic opacity-75">
              <summary className="cursor-pointer select-none">thinking</summary>
              <p className="mt-1 whitespace-pre-wrap break-words" data-testid={`chat-turn-${turn.id}-thinking`}>{turn.thinking}</p>
            </details>
          )}
          <p className="mt-2 whitespace-pre-wrap break-words" data-testid={`chat-turn-${turn.id}-text`}>
            {turn.text}
            {turn.status === "streaming" && <span className="opacity-50">▌</span>}
          </p>
          {turn.toolCalls.map((call, i) => (
            <p key={i} className="mt-1 text-xs opacity-70" data-testid={`chat-turn-${turn.id}-tool-call`}>
              → <code>{call}</code> <span className="opacity-60">— nothing runs it here either</span>
            </p>
          ))}
          {turn.status === "error" && turn.error && (
            <div className="mt-2" data-testid={`chat-turn-${turn.id}-error`}><ErrorPanel error={turn.error} /></div>
          )}
          {turn.done && !turn.error && (
            <div className="mt-2 flex flex-wrap items-center gap-2 text-xs opacity-80" data-testid={`chat-turn-${turn.id}-done`}>
              <Pill tone={costLine(turn.done.cost).tone} size="sm">{costLine(turn.done.cost).text}</Pill>
              <span className="font-mono">{usageLine(turn.done.usage)}</span>
              {turn.done.stopReason && <span>stop: {turn.done.stopReason}</span>}
            </div>
          )}
        </div>
      </div>
    </li>
  );
}
