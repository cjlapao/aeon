import { useEffect, useState, type CSSProperties } from "react";
import { AppIcon } from "../icons";
import { CustomIcon } from "../kit";
import { AgentAvatar, Chip, DiffLines, Expand, Mono, Prose } from "./parts";
import { NEGATIVE_ANSWER as NEGATIVE, POSITIVE_ANSWER as POSITIVE, type ChatActions, type ChatAgent, type ChatItem, type ChatQuestion } from "./types";
import { formIntro, parseForm, parseFormAnswer, QuestionForm, SKIPPED } from "./QuestionForm";
import { humanSize, isPreviewable, type AttachmentReceipt } from "../../lib/attachments";

type Of<K extends ChatItem["kind"]> = Extract<ChatItem, { kind: K }>;

const REVISE = /^revise/i;
const PLAN_GATE = /plan/i;
const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

const BTN = "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50";

function useBusy() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const run = async (fn: () => void | Promise<void>) => {
    setBusy(true); setError("");
    try { await fn(); } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); } finally { setBusy(false); }
  };
  return { busy, error, run };
}

function PendingQuestion({ q, onAnswer, onAllowAll, allowedAll, disabled }: { q: ChatQuestion; onAnswer?: ChatActions["onAnswer"]; onAllowAll?: () => void; allowedAll?: boolean; disabled?: boolean }) {
  const [free, setFree] = useState("");
  const { busy, error, run } = useBusy();
  const can = Boolean(onAnswer) && !disabled && !busy;
  const answer = (value: string) => run(() => onAnswer?.(q, value));
  const form = parseForm(q.options);
  return <div className="rounded-lg border border-slate-200 bg-white p-3 dark:border-white/10! dark:bg-white/5!" data-testid="agent-chat-question-pending">
    {(form ? formIntro(q.prompt) : q.prompt) && <p className="whitespace-pre-line text-sm font-medium text-slate-800 dark:text-slate-100!">{form ? formIntro(q.prompt) : q.prompt}</p>}
    {form ? <QuestionForm questions={form} disabled={!can} onSubmit={(value) => void answer(value)} /> : q.options && q.options.length > 0 ? <div className="mt-2 flex flex-wrap gap-2">
      {q.options.map((option) => <button key={option} type="button" disabled={!can} onClick={() => void answer(option)}
        className={`${BTN} ${q.kind === "permission" && POSITIVE.test(option) ? "bg-emerald-600 text-white hover:bg-emerald-700" :
          q.kind === "permission" && NEGATIVE.test(option) ? "border border-red-300 bg-white text-red-700 hover:bg-red-50 dark:bg-transparent! dark:text-red-300! dark:border-red-400/50! dark:hover:bg-red-400/10!" :
          "border border-slate-300 bg-white text-slate-700 hover:bg-slate-50 dark:border-white/20! dark:bg-transparent! dark:text-slate-200!"}`}>
        {option}
      </button>)}
      {q.kind === "permission" && onAllowAll && !allowedAll && <button type="button" disabled={!can} onClick={onAllowAll} data-testid="agent-chat-allow-all"
        className={`${BTN} ml-auto border border-amber-400 bg-amber-50 text-amber-800 hover:bg-amber-100 dark:border-amber-400/50! dark:bg-transparent! dark:text-amber-300!`}>Allow everything in this loop…</button>}
    </div> : <form className="mt-2 flex gap-2" onSubmit={(e) => { e.preventDefault(); if (free.trim()) void answer(free.trim()); }}>
      <input value={free} onChange={(e) => setFree(e.target.value)} disabled={!can} placeholder="Type an answer…" aria-label="answer"
        className="min-w-0 flex-1 rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm dark:border-white/20! dark:bg-black/20!" />
      <button type="submit" disabled={!can || !free.trim()} className={`${BTN} bg-blue-600 text-white hover:bg-blue-700`}>Answer</button>
    </form>}
    {!onAnswer && <p className="mt-2 text-xs text-slate-500">Read-only here — answer from the loop screen.</p>}
    {error && <p role="alert" className="mt-2 text-xs text-red-700">{error}</p>}
  </div>;
}

export function QuestionCard({ item, actions, readOnly }: { item: Of<"question">; actions: ChatActions; readOnly?: boolean }) {
  // A card that arrives already answered (a reopened loop) starts folded: the history is long and the answers are
  // one click away. A card with something still owed starts open, and stays as the person left it once answered.
  const [open, setOpen] = useState(() => item.questions.some((q) => q.answer === undefined));
  const [generation, setGeneration] = useState(0);
  const answered = item.questions.filter((q) => q.answer !== undefined).length;
  const total = item.questions.length;
  const permission = item.questions.some((q) => q.kind === "permission");
  const pending = item.questions.filter((q) => q.answer === undefined);
  const done = item.questions.filter((q) => q.answer !== undefined);
  return <div data-testid="agent-chat-question" className={`my-2 rounded-xl border p-3 transition-colors duration-300 ${pending.length > 0 ? "agent-chat-attention" : ""} ${permission
    ? "border-amber-200 bg-amber-50/70 dark:border-amber-400/30! dark:bg-amber-400/10!"
    : "border-blue-100 bg-blue-50/50 dark:border-blue-400/20! dark:bg-blue-400/5!"}`}>
    <div className="flex items-center gap-2">
      <CustomIcon icon={permission ? "Key" : "Help"} size="sm" className={permission ? "text-amber-600" : "text-blue-600"} />
      <span className="font-medium text-slate-800 dark:text-slate-100!">{permission ? "Permission request" : "Ask question"}</span>
      <span className="text-slate-400">·</span>
      <Chip tone={answered === total ? "green" : "amber"}>{answered}/{total} answered</Chip>
      {done.length > 0 && <button type="button" aria-expanded={open} data-testid="agent-chat-answers-toggle"
        onClick={() => { setOpen((v) => !v); if (!open) setGeneration((g) => g + 1); }}
        className={`ml-auto inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors duration-200 ${open
          ? "border-blue-200 bg-white text-blue-700 hover:bg-blue-50 dark:border-blue-400/30! dark:bg-white/5! dark:text-blue-300! dark:hover:bg-blue-400/10!"
          : "border-blue-600 bg-blue-600 text-white hover:bg-blue-700 dark:border-blue-500! dark:bg-blue-500! dark:hover:bg-blue-400!"}`}>
        {open ? "Hide answers" : `Show ${done.length === 1 ? "answer" : `${done.length} answers`}`}
        <span className="agent-chat-turn" data-open={open}><AppIcon name="chevron-hover" size={12} /></span>
      </button>}
    </div>
    {done.length > 0 && <Expand open={open}>
      {/* Re-keyed on every open, so the tiles' staggered rise plays again. */}
      <div key={generation} className="grid gap-2 pt-3 sm:grid-cols-2" data-testid="agent-chat-answers">
        {done.map((q, i) => {
          const rows = parseFormAnswer(q.answer);
          // A form's answer is one row per question, each with its own answer (a skipped one says so).
          if (rows) return <div key={q.id} className="agent-chat-tile rounded-lg border border-slate-200 bg-white dark:border-white/10! dark:bg-white/5! sm:col-span-2"
            style={{ "--i": i } as CSSProperties} data-testid="agent-chat-answer-rows">
            <ul className="divide-y divide-slate-200 dark:divide-white/10!">
              {rows.map((row) => <li key={row.n} className="flex items-start gap-3 px-3 py-2">
                <span className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded bg-slate-200 text-xs text-slate-700 dark:bg-white/10! dark:text-slate-200!">{row.n}</span>
                <span className="min-w-0 flex-1">
                  <span className="block text-xs text-slate-500">{row.question}</span>
                  <span className={`block text-sm font-semibold ${row.skipped ? "italic font-normal text-slate-400" : "text-slate-800 dark:text-slate-100!"}`}>{row.skipped ? SKIPPED.slice(1, -1) : row.answer}</span>
                </span>
              </li>)}
            </ul>
          </div>;
          return <div key={q.id} className="agent-chat-tile rounded-lg border border-slate-200 bg-white p-2.5 dark:border-white/10! dark:bg-white/5!"
            style={{ "--i": i } as CSSProperties}>
            <p className="text-xs text-slate-500">Q{i + 1}: {q.label ?? q.prompt}</p>
            <Mono className="whitespace-pre-wrap text-sm font-semibold text-slate-800 dark:text-slate-100!">{q.answer}</Mono>
          </div>;
        })}
      </div>
    </Expand>}
    {pending.length > 0 && <div className="mt-3 space-y-2">
      {pending.map((q) => <PendingQuestion key={q.id} q={q} onAnswer={readOnly ? undefined : actions.onAnswer} onAllowAll={readOnly ? undefined : actions.onAllowAll} allowedAll={actions.allowedAll} />)}
    </div>}
  </div>;
}

export function GateCard({ item, actions, readOnly }: { item: Of<"gate">; actions: ChatActions; readOnly?: boolean }) {
  const [feedbackFor, setFeedbackFor] = useState<string | null>(null);
  const [feedback, setFeedback] = useState("");
  const [full, setFull] = useState(false);
  const { busy, error, run } = useBusy();
  const can = Boolean(actions.onGate) && !readOnly && item.state === "awaiting" && !busy;
  const submit = (answer: string, note?: string) => run(() => actions.onGate?.(item.id, answer, note));
  return <div data-testid="agent-chat-gate" data-state={item.state}
    className={`my-3 rounded-xl border-2 p-4 transition-colors duration-300 ${item.state === "awaiting"
      ? "agent-chat-attention border-amber-300 bg-amber-50/60 dark:border-amber-400/40! dark:bg-amber-400/10!"
      : "agent-chat-settle border-slate-200 bg-white dark:border-white/10! dark:bg-white/5!"}`}>
    <div className="flex items-start gap-3">
      <span className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-amber-500 text-white"><CustomIcon icon="Unlock" size="sm" /></span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold uppercase tracking-wide text-amber-600">Approval gate</span>
          {item.stepLabel && <span className="rounded border border-slate-300 bg-white px-1.5 font-mono text-xs text-slate-700 dark:border-white/20! dark:bg-transparent! dark:text-slate-200!">{item.stepLabel}</span>}
          <span className="ml-auto">{item.state === "awaiting"
            ? <Chip tone="amber">Awaiting signoff</Chip>
            : <Chip tone={item.answer && NEGATIVE.test(item.answer) ? "red" : "green"}>{item.answer ?? "answered"}</Chip>}</span>
        </div>
        <h3 className="mt-1 text-lg font-semibold text-slate-900 dark:text-slate-50!">{item.title}</h3>
        {item.description && <p className="mt-1 text-sm text-slate-600 dark:text-slate-300!">{item.description}</p>}
      </div>
    </div>
    {item.preview && <div className="mt-3"><DiffLines text={item.preview} maxLines={full ? undefined : 6} /></div>}
    {feedbackFor && <div className="mt-3">
      <textarea value={feedback} onChange={(e) => setFeedback(e.target.value)} rows={2} aria-label="feedback"
        placeholder={feedbackFor && REVISE.test(feedbackFor) ? "What should be revised? The agent gets exactly this text." : "What should change? This goes back with your answer."}
        className="w-full rounded-lg border border-red-200 bg-white p-2 text-sm dark:border-red-400/30! dark:bg-black/20!" />
    </div>}
    <div className="mt-3 flex flex-wrap items-center gap-2">
      {actions.onShowTasks && PLAN_GATE.test(`${item.stepLabel ?? ""} ${item.title}`) && <button type="button" data-testid="agent-chat-show-tasks" onClick={actions.onShowTasks}
        className={`${BTN} border border-blue-300 bg-white text-blue-700 hover:bg-blue-50 dark:bg-transparent! dark:text-blue-300! dark:border-blue-400/50!`}><CustomIcon icon="ViewRows" size="xs" /> Show tasks</button>}
      {item.preview && <button type="button" onClick={() => setFull((v) => !v)} className="inline-flex items-center gap-1.5 font-mono text-sm text-blue-700 hover:underline dark:text-blue-300!">
        <CustomIcon icon="EyeOpen" size="xs" /> {full ? "Collapse diff" : `Review full diff${item.previewLines ? ` (${item.previewLines} lines)` : ""}`}
      </button>}
      {item.state === "awaiting" && <div className="ml-auto flex flex-wrap gap-2">
        {item.options.map((option) => {
          const negative = NEGATIVE.test(option);
          const positive = POSITIVE.test(option);
          // Sending work back is never a bare click: the person says what to change, and that text goes with the answer.
          if (REVISE.test(option)) {
            return feedbackFor === option
              ? <button key={option} type="button" disabled={!can || !feedback.trim()} onClick={() => void submit(option, feedback.trim())}
                className={`${BTN} bg-amber-600 text-white hover:bg-amber-700`}>Send revision request</button>
              : <button key={option} type="button" disabled={!can} onClick={() => setFeedbackFor(option)}
                className={`${BTN} border border-amber-400 bg-white text-amber-800 hover:bg-amber-50 dark:bg-transparent! dark:text-amber-300! dark:border-amber-400/50!`}>{cap(option)}…</button>;
          }
          if (negative) {
            return feedbackFor === option
              ? <button key={option} type="button" disabled={!can} onClick={() => void submit(option, feedback.trim() || undefined)}
                className={`${BTN} bg-red-600 text-white hover:bg-red-700`}>Send {option}</button>
              : <button key={option} type="button" disabled={!can} onClick={() => setFeedbackFor(option)}
                className={`${BTN} border border-red-300 bg-white text-red-600 hover:bg-red-50 dark:bg-transparent! dark:text-red-300! dark:border-red-400/50! dark:hover:bg-red-400/10!`}>{cap(option)} with feedback</button>;
          }
          return <button key={option} type="button" disabled={!can} onClick={() => void submit(option)}
            className={`${BTN} ${positive ? "bg-emerald-600 text-white hover:bg-emerald-700" : "border border-slate-300 bg-white text-slate-700 hover:bg-slate-50 dark:bg-transparent! dark:text-slate-200!"}`}>
            {positive && <CustomIcon icon="CheckCircle" size="xs" />}{positive ? `${cap(option)} & continue loop` : option}
          </button>;
        })}
      </div>}
    </div>
    {!actions.onGate && item.state === "awaiting" && <p className="mt-2 text-xs text-slate-500">Read-only here — answer from the loop screen.</p>}
    {error && <p role="alert" className="mt-2 text-xs text-red-700">{error}</p>}
  </div>;
}

/**
 * A person-side thread message (S39 CR6). Right-aligned like a steer — it
 * comes from the person's side of the glass — and the provenance chip appears
 * only when the daemon says the words were NOT typed (`source` other than
 * `user`/absent): a template, an automation, a blueprint kickoff. Typed
 * words carry no label; the person typed them, that is the label.
 */
/**
 * The receipt row under a person-side bubble (S69 AT11): name · media type
 * · size, from the STORE'S fields. Previewable images read their pinned
 * revision through the host's door — RENDERING only, bytes never re-enter
 * a send — and a failed read renders the absence honestly: one visibly
 * broken chip naming document and revision (ruling 3), never a blank,
 * never a fake icon.
 */
function AttachmentChip({ receipt, preview }: { receipt: AttachmentReceipt; preview?: ChatActions["attachmentPreview"] }) {
  const wants = isPreviewable(receipt.mediaType) && preview !== undefined;
  const [url, setUrl] = useState<string | null>(null);
  const [broken, setBroken] = useState(false);
  useEffect(() => {
    if (!wants || !preview) return;
    let on = true;
    preview(receipt.document, receipt.rev).then((u) => { if (on) setUrl(u); }, () => { if (on) setBroken(true); });
    return () => { on = false; };
  }, [wants, preview, receipt.document, receipt.rev]);
  return <div className="flex flex-col items-end gap-1">
    {broken
      ? <span data-testid="agent-chat-attachment-broken" role="status"
        className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-red-300 bg-red-50 px-2 py-1 font-mono text-[11px] text-red-700 dark:border-red-400/40! dark:bg-red-400/10! dark:text-red-300!">
        {receipt.name} · could not be read — {receipt.document} at revision {receipt.rev} is no longer in this loop's store
      </span>
      : <span data-testid="agent-chat-attachment-chip"
        className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2 py-1 font-mono text-[11px] text-slate-600 dark:border-white/10! dark:bg-white/5! dark:text-slate-300!">
        {receipt.name} · {receipt.mediaType} · {humanSize(receipt.size)}{wants && url === null ? " · loading…" : ""}
      </span>}
    {url !== null && <img src={url} alt={receipt.name} data-testid="agent-chat-attachment-preview"
      className="max-h-40 max-w-[240px] rounded-lg border border-slate-200 dark:border-white/10!" />}
  </div>;
}

function AttachmentChips({ attachments, preview }: { attachments: readonly AttachmentReceipt[]; preview?: ChatActions["attachmentPreview"] }) {
  return <div data-testid="agent-chat-attachments" className="mr-8 mt-1 flex flex-col items-end gap-1">
    {attachments.map((r) => <AttachmentChip key={`${r.document}@${r.rev}`} receipt={r} preview={preview} />)}
  </div>;
}

export function MessageBubble({ item, actions }: { item: Of<"message">; actions?: ChatActions }) {
  return <div data-testid="agent-chat-message" data-author={item.author} className="my-3 flex flex-col items-end">
    <div className="mr-8 max-w-[75%] whitespace-pre-wrap rounded-2xl border border-slate-200 bg-slate-50 px-4 py-2.5 text-[15px] text-slate-900 dark:border-white/10! dark:bg-white/5! dark:text-slate-100!">
      <Prose text={item.text} />
    </div>
    {item.source && item.source !== "user" && <Mono className="mr-8 text-xs text-slate-500">from {item.source}</Mono>}
    {/* S68 B8: the chosen-skills receipt sits UNDER the bubble — the
     * person's words are theirs alone; the names are the durable trace of
     * whose instruction lines went into this turn's system half (which the
     * window does not show). Never inside the text. */}
    {item.skills && item.skills.length > 0 && (
      <div data-testid="agent-chat-message-skills" className="mr-8 mt-1 flex flex-wrap items-center gap-1">
        {item.skills.map((name) => (
          <Chip key={name} tone="violet">{name}</Chip>
        ))}
        <span className="text-[10px] text-slate-400">shown to the model, not the thread</span>
      </div>
    )}
    {item.attachments && item.attachments.length > 0 && <AttachmentChips attachments={item.attachments} preview={actions?.attachmentPreview} />}
  </div>;
}

const RUN_STATE = {
  running: { tone: "blue" as const, label: "running" },
  done: { tone: "green" as const, label: "done" },
  failed: { tone: "red" as const, label: "failed" },
  cancelled: { tone: "slate" as const, label: "cancelled" },
};

/**
 * The run's anchor card (S39 CR1): the step items of the run stream between
 * its `loop.run.started` and `loop.run.finished`, and this card is what the
 * run reads as on the thread. The cost is the daemon's P32 floor: `exact`
 * says "$", anything else is a floor and must wear "≥", and an absent cost
 * is unknown — never a confident "$0.00".
 */
export function RunCard({ item }: { item: Of<"run"> }) {
  const state = RUN_STATE[item.state];
  const cost = item.cost === undefined ? "—" : `${item.cost.exact ? "" : "≥ "}$${item.cost.usdAtLeast.toFixed(2)}`;
  return <div data-testid="agent-chat-run" data-state={item.state}
    className="my-3 flex flex-wrap items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2.5 dark:border-white/10! dark:bg-white/5!">
    <span className="font-medium text-slate-800 dark:text-slate-100!">Run {item.runSeq}{item.blueprint ? ` · ${item.blueprint}` : ""}</span>
    {item.version !== undefined && <Mono className="text-xs text-slate-500">v{item.version}</Mono>}
    <Chip tone={state.tone}>{state.label}</Chip>
    {item.startedBy && <span className="text-xs text-slate-500">started by {item.startedBy}</span>}
    <span className="ml-auto font-mono text-sm text-slate-700 dark:text-slate-200!">{cost}</span>
  </div>;
}

const STEER_STATE = {
  queued: { tone: "amber" as const, label: "queued · after this turn" },
  delivered: { tone: "green" as const, label: "delivered" },
  cancelled: { tone: "slate" as const, label: "withdrawn" },
};

export function SteerBubble({ item, actions, agents, readOnly }: { item: Of<"steer">; actions: ChatActions; agents?: Record<string, ChatAgent>; readOnly?: boolean }) {
  const state = STEER_STATE[item.state];
  const change = [item.route, item.reasoningEffort && `effort ${item.reasoningEffort}`].filter(Boolean).join(" · ");
  return <div data-testid="agent-chat-steer" data-state={item.state} className="my-3 flex flex-col items-end gap-1">
    <div className="flex items-center gap-2 text-sm">
      <Chip tone={state.tone}><span className="h-1.5 w-1.5 rounded-full bg-current" />{state.label}</Chip>
      <span className="font-medium text-slate-800 dark:text-slate-100!">you → {agents?.[item.target]?.name ?? item.target} · steer</span>
      <AgentAvatar id="you" agents={agents} size={24} />
    </div>
    {item.text && <div className={`mr-8 max-w-[75%] whitespace-pre-wrap rounded-2xl border border-blue-200 bg-blue-50 px-4 py-2.5 text-[15px] text-slate-900 dark:border-blue-400/30! dark:bg-blue-400/10! dark:text-slate-100! ${item.state === "cancelled" ? "line-through opacity-60" : ""}`}>{item.text}</div>}
    {change && <Mono className="mr-8 text-xs text-slate-500">switch to {change}</Mono>}
    {item.attachments && item.attachments.length > 0 && <AttachmentChips attachments={item.attachments} preview={actions.attachmentPreview} />}
    {item.state === "queued" && item.steerId !== undefined && actions.onCancelSteer && !readOnly &&
      <button type="button" onClick={() => void actions.onCancelSteer?.(item.steerId!)} className="mr-8 text-xs text-slate-500 hover:text-red-600 hover:underline">withdraw</button>}
  </div>;
}
