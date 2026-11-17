/**
 * memoryModel — the pure half of the S52 Memory surface (LM10).
 *
 * Everything here is a function over wire data: no fetching, no JSX, no
 * clock of its own. The rules that shape it:
 *
 *  • The panel never names a provenance, a scope or an authority sentence
 *    itself (LM6/LM9): the closed sets arrive from `learnings.vocabulary`,
 *    every per-row sentence from the row's own `describe`, and the retract
 *    confirm quotes `retractLaw` — the same string the CLI prints (RM9's
 *    one-string law). This file only decides WHICH daemon-provided word
 *    goes WHERE, and with what local colour.
 *  • Colours are ours, words are the daemon's. A provenance word that
 *    arrives is a word that renders, text beside every dot; an unknown
 *    word gets its own honest slate, never a silence and never a rename.
 *    The four provenance tones are DISTINCT by test (LM10): `relayed` —
 *    the channel fact — never shares a colour with a person's word.
 *  • The retraction fields are widened HERE, not invented: the Go
 *    `LearningsView` carries `retracted · retractedAt · retractedReason ·
 *    retractedBy`, and the board's T2 fixtures predate retractions, so the
 *    generated contract never observed those keys. The same documented-
 *    widening idiom `triageModel.ts` uses for `legalNext`; the assertion
 *    lives at the read boundary and the wire is the truth.
 *  • Strength is `×N` beside the word — M3's `observedCount` is shown,
 *    never merged into the provenance colour (the strength is NOT the
 *    provenance; one pill that blended them would be one lie with two
 *    excuses).
 */

import type { RpcMethods } from "../../../proto/contract";

// ── wire types (widened where the fixture-derived contract is narrower) ───

/** The retraction stamp as the Go `LearningsView` carries it. Absent from
 *  the generated shapes because the fixtures answered no retraction (the
 *  keys are `omitempty` on the wire — a live row genuinely omits them). */
export interface Retraction {
  retracted: boolean;
  retractedAt: string;
  retractedReason: string;
  retractedBy: string;
}

/** One board row: the learning as `learnings.board` answers it plus the
 *  daemon's own `describe` sentence (LM6 — the panel quotes it verbatim). */
export type MemoryRow = RpcMethods["learnings.board"]["result"]["learnings"][number] &
  Partial<Retraction>;

/** The full view the drawer reads (`learnings.show`), widened alike. */
export type MemoryView = RpcMethods["learnings.show"]["result"]["learning"] &
  Partial<Retraction>;

/** One audit line — `summary` is the daemon's sentence, quoted verbatim
 *  wherever it renders. */
export type HistoryRow = RpcMethods["learnings.history"]["result"]["changes"][number];

/** One `forAgent` hit: the row, and the WHY the ranking gave it its place
 *  (rank, basis, and the shared M1 note the panel prints once). */
export type ForAgentHit = RpcMethods["learnings.forAgent"]["result"]["hits"][number];

/** The vocabulary — the closed sets, the limits, the M1 note, and the ONE
 *  retract law every confirm quotes. Fully typed by the contract. */
export type Vocabulary = RpcMethods["learnings.vocabulary"]["result"];

/** The proposal kinds this panel scopes S45's desk to: the agent's ONLY
 *  move in this plane is a retraction proposal, and it stays inert until a
 *  person commits it (LM8; S35 TD3). The desk itself is S45's component,
 *  doors and all — this list only names which rows it shows here. */
export const MEMORY_PROPOSAL_KINDS = ["learning_retract"] as const;

// ── the board's filters (the daemon's params, never a client sweep) ────────

/** What the dials hold. Empty strings mean "any" and are OMITTED from the
 *  params — the daemon reads an absent filter as no filter (LM6: the
 *  filtering happens in the query, and this panel never filters locally). */
export interface MemoryFilterDraft {
  scope: string;
  provenance: string;
  tag: string;
  agent: string;
  project: string;
  loop: string;
  includeRetracted: boolean;
}

export const emptyMemoryFilters: MemoryFilterDraft = {
  scope: "",
  provenance: "",
  tag: "",
  agent: "",
  project: "",
  loop: "",
  includeRetracted: false,
};

/** Compose `learnings.board` params from the draft. The generated params
 *  type is fixture-narrower than the wire (the fixture sent only
 *  `provenance`), so the composed object is returned wide and the rpc
 *  wrapper asserts it into the contract's shape at the boundary — the
 *  wire itself carries the full seven the Go handler decodes. */
export function memoryBoardParams(draft: MemoryFilterDraft): Record<string, unknown> {
  const params: Record<string, unknown> = {};
  if (draft.scope !== "") params.scope = draft.scope;
  if (draft.provenance !== "") params.provenance = draft.provenance;
  if (draft.tag !== "") params.tag = draft.tag;
  if (draft.agent !== "") params.agent = draft.agent;
  if (draft.project !== "") params.project = draft.project;
  if (draft.loop !== "") params.loop = draft.loop;
  if (draft.includeRetracted) params.includeRetracted = true;
  return params;
}

/** Whether the board may ask at all: no filter is a legitimate state —
 *  the board answers everything (which is exactly what the daemon's
 *  default read is), so unlike triage there is no root that must be named
 *  first. Kept as a named predicate so the section's shape stays honest. */
export function memoryBoardIsLoadable(_draft: MemoryFilterDraft): boolean {
  return true;
}

// ── colour, never words (the words are the daemon's) ───────────────────────

/** The provenance pill's colour (LM10: the four words render distinctly —
 *  the honesty ORDER is the daemon's M1 note, the COLOUR is ours). An
 *  unknown word is slate with its own text, never a rename. */
export type MemoryTone = "green" | "sky" | "amber" | "slate" | "violet";

export function provenanceTone(provenance: string): MemoryTone {
  switch (provenance) {
    case "told":
      return "green";
    case "observed":
      return "sky";
    case "assumed":
      return "amber";
    case "relayed":
      return "slate";
    default:
      return "violet";
  }
}

/** The scope pill's colour — three words, three tones, slate for the
 *  word nobody compiled yet. */
export function scopeTone(scope: string): MemoryTone {
  switch (scope) {
    case "person":
      return "green";
    case "project":
      return "sky";
    case "agent":
      return "amber";
    default:
      return "slate";
  }
}

/** The audit line's actor-kind tone: a person's act and the CLI's offline
 *  twin read alike (both are humans, LM4), an agent's line reads as its
 *  own fact, `system` as the machine it is. */
export function actorTone(actorKind: string): MemoryTone {
  switch (actorKind) {
    case "user":
    case "cli":
      return "green";
    case "agent":
      return "amber";
    case "system":
      return "sky";
    default:
      return "slate";
  }
}

/** M3's strength as it is shown: `×N`, only when the daemon counted. An
 *  absent count is NO badge, not `×0` and not `×1` — the absence is the
 *  fact. */
export function strengthLabel(observedCount: number | undefined): string {
  return observedCount === undefined || observedCount <= 1 ? "" : `×${observedCount}`;
}

/** The retraction, once the row carries one: the marker text is built
 *  from the stamp's OWN fields — when, by whom, why — nothing invented. */
export function retractionLabel(row: Partial<Retraction>): string | undefined {
  if (row.retracted !== true) return undefined;
  return `${row.retractedAt ?? "?"} · ${row.retractedBy ?? "?"} · ${row.retractedReason ?? ""}`;
}
