import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AppIcon } from "../icons";
import { CustomIcon } from "../kit";
import { agentColor } from "./agentStyle";
import type { ChatAgent, ComposerOption, SteerRequest } from "./types";
import { MessageComposer, type MessageComposerProps } from "./MessageComposer";
import type { ChatAttachment } from "./ComposerAttachments";
export type { ChatSpecialKeyRequest } from "./MessageComposer";

/** Chat-specific controls and callbacks over the shared MessageComposer surface. */
export interface ChatComposerProps extends Omit<MessageComposerProps, "onSubmit" | "allowEmptySubmit"> {
  /** Agents a message can go to. One, or a `fixedTarget`, hides the picker. */
  targets?: ComposerOption[];
  /** The zoomed agent view: messages go here and the "To" picker is hidden. */
  fixedTarget?: string;
  defaultTarget?: string;
  routes?: ComposerOption[];
  efforts?: ComposerOption[];
  /** What the target is using now. A steer carries route/effort only when they differ. */
  currentRoute?: string;
  currentEffort?: string;
  /**
   * The loop's own model choices, set from here at any time: the thinking switch (only for a route that has one) and
   * the effort. They are the loop's, so they apply from its next model step; while an agent runs, an effort change
   * is also a steer for that agent. `steerable` says whether any listed agent is running right now.
   */
  loopOptions?: { thinking: string; effort: string; onChange: (o: { thinking?: string; reasoningEffort?: string }) => void | Promise<void>; disabled?: boolean };
  steerable?: boolean;
  /** The button beside the permission menu that shows or hides the Changes panel. No prop, no button. */
  changesToggle?: { count: number; on: boolean; onToggle: () => void };
  onSteer: (request: SteerRequest) => void | Promise<void>;
  /**
   * `message` is the idle loop's composer (S39 CR3/D3): the person is talking
   * to the LOOP, not steering a running agent — no To: picker, no Stop, one
   * Send. Default `steer`; every existing caller renders exactly what it did
   * before (the message branch only ADDS code paths).
   */
  mode?: "steer" | "message";
  /** The message-mode send. Absent means the host has no loop to message. */
  onMessage?: (text: string, route?: string, attachments?: readonly ChatAttachment[]) => void | Promise<void>;
  /** No callback, no Stop button. */
  onStop?: (target: string) => void | Promise<void>;
  /**
   * The "Ask" / "Auto-approve" chip (plai-client-app's AutoApproveToggle).
   * OFF = every call policy asks about parks for the person's Approve/Decline;
   * ON = the host answers each one itself. The host reads it live, so a flip
   * mid-run applies from the next parked call. `lockedReason` disables it
   * and says why (plai locks it when destructive tools are in play).
   */
  autoApprove?: { on: boolean; onChange: (on: boolean) => void; lockedReason?: string };
  /**
   * The loop's permission mode, as a shield menu beside Stop: Ask (every asked call waits for the person) or
   * Approve all (the host lets them run). Choosing Approve all is the host's to confirm, so the menu only reports
   * the choice; it never assumes the mode changed. No prop, no control.
   */
  access?: { mode: "ask" | "tools" | "full"; label: string; onAsk: () => void; onFull: () => void; disabled?: boolean };
  agents?: Record<string, ChatAgent>;
}

function PillSelect({ icon, prefix, value, options, onChange, label, dot, disabled }: {
  icon?: ReactNode; prefix?: string; value: string; options: ComposerOption[]; onChange: (v: string) => void; label: string; dot?: string; disabled?: boolean;
}) {
  return <label className="relative inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-slate-50 px-2.5 py-1.5 font-mono text-sm text-slate-700 hover:bg-white dark:border-white/10! dark:bg-white/5! dark:text-slate-200!">
    {dot && <span className="h-2 w-2 rounded-full" style={{ background: dot }} />}
    {icon}
    <span className="whitespace-nowrap">{prefix}{options.find((o) => o.value === value)?.label ?? value}</span>
    <AppIcon name="chevron-hover" size={12} className="text-slate-400" />
    <select disabled={disabled} aria-label={label} value={value} onChange={(e) => onChange(e.target.value)} className="absolute inset-0 cursor-pointer opacity-0">
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
  </label>;
}

function AutoApproveChip({ on, onChange, lockedReason }: NonNullable<ChatComposerProps["autoApprove"]>) {
  const title = lockedReason ?? (on
    ? "Auto-approve: calls policy asks about run without waiting for you"
    : "Ask: every call policy asks about waits for your Approve / Decline");
  return <button type="button" aria-pressed={on} aria-label={title} title={title} disabled={Boolean(lockedReason)}
    onClick={() => onChange(!on)} data-testid="agent-chat-auto-approve"
    className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 font-mono text-sm transition-colors duration-200 disabled:cursor-not-allowed disabled:opacity-50 ${on
      ? "border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-500/40! dark:bg-blue-500/10! dark:text-blue-300!"
      : "border-slate-200 bg-slate-50 text-slate-700 hover:bg-white dark:border-white/10! dark:bg-white/5! dark:text-slate-200!"}`}>
    <CustomIcon icon={on ? "Check" : "Verified"} size="xs" className={on ? "text-blue-500" : "text-slate-400"} />
    {on ? "Auto-approve" : "Ask"}
  </button>;
}

function AccessMenu({ mode, label, onAsk, onFull, disabled }: NonNullable<ChatComposerProps["access"]>) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => { if (!box.current?.contains(e.target as Node)) setOpen(false); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", away); document.addEventListener("keydown", key);
    return () => { document.removeEventListener("mousedown", away); document.removeEventListener("keydown", key); };
  }, [open]);
  const full = mode === "full";
  const item = (on: boolean, text: string, hint: string, pick: () => void, testid: string) =>
    <button type="button" role="menuitemradio" aria-checked={on} data-testid={testid} onClick={() => { setOpen(false); if (!on) pick(); }}
      className="flex w-full items-start gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-slate-100 dark:hover:bg-white/10!">
      <span className="mt-0.5 w-3 shrink-0">{on ? "✓" : ""}</span>
      <span><span className="block font-medium">{text}</span><span className="block text-xs opacity-70">{hint}</span></span>
    </button>;
  return <span ref={box} className="relative inline-flex">
    <button type="button" aria-haspopup="menu" aria-expanded={open} disabled={disabled} onClick={() => setOpen((v) => !v)} data-testid="agent-chat-access"
      title="What this loop's agents may do without asking"
      className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 font-mono text-sm transition-colors duration-200 disabled:cursor-not-allowed disabled:opacity-50 ${full
        ? "border-red-300 bg-red-50 text-red-700 dark:border-red-400/40! dark:bg-red-400/10! dark:text-red-300!"
        : mode === "tools" ? "border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-400/40! dark:bg-amber-400/10! dark:text-amber-300!"
        : "border-slate-200 bg-slate-50 text-slate-700 hover:bg-white dark:border-white/10! dark:bg-white/5! dark:text-slate-200!"}`}>
      <CustomIcon icon="Shield" size="xs" />{label}
      <AppIcon name="chevron-hover" size={12} className="opacity-60" />
    </button>
    {open && <div role="menu" data-testid="agent-chat-access-menu"
      className="absolute bottom-full left-0 z-30 mb-2 w-64 rounded-xl border border-slate-200 bg-white p-1 text-slate-800 shadow-lg dark:border-white/15! dark:bg-slate-900! dark:text-slate-100!">
      {item(mode === "ask", "Ask", "Shell, file writes and the network wait for you.", onAsk, "agent-chat-access-ask")}
      {item(full, "Approve all", "This loop's agents run without asking. Change it back any time.", onFull, "agent-chat-access-full")}
    </div>}
  </span>;
}

export function ChatComposer({ targets = [], fixedTarget, defaultTarget, routes = [], efforts = [], currentRoute, currentEffort, loopOptions, steerable = true, changesToggle,
  onSteer, mode = "steer", onMessage, onStop, autoApprove, access, agents, selectors, actions, footer,
  placeholder, submitDisabled, submitLabel, submittingLabel, ...shared }: ChatComposerProps) {
  const [target, setTarget] = useState(fixedTarget ?? defaultTarget ?? targets[0]?.value ?? "");
  // "" means "whatever the agent is on" — shown as `default`, never sent.
  const [route, setRoute] = useState(currentRoute ?? "");
  const [effort, setEffort] = useState(currentEffort ?? "");
  useEffect(() => { if (fixedTarget) setTarget(fixedTarget); }, [fixedTarget]);
  // A target that disappeared (its step finished) falls back to the first one still running.
  useEffect(() => {
    if (!fixedTarget && targets.length > 0 && !targets.some((item) => item.value === target)) setTarget(targets[0]?.value ?? "");
  }, [targets, target, fixedTarget]);
  useEffect(() => { setRoute(currentRoute ?? ""); }, [currentRoute, target]);
  useEffect(() => { setEffort(currentEffort ?? loopOptions?.effort ?? ""); }, [currentEffort, loopOptions?.effort, target]);
  const declaredEfforts = routes.find((option) => option.value === (route || currentRoute))?.reasoningEfforts;
  const allowedEfforts = declaredEfforts != null ? declaredEfforts.map((value) => ({ value, label: value })) : efforts;
  const selectedEffort = declaredEfforts != null && !declaredEfforts.includes(effort) ? "" : effort;
  const activeRoute = routes.find((option) => option.value === (route || currentRoute));
  const canThink = Boolean(loopOptions) && activeRoute?.thinking === true;
  const thinkingValue = loopOptions?.thinking ?? "";
  // A loop-level effort change (nobody to steer, or the idle loop's composer) is applied at once; otherwise it rides
  // the next steer as before.
  const effortIsLoopLevel = Boolean(loopOptions) && (!steerable || mode === "message");
  const routeChanged = route !== "" && route !== (currentRoute ?? "");
  const effortChanged = selectedEffort !== "" && selectedEffort !== (currentEffort ?? "");
  const withDefault = (options: ComposerOption[], current?: string) =>
    current && options.some((option) => option.value === current) ? options : [{ value: "", label: current ?? "default" }, ...options];
  // Message mode needs nobody to address: a person typing is the whole send.
  return <MessageComposer {...shared} variant={shared.variant ?? "chat"} data-testid="agent-chat-composer"
    placeholder={placeholder ?? (mode === "message" ? "Message the loop…" : "Message an agent…")}
    submitDisabled={submitDisabled || (mode !== "message" && !target)}
    allowEmptySubmit={mode !== "message" && (routeChanged || effortChanged)}
    submitLabel={submitLabel ?? (mode === "message" ? "Send" : "Steer")} submittingLabel={submittingLabel}
    onSubmit={async ({ text, attachments }) => {
      if (mode === "message") {
        if (attachments.length) await onMessage?.(text, routeChanged ? route : undefined, attachments);
        else await onMessage?.(text, routeChanged ? route : undefined);
      } else await onSteer({ target, text, ...(routeChanged ? { route } : {}), ...(effortChanged ? { reasoningEffort: selectedEffort } : {}),
        ...(attachments.length ? { attachments } : {}) });
    }}
    selectors={(controls) => <>
      {mode !== "message" && !fixedTarget && targets.length > 0 && <PillSelect label="send to" prefix="To: " value={target}
        options={targets} onChange={setTarget} disabled={loopOptions ? loopOptions.disabled : controls.disabled} dot={target ? agentColor(target, agents) : undefined} />}
      {routes.length > 0 && <PillSelect label="provider" icon={<CustomIcon icon="Idea" size="xs" className="aeon-accent-text" />}
        value={route} options={withDefault(routes, currentRoute)} disabled={controls.disabled} onChange={(value) => { setRoute(value); setEffort(""); }} />}
      {canThink && <PillSelect label="thinking" icon={<CustomIcon icon="Idea" size="xs" />} prefix="Thinking: " value={thinkingValue}
        options={[{ value: "", label: "default" }, { value: "on", label: "on" }, { value: "off", label: "off" }]}
        onChange={(v) => void loopOptions?.onChange({ thinking: v })} disabled={loopOptions?.disabled} />}
      {thinkingValue !== "off" && allowedEfforts.length > 0 && (mode !== "message" || loopOptions) && <PillSelect label="reasoning effort" icon={<CustomIcon icon="Dashboard" size="xs" />}
        prefix="Effort: " value={selectedEffort} options={withDefault(allowedEfforts, declaredEfforts != null ? undefined : (currentEffort ?? loopOptions?.effort))}
        onChange={(v) => { setEffort(v); if (effortIsLoopLevel) void loopOptions?.onChange({ reasoningEffort: v }); }}
        disabled={effortIsLoopLevel ? loopOptions?.disabled : controls.disabled} />}
      {autoApprove && <AutoApproveChip {...autoApprove} />}
      {typeof selectors === "function" ? selectors(controls) : selectors}
    </>}
    actions={(controls) => <>
      {changesToggle && changesToggle.count > 0 && <button type="button" aria-pressed={changesToggle.on} data-testid="agent-chat-changes-toggle" onClick={changesToggle.onToggle}
        title={changesToggle.on ? "Hide the changes panel" : "Show what the agents changed"}
        className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 font-mono text-sm transition-colors ${changesToggle.on
          ? "border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-400/40! dark:bg-blue-400/10! dark:text-blue-300!"
          : "border-slate-200 bg-slate-50 text-slate-700 hover:bg-white dark:border-white/10! dark:bg-white/5! dark:text-slate-200!"}`}>
        <AppIcon name="edit" size={14} />{changesToggle.count} {changesToggle.count === 1 ? "file" : "files"}
      </button>}
      {access && <AccessMenu {...access} disabled={access.disabled || controls.disabled} />}
      {onStop && mode !== "message" && <button type="button" onClick={() => void controls.runAction("stop", () => onStop(target))}
        disabled={controls.disabled || !target}
        className="inline-flex items-center gap-1.5 rounded-md border border-red-200 bg-red-50 px-4 py-1.5 text-sm font-medium text-red-600 hover:bg-red-100 disabled:opacity-50 dark:border-red-400/30! dark:bg-red-400/10! dark:text-red-300!">
        <AppIcon name="stop" size={14} strokeWidth={1.5} />{controls.pendingAction === "stop" ? "Stopping…" : "Stop"}
      </button>}
      {typeof actions === "function" ? actions(controls) : actions}
    </>}
    footer={(controls) => <>
      {mode !== "message" && (routeChanged || effortChanged) && <p className="mt-1 font-mono text-xs text-slate-500">applies from {target}'s next turn and sticks for the rest of its run</p>}
      {typeof footer === "function" ? footer(controls) : footer}
    </>} />;
}
