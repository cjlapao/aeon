import { useState, type ReactNode } from "react";
import { AppIcon } from "../icons";
import { CustomIcon, ScrollArea } from "../kit";
import { Chip, DiffLines, Expand, formatDuration, Mono, Spinner, useNow } from "./parts";
import { parsePolicySentence } from "./policyText";
import { NEGATIVE_ANSWER, POSITIVE_ANSWER, type ChatActions, type ChatItem, type ChatQuestion, type FileChange } from "./types";

type Of<K extends ChatItem["kind"]> = Extract<ChatItem, { kind: K }>;

const DIVIDER_TONE = {
  orchestrator: "border-blue-200 bg-blue-50/80 text-slate-700 dark:border-blue-400/30! dark:bg-blue-400/10! dark:text-slate-200!",
  policy: "border-slate-200 bg-white text-slate-600 dark:border-white/10! dark:bg-white/5! dark:text-slate-300!",
  neutral: "border-slate-200 bg-white text-slate-500 dark:border-white/10! dark:bg-white/5! dark:text-slate-400!",
  success: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/30! dark:bg-emerald-400/10! dark:text-emerald-300!",
  danger: "border-red-200 bg-red-50 text-red-700 dark:border-red-400/30! dark:bg-red-400/10! dark:text-red-300!",
} as const;

const DIVIDER_ICON = { orchestrator: "Orchestrator", policy: "Verified", neutral: "Info", success: "CheckCircle", danger: "Warning" } as const;

/** Highlights (agent names) are coloured inside the divider text. */
function withHighlights(text: string, highlights?: string[]): ReactNode {
  if (!highlights?.length) return text;
  const pattern = new RegExp(`(${highlights.map((h) => h.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})`, "g");
  return text.split(pattern).map((part, i) => highlights.includes(part)
    ? <span key={i} className="font-semibold text-blue-700 dark:text-blue-300!">{part}</span> : part);
}

export function Divider({ item }: { item: Of<"divider"> }) {
  const [open, setOpen] = useState(false);
  const pill = `inline-flex max-w-[80%] items-center gap-2 rounded-full border px-3 py-1 text-xs ${DIVIDER_TONE[item.tone]} ${item.tone === "orchestrator" ? "font-mono text-sm" : "font-mono"}`;
  const inner = <>
    <CustomIcon icon={DIVIDER_ICON[item.tone]} size="xs" className="shrink-0" />
    <span className="truncate">{withHighlights(item.text, item.highlights)}</span>
  </>;
  return <div data-testid="agent-chat-divider" data-tone={item.tone} className="my-3">
    <div className="flex items-center gap-3">
      <span className="h-px flex-1 bg-slate-200 dark:bg-white/10!" />
      {item.detail
        ? <button type="button" className={`${pill} cursor-pointer`} aria-expanded={open} title="Show the whole message" onClick={() => setOpen((v) => !v)} data-testid="agent-chat-divider-toggle">{inner}</button>
        : <span className={pill}>{inner}</span>}
      <span className="h-px flex-1 bg-slate-200 dark:bg-white/10!" />
    </div>
    {item.detail && open && <pre className={`mx-auto mt-2 max-h-72 max-w-[80%] overflow-auto whitespace-pre-wrap break-words rounded-lg border p-3 text-left font-mono text-xs ${DIVIDER_TONE[item.tone]}`} data-testid="agent-chat-divider-detail">{item.detail}</pre>}
  </div>;
}

/**
 * The row's leading icon. On an expandable row it gives way to a chevron:
 * on hover (or keyboard focus) the tool's icon spins out and the "can expand"
 * chevron spins in; once open, the "expanded" chevron takes its place and
 * stays. The motion lives in agentChat.css (`.agent-chat-rowicon`).
 */
function RowIcon({ icon, expandable, open }: { icon: ReactNode; expandable?: boolean; open?: boolean }) {
  return <span className="agent-chat-rowicon" data-expandable={expandable ? "true" : undefined} data-open={open ? "true" : undefined}>
    <span className="agent-chat-rowicon-base">{icon}</span>
    {expandable && <>
      <span className="agent-chat-rowicon-hover"><AppIcon name="chevron-hover" /></span>
      <span className="agent-chat-rowicon-open"><AppIcon name="chevron-expanded" /></span>
    </>}
  </span>;
}

function RowLabel({ icon, label, expandable, open }: { icon: ReactNode; label: string; expandable?: boolean; open?: boolean }) {
  return <span className="inline-flex shrink-0 items-center gap-2 text-slate-700 dark:text-slate-200!">
    <span className="inline-flex w-5 justify-center text-slate-500 dark:text-slate-400!"><RowIcon icon={icon} expandable={expandable} open={open} /></span>
    <Mono className="text-sm font-medium">{label}</Mono>
  </span>;
}

/**
 * The expanded body of a row: only the hidden part is boxed — the row itself
 * stays a plain line, so opening one never reflows the run around it.
 */
function DetailBox({ children, testId }: { children: ReactNode; testId?: string }) {
  return <div data-testid={testId} className="mb-2 ml-7 mt-1 overflow-hidden rounded-lg border border-slate-200 bg-white text-xs shadow-sm dark:border-white/10! dark:bg-black/20!">
    {children}
  </div>;
}

/** Long content scrolls inside its box, with the kit's hover-only scrollbars. */
function HoverScroll({ children, maxHeight }: { children: ReactNode; maxHeight: string }) {
  return <ScrollArea variant="hover" horizontal={false} className={maxHeight}>{children}</ScrollArea>;
}

/** Above this a thought is "long": it says so, and its body scrolls. */
const LONG_THOUGHT_WORDS = 120;

export function ThinkingRow({ item }: { item: Of<"thinking"> }) {
  const [open, setOpen] = useState(false);
  const firstLine = item.text.split("\n").find((l) => l.trim()) ?? "";
  const words = item.text.trim() ? item.text.trim().split(/\s+/).length : 0;
  const long = words > LONG_THOUGHT_WORDS;
  const body = <p className="whitespace-pre-wrap px-3 py-2.5 text-sm leading-relaxed text-slate-600 dark:text-slate-300!">{item.text}</p>;
  return <div data-testid="agent-chat-thinking" data-open={open} className="my-0.5">
    <button type="button" aria-expanded={open} onClick={() => setOpen((v) => !v)}
      className="agent-chat-row flex w-full min-w-0 items-center gap-2 rounded-md py-1 text-left">
      <RowLabel icon={<AppIcon name="think" />} label="Think" expandable open={open} />
      <span className="text-slate-400">·</span>
      <span className="min-w-0 flex-1 truncate text-sm italic text-slate-600 dark:text-slate-400!">{firstLine || "…"}</span>
      {long && <Chip tone="violet">long · {words} words</Chip>}
      {item.durationMs !== undefined && open && <Mono className="text-xs text-slate-400">{formatDuration(item.durationMs)} elapsed</Mono>}
      {item.streaming && <span className="h-2 w-2 animate-pulse rounded-full bg-blue-500" aria-label="thinking" />}
    </button>
    <Expand open={open}>
      <DetailBox testId="agent-chat-thinking-body">{long ? <HoverScroll maxHeight="max-h-[28rem]">{body}</HoverScroll> : body}</DetailBox>
    </Expand>
  </div>;
}

const TOOL_LOOK: Record<string, { icon: ReactNode; label: string }> = {
  bash: { icon: <AppIcon name="bash" />, label: "Bash" },
  read: { icon: <AppIcon name="read" />, label: "read" },
  list: { icon: <CustomIcon icon="ViewRows" size="xs" />, label: "list" },
  grep: { icon: <CustomIcon icon="Search" size="xs" />, label: "grep" },
  glob: { icon: <CustomIcon icon="Search" size="xs" />, label: "glob" },
  edit: { icon: <AppIcon name="edit" />, label: "edit" },
  write_file: { icon: <AppIcon name="edit" />, label: "write_file" },
  question: { icon: <CustomIcon icon="Help" size="xs" />, label: "question" },
};

function toolLook(tool: string) {
  return TOOL_LOOK[tool] ?? { icon: <CustomIcon icon="Cog" size="xs" />, label: tool };
}

function RunningMark({ at, liveClock }: { at?: string; liveClock?: boolean }) {
  const now = useNow(Boolean(liveClock && at));
  const since = at ? Date.parse(at) : NaN;
  return <span className="inline-flex items-center gap-1.5 text-blue-500">
    {liveClock && Number.isFinite(since) && now - since > 3000 && <Mono className="text-xs">{formatDuration(now - since)}</Mono>}
    <Spinner />
  </span>;
}

function ToolStatusMark({ item, liveClock }: { item: Of<"tool">; liveClock?: boolean }) {
  switch (item.status) {
    case "running": return <RunningMark at={item.at} liveClock={liveClock} />;
    case "done": return item.durationMs !== undefined ? <Mono className="text-xs text-slate-400">{formatDuration(item.durationMs)}</Mono> : null;
    case "error": return <Chip tone="red">failed</Chip>;
    case "skipped": return <Chip tone="slate">skipped · steered</Chip>;
    case "awaiting": return <Chip tone="amber">awaiting permission</Chip>;
    case "denied": return item.approval?.answer !== undefined
      ? <Chip tone="slate">declined by you</Chip>
      : <Chip tone="red">denied by policy</Chip>;
  }
}

/**
 * A call parked on a person's approval — plai-client-app's ToolCallCard
 * gate, in Aeon's look. Always open: approving a call without seeing what it
 * is would be a rubber stamp. The answer goes back as the permission
 * question's answer, which is what releases the parked step.
 */
function ApprovalCard({ item, actions, readOnly }: { item: Of<"tool"> & { approval: ChatQuestion }; actions: ChatActions; readOnly?: boolean }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const look = toolLook(item.tool);
  const options = item.approval.options?.length ? item.approval.options : ["deny", "approve"];
  const can = Boolean(actions.onAnswer) && !readOnly && !busy;
  const answer = async (value: string) => {
    setBusy(true); setError("");
    try { await actions.onAnswer?.(item.approval, value); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setBusy(false); }
  };
  return <div data-testid="agent-chat-approval" className="agent-chat-attention my-2 overflow-hidden rounded-xl border border-amber-300 bg-amber-50/70 dark:border-amber-400/40! dark:bg-amber-400/10!">
    <div className="flex items-center gap-2.5 px-3 py-2">
      <CustomIcon icon="Unlock" size="sm" className="shrink-0 text-amber-600" />
      <RowLabel icon={look.icon} label={look.label} />
      {item.summary && <Mono className="min-w-0 flex-1 truncate text-sm text-slate-700 dark:text-slate-200!">{item.summary}</Mono>}
      <Chip tone="amber">needs approval</Chip>
    </div>
    <div className="space-y-2 border-t border-amber-200/80 px-3 py-2.5 text-sm dark:border-amber-400/20!">
      {(item.approval.runStart || item.tool === "run_start") && (
        // S39 CR8: the confirmation names what is being started. With the
        // structured input (a transcript, a fixture) it names the blueprint
        // and the record; without it the card says the truth it has — the
        // daemon's own body follows below either way.
        <p data-testid="agent-chat-run-start-heading" className="font-semibold text-slate-900 dark:text-slate-50!">
          {item.approval.runStart
            ? `Start blueprint ${item.approval.runStart.blueprint}${item.approval.runStart.goal ? ` on goal ${item.approval.runStart.goal}` : item.approval.runStart.task ? ` on task ${item.approval.runStart.task}` : ""}`
            : "Start a blueprint run"}
        </p>
      )}
      <p className="text-slate-800 dark:text-slate-100!">{item.approval.prompt}</p>
      {item.verdict?.reason && <p className="font-mono text-xs text-slate-500 dark:text-slate-400!">
        <span className="font-semibold">policy:</span> {item.verdict.reason}</p>}
      {item.detail && <DiffLines text={item.detail} maxLines={12} />}
    </div>
    <div className="flex flex-wrap items-center gap-2 border-t border-amber-200/80 px-3 py-2 dark:border-amber-400/20!">
      <span className="mr-auto text-xs text-slate-500 dark:text-slate-400!">
        {actions.onAnswer && !readOnly ? "The step is parked until you decide." : "Read-only here — answer from the loop screen."}
      </span>
      {actions.allowedAll && <span data-testid="agent-chat-allowed-all" className="rounded-md border border-red-300 bg-red-50 px-3 py-1 text-xs font-medium text-red-700 dark:border-red-400/40! dark:bg-red-400/10! dark:text-red-300!">Everything allowed in this loop</span>}
      {actions.onAllowAll && !actions.allowedAll && !readOnly && <button type="button" disabled={!can} onClick={actions.onAllowAll} data-testid="agent-chat-allow-all"
        className="rounded-md border border-amber-400 bg-white px-3 py-1 text-xs font-medium text-amber-800 transition-colors hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-50 dark:border-amber-400/50! dark:bg-transparent! dark:text-amber-300!">
        Allow everything in this loop…
      </button>}
      {options.map((option) => {
        const yes = POSITIVE_ANSWER.test(option);
        return <button key={option} type="button" disabled={!can} onClick={() => void answer(option)}
          data-testid={yes ? "agent-chat-approve" : "agent-chat-decline"}
          className={`rounded-md px-3 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${yes
            ? "bg-blue-600 text-white hover:bg-blue-700"
            : "border border-slate-300 text-slate-600 hover:bg-slate-100 dark:border-white/20! dark:text-slate-300! dark:hover:bg-white/5!"}`}>
          {option.charAt(0).toUpperCase() + option.slice(1)}
        </button>;
      })}
    </div>
    {error && <p role="alert" className="px-3 pb-2 text-xs text-red-700 dark:text-red-300!">{error}</p>}
  </div>;
}

export function FileChangeCard({ item, actions }: { item: Of<"tool"> & { file: FileChange }; actions: ChatActions }) {
  const { file } = item;
  return <div data-testid="agent-chat-file" className="my-2 rounded-xl border border-slate-200 bg-white p-3 shadow-sm dark:border-white/10! dark:bg-white/5!">
    <div className="flex items-center gap-2">
      <AppIcon name="edit" size={16} className="text-emerald-600" />
      <Mono className="font-semibold text-slate-800 dark:text-slate-100!">{item.tool} {file.path}</Mono>
      {(file.linesAdded ?? 0) > 0 && <Chip tone="green">+{file.linesAdded}</Chip>}
      {(file.linesRemoved ?? 0) > 0 && <Chip tone="red">−{file.linesRemoved}</Chip>}
      {item.status === "running" && <Spinner className="text-blue-500" />}
      {item.status !== "running" && item.status !== "done" && <ToolStatusMark item={item} />}
      {actions.onViewDiff && item.status === "done" &&
        <button type="button" onClick={() => actions.onViewDiff?.(file)} className="ml-auto inline-flex items-center gap-1 font-mono text-sm text-blue-700 hover:underline dark:text-blue-300!">
          view diff <CustomIcon icon="OpenApp" size="xs" />
        </button>}
    </div>
    {(item.result || file.bytes !== undefined) && <div className="mt-2 flex items-center justify-between rounded-lg bg-slate-50 px-3 py-2 font-mono text-sm text-slate-600 dark:bg-white/5! dark:text-slate-300!">
      <span className="truncate">{item.summary && item.summary !== file.path ? item.summary : item.result}</span>
      {file.bytes !== undefined && <span className="ml-3 shrink-0 text-xs text-slate-400">{file.bytes} bytes</span>}
    </div>}
  </div>;
}

/** Copies text, then says so for a moment. */
function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return <button type="button" data-testid="agent-chat-copy" onClick={() => {
    void navigator.clipboard?.writeText(text).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }).catch(() => undefined);
  }} className="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-xs text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-800 dark:text-slate-400! dark:hover:bg-white/10! dark:hover:text-slate-100!">
    <AppIcon name={copied ? "check" : "copy"} size={12} />{copied ? "Copied" : "Copy"}
  </button>;
}

const RUN_DOT: Record<string, string> = {
  running: "bg-blue-500 animate-pulse", done: "bg-emerald-500", error: "bg-red-500",
  denied: "bg-slate-400", skipped: "bg-slate-300", awaiting: "bg-amber-500",
};

/** A call that never ran printed nothing — say why, once (the strip has the detail). */
const NOT_RUN: Partial<Record<string, (item: Of<"tool">) => string>> = {
  denied: (item) => item.approval?.answer !== undefined ? "not run — you declined it" : "not run — refused by policy",
  skipped: () => "not run — you steered before it ran",
};

/**
 * A command and what it printed, the terminal way: the command (wrapped,
 * copyable) on top, a rule, then the output in a hover-scrolling box.
 */
function BashBox({ item }: { item: Of<"tool"> }) {
  const command = item.command ?? item.summary ?? "";
  const output = item.output ?? item.detail;
  return <DetailBox testId="agent-chat-bash">
    <DecisionStrip item={item} />
    <div className="flex items-start gap-2 px-3 py-2.5">
      <span className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${RUN_DOT[item.status] ?? "bg-slate-400"}`} aria-hidden />
      <pre data-testid="agent-chat-bash-command" className="min-w-0 flex-1 whitespace-pre-wrap break-words font-mono text-[13px] leading-relaxed text-slate-800 dark:text-slate-100!">{command}</pre>
      {command && <CopyButton text={command} />}
    </div>
    <div className="border-t border-slate-200 bg-slate-50/70 dark:border-white/10! dark:bg-white/[0.03]!">
      {item.status === "running" ? <p className="px-3 py-2.5 font-mono text-slate-400">running…</p> :
        output ? <HoverScroll maxHeight="max-h-64">
          <pre data-testid="agent-chat-bash-output" className="whitespace-pre-wrap break-words px-3 py-2.5 font-mono text-[12.5px] leading-relaxed text-slate-700 dark:text-slate-300!">{output}</pre>
        </HoverScroll> :
        <p className="px-3 py-2.5 font-mono text-slate-500">{NOT_RUN[item.status] ? NOT_RUN[item.status]!(item) : item.result ?? "no output"}</p>}
      {item.outputTruncated && <p className="border-t border-slate-200 px-3 py-1.5 text-[11px] text-slate-400 dark:border-white/10!">output cut on the stream — the transcript has all of it</p>}
    </div>
  </DetailBox>;
}

const VERDICT_LOOK = {
  allowed: { label: "Policy allowed", tone: "text-emerald-700 bg-emerald-50 border-emerald-200 dark:text-emerald-300! dark:bg-emerald-400/10! dark:border-emerald-400/30!" },
  denied: { label: "Policy denied", tone: "text-red-700 bg-red-50 border-red-200 dark:text-red-300! dark:bg-red-400/10! dark:border-red-400/30!" },
  asked: { label: "Policy asked you", tone: "text-amber-700 bg-amber-50 border-amber-200 dark:text-amber-300! dark:bg-amber-400/10! dark:border-amber-400/30!" },
} as const;

function DecisionBadge({ icon, label, tone }: { icon: "shield" | "user"; label: string; tone: string }) {
  return <span className={`inline-flex shrink-0 items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] font-semibold ${tone}`}>
    <AppIcon name={icon} size={12} />{label}
  </span>;
}

/**
 * Who decided this call, and on what grounds — the top of every expanded box.
 * A person's answer first (it is what released the call), then the policy
 * verdict with its rule pulled forward and the rest of the sentence as quiet
 * chips. Rendered from the verdict sentence verbatim (policyText.ts), so
 * nothing the gate said is reworded.
 */
/**
 * What the chip on a call says about who let it run. A person's answer is "approved by you"; a standing
 * pre-approval (full access, or this tool pre-approved for the loop) is "approved by you, auto" because the
 * person set it up but did not answer this call; a client acting for no one says so. A rule that allowed the call
 * outright, and a declined call, get no approval chip.
 */
export function approvalLabel(item: Of<"tool">): string | undefined {
  const decided = item.approval?.answer;
  if (decided !== undefined && NEGATIVE_ANSWER.test(decided)) return undefined;
  if (item.approvedBy === "auto") return "approved by you, auto";
  if (item.approvedBy === "client") return "approved by a client";
  if (item.approvedBy === "person" || decided !== undefined) return "approved by you";
  return undefined;
}

function DecisionStrip({ item }: { item: Of<"tool"> }) {
  const decided = item.approval?.answer;
  const verdict = item.verdict;
  if (!item.approval && !verdict) return null;
  const policy = parsePolicySentence(verdict?.reason);
  const declined = decided !== undefined && NEGATIVE_ANSWER.test(decided);
  return <div data-testid="agent-chat-decisions" className="space-y-1.5 border-b border-slate-200 bg-slate-50/80 px-3 py-2 dark:border-white/10! dark:bg-white/[0.03]!">
    {item.approval && <div className="flex items-start gap-2" data-testid="agent-chat-decision-person">
      <DecisionBadge icon="user"
        label={decided === undefined ? "Waiting for you" : declined ? "You declined" : "You approved"}
        tone={decided === undefined ? VERDICT_LOOK.asked.tone : declined
          ? "text-slate-700 bg-white border-slate-300 dark:text-slate-200! dark:bg-white/5! dark:border-white/20!"
          : "text-blue-700 bg-blue-50 border-blue-200 dark:text-blue-300! dark:bg-blue-400/10! dark:border-blue-400/30!"} />
      <span className="min-w-0 pt-px text-xs leading-relaxed text-slate-500 dark:text-slate-400!">{item.approval.prompt}</span>
    </div>}
    {verdict && <div className="flex flex-wrap items-center gap-1.5" data-testid="agent-chat-decision-policy">
      <DecisionBadge icon="shield" label={VERDICT_LOOK[verdict.verdict].label} tone={VERDICT_LOOK[verdict.verdict].tone} />
      {policy.rule && <span className="inline-flex items-center gap-1 rounded-md bg-white px-1.5 py-0.5 font-mono text-[11px] text-slate-700 ring-1 ring-slate-200 dark:bg-white/5! dark:text-slate-200! dark:ring-white/10!">
        <span className="text-slate-400">rule</span>{policy.rule}</span>}
      {policy.details.map((d) => <span key={d} className="font-mono text-[11px] text-slate-500 dark:text-slate-400!">{d}</span>)
        .flatMap((el, i) => i === 0 ? [el] : [<span key={`sep${i}`} className="text-slate-300 dark:text-slate-600!">·</span>, el])}
    </div>}
  </div>;
}

export function ToolRow({ item, actions, liveClock, readOnly }: { item: Of<"tool">; actions: ChatActions; liveClock?: boolean; readOnly?: boolean }) {
  const [open, setOpen] = useState(false);
  // Waiting on a person: the call IS the approval card until it is answered.
  if (item.approval && item.approval.answer === undefined && item.status === "awaiting") {
    return <ApprovalCard item={item as Of<"tool"> & { approval: ChatQuestion }} actions={actions} readOnly={readOnly} />;
  }
  if (item.file && (item.tool === "write_file" || item.tool === "edit")) return <FileChangeCard item={item as Of<"tool"> & { file: FileChange }} actions={actions} />;
  const look = toolLook(item.tool);
  const bash = item.tool === "bash";
  const expandable = bash || Boolean(item.detail || item.result || item.verdict?.reason || item.approval);
  const muted = item.status === "skipped" ? "line-through opacity-60" : "";
  const decided = item.approval?.answer;
  return <div data-testid="agent-chat-tool" data-status={item.status} className={decided !== undefined ? "agent-chat-settle" : ""}>
    <button type="button" disabled={!expandable} aria-expanded={expandable ? open : undefined} onClick={() => setOpen((v) => !v)}
      className={`agent-chat-row flex w-full min-w-0 items-center gap-2 rounded-md py-1 text-left ${expandable ? "cursor-pointer" : "cursor-default"}`}>
      <RowLabel icon={look.icon} label={look.label} expandable={expandable} open={open} />
      {item.summary && <><span className="text-slate-400">·</span>
        <Mono className={`min-w-0 flex-1 truncate text-sm text-slate-600 dark:text-slate-300! ${muted}`}>{item.summary}</Mono></>}
      {!item.summary && <span className="flex-1" />}
      {approvalLabel(item) && <Chip tone="blue">{approvalLabel(item)}</Chip>}
      {item.verdict && item.verdict.verdict !== "allowed" && <CustomIcon icon="Verified" size="xs" alt={`policy ${item.verdict.verdict}`} className="text-amber-600" />}
      <ToolStatusMark item={item} liveClock={liveClock} />
    </button>
    <Expand open={open}>
      {bash ? <BashBox item={item} /> :
        <DetailBox testId="agent-chat-tool-body">
          <DecisionStrip item={item} />
          {item.result && <p className="px-3 py-2 font-mono text-slate-700 dark:text-slate-200!">{item.result}</p>}
          {item.detail && item.detail !== item.result && <div className="border-t border-slate-200 dark:border-white/10!"><HoverScroll maxHeight="max-h-64"><div className="p-2"><DiffLines text={item.detail} /></div></HoverScroll></div>}
        </DetailBox>}
    </Expand>
  </div>;
}

export function ActivityRow({ item }: { item: Of<"activity"> }) {
  const now = useNow(!item.done);
  const since = Date.parse(item.since);
  const elapsed = Number.isFinite(since) ? now - since : undefined;
  return <div data-testid="agent-chat-activity" className="flex items-center gap-2 py-1 font-mono text-sm text-violet-600 dark:text-violet-300!">
    <CustomIcon icon="Live" size="xs" />
    <span>{item.label}{elapsed !== undefined ? ` for ${formatDuration(elapsed)}` : ""}{item.done ? "" : "…"}</span>
  </div>;
}

/**
 * S66: the catalog rode this step's context. Compact, mono, one line —
 * the operator's question is "was MY skill offered, and which ones", and
 * the tooltip carries the daemon's own sentence verbatim (item.marker),
 * because live and replay must print one renderer's words.
 */
export function InjectionRow({ item }: { item: Of<"injection"> }) {
  return <div data-testid="agent-chat-injection" title={item.marker}
    className="flex flex-wrap items-center gap-2 py-1 font-mono text-sm text-sky-700 dark:text-sky-300!">
    <CustomIcon icon="Orchestrator" size="xs" />
    <span className="text-slate-500 dark:text-slate-400!">skills injected</span>
    <Chip>{item.count}</Chip>
    <span className="min-w-0 flex-1 truncate">{item.names.join(", ")}{item.truncated ? " (+more)" : ""}</span>
  </div>;
}

/**
 * S70 MB6: one row of the agents' own exchange, rendered as the exchange
 * it is — WHO spoke, WHOM to, the row's own words verbatim, and where the
 * row stands (posted → a boundary delivered it → a reply settled it). The
 * thread chip names the conversation by its header row (MB3: a thread IS
 * its first ask's id), so a reader who sees one row can find the rest by
 * that number — `agent.messages {loopId, thread}` is the whole door.
 * Nobody rewrites the words (MB5): this renderer adds no adjectives.
 */
export function ExchangeRow({ item }: { item: Of<"exchange"> }) {
  return <div data-testid="agent-chat-exchange"
    className="flex flex-wrap items-center gap-2 py-1 font-mono text-sm text-indigo-700 dark:text-indigo-300!">
    <CustomIcon icon="Live" size="xs" />
    <span className="min-w-0">
      <span className="font-semibold">{item.from}</span>
      <span className="mx-1 text-slate-400">→</span>
      <span className="font-semibold">{item.to}</span>
      <span className="mx-1 text-slate-400">·</span>
      <span className="text-slate-600 dark:text-slate-300!">{item.text}</span>
    </span>
    <span className="ml-auto flex items-center gap-1.5">
      {item.state === "posted" && <Chip tone="slate">{item.expectReply ? "posted · awaiting reply" : "posted"}</Chip>}
      {item.state === "delivered" && <Chip tone="blue">delivered</Chip>}
      {item.state === "answered" && <Chip tone="green">answered</Chip>}
      <Chip tone="slate">thread {item.thread}</Chip>
    </span>
  </div>;
}
