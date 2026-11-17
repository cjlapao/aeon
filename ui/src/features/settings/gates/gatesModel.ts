// gatesModel.ts — S57 T11: the pure helpers the Gates panel renders with.
//
// Every word the panel shows about a gate — its requirement, its run state,
// its source, the rule's field values — arrives from the daemon and is
// rendered as sent; the tone of a `Pill` is chosen by the word's POSITION in
// the daemon's own vocabulary list, never by comparing the word to a string
// typed here (`gatesRpc.guard.test.ts` greps for such a literal and fails
// the build). What lives here is presentation arithmetic: reading a rule
// diff, folding an inherited gate into an add payload, sizing a duration.
//
// The types below name what the GENERATED contract already describes: the
// five conformance fixtures T11 added (`list-effective`, `add-memory`,
// `update-fields`, `runs-summary`, `try-truncated`) put the effective-read's
// source pair, the declared memory bound, the run summary and the truncated
// tails into the fixtures, so the contract — not this file — is the source
// of their shape.

import type { RpcMethods } from "../../../proto/contract";
import type { TrueColor } from "@cjlapao/ui-kit";

/**
 * One row of `verify.list`, widened only by `archivedAt` — the flag the
 * archive/restore verbs set, which no LIST fixture carries because an
 * archived row is only shown when the person asks for archived rows (and
 * the daemon sends the member exactly when the `VerifyGateView` struct
 * says: `omitempty` on the archived time).
 */
export type GateView = RpcMethods["verify.list"]["result"]["gates"][number] & {
  archivedAt?: string;
};

/** One gate as `verify.add` takes it — the payload a copy or a form sends. */
export type AddGatePayload = RpcMethods["verify.add"]["params"];

/** The effective rule as `mergerule.get` carries it (nil only for a specific read of a component with no row). */
export type MergeRuleView = NonNullable<RpcMethods["mergerule.get"]["result"]["rule"]>;

/** The floor checklist — the daemon's ok/missing, each missing item with its sentence. */
export type FloorView = NonNullable<RpcMethods["mergerule.get"]["result"]["floor"]>;

/** The rule closed sets, straight from `mergerule.vocabulary`. */
export type MergeRuleVocabulary = RpcMethods["mergerule.vocabulary"]["result"];

/** The verify closed sets plus the daemon's try statement. */
export type VerifyVocabulary = RpcMethods["verify.vocabulary"]["result"];

/** One run as the list carries it (summary/reason arrive on the rows that have something to say). */
export type RunView = RpcMethods["verify.runs"]["result"]["runs"][number];

/**
 * One per-gate result as `verify.try` and `verify.runShow` both carry it —
 * the same `VerifyRunResultView` struct behind both doors; the try's
 * sampled type is named here because it is the door whose tail the person
 * expands most.
 */
export type RunResultView = RpcMethods["verify.try"]["result"]["results"][number];

/**
 * A pill tone for a daemon word, chosen by the word's index in the daemon's
 * own vocabulary list. The palette is a presentation decision — position 0
 * is the strongest claim the word makes — and a word outside the palette's
 * length (one the daemon invents tomorrow) falls back to the neutral tone
 * rather than to a crash or a guessed colour.
 */
export function pillTone(words: readonly string[] | undefined, value: string, palette: readonly TrueColor[]): TrueColor {
  if (!words) return "slate";
  const at = words.indexOf(value);
  return at < 0 ? "slate" : palette[at % palette.length] ?? "slate";
}

/**
 * Whether a gate row carries a memory bound. The bound is DECLARED, not
 * enforced — that sentence is the panel's own i18n text; the NUMBER is the
 * daemon's. This helper only answers "is there a bound to show at all",
 * which is a null check, not a vocabulary decision.
 */
export function hasMemoryBound(gate: GateView): boolean {
  return typeof gate.memoryMb === "number" && gate.memoryMb > 0;
}

/**
 * The gates `verify.list` returned that the component does not own.
 * `source` is the daemon's word and "own" is the value that means the
 * component holds the row itself; the comparison reads the wire's member
 * value, not a vocabulary set — the same kind of structural constant as a
 * params key, which is why the grep guard exempts exactly this spelling.
 */
export function inheritedGates(gates: readonly GateView[] | undefined): GateView[] {
  return (gates ?? []).filter((gate) => gate.source !== undefined && gate.source !== "own"); // wire-member
}

/**
 * Whether a row is the component's own — the mirror of `inheritedGates`:
 * edit and archive belong to the row's owner, so the panel offers them
 * only here; the daemon would refuse a write from a child to the root's
 * row anyway, and it would say so verbatim.
 */
export function ownedRow(gate: GateView): boolean {
  return gate.source === undefined || gate.source === "own"; // wire-member
}

/**
 * Whether a `mergerule.get` answer says the rule on screen is the
 * component's OWN row (as opposed to an inherited root row, the shipped
 * baseline, or none) — the only state in which a Clear has an own row to
 * remove. Wire-member comparison like `ownedRow`, not a vocabulary set.
 */
export function isOwnSource(source: string | undefined): boolean {
  return source === "own"; // wire-member
}

/**
 * The `verify.add` payload that copies one inherited gate into the child —
 * every member read off the daemon's row, so a copy carries exactly what
 * the root's row carries and nothing this file invented.
 */
export function copyPayload(componentId: string, gate: GateView): AddGatePayload {
  const payload: AddGatePayload = {
    componentId,
    name: gate.name,
    command: gate.command,
    requirement: gate.requirement,
    timeoutS: gate.timeoutS,
  };
  if (typeof gate.memoryMb === "number") payload.memoryMb = gate.memoryMb;
  return payload;
}

/** One changed field of a merge-rule draft: the field, the old value, the new. */
export interface RuleDiffRow {
  field: string;
  before: string;
  after: string;
}

function display(value: unknown): string {
  if (Array.isArray(value)) return value.length > 0 ? value.join(", ") : "—";
  if (typeof value === "boolean") return value ? "on" : "off";
  if (value == null) return "—";
  return String(value);
}

/**
 * The field-by-field diff between the rule the daemon holds and the draft
 * about to be sent. This is JSON comparison over the rule's own members —
 * no field is privileged, no value is interpreted, and the daemon's OWN
 * audit sentence (its `lastChange.summary`, re-read after the write) stays
 * the authoritative record of what changed. The diff exists so the confirm
 * dialog can show a person every value they are about to change.
 */
export function ruleDiff(before: MergeRuleView | undefined, draft: Record<string, unknown> | undefined): RuleDiffRow[] {
  if (!before || !draft) return [];
  const current = before as unknown as Record<string, unknown>;
  const rows: RuleDiffRow[] = [];
  for (const field of Object.keys(draft)) {
    if (!(field in current)) continue;
    if (JSON.stringify(current[field]) !== JSON.stringify(draft[field])) {
      rows.push({ field, before: display(current[field]), after: display(draft[field]) });
    }
  }
  return rows;
}

/** True when the draft differs from the rule the daemon answered with. */
export function ruleDirty(before: MergeRuleView | undefined, draft: Record<string, unknown> | undefined): boolean {
  return ruleDiff(before, draft).length > 0;
}

/** The comma-list editor's parser: trimmed, empty-dropped entries; order kept. */
export function parseList(text: string): string[] {
  return text
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
}

/** A run's duration as the human reads it: ms under a second, one decimal under a minute, whole seconds beyond. */
export function wallLine(wallMs: number): string {
  if (wallMs < 1000) return `${wallMs} ms`;
  const seconds = wallMs / 1000;
  return seconds < 60 ? `${seconds.toFixed(1)} s` : `${Math.round(seconds)} s`;
}
