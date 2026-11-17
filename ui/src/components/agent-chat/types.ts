/**
 * The agent chat's presentation model.
 *
 * # Why a model of its own, and not the protocol's events
 *
 * The chat renders the same things from three sources: live `step.*` /
 * `loop.*` events, a saved transcript (`step.show`'s `messages`), and fixtures
 * in the test bench. Rendering any one of them directly would weld the
 * component to that source. Instead each source is ADAPTED into these items
 * (`fromEvents.ts` for the stream) and the component knows only this file —
 * which is what lets the loop workspace, the step screen and the bench share
 * one renderer.
 */

import type { ChatAttachment } from "./ComposerAttachments";
import type { AttachmentReceipt } from "../../lib/attachments";

export interface ChatAgent {
  /** Stable key — the step's `agent`, or "you" / "orchestrator". */
  id: string;
  /** What the header shows. Defaults to `id`. */
  name?: string;
  /** A short badge beside the name ("ast-vet", "lead-explorer"). */
  role?: string;
  /** Overrides the colour picked from `id`. Any CSS colour. */
  color?: string;
  /** The agent's own avatar. When present it replaces the coloured initial. */
  avatar?: { type: string; color: string; mood?: string };
}

/**
 * A tool call's lifecycle as the stream reports it. `awaiting` is a call the
 * policy gate asked a person about; `skipped` one a steer closed before it ran;
 * `denied` one policy refused. Each is a different truth, so each renders
 * differently — collapsing them into "error" would make a refusal and a crash
 * look alike.
 */
export type ToolStatus = "running" | "done" | "error" | "skipped" | "awaiting" | "denied";

export type PolicyVerdict = "allowed" | "denied" | "asked";

export interface FileChange {
  path: string;
  linesAdded?: number;
  linesRemoved?: number;
  bytes?: number;
}

export type QuestionKind = "clarify" | "gate" | "permission" | "free";

export interface ChatQuestion {
  /** The question row's id, when the daemon gave one — what `answer` needs. */
  id: string;
  prompt: string;
  kind: QuestionKind;
  /** Short label for the answered grid ("Target runtime"). */
  label?: string;
  options?: string[];
  answer?: string;
  /**
   * Set when the permission question is the agent's `run_start` (S39 CR8):
   * the card can then name the blueprint and the record it leans on instead
   * of leaving the person to read the daemon's sentence for it. The event
   * stream carries no structured tool input (StepEvent has Summary and a
   * bash-only Command — verified at runtime/types.go:409-434), so today this
   * is supplied by whoever holds the input (the transcript, a fixture), and
   * the card falls back to naming the tool without inventing a blueprint.
   */
  runStart?: { blueprint: string; goal?: string; task?: string };
}

interface Base {
  id: string;
  /** ISO time. Optional because a fixture or transcript may not have one. */
  at?: string;
}

interface AgentScoped extends Base {
  agent: string;
}

/** A cost as the daemon reports it: a state and the amount that state allows. */
export interface TurnCost { state: "known" | "partial" | "unknown"; usd?: number; usdAtLeast?: number; reason?: string }

export type ChatItem =
  /** A centred rule across the stream: the orchestrator's moves, policy, loop state. */
  | (Base & { kind: "divider"; tone: "orchestrator" | "policy" | "neutral" | "success" | "danger"; text: string; highlights?: string[]; /** The whole text when the row cuts it short: a click opens it. */ detail?: string })
  /**
   * S66: the daemon appended the operator's Agent Skills catalog to this
   * step's context — WHICH skills it offered is the point, so it is a row
   * of its own rather than a divider tone. Not agent-scoped: the catalog
   * belongs to the daemon, and every agent under it was offered the same.
   * `marker` is the daemon's ONE sentence for the injection; live and
   * replay render it verbatim because both read it off `step.started`.
   */
  | (Base & { kind: "injection"; count: number; names: string[]; truncated?: boolean; marker: string })
  | (AgentScoped & { kind: "text"; text: string; streaming?: boolean })
  | (AgentScoped & { kind: "thinking"; text: string; streaming?: boolean; durationMs?: number })
  | (AgentScoped & {
    kind: "tool";
    tool: string;
    toolUseId?: string;
    /** The daemon step the call ran in — what ties a call to a lane. */
    stepId?: number;
    summary?: string;
    status: ToolStatus;
    /** One-line outcome the stream carried. */
    result?: string;
    /** Longer output (transcript content, error text) shown when expanded. */
    detail?: string;
    durationMs?: number;
    /** A bash call's whole command (the summary may be cut). */
    command?: string;
    /** What the call printed — capped on the live stream, whole in a transcript. */
    output?: string;
    /** The stream cut `output`; the transcript has the rest. */
    outputTruncated?: boolean;
    file?: FileChange;
    verdict?: { verdict: PolicyVerdict; reason?: string };
    /**
     * The permission question parked on THIS call (policy said "ask"). The
     * call renders as the approval card itself — like plai-client-app's
     * ToolCallCard — rather than as a separate question, because what is
     * being approved is this exact call.
     */
    approval?: ChatQuestion;
    /**
     * Who let this call run when policy asked: `person` answered the question, `auto` is a standing pre-approval
     * for the loop (full access, or this tool), `policy` is a rule that allowed it outright. The chip reads it.
     */
    approvedBy?: "person" | "auto" | "policy" | "client";
  })
  /**
   * One model call, finished: what it spent and how long it took, drawn as a quiet line after the call's thinking,
   * text and tool calls. `cost` and `stepCost` are the daemon's cost objects, with their state (known, partial,
   * unknown) kept, so an unknown cost reads as unknown and never as $0.
   */
  | (AgentScoped & { kind: "turn"; iteration?: number; inputTokens?: number; outputTokens?: number; reasoningTokens?: number; wallMs?: number; cost?: TurnCost; stepCost?: TurnCost })
  | (AgentScoped & { kind: "question"; questions: ChatQuestion[] })
  | (Base & {
    kind: "gate";
    title: string;
    description?: string;
    stepLabel?: string;
    /** A unified diff or plain lines; `+`/`-` prefixes are coloured. */
    preview?: string;
    previewLines?: number;
    options: string[];
    state: "awaiting" | "answered";
    answer?: string;
  })
  | (Base & {
    kind: "steer";
    target: string;
    text?: string;
    route?: string;
    reasoningEffort?: string;
    state: "queued" | "delivered" | "cancelled";
    steerId?: number;
    /** The steer's attachment receipts (S69 AT8) — same chips, same rule. */
    attachments?: readonly AttachmentReceipt[];
  })
  /** A long-running "still working" marker. `since` drives the live timer. */
  | (AgentScoped & { kind: "activity"; label: string; since: string; done?: boolean })
  /**
   * A person-side message on the loop's thread (S39 CR1/CR6). The AGENT's
   * replies are `text` items from the stream — an agent-authored
   * `loop.message.posted` is deliberately not drawn here (the same words
   * would print twice), so only human/system authors reach this item.
   * `source` is the daemon's provenance word; absent means the person typed
   * it (the NULL of `chat_message.source`), which needs no chip.
   * `skills` (S68 B8) is the row's chosen-skills receipt — the NAMES whose
   * canonical lines the daemon rendered into this turn's system half. The
   * text stays the person's pure words; the receipt renders as chips under
   * the bubble, never inside it.
   */
  | (Base & { kind: "message"; author: "human" | "system"; text: string; source?: "user" | "template" | "automation" | "blueprint"; messageId?: number; skills?: readonly string[];
    /** The row's attachment receipts (S69 AT8) — the STORE's facts, rendered
     * as chips under the bubble; the words above stay the person's own. */
    attachments?: readonly AttachmentReceipt[] })
  /**
   * S70 MB6: one row of the agents' own exchange — a message one agent
   * filed for another. The human's doors are steer (S39) and their own
   * bubble above; this row belongs to the AGENTS: `from` and `to` are
   * agent names, `thread` is the conversation's header row (its first
   * ask's id, MB3), and `state` rides the stream the rows ride: posted
   * → the ask or reply landed; delivered → a step boundary folded the
   * row into a turn (D4's pair stamp, one write); answered → a reply
   * settled the ask through its result_of link. `text` is the filing
   * agent's OWN words, verbatim — nobody rewrites them (MB5).
   */
  | (Base & { kind: "exchange"; messageId: number; thread: number; from: string; to: string; text: string; expectReply: boolean; state: "posted" | "delivered" | "answered" })
  /**
   * One run of a blueprint inside the loop (S39 CR1): the card the run's
   * steps sit under. The cost is the daemon's floor, never an invented one
   * — absent means unknown, and unknown renders as "—", never "$0".
   */
  | (Base & { kind: "run"; runSeq: number; blueprint: string; version?: number; state: "running" | "done" | "failed" | "cancelled"; cost?: { usdAtLeast: number; exact: boolean }; startedBy?: string });

export type ChatItemKind = ChatItem["kind"];

/** Everything the chat can ask its host to do. Every callback is optional: no callback, no control. */
export interface ChatActions {
  onAnswer?: (question: ChatQuestion, answer: string) => void | Promise<void>;
  onGate?: (gateId: string, answer: string, feedback?: string) => void | Promise<void>;
  onViewDiff?: (file: FileChange) => void;
  /** Open the host's "let this whole loop run without asking" control. Offered beside a permission ask only. */
  /** Open the loop's task list beside the chat (a plan gate offers it so the person can see what they approve). */
  onShowTasks?: () => void;
  onAllowAll?: () => void;
  /** True once the host has allowed everything: the approval card's override button then says so instead of offering it. */
  allowedAll?: boolean;
  onCancelSteer?: (steerId: number) => void | Promise<void>;
  /**
   * The preview door (S69 AT11): resolves to a data URL for a pinned
   * revision, or REJECTS — and a rejection is what the chip renders as
   * its honest sentence naming document and revision (ruling 3). Reads
   * are for rendering only; bytes never re-enter a send through here.
   */
  attachmentPreview?: (document: string, rev: number) => Promise<string>;
}

export interface SteerRequest {
  target: string;
  /** The operator's PURE words (S68 B7): the host forwards them untouched;
   * the daemon composes the wire message at the drain, never the UI. */
  text: string;
  route?: string;
  reasoningEffort?: string;
  attachments?: readonly ChatAttachment[];
  /** Chosen skill NAMES (S68 B7) — the queue validates them at its door,
   * files them on the row beside the text, and renders the instruction
   * lines at delivery. The bubble reads `text` alone. */
  skills?: readonly string[];
}

/** Answers that read as "no" — a decline, a reject, a deny. */
export const NEGATIVE_ANSWER = /^(reject|rejected|no|deny|denied|decline|declined|block|stop|cancel|always deny)/i;
/** Answers that read as "yes". */
export const POSITIVE_ANSWER = /^(approve|approved|yes|accept|allow|continue|ok|proceed|always allow)/i;

export interface ComposerOption {
  /** Per-route effort declaration. Absent preserves the host defaults. */
  reasoningEfforts?: string[] | null;
  /** The route's model can think, so a thinking on/off switch is offered for it. */
  thinking?: boolean;
  value: string;
  label: string;
}
