/**
 * continuousModel — the S27 T10 model: wire types read off the generated
 * contract, and the ONE home for every daemon-word the panel tones.
 *
 * CN13/CN22 (rule 4) shape the whole file: the panel holds no scheduling
 * vocabulary and no logic. Reasons, verdict sentences, state sentences and
 * the raise instruction ARRIVE in the answers and ride verbatim — this
 * model does not compose, translate, or reinterpret one of them. What lives
 * here is (a) the types, as indexed access into `RpcMethods` so the contract
 * is the only schema, and (b) tone maps keyed by the daemon's own words,
 * each word present exactly once in this file (the guard test scans the
 * other folders' sources for them), with a slate fallback so an unknown
 * future word still renders — as the daemon said it — instead of vanishing.
 *
 * The word sets come from the daemon: the repository state words are
 * `repoStateWord` (internal/daemon/continuous_socket.go), the verdict words
 * are the core's `VerdictStart/Defer/Skip` (internal/continuous/types.go),
 * and the needs-you kinds are the ledger's blocked decisions shown verbatim
 * (CN21's no-interpretation rule) — so the maps key only on what the daemon
 * actually answers, and nothing else decides by word anywhere.
 */

import type { RpcMethods } from "../../../proto/contract";

// ── the wire types, read off the contract, never re-declared ───────────────

export type SchedulerStatus = RpcMethods["scheduler.status"]["result"];
export type SchedulerRepo = SchedulerStatus["repos"][number];
export type SchedulerSettingsRow = SchedulerStatus["settings"][number];
export type SchedulerDescriptor = SchedulerStatus["descriptor"];
export type SchedulerPreview = RpcMethods["scheduler.preview"]["result"];
export type SchedulerPreviewLine = SchedulerPreview["decisions"][number];
export type SchedulerDecision = RpcMethods["scheduler.decisions"]["result"][number];
export type SchedulerAttention = RpcMethods["scheduler.attention"]["result"];
export type SchedulerAttentionItem = SchedulerAttention["items"][number];
export type RootSetInput = RpcMethods["scheduler.root.set"]["params"];
export type RootSetResult = RpcMethods["scheduler.root.set"]["result"];

/** Is the `scheduler.status` answer shaped as the contract describes? A
 *  daemon answering something else (a bare `{}` over a half-wired plane,
 *  S51's shape) gets an honest "unreadable" — never a crash that takes the
 *  Settings screen down with it, and never invented numbers. */
export function readableStatus(v: unknown): v is SchedulerStatus {
  const o = v as Record<string, unknown> | undefined;
  return (
    typeof o === "object" &&
    o !== null &&
    typeof o.enabled === "boolean" &&
    (Array.isArray(o.repos) || o.repos === null || o.repos === undefined) &&
    typeof o.slots === "object" &&
    o.slots !== null &&
    typeof (o.slots as Record<string, unknown>).cap === "number"
  );
}

/** A status answer with its lists made lists: an older daemon answered `null` for "no repositories
 *  enrolled yet", which is an honest empty, not an unreadable answer. */
export const withLists = (v: SchedulerStatus): SchedulerStatus => ({
  ...v,
  repos: Array.isArray(v.repos) ? v.repos : [],
  settings: Array.isArray(v.settings) ? v.settings : [],
});

/** The decisions page size the panel asks for — the daemon's own cap is
 *  1..500; the newest 200 rows is the panel's honest window. */
export const DECISIONS_LIMIT = 200;

// ── tone maps: one home per daemon word ────────────────────────────────────

export type TrueTone = "green" | "amber" | "blue" | "red" | "slate";

/** `state.kind` of a repository row (`repoStateWord`, the tick's own order):
 *  waiting words amber, working words blue, stopped words slate, faults
 *  red. An unknown future word falls to slate and RENDERS — the daemon
 *  added a state, the panel does not get to hide it. */
const REPO_STATE_TONE: Record<string, TrueTone> = {
  ready: "green",
  occupied: "blue",
  idle: "slate",
  off: "slate",
  "master-off": "slate",
  paused: "slate",
  "out-of-window": "amber",
  "template-invalid": "red",
  "budget-exhausted": "red",
};

export const repoStateTone = (kind: string): TrueTone => REPO_STATE_TONE[kind] ?? "slate";

/** The preview verdicts (the core's three). `would start` green, `defer`
 *  amber, `skip` slate — tone only; the line's reason and detail are the
 *  daemon's own words, rendered untouched. */
const VERDICT_TONE: Record<string, TrueTone> = {
  start: "green",
  defer: "amber",
  skip: "slate",
  refuse: "red",
};

export const verdictTone = (verdict: string): TrueTone => VERDICT_TONE[verdict] ?? "slate";

/** The master switch's own tone: on is green, off/paused say their state. */
export const masterTone = (enabled: boolean, paused: boolean): TrueTone =>
  !enabled ? "slate" : paused ? "amber" : "green";

// ── small honest helpers (display, never scheduling) ───────────────────────

/** One line of a preview subject: `queue/7`, `roadmap/9` — joining the two
 *  fields the daemon separated, adding no name of our own. */
export const subjectWords = (kind: string, id: string | number): string => `${kind}/${id}`;

/** `window-closed`/rate-limit lines carry their instant in the daemon's
 *  `detail`; the panel shows the line's reason and detail exactly as
 *  answered, so this only decides whether there is a detail to show. */
export const hasDetail = (line: { detail?: string }): boolean =>
  line.detail !== undefined && line.detail !== "";

/** The "not starting" half of the preview: every decided line that is not
 *  in `wouldStart` — a filter over the daemon's own lines by identity,
 *  never a recomputation of the tick. */
export const notStartingLines = (preview: SchedulerPreview): SchedulerPreviewLine[] => {
  const would = Array.isArray(preview.wouldStart) ? preview.wouldStart : [];
  const decided = Array.isArray(preview.decisions) ? preview.decisions : [];
  const starting = new Set(would.map((l) => `${l.componentRoot}|${l.subjectKind}|${l.subjectId}`));
  return decided.filter((l) => !starting.has(`${l.componentRoot}|${l.subjectKind}|${l.subjectId}`));
};

/** Is this attention item's note (the daemon attaches S55's raise
 *  instruction to the kinds whose remedy is a different act — CN21's
 *  no-interpretation rule means the panel never matches the kind to choose
 *  the note; the note rides along or it does not). */
export const attentionNote = (item: SchedulerAttentionItem): string | undefined =>
  item.note === "" ? undefined : item.note;

/** A decisions filter row is kept when its fields stand — string
 *  containment over the daemon's OWN fields (subject `kind/id`, decision,
 *  reason); the door filters the repository, the panel narrows the newest
 *  window it was given, and nothing is computed. */
export interface DecisionFilters {
  componentRoot: string;
  subject: string;
  decision: string;
  reason: string;
}

export const filterDecisions = (rows: SchedulerDecision[], f: DecisionFilters): SchedulerDecision[] =>
  (Array.isArray(rows) ? rows : []).filter((row) => {
    if (f.componentRoot !== "" && row.componentRoot !== f.componentRoot) return false;
    if (f.subject !== "" && !subjectWords(row.subjectKind, row.subjectId).includes(f.subject)) return false;
    if (f.decision !== "" && !row.decision.includes(f.decision)) return false;
    if (f.reason !== "" && !row.reason.includes(f.reason)) return false;
    return true;
  });

/** The distinct words the window of rows actually carries — the filters'
 *  choices come from the daemon's ANSWERS, never from a typed list. */
export const distinctWords = (values: string[]): string[] => [...new Set(values)].sort();

/** The words the tone maps recognize — exported so the GUARD compares them
 *  against the daemon's own source (`repoStateWord`, the core's verdicts)
 *  and fails the drift in either direction: a word the daemon never
 *  answers, or a state the daemon answers that the map forgot (which would
 *  silently fall to slate). */
export const REPO_STATE_WORDS = Object.keys(REPO_STATE_TONE);
export const VERDICT_WORDS = Object.keys(VERDICT_TONE);
