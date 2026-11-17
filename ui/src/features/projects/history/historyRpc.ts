/**
 * historyRpc — the ONE file through which the Projects → History surface
 * speaks (S53 T5, HV7/P7).
 *
 * Every method literal the surface uses stands here spelled exactly once, so
 * the guard (`historyRpc.guard.test.ts`) has one surface to scan besides the
 * components that call these wrappers, and the contract itself settles
 * whether a name is registered at all. Same discipline as `roadmapRpc.ts`
 * (S50), `triageRpc.ts` (S51), `automationRpc.ts` (S49) and
 * `environmentsRpc.ts` (S59).
 *
 * # Five doors, and only five — four reads and one read that downloads
 *
 * `history.list`, `history.show`, `history.coverage`, `history.vocabulary`
 * are the reads; `history.export` is the SAME query as `history.list` (Go
 * `handleHistoryExport` runs the same `s.List`, so the two cannot produce
 * different rows — H3). **This slice adds no write control anywhere** (HV3):
 * there is no create, update, delete or edit door to call, and the folder
 * owns none. The export is a person's act (HV3: it is not on the narrow
 * surface and an agent has only `history_list`/`history_show`), so the panel
 * reaches it behind the `canWrite` gate and never on a machine's behalf.
 *
 * # The scope read is not this folder's door
 *
 * The scope picker lists ROOT components, and the `component.list` literal
 * that answers it lives in `../useComponentRoots.ts` — the projects
 * folder's own read, the one `ProjectsScreen.tsx` already spells. A second
 * copy here would be a second owner of the same door, so this folder spells
 * no component literal and the guard's allow-list equals EXACTLY the five
 * history doors above (the same shape as roadmap's note about S49's
 * blueprint picker).
 *
 * # The widenings, all of them, in this file
 *
 * The generated contract is fixture-derived: `history.list`'s fixture sent
 * only `projectRoot`, so the generated params type demands it and knows
 * nothing of the other filters, and the coverage fixture answered `[]` for
 * `notShown` and `undecodable.ids`. Each assertion below is `omitempty` on
 * the wire, measured against the Go struct named beside it — never an
 * invented field. The READ of each widened shape lives in `historyModel.ts`,
 * so a component receives the real shape or nothing at all.
 */

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import {
  listShape,
  scopeParams,
  showShape,
  type Coverage,
  type HistoryListResult,
  type ScopeDraft,
  type ShowResult,
  type Vocabulary,
} from "./historyModel";

type ListParams = RpcMethods["history.list"]["params"];
type ExportParams = RpcMethods["history.export"]["params"];
type CoverageParams = RpcMethods["history.coverage"]["params"];
type ShowParams = RpcMethods["history.show"]["params"];

/** One scope question, assembled from what the person picked. The contract
 *  demands `projectRoot` because the fixture sent it; the Go structs
 *  `HistoryListParams` (aeon/internal/protocol/history.go:65-69) and
 *  `HistoryCoverageParams` (the same file, :186-191) carry
 *  `projectRoot`/`componentRoot`/`groupBy`/`since` all `omitempty`. This
 *  surface sends `projectRoot` — CT15's wire name for a root component, the
 *  name `history.list` itself carries — plus the two filter words, and never
 *  both scope keys at once: naming both is refused by name (DB19,
 *  `AnswerHistoryCoverage`), so the panel asks one scope, one way. */
function filterParams(draft: ScopeDraft): Record<string, string> {
  return scopeParams(draft) as Record<string, string>;
}

// ── reads ───────────────────────────────────────────────────────────────────

/** `history.vocabulary` — the closed sets as data (HV2): the lane words the
 *  switch offers, the outcome words the rows carry, the filter keys the
 *  reads take. The SOURCE of every state word this screen renders; loaded
 *  once per mount, exactly as roadmap and triage read theirs. */
export function useHistoryVocabulary(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<Vocabulary> {
  return useRpcQuery("history.vocabulary", {}, controller, live);
}

/** `history.coverage` — the honesty read (HV1): what the view SHOWS, what it
 *  DOES NOT SHOW and what it CANNOT SAY, for this scope. Read through
 *  `historyModel.summarizeCoverage`, which is where the contract's
 *  fixture-coarse `Array<unknown>`s become the Go shapes they are —
 *  `history.NotShown{reason, count}` and `history.Undecodable{count,
 *  ids []string}` (aeon/internal/history/coverage.go:47-62). The floor
 *  statement rides the answer and renders verbatim. */
export function useHistoryCoverage(
  draft: ScopeDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<Coverage> {
  return useRpcQuery("history.coverage", filterParams(draft) as unknown as CoverageParams, controller, live);
}

/** `history.list` — the rows, each already carrying the daemon's `describe`
 *  (lane/outcome/cost/costState/reason/links) and its floored cost. The
 *  daemon does the grouping and the filtering; the panel never sweeps or
 *  re-sorts a fetched list. The empty answer is the daemon's `statement`
 *  (Go `HistoryListResult.Statement`, omitempty — P31's honest empty), which
 *  this screen quotes rather than re-wording. */
export function useHistoryRows(
  draft: ScopeDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<HistoryListResult> {
  const query = useRpcQuery("history.list", filterParams(draft) as unknown as ListParams, controller, live);
  return {
    data: query.data === undefined ? undefined : listShape(query.data),
    loading: query.loading,
    error: query.error,
    refetch: query.refetch,
  };
}

/** `history.show` — the drill-down for ONE entry, read through its own door:
 *  a live loop's transcript, an archived loop's cold-stored files, or the
 *  wrong-door refusal travelling verbatim (DB19 — never an empty read that
 *  would lie about an archived loop). `id` is required on the wire
 *  (`HistoryShowParams.ID`, no omitempty); `projectRoot` rides only when a
 *  scope is named. */
export function useHistoryShow(
  id: string,
  draft: ScopeDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<ShowResult> {
  const params: Record<string, string> = { id };
  const scope = scopeParams(draft);
  if (scope.projectRoot !== undefined) params.projectRoot = scope.projectRoot;
  const query = useRpcQuery("history.show", params as unknown as ShowParams, controller, live);
  return {
    data: query.data === undefined ? undefined : showShape(query.data),
    loading: query.loading,
    error: query.error,
    refetch: query.refetch,
  };
}

// ── the person's export (a read that downloads; HV3) ───────────────────────

/** `history.export` — the SAME query as `history.list` (H3: one `s.List`,
 *  two doors), asked when a PERSON clicks and never by anything else. The
 *  rows are the identical rows the list already shows; what differs is only
 *  what the app then does with them: it writes them out client-side, from
 *  the daemon's payload, and never reads a file back. */
export const exportHistoryRows = (draft: ScopeDraft): Promise<HistoryListResult> =>
  rpcTyped("history.export", filterParams(draft) as unknown as ExportParams).then(
    (answer) => listShape(answer as unknown),
  );

/** One row per line, the shape the CLI's `aeon history export` emits (T3's
 *  NDJSON): the payload travels as the daemon answered it, field for field,
 *  with nothing added, renamed or dropped on the way out. */
export function historyNdjson(result: HistoryListResult): string {
  return `${result.rows.map((row) => JSON.stringify(row)).join("\n")}\n`;
}

/** Where the payload goes: the browser's own save, from a Blob URL this
 *  window made and immediately releases. No socket, no file read, no
 *  storage — the payload lives in the download and nowhere else. */
export function saveHistoryExport(result: HistoryListResult, filename: string): void {
  const url = URL.createObjectURL(new Blob([historyNdjson(result)], { type: "application/x-ndjson" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

/** The download's name, naming the scope it was asked for: `all` is the
 *  every-component span, so the file says which question it answers. */
export function historyExportFilename(draft: ScopeDraft): string {
  const scope = scopeParams(draft).projectRoot ?? "all";
  const lane = scopeParams(draft).groupBy;
  const clean = scope.replace(/^\/+|\/+$/g, "").replace(/[^\w.-]+/g, "-") || "all";
  return `aeon-history-${clean}${lane === undefined ? "" : `-${lane}`}.ndjson`;
}
