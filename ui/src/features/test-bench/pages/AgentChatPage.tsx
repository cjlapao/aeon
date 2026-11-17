import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, MultiToggle, Panel, Pill } from "../../../components/kit";
import {
  AgentChat, chatItemsFromEvents, chatItemsFromTranscript, deriveSession, DiffLines, NEGATIVE_ANSWER,
  type ChatActions, type ChatAgent, type ChatEventLike, type ChatItem, type ComposerOption, type FileChange, type SteerRequest,
} from "../../../components/agent-chat";
import { AppIcon } from "../../../components/icons";
import { useChatActions } from "../../loop-workspace/center/useChatActions";
import { LoopWorkspaceView } from "../../loop-workspace/LoopWorkspaceView";
import { syntheticLoop } from "../syntheticLoop";
import type { LiveState } from "../../../lib/live";
import { listenerUnavailable, onEvent, rpc, toRpcError } from "../../../lib/bridge";
import { useSkillPicker } from "../../../lib/skills";
import { REASONING_EFFORTS, type AeonEvent, type RouteView, type RoutesListResult, type SubscribeResult } from "../../../lib/protocol";
import { SCENARIOS, WAIT_KIND, type Scenario, type WaitSpec } from "../agentChatScenarios";
import type { ChatAttachment } from "../../../components/agent-chat";

/**
 * Agent chat — the bench for the reusable chat component
 * (`src/components/agent-chat`), until the loop workspace exists to host it.
 *
 * Two modes, on purpose:
 * - **Scenarios** replay scripted event envelopes through the SAME adapter a
 *   live loop uses, with play/step/reset, and every control answers locally
 *   (an answer appends `question.answered`, a steer appends `queued` then
 *   `delivered`…) — so the whole interaction loop is testable with no daemon,
 *   no model and no spend.
 * - **Live** subscribes to the daemon and wires every control to its real RPC
 *   (`step.steer`, `step.stop`, `answer`, `loop.answer`, `step.steer.cancel`,
 *   `workspace.git.diff`).
 */

const EFFORTS: ComposerOption[] = REASONING_EFFORTS.map((e) => ({ value: e, label: e }));
const SPEEDS = [{ id: "1", label: "1×" }, { id: "4", label: "4×" }, { id: "20", label: "20×" }, { id: "0", label: "instant" }];
const LIVE_KINDS = ["loop.*", "step.*", "model.delta", "question.*"];

type Log = { at: string; text: string };

const BENCH_LIVE: LiveState = { state: "current", endpoint: "bench", daemonVersion: "bench", protocolVersion: 1, generation: 0 };
const ROUTES = [{ value: "anthropic/sonnet", label: "Anthropic · Sonnet" }, { value: "anthropic/opus", label: "Anthropic · Opus" }, { value: "openai/gpt", label: "OpenAI · GPT" }];
const demoDiff = (file: { path: string; linesAdded?: number }) =>
  `--- a/${file.path}\n+++ b/${file.path}\n@@ -1,3 +1,${(file.linesAdded ?? 1) + 2} @@\n # Aeon QA Workspace Survey\n-Module: example.com/hello\n+Module: example.com/hello · Go 1.23\n+Status: 1 failing test identified in cmd/aeon`;

function useLog() {
  const [log, setLog] = useState<Log[]>([]);
  const add = useCallback((text: string) => setLog((l) => [{ at: new Date().toLocaleTimeString(), text }, ...l].slice(0, 60)), []);
  return { log, add, clear: () => setLog([]) };
}

/** Gate items get what the stream does not carry: the scenario's diff, or the live workspace's. */
function enrichGates(items: ChatItem[], extras: Record<string, Partial<Extract<ChatItem, { kind: "gate" }>>>): ChatItem[] {
  return items.map((it) => (it.kind === "gate" && extras[it.id] ? { ...it, ...extras[it.id] } as ChatItem : it));
}

function ViewPicker({ agents, value, onChange }: { agents: string[]; value: string; onChange: (v: string) => void }) {
  if (agents.length === 0) return null;
  return <MultiToggle size="sm" value={value} onChange={(v: string) => onChange(v)}
    options={[{ value: "", label: "Loop view" }, ...agents.map((a) => ({ value: a, label: a }))]} data-testid="agent-chat-view" />;
}

function DiffPanel({ title, diff, onClose }: { title: string; diff: string; onClose: () => void }) {
  return <Panel title={`Diff · ${title}`} data-testid="agent-chat-diff-panel">
    <div className="mb-2 flex justify-end"><button type="button" className="text-sm text-blue-700 underline" onClick={onClose}>close</button></div>
    {diff ? <DiffLines text={diff} /> : <p className="text-sm text-slate-500">No changes against HEAD.</p>}
  </Panel>;
}

function ActionLog({ log, onClear }: { log: Log[]; onClear: () => void }) {
  return <Panel title="What the chat asked the host to do" data-testid="agent-chat-log">
    {log.length === 0 ? <p className="text-sm text-slate-500">Click something in the chat — every callback lands here.</p> :
      <ol className="max-h-64 space-y-1 overflow-y-auto font-mono text-xs">{log.map((l, i) => <li key={i}><span className="text-slate-400">{l.at}</span> {l.text}</li>)}</ol>}
    {log.length > 0 && <button type="button" className="mt-2 text-xs text-blue-700 underline" onClick={onClear}>clear</button>}
  </Panel>;
}

/* ───────────────────────────── Scenarios ───────────────────────────── */

function ScenarioMode() {
  const [scenarioId, setScenarioId] = useState(SCENARIOS[0]!.id);
  const scenario: Scenario = SCENARIOS.find((s) => s.id === scenarioId) ?? SCENARIOS[0]!;
  // What has happened (scripted events stamped as they play, plus everything
  // the person's clicks appended) and what is still to come. A wait marker at
  // the head of the queue holds playback until it is satisfied.
  const [played, setPlayed] = useState<ChatEventLike[]>([]);
  const [queue, setQueue] = useState<ChatEventLike[]>([]);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState("4");
  const [steerTexts, setSteerTexts] = useState<Record<number, string>>({});
  const [view, setView] = useState("");
  const [forceReadOnly, setForceReadOnly] = useState<"auto" | "on" | "off">("auto");
  const [autoApprove, setAutoApprove] = useState(false);
  const [layout, setLayout] = useState<"chat" | "page">("chat");
  const [diff, setDiff] = useState<{ title: string; diff: string } | null>(null);
  const { log, add, clear } = useLog();
  // S67: the catalog reaches the bench composer too — `/` and the plus
  // menu open the same picker the loop workspace shows. No daemon means
  // the picker says so honestly; the scripted events stay scripted.
  const skills = useSkillPicker(true);
  const nextSeq = useRef(1);
  const lastScriptAt = useRef<string | undefined>(undefined);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const autoAnswered = useRef(new Set<number>());

  // Played events carry real time, so durations and the live clock are what
  // the person actually watched, not the script's invented timestamps.
  const stamp = useCallback((ev: ChatEventLike): ChatEventLike =>
    ({ ...ev, seq: nextSeq.current++, at: new Date().toISOString() }), []);

  const later = useCallback((fn: () => void, ms: number) => { timers.current.push(setTimeout(fn, ms)); }, []);

  const reset = useCallback((toEnd = false, autoplay = false) => {
    timers.current.forEach(clearTimeout); timers.current = [];
    autoAnswered.current.clear();
    lastScriptAt.current = undefined;
    setSteerTexts({}); setView(""); setDiff(null);
    if (toEnd) {
      const drained = drainUntilWait(scenario.events, [], stamp);
      setPlayed(drained.played); setQueue(drained.queue);
    } else {
      setPlayed([]); setQueue(scenario.events);
    }
    setPlaying(autoplay);
  }, [scenario, stamp]);

  // An interactive scenario plays itself from the start at 1×; the others
  // open at their end, the way they always have.
  useEffect(() => {
    if (scenario.interactive) { setSpeed("1"); reset(false, true); } else reset(true);
    return () => { timers.current.forEach(clearTimeout); timers.current = []; };
  }, [scenarioId]); // eslint-disable-line react-hooks/exhaustive-deps

  const head = queue[0];
  const waiting = head?.kind === WAIT_KIND && !waitOutcome(head, played).done ? head.payload as WaitSpec : null;

  /** Takes a satisfied wait off the head, splicing in the branch the answer chose. */
  const passWait = useCallback((wait: ChatEventLike, rest: ChatEventLike[]) => {
    const outcome = waitOutcome(wait, played);
    const spec = wait.payload as WaitSpec;
    const branch = (outcome.yes ? spec.branches?.yes : spec.branches?.no) ?? [];
    lastScriptAt.current = undefined;
    setQueue([...branch, ...rest]);
  }, [played]);

  useEffect(() => {
    if (!playing) return;
    if (!head) { setPlaying(false); return; }
    if (head.kind === WAIT_KIND) {
      // Unsatisfied: nothing to do — this effect runs again when the person's
      // answer lands in `played`.
      if (waitOutcome(head, played).done) passWait(head, queue.slice(1));
      return;
    }
    const factor = Number(speed);
    if (factor === 0) {
      const drained = drainUntilWait(queue, played, stamp);
      setPlayed(drained.played); setQueue(drained.queue);
      return;
    }
    const prev = lastScriptAt.current;
    const gap = prev && head.at ? Math.min(1500, Math.max(15, (Date.parse(head.at) - Date.parse(prev)) / factor)) : 150;
    const t = setTimeout(() => {
      lastScriptAt.current = head.at;
      setPlayed((p) => [...p, stamp(head)]);
      setQueue((q) => q.slice(1));
    }, gap);
    return () => clearTimeout(t);
  }, [playing, head, queue, played, speed, stamp, passWait]);

  const append = useCallback((kind: string, payload: Record<string, unknown>) => {
    setPlayed((p) => [...p, stamp({ seq: 0, kind, payload })]);
  }, [stamp]);

  // Auto-approve, read live like plai-client-app's: every permission question
  // still open gets "approve" after a beat (long enough to see the card), and
  // switching it on while one is open answers that one too.
  useEffect(() => {
    if (!autoApprove) return;
    const answered = new Set(played.filter((e) => e.kind === "question.answered")
      .map((e) => (e.payload as { questionId?: number }).questionId));
    for (const e of played) {
      const p = e.payload as { id?: number; kind?: string; stepId?: number };
      if (e.kind !== "question.asked" || p.kind !== "permission" || p.id === undefined) continue;
      if (answered.has(p.id) || autoAnswered.current.has(p.id)) continue;
      const id = p.id;
      autoAnswered.current.add(id);
      later(() => {
        add(`auto-approved question ${id}`);
        append("question.answered", { questionId: id, answer: "approve", stepId: p.stepId });
      }, 900);
    }
  }, [autoApprove, played, add, append, later]);

  const session = useMemo(() => deriveSession(played), [played]);
  const items = useMemo(() => {
    const base = scenario.transcript
      ? chatItemsFromTranscript(scenario.transcript.messages, { agent: scenario.transcript.agent })
      : chatItemsFromEvents(played, { steerTexts: { ...scenario.steerTexts, ...steerTexts } });
    return enrichGates([...base, ...(queue.length === 0 ? scenario.extraItems ?? [] : [])], scenario.gateExtras ?? {});
  }, [scenario, played, steerTexts, queue.length]);

  const running = session.runs.filter((r) => r.state !== "done");
  // History only once the loop itself ended — a loop waiting at a gate has
  // nobody running and must still be answerable.
  const ended = session.loopStatus === "finished" || session.loopStatus === "blocked";
  const readOnly = forceReadOnly === "on" || (forceReadOnly === "auto" && (Boolean(scenario.readOnly) || ended));
  const runOf = (agent: string) => session.runs.find((r) => r.agent === agent);

  const actions: ChatActions = {
    onAnswer: (q, answer) => {
      add(`answer question ${q.id}: ${answer}`);
      append("question.answered", { questionId: Number(q.id), answer, stepId: session.questionStep.get(Number(q.id)) });
    },
    onGate: (gateId, answer, feedback) => {
      add(`loop.answer ${gateId}: ${answer}${feedback ? ` · feedback "${feedback}"` : ""}`);
      const qid = Number(gateId.replace("gate:", ""));
      append("loop.advanced", { lane: session.gates.get(qid)?.lane ?? "gate", questionId: qid, answer, outcome: answer });
    },
    onViewDiff: (file: FileChange) => {
      add(`view diff ${file.path}`);
      setDiff({ title: file.path, diff: demoDiff(file) });
    },
    onCancelSteer: (steerId) => { add(`step.steer.cancel ${steerId}`); append("step.steer.cancelled", { steerId }); },
    // S69: the scripted doors. Everything the script "put" reads back as
    // one honest pixel — except a revision at/above 900 (or named gone),
    // which REJECTS: that is the visibly broken chip and its sentence,
    // walked without a daemon (AT11/ruling 3).
    attachmentPreview: async (document, rev) => {
      add(`attachment.read ${document} rev ${rev} (scripted)`);
      if (rev >= 900 || document.includes("gone")) throw new Error(`attachment.read ${document}@${rev}: no longer in this loop's store`);
      return "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
    },
  };

  const onSteer = (req: SteerRequest) => {
    const run = runOf(req.target);
    const steerId = 9000 + nextSeq.current;
    add(`step.steer → ${req.target} (step ${run?.stepId ?? "?"})${req.text ? `: "${req.text}"` : ""}${req.route ? ` · route ${req.route}` : ""}${req.reasoningEffort ? ` · effort ${req.reasoningEffort}` : ""}`);
    if (req.text) setSteerTexts((t) => ({ ...t, [steerId]: req.text }));
    append("step.steer.queued", { stepId: run?.stepId, agent: req.target, steerId, hasText: Boolean(req.text), route: req.route, reasoningEffort: req.reasoningEffort });
    // The next turn boundary, simulated.
    later(() => {
      append("step.steer.delivered", { stepId: run?.stepId, agent: req.target, steerId, iteration: 9 });
      if (req.route || req.reasoningEffort) append("step.route.changed", { stepId: run?.stepId, agent: req.target, route: req.route ?? run?.route, reasoningEffort: req.reasoningEffort ?? run?.reasoningEffort, fromIteration: 9 });
    }, 1500);
  };
  const onStop = (target: string) => {
    const run = runOf(target);
    add(`step.stop → ${target} (step ${run?.stepId ?? "?"})`);
    append("step.stopped", { stepId: run?.stepId, agent: target });
    append("loop.waiting", { reason: "stopped", stepId: run?.stepId });
  };

  // The S39 doors, scripted like everything else in this mode: the words
  // echo back through the same events the real daemon would have sent —
  // including S68's chosen-skills receipt: the body echoes the PURE words
  // and the names ride beside them, exactly the posted shape.
  const onMessage = (text: string, route?: string, skills?: readonly string[], files?: readonly ChatAttachment[]) => {
    const id = 9000 + nextSeq.current;
    // The scripted put (S69 AT2): bytes "cross once", receipts come back
    // as the store's shape would give them; a file named *gone*/*missing*
    // pins revision 900 so its chip demonstrates the honest absence.
    const receipts = (files ?? []).map((a, i) => {
      const name = a.file.name;
      const lost = /gone|missing/i.test(name);
      const sha = `${(name.length * 2654435761 >>> 0).toString(16).padStart(8, "0").slice(0, 8)}`;
      add(`attachment.put ${name} · ${a.file.size} bytes (scripted${lost ? " · revision lost" : ""})`);
      return { name, document: `@attachments/${sha}-${name}`, rev: lost ? 900 : 1 + i,
        mediaType: a.file.type || "application/octet-stream", size: a.file.size, sha256: `${sha}${"0".repeat(56)}` };
    });
    add(`loop.message → "${text}"${route ? ` · route ${route}` : ""}${skills?.length ? ` · skills ${skills.join(", ")}` : ""}${receipts.length ? ` · ${receipts.length} attachment(s)` : ""}`);
    append("loop.message.posted", { loopId: "loop-demo", messageId: id, seq: id, author: "human", body: text,
      ...(skills?.length ? { skills } : {}), ...(receipts.length ? { attachments: receipts } : {}) });
    return Promise.resolve();
  };
  const loadRunOptions = () => {
    add("loop.run.options (scripted: none — the real verdict comes from loop.run.options)");
    return Promise.resolve([] as { blueprint: string; version?: number; runnable: boolean; reason?: string; candidates?: { kind: "goal" | "task"; id: string; title: string; state: string }[] }[]);
  };
  const onRunBlueprint = (req: { blueprint: string; goalId?: string; taskId?: string; message?: string }) => {
    add(`loop.run ${req.blueprint}${req.goalId ? ` · goal ${req.goalId}` : ""}${req.taskId ? ` · task ${req.taskId}` : ""}`);
    append("loop.run.started", { loopId: "loop-demo", runSeq: 9000 + nextSeq.current, blueprint: req.blueprint, startedByKind: "user" });
    return Promise.resolve();
  };

  const stepOnce = () => {
    if (!head) return;
    if (head.kind === WAIT_KIND) { if (!waiting) passWait(head, queue.slice(1)); return; }
    lastScriptAt.current = head.at;
    setPlayed((p) => [...p, stamp(head)]);
    setQueue((q) => q.slice(1));
  };
  const toEnd = () => {
    const drained = drainUntilWait(queue, played, stamp);
    setPlayed(drained.played); setQueue(drained.queue);
  };

  const agentNames = session.runs.map((r) => r.agent);
  const target = view || running[0]?.agent;
  const agents: Record<string, ChatAgent> | undefined = scenario.agents;
  const toGo = queue.filter((e) => e.kind !== WAIT_KIND).length;
  const page = layout === "page" && Boolean(scenario.lanes);
  const loop = useMemo(() => (scenario.lanes ? syntheticLoop(scenario.lanes, played) : null), [scenario.lanes, played]);

  return <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
    <div className="min-w-0 space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <select aria-label="scenario" value={scenarioId} onChange={(e) => setScenarioId(e.target.value)}
          className="rounded-lg border border-slate-300 bg-white px-2 py-1.5 text-sm dark:border-white/20 dark:bg-slate-900">
          {SCENARIOS.map((s) => <option key={s.id} value={s.id}>{s.title}</option>)}
        </select>
        <button type="button" className="rounded-lg border px-3 py-1.5 text-sm" onClick={() => { if (!head) reset(false, true); else setPlaying((p) => !p); }} data-testid="agent-chat-play">
          <span className="inline-flex items-center gap-1.5"><AppIcon name={playing ? "pause" : "play"} />{playing ? "Pause" : "Play"}</span></button>
        <button type="button" className="rounded-lg border px-3 py-1.5 text-sm disabled:opacity-50" disabled={!head || Boolean(waiting)} onClick={stepOnce} data-testid="agent-chat-step">Step +1</button>
        <button type="button" className="rounded-lg border px-3 py-1.5 text-sm" onClick={() => reset()} data-testid="agent-chat-reset"><span className="inline-flex items-center gap-1.5"><AppIcon name="reset" />Reset</span></button>
        <button type="button" className="rounded-lg border px-3 py-1.5 text-sm disabled:opacity-50" disabled={!head || Boolean(waiting)} onClick={toEnd} data-testid="agent-chat-end">
          <span className="inline-flex items-center gap-1.5"><AppIcon name="skip" />{scenario.interactive ? "Next stop" : "End"}</span></button>
        <MultiToggle size="sm" value={speed} onChange={(v: string) => setSpeed(v)} options={SPEEDS.map((s) => ({ value: s.id, label: s.label }))} />
        <Pill tone="slate">{played.length} played · {toGo} to go</Pill>
      </div>
      <p className="text-sm text-slate-600 dark:text-slate-300">{scenario.description}</p>
      {waiting && <div role="status" data-testid="agent-chat-waiting"
        className="agent-chat-attention flex items-center gap-2 rounded-lg border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-400/40! dark:bg-amber-400/10! dark:text-amber-200!">
        <span className="h-2 w-2 animate-pulse rounded-full bg-amber-500" />
        <span><b>Waiting for you:</b> {waiting.label}. Playback continues when you answer in the chat.</span>
      </div>}
      <div className="flex flex-wrap items-center gap-3">
        {scenario.lanes && <MultiToggle size="sm" value={layout} onChange={(v: string) => setLayout(v as "chat" | "page")}
          options={[{ value: "chat", label: "Chat only" }, { value: "page", label: "Full page" }]} data-testid="agent-chat-layout" />}
        {!page && <ViewPicker agents={agentNames} value={view} onChange={setView} />}
        <label className="text-sm">Read-only{" "}
          <select aria-label="read-only" value={forceReadOnly} onChange={(e) => setForceReadOnly(e.target.value as "auto" | "on" | "off")}
            className="rounded border border-slate-300 bg-white px-1 py-0.5 text-sm dark:border-white/20 dark:bg-slate-900">
            <option value="auto">auto (finished = read-only)</option><option value="on">on</option><option value="off">off</option>
          </select></label>
      </div>
      {page ? <div className="h-[78vh] overflow-hidden rounded-2xl border border-slate-200 dark:border-white/10!" data-testid="agent-chat-full-page">
        <LoopWorkspaceView loopId="loop-demo" loop={loop} live={BENCH_LIVE} connected items={items} session={session} git={null}
          chat={{ actions, onSteer: async (r) => onSteer(r), onStop: async (t) => onStop(t), onMessage, loadRunOptions, onRunBlueprint, onLoopOptions: async () => undefined }} routes={ROUTES} componentRoot=""
          renderFiles={() => <p className="p-4 text-sm text-slate-500">The Files tab lists a real workspace — open Live mode to try it.</p>}
          renderFile={(path) => <div className="p-4"><h3 className="mb-2 break-all font-mono text-sm font-semibold">{path}</h3><DiffLines text={demoDiff({ path })} /></div>} />
      </div> : <AgentChat height="72vh" items={items} agents={agents} actions={actions} agentFilter={view || undefined} readOnly={readOnly} liveClock
        picker={skills.panel ?? undefined} onPickerDismiss={skills.close}
        composer={{
          targets: running.map((r) => ({ value: r.agent, label: r.agent })),
          routes: ROUTES,
          efforts: EFFORTS,
          currentRoute: target ? runOf(target)?.route ?? "anthropic/sonnet" : undefined,
          currentEffort: target ? runOf(target)?.reasoningEffort ?? "medium" : undefined,
          onSteer: skills.steerWith(onSteer),
          onStop,
          onSpecialKeyChange: skills.onSpecialKeyChange,
          onAddAction: skills.onAddAction,
          chips: skills.chips,
          autoApprove: { on: autoApprove, onChange: (on) => { add(`auto-approve ${on ? "on" : "off"}`); setAutoApprove(on); } },
          disabled: running.length === 0,
          disabledReason: "No agent is running — nothing to steer.",
        }} />}
    </div>
    <div className="space-y-4">
      {diff && <DiffPanel title={diff.title} diff={diff.diff} onClose={() => setDiff(null)} />}
      <ActionLog log={log} onClear={clear} />
      <Panel title="Agents in this run">
        {session.runs.length === 0 ? <p className="text-sm text-slate-500">None yet.</p> :
          <ul className="space-y-1 text-sm">{session.runs.map((r) => <li key={r.stepId} className="flex justify-between gap-2 font-mono">
            <span>{r.agent} · step {r.stepId}</span><span className="text-slate-500">{r.state}{r.route ? ` · ${r.route}` : ""}</span></li>)}</ul>}
      </Panel>
    </div>
  </div>;
}

/** Is a wait satisfied by what has happened, and did the answer read as yes? */
function waitOutcome(wait: ChatEventLike, played: readonly ChatEventLike[]): { done: boolean; yes: boolean } {
  const spec = wait.payload as WaitSpec;
  const answerOf = (pred: (p: Record<string, unknown>) => boolean, kind: string) => {
    const ev = played.find((e) => e.kind === kind && pred((e.payload ?? {}) as Record<string, unknown>));
    return ev ? String((ev.payload as { answer?: unknown }).answer ?? "") : undefined;
  };
  if (spec.gate !== undefined) {
    const answer = answerOf((p) => p.questionId === spec.gate, "loop.advanced");
    return { done: answer !== undefined, yes: answer !== undefined && !NEGATIVE_ANSWER.test(answer) };
  }
  const answers = (spec.questions ?? []).map((id) => answerOf((p) => p.questionId === id, "question.answered"));
  const done = answers.every((a) => a !== undefined);
  return { done, yes: done && !NEGATIVE_ANSWER.test(answers[0] ?? "") };
}

/** Plays everything up to the first wait that is not yet satisfied, instantly. */
function drainUntilWait(queue: readonly ChatEventLike[], played: readonly ChatEventLike[], stamp: (e: ChatEventLike) => ChatEventLike) {
  const out = [...played];
  const rest = [...queue];
  while (rest.length > 0) {
    const head = rest[0]!;
    if (head.kind === WAIT_KIND) {
      const outcome = waitOutcome(head, out);
      if (!outcome.done) break;
      const spec = head.payload as WaitSpec;
      rest.splice(0, 1, ...((outcome.yes ? spec.branches?.yes : spec.branches?.no) ?? []));
      continue;
    }
    out.push(stamp(head));
    rest.shift();
  }
  return { played: out, queue: rest };
}

/* ───────────────────────────── Live ───────────────────────────── */

interface LoopRow { id: string; title: string; status: string; componentRoot: string; slug: string }

function LiveMode() {
  const [loops, setLoops] = useState<LoopRow[]>([]);
  const [loopId, setLoopId] = useState("");
  const [routes, setRoutes] = useState<RouteView[]>([]);
  const [events, setEvents] = useState<AeonEvent[]>([]);
  const [steerTexts, setSteerTexts] = useState<Record<number, string>>({});
  const [view, setView] = useState("");
  const [error, setError] = useState("");
  const [diff, setDiff] = useState<{ title: string; diff: string } | null>(null);
  const [gateExtras, setGateExtras] = useState<Record<string, Partial<Extract<ChatItem, { kind: "gate" }>>>>({});
  const { log, add, clear } = useLog();
  const subId = useRef<string | null>(null);
  // S67: same picker as the loop workspace — one hook, no host sentences.
  const skills = useSkillPicker(true);

  useEffect(() => {
    void (async () => {
      try {
        const [l, r] = await Promise.all([rpc<{ loops: LoopRow[] }>("loop.list", { all: true }), rpc<RoutesListResult>("routes.list")]);
        setLoops(l.loops ?? []); setRoutes(r.routes ?? []);
        setLoopId((cur) => cur || l.loops?.find((x) => x.status === "running")?.id || l.loops?.[0]?.id || "");
      } catch (e) { setError(toRpcError(e).message); }
    })();
  }, []);

  // One listener, StrictMode-safe (the `disposed` guard ChatPage documents).
  useEffect(() => {
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void (async () => {
      try {
        const stop = await onEvent((payload) => {
          const ev = payload as AeonEvent;
          if (!subId.current || ev.subscriptionId !== subId.current) return;
          setEvents((list) => (list.some((x) => x.seq === ev.seq) ? list : [...list, ev]));
        });
        if (disposed) { stop(); return; }
        unlisten = stop;
        // after: 0 replays what the daemon's ring still holds, so a loop that
        // is already halfway shows its history.
        const sub = await rpc<SubscribeResult>("subscribe", { filter: { kinds: LIVE_KINDS }, after: 0 });
        subId.current = sub.subscriptionId;
      } catch (e) { if (!disposed) setError(listenerUnavailable(e).message); }
    })();
    return () => { disposed = true; unlisten?.(); };
  }, []);

  const loop = loops.find((l) => l.id === loopId);
  // A loop's events: its own loop.* by loopId, and every step event of a step it dispatched.
  const scoped = useMemo(() => {
    if (!loopId) return events;
    const steps = new Set<number>();
    for (const ev of events) {
      const p = (ev.payload ?? {}) as Record<string, unknown>;
      if (ev.kind === "loop.step.dispatched" && p.loopId === loopId && typeof p.stepId === "number") steps.add(p.stepId);
    }
    return events.filter((ev) => {
      const p = (ev.payload ?? {}) as Record<string, unknown>;
      return p.loopId === loopId || (typeof p.stepId === "number" && steps.has(p.stepId));
    });
  }, [events, loopId]);

  const session = useMemo(() => deriveSession(scoped), [scoped]);
  const items = useMemo(() => enrichGates(chatItemsFromEvents(scoped, { steerTexts }), gateExtras), [scoped, steerTexts, gateExtras]);

  // A gate gets the diff of the last file written before it.
  useEffect(() => {
    if (!loop) return;
    items.forEach((it, i) => {
      if (it.kind !== "gate" || gateExtras[it.id]) return;
      const write = [...items.slice(0, i)].reverse().find((x) => x.kind === "tool" && x.file);
      const path = write?.kind === "tool" ? write.file?.path : undefined;
      setGateExtras((g) => ({ ...g, [it.id]: { description: path ? `Changes to ${path} since HEAD.` : undefined } }));
      if (!path) return;
      void rpc<{ diff?: string }>("workspace.git.diff", { componentRoot: loop.componentRoot, path })
        .then((r) => setGateExtras((g) => ({ ...g, [it.id]: { ...g[it.id], preview: r.diff, previewLines: r.diff?.split("\n").length } })))
        .catch(() => undefined);
    });
  }, [items, loop, gateExtras]);

  const rememberSteer = useCallback((steerId: number, text: string) => setSteerTexts((t) => ({ ...t, [steerId]: text })), []);
  const showDiff = useCallback((title: string, text: string) => setDiff({ title, diff: text }), []);
  const host = useMemo(() => ({ loopId, session, rememberSteer }), [loopId, session, rememberSteer]);
  const { actions, onSteer, onStop } = useChatActions(host, loop?.componentRoot ?? "", showDiff, add);
  const runOf = (agent: string) => session.runs.find((r) => r.agent === agent && r.state !== "done") ?? session.runs.find((r) => r.agent === agent);

  const running = session.runs.filter((r) => r.state === "running");
  const target = view || running[0]?.agent;
  const routeOptions = routes.map((r) => ({ value: r.id, label: `${r.provider} · ${r.model}` }));
  // The loop's own events win; with none in the ring, its listed status decides.
  const readOnly = session.loopStatus === "finished" || session.loopStatus === "blocked" ||
    (session.loopStatus === "unknown" && Boolean(loop) && !["running", "waiting"].includes(loop!.status));

  return <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
    <div className="min-w-0 space-y-3">
      {error && <Alert tone="red" title="Live mode" description={error} />}
      <div className="flex flex-wrap items-center gap-2">
        <select aria-label="loop" value={loopId} onChange={(e) => { setLoopId(e.target.value); setView(""); }}
          className="rounded-lg border border-slate-300 bg-white px-2 py-1.5 text-sm dark:border-white/20 dark:bg-slate-900">
          <option value="">All events (no loop filter)</option>
          {loops.map((l) => <option key={l.id} value={l.id}>{l.title || l.slug} · {l.status}</option>)}
        </select>
        <Pill tone="slate">{scoped.length} events</Pill>
        <ViewPicker agents={session.runs.map((r) => r.agent)} value={view} onChange={setView} />
      </div>
      <p className="text-sm text-slate-600 dark:text-slate-300">
        Start a loop from the Loops screen (or <code>aeon loop start</code>) and watch it here. The daemon's event ring is replayed on open, so a loop already underway shows what it still holds.
      </p>
      <AgentChat height="72vh" items={items} actions={actions} agentFilter={view || undefined} readOnly={readOnly} liveClock
        agents={Object.fromEntries(session.runs.map((r) => [r.agent, { id: r.agent, role: r.step }]))}
        empty={<p className="py-8 text-center text-sm text-slate-500">No events for this selection yet.</p>}
        picker={skills.panel ?? undefined} onPickerDismiss={skills.close}
        composer={{
          targets: running.map((r) => ({ value: r.agent, label: r.agent })),
          routes: routeOptions, efforts: EFFORTS,
          currentRoute: target ? runOf(target)?.route : undefined,
          currentEffort: target ? runOf(target)?.reasoningEffort : undefined,
          onSteer: skills.steerWith(onSteer),
          onStop,
          onSpecialKeyChange: skills.onSpecialKeyChange,
          onAddAction: skills.onAddAction,
          chips: skills.chips,
          disabled: running.length === 0, disabledReason: "No agent is running — nothing to steer.",
        }} />
    </div>
    <div className="space-y-4">
      {diff && <DiffPanel title={diff.title} diff={diff.diff} onClose={() => setDiff(null)} />}
      <ActionLog log={log} onClear={clear} />
    </div>
  </div>;
}

export function AgentChatPage() {
  const [mode, setMode] = useState<"scenarios" | "live">("scenarios");
  return <div className="space-y-4" data-testid="agent-chat-page">
    <Alert tone="slate" title="Agent chat — the reusable component, on the bench"
      description="Scenarios replay scripted daemon events through the same adapter a live loop uses; every control answers locally. Live wires the same component to the daemon. The designed loop workspace will host this component next." />
    <MultiToggle value={mode} onChange={(v: string) => setMode(v as "scenarios" | "live")}
      options={[{ value: "scenarios", label: "Scenarios (no daemon)" }, { value: "live", label: "Live (daemon)" }]} data-testid="agent-chat-mode" />
    {mode === "scenarios" ? <ScenarioMode /> : <LiveMode />}
  </div>;
}
