import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { KnowledgeExplorer, KnowledgeMap, MemoryDetail, useI18n, type KnowledgeLabels } from "@cjlapao/ui-kit";
import { Alert, Input, Select, Toggle } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import { useLearningsBoard, useLearningsMap } from "./memoryRpc";
import type { MemoryFilterDraft, MemoryRow, Vocabulary } from "./memoryModel";
import { daemonMapOf, layoutOf, recordsOf, relationshipsOf, semanticGraphOf } from "./knowledgeModel";
import { MemoryDrawer } from "./MemoryDrawer";

/**
 * MemoryBoard — the daemon-side dials (S52 T6, LM6/LM10) above the knowledge explorer. The dials own
 * INPUTS and nothing else: the draft travels up, the section composes `learnings.board` PARAMS from
 * it, and the DAEMON filters — a board that swept a fetched list would be a second, smaller truth
 * about what memory matches. The explorer's own search and filters (type, status, main tag) only
 * narrow what is already on the screen. Every dial word is the vocabulary's: a word outside the
 * daemon's closed set is not selectable.
 */
export interface MemoryBoardProps {
  draft: MemoryFilterDraft;
  vocabulary: Vocabulary | undefined;
  onDraftChange: (next: MemoryFilterDraft) => void;
  children?: ReactNode;
}

export function MemoryBoard({ draft, vocabulary, onDraftChange, children }: MemoryBoardProps) {
  const { t } = useI18n();
  const set = (patch: Partial<MemoryFilterDraft>) => onDraftChange({ ...draft, ...patch });
  return (
    <div className="flex min-h-[30rem] flex-1 flex-col gap-3" data-testid="memory-board">
      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="memory-filters">
        <div className="w-44 shrink-0">
          <Select variant="ghost" size="md" value={draft.scope} aria-label={t("settings.memory.filters.scope")} data-testid="memory-filter-scope" onChange={(e) => set({ scope: e.target.value })}>
            <option value="">{t("settings.memory.filters.scope")}: {t("settings.memory.filters.any")}</option>
            {(vocabulary?.scopes ?? []).map((scope) => <option key={scope} value={scope}>{scope}</option>)}
          </Select>
        </div>
        <div className="w-48 shrink-0">
          <Select variant="ghost" size="md" value={draft.provenance} aria-label={t("settings.memory.filters.provenance")} data-testid="memory-filter-provenance" onChange={(e) => set({ provenance: e.target.value })}>
            <option value="">{t("settings.memory.filters.provenance")}: {t("settings.memory.filters.any")}</option>
            {(vocabulary?.provenances ?? []).map((provenance) => <option key={provenance} value={provenance}>{provenance}</option>)}
          </Select>
        </div>
        <div className="w-40 shrink-0"><Input variant="ghost" value={draft.tag} placeholder={t("settings.memory.filters.tag")} aria-label={t("settings.memory.filters.tag")} data-testid="memory-filter-tag" onChange={(e) => set({ tag: e.target.value })} /></div>
        <div className="w-40 shrink-0"><Input variant="ghost" value={draft.agent} placeholder={t("settings.memory.filters.agent")} aria-label={t("settings.memory.filters.agent")} data-testid="memory-filter-agent" onChange={(e) => set({ agent: e.target.value })} /></div>
        <div className="w-48 shrink-0"><Input variant="ghost" value={draft.project} placeholder={t("settings.memory.filters.project")} aria-label={t("settings.memory.filters.project")} data-testid="memory-filter-project" onChange={(e) => set({ project: e.target.value })} /></div>
        <div className="w-44 shrink-0"><Input variant="ghost" value={draft.loop} placeholder={t("settings.memory.filters.loop")} aria-label={t("settings.memory.filters.loop")} data-testid="memory-filter-loop" onChange={(e) => set({ loop: e.target.value })} /></div>
        <div data-testid="memory-filter-include-retracted">
          <Toggle size="sm" aria-label={t("settings.memory.filters.includeRetracted")} label={t("settings.memory.filters.includeRetractedShort")} checked={draft.includeRetracted} onChange={(e) => set({ includeRetracted: e.target.checked })} />
        </div>
      </div>
      {children}
    </div>
  );
}

/** The labels the kit explorer prints, from our catalogs — so the map speaks the app's language and
 *  says what the memories ARE here, not what the kit's demo data was. */
function useKnowledgeLabels(): KnowledgeLabels {
  const { t } = useI18n();
  return useMemo(() => {
    const k = (key: string) => t(`settings.memory.kit.${key}`);
    return {
      library: k("library"), map: k("map"), details: k("details"), search: k("search"), kind: k("kind"), status: k("status"),
      topic: k("topic"), all: k("all"), cards: k("cards"), list: k("list"), empty: k("empty"), noResults: k("noResults"),
      selectMemory: k("selectMemory"), sources: k("sources"), noSources: k("noSources"), related: k("related"),
      topicLayout: k("topicLayout"), projectionNote: k("projectionNote"), relationships: k("relationships"), ungrouped: k("ungrouped"),
      "kinds.lesson": k("kinds.lesson"), "kinds.decision": k("kinds.decision"), "kinds.convention": k("kinds.convention"),
      "kinds.pitfall": k("kinds.pitfall"), "kinds.code": k("kinds.code"), "kinds.documentation": k("kinds.documentation"),
      "statuses.active": k("statuses.active"), "statuses.needs-review": k("statuses.needs-review"),
      "statuses.superseded": k("statuses.superseded"), "statuses.archived": k("statuses.archived"),
    } satisfies KnowledgeLabels;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t]);
}

/** The element's own height, so the canvas map can take the room the page has instead of a fixed 360. */
function useBoxHeight(): [React.RefObject<HTMLDivElement | null>, number] {
  const ref = useRef<HTMLDivElement | null>(null);
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const update = () => setHeight(node.clientHeight);
    update();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  return [ref, height];
}

/**
 * The board's data half: it asks the daemon for THESE filters, hands the rows it answers to the kit's
 * `KnowledgeExplorer` (library · map · detail), and reports the fresh rows up so the open memory never
 * freezes on a stale snapshot. Nothing is swept or hidden here; retracted rows appear only when the
 * person turned the dial (LM2). The detail region is ours: the kit's `MemoryDetail` for the text, the
 * sources and the links, then the acts and the audit career the plane owns.
 */
export function MemoryBoardRows({ draft, vocabulary, controller, live, canWrite, selectedId, onSelect, onRows, onChanged }: {
  draft: MemoryFilterDraft;
  vocabulary: Vocabulary | undefined;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  selectedId: number | null;
  onSelect: (id: number | null) => void;
  onRows: (rows: MemoryRow[]) => void;
  onChanged: () => void;
}) {
  const { t } = useI18n();
  const board = useLearningsBoard(draft, controller, live);
  const mapAnswer = useLearningsMap(draft, controller, live);
  const labels = useKnowledgeLabels();
  const [boxRef, boxHeight] = useBoxHeight();

  useEffect(() => {
    if (board.data !== undefined) onRows(board.data);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [board.data]);

  const rows = useMemo(() => board.data ?? [], [board.data]);
  const daemonMap = useMemo(() => daemonMapOf(mapAnswer.data), [mapAnswer.data]);
  // Positioned by MEANING when the daemon has stored vectors to project (its points, its nearest-neighbour
  // links); otherwise by the tags and loops the learnings share, which is all a row can honestly say alone.
  const graph = useMemo(() => {
    const derived = relationshipsOf(rows);
    const base = recordsOf(rows, derived);
    if (daemonMap?.projected === true) {
      const sem = semanticGraphOf(daemonMap, base);
      return { records: recordsOf(rows, sem.relationships), points: sem.points, relationships: sem.relationships, semantic: true };
    }
    return { records: base, points: layoutOf(base, derived), relationships: derived, semantic: false };
  }, [rows, daemonMap]);
  const { records, points, relationships } = graph;
  const byId = useMemo(() => new Map(records.map((record) => [record.id, record])), [records]);
  const mapHeight = Math.max(300, Math.round(boxHeight * 0.5));

  return (
    <div className="flex min-h-[28rem] flex-1 flex-col gap-2" data-testid="memory-rows">
      {board.error !== undefined && <Alert className="shrink-0" intent="danger" title={t("settings.memory.board.failed")} description={board.error} data-testid="memory-board-error" />}
      {/* What the positions are, in the daemon's own words when it placed them by meaning; ours when it could not. */}
      <p className="shrink-0 text-xs opacity-70" data-testid="memory-map-note">
        {graph.semantic ? daemonMap?.basis : t("settings.memory.mapNote")}
      </p>
      {daemonMap !== undefined && !graph.semantic && daemonMap.basis !== "" && <p className="shrink-0 text-xs opacity-60" data-testid="memory-map-basis">{daemonMap.basis}</p>}
      {daemonMap !== undefined && daemonMap.canEmbed && daemonMap.embedded < daemonMap.total && (
        <p className="shrink-0 text-xs opacity-60" data-testid="memory-map-pending">{t("settings.memory.pendingHint")}</p>
      )}
      <div ref={boxRef} className="aeon-knowledge min-h-[26rem] flex-1" data-testid="memory-explorer">
        <KnowledgeExplorer
          memories={records}
          points={points}
          relationships={relationships}
          labels={labels}
          loading={board.data === undefined && board.loading}
          regions={{ trace: false }}
          selectedId={selectedId === null ? null : String(selectedId)}
          onSelectionChange={(id) => onSelect(id === null ? null : Number(id))}
          renderRegion={{
            map: (ctx) => (
              <KnowledgeMap
                labels={labels}
                memories={records}
                points={points}
                relationships={relationships}
                height={mapHeight}
                selectedId={ctx.selectedId}
                onSelectionChange={ctx.selectMemory}
                highlightedIds={ctx.query || ctx.filters.kind || ctx.filters.status ? undefined : []}
                selectedTopic={ctx.filters.topic}
                onTopicSelect={(topic) => ctx.setFilters({ ...ctx.filters, topic: topic ?? undefined })}
              />
            ),
            detail: (ctx) => {
              const memory = ctx.selectedMemory;
              const row = memory ? rows.find((candidate) => String(candidate.id) === memory.id) : undefined;
              const related = (memory?.relatedIds ?? []).flatMap((id) => {
                const record = byId.get(id);
                return record ? [record] : [];
              });
              return (
                <div className="space-y-4" data-testid="memory-detail-region">
                  <MemoryDetail labels={labels} memory={memory} relatedMemories={related} onRelatedSelect={ctx.selectMemory} />
                  {row !== undefined && (
                    <MemoryDrawer key={row.id} row={row} vocabulary={vocabulary} controller={controller} live={live} canWrite={canWrite} onChanged={() => { board.refetch(); onChanged(); }} />
                  )}
                </div>
              );
            },
          }}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="memory-count">{t("settings.memory.count", { shown: rows.length })}</div>
    </div>
  );
}
