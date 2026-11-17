import { availableEfforts } from "../../models/modelConfig";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  Alert,
  Button,
  Checkbox,
  EmptyState,
  FormField,
  InfoRow,
  Input,
  Panel,
  Pill,
  Select,
  Textarea,
} from "../../../components/kit";
import { listenerUnavailable, onEvent, rpc, RpcError, toRpcError } from "../../../lib/bridge";
import type {
  AeonEvent,
  RouteView,
  RoutesListResult,
  StepEventPayload,
  StepParams,
  StepRunResult,
  StepView,
  AnswerResult,
  StepVerifyResult,
  SubscribeResult,
} from "../../../lib/protocol";
import {
  EVENT_STEP_DONE,
  EVENT_STEP_MODEL_DELTA,
  EVENT_STEP_STARTED,
  EVENT_STEP_TOOL_END,
  EVENT_STEP_TOOL_START,
  METHOD_STEP,
  METHOD_STEP_SHOW,
  METHOD_STEP_VERIFY,
  METHOD_ANSWER,
  EVENT_STEP_COMPACTED,
  EVENT_STEP_SAVED,
  EVENT_QUESTION_ASKED,
} from "../../../lib/protocol";
import { costLine, usageLine } from "../../../lib/cost";
import { ErrorPanel } from "../../../components/ErrorPanel";

/**
 * Step — the page that exercises the loop, the tools and the allow-list.
 *
 * # What this page is FOR
 *
 * Three things the terminal shows less clearly:
 *
 * 1. **the step as it happens** — text arriving, a tool starting, its result,
 *    the next turn. The whole reason events are emitted as they happen (K19)
 *    rather than at the end is that somebody is watching, and this is where
 *    that is visible.
 * 2. **the grant list as a decision somebody makes.** `bash` is a checkbox
 *    that is off, with the warning next to it — not a default somebody
 *    discovers afterwards.
 * 3. **the four outcomes, distinguishable.** `reject` is not red. A handler
 *    that considered the question and answered no did its job, and a UI that
 *    painted that as a failure would teach the opposite.
 *
 * # What it deliberately does NOT do
 *
 * No transcript editing, no re-run, no queue. S9 builds the app; this is the
 * surface that proves S5 and S6 work where they will be consumed.
 */
export function StepPage() {
  const [routes, setRoutes] = useState<RouteView[] | null>(null);
  const [routeId, setRouteId] = useState("");
  const [prompt, setPrompt] = useState("");
  const [workspace, setWorkspace] = useState("");
  const [allowBash, setAllowBash] = useState(false);
  const [allowQuestion, setAllowQuestion] = useState(false);
  const [contextFiles, setContextFiles] = useState("");
  const [maxIterations, setMaxIterations] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState("");
  const [error, setError] = useState<RpcError | null>(null);

  const [running, setRunning] = useState(false);
  const [events, setEvents] = useState<StepLine[]>([]);
  const [result, setResult] = useState<StepRunResult | null>(null);
  const [savedStep, setSavedStep] = useState<StepView | null>(null);
  const [lookupId, setLookupId] = useState("");
  const [answer, setAnswer] = useState("");
  const [answerBusy, setAnswerBusy] = useState(false);
  const [resuming, setResuming] = useState(false);
  const [verify, setVerify] = useState<StepVerifyResult | null>(null);

  const [listening, setListening] = useState(false);
  const [listenerError, setListenerError] = useState<RpcError | null>(null);

  /**
   * ONE subscription for the page's lifetime.
   *
   * # The bug this fixes
   *
   * It used to subscribe on every run and never unsubscribe. The daemon fans
   * each event out to **every** matching subscription on the connection, so by
   * the fourth run every event arrived four times — and the stream showed four
   * copies of "step.started" and four of the outcome. It read as the daemon
   * emitting duplicates, which is the worst kind of bug to inherit: the symptom
   * is in somebody else's code.
   *
   * A subscription is per connection and matches by kind, so one is all a page
   * needs. It is also never released while the page lives, which is correct:
   * the alternative is a window between runs where an event has nowhere to go.
   */
  const subscriptionRef = useRef<string | null>(null);
  const seenSeqRef = useRef<Set<number>>(new Set());
  const mountedRef = useRef(true);

  // Whether a step is in flight. A ref because the listener is installed once
  // and must see the current value without being re-subscribed.
  const runningRef = useRef(false);

  /**
   * The routes, so the step can say WHICH model to call.
   *
   * The daemon takes the only configured route when there is exactly one, and
   * **refuses when there are several** — picking one would spend money on a
   * model nobody chose. So a page that cannot name a route works on a
   * single-route daemon and fails on everybody else's.
   */
  useEffect(() => {
    void (async () => {
      try {
        const result = await rpc<RoutesListResult>("routes.list");
        // `routes` is normalised BEFORE it is indexed.
        //
        // `result.routes[0]` inside a state updater throws during RENDER, not
        // inside this try — so a `routes.list` answer without the field killed
        // the whole tab, and the console showed a React crash rather than a bad
        // response. The same lesson as S4's `pending: null`: a collection is
        // not present because a struct says it should be.
        const list = result.routes ?? [];
        setRoutes(list);
        setRouteId((current) => current || list[0]?.id || "");
      } catch (e) {
        setError(toRpcError(e));
      }
    })();
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    let unlisten: (() => void) | undefined;
    let disposed = false;
    void (async () => {
      try {
        const stop = await onEvent((payload) => {
          const event = payload as AeonEvent;
          if (!runningRef.current && !resumingRef.current) return;
          if (event.subscriptionId && event.subscriptionId !== subscriptionRef.current) return;
          if (event.seq && seenSeqRef.current.has(event.seq)) return;
          if (event.seq) seenSeqRef.current.add(event.seq);
          const stepId = (event.payload as { stepId?: number } | undefined)?.stepId;
          if (resumingRef.current && stepId && stepId !== activeStepIdRef.current) return;
          const line = lineFor(event);
          if (line) setEvents((current) => [...current, line]);
          if (resumingRef.current && (event.kind === EVENT_STEP_SAVED || event.kind === EVENT_QUESTION_ASKED || event.kind === "step.persistence_failed")) {
            void refreshStep(activeStepIdRef.current);
          }
        });
        if (disposed) stop();
        else { unlisten = stop; setListening(true); }
      } catch (e) {
        setListening(false);
        // Surfaced, not swallowed. A page that cannot receive events would
        // look exactly like a slow model: a step that started and showed
        // nothing.
        setListenerError(listenerUnavailable(e));
      }
    })();
    return () => {
      mountedRef.current = false;
      disposed = true;
      unlisten?.();
      if (subscriptionRef.current) {
        void rpc("unsubscribe", { subscriptionId: subscriptionRef.current });
        subscriptionRef.current = null;
      }
    };
  }, []);

  const resumingRef = useRef(false);
  const activeStepIdRef = useRef(0);
  const readSeqRef = useRef(0);

  const refreshStep = useCallback(async (id: number) => {
    if (!id || !mountedRef.current) return;
    const readSeq = ++readSeqRef.current;
    try {
      const step = await rpc<StepView>(METHOD_STEP_SHOW, { id });
      if (!mountedRef.current || activeStepIdRef.current !== id || readSeq !== readSeqRef.current) return;
      setSavedStep(step);
      setLookupId(String(id));
      const stillRunning = step.status === "running" && !runningRef.current;
      setResuming(stillRunning);
      resumingRef.current = stillRunning;
    } catch (e) {
      if (mountedRef.current && readSeq === readSeqRef.current) setError(toRpcError(e));
    }
  }, []);

  const ensureSubscription = useCallback(async () => {
    if (subscriptionRef.current) return subscriptionRef.current;
    const sub = await rpc<SubscribeResult>("subscribe", { filter: { kinds: ["step.*", "model.*", "question.*"] } });
    if (!mountedRef.current) {
      void rpc("unsubscribe", { subscriptionId: sub.subscriptionId });
      return null;
    }
    subscriptionRef.current = sub.subscriptionId;
    return sub.subscriptionId;
  }, []);

  useEffect(() => {
    if (!resuming || !savedStep) return;
    const id = activeStepIdRef.current;
    const timer = window.setInterval(() => { void refreshStep(id); }, 1000);
    return () => window.clearInterval(timer);
  }, [resuming, savedStep, refreshStep]);

  const run = useCallback(async () => {
    if (runningRef.current || resumingRef.current) return;
    if (!listening) {
      // Refused rather than attempted: the daemon would do the work, bill for
      // it, and stream it into a void.
      setError(listenerError ?? listenerUnavailable("the listener is not installed yet"));
      return;
    }

    setError(null);
    setEvents([]);
    seenSeqRef.current.clear();
    setResult(null);
    setSavedStep(null);
    setVerify(null);
    setRunning(true);
    runningRef.current = true;

    try {
      // **Subscribe before the first run**, for the same reason Ask does: the
      // subscription must exist before the call that streams into it. Reused
      // afterwards — see `subscriptionRef`.
      const subscriptionId = await ensureSubscription();
      if (!subscriptionId) return;

      const params: StepParams = {
        prompt,
        workspaceRoot: workspace.trim(),
        route: routeId || undefined,
        subscriptionId,
        // The grant list, built from the checkbox. Workspace file tools always; `bash` only
        // when somebody ticked it.
        tools: ["read", "list", "write_file", ...(allowQuestion ? ["question"] : []), ...(allowBash ? ["bash"] : [])],
      };
      const files = contextFiles.split(/[\n,]/).map((s) => s.trim()).filter(Boolean);
      if (files.length) params.contextFiles = files;
      // Only when chosen. Empty means "let the step decide", and the step
      // lowers a reasoning route to `none` by itself — sending "" explicitly
      // would be the same thing, but sending the field at all suggests the
      // page had an opinion.
      if (reasoningEffort) params.reasoningEffort = reasoningEffort;
      const bound = Number.parseInt(maxIterations, 10);
      if (Number.isFinite(bound) && bound > 0) params.maxIterations = bound;

      // The call blocks for the whole step — the method returns when the step
      // is done — and the events arrive on the listener meanwhile.
      const out = await rpc<StepRunResult>(METHOD_STEP, params);
      setResult(out);
      activeStepIdRef.current = out.stepId;
      await refreshStep(out.stepId);
    } catch (e) {
      setError(toRpcError(e));
    } finally {
      setRunning(false);
      runningRef.current = false;
    }
  }, [
    allowBash,
    allowQuestion,
    contextFiles,
    ensureSubscription,
    listenerError,
    listening,
    maxIterations,
    prompt,
    reasoningEffort,
    routeId,
    refreshStep,
    workspace,
  ]);

  const loadStep = async () => {
    const id = Number(lookupId);
    if (!Number.isSafeInteger(id) || id <= 0) return;
    setError(null);
    setResult(null);
    setVerify(null);
    setEvents([]);
    setAnswer("");
    activeStepIdRef.current = id;
    await refreshStep(id);
  };

  const submitAnswer = async () => {
    const step = savedStep;
    if (!step?.question || answerBusy || resuming) return;
    if (step.question.options?.length && !step.question.options.includes(answer)) return;
    if (!listening) { setError(listenerError ?? listenerUnavailable("the listener is not installed yet")); return; }
    setAnswerBusy(true);
    setError(null);
    setEvents([]);
    seenSeqRef.current.clear();
    activeStepIdRef.current = step.id;
    try {
      if (!await ensureSubscription()) return;
      const accepted = await rpc<AnswerResult>(METHOD_ANSWER, { stepId: step.id, questionId: step.question.id, answer });
      setResult(null);
      setAnswer("");
      setResuming(true);
      resumingRef.current = true;
      setSavedStep({ ...step, status: accepted.status, question: undefined });
      void refreshStep(step.id);
    } catch (e) {
      setError(toRpcError(e));
      await refreshStep(step.id);
    } finally {
      setAnswerBusy(false);
    }
  };

  const verifyStep = async () => {
    if (!savedStep) return;
    try { setVerify(await rpc<StepVerifyResult>(METHOD_STEP_VERIFY, { id: savedStep.id })); }
    catch (e) { setError(toRpcError(e)); }
  };

  const selectedRoute = routes?.find((r) => r.id === routeId);

  return (
    <div className="space-y-4">
      <Panel data-testid="step-reopen-panel">
        <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); void loadStep(); }}>
          <FormField label="Open saved step" hint="Use its ID after closing the app or restarting the daemon.">
            <Input type="number" min="1" value={lookupId} onChange={(e) => setLookupId(e.target.value)} placeholder="Step ID" data-testid="step-lookup-id" />
          </FormField>
          <Button type="submit" variant="outline" disabled={!Number.isSafeInteger(Number(lookupId)) || Number(lookupId) <= 0} data-testid="step-load">Open step</Button>
        </form>
      </Panel>

      {routes && routes.length === 0 ? (
        <EmptyState title="No routes to run a step with" subtitle="Add a provider in Providers first. You can still open a saved step above." data-testid="step-no-routes" />
      ) : (
      <Panel>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            void run();
          }}
        >
          <FormField label="Route" required hint="Which model this step calls.">
            <Select
              value={routeId}
              onChange={(e) => { setRouteId(e.target.value); setReasoningEffort(""); }}
              data-testid="step-route"
            >
              {(routes ?? []).map((r) => (
                <option key={r.id} value={r.id}>
                  {r.displayName || r.id} — {r.provider}/{r.model}
                </option>
              ))}
            </Select>
          </FormField>

          {/* What this step will actually send, worked out here rather than
              left for the person to deduce from a provider's refusal. */}
          {selectedRoute && !reasoningEffort && lowersReasoning(selectedRoute) && (
            <Alert
              tone="slate"
              title={`This step will use reasoning \`none\``}
              description={`Route ${selectedRoute.id} is set to \`${selectedRoute.reasoningEffort}\`, and models with that dial refuse function tools while reasoning is on. Ask still uses \`${selectedRoute.reasoningEffort}\`; only steps are lowered.`}
              data-testid="step-reasoning-lowered"
            />
          )}
          {selectedRoute && !selectedRoute.hasKey && (
            <Alert
              tone="amber"
              title="This route has no API key"
              description="The step will fail on its first model call unless its base URL points at something that needs no credential."
              data-testid="step-no-key-warning"
            />
          )}

          <FormField
            label="Workspace"
            required
            hint="The project the step reads. There is no default: the daemon serves every project on this machine, and its own directory is nobody's workspace."
          >
            <Input
              value={workspace}
              onChange={(e) => setWorkspace(e.target.value)}
              placeholder="/absolute/path/to/a/project"
              data-testid="step-workspace"
            />
          </FormField>

          <FormField label="Prompt" required>
            <Textarea
              value={prompt}
              rows={3}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="list files, then read the project README"
              data-testid="step-prompt"
            />
          </FormField>

          <FormField label="Declared context files" hint="Workspace-relative paths, one per line or comma separated. Their opening contents are frozen so changes during suspension can be reported.">
            <Textarea value={contextFiles} rows={2} onChange={(e) => setContextFiles(e.target.value)} placeholder="README.md" data-testid="step-context-files" />
          </FormField>

          <FormField
            label="Iteration bound"
            hint="How many model calls the step may make. Empty uses the default."
          >
            <Input
              value={maxIterations}
              onChange={(e) => setMaxIterations(e.target.value)}
              placeholder="12"
              data-testid="step-max-iterations"
            />
          </FormField>

          <FormField
            label="Reasoning effort"
            hint="Leave on `the step decides`: a step that offers tools cannot have reasoning on, and it lowers the route's setting itself — saying so when it does."
          >
            <Select
              value={reasoningEffort}
              onChange={(e) => setReasoningEffort(e.target.value)}
              data-testid="step-reasoning-effort"
            >
              <option value="">the step decides</option>
              {availableEfforts(selectedRoute).map((e) => (
                <option key={e} value={e}>
                  {e}
                </option>
              ))}
            </Select>
          </FormField>

          <Checkbox checked={allowQuestion} onChange={(e) => setAllowQuestion(e.target.checked)} label="Allow the step to ask me a question" data-testid="step-allow-question" />

          {/* The shell grant is separate from asking a question. */}
          <Checkbox
            checked={allowBash}
            onChange={(e) => setAllowBash(e.target.checked)}
            label="Also grant the shell (bash)"
            data-testid="step-allow-bash"
          />
          {allowBash && (
            <Alert
              tone="red"
              title="This step can run shell commands"
              description="There is no command policy at this slice: whatever the model asks for runs, in the workspace, with the daemon's environment. It is bounded by a timeout and by the iteration limit, and by nothing else."
              data-testid="bash-warning"
            />
          )}

          <Button
            type="submit"
            loading={running}
            disabled={running || resuming || !prompt.trim() || !workspace.trim() || !routeId || !listening}
            data-testid="step-submit"
          >
            {running ? "Running…" : "Run step"}
          </Button>
          {!listening && !listenerError && (
            <p className="text-xs opacity-60" data-testid="step-listener-pending">
              installing the event listener…
            </p>
          )}
        </form>
      </Panel>
      )}

      {listenerError && <ErrorPanel error={listenerError} />}
      {error && <ErrorPanel error={error} />}

      {(events.length > 0 || running || resuming) && <StepStream lines={events} running={running || resuming} />}
      {result && <StepOutcome result={result} />}
      {savedStep && (
        <SavedStepPanel step={savedStep} verify={verify} onVerify={() => void verifyStep()}>
          {savedStep.status === "suspended" && savedStep.question && (
            <form className="mt-4 space-y-3 border-t border-white/10 pt-4" onSubmit={(e) => { e.preventDefault(); void submitAnswer(); }} data-testid="step-question">
              <Pill tone="amber">awaiting answer</Pill>
              <p className="text-sm font-medium">{savedStep.question.body}</p>
              {savedStep.question.options?.length ? (
                <FormField label="Choose an answer">
                  <Select value={answer} onChange={(e) => setAnswer(e.target.value)} data-testid="step-answer-choice">
                    <option value="">choose…</option>
                    {savedStep.question.options.map((choice) => <option key={choice} value={choice}>{choice}</option>)}
                  </Select>
                </FormField>
              ) : (
                <FormField label="Your answer" hint="An empty answer is valid. The step will resume with exactly what you enter.">
                  <Textarea value={answer} rows={2} onChange={(e) => setAnswer(e.target.value)} data-testid="step-answer-text" />
                </FormField>
              )}
              <Button type="submit" loading={answerBusy} disabled={answerBusy || !listening || (!!savedStep.question.options?.length && !savedStep.question.options.includes(answer))} data-testid="step-answer-submit">Answer and resume</Button>
            </form>
          )}
          {resuming && <p className="mt-3 text-sm opacity-70" data-testid="step-resuming">Answer saved. The daemon is continuing step {savedStep.id}.</p>}
        </SavedStepPanel>
      )}
    </div>
  );
}

/** One rendered line of the stream. */
interface StepLine {
  kind: string;
  text: string;
  tone: "plain" | "quiet" | "error";
}

/**
 * lineFor turns an event into a line, or into nothing.
 *
 * `model.delta` is appended as TEXT rather than as its own line, because a
 * delta per token would otherwise produce a thousand rows. Every other kind is
 * a row, because each one is a thing that happened.
 */
function lineFor(event: AeonEvent): StepLine | null {
  const payload = (event.payload ?? {}) as StepEventPayload;

  switch (event.kind) {
    case EVENT_STEP_STARTED:
      return { kind: event.kind, text: payload.summary ?? "started", tone: "quiet" };
    case EVENT_STEP_MODEL_DELTA:
      return payload.text ? { kind: event.kind, text: payload.text, tone: "plain" } : null;
    case EVENT_STEP_TOOL_START:
      return {
        kind: event.kind,
        text: `→ ${payload.tool ?? "tool"} ${payload.summary ?? ""}`.trim(),
        tone: "quiet",
      };
    case EVENT_STEP_TOOL_END:
      return {
        kind: event.kind,
        text: `${payload.isError ? "✗" : "←"} ${firstLine(payload.result ?? "")}`,
        tone: payload.isError ? "error" : "quiet",
      };
    case EVENT_STEP_DONE:
      return { kind: event.kind, text: payload.outcome ?? "done", tone: "quiet" };
    case EVENT_STEP_COMPACTED:
      return { kind: event.kind, text: `compacted messages ${payload.start ?? "?"}–${payload.end ?? "?"} (${payload.estimateBefore ?? "?"} → ${payload.estimateAfter ?? "?"} estimated tokens)`, tone: "quiet" };
    case EVENT_QUESTION_ASKED:
      return { kind: event.kind, text: "question asked — step suspended", tone: "quiet" };
    case EVENT_STEP_SAVED:
      return { kind: event.kind, text: "continuation saved", tone: "quiet" };
    case "step.persistence_failed":
      return { kind: event.kind, text: `continuation could not be saved: ${payload.reason ?? "unknown error"}`, tone: "error" };
    default:
      return null;
  }
}

function firstLine(s: string): string {
  const line = s.split("\n")[0] ?? "";
  return line.length > 140 ? `${line.slice(0, 137)}…` : line;
}

/**
 * The stream, as it happens.
 *
 * Consecutive model deltas are joined, so the model's prose reads as prose
 * rather than as a column of fragments.
 */
function StepStream({ lines, running }: { lines: StepLine[]; running: boolean }) {
  const grouped: StepLine[] = [];
  for (const line of lines) {
    const last = grouped[grouped.length - 1];
    if (last && last.kind === EVENT_STEP_MODEL_DELTA && line.kind === EVENT_STEP_MODEL_DELTA) {
      grouped[grouped.length - 1] = { ...last, text: last.text + line.text };
      continue;
    }
    grouped.push(line);
  }

  return (
    <Panel data-testid="step-stream">
      {grouped.length === 0 && (
        <p className="text-sm opacity-60" data-testid="step-stream-waiting">
          waiting for the first event…
        </p>
      )}
      {grouped.map((line, i) => (
        <p
          key={i}
          className={
            line.tone === "error"
              ? "whitespace-pre-wrap text-sm text-red-300"
              : line.tone === "quiet"
                ? "whitespace-pre-wrap text-sm opacity-60"
                : "whitespace-pre-wrap"
          }
          data-testid={line.tone === "plain" ? "step-text" : "step-trace"}
        >
          {line.text}
          {running && i === grouped.length - 1 && line.tone === "plain" && (
            <span className="opacity-50">▌</span>
          )}
        </p>
      ))}
    </Panel>
  );
}

/**
 * The outcome, with the four states distinguishable.
 *
 * **`reject` is not red.** A handler that considered the question and answered
 * no did its job; painting that as a failure would teach a person to treat a
 * considered no as a bug. Only `failed` is red.
 */
function StepOutcome({ result }: { result: StepRunResult }) {
  const cost = costLine(result.cost);

  return (
    <Panel data-testid="step-outcome">
      <div className="flex flex-wrap items-center gap-2">
        <Pill tone={outcomeTone(result.outcome)} data-testid="step-outcome-pill">
          {result.kind === "suspended" ? "suspended" : result.outcome ?? "no outcome"}
        </Pill>
        <span className="text-sm opacity-70">step {result.stepId}</span>
      </div>

      {result.reason && (
        <p className="mt-2 text-sm opacity-80" data-testid="step-reason">
          {result.reason}
        </p>
      )}

      {result.handoff !== undefined && result.handoff !== null && (
        <pre
          className="mt-3 overflow-x-auto rounded bg-black/20 p-3 text-xs"
          data-testid="step-handoff"
        >
          {JSON.stringify(result.handoff, null, 2)}
        </pre>
      )}

      <div className="mt-4 border-t border-white/10 pt-3">
        <InfoRow label="iterations" value={String(result.iterations)} mono />
        <InfoRow
          label="cost"
          value={
            <span data-testid="step-cost-line">
              <Pill tone={cost.tone} size="sm">
                {cost.text}
              </Pill>
              {cost.detail && (
                <span className="ml-2 text-xs opacity-70">{cost.detail}</span>
              )}
            </span>
          }
        />
        <InfoRow label="tokens" value={usageLine(result.usage)} mono />
        {result.sinkFailures ? (
          // Said rather than hidden: a client that saw no events and a
          // successful result would otherwise have no way to tell which half
          // broke.
          <InfoRow
            label="events"
            value={`${result.sinkFailures} could not be delivered to this window`}
          />
        ) : null}
      </div>

      <p className="mt-3 text-xs opacity-60" data-testid="step-where-stored">
        The transcript is in the store. The Store tab reads it — step {result.stepId}.
      </p>
    </Panel>
  );
}

function SavedStepPanel({ step, verify, onVerify, children }: { step: StepView; verify: StepVerifyResult | null; onVerify: () => void; children?: ReactNode }) {
  const messages = step.messages ?? [];
  const blocks = messages.flatMap((message) => message.content ?? []);
  const questionUses = blocks.filter((block) => block.kind === "tool_use" && block.name === "question");
  const questionResults = blocks.filter((block) => block.kind === "tool_result" && questionUses.some((use) => use.id === block.toolUseId));
  const changedNotes = blocks.filter((block) => block.kind === "text" && /(?:Changed|Missing) while this step waited:/.test(block.text ?? ""));
  const manifest = step.contextManifest as { compactions?: { start?: number; end?: number; summary?: string; estimateBefore?: number; estimateAfter?: number; cost?: { usd?: number } }[] } | undefined;
  const compactions = manifest?.compactions ?? [];
  return (
    <Panel data-testid="step-saved">
      <div className="flex flex-wrap items-center gap-2">
        <Pill tone={step.status === "suspended" ? "amber" : step.status === "done" ? "green" : "slate"}>{step.status}</Pill>
        <span className="text-sm">step {step.id}</span>
        <span className="text-xs opacity-60">{step.componentRoot}</span>
      </div>
      <div className="mt-3 grid gap-2 text-sm sm:grid-cols-3">
        <InfoRow label="question uses" value={String(questionUses.length)} mono />
        <InfoRow label="question results" value={String(questionResults.length)} mono />
        <InfoRow label="saved cost" value={costLine(step.cost).text} />
      </div>
      {changedNotes.map((note, index) => <Alert key={index} tone="amber" title="Declared file changed while waiting" description={note.text ?? ""} data-testid="step-file-note" />)}
      {compactions.length > 0 && <div className="mt-3 space-y-2" data-testid="step-compactions">
        <p className="text-sm font-medium">Compactions</p>
        {compactions.map((item, index) => <div key={index} className="rounded border border-white/10 p-2 text-xs">
          <p>messages {item.start ?? "?"}–{item.end ?? "?"} · estimated tokens {item.estimateBefore ?? "?"} → {item.estimateAfter ?? "?"} · cost {item.cost?.usd === undefined ? "unknown" : `$${item.cost.usd.toFixed(5)}`}</p>
          <p className="mt-1 whitespace-pre-wrap opacity-70">{item.summary}</p>
        </div>)}
      </div>}
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button variant="outline" size="sm" onClick={onVerify} data-testid="step-verify">Verify transcript</Button>
        {verify && <span className="text-sm" data-testid="step-verify-result">{verify.ok ? `verified · ${verify.messages} messages · ${verify.signatureLength} signature bytes` : `verification failed: ${verify.mismatch ?? "unknown mismatch"}`}</span>}
      </div>
      <details className="mt-3 text-sm"><summary className="cursor-pointer">Canonical transcript ({messages.length} messages)</summary>
        <pre className="mt-2 max-h-96 overflow-auto rounded bg-black/20 p-3 text-xs" data-testid="step-transcript">{JSON.stringify(messages, null, 2)}</pre>
      </details>
      {children}
    </Panel>
  );
}

/** The outcome's colour. `reject` is deliberately not an alarm. */
export function outcomeTone(outcome: string | undefined): "green" | "slate" | "amber" | "red" {
  switch (outcome) {
    case "ok":
      return "green";
    case "reject":
      return "slate";
    case "blocked":
      return "amber";
    case "failed":
      return "red";
    default:
      return "slate";
  }
}

/**
 * lowersReasoning reports whether a step on this route will have its reasoning
 * lowered to `none`.
 *
 * It mirrors `reasoningFor` in `internal/runtime/compile.go`, and only to
 * EXPLAIN — the daemon decides. A copy that drifts says the wrong thing about a
 * decision somebody else made, which is why the page shows what the route is
 * set to rather than asserting what will be sent.
 */
export function lowersReasoning(route: RouteView): boolean {
  // `/v1/responses` accepts tools and reasoning together, so nothing is
  // lowered there — and saying otherwise would be the page teaching a rule
  // that does not apply to the route in front of it.
  if ((route.openaiEndpoint ?? "") === "responses") return false;
  const effort = route.reasoningEffort ?? "";
  return effort !== "" && effort !== "none";
}
