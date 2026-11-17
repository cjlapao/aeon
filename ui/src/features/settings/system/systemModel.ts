/**
 * systemModel — the pure half of Settings → System (S54 T6, SP8/P7).
 *
 * Everything here is formatting or colour. The panels never compare a
 * daemon word to a literal of their own: the five state words and the
 * three cost states exist exactly once, as the keys of the two tone maps,
 * and every comparison a panel needs goes through a named helper. The
 * panel renders the DAEMON's word beside that colour — a state is never
 * invented, renamed, or recomputed here (SP8: "No hardcoded states").
 *
 * The wire shapes below widen the generated contract where the T2
 * conformance fixtures were narrower than the wire (the embed-route trio
 * and the flying flag ride `omitempty`, so the generated type never saw
 * them). The widening is declared at this boundary and nowhere else, and
 * the wire decodes them — same discipline `memoryRpc.ts` records for its
 * fixture-narrower params.
 */

import type { RpcMethods } from "../../../proto/contract";

export type PlatformStatusResult = RpcMethods["platform.status"]["result"];
export type PlatformChangesResult = RpcMethods["platform.changes"]["result"];
export type ReindexPlanResult = RpcMethods["retrieval.reindexPlan"]["result"];

/** One component as the wire answers it: the S22 availability trio rides
 *  `omitempty`, invisible to the fixture-narrowed generated type. */
export interface StatusComponent {
  component: string;
  embedRoute?: string;
  embedModel?: string;
  embedDim?: number;
  corpora?: Array<{ name: string; files: number; staleChunks: number; reindexing?: boolean }>;
}

export interface RetrievalBlock {
  state: string;
  error?: string;
  availability?: string;
  components?: StatusComponent[];
}

export interface StatusView {
  /** The contract always answers one; the local type stays tolerant so a
   *  malformed never-crashes — the section simply prints no evaluated line. */
  evaluatedAt?: string;
  retrieval: RetrievalBlock;
  graph: {
    state: string;
    error?: string;
    roots?: Array<{ root: string; neverBuilt?: boolean; files: number; nodes: number; edges: number }>;
  };
  usage: {
    state: string;
    error?: string;
    workspace?: {
      loopsRun: number;
      inputTokensTotal: number;
      outputTokensTotal: number;
      costUsdTotal: number;
      costState: string;
      recorded: boolean;
    };
  };
}

/** The answer decodes into this shape by construction (platformview owns
 *  the bytes); the cast is the contract-widening boundary, not a guess. */
export function statusViewOf(result: PlatformStatusResult | undefined): StatusView | undefined {
  return result === undefined ? undefined : (result as unknown as StatusView);
}

/** The state words (SP1's five) exist only as these tone keys. An
 *  unknown word keeps its own honest dot and renders verbatim anyway. */
export const STATE = {
  available: "available",
  stale: "stale",
  building: "building",
  unwired: "unwired",
  error: "error",
} as const;

/** The three cost states (T0's ruling) — same rule, tone keys only. */
export const COST_STATE = {
  known: "known",
  partial: "partial",
  unknown: "unknown",
} as const;

export type TrueTone = "green" | "amber" | "blue" | "slate" | "red";

export function stateTone(word: string): TrueTone {
  switch (word) {
    case STATE.available:
      return "green";
    case STATE.stale:
      return "amber";
    case STATE.building:
      return "blue";
    case STATE.unwired:
      return "slate";
    case STATE.error:
      return "red";
    default:
      // A word the slice never imagined is its own honest dot, never a
      // rename and never a silence: the pill still prints the word.
      return "slate";
  }
}

export function costTone(word: string): TrueTone {
  switch (word) {
    case COST_STATE.known:
      return "green";
    case COST_STATE.partial:
      return "amber";
    default:
      return "slate";
  }
}

/** Is a pass flying on this component's corpus? The daemon's own boolean
 *  (`reindexing`) answers it — the panel never infers flight from counts. */
export function corpusFlying(component: StatusComponent | undefined, corpus: string): boolean {
  return (component?.corpora ?? []).some((c) => c.name === corpus && c.reindexing === true);
}

/** A whole block flying is the daemon's state word, compared through the
 *  constant so no panel ever carries a state literal of its own. */
export function blockFlying(state: string | undefined): boolean {
  return state === STATE.building;
}

const usd = (n: number): string => `$${n.toFixed(4)}`;

/** The money line keeps the THREE states distinct (SP8): a figure always
 *  rides its state word, `partial` rides the daemon's reason too, and
 *  `unknown` prints the daemon's reason and no number — an estimate is
 *  never shown as an invoice and an absent estimate is never guessed. */
/** The components list, stably empty when the block carries none (an
 *  unwired retrieval has no components — that is the daemon's answer). */
export function componentsOf(block: RetrievalBlock | undefined): StatusComponent[] {
  return block?.components ?? NO_COMPONENTS;
}

export function rootsOf(block: StatusView["graph"] | undefined): NonNullable<StatusView["graph"]["roots"]> {
  return block?.roots ?? NO_ROOTS;
}

export function costWords(cost: ReindexPlanResult["cost"] | undefined): {
  state: string;
  figure: string | undefined;
  reason: string | undefined;
  tone: TrueTone;
} {
  if (cost === undefined) {
    // A plan with no cost block at all is the daemon saying NOTHING about
    // money — the honest render is no pill, never an invented `unknown`.
    return { state: "", figure: undefined, reason: undefined, tone: "slate" };
  }
  return {
    state: cost.state,
    figure: typeof cost.usd === "number" ? usd(cost.usd) : undefined,
    reason: cost.reason,
    tone: costTone(cost.state),
  };
}

const group = (n: number): string => n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");

export const fmtInt = group;
export const fmtUsd = usd;

/** The ledger's actor column: the daemon already carries kind and id as
 *  two words; the panel joins them for reading and invents nothing. */
export function actorWords(actorKind: string, actorId: string): string {
  return `${actorKind}:${actorId}`;
}

/** A write whose pass was already flying answers with its starter's words
 *  (T1's additive `startedBy`/`startedAt` naming on the accepted shape).
 *  This reads them from the plain answer object — a boundary cast over
 *  what the T0-era fixtures never showed — so the panel can print exactly
 *  what the daemon said about who is running. */
export function startedWordsOf(answer: unknown): { by: string; at: string } | undefined {
  if (answer === null || typeof answer !== "object") return undefined;
  const rec = answer as Record<string, unknown>;
  const by = typeof rec.startedBy === "string" && rec.startedBy !== "" ? rec.startedBy : undefined;
  if (by === undefined) return undefined;
  return { by, at: typeof rec.startedAt === "string" ? rec.startedAt : "" };
}

/** `platform.changes` takes a limit; the panel's page size is a constant
 *  it sends verbatim (the daemon clamps [1,500] underneath). */
export const CHANGES_LIMIT = 100;

/** Stable empties: an answer shaped wrong renders as the empty the daemon
 *  proved, never a fresh literal that livelocks a dependent effect. */
export const NO_CHANGES: PlatformChangesResult = [];
export const NO_COMPONENTS: StatusComponent[] = [];
export const NO_ROOTS: NonNullable<StatusView["graph"]["roots"]> = [];

/** `platform.changes` answers the array directly; a malformed answer is
 *  the empty, and the emptiness is this shared constant (S52's lesson). */
export function changesOf(data: unknown): PlatformChangesResult {
  return Array.isArray(data) ? (data as PlatformChangesResult) : NO_CHANGES;
}
