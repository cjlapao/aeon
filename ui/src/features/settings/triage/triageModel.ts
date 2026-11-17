/**
 * triageModel — the pure half of the S51 T7 triage surface (TM10).
 *
 * Everything here is a function over wire data: no fetching, no JSX, no
 * clock of its own. The rules that shape it:
 *
 *  • The panel never describes a bug, composes an authority sentence or
 *    names a state itself (TM9/TM10): the sentences come from the daemon —
 *    `triage.board`'s per-row `describe` (validationSentence,
 *    prioritySentence, duplicate, handoff), `triage.vocabulary`'s
 *    validateStatement/handoffStatement — and this file only decides WHICH
 *    daemon-provided word goes WHERE, and with what local colour.
 *  • The two closed sets are ALWAYS separate pills (TM10): a validation
 *    word and a priority word never share a control, a colour scale or a
 *    sentence. `unvalidated` and `could-not-tell` are visibly different
 *    tones — the daemon's own law that they are different facts (D1).
 *  • Colours are ours, words are the daemon's. A state word that arrives
 *    is a state word that renders, text beside every dot; an unknown word
 *    gets its own honest slate, never a silence and never a rename.
 *  • The generated contract is fixture-derived and NARROWER than the wire
 *    in two places only — `triage.health`'s settings array (the fixture
 *    answered empty, `Array<unknown>`) and `triage.vocabulary`'s
 *    `legalNext` values (three of four states answered empty). The same
 *    documented-widening idiom `roadmapModel.ts` (S50) uses: the types are
 *    widened HERE, the assertion lives at the read boundary, documented
 *    against the Go structs `TriageHealthSetting`, `legalNext`.
 *  • The authority statements carry NO `<bug>` slot (S51 T6 deviation):
 *    one string per verb, quoted byte-for-byte, and the bug rides its own
 *    line beside it. No filling, no replacement — the roadmap's
 *    `filledDispatchStatement` has no triage twin because the daemon's
 *    sentence was written without a slot to fill.
 */

import type { RpcMethods } from "../../../proto/contract";

// ── wire types (widened where the fixture-derived contract is narrower) ───

/** One board row, exactly as `TriageBoardRow` (Go) emits it: the bug, the
 *  daemon's Describe for it, the audit-counted attempts, and the SCORED
 *  candidates (vector `distance`, closer is more alike). The triage board
 *  fixture carried full rows, so the generated type is already honest —
 *  this alias only names it for the components. */
export type BoardRow = RpcMethods["triage.board"]["result"][number];

/** The bug as every read carries it (`TriageView`, S29's shape, extended
 *  by S51's `filedBy`, links and the always-worded priority pair). */
export type BugView = BoardRow["bug"];

/** `triage.history`'s rows — one audit line; `summary` is the daemon's
 *  sentence, quoted verbatim wherever it renders. */
export type HistoryRow = RpcMethods["triage.history"]["result"][number];

/** The vocabulary — both closed sets, the legal edges, and the two
 *  authority statements. Fully typed by the contract (T6's fixture
 *  refresh carried the statements). */
export type Vocabulary = RpcMethods["triage.vocabulary"]["result"];

/** `triage.health`'s per-setting row. `state` is one of the daemon's four
 *  words — ok | missing | invalid | unverifiable — and this file never
 *  renames one. The fixture answered an empty array, so the contract says
 *  `Array<unknown>`; this is the documented widening over the Go struct
 *  `TriageHealthSetting` (key, name, state — no report counts exist for
 *  this plane's one setting). */
export interface HealthSetting {
  key: string;
  name: string;
  state: string;
}

export interface HealthResult {
  settings: HealthSetting[];
  evaluatedAt: string;
}

// ── the one declared setting this panel owns (TM4) ─────────────────────────

/** The S44-declared key whose blueprint answers validation. Spelled ONCE,
 *  here; no blueprint NAME appears anywhere in this folder. */
export const VALIDATION_BLUEPRINT_KEY = "triage.validationBlueprint";

// ── board params ────────────────────────────────────────────────────────────

/** The filter draft as the dial holds it. Empty strings mean "the dial is
 *  untouched" and never reach the wire — the daemon filters what it is
 *  asked to filter (TM10: filters as params, no client-side sweep). */
export interface FilterDraft {
  componentRoot: string;
  validation: string;
  priority: string;
  /** "" = any; "yes" / "no" are the only answers that ride the wire. */
  handedOff: string;
}

export const emptyFilters: FilterDraft = {
  componentRoot: "",
  validation: "",
  priority: "",
  handedOff: "",
};

/** `triage.board` params from the draft: the root is required (T2's law —
 *  the board answers ONE root at a time), every other dial rides only
 *  when touched (`omitempty` on the Go `TriageBoardParams`). */
export function boardParams(draft: FilterDraft): {
  componentRoot: string;
  validation?: string;
  priority?: string;
  handedOff?: boolean;
} {
  const p: { componentRoot: string; validation?: string; priority?: string; handedOff?: boolean } = {
    componentRoot: draft.componentRoot.trim(),
  };
  if (draft.validation !== "") p.validation = draft.validation;
  if (draft.priority !== "") p.priority = draft.priority;
  if (draft.handedOff === "yes") p.handedOff = true;
  if (draft.handedOff === "no") p.handedOff = false;
  return p;
}

/** The board is only worth asking once the root is named — an empty root
 *  is not a question, it is the refusal to ask one (the same discipline
 *  the roadmap board keeps). */
export function boardIsLoadable(draft: FilterDraft): boolean {
  return draft.componentRoot.trim() !== "";
}

// ── the two tone maps (colours are ours; the words are never touched) ──────

/** Validation tones — FOUR distinct colours so `unvalidated` and
 *  `could-not-tell` cannot be confused at a glance (TM10); an unknown
 *  word is its own honest slate. */
export function validationTone(state: string): "amber" | "green" | "red" | "sky" | "slate" {
  switch (state) {
    case "unvalidated":
      return "amber";
    case "reproduced":
      return "green";
    case "not-reproduced":
      return "red";
    case "could-not-tell":
      return "sky";
    default:
      return "slate";
  }
}

/** Priority honesty (D2): `unknown` is the honest absence, visibly not a
 *  stated state. */
export function priorityTone(state: string): "green" | "amber" | "slate" {
  switch (state) {
    case "known":
      return "green";
    case "assumed":
      return "amber";
    default:
      return "slate";
  }
}

/** Health tones — the roadmap plane's words, the same four, the same map
 *  (one law: advice dots). */
export function healthTone(state: string | undefined): "green" | "amber" | "red" | "slate" {
  switch (state) {
    case "ok":
      return "green";
    case "missing":
      return "amber";
    case "invalid":
      return "red";
    default:
      return "slate";
  }
}

// ── read-boundary widenings (documented, asserted where used) ──────────────

/** Widen `triage.health`'s answer to `HealthResult` (the fixture-coarse
 *  contract says `settings: Array<unknown>`). Same shape-guard as S50's
 *  `healthShape`: a bare `{}` survives as an honest empty. */
export function healthShape(answer: unknown): HealthResult {
  const empty: HealthResult = { settings: [], evaluatedAt: "" };
  if (typeof answer !== "object" || answer === null) return empty;
  const raw = answer as { settings?: unknown; evaluatedAt?: unknown };
  const settings: HealthSetting[] = [];
  if (Array.isArray(raw.settings)) {
    for (const candidate of raw.settings) {
      if (typeof candidate !== "object" || candidate === null) continue;
      const row = candidate as Partial<HealthSetting>;
      if (typeof row.key !== "string") continue;
      settings.push({
        key: row.key,
        name: typeof row.name === "string" ? row.name : "",
        state: typeof row.state === "string" ? row.state : "",
      });
    }
  }
  return { settings, evaluatedAt: typeof raw.evaluatedAt === "string" ? raw.evaluatedAt : "" };
}

/** The legalNext map widened: three of the four state keys answered an
 *  empty fixture array, so the contract types them `Array<unknown>`; the
 *  wire answer is one array of words per state (transitions.go owns the
 *  edges; this file only reads them). */
export function legalNextWords(vocabulary: Vocabulary | undefined, state: string): string[] {
  if (vocabulary === undefined) return [];
  const edges = vocabulary.legalNext as unknown as Record<string, unknown>;
  const words = edges[state];
  if (!Array.isArray(words)) return [];
  return words.filter((w): w is string => typeof w === "string");
}

/** The health line for this panel's one setting — the daemon's own row,
 *  or undefined when the sweep has nothing to say about it. */
export function settingHealth(health: HealthResult | undefined, key: string): HealthSetting | undefined {
  return health?.settings.find((row) => row.key === key);
}
