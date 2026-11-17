import { useRef, useState, type ReactNode, type Ref, type RefObject } from "react";
import { CustomIcon, IconButton, Panel, Textarea } from "../kit";
import { useAppSettings } from "../../lib/appSettings";
import { ChatPanelDock, type ChatPanelsProps } from "./ChatPanels";
import { ComposerAddMenu, type ChatAddAction } from "./ComposerAddMenu";
import { ComposerAttachments, useComposerAttachments, type ChatAttachment } from "./ComposerAttachments";
import "./composerAttachments.css";

export interface ComposerDraft {
  text: string;
  attachments: readonly ChatAttachment[];
}

export interface ChatSpecialKeyRequest {
  key: string;
  query: string;
  /** Trigger token range in the current composer text. */
  start: number;
  end: number;
}

export interface ComposerControls {
  draft: ComposerDraft;
  busy: boolean;
  disabled: boolean;
  pendingAction: string | null;
  /** Share the submit lock and error display with custom asynchronous controls. */
  runAction: (name: string, action: () => void | Promise<void>) => Promise<void>;
}

export type ComposerSlot = ReactNode | ((controls: ComposerControls) => ReactNode);

/** One selected-context chip (S67: the chosen skills ride every send).
 * Labels and meaning are the host's; the composer only renders the row and
 * hands back the remove. `title` carries the daemon's sentence verbatim —
 * the same words the operator will find woven into what the model reads. */
export interface ComposerChip {
  id: string;
  label: string;
  title?: string;
  onRemove?: () => void;
}

export interface MessageComposerProps extends ChatPanelsProps {
  value?: ComposerDraft;
  defaultValue?: ComposerDraft;
  onChange?: (draft: ComposerDraft) => void;
  onSubmit: (draft: ComposerDraft) => void | Promise<void>;
  /** Validation gate for submission; leaves text and selectors editable. */
  submitDisabled?: boolean;
  /** Allows adapters to submit settings changes without text or attachments. */
  allowEmptySubmit?: boolean;
  submitLabel?: string;
  submittingLabel?: string;
  /** Show the send action as an icon button only; the label becomes its tooltip and accessible name. */
  submitIconOnly?: boolean;
  disabled?: boolean;
  disabledReason?: string;
  placeholder?: string;
  inputLabel?: string;
  inputRef?: Ref<HTMLTextAreaElement>;
  rows?: number;
  selectors?: ComposerSlot;
  actions?: ComposerSlot;
  footer?: ComposerSlot;
  /** Selected-context chips (S67 skills): a row above the textarea, each
   * removable by the host. Empty/absent renders nothing. */
  chips?: readonly ComposerChip[];
  variant?: "standalone" | "chat";
  className?: string;
  onAddAction?: (action: ChatAddAction) => void;
  onAttachmentsChange?: (attachments: readonly ChatAttachment[]) => void;
  dropTargetRef?: RefObject<HTMLElement | null>;
  /** Triggers at whitespace-delimited token boundaries. The longest matching key wins. */
  specialKeys?: readonly string[];
  /** Host opens/filters a picker; null dismisses it. No data lookup happens here. */
  onSpecialKeyChange?: (request: ChatSpecialKeyRequest | null) => void;
  "data-testid"?: string;
}

const defaultSpecialKeys = ["/", "//", "@", "$"];
const emptyDraft = (): ComposerDraft => ({ text: "", attachments: [] });
const renderSlot = (slot: ComposerSlot, controls: ComposerControls) => typeof slot === "function" ? slot(controls) : slot;

/** Shared input surface. Hosts own selection meaning, uploads, and submit effects. */
export function MessageComposer({ value, defaultValue, onChange, onSubmit, submitDisabled, allowEmptySubmit,
  submitLabel = "Send", submittingLabel = "Sending…", submitIconOnly = false, disabled, disabledReason, placeholder = "Message…", inputLabel = "message", inputRef, rows = 2,
  selectors, actions, footer, chips, variant = "standalone", className = "", onAddAction, onAttachmentsChange, dropTargetRef,
  specialKeys = defaultSpecialKeys, onSpecialKeyChange, panels, taskList, changes, picker, onPickerDismiss, "data-testid": testId = "message-composer" }: MessageComposerProps) {
  const [internalDraft, setInternalDraft] = useState<ComposerDraft>(() => defaultValue ?? emptyDraft());
  const draft = value ?? internalDraft;
  const current = useRef(draft);
  current.current = draft;
  const updateDraft = (next: ComposerDraft) => {
    const previous = current.current;
    current.current = next;
    if (value === undefined) setInternalDraft(next);
    onChange?.(next);
    if (previous.attachments !== next.attachments) onAttachmentsChange?.(next.attachments);
  };
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const locked = useRef(false);
  const [error, setError] = useState("");
  const composerRef = useRef<HTMLDivElement>(null);
  const busy = pendingAction !== null;
  const blocked = Boolean(disabled || busy);
  const files = useComposerAttachments({ targetRef: dropTargetRef ?? composerRef, attachments: draft.attachments, disabled: blocked,
    onChange: (attachments) => updateDraft({ ...current.current, attachments }), onError: setError });
  const activeSpecialKey = useRef<ChatSpecialKeyRequest | null>(null);
  const dismissedToken = useRef<string | null>(null);
  const updateSpecialKey = (text: string, cursor: number) => {
    if (!onSpecialKeyChange) return;
    const token = text.slice(0, cursor).match(/(?:^|\s)(\S+)$/)?.[1];
    const start = token ? cursor - token.length : cursor;
    const key = token ? specialKeys.reduce<string | null>((match, candidate) =>
      candidate.length > (match?.length ?? 0) && token.startsWith(candidate) ? candidate : match, null) : null;
    const identity = key ? `${start}:${key}` : null;
    if (!token || !key || text[cursor] && !/\s/.test(text[cursor]!)) {
      dismissedToken.current = null;
      if (activeSpecialKey.current) onSpecialKeyChange(null);
      activeSpecialKey.current = null;
      return;
    }
    if (identity === dismissedToken.current) return;
    dismissedToken.current = null;
    const request = { key, query: token.slice(key.length), start, end: cursor };
    activeSpecialKey.current = request;
    onSpecialKeyChange(request);
  };
  const runAction = async (name: string, action: () => void | Promise<void>) => {
    if (disabled || locked.current) return;
    locked.current = true;
    setPendingAction(name); setError("");
    try { await action(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { locked.current = false; setPendingAction(null); }
  };
  const { controlTone } = useAppSettings();
  const canSubmit = !blocked && !submitDisabled && Boolean(draft.text.trim() || draft.attachments.length || allowEmptySubmit);
  const submit = async () => {
    if (!canSubmit) return;
    const snapshot = current.current;
    await runAction("submit", async () => {
      await onSubmit({ text: snapshot.text.trim(), attachments: snapshot.attachments });
      // A host may load a new draft while the old submission is pending.
      if (current.current.text === snapshot.text && current.current.attachments === snapshot.attachments) updateDraft(emptyDraft());
      activeSpecialKey.current = null;
      dismissedToken.current = null;
      onSpecialKeyChange?.(null);
    });
  };
  const controls: ComposerControls = { draft, busy, disabled: blocked, pendingAction, runAction };
  return <ChatPanelDock panels={panels} taskList={taskList} changes={changes} picker={picker} onPickerDismiss={onPickerDismiss} layout={variant}>
    <Panel ref={composerRef} data-testid={testId} variant="simple" padding="xs"
      className={`chat-composer ${variant === "chat" ? "border-t" : ""} ${className}`}
      onPaste={(event) => {
        if (blocked) return;
        const images = Array.from(event.clipboardData.items).filter((item) => item.kind === "file" && item.type.startsWith("image/"))
          .map((item) => item.getAsFile()).filter((file): file is File => file !== null);
        if (images.length) { event.preventDefault(); files.addFiles(images); }
      }}>
      {files.dragging && <div className="chat-composer-drop" role="status">Drop files or images to attach</div>}
      {chips && chips.length > 0 && <div data-testid={`${testId}-chips`} className="mb-2 flex flex-wrap gap-1.5">
        {chips.map((chip) => <span key={chip.id} data-testid={`${testId}-chip-${chip.id}`} title={chip.title}
          className="aeon-accent-panel aeon-accent-wash aeon-accent-text inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-xs">
          {chip.label}
          {chip.onRemove && <button type="button" aria-label={`Remove ${chip.label}`} title={`Remove ${chip.label}`}
            data-testid={`${testId}-chip-${chip.id}-remove`} disabled={blocked} onClick={chip.onRemove}
            className="cursor-pointer opacity-60 hover:opacity-100 disabled:cursor-not-allowed disabled:opacity-50">×</button>}
        </span>)}
      </div>}
      {draft.attachments.length > 0 && <ComposerAttachments attachments={draft.attachments} onRemove={files.remove} disabled={blocked} />}
      <Textarea resize="none" ref={inputRef} value={draft.text} rows={rows} disabled={blocked} aria-label={inputLabel}
        placeholder={disabled && disabledReason ? disabledReason : placeholder}
        onChange={(event) => {
          updateDraft({ ...current.current, text: event.target.value });
          if (!(event.nativeEvent as InputEvent).isComposing) updateSpecialKey(event.target.value, event.target.selectionStart);
        }} onCompositionEnd={(event) => updateSpecialKey(event.currentTarget.value, event.currentTarget.selectionStart)}
        onClick={(event) => updateSpecialKey(event.currentTarget.value, event.currentTarget.selectionStart)}
        onKeyUp={(event) => { if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) updateSpecialKey(event.currentTarget.value, event.currentTarget.selectionStart); }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === "Escape") {
            const active = activeSpecialKey.current;
            if (active) dismissedToken.current = `${active.start}:${active.key}`;
            activeSpecialKey.current = null;
            onSpecialKeyChange?.(null);
            return;
          }
          if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void submit(); }
        }}
        className="aeon-accent-wash w-full resize-none rounded-md border border-slate-200 px-3 py-2.5 text-[15px] text-slate-900 outline-none placeholder:text-slate-400 focus:border-[var(--aeon-accent-text)] focus:ring-2 focus:ring-[color-mix(in_srgb,var(--aeon-accent-text)_25%,transparent)] disabled:opacity-60 dark:border-white/10! dark:text-slate-100!" />
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <ComposerAddMenu disabled={blocked} onFiles={files.addFiles} onAction={onAddAction} />
        {renderSlot(selectors, controls)}
        <div className="ml-auto flex gap-2">
          {renderSlot(actions, controls)}
          {submitIconOnly
            ? <IconButton icon="Send" size="md" color={controlTone} srLabel={submitLabel} tooltip={submitLabel} data-testid="composer-submit"
                onClick={() => void submit()} disabled={!canSubmit} loading={pendingAction === "submit"} />
            : <button type="button" onClick={() => void submit()} disabled={!canSubmit}
              className="aeon-accent-button inline-flex items-center gap-1.5 rounded-md px-5 py-1.5 text-sm font-semibold hover:opacity-90 disabled:opacity-50">
              <CustomIcon icon="Send" size="xs" />{pendingAction === "submit" ? submittingLabel : submitLabel}
            </button>}
        </div>
      </div>
      {renderSlot(footer, controls)}
      {error && <p role="alert" className="mt-1 text-xs text-red-700">{error}</p>}
    </Panel>
  </ChatPanelDock>;
}
