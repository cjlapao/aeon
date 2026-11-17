/**
 * historyModel — the pure half of the S53 T5 History surface (HV7, P7).
 *
 * Everything here is a function over wire data: no fetching, no JSX, no
 * clock of its own. The rules that shape it:
 *
 *  • **No vocabulary is this file's.** The lane words and the outcome words
 *    are `history.vocabulary`'s answer and nothing else (HV2, acceptance
 *    criterion 2). A row's displayed lane is its own `describe.lane`, its
 *    displayed outcome its own `describe.outcome`, each byte for byte —
 *    this file reads fields, it never names a state. The one place a
 *    vocabulary word is spelled at all is `LINK_ROUTES` below, and that is
 *    a ROUTING table over the row's own link kinds, not a word list a
 *    person can read; the guard (`historyRpc.guard.test.ts`) pins the
 *    exemption instead of trusting this comment.
 *  • Every daemon sentence renders VERBATIM: the floor statement, each
 *    row's `describe`, the stored `reason`, the notShown reason words, the
 *    empty-scope `statement`. A paraphrase here would be the second truth
 *    S25/H4 exists to forbid.
 *  • The contract is fixture-derived and narrower than the wire on three
 *    shapes (`notShown`/`undecodable.ids` as `Array<unknown>`, the params'
 *    omitempty fields, the list's `statement`). Each widening is documented
 *    where it is declared and asserted in ONE place — `historyRpc.ts` —
 *    measured against the Go structs, never invented.
 */

import type { RpcMethods } from "../../../proto/contract";

// ── wire types (widened where the fixture-derived contract is narrower) ───

/** One side of the scope's shown count. Go `history.Counted`
 *  (aeon/internal/history/coverage.go:36-40): `loops` always, `oldest` and
 *  `newest` omitempty — the span the SHOWN entries actually cover. */
export interface Counted {
  loops: number;
  oldest?: string;
  newest?: string;
}

/** The "cannot say" state. Go `history.Undecodable`
 *  (aeon/internal/history/coverage.go:47-50): `{count, ids}` where `ids` is
 *  `[]string` capped at `history.UndecodableIDsListed` (20) — the count is
 *  the truth even when the list is capped, which is why the UI shows both. */
export interface Undecodable {
  count: number;
  ids: string[];
}

/** One honest "the view does not show this, and here is why". Go
 *  `history.NotShown` (aeon/internal/history/coverage.go:59-62):
 *  `{reason, count}`. The contract types this `Array<unknown>` because the
 *  coverage fixture answered `[]` — the widening is measured against that
 *  struct, not imagined. */
export interface NotShown {
  reason: string;
  count: number;
}

/** `history.coverage`'s answer: Go `history.CoverageView`
 *  (aeon/internal/history/coverage.go:74-82) — `scope`, `live`, `archived`,
 *  `undecodable`, `floor{statement}`, `notShown[]`. */
export type Coverage = RpcMethods["history.coverage"]["result"];

/** One link out of a row. Go `history.Link` (describe.go:36-39). */
export interface HistoryLink {
  kind: string;
  id: string;
}

/** One row's details, through the daemon's ONE formatter (HV2). Go
 *  `history.Described` (describe.go:142-…): lane/outcome/cost/costState
 *  always, duration/reason omitempty, `links` as data. `duration` and
 *  `reason` are absent from the generated contract because its fixture
 *  carried neither — widening, documented, measured against the struct. */
export interface HistoryDescribe {
  lane: string;
  outcome: string;
  cost: string;
  costState: string;
  duration?: string;
  reason?: string;
  links: HistoryLink[];
}

/** One history row. Go `history.HistoryRow` (row.go:24-38): the eight S25
 *  fields plus `describe`. `reason` is omitempty on the wire (D4/H4: the
 *  stored reason of a blocked loop, verbatim) — the contract's fixture
 *  carried none, so it is absent there and widened here. */
export type HistoryRow = RpcMethods["history.list"]["result"]["rows"][number] & {
  reason?: string;
  describe: HistoryDescribe;
};

/** `history.list`'s / `history.export`'s answer. Go `HistoryListResult`
 *  (protocol/history.go:73-76) carries `statement` omitempty — P31's honest
 *  empty, "no history" as a fact rather than a gap. The fixture was
 *  populated so the generated type omits it; reading it is how the screen
 *  renders the honest empty instead of inventing one. */
export interface HistoryListResult {
  rows: HistoryRow[];
  statement?: string;
}

/** `history.vocabulary`'s answer — the closed sets as data (HV2). The only
 *  vocabulary the surface knows, and it arrives from the daemon. */
export type Vocabulary = RpcMethods["history.vocabulary"]["result"];

/** `history.show`'s answer plus the two omitempty fields its fixture never
 *  carried: `files` (an archived entry's cold-stored content) and `refused`
 *  (a wrong-door refusal travelling verbatim). Go `HistoryShowResult`
 *  (protocol/history.go:152-158). */
export interface ShowResult {
  id: string;
  kind: string;
  transcript: Array<{ role: string; content: Array<{ kind: string; text: string; isError: boolean }> }>;
  files?: Array<{ path: string; bytes: number; sha256: string; modified?: string; content?: string }>;
  refused?: string;
}

// ── the draft the pickers hold ──────────────────────────────────────────────

/** What the person has pointed the view at. Every field is a wire filter
 *  word `history.vocabulary.filters` names; nothing else can be asked.
 *  `root` empty is the every-component span (HV1: `""` is the merge's own
 *  `--all` reading, never a silent default) — and the screen says so. */
export interface ScopeDraft {
  root: string;
  /** The lane switch's value: a word from `history.vocabulary.lanes`, or ""
   *  for the daemon's default grouping (omitted, never renamed). */
  lane: string;
  since: string;
}

export function emptyScopeDraft(): ScopeDraft {
  return { root: "", lane: "", since: "" };
}

/** The lane switch's options: exactly the daemon's served words, in its
 *  order, plus the empty selection that sends NOTHING (the daemon's own
 *  default). An empty vocabulary is an empty switch — the screen renders
 *  no lane it was not given. */
export function laneOptions(vocabulary: Vocabulary | undefined): string[] {
  return Array.isArray(vocabulary?.lanes) ? vocabulary.lanes : [];
}

/** The outcome words the daemon serves, for the outcome caption beside a
 *  row. Never a list of this file's: an empty answer renders nothing. */
export function outcomeOptions(vocabulary: Vocabulary | undefined): string[] {
  return Array.isArray(vocabulary?.outcomes) ? vocabulary.outcomes : [];
}

/** Does the draft name a lower bound the wire should carry? Blank never
 *  rides the wire (omitempty on `HistoryListParams.Since`). */
export function scopeParams(draft: ScopeDraft): { projectRoot?: string; groupBy?: string; since?: string } {
  const params: { projectRoot?: string; groupBy?: string; since?: string } = {};
  if (draft.root.trim() !== "") params.projectRoot = draft.root.trim();
  if (draft.lane !== "") params.groupBy = draft.lane;
  if (draft.since.trim() !== "") params.since = draft.since.trim();
  return params;
}

// ── coverage: the three states (HV1, P31) ───────────────────────────────────

/** A coverage answer read into the three states the banner renders:
 *  **shown** (live + archived, with the span they cover), **not-shown**
 *  (the reasons and their counts), **cannot-say** (the undecodable archives
 *  the probe could not prove). `state` is the banner's third distinction:
 *  `healthy` when nothing is undecodable, `warning` when something is —
 *  P31's third state is a visible warning, never silence, and the floor
 *  statement rides in both. */
export interface CoverageSummary {
  scope: string;
  /** True when the scope named nothing, i.e. the every-component span. */
  wholeSpan: boolean;
  shown: { live: Counted; archived: Counted };
  cannotSay: Undecodable;
  notShown: NotShown[];
  floor: string;
  state: "healthy" | "warning";
}

/** The coverage ANSWER as the wire can actually send it. The generated
 *  contract types every field of `history.coverage`'s result as present with
 *  required stamps, because its fixture (`conformance/history/coverage.json`)
 *  carried every field; Go's `CoverageView` (`coverage.go:65-…`) omits what
 *  is empty — `Counted.Oldest`/`Newest` are `omitempty` (`coverage.go:36-40`)
 *  and an empty scope answers `{loops: 0}` with no span at all. Reading a
 *  missing stamp as `""` would be inventing a time, so the type says absent
 *  and the banner says nothing about the span. A widening of the GENERATED
 *  type only, measured against the Go struct; `contract.ts` is untouched. */
export type CoverageWire = {
  scope?: string;
  live?: Counted;
  archived?: Counted;
  undecodable?: Undecodable;
  floor?: { statement?: string };
  /** `unknown[]` on purpose: the generated contract types this `Array<unknown>`
   *  because its fixture answered an empty list, so the element shape is known
   *  only from Go (`NotShown{reason, count}`, `coverage.go:57-60`) — read field
   *  by field below rather than asserted into a shape. */
  notShown?: unknown[];
};

export function summarizeCoverage(coverage: Coverage | CoverageWire | undefined): CoverageSummary | undefined {
  if (coverage === undefined || coverage === null) return undefined;
  const live = coverage.live ?? { loops: 0 };
  const archived = coverage.archived ?? { loops: 0 };
  const undecodable = coverage.undecodable ?? { count: 0, ids: [] };
  const cannotSay: Undecodable = {
    count: typeof undecodable.count === "number" ? undecodable.count : 0,
    ids: Array.isArray(undecodable.ids) ? undecodable.ids.filter((id): id is string => typeof id === "string") : [],
  };
  // Each entry is read field by field rather than trusted shape-first: an
  // answer with a half-row says what it says, and a blank reason is rendered
  // as the daemon's blank, not as a sentence this file invented.
  const notShown: NotShown[] = Array.isArray(coverage.notShown)
    ? coverage.notShown.map((raw) => {
        const entry = (raw ?? {}) as { reason?: unknown; count?: unknown };
        return {
          reason: typeof entry.reason === "string" ? entry.reason : "",
          count: typeof entry.count === "number" ? entry.count : 0,
        };
      })
    : [];
  return {
    scope: typeof coverage.scope === "string" ? coverage.scope : "",
    wholeSpan: coverage.scope === "" || coverage.scope === undefined,
    shown: { live, archived },
    cannotSay,
    notShown,
    floor: coverage.floor?.statement ?? "",
    state: cannotSay.count > 0 ? "warning" : "healthy",
  };
}

/** The banner's tone, and the only local decision it makes: the WORD and
 *  the NUMBER are the daemon's, the colour is the app's. An answer with
 *  nothing undecodable is healthy; anything undecodable is a warning. */
export function coverageTone(state: CoverageSummary["state"] | undefined): "green" | "amber" {
  return state === "warning" ? "amber" : "green";
}

/** The span the SHOWN entries actually cover: the oldest `oldest` and the
 *  newest `newest` across both sides, each as the daemon stamped it (RFC3339
 *  text, never reformatted here). An absent end is an absent end — the
 *  banner says nothing rather than reading a missing stamp as a time. */
export function coverageSpan(summary: CoverageSummary): { oldest: string; newest: string } {
  const stamps = (side: Counted, key: "oldest" | "newest"): string[] => {
    const value = side[key];
    return typeof value === "string" && value !== "" ? [value] : [];
  };
  const oldest = [...stamps(summary.shown.live, "oldest"), ...stamps(summary.shown.archived, "oldest")].sort()[0] ?? "";
  const newest = [...stamps(summary.shown.live, "newest"), ...stamps(summary.shown.archived, "newest")].sort().pop() ?? "";
  return { oldest, newest };
}

// ── the row → render model ─────────────────────────────────────────────────

/** The cost's honesty word as a tone (S33's three-state rule, and the same
 *  shape `CostHonesty` produces: known | partial | unknown). The word itself
 *  always renders as text beside the colour — a colour never replaces it,
 *  and an unknown word is its own honest dot rather than a rename. */
export function costTone(costState: string | undefined): "green" | "amber" | "slate" {
  switch (costState) {
    case "known":
      return "green";
    case "partial":
      return "amber";
    default:
      return "slate";
  }
}

/** The link kinds this app has a screen for, and where each one goes. The
 *  keys are Go `history`'s link constants — `LinkLoop` and `LinkRoadmap`
 *  (aeon/internal/history/describe.go:31-34), two of the four kinds a row's
 *  own `links[].kind` can carry. The other two (`LinkTask`, `LinkGoal`) have
 *  no screen in this app yet, so they name NO route here and the row renders
 *  them as the plain words they are — never a link that goes nowhere. The
 *  roadmap form is the one the triage panel already uses for the same
 *  pointer (`/roadmap#item-<id>`).
 *
 *  This table ROUTES, it never LABELS: it is the one place in the surface a
 *  lane word is spelled, it is a key rather than text, and the word a person
 *  reads beside a link is the row's own `link.kind`, printed verbatim. The
 *  folder guard's vocabulary census exempts exactly this literal block and
 *  nothing else (`historyRpc.guard.test.ts`). */
export const LINK_ROUTES: Record<string, (id: string) => string> = {
  loop: (id: string) => `/loops/${encodeURIComponent(id)}`,
  roadmap: (id: string) => `/roadmap#item-${encodeURIComponent(id)}`,
};

export function linkHref(kind: string, id: string): string | undefined {
  const build = LINK_ROUTES[kind];
  return build === undefined ? undefined : build(id);
}

/** One row as the table renders it. Every displayed word is the daemon's:
 *  lane, outcome, cost, costState and reason come from `describe` verbatim
 *  (`describe.reason` when present, else the row's stored `reason`, else
 *  nothing — the honest absence), and the links keep their own kind words. */
export interface RowRender {
  id: string;
  kind: string;
  slug: string;
  title: string;
  status: string;
  timeline: string;
  lane: string;
  outcome: string;
  cost: string;
  costState: string;
  costTone: "green" | "amber" | "slate";
  duration: string;
  reason: string;
  links: Array<{ kind: string; id: string; href: string | undefined }>;
}

export function rowRender(row: HistoryRow): RowRender {
  const describe = row.describe;
  return {
    id: row.id,
    kind: row.kind,
    slug: row.slug,
    title: row.title,
    status: row.status,
    timeline: row.timeline,
    lane: describe?.lane ?? "",
    outcome: describe?.outcome ?? "",
    cost: describe?.cost ?? row.cost,
    costState: describe?.costState ?? "",
    costTone: costTone(describe?.costState),
    duration: describe?.duration ?? "",
    reason: describe?.reason ?? row.reason ?? "",
    links: Array.isArray(describe?.links) ? describe.links.map((link) => ({ kind: link.kind, id: link.id, href: linkHref(link.kind, link.id) })) : [],
  };
}

/** The list answer read defensively: a bare `{}` survives as no rows plus
 *  no statement (the screen then says it has no statement to quote), never
 *  a crash and never a fabricated row. */
export function listShape(answer: unknown): HistoryListResult {
  if (typeof answer !== "object" || answer === null) return { rows: [] };
  const raw = answer as { rows?: unknown; statement?: unknown };
  const rows = Array.isArray(raw.rows) ? (raw.rows.filter((row) => typeof row === "object" && row !== null) as HistoryRow[]) : [];
  return {
    rows,
    ...(typeof raw.statement === "string" && raw.statement !== "" ? { statement: raw.statement } : {}),
  };
}

/** The drill-down answer read defensively, same law: `refused` is the
 *  daemon's own wrong-door sentence and travels whole (DB19), `files` is an
 *  archived entry's cold content, `transcript` a live one's. */
export function showShape(answer: unknown): ShowResult {
  const empty: ShowResult = { id: "", kind: "", transcript: [] };
  if (typeof answer !== "object" || answer === null) return empty;
  const raw = answer as Partial<ShowResult> & { transcript?: unknown; files?: unknown };
  return {
    id: typeof raw.id === "string" ? raw.id : "",
    kind: typeof raw.kind === "string" ? raw.kind : "",
    transcript: Array.isArray(raw.transcript) ? (raw.transcript as ShowResult["transcript"]) : [],
    ...(Array.isArray(raw.files) ? { files: raw.files as ShowResult["files"] } : {}),
    ...(typeof raw.refused === "string" && raw.refused !== "" ? { refused: raw.refused } : {}),
  };
}

/** What the drill-down has to say about an answer: a refusal (verbatim), a
 *  live transcript, an archived set of cold files, or the honest absence of
 *  all three. Decided by the answer's own fields, never by a guessed kind. */
export type DrilldownBody = { kind: "refused"; sentence: string } | { kind: "transcript"; messages: ShowResult["transcript"] } | { kind: "files"; files: NonNullable<ShowResult["files"]> } | { kind: "nothing" };

export function drilldownBody(answer: ShowResult | undefined): DrilldownBody {
  if (answer === undefined) return { kind: "nothing" };
  if (answer.refused !== undefined && answer.refused !== "") return { kind: "refused", sentence: answer.refused };
  if (answer.transcript.length > 0) return { kind: "transcript", messages: answer.transcript };
  if (answer.files !== undefined && answer.files.length > 0) return { kind: "files", files: answer.files };
  return { kind: "nothing" };
}

/** A refusal's parts, in the daemon's words and its order (AGENTS §4b: an
 *  error is `what` / `means` / `todo`). Joining them is the ONLY thing this
 *  does — no sentence here is written by the app, and a refusal with no parts
 *  comes back as the one sentence the wire sent. Pure; the caller owns the
 *  `RpcError`. */
export function refusalSentence(message: string, parts?: { what?: string; means?: string; todo?: string }): string {
  const words = [message, parts?.what, parts?.means, parts?.todo].filter((part): part is string => typeof part === "string" && part !== "");
  return words.join(" — ");
}
