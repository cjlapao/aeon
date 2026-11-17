import { DetachedNote } from "../../components/DetachedNote";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../lib/bridge";
import { ErrorPanel } from "../../components/ErrorPanel";
import { METHOD_ROADMAP_LIST, ROADMAP_STATUSES, type RoadmapItemView, type RoadmapListResult } from "../../lib/protocol";
import { STATUS_LABELS, estimateWords, priorityWords } from "./render";
import type { LiveController, LiveState } from "../../lib/live";
import { BoardFilters } from "./BoardFilters";
import { DispatchSettings } from "./DispatchSettings";
import { AnswerFacts, ItemDrawer } from "./ItemDrawer";
import { boardIsLoadable, emptyFilterDraft, ideaField, roadmapIdeaProposals, type BoardRow, type FilterDraft } from "./roadmapModel";
import { commitRoadmapIdea, readProposal, rejectRoadmapIdea, useProposedIdeas, useRoadmapBoard, useRoadmapVocabulary } from "./roadmapRpc";

/**
 * The forward Gantt (S26 T6) — the same framing the sketches locked: history
 * occupies the past, this occupies the future, and the present is wherever a
 * loop is actually running (the loop surfaces own that; this page never
 * pretends to).
 *
 * # Lanes: by epic, by priority — never by agent
 *
 * design.md's locked decision: the agent lane is a built-loop concept this
 * stage's items mostly do not have yet; epic and priority are what a
 * not-yet-built item actually carries. The lane axis is a toggle, not a
 * second page.
 *
 * # The honest words are the shared ones
 *
 * `unknown` priority renders the WORD, an estimate renders `min–max (basis)`
 * or its explicit absence, provenance renders where the item came from —
 * the same sentences the bench and the CLI print, imported from `render.ts`
 * (one definition; a second copy is the slice's forbidden second truth).
 *
 * # What this page does NOT do (the standing §4b rule)
 *
 * It reads: `roadmap.list` through the generic `rpc()` bridge and nothing
 * else — no writes, no dispatches, no new Tauri command. The CRUD and the
 * rung buttons live on the bench tab (`test-bench/pages/RoadmapPage`);
 * the app's designed write surfaces are S9's to design. And one component
 * truth, recorded not hidden: the S25 lane/Gantt component this task was
 * written to reuse is **not in this tree** — the base's wip commit deleted
 * the history feature and another agent owns its restoration; when it
 * returns, this page's lane rendering is meant to swap onto it. Until then
 * the Gantt is composed here from the kit, honestly labelled.
 *
 * # Extended at S50 T7 — the management half joins, the Gantt stays
 *
 * Everything above the fold is UNCHANGED S26 behaviour (the header comment
 * above is its own record, kept as written). Below it joins the management
 * surface: the board with the daemon's sentences, the item drawer, the
 * dispatch-settings card and the roadmap-idea proposals strip — every door
 * through `roadmapRpc.ts` (the one file with method literals), every
 * status/limit/authority word from `roadmap.vocabulary` or the answers
 * themselves. The claim two paragraphs up — "no writes" — was true of the
 * Gantt half and is now the Gantt half's only scope: the manage half below
 * writes through the human doors, behind the daemon's own authority
 * statement, and the guard test pins every door it may ever reach.
 */

type LaneAxis = "epic" | "priority";

const PRIORITY_BANDS: readonly string[] = ["known", "assumed", "unknown"];

export interface RoadmapPageProps {
  /** The shell's live pair. The Gantt half never needs them (it asks on a
   *  click through the plain bridge); the management half mounts only when
   *  the route supplies them — which the real route does. A mount without
   *  them is the S26 page, unchanged. */
  controller?: LiveController;
  live?: LiveState;
  canWrite?: boolean;
  /** Start on this component root (the Work page's scope); the board loads at once. */
  initialRoot?: string;
}

export function RoadmapPage({ controller, live, canWrite, initialRoot = "" }: RoadmapPageProps) {
  const [root, setRoot] = useState(initialRoot);
  const [items, setItems] = useState<RoadmapItemView[] | null>(null);
  const [laneAxis, setLaneAxis] = useState<LaneAxis>("epic");
  const [error, setError] = useState<RpcError | null>(null);

  const load = useCallback(async () => {
    if (!root.trim()) return;
    try {
      setError(null);
      const board = await rpc<RoadmapListResult>(METHOD_ROADMAP_LIST, { componentRoot: root.trim() });
      setItems(board.items);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [root]);

  useEffect(() => {
    setItems(null);
  }, [root]);

  // Opened from the Work page with a component picked: look at its future without a click.
  useEffect(() => {
    if (initialRoot) void load();
  }, [initialRoot, load]);

  // The door returns the Gantt order (status rung, then created); lanes
  // group it — the page sorts nothing else.
  const lanes = useMemo(() => {
    if (items === null) return null;
    const groups = new Map<string, RoadmapItemView[]>();
    const keyFor = (item: RoadmapItemView): string =>
      laneAxis === "epic" ? (item.epic ? `epic: ${item.epic}` : "no epic (a tag, not a row — R2)") : `priority ${item.priorityState}`;
    for (const item of items) {
      const key = keyFor(item);
      const bucket = groups.get(key);
      if (bucket) bucket.push(item);
      else groups.set(key, [item]);
    }
    const ordered = [...groups.entries()];
    if (laneAxis === "priority") {
      // known first, assumed next, unknown LAST and visible — never hidden
      // behind the stated ones, which is how an implied-low creeps back in.
      ordered.sort((a, b) => PRIORITY_BANDS.indexOf(a[0].replace("priority ", "")) - PRIORITY_BANDS.indexOf(b[0].replace("priority ", "")));
    } else {
      ordered.sort((a, b) => a[0].localeCompare(b[0]));
    }
    return ordered;
  }, [items, laneAxis]);

  return (
    <div className="stack">
      {error && <ErrorPanel error={error} onRetry={() => void load()} />}

      <Panel tone="slate" data-testid="roadmap-gantt">
        <FormField label="Project (the component root whose roadmap to look at)">
          <Input value={root} onChange={(e) => setRoot(e.target.value)} placeholder="/srv/projects/ledger" aria-label="component root" />
        </FormField>
        <FormField label="Lanes (the locked decision — by epic or by priority; by-agent is a built-loop concept items here mostly do not have yet)">
          <Select value={laneAxis} onChange={(e) => setLaneAxis(e.target.value as LaneAxis)} aria-label="lane axis">
            <option value="epic">by epic</option>
            <option value="priority">by priority</option>
          </Select>
        </FormField>
        <Button onClick={() => void load()} disabled={!root.trim()} data-testid="load-gantt">
          Look at the future
        </Button>

        {items === null ? (
          <Alert tone="slate">Nothing loaded yet — the roadmap is per component root; name one.</Alert>
        ) : items.length === 0 ? (
          // D8: the empty board speaks, and says what does not happen.
          <Alert tone="slate">
            The roadmap of {root.trim()} is EMPTY — and loop.start never asked about any of this.
          </Alert>
        ) : (
          lanes?.map(([lane, laneItems]) => (
            <section key={lane} data-testid={`lane-${laneAxis}`} data-lane={lane}>
              <h3>{lane}</h3>
              {laneItems.map((item) => (
                <div key={item.id} className="roadmap-bar" data-status={item.status} data-priority={item.priorityState} data-testid={`bar-${item.id}`}>
                  {/* The bar sits on its rung; eight distinct labels, asserted distinct by test. */}
                  <Pill data-testid={`status-${item.id}`}>{STATUS_LABELS[item.status]}</Pill>{" "}
                  <strong>#{item.id} {item.title}</strong>{" "}
                  <span data-testid={`priority-${item.id}`} data-state={item.priorityState}>{priorityWords(item)}</span>{" "}
                  <span data-testid={`estimate-${item.id}`} className={item.estimateMin === null ? "text-muted" : undefined}>
                    {estimateWords(item)}
                  </span>{" "}
                  <span data-testid={`provenance-${item.id}`}>from {item.provenance}</span>{" "}
                  {item.loopSlug && <span>loop={item.loopSlug}</span>}
                  {<DetachedNote kind="roadmap" id={String(item.id)} />}
                </div>
              ))}
            </section>
          ))
        )}
        {items !== null && items.length > 0 && (
          <p className="text-muted">
            the rungs: {ROADMAP_STATUSES.join(" → ")} — this is a view, not an editor; the bench tab holds the doors.
          </p>
        )}
      </Panel>

      {controller !== undefined && live !== undefined && (
        <RoadmapManage controller={controller} live={live} canWrite={canWrite === true} initialRoot={initialRoot} />
      )}
    </div>
  );
}

// ── S50 T7 · the management half ───────────────────────────────────────────
//
// The board, the drawer host, the settings card and the proposals strip.
// Everything state-shaped here reads from `roadmap.vocabulary` or from the
// answers themselves — no status word, limit or authority sentence is this
// file's invention, and `roadmapRpc.guard.test.ts` pins every door below
// to the registered set.

function RoadmapManage({ controller, live, canWrite, initialRoot = "" }: { controller: LiveController; live: LiveState; canWrite: boolean; initialRoot?: string }) {
  const { t } = useI18n();
  const vocabulary = useRoadmapVocabulary(controller, live);
  const [draft, setDraft] = useState<FilterDraft>({ ...emptyFilterDraft(), componentRoot: initialRoot });
  const [openItem, setOpenItem] = useState<number | null>(null);
  // A board refresh after a write: re-mounting the panel re-runs its
  // reader — the daemon's answer, not a locally patched copy.
  const [refresh, setRefresh] = useState(0);

  if (live.state !== "current") {
    // Three states, three shapes: reconnecting asks nothing (S49's rule).
    return (
      <Panel tone="slate" data-testid="roadmap-manage">
        <Alert intent="warning" title={t("roadmap.manage.reconnecting")} data-testid="roadmap-manage-reconnecting" />
      </Panel>
    );
  }

  return (
    <div className="stack" data-testid="roadmap-manage">
      <Panel tone="slate" data-testid="roadmap-manage-board">
        <h3>{t("roadmap.manage.boardTitle")}</h3>
        {vocabulary.error !== undefined && <Alert intent="danger" title={vocabulary.error} data-testid="board-vocabulary-error" />}
        <BoardFilters draft={draft} vocabulary={vocabulary.data} onChange={setDraft} />
        {boardIsLoadable(draft) ? (
          <BoardRows key={refresh} draft={draft} controller={controller} live={live} onOpen={setOpenItem} />
        ) : (
          <p className="text-sm opacity-70" data-testid="board-needs-root">{t("roadmap.manage.needsRoot")}</p>
        )}
      </Panel>

      <Panel tone="slate" data-testid="roadmap-manage-settings">
        <h3>{t("roadmap.manage.settingsTitle")}</h3>
        <DispatchSettings controller={controller} live={live} canWrite={canWrite} />
      </Panel>

      <ProposalsStrip controller={controller} live={live} canWrite={canWrite} />

      {openItem !== null && (
        <ItemDrawer
          itemId={openItem}
          vocabulary={vocabulary.data}
          controller={controller}
          live={live}
          canWrite={canWrite}
          onClose={() => setOpenItem(null)}
          onChanged={() => setRefresh((n) => n + 1)}
        />
      )}
    </div>
  );
}

/** Board rows as the daemon answered them — `describe` verbatim, the
 *  blocker sentence verbatim when the row is not ready, the loop the item
 *  is pinned to when there is one. A click opens the drawer; the row makes
 *  no state change of its own. */
function BoardRows({
  draft,
  controller,
  live,
  onOpen,
}: {
  draft: FilterDraft;
  controller: LiveController;
  live: LiveState;
  onOpen: (id: number) => void;
}) {
  const { t } = useI18n();
  const board = useRoadmapBoard(draft, controller, live);

  if (board.error !== undefined) {
    return <Alert intent="danger" title={board.error} data-testid="board-error" />;
  }
  if (board.data === undefined) {
    return <p className="text-sm opacity-60" data-testid="board-loading">{board.loading ? t("roadmap.manage.boardLoading") : ""}</p>;
  }
  const groups = Object.entries(board.data.groups);
  return (
    <div className="space-y-2" data-testid="board-rows">
      {groups.length > 0 && (
        <div className="flex flex-wrap gap-1.5" data-testid="board-groups">
          {groups.map(([word, count]) => (
            // The daemon's per-status counts, words and numbers as answered.
            <Pill key={word} tone="slate">{word}: {count}</Pill>
          ))}
        </div>
      )}
      {board.data.rows.length === 0 && (
        <p className="text-sm opacity-70" data-testid="board-empty">{t("roadmap.manage.boardEmpty")}</p>
      )}
      {board.data.rows.map((row: BoardRow) => (
        <button
          key={row.item.id}
          type="button"
          className="block w-full rounded-lg border border-slate-200 p-2 text-left hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-800/40"
          data-testid={`board-row-${row.item.id}`}
          data-ready={row.ready}
          onClick={() => onOpen(row.item.id)}
        >
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="font-mono opacity-70">#{row.item.id}</span>
            <strong>{row.item.title}</strong>
            <Pill tone="slate">{row.item.status}</Pill>
            {row.ready ? (
              <Pill tone="green" data-testid={`board-ready-${row.item.id}`}>{t("roadmap.manage.ready")}</Pill>
            ) : (
              <Pill tone="amber" data-testid={`board-blocked-${row.item.id}`}>{t("roadmap.manage.notReady")}</Pill>
            )}
            {row.loop !== undefined && (
              <Pill tone="purple" data-testid={`board-loop-${row.item.id}`}>{row.loop.slug} · {row.loop.status}</Pill>
            )}
          </div>
          {/* The daemon's describe sentence, byte for byte (RM6). */}
          <p className="pt-1 text-xs opacity-80" data-testid={`board-describe-${row.item.id}`}>{row.describe}</p>
          {!row.ready && row.blocker !== undefined && (
            // The blocker sentence verbatim — the daemon names the missing
            // thing; a paraphrase here would be a second opinion.
            <p className="pt-1 text-xs text-amber-700 dark:text-amber-400" data-testid={`board-blocker-${row.item.id}`}>
              {row.blocker}
            </p>
          )}
        </button>
      ))}
    </div>
  );
}

/** The shared desk narrowed to `roadmap_idea` (S45's generic doors): commit
 *  carries the digest the person READ (`proposal.get` first — S45's PM7
 *  discipline), reject REQUIRES a reason (the door refuses without one, so
 *  the control stops it before the call), and every refusal renders whole. */
export function ProposalsStrip({ controller, live, canWrite }: { controller: LiveController; live: LiveState; canWrite: boolean }) {
  const { t } = useI18n();
  const list = useProposedIdeas(controller, live);
  const proposals = roadmapIdeaProposals((list.data as { proposals?: unknown } | undefined)?.proposals);
  const [reasons, setReasons] = useState<Record<number, string>>({});
  const [busyId, setBusyId] = useState<number | null>(null);
  const [answers, setAnswers] = useState<Record<number, unknown>>({});
  const [refusals, setRefusals] = useState<Record<number, string>>({});

  async function commit(id: number) {
    setBusyId(id);
    setRefusals((r) => ({ ...r, [id]: "" }));
    try {
      const read = await readProposal(id);
      const digest = (read as { digest?: unknown }).digest;
      if (typeof digest !== "string") {
        // No digest to carry means no honest commit is possible — said
        // plainly, not papered over with an empty one.
        setRefusals((r) => ({ ...r, [id]: t("roadmap.manage.proposals.noDigest") }));
        return;
      }
      const committed = await commitRoadmapIdea(id, digest);
      setAnswers((a) => ({ ...a, [id]: committed }));
      list.refetch();
    } catch (cause) {
      setRefusals((r) => ({ ...r, [id]: toRpcError(cause).message }));
    } finally {
      setBusyId(null);
    }
  }

  async function reject(id: number) {
    const reason = (reasons[id] ?? "").trim();
    if (reason === "") return; // the door refuses without one; so do we.
    setBusyId(id);
    setRefusals((r) => ({ ...r, [id]: "" }));
    try {
      const rejected = await rejectRoadmapIdea(id, reason);
      setAnswers((a) => ({ ...a, [id]: rejected }));
      list.refetch();
    } catch (cause) {
      setRefusals((r) => ({ ...r, [id]: toRpcError(cause).message }));
    } finally {
      setBusyId(null);
    }
  }

  return (
    <Panel tone="slate" data-testid="roadmap-proposals">
      <h3>{t("roadmap.manage.proposals.title")}</h3>
      {list.error !== undefined && <Alert intent="danger" title={list.error} data-testid="proposals-error" />}
      {list.data !== undefined && proposals.length === 0 && (
        <p className="text-sm opacity-70" data-testid="proposals-empty">{t("roadmap.manage.proposals.empty")}</p>
      )}
      {proposals.map((proposal) => {
        const reason = reasons[proposal.id] ?? "";
        return (
          <div key={proposal.id} className="rounded-lg border border-slate-200 p-2 dark:border-slate-700" data-testid={`proposal-row-${proposal.id}`}>
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="font-mono opacity-70">#{proposal.id}</span>
              {/* The agent's OWN payload words, verbatim. */}
              <strong data-testid={`proposal-title-${proposal.id}`}>{ideaField(proposal, "title")}</strong>
              {ideaField(proposal, "epic") !== "" && <Pill tone="slate">{ideaField(proposal, "epic")}</Pill>}
              <span className="opacity-60">{proposal.createdByKind}:{proposal.createdById}</span>
            </div>
            {ideaField(proposal, "note") !== "" && (
              <p className="pt-1 text-xs opacity-80" data-testid={`proposal-note-${proposal.id}`}>{ideaField(proposal, "note")}</p>
            )}
            <div className="flex flex-wrap items-center gap-2 pt-2">
              <Button size="sm" disabled={!canWrite || busyId === proposal.id} data-testid={`proposal-commit-${proposal.id}`} onClick={() => void commit(proposal.id)}>
                {t("roadmap.manage.proposals.commit")}
              </Button>
              <Input
                className="max-w-52"
                value={reason}
                placeholder={t("roadmap.manage.proposals.reasonPlaceholder")}
                aria-label={t("roadmap.manage.proposals.reasonPlaceholder")}
                data-testid={`proposal-reason-${proposal.id}`}
                onChange={(e) => setReasons((r) => ({ ...r, [proposal.id]: e.target.value }))}
              />
              <Button size="sm" variant="soft" disabled={!canWrite || busyId === proposal.id || reason.trim() === ""} data-testid={`proposal-reject-${proposal.id}`} onClick={() => void reject(proposal.id)}>
                {t("roadmap.manage.proposals.reject")}
              </Button>
            </div>
            {refusals[proposal.id] !== undefined && refusals[proposal.id] !== "" && (
              <Alert intent="danger" title={refusals[proposal.id]} data-testid={`proposal-refusal-${proposal.id}`} />
            )}
            {answers[proposal.id] !== undefined && <AnswerFacts answer={answers[proposal.id]} />}
          </div>
        );
      })}
    </Panel>
  );
}
